package workbench

import (
	"slices"

	"github.com/vivaui/viva/ui"
)

type toolbarGroup uint8

const (
	toolbarWindow toolbarGroup = iota
	toolbarTerminal
	toolbarSFTP
)

type toolbarEntry struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
	Side    string `json:"side,omitempty"`
	Zone    string `json:"zone,omitempty"`
}
type toolbarSpec struct {
	key  string
	id   ui.ID
	icon uint8
}

// Catalog order is visual left-to-right, including entries which only become
// available in a particular connection/split/local-files context.
var toolbarCatalog = [3][]toolbarSpec{
	toolbarWindow: {
		{"transfers", hitTransferCenter, iconTransfer}, {"assistant", hitAIToggle, iconBot},
		{"local-terminal", hitWindowLocalTerminal, iconTerminal}, {"commands", hitCommandPalette, iconSearch},
		{"theme", hitTheme, iconMoon}, {"github", hitGitHub, iconGitHub}, {"settings", hitWindowSettings, iconSettings},
	},
	toolbarTerminal: {
		{"connection-color", hitTerminalConnectionColor, iconPipette},
		{"latency", hitTerminalLatency, iconClock},
		{"close-pane", hitPaneClose, iconClose}, {"broadcast", hitBroadcast, iconRadio},
		{"split-horizontal", hitPaneSplitHorizontal, iconColumns2}, {"split-vertical", hitPaneSplitVertical, iconRows2},
		{"record", hitTerminalRecord, iconRecord}, {"export-recording", hitTerminalRecordingExport, iconFilm},
		{"save-output", hitTerminalSaveOutput, iconSave},
		{"export-log", hitConnExport, iconFileDown}, {"resources", hitRemoteStats, iconActivity},
		{"files", hitSFTPOpen, iconFolderOpen}, {"file-transfer", hitSFTPDetach, iconSFTPTransfer},
		{"remote-tools", hitRemoteToolsOpen, iconWrench},
	},
	toolbarSFTP: {
		{"upload-file", hitSFTPUpload, iconUpload}, {"upload-folder", hitSFTPUploadDirectory, iconFolderUp},
		{"new-folder", hitSFTPNewDirectory, iconFolderPlus}, {"new-link", hitSFTPNewLink, iconLink},
		{"local-remote", hitSFTPPaneToggle, iconHosts}, {"refresh", hitSFTPRefresh, iconRefresh},
		{"panel-side", hitSFTPPanelSide, iconColumns2}, {"close", hitSFTPClose, iconClose},
	},
}

var toolbarDefaults = func() (defaults [3][]toolbarEntry) {
	for group, catalog := range toolbarCatalog {
		for _, spec := range catalog {
			entry := toolbarEntry{Key: spec.key, Enabled: spec.key != "latency"}
			if toolbarGroup(group) == toolbarTerminal {
				zone := terminalToolbarZoneFromParts(terminalToolbarRight, terminalToolbarDockTop)
				entry.Side = zone.Side().String()
				entry.Zone = zone.String()
			}
			defaults[group] = append(defaults[group], entry)
		}
	}
	return
}()

type toolbarPreferences struct {
	layouts                     [3][]toolbarEntry
	summary                     [3]string
	summaryCounts, summarySizes [3]int
	summaryFormat               string
}
type savedToolbars struct {
	Window   []toolbarEntry `json:"window,omitempty"`
	Terminal []toolbarEntry `json:"terminal,omitempty"`
	SFTP     []toolbarEntry `json:"sftp,omitempty"`
}

func (prefs *toolbarPreferences) entries(group toolbarGroup) []toolbarEntry {
	if prefs.layouts[group] == nil {
		return toolbarDefaults[group]
	}
	return prefs.layouts[group]
}

