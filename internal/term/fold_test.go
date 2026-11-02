package term

import (
	"fmt"
	"strings"
	"testing"
)

// The shell integration's markers, as it sends them.
const (
	markerA = "\x1b]133;A\x07"
	markerB = "\x1b]133;B\x07"
	markerC = "\x1b]133;C\x07"
)

// shellPrompt is what the integration's prompt hook writes.
func shellPrompt(screen *Terminal, text string) { feed(screen, markerA+text+markerB) }

// shellRun is the user pressing Enter on a typed command, and what it printed.
func shellRun(screen *Terminal, command, output string) {
	feed(screen, command+"\r\n"+markerC+output)
}

// session is a screen showing one command and what it printed, then the prompt
// the command left behind:
//
//	0  $ ll        <- the block's anchor
//	1  alpha
//	2  beta
//	3  gamma
//	4  $
func session(t *testing.T, cols, rows, scrollback int) *Terminal {
	t.Helper()
	screen := New(cols, rows, scrollback)
	screen.SetFolding(true)
	shellPrompt(screen, "$ ")
	shellRun(screen, "ll", "alpha\r\nbeta\r\ngamma\r\n")
	shellPrompt(screen, "$ ")
	return screen
}

func viewText(screen *Terminal) []string {
	_, rows := screen.Size()
	out := make([]string, rows)
	for i := range out {
		out[i] = screen.RowText(i)
	}
	return out
}

// expectRows checks the view starts with want and has nothing after it.
func expectRows(t *testing.T, screen *Terminal, want ...string) {
	t.Helper()
	got := viewText(screen)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("view = %q, want it to start %q", got, want)
		}
	}
	for _, rest := range got[len(want):] {
		if rest != "" {
			t.Fatalf("view = %q, want it to end after %q", got, want)
		}
	}
}

func TestFoldHidesTheOutputAndKeepsTheCommand(t *testing.T) {
	screen := session(t, 20, 8, 100)
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
	expectRows(t, screen, "$ ll", "$")
	if got := screen.LineNumber(1); got != 5 {
		t.Fatalf("the prompt after the block is numbered %d, want 5: the hidden rows keep their numbers", got)
	}
	if got := screen.Cursor(); got.Y != 1 || !got.Visible {
		t.Fatalf("cursor = %+v, want it on the prompt pulled up to row 1", got)
	}
	if got := screen.ViewLine(1); got != 4 {
		t.Fatalf("row 1 shows buffer line %d, want 4", got)
	}
	if !screen.FoldAt(0).Folded {
		t.Fatal("the anchor does not say it is folded")
	}
	if !screen.ToggleFold(0) {
		t.Fatal("opening the block changed nothing")
	}
	expectRows(t, screen, "$ ll", "alpha", "beta", "gamma", "$")
}

func TestOnlyTheShellsMarkersMakeBlocks(t *testing.T) {
	// Output with no integration at all: nothing to fold.
	plain := New(20, 6, 100)
	plain.SetFolding(true)
	feed(plain, "$ ls\r\nalpha\r\nbeta\r\n$ ")
	for row := 0; row < 6; row++ {
		if plain.FoldAt(row) != (FoldInfo{}) || plain.ToggleFold(row) {
			t.Fatalf("row %d of a session without the integration offers a fold", row)
		}
	}
	if plain.ToggleLastFold() {
		t.Fatal("a session without the integration folded something")
	}

	// Once the terminal expects the shell's token, a program that sends the
	// standard markers on its own, a nested shell or an editor, opens no block.
	screen := New(20, 6, 100)
	screen.SetFolding(true)
	screen.SetPromptToken("secret")
	feed(screen, "\x1b]133;A\x07$ ls\x1b]133;B\x07\r\n\x1b]133;C\x07alpha\r\nbeta\r\n\x1b]133;A\x07$ ")
	if screen.FoldAt(0).Anchor {
		t.Fatal("markers without the shell's token opened a block")
	}
	screen = New(20, 6, 100)
	screen.SetFolding(true)
	screen.SetPromptToken("secret")
	feed(screen, "\x1b]133;A;motty=secret\x07$ ls\x1b]133;B;motty=secret\x07\r\n\x1b]133;C;motty=secret\x07alpha\r\nbeta\r\n\x1b]133;A;motty=secret\x07$ ")
	if !screen.FoldAt(0).Anchor {
		t.Fatal("the shell's own markers opened no block")
	}
}

