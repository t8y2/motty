package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"motty/internal/i18n"
	"motty/internal/sessionlog"
)

// The session journal's rows in Settings > Security. Their hit ids live in the
// settings scope, so the existing click dispatcher finds them; declaring them
// here keeps the feature in its own files.
var (
	hitSessionLogsSettingsToggle   = tree.ID(settingsHits, "settings-session-logs-toggle")
	hitSessionLogsSettingsClear    = tree.ID(settingsHits, "settings-session-logs-clear")
	hitSessionLogsSettingsClearAll = tree.ID(settingsHits, "settings-session-logs-clear-all")
)

// sessionLogsSettings caches what Settings > Security shows about the journal:
// how many records it holds and what it occupies. The counts are read off the
// paint path and the text is prepared when they arrive, so painting only copies
// prepared strings.
type sessionLogsSettings struct {
	stats       sessionlog.Stats
	hasStats    bool
	requested   bool
	unavailable bool
	text        string
	language    i18n.Language
	// pageVisible tracks whether the security page was on screen at the last
	// Wake, so re-entering it refreshes the footprint.
	pageVisible bool
}

// paintSessionLogSettingsGroup declares the session-journal group of Settings >
// Security. It is painted even when this computer has no password vault.
func (state *State) paintSessionLogSettingsGroup(page *settingsPage) {
	m := state.messages.SessionLogs
	actions := []components.ButtonAction{
		{ID: hitSessionLogsSettingsClear, Label: m.ClearUnfavorited, Variant: components.ButtonOutline},
		{ID: hitSessionLogsSettingsClearAll, Label: m.ClearAll, Variant: components.ButtonOutline},
	}
	page.group(m.SettingsGroup, func() {
		page.toggleRow(m.SettingsCaptureRow, m.SettingsCaptureDescription, !state.sessionLogsOff, hitSessionLogsSettingsToggle)
		page.actionRow(m.SettingsStorageRow, state.sessionLogs.settings.text, state.sessionLogs.store == nil, actions...)
	})
}

// prepareSessionLogsSettings prepares the footprint line from cached counts,
// and re-prepares it when the language changed while the page was open.
func (state *State) prepareSessionLogsSettings() {
	settings := &state.sessionLogs.settings
	m := state.messages.SessionLogs
	switch {
	case state.sessionLogs.store == nil:
		settings.text = m.SettingsStorageUnavailable
	case settings.unavailable:
		settings.text = m.SettingsStorageUnavailable
	case settings.hasStats:
		settings.text = fmt.Sprintf(m.SettingsStorageFmt, settings.stats.Records, formatBytes(settings.stats.Bytes))
	default:
		settings.text = m.Loading
	}
	settings.language = state.language
}

// pumpSessionLogsSettings asks for the journal footprint the first time the
// security page is shown and again each time it is re-entered. Wake calls it;
// Paint never starts work.
func (state *State) pumpSessionLogsSettings() {
	settings := &state.sessionLogs.settings
	visible := state.activeSection == sectionSettings && state.settingsCategory == settingsSecurity
	if !visible {
		settings.pageVisible = false
		return
	}
	if !settings.pageVisible {
		settings.pageVisible = true
		settings.requested = false
	}
	if settings.language != state.language {
		state.prepareSessionLogsSettings()
	}
	if settings.requested {
		return
	}
	settings.requested = true
	store := state.sessionLogs.store
	if store == nil {
		settings.unavailable, settings.hasStats = true, false
		state.prepareSessionLogsSettings()
		return
	}
	invalidate := state.options.Invalidate
	go func() {
		stats, err := store.Stats()
		state.aiPostWithInvalidator(func(state *State) { state.applySessionLogsStats(stats, err) }, invalidate)
	}()
}

// applySessionLogsStats stores a completed footprint read on the window thread.
func (state *State) applySessionLogsStats(stats sessionlog.Stats, err error) {
	settings := &state.sessionLogs.settings
	if err != nil {
		settings.unavailable, settings.hasStats = true, false
		state.reportSessionLogProblem("stats", err)
	} else {
		settings.stats, settings.hasStats, settings.unavailable = stats, true, false
	}
	state.prepareSessionLogsSettings()
}

// toggleSessionLogs flips the device-local capture switch and saves it with the
// rest of the state.
func (state *State) toggleSessionLogs() bool {
	state.setSessionLogsOff(!state.sessionLogsOff)
	return true
}

// setSessionLogsOff applies the capture switch to this window and, through the
// group, to the other windows of the process. It never touches a connection.
func (state *State) setSessionLogsOff(off bool) {
	if state.sessionLogsOff == off {
		return
	}
	state.sessionLogsOff = off
	enabled := !off
	if state.options.Group != nil {
		state.options.Group.broadcastSessionLogsEnabled(enabled)
	} else {
		state.setSessionLogsEnabled(enabled)
	}
	state.sessionLogs.settings.requested = false
}

// clearSessionLogsAndNotify clears this journal and, once the write has landed,
// refreshes the settings footprint and the page. It waits only on a goroutine:
// the dialog and the paint path stay non-blocking.
func (state *State) clearSessionLogsAndNotify(includeFavorites bool) {
	queue := state.sessionLogs.queue
	if queue == nil {
		return
	}
	ack := queue.Submit(sessionlog.Operation{Kind: sessionlog.OpClear, IncludeFavorites: includeFavorites})
	invalidate := state.options.Invalidate
	go func() {
		err := <-ack
		state.aiPostWithInvalidator(func(state *State) {
			state.sessionLogs.settings.requested = false
			if err != nil {
				state.reportSessionLogProblem("clear", err)
				state.sessionLogs.view.statusText = state.messages.SessionLogs.WriteFailed
				state.sessionLogs.view.statusDanger = true
				state.showActionError(state.messages.SessionLogs.Title, state.messages.SessionLogs.WriteFailed)
			} else {
				if includeFavorites {
					state.sessionLogs.view.statusText = state.messages.SessionLogs.ClearedAll
				} else {
					state.sessionLogs.view.statusText = state.messages.SessionLogs.ClearedUnfavorited
				}
				state.sessionLogs.view.statusDanger = false
				state.sessionLogs.view.selected = ""
				state.sessionLogs.view.detailOpen = false
				state.sessionLogs.view.focusPending = hitSessionLogsSearch
			}
			if state.activeSection == sectionSessionLogs {
				state.sessionLogs.view.dirty = true
			}
		}, invalidate)
	}()
}
