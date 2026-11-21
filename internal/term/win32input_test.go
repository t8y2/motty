package term

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestEncodeWin32KeyPacket(t *testing.T) {
	event := gfx.KeyEvent{Win32: gfx.Win32Key{VirtualKey: 'A', ScanCode: 0x1e, Unicode: 'a', Repeat: 2}}
	if got, want := string(EncodeWin32Key(event)), "\x1b[65;30;97;1;0;2_"; got != want {
		t.Fatalf("press = %q, want %q", got, want)
	}
	event.Release = true
	if got, want := string(EncodeWin32Key(event)), "\x1b[65;30;97;0;0;2_"; got != want {
		t.Fatalf("release = %q, want %q", got, want)
	}
	if got := EncodeWin32Key(gfx.KeyEvent{Text: "a"}); got != nil {
		t.Fatalf("an event without a console record encoded as %q", got)
	}
}

func TestWin32InputModeIsAcceptedOnlyWhereThePlatformHasIt(t *testing.T) {
	term := newTerm(20, 3)
	feed(term, "\x1b[?9001h")
	if win32InputSupported {
		if !term.Modes().Win32Input || term.modeState(9001, true) != 1 {
			t.Fatalf("9001 was not set: modes=%+v state=%d", term.Modes(), term.modeState(9001, true))
		}
	} else {
		if term.Modes().Win32Input || term.modeState(9001, true) != 0 {
			t.Fatalf("9001 was accepted without console key records: modes=%+v state=%d", term.Modes(), term.modeState(9001, true))
		}
	}
}

func TestWin32LogicalKey(t *testing.T) {
	for _, test := range []struct {
		name  string
		event gfx.KeyEvent
		want  string
	}{
		{"text", gfx.KeyEvent{Win32: gfx.Win32Key{VirtualKey: 'A', Unicode: 'a', Repeat: 2}}, "aa"},
		{"release", gfx.KeyEvent{Release: true, Win32: gfx.Win32Key{VirtualKey: 'A', Unicode: 'a'}}, ""},
		{"enter", gfx.KeyEvent{Win32: gfx.Win32Key{VirtualKey: 13, Unicode: 13}}, "\r"},
		{"backspace", gfx.KeyEvent{Win32: gfx.Win32Key{VirtualKey: 8, Unicode: 8}}, "\x7f"},
		{"tab", gfx.KeyEvent{Win32: gfx.Win32Key{VirtualKey: 9, Unicode: 9}}, "\t"},
		{"up", gfx.KeyEvent{Win32: gfx.Win32Key{VirtualKey: 38}}, "\x1b[A"},
		{"surrogate", gfx.KeyEvent{Win32: gfx.Win32Key{VirtualKey: 'A', Unicode: 0xd83d}}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := string(Win32LogicalKey(test.event)); got != test.want {
				t.Fatalf("logical input = %q, want %q", got, test.want)
			}
		})
	}
}
