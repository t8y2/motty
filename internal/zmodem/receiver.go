package zmodem

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Errors a transfer ends with.
var (
	// ErrCancelled is a transfer the user ended.
	ErrCancelled = errors.New("zmodem: transfer cancelled")
	// ErrPeerCancelled is a transfer the other end gave up on.
	ErrPeerCancelled = errors.New("zmodem: the other end cancelled the transfer")
	// ErrTooManyErrors is a stream that kept arriving damaged.
	ErrTooManyErrors = errors.New("zmodem: too many damaged frames")
)

// FileInfo describes one file of a transfer.
type FileInfo struct {
	// Name is a plain file name: whatever directories or odd characters the
	// sender put in front of it are gone.
	Name string
	// Size is the length the sender announced, or -1.
	Size    int64
	ModTime time.Time
	Mode    uint32
}

// Progress is how far a transfer has come.
type Progress struct {
	// Name is the file being transferred now; empty between files.
	Name string
	// Size is its announced length, or -1.
	Size int64
	// Done is how much of it has moved.
	Done int64
	// Files is how many files have completed.
	Files int
	// Sending says which way the transfer goes.
	Sending bool
}

// Engine is what the owner of a transfer drives.
type Engine interface {
	// Feed takes bytes that arrived from the other end. Once the transfer has
	// finished, further bytes are kept apart as Leftover.
	Feed(data []byte)
	// TakeOutput appends the bytes to send to dst and forgets them.
	TakeOutput(dst []byte) []byte
	// Finished reports that the transfer is over, successfully or not.
	Finished() bool
	// Settled reports that nothing more of it will arrive. It is later than
	// Finished by the two bytes "OO" that a sender puts after its last frame, so
	// an owner keeps feeding the engine until then and shows what is left over.
	Settled() bool
	// Err is why it failed; nil while it runs and after it succeeded.
	Err() error
	Progress() Progress
	// Timeout is called by the owner when nothing has arrived for a while (a few
	// seconds) while the transfer is unfinished. The engine says again what it
	// last needed, so one lost frame does not end a transfer. After enough
	// timeouts in a row the transfer fails.
	Timeout()
	// Abort ends the transfer because its bytes could not be delivered to the
	// other end. The engine has already counted them as sent, so there is
	// nothing to say again: the transfer fails with err.
	Abort(err error)
	// Cancel ends the transfer and produces the bytes that tell the other end.
	Cancel()
	// Close releases whatever the engine holds open, without telling anyone.
	Close()
	// Leftover is what arrived after the transfer ended and belongs to the
	// terminal.
	Leftover() []byte
}

// base is the part of both engines that is the same.
type base struct {
	p        parser
	out      []byte
	err      error
	finished bool
	// settled is set once nothing more of the transfer can arrive: it is later
	// than finished by the "OO" a sender puts after its last frame.
	settled  bool
	leftover []byte
	// badRun counts damaged frames in a row.
	badRun int
}

const maxBadRun = 30

// maxTimeouts is how many silences in a row a transfer survives.
const maxTimeouts = 8

// ErrTimeout is a transfer the other end stopped answering.
var ErrTimeout = errors.New("zmodem: the other end stopped answering")

func (b *base) TakeOutput(dst []byte) []byte {
	dst = append(dst, b.out...)
	b.out = b.out[:0]
	if cap(b.out) > 1<<20 {
		b.out = nil
	}
	return dst
}

func (b *base) Finished() bool   { return b.finished }
func (b *base) Settled() bool    { return b.settled }
func (b *base) Err() error       { return b.err }
func (b *base) Leftover() []byte { return b.leftover }

func (b *base) fail(err error) {
	if !b.finished {
		b.err = err
		b.finished, b.settled = true, true
	}
}

// Abort ends the transfer with err; the bytes it produced are not sent again.
func (b *base) Abort(err error) { b.fail(err) }

func (b *base) bad() bool {
	b.badRun++
	if b.badRun > maxBadRun {
		b.out = append(b.out[:0], cancelSequence...)
		b.fail(ErrTooManyErrors)
		return false
	}
	return true
}

