// Package xymodem implements XMODEM (checksum, CRC and 1K) and YMODEM batch
// transfers as two engines, a Receiver and a Sender, with the same shape as the
// ZMODEM ones: the owner feeds them the bytes that arrive, sends the bytes they
// produce, and calls Timeout when the line has been quiet. They never touch the
// network or a terminal themselves.
//
// Neither protocol announces itself in the stream, so a transfer is always
// started by the user, who says which protocol and which direction. A peer
// controls only the bytes of a file and, for YMODEM, a file name, which is
// reduced to a plain name; it cannot choose a path.
package xymodem

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"motty/internal/zmodem"
)

// The control bytes of both protocols.
const (
	soh = 0x01 // a block of 128 bytes follows
	stx = 0x02 // a block of 1024 bytes follows
	eot = 0x04
	ack = 0x06
	nak = 0x15
	can = 0x18
	sub = 0x1A // pads the last block of an XMODEM file
	crc = 'C'  // the receiver asks for CRC blocks
)

// Mode is the protocol.
type Mode uint8

const (
	// XMODEM is the original, with 128-byte blocks. The receiver asks for CRC
	// checks and falls back to the arithmetic checksum for an old sender.
	XMODEM Mode = iota
	// XMODEM1K sends 1024-byte blocks.
	XMODEM1K
	// YMODEM sends 1024-byte blocks and, before each file, its name and size.
	YMODEM
)

// Errors a transfer ends with.
var (
	ErrCancelled     = zmodem.ErrCancelled
	ErrPeerCancelled = zmodem.ErrPeerCancelled
	ErrTimeout       = zmodem.ErrTimeout
	// ErrTooManyErrors is a line that kept delivering damaged blocks.
	ErrTooManyErrors = errors.New("xymodem: too many damaged blocks")
	// ErrOutOfStep is a peer that sent a block number that does not follow.
	ErrOutOfStep = errors.New("xymodem: a block arrived out of order")
)

const (
	maxRetries = 10
	// blockOverhead is the bytes around a block's data: number and its
	// complement, plus the check.
	maxFileInfo = 1024
)

// crc16 is the CRC-16/XMODEM of data.
func crc16(data []byte) uint16 {
	var sum uint16
	for _, b := range data {
		sum ^= uint16(b) << 8
		for range 8 {
			if sum&0x8000 != 0 {
				sum = sum<<1 ^ 0x1021
			} else {
				sum <<= 1
			}
		}
	}
	return sum
}

func checksum(data []byte) byte {
	var sum byte
	for _, b := range data {
		sum += b
	}
	return sum
}

// base is what both engines share.
type base struct {
	out      []byte
	err      error
	finished bool
	leftover []byte
	// retries counts failures in a row; quiet counts timeouts in a row.
	retries, quiet int
	cans           int
}

func (b *base) TakeOutput(dst []byte) []byte {
	dst = append(dst, b.out...)
	b.out = b.out[:0]
	return dst
}

func (b *base) Finished() bool   { return b.finished }
func (b *base) Settled() bool    { return b.finished }
func (b *base) Err() error       { return b.err }
func (b *base) Leftover() []byte { return b.leftover }
func (b *base) Close()           {}

func (b *base) fail(err error) {
	if !b.finished {
		b.err, b.finished = err, true
	}
}

// abort tells the other end to stop and ends the transfer.
func (b *base) abort(err error) {
	if !b.finished {
		b.out = append(b.out[:0], can, can, can, can, can, 8, 8, 8, 8, 8)
		b.fail(err)
	}
}

// Abort ends the transfer because its bytes could not be delivered to the
// other end: a cancellation the terminal would not carry is not worth writing.
func (b *base) Abort(err error) { b.fail(err) }

// cancelled counts a CAN byte and reports whether two in a row have arrived.
func (b *base) cancelled(value byte) bool {
	if value != can {
		b.cans = 0
		return false
	}
	b.cans++
	return b.cans >= 2
}

// plainName reduces a name the peer sent to a file name.
func plainName(name string) string {
	if cut := strings.LastIndexAny(name, `/\`); cut >= 0 {
		name = name[cut+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`:*?"<>|`, r) {
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

// parseFileInfo reads YMODEM's block 0: the name, then "size mtime mode ..." with
// the time and mode in octal.
func parseFileInfo(data []byte) zmodem.FileInfo {
	name, rest, _ := strings.Cut(string(data), "\x00")
	info := zmodem.FileInfo{Name: plainName(name), Size: -1}
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

func describe(mode Mode) string {
	switch mode {
	case XMODEM1K:
		return "XMODEM-1K"
	case YMODEM:
		return "YMODEM"
	}
	return "XMODEM"
}

var _ = fmt.Sprint
