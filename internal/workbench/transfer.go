package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/zmodem"
)

// File transfer over the terminal's own stream (ZMODEM): a program on the other
// end, usually sz or rz, starts it, and the terminal answers. Nothing moves until
// the user has agreed and chosen where files go or which files to send, and the
// other end never picks a path: names it sends are reduced to a file name inside
// the folder the user chose.

// transferHits is the place of the controls this file paints.
var transferHits = tree.Feature("transfer")

var (
	hitTransferDownloads = tree.ID(transferHits, "transfer-downloads")
	hitTransferChoose    = tree.ID(transferHits, "transfer-choose")
	hitTransferDecline   = tree.ID(transferHits, "transfer-decline")
	hitTransferClose     = tree.ID(transferHits, "transfer-close")
	hitTransferDismiss   = tree.ID(transferHits, "transfer-dismiss")
	hitTransferCancel    = tree.ID(transferHits, "transfer-cancel")
	// hitTransferCancelFor is one cancel button per session, so two progress
	// cards do not share a click.
	hitTransferCancelFor = tree.Sub(transferHits, tree.Name("transfer-cancel-for"))
)

// maxHeldTransferBytes bounds what is kept while the user decides: a sender
// repeats its first header until it is answered, so little is needed.
const maxHeldTransferBytes = 1 << 20

// transferQuiet is how long a running transfer may hear nothing before the
// engine is told to ask again.
const transferQuiet = 5 * time.Second

// maxSendFiles bounds one upload; maxSendSize is what a 32-bit position can name.
const (
	maxSendFiles = 200
	maxSendSize  = 1<<32 - 1
)

type transferPhase uint8

const (
	transferNone transferPhase = iota
	transferOffered
	transferRunning
	transferClosing // the transfer is over; the sender's closing "OO" is still to come
)

// fileTransfer is one session's transfer. A window runs one per terminal.
type fileTransfer struct {
	phase     transferPhase
	sessionID uint32
	sending   bool
	// held is what the other end has sent since it began, kept for the engine.
	held       []byte
	engine     zmodem.Engine
	lastActive time.Duration
	closedAt   time.Duration
	// out receives into a folder; sources are the files being sent.
	target  *folderTarget
	sources []*os.File
	// progress is what the card shows, refreshed as bytes move.
	progress    zmodem.Progress
	dialogError string
	// sizeText and doneText are built when the numbers change, not per paint.
	text transferText
}

type transferText struct {
	done, size int64
	line       string
}

// transferBusy reports whether a file transfer owns this shell, whether the
// remote program is still being started or bytes are already moving. An armed
// rz/sz job holds the shell the same way a running engine does.
func (session *Session) transferBusy() bool {
	return session.transfer.phase != transferNone || session.rzsz != nil
}

// zmodem signatures of the first header each side sends: a sender asks to send
// (ZRQINIT), a receiver says it is ready (ZRINIT).
var (
	zmodemSignatureSend    = []byte("**\x18B00")
	zmodemSignatureReceive = []byte("**\x18B01")
)

// splitTransferStart looks for the start of a transfer in data, with the last
// bytes of the output before it in tail. It returns where the transfer begins in
// data (or -1), and whether this is a receive (the other end sends).
func splitTransferStart(tail, data []byte) (index int, receiving bool) {
	combined := make([]byte, 0, len(tail)+len(data))
	combined = append(append(combined, tail...), data...)
	if at := bytes.Index(combined, zmodemSignatureSend); at >= 0 {
		receiving, index = true, at
	} else if at := bytes.Index(combined, zmodemSignatureReceive); at >= 0 {
		receiving, index = false, at
	} else {
		return -1, false
	}
	if index < len(tail) {
		index = 0 // the first bytes were already shown; the rest of the header starts here
	} else {
		index -= len(tail)
	}
	return index, receiving
}

