//go:build darwin

package workbench

import (
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
)

// defaultPaneSplitChords are the split bindings this platform ships: the chords
// Warp, iTerm2 and the macOS terminals use, so a split is where a terminal user
// expects it. Command+D and Shift+Command+D are clear of both Motty's other
// defaults and the menu bar Viva installs.
func defaultPaneSplitChords() (horizontal, vertical keymap.Chord) {
	return keymap.Chord{Modifiers: ui.ModifierMeta, Char: 'd'},
		keymap.Chord{Modifiers: ui.ModifierMeta | ui.ModifierShift, Char: 'd'}
}
