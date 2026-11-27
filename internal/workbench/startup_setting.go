package workbench

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"motty/internal/localshell"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// The launch row of the connection settings: what opening the app connects,
// chosen in one menu — a local terminal and the saved hosts.
var (
	hitSettingsStartupEnable = tree.ID(settingsHits, "settings-startup-enable")
	hitSettingsStartupHosts  = tree.ID(settingsHits, "settings-startup-hosts")
	hitSettingsStartupReuse  = tree.ID(settingsHits, "settings-startup-reuse")
)

// cleanStartupHosts keeps the catalog ids that name a host, in order, without
// repeating one.
func cleanStartupHosts(ids []string) []string {
	kept := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" && !slices.Contains(kept, id) {
			kept = append(kept, id)
		}
	}
	return kept
}

// startupHostSelected says whether a host opens at launch.
func (state *State) startupHostSelected(host Host) bool {
	return host.CatalogID != "" && slices.Contains(state.startupHosts, host.CatalogID)
}

// resolveStartupShell names the shell a launch would open: the detected shell
// whose program is startupShell, or nothing when the list has not reached it.
func (state *State) resolveStartupShell() localshell.Choice {
	if !state.localShellMenu.loaded {
		state.refreshLocalShells()
	}
	for _, choice := range state.localShellMenu.choices {
		if strings.EqualFold(choice.Path, state.startupShell) {
			return choice
		}
	}
	return localshell.Choice{}
}

// startupSummary describes the choice, for the select's value.
func (state *State) startupSummary() string {
	m := state.messages
	if !state.startupEnabled {
		return m.SettingsStartupDisabled
	}
	var parts []string
	switch {
	case state.startupLocal:
		parts = append(parts, m.LocalTerminal)
	case state.startupShell != "":
		if choice := state.resolveStartupShell(); choice.Name != "" {
			parts = append(parts, choice.Name)
		} else {
			parts = append(parts, filepath.Base(state.startupShell))
		}
	}
	ids := cleanStartupHosts(state.startupHosts)
	switch len(ids) {
	case 0:
	case 1:
		if host, ok := state.hostByCatalogID(ids[0]); ok {
			parts = append(parts, host.Label)
		} else {
			parts = append(parts, fmt.Sprintf(m.SettingsStartupHostsCount, 1))
		}
	default:
		parts = append(parts, fmt.Sprintf(m.SettingsStartupHostsCount, len(ids)))
	}
	if len(parts) == 0 {
		return m.SettingsStartupNone
	}
	return strings.Join(parts, m.SettingsStartupJoiner)
}

// startupHostsSettingVisible is the select's guard: the chooser only lives on
// the connection page, and a search draws the page itself, not the selects.
func (state *State) startupHostsSettingVisible() bool {
	return state.activeSection == sectionSettings && state.settingsCategory == settingsConnection && !state.settingsSearching()
}

// paintStartupSettings declares the launch rows: the feature switch, the select
// that opens the menu of what a launch connects, and the reuse switch.
func (state *State) paintStartupSettings(page *settingsPage) {
	m := state.messages
	page.toggleRow(m.SettingsStartupEnable, m.SettingsStartupEnableDescription, state.startupEnabled, hitSettingsStartupEnable)
	state.declareStartupPicker(page)
	page.toggleRow(m.SettingsStartupReuse, m.SettingsStartupReuseDescription, state.startupReuse, hitSettingsStartupReuse)
}

