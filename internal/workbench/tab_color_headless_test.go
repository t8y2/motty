//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestTabColorSelectionThroughDriver(t *testing.T) {
	for _, scale := range []float64{1, 2} {
		for _, light := range []bool{false, true} {
			t.Run(fmt.Sprintf("scale=%g/light=%v", scale, light), func(t *testing.T) {
				state, tabs := fourTabs(t)
				state.setLanguage(i18n.Chinese)
				if light {
					state.setAppearance(appearanceLight)
				}
				view, err := headless.Open(app.Config{
					Width: 1100, Height: 760, Tree: Tree(), Paint: state.Paint,
					OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize, OnScroll: state.Scroll,
				}, nil, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				right := func() { hostFilterContextClick(t, view, hitTabList.At(int(tabs[0])), true) }
				right()
				hostFilterContextClick(t, view, hitTabColors.At(4), false)
				index, _ := state.workspaceTab(tabs[0])
				if state.tabMenu.open || state.workspaceTabs[index].Color != tabColors[4] || state.deck.ActiveWorkspace() != tabs[len(tabs)-1] {
					t.Fatal("native click did not change only the target tab")
				}
				right()
				selected := hostFilterContextHit(t, view, hitTabColors.At(4))
				if !selected.Selected || !selected.Checked {
					t.Fatal("selected color ring is missing")
				}
				if dir := os.Getenv("MOTTY_TAB_COLOR_SCREENSHOTS"); dir != "" {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("tab-color-scale-%g-light-%v.png", scale, light))); err != nil {
						t.Fatal(err)
					}
				}
				hostFilterContextClick(t, view, hitTabColors.At(0), false)
				if state.workspaceTabs[index].Color != tabColorNone {
					t.Fatal("native no-color click failed")
				}
			})
		}
	}
}
