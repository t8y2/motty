package workbench

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/configstore"
	"motty/internal/i18n"
	"motty/internal/monitoring"
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
	RecentConnectSort bool                 `json:"recent_connect_sort,omitempty"`
	RecentConnections map[string]time.Time `json:"recent_connections,omitempty"`

	MonitorPanels              []monitoring.Panel  `json:"monitor_panels,omitempty"`
	MonitorActive              string              `json:"monitor_active,omitempty"`
	Version                    int                 `json:"version"`
	Language                   string              `json:"language,omitempty"`
	Theme                      string              `json:"theme,omitempty"`
	BackgroundOutputAlerts     bool                `json:"background_output_alerts,omitempty"`
	TabOrientation             string              `json:"tab_orientation,omitempty"`
	VerticalTabsAutoHide       bool                `json:"vertical_tabs_auto_hide,omitempty"`
	VerticalTabsGrouping       bool                `json:"vertical_tabs_grouping,omitempty"`
	NetworkProxy               string              `json:"network_proxy,omitempty"`
	TerminalOption             string              `json:"terminalOption,omitempty"`
	LocalShell                 string              `json:"local_shell,omitempty"`
	SSHAgent                   string              `json:"ssh_agent,omitempty"`
	SSHAgentPath               string              `json:"ssh_agent_path,omitempty"`
	TerminalFontSize           int                 `json:"terminal_font_size,omitempty"`
	TextScale                  string              `json:"text_scale,omitempty"`
	InterfaceFont              string              `json:"interface_font,omitempty"`
	TerminalFontFace           string              `json:"terminal_font_face,omitempty"`
	TerminalFontFamily         string              `json:"terminal_font_family,omitempty"`
	TerminalFontFallbacks      []string            `json:"terminal_font_fallbacks,omitempty"`
	TerminalScrollback         int                 `json:"terminal_scrollback,omitempty"`
	ConnectionTimeoutSeconds   int                 `json:"connection_timeout_seconds,omitempty"`
	ConnectionKeepAliveSeconds int                 `json:"connection_keepalive_seconds,omitempty"`
	ConnectionReconnect        string              `json:"connection_reconnect,omitempty"`
	RestoreConnect             string              `json:"restore_connect,omitempty"`
	ShareConnections           string              `json:"share_connections,omitempty"`
	TerminalClipboard          string              `json:"terminal_clipboard,omitempty"`
	TerminalCursorSteady       bool                `json:"terminal_cursor_steady,omitempty"`
	TerminalCopyOnSelect       bool                `json:"terminal_copy_on_select,omitempty"`
	TerminalRightClickPaste    bool                `json:"terminal_right_click_paste,omitempty"`
	TerminalTabTitle           bool                `json:"terminal_tab_title,omitempty"`
	TerminalShowLatency        bool                `json:"terminal_show_latency,omitempty"`
	TerminalToolbarPosition    string              `json:"terminal_toolbar_position,omitempty"`
	TerminalToolbarSize        string              `json:"terminal_toolbar_size,omitempty"`
	TerminalBellOff            bool                `json:"terminal_bell_off,omitempty"`
	TerminalLinkPlainClick     bool                `json:"terminal_link_plain_click,omitempty"`
	AIPanelWidth               float32             `json:"ai_panel_width,omitempty"`
	AIComposerHeight           float32             `json:"ai_composer_height,omitempty"`
	MonitorPanelWidth          float32             `json:"monitor_panel_width,omitempty"`
	WindowX                    int                 `json:"window_x,omitempty"`
	WindowY                    int                 `json:"window_y,omitempty"`
	WindowWidth                int                 `json:"window_width,omitempty"`
	WindowHeight               int                 `json:"window_height,omitempty"`
	TerminalScheme             string              `json:"terminal_scheme,omitempty"` // read only: before the scheme was kept per look
	TerminalSchemeDark         string              `json:"terminal_scheme_dark,omitempty"`
	TerminalSchemeLight        string              `json:"terminal_scheme_light,omitempty"`
	TerminalPadding            string              `json:"terminal_padding,omitempty"`
	TerminalGutter             string              `json:"terminal_gutter,omitempty"`
	TerminalIntense            string              `json:"terminal_intense,omitempty"`
	TerminalNotifications      string              `json:"terminal_notifications,omitempty"`
	TerminalPasteImage         string              `json:"terminal_paste_image,omitempty"`
	SFTPParallel               int                 `json:"sftp_parallel,omitempty"`
	SFTPKeepTimes              string              `json:"sftp_keep_times,omitempty"`
	SFTPFollow                 string              `json:"sftp_follow,omitempty"`
	SFTPTerminalFollow         bool                `json:"sftp_terminal_follow,omitempty"`
	SFTPPathInput              bool                `json:"sftp_path_input,omitempty"`
	SFTPPanelAutoOpen          bool                `json:"sftp_panel_auto_open,omitempty"`
	SFTPPanelSide              string              `json:"sftp_panel_side,omitempty"`
	SFTPPanelWidth             float32             `json:"sftp_panel_width,omitempty"`
	SFTPWindowOrder            string              `json:"sftp_window_order,omitempty"`
	SFTPOpenMode               string              `json:"sftp_open_mode,omitempty"`
	SFTPColumns                string              `json:"sftp_columns,omitempty"`
	SFTPDownloadDirectory      string              `json:"sftp_download_directory,omitempty"`
	DefaultEditor              string              `json:"default_editor,omitempty"` // this computer's editor for remote folders
	CustomEditorPath           string              `json:"custom_editor_path,omitempty"`
	Toolbars                   *savedToolbars      `json:"toolbars,omitempty"`
	UpdateCheck                string              `json:"update_check,omitempty"`
	CloseAction                string              `json:"close_action,omitempty"`
	UpdateSkipped              string              `json:"update_skipped,omitempty"`
	SFTPBookmarks              []savedBookmark     `json:"sftp_bookmarks,omitempty"`
	SFTPLocalBookmarks         []string            `json:"sftp_local_bookmarks,omitempty"`
	OutboundProxy              *savedOutboundProxy `json:"outbound_proxy,omitempty"`
	HighlightBuiltin           string              `json:"highlight_builtin,omitempty"`
	Highlights                 []savedHighlight    `json:"highlights,omitempty"`
	Snippets                   []savedSnippet      `json:"snippets,omitempty"`
	SnippetShortcuts           []savedSnippetKey   `json:"snippet_shortcuts,omitempty"`
	Autofill                   []savedAutofill     `json:"autofill_rules,omitempty"`
	CommandHistory             bool                `json:"command_history,omitempty"`
	CommandSuggestOff          bool                `json:"command_suggest_off,omitempty"`
	// SessionLogsOff stops the local session journal. Unset means capture is on;
	// it belongs to this computer and is never synced.
	SessionLogsOff          bool                  `json:"session_logs_off,omitempty"`
	SessionLogsView         *savedSessionLogsView `json:"session_logs_view,omitempty"`
	CommandInlineSuggestOff bool                  `json:"command_inline_suggest_off,omitempty"`
	LocalProxyPort          int                   `json:"local_proxy_port,omitempty"`
	ShortcutVersion         int                   `json:"shortcut_version,omitempty"`
	Shortcuts               []savedShortcut       `json:"shortcuts,omitempty"`
	Workspace               string                `json:"workspace,omitempty"`
	ActiveSession           int                   `json:"active_session,omitempty"`
	HostCatalogVersion      int                   `json:"host_catalog_version"`
	Hosts                   []savedHost           `json:"hosts"`
	HostGroups              []savedHostGroup      `json:"host_groups,omitempty"`
	WorkspaceTemplates      []WorkspaceTemplate   `json:"workspace_templates,omitempty"`
	IdentityFiles           []string              `json:"identity_files,omitempty"`
	// AgentHosts are the catalog ids of the hosts an agent may use without a
	// window. It belongs to this computer, not to the hosts that sync, so a host
	// that arrives from another device starts out closed to agents.
	AgentHosts []string            `json:"agent_hosts,omitempty"`
	Forwards   []savedForward      `json:"forwards,omitempty"`
	Proxies    []savedProxy        `json:"proxies,omitempty"`
	Sessions   []savedSession      `json:"sessions,omitempty"`
	Deck       *layoutcore.DeckDTO `json:"deck,omitempty"`
	// An empty array distinguishes a new snapshot with no managed workspaces
	// from an older snapshot that never recorded workspace identity.
	WorkspaceLayouts []savedWorkspaceLayout `json:"workspace_layouts,omitzero"`
	Sync             *savedSync             `json:"sync,omitempty"`
	HostView         string                 `json:"host_view,omitempty"`
	Background       *savedBackground       `json:"background,omitempty"`
	// Provider collections are nil while the assistant's database is unread.
	AIProviders    *[]savedAIProvider `json:"ai_providers,omitempty"`
	AICLIProviders *[]savedAIProvider `json:"ai_cli_providers,omitempty"`
}

