package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

type terminalToolbarPosition uint8

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
		terminal.railViewport = ui.Rect{}
		terminal.railBounds = ui.Rect{}
		terminal.railContentExtent = 0
	}
}
