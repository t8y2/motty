package xymodem

import (
	"errors"
	"fmt"
	"io"
	"time"

	"motty/internal/zmodem"
)

type senderState uint8

const (
	sWaitStart     senderState = iota // waiting for the receiver's 'C' or NAK
	sWaitInfoAck                      // YMODEM: block 0 is out
	sWaitDataStart                    // YMODEM: block 0 was acknowledged; waiting for 'C'
	sWaitAck                          // a data block is out
	sWaitEOTAck                       // EOT is out
	sWaitNextStart                    // YMODEM: the file ended; waiting for 'C' for the next block 0
	sWaitFinalAck                     // YMODEM: the empty block 0 is out
)

// Sender sends files. XMODEM sends one; YMODEM a batch.
type Sender struct {
	base
	mode  Mode
	files []zmodem.SendFile

	state  senderState
	index  int
	useCRC bool
	// number is the block number of the one in flight; offset is where the
	// next data starts.
	number byte
	offset int64
	// block is the bytes of the block in flight, for sending it again.
	block []byte
	eots  int
	sent  int
	// handshake is set while block 1 of an XMODEM file is in flight and
	// unacknowledged: the receiver keeps sending its start NAK until it sees
	// SOH, so NAKs queued before the block arrived are not complaints about it.
	// A real NAK is recovered by Timeout.
	handshake bool
}

var errShort = errors.New("xymodem: the file is shorter than it was when the transfer began")

// NewSender prepares to send files. It sends nothing until the receiver asks.
// XMODEM sends only the first file.
func NewSender(mode Mode, files []zmodem.SendFile) *Sender {
	if mode != YMODEM && len(files) > 1 {
		files = files[:1]
	}
	return &Sender{mode: mode, files: files, useCRC: true}
}

func (s *Sender) Progress() zmodem.Progress {
	progress := zmodem.Progress{Files: s.sent, Sending: true}
	if s.index < len(s.files) && s.state >= sWaitAck && s.state <= sWaitEOTAck {
		file := s.files[s.index]
		progress.Name, progress.Size, progress.Done = file.Name, file.Size, min(s.offset, file.Size)
	}
	return progress
}

func (s *Sender) Cancel() {
	if !s.finished {
		s.abort(ErrCancelled)
	}
}

// Timeout sends again what the receiver has not answered.
func (s *Sender) Timeout() {
	if s.finished {
		return
	}
	s.quiet++
	if s.quiet > maxRetries {
		s.abort(ErrTimeout)
		return
	}
	if s.state != sWaitStart && s.state != sWaitDataStart && s.state != sWaitNextStart && len(s.block) > 0 {
		s.out = append(s.out, s.block...)
	}
}

func (s *Sender) Feed(data []byte) {
	for index, value := range data {
		if s.finished {
			s.leftover = append(s.leftover, data[index:]...)
			return
		}
		s.quiet = 0
		if s.cancelled(value) {
			s.fail(ErrPeerCancelled)
			continue
		}
		s.reply(value)
	}
}

func (s *Sender) reply(value byte) {
	switch s.state {
	case sWaitStart:
		switch value {
		case crc:
			s.useCRC = true
		case nak:
			if s.mode == YMODEM {
				return // YMODEM needs CRC
			}
			s.useCRC = false
		default:
			return
		}
		if s.mode == YMODEM {
			s.sendInfo()
		} else {
			s.sendData()
		}
	case sWaitInfoAck:
		switch value {
		case ack:
			s.state = sWaitDataStart
		case nak:
			s.resend()
		}
	case sWaitDataStart:
		if value == crc {
			s.sendData()
		}
	case sWaitAck:
		switch value {
		case ack:
			s.retries = 0
			s.handshake = false
			if s.offset >= s.files[s.index].Size {
				s.sendEOT()
			} else {
				s.sendData()
			}
		case nak:
			if !s.handshake {
				s.resend()
			}
		}
	case sWaitEOTAck:
		switch value {
		case ack:
			s.sent++
			s.index++
			s.eots = 0
			if s.mode == YMODEM {
				s.state = sWaitNextStart
			} else {
				s.finished = true
			}
		case nak:
			if s.mode == YMODEM && s.eots == 1 {
				// The receiver is making sure: say it again.
				s.sendEOT()
			} else {
				s.resend()
			}
		}
	case sWaitNextStart:
		if value == crc {
			if s.index < len(s.files) {
				s.sendInfo()
			} else {
				s.sendEmptyInfo()
			}
		}
	case sWaitFinalAck:
		if value == ack {
			s.finished = true
		} else if value == nak {
			s.resend()
		}
	}
}

// resend sends the block in flight again, a limited number of times.
func (s *Sender) resend() {
	s.retries++
	if s.retries > maxRetries {
		s.abort(ErrTooManyErrors)
		return
	}
	s.out = append(s.out, s.block...)
}

// frame builds a block of the given data length around data.
func (s *Sender) frame(number byte, data []byte, length int, pad byte) []byte {
	header := byte(soh)
	if length == 1024 {
		header = stx
	}
	block := make([]byte, 0, 3+length+2)
	block = append(block, header, number, ^number)
	block = append(block, data...)
	for len(block) < 3+length {
		block = append(block, pad)
	}
	body := block[3:]
	if s.useCRC {
		sum := crc16(body)
		block = append(block, byte(sum>>8), byte(sum))
	} else {
		block = append(block, checksum(body))
	}
	return block
}

func (s *Sender) sendBlock(block []byte) {
	s.block = block
	s.out = append(s.out, block...)
}

// sendInfo sends YMODEM's block 0 for the current file.
func (s *Sender) sendInfo() {
	file := s.files[s.index]
	mtime := int64(0)
	if !file.ModTime.IsZero() {
		mtime = file.ModTime.Unix()
	}
	info := fmt.Sprintf("%s\x00%d %o %o", plainName(file.Name), file.Size, mtime, file.Mode&0o7777|0o100000)
	length := 128
	if len(info) > 128 {
		length = 1024
	}
	s.offset, s.number, s.retries = 0, 0, 0
	s.sendBlock(s.frame(0, []byte(info), length, 0))
	s.state = sWaitInfoAck
}

// sendEmptyInfo ends a YMODEM batch.
func (s *Sender) sendEmptyInfo() {
	s.sendBlock(s.frame(0, nil, 128, 0))
	s.state = sWaitFinalAck
}

// sendData sends the next block of the file.
func (s *Sender) sendData() {
	file := s.files[s.index]
	if file.Size == 0 {
		s.sendEOT() // an empty file has no blocks
		return
	}
	length := 128
	if s.mode != XMODEM && file.Size-s.offset > 128 {
		length = 1024
	}
	want := int64(length)
	if left := file.Size - s.offset; left < want {
		want = left
	}
	data := make([]byte, want)
	if want > 0 {
		n, err := file.Source.ReadAt(data, s.offset)
		if int64(n) < want {
			if err == nil || errors.Is(err, io.EOF) {
				err = errShort
			}
			s.abort(err)
			return
		}
	}
	s.offset += want
	s.number++
	pad := byte(sub)
	if s.mode == YMODEM {
		pad = 0
	}
	s.sendBlock(s.frame(s.number, data, length, pad))
	s.handshake = s.number == 1 && s.mode != YMODEM
	s.state = sWaitAck
}

func (s *Sender) sendEOT() {
	s.eots++
	s.block = []byte{eot}
	s.out = append(s.out, eot)
	s.state = sWaitEOTAck
}

var _ = time.Second