func TestACommandWithNoOutputHasNothingToFold(t *testing.T) {
	screen := New(20, 6, 100)
	screen.SetFolding(true)
	shellPrompt(screen, "$ ")
	shellRun(screen, "true", "")
	shellPrompt(screen, "$ ")
	// An empty Enter starts another prompt with no command at all.
	feed(screen, "\r\n")
	shellPrompt(screen, "$ ")
	for row := 0; row < 6; row++ {
		if screen.FoldAt(row).Anchor {
			t.Fatalf("row %d offers a fold though no command printed anything", row)
		}
	}
}

func TestARunningCommandFoldsWhatItHasPrintedSoFar(t *testing.T) {
	screen := New(20, 8, 100)
	screen.SetFolding(true)
	shellPrompt(screen, "$ ")
	shellRun(screen, "tail -f log", "one\r\ntwo\r\npartial")
	if !screen.FoldAt(0).Anchor {
		t.Fatal("no box over a command that has printed full lines")
	}
	screen.ToggleFold(0)
	// The row the output is still being written on is not part of the block.
	expectRows(t, screen, "$ tail -f log", "partial")
	if got := screen.Cursor(); got.Y != 1 {
		t.Fatalf("cursor row = %d, want it on the unfinished row at 1", got.Y)
	}
}

func TestFoldFollowsTheBlockIntoTheScrollback(t *testing.T) {
	screen := New(20, 4, 100)
	screen.SetFolding(true)
	shellPrompt(screen, "$ ")
	shellRun(screen, "one", "1\r\n2\r\n3\r\n4\r\n")
	shellPrompt(screen, "$ ")
	shellRun(screen, "two", "x\r\ny\r\n")
	shellPrompt(screen, "$ ")
	if screen.Scrollback() == 0 {
		t.Fatal("the first command did not scroll off")
	}
	anchor := -1
	for row := 0; row < 4; row++ {
		if screen.FoldAt(row).Anchor {
			anchor = row
		}
	}
	if anchor < 0 || screen.RowText(anchor) != "$ two" {
		t.Fatalf("view %q, want the second command's box", viewText(screen))
	}
	screen.ToggleFold(anchor)
	// Scroll the first command back into the view and fold it there, too.
	screen.Scroll(screen.Scrollback())
	first := -1
	for row := 0; row < 4; row++ {
		if screen.FoldAt(row).Anchor && screen.RowText(row) == "$ one" {
			first = row
		}
	}
	if first < 0 {
		t.Fatalf("no box on the first command in the history view %q", viewText(screen))
	}
	screen.ToggleFold(first)
	screen.ScrollToBottom()
	expectRows(t, screen, "$ two", "$")
	screen.Scroll(screen.Scrollback())
	expectRows(t, screen, "$ one", "$ two", "$")
}

func TestAnAlternateScreenOverScrollbackIsNotFolded(t *testing.T) {
	// A full-screen program shows exactly what it drew whatever the main screen
	// holds above it, and the main screen comes back as it was.
	screen := New(20, 5, 100)
	screen.SetFolding(true)
	shellPrompt(screen, "$ ")
	shellRun(screen, "seq 1 20", strings.Repeat("n\r\n", 20))
	shellPrompt(screen, "$ vim")
	if screen.Scrollback() == 0 {
		t.Fatal("the output did not reach the scrollback")
	}
	before := viewText(screen)
	feed(screen, "\x1b[?1049h\x1b[H\x1b[2Jvim-content\x1b[3;1Hthird")
	if got := screen.RowText(0); got != "vim-content" {
		t.Fatalf("alternate screen row 0 = %q, want what the program drew", got)
	}
	if got := screen.RowText(2); got != "third" {
		t.Fatalf("alternate screen row 2 = %q", got)
	}
	if got := string(rowRunes(screen.Row(2))); got != "third" {
		t.Fatalf("alternate screen Row(2) = %q", got)
	}
	if got := screen.ViewLine(2); got != 2 {
		t.Fatalf("alternate screen row 2 is buffer line %d", got)
	}
	if info := screen.FoldAt(0); info != (FoldInfo{}) {
		t.Fatalf("alternate screen row 0 = %+v, want no fold", info)
	}
	if got, ok := screen.ScreenRow(3); !ok || got != 3 {
		t.Fatalf("ScreenRow(3) = %d, %v", got, ok)
	}
	if got := screen.Cursor(); got.Y != 2 {
		t.Fatalf("cursor = %+v, want the program's row", got)
	}
	feed(screen, "\x1b[?1049l")
	if got := viewText(screen); strings.Join(got, "|") != strings.Join(before, "|") {
		t.Fatalf("main screen came back as %q, was %q", got, before)
	}
}

