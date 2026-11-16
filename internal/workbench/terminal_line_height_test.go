package workbench

import (
	"encoding/json"
	"testing"

	"github.com/vivaui/viva/gfx"
)

// TestTerminalLineHeightDefaultLeavesCellHeightUntouched pins the contract that
// the new line-spacing setting is off by default: with no choice made, the row
// height is exactly the font's own cell, so every existing terminal and scroll
// measurement keeps its prior value.
func TestTerminalLineHeightDefaultLeavesCellHeightUntouched(t *testing.T) {
	state := newTestState(t, testOptions(t))
	base := state.cellHeight // normal spacing == the font's own cell
	if state.terminalLineHeight != lineHeightNormal {
		t.Fatalf("default spacing = %v, want normal", state.terminalLineHeight)
	}
	// Normal must be identity, else the default view would already differ.
	if lineHeightNormal.factor() != 1 {
		t.Fatalf("normal factor = %v, want 1", lineHeightNormal.factor())
	}
	// The factors step away from the base in both directions.
	if lineHeightCompact.factor() >= 1 {
		t.Fatalf("compact factor = %v, want < 1", lineHeightCompact.factor())
	}
	if lineHeightRoomy.factor() <= 1 {
		t.Fatalf("roomy factor = %v, want > 1", lineHeightRoomy.factor())
	}
	_ = base
}

// TestTerminalLineHeightScalesCellHeight checks that each choice rescales the row
// height from the same font, and only the height (columns are untouched).
func TestTerminalLineHeightScalesCellHeight(t *testing.T) {
	state := newTestState(t, testOptions(t))
	var canvas gfx.Canvas
	paint := func() { state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900}) }
	paint()
	base := state.cellHeight
	baseWidth := state.cellWidth
	for _, mode := range []lineHeight{lineHeightCompact, lineHeightRoomy} {
		if !state.setTerminalLineHeight(mode) {
			t.Fatalf("setTerminalLineHeight(%v) reported no change", mode)
		}
		paint()
		if got := state.cellHeight; !aboutEqual(got, base*mode.factor()) {
			t.Fatalf("after %v cell height = %v, want %v", mode, got, base*mode.factor())
		}
		if got := state.cellWidth; !aboutEqual(got, baseWidth) {
			t.Fatalf("after %v cell width changed to %v, want %v", mode, got, baseWidth)
		}
	}
	// Switching back to normal restores the exact base height.
	if !state.setTerminalLineHeight(lineHeightNormal) {
		t.Fatal("returning to normal reported no change")
	}
	paint()
	if !aboutEqual(state.cellHeight, base) {
		t.Fatalf("after normal cell height = %v, want base %v", state.cellHeight, base)
	}
}

// TestTerminalLineHeightPersistsRoundTrip checks the setting survives a save and
// load with the same spacing and the same derived cell height.
func TestTerminalLineHeightPersistsRoundTrip(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.setTerminalLineHeight(lineHeightRoomy)
	state.save()
	reloaded := newTestState(t, options)
	if reloaded.terminalLineHeight != lineHeightRoomy {
		t.Fatalf("reloaded spacing = %v, want roomy", reloaded.terminalLineHeight)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	reloaded.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if !aboutEqual(reloaded.cellHeight, state.cellHeight) {
		t.Fatalf("reloaded cell height = %v, want %v", reloaded.cellHeight, state.cellHeight)
	}
}

func aboutEqual(a, b float32) bool {
	const eps = 0.01
	return a-b < eps && b-a < eps
}

// TestSyncedLineSpacingRefitsOpenTerminals covers the live-window restore path:
// a spacing that arrives from another device has to change the open grid with the
// next frame, not on the next resize.
func TestSyncedLineSpacingRefitsOpenTerminals(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.sessions = append(state.sessions, Session{ID: 1, term: newTerminal(80, 24)})
	state.activeSessionID = 1
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	state.fitTerminals()
	_, before := state.sessions[0].term.screen.Size()

	var saved savedState
	if err := json.Unmarshal([]byte(`{"terminal_line_height":"roomy"}`), &saved); err != nil {
		t.Fatal(err)
	}
	state.restoreShared(saved)
	if state.terminalLineHeight != lineHeightRoomy {
		t.Fatalf("spacing = %v, want roomy", state.terminalLineHeight)
	}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	_, after := state.sessions[0].term.screen.Size()
	if after >= before {
		t.Fatalf("rows %d then %d: a taller row should fit fewer", before, after)
	}
	state.fitTerminals()
	if _, again := state.sessions[0].term.screen.Size(); again != after {
		t.Fatalf("rows %d after a second fit, want the same %d", again, after)
	}
}
