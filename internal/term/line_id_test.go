package term

import "testing"

func TestLineIDNamesTheLineThatLineNumberGives(t *testing.T) {
	screen := New(10, 3, 100)
	screen.Write([]byte("one\r\ntwo\r\nthree\r\nfour\r\nfive"))
	// "one" and "two" scrolled off; the view shows three, four, five.
	if screen.Scrollback() != 2 {
		t.Fatalf("scrollback = %d, want 2", screen.Scrollback())
	}
	check := func(what string) {
		for row := 0; row < 3; row++ {
			id, ok := screen.LineID(screen.LineNumber(row))
			if !ok || id != screen.Row(row).ID {
				t.Fatalf("%s: line %d = %d, %v; want row %d's ID %d", what, screen.LineNumber(row), id, ok, row, screen.Row(row).ID)
			}
		}
	}
	check("live screen")
	if id, ok := screen.LineID(screen.LineNumber(2) + 1); ok || id != 0 {
		t.Fatalf("a line number past the last line = %d, %v; want no line", id, ok)
	}
	screen.Scroll(2)
	check("history")
	if id, ok := screen.LineID(screen.LineNumber(0) - 1); ok || id != 0 {
		t.Fatalf("a dropped line number = %d, %v; want no line", id, ok)
	}
}
