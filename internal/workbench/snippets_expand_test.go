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
	// A throwaway paint names the rows without leaving its frame request on the
	// canvas whose frame deadline this test reads.
	var ids gfx.Canvas
	state.Paint(&ids, gfx.Size{Width: 1400, Height: 900})
	row := state.snippets.rows.RowID(0)

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
	paintSnippetsPage(state, &canvas)

	canvas.SetInteraction(state.snippets.rows.RowID(1), gfx.ID{}, gfx.ID{}, false)
	paintSnippetsPage(state, &canvas)
	if state.snippets.hoverOpen {
		t.Fatal("a single-line command got a preview")
	}

	state.snippets.expanded = 1
	canvas.SetInteraction(state.snippets.rows.RowID(0), gfx.ID{}, gfx.ID{}, false)
	paintSnippetsPage(state, &canvas)
	if state.snippets.hoverOpen {
		t.Fatal("the expanded row got a preview on top of its detail")
	}

	state.snippets.expanded = 0
	state.modal = modalSnippet
	canvas.SetInteraction(state.snippets.rows.RowID(0), gfx.ID{}, gfx.ID{}, false)
	paintSnippetsPage(state, &canvas)
	if state.snippets.hoverOpen {
		t.Fatal("the preview opened under a dialog")
	}
}

func TestClickingASnippetRowSelectsIt(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "First", Command: "echo first"},
		snippet{Name: "Second", Command: "echo second"})
	view := openCatalogWindow(t, state)
	row, ok := findSnapshotHit(view.Snapshot(), state.snippets.rows.RowID(1))
	if !ok {
		t.Fatal("the second row is not painted")
	}
	if err := view.Driver().Click(row.Bounds.X+row.Bounds.Width/2, row.Bounds.Y+row.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.snippets.selected != 1 {
		t.Fatalf("selected = %d, want the second snippet", state.snippets.selected)
	}
	if state.snippets.rows.Selected() != 1 {
		t.Fatalf("the list's row = %d, want the second", state.snippets.rows.Selected())
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
	paintSnippetsPage(state, &canvas)
	canvas.SetInteraction(state.snippets.rows.RowID(0), gfx.ID{}, gfx.ID{}, false)
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

// The pointer's command preview is titled with the snippet, opens 4px under
// the row and 4px in from the command cell, and shows the command's own lines.
func TestSnippetHoverCardKeepsItsTitleAndOffset(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "Two lines", Command: "echo first\n  echo second"})
	size := gfx.Size{Width: 1400, Height: 900}
	paint := func(now time.Duration) *gfx.Canvas {
		var canvas gfx.Canvas
		canvas.SetClock(func() time.Duration { return now })
		canvas.BeginFrame()
		state.Paint(&canvas, size)
		canvas.EndFrame()
		return &canvas
	}
	canvas := paint(0)
	row, found := hitByLabelRoleLast(canvas, "Two lines", gfx.RoleOption)
	if !found {
		t.Fatal("the snippet row is not painted")
	}
	hover := func(now time.Duration) *gfx.Canvas {
		var canvas gfx.Canvas
		canvas.SetClock(func() time.Duration { return now })
		canvas.BeginFrame()
		canvas.SetInteraction(row.ID, gfx.ID{}, gfx.ID{}, false)
		state.Paint(&canvas, size)
		canvas.EndFrame()
		return &canvas
	}
	hover(0)
	canvas = hover(500 * time.Millisecond)
	if !state.snippets.hoverOpen {
		t.Fatal("resting on the row did not open its command preview")
	}
	if !hasTextValue(canvas, "echo first") || !hasTextValue(canvas, "  echo second") {
		t.Fatalf("the preview lacks the command's lines: %v", canvas.Texts())
	}
	names := 0
	for _, text := range canvas.Texts() {
		if text.Value == "Two lines" {
			names++
		}
	}
	if names < 2 {
		t.Fatalf("the preview is not titled with the snippet: %d names painted", names)
	}
	card, found := hitByID(canvas, hitSnippetHoverCard)
	if !found {
		t.Fatal("the preview has no card")
	}
	var panel gfx.Hit
	foundPanel := false
	at := gfx.Point{X: card.Bounds.X + 1, Y: card.Bounds.Y + 1}
	for _, hit := range canvas.Hits() {
		b := hit.Bounds
		if hit.Block && at.X >= b.X && at.X < b.X+b.Width && at.Y >= b.Y && at.Y < b.Y+b.Height {
			panel, foundPanel = hit, true
			break
		}
	}
	if !foundPanel {
		t.Fatal("the preview has no panel")
	}
	anchor := state.snippets.hoverAnchor.Rect()
	t.Logf("snippet anchor=%+v panel=%+v", anchor, panel.Bounds)
	if want := anchor.Y + anchor.Height + 4; panel.Bounds.Y != want {
		t.Fatalf("preview top = %v, want %v (4px under the row)", panel.Bounds.Y, want)
	}
	if panel.Bounds.X != anchor.X {
		t.Fatalf("preview starts at %v, want the command cell's edge %v plus 4", panel.Bounds.X, anchor.X)
	}
}
