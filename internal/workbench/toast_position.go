package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
)

// toastPlacement is the corner of the window the notices gather in. Top center
// is what Motty has always used, so a state that never chose one keeps it.
type toastPlacement uint8

const (
	toastTopCenter toastPlacement = iota
	toastBottomCenter
	toastTopLeft
	toastTopRight
	toastBottomLeft
	toastBottomRight
)

func (placement toastPlacement) String() string {
	switch placement {
	case toastBottomCenter:
		return "bottom-center"
	case toastTopLeft:
		return "top-left"
	case toastTopRight:
		return "top-right"
	case toastBottomLeft:
		return "bottom-left"
	case toastBottomRight:
		return "bottom-right"
	default:
		return "top-center"
	}
}

func toastPlacementFromString(value string) toastPlacement {
	switch strings.TrimSpace(value) {
	case "bottom-center":
		return toastBottomCenter
	case "top-left":
		return toastTopLeft
	case "top-right":
		return toastTopRight
	case "bottom-left":
		return toastBottomLeft
	case "bottom-right":
		return toastBottomRight
	default:
		return toastTopCenter
	}
}

// position is the framework corner the placement names.
func (placement toastPlacement) position() components.ToastPosition {
	switch placement {
	case toastBottomCenter:
		return components.ToastBottomCenter
	case toastTopLeft:
		return components.ToastTopLeft
	case toastTopRight:
		return components.ToastTopRight
	case toastBottomLeft:
		return components.ToastBottomLeft
	case toastBottomRight:
		return components.ToastBottomRight
	default:
		return components.ToastTopCenter
	}
}

var settingsToastPlacementChoices = [...]toastPlacement{toastTopCenter, toastBottomCenter, toastTopLeft, toastTopRight, toastBottomLeft, toastBottomRight}

func (state *State) settingsToastPlacementLabel(placement toastPlacement) string {
	m := state.messages
	switch placement {
	case toastBottomCenter:
		return m.SettingsToastBottomCenter
	case toastTopLeft:
		return m.SettingsToastTopLeft
	case toastTopRight:
		return m.SettingsToastTopRight
	case toastBottomLeft:
		return m.SettingsToastBottomLeft
	case toastBottomRight:
		return m.SettingsToastBottomRight
	default:
		return m.SettingsToastTopCenter
	}
}
