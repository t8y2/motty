//go:build windows || darwin || linux

package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

func snippetTestState(t *testing.T, snippets ...snippet) *State {
	t.Helper()
	state := newTestState(t, testOptions(t))
	state.snippets.list = snippets
	state.refreshSnippets()
	state.activeSection = sectionSnippets
	return state
}

func paintSnippetsPage(state *State, canvas *gfx.Canvas) {
	canvas.Reset()
	state.Paint(canvas, gfx.Size{Width: 1400, Height: 900})
}

func hasTextValue(canvas *gfx.Canvas, value string) bool {
	for _, text := range canvas.Texts() {
		if text.Value == value {
			return true
		}
	}
	return false
}

func TestExpandingASnippetShowsItsWholeCommand(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "Two lines", Command: "echo first\n  echo second"},
		snippet{Name: "One line", Command: "ls"})
	var canvas gfx.Canvas
	paintSnippetsPage(state, &canvas)
	// The row summary is one line with ↵; the detail is the only place the
	// command's own lines are painted as they are.
	if hasTextValue(&canvas, "echo first") {
		t.Fatal("a collapsed row painted its command's first line")
	}
	state.Click(hitSnippetExpandList.At(0))
	paintSnippetsPage(state, &canvas)
	if state.snippets.expanded != 1 {
		t.Fatalf("expanded = %d, want the first snippet", state.snippets.expanded)
	}
	if !hasTextValue(&canvas, "echo first") || !hasTextValue(&canvas, "  echo second") {
		t.Fatalf("the expanded command's lines are missing: %v", canvas.Texts())
	}
	expander, ok := hitByID(&canvas, hitSnippetExpandList.At(0))
	if !ok || expander.Role != gfx.RoleButton || !expander.Expanded || !expander.Expandable || expander.Description != "Two lines" {
		t.Fatalf("expander hit = %+v, want an expanded button naming its snippet", expander)
	}
	// The detail is one non-actionable region named by the snippet.
	found := false
	for _, hit := range canvas.Hits() {
		if hit.Container && hit.Role == gfx.RoleGroup && hit.Label == "Two lines" && hit.Description == "echo first\n  echo second" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the expanded command has no accessibility region")
	}
	state.Click(hitSnippetExpandList.At(0))
	paintSnippetsPage(state, &canvas)
	if state.snippets.expanded != 0 || hasTextValue(&canvas, "echo first") {
		t.Fatalf("collapsing left expanded = %d and the detail text", state.snippets.expanded)
	}
}

func TestOnlyOneSnippetRowExpandsAtATime(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "First", Command: "echo first\n  echo second"},
		snippet{Name: "Second", Command: "echo third\n  echo fourth"})
	state.Click(hitSnippetExpandList.At(0))
	state.Click(hitSnippetExpandList.At(1))
	var canvas gfx.Canvas
	paintSnippetsPage(state, &canvas)
	if state.snippets.expanded != 2 {
		t.Fatalf("expanded = %d, want the second snippet", state.snippets.expanded)
	}
	if hasTextValue(&canvas, "echo first") || !hasTextValue(&canvas, "echo third") {
		t.Fatalf("both rows stayed expanded: %v", canvas.Texts())
	}
}

func TestSnippetExpansionClearsWhenTheListChanges(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "First", Command: "echo first"},
		snippet{Name: "Second", Command: "echo second"})
	state.snippets.expanded = 1
	state.refreshSnippets()
	if state.snippets.expanded != 1 {
		t.Fatal("sorting or filtering must not close an expanded row")
	}
	state.moveSnippet(0, 1)
	if state.snippets.expanded != 0 {
		t.Fatalf("a move left expanded = %d", state.snippets.expanded)
	}
	state.snippets.expanded = 1
	state.deleteSnippet(0)
	if state.snippets.expanded != 0 {
		t.Fatalf("a delete left expanded = %d", state.snippets.expanded)
	}
}

