package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

type terminalToolbarPosition uint8

type terminalToolbarSize uint8

const (
	terminalToolbarSmall terminalToolbarSize = iota
	terminalToolbarMedium
	terminalToolbarLarge
)

func (size terminalToolbarSize) String() string {
	switch size {
	case terminalToolbarMedium:
		return "medium"
	case terminalToolbarLarge:
		return "large"
	default:
		return "small"
	}
}

func terminalToolbarSizeFromString(value string) terminalToolbarSize {
	switch strings.TrimSpace(value) {
	case "medium":
		return terminalToolbarMedium
	case "large":
		return terminalToolbarLarge
	default:
		return terminalToolbarSmall
	}
}

type terminalToolbarMetrics struct {
	railExtent, toolSize, gap, iconSize float32
}

func (state *State) terminalToolbarMetrics() terminalToolbarMetrics {
	switch state.terminalToolbarSize {
	case terminalToolbarMedium:
		return terminalToolbarMetrics{48, 40, 5, 20}
	case terminalToolbarLarge:
		return terminalToolbarMetrics{56, 48, 6, 24}
	default:
		return terminalToolbarMetrics{terminalToolRailExtent, terminalBarToolSize, terminalBarToolGap, 17}
	}
}

func (state *State) settingsToolbarSizeOptions() [3]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitToolbarSizeSmall, Label: state.messages.SettingsToolbarSizeSmall},
		{ID: hitToolbarSizeMedium, Label: state.messages.SettingsToolbarSizeMedium},
		{ID: hitToolbarSizeLarge, Label: state.messages.SettingsToolbarSizeLarge},
	}
}

func (state *State) setTerminalToolbarSize(size terminalToolbarSize) bool {
	if state.terminalToolbarSize == size {
		return false
	}
	state.terminalToolbarSize = size
	state.toolbarDockChanged()
	return true
}

const (
	terminalToolbarRight terminalToolbarPosition = iota
	terminalToolbarLeft
)

func (position terminalToolbarPosition) String() string {
	switch position {
	case terminalToolbarLeft:
		return "left"
	default:
		return "right"
	}
}

func terminalToolbarPositionFromString(value string) terminalToolbarPosition {
	switch strings.TrimSpace(value) {
	case "left":
		return terminalToolbarLeft
	default:
		return terminalToolbarRight
	}
}

func (state *State) settingsToolbarPositionOptions() [2]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsToolbarLeft, Label: state.messages.SettingsToolbarPositionLeft},
		{ID: hitSettingsToolbarRight, Label: state.messages.SettingsToolbarPositionRight},
	}
}

func (state *State) setTerminalToolbarPosition(position terminalToolbarPosition) bool {
	if state.terminalToolbarPosition == position {
		return false
	}
	if state.toolbars.dock != nil {
		// The group setting moves session tools together. Direct manipulation
		// can subsequently distribute individual tools across both sides.
		for i := range state.toolbars.dock {
			entry := &state.toolbars.dock[i]
			if entry.Group != toolbarTerminal || !toolbarDockVertical(entry.Zone) {
				continue
			}
			lower := entry.Zone == toolbarDockSideBottom || entry.Zone == toolbarDockLeftBottom
			entry.Zone = toolbarDockSideTop
			if position == terminalToolbarLeft {
				entry.Zone = toolbarDockLeftTop
			}
			if lower {
				entry.Zone = toolbarDockSideBottom
				if position == terminalToolbarLeft {
					entry.Zone = toolbarDockLeftBottom
				}
			}
		}
	}
	state.terminalToolbarPosition = position
	state.toolbarDock.sideScroll = [4]float32{}
	state.resetTerminalToolRails()
	state.fitTerminals()
	state.fitSFTP(state.viewport)
	return true
}

func (state *State) resetTerminalToolRails() {
	for index := range state.sessions {
		terminal := state.sessions[index].term
		if terminal == nil {
			continue
		}
		terminal.railScroll = 0
		terminal.railViewport = gfx.Rect{}
		terminal.railBounds = gfx.Rect{}
		terminal.railContentExtent = 0
		terminal.railIDs = [maxBarTools]gfx.ID{}
		terminal.railEnabled = [maxBarTools]bool{}
		terminal.railCount = 0
	}
}
