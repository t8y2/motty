package workbench

import (
	"testing"
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
	base := state.cellHeight
	baseWidth := state.cellWidth
	for _, mode := range []lineHeight{lineHeightCompact, lineHeightRoomy} {
		if !state.setTerminalLineHeight(mode) {
			t.Fatalf("setTerminalLineHeight(%v) reported no change", mode)
		}
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
	if !aboutEqual(reloaded.cellHeight, state.cellHeight) {
		t.Fatalf("reloaded cell height = %v, want %v", reloaded.cellHeight, state.cellHeight)
	}
}

func aboutEqual(a, b float32) bool {
	const eps = 0.01
	return a-b < eps && b-a < eps
}
