package term

import "testing"

func TestClearingTheScreenNotifiesTheOwner(t *testing.T) {
	screen := New(20, 5, 100)
	screen.Write([]byte("one\r\ntwo\r\nthree\r\nfour\r\nfive\r\nsix\r\n"))

	cleared, pruned := 0, 0
	screen.SetScreenCleared(func() { cleared++ })
	screen.SetScrollbackCleared(func() { pruned++ })

	if !screen.ClearScreen() {
		t.Fatal("ClearScreen on the main screen returned false")
	}
	if cleared != 1 {
		t.Fatalf("ClearScreen notified %d times, want 1", cleared)
	}
	screen.Write([]byte("\x1b[2J"))
	if cleared != 2 {
		t.Fatalf("CSI 2J notified %d times, want 2", cleared)
	}
	screen.Write([]byte("\x1b[3J"))
	if pruned != 1 {
		t.Fatalf("CSI 3J notified the scrollback owner %d times, want 1", pruned)
	}
	screen.Reset()
	if cleared != 3 {
		t.Fatalf("Reset notified %d times, want 3", cleared)
	}
}

func TestADisplayEraseOnTheAlternateScreenDoesNotNotify(t *testing.T) {
	screen := New(20, 5, 100)
	cleared := 0
	screen.SetScreenCleared(func() { cleared++ })
	screen.Write([]byte("\x1b[?1049h\x1b[2J"))
	if cleared != 0 {
		t.Fatalf("an alternate-screen erase notified %d times", cleared)
	}
}
