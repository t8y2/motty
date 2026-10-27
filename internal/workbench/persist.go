package workbench

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

// stateFileVersion is the only state-file format this build reads; a file of
// another version is set aside (older) or left alone (newer).
const stateFileVersion = 4

const shortcutSchemaVersion = 1

// savedState is what survives a restart: user-owned configuration plus enough
// workspace metadata to restore tabs without reconnecting. Run-local numeric
// IDs are not saved; stable host catalog IDs are.
type savedState struct {
	Version                    int                 `json:"version"`
	Language                   string              `json:"language,omitempty"`
	Theme                      string              `json:"theme,omitempty"`
	TerminalOption             string              `json:"terminalOption,omitempty"`
	TerminalFontSize           int                 `json:"terminal_font_size,omitempty"`
	TerminalFontFace           string              `json:"terminal_font_face,omitempty"`
	TerminalFontFamily         string              `json:"terminal_font_family,omitempty"`
	TerminalScrollback         int                 `json:"terminal_scrollback,omitempty"`
	ConnectionTimeoutSeconds   int                 `json:"connection_timeout_seconds,omitempty"`
	ConnectionKeepAliveSeconds int                 `json:"connection_keepalive_seconds,omitempty"`
	ConnectionReconnect        string              `json:"connection_reconnect,omitempty"`
	RestoreConnect             string              `json:"restore_connect,omitempty"`
	ShareConnections           string              `json:"share_connections,omitempty"`
	TerminalClipboard          string              `json:"terminal_clipboard,omitempty"`
	TerminalCursorSteady       bool                `json:"terminal_cursor_steady,omitempty"`
	AIPanelWidth               float32             `json:"ai_panel_width,omitempty"`
	WindowX                    int                 `json:"window_x,omitempty"`
	WindowY                    int                 `json:"window_y,omitempty"`
	WindowWidth                int                 `json:"window_width,omitempty"`
	WindowHeight               int                 `json:"window_height,omitempty"`
	TerminalScheme             string              `json:"terminal_scheme,omitempty"`
	TerminalPadding            string              `json:"terminal_padding,omitempty"`
	TerminalGutter             string              `json:"terminal_gutter,omitempty"`
	TerminalNotifications      string              `json:"terminal_notifications,omitempty"`
	SFTPParallel               int                 `json:"sftp_parallel,omitempty"`
	SFTPKeepTimes              string              `json:"sftp_keep_times,omitempty"`
	SFTPFollow                 string              `json:"sftp_follow,omitempty"`
	UpdateCheck                string              `json:"update_check,omitempty"`
	UpdateSkipped              string              `json:"update_skipped,omitempty"`
	SFTPBookmarks              []savedBookmark     `json:"sftp_bookmarks,omitempty"`
	OutboundProxy              *savedOutboundProxy `json:"outbound_proxy,omitempty"`
	HighlightBuiltin           string              `json:"highlight_builtin,omitempty"`
	Highlights                 []savedHighlight    `json:"highlights,omitempty"`
	Snippets                   []savedSnippet      `json:"snippets,omitempty"`
	Autofill                   []savedAutofill     `json:"autofill_rules,omitempty"`
	CommandHistory             bool                `json:"command_history,omitempty"`
	CommandSuggestOff          bool                `json:"command_suggest_off,omitempty"`
	ShortcutVersion            int                 `json:"shortcut_version,omitempty"`
	Shortcuts                  []savedShortcut     `json:"shortcuts,omitempty"`
	Workspace                  string              `json:"workspace,omitempty"`
	ActiveSession              int                 `json:"active_session,omitempty"`
	HostCatalogVersion         int                 `json:"host_catalog_version"`
	Hosts                      []savedHost         `json:"hosts"`
	HostGroups                 []savedHostGroup    `json:"host_groups,omitempty"`
	IdentityFiles              []string            `json:"identity_files,omitempty"`
	Forwards                   []savedForward      `json:"forwards,omitempty"`
	Proxies                    []savedProxy        `json:"proxies,omitempty"`
	Sessions                   []savedSession      `json:"sessions,omitempty"`
	Deck                       *layoutcore.DeckDTO `json:"deck,omitempty"`
	Sync                       *savedSync          `json:"sync,omitempty"`
	HostView                   string              `json:"host_view,omitempty"`
	Background                 *savedBackground    `json:"background,omitempty"`
	// Provider collections are nil while the assistant's database is unread.
	AIProviders    *[]savedAIProvider `json:"ai_providers,omitempty"`
	AICLIProviders *[]savedAIProvider `json:"ai_cli_providers,omitempty"`
}

