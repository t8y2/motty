package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// A click on a host row of the list view selects it, and a click on a group row
// toggles the group, through the rows the Tree names.
func TestHostListClickSelectsAHostAndTogglesAGroup(t *testing.T) {
	state := nestedHostGroupState(t)
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	host := state.hosts[0]
	row, ok := hostListHostHit(canvas, state, host.ID)
	if !ok {
		t.Fatal("the host row is not painted")
	}
	clickRow(state, size, row.ID)
	if state.selectedHostID != host.ID {
		t.Fatalf("click selected %d, want %d", state.selectedHostID, host.ID)
	}

	group := state.hostGroupIndex("Team/Prod")
	expanded := state.hostGroups[group].Expanded
	canvas, _ = paintAt(state, size)
	header, ok := hostListGroupHit(canvas, state, group)
	if !ok {
		t.Fatal("the group row is not painted")
	}
	clickRow(state, size, header.ID)
	if state.hostGroups[group].Expanded == expanded {
		t.Fatal("clicking the group row did not toggle it")
	}
}

// The arrows move between the list's rows, and Enter on a focused host row
// connects it, as the keyboard paths always did.
func TestHostListArrowsAndEnterUseTheTreeRows(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	first, ok := hostListHostHit(canvas, state, state.hosts[0].ID)
	if !ok {
		t.Fatal("the first host row is not painted")
	}
	second, ok := hostListHostHit(canvas, state, state.hosts[1].ID)
	if !ok {
		t.Fatal("the second host row is not painted")
	}
	result := state.Key(gfx.KeyEvent{Key: gfx.KeyDown, Target: first.ID})
	if !result.Handled || result.Focus != second.ID {
		t.Fatalf("Down moved to %v (handled %v), want %v", result.Focus, result.Handled, second.ID)
	}
	before := len(world.conns)
	result = state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: second.ID})
	if !result.Handled || len(world.conns) != before+1 {
		t.Fatalf("Enter on a row dialed %d connections (handled %v)", len(world.conns)-before, result.Handled)
	}
}

// clickRow delivers a click on a page-named row the way the runtime does: the
// paint runs as the click's event pass, and what it deferred runs after it.
func clickRow(state *State, size gfx.Size, id gfx.ID) {
	var canvas gfx.Canvas
	canvas.BeginEventPass(id, gfx.ClickInfo{Clicks: 1})
	state.Paint(&canvas, size)
	canvas.EndEventPass()
	canvas.RunAfterPaint()
}