func TestSnippetHoverPreviewOpensAfterARestAndStaysOnTheCard(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "Two lines", Command: "echo first\n  echo second"},
		snippet{Name: "One line", Command: "ls"})
	var canvas gfx.Canvas
	now := time.Duration(0)
	canvas.SetClock(func() time.Duration { return now })
	row := hitSnippetRowList.At(0)

	canvas.SetInteraction(row, gfx.ID{}, gfx.ID{}, false)
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if state.snippets.hoverOpen {
		t.Fatal("the preview opened before the pointer rested")
	}
	if delay, ok := canvas.NextFrame(); !ok || delay != snippetHoverDelay {
		t.Fatalf("next frame = %v, %v, want the rest deadline %v", delay, ok, snippetHoverDelay)
	}

	now = snippetHoverDelay
	paintSnippetsPage(state, &canvas)
	if !state.snippets.hoverOpen || state.snippets.hoverRow != 1 {
		t.Fatalf("hover = %v at row %d after the rest", state.snippets.hoverOpen, state.snippets.hoverRow)
	}
	if !hasTextValue(&canvas, "echo first") || !hasTextValue(&canvas, "  echo second") {
		t.Fatalf("the preview does not show the command: %v", canvas.Texts())
	}

	// The pointer reaches the card: the preview stays while it is there.
	canvas.SetInteraction(hitSnippetHoverCard, gfx.ID{}, gfx.ID{}, false)
	paintSnippetsPage(state, &canvas)
	if !state.snippets.hoverOpen {
		t.Fatal("the preview closed when the pointer moved onto it")
	}

	// Leaving both the row and the card closes it.
	canvas.SetInteraction(gfx.ID{}, gfx.ID{}, gfx.ID{}, false)
	paintSnippetsPage(state, &canvas)
	if state.snippets.hoverOpen {
		t.Fatal("the preview stayed after the pointer left")
	}
}

func TestSnippetHoverPreviewSkipsRowsItWouldNotHelp(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "Two lines", Command: "echo first\n  echo second"},
		snippet{Name: "One line", Command: "ls"})
	var canvas gfx.Canvas
	now := snippetHoverDelay
	canvas.SetClock(func() time.Duration { return now })

	canvas.SetInteraction(hitSnippetRowList.At(1), gfx.ID{}, gfx.ID{}, false)
	paintSnippetsPage(state, &canvas)
	if state.snippets.hoverOpen {
		t.Fatal("a single-line command got a preview")
	}

	state.snippets.expanded = 1
	canvas.SetInteraction(hitSnippetRowList.At(0), gfx.ID{}, gfx.ID{}, false)
	paintSnippetsPage(state, &canvas)
	if state.snippets.hoverOpen {
		t.Fatal("the expanded row got a preview on top of its detail")
	}

	state.snippets.expanded = 0
	state.modal = modalSnippet
	canvas.SetInteraction(hitSnippetRowList.At(0), gfx.ID{}, gfx.ID{}, false)
	paintSnippetsPage(state, &canvas)
	if state.snippets.hoverOpen {
		t.Fatal("the preview opened under a dialog")
	}
}

func TestExpandedSnippetSteadyPaintDoesNotAllocate(t *testing.T) {
	state := snippetTestState(t, snippet{Name: "Two lines", Command: "echo first\n  echo second"})
	state.snippets.expanded = 1
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("an expanded row's paint allocations = %v", allocations)
	}
}

func TestSnippetHoverCardSteadyPaintDoesNotAllocate(t *testing.T) {
	state := snippetTestState(t, snippet{Name: "Two lines", Command: "echo first\n  echo second"})
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	now := time.Duration(0)
	canvas.SetClock(func() time.Duration { return now })
	canvas.SetInteraction(hitSnippetRowList.At(0), gfx.ID{}, gfx.ID{}, false)
	state.Paint(&canvas, size)
	now = snippetHoverDelay
	paintSnippetsPage(state, &canvas)
	if !state.snippets.hoverOpen {
		t.Fatal("the preview did not open")
	}
	canvas.SetInteraction(hitSnippetHoverCard, gfx.ID{}, gfx.ID{}, false)
	allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		canvas.SetInteraction(hitSnippetHoverCard, gfx.ID{}, gfx.ID{}, false)
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("the preview's steady paint allocations = %v", allocations)
	}
}