type savedOutboundProxy struct {
	Kind    string `json:"kind"`
	Address string `json:"address,omitempty"`
	User    string `json:"user,omitempty"`
	Bypass  string `json:"bypass,omitempty"`
	// Unconfirmed is an imported proxy command not yet saved by the user (see
	// outboundProxy.unconfirmed).
	Unconfirmed bool `json:"unconfirmed,omitempty"`
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

// savedSnippetKey is the shortcut of the snippet of that name. It lives apart
// from the snippet, which is shared with other computers: the modifier a
// shortcut can use differs by platform.
type savedSnippetKey struct {
	Name      string `json:"name"`
	Key       string `json:"key"`
	Modifiers uint8  `json:"modifiers"`
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
	Explicit  bool   `json:"explicit,omitempty"`
	Pinned    bool   `json:"pinned,omitempty"`
	Order     int    `json:"order,omitempty"`
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
	Stopped       bool   `json:"stopped,omitempty"`
	ViaShell      bool   `json:"via_shell,omitempty"`
	RelayCommand  string `json:"relay_command,omitempty"`
}

type savedSession struct {
	Label     string `json:"label,omitempty"`
	Address   string `json:"address"`
	User      string `json:"user,omitempty"`
	Port      uint16 `json:"port,omitempty"`
	System    string `json:"system,omitempty"`
	SavedHost bool   `json:"saved_host,omitempty"`
	// Local marks a shell on this computer: it has no address to dial.
	Local          bool   `json:"local,omitempty"`
	LocalDirectory string `json:"local_directory,omitempty"`
	// LocalShell is the shell this local tab was opened with; empty follows the default.
	LocalShell string `json:"local_shell,omitempty"`
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
	// workspaceErr is a damaged development workspace. It is local to that
	// record, so the shared database and release workspace remain usable.
	workspaceErr error
}

// describe says how the file was read, for the diagnostics report.
func (report loadReport) describe() string {
	switch report.outcome {
	case loadTooNew:
		return "newer than this build (" + report.found + ", supports " + report.supported + ")"
	case loadUnreadable:
		return "unreadable, set aside"
	}
	if report.workspaceErr != nil {
		return "read, development workspace reset"
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
	savedAgent, savedAgentPath := state.sshAgent.saved()
	saved := savedState{
		MonitorPanels:              state.dashboard.panelDefinitions(),
		MonitorActive:              state.dashboard.active,
		Version:                    stateFileVersion,
		Language:                   state.savedLanguage(),
		Theme:                      state.appearanceMode.saved(),
		BackgroundOutputAlerts:     state.backgroundOutputAlerts,
		TabOrientation:             state.tabOrientation.String(),
		VerticalTabsAutoHide:       state.verticalTabs.autoHide,
		VerticalTabsGrouping:       state.verticalTabs.grouping,
		NetworkProxy:               state.networkProxy.String(),
		TerminalOption:             state.terminalOption.String(),
		LocalShell:                 state.localShell,
		RecentConnectSort:          state.recentConnectSort,
		RecentConnections:          maps.Clone(state.recentConnections),
		SSHAgent:                   savedAgent,
		SSHAgentPath:               savedAgentPath,
		TerminalFontSize:           int(state.terminalFontSize),
		TextScale:                  state.textScale.String(),
		InterfaceFont:              state.uiFont.family,
		TerminalFontFace:           terminalFontFaceString(state.fonts.classic),
		TerminalFontFamily:         state.fonts.family,
		TerminalFontFallbacks:      slices.Clone(state.fonts.fallbacks),
		TerminalScrollback:         state.terminalScrollback,
		ConnectionTimeoutSeconds:   int(state.connectionTimeout / time.Second),
		ConnectionKeepAliveSeconds: int(state.connectionKeepAlive / time.Second),
		ConnectionReconnect:        state.connectionReconnect.String(),
		RestoreConnect:             state.restoreConnect.String(),
		ShareConnections:           state.shareConnections.String(),
		TerminalClipboard:          state.terminalClipboard.String(),
		TerminalCursorSteady:       state.terminalCursorSteady,
		TerminalCopyOnSelect:       state.terminalCopyOnSelect,
		TerminalRightClickPaste:    state.terminalRightClickPaste,
		TerminalTabTitle:           state.terminalTabTitle,
		TerminalShowLatency:        state.terminalShowLatency,
		TerminalToolbarPosition:    state.terminalToolbarPosition.String(),
		TerminalToolbarSize:        state.terminalToolbarSize.String(),
		TerminalBellOff:            state.terminalBellOff,
		TerminalLinkPlainClick:     state.terminalLinkPlainClick,
		AIPanelWidth:               state.ai.panelWidth,
		AIComposerHeight:           state.ai.composerHeight,
		MonitorPanelWidth:          state.monitorWidth,
		TerminalSchemeDark:         state.terminalSchemes[appearanceDark],
		TerminalSchemeLight:        state.terminalSchemes[appearanceLight],
		TerminalPadding:            state.terminalPad.String(),
		TerminalGutter:             state.terminalGutter.String(),
		TerminalIntense:            state.terminalIntense.String(),
		TerminalNotifications:      notifyString(state.notifyOff),
		TerminalPasteImage:         notifyString(state.pasteImageOff),
		CommandHistory:             state.history.on,
		CommandSuggestOff:          state.history.suggestOff,
		SessionLogsOff:             state.sessionLogsOff,
		SessionLogsView:            state.snapshotSessionLogsView(),
		CommandInlineSuggestOff:    state.history.inlineOff,
		SFTPParallel:               state.sftpParallel,
		SFTPKeepTimes:              notifyString(state.sftpKeepTimesOff),
		SFTPFollow:                 notifyString(state.sftpFollowOff),
		SFTPTerminalFollow:         state.sftpTerminalFollow,
		SFTPPathInput:              state.sftpPathInput,
		SFTPPanelAutoOpen:          state.sftpPanelAutoOpen,
		SFTPPanelSide:              sftpPanelSideString(state.sftpPanelSide),
		SFTPPanelWidth:             state.sftpPanelWidth,
		SFTPWindowOrder:            state.sftpWindowOrder.String(),
		SFTPOpenMode:               state.sftpOpenMode.String(),
		SFTPColumns:                state.sftpColumns.String(),
		SFTPDownloadDirectory:      savedSFTPDownloadDirectory(state.sftpDownloadDirectory),
		DefaultEditor:              string(state.defaultEditor),
		CustomEditorPath:           state.customEditorPath,
		Toolbars:                   state.toolbars.snapshot(),
		UpdateCheck:                updateCheckString(state.updateAtStart),
		CloseAction:                state.closeAction.String(),
		UpdateSkipped:              state.update.skipped,
		ShortcutVersion:            shortcutSchemaVersion,
		Shortcuts:                  make([]savedShortcut, 0, shortcutActionCount-1),
		Workspace:                  state.workspaceName(),
		HostCatalogVersion:         hostCatalogSchemaVersion,
		Hosts:                      make([]savedHost, 0, len(state.hosts)),
		HostGroups:                 make([]savedHostGroup, 0, len(state.hostGroups)),
		WorkspaceTemplates:         slices.Clone(state.templates),
		IdentityFiles:              make([]string, 0, len(state.identities)),
		Forwards:                   make([]savedForward, 0, len(state.forwards)),
		Proxies:                    make([]savedProxy, 0, len(state.proxies)),
		Sessions:                   make([]savedSession, 0, len(state.sessions)),
	}
	if state.localProxyPortSet {
		saved.LocalProxyPort = int(state.localProxyPort)
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
		if entry.Shortcut.valid() {
			saved.SnippetShortcuts = append(saved.SnippetShortcuts, savedSnippetKey{
				Name: entry.Name, Key: string([]byte{entry.Shortcut.Key}), Modifiers: uint8(entry.Shortcut.Modifiers),
			})
		}
	}
	if state.proxy.kind != sshconn.ProxyNone || state.proxy.bypass != "" {
		saved.OutboundProxy = &savedOutboundProxy{
			Kind: proxyKindString(state.proxy.kind), Address: state.proxy.address, User: state.proxy.user, Bypass: state.proxy.bypass,
		}
	}
	for action := shortcutAction(1); action < shortcutActionCount; action++ {
		chord := state.shortcuts.chords[action]
		saved.Shortcuts = append(saved.Shortcuts, savedShortcut{
			Action: action.String(), Key: chord.savedKey(), Modifiers: uint8(chord.Modifiers),
		})
	}
	for _, host := range state.hosts {
		saved.Hosts = append(saved.Hosts, savedHost{
			ID: host.CatalogID, Label: host.Label, Group: host.Group, Tags: splitHostTags(host), Icon: host.Icon.String(), Color: host.Color,
			Address: host.Address, User: host.User, Port: host.Port, System: host.System.String(),
			savedTransport: host.transport(),
		})
	}
	// A host that is gone takes its opening with it: a host added later at the
	// same address must not come back already open to agents.
	for id := range state.agentHosts {
		if !slices.ContainsFunc(state.hosts, func(host Host) bool { return host.isSSH() && host.CatalogID == id }) {
			delete(state.agentHosts, id)
		}
	}
	for _, host := range state.hosts {
		if state.agentHosts[host.CatalogID] {
			saved.AgentHosts = append(saved.AgentHosts, host.CatalogID)
		}
	}
	for _, bookmark := range state.sftpBookmarks {
		saved.SFTPBookmarks = append(saved.SFTPBookmarks, savedBookmark{Host: bookmark.host, Path: bookmark.path, Name: bookmark.name, Local: bookmark.local, Shared: bookmark.shared})
	}
	saved.SFTPLocalBookmarks = slices.Clone(state.sftpLocalBookmarks)
	for _, group := range state.hostGroups {
		saved.HostGroups = append(saved.HostGroups, savedHostGroup{Name: group.Name, Collapsed: !group.Expanded, Explicit: group.Explicit, Pinned: group.Pinned, Order: group.Order})
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
			JumpAddress: jump.Address, JumpUser: jump.User, JumpPort: jump.Port, Stopped: route.Stopped,
			ViaShell: route.ViaShell, RelayCommand: route.Relay,
		})
	}
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.containerCommand != "" {
			continue
		}
		if session.isLocal() {
			saved.Sessions = append(saved.Sessions, savedSession{Label: session.Title, Local: true, LocalDirectory: session.localDirectory, LocalShell: session.host.localShell})
			if session.ID == state.activeSessionID {
				saved.ActiveSession = len(saved.Sessions)
			}
			continue
		}
		saved.Sessions = append(saved.Sessions, savedSession{
			Label: session.Title, Address: session.host.Address, User: session.host.User,
			Port: session.host.Port, System: session.host.System.String(), SavedHost: session.HostID != 0,
			savedTransport: session.host.transport(),
		})
		if session.ID == state.activeSessionID {
			saved.ActiveSession = len(saved.Sessions)
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
	saved.WorkspaceLayouts = state.savedWorkspaceLayouts()
	return saved
}

// save persists the state. Callers that need to distinguish a write from a
// skipped save use saveResult.
func (state *State) save() {
	_ = state.saveWithResult()
}

// saveResult reports whether the snapshot reached the store.
func (state *State) saveResult() bool {
	return state.saveWithResult() == nil
}

// saveWithResult reports why a snapshot did not reach the store, so a caller
// that staged something beside it can put that back.
func (state *State) saveWithResult() error {
	return state.saveWithResultMode(true)
}

// saveWithResultMode reports whether the snapshot reached the store. A failure
// is reported in the status line instead of interrupting what the user is
// doing; a state that started from a file of a newer build never saves, so that
// file stays as it is and the notice comes back on each change. flushSync is
// false for a caller that schedules the sync itself.
func (state *State) saveWithResultMode(flushSync bool) (saveErr error) {
	defer func() {
		if flushSync && saveErr == nil {
			state.sshSyncFlush()
		}
	}()
	previous := state.status
	err := state.persistSnapshot(flushSync)
	if err == nil || !errors.Is(err, configstore.ErrConflict) || state.saveRetrying {
		return err
	}
	// Another window changed what this save was built on. Its shared records win,
	// as the conflict said; this window's device-local changes — its open
	// sessions, its settings, the time it just recorded — still go in, so a
	// window that read the store a moment ago is not refused until it restarts.
	state.saveRetrying = true
	reloadErr := state.refreshShared()
	state.saveRetrying = false
	if reloadErr != nil {
		return err
	}
	if _, local, err := state.config.Load(); err == nil {
		state.configBase.local = local[localKey]
	}
	snapshot := state.snapshot()
	entries, local, err := splitState(snapshot, state.configBase, state.configDefaults)
	if err != nil {
		return err
	}
	localValues, err := routeLocalState(state.configBase, local)
	if err != nil {
		return err
	}
	// The refreshed base already holds every shared record, so this writes the
	// local document alone and keeps the base it was built on.
	if _, err := writeEntries(state.config, state.configBase, entries, localValues); err != nil {
		state.status = fmt.Sprintf(state.messages.StatusSaveFailedFmt, err)
		return err
	}
	state.lastSaved, state.hasSaved = snapshot, true
	state.status = previous
	return nil
}

// persistSnapshot writes what changed since the last save and reports why it
// could not be written.
func (state *State) persistSnapshot(flushSync bool) error {
	if state.saveBlocked {
		if state.modal == modalNone {
			state.modal = modalStateNotice
		}
		return errors.New("configuration was written by a newer build")
	}
	if state.config == nil || state.pauseSave > 0 {
		return errors.New("configuration store is unavailable")
	}
	snapshot := state.snapshot()
	// Many actions save after changing only run-local UI state. Comparing the
	// snapshot to the last saved one is tens of microseconds, where encoding and
	// diffing it costs about a millisecond and 700 KB of garbage.
	if state.hasSaved {
		changes := changedSavedFields(state.lastSaved, snapshot)
		if len(changes) == 0 {
			return nil
		}
		shared, handled, err := writeChangedFields(state.config, state.configBase, state.configDefaults, changes)
		if handled {
			if err != nil {
				state.status = fmt.Sprintf(state.messages.StatusSaveFailedFmt, err)
				return err
			}
			state.lastSaved, state.hasSaved = snapshot, true
			if shared && flushSync {
				state.scheduleSync()
			}
			return nil
		}
	}
	shared, err := writeConfig(state.config, state.configBase, state.configDefaults, snapshot)
	if err != nil {
		state.status = fmt.Sprintf(state.messages.StatusSaveFailedFmt, err)
		return err
	}
	state.lastSaved, state.hasSaved = snapshot, true
	if shared && flushSync {
		state.scheduleSync()
	}
	return nil
}

// restore applies a loaded state at start: its settings and catalog, then the
// sessions and the layout it left open.
func (state *State) restore(saved savedState) {
	state.restoreSettings(saved)
	state.stopAppearanceWatch = state.options.WatchSystemAppearance(state.systemAppearanceChanged)
	sourceSessions := state.restoreSessions(saved.Sessions, saved.ActiveSession)
	state.restoreWorkspaceDeck(saved.Deck, sourceSessions)
	state.restoreWorkspaceLayouts(saved.WorkspaceLayouts)
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
	state.restoreDashboard(saved.MonitorPanels, saved.MonitorActive)
	state.backgroundOutputAlerts = saved.BackgroundOutputAlerts
	state.setTabOrientation(tabOrientationFromString(saved.TabOrientation))
	state.setVerticalTabsAutoHide(saved.VerticalTabsAutoHide)
	state.setVerticalTabsGrouping(saved.VerticalTabsGrouping)
	state.restoreSFTPLocalFavorites(saved.SFTPLocalBookmarks)
	state.toolbars.restore(saved.Toolbars)
	state.terminalToolbarPosition = terminalToolbarPositionFromString(saved.TerminalToolbarPosition)
	state.terminalToolbarSize = terminalToolbarSizeFromString(saved.TerminalToolbarSize)
	state.localShell = saved.LocalShell
	state.recentConnectSort = saved.RecentConnectSort
	state.recentConnections = maps.Clone(saved.RecentConnections)
	state.sessionLogsOff = saved.SessionLogsOff
	state.restoreSessionLogsView(saved.SessionLogsView)
	state.localProxyPort = defaultLocalProxyPort
	state.localProxyPortSet = false
	if saved.LocalProxyPort >= 1 && saved.LocalProxyPort <= 65535 {
		state.localProxyPort = uint16(saved.LocalProxyPort)
		state.localProxyPortSet = true
	}
	// The saved editor is read as it is: a value this build does not know stays
	// unknown rather than becoming another editor.
	state.defaultEditor = ExternalEditor(strings.TrimSpace(saved.DefaultEditor))
	state.sshAgent = agentChoiceFromSaved(saved.SSHAgent, saved.SSHAgentPath)
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
	state.networkProxy = networkProxyModeFromString(saved.NetworkProxy)
	state.syncNativeAppearance()
	// The theme setAppearance builds copies the metrics by value, so the saved
	// text scale must be in effect before it runs.
	state.textScale = textScaleFromString(saved.TextScale)
	applyTextMetrics(state.textScale.factor())
	state.setAppearance(state.resolveAppearance())
	state.setTerminalFontSize(float32(saved.TerminalFontSize))
	// Only the choice: what is in effect is decided once the window starts, when
	// the platform has been asked what it can follow (validateInterfaceFont).
	state.uiFont.family = strings.TrimSpace(saved.InterfaceFont)
	state.fonts.family, state.fonts.classic = "", terminalFontFaceFromString(saved.TerminalFontFace) == ui.MonoClassic
	if family := strings.TrimSpace(saved.TerminalFontFamily); family != "" && ui.MonoFamily(family) != ui.MonoSystem {
		state.fonts.family, state.fonts.classic = family, false
	}
	// Kept as saved, whatever this computer has installed: a copy carried to another
	// computer comes back with the choice it was given.
	state.fonts.fallbacks = cleanFallbackFonts(saved.TerminalFontFallbacks, state.fonts.family)
	state.applyTerminalFace()
	state.restoreConnect = restoreConnectPolicyFromString(saved.RestoreConnect)
	state.shareConnections = shareConnectionsPolicyFromString(saved.ShareConnections)
	state.ai.panelWidth = saved.AIPanelWidth
	if saved.AIComposerHeight > 0 {
		state.ai.composerHeight = saved.AIComposerHeight
	}
	if saved.MonitorPanelWidth >= layoutcore.MinMonitorWidth && saved.MonitorPanelWidth <= layoutcore.MaxMonitorWidth {
		state.monitorWidth = saved.MonitorPanelWidth
	}
	if saved.SFTPPanelWidth >= layoutcore.MinContextWidth && saved.SFTPPanelWidth <= layoutcore.MaxContextWidth {
		state.sftpPanelWidth = saved.SFTPPanelWidth
	}
	state.restoreIdentities(saved.IdentityFiles)
	state.agentHosts = agentHostSet(saved.AgentHosts)
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
	state.connectionTimeout, state.connectionKeepAlive = connectionTimes(saved)
	state.connectionReconnect = reconnectPolicyFromString(saved.ConnectionReconnect)
	state.terminalClipboard = terminalClipboardPolicyFromString(saved.TerminalClipboard)
	state.terminalIntense = terminalIntenseFromString(saved.TerminalIntense)
	state.terminalCursorSteady = saved.TerminalCursorSteady
	state.terminalCopyOnSelect = saved.TerminalCopyOnSelect
	state.terminalRightClickPaste = saved.TerminalRightClickPaste
	state.terminalTabTitle = saved.TerminalTabTitle
	state.terminalShowLatency = saved.TerminalShowLatency
	state.terminalBellOff = saved.TerminalBellOff
	state.terminalLinkPlainClick = saved.TerminalLinkPlainClick
	if saved.TerminalSchemeDark == "" && saved.TerminalSchemeLight == "" && saved.TerminalScheme != "" {
		state.terminalSchemes[appearanceDark], state.terminalSchemes[appearanceLight] = migrateTerminalScheme(saved.TerminalScheme)
	} else {
		state.terminalSchemes[appearanceDark] = normalizeSchemeID(saved.TerminalSchemeDark)
		state.terminalSchemes[appearanceLight] = normalizeSchemeID(saved.TerminalSchemeLight)
	}
	state.terminalPad = terminalPadFromString(saved.TerminalPadding)
	state.terminalGutter = terminalGutterFromString(saved.TerminalGutter)
	state.notifyOff = saved.TerminalNotifications == "off"
	state.pasteImageOff = saved.TerminalPasteImage == "off"
	state.history.on = saved.CommandHistory
	state.history.suggestOff = saved.CommandSuggestOff
	state.history.inlineOff = saved.CommandInlineSuggestOff
	state.restoreSFTPSettings(saved)
	state.proxy = decodeOutboundProxy(saved.OutboundProxy)
	state.restoreTerminalTools(saved)
	state.restoreAutofill(saved)

	// A host keeps the number the sessions and the selection know it by.
	known := make(map[string]uint32, len(state.hosts))
	for _, host := range state.hosts {
		if host.CatalogID != "" {
			known[host.CatalogID] = host.ID
		}
	}
	clear(state.hosts)
	clear(state.hostGroups)
	state.hosts, state.nextHostID = decodeHosts(saved.Hosts, known, state.nextHostID)
	state.templates = state.templates[:0]
	for _, template := range saved.WorkspaceTemplates {
		if normalized, err := normalizeWorkspaceTemplate(template); err == nil {
			state.templates = append(state.templates, normalized)
		}
	}
	state.hostGroups = state.hostGroups[:0]
	for _, entry := range saved.HostGroups {
		name := strings.TrimSpace(entry.Name)
		if state.hostGroupIndex(name) >= 0 {
			continue
		}
		state.hostGroups = append(state.hostGroups, hostGroup{Name: name, Expanded: !entry.Collapsed, Explicit: entry.Explicit, Pinned: entry.Pinned, Order: max(0, entry.Order)})
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
	case sectionWorkspaces:
		return "workspaces"
	case sectionDashboard:
		return "monitor"
	case sectionSessionLogs:
		return "session-logs"
	case sectionTerminal:
		return "terminal"
	case sectionKeys:
		return "keys"
	case sectionForwards:
		return "forwards"
	case sectionProxies:
		return "proxies"
	case sectionSnippets:
		return "snippets"
	case sectionSettings:
		if state.settingsCategory == settingsShortcuts {
			return "shortcuts"
		}
		return "settings"
	default:
		return "hosts"
	}
}

// restoreShortcuts applies the bindings the file holds, then fills what the
// file does not mention with the defaults. The file wins on purpose: a chord
// saved for one action is not taken by the default of an action a later build
// added. Such an action stays unbound, reading "—" in the settings until it is
// given a chord, rather than quietly moving a shortcut the user chose. An entry
// with no key is an action the user removed: it stays unbound instead of taking
// its default back.
func restoreShortcuts(saved []savedShortcut) shortcutMap {
	defaults := defaultShortcuts()
	var shortcuts shortcutMap
	var specified [shortcutActionCount]bool
	for _, entry := range saved {
		action := shortcutActionFromString(entry.Action)
		if action == shortcutNone {
			continue
		}
		if entry.Key == "" {
			shortcuts.unbind(action)
			specified[action] = true
			continue
		}
		chord, ok := chordFromSaved(ui.Modifiers(entry.Modifiers), entry.Key)
		if !ok {
			continue
		}
		if _, bound := shortcuts.bind(action, chord); bound {
			specified[action] = true
		}
	}
	for action := shortcutAction(1); action < shortcutActionCount; action++ {
		if specified[action] || shortcuts.chords[action].valid() {
			continue
		}
		shortcuts.bind(action, defaults.chords[action])
	}
	shortcuts.refreshLabels()
	return shortcuts
}

func (state *State) restoreWorkspace(workspace string) {
	if state.devRestoreWorkspace(workspace) {
		return
	}
	switch workspace {
	case "workspaces":
		state.activeSection = sectionWorkspaces
	case "monitor":
		state.activeSection = sectionDashboard
	case "session-logs":
		state.activeSection = sectionSessionLogs
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
	case "snippets":
		state.activeSection = sectionSnippets
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
	state.proxies = append(state.proxies[:0], decodeProxies(saved, state.hosts)...)
	state.selectedProxyID = 0
	state.nextProxyID = uint32(len(state.proxies)) + 1
	if len(state.proxies) > 0 {
		state.selectedProxyID = state.proxies[0].ID
	}
	state.refreshProxies()
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
		title := entry.Label
		if title == "" {
			title = host.Label
		}
		state.sessions = append(state.sessions, Session{
			ID: state.nextSessionID, HostID: host.ID, Title: state.uniqueSessionTitle(title, 0),
			Address: state.sessionAddress(host),
			host:    host, phase: sshconn.PhaseClosed, restored: true, paneSlot: state.claimPaneSlot(),
		})
		session := &state.sessions[len(state.sessions)-1]
		if entry.Local && validLocalDirectory(entry.LocalDirectory) {
			session.localDirectory = filepath.Clean(entry.LocalDirectory)
		}
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
		host := localHost(state.messages.LocalTerminalTitle)
		if validLocalShell(entry.LocalShell) {
			host.localShell = entry.LocalShell
		}
		return host, true
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