// declareStartupPicker declares the launch select: its trigger in the row, and the menu of
// what a launch connects. A choice toggles and keeps the menu open, a press anywhere else
// closes it and saves.
func (state *State) declareStartupPicker(page *settingsPage) {
	picker, m, c := &state.startupPicker, state.messages, page.c
	if picker.open && (!state.startupHostsSettingVisible() || state.modal != modalNone || !state.startupEnabled) {
		picker.open = false
	}
	if !picker.open && picker.shown {
		picker.shown = false
		state.save()
	}
	controlWidth := max(min(float32(300), max(float32(190), page.width*0.40)), 0)
	controlWidth = min(controlWidth, max(page.width-60, 0))
	page.lineWith(m.SettingsStartupRow, m.SettingsStartupDescription, controlWidth, func() {
		summary := state.startupSummary()
		trigger := ui.Button(c, summary).Outline().Icon(lucideIcons[iconChevronDown]).Width(controlWidth).
			HitWith(hitSettingsStartupHosts, m.SettingsStartupRow, gfx.HitOptions{Role: gfx.RoleCombobox, Value: summary, Expanded: picker.open})
		if !picker.open {
			return
		}
		picker.shown = true
		ui.PopupLayer(c, popupLayer)
		ui.Menu(c, trigger, &picker.open, func(items *ui.MenuItems) {
			items.RowHeight(contextMenuRow)
			choose := func(item ui.MenuItem, toggle func()) {
				if item.Clicked() {
					// A choice holds while the menu stays open.
					c.After(func() { toggle(); picker.open = true; state.save() })
				}
			}
			choose(items.Item(m.LocalTerminal).Checked(state.startupLocal), func() {
				state.startupLocal = !state.startupLocal
				if state.startupLocal {
					state.startupShell = "" // the default and a picked shell exclude each other
				}
			})
			for _, shell := range state.localShellMenu.choices {
				choose(items.Item(shell.Name).Checked(strings.EqualFold(shell.Path, state.startupShell)), func() {
					if strings.EqualFold(shell.Path, state.startupShell) {
						state.startupShell = ""
					} else {
						state.startupShell, state.startupLocal = shell.Path, false
					}
				})
			}
			for _, host := range state.hosts {
				choose(items.Item(host.Label).Checked(state.startupHostSelected(host)), func() {
					if state.startupHostSelected(host) {
						state.startupHosts = slices.DeleteFunc(state.startupHosts, func(candidate string) bool { return candidate == host.CatalogID })
					} else if host.CatalogID != "" {
						state.startupHosts = cleanStartupHosts(append(state.startupHosts, host.CatalogID))
					}
				})
			}
		})
	})
}

// clickStartupHosts works the chooser: the trigger opens it, an item toggles
// its choice and keeps the menu open, a press anywhere else closes it.
func (state *State) clickStartupHosts(id gfx.ID) bool {
	if !state.startupHostsSettingVisible() {
		return false
	}
	picker := &state.startupPicker
	switch id {
	case hitSettingsStartupEnable:
		state.startupEnabled = !state.startupEnabled
		state.save()
		return true
	case hitSettingsStartupHosts:
		if !state.startupEnabled {
			return true // the feature is off: the chooser stays closed
		}
		if !picker.open {
			state.refreshLocalShells() // the shells of this machine, found again like the picker does
		}
		picker.open = !picker.open
		return true
	case hitSettingsStartupReuse:
		state.startupReuse = !state.startupReuse
		state.save()
		return true
	}
	return false
}

// startupWanted reports whether the first Wake has anything to open: the
// restored tabs, the restored local shells (which never wait on the restore
// policy), or the launch choice — a local terminal, a picked shell or hosts. A launch
// with only a picked shell counts.
func (state *State) startupWanted() bool {
	return state.restoreConnect != restoreConnectOff ||
		state.hasRestoredLocalSession() ||
		(state.startupEnabled && (state.startupLocal || state.startupShell != "" || len(state.startupHosts) > 0))
}

// startupSession finds a tab the launch choices can reuse. For a local
// terminal the tab's own shell must match, not just the host.
func (state *State) startupSession(hostID uint32, shell string) *Session {
	if state.startupReuse {
		for i := range state.sessions {
			s := &state.sessions[i]
			if s.HostID == hostID && (hostID != localHostID || strings.EqualFold(s.host.localShell, shell)) {
				return s
			}
		}
	}
	return nil
}

