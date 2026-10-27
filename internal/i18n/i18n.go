// Package i18n holds the workbench string catalog. Language selection is
// plain data: the state carries one Messages value and every paint reads
// from it, so switching languages is one assignment plus a repaint.
//
// The catalogs live one per language (english.go, chinese.go); the message
// structs stay with the area they describe (terminal_tools.go, ai_panel.go,
// and so on), so adding a string touches its struct and both catalogs.
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
	FileWindow FileWindowMessages
	SFTPWindow SFTPWindowMessages
	Transfers  TransferCenterMessages
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
	// SSHSync holds the texts of the sync with the OpenSSH config; see ssh_sync.go.
	SSHSync SSHSyncMessages
	// Desktop holds the remote-desktop tabs' texts; see desktop.go.
	Desktop DesktopMessages
	// About holds the About page's texts; see about.go.
	About AboutMessages

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
	TrayCheckUpdates  string
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
	ShortcutLocalTerminal                string
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
	FieldPassword               string
	FieldPasswordHint           string
	FieldPasswordKeepHint       string
	FieldPasswordCopyHint       string
	FieldVNCPassword            string
	HostPasswordShared          string
	HostPasswordNotCarried      string
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
// Chinese catalog.
// For returns the catalog for one language.
func For(language Language) Messages {
	switch language {
	case Chinese:
		messages := ChineseMessages
		messages.AIPanel, messages.AISettings, messages.Tools = chineseAIPanel, chineseAISettings, chineseTools
		messages.Autofill = chineseAutofill
		messages.History = chineseHistory
		messages.SSHSync = chineseSSHSync
		messages.Desktop = chineseDesktop
		messages.About = chineseAbout
		return messages
	default:
		messages := EnglishMessages
		messages.AIPanel, messages.AISettings, messages.Tools = englishAIPanel, englishAISettings, englishTools
		messages.Autofill = englishAutofill
		messages.History = englishHistory
		messages.SSHSync = englishSSHSync
		messages.Desktop = englishDesktop
		messages.About = englishAbout
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
