//go:build darwin || windows || linux

package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
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
	find := func(id ui.ID) ui.Hit {
		for _, hit := range view.Snapshot().Hits {
			if hit.ID == id {
				return hit
			}
		}
		t.Fatalf("setting hit %v is missing", id)
		return ui.Hit{}
	}
	connection := find(hitSettingsCategoryList.At(int(settingsConnection)))
	if err := view.Click(connection.Bounds.X+connection.Bounds.Width/2, connection.Bounds.Y+connection.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if hit := find(hitSettingsNetworkDirect); !hit.Selected {
		t.Fatal("direct is not the default selection")
	}
	system := find(hitSettingsNetworkSystem)
	if err := view.Click(system.Bounds.X+system.Bounds.Width/2, system.Bounds.Y+system.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.networkProxy != networkProxySystem || !find(hitSettingsNetworkSystem).Selected || find(hitSettingsNetworkDirect).Selected {
		t.Fatal("the native driver did not select follow system")
	}
	if directory := os.Getenv("MOTTY_NETWORK_PROXY_SCREENSHOTS"); directory != "" {
		if err := view.Screenshot(filepath.Join(directory, "connection-local-terminal-proxy.png")); err != nil {
			t.Fatal(err)
		}
	}
}
