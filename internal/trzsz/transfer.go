package trzsz

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Progress is how far a transfer has come. It mirrors the shape the terminal's
// transfer card shows, so the two protocols can share one card.
type Progress struct {
	Name    string
	Size    int64
	Done    int64
	Files   int
	Sending bool
}

// SendFile is one entry of an upload: a regular file, or a directory when the
// remote expects a tree. Rel is the path inside the transfer, starting with the
// top-level name the user chose.
type SendFile struct {
	Path    string
	Rel     []string
	Dir     bool
	Size    int64
	Mode    uint32
	ModTime time.Time
	PathID  int
}

// Limits on what one transfer may cost. They bound the memory a hostile remote
// can make the terminal allocate and the files a hostile peer can make it create.
const (
	maxMessage     = 1 << 20  // a name, a configuration or an error message
	maxChunk       = 64 << 20 // one decoded data chunk
	maxEntries     = 100000
	maxReceiveSize = 1 << 40
	sendChunk      = 64 << 10
)

// stage is the message the transfer is waiting for.
type stage uint8

const (
	stageConfig stage = iota
	stageSendNumber
	stageSendName
	stageSendSize
	stageSendData
	stageSendDigest
	stageReceiveNumber
	stageReceiveName
	stageReceiveSize
	stageReceiveData
	stageReceiveDigest
	stageDone
)

// Transfer is the terminal side of one trzsz transfer. It is driven by the
// terminal's owner: Feed takes what arrived from the remote, TakeOutput gives
// back what must be written to it. It is not safe for concurrent use.
type Transfer struct {
	sending bool
	dir     string
	files   []SendFile

	output   []byte
	input    []byte
	leftover []byte

	dirMode bool

	expect string
	stage  stage
	// pending is the value the next acknowledgement must repeat: a count, a
	// length, or how many bytes of a file have been sent.
	pending int64

	index     int
	source    *os.File
	size      int64
	done      int64
	digest    hash.Hash
	chunkSize int

	entriesLeft int
	filesDone   int
	name        string

	topNames map[int]string
	roots    []string
	current  *os.File

	err      error
	finished bool
	settled  bool
}

// NewSend starts an upload for an offer from trz. The caller has already asked
// the user which files to send.
func NewSend(files []SendFile, trigger Trigger) (*Transfer, error) {
	if !trigger.Sending() {
		return nil, errors.New("trzsz: the remote is not receiving files")
	}
	if len(files) == 0 {
		return nil, errors.New("trzsz: no files to send")
	}
	for i := range files {
		if len(files[i].Rel) == 0 {
			return nil, errors.New("trzsz: a file has no name")
		}
		if !filepath.IsAbs(files[i].Path) {
			return nil, errors.New("trzsz: file paths must be absolute")
		}
		for _, component := range files[i].Rel {
			if err := checkComponent(component); err != nil {
				return nil, err
			}
		}
	}
	transfer := &Transfer{
		sending:   true,
		files:     files,
		expect:    "CFG",
		stage:     stageConfig,
		digest:    md5.New(),
		chunkSize: sendChunk,
	}
	transfer.declare(true)
	return transfer, nil
}

// NewReceive starts a download for an offer from tsz into dir.
func NewReceive(dir string, trigger Trigger) (*Transfer, error) {
	if trigger.Sending() {
		return nil, errors.New("trzsz: the remote is not sending files")
	}
	if !filepath.IsAbs(dir) {
		return nil, errors.New("trzsz: the download folder must be absolute")
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("trzsz: the download folder must be a real directory")
	}
	transfer := &Transfer{
		dir:      filepath.Clean(dir),
		expect:   "CFG",
		stage:    stageConfig,
		topNames: make(map[int]string),
	}
	transfer.declare(true)
	return transfer, nil
}

