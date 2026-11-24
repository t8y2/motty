//go:build !darwin

package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

// defaultPaneSplitChords are the split bindings this platform ships: Control
// with Shift, because Control+Alt belongs to the window managers of GNOME and
// KDE (workspace switching, terminal launch), and a chord there is taken by the
// desktop before Motty ever sees it. Control+Shift+D and Control+Shift+E are
// clear of Motty's other defaults.
func defaultPaneSplitChords() (horizontal, vertical keymap.Chord) {
	return keymap.Chord{Modifiers: gfx.ModifierControl | gfx.ModifierShift, Char: 'd'},
		keymap.Chord{Modifiers: gfx.ModifierControl | gfx.ModifierShift, Char: 'e'}
}

// defaultBroadcastChord is the broadcast-input binding this platform ships:
// Control with Shift for the reason the split chords use it, and clear of
// Motty's other defaults. Control+Alt+I would never leave GNOME and KDE.
// defaultBroadcastAllChord adds Alt to it for the broadcast to all tabs.
func defaultBroadcastAllChord() keymap.Chord {
	return keymap.Chord{Modifiers: gfx.ModifierControl | gfx.ModifierShift | gfx.ModifierAlt, Char: 'i'}
}

func defaultBroadcastChord() keymap.Chord {
	return keymap.Chord{Modifiers: gfx.ModifierControl | gfx.ModifierShift, Char: 'i'}
}
