package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// The dialog's four actions stay inside its footer in both languages, in the
// narrowest window.
func TestToolbarSettingsFooterFitsInBothLanguages(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			options := freshOptions(t)
			options.PreferredLanguages = func() []string { return []string{language} }
			state := newTestState(t, options)
			state.openToolbarSettings(toolbarTerminal)
			size := gfx.Size{Width: 640, Height: 480}
			canvas, _ := paintAt(state, size)
			snapshot := app.Snapshot{Width: int(size.Width), Height: int(size.Height), Texts: canvas.Texts(), Hits: canvas.Hits()}
			var footer []gfx.Rect
			for _, id := range []gfx.ID{hitToolbarEnableAll, hitToolbarDisableAll, hitToolbarReset, hitToolbarSettingsClose} {
				hit, ok := findSnapshotHit(snapshot, id)
				if !ok {
					t.Fatalf("action %v is not in the footer", id)
				}
				if hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > size.Width || hit.Bounds.Y+hit.Bounds.Height > size.Height {
					t.Fatalf("action %v at %+v leaves the window", id, hit.Bounds)
				}
				for _, other := range footer {
					if hit.Bounds.X < other.X+other.Width && other.X < hit.Bounds.X+hit.Bounds.Width {
						t.Fatalf("action %v at %+v runs into %+v", id, hit.Bounds, other)
					}
				}
				footer = append(footer, hit.Bounds)
			}
		})
	}
}