type savedOutboundProxy struct {
	Kind    string `json:"kind"`
	Address string `json:"address,omitempty"`
	User    string `json:"user,omitempty"`
	Bypass  string `json:"bypass,omitempty"`
}

type savedHighlight struct {
	Pattern       string `json:"pattern"`
	Regex         bool   `json:"regex,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
	Color         string `json:"color"`
}

type savedSnippet struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Run     bool   `json:"run,omitempty"`
}

type savedShortcut struct {
	Action    string `json:"action"`
	Key       string `json:"key"`
	Modifiers uint8  `json:"modifiers"`
}

type savedHost struct {
	Color   string   `json:"color,omitempty"`
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Group   string   `json:"group,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Icon    string   `json:"icon,omitempty"`
	Address string   `json:"address"`
	User    string   `json:"user,omitempty"`
	Port    uint16   `json:"port,omitempty"`
	System  string   `json:"system,omitempty"`
	savedTransport
}

type savedHostGroup struct {
	Name      string `json:"name"`
	Collapsed bool   `json:"collapsed,omitempty"`
}

type savedForward struct {
	Label           string `json:"label"`
	Mode            string `json:"mode,omitempty"`
	HostLabel       string `json:"host_label"`
	HostAddress     string `json:"host_address"`
	HostUser        string `json:"host_user,omitempty"`
	HostPort        uint16 `json:"host_port"`
	LocalPort       uint16 `json:"local_port"`
	DestinationHost string `json:"destination_host,omitempty"`
	DestinationPort uint16 `json:"destination_port,omitempty"`
}

type savedProxy struct {
	TargetAddress string `json:"target_address"`
	TargetUser    string `json:"target_user,omitempty"`
	TargetPort    uint16 `json:"target_port"`
	JumpAddress   string `json:"jump_address"`
	JumpUser      string `json:"jump_user,omitempty"`
	JumpPort      uint16 `json:"jump_port"`
}

type savedSession struct {
	Label     string `json:"label,omitempty"`
	Address   string `json:"address"`
	User      string `json:"user,omitempty"`
	Port      uint16 `json:"port,omitempty"`
	System    string `json:"system,omitempty"`
	SavedHost bool   `json:"saved_host,omitempty"`
	// Local marks a shell on this computer: it has no address to dial.
	Local bool `json:"local,omitempty"`
	savedTransport
}

const maxRestoredSessions = 32

// loadOutcome says what loadState found.
type loadOutcome uint8

const (
	// loadOK is a file that was read, or no file yet: a first run.
	loadOK loadOutcome = iota
	// loadUnreadable is a damaged file. It was set aside and the app starts empty.
	loadUnreadable
	// loadTooNew is a file written by a newer build. It is left exactly as it is,
	// and nothing may be saved over it: an older build cannot tell what the newer
	// one put in it, and dropping that would lose the user's data.
	loadTooNew
)

// loadReport carries the details of a file that was not simply loaded.
type loadReport struct {
	outcome loadOutcome
	// kept is where an unreadable file was set aside.
	kept string
	// found is the format versions of a file that is too new, and supported the
	// ones this build reads, as "state.catalog.shortcuts".
	found, supported string
}

