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

func TestWin32LogicalBytes(t *testing.T) {
	// Not a win32 packet
	plain := []byte("hello world")
	if got := Win32LogicalBytes(plain); string(got) != "hello world" {
		t.Fatalf("plain = %q", got)
	}

	// Press of 'a'
	pressA := EncodeWin32Key(gfx.KeyEvent{Win32: gfx.Win32Key{VirtualKey: 'A', ScanCode: 0x1e, Unicode: 'a', Repeat: 1}})
	if got := Win32LogicalBytes(pressA); string(got) != "a" {
		t.Fatalf("pressA = %q, want 'a'", got)
	}

	// Release of 'a' -> should return empty
	relA := EncodeWin32Key(gfx.KeyEvent{Win32: gfx.Win32Key{VirtualKey: 'A', ScanCode: 0x1e, Unicode: 'a'}, Release: true})
	if got := Win32LogicalBytes(relA); len(got) != 0 {
		t.Fatalf("relA = %q, want empty", got)
	}

	// Press of Enter (13)
	pressEnter := EncodeWin32Key(gfx.KeyEvent{Win32: gfx.Win32Key{VirtualKey: 13, ScanCode: 28, Unicode: 13, Repeat: 1}})
	if got := Win32LogicalBytes(pressEnter); string(got) != "\r" {
		t.Fatalf("pressEnter = %q, want '\\r'", got)
	}

	// Press of Backspace (8)
	pressBS := EncodeWin32Key(gfx.KeyEvent{Win32: gfx.Win32Key{VirtualKey: 8, ScanCode: 14, Unicode: 8, Repeat: 1}})
	if got := Win32LogicalBytes(pressBS); string(got) != "\x7f" {
		t.Fatalf("pressBS = %q, want '\\x7f'", got)
	}

	// Press of Tab (9)
	pressTab := EncodeWin32Key(gfx.KeyEvent{Win32: gfx.Win32Key{VirtualKey: 9, ScanCode: 15, Unicode: 9, Repeat: 1}})
	if got := Win32LogicalBytes(pressTab); string(got) != "\t" {
		t.Fatalf("pressTab = %q, want '\\t'", got)
	}

	// Press of Up arrow (vk 38, uc 0)
	pressUp := EncodeWin32Key(gfx.KeyEvent{Win32: gfx.Win32Key{VirtualKey: 38, ScanCode: 72, Unicode: 0, Repeat: 1}})
	if got := Win32LogicalBytes(pressUp); string(got) != "\x1b[A" {
		t.Fatalf("pressUp = %q, want '\\x1b[A'", got)
	}
}
