//go:build darwin

package workbench

import "github.com/vivaui/viva/gfx"

// modifierKeycap is the symbol a Mac keyboard prints on a modifier key.
func modifierKeycap(modifier gfx.Modifiers) string {
	switch modifier {
	case gfx.ModifierControl:
		return "⌃"
	case gfx.ModifierAlt:
		return "⌥"
	case gfx.ModifierShift:
		return "⇧"
	default:
		return "⌘"
	}
}
