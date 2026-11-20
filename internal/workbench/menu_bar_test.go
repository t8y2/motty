package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

// The macOS File menu shows each action's chord. Rebinding one must rebuild the
// menu, or it keeps showing and running the old chord.
func TestRebindingAShortcutRebuildsTheMenuBar(t *testing.T) {
	options := testOptions(t)
	var menus [][]app.Menu
	options.SetMenuBar = func(menu []app.Menu) error {
		menus = append(menus, menu)
		return nil
	}
	state := newTestState(t, options)
	state.SyncMenuBar()
	if len(menus) != 1 {
		t.Fatalf("initial menu installs = %d, want 1", len(menus))
	}

	chord := keymap.Chord{Modifiers: keymap.Primary | gfx.ModifierAlt | gfx.ModifierShift, Char: 'q'}
	if conflict, ok := state.shortcuts.bind(shortcutSettings, chord); !ok {
		t.Fatalf("rebinding Settings conflicted with %v", conflict)
	}
	state.SyncMenuBar()
	if len(menus) != 2 {
		t.Fatalf("menu installs after rebinding = %d, want a rebuild", len(menus))
	}
	found := false
	for _, menu := range menus[1] {
		for _, item := range menu.Items {
			if item.Shortcut == chord {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("the rebuilt menu does not carry the new Settings chord %v", chord)
	}
}