// Decline answers an offer the user turned down. The remote program then exits
// on its own instead of waiting out its timeout.
func Decline(trigger Trigger) ([]byte, error) {
	payload, err := json.Marshal(declarationFor(false))
	if err != nil {
		return nil, err
	}
	return []byte("#ACT:" + EncodeText(payload) + "\n"), nil
}

// Feed takes bytes that arrived from the remote.
func (t *Transfer) Feed(data []byte) {
	if t.finished {
		if len(data) > 0 {
			t.leftover = append(t.leftover, data...)
		}
		return
	}
	t.input = append(t.input, data...)
	for len(t.input) > 0 && !t.finished {
		newline := bytes.IndexByte(t.input, '\n')
		if newline < 0 {
			if len(t.input) > maxChunk {
				t.fail(errors.New("trzsz: the remote sent an oversized line"))
			}
			return
		}
		if newline > maxChunk {
			t.fail(errors.New("trzsz: the remote sent an oversized line"))
			return
		}
		line := t.input[:newline]
		t.input = t.input[newline+1:]
		t.handleLine(line)
	}
	if t.finished && len(t.input) > 0 {
		t.leftover = append(t.leftover, t.input...)
		t.input = nil
	}
}

// handleLine reads one protocol line. Terminal junk in front of it (a tmux
// status line, an echoed escape sequence) is dropped by starting at the last
// '#': every payload is base64 or a number, so a '#' inside one is impossible.
func (t *Transfer) handleLine(line []byte) {
	line = bytes.TrimRight(line, "\r")
	if len(line) == 0 {
		return
	}
	if line[0] != '#' {
		index := bytes.LastIndexByte(line, '#')
		if index < 0 || index == len(line)-1 {
			return
		}
		line = line[index:]
	}
	colon := bytes.IndexByte(line, ':')
	if colon < 2 {
		return
	}
	kind, payload := string(line[1:colon]), string(line[colon+1:])
	if isRemoteFailure(kind) {
		message, err := DecodeText(payload, maxMessage)
		if err != nil {
			t.failRemote(fmt.Errorf("trzsz: the remote stopped the transfer (%s)", kind))
			return
		}
		t.failRemote(fmt.Errorf("%s", message))
		return
	}
	if t.expect == "" {
		return // a duplicate acknowledgement; the next request is already out
	}
	if kind != t.expect {
		t.fail(fmt.Errorf("trzsz: the remote sent %s while %s was expected", kind, t.expect))
		return
	}
	t.expect = ""
	t.handle(kind, payload)
}

func isRemoteFailure(kind string) bool {
	switch kind {
	case "fail", "FAIL", "EXIT":
		return true
	}
	return false
}

func (t *Transfer) handle(kind, payload string) {
	switch t.stage {
	case stageConfig:
		t.onConfig(payload)
	case stageSendNumber:
		t.onSendNumber(payload)
	case stageSendName:
		t.onSendName(payload)
	case stageSendSize:
		t.onSendSize(payload)
	case stageSendData:
		t.onSendData(payload)
	case stageSendDigest:
		t.onSendDigest(payload)
	case stageReceiveNumber:
		t.onReceiveNumber(payload)
	case stageReceiveName:
		t.onReceiveName(payload)
	case stageReceiveSize:
		t.onReceiveSize(payload)
	case stageReceiveData:
		t.onReceiveData(payload)
	case stageReceiveDigest:
		t.onReceiveDigest(payload)
	}
}

func (t *Transfer) declare(confirm bool) {
	payload, err := json.Marshal(declarationFor(confirm))
	if err != nil {
		t.fail(err)
		return
	}
	t.sendString("ACT", string(payload))
}

