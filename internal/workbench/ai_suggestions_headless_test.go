//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestSuggestionControlsFitInNarrowHeadlessPanel(t *testing.T) {
	const width, height = 1400, 900
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		for _, scale := range []float64{1, 2} {
			t.Run(fmt.Sprintf("%s/%gx", language, scale), func(t *testing.T) {
				state, _ := panelState(t, nil)
				withModel(t, state)
				state.setLanguage(language)
				state.setAIPanelOpen(true)
				state.ai.panelWidth = 300
				view, err := headless.Open(app.Config{
					Width: width, Height: height, Tree: Tree(), Paint: state.Paint, OnResize: state.Resize,
					OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
				}, nil, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				panel := state.ai.panel.rect
				if panel.Width != 300 {
					t.Fatalf("test needs a 300px panel, got %+v", panel)
				}
				checkControls := func(outer gfx.Rect, ids ...gfx.ID) {
					t.Helper()
					snapshot := view.Snapshot()
					var previous []gfx.Hit
					for _, id := range ids {
						hit, ok := hitByIDSnapshot(snapshot.Hits, id)
						bounds := hit.Bounds
						if !ok || hit.Disabled || bounds.Width < 16 || bounds.Height < 16 || bounds.X < outer.X-0.01 || bounds.Y < outer.Y-0.01 ||
							bounds.X+bounds.Width > outer.X+outer.Width+0.01 || bounds.Y+bounds.Height > outer.Y+outer.Height+0.01 {
							t.Fatalf("control %q (%v) is missing, disabled or outside %+v: %+v", hit.Label, id, outer, bounds)
						}
						for _, other := range previous {
							overlap := intersectRect(bounds, other.Bounds)
							if overlap.Width > 0.01 && overlap.Height > 0.01 {
								t.Errorf("controls %q and %q overlap: %+v", hit.Label, other.Label, overlap)
							}
						}
						previous = append(previous, hit)
					}
				}
				click := func(id gfx.ID) {
					t.Helper()
					hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
					if !ok {
						t.Fatalf("missing click target %v", id)
					}
					if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
					if err := view.Rest(); err != nil {
						t.Fatal(err)
					}
				}
				checkControls(panel, hitAIExamples.At(0), hitAIExamples.At(1), hitAIExamples.At(2),
					hitAIManageSuggestions, hitAIExamplesRefresh, hitAIPreviewSuggestions)
				click(hitAIPreviewSuggestions)
				if state.ai.suggestions.preview == nil || state.ai.suggestions.request != nil {
					t.Fatal("preview action did not open the dialog without starting a request")
				}
				checkControls(gfx.Rect{Width: width, Height: height}, hitAICancelSuggestionPreview, hitAIGenerateSuggestions)
				click(hitAICancelSuggestionPreview)
				if state.ai.suggestions.preview != nil || state.ai.suggestions.request != nil {
					t.Fatal("cancel left a preview or started a request")
				}
				click(hitAIManageSuggestions)
				if state.activeSection != sectionSettings || state.settingsCategory != settingsAssistant {
					t.Fatal("manage action did not open Assistant settings")
				}
			})
		}
	}
}
