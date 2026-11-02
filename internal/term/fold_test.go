package term

import "testing"

// foldedScreen is a screen showing one command and what it printed:
//
//	0  $ ll        <- the command row, marked
//	1  alpha
//	2  beta
//	3  gamma
//	4  $           <- the prompt the command left behind
func foldedScreen(t *testing.T) *Terminal {
	t.Helper()
	screen := New(20, 6, 100)
	screen.Write([]byte("$ ll"))
	screen.MarkCommand()
	screen.Write([]byte("\r\nalpha\r\nbeta\r\ngamma\r\n$ "))
	return screen
}

func TestFoldHidesTheRowsACommandPrinted(t *testing.T) {
	screen := foldedScreen(t)
	if info := screen.FoldAt(0); !info.Anchor || info.Folded {
		t.Fatalf("row 0 = %+v, want an open anchor", info)
	}
	if info := screen.FoldAt(2); !info.Inside {
		t.Fatalf("row 2 = %+v, want inside the block", info)
	}
	if info := screen.FoldAt(4); info.Anchor || info.Inside {
		t.Fatalf("row 4 = %+v, want an ordinary row", info)
	}

	if !screen.ToggleFold(0) {
		t.Fatal("folding the command row changed nothing")
	}
	// The anchor stays, the prompt that followed it moves up, and the numbers
	// after the block keep counting from the anchor.
	if got := screen.RowText(0); got != "$ ll" {
		t.Fatalf("row 0 = %q", got)
	}
	if got := screen.RowText(1); got != "$" {
		t.Fatalf("row 1 = %q, want the prompt pulled up", got)
	}
	if got := screen.LineNumber(1); got != 5 {
		t.Fatalf("row 1 number = %d, want 5", got)
	}
	if got := screen.Cursor().Y; got != 1 {
		t.Fatalf("cursor row = %d, want 1", got)
	}
	if got := screen.ViewLine(1); got != 4 {
		t.Fatalf("row 1 shows buffer line %d, want 4", got)
	}
	if got := screen.FoldAt(0); !got.Folded {
		t.Fatalf("row 0 = %+v, want folded", got)
	}

	if !screen.ToggleFold(0) {
		t.Fatal("opening the block changed nothing")
	}
	for row, want := range []string{"$ ll", "alpha", "beta", "gamma", "$"} {
		if got := screen.RowText(row); got != want {
			t.Fatalf("row %d = %q, want %q", row, got, want)
		}
	}
}

func TestFoldOfACommandInTheScrollback(t *testing.T) {
	screen := New(20, 4, 100)
	// Two commands, the first long enough to scroll its top away.
	screen.Write([]byte("$ one"))
	screen.MarkCommand()
	screen.Write([]byte("\r\n1\r\n2\r\n3\r\n4\r\n$ two"))
	screen.MarkCommand()
	screen.Write([]byte("\r\nx\r\ny\r\n$ "))
	if screen.Scrollback() == 0 {
		t.Fatal("the first command did not scroll off")
	}
	// Fold the second command, the one whose rows are all on screen.
	var anchor int
	for row := 0; row < 4; row++ {
		if screen.FoldAt(row).Anchor {
			anchor = row
		}
	}
	if got := screen.RowText(anchor); got != "$ two" {
		t.Fatalf("anchor row %d = %q, want the second command", anchor, got)
	}
	// The prompt the second command left behind. Folding moves it up under the
	// anchor but leaves its number alone: the rows the block hid keep theirs,
	// they are only not shown.
	prompt, promptNumber := -1, uint64(0)
	for row := 0; row < 4; row++ {
		if screen.RowText(row) == "$" {
			prompt, promptNumber = row, screen.LineNumber(row)
		}
	}
	if prompt <= anchor {
		t.Fatalf("no prompt after the anchor: %q", screen.Text())
	}
	if !screen.ToggleFold(anchor) {
		t.Fatal("folding the second command changed nothing")
	}
	if got := screen.RowText(anchor + 1); got != "$" {
		t.Fatalf("row after the anchor = %q, want the prompt", got)
	}
	if got := screen.LineNumber(anchor + 1); got != promptNumber {
		t.Fatalf("number after the block = %d, want %d", got, promptNumber)
	}
}

func TestAlternateScreenRecordsNoCommands(t *testing.T) {
	screen := New(20, 4, 100)
	screen.Write([]byte("\x1b[?1049h"))
	screen.MarkCommand()
	if info := screen.FoldAt(0); info.Anchor {
		t.Fatalf("alternate screen row = %+v, want no block", info)
	}
	if screen.ToggleFold(0) {
		t.Fatal("the alternate screen folded a row")
	}
}
