package workbench

import "testing"

func TestActiveTabRemainsVisuallyDistinct(t *testing.T) {
	for name, pal := range map[string]*palette{"dark": &darkPalette, "light": &lightPalette} {
		t.Run(name, func(t *testing.T) {
			if contrast := inkContrast(pal.tabActive, pal.tab); contrast < 1.2 {
				t.Errorf("active and inactive tab backgrounds are too similar: %.2f", contrast)
			}
			colors := append([]string{"", "#ffffff", "#000000", "invalid"}, tabColors[1:]...)
			for _, color := range colors {
				activeFill := tabFill(pal, pal.tabActive, color, true, false)
				if contrast := inkContrast(pal.text, activeFill); contrast < tabColorInkFloor {
					t.Errorf("%q selected tab title is unreadable: %.2f", color, contrast)
				}
			}
		})
	}
}