func (t *Transfer) onConfig(payload string) {
	decoded, err := DecodeText(payload, maxMessage)
	if err != nil {
		t.fail(err)
		return
	}
	cfg, err := parseConfig(string(decoded))
	if err != nil {
		t.fail(err)
		return
	}
	if cfg.Binary {
		// The offer said binary mode was not available, so a remote that still
		// asks for it did not read the offer, and its data would look like
		// terminal output.
		t.fail(errors.New("trzsz: the remote asked for a binary stream"))
		return
	}
	t.dirMode = cfg.Dir
	if t.sending {
		if !cfg.Dir && t.tree() {
			t.fail(errors.New("trzsz: the remote does not accept a directory"))
			return
		}
		t.pending = int64(len(t.files))
		t.sendInteger("NUM", t.pending)
		t.expect, t.stage = "SUCC", stageSendNumber
		return
	}
	t.expect, t.stage = "NUM", stageReceiveNumber
}

// tree reports whether the upload holds anything beyond plain top-level files.
func (t *Transfer) tree() bool {
	for _, file := range t.files {
		if file.Dir || len(file.Rel) > 1 {
			return true
		}
	}
	return false
}

func (t *Transfer) onSendNumber(payload string) {
	number, err := parseCount(payload)
	if err != nil {
		t.fail(err)
		return
	}
	if number != t.pending {
		t.fail(fmt.Errorf("trzsz: the remote acknowledged %d files, not %d", number, t.pending))
		return
	}
	t.beginFile()
}

func (t *Transfer) beginFile() {
	if t.index >= len(t.files) {
		t.sendString("EXIT", fmt.Sprintf("Sent %d files", t.filesDone))
		t.doneTransfer()
		return
	}
	file := t.files[t.index]
	t.name = file.Rel[len(file.Rel)-1]
	t.size, t.done = file.Size, 0
	if file.Dir {
		t.size = -1
		t.queueName(file)
		return
	}
	source, err := os.Open(file.Path)
	if err != nil {
		t.fail(err)
		return
	}
	info, err := source.Stat()
	if err != nil {
		source.Close()
		t.fail(err)
		return
	}
	if !info.Mode().IsRegular() || info.Size() != file.Size {
		source.Close()
		t.fail(fmt.Errorf("trzsz: %s changed while it was being sent", filepath.Base(file.Path)))
		return
	}
	t.source = source
	t.queueName(file)
}

func (t *Transfer) queueName(file SendFile) {
	name := file.Rel[len(file.Rel)-1]
	if t.dirMode {
		payload, err := json.Marshal(sourceFile{
			PathID:   file.PathID,
			PathName: file.Rel,
			IsDir:    file.Dir,
			Size:     file.Size,
			Perm:     permission(file.Mode),
		})
		if err != nil {
			t.fail(err)
			return
		}
		name = string(payload)
	}
	t.sendString("NAME", name)
	t.expect, t.stage = "SUCC", stageSendName
}

func permission(mode uint32) *uint32 {
	if mode == 0 {
		return nil
	}
	return &mode
}

func (t *Transfer) onSendName(payload string) {
	if _, err := DecodeText(payload, maxMessage); err != nil {
		t.fail(err)
		return
	}
	if t.files[t.index].Dir {
		t.index++
		t.beginFile()
		return
	}
	t.sendInteger("SIZE", t.size)
	t.expect, t.stage = "SUCC", stageSendSize
}

func (t *Transfer) onSendSize(payload string) {
	size, err := parseCount(payload)
	if err != nil {
		t.fail(err)
		return
	}
	if size != t.size {
		t.fail(fmt.Errorf("trzsz: the remote acknowledged a size of %d, not %d", size, t.size))
		return
	}
	t.digest.Reset()
	t.sendChunk()
}

// sendChunk queues one data message and waits for its acknowledgement. A chunk
// at a time keeps the remote's flow control meaningful: the next bytes do not
// leave until the previous ones are on disk.
func (t *Transfer) sendChunk() {
	if t.done >= t.size {
		t.sendBinary("MD5", t.digest.Sum(nil))
		t.expect, t.stage = "SUCC", stageSendDigest
		return
	}
	length := int64(t.chunkSize)
	if remaining := t.size - t.done; remaining < length {
		length = remaining
	}
	buffer := make([]byte, length)
	if _, err := io.ReadFull(t.source, buffer); err != nil {
		t.fail(fmt.Errorf("trzsz: reading the file failed: %w", err))
		return
	}
	t.digest.Write(buffer)
	t.sendBinary("DATA", buffer)
	t.pending = length
	t.expect, t.stage = "SUCC", stageSendData
}