// describe says how the file was read, for the diagnostics report.
func (report loadReport) describe() string {
	switch report.outcome {
	case loadTooNew:
		return "newer than this build (" + report.found + ", supports " + report.supported + ")"
	case loadUnreadable:
		return "unreadable, set aside"
	}
	return "read"
}

// formatVersions names the three schema versions a state file carries.
func formatVersions(state, catalog, shortcuts int) string {
	return fmt.Sprintf("%d.%d.%d", state, catalog, shortcuts)
}

// loadState reads the saved state from the store at path and closes it again.
// A missing file is a normal first run. A store from a newer build is reported
// as loadTooNew and not touched. A file that cannot be understood is set aside
// under a name that does not replace an earlier one, and reported as
// loadUnreadable so the user can recover it.
func loadState(path string) (savedState, loadReport, error) {
	store, saved, _, report, err := openConfig(path)
	if store != nil {
		store.Close()
	}
	return saved, report, err
}

// setAside renames an unreadable file to path.corrupt, or path.corrupt.2 and so
// on when an earlier one is there: a second damaged file must not replace the
// first, which may hold what the second lost.
func setAside(path string) (string, error) {
	kept := path + ".corrupt"
	for number := 2; ; number++ {
		if _, err := os.Lstat(kept); errors.Is(err, fs.ErrNotExist) {
			break
		}
		kept = fmt.Sprintf("%s.corrupt.%d", path, number)
	}
	return kept, os.Rename(path, kept)
}

// backupState copies the file that was just loaded to path.bak, so a state that
// later goes wrong has the last good one beside it. An empty catalog is not
// worth keeping and never replaces a backup that has hosts in it.
func backupState(path string, saved savedState) {
	if len(saved.Hosts) == 0 {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	_ = writeFileAtomic(path+".bak", data)
}

// checkSavedState rejects a decoded state whose versions this build does not read.
func checkSavedState(state *savedState) error {
	if state.Version != stateFileVersion {
		return fmt.Errorf("unsupported state version %d", state.Version)
	}
	if state.HostCatalogVersion != hostCatalogSchemaVersion {
		return fmt.Errorf("unsupported host catalog version %d", state.HostCatalogVersion)
	}
	if state.ShortcutVersion != shortcutSchemaVersion {
		return fmt.Errorf("unsupported shortcut version %d", state.ShortcutVersion)
	}
	return nil
}

// saveState makes the store at path hold exactly state, and closes it again.
func saveState(path string, state savedState) error {
	store, _, base, report, err := openConfig(path)
	if err != nil {
		return err
	}
	if store == nil {
		return fmt.Errorf("%s is newer than this build (%s, this one reads %s)", path, report.found, report.supported)
	}
	defer store.Close()
	_, err = writeConfig(store, base, nil, state)
	return err
}

// writeFileAtomic replaces path with data through a temporary file and a rename,
// so a crash leaves either the old file or the new one, never half of one.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".state-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		os.Remove(temporary)
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		os.Remove(temporary)
		return err
	}
	return nil
}