func rowRunes(row Row) []rune {
	var out []rune
	for _, c := range row.Cells {
		if c.R != 0 {
			out = append(out, c.R)
		}
	}
	return out
}

func TestAWrappedCommandKeepsItsContinuationRowsWhenFolded(t *testing.T) {
	screen := New(10, 8, 100)
	screen.SetFolding(true)
	shellPrompt(screen, "$ ")
	shellRun(screen, "echo abcdefghijklmnop", "out1\r\nout2\r\n")
	shellPrompt(screen, "$ ")
	// The command wrapped over three rows: "$ echo abc", "defghijklm", "nop".
	if !screen.Row(0).Wrapped || !screen.Row(1).Wrapped {
		t.Fatalf("the command did not wrap: %q", viewText(screen))
	}
	if !screen.FoldAt(0).Anchor || screen.FoldAt(1).Anchor || screen.FoldAt(2).Anchor {
		t.Fatalf("the box belongs on the command's first row, got %+v %+v %+v", screen.FoldAt(0), screen.FoldAt(1), screen.FoldAt(2))
	}
	if !screen.FoldAt(1).Inside || !screen.FoldAt(2).Inside {
		t.Fatal("the command's tail rows are not inside its block, so the guide line would break")
	}
	screen.ToggleFold(0)
	expectRows(t, screen, "$ echo abc", "defghijklm", "nop", "$")
	if !screen.ContinuesAbove(1) || !screen.ContinuesAbove(2) {
		t.Fatal("the folded command's tail rows no longer say they continue the line above")
	}
}

func TestResizeKeepsABlockAnchoredOnItsCommand(t *testing.T) {
	build := func() *Terminal {
		screen := New(24, 10, 100)
		screen.SetFolding(true)
		shellPrompt(screen, "$ ")
		shellRun(screen, "echo abcdefghijklmnop", "out1\r\nout2\r\n")
		shellPrompt(screen, "$ ")
		return screen
	}

	// Narrower: the command wraps into more rows; the box and the fold stay
	// with the command's first row and its tail is never hidden.
	screen := build()
	screen.ToggleFold(0)
	screen.Resize(8, 10)
	checkInvariants(t, screen)
	if !screen.FoldAt(0).Anchor || !screen.FoldAt(0).Folded {
		t.Fatalf("after narrowing, row 0 = %+v: %q", screen.FoldAt(0), viewText(screen))
	}
	expectRows(t, screen, "$ echo a", "bcdefghi", "jklmnop", "$")

	// Wider again: the rows rejoin and the fold is still on the same command.
	screen.Resize(40, 10)
	checkInvariants(t, screen)
	expectRows(t, screen, "$ echo abcdefghijklmnop", "$")
	if !screen.FoldAt(0).Folded {
		t.Fatal("widening opened a block the reader had folded")
	}
	screen.ToggleFold(0)
	expectRows(t, screen, "$ echo abcdefghijklmnop", "out1", "out2", "$")

	// An open block that is narrowed shows its output with the command intact.
	screen = build()
	screen.Resize(8, 10)
	expectRows(t, screen, "$ echo a", "bcdefghi", "jklmnop", "out1", "out2", "$")
	if info := screen.FoldAt(3); !info.Inside {
		t.Fatalf("output row = %+v, want inside", info)
	}
	if !screen.ToggleFold(0) {
		t.Fatal("the reflowed block cannot be folded")
	}
	expectRows(t, screen, "$ echo a", "bcdefghi", "jklmnop", "$")
}