func (t *Transfer) onSendData(payload string) {
	length, err := parseCount(payload)
	if err != nil {
		t.fail(err)
		return
	}
	if length != t.pending {
		t.fail(fmt.Errorf("trzsz: the remote acknowledged %d bytes, not %d", length, t.pending))
		return
	}
	t.done += length
	t.sendChunk()
}

func (t *Transfer) onSendDigest(payload string) {
	remote, err := DecodeText(payload, md5.Size)
	if err != nil {
		t.fail(err)
		return
	}
	if !bytes.Equal(remote, t.digest.Sum(nil)) {
		t.fail(errors.New("trzsz: the remote's checksum does not match"))
		return
	}
	t.source.Close()
	t.source = nil
	t.filesDone++
	t.index++
	t.beginFile()
}

func (t *Transfer) onReceiveNumber(payload string) {
	number, err := parseCount(payload)
	if err != nil {
		t.fail(err)
		return
	}
	if number > maxEntries {
		t.fail(errors.New("trzsz: the remote offered too many files"))
		return
	}
	t.entriesLeft = int(number)
	t.sendInteger("SUCC", number)
	if number == 0 {
		t.finishReceive()
		return
	}
	t.expect, t.stage = "NAME", stageReceiveName
}

func (t *Transfer) onReceiveName(payload string) {
	decoded, err := DecodeText(payload, maxMessage)
	if err != nil {
		t.fail(err)
		return
	}
	entry, err := t.createEntry(decoded)
	if err != nil {
		t.fail(err)
		return
	}
	t.name = entry.name
	t.size, t.done = -1, 0
	if entry.dir {
		t.size = 0
	}
	t.sendString("SUCC", entry.local)
	if entry.dir {
		t.entriesLeft--
		if t.entriesLeft == 0 {
			t.finishReceive()
			return
		}
		t.expect, t.stage = "NAME", stageReceiveName
		return
	}
	t.expect, t.stage = "SIZE", stageReceiveSize
}

func (t *Transfer) onReceiveSize(payload string) {
	size, err := parseCount(payload)
	if err != nil {
		t.fail(err)
		return
	}
	if size > maxReceiveSize {
		t.fail(errors.New("trzsz: the remote announced an impossible file size"))
		return
	}
	t.size = size
	t.sendInteger("SUCC", size)
	t.digest = md5.New()
	if size == 0 {
		t.expect, t.stage = "MD5", stageReceiveDigest
		return
	}
	t.expect, t.stage = "DATA", stageReceiveData
}

func (t *Transfer) onReceiveData(payload string) {
	data, err := DecodeText(payload, maxChunk)
	if err != nil {
		t.fail(err)
		return
	}
	if t.current == nil {
		t.fail(errors.New("trzsz: data arrived without a file"))
		return
	}
	if t.done+int64(len(data)) > t.size {
		t.fail(errors.New("trzsz: the remote sent more bytes than it announced"))
		return
	}
	if _, err := t.current.Write(data); err != nil {
		t.fail(err)
		return
	}
	t.digest.Write(data)
	t.done += int64(len(data))
	t.sendInteger("SUCC", int64(len(data)))
	if t.done == t.size {
		t.expect, t.stage = "MD5", stageReceiveDigest
		return
	}
	t.expect, t.stage = "DATA", stageReceiveData
}

func (t *Transfer) onReceiveDigest(payload string) {
	remote, err := DecodeText(payload, md5.Size)
	if err != nil {
		t.fail(err)
		return
	}
	local := t.digest.Sum(nil)
	if !bytes.Equal(remote, local) {
		t.fail(errors.New("trzsz: the file's checksum does not match"))
		return
	}
	if err := t.current.Close(); err != nil {
		t.current = nil
		t.fail(err)
		return
	}
	t.current = nil
	t.sendBinary("SUCC", local)
	t.filesDone++
	t.entriesLeft--
	if t.entriesLeft == 0 {
		t.finishReceive()
		return
	}
	t.expect, t.stage = "NAME", stageReceiveName
}