// snapshot is the state as it should be saved right now.
func (state *State) snapshot() savedState {
	saved := savedState{
		Version:                    stateFileVersion,
		Language:                   state.savedLanguage(),
		Theme:                      state.appearanceMode.saved(),
		TerminalOption:             state.terminalOption.String(),
		TerminalFontSize:           int(state.terminalFontSize),
		TerminalFontFace:           terminalFontFaceString(state.terminalFontFace),
		TerminalFontFamily:         state.fonts.family,
		TerminalScrollback:         state.terminalScrollback,
		ConnectionTimeoutSeconds:   int(state.connectionTimeout / time.Second),
		ConnectionKeepAliveSeconds: int(state.connectionKeepAlive / time.Second),
		ConnectionReconnect:        state.connectionReconnect.String(),
		RestoreConnect:             state.restoreConnect.String(),
		ShareConnections:           state.shareConnections.String(),
		TerminalClipboard:          state.terminalClipboard.String(),
		TerminalCursorSteady:       state.terminalCursorSteady,
		AIPanelWidth:               state.ai.panelWidth,
		TerminalScheme:             state.terminalScheme,
		TerminalPadding:            state.terminalPad.String(),
		TerminalGutter:             state.terminalGutter.String(),
		TerminalNotifications:      notifyString(state.notifyOff),
		CommandHistory:             state.history.on,
		CommandSuggestOff:          state.history.suggestOff,
		SFTPParallel:               state.sftpParallel,
		SFTPKeepTimes:              notifyString(state.sftpKeepTimesOff),
		SFTPFollow:                 notifyString(state.sftpFollowOff),
		UpdateCheck:                updateCheckString(state.updateAtStart),
		UpdateSkipped:              state.update.skipped,
		ShortcutVersion:            shortcutSchemaVersion,
		Shortcuts:                  make([]savedShortcut, 0, shortcutActionCount-1),
		Workspace:                  state.workspaceName(),
		HostCatalogVersion:         hostCatalogSchemaVersion,
		Hosts:                      make([]savedHost, 0, len(state.hosts)),
		HostGroups:                 make([]savedHostGroup, 0, len(state.hostGroups)),
		IdentityFiles:              make([]string, 0, len(state.identities)),
		Forwards:                   make([]savedForward, 0, len(state.forwards)),
		Proxies:                    make([]savedProxy, 0, len(state.proxies)),
		Sessions:                   make([]savedSession, 0, len(state.sessions)),
	}
	if frame, ok := state.savedWindowFrame(); ok {
		saved.WindowX = int(math.Round(float64(frame.X)))
		saved.WindowY = int(math.Round(float64(frame.Y)))
		saved.WindowWidth = int(math.Round(float64(frame.Width)))
		saved.WindowHeight = int(math.Round(float64(frame.Height)))
	}
	if state.highlighting.builtinOff {
		saved.HighlightBuiltin = "off"
	}
	for _, rule := range state.highlighting.rules {
		saved.Highlights = append(saved.Highlights, savedHighlight{
			Pattern: rule.Pattern, Regex: rule.Regex, CaseSensitive: rule.CaseSensitive, Color: rule.Slot.Name(),
		})
	}
	saved.Autofill = state.savedAutofillRules()
	for _, entry := range state.snippets.list {
		saved.Snippets = append(saved.Snippets, savedSnippet{Name: entry.Name, Command: entry.Command, Run: entry.Run})
	}
	if state.proxy.kind != sshconn.ProxyNone || state.proxy.bypass != "" {
		saved.OutboundProxy = &savedOutboundProxy{
			Kind: proxyKindString(state.proxy.kind), Address: state.proxy.address, User: state.proxy.user, Bypass: state.proxy.bypass,
		}
	}
	for action := shortcutAction(1); action < shortcutActionCount; action++ {
		chord := state.shortcuts.chords[action]
		saved.Shortcuts = append(saved.Shortcuts, savedShortcut{
			Action: action.String(), Key: string([]byte{chord.Key}), Modifiers: uint8(chord.Modifiers),
		})
	}
	for _, host := range state.hosts {
		saved.Hosts = append(saved.Hosts, savedHost{
			ID: host.CatalogID, Label: host.Label, Group: host.Group, Tags: splitHostTags(host), Icon: host.Icon.String(), Color: host.Color,
			Address: host.Address, User: host.User, Port: host.Port, System: host.System.String(),
			savedTransport: host.transport(),
		})
	}
	for _, bookmark := range state.sftpBookmarks {
		saved.SFTPBookmarks = append(saved.SFTPBookmarks, savedBookmark{Host: bookmark.host, Path: bookmark.path})
	}
	for _, group := range state.hostGroups {
		saved.HostGroups = append(saved.HostGroups, savedHostGroup{Name: group.Name, Collapsed: !group.Expanded})
	}
	for _, identity := range state.identities {
		saved.IdentityFiles = append(saved.IdentityFiles, identity.Path)
	}
	for _, forward := range state.forwards {
		saved.Forwards = append(saved.Forwards, savedForward{
			Label: forward.Label, Mode: forward.savedMode(), HostLabel: forward.HostLabel, HostAddress: forward.HostAddress,
			HostUser: forward.HostUser, HostPort: forward.HostPort, LocalPort: forward.LocalPort,
			DestinationHost: forward.DestinationHost, DestinationPort: forward.DestinationPort,
		})
	}
	for _, route := range state.proxies {
		target, targetOK := state.hostByID(route.TargetID)
		jump, jumpOK := state.hostByID(route.JumpID)
		if !targetOK || !jumpOK {
			continue
		}
		saved.Proxies = append(saved.Proxies, savedProxy{
			TargetAddress: target.Address, TargetUser: target.User, TargetPort: target.Port,
			JumpAddress: jump.Address, JumpUser: jump.User, JumpPort: jump.Port,
		})
	}
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.isLocal() {
			saved.Sessions = append(saved.Sessions, savedSession{Label: session.Title, Local: true})
			if session.ID == state.activeSessionID {
				saved.ActiveSession = index + 1
			}
			continue
		}
		saved.Sessions = append(saved.Sessions, savedSession{
			Label: session.Title, Address: session.host.Address, User: session.host.User,
			Port: session.host.Port, System: session.host.System.String(), SavedHost: session.HostID != 0,
			savedTransport: session.host.transport(),
		})
		if session.ID == state.activeSessionID {
			saved.ActiveSession = index + 1
		}
	}
	saved.AIProviders = state.savedAIProviders()
	saved.AICLIProviders = state.savedAICLIProviders()
	saved.Sync = state.savedSyncValue()
	saved.HostView = state.hostView.saved()
	saved.Background = state.savedBackgroundValue()
	if len(state.workspaceTabs) > 0 {
		deck := state.savedWorkspaceDeck()
		saved.Deck = &deck
	}
	return saved
}

