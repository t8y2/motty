package term

// Temporary harness for issue 1390: replays a captured byte stream and dumps
// the cells of the visible rows with their colors. Not to be committed.

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestZZRepro1390(t *testing.T) {
	path := os.Getenv("REPRO1390_STREAM")
	if path == "" {
		t.Skip("REPRO1390_STREAM not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tm := New(100, 30, 100)
	tm.Write(data)
	cur := tm.Cursor()
	fmt.Printf("cursor x=%d y=%d visible=%v\n", cur.X, cur.Y, cur.Visible)
	for y := 0; y < 30; y++ {
		row := tm.Row(y)
		var text strings.Builder
		var attrs []string
		for x := 0; x < 60 && x < len(row.Cells); x++ {
			c := row.Cells[x]
			r := c.R
			if r == 0 || r == ' ' {
				r = '.'
			}
			text.WriteRune(r)
			if c == (Cell{}) {
				continue
			}
			cell := fmt.Sprintf("c%d[%q", x, c.R)
			if c.FG != 0 {
				cell += " fg=" + colorName(c.FG)
			}
			if c.BG != 0 {
				cell += " bg=" + colorName(c.BG)
			}
			if c.Attr != 0 {
				cell += fmt.Sprintf(" attr=%#x", c.Attr)
			}
			attrs = append(attrs, cell+"]")
		}
		if len(attrs) == 0 && strings.Trim(text.String(), ".") == "" {
			continue
		}
		fmt.Printf("row %2d |%s|\n", y, text.String())
		for _, a := range attrs {
			fmt.Printf("         %s\n", a)
		}
	}
}

func colorName(c Color) string {
	switch c.Kind() {
	case PaletteColor:
		return fmt.Sprintf("p%d", c.Index())
	case TrueColor:
		r, g, b := c.Components()
		return fmt.Sprintf("rgb(%d,%d,%d)", r, g, b)
	}
	return "default"
}