// PrepareInitialLocalSessions gives local tabs their input surface before the
// first paint. The first Wake resolves their shells and opens remote sessions.
func (state *State) PrepareInitialLocalSessions() {
	if !state.restorePending {
		return
	}
	state.preparingInitialLocal = true
	defer func() { state.preparingInitialLocal = false }()
	state.startRestoredLocalSessions()
	if !state.startupEnabled || (!state.startupLocal && state.startupShell == "") {
		return
	}
	shell := state.startupShell
	shown := state.startupSession(localHostID, shell)
	if shown == nil {
		state.pendingWorkspace, state.pendingPane = 0, 0
		if shell != "" {
			// The window can show the saved name until the first Wake resolves it.
			shown = state.openLocalTerminalWith(localshell.Choice{Path: shell, Name: filepath.Base(shell)})
			if shown != nil {
				shown.conn.(*localConn).pickedAtStartup = true
			}
		} else {
			shown = state.openLocalTerminal()
		}
	}
	if shown == nil {
		return
	}
	state.startupLocalPrepared = shown.ID
	state.activeSection = sectionTerminal
	state.ActivatePane(shown.ID)
}

// Resolve a prepared tab without replacing its terminal: the native input
// method may already have an active composition on that screen.
func (state *State) resolvePreparedLocalSessions() {
	for i := range state.sessions {
		s := &state.sessions[i]
		pending, ok := s.conn.(*localConn)
		if !ok || !pending.prepareOnWake {
			continue
		}
		if pending.pickedAtStartup {
			choice := state.resolveStartupShell()
			s.host.localShell = choice.Path
			s.host.Label = state.messages.LocalTerminalTitle
			if choice.Path == "" {
				// Text entered for the named program must not go to its fallback.
				s.term.startupText = nil
				s.term.preedit = app.TextState{}
			} else {
				s.host.Label = choice.Name
			}
			if !s.customTitle {
				state.renameSession(s, s.host.Label)
				state.rebuildTabOptions()
			}
		}
		conn := state.localConnection(s, s.host)
		conn.backgroundShell = true
		s.conn = conn
	}
}

// openStartupSessions opens what the settings ask a launch to open: a local
// terminal, then the chosen hosts. Like the restored tabs' dialing, it runs
// once, from the first Wake, where the window exists and the catalog is loaded.
// A host that has left the catalog is skipped and dropped from the choice. With
// reuse on, a terminal or host that is already open — one the restore brought
// back, say — is switched to instead of opened again.
func (state *State) openStartupSessions() {
	if !state.startupEnabled || (!state.startupLocal && state.startupShell == "" && len(state.startupHosts) == 0) {
		return
	}
	var shown *Session
	preparedLocal := state.startupLocalPrepared != 0
	if state.startupLocal || state.startupShell != "" {
		if state.startupLocalPrepared != 0 {
			shown, _ = state.sessionByIDPointer(state.startupLocalPrepared)
			state.startupLocalPrepared = 0
		} else {
			shell := state.startupShell
			if existing := state.startupSession(localHostID, shell); existing != nil {
				shown = existing
			} else {
				state.pendingWorkspace, state.pendingPane = 0, 0
				if shell != "" {
					shown = state.openLocalTerminalWith(state.resolveStartupShell())
				} else {
					shown = state.openLocalTerminal()
				}
			}
		}
	}
	kept := state.startupHosts[:0]
	for _, id := range state.startupHosts {
		host, ok := state.hostByCatalogID(id)
		if !ok {
			continue
		}
		kept = append(kept, id)
		if existing := state.startupSession(host.ID, ""); existing != nil {
			shown = existing
			continue
		}
		state.pendingWorkspace, state.pendingPane = 0, 0
		shown = state.newHostSession(host)
	}
	state.startupHosts = kept
	if shown != nil {
		state.setActiveSection(sectionTerminal)
		state.ActivatePane(shown.ID)
		// createHostSession deliberately skips persistence while the first
		// terminal is prepared before OpenWindow. Save after that window has
		// presented its first frame, keeping startup I/O off the capture path.
		if preparedLocal {
			state.save()
		}
	}
}