// save persists the state. A failure is reported in the status line instead of
// interrupting what the user is doing; the change stays in memory. A state that
// started from a file of a newer build never saves, so that file stays as it is;
// the dialog that says so comes back each time the user changes something.
func (state *State) save() {
	defer state.sshSyncFlush()
	if state.saveBlocked {
		if state.modal == modalNone {
			state.modal = modalStateNotice
		}
		return
	}
	if state.config == nil || state.pauseSave > 0 {
		return
	}
	snapshot := state.snapshot()
	// Many actions save after changing only run-local UI state. Comparing the
	// snapshot to the last saved one is tens of microseconds, where encoding and
	// diffing it costs about a millisecond and 700 KB of garbage.
	if state.hasSaved {
		changes := changedSavedFields(state.lastSaved, snapshot)
		if len(changes) == 0 {
			return
		}
		shared, handled, err := writeChangedFields(state.config, state.configBase, state.configDefaults, changes)
		if handled {
			if err != nil {
				state.status = fmt.Sprintf(state.messages.StatusSaveFailedFmt, err)
				return
			}
			state.lastSaved, state.hasSaved = snapshot, true
			if shared {
				state.scheduleSync()
			}
			return
		}
	}
	shared, err := writeConfig(state.config, state.configBase, state.configDefaults, snapshot)
	if err != nil {
		state.status = fmt.Sprintf(state.messages.StatusSaveFailedFmt, err)
		return
	}
	state.lastSaved, state.hasSaved = snapshot, true
	if shared {
		state.scheduleSync()
	}
}

// restore applies a loaded state at start: its settings and catalog, then the
// sessions and the layout it left open.
func (state *State) restore(saved savedState) {
	state.restoreSettings(saved)
	state.stopAppearanceWatch = state.options.WatchSystemAppearance(state.systemAppearanceChanged)
	sourceSessions := state.restoreSessions(saved.Sessions, saved.ActiveSession)
	state.restoreWorkspaceDeck(saved.Deck, sourceSessions)
	state.rebuildTabOptions()
	state.restoreWorkspace(saved.Workspace)
}

