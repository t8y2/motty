//go:build dev

package workbench

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// The Debug entry is a sidebar page like the host list. Its page is the
// framework's code editor, filled with sample text, so the editor can be tried
// against the real theme, layout and input.
func TestDebugPageShowsTheCodeEditor(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.Click(hitDebug)
	if state.activeSection != sectionDebug {
		t.Fatalf("Debug did not open its page: section = %d", state.activeSection)
	}

	canvas, texts := paintedTexts(state)
	navigation, ok := hitByID(canvas, hitDebug)
	if !ok || !navigation.Selected {
		t.Fatalf("Debug navigation item = %+v, ok=%v", navigation, ok)
	}
	if !contains(texts, state.messages.DebugTitle) {
		t.Fatalf("the page heading is not painted: %v", texts)
	}
	editor, hasEditor := hitByID(canvas, hitDebugEditor)
	if !hasEditor || editor.Role != gfx.RoleTextField || !editor.Multiline {
		t.Fatalf("the code editor = %+v, found=%v", editor, hasEditor)
	}
	if !strings.Contains(editor.Value, "func main()") {
		t.Fatalf("the editor does not hold the sample text: %q", editor.Value)
	}

	// Opening the Debug page is a workspace choice, so it comes back the way the
	// other management pages do.
	restarted := newTestState(t, options)
	if restarted.activeSection != sectionDebug {
		t.Fatalf("restarted section = %d, want Debug", restarted.activeSection)
	}
}

// Typing reaches the editor through the text system, and the editing commands
// change the same text: the page keeps the text and Viva keeps the grid, the
// caret and the scroll position.
func TestDebugEditorTakesTypedTextAndCommands(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitDebug)
	paintedTexts(state)

	input := state.EditorFor(hitDebugEditor)
	if input == nil {
		t.Fatal("the page does not offer its editor to the text system")
	}
	if got := input.String(); !strings.Contains(got, "package main") {
		t.Fatalf("editor text = %q, want the sample program", got)
	}

	input.MoveTo(0, false)
	if got := input.Caret(); got != 0 {
		t.Fatalf("caret = %d, want the start after moving there", got)
	}
	canvas, _ := paintedTexts(state)
	if hit, ok := hitByID(canvas, hitDebugEditor); !ok || hit.Value == "" {
		t.Fatalf("the editor stopped painting: %+v, ok=%v", hit, ok)
	}
}

// The page only answers for its own editor while it is the page on show, so the
// sidebar entry of another section never receives the gallery's text.
func TestDebugEditorIsClaimedOnlyOnItsOwnPage(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitDebug)
	if state.EditorFor(hitDebugEditor) == nil {
		t.Fatal("the Debug page does not claim its editor")
	}
	state.activeSection = sectionHosts
	if state.EditorFor(hitDebugEditor) != nil {
		t.Fatal("the editor is still claimed after leaving the page")
	}
}

func TestDebugPagePaintDoesNotAllocateOnceWarm(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitDebug)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	if allocations := testing.AllocsPerRun(50, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}); allocations != 0 {
		t.Fatalf("Debug page paint allocated %v times", allocations)
	}
}

// The page's caret blinks on the framework's clock: lit when it first gets the
// keyboard and after moving, dark a half period later, and lit again a full
// period later. A window left alone must stop asking for frames.
func TestDebugCaretBlinksAndThenRests(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitDebug)
	paintDebug := func(canvas *gfx.Canvas, now time.Duration) bool {
		canvas.SetClock(func() time.Duration { return now })
		canvas.SetInteraction(gfx.ID{}, gfx.ID{}, hitDebugEditor, false)
		canvas.Reset()
		canvas.BeginFrame()
		state.Paint(canvas, gfx.Size{Width: 1400, Height: 900})
		canvas.EndFrame()
		return caretPainted(canvas, state)
	}

	var canvas gfx.Canvas
	if !paintDebug(&canvas, 0) {
		t.Fatal("the caret is dark on the first focused frame")
	}
	if !paintDebug(&canvas, components.CaretBlinkHalf/2) {
		t.Fatal("the caret went dark inside the first half period")
	}
	if paintDebug(&canvas, components.CaretBlinkHalf+time.Millisecond) {
		t.Fatal("the caret is still lit a half period after it was drawn")
	}
	if !paintDebug(&canvas, 2*components.CaretBlinkHalf+time.Millisecond) {
		t.Fatal("the caret did not come back a full period after it was drawn")
	}

	// Left alone past the span the caret stays lit and the canvas comes to rest.
	if !paintDebug(&canvas, components.CaretBlinkSpan+time.Hour) {
		t.Fatal("the idle caret is dark")
	}
	if _, ok := canvas.NextFrame(); ok {
		t.Fatal("the idle caret still asks for a frame")
	}
}

// caretPainted reports whether one of this frame's rectangles is the caret: a
// fill in the theme's cursor color, as thick as the editor draws it.
func caretPainted(canvas *gfx.Canvas, state *State) bool {
	caret, shown := state.dev.code.CaretBounds()
	if !shown {
		return false
	}
	for _, rect := range canvas.Rectangles() {
		if rect.Color == state.pal.cursor && rect.Bounds.Width == caret.Width && rect.Bounds.Height > caret.Width {
			return true
		}
	}
	return false
}

func countText(texts []string, value string) int {
	count := 0
	for _, text := range texts {
		if text == value {
			count++
		}
	}
	return count
}

// The inspector names the control the pointer is over, so a person building a
// page can see in the real window what a headless tree lists. Its chord cycles
// it off, to the hovered control, to every control, and off again.
func TestDevInspectorNamesTheHoveredControl(t *testing.T) {
	state := newTestState(t, testOptions(t))
	cycle := gfx.KeyEvent{Modifiers: devInspectorChord.Modifiers, Text: string(rune(devInspectorChord.Char))}
	if result := state.Shortcut(cycle); !result.Handled {
		t.Fatalf("the inspector chord was not taken: %+v", result)
	}
	var canvas gfx.Canvas
	canvas.SetInteraction(hitDebug, gfx.ID{}, gfx.ID{}, false)
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	entry, found := hitByID(&canvas, hitDebug)
	if !found {
		t.Fatal("the Debug entry is not on the page")
	}
	described := entry.Role.String() + "  " + tree.Text(hitDebug)
	var texts []string
	for _, text := range canvas.Texts() {
		texts = append(texts, text.Value)
	}
	if !contains(texts, described) {
		t.Fatalf("the hovered control is not named: %v", texts)
	}
	state.Shortcut(cycle)
	state.Shortcut(cycle)
	canvas.Reset()
	canvas.SetInteraction(hitDebug, gfx.ID{}, gfx.ID{}, false)
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	for _, text := range canvas.Texts() {
		if strings.Contains(text.Value, described) {
			t.Fatalf("the inspector is still on: %q", text.Value)
		}
	}
}
