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

// The minimum window height is a promise to the sidebar: at it every row down to
// Settings is still on screen, and the header keeps the spacing it has at any
// other height. The rows have fixed heights, so a row added without raising the
// minimum would push the last one out, and the header must give up the flexible
// space rather than its gap to the first row.
func TestSidebarFitsAtTheMinimumWindowSize(t *testing.T) {
	paint := func(height float32) (*gfx.Canvas, *State) {
		state := newTestState(t, testOptions(t))
		var canvas gfx.Canvas
		state.Paint(&canvas, gfx.Size{Width: MinimumWindowWidth, Height: height})
		return &canvas, state
	}
	canvas, state := paint(MinimumWindowHeight)
	snapshot := app.Snapshot{Width: MinimumWindowWidth, Height: MinimumWindowHeight, Texts: canvas.Texts(), Hits: canvas.Hits()}
	for _, problem := range headless.CheckLayout(snapshot) {
		t.Errorf("%s %q %s at %+v", problem.Kind, problem.Text, problem.Detail, problem.Bounds)
	}
	tallCanvas, tall := paint(MinimumWindowHeight + 200)
	shortGap := panelHit(t, canvas, hitHosts).Bounds.Y - state.chrome.lockup.Rect().Bottom()
	tallGap := panelHit(t, tallCanvas, hitHosts).Bounds.Y - tall.chrome.lockup.Rect().Bottom()
	if shortGap != tallGap {
		t.Errorf("the logo sits %.1f above the first row at the minimum height and %.1f at a taller one", shortGap, tallGap)
	}
}
