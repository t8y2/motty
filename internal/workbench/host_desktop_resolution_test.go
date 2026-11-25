package workbench

import (
	"encoding/json"
	"strings"
	"testing"

	"motty/internal/i18n"
)

func TestParseDesktopSize(t *testing.T) {
	for text, want := range map[string]desktopSize{
		"1920x1080":    {1920, 1080},
		" 1920 × 1080": {1920, 1080},
		"1281*721":     {1280, 721}, // an odd width is rounded down, as the display channel needs it
	} {
		if got, ok := parseDesktopSize(text); !ok || got != want {
			t.Errorf("parseDesktopSize(%q) = %v, %v; want %v", text, got, ok, want)
		}
	}
	for _, text := range []string{"", "1920", "1920x", "x1080", "100x100", "99999x1080", "ax1080", "1920x1080x2"} {
		if got, ok := parseDesktopSize(text); ok {
			t.Errorf("parseDesktopSize(%q) = %v, want no size", text, got)
		}
	}
}

// A host that follows its pane is saved as it always was; a chosen resolution
// survives the round trip, and RDP is the only protocol that keeps one.
func TestDesktopResolutionIsSavedOnlyWhenChosen(t *testing.T) {
	host := Host{Label: "h", Address: "h.example.com", Protocol: protocolRDP}
	encoded, _ := json.Marshal(host.transport())
	if strings.Contains(string(encoded), "desktop_width") {
		t.Fatalf("a host that never chose is saved with a size: %s", encoded)
	}
	host.DesktopWidth, host.DesktopHeight = 1920, 1080
	var restored Host
	if !restored.applyTransport(host.transport()) || restored.DesktopWidth != 1920 || restored.DesktopHeight != 1080 {
		t.Fatalf("restored = %+v", restored)
	}
	if _, fixed := restored.fixedDesktopSize(); !fixed {
		t.Fatal("a restored resolution is not fixed")
	}
	host.DesktopWidth = 50
	restored = Host{}
	if !restored.applyTransport(host.transport()) || restored.DesktopWidth != 0 || restored.DesktopHeight != 0 {
		t.Fatalf("a size the protocol cannot carry restored as %+v", restored)
	}
	vnc := Host{Protocol: protocolVNC, DesktopWidth: 1920, DesktopHeight: 1080}
	if _, fixed := vnc.fixedDesktopSize(); fixed {
		t.Fatal("a VNC host has a fixed resolution")
	}
}

// The form shows what the host has and gives back what it shows, and a custom
// size that is not one is refused before it is saved.
func TestHostEditorResolution(t *testing.T) {
	for _, tc := range []struct {
		width, height int
		choice        desktopSize
	}{{0, 0, desktopSize{}}, {1920, 1080, desktopSize{1920, 1080}}, {1366, 768, customDesktopSize}} {
		var editor hostEditor
		editor.load(Host{Label: "h", Address: "h.example.com", Port: 3389, User: "u", Protocol: protocolRDP, DesktopWidth: tc.width, DesktopHeight: tc.height})
		if editor.resolution != tc.choice {
			t.Fatalf("%dx%d: choice = %v, want %v", tc.width, tc.height, editor.resolution, tc.choice)
		}
		if host := editor.host(1); host.DesktopWidth != tc.width || host.DesktopHeight != tc.height {
			t.Fatalf("%dx%d: saved as %dx%d", tc.width, tc.height, host.DesktopWidth, host.DesktopHeight)
		}
	}
	var editor hostEditor
	editor.load(Host{Label: "h", Address: "h.example.com", Port: 3389, User: "u", Protocol: protocolRDP})
	editor.resolution = customDesktopSize
	editor.field(fieldResolution).Insert("wide")
	if message := editor.validate(nil, 0, i18n.For(i18n.English)); message == "" {
		t.Fatal("a custom resolution that is not a size was accepted")
	}
}
