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
		{"broadcast", hitBroadcast, iconRadio}, {"split-horizontal", hitPaneSplitHorizontal, iconColumns2},
		{"split-vertical", hitPaneSplitVertical, iconRows2}, {"close-pane", hitPaneClose, iconClose},
		{"resources", hitRemoteStats, iconActivity}, {"record", hitTerminalRecord, iconRecord},
		{"export-recording", hitTerminalRecordingExport, iconFilm}, {"export-log", hitConnExport, iconFileDown},
		{"files", hitSFTPOpen, iconFolderOpen}, {"file-transfer", hitSFTPDetach, iconTransfer},
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
			defaults[group] = append(defaults[group], toolbarEntry{Key: spec.key, Enabled: true})
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

func normalizeToolbar(group toolbarGroup, entries []toolbarEntry) []toolbarEntry {
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
		result = append(result, entry)
	}
	// New entries become available after an upgrade; existing hidden entries
	// remain hidden. Unknown/duplicate IDs cannot create phantom controls.
	for _, entry := range toolbarDefaults[group] {
		if !slices.ContainsFunc(result, func(other toolbarEntry) bool { return other.Key == entry.Key }) {
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

func (prefs *toolbarPreferences) restore(saved *savedToolbars) {
	*prefs = toolbarPreferences{}
	if saved == nil {
		return
	}
	prefs.layouts[toolbarWindow] = normalizeToolbar(toolbarWindow, saved.Window)
	prefs.layouts[toolbarTerminal] = normalizeToolbar(toolbarTerminal, saved.Terminal)
	prefs.layouts[toolbarSFTP] = normalizeToolbar(toolbarSFTP, saved.SFTP)
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
	case hitTerminalRecord:
		return m.TipRecordStart
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