func (t *Transfer) finishReceive() {
	t.sendString("EXIT", fmt.Sprintf("Saved %d files to %s", t.filesDone, t.dir))
	t.doneTransfer()
}

// doneTransfer ends a transfer that went through. The bytes the remote prints
// after it (its own "Saved" line) are terminal output, not protocol.
func (t *Transfer) doneTransfer() {
	if t.finished {
		return
	}
	t.finished, t.settled = true, true
	t.expect = ""
	t.stage = stageDone
	t.closeFiles()
}

// fail ends the transfer on the terminal's side and tells the remote, so it
// stops instead of waiting for the next message.
func (t *Transfer) fail(err error) {
	if t.finished {
		return
	}
	t.err = err
	t.finished, t.settled = true, true
	t.expect = ""
	t.stage = stageDone
	t.sendString("fail", err.Error())
	t.closeFiles()
	t.discardReceived()
}

// failRemote ends the transfer because the remote said so. Answering a failure
// with another failure would only add noise to its screen.
func (t *Transfer) failRemote(err error) {
	if t.finished {
		return
	}
	t.err = err
	t.finished, t.settled = true, true
	t.expect = ""
	t.stage = stageDone
	t.closeFiles()
	t.discardReceived()
}

// Cancel ends the transfer and tells the remote why.
func (t *Transfer) Cancel() {
	if t.finished {
		return
	}
	t.fail(ErrCancelled)
}

// Abort ends the transfer because its bytes could not be delivered.
func (t *Transfer) Abort(err error) {
	t.failRemote(err)
}

// Timeout is a no-op: the protocol runs over a reliable stream, so a quiet
// transfer is a slow remote rather than a lost message, and repeating a
// request would duplicate a file. The remote's own timeout ends a dead one.
func (t *Transfer) Timeout() {}

// TakeOutput appends the bytes to write to the remote.
func (t *Transfer) TakeOutput(dst []byte) []byte {
	dst = append(dst, t.output...)
	t.output = t.output[:0]
	return dst
}

// Finished reports that the transfer is over.
func (t *Transfer) Finished() bool { return t.finished }

// Settled reports that no more of the protocol is expected.
func (t *Transfer) Settled() bool { return t.settled }

// Err is why the transfer failed; nil while it runs and after it succeeded.
func (t *Transfer) Err() error { return t.err }

// Leftover is what arrived after the transfer ended.
func (t *Transfer) Leftover() []byte {
	leftover := t.leftover
	t.leftover = nil
	return leftover
}

// Progress is how far the transfer has come.
func (t *Transfer) Progress() Progress {
	progress := Progress{
		Name:    t.name,
		Size:    t.size,
		Done:    t.done,
		Files:   t.filesDone,
		Sending: t.sending,
	}
	return progress
}

// Close releases what the transfer holds open without telling anyone.
func (t *Transfer) Close() {
	t.closeFiles()
}

func (t *Transfer) closeFiles() {
	if t.source != nil {
		t.source.Close()
		t.source = nil
	}
	if t.current != nil {
		t.current.Close()
		t.current = nil
	}
}

// discardReceived removes the files this transfer created, so a cancelled or
// failed download leaves no half copy behind. Every root is a name this
// transfer picked because it did not exist.
func (t *Transfer) discardReceived() {
	if t.sending {
		return
	}
	for i := len(t.roots) - 1; i >= 0; i-- {
		os.RemoveAll(t.roots[i])
	}
	t.roots = nil
}

func (t *Transfer) sendString(kind, value string) {
	t.sendLine(kind, EncodeText([]byte(value)))
}