// cleanName reduces a name a peer sent to a plain file name.
func cleanName(name string) string {
	if cut := strings.LastIndexAny(name, `/\`); cut >= 0 {
		name = name[cut+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if len(name) > 200 {
		name = strings.ToValidUTF8(name[:200], "")
	}
	if name == "" {
		return "download"
	}
	return name
}

// parseFileInfo reads the subpacket that follows a ZFILE header: the name, then
// "size mtime mode ..." with the time and the mode in octal.
func parseFileInfo(data []byte) FileInfo {
	name, rest, _ := strings.Cut(string(data), "\x00")
	info := FileInfo{Name: cleanName(name), Size: -1}
	fields := strings.Fields(rest)
	if len(fields) > 0 {
		if size, err := strconv.ParseInt(fields[0], 10, 64); err == nil && size >= 0 {
			info.Size = size
		}
	}
	if len(fields) > 1 {
		if seconds, err := strconv.ParseInt(fields[1], 8, 64); err == nil && seconds > 0 && seconds < 1<<40 {
			info.ModTime = time.Unix(seconds, 0)
		}
	}
	if len(fields) > 2 {
		if mode, err := strconv.ParseUint(fields[2], 8, 32); err == nil {
			info.Mode = uint32(mode)
		}
	}
	return info
}

// ---- Receiver ----

// Target is where a Receiver puts the files it is sent.
type Target interface {
	// Open is called when the sender offers a file. It returns what to write the
	// file to, or nil to skip it. An error ends the transfer.
	Open(info FileInfo) (io.WriteCloser, error)
}

type receiverState uint8

const (
	rIdle receiverState = iota
	rSINIT
	rFileInfo
	rData
	rResync // waiting for the sender to start over where it was told to
	rCommand
	rWaitOver // after ZFIN, the sender's "OO" is still to come
)

// Receiver takes a file, or several, that the other end sends.
type Receiver struct {
	base
	target Target
	state  receiverState

	file     io.WriteCloser
	info     FileInfo
	position uint32
	files    int
	wide     bool // the width of the header that announced the data being read
	over     int  // 'O's still to swallow after ZFIN
	skipBS   bool // after the other end cancelled, its backspaces
	last     []byte
	timeouts int
}

// NewReceiver starts a receiver. It sends nothing until the other end's first
// header has been fed to it.
func NewReceiver(target Target) *Receiver {
	return &Receiver{target: target}
}

func (r *Receiver) Progress() Progress {
	progress := Progress{Files: r.files}
	if r.file != nil {
		progress.Name, progress.Size, progress.Done = r.info.Name, r.info.Size, int64(r.position)
	}
	return progress
}

func (r *Receiver) reply(kind byte, args [4]byte) {
	start := len(r.out)
	r.out = appendHexHeader(r.out, kind, args)
	r.last = append(r.last[:0], r.out[start:]...)
}

func (r *Receiver) sendRINIT() {
	r.reply(zRINIT, [4]byte{0, 0, 0, canFDX | canOVIO | canFC32})
}

// Close releases the file being written.
func (r *Receiver) Close() {
	if r.file != nil {
		r.file.Close()
		r.file = nil
	}
}

// Cancel ends the transfer and tells the other end.
func (r *Receiver) Cancel() {
	if r.finished {
		return
	}
	r.Close()
	r.out = append(r.out, cancelSequence...)
	r.fail(ErrCancelled)
}

func (r *Receiver) Feed(data []byte) {
	for index, b := range data {
		if r.finished {
			switch {
			case r.skipBS && (b == 8 || b == zdle):
			case r.state == rWaitOver && r.over == 2 && (b == '\r' || b == '\n' || b == 0x8d || b == 0x8a || b == xon):
				// The end of the ZFIN header that was just read.
			case r.state == rWaitOver && r.over > 0 && b == 'O':
				// The sender's "over and out" that follows its last ZFIN.
				r.over--
				r.settled = r.over == 0
			default:
				r.skipBS, r.settled = false, true
				r.leftover = append(r.leftover, data[index:]...)
				return
			}
			continue
		}
		switch r.p.step(b) {
		case evHeader:
			r.badRun, r.timeouts = 0, 0
			r.header(r.p.hdr)
		case evBadHeader:
			if r.bad() {
				r.out = appendHexHeader(r.out, zNAK, [4]byte{})
			}
		case evData:
			r.badRun, r.timeouts = 0, 0
			r.subpacket()
		case evBadData:
			if r.bad() {
				r.badData()
			}
		case evCancel:
			r.Close()
			r.skipBS = true
			r.fail(ErrPeerCancelled)
		}
	}
}

// Timeout asks the sender again for what the receiver is waiting for.
func (r *Receiver) Timeout() {
	if r.finished {
		return
	}
	r.timeouts++
	if r.timeouts > maxTimeouts {
		r.Close()
		r.out = append(r.out, cancelSequence...)
		r.fail(ErrTimeout)
		return
	}
	switch {
	case r.file != nil:
		r.reply(zRPOS, positionBytes(r.position))
		r.state = rResync
		r.p.wantData(r.wide)
	case len(r.last) > 0:
		r.out = append(r.out, r.last...)
	default:
		r.sendRINIT()
	}
}

func (r *Receiver) header(h header) {
	switch h.kind {
	case zRQINIT:
		r.Close()
		r.state = rIdle
		r.sendRINIT()
	case zSINIT:
		r.state, r.wide = rSINIT, h.crc32
		r.p.wantData(h.crc32)
	case zFILE:
		r.Close()
		r.state, r.wide = rFileInfo, h.crc32
		r.p.wantData(h.crc32)
	case zDATA:
		if r.file == nil {
			r.reply(zRPOS, positionBytes(0))
			r.state = rResync
			r.p.wantData(h.crc32) // discard what follows
			return
		}
		if h.position != r.position {
			r.reply(zRPOS, positionBytes(r.position))
			r.state = rResync
			r.p.wantData(h.crc32)
			return
		}
		r.state, r.wide = rData, h.crc32
		r.p.wantData(h.crc32)
	case zEOF:
		if r.file != nil && h.position == r.position {
			err := r.file.Close()
			r.file = nil
			r.files++
			r.state = rIdle
			if err != nil {
				r.out = append(r.out, cancelSequence...)
				r.fail(fmt.Errorf("zmodem: finishing %s: %w", r.info.Name, err))
				return
			}
			r.sendRINIT()
		}
	case zFIN:
		r.Close()
		r.reply(zFIN, [4]byte{})
		r.state, r.over, r.finished = rWaitOver, 2, true
	case zCOMMAND:
		// A command is never carried out. Take its data and say it failed.
		r.state = rCommand
		r.p.wantData(h.crc32)
	case zCHALLENGE:
		r.reply(zACK, h.flags)
	case zFREECNT:
		r.reply(zACK, positionBytes(0))
	case zNAK:
		if len(r.last) > 0 {
			r.out = append(r.out, r.last...)
		}
	case zABORT, zFERR, zCAN:
		r.Close()
		r.fail(ErrPeerCancelled)
	}
}

func (r *Receiver) subpacket() {
	p := &r.p
	switch r.state {
	case rSINIT:
		r.reply(zACK, positionBytes(1))
		r.state = rIdle
	case rFileInfo:
		r.state = rIdle
		info := parseFileInfo(p.data)
		if info.Size > 1<<32-1 {
			r.reply(zSKIP, [4]byte{}) // positions are 32 bits
			return
		}
		file, err := r.target.Open(info)
		switch {
		case err != nil:
			r.out = append(r.out, cancelSequence...)
			r.fail(err)
		case file == nil:
			r.reply(zSKIP, [4]byte{})
		default:
			r.file, r.info, r.position = file, info, 0
			r.reply(zRPOS, positionBytes(0))
		}
	case rData:
		if len(p.data) > 0 {
			if _, err := r.file.Write(p.data); err != nil {
				r.Close()
				r.out = append(r.out, cancelSequence...)
				r.fail(fmt.Errorf("zmodem: writing %s: %w", r.info.Name, err))
				return
			}
			r.position += uint32(len(p.data))
		}
		switch p.end {
		case zcrcq:
			r.reply(zACK, positionBytes(r.position))
			p.resumeData()
		case zcrcg:
			p.resumeData()
		case zcrcw:
			r.reply(zACK, positionBytes(r.position))
			r.state = rIdle
		default:
			r.state = rIdle
		}
	case rResync:
		if p.end == zcrcg || p.end == zcrcq {
			p.resumeData()
		} else {
			r.state = rResync
		}
	case rCommand:
		r.reply(zCOMPL, [4]byte{1})
		r.state = rIdle
	}
}

func (r *Receiver) badData() {
	switch r.state {
	case rData:
		// Ask for everything from the last good byte on, and ignore what the
		// sender has already put on the wire until it starts over.
		r.reply(zRPOS, positionBytes(r.position))
		r.state = rResync
	case rResync:
	default:
		r.out = appendHexHeader(r.out, zNAK, [4]byte{})
		r.state = rIdle
	}
}
