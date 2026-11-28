package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestApplicationThemeWindowsFollowSharedSchemeChanges(t *testing.T) {
	pair := newWindowsOnOneStore(t)
	first, second := pair.first, pair.second
	scheme := colorScheme{
		name: "Window test violet", background: [3]uint8{24, 12, 40},
		foreground: [3]uint8{240, 233, 248}, cursor: [3]uint8{240, 233, 248},
	}
	id, err := first.saveCustomScheme(scheme)
	if err != nil {
		t.Fatal(err)
	}
	first.chooseAppearanceMode(appearanceFollowTerminal)
	first.setTerminalScheme(id)
	first.save()
	if second.appearanceMode != appearanceFollowTerminal || second.activeScheme() != id || second.pal.workspace != gfx.RGB(24, 12, 40) {
		t.Fatalf("second window: mode %v, scheme %q, workspace %v", second.appearanceMode, second.activeScheme(), second.pal.workspace)
	}

	scheme.id = id
	scheme.background, scheme.foreground = [3]uint8{238, 216, 254}, [3]uint8{24, 12, 40}
	if _, err := first.saveCustomScheme(scheme); err != nil {
		t.Fatal(err)
	}
	first.refreshApplicationTheme()
	first.save()
	if second.activeScheme() != id || second.appearance != appearanceLight || second.pal.workspace != gfx.RGB(238, 216, 254) {
		t.Fatalf("second window after edit: scheme %q, appearance %v, workspace %v", second.activeScheme(), second.appearance, second.pal.workspace)
	}
}
