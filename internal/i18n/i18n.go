// Package i18n holds the workbench string catalog. Language selection is
// plain data: the state carries one Messages value and every paint reads
// from it, so switching languages is one assignment plus a repaint.
package i18n

// Language selects one catalog entry.
type Language uint8

const (
	English Language = iota
	Chinese
)

// String returns the BCP-ish tag for persistence or debugging.
func (language Language) String() string {
	switch language {
	case Chinese:
		return "zh-CN"
	default:
		return "en"
	}
}

// Messages is the complete set of user-visible workbench strings. Adding a
// language means adding one field initialization, not touching call sites.
type Messages struct {
	// AIPanel and AISettings hold the assistant's texts; see ai_panel.go and
	// ai_settings.go.
	AIPanel    AIPanelMessages
	AISettings AISettingsMessages
	// Tools holds the terminal tools' texts; see terminal_tools.go.
	Tools ToolsMessages
	// Autofill holds the autofill rules' texts; see autofill.go.
	Autofill AutofillMessages
	// History holds the command history's texts; see history.go.
	History HistoryMessages
	// Desktop holds the remote-desktop tabs' texts; see desktop.go.
	Desktop DesktopMessages

	// Window and sections.

	// Navigation.
	NavHosts                  string
	NavKeychain               string
	NavProxies                string
	NavPortForwarding         string
	NavKnownHosts             string
	NavSettings               string
	NavDebug                  string
	NavSearch                 string
	SearchPlaceholder         string
	CommandPaletteTitle       string
	CommandPalettePlaceholder string
	CommandPaletteEmpty       string
	CommandSwitchSessionFmt   string
	NewWindow                 string

	// Debug workspace: a preview surface for the framework's components.
	DebugTitle                  string
	DebugDescription            string
	DebugComponents             string
	DebugButtonGroupTitle       string
	DebugButtonGroupDescription string
	DebugButtonGroupChoice      string
	DebugAlignLeft              string
	DebugAlignCenter            string
	DebugAlignRight             string
	DebugSelectTitle            string
	DebugSelectDescription      string
	DebugSelectChoice           string
	DebugEnvironmentDevelopment string
	DebugEnvironmentStaging     string
	DebugEnvironmentProduction  string

	// What the user is told when the saved state could not be loaded as it was.
	// The Fmt strings take the file names or versions named in persist.go.
	StateTooNewTitle       string
	StateTooNewBodyFmt     string
	StateUnreadableTitle   string
	StateUnreadableBodyFmt string
	StateNoticeOK          string

	// The status-area icon and the panel beside it. TrayTooltipFmt takes the
	// number of open sessions.
	TrayTitle         string
	TrayEmpty         string
	TrayQuit          string
	TrayLocalTerminal string
	TrayShowWindow    string
	TrayTooltipFmt    string
	TrayTooltipEmpty  string

	// Settings workspace.
	SettingsTitle                          string
	SettingsDescription                    string
	SettingsLanguage                       string
	SettingsLanguageEnglish                string
	SettingsLanguageChinese                string
	SettingsAppearance                     string
	SettingsAppearanceSystem               string
	SettingsAppearanceDark                 string
	SettingsAppearanceLight                string
	SettingsTerminalOption                 string
	SettingsTerminalOptionDescription      string
	SettingsTerminalOptionText             string
	SettingsTerminalOptionMeta             string
	SettingsTerminalFontSize               string
	SettingsTerminalFontSizeDescription    string
	SettingsTerminalFontFamily             string
	SettingsTerminalFontSystem             string
	SettingsTerminalFontClassic            string
	SettingsTerminalScrollback             string
	SettingsTerminalScrollbackDescription  string
	SettingsConnectionTimeout              string
	SettingsConnectionTimeoutDescription   string
	SettingsConnectionKeepAlive            string
	SettingsConnectionKeepAliveDescription string
	SettingsConnectionKeepAliveOff         string
	SettingsConnectionReconnect            string
	SettingsConnectionReconnectDescription string
	SettingsConnectionReconnectOff         string
	SettingsConnectionReconnectThree       string
	SettingsConnectionReconnectForever     string
	SettingsShare                          string
	SettingsShareDescription               string
	ConnShareRefusedFmt                    string
	PasswordSave                           string
	KeyPassphraseSave                      string
	PasswordSavedRefused                   string
	SettingsGroupSecurity                  string
	SettingsVault                          string
	SettingsVaultDescriptionFmt            string
	SettingsVaultDescriptionLockedFmt      string
	SettingsVaultEmpty                     string
	SettingsVaultForgetAll                 string
	SettingsMasterPassword                 string
	SettingsMasterPasswordDescription      string
	SettingsMasterPasswordSet              string
	SettingsMasterPasswordChange           string
	SettingsMasterPasswordRemove           string
	SettingsMasterPasswordLock             string
	SettingsMasterPasswordUnlock           string
	VaultUnlockTitle                       string
	VaultUnlockLabel                       string
	VaultUnlockButton                      string
	VaultWrongPassword                     string
	MasterPasswordTitleSet                 string
	MasterPasswordTitleChange              string
	MasterPasswordDescription              string
	MasterPasswordNewLabel                 string
	MasterPasswordConfirmLabel             string
	MasterPasswordSave                     string
	MasterPasswordTooShortFmt              string
	MasterPasswordMismatch                 string
	VaultForgetTitle                       string
	VaultForgetDescription                 string
	VaultForgetConfirm                     string
	VaultSavedFmt                          string
	VaultSaveFailedFmt                     string
	VaultKeyLost                           string
	VaultUnavailable                       string
	VaultCorruptFmt                        string
	VaultMasterSet                         string
	VaultMasterRemoved                     string
	VaultCleared                           string
	SettingsRestore                        string
	SettingsRestoreDescription             string
	SettingsRestoreOff                     string
	SettingsRestoreActive                  string
	SettingsRestoreAll                     string
	SessionRestoreNeedsYou                 string
	TipSFTPClose                           string
	TipFiles                               string
	TipFilesLocal                          string
	SFTPInsertPath                         string
	MenuCopy                               string
	MenuPaste                              string
	MenuSelectAll                          string
	MenuClearScrollback                    string
	MenuResourceMonitor                    string
	LocalTerminal                          string
	LocalTerminalTitle                     string

	TipStatsStart                        string
	TipStatsStop                         string
	TipStatsRetry                        string
	TipRecordStart                       string
	TipRecordStop                        string
	SettingsTerminalClipboard            string
	SettingsTerminalClipboardDescription string
	SettingsTerminalClipboardBlock       string
	SettingsTerminalClipboardAsk         string
	SettingsTerminalClipboardAllow       string
	SettingsShortcuts                    string
	ShortcutsReset                       string
	ShortcutsCaptureTitle                string
	ShortcutsCaptureDescription          string
	ShortcutsConflictFmt                 string
	ShortcutsInvalid                     string
	ShortcutCommandPalette               string
	ShortcutFind                         string
	ShortcutCopy                         string
	ShortcutPaste                        string
	ShortcutNewTab                       string
	ShortcutCloseTab                     string
	RecordingStart                       string
	RecordingStop                        string
	RecordingExport                      string
	RecordingStarted                     string
	RecordingStopped                     string
	RecordingExportedFmt                 string
	RecordingExportFailedFmt             string

	// Toolbar and host table.
	NewHost       string
	EditHost      string
	CopyHost      string
	DeleteHost    string
	MoveHostGroup string
	ExportHosts   string
	ImportCatalog string
	ImportMenu    string
	Connect       string
	ColumnName    string
	HostsEmpty    string
	HostsEmptySub string

	// OpenSSH config import.
	SSHConfigImport             string
	SSHConfigPickerTitle        string
	SSHConfigImportCanceled     string
	SSHConfigImportFailedFmt    string
	SSHConfigResultTitle        string
	SSHConfigSummaryFmt         string
	SSHConfigWarningsFmt        string
	SSHConfigNoWarnings         string
	SSHConfigDone               string
	SSHConfigWarningSourceFmt   string
	SSHConfigWarningHostFmt     string
	SSHConfigWarningIdentityFmt string
	SSHConfigWarningProxyFmt    string
	SSHConfigWarningMoreFmt     string

	// New Host dialog.
	DialogTitle                 string
	EditHostTitle               string
	CopyHostTitle               string
	FieldLabel                  string
	FieldLabelPlaceholder       string
	FieldAddress                string
	FieldAddressPlaceholder     string
	FieldUser                   string
	FieldUserPlaceholder        string
	FieldGroup                  string
	FieldGroupPlaceholder       string
	FieldPort                   string
	FieldTags                   string
	FieldTagsPlaceholder        string
	FieldIcon                   string
	IconAuto                    string
	IconServer                  string
	IconTerminal                string
	IconGlobe                   string
	IconKey                     string
	IconDesktop                 string
	ErrLabelRequired            string
	ErrGroupRequired            string
	ErrAddressTaken             string
	ErrPortInvalid              string
	ErrHostTagsInvalid          string
	Cancel                      string
	AddHost                     string
	SaveHost                    string
	DeleteHostTitle             string
	DeleteHostDescriptionFmt    string
	DeleteHostConfirm           string
	HostDeleteBlockedProxyFmt   string
	HostDeleteBlockedForwardFmt string
	HostDeleteBlockedSessionFmt string
	HostDeletedFmt              string
	HostUpdated                 string
	HostCopied                  string
	HostCopyLabelFmt            string
	HostSelectionFmt            string
	MoveGroupNone               string
	MoveGroupNew                string
	MoveGroupTitle              string
	MoveGroupField              string
	MoveGroupSubmit             string
	HostMovedFmt                string
	HostFilterAll               string
	HostFilterMore              string
	HostGroupDefault            string
	HostCatalogPickerTitle      string
	HostCatalogImportFailedFmt  string
	HostCatalogImportedFmt      string
	HostCatalogExportedFmt      string
	HostCatalogExportFailedFmt  string

	// Shown when the search box matches no saved host.
	NoMatches    string
	NoMatchesSub string

	// ErrBadTargetFmt reports typed text that is not a connection target; it
	// takes the text.
	ErrBadTargetFmt string

	// Per-host connection state shown in the host table.
	HostStateIdle       string
	HostStateConnecting string
	HostStateConnected  string
	HostStateFailed     string

	// Accessible names of the window-bar buttons and tabs. LabelTerminalFmt
	// names a terminal and takes its address.
	LabelLanguage           string
	LabelThemeToLight       string
	LabelThemeToDark        string
	LabelNewTab             string
	NewTabConnectTo         string
	MenuCloseOtherTabs      string
	MenuCloseTabsToRight    string
	MenuDuplicateTab        string
	MenuRenameTab           string
	CloseTabsTitleFmt       string
	CloseTabsDescription    string
	CloseTabsConfirm        string
	RenameTabTitle          string
	RenameTabField          string
	RenameTabPlaceholder    string
	RenameTabSubmit         string
	ErrTabNameRequired      string
	NewTabAllHosts          string
	NewTabPickerPlaceholder string
	NewTabPickerEmpty       string
	LabelCloseTab           string
	LabelTerminalFmt        string
	// Names of the window's own minimize, maximize, restore and close buttons,
	// which Viva draws on Windows and Linux.
	LabelWindowMinimize string
	LabelWindowMaximize string
	LabelWindowRestore  string
	LabelWindowClose    string
	PanePrevious        string
	PaneNext            string
	PaneSplitHorizontal string
	PaneSplitVertical   string
	PaneClose           string
	PaneResize          string

	// Saved-state problems, reported once at startup or after a failed save.
	StatusSaveFailedFmt          string
	StatusNewWindowFailedFmt     string
	StatusOpenURLFailedFmt       string
	StatusClipboardBlocked       string
	StatusClipboardCopied        string
	StatusClipboardFailedFmt     string
	StatusClipboardReadFailedFmt string

	// Connection and session failures. Fmt fields take one %s.
	ConnShellFailedFmt    string
	SessionEnded          string
	SessionEndedStatusFmt string
	SessionRestoredTitle  string
	SessionRestoredBody   string
	SessionReconnect      string
	SessionReconnectHint  string
	ConnClosed            string
	ConnCanceled          string
	ConnRejected          string
	ConnFailedFmt         string
	ConnAuthFailed        string
	ConnSkippedKeyFmt     string
	ConnChangedTitle      string
	ConnChangedBodyFmt    string
	ConnChangedTrustedFmt string
	ConnChangedPresented  string
	ConnChangedHintFmt    string

	// Host key and password prompts.
	HostKeyTitle                   string
	HostKeyDescriptionFmt          string
	HostKeyType                    string
	HostKeyFingerprint             string
	HostKeyTrust                   string
	HostKeyOnce                    string
	HostKeyReject                  string
	PasswordTitle                  string
	PasswordDescription            string
	PasswordLabel                  string
	PasswordHint                   string
	PasswordRetry                  string
	StatusConnectedFmt             string
	StatusConnectFailed            string
	ClipboardRequestTitle          string
	ClipboardRequestDescriptionFmt string
	ClipboardAllowOnce             string

	// Status bar.
	StatusReadyIdle    string
	StatusSelected     string // prefix: host name follows
	StatusOpened       string // prefix: session title follows
	StatusReused       string // prefix: session title follows
	StatusHostAdded    string
	StatusEditCanceled string

	// OpenSSH known_hosts management.
	KnownHostsTitle                 string
	KnownHostsDescription           string
	KnownHostsSearchPlaceholder     string
	KnownHostsRefresh               string
	KnownHostsDelete                string
	KnownHostsRetrust               string
	KnownHostsColumnLine            string
	KnownHostsColumnHosts           string
	KnownHostsColumnFlags           string
	KnownHostsColumnType            string
	KnownHostsColumnFingerprint     string
	KnownHostsColumnComment         string
	KnownHostsEmpty                 string
	KnownHostsEmptySub              string
	KnownHostsNoMatches             string
	KnownHostsNoMatchesSub          string
	KnownHostsError                 string
	KnownHostsCountFmt              string
	KnownHostsFilteredCountFmt      string
	KnownHostsSelectedFmt           string
	KnownHostsPageFmt               string
	KnownHostsPrevious              string
	KnownHostsNext                  string
	KnownHostsLoadedFmt             string
	KnownHostsLoadFailedFmt         string
	KnownHostsDeleteTitle           string
	KnownHostsDeleteDescriptionFmt  string
	KnownHostsDeleteConfirm         string
	KnownHostsDeleteFailedFmt       string
	KnownHostsDeletedFmt            string
	KnownHostsRetrustTitle          string
	KnownHostsRetrustDescriptionFmt string
	KnownHostsRetrustConfirm        string
	KnownHostsRetrustedFmt          string
	KnownHostsVerifyWarning         string

	// SSH identity management and private-key passphrase prompts.
	KeyTitle                    string
	KeyDescription              string
	KeyImport                   string
	KeyRemove                   string
	KeyColumnName               string
	KeyColumnType               string
	KeyColumnProtection         string
	KeyColumnFingerprint        string
	KeyEmpty                    string
	KeyEmptySub                 string
	KeyCountFmt                 string
	KeyUnavailable              string
	KeyUnencrypted              string
	KeyPassphraseProtected      string
	KeyEncryptedUnknown         string
	KeyImportTitle              string
	KeyImportCanceled           string
	KeyImportFailedFmt          string
	KeyInvalidFmt               string
	KeyImportedFmt              string
	KeyNothingImported          string
	KeySelected                 string
	KeyRemoved                  string
	KeyPassphraseTitle          string
	KeyPassphraseDescriptionFmt string
	KeyPassphraseLabel          string
	KeyPassphraseHint           string
	KeyPassphraseRetry          string
	KeyPassphraseSkip           string
	KeyUnlock                   string

	// Local SSH port forwarding.
	ForwardTitle                  string
	ForwardDescription            string
	ForwardAdd                    string
	ForwardRemove                 string
	ForwardStart                  string
	ForwardStop                   string
	ForwardColumnName             string
	ForwardColumnHost             string
	ForwardColumnListener         string
	ForwardColumnDestination      string
	ForwardColumnStatus           string
	ForwardEmpty                  string
	ForwardEmptySub               string
	ForwardCountFmt               string
	ForwardStopped                string
	ForwardStoppedError           string
	ForwardWaiting                string
	ForwardStarting               string
	ForwardRunningFmt             string
	ForwardRunningError           string
	ForwardTrafficFmt             string
	ForwardHostUnavailable        string
	ForwardConnectionClosed       string
	ForwardSelected               string
	ForwardAdded                  string
	ForwardRemoved                string
	ForwardStoppedStatus          string
	ForwardConnectingFmt          string
	ForwardStartingFmt            string
	ForwardStartedFmt             string
	ForwardStartFailedFmt         string
	ForwardDialogTitle            string
	ForwardDialogDescription      string
	ForwardFieldName              string
	ForwardFieldHost              string
	ForwardFieldType              string
	ForwardModeLocal              string
	ForwardModeRemote             string
	ForwardModeSOCKS5             string
	ForwardModeHTTPConnect        string
	ForwardSOCKS5Hint             string
	ForwardHTTPConnectHint        string
	ForwardFieldLocalPort         string
	ForwardFieldRemotePort        string
	ForwardFieldDestination       string
	ForwardFieldDestinationPort   string
	ForwardDestinationPlaceholder string
	ForwardLocalDestinationHint   string
	ForwardRemoteListenerFmt      string
	ForwardCreate                 string
	ErrForwardNameRequired        string
	ErrForwardHostRequired        string
	ErrForwardPortInvalid         string
	ErrForwardDestinationRequired string
	ErrForwardNameTaken           string
	ErrForwardLocalTaken          string
	ErrForwardRemoteTaken         string

	// SSH jump-host routing.
	ProxyTitle                 string
	ProxyDescription           string
	ProxyAdd                   string
	ProxyRemove                string
	ProxyColumnTarget          string
	ProxyColumnJump            string
	ProxyColumnRoute           string
	ProxyEmpty                 string
	ProxyEmptySub              string
	ProxyCountFmt              string
	ProxySelected              string
	ProxyAdded                 string
	ProxyRemoved               string
	ProxyDialogTitle           string
	ProxyFieldTarget           string
	ProxyFieldJump             string
	ProxyCreate                string
	ErrProxyHostsRequired      string
	ErrProxyTargetRequired     string
	ErrProxyJumpRequired       string
	ErrProxySameHost           string
	ErrProxyCycle              string
	ErrProxyTargetTaken        string
	ConnDetailConnectingViaFmt string

	// SFTP remote-file page.
	SFTPFiles                   string
	SFTPUp                      string
	SFTPRefresh                 string
	SFTPDownload                string
	SFTPUpload                  string
	SFTPUploadDirectory         string
	SFTPNewDirectory            string
	SFTPOpen                    string
	SFTPRename                  string
	SFTPDelete                  string
	SFTPPermissions             string
	SFTPCancelTransfer          string
	SFTPLoading                 string
	SFTPUnavailable             string
	SFTPCanceling               string
	SFTPCanceled                string
	SFTPSaveTitle               string
	SFTPSaveCanceled            string
	SFTPEmpty                   string
	SFTPErrorFmt                string
	SFTPItemsFmt                string
	SCPItemsFmt                 string
	SFTPDownloadedFmt           string
	SFTPUploadTitle             string
	SFTPUploadCanceled          string
	SFTPUploadedFmt             string
	SFTPUploadDirectoryTitle    string
	SFTPDownloadDirectoryTitle  string
	SFTPTransferQueue           string
	SFTPQueuedFmt               string
	SFTPProgressFmt             string
	SFTPTransferDoneFmt         string
	SFTPTransferFailedFmt       string
	SFTPRetryTransfer           string
	SFTPOverwriteTitle          string
	SFTPOverwriteDescriptionFmt string
	SFTPOverwriteConfirm        string
	SFTPNewDirectoryTitle       string
	SFTPDirectoryName           string
	SFTPCreateDirectory         string
	SFTPErrDirectoryRequired    string
	SFTPCreatingDirectoryFmt    string
	SFTPDirectoryCreatedFmt     string
	SFTPRenameTitle             string
	SFTPRenameDescription       string
	SFTPDestinationPath         string
	SFTPRenameSubmit            string
	SFTPErrDestinationRequired  string
	SFTPRenamingFmt             string
	SFTPRenamedFmt              string
	SFTPDeleteFileTitle         string
	SFTPDeleteDirectoryTitle    string
	SFTPDeleteDescriptionFmt    string
	SFTPDeleteConfirm           string
	SFTPDeletingFmt             string
	SFTPDeletedFmt              string
	SFTPPermissionsTitle        string
	SFTPPermissionsDescription  string
	SFTPPermissionsMode         string
	SFTPPermissionsSubmit       string
	SFTPErrModeInvalid          string
	SFTPChangingPermissionsFmt  string
	SFTPPermissionsChangedFmt   string
	SFTPEditOpeningFmt          string
	SFTPEditBack                string
	SFTPEditSave                string
	SFTPEditReload              string
	SFTPEditLabel               string
	SFTPEditSafety              string
	SFTPEditReady               string
	SFTPEditDirty               string
	SFTPEditSaving              string
	SFTPEditSaved               string
	SFTPEditReloading           string
	SFTPEditConflict            string
	SFTPEditDiscardTitle        string
	SFTPEditDiscardDescription  string
	SFTPEditDiscardConfirm      string
	SFTPEditReloadTitle         string
	SFTPEditReloadDescription   string
	SFTPEditConflictTitle       string
	SFTPEditConflictDescription string
	SFTPEditReloadConfirm       string

	// Opt-in remote resource monitoring in the terminal info bar.
	StatsMeasuring   string
	StatsUnsupported string
	StatsUnavailable string

	// Terminal find bar.
	TerminalFindLabel        string
	TerminalFindPlaceholder  string
	TerminalFindPrevious     string
	TerminalFindNext         string
	TerminalFindClose        string
	TerminalFindNoMatches    string
	TerminalFindCountFmt     string
	TerminalFindTruncatedFmt string

	// The connection page shown until a shell speaks: one row per stage, each
	// with a line saying what is happening in it now.
	ConnStepConnect            string
	ConnStepHostKey            string
	ConnStepLogin              string
	ConnStepShell              string
	ConnDetailDialing          string
	ConnDetailDialingViaFmt    string
	ConnDetailJumpDialingFmt   string
	ConnDetailJumpKeyFmt       string
	ConnDetailJumpLoginFmt     string
	ConnDetailJumpReadyFmt     string
	ConnDetailServerFmt        string
	ConnDetailKnownHost        string
	ConnDetailKeyAskFmt        string
	ConnDetailAgent            string
	ConnDetailKeyPassphraseFmt string
	ConnDetailPasswordAsk      string
	ConnDetailMethodFmt        string
	ConnDetailShellOpening     string
	ConnDetailShellWaiting     string
	ConnNeedsYou               string
	ConnElapsedFmt             string
	ConnShowLog                string
	ConnShowProgress           string
	ConnClose                  string
	ConnRetry                  string
	ConnCancelReconnect        string
	ConnExportLog              string
	ConnReconnectWaitingFmt    string
	ConnReconnectTryingFmt     string
	ConnReconnectStoppedFmt    string
	ConnReconnectCanceled      string
	ConnExportedFmt            string
	ConnExportFailedFmt        string
	ConnChangedShort           string
}

