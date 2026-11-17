package workbench

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"motty/internal/localshell"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// The launch row of the connection settings: what opening the app connects,
// chosen in one menu — a local terminal and the saved hosts.
var (
	hitSettingsStartupEnable  = tree.ID(settingsHits, "settings-startup-enable")
	hitSettingsStartupHosts   = tree.ID(settingsHits, "settings-startup-hosts")
	hitSettingsStartupDismiss = tree.ID(settingsHits, "settings-startup-dismiss")
	hitSettingsStartupLocal   = tree.ID(settingsHits, "settings-startup-local")
	// hitSettingsStartupShells lists this machine's detected shells by place:
	// picking one opens that shell at a launch instead of the default.
	hitSettingsStartupShells   = tree.Sub(settingsHits, tree.Name("settings-startup-shell"))
	hitSettingsStartupReuse    = tree.ID(settingsHits, "settings-startup-reuse")
	hitSettingsStartupHostList = tree.Sub(settingsHits, tree.Name("settings-startup-host")) // + the row's place
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
	page.add(settingsControl{kind: controlStartupHosts, slot: page.rowFrame(m.SettingsStartupRow, m.SettingsStartupDescription)})
	page.toggleRow(m.SettingsStartupReuse, m.SettingsStartupReuseDescription, state.startupReuse, hitSettingsStartupReuse)
}

// paintStartupHostSelect paints the launch select in the menu pass, outside the
// settings page's clip, so its popup can leave the page.
func (state *State) paintStartupHostSelect(canvas *gfx.Canvas) {
	picker, m := &state.startupPicker, state.messages
	bounds := picker.bounds
	if !state.startupHostsSettingVisible() || state.modal != modalNone || bounds.Width <= 0 || bounds.Y < windowBarHeight || bounds.Y+bounds.Height > state.viewport.Height {
		picker.open = false
		return
	}
	below := max(state.viewport.Height-8-bounds.Y-bounds.Height-4, 0)
	above := max(bounds.Y-windowBarHeight-8, 0)
	rows := 1 + len(state.localShellMenu.choices) + len(state.hosts) // the local terminal, the shells, the hosts
	height := min(float32(8+rows*32), max(above, below))
	content := gfx.Rect{X: bounds.X, Y: bounds.Y + bounds.Height + 4, Width: bounds.Width, Height: height}
	if below < height {
		content.Y = bounds.Y - height - 4
	}
	scroll := state.selectScroll(picker.open, hitSettingsStartupDismiss)
	control := components.BeginSelect(canvas, state.Theme(), bounds, components.SelectOptions{
		TriggerID: hitSettingsStartupHosts, Label: m.SettingsStartupRow, Value: state.startupSummary(),
		Open: picker.open, Content: content, Layer: popupLayer,
		Scope: hitSettingsStartupDismiss, DismissID: hitSettingsStartupDismiss, Viewport: state.windowRect(), Scroll: scroll,
	})
	defer control.End()
	if picker.open {
		state.menuPlaced(control.ContentBounds())
		control.AddItem(components.SelectItem{ID: hitSettingsStartupLocal, Label: m.LocalTerminal, Selected: state.startupLocal})
		for index, choice := range state.localShellMenu.choices {
			control.AddItem(components.SelectItem{ID: hitSettingsStartupShells.At(index), Label: choice.Name, Selected: strings.EqualFold(choice.Path, state.startupShell)})
		}
		for index, host := range state.hosts {
			control.AddItem(components.SelectItem{ID: hitSettingsStartupHostList.At(index), Label: host.Label, Selected: state.startupHostSelected(host)})
		}
	}
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
	case hitSettingsStartupDismiss:
		if picker.open {
			picker.open = false
			state.save()
		}
		return true
	case hitSettingsStartupLocal:
		state.startupLocal = !state.startupLocal
		if state.startupLocal {
			state.startupShell = "" // the default and a picked shell exclude each other
		}
		state.save() // the choice holds while the menu stays open
		return true
	case hitSettingsStartupReuse:
		state.startupReuse = !state.startupReuse
		state.save()
		return true
	}
	if index, ok := id.In(hitSettingsStartupShells); ok && index >= 0 && index < len(state.localShellMenu.choices) {
		choice := state.localShellMenu.choices[index]
		if strings.EqualFold(choice.Path, state.startupShell) {
			state.startupShell = ""
		} else {
			state.startupShell = choice.Path
			state.startupLocal = false
		}
		state.save()
		return true
	}
	index, ok := id.In(hitSettingsStartupHostList)
	if !ok || index < 0 || index >= len(state.hosts) {
		return false
	}
	host := state.hosts[index]
	if state.startupHostSelected(host) {
		state.startupHosts = slices.DeleteFunc(state.startupHosts, func(candidate string) bool { return candidate == host.CatalogID })
	} else if host.CatalogID != "" {
		state.startupHosts = cleanStartupHosts(append(state.startupHosts, host.CatalogID))
	}
	state.save() // the choice holds while the menu stays open
	return true
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
	// locate finds the open session of a host; on this machine the session's
	// own shell counts, so a launch reuses the shell it was asked to open.
	locate := func(hostID uint32, shell string) *Session {
		for index := range state.sessions {
			session := &state.sessions[index]
			if session.HostID != hostID {
				continue
			}
			if hostID == localHostID && !strings.EqualFold(session.host.localShell, shell) {
				continue
			}
			return session
		}
		return nil
	}
	var shown *Session
	if state.startupLocal || state.startupShell != "" {
		shell := state.startupShell
		if existing := locate(localHostID, shell); existing != nil && state.startupReuse {
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
	kept := state.startupHosts[:0]
	for _, id := range state.startupHosts {
		host, ok := state.hostByCatalogID(id)
		if !ok {
			continue
		}
		kept = append(kept, id)
		if existing := locate(host.ID, ""); existing != nil && state.startupReuse {
			shown = existing
			continue
		}
		state.pendingWorkspace, state.pendingPane = 0, 0
		shown = state.newHostSession(host)
	}
	state.startupHosts = kept
	if shown != nil {
		state.activeSection = sectionTerminal
		state.ActivatePane(shown.ID)
	}
}