func (state *State) toolbarVisible(group toolbarGroup, id ui.ID) bool {
	for _, entry := range state.toolbars.entries(group) {
		if spec, ok := toolbarSpecFor(group, entry.Key); ok && spec.id == id {
			return entry.Enabled
		}
	}
	return false
}

// windowBarButtons counts the window entries the bar shows. The bar lays its
// tabs out around them, so changing one reshapes the room the tabs get.
func (state *State) windowBarButtons() int {
	count := 0
	for _, entry := range state.toolbars.entries(toolbarWindow) {
		if !entry.Enabled {
			continue
		}
		if _, ok := toolbarSpecFor(toolbarWindow, entry.Key); ok {
			count++
		}
	}
	return count
}

func normalizeToolbar(group toolbarGroup, entries []toolbarEntry, fallbackSide terminalToolbarSide) []toolbarEntry {
	if entries == nil {
		return nil
	}
	result := make([]toolbarEntry, 0, len(toolbarCatalog[group]))
	for _, entry := range entries {
		if _, ok := toolbarSpecFor(group, entry.Key); !ok {
			continue
		}
		if slices.ContainsFunc(result, func(other toolbarEntry) bool { return other.Key == entry.Key }) {
			continue
		}
		if group == toolbarTerminal {
			zone := terminalToolbarZoneForEntry(entry, fallbackSide)
			entry.Side = zone.Side().String()
			entry.Zone = zone.String()
		} else {
			entry.Side = ""
			entry.Zone = ""
		}
		result = append(result, entry)
	}
	// New entries become available after an upgrade; existing hidden entries
	// remain hidden. Unknown/duplicate IDs cannot create phantom controls.
	for _, entry := range toolbarDefaults[group] {
		if !slices.ContainsFunc(result, func(other toolbarEntry) bool { return other.Key == entry.Key }) {
			if group == toolbarTerminal {
				entry.Side = fallbackSide.String()
				entry.Zone = terminalToolbarZoneFromParts(fallbackSide, terminalToolbarDockTop).String()
			}
			result = append(result, entry)
		}
	}
	return result
}

func toolbarSpecFor(group toolbarGroup, key string) (toolbarSpec, bool) {
	for _, spec := range toolbarCatalog[group] {
		if spec.key == key {
			return spec, true
		}
	}
	return toolbarSpec{}, false
}

func toolbarEntryEnabled(entries []toolbarEntry, key string) (bool, bool) {
	for _, entry := range entries {
		if entry.Key == key {
			return entry.Enabled, true
		}
	}
	return false, false
}

func (prefs *toolbarPreferences) setTerminalEntryEnabled(key string, enabled bool) bool {
	entries := prefs.entries(toolbarTerminal)
	current, ok := toolbarEntryEnabled(entries, key)
	if ok && current == enabled {
		return false
	}
	entries = prefs.customize(toolbarTerminal)
	for index := range entries {
		if entries[index].Key == key {
			entries[index].Enabled = enabled
			return true
		}
	}
	return false
}

func savedToolbarEntryEnabled(saved *savedToolbars, key string) (bool, bool) {
	if saved == nil {
		return false, false
	}
	return toolbarEntryEnabled(saved.Terminal, key)
}

func (prefs *toolbarPreferences) restore(saved *savedToolbars, terminalFallback terminalToolbarSide) {
	*prefs = toolbarPreferences{}
	if saved == nil {
		if terminalFallback == terminalToolbarLeft {
			prefs.layouts[toolbarTerminal] = terminalToolbarDefaultsOn(terminalFallback)
		}
		return
	}
	prefs.layouts[toolbarWindow] = normalizeToolbar(toolbarWindow, saved.Window, terminalToolbarRight)
	if saved.Terminal == nil && terminalFallback == terminalToolbarLeft {
		prefs.layouts[toolbarTerminal] = terminalToolbarDefaultsOn(terminalFallback)
	} else {
		prefs.layouts[toolbarTerminal] = normalizeToolbar(toolbarTerminal, saved.Terminal, terminalFallback)
	}
	prefs.layouts[toolbarSFTP] = normalizeToolbar(toolbarSFTP, saved.SFTP, terminalToolbarRight)
}

