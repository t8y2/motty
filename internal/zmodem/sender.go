package zmodem

import (
	"errors"
	"fmt"
	"io"
	"time"
)

// SendFile is one file for a Sender to send.
type SendFile struct {
	Name    string
	Size    int64
	ModTime time.Time
	Mode    uint32
	// Source supplies the bytes. The Sender never closes it.
	Source io.ReaderAt
}

// SendOptions tune a Sender. The zero value is right for a network; a slow
// serial line wants a smaller Window.
type SendOptions struct {
	// BlockSize is the data in one subpacket. Default 1024.
	BlockSize int
	// Window is how much is sent before the receiver has to answer. Default
	// 32 KiB; the receiver's own buffer limit lowers it.
	Window int
}

type senderState uint8

const (
	sWaitRINIT senderState = iota // before the first file and after each one
	sWaitRPOS                     // a ZFILE is out: where should it start?
	sData
	sWaitEOF // the file's end is out: is it all there?
	sWaitFIN
)

// Sender sends files to a receiver, which began by asking for them.
type Sender struct {
	base
	files   []SendFile
	options SendOptions

	state  senderState
	index  int
	wide   bool
	window int

	position uint32
	// waitAck is set after a subpacket the receiver has to answer, until it does.
	waitAck bool
	ackPos  uint32
	blocks  int
	buf     []byte
	sent    int // files finished
	last    []byte
	skipBS  bool
	// endTrailer is how many bytes of the closing hex header's CR LF still
	// belong to the protocol. The terminal gets what follows them.
	endTrailer int
	timeouts   int
}

var errShortFile = errors.New("zmodem: the file is shorter than it was when the transfer began")

// NewSender prepares to send files. It sends nothing until the receiver's ZRINIT
// has been fed to it.
func NewSender(files []SendFile, options SendOptions) *Sender {
	if options.BlockSize <= 0 || options.BlockSize > 8192 {
		options.BlockSize = 1024
	}
	if options.Window < options.BlockSize {
		options.Window = 32 << 10
	}
	return &Sender{files: files, options: options, buf: make([]byte, options.BlockSize)}
}

func (s *Sender) Progress() Progress {
	progress := Progress{Files: s.sent, Sending: true}
	if s.index < len(s.files) && (s.state == sWaitRPOS || s.state == sData || s.state == sWaitEOF) {
		file := s.files[s.index]
		progress.Name, progress.Size, progress.Done = file.Name, file.Size, int64(s.position)
	}
	return progress
}

func (s *Sender) Close() {}

// Timeout says again what the receiver has not answered.
func (s *Sender) Timeout() {
	if s.finished {
		return
	}
	s.timeouts++
	if s.timeouts > maxTimeouts {
		s.out = append(s.out, cancelSequence...)
		s.fail(ErrTimeout)
		return
	}
	switch s.state {
	case sWaitRPOS, sWaitEOF, sWaitFIN:
		s.out = append(s.out, s.last...)
	case sData:
		// The receiver drives recovery of lost data, but an acknowledgement
		// that was lost is the sender's to ask for again.
		if s.waitAck {
			s.waitAck = false
			s.fill()
		}
	}
}

func (s *Sender) Cancel() {
	if s.finished {
		return
	}
	s.out = append(s.out, cancelSequence...)
	s.fail(ErrCancelled)
}

func (s *Sender) Feed(data []byte) {
	for index, b := range data {
		if s.finished {
			switch {
			case s.skipBS && (b == 8 || b == zdle):
			case s.endTrailer > 0 && isHeaderEnd(b):
				s.endTrailer--
				s.settled = s.endTrailer == 0
			default:
				s.settled, s.skipBS, s.endTrailer = true, false, 0
				s.leftover = append(s.leftover, data[index:]...)
				return
			}
			continue
		}
		switch s.p.step(b) {
		case evHeader:
			s.badRun, s.timeouts = 0, 0
			s.header(s.p.hdr)
		case evBadHeader, evBadData:
			s.bad()
		case evCancel:
			s.skipBS = true
			s.fail(ErrPeerCancelled)
		}
	}
}

// remember keeps the headers that have to be sent again when the receiver
// could not read them.
func (s *Sender) remember(start int) {
	s.last = append(s.last[:0], s.out[start:]...)
}