// scanTransfer is run on shell output before it reaches the screen. It returns
// the part that belongs to the screen; the rest has been taken by a transfer.
func (state *State) scanTransfer(session *Session, rx []byte) []byte {
	t := session.term
	transfer := &session.transfer
	if transfer.phase != transferNone {
		return state.feedTransfer(session, rx)
	}
	// Every signature has a ZDLE in it, so output without one (nearly all of it)
	// costs one scan and nothing else. Another tab's transfer does not hide a
	// header on this one: each shell has its own stream.
	index, receiving := -1, false
	if bytes.IndexByte(rx, 0x18) >= 0 || bytes.IndexByte(t.zmodemTail, 0x18) >= 0 {
		index, receiving = splitTransferStart(t.zmodemTail, rx)
	}
	if index < 0 {
		// Keep the last bytes in case a header is cut between two reads.
		const keep = 5
		if len(rx) >= keep {
			t.zmodemTail = append(t.zmodemTail[:0], rx[len(rx)-keep:]...)
		} else {
			t.zmodemTail = append(t.zmodemTail, rx...)
			if extra := len(t.zmodemTail) - keep; extra > 0 {
				t.zmodemTail = append(t.zmodemTail[:0], t.zmodemTail[extra:]...)
			}
		}
		return rx
	}
	t.zmodemTail = t.zmodemTail[:0]
	*transfer = fileTransfer{phase: transferOffered, sessionID: session.ID, sending: !receiving}
	transfer.held = append(transfer.held, rx[index:]...)
	if state.startArmedRZSZ(session, receiving) {
		return rx[:index]
	}
	state.showNextPrompt()
	return rx[:index]
}

// feedTransfer routes output to the transfer of its session.
func (state *State) feedTransfer(session *Session, rx []byte) []byte {
	transfer := &session.transfer
	switch transfer.phase {
	case transferOffered:
		if len(transfer.held)+len(rx) > maxHeldTransferBytes {
			state.declineTransfer(session)
			return nil
		}
		transfer.held = append(transfer.held, rx...)
		return nil
	case transferRunning, transferClosing:
		transfer.lastActive = state.now()
		transfer.engine.Feed(rx)
		state.afterTransferFeed(session)
		if transfer.phase == transferNone {
			return state.takeTransferLeftover(session)
		}
	}
	return nil
}

// afterTransferFeed sends what the engine produced and notices its end.
func (state *State) afterTransferFeed(session *Session) {
	transfer := &session.transfer
	engine := transfer.engine
	if out := engine.TakeOutput(nil); len(out) > 0 && session.term.shell != nil {
		session.term.shell.Write(out)
	}
	transfer.progress = engine.Progress()
	state.noteRZSZ(session)
	if engine.Finished() && transfer.phase == transferRunning {
		transfer.phase, transfer.closedAt = transferClosing, state.now()
		state.releaseTransferModal(session)
		state.reportTransfer(session)
	}
	if engine.Settled() {
		state.endTransfer(session)
	}
	// The progress above is the newest the engine has, and the desktop's bar
	// follows this transfer as it follows the file panels'.
	state.reportTransferProgress()
}

// takeTransferLeftover is the output that followed the end of the transfer.
func (state *State) takeTransferLeftover(session *Session) []byte {
	if session == nil || session.term == nil {
		return nil
	}
	leftover := session.term.transferLeftover
	session.term.transferLeftover = nil
	return leftover
}

// reportTransfer tells the terminal and the status line how the transfer ended.
func (state *State) reportTransfer(session *Session) {
	transfer, m := &session.transfer, state.messages.Tools
	engine := transfer.engine
	var line string
	switch err := engine.Err(); {
	case err == nil && transfer.sending:
		line = fmt.Sprintf(m.TransferSentFmt, engine.Progress().Files)
	case err == nil:
		line = fmt.Sprintf(m.TransferReceivedFmt, engine.Progress().Files, transfer.target.dir)
	case errors.Is(err, zmodem.ErrCancelled):
		line = m.TransferCancelled
	default:
		line = fmt.Sprintf(m.TransferFailedFmt, err)
	}
	if engine.Err() != nil && transfer.target != nil {
		transfer.target.discard()
	}
	session.term.say(sgrMuted, "\r\n"+line)
	state.status = line
}

