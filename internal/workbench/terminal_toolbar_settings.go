package workbench

import (
	"strings"

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

func (state *State) setTerminalToolbarSize(size terminalToolbarSize) bool {
	if state.terminalToolbarSize == size {
		return false
	}
	state.terminalToolbarSize = size
	state.applyTerminalToolbarSize()
	return true
}

// applyTerminalToolbarSize applies the size the page's choice already wrote.
func (state *State) applyTerminalToolbarSize() {
	state.toolbarDockChanged()
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

func (state *State) setTerminalToolbarPosition(position terminalToolbarPosition) bool {
	if state.terminalToolbarPosition == position {
		return false
	}
	state.terminalToolbarPosition = position
	state.applyTerminalToolbarPosition()
	return true
}

// applyTerminalToolbarPosition moves the terminal tools for the position the
// page's choice already wrote.
func (state *State) applyTerminalToolbarPosition() {
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
			if state.terminalToolbarPosition == terminalToolbarLeft {
				entry.Zone = toolbarDockLeftTop
			}
			if lower {
				entry.Zone = toolbarDockSideBottom
				if state.terminalToolbarPosition == terminalToolbarLeft {
					entry.Zone = toolbarDockLeftBottom
				}
			}
		}
	}
	state.toolbarDock.sideScroll = [4]float32{}
	state.resetTerminalToolRails()
	state.fitTerminals()
	state.fitSFTP(state.viewport)
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