// restoreSettings applies the settings and the catalogs of a saved state, and
// can be called again to replace them.
func (state *State) restoreSettings(saved savedState) {
	state.restoreLocal(saved)
	state.restoreShared(saved)
	state.restoreBackground(saved.Background)
}

// restoreLocal applies what belongs to this computer: the language and look, the
// fonts, what it does at start, the keys it has, the cloud account, the picture.
func (state *State) restoreLocal(saved savedState) {
	// A saved language is the user's choice. Without one (a first run, or a state
	// from before the choice was told apart) the system's language decides, and
	// stays undecided so a change of system language is followed.
	switch saved.Language {
	case i18n.Chinese.String():
		state.language, state.languageChosen = i18n.Chinese, true
	case i18n.English.String():
		state.language, state.languageChosen = i18n.English, true
	default:
		state.language, state.languageChosen = state.systemLanguage(), false
	}
	state.messages = i18n.For(state.language)
	state.appearanceMode = appearanceModeFromString(saved.Theme)
	state.syncNativeAppearance()
	state.setAppearance(state.resolveAppearance())
	state.setTerminalFontSize(float32(saved.TerminalFontSize))
	state.setTerminalFontFace(terminalFontFaceFromString(saved.TerminalFontFace))
	state.fonts.family = ""
	if family := strings.TrimSpace(saved.TerminalFontFamily); family != "" {
		if face := ui.MonoFamily(family); face != ui.MonoSystem {
			state.fonts.family = family
			state.setTerminalFontFace(face)
		}
	}
	state.restoreConnect = restoreConnectPolicyFromString(saved.RestoreConnect)
	state.shareConnections = shareConnectionsPolicyFromString(saved.ShareConnections)
	state.ai.panelWidth = saved.AIPanelWidth
	state.restoreIdentities(saved.IdentityFiles)
	state.restoreSync(saved.Sync)
	state.hostView = hostViewFromString(saved.HostView)
	if saved.WindowWidth > 0 && saved.WindowHeight > 0 {
		state.restoreWindowFrame(ui.Rect{
			X: float32(saved.WindowX), Y: float32(saved.WindowY),
			Width: float32(saved.WindowWidth), Height: float32(saved.WindowHeight),
		})
	}
}

