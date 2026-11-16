package workbench

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
)

type sftpColumnKey uint8

const (
	sftpColumnName sftpColumnKey = iota
	sftpColumnSize
	sftpColumnModified
	sftpColumnCount
)

type sftpColumnEntry struct {
	Key     sftpColumnKey
	Enabled bool
}

// sftpColumnPreferences keeps the file list's visual column order and
// visibility together. The name column is always first and visible.
type sftpColumnPreferences struct {
	entries [sftpColumnCount]sftpColumnEntry
}

func defaultSFTPColumnPreferences() sftpColumnPreferences {
	return sftpColumnPreferences{entries: [...]sftpColumnEntry{
		{Key: sftpColumnName, Enabled: true},
		{Key: sftpColumnSize, Enabled: true},
		{Key: sftpColumnModified, Enabled: true},
	}}
}

func (prefs *sftpColumnPreferences) reset() {
	*prefs = defaultSFTPColumnPreferences()
}

func (prefs *sftpColumnPreferences) enabled(key sftpColumnKey) bool {
	if key == sftpColumnName {
		return true
	}
	for _, entry := range prefs.entries {
		if entry.Key == key {
			return entry.Enabled
		}
	}
	return false
}

func (prefs *sftpColumnPreferences) setEnabled(key sftpColumnKey, enabled bool) bool {
	if key == sftpColumnName {
		return false
	}
	for index := range prefs.entries {
		if prefs.entries[index].Key != key {
			continue
		}
		if prefs.entries[index].Enabled == enabled {
			return false
		}
		prefs.entries[index].Enabled = enabled
		return true
	}
	return false
}

// moveOptional applies a move from the dialog's complete row order. The name
// row is disabled in the UI; clamping a drop above it keeps an optional column
// at the first position available to it.
func (prefs *sftpColumnPreferences) moveOptional(from, to int) bool {
	if to == 0 {
		to = 1
	}
	if from < 1 || from >= int(sftpColumnCount) || to < 1 || to >= int(sftpColumnCount) || from == to {
		return false
	}
	entry := prefs.entries[from]
	if from < to {
		copy(prefs.entries[from:to], prefs.entries[from+1:to+1])
	} else {
		copy(prefs.entries[to+1:from+1], prefs.entries[to:from])
	}
	prefs.entries[to] = entry
	return true
}

func (prefs sftpColumnPreferences) visibleOptional() int {
	count := 0
	for _, entry := range prefs.entries {
		if entry.Key != sftpColumnName && entry.Enabled {
			count++
		}
	}
	return count
}

// String is the persisted form: name:1,size:0,modified:1.
func (prefs sftpColumnPreferences) String() string {
	var values [sftpColumnCount]string
	for index, entry := range prefs.entries {
		enabled := "0"
		if entry.Enabled {
			enabled = "1"
		}
		values[index] = sftpColumnKeyName(entry.Key) + ":" + enabled
	}
	return strings.Join(values[:], ",")
}

func sftpColumnKeyName(key sftpColumnKey) string {
	switch key {
	case sftpColumnName:
		return "name"
	case sftpColumnSize:
		return "size"
	case sftpColumnModified:
		return "modified"
	}
	return ""
}

func sftpColumnKeyFromName(value string) (sftpColumnKey, bool) {
	switch value {
	case "name":
		return sftpColumnName, true
	case "size":
		return sftpColumnSize, true
	case "modified":
		return sftpColumnModified, true
	}
	return 0, false
}

