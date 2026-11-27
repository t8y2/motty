package workbench

import (
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// TestTheNoticePositionSettingMovesTheNotices walks the setting the way a
// reader does: the notice starts in the top center, choosing the bottom right
// corner moves it there, and the choice is still there after a restart.
func TestTheNoticePositionSettingMovesTheNotices(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsAppearance)

	state.fileNotices.show("Saved", "the change is in place", components.ToastInfo)
	canvas, _ := paintAt(state, gfx.Size{Width: 900, Height: 700})
	top, ok := canvas.HitBounds(hitFileNotices.At(1))
	if !ok {
		t.Fatal("the notice was not painted in the top center")
	}
	if top.Y > 700/2 {
		t.Fatalf("the notice painted at Y=%v, below the middle of a window it should sit at the top of", top.Y)
	}

	chooseSelectOption(t, state, state.messages.SettingsToastPosition, state.settingsToastPlacementLabel(toastBottomRight))
	if state.toastPlacement != toastBottomRight {
		t.Fatalf("the bottom right choice was not taken: %v", state.toastPlacement)
	}
	canvas, _ = paintAt(state, gfx.Size{Width: 900, Height: 700})
	bottom, ok := canvas.HitBounds(hitFileNotices.At(1))
	if !ok {
		t.Fatal("the notice was not painted in the bottom right corner")
	}
	if bottom.Y < 700/2 {
		t.Fatalf("the notice painted at Y=%v, above the middle of a window it should sit at the bottom of", bottom.Y)
	}

	if bottom.X+bottom.Width/2 < 900/2 {
		t.Fatalf("the notice is centered at X=%v, left of the middle of a window it should sit right of", bottom.X+bottom.Width/2)
	}

	restarted := newTestState(t, options)
	if restarted.toastPlacement != toastBottomRight {
		t.Fatalf("after a restart the notice position is %v, want the bottom right", restarted.toastPlacement)
	}
}

// TestANoticePositionThisBuildDoesNotKnowFallsBackToTheTop keeps an older or
// newer state from hiding the notices: an unknown value reads as the default.
func TestANoticePositionThisBuildDoesNotKnowFallsBackToTheTop(t *testing.T) {
	for _, value := range []string{"", "middle-left", "Top-Center"} {
		if got := toastPlacementFromString(value); got != toastTopCenter {
			t.Errorf("toastPlacementFromString(%q) = %v, want the top center", value, got)
		}
	}
	for _, placement := range settingsToastPlacementChoices {
		if got := toastPlacementFromString(placement.String()); got != placement {
			t.Errorf("the placement %v did not survive its own name", placement)
		}
	}
}
