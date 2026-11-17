package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestSettingsCategoryListScrollsAndRevealsTheChosenCategory(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	size := gfx.Size{Width: 1000, Height: 520}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	view := &state.settings
	if view.navContent <= view.navView {
		t.Fatalf("the category list (%v) fits its window (%v), so the test would prove nothing", view.navContent, view.navView)
	}

	// The wheel over the list moves the list, not the page behind it.
	page := state.settingsScroll
	state.scrollSettings(gfx.ScrollEvent{X: view.navRect.X + 10, Y: view.navRect.Y + 10, DeltaY: 80, Precise: true})
	if view.navScroll == 0 {
		t.Fatal("the wheel over the category list did not scroll it")
	}
	if state.settingsScroll != page {
		t.Fatalf("the wheel over the category list scrolled the page: %v -> %v", page, state.settingsScroll)
	}
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)
	if view.navScroll == 0 {
		t.Fatal("the next paint put the category list back where it was")
	}

	// Choosing a category the short window cannot show brings its row into view.
	state.settings.navScroll = 0
	state.selectSettingsCategory(settingsAbout)
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)
	hit, ok := hitByID(&canvas, hitSettingsCategoryList.At(int(settingsAbout)))
	if !ok {
		t.Fatal("the About category is not painted")
	}
	if hit.Bounds.Y < view.navRect.Y || hit.Bounds.Bottom() > view.navRect.Bottom() {
		t.Fatalf("the About row %+v is not inside the list's window %+v", hit.Bounds, view.navRect)
	}
}