func TestClearingTheDisplayTakesTheBlocksWithIt(t *testing.T) {
	screen := session(t, 20, 8, 100)
	screen.ToggleFold(0)
	// `clear`: home, erase the display, erase the scrollback.
	shellRun(screen, "clear", "\x1b[H\x1b[2J\x1b[3J")
	shellPrompt(screen, "$ ")
	shellRun(screen, "seq 1 4", "1\r\n2\r\n3\r\n4\r\n")
	shellPrompt(screen, "$ ")
	// The one box is the new command's: nothing is folded, nothing hidden, and
	// no row of the output carries a box without a command.
	expectRows(t, screen, "$ seq 1 4", "1", "2", "3", "4", "$")
	boxes := 0
	for row := 0; row < 8; row++ {
		if screen.FoldAt(row).Anchor {
			boxes++
			if got := screen.RowText(row); got != "$ seq 1 4" {
				t.Fatalf("box on %q", got)
			}
			if screen.FoldAt(row).Folded {
				t.Fatal("the new command came up folded")
			}
		}
	}
	if boxes != 1 {
		t.Fatalf("%d boxes after clear, want 1", boxes)
	}
	if got := screen.LineNumber(1); got != 2 {
		t.Fatalf("row 1 is numbered %d after the clear, want the rows to count on without a hidden gap", got)
	}
}

func TestClearScreenDropsTheBlocksAboveThePromptItKeeps(t *testing.T) {
	screen := session(t, 20, 8, 100)
	shellRun(screen, "ls", "x\r\ny\r\n")
	shellPrompt(screen, "$ ")
	feed(screen, "typed")
	screen.ToggleFold(0)
	screen.ClearScreen()
	expectRows(t, screen, "$ typed")
	if screen.FoldAt(0).Folded {
		t.Fatal("a fold survived on a row that is not the command it was made for")
	}
	// The prompt the clear kept still opens its block when the command runs.
	shellRun(screen, "", "out\r\nmore\r\n")
	shellPrompt(screen, "$ ")
	if !screen.FoldAt(0).Anchor {
		t.Fatalf("the kept prompt lost its block: %q", viewText(screen))
	}
}

func TestErasingRowsInPlaceKeepsThePromptsBlock(t *testing.T) {
	// A prompt that repaints itself, as a transient prompt does, wipes its own
	// row and writes it again. It is still that command's prompt.
	screen := New(20, 6, 100)
	screen.SetFolding(true)
	shellPrompt(screen, "$ ")
	feed(screen, "ls")
	feed(screen, "\r\x1b[2K$ ls") // repaint the row in place
	feed(screen, "\r\n"+markerC+"alpha\r\nbeta\r\n")
	shellPrompt(screen, "$ ")
	if !screen.FoldAt(0).Anchor {
		t.Fatalf("a repainted prompt lost its block: %q", viewText(screen))
	}
}

func TestFoldingOffShowsEverythingAndRemembersTheChoice(t *testing.T) {
	screen := session(t, 20, 8, 100)
	screen.ToggleFold(0)
	screen.SetFolding(false)
	expectRows(t, screen, "$ ll", "alpha", "beta", "gamma", "$")
	if screen.FoldAt(0) != (FoldInfo{}) || screen.ToggleFold(0) {
		t.Fatal("a terminal that does not fold still offers a box")
	}
	if got := screen.Cursor().Y; got != 4 {
		t.Fatalf("cursor row = %d, want 4", got)
	}
	screen.SetFolding(true)
	expectRows(t, screen, "$ ll", "$")
}

func TestUnfoldAllOpensEveryBlock(t *testing.T) {
	screen := New(20, 12, 100)
	screen.SetFolding(true)
	for _, name := range []string{"a", "b"} {
		shellPrompt(screen, "$ ")
		shellRun(screen, name, "out-"+name+"\r\n")
	}
	shellPrompt(screen, "$ ")
	screen.ToggleFold(0)
	screen.ToggleFold(1)
	expectRows(t, screen, "$ a", "$ b", "$")
	if !screen.UnfoldAll() {
		t.Fatal("UnfoldAll found nothing folded")
	}
	expectRows(t, screen, "$ a", "out-a", "$ b", "out-b", "$")
	if screen.UnfoldAll() {
		t.Fatal("UnfoldAll reported a change with nothing folded")
	}
}

func TestToggleLastFoldFoldsTheLatestCommandWithOutput(t *testing.T) {
	screen := New(20, 12, 100)
	screen.SetFolding(true)
	shellPrompt(screen, "$ ")
	shellRun(screen, "a", "out-a\r\n")
	shellPrompt(screen, "$ ")
	shellRun(screen, "b", "out-b\r\n")
	shellPrompt(screen, "$ ")
	shellRun(screen, "true", "")
	shellPrompt(screen, "$ ")
	if !screen.ToggleLastFold() {
		t.Fatal("nothing folded")
	}
	expectRows(t, screen, "$ a", "out-a", "$ b", "$ true", "$")
	if !screen.ToggleLastFold() {
		t.Fatal("the second toggle changed nothing")
	}
	expectRows(t, screen, "$ a", "out-a", "$ b", "out-b", "$ true", "$")
}