// restoreSFTPColumnPreferences upgrades the previous comma-separated form.
// Tokens without an explicit state were visible. Unknown, duplicate and missing
// optional columns are normalized without ever changing the name column.
func restoreSFTPColumnPreferences(value string) sftpColumnPreferences {
	if value == "" {
		return defaultSFTPColumnPreferences()
	}
	var result sftpColumnPreferences
	seen := [sftpColumnCount]bool{}
	recognized := 0
	result.entries[0] = sftpColumnEntry{Key: sftpColumnName, Enabled: true}
	count := 1
	for _, token := range strings.Split(value, ",") {
		name, state, hasState := strings.Cut(strings.TrimSpace(token), ":")
		key, ok := sftpColumnKeyFromName(name)
		if ok {
			recognized++
		}
		if !ok || seen[key] {
			continue
		}
		if key == sftpColumnName {
			seen[key] = true
			continue
		}
		enabled := true
		if hasState {
			parsed, err := strconv.ParseBool(strings.TrimSpace(state))
			if err != nil {
				continue
			}
			enabled = parsed
		}
		seen[key] = true
		result.entries[count] = sftpColumnEntry{Key: key, Enabled: enabled}
		count++
	}
	// A value with no recognizable token at all is damaged rather than an empty
	// choice; restoring the default columns reads better than hiding every
	// optional column.
	if recognized == 0 {
		return defaultSFTPColumnPreferences()
	}
	for _, key := range [...]sftpColumnKey{sftpColumnSize, sftpColumnModified} {
		if seen[key] {
			continue
		}
		result.entries[count] = sftpColumnEntry{Key: key}
		count++
	}
	return result
}

// sftpFilesSettingsCache keeps descriptions and button actions stable across
// settings repaints. The settings page has a zero-allocation contract once its
// slices and text caches are warm.
type sftpFilesSettingsCache struct {
	columnsFormat       string
	columnsVisible      int
	columnsTotal        int
	columnsSummary      string
	downloadDirectory   string
	downloadCurrentFmt  string
	downloadAskDesc     string
	downloadChooseLabel string
	downloadAskLabel    string
	downloadDescription string
	downloadActions     [2]components.ButtonAction
	downloadActionCount int
}

func (state *State) sftpFilesColumnsSummary() string {
	prefs, cache, m := &state.sftpColumns, &state.sftpFilesSettings, state.messages.Tools
	visible, total := prefs.visibleOptional(), int(sftpColumnCount)-1
	if cache.columnsFormat != m.FilesColumnsEnabledFmt || cache.columnsVisible != visible || cache.columnsTotal != total {
		cache.columnsFormat = m.FilesColumnsEnabledFmt
		cache.columnsVisible, cache.columnsTotal = visible, total
		cache.columnsSummary = fmt.Sprintf(m.FilesColumnsEnabledFmt, visible, total)
	}
	return cache.columnsSummary
}

func (state *State) sftpFilesDownloadSetting() (string, []components.ButtonAction) {
	cache, m := &state.sftpFilesSettings, state.messages.Tools
	directory := state.sftpDownloadDirectory
	if cache.downloadDirectory != directory ||
		cache.downloadCurrentFmt != m.FilesDownloadCurrentFmt ||
		cache.downloadAskDesc != m.FilesDownloadAskDescription ||
		cache.downloadChooseLabel != m.FilesDownloadChoose ||
		cache.downloadAskLabel != m.FilesDownloadAsk {
		cache.downloadDirectory = directory
		cache.downloadCurrentFmt = m.FilesDownloadCurrentFmt
		cache.downloadAskDesc = m.FilesDownloadAskDescription
		cache.downloadChooseLabel = m.FilesDownloadChoose
		cache.downloadAskLabel = m.FilesDownloadAsk
		cache.downloadDescription = m.FilesDownloadAskDescription
		cache.downloadActions[0] = components.ButtonAction{
			ID: hitSettingsDownloadDirectory, Label: m.FilesDownloadChoose, Variant: components.ButtonOutline,
		}
		cache.downloadActionCount = 1
		if directory != "" {
			cache.downloadDescription = fmt.Sprintf(m.FilesDownloadCurrentFmt, directory)
			cache.downloadActions[1] = components.ButtonAction{
				ID: hitSettingsDownloadAsk, Label: m.FilesDownloadAsk, Variant: components.ButtonGhost,
			}
			cache.downloadActionCount = 2
		}
	}
	return cache.downloadDescription, cache.downloadActions[:cache.downloadActionCount]
}
