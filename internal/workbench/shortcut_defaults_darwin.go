//go:build darwin

package workbench

import "github.com/vivaui/viva/ui"

func defaultShortcutModifier() ui.Modifiers { return ui.ModifierMeta }

// defaultPaneSplitChords are the split bindings this platform ships: the chords
// Warp, iTerm2 and the macOS terminals use, so a split is where a terminal user
// expects it. Command+D and Shift+Command+D are clear of both Motty's other
// defaults and the menu bar Viva installs.
func defaultPaneSplitChords() (horizontal, vertical shortcutChord) {
	return shortcutChord{Modifiers: ui.ModifierMeta, Key: 'd'},
		shortcutChord{Modifiers: ui.ModifierMeta | ui.ModifierShift, Key: 'd'}
}