// endTransfer lets go of everything a transfer held.
func (state *State) endTransfer(session *Session) {
	if session == nil {
		return
	}
	state.releaseTransferInput(session)
	state.finishRZSZ(session)
	// Clear this session before the next offer is chosen. A dialog that was
	// waiting on another tab must not be handed back to the transfer that just
	// ended, and closing that tab must not dismiss the one that takes its place.
	transfer := session.transfer
	session.transfer = fileTransfer{}
	if transfer.engine != nil && session.term != nil {
		session.term.transferLeftover = append(session.term.transferLeftover[:0], transfer.engine.Leftover()...)
		transfer.engine.Close()
	}
	if transfer.target != nil {
		transfer.target.close()
	}
	for _, file := range transfer.sources {
		file.Close()
	}
	state.releaseTransferModal(session)
	// A transfer can end without another byte arriving (declined, abandoned,
	// or cancelled), and the desktop's bar has to hear about that too.
	state.reportTransferProgress()
}

// pumpTransfer is run on every wake: it asks again when a transfer has gone
// quiet, and stops waiting for a closing "OO" that never comes.
func (state *State) pumpTransfer() {
	state.watchRZSZ()
	for index := range state.sessions {
		state.pumpSessionTransfer(&state.sessions[index])
	}
}

func (state *State) pumpSessionTransfer(session *Session) {
	transfer := &session.transfer
	if transfer.phase == transferNone {
		return
	}
	if session.term == nil || session.term.shell == nil {
		state.abandonTransfer(session)
		return
	}
	now := state.now()
	switch transfer.phase {
	case transferRunning:
		if now-transfer.lastActive >= transferQuiet {
			transfer.lastActive = now
			transfer.engine.Timeout()
			state.afterTransferFeed(session)
		}
	case transferClosing:
		if now-transfer.closedAt >= time.Second {
			state.endTransfer(session)
		}
	}
}

// abandonTransfer drops a transfer whose terminal has gone.
func (state *State) abandonTransfer(session *Session) {
	if session == nil {
		return
	}
	if session.transfer.target != nil {
		session.transfer.target.discard()
	}
	if session.rzsz != nil && session.rzsz.reason == nil {
		session.rzsz.reason = errors.New("the shell ended")
	}
	state.endTransfer(session)
}

// releaseTransferModal closes the offer dialog when it belongs to this session,
// then shows the next one that is waiting.
func (state *State) releaseTransferModal(session *Session) {
	if state.modal != modalFileTransfer || session == nil || state.modalSession != session.ID {
		return
	}
	state.closeModal()
	state.showNextPrompt()
}

// declineTransfer turns an offer down: the other end is told to stop.
func (state *State) declineTransfer(session *Session) {
	if session != nil && session.term != nil && session.term.shell != nil {
		session.term.shell.Write([]byte("\x18\x18\x18\x18\x18\x18\x18\x18\b\b\b\b\b\b\b\b\b\b"))
	}
	if session != nil && session.term != nil {
		session.term.transferLeftover = nil
	}
	state.endTransfer(session)
	state.status = state.messages.Tools.TransferDeclined
}

// startTransfer begins a transfer with the engine the user's choice made.
func (state *State) startTransfer(session *Session, engine zmodem.Engine) {
	transfer := &session.transfer
	transfer.engine, transfer.phase = engine, transferRunning
	transfer.sessionID = session.ID
	transfer.lastActive = state.now()
	if session.term != nil {
		session.term.transferHoldsInput = true
	}
	// The progress card is not a dialog. Closing this session's offer must not
	// cancel a transfer that belongs to another tab.
	state.releaseTransferModal(session)
	held := transfer.held
	transfer.held = nil
	engine.Feed(newestTransferStart(held, transfer.sending))
	state.afterTransferFeed(session)
}

// releaseTransferInput gives the shell back to the keyboard.
func (state *State) releaseTransferInput(session *Session) {
	if session != nil && session.term != nil {
		session.term.transferHoldsInput = false
	}
}