func (t *Transfer) sendBinary(kind string, data []byte) {
	t.sendLine(kind, EncodeText(data))
}

func (t *Transfer) sendInteger(kind string, value int64) {
	t.sendLine(kind, fmt.Sprintf("%d", value))
}

func (t *Transfer) sendLine(kind, payload string) {
	t.output = append(t.output, '#')
	t.output = append(t.output, kind...)
	t.output = append(t.output, ':')
	t.output = append(t.output, payload...)
	t.output = append(t.output, '\n')
}

func parseCount(payload string) (int64, error) {
	if payload == "" || len(payload) > 19 {
		return 0, errors.New("trzsz: the remote sent an invalid number")
	}
	for i := 0; i < len(payload); i++ {
		if payload[i] < '0' || payload[i] > '9' {
			return 0, errors.New("trzsz: the remote sent an invalid number")
		}
	}
	value, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return 0, errors.New("trzsz: the remote sent an invalid number")
	}
	return value, nil
}

// sourceFile is the directory entry a name message carries. Field names are
// the protocol's.
type sourceFile struct {
	PathID   int      `json:"path_id"`
	PathName []string `json:"path_name"`
	IsDir    bool     `json:"is_dir"`
	Archive  bool     `json:"archive"`
	Size     int64    `json:"size"`
	Perm     *uint32  `json:"perm"`
}

// receivedEntry is one created path on the download side.
type receivedEntry struct {
	name  string
	local string
	dir   bool
}

// createEntry turns a name message into a local path. It never trusts the
// remote with a path: every component is a plain name, the top-level name is
// one this side picked because it did not exist, and every directory between
// the chosen folder and the file is checked to be a real directory.
func (t *Transfer) createEntry(decoded []byte) (receivedEntry, error) {
	if !t.dirMode {
		name := string(decoded)
		if err := checkComponent(name); err != nil {
			return receivedEntry{}, err
		}
		local, _, file, err := t.createRoot(name, false)
		if err != nil {
			return receivedEntry{}, err
		}
		t.current = file
		return receivedEntry{name: name, local: local}, nil
	}
	var entry sourceFile
	if err := json.Unmarshal(decoded, &entry); err != nil {
		return receivedEntry{}, fmt.Errorf("trzsz: bad file name message: %w", err)
	}
	if entry.Archive {
		return receivedEntry{}, errors.New("trzsz: the remote sent an archive this client does not accept")
	}
	if entry.PathID < 0 || len(entry.PathName) == 0 || len(entry.PathName) > 64 {
		return receivedEntry{}, errors.New("trzsz: the remote sent an empty path")
	}
	for _, component := range entry.PathName {
		if err := checkComponent(component); err != nil {
			return receivedEntry{}, err
		}
	}
	name := entry.PathName[len(entry.PathName)-1]
	local, ok := t.topNames[entry.PathID]
	if !ok {
		var file *os.File
		var err error
		local, _, file, err = t.createRoot(entry.PathName[0], entry.IsDir && len(entry.PathName) == 1)
		if err != nil {
			return receivedEntry{}, err
		}
		t.topNames[entry.PathID] = local
		if file != nil {
			t.current = file
		}
		if len(entry.PathName) == 1 {
			return receivedEntry{name: name, local: local, dir: entry.IsDir}, nil
		}
	}
	root := filepath.Join(t.dir, local)
	path := filepath.Join(append([]string{root}, entry.PathName[1:]...)...)
	if !within(t.dir, path) {
		return receivedEntry{}, errors.New("trzsz: the remote's path leaves the download folder")
	}
	if entry.IsDir {
		if err := makeParents(t.dir, path); err != nil {
			return receivedEntry{}, err
		}
		if err := os.Mkdir(path, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return receivedEntry{}, err
		}
		if info, err := os.Lstat(path); err != nil || !info.IsDir() {
			return receivedEntry{}, errors.New("trzsz: a directory in the download path is not a directory")
		}
		return receivedEntry{name: name, local: local, dir: true}, nil
	}
	if err := makeParents(t.dir, path); err != nil {
		return receivedEntry{}, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return receivedEntry{}, err
	}
	t.current = file
	return receivedEntry{name: name, local: local}, nil
}

