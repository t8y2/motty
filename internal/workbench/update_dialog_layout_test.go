package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
)

// The footer gives the actions their room first, and the source picker what is
// left. In English the three action labels are wide enough that the picker's
// fixed width used to overflow the row and paint under Skip; every language
// must keep the picker and the actions apart.
func TestTheUpdateFooterKeepsTheSourcePickerClearOfItsActions(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		t.Run(language.String(), func(t *testing.T) {
			updater, quits, opened := &fakeUpdater{}, 0, []string(nil)
			state := updateWindow(t, updater, &quits, &opened)
			state.setLanguage(language)
			var canvas gfx.Canvas
			state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})

			official := state.messages.Tools.GeneralUpdateSourceOfficial
			source, ok := canvasHitWhere(&canvas, func(hit gfx.Hit) bool {
				return hit.Role == gfx.RoleCombobox && hit.Value == official
			})
			if !ok {
				t.Fatalf("the update source picker is not painted: %+v", canvas.Hits())
			}
			// The widest option has to be readable at the dialog's normal size,
			// not merely clipped to avoid the overlap.
			if source.Bounds.Width < updateSourceSelectWidth {
				t.Fatalf("the source picker is %g wide, want %g", source.Bounds.Width, updateSourceSelectWidth)
			}
			actions := 0
			for _, id := range []gfx.ID{hitUpdateSkip, hitUpdateLater, hitUpdateInstall, hitUpdateDownload} {
				action, ok := canvasHit(canvas.Hits(), id)
				if !ok {
					continue
				}
				actions++
				if overlap := intersectRect(source.Bounds, action.Bounds); overlap.Width > 0 && overlap.Height > 0 {
					t.Fatalf("the source picker %+v overlaps %q %+v", source.Bounds, action.Label, action.Bounds)
				}
			}
			if actions < 3 {
				t.Fatalf("the footer painted %d actions, want at least three: %+v", actions, canvas.Hits())
			}
		})
	}
}

// A window smaller than the dialog's preferred width still must not stack the
// picker and the actions: the picker takes what the row leaves and its label
// is clipped instead of painted over the buttons.
func TestTheUpdateFooterFitsANarrowWindow(t *testing.T) {
	updater, quits, opened := &fakeUpdater{}, 0, []string(nil)
	state := updateWindow(t, updater, &quits, &opened)
	state.setLanguage(i18n.English)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 640, Height: 600})

	official := state.messages.Tools.GeneralUpdateSourceOfficial
	source, ok := canvasHitWhere(&canvas, func(hit gfx.Hit) bool {
		return hit.Role == gfx.RoleCombobox && hit.Value == official
	})
	if !ok {
		t.Fatalf("the update source picker is not painted: %+v", canvas.Hits())
	}
	for _, id := range []gfx.ID{hitUpdateSkip, hitUpdateLater, hitUpdateInstall, hitUpdateDownload} {
		action, ok := canvasHit(canvas.Hits(), id)
		if !ok {
			continue
		}
		if overlap := intersectRect(source.Bounds, action.Bounds); overlap.Width > 0 && overlap.Height > 0 {
			t.Fatalf("the source picker %+v overlaps %q %+v", source.Bounds, action.Label, action.Bounds)
		}
	}
}
