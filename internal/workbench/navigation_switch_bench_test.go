//go:build darwin || windows || linux

package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func BenchmarkNavigationSwitchThroughTheDriver(bench *testing.B) {
	for _, scenario := range []struct {
		name     string
		settings bool
		targets  [2]gfx.ID
	}{
		{"hosts-workspaces", false, [2]gfx.ID{hitHosts, hitWorkspaces}},
		{"hosts-settings", false, [2]gfx.ID{hitHosts, hitSettings}},
		{"general-terminal", true, [2]gfx.ID{hitSettingsCategoryList.At(int(settingsGeneral)), hitSettingsCategoryList.At(int(settingsTerminal))}},
		{"general-connection", true, [2]gfx.ID{hitSettingsCategoryList.At(int(settingsGeneral)), hitSettingsCategoryList.At(int(settingsConnection))}},
		{"general-files", true, [2]gfx.ID{hitSettingsCategoryList.At(int(settingsGeneral)), hitSettingsCategoryList.At(int(settingsFiles))}},
		{"general-shortcuts", true, [2]gfx.ID{hitSettingsCategoryList.At(int(settingsGeneral)), hitSettingsCategoryList.At(int(settingsShortcuts))}},
	} {
		bench.Run(scenario.name, func(bench *testing.B) {
			state := newTestState(bench, testOptions(bench))
			state.setLanguage(i18n.Chinese)
			if scenario.settings {
				state.Click(hitSettings)
			}
			session, err := headless.Open(app.Config{
				Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint,
				OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
				OnResize: state.Resize, OnScroll: state.Scroll,
			}, nil, headless.Options{Scale: 2})
			if err != nil {
				bench.Fatal(err)
			}
			defer session.Close()
			var points [2]gfx.Point
			for index, target := range scenario.targets {
				found := false
				for _, hit := range session.Snapshot().Hits {
					if hit.ID == target {
						points[index] = gfx.Point{X: hit.Bounds.X + hit.Bounds.Width/2, Y: hit.Bounds.Y + hit.Bounds.Height/2}
						found = true
						break
					}
				}
				if !found {
					bench.Fatalf("navigation target %v is not visible", target)
				}
			}
			switchTo := func(index int) {
				point := points[index]
				if err := session.Click(point.X, point.Y); err != nil {
					bench.Fatal(err)
				}
			}
			for range 2 {
				for index := range points {
					switchTo(index)
				}
			}
			bench.ReportAllocs()
			bench.ResetTimer()
			for index := 0; bench.Loop(); index++ {
				switchTo(index % len(points))
				if err := session.Driver().Advance(40 * time.Millisecond); err != nil {
					bench.Fatal(err)
				}
			}
		})
	}
}
