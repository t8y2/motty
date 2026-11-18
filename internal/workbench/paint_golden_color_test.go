package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The find settings and the tab menu's color row are the surfaces the page
// color controls were migrated onto; these scenes pin what they paint.
func init() {
	extraPaintScenes = append(extraPaintScenes,
		settingsScene("terminal-find-settings", func(t *testing.T, w *settingsWorld) {
			w.state.selectSettingsCategory(settingsTerminal)
			w.state.settings.jump = w.state.messages.FindSettings.Title
			w.state.Paint(&gfx.Canvas{}, paintDesktop)
		}),
		settingsScene("terminal-find-settings-colors", func(t *testing.T, w *settingsWorld) {
			w.state.selectSettingsCategory(settingsTerminal)
			w.state.settings.jump = w.state.messages.FindSettings.Title
			w.state.Paint(&gfx.Canvas{}, paintDesktop)
			clickPageLabelIn(t, w.state, paintDesktop, gfx.RoleButton, w.state.messages.FindSettings.Foreground)
		}),
		paintScene{name: "tab-menu-colors", sizes: []gfx.Size{paintDesktop}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
			state, tabs := fourTabs(t)
			rightClickTab(state, hitTabList.At(int(tabs[0])))
			return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
		}},
	)
}
