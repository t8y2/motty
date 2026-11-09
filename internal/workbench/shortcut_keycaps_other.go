//go:build !darwin

package workbench

import "github.com/vivaui/viva/gfx"

// modifierKeycap is the name printed on a modifier key.
func modifierKeycap(modifier gfx.Modifiers) string {
	switch modifier {
	case gfx.ModifierControl:
		return "Ctrl"
	case gfx.ModifierAlt:
		return "Alt"
	case gfx.ModifierShift:
		return "Shift"
	default:
		return "Win"
	}
}
