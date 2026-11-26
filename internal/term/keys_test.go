package term

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestEncodeKey(t *testing.T) {
	shift, alt, control, meta := gfx.ModifierShift, gfx.ModifierAlt, gfx.ModifierControl, gfx.ModifierMeta
	for _, test := range []struct {
		name      string
		key       gfx.Key
		modifiers gfx.Modifiers
		text      string
		appCursor bool
		backspace byte // zero is the default, DeleteByte
		want      string
	}{
		{"enter", gfx.KeyEnter, 0, "", false, 0, "\r"},
		{"shift+enter", gfx.KeyEnter, shift, "", false, 0, "\x1b[13;2u"},
		{"ctrl+enter", gfx.KeyEnter, control, "", false, 0, "\x1b[13;5u"},
		{"alt+enter", gfx.KeyEnter, alt, "", false, 0, "\x1b\r"},
		{"shift+alt+enter", gfx.KeyEnter, shift | alt, "", false, 0, "\x1b[13;4u"},
		{"ctrl+alt+enter", gfx.KeyEnter, control | alt, "", false, 0, "\x1b[13;7u"},
		{"tab", gfx.KeyTab, 0, "", false, 0, "\t"},
		{"shift+tab", gfx.KeyTab, shift, "", false, 0, "\x1b[Z"},
		{"escape", gfx.KeyEscape, 0, "", false, 0, "\x1b"},
		{"backspace", gfx.KeyBackspace, 0, "", false, 0, "\x7f"},
		{"ctrl+backspace deletes a word", gfx.KeyBackspace, control, "", false, 0, "\x17"},
		{"alt+backspace deletes a word", gfx.KeyBackspace, alt, "", false, 0, "\x1b\x7f"},
		{"ctrl+space", gfx.KeySpace, control, "", false, 0, "\x00"},
		{"up", gfx.KeyUp, 0, "", false, 0, "\x1b[A"},
		{"up in application mode", gfx.KeyUp, 0, "", true, 0, "\x1bOA"},
		{"down", gfx.KeyDown, 0, "", false, 0, "\x1b[B"},
		{"right", gfx.KeyRight, 0, "", false, 0, "\x1b[C"},
		{"left", gfx.KeyLeft, 0, "", false, 0, "\x1b[D"},
		{"home", gfx.KeyHome, 0, "", false, 0, "\x1b[H"},
		{"end in application mode", gfx.KeyEnd, 0, "", true, 0, "\x1bOF"},
		{"ctrl+left", gfx.KeyLeft, control, "", false, 0, "\x1b[1;5D"},
		{"alt+right", gfx.KeyRight, alt, "", false, 0, "\x1b[1;3C"},
		{"shift+up", gfx.KeyUp, shift, "", true, 0, "\x1b[1;2A"},
		{"ctrl+shift+alt+home", gfx.KeyHome, control | shift | alt, "", false, 0, "\x1b[1;8H"},
		{"insert", gfx.KeyInsert, 0, "", false, 0, "\x1b[2~"},
		{"delete", gfx.KeyDelete, 0, "", false, 0, "\x1b[3~"},
		{"page up", gfx.KeyPageUp, 0, "", false, 0, "\x1b[5~"},
		{"page down", gfx.KeyPageDown, 0, "", false, 0, "\x1b[6~"},
		{"ctrl+delete", gfx.KeyDelete, control, "", false, 0, "\x1b[3;5~"},
		{"f1", gfx.KeyF1, 0, "", false, 0, "\x1bOP"},
		{"f4", gfx.KeyF4, 0, "", false, 0, "\x1bOS"},
		{"shift+f2", gfx.KeyF2, shift, "", false, 0, "\x1b[1;2Q"},
		{"f5", gfx.KeyF5, 0, "", false, 0, "\x1b[15~"},
		{"f10", gfx.KeyF10, 0, "", false, 0, "\x1b[21~"},
		{"f12", gfx.KeyF12, 0, "", false, 0, "\x1b[24~"},
		{"ctrl+f6", gfx.KeyF6, control, "", false, 0, "\x1b[17;5~"},
		{"ctrl+c", gfx.KeyUnknown, control, "c", false, 0, "\x03"},
		{"ctrl+C as the Shift-less base letter", gfx.KeyUnknown, control, "C", false, 0, "\x03"},
		{"ctrl+d", gfx.KeyUnknown, control, "d", false, 0, "\x04"},
		{"ctrl+z", gfx.KeyUnknown, control, "z", false, 0, "\x1a"},
		{"ctrl+[", gfx.KeyUnknown, control, "[", false, 0, "\x1b"},
		{"ctrl+\\", gfx.KeyUnknown, control, "\\", false, 0, "\x1c"},
		{"ctrl+/", gfx.KeyUnknown, control, "/", false, 0, "\x1f"},
		{"ctrl+2", gfx.KeyUnknown, control, "2", false, 0, "\x00"},
		{"ctrl+8", gfx.KeyUnknown, control, "8", false, 0, "\x7f"},
		{"ctrl+alt+c", gfx.KeyUnknown, control | alt, "c", false, 0, "\x1b\x03"},
		{"alt+x", gfx.KeyUnknown, alt, "x", false, 0, "\x1bx"},
		{"ctrl+1 has no control code", gfx.KeyUnknown, control, "1", false, 0, ""},
		{"command chords belong to the application", gfx.KeyUnknown, meta, "c", false, 0, ""},
		{"command+left goes to the start of the line", gfx.KeyLeft, meta, "", false, 0, "\x01"},
		{"command+right goes to the end of the line", gfx.KeyRight, meta, "", false, 0, "\x05"},
		{"command+backspace deletes to the start of the line", gfx.KeyBackspace, meta, "", false, 0, "\x15"},
		{"command+left ignores application cursor keys", gfx.KeyLeft, meta, "", true, 0, "\x01"},
		{"command+shift+left is not a line edit", gfx.KeyLeft, meta | shift, "", false, 0, ""},
		{"command+alt+backspace is not a line edit", gfx.KeyBackspace, meta | alt, "", false, 0, ""},
		{"command+control+right is not a line edit", gfx.KeyRight, meta | control, "", false, 0, ""},
		{"command+up stays local", gfx.KeyUp, meta, "", false, 0, ""},
		{"command+forward delete stays local", gfx.KeyDelete, meta, "", false, 0, ""},
		{"nothing at all", gfx.KeyUnknown, 0, "", false, 0, ""},
		{"a multi-character chord", gfx.KeyUnknown, control, "ab", false, 0, ""},
		// A serial host whose console erases on BS alone makes its Backspace key
		// that byte; the chords keep their own meanings and DEL stays reachable.
		{"backspace of a Ctrl+H host", gfx.KeyBackspace, 0, "", false, CtrlHByte, "\b"},
		{"shift+backspace of a Ctrl+H host", gfx.KeyBackspace, shift, "", false, CtrlHByte, "\b"},
		{"alt+backspace of a Ctrl+H host", gfx.KeyBackspace, alt, "", false, CtrlHByte, "\x1b\b"},
		{"ctrl+backspace of a Ctrl+H host still deletes a word", gfx.KeyBackspace, control, "", false, CtrlHByte, "\x17"},
		{"ctrl+8 of a Ctrl+H host is still DEL", gfx.KeyUnknown, control, "8", false, CtrlHByte, "\x7f"},
	} {
		backspace := test.backspace
		if backspace == 0 {
			backspace = DeleteByte
		}
		got := string(EncodeKey(test.key, test.modifiers, test.text, test.appCursor, backspace))
		if got != test.want {
			t.Errorf("%s: %q, want %q", test.name, got, test.want)
		}
	}
}

func TestEncodePaste(t *testing.T) {
	for _, test := range []struct {
		name      string
		text      string
		bracketed bool
		want      string
	}{
		{"plain text", "ls -la", false, "ls -la"},
		{"line breaks become returns", "a\nb\r\nc", false, "a\rb\rc"},
		{"bracketed", "echo hi\n", true, "\x1b[200~echo hi\r\x1b[201~"},
		{"the end marker cannot be smuggled in", "x\x1b[201~rm -rf ~", true, "\x1b[200~xrm -rf ~\x1b[201~"},
		{"empty", "", true, "\x1b[200~\x1b[201~"},
		{"CJK is untouched", "你好", false, "你好"},
	} {
		if got := string(EncodePaste(test.text, test.bracketed)); got != test.want {
			t.Errorf("%s: %q, want %q", test.name, got, test.want)
		}
	}
}
