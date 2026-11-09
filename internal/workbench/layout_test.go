package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

// A window the size of a small laptop's must show every page whole: no line cut
// off by its box, none running out of its button, none over another. headless
// CheckLayout is the same check a developer runs with the layout command.
func TestPagesShowEveryLineWholeInASmallWindow(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		options := testOptions(t)
		options.PreferredLanguages = func() []string { return []string{language} }
		state := newTestState(t, options)
		check := func(page string) {
			t.Helper()
			var canvas gfx.Canvas
			state.Paint(&canvas, gfx.Size{Width: 1000, Height: 640})
			snapshot := app.Snapshot{Width: 1000, Height: 640, Texts: canvas.Texts(), Hits: canvas.Hits()}
			for _, problem := range headless.CheckLayout(snapshot) {
				t.Errorf("%s, %s: %s %q %s at %+v", language, page, problem.Kind, problem.Text, problem.Detail, problem.Bounds)
			}
		}
		check("hosts")
		for name, id := range map[string]gfx.ID{"keychain": hitKeychain, "proxies": hitProxies, "port forwarding": hitPortForwarding, "known hosts": hitKnownHosts} {
			state.Click(id)
			check(name)
		}
		state.Click(hitSettings)
		for category := settingsGeneral; category < settingsCategoryCount; category++ {
			state.selectSettingsCategory(category)
			check("settings " + state.settingsCategoryName(category))
		}
	}
}