// English catalog.
var EnglishMessages = Messages{

	NavHosts:                  "Hosts",
	NavKeychain:               "Keychain",
	NavProxies:                "Proxies",
	NavPortForwarding:         "Port Forwarding",
	NavKnownHosts:             "Known Hosts",
	NavSettings:               "Settings",
	NavDebug:                  "Debug",
	NavSearch:                 "Search",
	SearchPlaceholder:         "Search hosts or ssh user@hostname…",
	CommandPaletteTitle:       "Command Palette",
	CommandPalettePlaceholder: "Type a command…",
	CommandPaletteEmpty:       "No matching commands",
	CommandSwitchSessionFmt:   "Switch to %s",
	NewWindow:                 "New Window",

	DebugTitle:                  "Debug",
	DebugDescription:            "Try the application's components with the real theme, layout and input. Nothing here changes your hosts or sessions.",
	DebugComponents:             "Components",
	DebugButtonGroupTitle:       "Button group",
	DebugButtonGroupDescription: "Related actions joined into one control. Hover, focus and press it to check the shared borders and rounded ends.",
	DebugButtonGroupChoice:      "Last pressed:",
	DebugAlignLeft:              "Left",
	DebugAlignCenter:            "Center",
	DebugAlignRight:             "Right",
	DebugSelectTitle:            "Environment",
	DebugSelectDescription:      "A native select whose value and open state belong to the page.",
	DebugSelectChoice:           "Selected:",
	DebugEnvironmentDevelopment: "Development",
	DebugEnvironmentStaging:     "Staging",
	DebugEnvironmentProduction:  "Production",

	StateTooNewTitle:       "Saved data is from a newer Motty",
	StateTooNewBodyFmt:     "%s was written by a newer version (format %s; this version reads up to %s). It has not been changed, and this version will not save over it, so your hosts and settings are safe. Open it with the newer version.",
	StateUnreadableTitle:   "Saved data could not be read",
	StateUnreadableBodyFmt: "Motty started empty. The unreadable file was kept as %s; your hosts are in that copy.",
	StateNoticeOK:          "OK",
	TrayTitle:              "Sessions",
	TrayEmpty:              "No open sessions",
	TrayQuit:               "Quit Motty",
	TrayLocalTerminal:      "New local terminal",
	TrayShowWindow:         "Show Motty",
	TrayTooltipFmt:         "Motty (%d open)",
	TrayTooltipEmpty:       "Motty",

	SettingsTitle:                          "Settings",
	SettingsDescription:                    "Choose how Motty looks and how terminal input behaves.",
	SettingsLanguage:                       "Language",
	SettingsLanguageEnglish:                "English",
	SettingsLanguageChinese:                "简体中文",
	SettingsAppearance:                     "Appearance",
	SettingsAppearanceSystem:               "System",
	SettingsAppearanceDark:                 "Dark",
	SettingsAppearanceLight:                "Light",
	SettingsTerminalOption:                 "Option key",
	SettingsTerminalOptionDescription:      "Type macOS Option characters, or send Meta as an Escape prefix.",
	SettingsTerminalOptionText:             "Text",
	SettingsTerminalOptionMeta:             "Meta (Esc)",
	SettingsTerminalFontSize:               "Terminal font size",
	SettingsTerminalFontSizeDescription:    "Changes every tab at once.",
	SettingsTerminalFontFamily:             "Terminal font family",
	SettingsTerminalFontSystem:             "System Mono",
	SettingsTerminalFontClassic:            "Classic Mono",
	SettingsTerminalScrollback:             "Scrollback lines",
	SettingsTerminalScrollbackDescription:  "Choose how much history new terminal tabs retain.",
	SettingsConnectionTimeout:              "Connection timeout",
	SettingsConnectionTimeoutDescription:   "Bounds each TCP attempt before authentication.",
	SettingsConnectionKeepAlive:            "SSH keepalive",
	SettingsConnectionKeepAliveDescription: "Probes an established connection.",
	SettingsConnectionKeepAliveOff:         "Off",
	SettingsConnectionReconnect:            "Automatic reconnect",
	SettingsConnectionReconnectDescription: "Retry only sessions that were connected and then unexpectedly disconnected.",
	SettingsConnectionReconnectOff:         "Off",
	SettingsConnectionReconnectThree:       "3 tries",
	SettingsConnectionReconnectForever:     "Forever",
	SettingsShare:                          "Share connections",
	SettingsShareDescription:               "A split or new tab reuses the connection: no handshake or host-key check.",
	ConnShareRefusedFmt:                    "The server would not open another shell on the shared connection (%v). Connecting separately.",
	PasswordSave:                           "Save password",
	KeyPassphraseSave:                      "Save passphrase",
	PasswordSavedRefused:                   "The saved password was refused and has been forgotten.",
	SettingsGroupSecurity:                  "Saved passwords",
	SettingsVault:                          "Saved passwords",
	SettingsVaultDescriptionFmt:            "%d saved. They are encrypted; the key is kept in your system's keychain.",
	SettingsVaultDescriptionLockedFmt:      "%d saved. They are encrypted with your master password.",
	SettingsVaultEmpty:                     "None saved. Tick \"Save password\" when a server asks for one.",
	SettingsVaultForgetAll:                 "Forget all",
	SettingsMasterPassword:                 "Master password",
	SettingsMasterPasswordDescription:      "Asked once at startup; locked again when you quit.",
	SettingsMasterPasswordSet:              "Set",
	SettingsMasterPasswordChange:           "Change",
	SettingsMasterPasswordRemove:           "Remove",
	SettingsMasterPasswordLock:             "Lock now",
	SettingsMasterPasswordUnlock:           "Unlock",
	VaultUnlockTitle:                       "Unlock saved passwords",
	VaultUnlockLabel:                       "Master password",
	VaultUnlockButton:                      "Unlock",
	VaultWrongPassword:                     "That is not the master password.",
	MasterPasswordTitleSet:                 "Set master password",
	MasterPasswordTitleChange:              "Change master password",
	MasterPasswordDescription:              "Saved passwords are locked with it. If you forget it, the only way back is to forget all saved passwords.",
	MasterPasswordNewLabel:                 "New master password",
	MasterPasswordConfirmLabel:             "Confirm",
	MasterPasswordSave:                     "Save",
	MasterPasswordTooShortFmt:              "Use at least %d characters.",
	MasterPasswordMismatch:                 "The two passwords differ.",
	VaultForgetTitle:                       "Forget all saved passwords?",
	VaultForgetDescription:                 "They are deleted from this computer, and so is the master password. You will be asked again the next time a server needs one.",
	VaultForgetConfirm:                     "Forget all",
	VaultSavedFmt:                          "Saved the password for %s.",
	VaultSaveFailedFmt:                     "Could not save the password: %v",
	VaultKeyLost:                           "The key for the saved passwords is no longer in the system keychain. Forget all saved passwords to start again.",
	VaultUnavailable:                       "This system has no keychain to keep the key in. Set a master password to save passwords.",
	VaultCorruptFmt:                        "The saved-passwords file could not be read and was set aside as %s.",
	VaultMasterSet:                         "Master password set.",
	VaultMasterRemoved:                     "Master password removed.",
	VaultCleared:                           "All saved passwords were forgotten.",
	SettingsRestore:                        "On restore",
	SettingsRestoreDescription:             "Reconnects last session's tabs; password and host-key prompts are skipped.",
	SettingsRestoreOff:                     "Off",
	SettingsRestoreActive:                  "Current tab",
	SettingsRestoreAll:                     "All tabs",
	SessionRestoreNeedsYou:                 "This host needs a password or a host key confirmation, so it was not connected automatically.",
	TipSFTPClose:                           "Close the files panel",
	TipFiles:                               "Files: browse and transfer over SFTP",
	TipFilesLocal:                          "Files: browse this computer",
	SFTPInsertPath:                         "Type the path into the terminal",
	MenuCopy:                               "Copy",
	MenuPaste:                              "Paste",
	MenuSelectAll:                          "Select All",
	MenuClearScrollback:                    "Clear Scrollback",
	MenuResourceMonitor:                    "Resource Monitor",
	LocalTerminal:                          "Local Terminal",
	LocalTerminalTitle:                     "This Computer",
	TipStatsStart:                          "Show the server's CPU, memory, network and GPU",
	TipStatsStop:                           "Stop the resource monitor",
	TipStatsRetry:                          "Try the resource monitor again",
	TipRecordStart:                         "Record this terminal session",
	TipRecordStop:                          "Stop recording",
	SettingsTerminalClipboard:              "Remote clipboard (OSC 52)",
	SettingsTerminalClipboardDescription:   "Control whether a remote host may replace the local clipboard. Reads are always denied.",
	SettingsTerminalClipboardBlock:         "Block",
	SettingsTerminalClipboardAsk:           "Ask",
	SettingsTerminalClipboardAllow:         "Allow",
	SettingsShortcuts:                      "Keyboard shortcuts",
	ShortcutsReset:                         "Reset defaults",
	ShortcutsCaptureTitle:                  "Press a shortcut",
	ShortcutsCaptureDescription:            "Use Control or Command, optionally with Shift or Alt, plus one letter or digit.",
	ShortcutsConflictFmt:                   "That shortcut is already assigned to %s.",
	ShortcutsInvalid:                       "Use Control or Command plus one letter or digit.",
	ShortcutCommandPalette:                 "Open command palette",
	ShortcutFind:                           "Find terminal history",
	ShortcutCopy:                           "Copy terminal selection",
	ShortcutPaste:                          "Paste into terminal",
	ShortcutNewTab:                         "New tab",
	ShortcutCloseTab:                       "Close tab",
	RecordingStart:                         "Record",
	RecordingStop:                          "Stop recording",
	RecordingExport:                        "Export recording",
	RecordingStarted:                       "Terminal recording started",
	RecordingStopped:                       "Terminal recording stopped",
	RecordingExportedFmt:                   "Terminal recording exported to %s",
	RecordingExportFailedFmt:               "Could not export terminal recording: %s",

	NewHost:                     "New Host",
	EditHost:                    "Edit",
	CopyHost:                    "Copy",
	DeleteHost:                  "Delete",
	MoveHostGroup:               "Move Group",
	ExportHosts:                 "Export",
	ImportCatalog:               "Import Catalog",
	ImportMenu:                  "Import",
	Connect:                     "Connect",
	ColumnName:                  "NAME",
	HostsEmpty:                  "Set up your hosts",
	HostsEmptySub:               "Save hosts to connect to servers, VMs and containers quickly.",
	SSHConfigImport:             "Import SSH Config",
	SSHConfigPickerTitle:        "Import OpenSSH config",
	SSHConfigImportCanceled:     "SSH config import canceled",
	SSHConfigImportFailedFmt:    "Could not import SSH config: %s",
	SSHConfigResultTitle:        "SSH Config Imported",
	SSHConfigSummaryFmt:         "Imported %d hosts, %d keys and %d jump routes; skipped %d duplicate hosts.",
	SSHConfigWarningsFmt:        "%d import warnings",
	SSHConfigNoWarnings:         "All supported settings were imported without warnings.",
	SSHConfigDone:               "Done",
	SSHConfigWarningSourceFmt:   "%s:%d · %s",
	SSHConfigWarningHostFmt:     "Skipped host %s because its HostName is not static.",
	SSHConfigWarningIdentityFmt: "Skipped identity %s: %s",
	SSHConfigWarningProxyFmt:    "Could not safely map ProxyJump for %s.",
	SSHConfigWarningMoreFmt:     "%d more warnings",

	DialogTitle:                 "New Host",
	EditHostTitle:               "Edit Host",
	CopyHostTitle:               "Copy Host",
	FieldLabel:                  "Label",
	FieldLabelPlaceholder:       "Shown in the host list",
	FieldAddress:                "Address",
	FieldAddressPlaceholder:     "Hostname or IP",
	FieldUser:                   "User",
	FieldUserPlaceholder:        "Login account",
	FieldGroup:                  "Group",
	FieldGroupPlaceholder:       "Optional grouping",
	FieldPort:                   "Port",
	FieldTags:                   "Tags",
	FieldTagsPlaceholder:        "Comma-separated",
	FieldIcon:                   "Icon",
	IconAuto:                    "Automatic (by system)",
	IconServer:                  "Server",
	IconTerminal:                "Terminal",
	IconGlobe:                   "Globe",
	IconKey:                     "Key",
	IconDesktop:                 "Desktop",
	ErrLabelRequired:            "Label is required",
	ErrGroupRequired:            "Group is required",
	ErrAddressTaken:             "This host already exists",
	ErrPortInvalid:              "Port must be 1-65535",
	ErrHostTagsInvalid:          "Use at most 12 tags of 32 characters each",
	Cancel:                      "Cancel",
	AddHost:                     "Add Host",
	SaveHost:                    "Save Host",
	DeleteHostTitle:             "Delete Hosts",
	DeleteHostDescriptionFmt:    "Delete %d selected host(s)? This cannot be undone.",
	DeleteHostConfirm:           "Delete",
	HostDeleteBlockedProxyFmt:   "Cannot delete %s because a proxy route references it.",
	HostDeleteBlockedForwardFmt: "Cannot delete %s because a port forward references it.",
	HostDeleteBlockedSessionFmt: "Cannot delete %s because an open or restored session references it.",
	HostDeletedFmt:              "Deleted %d host(s)",
	HostUpdated:                 "Updated host · ",
	HostCopied:                  "Copied host · ",
	HostCopyLabelFmt:            "%s Copy",
	HostSelectionFmt:            "%d selected",
	MoveGroupNone:               "Remove from group",
	MoveGroupNew:                "New group",
	MoveGroupTitle:              "Move Hosts to Group",
	MoveGroupField:              "Group",
	MoveGroupSubmit:             "Move",
	HostMovedFmt:                "Moved %d host(s) to %s",
	HostGroupDefault:            "Hosts",
	HostFilterAll:               "All",
	HostFilterMore:              "More",
	HostCatalogPickerTitle:      "Import Motty host catalog",
	HostCatalogImportFailedFmt:  "Could not import host catalog: %s",
	HostCatalogImportedFmt:      "Imported %d host(s); skipped %d duplicate(s)",
	HostCatalogExportedFmt:      "Exported %d host(s) to %s",
	HostCatalogExportFailedFmt:  "Could not export host catalog: %s",

	NoMatches:                      "No matching hosts",
	NoMatchesSub:                   "Connect opens what you typed, written as user@host or user@host:port.",
	ErrBadTargetFmt:                "\"%s\" is not something to connect to. Write it as user@host or user@host:port.",
	HostStateIdle:                  "Not connected",
	HostStateConnecting:            "Connecting",
	HostStateConnected:             "Connected",
	HostStateFailed:                "Failed",
	LabelLanguage:                  "Switch language",
	LabelThemeToLight:              "Switch to light theme",
	LabelThemeToDark:               "Switch to dark theme",
	LabelNewTab:                    "New tab",
	NewTabConnectTo:                "Connect to",
	MenuCloseOtherTabs:             "Close Other Tabs",
	MenuCloseTabsToRight:           "Close Tabs to the Right",
	MenuDuplicateTab:               "Duplicate Tab",
	MenuRenameTab:                  "Rename",
	CloseTabsTitleFmt:              "Close %d tabs?",
	CloseTabsDescription:           "Their sessions will be disconnected and their terminals closed.",
	CloseTabsConfirm:               "Close Tabs",
	RenameTabTitle:                 "Rename Tab",
	RenameTabField:                 "Name",
	RenameTabPlaceholder:           "Tab name",
	RenameTabSubmit:                "Rename",
	ErrTabNameRequired:             "Enter a name for the tab.",
	NewTabAllHosts:                 "All hosts",
	NewTabPickerPlaceholder:        "Search hosts…",
	NewTabPickerEmpty:              "No matching hosts",
	LabelCloseTab:                  "Close tab",
	LabelWindowMinimize:            "Minimize",
	LabelWindowMaximize:            "Maximize",
	LabelWindowRestore:             "Restore",
	LabelWindowClose:               "Close",
	LabelTerminalFmt:               "Terminal %s",
	PanePrevious:                   "Previous pane",
	PaneNext:                       "Next pane",
	PaneSplitHorizontal:            "Split Right",
	PaneSplitVertical:              "Split Down",
	PaneClose:                      "Close pane",
	PaneResize:                     "Resize panes",
	StatusSaveFailedFmt:            "Could not save settings: %s",
	StatusNewWindowFailedFmt:       "Could not open a new window: %s",
	StatusOpenURLFailedFmt:         "Could not open link: %s",
	StatusClipboardBlocked:         "Remote clipboard write blocked",
	StatusClipboardCopied:          "Remote text copied to the clipboard",
	StatusClipboardFailedFmt:       "Could not update clipboard: %s",
	StatusClipboardReadFailedFmt:   "Could not read clipboard: %s",
	ConnShellFailedFmt:             "Could not start a shell: %s",
	SessionEnded:                   "[Session ended]",
	SessionEndedStatusFmt:          "[Session ended with exit status %d]",
	SessionRestoredTitle:           "Session restored",
	SessionRestoredBody:            "This tab was restored locally. No connection was opened.",
	SessionReconnect:               "Connect",
	SessionReconnectHint:           "Press Enter to connect again.",
	ConnClosed:                     "Connection closed.",
	ConnCanceled:                   "Connection canceled.",
	ConnRejected:                   "Host key rejected. Nothing was sent to the server.",
	ConnFailedFmt:                  "Connection failed: %s",
	ConnAuthFailed:                 "Authentication failed. The server refused the credentials.",
	ConnSkippedKeyFmt:              "Skipped %s for this connection.",
	ConnChangedTitle:               "WARNING: THE HOST KEY HAS CHANGED",
	ConnChangedBodyFmt:             "%s presented a different key than the one you trusted. This can mean an attack. Nothing was sent.",
	ConnChangedTrustedFmt:          "Trusted:   %s",
	ConnChangedPresented:           "Presented: %s",
	ConnChangedHintFmt:             "Only if you expect the server changed, remove its old line from %s.",
	ClipboardRequestTitle:          "Remote clipboard request",
	ClipboardRequestDescriptionFmt: "%s wants to replace your local clipboard. Review the text before allowing it.",
	ClipboardAllowOnce:             "Allow once",

	HostKeyTitle:          "Trust this host?",
	HostKeyDescriptionFmt: "%s is new to this app. Check the fingerprint before you trust it.",
	HostKeyType:           "Key type",
	HostKeyFingerprint:    "SHA256",
	HostKeyTrust:          "Trust and Save",
	HostKeyOnce:           "Connect Once",
	HostKeyReject:         "Reject",
	PasswordTitle:         "Password required",
	PasswordDescription:   "Sign in as %s",
	PasswordLabel:         "Password",
	PasswordHint:          "Enter submits. It is saved only if you tick the box below.",
	PasswordRetry:         "Permission denied. Try again.",
	StatusConnectedFmt:    "Connected · ",
	StatusConnectFailed:   "Connection failed · ",

	StatusReadyIdle:    "Ready · no background activity",
	StatusSelected:     "Selected ",
	StatusOpened:       "Opened workspace · ",
	StatusReused:       "Reused session · ",
	StatusHostAdded:    "Host added · ",
	StatusEditCanceled: "New host canceled",

	KnownHostsTitle:                 "Known Hosts",
	KnownHostsDescription:           "Review the host keys this app trusts. Select one or more entries to delete or re-verify.",
	KnownHostsSearchPlaceholder:     "Search host, type, fingerprint, or comment",
	KnownHostsRefresh:               "Refresh",
	KnownHostsDelete:                "Delete",
	KnownHostsRetrust:               "Re-trust",
	KnownHostsColumnLine:            "Line",
	KnownHostsColumnHosts:           "Host patterns",
	KnownHostsColumnFlags:           "Marker / hash",
	KnownHostsColumnType:            "Key type",
	KnownHostsColumnFingerprint:     "Fingerprint",
	KnownHostsColumnComment:         "Comment",
	KnownHostsEmpty:                 "No trusted host keys",
	KnownHostsEmptySub:              "Keys saved after host verification will appear here.",
	KnownHostsNoMatches:             "No matching entries",
	KnownHostsNoMatchesSub:          "Try a host, key type, fingerprint, or comment.",
	KnownHostsError:                 "Known hosts could not be loaded",
	KnownHostsCountFmt:              "%d entries",
	KnownHostsFilteredCountFmt:      "%d / %d entries",
	KnownHostsSelectedFmt:           "%d selected",
	KnownHostsPageFmt:               "Page %d / %d",
	KnownHostsPrevious:              "Previous",
	KnownHostsNext:                  "Next",
	KnownHostsLoadedFmt:             "Loaded %d known-host entries",
	KnownHostsLoadFailedFmt:         "Could not read known_hosts: %s",
	KnownHostsDeleteTitle:           "Delete trusted host keys?",
	KnownHostsDeleteDescriptionFmt:  "Delete %d selected entries from known_hosts? This cannot be undone.",
	KnownHostsDeleteConfirm:         "Delete entries",
	KnownHostsDeleteFailedFmt:       "Could not update known_hosts: %s",
	KnownHostsDeletedFmt:            "Deleted %d known-host entries",
	KnownHostsRetrustTitle:          "Re-verify these hosts?",
	KnownHostsRetrustDescriptionFmt: "Remove %d old entries? No replacement key will be trusted now.",
	KnownHostsRetrustConfirm:        "Remove old keys",
	KnownHostsRetrustedFmt:          "Removed %d old entries. The next connection will verify the host key again; no new key was trusted.",
	KnownHostsVerifyWarning:         "Only continue after independently confirming that the server key is expected to change.",

	KeyTitle:                    "SSH Keys",
	KeyDescription:              "Additional private keys tried after your default OpenSSH identities.",
	KeyImport:                   "Import Key",
	KeyRemove:                   "Remove",
	KeyColumnName:               "NAME",
	KeyColumnType:               "TYPE",
	KeyColumnProtection:         "PROTECTION",
	KeyColumnFingerprint:        "FINGERPRINT",
	KeyEmpty:                    "No additional identities",
	KeyEmptySub:                 "Import existing SSH private keys. Files stay in place; a passphrase is saved only if you ask.",
	KeyCountFmt:                 "%d keys",
	KeyUnavailable:              "Unavailable",
	KeyUnencrypted:              "None",
	KeyPassphraseProtected:      "Passphrase",
	KeyEncryptedUnknown:         "Encrypted key",
	KeyImportTitle:              "Import SSH private keys",
	KeyImportCanceled:           "Key import canceled",
	KeyImportFailedFmt:          "Could not choose keys: %s",
	KeyInvalidFmt:               "%s is not a usable SSH private key: %s",
	KeyImportedFmt:              "Imported %d keys",
	KeyNothingImported:          "No new keys were imported",
	KeySelected:                 "Selected key · ",
	KeyRemoved:                  "Removed key reference · ",
	KeyPassphraseTitle:          "Unlock SSH key",
	KeyPassphraseDescriptionFmt: "Enter the passphrase for %s. It is saved only if you tick the box below.",
	KeyPassphraseLabel:          "Key passphrase",
	KeyPassphraseHint:           "Enter unlocks this key for the current connection.",
	KeyPassphraseRetry:          "Incorrect passphrase. Try again or skip this key.",
	KeyPassphraseSkip:           "Skip Key",
	KeyUnlock:                   "Unlock",

	ForwardTitle:                  "Port Forwarding",
	ForwardDescription:            "Route loopback listeners across SSH with local, remote, SOCKS5, or HTTP CONNECT rules. Nothing listens until started.",
	ForwardAdd:                    "New Forward",
	ForwardRemove:                 "Remove",
	ForwardStart:                  "Start",
	ForwardStop:                   "Stop",
	ForwardColumnName:             "NAME",
	ForwardColumnHost:             "SSH HOST",
	ForwardColumnListener:         "LISTENER",
	ForwardColumnDestination:      "DESTINATION",
	ForwardColumnStatus:           "STATUS",
	ForwardEmpty:                  "No port forwards",
	ForwardEmptySub:               "Add a local, remote, SOCKS5, or HTTP CONNECT rule through a saved SSH host.",
	ForwardCountFmt:               "%d forwards",
	ForwardStopped:                "Stopped",
	ForwardStoppedError:           "Stopped · action required",
	ForwardWaiting:                "Waiting for SSH",
	ForwardStarting:               "Starting remote listener",
	ForwardRunningFmt:             "Running · %d active",
	ForwardRunningError:           "Running · last connection failed",
	ForwardTrafficFmt:             "%d accepted · %d B up · %d B down",
	ForwardHostUnavailable:        "SSH host unavailable",
	ForwardConnectionClosed:       "The SSH connection closed",
	ForwardSelected:               "Selected forward · ",
	ForwardAdded:                  "Added forward · ",
	ForwardRemoved:                "Removed forward · ",
	ForwardStoppedStatus:          "Stopped forward · ",
	ForwardConnectingFmt:          "Connecting to %s before starting the forward…",
	ForwardStartingFmt:            "Starting remote listener %s…",
	ForwardStartedFmt:             "Forwarding %s → %s",
	ForwardStartFailedFmt:         "Could not start forward: %s",
	ForwardDialogTitle:            "New Port Forward",
	ForwardDialogDescription:      "Traffic binds to 127.0.0.1 and travels through the selected SSH host.",
	ForwardFieldName:              "Name",
	ForwardFieldHost:              "SSH host",
	ForwardFieldType:              "Forward type",
	ForwardModeLocal:              "Local",
	ForwardModeRemote:             "Remote",
	ForwardModeSOCKS5:             "SOCKS5",
	ForwardModeHTTPConnect:        "HTTP CONNECT",
	ForwardSOCKS5Hint:             "Clients choose each destination through the proxy.",
	ForwardHTTPConnectHint:        "Clients choose each tunnel destination.",
	ForwardFieldLocalPort:         "Local port",
	ForwardFieldRemotePort:        "Remote port",
	ForwardFieldDestination:       "Destination host",
	ForwardFieldDestinationPort:   "Destination port",
	ForwardDestinationPlaceholder: "Reachable from the SSH server",
	ForwardLocalDestinationHint:   "Reachable from this computer",
	ForwardRemoteListenerFmt:      "Remote · %s",
	ForwardCreate:                 "Add Forward",
	ErrForwardNameRequired:        "A forward name is required",
	ErrForwardHostRequired:        "Add and select an SSH host first",
	ErrForwardPortInvalid:         "Ports must be 1-65535",
	ErrForwardDestinationRequired: "Enter a destination host without spaces",
	ErrForwardNameTaken:           "A forward already uses that name",
	ErrForwardLocalTaken:          "A forward already uses that local port",
	ErrForwardRemoteTaken:         "That SSH host already uses the remote port",

	ProxyTitle:                 "Jump Hosts",
	ProxyDescription:           "Chain saved SSH hosts; every jump is independently verified and authenticated.",
	ProxyAdd:                   "Add Route",
	ProxyRemove:                "Remove",
	ProxyColumnTarget:          "TARGET",
	ProxyColumnJump:            "JUMP HOST",
	ProxyColumnRoute:           "ROUTE",
	ProxyEmpty:                 "No jump routes",
	ProxyEmptySub:              "Add a route when a target is reachable only through another saved SSH host.",
	ProxyCountFmt:              "%d jump routes",
	ProxySelected:              "Selected route · ",
	ProxyAdded:                 "Jump route added · ",
	ProxyRemoved:               "Jump route removed · ",
	ProxyDialogTitle:           "New Jump Route",
	ProxyFieldTarget:           "Target Host",
	ProxyFieldJump:             "Jump Host",
	ProxyCreate:                "Add Route",
	ErrProxyHostsRequired:      "Save at least two hosts before adding a jump route",
	ErrProxyTargetRequired:     "Choose a target host",
	ErrProxyJumpRequired:       "Choose a jump host",
	ErrProxySameHost:           "The target and jump host must be different",
	ErrProxyCycle:              "That jump route would create a cycle",
	ErrProxyTargetTaken:        "That target already has a jump route",
	ConnDetailConnectingViaFmt: "%s via %s",

	SFTPFiles:                   "Files",
	SFTPUp:                      "Up",
	SFTPRefresh:                 "Refresh",
	SFTPDownload:                "Download",
	SFTPUpload:                  "Upload",
	SFTPUploadDirectory:         "Upload Folder",
	SFTPNewDirectory:            "New Folder",
	SFTPOpen:                    "Open",
	SFTPRename:                  "Rename / Move",
	SFTPDelete:                  "Delete",
	SFTPPermissions:             "Permissions",
	SFTPCancelTransfer:          "Cancel transfer",
	SFTPLoading:                 "Loading remote files…",
	SFTPUnavailable:             "SFTP worker is unavailable",
	SFTPCanceling:               "Canceling transfer…",
	SFTPCanceled:                "Transfer canceled",
	SFTPSaveTitle:               "Save remote file",
	SFTPSaveCanceled:            "Download canceled before transfer",
	SFTPEmpty:                   "This directory is empty",
	SFTPErrorFmt:                "SFTP error: %s",
	SFTPItemsFmt:                "%d items",
	SCPItemsFmt:                 "%d items · SCP, approximate listing",
	SFTPDownloadedFmt:           "Downloaded %d bytes to %s",
	SFTPUploadTitle:             "Upload local file",
	SFTPUploadCanceled:          "Upload canceled before transfer",
	SFTPUploadedFmt:             "Uploaded %d bytes to %s",
	SFTPUploadDirectoryTitle:    "Choose folder to upload",
	SFTPDownloadDirectoryTitle:  "Choose download folder",
	SFTPTransferQueue:           "Transfer queue",
	SFTPQueuedFmt:               "Queued · %s",
	SFTPProgressFmt:             "%d / %d bytes · %d / %d files",
	SFTPTransferDoneFmt:         "Complete · %d bytes",
	SFTPTransferFailedFmt:       "Failed · %s",
	SFTPRetryTransfer:           "Retry",
	SFTPOverwriteTitle:          "Replace Existing Destination?",
	SFTPOverwriteDescriptionFmt: "%s already exists. Replace it (a matching partial transfer is resumed), or skip what exists and copy only what is missing.",
	SFTPOverwriteConfirm:        "Replace / Resume",
	SFTPNewDirectoryTitle:       "New Folder",
	SFTPDirectoryName:           "Folder name",
	SFTPCreateDirectory:         "Create",
	SFTPErrDirectoryRequired:    "Enter a folder name",
	SFTPCreatingDirectoryFmt:    "Creating %s…",
	SFTPDirectoryCreatedFmt:     "Created folder %s",
	SFTPRenameTitle:             "Rename or Move",
	SFTPRenameDescription:       "Enter the new remote path. Relative paths stay under the current directory.",
	SFTPDestinationPath:         "Destination path",
	SFTPRenameSubmit:            "Rename / Move",
	SFTPErrDestinationRequired:  "Enter a destination path",
	SFTPRenamingFmt:             "Renaming %s…",
	SFTPRenamedFmt:              "Renamed %s to %s",
	SFTPDeleteFileTitle:         "Delete File",
	SFTPDeleteDirectoryTitle:    "Delete Empty Folder",
	SFTPDeleteDescriptionFmt:    "Permanently delete %s? This action cannot be undone.",
	SFTPDeleteConfirm:           "Delete",
	SFTPDeletingFmt:             "Deleting %s…",
	SFTPDeletedFmt:              "Deleted %s",
	SFTPPermissionsTitle:        "Change Permissions",
	SFTPPermissionsDescription:  "Enter a three-digit octal mode such as 644 or 755.",
	SFTPPermissionsMode:         "Octal mode",
	SFTPPermissionsSubmit:       "Apply",
	SFTPErrModeInvalid:          "Enter an octal mode from 000 to 777",
	SFTPChangingPermissionsFmt:  "Changing permissions for %s…",
	SFTPPermissionsChangedFmt:   "Changed %s permissions to %03o",
	SFTPEditOpeningFmt:          "Opening %s for editing…",
	SFTPEditBack:                "Files",
	SFTPEditSave:                "Save Remote",
	SFTPEditReload:              "Reload",
	SFTPEditLabel:               "Remote file contents",
	SFTPEditSafety:              "UTF-8 text · 1 MiB maximum · conflict-checked atomic save",
	SFTPEditReady:               "Remote file opened",
	SFTPEditDirty:               "Unsaved changes",
	SFTPEditSaving:              "Saving remote file atomically…",
	SFTPEditSaved:               "Remote file saved",
	SFTPEditReloading:           "Reloading remote file…",
	SFTPEditConflict:            "Remote file changed; your edits were not uploaded",
	SFTPEditDiscardTitle:        "Discard Unsaved Changes?",
	SFTPEditDiscardDescription:  "Return to the file list without saving your edits?",
	SFTPEditDiscardConfirm:      "Discard",
	SFTPEditReloadTitle:         "Reload Remote File?",
	SFTPEditReloadDescription:   "Reloading discards your unsaved local edits.",
	SFTPEditConflictTitle:       "Remote File Changed",
	SFTPEditConflictDescription: "The server has a newer version. Keep your local edits, or reload the remote file and discard them.",
	SFTPEditReloadConfirm:       "Reload Remote",

	StatsMeasuring:   "Measuring server resources…",
	StatsUnsupported: "Resource stats unsupported",
	StatsUnavailable: "Resource stats unavailable",

	TerminalFindLabel:        "Find in terminal",
	TerminalFindPlaceholder:  "Find",
	TerminalFindPrevious:     "Previous match",
	TerminalFindNext:         "Next match",
	TerminalFindClose:        "Close find",
	TerminalFindNoMatches:    "No matches",
	TerminalFindCountFmt:     "%d of %d",
	TerminalFindTruncatedFmt: "%d of %d+",

	ConnStepConnect:            "Reach the server",
	ConnStepHostKey:            "Check the host key",
	ConnStepLogin:              "Sign in",
	ConnStepShell:              "Open a shell",
	ConnDetailDialing:          "Waiting for the server to answer…",
	ConnDetailDialingViaFmt:    "Through %s…",
	ConnDetailJumpDialingFmt:   "Waiting for jump host %s to answer…",
	ConnDetailJumpKeyFmt:       "Check the jump host key · %s",
	ConnDetailJumpLoginFmt:     "Signing in to jump host %s",
	ConnDetailJumpReadyFmt:     "Reached the target through %s",
	ConnDetailServerFmt:        "Connected · %s",
	ConnDetailKnownHost:        "Matches the key saved in known_hosts",
	ConnDetailKeyAskFmt:        "%s %s · new to this app",
	ConnDetailAgent:            "Trying your keys and ssh-agent…",
	ConnDetailKeyPassphraseFmt: "Waiting to unlock %s",
	ConnDetailPasswordAsk:      "The server asks for a password",
	ConnDetailMethodFmt:        "Signed in with %s",
	ConnDetailShellOpening:     "Asking for a terminal…",
	ConnDetailShellWaiting:     "Waiting for the first output…",
	ConnNeedsYou:               "Needs you",
	ConnElapsedFmt:             "Elapsed %s",
	ConnShowLog:                "Show log",
	ConnShowProgress:           "Show progress",
	ConnClose:                  "Close",
	ConnRetry:                  "Try again",
	ConnCancelReconnect:        "Cancel retry",
	ConnExportLog:              "Export log",
	ConnReconnectWaitingFmt:    "Reconnect attempt %d in %s",
	ConnReconnectTryingFmt:     "Reconnect attempt %d",
	ConnReconnectStoppedFmt:    "Automatic reconnect stopped after %d attempts.",
	ConnReconnectCanceled:      "Automatic reconnect canceled.",
	ConnExportedFmt:            "Connection log exported to %s.",
	ConnExportFailedFmt:        "Could not export connection log: %s",
	ConnChangedShort:           "The host key has changed. Nothing was sent.",
}