// restoreShared applies what every computer of the user shares: the terminal and
// connection settings, the rules and snippets, the hosts and what refers to
// them. Called again with open sessions it keeps what is running: a host keeps
// its place for the sessions that use it, and a forward that is still wanted
// keeps running.
func (state *State) restoreShared(saved savedState) {
	state.terminalOption = terminalOptionModeFromString(saved.TerminalOption)
	state.shortcuts = restoreShortcuts(saved.Shortcuts)
	state.setTerminalScrollback(normalizeTerminalScrollback(saved.TerminalScrollback))
	state.connectionTimeout = time.Duration(normalizeConnectionTimeoutSeconds(saved.ConnectionTimeoutSeconds)) * time.Second
	state.connectionKeepAlive = time.Duration(normalizeConnectionKeepAliveSeconds(saved.ConnectionKeepAliveSeconds)) * time.Second
	state.connectionReconnect = reconnectPolicyFromString(saved.ConnectionReconnect)
	state.terminalClipboard = terminalClipboardPolicyFromString(saved.TerminalClipboard)
	state.terminalCursorSteady = saved.TerminalCursorSteady
	state.terminalScheme = normalizeSchemeID(saved.TerminalScheme)
	state.terminalPad = terminalPadFromString(saved.TerminalPadding)
	state.terminalGutter = terminalGutterFromString(saved.TerminalGutter)
	state.notifyOff = saved.TerminalNotifications == "off"
	state.history.on = saved.CommandHistory
	state.history.suggestOff = saved.CommandSuggestOff
	state.restoreSFTPSettings(saved)
	state.proxy = outboundProxy{}
	if entry := saved.OutboundProxy; entry != nil {
		kind := proxyKindFromString(entry.Kind)
		if kind != sshconn.ProxyNone && !validProxyAddress(entry.Address) {
			kind = sshconn.ProxyNone // a damaged address would only fail every connection
		}
		state.proxy = outboundProxy{kind: kind, bypass: strings.TrimSpace(entry.Bypass)}
		if kind != sshconn.ProxyNone {
			state.proxy.address, state.proxy.user = entry.Address, entry.User
		}
	}
	state.restoreTerminalTools(saved)
	state.restoreAutofill(saved)

	// A host keeps the number the sessions and the selection know it by.
	known := make(map[string]uint32, len(state.hosts))
	for _, host := range state.hosts {
		if host.CatalogID != "" {
			known[host.CatalogID] = host.ID
		}
	}
	next := max(state.nextHostID, 1)
	clear(state.hosts)
	clear(state.hostGroups)
	state.hosts = state.hosts[:0]
	state.hostGroups = state.hostGroups[:0]
	usedIDs := make(map[string]struct{}, len(saved.Hosts))
	for _, entry := range saved.Hosts {
		if entry.Label == "" || entry.Address == "" {
			continue
		}
		id, kept := known[entry.ID]
		if !kept {
			id = next
			next++
		}
		host := Host{
			ID: id, CatalogID: entry.ID, Label: entry.Label, Group: strings.TrimSpace(entry.Group),
			Address: entry.Address, User: entry.User, Port: entry.Port, System: sshconn.ParseRemoteSystem(entry.System),
		}
		if !host.applyTransport(entry.savedTransport) {
			if !kept {
				next--
			}
			continue
		}
		host.Icon, _ = parseHostIcon(entry.Icon)
		host.Color, _ = normalizeHostColor(entry.Color)
		if tags, ok := normalizeHostTags(entry.Tags); ok {
			setHostTags(&host, tags)
		}
		host.normalizeEndpoint()
		host.finish()
		host.CatalogID = uniqueCatalogID(host.CatalogID, host, usedIDs)
		state.hosts = append(state.hosts, host)
	}
	state.nextHostID = next
	for _, entry := range saved.HostGroups {
		name := strings.TrimSpace(entry.Name)
		if state.hostGroupIndex(name) >= 0 {
			continue
		}
		state.hostGroups = append(state.hostGroups, hostGroup{Name: name, Expanded: !entry.Collapsed})
	}
	state.restoreForwards(saved.Forwards)
	state.restoreProxies(saved.Proxies)
	if (saved.AIProviders != nil || saved.AICLIProviders != nil) && !state.starting {
		state.importAIProviderCollections(saved.AIProviders, saved.AICLIProviders)
	}
}

func (state *State) workspaceName() string {
	if name, ok := state.devWorkspaceName(); ok {
		return name
	}
	switch state.activeSection {
	case sectionTerminal:
		return "terminal"
	case sectionKeys:
		return "keys"
	case sectionForwards:
		return "forwards"
	case sectionProxies:
		return "proxies"
	case sectionSettings:
		if state.settingsCategory == settingsShortcuts {
			return "shortcuts"
		}
		return "settings"
	default:
		return "hosts"
	}
}

func restoreShortcuts(saved []savedShortcut) shortcutMap {
	shortcuts := defaultShortcuts()
	for _, entry := range saved {
		action := shortcutActionFromString(entry.Action)
		if action == shortcutNone || len(entry.Key) != 1 {
			continue
		}
		key := entry.Key[0]
		if key >= 'A' && key <= 'Z' {
			key += 'a' - 'A'
		}
		shortcuts.bind(action, shortcutChord{Modifiers: ui.Modifiers(entry.Modifiers), Key: key})
	}
	shortcuts.refreshLabels()
	return shortcuts
}

