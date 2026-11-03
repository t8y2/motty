package workbench

import (
	"fmt"
	"testing"

	"motty/internal/term"
)

func TestCJKRunStaysOnItsCellAfterBackspaceWithRightPrompt(t *testing.T) {
	for _, rightPrompt := range []bool{false, true} {
		t.Run(fmt.Sprintf("rightPrompt=%v", rightPrompt), func(t *testing.T) {
			screen := term.New(40, 3, 10)
			screen.Write([]byte("P> "))
			if rightPrompt {
				screen.Write([]byte("\x1b[35G✔\x1b[4G"))
			}
			var cache rowCache
			for _, step := range []struct{ name, output string }{
				{"type", "你好世界"},
				{"erase", "\b\b  \b\b"},
				{"append", "界"},
			} {
				screen.Write([]byte(step.output))
				cache.begin()
				entry := cache.row(screen.Row(0), 40, screen.Cursor().X, cellSpan{}, &darkPalette, false)
				offset := 0
				found := false
				for _, run := range entry.runs {
					piece := entry.text[offset : offset+int(run.Bytes)]
					offset += int(run.Bytes)
					if run.Column != 7 {
						continue
					}
					found = true
					if piece != "世" || run.Cells != 2 {
						t.Errorf("%s: 世 run = %q (%d cells); must remain a single character in two cells so it keeps the same centering", step.name, piece, run.Cells)
					}
				}
				if !found {
					t.Fatalf("%s: 世 has no run at column 7", step.name)
				}
			}
		})
	}
}

// The same rule covers a narrow non-ASCII character: with blanks between it and
// the right prompt, its run must stay one cell wide instead of absorbing them.
func TestNarrowNonASCIIRunStaysOnItsCellWithRightPrompt(t *testing.T) {
	screen := term.New(40, 3, 10)
	screen.Write([]byte("P> "))
	screen.Write([]byte("\x1b[35G✔\x1b[4G"))
	screen.Write([]byte("café"))
	screen.Write([]byte("\x1b[8G"))
	screen.Write([]byte("\b \b"))
	screen.Write([]byte("é "))

	var cache rowCache
	cache.begin()
	entry := cache.row(screen.Row(0), 40, screen.Cursor().X, cellSpan{}, &darkPalette, false)
	offset := 0
	found := false
	for _, run := range entry.runs {
		piece := entry.text[offset : offset+int(run.Bytes)]
		offset += int(run.Bytes)
		if run.Column != 6 {
			continue
		}
		found = true
		if piece != "é" || run.Cells != 1 {
			t.Errorf("run = %q (%d cells); a narrow character must keep its single cell", piece, run.Cells)
		}
	}
	if !found {
		t.Fatal("é has no run at column 6")
	}
}
