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
	"github.com/vivaui/viva/flex"
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

// fileTransfer is the one transfer a window can have at a time.
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
	// progress is what the dialog shows, refreshed as bytes move.
	progress    zmodem.Progress
	dialogError string
	// sizeText and doneText are built when the numbers change, not per paint.
	text transferText
}

type transferText struct {
	done, size int64
	line       string
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
	transfer := &state.transfer
	if transfer.phase != transferNone && transfer.sessionID == session.ID {
		return state.feedTransfer(session, rx)
	}
	// Every signature has a ZDLE in it, so output without one (nearly all of it)
	// costs one scan and nothing else.
	index, receiving := -1, false
	if bytes.IndexByte(rx, 0x18) >= 0 || bytes.IndexByte(t.zmodemTail, 0x18) >= 0 {
		index, receiving = splitTransferStart(t.zmodemTail, rx)
	}
	if index < 0 || transfer.phase != transferNone {
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
	state.showNextPrompt()
	return rx[:index]
}

// feedTransfer routes output to the transfer of its session.
func (state *State) feedTransfer(session *Session, rx []byte) []byte {
	transfer := &state.transfer
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
			return state.takeTransferLeftover()
		}
	}
	return nil
}

// afterTransferFeed sends what the engine produced and notices its end.
func (state *State) afterTransferFeed(session *Session) {
	transfer := &state.transfer
	engine := transfer.engine
	if out := engine.TakeOutput(nil); len(out) > 0 && session.term.shell != nil {
		session.term.shell.Write(out)
	}
	transfer.progress = engine.Progress()
	if engine.Finished() && transfer.phase == transferRunning {
		transfer.phase, transfer.closedAt = transferClosing, state.now()
		if state.modal == modalFileTransfer {
			state.closeModal()
		}
		state.reportTransfer(session)
	}
	if engine.Settled() {
		state.endTransfer(session)
	}
}

// takeTransferLeftover is the output that followed the end of the transfer.
func (state *State) takeTransferLeftover() []byte {
	leftover := state.transferLeftover
	state.transferLeftover = nil
	return leftover
}

// reportTransfer tells the terminal and the status line how the transfer ended.
func (state *State) reportTransfer(session *Session) {
	transfer, m := &state.transfer, state.messages.Tools
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
	transfer := &state.transfer
	if transfer.engine != nil {
		state.transferLeftover = append(state.transferLeftover[:0], transfer.engine.Leftover()...)
		transfer.engine.Close()
	}
	if transfer.target != nil {
		transfer.target.close()
	}
	for _, file := range transfer.sources {
		file.Close()
	}
	if state.modal == modalFileTransfer {
		state.closeModal()
	}
	*transfer = fileTransfer{}
}