// Chinese catalog.
var ChineseMessages = Messages{

	NavHosts:                  "主机",
	NavKeychain:               "密钥链",
	NavProxies:                "代理",
	NavPortForwarding:         "端口转发",
	NavKnownHosts:             "已知主机",
	NavSettings:               "设置",
	NavDebug:                  "调试",
	NavSearch:                 "搜索",
	SearchPlaceholder:         "搜索主机或 ssh user@hostname…",
	CommandPaletteTitle:       "命令面板",
	CommandPalettePlaceholder: "输入命令…",
	CommandPaletteEmpty:       "没有匹配的命令",
	CommandSwitchSessionFmt:   "切换到 %s",
	NewWindow:                 "新建窗口",

	DebugTitle:                  "调试",
	DebugDescription:            "用真实的主题、布局和输入测试应用组件。这里不会改动你的主机或会话。",
	DebugComponents:             "组件",
	DebugButtonGroupTitle:       "按钮组",
	DebugButtonGroupDescription: "把相关操作连接成一个控件。悬停、聚焦和按下它，检查共享边框与圆角。",
	DebugButtonGroupChoice:      "最近按下：",
	DebugAlignLeft:              "左对齐",
	DebugAlignCenter:            "居中",
	DebugAlignRight:             "右对齐",
	DebugSelectTitle:            "环境",
	DebugSelectDescription:      "一个由页面持有选中值和开合状态的原生选择菜单。",
	DebugSelectChoice:           "已选择：",
	DebugEnvironmentDevelopment: "开发",
	DebugEnvironmentStaging:     "预发布",
	DebugEnvironmentProduction:  "生产",

	StateTooNewTitle:       "已保存的数据来自更新版本的 Motty",
	StateTooNewBodyFmt:     "%s 由更新的版本写入(格式 %s;当前版本最高读取 %s)。它没有被改动,当前版本也不会覆盖它,你的主机和设置是安全的。请用新版本打开。",
	StateUnreadableTitle:   "无法读取已保存的数据",
	StateUnreadableBodyFmt: "Motty 已从空状态启动。无法读取的文件保留为 %s,你的主机都在这个副本里。",
	StateNoticeOK:          "知道了",
	TrayTitle:              "会话",
	TrayEmpty:              "没有打开的会话",
	TrayQuit:               "退出 Motty",
	TrayLocalTerminal:      "新建本地终端",
	TrayShowWindow:         "显示 Motty",
	TrayTooltipFmt:         "Motty · %d 个打开的会话",
	TrayTooltipEmpty:       "Motty",

	SettingsTitle:                          "设置",
	SettingsDescription:                    "选择 Motty 的外观与终端输入行为。",
	SettingsLanguage:                       "语言",
	SettingsLanguageEnglish:                "English",
	SettingsLanguageChinese:                "简体中文",
	SettingsAppearance:                     "外观",
	SettingsAppearanceSystem:               "跟随系统",
	SettingsAppearanceDark:                 "深色",
	SettingsAppearanceLight:                "浅色",
	SettingsTerminalOption:                 "Option 键",
	SettingsTerminalOptionDescription:      "输入 macOS Option 字符，或用 Esc 前缀发送 Meta。",
	SettingsTerminalOptionText:             "文本",
	SettingsTerminalOptionMeta:             "Meta (Esc)",
	SettingsTerminalFontSize:               "终端字号",
	SettingsTerminalFontSizeDescription:    "所有标签页一起调整。",
	SettingsTerminalFontFamily:             "终端字体",
	SettingsTerminalFontSystem:             "系统等宽",
	SettingsTerminalFontClassic:            "经典等宽",
	SettingsTerminalScrollback:             "回滚行数",
	SettingsTerminalScrollbackDescription:  "选择新终端标签页保留的历史行数。",
	SettingsConnectionTimeout:              "连接超时",
	SettingsConnectionTimeoutDescription:   "认证前每次 TCP 连接的等待上限。",
	SettingsConnectionKeepAlive:            "SSH 保活",
	SettingsConnectionKeepAliveDescription: "探测已建立的连接是否仍可用。",
	SettingsConnectionKeepAliveOff:         "关闭",
	SettingsConnectionReconnect:            "自动重连",
	SettingsConnectionReconnectDescription: "仅重试已成功连接后意外断开的会话。",
	SettingsConnectionReconnectOff:         "关闭",
	SettingsConnectionReconnectThree:       "重试 3 次",
	SettingsConnectionReconnectForever:     "持续重试",
	SettingsShare:                          "共享连接",
	SettingsShareDescription:               "分屏或新开标签时复用连接，不再握手或确认主机密钥。",
	ConnShareRefusedFmt:                    "服务器不再为共享连接打开新的 shell(%v),改为单独连接。",
	PasswordSave:                           "保存密码",
	KeyPassphraseSave:                      "保存口令",
	PasswordSavedRefused:                   "已保存的密码被服务器拒绝,已忘记。",
	SettingsGroupSecurity:                  "已保存的密码",
	SettingsVault:                          "已保存的密码",
	SettingsVaultDescriptionFmt:            "已保存 %d 个。它们已加密,密钥保存在系统钥匙串中。",
	SettingsVaultDescriptionLockedFmt:      "已保存 %d 个。它们由主密码加密。",
	SettingsVaultEmpty:                     "还没有保存。服务器要求密码时勾选「保存密码」即可。",
	SettingsVaultForgetAll:                 "全部忘记",
	SettingsMasterPassword:                 "主密码",
	SettingsMasterPasswordDescription:      "启动后输入一次，退出后重新锁定。",
	SettingsMasterPasswordSet:              "设置",
	SettingsMasterPasswordChange:           "修改",
	SettingsMasterPasswordRemove:           "移除",
	SettingsMasterPasswordLock:             "立即锁定",
	SettingsMasterPasswordUnlock:           "解锁",
	VaultUnlockTitle:                       "解锁已保存的密码",
	VaultUnlockLabel:                       "主密码",
	VaultUnlockButton:                      "解锁",
	VaultWrongPassword:                     "主密码不正确。",
	MasterPasswordTitleSet:                 "设置主密码",
	MasterPasswordTitleChange:              "修改主密码",
	MasterPasswordDescription:              "已保存的密码由它加锁。如果忘记,只能「全部忘记」已保存的密码后重新开始。",
	MasterPasswordNewLabel:                 "新主密码",
	MasterPasswordConfirmLabel:             "再次输入",
	MasterPasswordSave:                     "保存",
	MasterPasswordTooShortFmt:              "至少 %d 个字符。",
	MasterPasswordMismatch:                 "两次输入不一致。",
	VaultForgetTitle:                       "忘记全部已保存的密码?",
	VaultForgetDescription:                 "它们和主密码都会从这台电脑上删除。下次服务器需要时会再次询问。",
	VaultForgetConfirm:                     "全部忘记",
	VaultSavedFmt:                          "已保存 %s 的密码。",
	VaultSaveFailedFmt:                     "无法保存密码:%v",
	VaultKeyLost:                           "已保存密码的密钥不在系统钥匙串里了。请「全部忘记」后重新开始。",
	VaultUnavailable:                       "这个系统没有可存放密钥的钥匙串。设置主密码后才能保存密码。",
	VaultCorruptFmt:                        "已保存密码的文件无法读取,已改名为 %s。",
	VaultMasterSet:                         "主密码已设置。",
	VaultMasterRemoved:                     "主密码已移除。",
	VaultCleared:                           "已忘记全部已保存的密码。",
	SettingsRestore:                        "恢复会话时",
	SettingsRestoreDescription:             "自动连接上次的标签页；需要密码或确认主机密钥的会跳过。",
	SettingsRestoreOff:                     "不连接",
	SettingsRestoreActive:                  "当前标签",
	SettingsRestoreAll:                     "全部标签",
	SessionRestoreNeedsYou:                 "此主机需要你输入密码或确认主机密钥,因此没有自动连接。",
	TipSFTPClose:                           "关闭文件面板",
	TipFiles:                               "文件:通过 SFTP 浏览和传输",
	TipFilesLocal:                          "文件:浏览本机文件",
	SFTPInsertPath:                         "把路径输入终端",
	MenuCopy:                               "复制",
	MenuPaste:                              "粘贴",
	MenuSelectAll:                          "全选",
	MenuClearScrollback:                    "清除滚动历史",
	MenuResourceMonitor:                    "资源监控",
	LocalTerminal:                          "本地终端",
	LocalTerminalTitle:                     "本机",
	TipStatsStart:                          "显示服务器的 CPU、内存、网络和 GPU",
	TipStatsStop:                           "停止资源监控",
	TipStatsRetry:                          "重试资源监控",
	TipRecordStart:                         "录制此终端会话",
	TipRecordStop:                          "停止录制",
	SettingsTerminalClipboard:              "远端剪贴板 (OSC 52)",
	SettingsTerminalClipboardDescription:   "控制远端主机能否替换本地剪贴板；始终拒绝读取本地剪贴板。",
	SettingsTerminalClipboardBlock:         "阻止",
	SettingsTerminalClipboardAsk:           "询问",
	SettingsTerminalClipboardAllow:         "允许",
	SettingsShortcuts:                      "键盘快捷键",
	ShortcutsReset:                         "恢复默认",
	ShortcutsCaptureTitle:                  "按下快捷键",
	ShortcutsCaptureDescription:            "使用 Control 或 Command，可组合 Shift 或 Alt，再按一个字母或数字。",
	ShortcutsConflictFmt:                   "该快捷键已分配给“%s”。",
	ShortcutsInvalid:                       "请使用 Control 或 Command 加一个字母或数字。",
	ShortcutCommandPalette:                 "打开命令面板",
	ShortcutFind:                           "查找终端历史",
	ShortcutCopy:                           "复制终端选择",
	ShortcutPaste:                          "粘贴到终端",
	ShortcutNewTab:                         "新标签页",
	ShortcutCloseTab:                       "关闭标签页",
	RecordingStart:                         "录制",
	RecordingStop:                          "停止录制",
	RecordingExport:                        "导出录制",
	RecordingStarted:                       "终端录制已开始",
	RecordingStopped:                       "终端录制已停止",
	RecordingExportedFmt:                   "终端录制已导出到 %s",
	RecordingExportFailedFmt:               "无法导出终端录制：%s",

	NewHost:                     "新建主机",
	EditHost:                    "编辑",
	CopyHost:                    "复制",
	DeleteHost:                  "删除",
	MoveHostGroup:               "移动分组",
	ExportHosts:                 "导出",
	ImportCatalog:               "导入目录",
	ImportMenu:                  "导入",
	Connect:                     "连接",
	ColumnName:                  "名称",
	HostsEmpty:                  "添加你的主机",
	HostsEmptySub:               "保存主机,即可快速连接服务器、虚拟机和容器。",
	SSHConfigImport:             "导入 SSH Config",
	SSHConfigPickerTitle:        "导入 OpenSSH 配置",
	SSHConfigImportCanceled:     "已取消导入 SSH 配置",
	SSHConfigImportFailedFmt:    "无法导入 SSH 配置:%s",
	SSHConfigResultTitle:        "SSH 配置已导入",
	SSHConfigSummaryFmt:         "已导入 %d 台主机、%d 个密钥和 %d 条跳板路由;跳过 %d 台重复主机。",
	SSHConfigWarningsFmt:        "%d 条导入警告",
	SSHConfigNoWarnings:         "所有受支持的设置均已导入,没有警告。",
	SSHConfigDone:               "完成",
	SSHConfigWarningSourceFmt:   "文件 %s 第 %d 行 · %s",
	SSHConfigWarningHostFmt:     "已跳过主机 %s,因为其 HostName 不是静态值。",
	SSHConfigWarningIdentityFmt: "已跳过密钥 %s:%s",
	SSHConfigWarningProxyFmt:    "无法安全映射 %s 的 ProxyJump。",
	SSHConfigWarningMoreFmt:     "另有 %d 条警告",

	DialogTitle:                 "新建主机",
	EditHostTitle:               "编辑主机",
	CopyHostTitle:               "复制主机",
	FieldLabel:                  "名称",
	FieldLabelPlaceholder:       "显示在主机列表中",
	FieldAddress:                "地址",
	FieldAddressPlaceholder:     "主机名或 IP",
	FieldUser:                   "用户",
	FieldUserPlaceholder:        "登录账户",
	FieldGroup:                  "分组",
	FieldGroupPlaceholder:       "可选分组",
	FieldPort:                   "端口",
	FieldTags:                   "标签",
	FieldTagsPlaceholder:        "可选，以逗号分隔",
	FieldIcon:                   "图标",
	IconAuto:                    "自动 (按系统)",
	IconServer:                  "服务器",
	IconTerminal:                "终端",
	IconGlobe:                   "地球",
	IconKey:                     "钥匙",
	IconDesktop:                 "桌面",
	ErrLabelRequired:            "请填写名称",
	ErrGroupRequired:            "请填写分组",
	ErrAddressTaken:             "该主机已存在",
	ErrPortInvalid:              "端口范围 1-65535",
	ErrHostTagsInvalid:          "最多 12 个标签，每个不超过 32 个字符",
	Cancel:                      "取消",
	AddHost:                     "添加主机",
	SaveHost:                    "保存主机",
	DeleteHostTitle:             "删除主机",
	DeleteHostDescriptionFmt:    "删除选中的 %d 台主机？此操作无法撤销。",
	DeleteHostConfirm:           "删除",
	HostDeleteBlockedProxyFmt:   "无法删除 %s，因为跳板路由正在引用它。",
	HostDeleteBlockedForwardFmt: "无法删除 %s，因为端口转发正在引用它。",
	HostDeleteBlockedSessionFmt: "无法删除 %s，因为打开或恢复的会话正在引用它。",
	HostDeletedFmt:              "已删除 %d 台主机",
	HostUpdated:                 "已更新主机 · ",
	HostCopied:                  "已复制主机 · ",
	HostCopyLabelFmt:            "%s 副本",
	HostSelectionFmt:            "已选择 %d 台",
	MoveGroupNone:               "移出分组",
	MoveGroupNew:                "新建分组",
	MoveGroupTitle:              "移动主机分组",
	MoveGroupField:              "分组",
	MoveGroupSubmit:             "移动",
	HostMovedFmt:                "已将 %d 台主机移动到 %s",
	HostGroupDefault:            "主机",
	HostFilterAll:               "全部",
	HostFilterMore:              "更多",
	HostCatalogPickerTitle:      "导入 Motty 主机目录",
	HostCatalogImportFailedFmt:  "无法导入主机目录：%s",
	HostCatalogImportedFmt:      "已导入 %d 台主机；跳过 %d 台重复主机",
	HostCatalogExportedFmt:      "已导出 %d 台主机到 %s",
	HostCatalogExportFailedFmt:  "无法导出主机目录：%s",

	NoMatches:                      "没有匹配的主机",
	NoMatchesSub:                   "点击“连接”会直接连接你输入的内容,写成 user@host 或 user@host:端口。",
	ErrBadTargetFmt:                "“%s”不是可连接的目标,请写成 user@host 或 user@host:端口。",
	HostStateIdle:                  "未连接",
	HostStateConnecting:            "连接中",
	HostStateConnected:             "已连接",
	HostStateFailed:                "连接失败",
	LabelLanguage:                  "切换语言",
	LabelThemeToLight:              "切换到浅色主题",
	LabelThemeToDark:               "切换到深色主题",
	LabelNewTab:                    "新标签页",
	NewTabConnectTo:                "连接到",
	MenuCloseOtherTabs:             "关闭其他标签页",
	MenuCloseTabsToRight:           "关闭右侧标签页",
	MenuDuplicateTab:               "复制标签页",
	MenuRenameTab:                  "重命名",
	CloseTabsTitleFmt:              "关闭 %d 个标签页？",
	CloseTabsDescription:           "这些标签页里的会话会断开，终端也会关闭。",
	CloseTabsConfirm:               "关闭标签页",
	RenameTabTitle:                 "重命名标签页",
	RenameTabField:                 "名称",
	RenameTabPlaceholder:           "标签页名称",
	RenameTabSubmit:                "重命名",
	ErrTabNameRequired:             "请输入标签页名称。",
	NewTabAllHosts:                 "全部主机",
	NewTabPickerPlaceholder:        "搜索主机…",
	NewTabPickerEmpty:              "没有匹配的主机",
	LabelCloseTab:                  "关闭标签页",
	LabelWindowMinimize:            "最小化",
	LabelWindowMaximize:            "最大化",
	LabelWindowRestore:             "还原",
	LabelWindowClose:               "关闭",
	LabelTerminalFmt:               "终端 %s",
	PanePrevious:                   "上一个窗格",
	PaneNext:                       "下一个窗格",
	PaneSplitHorizontal:            "向右分屏",
	PaneSplitVertical:              "向下分屏",
	PaneClose:                      "关闭窗格",
	PaneResize:                     "调整窗格大小",
	StatusSaveFailedFmt:            "无法保存设置:%s",
	StatusNewWindowFailedFmt:       "无法新建窗口:%s",
	StatusOpenURLFailedFmt:         "无法打开链接:%s",
	StatusClipboardBlocked:         "已阻止远端写入剪贴板",
	StatusClipboardCopied:          "已将远端文本复制到剪贴板",
	StatusClipboardFailedFmt:       "无法更新剪贴板:%s",
	StatusClipboardReadFailedFmt:   "无法读取剪贴板:%s",
	ConnShellFailedFmt:             "无法启动 Shell:%s",
	SessionEnded:                   "[会话已结束]",
	SessionEndedStatusFmt:          "[会话已结束,退出状态 %d]",
	SessionRestoredTitle:           "会话已恢复",
	SessionRestoredBody:            "此标签仅在本地恢复,尚未建立任何网络连接。",
	SessionReconnect:               "连接",
	SessionReconnectHint:           "按回车重新连接。",
	ConnClosed:                     "连接已关闭。",
	ConnCanceled:                   "连接已取消。",
	ConnRejected:                   "已拒绝主机密钥,未向服务器发送任何内容。",
	ConnFailedFmt:                  "连接失败:%s",
	ConnAuthFailed:                 "认证失败,服务器拒绝了凭据。",
	ConnSkippedKeyFmt:              "本次连接已跳过 %s。",
	ConnChangedTitle:               "警告:主机密钥已变更",
	ConnChangedBodyFmt:             "%s 出示的密钥与您信任过的不同,这可能意味着遭到攻击。未发送任何内容。",
	ConnChangedTrustedFmt:          "已信任:%s",
	ConnChangedPresented:           "本次出示:%s",
	ConnChangedHintFmt:             "仅当您确认服务器已重建时,才从 %s 删除旧记录。",
	ClipboardRequestTitle:          "远端剪贴板请求",
	ClipboardRequestDescriptionFmt: "%s 想要替换你的本地剪贴板。允许前请核对文本。",
	ClipboardAllowOnce:             "仅允许本次",

	HostKeyTitle:          "信任此主机?",
	HostKeyDescriptionFmt: "%s 是首次连接。信任之前请核对指纹。",
	HostKeyType:           "密钥类型",
	HostKeyFingerprint:    "SHA256",
	HostKeyTrust:          "信任并保存",
	HostKeyOnce:           "仅本次连接",
	HostKeyReject:         "拒绝",
	PasswordTitle:         "需要密码",
	PasswordDescription:   "以 %s 登录",
	PasswordLabel:         "密码",
	PasswordHint:          "按回车提交。仅在勾选下方选项时才会保存。",
	PasswordRetry:         "密码错误,请重试。",
	StatusConnectedFmt:    "已连接 · ",
	StatusConnectFailed:   "连接失败 · ",

	StatusReadyIdle:    "就绪 · 无后台活动",
	StatusSelected:     "已选择 ",
	StatusOpened:       "已打开工作区 · ",
	StatusReused:       "复用会话 · ",
	StatusHostAdded:    "已添加主机 · ",
	StatusEditCanceled: "已取消新建主机",

	KnownHostsTitle:                 "已知主机",
	KnownHostsDescription:           "查看此应用信任的主机密钥。可选择一条或多条记录进行删除或重新验证。",
	KnownHostsSearchPlaceholder:     "搜索主机、类型、指纹或注释",
	KnownHostsRefresh:               "刷新",
	KnownHostsDelete:                "删除",
	KnownHostsRetrust:               "重新信任",
	KnownHostsColumnLine:            "行",
	KnownHostsColumnHosts:           "主机模式",
	KnownHostsColumnFlags:           "标记 / 哈希",
	KnownHostsColumnType:            "密钥类型",
	KnownHostsColumnFingerprint:     "指纹",
	KnownHostsColumnComment:         "注释",
	KnownHostsEmpty:                 "尚无已信任主机密钥",
	KnownHostsEmptySub:              "完成主机验证并保存后，密钥会显示在这里。",
	KnownHostsNoMatches:             "没有匹配记录",
	KnownHostsNoMatchesSub:          "请尝试搜索主机、密钥类型、指纹或注释。",
	KnownHostsError:                 "无法加载已知主机",
	KnownHostsCountFmt:              "%d 条记录",
	KnownHostsFilteredCountFmt:      "%d / %d 条记录",
	KnownHostsSelectedFmt:           "已选择 %d 条",
	KnownHostsPageFmt:               "第 %d / %d 页",
	KnownHostsPrevious:              "上一页",
	KnownHostsNext:                  "下一页",
	KnownHostsLoadedFmt:             "已加载 %d 条已知主机记录",
	KnownHostsLoadFailedFmt:         "无法读取 known_hosts：%s",
	KnownHostsDeleteTitle:           "删除已信任主机密钥？",
	KnownHostsDeleteDescriptionFmt:  "要从 known_hosts 删除选中的 %d 条记录吗？此操作无法撤销。",
	KnownHostsDeleteConfirm:         "删除记录",
	KnownHostsDeleteFailedFmt:       "无法更新 known_hosts：%s",
	KnownHostsDeletedFmt:            "已删除 %d 条已知主机记录",
	KnownHostsRetrustTitle:          "重新验证这些主机？",
	KnownHostsRetrustDescriptionFmt: "要删除选中的 %d 条旧记录吗？现在不会信任任何替代密钥。",
	KnownHostsRetrustConfirm:        "删除旧密钥",
	KnownHostsRetrustedFmt:          "已删除 %d 条旧记录。下次连接时会重新验证主机密钥；当前未信任任何新密钥。",
	KnownHostsVerifyWarning:         "仅在您已通过独立渠道确认服务器密钥预期发生变化后继续。",

	KeyTitle:                    "SSH 密钥",
	KeyDescription:              "在 OpenSSH 默认身份之后尝试的附加私钥。",
	KeyImport:                   "导入密钥",
	KeyRemove:                   "移除",
	KeyColumnName:               "名称",
	KeyColumnType:               "类型",
	KeyColumnProtection:         "保护",
	KeyColumnFingerprint:        "指纹",
	KeyEmpty:                    "尚未添加附加身份",
	KeyEmptySub:                 "导入已有 SSH 私钥。文件保留在原处，口令仅在你要求时才保存。",
	KeyCountFmt:                 "%d 个密钥",
	KeyUnavailable:              "不可用",
	KeyUnencrypted:              "无",
	KeyPassphraseProtected:      "口令",
	KeyEncryptedUnknown:         "加密密钥",
	KeyImportTitle:              "导入 SSH 私钥",
	KeyImportCanceled:           "已取消导入密钥",
	KeyImportFailedFmt:          "无法选择密钥:%s",
	KeyInvalidFmt:               "%s 不是可用的 SSH 私钥:%s",
	KeyImportedFmt:              "已导入 %d 个密钥",
	KeyNothingImported:          "没有导入新密钥",
	KeySelected:                 "已选择密钥 · ",
	KeyRemoved:                  "已移除密钥引用 · ",
	KeyPassphraseTitle:          "解锁 SSH 密钥",
	KeyPassphraseDescriptionFmt: "请输入 %s 的口令。仅在勾选下方选项时才会保存。",
	KeyPassphraseLabel:          "密钥口令",
	KeyPassphraseHint:           "按回车为本次连接解锁该密钥。",
	KeyPassphraseRetry:          "口令错误，请重试或跳过此密钥。",
	KeyPassphraseSkip:           "跳过密钥",
	KeyUnlock:                   "解锁",

	ForwardTitle:                  "端口转发",
	ForwardDescription:            "通过 SSH 建立本地、远程、SOCKS5 或 HTTP CONNECT 回环监听。只有手动启动后才会监听。",
	ForwardAdd:                    "新建转发",
	ForwardRemove:                 "移除",
	ForwardStart:                  "启动",
	ForwardStop:                   "停止",
	ForwardColumnName:             "名称",
	ForwardColumnHost:             "SSH 主机",
	ForwardColumnListener:         "监听地址",
	ForwardColumnDestination:      "目标地址",
	ForwardColumnStatus:           "状态",
	ForwardEmpty:                  "尚未添加端口转发",
	ForwardEmptySub:               "通过已保存 SSH 主机添加本地、远程、SOCKS5 或 HTTP CONNECT 规则。",
	ForwardCountFmt:               "%d 个转发",
	ForwardStopped:                "已停止",
	ForwardStoppedError:           "已停止 · 需要处理",
	ForwardWaiting:                "等待 SSH 连接",
	ForwardStarting:               "正在启动远程监听",
	ForwardRunningFmt:             "运行中 · %d 个活动连接",
	ForwardRunningError:           "运行中 · 上次连接失败",
	ForwardTrafficFmt:             "已接受 %d 个连接 · 上传 %d B · 下载 %d B",
	ForwardHostUnavailable:        "SSH 主机不可用",
	ForwardConnectionClosed:       "SSH 连接已关闭",
	ForwardSelected:               "已选择转发 · ",
	ForwardAdded:                  "已添加转发 · ",
	ForwardRemoved:                "已移除转发 · ",
	ForwardStoppedStatus:          "已停止转发 · ",
	ForwardConnectingFmt:          "正在连接 %s，连接后将启动转发…",
	ForwardStartingFmt:            "正在启动远程监听 %s…",
	ForwardStartedFmt:             "正在转发 %s → %s",
	ForwardStartFailedFmt:         "无法启动转发:%s",
	ForwardDialogTitle:            "新建端口转发",
	ForwardDialogDescription:      "流量仅绑定到 127.0.0.1，并通过所选 SSH 主机传输。",
	ForwardFieldName:              "名称",
	ForwardFieldHost:              "SSH 主机",
	ForwardFieldType:              "转发类型",
	ForwardModeLocal:              "本地",
	ForwardModeRemote:             "远程",
	ForwardModeSOCKS5:             "SOCKS5",
	ForwardModeHTTPConnect:        "HTTP CONNECT",
	ForwardSOCKS5Hint:             "客户端通过代理为每个连接选择目标地址。",
	ForwardHTTPConnectHint:        "客户端为每条隧道选择目标地址。",
	ForwardFieldLocalPort:         "本地端口",
	ForwardFieldRemotePort:        "远程端口",
	ForwardFieldDestination:       "目标主机",
	ForwardFieldDestinationPort:   "目标端口",
	ForwardDestinationPlaceholder: "SSH 服务器可访问的主机",
	ForwardLocalDestinationHint:   "本机可访问的主机",
	ForwardRemoteListenerFmt:      "远程 · %s",
	ForwardCreate:                 "添加转发",
	ErrForwardNameRequired:        "必须填写转发名称",
	ErrForwardHostRequired:        "请先添加并选择 SSH 主机",
	ErrForwardPortInvalid:         "端口必须为 1-65535",
	ErrForwardDestinationRequired: "请输入不含空格的目标主机",
	ErrForwardNameTaken:           "已有转发使用该名称",
	ErrForwardLocalTaken:          "已有转发使用该本地端口",
	ErrForwardRemoteTaken:         "该 SSH 主机已使用此远程端口",

	ProxyTitle:                 "跳板主机",
	ProxyDescription:           "串联已保存的 SSH 主机；每一跳都会独立校验主机密钥并认证。",
	ProxyAdd:                   "添加路由",
	ProxyRemove:                "移除",
	ProxyColumnTarget:          "目标主机",
	ProxyColumnJump:            "跳板主机",
	ProxyColumnRoute:           "路径",
	ProxyEmpty:                 "暂无跳板路由",
	ProxyEmptySub:              "当目标只能通过另一台已保存 SSH 主机访问时，请添加路由。",
	ProxyCountFmt:              "%d 条跳板路由",
	ProxySelected:              "已选择路由 · ",
	ProxyAdded:                 "已添加跳板路由 · ",
	ProxyRemoved:               "已移除跳板路由 · ",
	ProxyDialogTitle:           "新建跳板路由",
	ProxyFieldTarget:           "目标主机",
	ProxyFieldJump:             "跳板主机",
	ProxyCreate:                "添加路由",
	ErrProxyHostsRequired:      "至少保存两台主机后才能添加跳板路由",
	ErrProxyTargetRequired:     "请选择目标主机",
	ErrProxyJumpRequired:       "请选择跳板主机",
	ErrProxySameHost:           "目标主机和跳板主机必须不同",
	ErrProxyCycle:              "该跳板路由会形成环路",
	ErrProxyTargetTaken:        "该目标主机已有跳板路由",
	ConnDetailConnectingViaFmt: "%s，经由 %s",

	SFTPFiles:                   "文件",
	SFTPUp:                      "上级目录",
	SFTPRefresh:                 "刷新",
	SFTPDownload:                "下载",
	SFTPUpload:                  "上传",
	SFTPUploadDirectory:         "上传文件夹",
	SFTPNewDirectory:            "新建文件夹",
	SFTPOpen:                    "打开",
	SFTPRename:                  "重命名 / 移动",
	SFTPDelete:                  "删除",
	SFTPPermissions:             "权限",
	SFTPCancelTransfer:          "取消传输",
	SFTPLoading:                 "正在加载远程文件…",
	SFTPUnavailable:             "SFTP 工作线程不可用",
	SFTPCanceling:               "正在取消传输…",
	SFTPCanceled:                "传输已取消",
	SFTPSaveTitle:               "保存远程文件",
	SFTPSaveCanceled:            "下载已在传输前取消",
	SFTPEmpty:                   "此目录为空",
	SFTPErrorFmt:                "SFTP 错误:%s",
	SFTPItemsFmt:                "%d 个项目",
	SCPItemsFmt:                 "%d 个项目 · SCP 模式，列表为近似",
	SFTPDownloadedFmt:           "已下载 %d 字节到 %s",
	SFTPUploadTitle:             "上传本地文件",
	SFTPUploadCanceled:          "已在传输前取消上传",
	SFTPUploadedFmt:             "已上传 %d 字节到 %s",
	SFTPUploadDirectoryTitle:    "选择要上传的文件夹",
	SFTPDownloadDirectoryTitle:  "选择下载目录",
	SFTPTransferQueue:           "传输队列",
	SFTPQueuedFmt:               "已排队 · %s",
	SFTPProgressFmt:             "%d / %d 字节 · %d / %d 文件",
	SFTPTransferDoneFmt:         "已完成 · %d 字节",
	SFTPTransferFailedFmt:       "失败 · %s",
	SFTPRetryTransfer:           "重试",
	SFTPOverwriteTitle:          "替换已有目标？",
	SFTPOverwriteDescriptionFmt: "%s 已存在。可以替换它（匹配的部分传输会续传），或跳过已有内容、只复制缺少的部分。",
	SFTPOverwriteConfirm:        "替换 / 续传",
	SFTPNewDirectoryTitle:       "新建文件夹",
	SFTPDirectoryName:           "文件夹名称",
	SFTPCreateDirectory:         "创建",
	SFTPErrDirectoryRequired:    "请输入文件夹名称",
	SFTPCreatingDirectoryFmt:    "正在创建 %s…",
	SFTPDirectoryCreatedFmt:     "已创建文件夹 %s",
	SFTPRenameTitle:             "重命名或移动",
	SFTPRenameDescription:       "输入新的远程路径；相对路径位于当前目录下。",
	SFTPDestinationPath:         "目标路径",
	SFTPRenameSubmit:            "重命名 / 移动",
	SFTPErrDestinationRequired:  "请输入目标路径",
	SFTPRenamingFmt:             "正在重命名 %s…",
	SFTPRenamedFmt:              "已将 %s 重命名为 %s",
	SFTPDeleteFileTitle:         "删除文件",
	SFTPDeleteDirectoryTitle:    "删除空文件夹",
	SFTPDeleteDescriptionFmt:    "永久删除 %s？此操作无法撤销。",
	SFTPDeleteConfirm:           "删除",
	SFTPDeletingFmt:             "正在删除 %s…",
	SFTPDeletedFmt:              "已删除 %s",
	SFTPPermissionsTitle:        "修改权限",
	SFTPPermissionsDescription:  "输入三位八进制权限，例如 644 或 755。",
	SFTPPermissionsMode:         "八进制权限",
	SFTPPermissionsSubmit:       "应用",
	SFTPErrModeInvalid:          "请输入 000 到 777 的八进制权限",
	SFTPChangingPermissionsFmt:  "正在修改 %s 的权限…",
	SFTPPermissionsChangedFmt:   "已将 %s 的权限修改为 %03o",
	SFTPEditOpeningFmt:          "正在打开 %s 进行编辑…",
	SFTPEditBack:                "文件列表",
	SFTPEditSave:                "保存到远端",
	SFTPEditReload:              "重新加载",
	SFTPEditLabel:               "远程文件内容",
	SFTPEditSafety:              "仅 UTF-8 文本 · 最大 1 MiB · 冲突检查后原子保存",
	SFTPEditReady:               "已打开远程文件",
	SFTPEditDirty:               "有未保存的修改",
	SFTPEditSaving:              "正在原子保存远程文件…",
	SFTPEditSaved:               "已保存远程文件",
	SFTPEditReloading:           "正在重新加载远程文件…",
	SFTPEditConflict:            "远程文件已变化；本地修改未上传",
	SFTPEditDiscardTitle:        "放弃未保存的修改？",
	SFTPEditDiscardDescription:  "不保存修改并返回文件列表？",
	SFTPEditDiscardConfirm:      "放弃修改",
	SFTPEditReloadTitle:         "重新加载远程文件？",
	SFTPEditReloadDescription:   "重新加载会放弃尚未保存的本地修改。",
	SFTPEditConflictTitle:       "远程文件已变化",
	SFTPEditConflictDescription: "服务器上已有更新版本。可保留本地修改，或重新加载远程文件并放弃本地修改。",
	SFTPEditReloadConfirm:       "重新加载远程文件",

	StatsMeasuring:   "正在测量服务器资源…",
	StatsUnsupported: "服务器不支持资源读数",
	StatsUnavailable: "资源读数暂不可用",

	TerminalFindLabel:        "在终端中查找",
	TerminalFindPlaceholder:  "查找",
	TerminalFindPrevious:     "上一个匹配项",
	TerminalFindNext:         "下一个匹配项",
	TerminalFindClose:        "关闭查找",
	TerminalFindNoMatches:    "无匹配项",
	TerminalFindCountFmt:     "第 %d / %d 项",
	TerminalFindTruncatedFmt: "第 %d / %d+ 项",

	ConnStepConnect:            "连接服务器",
	ConnStepHostKey:            "核对主机密钥",
	ConnStepLogin:              "登录",
	ConnStepShell:              "打开 Shell",
	ConnDetailDialing:          "等待服务器响应…",
	ConnDetailDialingViaFmt:    "经由 %s…",
	ConnDetailJumpDialingFmt:   "等待跳板主机 %s 响应…",
	ConnDetailJumpKeyFmt:       "核对跳板主机密钥 · %s",
	ConnDetailJumpLoginFmt:     "正在登录跳板主机 %s",
	ConnDetailJumpReadyFmt:     "已通过 %s 到达目标主机",
	ConnDetailServerFmt:        "已连接 · %s",
	ConnDetailKnownHost:        "与 known_hosts 中保存的密钥一致",
	ConnDetailKeyAskFmt:        "%s %s · 首次见到",
	ConnDetailAgent:            "正在尝试你的密钥与 ssh-agent…",
	ConnDetailKeyPassphraseFmt: "等待解锁 %s",
	ConnDetailPasswordAsk:      "服务器要求输入密码",
	ConnDetailMethodFmt:        "已通过 %s 登录",
	ConnDetailShellOpening:     "正在请求终端…",
	ConnDetailShellWaiting:     "等待第一批输出…",
	ConnNeedsYou:               "需要你操作",
	ConnElapsedFmt:             "已用时 %s",
	ConnShowLog:                "显示日志",
	ConnShowProgress:           "显示进度",
	ConnClose:                  "关闭",
	ConnRetry:                  "重试",
	ConnCancelReconnect:        "取消重试",
	ConnExportLog:              "导出日志",
	ConnReconnectWaitingFmt:    "第 %d 次重连将在 %s 后开始",
	ConnReconnectTryingFmt:     "正在进行第 %d 次重连",
	ConnReconnectStoppedFmt:    "自动重连已在 %d 次尝试后停止。",
	ConnReconnectCanceled:      "已取消自动重连。",
	ConnExportedFmt:            "连接日志已导出到 %s。",
	ConnExportFailedFmt:        "无法导出连接日志:%s",
	ConnChangedShort:           "主机密钥已变更,未发送任何数据。",
}

// For returns the catalog for one language.
func For(language Language) Messages {
	switch language {
	case Chinese:
		messages := ChineseMessages
		messages.AIPanel, messages.AISettings, messages.Tools = chineseAIPanel, chineseAISettings, chineseTools
		messages.Autofill = chineseAutofill
		messages.History = chineseHistory
		messages.Desktop = chineseDesktop
		return messages
	default:
		messages := EnglishMessages
		messages.AIPanel, messages.AISettings, messages.Tools = englishAIPanel, englishAISettings, englishTools
		messages.Autofill = englishAutofill
		messages.History = englishHistory
		messages.Desktop = englishDesktop
		return messages
	}
}

// Next cycles English → Chinese → English.
func (language Language) Next() Language {
	if language == English {
		return Chinese
	}
	return English
}
