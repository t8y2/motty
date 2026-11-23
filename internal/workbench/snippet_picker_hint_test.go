package workbench

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

func TestSnippetPickerHoverShowsWholeCommand(t *testing.T) {
	state := newTestState(t, testOptions(t))
	command := "echo " + strings.Repeat("preview-command ", 18) + "\necho command-end-marker"
	addTestSnippets(state, snippet{Name: "Full command name", Command: command})
	state.openSnippetPicker()
	id := hitCommandPaletteItems.At(0)
	paint := func(hovered gfx.ID) *gfx.Canvas {
		var canvas gfx.Canvas
		canvas.SetClock(func() time.Duration { return time.Second })
		canvas.BeginFrame()
		canvas.SetInteraction(hovered, gfx.ID{}, gfx.ID{}, false)
		state.Paint(&canvas, gfx.Size{Width: 1200, Height: 900})
		canvas.EndFrame()
		return &canvas
	}
	canvas := paint(id)
	hit, ok := hitByID(canvas, id)
	if !ok || hit.Hint != "Full command name\n"+command {
		t.Fatal("command hint was truncated")
	}
	var shown strings.Builder
	for _, run := range canvas.Texts() {
		if run.Layer == tooltipLayer {
			shown.WriteString(run.Value)
		}
	}
	if !strings.Contains(shown.String(), "command-end-marker") || !strings.Contains(shown.String(), "Full command name") {
		t.Fatalf("hover preview did not show the full command: %q", shown.String())
	}
	canvas = paint(gfx.ID{})
	for _, run := range canvas.Texts() {
		if run.Layer == tooltipLayer {
			t.Fatal("tooltip remained after leaving the row")
		}
	}
}