// clickRunningTransfer is a progress card's cancel. Each card names its session,
// and the single cancel hit still stops the only running transfer.
func (state *State) clickRunningTransfer(id gfx.ID) bool {
	session := state.runningTransferByHit(id)
	if session == nil || session.transfer.engine == nil {
		return false
	}
	session.transfer.engine.Cancel()
	state.afterTransferFeed(session)
	return true
}

func (state *State) runningTransferByHit(id gfx.ID) *Session {
	if n, ok := id.In(hitTransferCancelFor); ok && n > 0 {
		session, ok := state.sessionByIDPointer(uint32(n))
		if !ok || session.transfer.phase != transferRunning {
			return nil
		}
		return session
	}
	if id != hitTransferCancel {
		return nil
	}
	var found *Session
	for index := range state.sessions {
		if state.sessions[index].transfer.phase != transferRunning {
			continue
		}
		if found != nil {
			return nil
		}
		found = &state.sessions[index]
	}
	return found
}

// newestTransferStart keeps only the newest copy of the request that began the
// transfer, dropping the copies that piled up while the user was deciding.
// The other end repeats its request every few seconds until it is answered, and
// every copy but the last has already timed out on its side: answering them all
// tells it the same thing more than once, which a receiver takes for more files
// and a sender takes for readiness to send again, and the transfer loses step
// and waits out the other end's timeout. A request that was cut by the reading
// boundary may be the only copy, so nothing is dropped unless a whole second
// one has arrived.
func newestTransferStart(held []byte, sending bool) []byte {
	signature := zmodemSignatureSend // the other end runs sz: it asks to send
	if sending {
		signature = zmodemSignatureReceive // the other end runs rz: it is ready
	}
	newest := bytes.LastIndex(held, signature)
	if newest <= 0 || bytes.Index(held[:newest], signature) < 0 {
		return held
	}
	return held[newest:]
}

func (state *State) clickTransfer(id gfx.ID) {
	if state.modal != modalFileTransfer {
		return
	}
	session, ok := state.sessionByIDPointer(state.modalSession)
	if !ok || session.transfer.phase != transferOffered {
		state.closeModal()
		return
	}
	transfer := &session.transfer
	switch id {
	case hitTransferDecline, hitTransferClose, hitTransferDismiss:
		state.closeModal()
		state.declineTransfer(session)
		state.showNextPrompt()
	case hitTransferDownloads, hitTransferChoose:
		if transfer.sending {
			state.chooseFilesToSend(session)
		} else {
			state.chooseFolderToReceive(session, id == hitTransferDownloads)
		}
	}
}

func (state *State) chooseFolderToReceive(session *Session, downloads bool) {
	m := state.messages.Tools
	dir := ""
	if downloads {
		dir = state.downloadFolder()
		if dir != "" {
			if err := prepareDownloadDirectory(dir); err != nil {
				session.transfer.dialogError = fmt.Sprintf(m.FilesDownloadDirectoryUnavailableFmt, dir)
				return
			}
		}
	} else {
		chosen, err := state.options.OpenDirectory(app.OpenDirectoryOptions{Title: m.TransferChooseFolderTitle})
		if errors.Is(err, app.ErrCanceled) || err == nil && chosen == "" {
			return // still offered: the user may pick again or decline
		}
		if err != nil {
			session.transfer.dialogError = err.Error()
			return
		}
		dir = chosen
	}
	if dir == "" {
		session.transfer.dialogError = m.TransferNoDownloads
		return
	}
	session.transfer.target = &folderTarget{dir: dir}
	state.closeModal()
	state.startTransfer(session, zmodem.NewReceiver(session.transfer.target))
	state.showNextPrompt()
}