// pumpTransfer is run on every wake: it asks again when a transfer has gone
// quiet, and stops waiting for a closing "OO" that never comes.
func (state *State) pumpTransfer() {
	transfer := &state.transfer
	if transfer.phase == transferNone {
		return
	}
	session, ok := state.sessionByIDPointer(transfer.sessionID)
	if !ok || session.term == nil || session.term.shell == nil {
		state.abandonTransfer()
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
func (state *State) abandonTransfer() {
	transfer := &state.transfer
	if transfer.target != nil {
		transfer.target.discard()
	}
	if state.modal == modalFileTransfer {
		state.closeModal()
	}
	state.endTransfer(nil)
}

// declineTransfer turns an offer down: the other end is told to stop.
func (state *State) declineTransfer(session *Session) {
	if session != nil && session.term != nil && session.term.shell != nil {
		session.term.shell.Write([]byte("\x18\x18\x18\x18\x18\x18\x18\x18\b\b\b\b\b\b\b\b\b\b"))
	}
	state.transferLeftover = nil
	state.endTransfer(session)
	state.status = state.messages.Tools.TransferDeclined
}

// startTransfer begins a transfer with the engine the user's choice made.
func (state *State) startTransfer(session *Session, engine zmodem.Engine) {
	transfer := &state.transfer
	transfer.engine, transfer.phase = engine, transferRunning
	transfer.lastActive = state.now()
	held := transfer.held
	transfer.held = nil
	engine.Feed(held)
	state.afterTransferFeed(session)
}

func (state *State) clickTransfer(id ui.ID) {
	transfer := &state.transfer
	session, ok := state.sessionByIDPointer(transfer.sessionID)
	if !ok || transfer.phase == transferNone {
		state.closeModal()
		return
	}
	switch transfer.phase {
	case transferOffered:
		switch id {
		case hitTransferDecline, hitTransferClose, hitTransferDismiss:
			state.closeModal()
			state.declineTransfer(session)
		case hitTransferDownloads, hitTransferChoose:
			if transfer.sending {
				state.chooseFilesToSend(session)
			} else {
				state.chooseFolderToReceive(session, id == hitTransferDownloads)
			}
		}
	case transferRunning:
		switch id {
		case hitTransferCancel, hitTransferClose, hitTransferDismiss:
			transfer.engine.Cancel()
			state.afterTransferFeed(session)
		}
	}
}

func (state *State) chooseFolderToReceive(session *Session, downloads bool) {
	m := state.messages.Tools
	dir := ""
	if downloads {
		dir = downloadsDirectory()
	} else {
		chosen, err := state.options.OpenDirectory(app.OpenDirectoryOptions{Title: m.TransferChooseFolderTitle})
		if errors.Is(err, app.ErrCanceled) || err == nil && chosen == "" {
			return // still offered: the user may pick again or decline
		}
		if err != nil {
			state.transfer.dialogError = err.Error()
			return
		}
		dir = chosen
	}
	if dir == "" {
		state.transfer.dialogError = m.TransferNoDownloads
		return
	}
	state.transfer.target = &folderTarget{dir: dir}
	state.closeModal()
	state.modal, state.modalSession = modalFileTransfer, session.ID
	state.startTransfer(session, zmodem.NewReceiver(state.transfer.target))
}

func (state *State) chooseFilesToSend(session *Session) {
	m := state.messages.Tools
	paths, err := state.options.OpenFiles(app.OpenFileOptions{Title: m.TransferChooseFilesTitle, AllowMultiple: true})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		return
	}
	if err != nil {
		state.transfer.dialogError = err.Error()
		return
	}
	if len(paths) > maxSendFiles {
		state.transfer.dialogError = fmt.Sprintf(m.TransferTooManyFmt, maxSendFiles)
		return
	}
	var files []zmodem.SendFile
	var opened []*os.File
	fail := func(message string) {
		for _, file := range opened {
			file.Close()
		}
		state.transfer.dialogError = message
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
	state.transfer.sources = opened
	state.closeModal()
	state.modal, state.modalSession = modalFileTransfer, session.ID
	state.startTransfer(session, zmodem.NewSender(files, zmodem.SendOptions{}))
}

// downloadsDirectory is the user's Downloads folder, or empty when there is none.
func downloadsDirectory() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, "Downloads")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
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

func (state *State) paintFileTransferDialog(canvas *ui.Canvas, viewport ui.Rect) {
	transfer, m, pal := &state.transfer, state.messages.Tools, state.pal
	session, _ := state.sessionByIDPointer(transfer.sessionID)
	address := ""
	if session != nil {
		address = session.Address
	}
	title, description := m.TransferReceiveTitle, fmt.Sprintf(m.TransferReceiveOfferFmt, address)
	if transfer.sending {
		title, description = m.TransferSendTitle, fmt.Sprintf(m.TransferSendOfferFmt, address)
	}
	// The offer has room for an error line; the running one for a name, a bar and
	// the amount done.
	bodyHeight := withError(0, transfer.dialogError)
	if transfer.phase == transferRunning {
		bodyHeight = 68
		description = address
	}
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitTransferDismiss, CloseID: hitTransferClose, InitialFocusID: hitTransferDecline,
		ContentSize: ui.Size{Width: 540}, FitBody: true, BodyHeight: bodyHeight,
	})
	layout := dialog.Begin(title, description)
	if transfer.phase == transferRunning {
		progress := transfer.progress
		name := progress.Name
		if name == "" {
			name = "…"
		}
		if transfer.text.done != progress.Done || transfer.text.size != progress.Size {
			transfer.text.done, transfer.text.size = progress.Done, progress.Size
			if progress.Size >= 0 {
				transfer.text.line = formatBytes(progress.Done) + " / " + formatBytes(progress.Size)
			} else {
				transfer.text.line = formatBytes(progress.Done)
			}
		}
		// The file's name, the bar and the amount done.
		var bar flex.Box
		f := state.partLayout.Begin(canvas, state.Theme(), layout.Body)
		f.Column().Fill().Children(func() {
			f.Text(name).FontSize(metrics.FontSizeM).Weight(ui.WeightSemibold).Color(pal.text).Ellipsis().LineHeight(22).MarginSides(4, 0, 0, 0)
			bar = f.Slot().Height(8).Shrink(0).MarginSides(8, 0, 0, 0)
			f.Text(transfer.text.line).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(20).MarginSides(6, 0, 0, 0)
		})
		f.End()
		fraction := float32(0)
		if progress.Size > 0 {
			fraction = float32(float64(progress.Done) / float64(progress.Size))
		}
		components.Progress(canvas, state.Theme(), bar.Rect(), fraction)
		canvas.RequestFrameIn(time.Second)
		dialog.Actions(layout, components.ButtonAction{ID: hitTransferCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary})
		dialog.End()
		return
	}
	if transfer.dialogError != "" {
		f := state.partLayout.Begin(canvas, state.Theme(), layout.Body)
		f.Text(transfer.dialogError).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.danger).LineHeight(20).MarginSides(8, 0, 0, 0)
		f.End()
	}
	decline := components.ButtonAction{ID: hitTransferDecline, Label: m.TransferDecline, Variant: components.ButtonSecondary}
	switch {
	case transfer.sending:
		dialog.Actions(layout, decline, components.ButtonAction{ID: hitTransferChoose, Label: m.TransferChooseFiles})
	case downloadsDirectory() != "":
		dialog.Actions(layout, decline,
			components.ButtonAction{ID: hitTransferChoose, Label: m.TransferChooseFolder, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitTransferDownloads, Label: m.TransferToDownloads})
	default:
		dialog.Actions(layout, decline, components.ButtonAction{ID: hitTransferChoose, Label: m.TransferChooseFolder})
	}
	dialog.End()
}