func (s *Sender) header(h header) {
	switch h.kind {
	case zRINIT:
		switch s.state {
		case sWaitRINIT:
			s.wide = h.flags[3]&canFC32 != 0
			s.window = s.options.Window
			if buffer := int(h.flags[0]) | int(h.flags[1])<<8; buffer > 0 {
				s.window = max(min(s.window, buffer), s.options.BlockSize)
			}
			s.startFile()
		case sWaitRPOS:
			// The first file has not been accepted yet. A ready that is still
			// arriving means the receiver never saw the header, so say it again.
			// After a file, ready means "send the next one", and a receiver sends
			// it again when a repeated end-of-file crosses that ready. Saying the
			// next header again makes the receiver open another file. A header
			// that was lost is repeated by a timeout or a ZNAK.
			if s.index == 0 {
				s.out = append(s.out, s.last...)
			}
		case sWaitEOF:
			s.sent++
			s.index++
			s.startFile()
		}
	case zRPOS:
		if s.state != sWaitRPOS && s.state != sData && s.state != sWaitEOF {
			return
		}
		if size := s.files[s.index].Size; int64(h.position) > size {
			s.out = append(s.out, cancelSequence...)
			s.fail(fmt.Errorf("zmodem: the receiver asked for byte %d of a %d byte file", h.position, size))
			return
		}
		s.position, s.waitAck, s.blocks = h.position, false, 0
		s.state = sData
		s.out = appendBinaryHeader(s.out, zDATA, positionBytes(s.position), s.wide)
		s.fill()
	case zACK:
		if s.state == sData && s.waitAck && h.position >= s.ackPos {
			s.waitAck = false
			s.fill()
		}
	case zSKIP:
		if s.state == sWaitRPOS || s.state == sData {
			s.nextFile()
		}
	case zCRC:
		if s.index < len(s.files) {
			s.out = appendHexHeader(s.out, zCRC, positionBytes(s.fileCRC(h.position)))
		}
	case zFIN:
		if s.state == sWaitFIN {
			s.out = append(s.out, 'O', 'O')
			s.finished = true
			// A hex header ends with CR LF, which still belongs to the protocol;
			// the terminal gets what follows it. A binary header has no terminator.
			s.endTrailer, s.settled = 0, true
			if h.hex {
				s.endTrailer, s.settled = 2, false
			}
		}
	case zCHALLENGE:
		s.out = appendHexHeader(s.out, zACK, h.flags)
	case zNAK:
		switch s.state {
		case sWaitRPOS, sWaitEOF, sWaitFIN:
			s.out = append(s.out, s.last...)
		case sData:
			s.out = appendBinaryHeader(s.out, zDATA, positionBytes(s.position), s.wide)
			s.waitAck = false
			s.fill()
		}
	case zABORT, zFERR, zCAN:
		s.fail(ErrPeerCancelled)
	}
}

// startFile begins the next file, or ends the session when there is none.
func (s *Sender) startFile() {
	if s.index >= len(s.files) {
		start := len(s.out)
		s.out = appendHexHeader(s.out, zFIN, [4]byte{})
		s.remember(start)
		s.state = sWaitFIN
		return
	}
	s.position, s.waitAck, s.blocks = 0, false, 0
	s.sendFileHeader()
	s.state = sWaitRPOS
}

func (s *Sender) sendFileHeader() {
	file := s.files[s.index]
	remaining := int64(0)
	for _, later := range s.files[s.index:] {
		remaining += later.Size
	}
	mtime := int64(0)
	if !file.ModTime.IsZero() {
		mtime = file.ModTime.Unix()
	}
	info := fmt.Sprintf("%s\x00%d %o %o 0 %d %d", cleanName(file.Name), file.Size, mtime, file.Mode&0o7777|0o100000, len(s.files)-s.index, remaining)
	start := len(s.out)
	s.out = appendBinaryHeader(s.out, zFILE, [4]byte{0, 0, 0, 1}, s.wide) // ZF0 = binary transfer
	s.out = appendSubpacket(s.out, []byte(info), zcrcw, s.wide)
	s.remember(start)
}

// nextFile goes on after a file that was skipped, or after the receiver took
// the end of one.
func (s *Sender) nextFile() {
	s.index++
	s.position, s.waitAck = 0, false
	if s.index >= len(s.files) {
		start := len(s.out)
		s.out = appendHexHeader(s.out, zFIN, [4]byte{})
		s.remember(start)
		s.state = sWaitFIN
		return
	}
	s.sendFileHeader()
	s.state = sWaitRPOS
}

// fill sends data while the window has room.
func (s *Sender) fill() {
	file := s.files[s.index]
	ackEvery := max(s.window/s.options.BlockSize, 1)
	for s.state == sData && !s.waitAck {
		n := int64(s.options.BlockSize)
		if left := file.Size - int64(s.position); left < n {
			n = left
		}
		block := s.buf[:n]
		if n > 0 {
			read, err := file.Source.ReadAt(block, int64(s.position))
			if int64(read) < n {
				if err == nil || errors.Is(err, io.EOF) {
					err = errShortFile
				}
				s.out = append(s.out, cancelSequence...)
				s.fail(err)
				return
			}
		}
		last := int64(s.position)+n >= file.Size
		s.blocks++
		end := byte(zcrcg)
		switch {
		case last:
			end = zcrce
		case s.blocks%ackEvery == 0:
			end = zcrcq
		}
		s.out = appendSubpacket(s.out, block, end, s.wide)
		s.position += uint32(n)
		switch end {
		case zcrcq:
			s.waitAck, s.ackPos = true, s.position
		case zcrce:
			start := len(s.out)
			s.out = appendBinaryHeader(s.out, zEOF, positionBytes(s.position), s.wide)
			s.remember(start)
			s.state = sWaitEOF
			return
		}
	}
}

// fileCRC is the CRC-32 of the first length bytes of the current file (all of it
// when length is 0), which a receiver asks for before it resumes a transfer.
func (s *Sender) fileCRC(length uint32) uint32 {
	file := s.files[s.index]
	total := file.Size
	if length > 0 && int64(length) < total {
		total = int64(length)
	}
	var crc uint32
	for offset := int64(0); offset < total; {
		n := min(int64(len(s.buf)), total-offset)
		read, _ := file.Source.ReadAt(s.buf[:n], offset)
		if int64(read) < n {
			break
		}
		crc = crc32Update(crc, s.buf[:n])
		offset += n
	}
	return crc
}