// restoreTerminalStatusEntries moves the two pre-list switches into ordinary
// terminal-toolbar entries. A state written after this layout existed already
// carries both keys, so those values win; older values are used only to seed a
// missing entry.
func (state *State) restoreTerminalStatusEntries(saved savedState) {
	colorSaved, colorExists := savedToolbarEntryEnabled(saved.Toolbars, "connection-color")
	latencySaved, latencyExists := savedToolbarEntryEnabled(saved.Toolbars, "latency")
	if !colorExists {
		state.toolbars.setTerminalEntryEnabled("connection-color", true)
	} else {
		state.toolbars.setTerminalEntryEnabled("connection-color", colorSaved)
	}
	if !latencyExists {
		state.toolbars.setTerminalEntryEnabled("latency", saved.TerminalShowLatency)
	} else {
		state.toolbars.setTerminalEntryEnabled("latency", latencySaved)
	}
	// Before these items existed, both status chips were painted above the
	// action rail. Keep that visual order for migrated states instead of
	// appending newly discovered entries after every action.
	if saved.Toolbars != nil && saved.Toolbars.Terminal != nil && !latencyExists {
		state.toolbars.prependTerminalEntry("latency")
	}
	if saved.Toolbars != nil && saved.Toolbars.Terminal != nil && !colorExists {
		state.toolbars.prependTerminalEntry("connection-color")
	}
	state.syncTerminalStatusEntries()
}

func (prefs *toolbarPreferences) prependTerminalEntry(key string) {
	entries := prefs.customize(toolbarTerminal)
	index := -1
	for at := range entries {
		if entries[at].Key == key {
			index = at
			break
		}
	}
	if index <= 0 {
		return
	}
	entry := entries[index]
	copy(entries[1:index+1], entries[:index])
	entries[0] = entry
}

func (state *State) syncTerminalStatusEntries() {
	entries := state.toolbars.entries(toolbarTerminal)
	if enabled, ok := toolbarEntryEnabled(entries, "connection-color"); ok {
		state.terminalHideConnectionColor = !enabled
	}
	if enabled, ok := toolbarEntryEnabled(entries, "latency"); ok {
		state.terminalShowLatency = enabled
	}
}

func terminalToolbarDefaultsOn(side terminalToolbarSide) []toolbarEntry {
	entries := slices.Clone(toolbarDefaults[toolbarTerminal])
	for index := range entries {
		entries[index].Side = side.String()
		entries[index].Zone = terminalToolbarZoneFromParts(side, terminalToolbarDockTop).String()
	}
	return entries
}

// restoreLegacyTerminalToolbarDocks upgrades the short-lived whole-side dock
// representation into the per-control four-zone model.
func (state *State) restoreLegacyTerminalToolbarDocks(saved savedState) {
	if saved.TerminalToolbarLeftDock == "" && saved.TerminalToolbarRightDock == "" {
		return
	}
	savedZones := make(map[string]terminalToolbarZone, len(toolbarCatalog[toolbarTerminal]))
	if saved.Toolbars != nil {
		for _, entry := range saved.Toolbars.Terminal {
			if zone, ok := terminalToolbarZoneFromString(entry.Zone); ok {
				savedZones[entry.Key] = zone
			}
		}
	}
	docks := [2]terminalToolbarDock{
		terminalToolbarLeft:  terminalToolbarDockFromString(saved.TerminalToolbarLeftDock),
		terminalToolbarRight: terminalToolbarDockFromString(saved.TerminalToolbarRightDock),
	}
	entries := state.toolbars.customize(toolbarTerminal)
	for index := range entries {
		// A per-control zone is the current representation and always wins.
		// The whole-side dock fields are consulted only for entries written
		// before that representation existed, or for newly introduced entries.
		zone, hasCurrentZone := savedZones[entries[index].Key]
		if hasCurrentZone {
			continue
		}
		side := terminalToolbarZoneForEntry(entries[index], terminalToolbarRight).Side()
		zone = terminalToolbarZoneFromParts(side, docks[side])
		entries[index].Side = side.String()
		entries[index].Zone = zone.String()
	}
	state.syncTerminalStatusEntries()
}