func TestRevealLineOpensTheBlockThatHidesIt(t *testing.T) {
	screen := session(t, 20, 8, 100)
	screen.ToggleFold(0)
	if !screen.RevealLine(2) {
		t.Fatal("revealing a hidden line changed nothing")
	}
	expectRows(t, screen, "$ ll", "alpha", "beta", "gamma", "$")
}

func TestScrollingStepsOverAFoldedBlock(t *testing.T) {
	screen := New(20, 4, 1000)
	screen.SetFolding(true)
	shellPrompt(screen, "$ ")
	var out strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&out, "line %d\r\n", i)
	}
	shellRun(screen, "seq", out.String())
	shellPrompt(screen, "$ ")
	shellRun(screen, "echo end", "end\r\n")
	shellPrompt(screen, "$ ")
	// Fold the long command from the history view.
	screen.Scroll(screen.Scrollback())
	if !screen.FoldAt(0).Anchor || screen.RowText(0) != "$ seq" {
		t.Fatalf("history view = %q, want the long command on top", viewText(screen))
	}
	screen.ToggleFold(0)
	screen.ScrollToBottom()
	// One notch back from the live screen reaches the folded command instead of
	// spending a notch on every hidden row, and one forward passes the block.
	for range 4 {
		screen.Scroll(1)
	}
	if got := screen.RowText(0); got != "$ seq" {
		t.Fatalf("scrolling back shows %q, want the folded command", viewText(screen))
	}
	screen.Scroll(-1)
	if got := screen.RowText(0); got == "$ seq" || strings.HasPrefix(got, "line ") {
		t.Fatalf("one notch forward is on %q: %q", got, viewText(screen))
	}
}

func TestHiddenRowsStillCopyAndStillBelongToTheAssistantsText(t *testing.T) {
	screen := session(t, 20, 8, 100)
	screen.ToggleFold(0)
	// A selection across the fold copies what the fold hides: folding changes
	// what is drawn, not what the user selected.
	got := screen.SelectedText(Position{Row: 0, Column: 0}, Position{Row: 1, Column: 0})
	if want := "$ ll\nalpha\nbeta\ngamma\n$"; got != want {
		t.Fatalf("copied %q, want %q", got, want)
	}
	// And Text(), which feeds the assistant, is the screen's content as held.
	if got, want := screen.Text(), "$ ll\nalpha\nbeta\ngamma\n$"; got != want {
		t.Fatalf("Text() = %q, want %q", got, want)
	}
}

func TestMouseRowsMapToTheApplicationsRows(t *testing.T) {
	screen := session(t, 20, 8, 100)
	if row, ok := screen.ScreenRow(3); !ok || row != 3 {
		t.Fatalf("unfolded ScreenRow(3) = %d, %v", row, ok)
	}
	screen.ToggleFold(0)
	if row, ok := screen.ScreenRow(1); !ok || row != 4 {
		t.Fatalf("folded ScreenRow(1) = %d, %v, want the application's row 4", row, ok)
	}
	// The three rows the fold hid leave the bottom of the view empty, and the
	// application has nothing there to receive a click.
	if _, ok := screen.ScreenRow(7); ok {
		t.Fatal("a row past the view reached the application")
	}
	// A view scrolled into the history without folds reports as it always did,
	// so a wheel over a program that asked for the mouse still reaches it.
	scrolled := New(20, 4, 100)
	scrolled.SetFolding(true)
	feed(scrolled, "1\r\n2\r\n3\r\n4\r\n5\r\n6\r\n")
	scrolled.Scroll(2)
	if row, ok := scrolled.ScreenRow(1); !ok || row != 1 {
		t.Fatalf("scrolled ScreenRow(1) = %d, %v", row, ok)
	}
}

func TestAHiddenRowsImageIsNotPlaced(t *testing.T) {
	screen := session(t, 20, 8, 100)
	if row, ok := screen.imageViewRow(2, 0); !ok || row != 2 {
		t.Fatalf("open block: row %d, %v", row, ok)
	}
	screen.ToggleFold(0)
	if _, ok := screen.imageViewRow(2, 0); ok {
		t.Fatal("an image anchored in a folded block is still placed")
	}
	if row, ok := screen.imageViewRow(4, 0); !ok || row != 1 {
		t.Fatalf("the prompt row's image is at %d, %v, want the row the prompt moved to", row, ok)
	}
}

