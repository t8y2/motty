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
	state.terminalToolbarPosition = position
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
		terminal.railContentExtent = 0
	}
}