func (state *State) restoreWorkspace(workspace string) {
	if state.devRestoreWorkspace(workspace) {
		return
	}
	switch workspace {
	case "terminal":
		if state.activeSessionID != 0 {
			state.activeSection = sectionTerminal
		}
	case "keys":
		state.activeSection = sectionKeys
	case "forwards":
		state.activeSection = sectionForwards
	case "proxies":
		state.activeSection = sectionProxies
	case "settings":
		state.activeSection = sectionSettings
	case "shortcuts":
		state.activeSection = sectionSettings
		state.settingsCategory = settingsShortcuts
	default:
		state.activeSection = sectionHosts
	}
}

func (state *State) restoreProxies(saved []savedProxy) {
	clear(state.proxies)
	state.proxies = state.proxies[:0]
	state.selectedProxyID = 0
	state.nextProxyID = 1
	for _, entry := range saved {
		target, targetOK := state.hostByConnection(entry.TargetAddress, entry.TargetUser, entry.TargetPort)
		jump, jumpOK := state.hostByConnection(entry.JumpAddress, entry.JumpUser, entry.JumpPort)
		if !targetOK || !jumpOK || target.ID == jump.ID || state.proxyTargetConfigured(target.ID) || state.proxyWouldCycle(target.ID, jump.ID) {
			continue
		}
		route := proxyRecord{ID: state.nextProxyID, TargetID: target.ID, JumpID: jump.ID}
		state.nextProxyID++
		state.proxies = append(state.proxies, route)
	}
	if len(state.proxies) > 0 {
		state.selectedProxyID = state.proxies[0].ID
	}
	state.refreshProxies()
}

func (state *State) hostByConnection(address, user string, port uint16) (Host, bool) {
	if port == 0 {
		port = defaultPort
	}
	for _, host := range state.hosts {
		if host.isSSH() && host.Address == address && host.User == user && host.Port == port {
			return host, true
		}
	}
	return Host{}, false
}

func (state *State) restoreSessions(entries []savedSession, active int) []uint32 {
	clear(state.sessions)
	state.sessions = state.sessions[:0]
	state.activeSessionID = 0
	state.nextSessionID = 1
	sourceSessions := make([]uint32, len(entries))
	for sourceIndex, entry := range entries {
		if len(state.sessions) == maxRestoredSessions {
			break
		}
		host, ok := state.restoreSessionHost(entry)
		if !ok {
			continue
		}
		state.sessions = append(state.sessions, Session{
			ID: state.nextSessionID, HostID: host.ID, Title: host.Label,
			Address: state.sessionAddress(host),
			host:    host, phase: sshconn.PhaseClosed, restored: true, paneSlot: state.claimPaneSlot(),
		})
		session := &state.sessions[len(state.sessions)-1]
		sourceSessions[sourceIndex] = session.ID
		state.nextSessionID++
		if sourceIndex+1 == active {
			state.activeSessionID = session.ID
		}
	}
	if state.activeSessionID == 0 && len(state.sessions) > 0 {
		state.activeSessionID = state.sessions[0].ID
	}
	state.rebuildTabOptions()
	return sourceSessions
}

func (state *State) restoreSessionHost(entry savedSession) (Host, bool) {
	if entry.Local {
		return localHost(state.messages.LocalTerminalTitle), true
	}
	if entry.Address == "" {
		return Host{}, false
	}
	if entry.Label == "" {
		entry.Label = entry.Address
	}
	host := Host{Label: entry.Label, Address: entry.Address, User: entry.User, Port: entry.Port, System: sshconn.ParseRemoteSystem(entry.System)}
	if !host.applyTransport(entry.savedTransport) {
		return Host{}, false
	}
	host.normalizeEndpoint()
	if entry.SavedHost {
		for _, saved := range state.hosts {
			if sameEndpoint(saved, host) {
				return saved, true
			}
		}
		return Host{}, false
	}
	host.finish()
	return host, true
}

// savedLanguage is what the state file keeps of the language: the choice, or
// nothing while the system still decides.
func (state *State) savedLanguage() string {
	if !state.languageChosen {
		return ""
	}
	return state.language.String()
}