func (prefs *toolbarPreferences) snapshot() *savedToolbars {
	if prefs.layouts[0] == nil && prefs.layouts[1] == nil && prefs.layouts[2] == nil {
		return nil
	}
	return &savedToolbars{Window: slices.Clone(prefs.layouts[0]), Terminal: slices.Clone(prefs.layouts[1]), SFTP: slices.Clone(prefs.layouts[2])}
}

func (prefs *toolbarPreferences) customize(group toolbarGroup) []toolbarEntry {
	if prefs.layouts[group] == nil {
		prefs.layouts[group] = slices.Clone(toolbarDefaults[group])
	}
	return prefs.layouts[group]
}

func (state *State) toolbarLabel(id ui.ID) string {
	m := state.messages
	switch id {
	case hitTransferCenter:
		return m.Transfers.Title
	case hitAIToggle:
		return m.AIPanel.ToggleTip
	case hitTerminalConnectionColor:
		return m.Toolbars.ConnectionColor
	case hitTerminalLatency:
		return m.Toolbars.Latency
	case hitWindowLocalTerminal:
		return m.LocalTerminal
	case hitCommandPalette:
		return m.CommandPaletteTitle
	case hitTheme:
		return m.SettingsAppearance
	case hitGitHub:
		return m.LabelGitHub
	case hitWindowSettings:
		return m.NavSettings
	case hitBroadcast:
		return m.Tools.BroadcastStart
	case hitPaneSplitHorizontal:
		return m.PaneSplitHorizontal
	case hitPaneSplitVertical:
		return m.PaneSplitVertical
	case hitPaneClose:
		return m.PaneClose
	case hitRemoteStats:
		return m.TipStatsStart
	case hitRemoteToolsOpen:
		return m.Tools.RemoteMenu
	case hitTerminalRecord:
		return m.TipRecordStart
	case hitTerminalSaveOutput:
		return m.Tools.MenuSaveOutput
	case hitTerminalRecordingExport:
		return m.RecordingExport
	case hitConnExport:
		return m.ConnExportLog
	case hitSFTPOpen:
		return m.TipFiles
	case hitSFTPDetach:
		return m.SFTPWindow.Open
	case hitSFTPUpload:
		return m.SFTPUpload
	case hitSFTPUploadDirectory:
		return m.SFTPUploadDirectory
	case hitSFTPNewDirectory:
		return m.SFTPNewDirectory
	case hitSFTPNewLink:
		return m.Tools.TipNewLink
	case hitSFTPPaneToggle:
		return m.Toolbars.LocalRemote
	case hitSFTPRefresh:
		return m.SFTPRefresh
	case hitSFTPPanelSide:
		return m.Toolbars.PanelSide
	case hitSFTPClose:
		return m.TipSFTPClose
	}
	return ""
}

func (state *State) orderToolbarTools(group toolbarGroup, tools []barTool, reverse bool) []barTool {
	var original [maxBarTools]barTool
	copy(original[:], tools)
	count := len(tools)
	tools = tools[:0]
	entries := state.toolbars.entries(group)
	for pos := range entries {
		i := pos
		if reverse {
			i = len(entries) - 1 - pos
		}
		if !entries[i].Enabled {
			continue
		}
		spec, _ := toolbarSpecFor(group, entries[i].Key)
		for j := 0; j < count; j++ {
			if original[j].id == spec.id {
				tools = append(tools, original[j])
				break
			}
		}
	}
	return tools
}