func (state *State) chooseFilesToSend(session *Session) {
	m := state.messages.Tools
	paths, err := state.options.OpenFiles(app.OpenFileOptions{Title: m.TransferChooseFilesTitle, AllowMultiple: true})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		return
	}
	if err != nil {
		session.transfer.dialogError = err.Error()
		return
	}
	if len(paths) > maxSendFiles {
		session.transfer.dialogError = fmt.Sprintf(m.TransferTooManyFmt, maxSendFiles)
		return
	}
	var files []zmodem.SendFile
	var opened []*os.File
	fail := func(message string) {
		for _, file := range opened {
			file.Close()
		}
		session.transfer.dialogError = message
	}
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			fail(err.Error())
			return
		}
		opened = append(opened, file)
		info, err := file.Stat()
		switch {
		case err != nil:
			fail(err.Error())
			return
		case !info.Mode().IsRegular():
			fail(fmt.Sprintf(m.TransferNotFileFmt, filepath.Base(path)))
			return
		case info.Size() > maxSendSize:
			fail(fmt.Sprintf(m.TransferTooBigFmt, filepath.Base(path)))
			return
		}
		files = append(files, zmodem.SendFile{
			Name: filepath.Base(path), Size: info.Size(), ModTime: info.ModTime(), Mode: uint32(info.Mode().Perm()), Source: file,
		})
	}
	session.transfer.sources = opened
	state.closeModal()
	state.startTransfer(session, zmodem.NewSender(files, zmodem.SendOptions{}))
	state.showNextPrompt()
}

// defaultDownloadDirectory is the current account's Downloads folder. It need
// not exist yet: the first download can create it.
func defaultDownloadDirectory() string {
	home, _ := os.UserHomeDir()
	return downloadDirectoryFromPlaces(home, nativeSFTPLocalPlaces())
}

func downloadDirectoryFromPlaces(home string, places []sftpLocalPlace) string {
	for _, place := range places {
		if place.kind == localPlaceDownloads && validSFTPDownloadDirectory(place.path) {
			return filepath.Clean(place.path)
		}
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, "Downloads")
}

// downloadsDirectory is the user's Downloads folder, or empty when there is none.
func downloadsDirectory() string {
	dir := defaultDownloadDirectory()
	if dir == "" {
		return ""
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// downloadFolder is the folder a download is saved in when the user does not
// pick one: the default download folder from the settings, else the system
// Downloads folder. Empty means neither is available.
func (state *State) downloadFolder() string {
	if directory := state.sftpDownloadDirectory; validSFTPDownloadDirectory(directory) {
		return filepath.Clean(directory)
	}
	return downloadsDirectory()
}

// downloadFolderLabel names that folder in the offer's button: its last path
// element, or the whole path when it has none.
func downloadFolderLabel(directory string) string {
	name := filepath.Base(directory)
	if name == "." || name == string(filepath.Separator) {
		return directory
	}
	return name
}

// prepareDownloadDirectory makes directory ready to receive files: it is
// created when missing, and must be a folder afterwards. The settings picker
// only offers folders that exist, but one can be deleted after it was chosen.
func prepareDownloadDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	info, err := os.Stat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return os.ErrInvalid
	}
	return nil
}

// folderTarget writes received files into one folder, never over a file that is
// already there.
type folderTarget struct {
	dir string
	// exact, when set, is the one name the file is written under, replacing a
	// file of that name: the user chose it in a save dialog.
	exact   string
	current *os.File
	partial string
	info    zmodem.FileInfo
}

func (target *folderTarget) Open(info zmodem.FileInfo) (io.WriteCloser, error) {
	target.finish()
	if target.exact != "" {
		path := filepath.Join(target.dir, target.exact)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return nil, err
		}
		target.current, target.partial, target.info = file, path, info
		return &targetFile{target: target, File: file}, nil
	}
	for attempt := 0; attempt < 1000; attempt++ {
		name := info.Name
		if attempt > 0 {
			ext := filepath.Ext(name)
			name = name[:len(name)-len(ext)] + " (" + strconv.Itoa(attempt) + ")" + ext
		}
		path := filepath.Join(target.dir, name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		target.current, target.partial, target.info = file, path, info
		return &targetFile{target: target, File: file}, nil
	}
	return nil, errors.New("too many files with that name")
}

// targetFile counts what it was given, so closing it can tell a finished file
// from one a cancelled transfer left short.
type targetFile struct {
	*os.File
	target  *folderTarget
	written int64
}

