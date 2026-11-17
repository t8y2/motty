// Package zmodem implements the ZMODEM file transfer protocol as two engines, a
// Receiver and a Sender, that never touch the network or a terminal themselves:
// the owner feeds them the bytes that arrive, and sends the bytes they produce.
// That is what a terminal needs, where the transfer starts in the middle of a
// shell's output (a remote sz or rz began) and ends in the middle of it.
//
// The engines run on the caller's thread, do no waiting and start no goroutines.
// Everything that came from the other end is treated as hostile: a name is
// reduced to a file name, a size claim allocates nothing, and nothing a peer
// asks for (a command, a path) is carried out.
package zmodem

import (
	"hash/crc32"
)

// Control characters of the protocol.
const (
	zpad   = '*'
	zdle   = 0x18
	zbin   = 'A' // binary header, 16-bit CRC
	zhex   = 'B' // hex header, 16-bit CRC
	zbin32 = 'C' // binary header, 32-bit CRC
	xon    = 0x11
	xoff   = 0x13
)

// Frame types.
const (
	zRQINIT    = 0
	zRINIT     = 1
	zSINIT     = 2
	zACK       = 3
	zFILE      = 4
	zSKIP      = 5
	zNAK       = 6
	zABORT     = 7
	zFIN       = 8
	zRPOS      = 9
	zDATA      = 10
	zEOF       = 11
	zFERR      = 12
	zCRC       = 13
	zCHALLENGE = 14
	zCOMPL     = 15
	zCAN       = 16
	zFREECNT   = 17
	zCOMMAND   = 18
)

// Data subpacket endings.
const (
	zcrce = 'h' // end of frame, header follows
	zcrcg = 'i' // more data follows, no answer wanted
	zcrcq = 'j' // more data follows, answer wanted
	zcrcw = 'k' // end of frame, answer wanted, header follows
)

// Receiver capability flags (ZF0 of ZRINIT).
const (
	canFDX  = 0x01
	canOVIO = 0x02
	canFC32 = 0x20
	escCtl  = 0x40
)

// header is one decoded frame header.
type header struct {
	kind     byte
	position uint32  // the four argument bytes read as a little-endian number
	flags    [4]byte // the argument bytes in transmission order: ZF3, ZF2, ZF1, ZF0
	// crc32 says the data that follows has 32-bit checks.
	crc32 bool
	// hex says the frame arrived as a hex header, which the other end follows
	// with CR LF (the LF may carry its high bit).
	hex bool
}

func crc16Step(crc uint16, b byte) uint16 {
	crc ^= uint16(b) << 8
	for range 8 {
		if crc&0x8000 != 0 {
			crc = crc<<1 ^ 0x1021
		} else {
			crc <<= 1
		}
	}
	return crc
}

var crc16Table = func() (table [256]uint16) {
	for index := range table {
		table[index] = crc16Step(0, byte(index))
	}
	return
}()

// crc16Fast is the CRC-16/XMODEM of data continued from crc.
func crc16Fast(crc uint16, data []byte) uint16 {
	for _, b := range data {
		crc = crc<<8 ^ crc16Table[byte(crc>>8)^b]
	}
	return crc
}

// crc32Update continues the CRC-32 every ZMODEM 32-bit check is: the common
// IEEE one, which hash/crc32 keeps ready to continue between calls, so a check
// over several pieces starts from zero.
func crc32Update(crc uint32, data []byte) uint32 {
	return crc32.Update(crc, crc32.IEEETable, data)
}

// positionBytes is the four argument bytes of a header carrying a position.
func positionBytes(position uint32) [4]byte {
	return [4]byte{byte(position), byte(position >> 8), byte(position >> 16), byte(position >> 24)}
}

// needsEscape reports whether byte b has to be sent as ZDLE and b^0x40, given
// the byte sent before it.
func needsEscape(b, previous byte) bool {
	switch b {
	case zdle, 0x10, 0x90, xon, 0x91, xoff, 0x93:
		return true
	case 0x0d, 0x8d:
		// A carriage return after '@' would start Telnet's command escape.
		return previous&0x7f == '@'
	}
	return false
}

// appendEscaped appends data with ZDLE escaping. previous is the byte that
// precedes it in the stream, and the one that precedes the next call is returned.
func appendEscaped(dst, data []byte, previous byte) ([]byte, byte) {
	for _, b := range data {
		if needsEscape(b, previous) {
			dst = append(dst, zdle, b^0x40)
		} else {
			dst = append(dst, b)
		}
		previous = b
	}
	return dst, previous
}

// isHeaderEnd reports whether b is part of the CR LF that terminates a hex
// header. The LF may carry the high bit.
func isHeaderEnd(b byte) bool {
	return b == '\r' || b == '\n' || b == 0x8d || b == 0x8a
}

const hexDigits = "0123456789abcdef"

// appendHexHeader appends a hex header: the form every answer that has to be
// readable by anything uses.
func appendHexHeader(dst []byte, kind byte, args [4]byte) []byte {
	dst = append(dst, zpad, zpad, zdle, zhex)
	body := [5]byte{kind, args[0], args[1], args[2], args[3]}
	crc := crc16Fast(0, body[:])
	for _, b := range body {
		dst = append(dst, hexDigits[b>>4], hexDigits[b&15])
	}
	dst = append(dst, hexDigits[byte(crc>>12)&15], hexDigits[byte(crc>>8)&15], hexDigits[byte(crc>>4)&15], hexDigits[byte(crc)&15])
	dst = append(dst, '\r', '\n'|0x80)
	if kind != zACK && kind != zFIN {
		dst = append(dst, xon)
	}
	return dst
}

// appendBinaryHeader appends a binary header with a 16- or 32-bit check.
func appendBinaryHeader(dst []byte, kind byte, args [4]byte, wide bool) []byte {
	dst = append(dst, zpad, zdle)
	body := [5]byte{kind, args[0], args[1], args[2], args[3]}
	var previous byte
	if wide {
		dst = append(dst, zbin32)
		dst, previous = appendEscaped(dst, body[:], 0)
		crc := crc32Update(0, body[:])
		sum := [4]byte{byte(crc), byte(crc >> 8), byte(crc >> 16), byte(crc >> 24)}
		dst, _ = appendEscaped(dst, sum[:], previous)
		return dst
	}
	dst = append(dst, zbin)
	dst, previous = appendEscaped(dst, body[:], 0)
	crc := crc16Fast(0, body[:])
	sum := [2]byte{byte(crc >> 8), byte(crc)}
	dst, _ = appendEscaped(dst, sum[:], previous)
	return dst
}

// appendSubpacket appends one data subpacket: the escaped data, the ending and
// the check over both.
func appendSubpacket(dst, data []byte, end byte, wide bool) []byte {
	var previous byte
	dst, previous = appendEscaped(dst, data, 0)
	dst = append(dst, zdle, end)
	if wide {
		crc := crc32Update(crc32Update(0, data), []byte{end})
		sum := [4]byte{byte(crc), byte(crc >> 8), byte(crc >> 16), byte(crc >> 24)}
		dst, _ = appendEscaped(dst, sum[:], 0)
		return dst
	}
	crc := crc16Fast(crc16Fast(0, data), []byte{end})
	sum := [2]byte{byte(crc >> 8), byte(crc)}
	dst, _ = appendEscaped(dst, sum[:], previous)
	return dst
}

// cancelSequence is what abandons a transfer: eight CANs, which no terminal
// treats as anything but noise, then backspaces to wipe them off the screen.
var cancelSequence = []byte{zdle, zdle, zdle, zdle, zdle, zdle, zdle, zdle, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8}
