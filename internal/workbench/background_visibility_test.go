package workbench

import (
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestBackgroundVisibilitySlider(t *testing.T) {
	picked := filepath.Join(t.TempDir(), "wall.png")
	writePNG(t, picked, 64, 64)
	state, options := backgroundTestState(t, picked)
	state.Click(hitBgChoose)
	waitFor(t, state, state.backgroundOn)
	searchSettings(t, state, state.messages.Tools.BgVisibility)
	for index, row := range state.settings.results[:state.settings.count] {
		if row.label == state.messages.Tools.BgVisibility {
			state.Click(hitSettingsResultList.At(index))
			break
		}
	}
	driver := paneDriver(t, state, gfx.Size{Width: 1400, Height: 900})
	if err := driver.Paint(); err != nil {
		t.Fatal(err)
	}
	slider := hitOf(t, driver, hitBgVisibility).Bounds
	x, y := slider.X+slider.Width/2, slider.Y+slider.Height/2
	before := state.bg.visibility.Value
	if err := driver.Press(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(x+slider.Width/4, y); err != nil {
		t.Fatal(err)
	}
	got := int(state.bg.visibility.Value)
	if got <= int(before) {
		t.Fatalf("dragging right left visibility at %d, from %v", got, before)
	}
	if err := driver.Release(x+slider.Width/4, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	saved, _, err := loadState(options.StatePath)
	if err != nil || saved.Background == nil || saved.Background.Visibility == nil || *saved.Background.Visibility != got {
		t.Fatalf("saved visibility = %+v, err %v", saved.Background, err)
	}
	if err := driver.Key(gfx.KeyRight, 0); err != nil {
		t.Fatal(err)
	}
	again := newTestState(t, options)
	if again.bg.visibility.Value != float32(got+1) {
		t.Fatalf("restored visibility = %v", again.bg.visibility.Value)
	}
}

func TestBackgroundVisibilityRestoresLegacyChoices(t *testing.T) {
	for _, tc := range []struct {
		strength string
		want     float32
	}{{"subtle", 8}, {"", 20}, {"strong", 38}} {
		t.Run(tc.strength, func(t *testing.T) {
			picked := filepath.Join(t.TempDir(), "wall.png")
			writePNG(t, picked, 64, 64)
			state, _ := backgroundTestState(t, picked)
			state.Click(hitBgChoose)
			waitFor(t, state, state.backgroundOn)
			state.restoreBackground(&savedBackground{Name: "wall.png", Strength: tc.strength})
			if state.bg.visibility.Value != tc.want {
				t.Fatalf("visibility = %v, want %v", state.bg.visibility.Value, tc.want)
			}
		})
	}
}
