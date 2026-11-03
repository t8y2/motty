package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

type terminalToolbarSide uint8

const (
	terminalToolbarRight terminalToolbarSide = iota
	terminalToolbarLeft
)

func (side terminalToolbarSide) String() string {
	switch side {
	case terminalToolbarLeft:
		return "left"
	default:
		return "right"
	}
}

func terminalToolbarSideFromString(value string, fallback terminalToolbarSide) terminalToolbarSide {
	switch strings.TrimSpace(value) {
	case "left":
		return terminalToolbarLeft
	case "right":
		return terminalToolbarRight
	default:
		if fallback == terminalToolbarLeft {
			return terminalToolbarLeft
		}
		return terminalToolbarRight
	}
}

type terminalToolbarDock uint8

const (
	terminalToolbarDockTop terminalToolbarDock = iota
	terminalToolbarDockBottom
)

func (dock terminalToolbarDock) String() string {
	if dock == terminalToolbarDockBottom {
		return "bottom"
	}
	return "top"
}

func terminalToolbarDockFromString(value string) terminalToolbarDock {
	if strings.TrimSpace(value) == terminalToolbarDockBottom.String() {
		return terminalToolbarDockBottom
	}
	return terminalToolbarDockTop
}

type terminalToolbarZone uint8

const (
	terminalToolbarZoneLeftTop terminalToolbarZone = iota
	terminalToolbarZoneLeftBottom
	terminalToolbarZoneRightTop
	terminalToolbarZoneRightBottom
)

func (zone terminalToolbarZone) String() string {
	switch zone {
	case terminalToolbarZoneLeftTop:
		return "left-top"
	case terminalToolbarZoneLeftBottom:
		return "left-bottom"
	case terminalToolbarZoneRightBottom:
		return "right-bottom"
	default:
		return "right-top"
	}
}

func terminalToolbarZoneFromString(value string) (terminalToolbarZone, bool) {
	switch strings.TrimSpace(value) {
	case "left-top":
		return terminalToolbarZoneLeftTop, true
	case "left-bottom":
		return terminalToolbarZoneLeftBottom, true
	case "right-top":
		return terminalToolbarZoneRightTop, true
	case "right-bottom":
		return terminalToolbarZoneRightBottom, true
	default:
		return terminalToolbarZoneRightTop, false
	}
}

func terminalToolbarZoneFromParts(side terminalToolbarSide, dock terminalToolbarDock) terminalToolbarZone {
	if side == terminalToolbarLeft {
		if dock == terminalToolbarDockBottom {
			return terminalToolbarZoneLeftBottom
		}
		return terminalToolbarZoneLeftTop
	}
	if dock == terminalToolbarDockBottom {
		return terminalToolbarZoneRightBottom
	}
	return terminalToolbarZoneRightTop
}

func (zone terminalToolbarZone) Side() terminalToolbarSide {
	if zone == terminalToolbarZoneLeftTop || zone == terminalToolbarZoneLeftBottom {
		return terminalToolbarLeft
	}
	return terminalToolbarRight
}

func (zone terminalToolbarZone) Dock() terminalToolbarDock {
	if zone == terminalToolbarZoneLeftBottom || zone == terminalToolbarZoneRightBottom {
		return terminalToolbarDockBottom
	}
	return terminalToolbarDockTop
}

func terminalToolbarZoneForEntry(entry toolbarEntry, fallback terminalToolbarSide) terminalToolbarZone {
	if zone, ok := terminalToolbarZoneFromString(entry.Zone); ok {
		return zone
	}
	return terminalToolbarZoneFromParts(
		terminalToolbarSideFromString(entry.Side, fallback),
		terminalToolbarDockTop,
	)
}

func (state *State) terminalToolbarSideFor(id ui.ID) terminalToolbarSide {
	return state.terminalToolbarZoneFor(id).Side()
}

func (state *State) terminalToolbarZoneFor(id ui.ID) terminalToolbarZone {
	for _, entry := range state.toolbars.entries(toolbarTerminal) {
		spec, ok := toolbarSpecFor(toolbarTerminal, entry.Key)
		if ok && spec.id == id {
			return terminalToolbarZoneForEntry(entry, terminalToolbarRight)
		}
	}
	return terminalToolbarZoneRightTop
}

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

func (state *State) settingsToolbarSizeOptions() [3]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitToolbarSizeSmall, Label: state.messages.SettingsToolbarSizeSmall},
		{ID: hitToolbarSizeMedium, Label: state.messages.SettingsToolbarSizeMedium},
		{ID: hitToolbarSizeLarge, Label: state.messages.SettingsToolbarSizeLarge},
	}
}

type terminalToolbarMetrics struct {
	railExtent float32
	toolSize   float32
	toolGap    float32
	iconSize   float32
	inset      float32
}

func (state *State) terminalToolbarMetrics() terminalToolbarMetrics {
	switch state.terminalToolbarSize {
	case terminalToolbarMedium:
		return terminalToolbarMetrics{railExtent: 48, toolSize: 40, toolGap: 5, iconSize: 20, inset: 4}
	case terminalToolbarLarge:
		return terminalToolbarMetrics{railExtent: 56, toolSize: 48, toolGap: 6, iconSize: 24, inset: 4}
	default:
		return terminalToolbarMetrics{
			railExtent: terminalToolRailExtent, toolSize: terminalBarToolSize,
			toolGap: terminalBarToolGap, iconSize: 17, inset: 4,
		}
	}
}

func (state *State) setTerminalToolbarSize(size terminalToolbarSize) bool {
	if state.terminalToolbarSize == size {
		return false
	}
	state.terminalToolbarSize = size
	return true
}

func (state *State) applyTerminalToolbarLayoutChange() {
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
		for side := range terminal.rails {
			terminal.rails[side] = terminalRailState{}
		}
	}
}