// createRoot creates a top-level entry under a name that does not exist yet,
// adding " (1)" and so on when the obvious name is taken. The chosen name is
// remembered so the whole tree goes under it and can be removed on failure.
func (t *Transfer) createRoot(name string, dir bool) (local, path string, file *os.File, err error) {
	base := filepath.Base(name)
	extension := filepath.Ext(base)
	stem := strings.TrimSuffix(base, extension)
	for attempt := 0; attempt < 1000; attempt++ {
		candidate := base
		if attempt > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, attempt, extension)
		}
		path = filepath.Join(t.dir, candidate)
		if dir {
			if err := os.Mkdir(path, 0o755); err == nil {
				t.roots = append(t.roots, path)
				return candidate, path, nil, nil
			} else if errors.Is(err, os.ErrExist) {
				continue
			} else {
				return "", "", nil, err
			}
		}
		file, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			t.roots = append(t.roots, path)
			return candidate, path, file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", "", nil, err
		}
	}
	return "", "", nil, errors.New("trzsz: too many files with the same name")
}

// checkComponent validates one path element from the remote.
func checkComponent(name string) error {
	if name == "" || name == "." || name == ".." {
		return errors.New("trzsz: the remote sent an empty path element")
	}
	if strings.ContainsAny(name, "/\\") {
		return errors.New("trzsz: the remote sent a path separator inside a name")
	}
	if !filepath.IsLocal(name) {
		return errors.New("trzsz: the remote sent a name this system does not allow")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return errors.New("trzsz: the remote sent a control character in a name")
		}
	}
	return nil
}

// makeParents creates the directories above path, refusing to walk through
// anything that is not a real directory inside root.
func makeParents(root, path string) error {
	parent := filepath.Dir(path)
	relative, err := filepath.Rel(root, parent)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("trzsz: the remote's path leaves the download folder")
	}
	if relative == "." {
		return nil
	}
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("trzsz: a directory in the download path is not a directory")
		}
	}
	return nil
}

// within reports whether path is inside root.
func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// ListDirectory builds the upload entries for a directory: the directory, its
// contents, and their contents, in the order a receiver needs to create them.
// Symbolic links are refused rather than followed, so a link cannot make the
// transfer read something the user did not choose.
func ListDirectory(path string) ([]SendFile, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("trzsz: the directory must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("trzsz: choose a directory, not a link")
	}
	base := filepath.Base(path)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return nil, errors.New("trzsz: the chosen directory has no name to send")
	}
	var files []SendFile
	err = appendDirectory(&files, path, 0, []string{base})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func appendDirectory(files *[]SendFile, path string, id int, rel []string) error {
	if len(*files) >= maxEntries {
		return errors.New("trzsz: the directory holds too many entries")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	*files = append(*files, SendFile{
		Path: path, Rel: rel, Dir: true, Mode: uint32(info.Mode().Perm()), ModTime: info.ModTime(), PathID: id,
	})
	children, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, child := range children {
		childPath := filepath.Join(path, child.Name())
		childRel := append(append([]string(nil), rel...), child.Name())
		info, err := os.Lstat(childPath)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("trzsz: %s is a symbolic link", childPath)
		case info.IsDir():
			if err := appendDirectory(files, childPath, id, childRel); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if len(*files) >= maxEntries {
				return errors.New("trzsz: the directory holds too many entries")
			}
			*files = append(*files, SendFile{
				Path: childPath, Rel: childRel, Size: info.Size(),
				Mode: uint32(info.Mode().Perm()), ModTime: info.ModTime(), PathID: id,
			})
		default:
			return fmt.Errorf("trzsz: %s is not a regular file", childPath)
		}
	}
	return nil
}
