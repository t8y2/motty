package term

import (
	"strconv"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
)

// EncodeWin32Key turns a key event into one win32-input-mode packet:
//
//	CSI Vk ; Sc ; Uc ; Kd ; Cs ; Rc _
//
// Vk and Sc are the Windows virtual-key and scan codes, Uc the UTF-16 code
// unit (zero for a key that types nothing), Kd 1 for a press and 0 for a
// release, Cs the console control-key bitset and Rc the repeat count. It
// returns nil when the event carries no console identity, which is every key
// on a platform without one.
func EncodeWin32Key(event gfx.KeyEvent) []byte {
	key := event.Win32
	if key.VirtualKey == 0 && key.Unicode == 0 {
		return nil
	}
	down := 1
	if event.Release {
		down = 0
	}
	repeat := key.Repeat
	if repeat == 0 {
		repeat = 1
	}
	b := make([]byte, 0, 32)
	b = append(b, "\x1b["...)
	b = strconv.AppendUint(b, uint64(key.VirtualKey), 10)
	b = append(b, ';')
	b = strconv.AppendUint(b, uint64(key.ScanCode), 10)
	b = append(b, ';')
	b = strconv.AppendUint(b, uint64(key.Unicode), 10)
	b = append(b, ';')
	b = strconv.AppendInt(b, int64(down), 10)
	b = append(b, ';')
	b = strconv.AppendUint(b, uint64(key.ControlState), 10)
	b = append(b, ';')
	b = strconv.AppendUint(b, uint64(repeat), 10)
	return append(b, '_')
}

// IsWin32KeyPacket reports whether p begins with a valid win32-input-mode packet.
func IsWin32KeyPacket(p []byte) bool {
	_, _, _, _, _, _, _, ok := parseWin32Packet(p)
	return ok
}

// Win32LogicalBytes converts win32-input-mode packets (DECSET 9001) in p into
// the logical bytes a terminal stream expects (such as characters, \r for Enter,
// \x7f for Backspace, \x03 for Ctrl+C). If p is not a win32 input packet, it
// returns p untouched.
func Win32LogicalBytes(p []byte) []byte {
	vk, sc, uc, kd, cs, rc, next, ok := parseWin32Packet(p)
	if !ok {
		return p
	}
	var out []byte
	for ok {
		if kd == 1 {
			out = appendWin32Logical(out, vk, sc, uc, cs, rc)
		}
		if len(next) == 0 {
			break
		}
		vk, sc, uc, kd, cs, rc, next, ok = parseWin32Packet(next)
	}
	return out
}

func parseWin32Packet(p []byte) (vk, sc, uc uint16, kd int, cs uint32, rc int, next []byte, ok bool) {
	if len(p) < 14 || p[0] != 0x1b || p[1] != '[' {
		return 0, 0, 0, 0, 0, 0, nil, false
	}
	end := -1
	for i := 2; i < len(p); i++ {
		if p[i] == '_' {
			end = i
			break
		}
	}
	if end < 0 {
		return 0, 0, 0, 0, 0, 0, nil, false
	}
	fields := [6]uint64{}
	fieldIdx := 0
	val := uint64(0)
	hasDigit := false
	for i := 2; i < end; i++ {
		c := p[i]
		if c >= '0' && c <= '9' {
			val = val*10 + uint64(c-'0')
			hasDigit = true
		} else if c == ';' {
			if !hasDigit || fieldIdx >= 5 {
				return 0, 0, 0, 0, 0, 0, nil, false
			}
			fields[fieldIdx] = val
			fieldIdx++
			val = 0
			hasDigit = false
		} else {
			return 0, 0, 0, 0, 0, 0, nil, false
		}
	}
	if !hasDigit || fieldIdx != 5 {
		return 0, 0, 0, 0, 0, 0, nil, false
	}
	fields[5] = val
	return uint16(fields[0]), uint16(fields[1]), uint16(fields[2]), int(fields[3]), uint32(fields[4]), int(fields[5]), p[end+1:], true
}

func appendWin32Logical(dst []byte, vk, sc, uc uint16, cs uint32, rc int) []byte {
	if rc <= 0 {
		rc = 1
	}
	if uc > 0 {
		var buf [utf8.UTFMax]byte
		var b []byte
		switch uc {
		case 13:
			b = []byte("\r")
		case 8:
			b = []byte{0x7f}
		case 9:
			b = []byte("\t")
		case 27:
			b = []byte("\x1b")
		default:
			if uc < 32 {
				b = []byte{byte(uc)}
			} else {
				n := utf8.EncodeRune(buf[:], rune(uc))
				b = buf[:n]
			}
		}
		for i := 0; i < rc; i++ {
			dst = append(dst, b...)
		}
		return dst
	}
	var seq string
	switch vk {
	case 38: // Up
		seq = "\x1b[A"
	case 40: // Down
		seq = "\x1b[B"
	case 39: // Right
		seq = "\x1b[C"
	case 37: // Left
		seq = "\x1b[D"
	case 36: // Home
		seq = "\x1b[H"
	case 35: // End
		seq = "\x1b[F"
	case 33: // PageUp
		seq = "\x1b[5~"
	case 34: // PageDown
		seq = "\x1b[6~"
	case 45: // Insert
		seq = "\x1b[2~"
	case 46: // Delete
		seq = "\x1b[3~"
	case 27: // Escape
		seq = "\x1b"
	}
	if seq != "" {
		for i := 0; i < rc; i++ {
			dst = append(dst, seq...)
		}
	}
	return dst
}

