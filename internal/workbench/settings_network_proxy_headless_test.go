//go:build darwin || windows || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestNetworkProxySettingThroughNativeDriver(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.setLanguage(i18n.Chinese)
	state.Click(hitSettings)
	view, err := headless.Open(app.Config{Title: "Local terminal proxy settings", Width: 1280, Height: 900, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize}, nil, headless.Options{Scale: 1.25})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	find := func(id gfx.ID) gfx.Hit {
		for _, hit := range view.Snapshot().Hits {
			if hit.ID == id {
				return hit
			}
		}
		t.Fatalf("setting hit %v is missing", id)
		return gfx.Hit{}
	}
	connection := find(hitSettingsCategoryList.At(int(settingsConnection)))
	if err := view.Click(connection.Bounds.X+connection.Bounds.Width/2, connection.Bounds.Y+connection.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	findChoice := func(label string) gfx.Hit {
		for _, hit := range view.Snapshot().Hits {
			if hit.Label == label && hit.Role == gfx.RoleRadio {
				return hit
			}
		}
		t.Fatalf("choice %q is missing", label)
		return gfx.Hit{}
	}
	directLabel := state.messages.SettingsNetworkProxyDirect
	systemLabel := state.messages.SettingsNetworkProxySystem
	if hit := findChoice(directLabel); !hit.Selected {
		t.Fatal("leaving the proxy unset is not the default selection")
	}
	system := findChoice(systemLabel)
	if err := view.Click(system.Bounds.X+system.Bounds.Width/2, system.Bounds.Y+system.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.networkProxy != networkProxySystem || !findChoice(systemLabel).Selected || findChoice(directLabel).Selected {
		t.Fatal("the native driver did not select follow system")
	}
}
