package term

import (
	"strconv"
	"unicode/utf16"
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

func Win32LogicalKey(event gfx.KeyEvent) []byte {
	if event.Release || event.Key.IsModifier() {
		return nil
	}
	uc, vk := event.Win32.Unicode, event.Win32.VirtualKey
	rc := int(event.Win32.Repeat)
	if rc == 0 {
		rc = 1
	}
	var dst []byte
	if uc > 0 {
		if utf16.IsSurrogate(rune(uc)) {
			return nil
		}
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
	if seq == "" {
		seq = string(EncodeKeyEvent(event, 0, false))
	}
	if seq != "" {
		for i := 0; i < rc; i++ {
			dst = append(dst, seq...)
		}
	}
	return dst
}
