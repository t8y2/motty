package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

// A program that draws its own cursor hides the real one and leaves placeholder
// text under the input method's composition. Nothing of that text may be drawn
// where the composition is, and the cells after it stay.
func TestCompositionHidesPlaceholderUnderIt(t *testing.T) {
	state, session, geometry := selectionState()
	session.term.write([]byte("\x1b[?25l\x1b[2;3HAsk Codex\x1b[2;3H"))
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextSetMarked, Text: "wo", MarkedSelection: app.TextRange{Start: 2, End: 2}})

	var canvas ui.Canvas
	state.paintGrid(&canvas, session, geometry)
	var text string
	for _, painted := range canvas.Texts() {
		text += painted.Value
	}
	if strings.Contains(text, "As") || !strings.Contains(text, "k Codex") {
		t.Fatalf("painted text = %q: the composed cells must be empty and the rest kept", text)
	}
}