func (file *targetFile) Write(p []byte) (int, error) {
	n, err := file.File.Write(p)
	file.written += int64(n)
	return n, err
}

func (file *targetFile) Close() error {
	err := file.File.Close()
	if size := file.target.info.Size; err == nil && (size < 0 || file.written >= size) {
		if !file.target.info.ModTime.IsZero() {
			_ = os.Chtimes(file.target.partial, file.target.info.ModTime, file.target.info.ModTime)
		}
		file.target.partial = "" // complete: it stays
	}
	return err
}

// finish forgets a file that was completed.
func (target *folderTarget) finish() { target.partial = "" }

// discard removes the file that was being written when the transfer failed.
func (target *folderTarget) discard() {
	if target.current != nil {
		target.current.Close()
		if target.exact != "" {
			// A file whose size was never announced cannot tell a short copy from a
			// whole one: after a failure it is removed.
			os.Remove(filepath.Join(target.dir, target.exact))
		}
	}
	if target.partial != "" {
		os.Remove(target.partial)
		target.partial = ""
	}
}

func (target *folderTarget) close() {
	if target.current != nil {
		target.current.Close()
		target.current = nil
	}
}

// declareFileTransferDialog declares the offer. A running transfer is the corner
// card, so this dialog only asks where the files go or which files to send.
func (state *State) declareFileTransferDialog(c ui.C) {
	session, ok := state.sessionByIDPointer(state.modalSession)
	if !ok || session.transfer.phase != transferOffered {
		return
	}
	transfer, m, pal := &session.transfer, state.messages.Tools, state.pal
	address := session.Address
	title, description := m.TransferReceiveTitle, fmt.Sprintf(m.TransferReceiveOfferFmt, address)
	if transfer.sending {
		title, description = m.TransferSendTitle, fmt.Sprintf(m.TransferSendOfferFmt, address)
	}
	// The offer has room for an error line under the question.
	bodyHeight := withError(0, transfer.dialogError)
	canvas := c.Canvas()
	viewport := dialogViewport(c)
	height := dialogContentHeight(canvas, *c.Theme(), viewport, 540, description, bodyHeight)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: description, Width: 540, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitTransferDismiss, CloseID: hitTransferClose, InitialFocusID: hitTransferDecline,
	})
	dialog.Body(func() {
		if transfer.dialogError == "" {
			return
		}
		ui.Column(c).Fill().Children(func() {
			ui.Text(c, transfer.dialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.danger).LineHeight(20).MarginSides(8, 0, 0, 0)
		})
	})
	saveFolder := state.downloadFolder()
	decline := components.ButtonAction{ID: hitTransferDecline, Label: m.TransferDecline, Variant: components.ButtonSecondary}
	dialog.Footer(func() {
		switch {
		case transfer.sending:
			state.dialogActionFooter(c, decline, components.ButtonAction{ID: hitTransferChoose, Label: m.TransferChooseFiles})
		case saveFolder != "":
			state.dialogActionFooter(c, decline,
				components.ButtonAction{ID: hitTransferChoose, Label: m.TransferChooseFolder, Variant: components.ButtonOutline},
				components.ButtonAction{ID: hitTransferDownloads, Label: fmt.Sprintf(m.TransferToFolderFmt, elideMiddleRunes(downloadFolderLabel(saveFolder), 24))})
		default:
			state.dialogActionFooter(c, decline, components.ButtonAction{ID: hitTransferChoose, Label: m.TransferChooseFolder})
		}
	})
}

// paintTransferProgressCard is the running transfer. It sits in a corner and
// does not take the window: clicking elsewhere leaves the transfer going, and
// only its cancel button stops it.
func (state *State) paintTransferProgressCard(canvas *gfx.Canvas) {
	if canvas == nil {
		return
	}
	running := 0
	for index := range state.sessions {
		if state.sessions[index].transfer.phase == transferRunning {
			running++
		}
	}
	if running == 0 {
		return
	}
	canvas.RequestFrameIn(time.Second)
	slot := 0
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.transfer.phase != transferRunning {
			continue
		}
		state.paintOneTransferCard(canvas, session, slot)
		slot++
	}
}