func TestMarkersFollowTheirLinesOutOfTheHistory(t *testing.T) {
	// A scrollback of two lines drops a block's opening row; what is left of
	// the block must not offer a box with no command, or hide anything.
	screen := New(20, 4, 2)
	screen.SetFolding(true)
	shellPrompt(screen, "$ ")
	shellRun(screen, "seq", "1\r\n2\r\n3\r\n4\r\n5\r\n6\r\n7\r\n")
	shellPrompt(screen, "$ ")
	for row := 0; row < 4; row++ {
		if screen.FoldAt(row).Anchor {
			t.Fatalf("row %d offers a box though the command row was dropped: %q", row, viewText(screen))
		}
	}
	if got := screen.Text(); !strings.Contains(got, "7") {
		t.Fatalf("text = %q", got)
	}
	checkInvariants(t, screen)
}

// The markers may arrive split at any byte, as the network delivers them.
func TestMarkersSplitAtAnyByteBuildTheSameBlocks(t *testing.T) {
	script := []byte(markerA + "$ " + markerB + "ls\r\n" + markerC + "alpha\r\nbeta\r\n" +
		markerA + "$ " + markerB + "pwd\r\n" + markerC + "/home\r\n" + markerA + "$ " + markerB)
	snapshot := func(screen *Terminal) string {
		var b strings.Builder
		for row := 0; row < 8; row++ {
			info := screen.FoldAt(row)
			fmt.Fprintf(&b, "%d|%q|%v%v%v\n", row, screen.RowText(row), info.Anchor, info.Folded, info.Inside)
		}
		return b.String()
	}
	whole := New(20, 8, 100)
	whole.SetFolding(true)
	whole.Write(script)
	want := snapshot(whole)
	if !strings.Contains(want, "true") {
		t.Fatalf("the reference run built no block:\n%s", want)
	}
	for cut := 1; cut < len(script); cut++ {
		screen := New(20, 8, 100)
		screen.SetFolding(true)
		screen.Write(script[:cut])
		screen.Write(script[cut:])
		if got := snapshot(screen); got != want {
			t.Fatalf("split at %d:\n%s\nwant\n%s", cut, got, want)
		}
	}
	bytewise := New(20, 8, 100)
	bytewise.SetFolding(true)
	for _, b := range script {
		bytewise.Write([]byte{b})
	}
	if got := snapshot(bytewise); got != want {
		t.Fatalf("byte by byte:\n%s\nwant\n%s", got, want)
	}
}

func FuzzFoldedView(f *testing.F) {
	f.Add([]byte(markerA + "$ " + markerB + "ls\r\n" + markerC + "alpha\r\n" + markerA + "$ "))
	f.Add([]byte("\x1b[2J\x1b[H" + markerC + "\x1b[3J" + markerA))
	f.Add([]byte(markerA + "abcdefghijklmnopqrstuvwxyz\r\n" + markerC + "1\r\n2\r\n3\r\n" + markerA))
	f.Add([]byte("\x1b[?1049h" + markerA + markerC + "\x1b[?1049l"))
	f.Fuzz(func(t *testing.T, data []byte) {
		screen := New(12, 6, 8)
		screen.SetFolding(true)
		prefix := []byte(markerA + "$ " + markerB + "ls\r\n" + markerC + "1\r\n2\r\n3\r\n4\r\n5\r\n6\r\n7\r\n" + markerA + "$ ")
		screen.Write(prefix)
		screen.Write(data)
		for row := 0; row < 6; row++ {
			if screen.FoldAt(row).Anchor {
				screen.ToggleFold(row)
				break
			}
		}
		screen.Resize(7, 6)
		screen.Scroll(3)
		checkInvariants(t, screen)
		// The view is always the buffer's rows in order, each at most once.
		last := -1
		for row := 0; row < 6; row++ {
			line := screen.ViewLine(row)
			if line < 0 {
				continue
			}
			if line <= last {
				t.Fatalf("view row %d shows buffer line %d after %d", row, line, last)
			}
			last = line
		}
		screen.UnfoldAll()
		screen.SetFolding(false)
		checkInvariants(t, screen)
	})
}
