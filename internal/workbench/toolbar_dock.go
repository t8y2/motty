package workbench

import (
	"fmt"
	"slices"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

type toolbarDockZone string

const (
	toolbarDockTop         toolbarDockZone = "top"
	toolbarDockSideTop     toolbarDockZone = "side-top"
	toolbarDockSideBottom  toolbarDockZone = "side-bottom"
	toolbarDockBottom      toolbarDockZone = "bottom"
	toolbarDockLeftTop     toolbarDockZone = "left-top"
	toolbarDockLeftBottom  toolbarDockZone = "left-bottom"
	toolbarDockBottomRight toolbarDockZone = "bottom-right"
)

var toolbarDockZones = [...]toolbarDockZone{toolbarDockTop, toolbarDockSideTop, toolbarDockSideBottom, toolbarDockBottom, toolbarDockLeftTop, toolbarDockLeftBottom, toolbarDockBottomRight}

func toolbarDockAtBottom(zone toolbarDockZone) bool {
	return zone == toolbarDockBottom || zone == toolbarDockBottomRight
}

func toolbarDockVertical(zone toolbarDockZone) bool {
	return zone != toolbarDockTop && !toolbarDockAtBottom(zone)
}

func toolbarDockLeft(zone toolbarDockZone) bool {
	return zone == toolbarDockLeftTop || zone == toolbarDockLeftBottom
}

func (state *State) defaultToolbarDockZone(group toolbarGroup) toolbarDockZone {
	if group == toolbarTerminal && state.terminalToolbarPosition == terminalToolbarLeft {
		return toolbarDockLeftTop
	}
	return defaultToolbarDockZone(group)
}

func (state *State) ensureToolbarDock() {
	if state.toolbars.dock != nil {
		return
	}
	state.toolbars.restoreDock([]toolbarDockEntry{})
	if state.terminalToolbarPosition == terminalToolbarLeft {
		for i := range state.toolbars.dock {
			if state.toolbars.dock[i].Group == toolbarTerminal {
				state.toolbars.dock[i].Zone = toolbarDockLeftTop
			}
		}
	}
}

// Group identifies the action's context; Zone only controls its placement.
// Entry order is shared by zones so actions from different contexts can mix.
type toolbarDockEntry struct {
	Group toolbarGroup    `json:"group"`
	Key   string          `json:"key"`
	Zone  toolbarDockZone `json:"zone"`
}

func defaultToolbarDockZone(group toolbarGroup) toolbarDockZone {
	if group == toolbarWindow {
		return toolbarDockTop
	}
	return toolbarDockSideTop
}

func (prefs *toolbarPreferences) restoreDock(saved []toolbarDockEntry) {
	if saved == nil {
		return
	}
	prefs.dock = make([]toolbarDockEntry, 0, len(toolbarCatalog[0])+len(toolbarCatalog[1]))
	for _, entry := range saved {
		if entry.Group > toolbarTerminal {
			continue
		}
		if _, ok := toolbarSpecFor(entry.Group, entry.Key); !ok {
			continue
		}
		if slices.ContainsFunc(prefs.dock, func(other toolbarDockEntry) bool { return other.Group == entry.Group && other.Key == entry.Key }) {
			continue
		}
		if !slices.Contains(toolbarDockZones[:], entry.Zone) {
			entry.Zone = defaultToolbarDockZone(entry.Group)
		}
		prefs.dock = append(prefs.dock, entry)
	}
	// A terminal tool added by an upgrade follows the side the user's existing
	// terminal tools use; the group setting is not consulted here because it is
	// restored after the layout.
	missingZone := func(group toolbarGroup) toolbarDockZone {
		if group == toolbarTerminal {
			for _, placed := range prefs.dock {
				if placed.Group != toolbarTerminal {
					continue
				}
				switch {
				case toolbarDockLeft(placed.Zone):
					return toolbarDockLeftTop
				case toolbarDockVertical(placed.Zone):
					return toolbarDockSideTop
				}
			}
		}
		return defaultToolbarDockZone(group)
	}
	for group := toolbarWindow; group <= toolbarTerminal; group++ {
		for _, entry := range prefs.entries(group) {
			if slices.ContainsFunc(prefs.dock, func(other toolbarDockEntry) bool { return other.Group == group && other.Key == entry.Key }) {
				continue
			}
			placed := toolbarDockEntry{group, entry.Key, missingZone(group)}
			// The latency readout used to sit at the top of the rail; keep it
			// ahead of the actions it was pinned above.
			if group == toolbarTerminal && entry.Key == "latency" {
				prefs.dock = slices.Insert(prefs.dock, 0, placed)
				continue
			}
			prefs.dock = append(prefs.dock, placed)
		}
	}
}

func toolbarDockIdentity(id gfx.ID) (toolbarDockEntry, bool) {
	for group := toolbarWindow; group <= toolbarTerminal; group++ {
		for _, spec := range toolbarCatalog[group] {
			if spec.id == id {
				return toolbarDockEntry{group, spec.key, defaultToolbarDockZone(group)}, true
			}
		}
	}
	return toolbarDockEntry{}, false
}

func (state *State) windowDockTool(spec toolbarSpec) barTool {
	tool := barTool{id: spec.id, icon: spec.icon, hint: state.toolbarLabel(spec.id)}
	switch spec.id {
	case hitTheme:
		tool.icon, tool.hint = iconSun, state.messages.LabelThemeToLight
		if state.appearance == appearanceLight {
			tool.icon, tool.hint = iconMoon, state.messages.LabelThemeToDark
		}
	case hitTransferCenter:
		if state.transfers.buttonLabel != "" {
			tool.hint = state.transfers.buttonLabel
		}
		if state.transfers.open {
			tool.style = iconButtonActive
		}
	case hitAIToggle:
		if state.aiPanelVisible() {
			tool.style = iconButtonActive
		}
	}
	return tool
}

func (state *State) toolbarDockTools(zone toolbarDockZone, dst *[maxBarTools]barTool) []barTool {
	var terminal [maxBarTools]barTool
	var terminalTools []barTool
	if session, ok := state.terminalSideRailSession(); ok {
		terminalTools = state.activeTerminalTools(session, &terminal)
	} else if state.noteSidebarVisible() {
		// Restored/disconnected sessions still need the action that closes the browser.
		terminalTools = append(terminal[:0], barTool{id: hitNotesPanel, icon: iconNotebook, hint: state.noteText("Notes", "笔记"), style: iconButtonActive})
	}
	tools := dst[:0]
	// Connection progress is a transient navigation action, not a customizable
	// tool; preserve its existing place ahead of the session actions.
	if zone == state.defaultToolbarDockZone(toolbarTerminal) {
		for _, tool := range terminalTools {
			if tool.id == hitConnProgress {
				tools = append(tools, tool)
			}
		}
	}
	// An available update stays in the bar until it is installed or skipped, and
	// is absent otherwise. Like connection progress it is not a customizable
	// tool: it comes and goes with state, so it has no catalog entry to hide.
	if zone == state.defaultToolbarDockZone(toolbarWindow) && state.updateIndicatorVisible() {
		tools = append(tools, barTool{
			id: hitUpdateToolbar, icon: iconDownload, style: iconButtonUpdate,
			hint: fmt.Sprintf(state.messages.Tools.UpdateAvailableFmt, state.update.release.Version),
		})
	}
	appendEntry := func(group toolbarGroup, key string) {
		spec, ok := toolbarSpecFor(group, key)
		if !ok || !state.toolbarVisible(group, spec.id) {
			return
		}
		if group == toolbarWindow {
			tools = append(tools, state.windowDockTool(spec))
			return
		}
		for _, tool := range terminalTools {
			if tool.id == spec.id {
				tools = append(tools, tool)
				return
			}
		}
	}
	if state.toolbars.dock != nil {
		for _, entry := range state.toolbars.dock {
			if entry.Zone == zone {
				appendEntry(entry.Group, entry.Key)
			}
		}
	} else {
		for group := toolbarWindow; group <= toolbarTerminal; group++ {
			if state.defaultToolbarDockZone(group) != zone {
				continue
			}
			for _, entry := range state.toolbars.entries(group) {
				appendEntry(group, entry.Key)
			}
		}
	}
	// The compact pane-tools menu is synthesized after catalog ordering and has
	// no customizable entry of its own. Keep it with the terminal's default rail.
	if zone == state.defaultToolbarDockZone(toolbarTerminal) {
		for _, tool := range terminalTools {
			if tool.id == hitWorkspacePaneTools {
				tools = append(tools, tool)
			}
		}
	}
	if zone == state.defaultToolbarDockZone(toolbarTerminal) && !state.toolbarVisible(toolbarTerminal, hitLocalProxyOpen) {
		if session, ok := state.terminalSideRailSession(); ok {
			if snapshot := state.localProxySnapshot(session); snapshot.Phase == localProxyConnecting || snapshot.Phase == localProxyConnected || snapshot.Phase == localProxyFailed {
				tool := barTool{id: hitLocalProxyOpen, icon: iconGlobe, hint: state.messages.LocalProxy.Title, style: iconButtonActive}
				if snapshot.Phase == localProxyConnected && snapshot.RemotePort != 0 {
					tool.hint = fmt.Sprintf(state.messages.LocalProxy.ConnectedFmt, snapshot.RemotePort)
				}
				tools = append(tools, tool)
			}
		}
	}
	return tools
}

func (state *State) moveToolbarDockTool(id gfx.ID, zone toolbarDockZone, before gfx.ID) {
	entry, ok := toolbarDockIdentity(id)
	if !ok || id == before || !slices.Contains(toolbarDockZones[:], zone) {
		return
	}
	state.ensureToolbarDock()
	entries := state.toolbars.dock
	index := slices.IndexFunc(entries, func(other toolbarDockEntry) bool { return other.Group == entry.Group && other.Key == entry.Key })
	entries = slices.Delete(entries, index, index+1)
	entry.Zone = zone
	index = len(entries)
	if target, ok := toolbarDockIdentity(before); ok {
		if at := slices.IndexFunc(entries, func(other toolbarDockEntry) bool { return other.Group == target.Group && other.Key == target.Key }); at >= 0 {
			index = at
		}
	}
	state.toolbars.dock = slices.Insert(entries, index, entry)
	// Keep the settings editor's order consistent with direct manipulation.
	ordered := state.toolbars.customize(entry.Group)
	previous := slices.Clone(ordered)
	ordered = ordered[:0]
	for _, placed := range state.toolbars.dock {
		if placed.Group != entry.Group {
			continue
		}
		for _, preference := range previous {
			if preference.Key == placed.Key {
				ordered = append(ordered, preference)
				break
			}
		}
	}
	state.toolbars.layouts[entry.Group] = ordered
	state.toolbarDockChanged()
}

func (state *State) toolbarDockChanged() {
	// The layout entry is only a mirror of the Show latency preference; keep it
	// equal so the customize list and the saved layout never disagree.
	state.syncLatencyToolbarEntry()
	state.chrome.geometry.valid = false
	state.toolbarDock.sideScroll = [4]float32{}
	state.toolbarDock.bottomScroll = [2]float32{}
	state.resetTerminalToolRails()
	state.fitTerminals()
	state.fitSFTP(state.viewport)
	state.save()
}

// Keep a useful tab area; excess top actions remain available in the menu.
func (state *State) toolbarTopLimit() int {
	leading, trailing := app.TitlebarInsets()
	return max(2, int((state.viewport.Width-leading-trailing-240)/(barButtonSize+2)))
}

func (state *State) toolbarDockZoneFor(group toolbarGroup, key string) toolbarDockZone {
	for _, entry := range state.toolbars.dock {
		if entry.Group == group && entry.Key == key {
			return entry.Zone
		}
	}
	return state.defaultToolbarDockZone(group)
}

func (state *State) toolbarDockZoneLabel(zone toolbarDockZone) string {
	m := state.messages.Toolbars
	switch zone {
	case toolbarDockTop:
		return m.Top
	case toolbarDockSideTop:
		return m.SideTop
	case toolbarDockSideBottom:
		return m.SideBottom
	case toolbarDockLeftTop:
		return m.LeftTop
	case toolbarDockLeftBottom:
		return m.LeftBottom
	case toolbarDockBottomRight:
		return m.BottomRight
	default:
		return m.Bottom
	}
}

func (state *State) resetToolbarDockGroup(group toolbarGroup) {
	if group > toolbarTerminal || state.toolbars.dock == nil {
		return
	}
	state.toolbars.dock = slices.DeleteFunc(state.toolbars.dock, func(entry toolbarDockEntry) bool { return entry.Group == group })
	for _, entry := range toolbarDefaults[group] {
		state.toolbars.dock = append(state.toolbars.dock, toolbarDockEntry{group, entry.Key, state.defaultToolbarDockZone(group)})
	}
	defaults := true
	for _, entry := range state.toolbars.dock {
		defaults = defaults && entry.Zone == state.defaultToolbarDockZone(entry.Group)
	}
	if defaults {
		// A default position alone is insufficient: a different context may still
		// have a custom order. Compare that order before dropping the snapshot.
		for other := toolbarWindow; other <= toolbarTerminal; other++ {
			index := 0
			for _, entry := range state.toolbars.dock {
				if entry.Group != other {
					continue
				}
				defaults = defaults && state.toolbars.entries(other)[index].Key == entry.Key
				index++
			}
		}
		if defaults {
			state.toolbars.dock = nil
		}
	}
}

func (state *State) reorderToolbarDockGroup(group toolbarGroup) {
	if group > toolbarTerminal || state.toolbars.dock == nil {
		return
	}
	entries := state.toolbars.entries(group)
	rank := func(key string) int {
		return slices.IndexFunc(entries, func(entry toolbarEntry) bool { return entry.Key == key })
	}
	// Reorder this context within each zone, keeping other contexts in their slots.
	for i := range state.toolbars.dock {
		first := &state.toolbars.dock[i]
		if first.Group != group {
			continue
		}
		for j := i + 1; j < len(state.toolbars.dock); j++ {
			other := &state.toolbars.dock[j]
			if other.Group == group && other.Zone == first.Zone && rank(other.Key) < rank(first.Key) {
				*first, *other = *other, *first
			}
		}
	}
}