func (state *State) paintOneTransferCard(canvas *gfx.Canvas, session *Session, slot int) {
	transfer := &session.transfer
	viewport := state.windowRect()
	const (
		cardWidth  = float32(360)
		cardHeight = float32(148)
		margin     = float32(16)
		cardGap    = float32(8)
	)
	width := min(cardWidth, viewport.Width-2*margin)
	if width < 200 || viewport.Height < cardHeight+margin {
		return
	}
	card := gfx.Rect{
		X:      viewport.X + viewport.Width - width - margin,
		Y:      viewport.Y + viewport.Height - margin - float32(slot+1)*cardHeight - float32(slot)*cardGap,
		Width:  width,
		Height: cardHeight,
	}
	if minY := viewport.Y + windowBarHeight + 8; card.Y < minY {
		card.Y = minY
	}
	address := session.Address
	m, pal := state.messages.Tools, state.pal
	title := m.TransferReceiveTitle
	if transfer.sending {
		title = m.TransferSendTitle
	}
	progress := transfer.progress
	name := progress.Name
	if name == "" {
		name = "…"
	}
	if transfer.text.line == "" || transfer.text.done != progress.Done || transfer.text.size != progress.Size {
		transfer.text.done, transfer.text.size = progress.Done, progress.Size
		if progress.Size >= 0 {
			transfer.text.line = formatBytes(progress.Done) + " / " + formatBytes(progress.Size)
		} else {
			transfer.text.line = formatBytes(progress.Done)
		}
	}
	canvas.PushLayer(transferProgressLayer)
	canvas.Block(card)
	canvas.Elevate(card, radius.popup, gfx.ShadowMD)
	canvas.RoundedBorder(card, radius.popup, 1, pal.line, pal.surface)
	const pad, gap = float32(12), float32(6)
	x, y := card.X+pad, card.Y+pad
	inner := card.Width - 2*pad
	state.paintTransferCardLine(canvas, title, gfx.Rect{X: x, Y: y, Width: inner, Height: 20}, metrics.FontSizeS, gfx.WeightSemibold, pal.text)
	y += 20 + gap
	if address != "" {
		state.paintTransferCardLine(canvas, address, gfx.Rect{X: x, Y: y, Width: inner, Height: 18}, metrics.FontSizeS, gfx.WeightRegular, pal.muted)
		y += 18 + gap
	}
	state.paintTransferCardLine(canvas, name, gfx.Rect{X: x, Y: y, Width: inner, Height: 20}, metrics.FontSizeM, gfx.WeightMedium, pal.text)
	y += 20 + gap
	bar := gfx.Rect{X: x, Y: y, Width: inner, Height: 8}
	y += 8 + gap
	row := gfx.Rect{X: x, Y: y, Width: inner, Height: 28}
	cancel := state.messages.Cancel
	cancelWidth := components.BeginButton(canvas, state.Theme(), gfx.ID{}, row).Small().SizeToFit(cancel).BoundsWidth()
	state.paintTransferCardLine(canvas, transfer.text.line, gfx.Rect{X: row.X, Y: row.Y, Width: max(row.Width-cancelWidth-8, 0), Height: row.Height}, metrics.FontSizeS, gfx.WeightRegular, pal.muted)
	components.BeginButtonRow(canvas, state.Theme(), row).Size(components.ButtonSizeSmall).Paint(
		components.ButtonAction{ID: hitTransferCancelFor.At(int(session.ID)), Label: cancel, Variant: components.ButtonSecondary},
	)
	fraction := float32(0)
	if progress.Size > 0 {
		fraction = float32(float64(progress.Done) / float64(progress.Size))
	}
	components.Progress(canvas, state.Theme(), bar, fraction)
	canvas.PopLayer()
}

func (state *State) paintTransferCardLine(canvas *gfx.Canvas, value string, bounds gfx.Rect, size, weight float32, color gfx.Color) {
	canvas.Label(elide(canvas, value, size, weight, bounds.Width), bounds, gfx.TextStyle{Size: size, Weight: weight, Color: color})
}
