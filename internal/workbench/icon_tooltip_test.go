package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// Icon-only buttons name themselves: every toolbar icon declares a hint, and
// resting the pointer on one shows it as a tooltip after the delay.
func TestToolbarIconsDeclareHintsAndShowThemAsTooltips(t *testing.T) {
	state, _, _, _ := liveSession(t)
	for index := range state.toolbars.customize(toolbarTerminal) {
		state.toolbars.customize(toolbarTerminal)[index].Enabled = true
	}
	size := gfx.Size{Width: 1400, Height: 900}
	paint := func(now time.Duration, hovered gfx.ID) *gfx.Canvas {
		var canvas gfx.Canvas
		canvas.SetClock(func() time.Duration { return now })
		canvas.BeginFrame()
		canvas.SetInteraction(hovered, gfx.ID{}, gfx.ID{}, false)
		state.Paint(&canvas, size)
		canvas.EndFrame()
		return &canvas
	}

	canvas := paint(0, gfx.ID{})
	for _, test := range []struct {
		id   gfx.ID
		hint string
	}{
		{hitSFTPOpen, state.messages.TipFiles},
		{hitConnExport, state.messages.ConnExportLog},
		{hitTerminalRecord, state.messages.TipRecordStart},
		{hitTerminalSaveOutput, state.messages.Tools.MenuSaveOutput},
		{hitTerminalRecordingExport, state.messages.RecordingExport},
		{hitProcessToolsOpen, state.messages.Tools.RemoteMenu},
		{hitCommandPalette, state.messages.CommandPaletteTitle},
		{hitWindowSettings, state.messages.NavSettings},
	} {
		hit, ok := hitByID(canvas, test.id)
		if !ok || hit.Hint != test.hint {
			t.Errorf("hit %d = %+v, %v; want hint %q", test.id, hit, ok, test.hint)
		}
	}

	files := state.messages.TipFiles
	showsTooltip := func(canvas *gfx.Canvas) bool {
		for _, text := range canvas.Texts() {
			if text.Value == files && text.Layer == tooltipLayer {
				return true
			}
		}
		return false
	}
	state.tooltip = components.HoverTooltip{}
	// The pinned shadcn provider uses delay 0, so the hint appears with the
	// pointer rather than after a rest.
	if !showsTooltip(paint(20*time.Second, hitSFTPOpen)) {
		t.Fatal("the tooltip did not appear on the button")
	}
	if showsTooltip(paint(21*time.Second, gfx.ID{})) {
		t.Fatal("the tooltip stayed after the pointer left")
	}
}

// Tabbing to an icon-only button names it at once, without waiting for the
// pointer to rest on it.
func TestToolbarIconTooltipShowsOnKeyboardFocus(t *testing.T) {
	state, _, _, _ := liveSession(t)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	canvas.SetClock(func() time.Duration { return 0 })
	canvas.BeginFrame()
	canvas.SetInteraction(gfx.ID{}, gfx.ID{}, hitSFTPOpen, true)
	state.Paint(&canvas, size)
	canvas.EndFrame()
	for _, text := range canvas.Texts() {
		if text.Value == state.messages.TipFiles && text.Layer == tooltipLayer {
			return
		}
	}
	t.Fatalf("no tooltip for the focused Files button: %+v", canvas.Texts())
}

// A keyboard-focused control without a hint (such as the terminal) must not
// hide hover tooltips on SSH tabs or right-side toolbar buttons.
func TestHoverTooltipStillShowsWhenFocusVisibleHasNoHint(t *testing.T) {
	state, _, _, _ := liveSession(t)
	size := gfx.Size{Width: 1400, Height: 900}
	termID := terminalControlHit(state.sessions[0].ID)
	tabID := hitTabList.At(int(state.deck.ActiveWorkspace()))

	paint := func(hovered, focused gfx.ID) *gfx.Canvas {
		var canvas gfx.Canvas
		canvas.SetClock(func() time.Duration { return 0 })
		canvas.BeginFrame()
		canvas.SetInteraction(hovered, gfx.ID{}, focused, !focused.IsZero())
		state.Paint(&canvas, size)
		canvas.EndFrame()
		return &canvas
	}
	hasTooltip := func(canvas *gfx.Canvas, want string) bool {
		for _, text := range canvas.Texts() {
			if text.Layer == tooltipLayer && text.Value == want {
				return true
			}
		}
		return false
	}

	tabCanvas := paint(tabID, termID)
	tabHit, ok := hitByID(tabCanvas, tabID)
	if !ok || tabHit.Hint == "" {
		t.Fatalf("tab hit = %+v, %v; want non-empty hint", tabHit, ok)
	}
	if state.tooltip.Showing() != tabID || !hasTooltip(tabCanvas, tabHit.Hint) {
		t.Fatalf("SSH tab tooltip did not show while terminal had FocusVisible (showing=%v)", state.tooltip.Showing())
	}

	railCanvas := paint(hitSFTPOpen, termID)
	if state.tooltip.Showing() != hitSFTPOpen || !hasTooltip(railCanvas, state.messages.TipFiles) {
		t.Fatalf("right toolbar tooltip did not show while terminal had FocusVisible (showing=%v)", state.tooltip.Showing())
	}
}
