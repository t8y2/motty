package workbench

import (
	"context"
	"fmt"
	"motty/internal/desktop"
	"os"
	"os/user"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/configstore"
	"motty/internal/highlight"
	"motty/internal/i18n"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

var (
	hitHosts                   = tree.ID(shellHits, "hosts")
	hitKeychain                = tree.ID(shellHits, "keychain")
	hitProxies                 = tree.ID(shellHits, "proxies")
	hitPortForwarding          = tree.ID(shellHits, "port-forwarding")
	hitKnownHosts              = tree.ID(shellHits, "known-hosts")
	hitSettings                = tree.ID(shellHits, "settings")
	hitWindowSettings          = tree.ID(shellHits, "window-settings")
	hitWindowLocalTerminal     = tree.ID(shellHits, "window-local-terminal")
	hitSearch                  = tree.ID(shellHits, "search")
	hitConnect                 = tree.ID(shellHits, "connect")
	hitAddHost                 = tree.ID(shellHits, "add-host")
	hitAddHostEmpty            = tree.ID(shellHits, "add-host-empty")
	hitImportSSHConfig         = tree.ID(shellHits, "import-ssh-config")
	hitEditHost                = tree.ID(shellHits, "edit-host")
	hitCopyHost                = tree.ID(shellHits, "copy-host")
	hitDeleteHost              = tree.ID(shellHits, "delete-host")
	hitMoveHostGroup           = tree.ID(shellHits, "move-host-group")
	hitExportHosts             = tree.ID(shellHits, "export-hosts")
	hitImportHostCatalog       = tree.ID(shellHits, "import-host-catalog")
	hitImportMenu              = tree.ID(shellHits, "import-menu")
	hitImportMenuDismiss       = tree.ID(shellHits, "import-menu-dismiss")
	hitHostGroupDismiss        = tree.ID(shellHits, "host-group-dismiss")
	hitHostFilterAll           = tree.ID(shellHits, "host-filter-all")
	hitHostFilterMore          = tree.ID(shellHits, "host-filter-more")
	hitHostFilterMenuDismiss   = tree.ID(shellHits, "host-filter-menu-dismiss")
	hitHostGroupNone           = tree.ID(shellHits, "host-group-none")
	hitHostGroupNew            = tree.ID(shellHits, "host-group-new")
	hitHostDeleteDismiss       = tree.ID(shellHits, "host-delete-dismiss")
	hitHostDeleteClose         = tree.ID(shellHits, "host-delete-close")
	hitHostDeleteCancel        = tree.ID(shellHits, "host-delete-cancel")
	hitHostDeleteConfirm       = tree.ID(shellHits, "host-delete-confirm")
	hitMoveGroupDismiss        = tree.ID(shellHits, "move-group-dismiss")
	hitMoveGroupClose          = tree.ID(shellHits, "move-group-close")
	hitMoveGroupCancel         = tree.ID(shellHits, "move-group-cancel")
	hitMoveGroupSubmit         = tree.ID(shellHits, "move-group-submit")
	hitMoveGroupField          = tree.ID(shellHits, "move-group-field")
	hitHostImportDismiss       = tree.ID(shellHits, "host-import-dismiss")
	hitHostImportClose         = tree.ID(shellHits, "host-import-close")
	hitHostImportDone          = tree.ID(shellHits, "host-import-done")
	hitLanguage                = tree.ID(shellHits, "language")
	hitTheme                   = tree.ID(shellHits, "theme")
	hitDialogDismiss           = tree.ID(shellHits, "dialog-dismiss")
	hitDialogClose             = tree.ID(shellHits, "dialog-close")
	hitDialogCancel            = tree.ID(shellHits, "dialog-cancel")
	hitDialogSubmit            = tree.ID(shellHits, "dialog-submit")
	hitDialogLabel             = tree.ID(shellHits, "dialog-label")
	hitDialogAddress           = tree.ID(shellHits, "dialog-address")
	hitDialogUser              = tree.ID(shellHits, "dialog-user")
	hitDialogPassword          = tree.ID(shellHits, "dialog-password")
	hitDialogGroup             = tree.ID(shellHits, "dialog-group")
	hitDialogPort              = tree.ID(shellHits, "dialog-port")
	hitDialogTags              = tree.ID(shellHits, "dialog-tags")
	hitDialogColor             = tree.ID(shellHits, "dialog-color")
	hitDialogIcon              = tree.ID(shellHits, "dialog-icon")
	hitHostKeyTrust            = tree.ID(shellHits, "host-key-trust")
	hitHostKeyOnce             = tree.ID(shellHits, "host-key-once")
	hitHostKeyReject           = tree.ID(shellHits, "host-key-reject")
	hitHostKeyClose            = tree.ID(shellHits, "host-key-close")
	hitHostKeyDismiss          = tree.ID(shellHits, "host-key-dismiss")
	hitPasswordFields          = tree.Sub(shellHits, tree.Name("password-fields"))
	hitPasswordField           = hitPasswordFields.At(1)
	hitPasswordSubmit          = tree.ID(shellHits, "password-submit")
	hitPasswordCancel          = tree.ID(shellHits, "password-cancel")
	hitPasswordClose           = tree.ID(shellHits, "password-close")
	hitPasswordDismiss         = tree.ID(shellHits, "password-dismiss")
	hitHome                    = tree.ID(shellHits, "home")
	hitNewTab                  = tree.ID(shellHits, "new-tab")
	hitNewWindow               = tree.ID(shellHits, "new-window")
	hitConnLog                 = tree.ID(connHits, "conn-log")
	hitConnProgress            = tree.ID(connHits, "conn-progress")
	hitConnClose               = tree.ID(connHits, "conn-close")
	hitConnRetry               = tree.ID(connHits, "conn-retry")
	hitConnCancelReconnect     = tree.ID(connHits, "conn-cancel-reconnect")
	hitConnExport              = tree.ID(connHits, "conn-export")
	hitKeyPassphraseField      = tree.ID(shellHits, "key-passphrase-field")
	hitKeyPassphraseSubmit     = tree.ID(shellHits, "key-passphrase-submit")
	hitKeyPassphraseSkip       = tree.ID(shellHits, "key-passphrase-skip")
	hitKeyPassphraseClose      = tree.ID(shellHits, "key-passphrase-close")
	hitKeyPassphraseDismiss    = tree.ID(shellHits, "key-passphrase-dismiss")
	hitPanePrevious            = tree.ID(shellHits, "pane-previous")
	hitPaneNext                = tree.ID(shellHits, "pane-next")
	hitPaneSplitHorizontal     = tree.ID(shellHits, "pane-split-horizontal")
	hitPaneSplitVertical       = tree.ID(shellHits, "pane-split-vertical")
	hitPaneClose               = tree.ID(shellHits, "pane-close")
	hitPaneChooseHost          = tree.ID(shellHits, "pane-choose-host")
	hitContextDivider          = tree.ID(shellHits, "context-divider")
	hitClipboardDismiss        = tree.ID(shellHits, "clipboard-dismiss")
	hitClipboardClose          = tree.ID(shellHits, "clipboard-close")
	hitClipboardCancel         = tree.ID(shellHits, "clipboard-cancel")
	hitClipboardAllow          = tree.ID(shellHits, "clipboard-allow")
	hitShortcutReset           = tree.ID(shellHits, "shortcut-reset")
	hitShortcutCaptureDismiss  = tree.ID(shellHits, "shortcut-capture-dismiss")
	hitShortcutCaptureCancel   = tree.ID(shellHits, "shortcut-capture-cancel")
	hitTerminalRecord          = tree.ID(shellHits, "terminal-record")
	hitTerminalRecordingExport = tree.ID(shellHits, "terminal-recording-export")
	hitStateNoticeDismiss      = tree.ID(shellHits, "state-notice-dismiss")
	hitStateNoticeOK           = tree.ID(shellHits, "state-notice-ok")
)

// Lists whose items are numbered by a database or session number: a host row, a
// tab, a tab's close button and the session's terminal.
var (
	hitHostList     = tree.Sub(shellHits, tree.Name("host"))
	hitTabList      = tree.Sub(shellHits, tree.Name("tab"))
	hitTabCloseList = tree.Sub(shellHits, tree.Name("tab-close"))
	hitTerminalList = tree.Sub(shellHits, tree.Name("terminal"))
)

var (
	hitShortcutRowList = tree.Sub(settingsHits, tree.Name("shortcut-row"))
)

var hitCommandSessionList = tree.Sub(shellHits, tree.Name("command-session"))
var (
	hitHostCheckList      = tree.Sub(shellHits, tree.Name("host-check"))
	hitHostGroupItemList  = tree.Sub(shellHits, tree.Name("host-group-item"))
	hitHostFilterList     = tree.Sub(shellHits, tree.Name("host-filter"))
	hitHostFilterMenuList = tree.Sub(shellHits, tree.Name("host-filter-menu-item"))
	hitHostGroupRowList   = tree.Sub(shellHits, tree.Name("host-group-row"))
	hitHostGroupOpenList  = tree.Sub(shellHits, tree.Name("host-group-open"))
)

// Key management: its page and the rows of its table.
var (
	hitKeysImport = tree.ID(keysHits, "keys-import")
	hitKeysRemove = tree.ID(keysHits, "keys-remove")
	hitKeyRowList = tree.Sub(keysHits, tree.Name("key-row"))
)

// Local forwarding: its page and dialog.
var (
	hitForwardAdd                   = tree.ID(forwardsHits, "forward-add")
	hitForwardRemove                = tree.ID(forwardsHits, "forward-remove")
	hitForwardStartStop             = tree.ID(forwardsHits, "forward-start-stop")
	hitForwardDialogDismiss         = tree.ID(forwardsHits, "forward-dialog-dismiss")
	hitForwardDialogClose           = tree.ID(forwardsHits, "forward-dialog-close")
	hitForwardDialogCancel          = tree.ID(forwardsHits, "forward-dialog-cancel")
	hitForwardDialogSubmit          = tree.ID(forwardsHits, "forward-dialog-submit")
	hitForwardDialogLabel           = tree.ID(forwardsHits, "forward-dialog-label")
	hitForwardDialogHost            = tree.ID(forwardsHits, "forward-dialog-host")
	hitForwardDialogLocalPort       = tree.ID(forwardsHits, "forward-dialog-local-port")
	hitForwardDialogDestination     = tree.ID(forwardsHits, "forward-dialog-destination")
	hitForwardDialogDestinationPort = tree.ID(forwardsHits, "forward-dialog-destination-port")
	hitForwardHostDismiss           = tree.ID(forwardsHits, "forward-host-dismiss")
	hitForwardHostScrollUp          = tree.ID(forwardsHits, "forward-host-scroll-up")
	hitForwardHostScrollDown        = tree.ID(forwardsHits, "forward-host-scroll-down")
	hitForwardModeLocal             = tree.ID(forwardsHits, "forward-mode-local")
	hitForwardModeSOCKS5            = tree.ID(forwardsHits, "forward-mode-socks5")
	hitForwardModeHTTPConnect       = tree.ID(forwardsHits, "forward-mode-http-connect")
	hitForwardModeRemote            = tree.ID(forwardsHits, "forward-mode-remote")
	hitForwardRowList               = tree.Sub(forwardsHits, tree.Name("forward-row"))
	hitForwardHostItemList          = tree.Sub(forwardsHits, tree.Name("forward-host-item"))
)

// Settings: its explicit choices.
var (
	hitSettingsLanguageEnglish  = tree.ID(settingsHits, "settings-language-english")
	hitSettingsLanguageChinese  = tree.ID(settingsHits, "settings-language-chinese")
	hitSettingsAppearanceSystem = tree.ID(settingsHits, "settings-appearance-system")
	hitSettingsAppearanceDark   = tree.ID(settingsHits, "settings-appearance-dark")
	hitSettingsAppearanceLight  = tree.ID(settingsHits, "settings-appearance-light")
	hitSettingsOptionText       = tree.ID(settingsHits, "settings-option-text")
	hitSettingsOptionMeta       = tree.ID(settingsHits, "settings-option-meta")
	hitSettingsFontSizeDown     = tree.ID(settingsHits, "settings-font-size-down")
	hitSettingsFontSizeReset    = tree.ID(settingsHits, "settings-font-size-reset")
	hitSettingsFontSizeUp       = tree.ID(settingsHits, "settings-font-size-up")
	hitSettingsPadCompact       = tree.ID(settingsHits, "settings-pad-compact")
	hitSettingsPadNormal        = tree.ID(settingsHits, "settings-pad-normal")
	hitSettingsPadRoomy         = tree.ID(settingsHits, "settings-pad-roomy")
	hitSettingsGutterOff        = tree.ID(settingsHits, "settings-gutter-off")
	hitSettingsGutterNumbers    = tree.ID(settingsHits, "settings-gutter-numbers")
	hitSettingsGutterTime       = tree.ID(settingsHits, "settings-gutter-time")
	hitSettingsGutterBoth       = tree.ID(settingsHits, "settings-gutter-both")
	hitSettingsScrollback1000   = tree.ID(settingsHits, "settings-scrollback1000")
	hitSettingsScrollback5000   = tree.ID(settingsHits, "settings-scrollback5000")
	hitSettingsScrollback10000  = tree.ID(settingsHits, "settings-scrollback10000")
	hitSettingsTimeout5         = tree.ID(settingsHits, "settings-timeout5")
	hitSettingsTimeout10        = tree.ID(settingsHits, "settings-timeout10")
	hitSettingsTimeout30        = tree.ID(settingsHits, "settings-timeout30")
	hitSettingsKeepAliveOff     = tree.ID(settingsHits, "settings-keep-alive-off")
	hitSettingsKeepAlive30      = tree.ID(settingsHits, "settings-keep-alive30")
	hitSettingsKeepAlive60      = tree.ID(settingsHits, "settings-keep-alive60")
	hitSettingsReconnectOff     = tree.ID(settingsHits, "settings-reconnect-off")
	hitSettingsReconnectThree   = tree.ID(settingsHits, "settings-reconnect-three")
	hitSettingsReconnectForever = tree.ID(settingsHits, "settings-reconnect-forever")
	hitSettingsClipboardBlock   = tree.ID(settingsHits, "settings-clipboard-block")
	hitSettingsCursorToggle     = tree.ID(settingsHits, "settings-cursor-blink")
	hitSettingsClipboardAsk     = tree.ID(settingsHits, "settings-clipboard-ask")
	hitSettingsClipboardAllow   = tree.ID(settingsHits, "settings-clipboard-allow")
	hitSettingsRestoreActive    = tree.ID(settingsHits, "settings-restore-active")
	hitSettingsRestoreOff       = tree.ID(settingsHits, "settings-restore-off")
	hitSettingsRestoreAll       = tree.ID(settingsHits, "settings-restore-all")
	hitSettingsShareToggle      = tree.ID(settingsHits, "settings-share")
	hitSettingsVaultForget      = tree.ID(settingsHits, "settings-vault-forget")
	hitSettingsMasterSet        = tree.ID(settingsHits, "settings-master-set")
	hitSettingsMasterChange     = tree.ID(settingsHits, "settings-master-change")
	hitSettingsMasterRemove     = tree.ID(settingsHits, "settings-master-remove")
	hitSettingsMasterLock       = tree.ID(settingsHits, "settings-master-lock")
	hitSettingsMasterUnlock     = tree.ID(settingsHits, "settings-master-unlock")
)

// Jump-host routing: its page and dialog.
var (
	hitProxyAdd              = tree.ID(proxiesHits, "proxy-add")
	hitProxyRemove           = tree.ID(proxiesHits, "proxy-remove")
	hitProxyDialogDismiss    = tree.ID(proxiesHits, "proxy-dialog-dismiss")
	hitProxyDialogClose      = tree.ID(proxiesHits, "proxy-dialog-close")
	hitProxyDialogCancel     = tree.ID(proxiesHits, "proxy-dialog-cancel")
	hitProxyDialogSubmit     = tree.ID(proxiesHits, "proxy-dialog-submit")
	hitProxyDialogTarget     = tree.ID(proxiesHits, "proxy-dialog-target")
	hitProxyDialogJump       = tree.ID(proxiesHits, "proxy-dialog-jump")
	hitProxyTargetDismiss    = tree.ID(proxiesHits, "proxy-target-dismiss")
	hitProxyTargetScrollUp   = tree.ID(proxiesHits, "proxy-target-scroll-up")
	hitProxyTargetScrollDown = tree.ID(proxiesHits, "proxy-target-scroll-down")
	hitProxyJumpDismiss      = tree.ID(proxiesHits, "proxy-jump-dismiss")
	hitProxyJumpScrollUp     = tree.ID(proxiesHits, "proxy-jump-scroll-up")
	hitProxyJumpScrollDown   = tree.ID(proxiesHits, "proxy-jump-scroll-down")
	hitProxyRowList          = tree.Sub(proxiesHits, tree.Name("proxy-row"))
	hitProxyTargetItemList   = tree.Sub(proxiesHits, tree.Name("proxy-target-item"))
	hitProxyJumpItemList     = tree.Sub(proxiesHits, tree.Name("proxy-jump-item"))
)

// Known-host management: its page and dialog, with the rows and their checkboxes
// as lists of their own.
var (
	hitKnownHostsSearch        = tree.ID(knownHostsHits, "known-hosts-search")
	hitKnownHostsRefresh       = tree.ID(knownHostsHits, "known-hosts-refresh")
	hitKnownHostsDelete        = tree.ID(knownHostsHits, "known-hosts-delete")
	hitKnownHostsRetrust       = tree.ID(knownHostsHits, "known-hosts-retrust")
	hitKnownHostsPrevious      = tree.ID(knownHostsHits, "known-hosts-previous")
	hitKnownHostsNext          = tree.ID(knownHostsHits, "known-hosts-next")
	hitKnownHostsDialogDismiss = tree.ID(knownHostsHits, "known-hosts-dialog-dismiss")
	hitKnownHostsDialogClose   = tree.ID(knownHostsHits, "known-hosts-dialog-close")
	hitKnownHostsDialogCancel  = tree.ID(knownHostsHits, "known-hosts-dialog-cancel")
	hitKnownHostsDialogConfirm = tree.ID(knownHostsHits, "known-hosts-dialog-confirm")
	hitKnownHostRowList        = tree.Sub(knownHostsHits, tree.Name("known-host-row"))
	hitKnownHostCheckList      = tree.Sub(knownHostsHits, tree.Name("known-host-check"))
)

// SFTP: its page and the lists on it.
var (
	hitSFTPOpen               = tree.ID(sftpHits, "sftp-open")
	hitSFTPClose              = tree.ID(sftpHits, "sftp-close")
	hitSFTPUp                 = tree.ID(sftpHits, "sftp-up")
	hitSFTPRefresh            = tree.ID(sftpHits, "sftp-refresh")
	hitSFTPDownload           = tree.ID(sftpHits, "sftp-download")
	hitSFTPCancel             = tree.ID(sftpHits, "sftp-cancel")
	hitSFTPUpload             = tree.ID(sftpHits, "sftp-upload")
	hitSFTPUploadDirectory    = tree.ID(sftpHits, "sftp-upload-directory")
	hitSFTPNewDirectory       = tree.ID(sftpHits, "sftp-new-directory")
	hitSFTPOpenEntry          = tree.ID(sftpHits, "sftp-open-entry")
	hitSFTPRename             = tree.ID(sftpHits, "sftp-rename")
	hitSFTPDelete             = tree.ID(sftpHits, "sftp-delete")
	hitSFTPChmod              = tree.ID(sftpHits, "sftp-chmod")
	hitSFTPDialogDismiss      = tree.ID(sftpHits, "sftp-dialog-dismiss")
	hitSFTPDialogClose        = tree.ID(sftpHits, "sftp-dialog-close")
	hitSFTPDialogCancel       = tree.ID(sftpHits, "sftp-dialog-cancel")
	hitSFTPDialogSubmit       = tree.ID(sftpHits, "sftp-dialog-submit")
	hitSFTPDialogField        = tree.ID(sftpHits, "sftp-dialog-field")
	hitSFTPEditor             = tree.ID(sftpHits, "sftp-editor")
	hitSFTPEditSave           = tree.ID(sftpHits, "sftp-edit-save")
	hitSFTPEditBack           = tree.ID(sftpHits, "sftp-edit-back")
	hitSFTPEditReload         = tree.ID(sftpHits, "sftp-edit-reload")
	hitSFTPBreadcrumbList     = tree.Sub(sftpHits, tree.Name("sftp-breadcrumb"))
	hitSFTPEntryList          = tree.Sub(sftpHits, tree.Name("sftp-entry"))
	hitSFTPTransferCancelList = tree.Sub(sftpHits, tree.Name("sftp-transfer-cancel"))
	hitSFTPTransferRetryList  = tree.Sub(sftpHits, tree.Name("sftp-transfer-retry"))
)

// Remote resource monitoring owns a fixed hit outside every page range. Only
// the active terminal paints it, so it does not carry a session ID.
var hitRemoteStats = tree.ID(shellHits, "remote-stats") // The terminal's context menu owns a fixed range too: it is painted by the
// window, over every pane, for the session that raised it.
var (
	hitTerminalMenuDismiss    = tree.ID(terminalMenuHits, "terminal-menu-dismiss")
	hitTerminalMenuCopy       = tree.ID(terminalMenuHits, "terminal-menu-copy")
	hitTerminalMenuPaste      = tree.ID(terminalMenuHits, "terminal-menu-paste")
	hitTerminalMenuSelectAll  = tree.ID(terminalMenuHits, "terminal-menu-select-all")
	hitTerminalMenuClear      = tree.ID(terminalMenuHits, "terminal-menu-clear")
	hitTerminalMenuSplitRight = tree.ID(terminalMenuHits, "terminal-menu-split-right")
	hitTerminalMenuSplitDown  = tree.ID(terminalMenuHits, "terminal-menu-split-down")
	hitTerminalMenuFiles      = tree.ID(terminalMenuHits, "terminal-menu-files")
	hitTerminalMenuStats      = tree.ID(terminalMenuHits, "terminal-menu-stats")
	hitLocalTerminal          = tree.ID(terminalMenuHits, "local-terminal")
	hitTerminalMenuReconnect  = tree.ID(terminalMenuHits, "terminal-menu-reconnect")
	hitTerminalMenuTransfer   = tree.ID(terminalMenuHits, "terminal-menu-transfer")
)

// The saved-passwords dialogs, and the boxes that offer to save.
var (
	hitVaultUnlockField      = tree.ID(vaultHits, "vault-unlock-field")
	hitVaultUnlockSubmit     = tree.ID(vaultHits, "vault-unlock-submit")
	hitVaultUnlockCancel     = tree.ID(vaultHits, "vault-unlock-cancel")
	hitVaultUnlockClose      = tree.ID(vaultHits, "vault-unlock-close")
	hitVaultUnlockDismiss    = tree.ID(vaultHits, "vault-unlock-dismiss")
	hitMasterPasswordNew     = tree.ID(vaultHits, "master-password-new")
	hitMasterPasswordConfirm = tree.ID(vaultHits, "master-password-confirm")
	hitMasterPasswordSubmit  = tree.ID(vaultHits, "master-password-submit")
	hitMasterPasswordCancel  = tree.ID(vaultHits, "master-password-cancel")
	hitMasterPasswordClose   = tree.ID(vaultHits, "master-password-close")
	hitMasterPasswordDismiss = tree.ID(vaultHits, "master-password-dismiss")
	hitVaultForgetConfirm    = tree.ID(vaultHits, "vault-forget-confirm")
	hitVaultForgetCancel     = tree.ID(vaultHits, "vault-forget-cancel")
	hitVaultForgetClose      = tree.ID(vaultHits, "vault-forget-close")
	hitVaultForgetDismiss    = tree.ID(vaultHits, "vault-forget-dismiss")
	hitPasswordSave          = tree.ID(vaultHits, "password-save")
	hitPasswordHint          = tree.ID(vaultHits, "password-hint")
	hitKeyPassphraseSave     = tree.ID(vaultHits, "key-passphrase-save")
)

var (
	hitPaneDividerList  = tree.Sub(shellHits, tree.Name("pane-divider"))
	hitPaneActivateList = tree.Sub(shellHits, tree.Name("pane-activate"))
)

const ()

// TitlebarHeight is the height of the window bar the workbench paints under the
// traffic lights. The window gives it to the platform as the custom titlebar
// strip, so the lights are centered in it and dragging it moves the window.
const TitlebarHeight = float32(36)

const (
	windowBarHeight = TitlebarHeight
	infoBarHeight   = float32(34)
	barTitleSize    = float32(13) // the name in a pane's bar: a size under the assistant's title
	navigationWidth = float32(204)
	popupLayer      = uint16(300) // a popup inside a page: above the page's text, below dialogs
	dialogLayer     = uint16(600)
	tooltipLayer    = uint16(900)
)

const (
	sectionHosts uint8 = iota
	sectionTerminal
	sectionKeys
	sectionForwards
	sectionProxies
	sectionSettings
	sectionKnownHosts
	// sectionDebug is the development-only component gallery: its page is built
	// only with the dev tag (see debug.go), but the section keeps its place so
	// the other sections do not renumber between builds.
	sectionDebug
)

// State owns the complete interactive state of the window.
// Paint reads it synchronously and never starts work or allocates background
// resources.
type State struct {
	closed bool
	// saveBlocked is set when the state file came from a newer build: nothing is
	// ever written over it. stateNotice is the dialog that tells the user why a
	// state file was not loaded as it was.
	saveBlocked bool
	// config is the store the configuration lives in, and configBase what this
	// window last read from or wrote to it.
	config         *configstore.Store
	configBase     *configBase
	configDefaults configDefaults
	// lastSaved is the last state written, to skip a save that would write the
	// same bytes. hasSaved tells a zero value from no save yet.
	lastSaved savedState
	hasSaved  bool
	// syncDelay and syncRetryBase are how long a change and a failed sync wait
	// before the next sync; syncPending says the start-up sync is still due.
	syncDelay, syncRetryBase time.Duration
	syncPending              bool
	// starting is set while the window is being made, before it has read the
	// assistant's database; pauseSave holds saving off while the window
	// is bringing in records that are already stored.
	starting           bool
	pauseSave          int
	stateNotice        stateNotice
	stateFileOutcome   string // how the saved state was read at start, for diagnostics
	hosts              []Host
	sessions           []Session
	tabOptions         []components.TabOption
	workspaceTabs      []layoutcore.WorkspaceInfo
	tabStrip           tabStrip
	tabMenu            tabMenu
	hostMenu           hostMenu
	sftpMenu           sftpMenu
	popups             popupMenus
	selectedHostID     uint32
	activeSessionID    uint32
	nextSessionID      uint32
	nextHostID         uint32
	nextIdentityID     uint32
	nextForwardID      uint32
	nextForwardStartID uint64
	nextProxyID        uint32
	identities         []identityRecord
	forwards           []forwardRecord
	proxies            []proxyRecord
	selectedIdentityID uint32
	selectedForwardID  uint32
	selectedProxyID    uint32
	activeSection      uint8
	hostFilterOn       bool   // only the group named hostFilter is listed
	hostFilter         string // a group name; empty is the unnamed bucket
	hostFilterMenu     bool   // the chips that did not fit, as a menu
	hostFilterHidden   int    // the first group whose chip is in that menu
	filterMoreButton   ui.Rect
	hostTotalDigits    string
	groupMenu          groupMenuAnchor // where the Move group picker opens
	hostSelectionText  string
	hostSelections     []uint32
	hostActionIDs      []uint32
	hostGroups         []hostGroup
	hostTreeRows       []hostTreeRow
	hostsScroll        float32
	tabDrag            tabDrag
	foreign            tabDrag // another window's tab being dragged over this one
	frame              func() (ui.Rect, bool)
	windowVisible      func() bool
	winChrome          windowChrome
	hostView           hostViewMode
	bg                 backgroundState
	hostsContent       float32
	hostGroupMenu      bool
	importMenu         bool
	hostsBounds        ui.Rect // the hosts page as last painted; the menus anchor to it
	terminalMenu       terminalMenu
	hostGroupEditor    text.Editor
	tabRename          tabRename
	tabsClose          tabsClose
	hostDialogMode     hostDialogMode
	hostDialogID       uint32
	hostDialogText     string
	identityCountText  string
	forwardCountText   string
	forwardStarts      chan forwardStartResult
	proxyCountText     string
	hostImportResult   hostImportResult
	knownHosts         knownHostsView
	knownHostsLayout   flex.View
	settingsRow        flex.View
	partLayout         flex.View // a dialog body, a host card or a group header: one at a time

	forwardLayout, proxyLayout, forwardDialogLayout, proxyDialogLayout flex.View // the Port Forwarding and Proxies pages and their dialogs
	chrome                                                             chromeLayout

	// area: AI panel
	aiFlex aiFlexViews // the assistant panel's layouts, see ai_panel_paint.go

	// search is the box beside Connect: typing narrows the host list, and
	// Connect opens what it holds. visible lists the indexes into hosts that
	// match it; it is rebuilt when the text or the host list changes.
	search              text.Editor
	visible             []int
	visibleQuery        string
	visibleHosts        int
	status              string
	language            i18n.Language
	languageChosen      bool // the user picked it; until then it follows the system
	messages            i18n.Messages
	modal               modalKind
	modalSession        uint32
	dialogError         string
	editor              hostEditor
	forwardEditor       forwardEditor
	forwardDialogError  string
	proxyEditor         proxyEditor
	proxyDialogError    string
	options             Options
	appearance          appearance
	appearanceMode      appearanceMode
	stopAppearanceWatch func()
	terminalOption      terminalOptionMode
	terminalFontSize    float32
	terminalFontFace    ui.MonoFace
	terminalScrollback  int
	connectionTimeout   time.Duration
	connectionKeepAlive time.Duration
	connectionReconnect reconnectPolicy
	restoreConnect      restoreConnectPolicy
	// openStream opens the stream of a Telnet or serial host; tests replace it.
	openStream func(context.Context, streamRequest) (Shell, error)
	// dialDesktop opens the remote desktop of a VNC host; tests replace it.
	dialDesktop func(context.Context, desktopRequest) (desktop.Session, error)
	// transfer is the file transfer in progress, if any; transferLeftover is what
	// followed the end of one and belongs to the screen.
	transfer fileTransfer
	xy       xySetup
	// Requests from outside to connect (see launch.go), the link being asked
	// about, and the update check.
	launchBacklog [][]string
	launchTarget  connection
	update        updateState
	updateAtStart bool
	updateStarted bool
	loginOn       bool
	// The file panel's settings and the folders kept for each host.
	sftpParallel      int
	sftpKeepTimesOff  bool
	sftpFollowOff     bool
	sftpBookmarks     []sftpBookmark
	transferLeftover  []byte
	secrets           secretsUI
	shareConnections  shareConnectionsPolicy
	tooltip           components.HoverTooltip
	selectTypeahead   selectTypeahead
	restorePending    bool // restored tabs have not yet been connected (see connectRestoredSessions)
	terminalClipboard terminalClipboardPolicy
	// terminalCursorSteady keeps the terminal cursor from blinking.
	terminalCursorSteady bool

	// settingsScroll is how far the settings page is scrolled, in pixels, and
	// settingsContent how tall it was when last painted.
	settingsScroll, settingsContent float32
	// settingsCategory is the category of settings shown.
	settingsCategory settingsCategory
	settings         settingsUI

	// area: settings
	settingsPage flex.View // the whole settings workspace

	// contextRect is where the last paint put the side panel (empty when there
	// was none), so wheel events can tell which view they are over.
	contextRect      ui.Rect
	shortcuts        shortcutMap
	shortcutCapture  shortcutAction
	shortcutError    string
	reconnectWaiters int
	pal              *palette
	theme            ui.Theme
	// termPal is the terminal's own look: pal with the chosen color scheme and
	// the highlight rules applied. termEpoch counts its rebuilds.
	termPal        palette
	termEpoch      uint16
	terminalScheme string
	terminalPad    terminalPad
	// terminalGutter is the per-line number/time strip; wallClock overrides the
	// time source it stamps lines with.
	terminalGutter terminalGutter
	wallClock      func() int64
	notifyOff      bool
	fonts          fontsUI
	proxy          outboundProxy
	proxyForm      outboundProxyForm
	form           formDialog
	formSerial     uint32
	closeAsked     bool
	sync           syncState
	sshSync        sshSyncState
	remote         remoteToolsView
	edits          externalEdits
	// fontSizeText is the font size as the settings page shows it, for fontSizeShown.
	fontSizeText  string
	fontSizeShown float32
	highlights    *highlight.Set
	highlighting  highlightUI
	snippets      snippetsUI
	history       historyUI
	historyLayout flex.View
	// broadcast is the tab whose panes type together, or zero.
	broadcast  layoutcore.WorkspaceID
	systemUser string // login used for hosts that name none

	// wrapped is scratch for text broken into lines while painting.
	wrapped          [failureLines]string
	restoredView     flex.View
	connectionLayout flex.View
	// filesLayout lays out the files panel, its editor and the panel before it has a list.
	filesLayout flex.View
	// area: terminal and workspace panes
	terminalPageLayout flex.View
	fontPickerLayout   flex.View // the font picker's list and sample
	paneChrome         flex.View // an empty split pane, the menu's switch and the like: one at a time

	// area: files panel and transfers
	filesList   flex.View
	filesPopup  flex.View
	statsLayout flex.View // the monitor's readout in the terminal's info bar
	// filesRowIcons and filesRowBoxes are scratch for the visible rows of the list.
	filesRowIcons []flex.Box
	filesRowBoxes []flex.Box

	// area: keychain, palette, import and the rest
	railLayout            flex.View
	keysLayout            flex.View
	keysEmptyLayout       flex.View
	paletteLayout         flex.View
	importLayout          flex.View
	knownHostsTableLayout flex.View
	knownHostsEmptyLayout flex.View
	hostToggleLayout      flex.View
	hostGridLayout        flex.View
	hostGridItems         []hostGridItem
	// dev is the development-only component gallery's state; a release build
	// keeps none of it (see debug.go and debug_release.go).
	dev devState
	// viewport and the terminal cell are the layout inputs of the last paint,
	// kept so a resize can work out how many rows and columns fit.
	viewport              ui.Size
	cellWidth, cellHeight float32
	paneCanvas            ui.Canvas
	contextCanvas         ui.Canvas
	panes                 []Pane
	paneSplits            []PaneSplit
	draggedPaneSplit      uint32
	draggedContextDivider bool
	deck                  layoutcore.Deck
	workspaceViewCacheID  layoutcore.WorkspaceID
	workspaceViewRevision uint64
	workspaceViewBuilds   uint64
	pendingWorkspace      layoutcore.WorkspaceID
	pendingPane           layoutcore.PaneID

	// Prompt state, filled when a connection asks a question.
	hostKeyDescription       string
	passwordFields           []text.Editor
	passwordRects            []ui.Rect
	passwordBoxes            []flex.Box
	passwordQuestions        []sshconn.Question
	passwordError            string
	passwordDescription      string
	keyPassphrase            text.Editor
	keyPassphraseError       string
	keyPassphraseDescription string
	clipboardRequest         remoteClipboardRequest
	autofillRules            autofillUI
	commandPalette           commandPalette
	// ai is the built-in assistant; see ai_state.go.
	ai aiState
}

// Close releases connections, transfers, monitors, and forwards owned by this
// window. It is idempotent and is called by app.Config.OnClosed.
func (state *State) Close() {
	if state == nil || state.closed {
		return
	}
	state.closed = true
	if state.update.install.cancel != nil {
		state.update.install.cancel()
		state.update.install.cancel = nil
	}
	state.stopUpdateCheck()
	if state.options.Group != nil {
		state.options.Group.leave(state)
	}
	if state.stopAppearanceWatch != nil {
		state.stopAppearanceWatch()
	}
	state.closeWindowChrome()
	state.closeAI()
	state.closeHistory()
	state.stopSyncTimer()
	if state.config != nil {
		state.config.Close()
	}
	state.dropExternalEdits(0)
	for index := range state.sessions {
		session := &state.sessions[index]
		state.cancelReconnect(session)
		state.closeSFTP(session)
		state.stopRemoteStats(session)
		if session.term != nil && session.term.shell != nil {
			releaseShell(session.term.shell)
		}
		session.term.abandonShell()
		if session.conn != nil {
			session.conn.Cancel()
		}
	}
	for index := range state.forwards {
		state.forwards[index].stop()
	}
}

// New returns the workspace. Options wire it to the outside world: where
// trusted host keys and the saved hosts live, how connections are dialed, and
// how a background change reaches the window. The saved hosts and the user's
// language and theme choices are loaded here.
func New(options Options) *State {
	messages := i18n.For(i18n.English)
	state := &State{
		options:            options.withDefaults(),
		nextSessionID:      1,
		nextHostID:         1,
		nextIdentityID:     1,
		nextForwardID:      1,
		forwardStarts:      make(chan forwardStartResult, 16),
		nextProxyID:        1,
		activeSection:      sectionHosts,
		language:           i18n.English,
		messages:           messages,
		status:             messages.StatusReadyIdle,
		systemUser:         systemLogin(),
		viewport:           ui.Size{Width: 1400, Height: 900},
		terminalFontSize:   termFontSize,
		terminalScrollback: scrollbackLines,
		connectionTimeout:  10 * time.Second,
		sftpParallel:       DefaultSFTPParallel,
		terminalClipboard:  terminalClipboardAsk,
		shortcuts:          defaultShortcuts(),
		terminalScheme:     defaultSchemeID,
		syncDelay:          syncDelayDefault,
		syncRetryBase:      syncRetryBaseDefault,
		update:             updateState{firstDelay: updateFirstDelayDefault, every: updateIntervalDefault},
		winChrome:          windowChrome{remember: options.RememberWindowFrame},
	}
	state.cellWidth, state.cellHeight = (&ui.Canvas{}).MonoCellSize(state.terminalFontSize, state.terminalFontFace)
	// An icon-only button names itself to the keyboard as well as the pointer.
	state.tooltip.ShowOnFocus()
	state.setAppearance(appearanceDark)
	state.recompileHighlights()
	state.initKnownHosts()
	state.initSecrets()

	state.starting = true
	state.configDefaults = defaultsOf(state.snapshot())
	config, saved, configBase, report, err := openConfig(state.options.StatePath)
	state.config, state.configBase = config, configBase
	if config != nil && state.secrets.vault != nil {
		// Saved passwords are versioned by the same clock as the records.
		state.secrets.vault.SetStamper(func() (string, error) {
			stamp, err := config.NextStamp()
			if err == nil {
				// A password changed: it is shared, so other computers will want it.
				state.requestSync()
			}
			return string(stamp), err
		})
	}
	if state.options.Blank {
		// Settings and hosts, but none of the tabs the last session left.
		saved.Sessions, saved.Deck, saved.ActiveSession, saved.Workspace = nil, nil, 0, ""
	}
	state.restore(saved)
	if report.outcome == loadOK && err == nil {
		backupState(state.options.StatePath, saved)
	}
	// Dialing waits for the first Wake: the window does not exist yet, so the
	// connection's wake-ups would have nowhere to go and its progress would
	// never reach the screen.
	state.restorePending = state.restoreConnect != restoreConnectOff
	switch {
	case err != nil:
		state.status = fmt.Sprintf(state.messages.StatusSaveFailedFmt, err)
	default:
		state.status = state.messages.StatusReadyIdle
	}
	state.syncPending = state.sync.configured() && !state.sync.manual
	state.stateFileOutcome = report.describe()
	state.explainStateFile(report)
	state.explainFailedUpdate()
	state.syncVisible()
	state.refreshHostGroups()
	state.refreshHostSelection()
	state.initAI()
	if state.bg.waiting {
		state.decodeBackground(state.bg.serial)
	}
	state.validateTerminalFont()
	state.loginOn = state.settingsLoginEnabled()
	state.starting = false
	if state.options.Group != nil {
		state.options.Group.join(state)
	}
	state.sshSyncStart()
	return state
}

// setAppearance selects the palette and rebuilds the component theme once, so
// painting only reads it.
// resolveAppearance is the look the current mode asks for: the pinned one, or the
// system's, dark when the system does not say.
func (state *State) resolveAppearance() appearance {
	switch state.appearanceMode {
	case appearancePinnedLight:
		return appearanceLight
	case appearancePinnedDark:
		return appearanceDark
	}
	if state.options.SystemAppearance() == app.AppearanceLight {
		return appearanceLight
	}
	return appearanceDark
}

// chooseAppearanceMode makes the user's choice, pinning a look or following the
// system again, and reports whether anything changed.
func (state *State) chooseAppearanceMode(mode appearanceMode) bool {
	changed := state.appearanceMode != mode
	state.appearanceMode = mode
	state.syncNativeAppearance() // before the system is read: a pin hides it
	if look := state.resolveAppearance(); look != state.appearance {
		state.setAppearance(look)
		changed = true
	}
	return changed
}

// syncNativeAppearance has what the system draws around the window (its buttons,
// menus, file dialogs, native text editing) follow a pinned look, or the system
// when the look is not pinned.
func (state *State) syncNativeAppearance() {
	switch state.appearanceMode {
	case appearancePinnedLight:
		state.options.SetNativeAppearance(app.AppearanceLight)
	case appearancePinnedDark:
		state.options.SetNativeAppearance(app.AppearanceDark)
	default:
		state.options.SetNativeAppearance(app.AppearanceUnknown)
	}
}

// systemAppearanceChanged is what the system's switch between light and dark
// comes to. A user who pinned a look keeps it.
func (state *State) systemAppearanceChanged() {
	if state.closed || state.appearanceMode != appearanceFollowSystem {
		return
	}
	if look := state.resolveAppearance(); look != state.appearance {
		state.setAppearance(look)
		if invalidate := state.options.Invalidate; invalidate != nil {
			invalidate()
		}
	}
}

func (state *State) setAppearance(appearance appearance) {
	state.appearance = appearance
	state.pal = appearance.palette()
	state.theme = state.pal.theme()
	state.rebuildTerminalLook()
}

// systemLogin is the account name SSH uses for a host that names none.
func systemLogin() string {
	if current, err := user.Current(); err == nil && current.Username != "" {
		return current.Username
	}
	return os.Getenv("USER")
}

// login is the account a connection to host signs in as.
func (state *State) login(host Host) string {
	if host.User != "" {
		return host.User
	}
	return state.systemUser
}

// WindowColor is the color behind everything the workbench paints; the window
// is cleared with it so edges that are exposed before a repaint match.
func (state *State) WindowColor() ui.Color {
	return state.pal.shell
}

// Messages exposes the active string catalog for the runtime text-input
// resolver without exposing the language field for mutation.
func (state *State) Messages() i18n.Messages {
	return state.messages
}

// EditorFor returns the editor behind a text field hit, or nil when the hit is
// not an open dialog's field. The runtime reports which hit holds keyboard
// focus; this maps it to the text it edits.
func (state *State) EditorFor(hit ui.ID) *text.Editor {
	if _, local, ok := state.decodePaneHit(hit); ok {
		hit = local
	}
	if editor := state.aiEditor(hit); editor != nil {
		return editor
	}
	switch state.modal {
	case modalNone:
		if editor := state.sftpEditor(hit); editor != nil {
			return editor
		}
		if hit == hitSearch && state.activeSection == sectionHosts {
			return &state.search
		}
		if hit == hitSettingsSearch && state.activeSection == sectionSettings {
			return &state.settings.search
		}
		if hit == hitKnownHostsSearch && state.activeSection == sectionKnownHosts && state.knownHosts.action == knownHostsActionNone {
			return &state.knownHosts.search
		}
	case modalNewHost:
		if isDialogField(hit) {
			return state.editor.field(fieldForDialogHit(hit))
		}
		if hit == hitDialogAlgorithms && state.editor.protocol == protocolSSH && state.editor.algoMode == algoCustom {
			return &state.editor.algorithms
		}
		if hit == hitDialogOTP && state.editor.protocol == protocolSSH {
			return &state.editor.otp
		}
	case modalMoveHostGroup:
		if hit == hitMoveGroupField {
			return &state.hostGroupEditor
		}
	case modalRenameTab:
		if hit == hitTabRenameField {
			return &state.tabRename.editor
		}
	case modalAIRename:
		if hit == hitAIRenameField {
			return &state.ai.rename.editor
		}
	case modalNewForward:
		if hit == hitForwardDialogLabel || hit == hitForwardDialogLocalPort || hit == hitForwardDialogDestination || hit == hitForwardDialogDestinationPort {
			return state.forwardEditor.field(forwardDialogField(hit))
		}
	case modalPassword:
		if index, ok := hit.In(hitPasswordFields); ok && index >= 1 && index-1 < len(state.passwordFields) {
			return &state.passwordFields[index-1]
		}
	case modalKeyPassphrase:
		if hit == hitKeyPassphraseField {
			return &state.keyPassphrase
		}
	case modalVaultUnlock:
		if hit == hitVaultUnlockField {
			return &state.secrets.unlock
		}
	case modalFontPicker:
		if hit == hitFontPickerSearch {
			return &state.fonts.search
		}
	case modalOutboundProxy:
		switch hit {
		case hitOutboundProxyAddress:
			return &state.proxyForm.address
		case hitOutboundProxyUser:
			return &state.proxyForm.user
		case hitOutboundProxyPassword:
			return &state.proxyForm.password
		case hitOutboundProxyBypass:
			return &state.proxyForm.bypass
		}
	case modalForm:
		return state.formEditor(hit)
	case modalAutofillRule:
		switch hit {
		case hitAutofillName:
			return &state.autofillRules.name
		case hitAutofillPattern:
			return &state.autofillRules.pattern
		case hitAutofillFixed:
			return &state.autofillRules.fixed
		case hitAutofillScope:
			return &state.autofillRules.scope
		}
	case modalHighlightRule:
		if hit == hitHighlightPattern {
			return &state.highlighting.pattern
		}
	case modalSnippet:
		switch hit {
		case hitSnippetName:
			return &state.snippets.name
		case hitSnippetCommand:
			return &state.snippets.command
		}
	case modalMasterPassword:
		switch hit {
		case hitMasterPasswordNew:
			return &state.secrets.newPassword
		case hitMasterPasswordConfirm:
			return &state.secrets.confirmPassword
		}
	case modalCommandPalette:
		if hit == hitCommandPaletteSearch {
			return &state.commandPalette.search
		}
	case modalHistory:
		if hit == hitHistorySearch {
			return &state.history.search
		}
	}
	if editor := state.terminalFindEditor(hit); editor != nil {
		return editor
	}
	return nil
}

// Theme returns the component theme for the current appearance. It is built
// when the appearance changes, not per paint.
func (state *State) Theme() ui.Theme {
	return state.theme
}

// WindowControlsStyle styles the minimize, maximize and close buttons Viva draws
// on Windows and Linux: the bar's own colors and the current language's names.
// The window reads it at every paint, so a theme or language change shows.
func (state *State) WindowControlsStyle() app.WindowControlsStyle {
	pal, m := state.pal, state.messages
	return app.WindowControlsStyle{
		Foreground: pal.text, Hover: pal.tabHover, Pressed: pal.tabActive,
		Minimize: m.LabelWindowMinimize, Maximize: m.LabelWindowMaximize,
		Restore: m.LabelWindowRestore, CloseName: m.LabelWindowClose,
	}
}

// Click applies one stable hit action.
func (state *State) Click(id ui.ID) {
	defer state.runLaunches()
	defer state.sshSyncCheck()
	if session, local, ok := state.decodePaneHit(id); ok {
		if state.activeSessionID != session.ID && state.ActivatePane(session.ID) {
			state.save()
		}
		id = local
	}
	messages := state.messages
	if state.aiClick(id) {
		return
	}
	if state.knownHosts.action != knownHostsActionNone {
		state.clickKnownHostsDialog(id)
		return
	}
	switch state.modal {
	case modalNewHost:
		state.clickHostDialog(id)
		return
	case modalDeleteHost:
		state.clickDeleteHostDialog(id)
		return
	case modalMoveHostGroup:
		state.clickMoveHostGroupDialog(id)
		return
	case modalRenameTab:
		state.clickTabRenameDialog(id)
		return
	case modalAIRename:
		state.clickAIRenameDialog(id)
		return
	case modalCloseTabs:
		state.clickCloseTabsDialog(id)
		return
	case modalHostImportResult:
		state.clickHostImportResult(id)
		return
	case modalNewForward:
		state.clickForwardDialog(id)
		return
	case modalNewProxy:
		state.clickProxyDialog(id)
		return
	case modalHostKey:
		state.clickHostKey(id)
		return
	case modalPassword:
		state.clickPassword(id)
		return
	case modalKeyPassphrase:
		state.clickKeyPassphrase(id)
		return
	case modalVaultUnlock:
		state.clickVaultUnlock(id)
		return
	case modalMasterPassword:
		state.clickMasterPassword(id)
		return
	case modalHighlightRule:
		state.clickHighlightDialog(id)
		return
	case modalAutofillRule:
		state.clickAutofillDialog(id)
		return
	case modalAutofillConfirm:
		state.clickAutofillConfirm(id)
		return
	case modalSnippet:
		state.clickSnippetDialog(id)
		return
	case modalImportSessions:
		state.clickImportSessions(id)
		return
	case modalFontPicker:
		state.clickFontPicker(id)
		return
	case modalOutboundProxy:
		state.clickOutboundProxy(id)
		return
	case modalForm:
		state.clickForm(id)
		return
	case modalRemoteTools:
		state.clickRemoteTools(id)
		return
	case modalVaultForget:
		state.clickVaultForget(id)
		return
	case modalRemoteClipboard:
		state.clickRemoteClipboard(id)
		return
	case modalFileTransfer:
		state.clickTransfer(id)
		return
	case modalXYTransfer:
		state.clickXYSetup(id)
		return
	case modalOpenLink:
		state.clickLaunchDialog(id)
		return
	case modalStateNotice:
		state.clickStateNotice(id)
		return
	case modalUpdate:
		state.clickUpdateDialog(id)
		return
	case modalUpdateProgress:
		state.clickUpdateProgress(id)
		return
	case modalCommandPalette:
		state.clickCommandPalette(id)
		return
	case modalHistory:
		state.clickHistory(id)
		return
	case modalShortcutCapture:
		state.clickShortcutCapture(id)
		return
	}
	if state.clickTerminalMenu(id) || state.clickTabMenu(id) || state.clickHostMenu(id) || state.clickSFTPMenu(id) {
		return
	}
	if state.clickTerminalFind(id) {
		return
	}
	if state.clickRemoteStats(id) {
		return
	}
	if state.clickDesktop(id) {
		return
	}
	if state.clickHostManagement(id) {
		return
	}
	if state.clickSettingsCategory(id) {
		return
	}
	if state.clickFilesSettings(id) {
		return
	}
	if state.clickLaunchSettings(id) {
		return
	}
	if state.clickTerminalTools(id) || state.clickBackupSettings(id) || state.clickRemoteToolsOpen(id) || state.clickHostView(id) || state.clickBackground(id) {
		return
	}

	switch {
	case id == hitTerminalRecord:
		if session, ok := state.sessionByIDPointer(state.activeSessionID); ok {
			state.toggleTerminalRecording(session)
		}
	case id == hitTerminalRecordingExport:
		if session, ok := state.sessionByIDPointer(state.activeSessionID); ok {
			state.exportTerminalRecording(session)
		}
	case id.Scope() == connHits:
		state.clickConnection(id)
	case id == hitHosts || id == hitHome:
		if session, ok := state.sessionByIDPointer(state.activeSessionID); ok {
			state.closeSFTP(session)
		}
		state.activeSection = sectionHosts
		state.save()
	case id == hitNewTab:
		state.openConnectPicker()
	case id == hitNewWindow:
		state.openNewWindow()
	case id == hitSearch:
		// Pressing the box only focuses it; the runtime already did.
	case id == hitKeychain:
		state.activeSection = sectionKeys
		state.save()
	case id == hitProxies:
		state.activeSection = sectionProxies
		state.save()
	case id == hitPortForwarding:
		state.activeSection = sectionForwards
		state.save()
	case id == hitKnownHosts:
		state.openKnownHosts()
	case state.devClick(id):
		// The development-only component gallery handled it; a release build has
		// no such page, so this is never its click.
	case id == hitSettings || id == hitWindowSettings:
		state.openSettings()
	case id == hitShortcutReset:
		state.shortcuts = defaultShortcuts()
		state.save()
	case id == hitConnect:
		state.connectTyped()
	case id == hitAddHost || id == hitAddHostEmpty:
		state.openHostDialog()
	case id == hitImportSSHConfig:
		state.importSSHConfig()
	case id == hitLocalTerminal || id == hitWindowLocalTerminal:
		state.openLocalTerminal()
	case id == hitLanguage:
		if state.chooseLanguage(state.language.Next()) {
			state.save()
		}
	case id == hitTheme:
		// The button flips the look that is showing, which is a choice.
		if state.appearance.toggled() == appearanceLight {
			state.chooseAppearanceMode(appearancePinnedLight)
		} else {
			state.chooseAppearanceMode(appearancePinnedDark)
		}
		state.save()
	case id == hitCommandPalette:
		state.openCommandPalette()
	case id == hitHistoryOpen:
		state.openHistoryActive()
	case id == hitSettingsHistoryToggle:
		state.toggleHistory()
	case id == hitSettingsSuggestToggle:
		state.toggleSuggestions()
	case id == hitSettingsHistoryClear:
		state.askClearHistory()
	case id == hitPanePrevious:
		if state.activateRelativePane(-1) {
			state.save()
		}
	case id == hitPaneNext:
		if state.activateRelativePane(1) {
			state.save()
		}
	case id == hitPaneSplitHorizontal:
		state.splitWithCurrentHost(layoutcore.Horizontal)
	case id == hitPaneSplitVertical:
		state.splitWithCurrentHost(layoutcore.Vertical)
	case id == hitPaneClose:
		state.closeActivePane()
	case state.clickNewTabPage(id), state.clickConnectPick(id):
	case id == hitPaneChooseHost:
		state.pendingWorkspace = state.deck.ActiveWorkspace()
		state.pendingPane = state.deck.ActivePane(state.pendingWorkspace)
		state.activeSection = sectionHosts
		state.save()
	case id.Scope() == hitCommandSessionList:
		state.ActivatePane(uint32(item(id)))
		state.save()
	case id.Scope() == hitPaneActivateList:
		index := item(id)
		state.refreshWorkspaceViews()
		if index < len(state.panes) && state.activatePane(state.deck.ActiveWorkspace(), state.panes[index].ID) {
			state.save()
		}
	case id.Scope() == hitHostList:
		hostID := uint32(item(id))
		if _, ok := state.hostByID(hostID); ok {
			state.selectedHostID = hostID
			state.status = messages.StatusSelected + state.hostLabel(hostID)
		}
	case tree.Within(id.Scope(), knownHostsHits):
		state.clickKnownHosts(id)
	case id.Scope() == hitShortcutRowList:
		state.openShortcutCapture(shortcutAction(uint32(item(id))) + 1)
	case tree.Within(id.Scope(), proxiesHits):
		state.clickProxies(id)
	case tree.Within(id.Scope(), settingsHits):
		state.clickSettings(id)
	case tree.Within(id.Scope(), forwardsHits):
		state.clickForwards(id)
	case tree.Within(id.Scope(), keysHits):
		state.clickKeys(id)
	case tree.Within(id.Scope(), sftpHits):
		state.clickSFTP(id)
	case id.Scope() == hitTerminalList:
		// Pressing the terminal only focuses it; the runtime already did.
	case id.Scope() == hitTabCloseList:
		state.closeWorkspace(layoutcore.WorkspaceID(uint32(item(id))))
	case id.Scope() == hitTabList:
		workspaceID := layoutcore.WorkspaceID(uint32(item(id)))
		if workspaceID != 0 {
			if state.activateWorkspace(workspaceID) {
				state.save()
			}
		}
	}
}

func (state *State) openNewWindow() {
	if state.options.NewWindow == nil {
		return
	}
	if err := state.options.NewWindow(); err != nil {
		state.status = fmt.Sprintf(state.messages.StatusNewWindowFailedFmt, err)
	}
}

// closeSession ends a tab: its connection and shell are released and the
// neighbouring tab, or the host list when it was the last, takes over.
func (state *State) closeSession(id uint32) {
	index := -1
	for at := range state.sessions {
		if state.sessions[at].ID == id {
			index = at
		}
	}
	if index < 0 {
		return
	}
	defer state.save()
	session := &state.sessions[index]
	state.cancelReconnect(session)
	state.stopSessionForwards(session.ID, false)
	if session.term != nil && session.term.shell != nil {
		session.term.recording.stop()
		releaseShell(session.term.shell)
	}
	session.term.abandonShell()
	state.dropExternalEdits(session.ID)
	state.closeSFTP(session)
	state.stopRemoteStats(session)
	if session.conn != nil {
		session.conn.Cancel()
	}
	if state.modalSession == id {
		state.closeModal()
	}
	if workspaceID, paneID, ok := state.deck.PaneForSession(layoutcore.SessionID(id)); ok {
		_, _ = state.deck.ClosePane(workspaceID, paneID)
		// The last pane of a tab takes the tab with it.
		if len(state.deck.Panes(workspaceID, nil)) == 0 {
			_, _ = state.deck.RemoveWorkspace(workspaceID, nil)
		}
	}
	// Delete rather than truncating an append: the vacated slot must not keep
	// the closed session's terminal and scrollback reachable.
	state.sessions = slices.Delete(state.sessions, index, index+1)
	state.syncActiveSession()
	if state.deck.ActiveWorkspace() == 0 {
		state.activeSection = sectionHosts
	}
	state.fitTerminals()
	state.fitSFTP(state.viewport)
	state.rebuildTabOptions()
	if state.activeSessionID != 0 && state.activeSessionID != id {
		return
	}
	if len(state.sessions) == 0 {
		state.activeSessionID = 0
		state.activeSection = sectionHosts
		return
	}
	state.activeSessionID = state.sessions[min(index, len(state.sessions)-1)].ID
}

func (state *State) closeActivePane() {
	workspaceID := state.deck.ActiveWorkspace()
	paneID := state.deck.ActivePane(workspaceID)
	pane, ok := state.deck.Pane(workspaceID, paneID)
	if !ok {
		return
	}
	if pane.SessionID != 0 {
		state.closeSession(uint32(pane.SessionID))
		return
	}
	_, _ = state.deck.ClosePane(workspaceID, paneID)
	state.syncActiveSession()
	state.refreshWorkspaceViews()
	state.save()
}

// closeActivePaneOrTab is what Command+W does: in a split workspace it closes
// the active pane, and a workspace of one pane closes its tab. A page with no
// terminal on show (the host list, with or without tabs open behind it) has
// nothing of that kind to close, so the window closes instead.
func (state *State) closeActivePaneOrTab() {
	workspaceID := state.deck.ActiveWorkspace()
	if workspaceID == 0 || state.activeSection != sectionTerminal {
		if state.options.CloseWindow != nil {
			state.options.CloseWindow()
		}
		return
	}
	if len(state.deck.Panes(workspaceID, nil)) > 1 {
		state.closeActivePane()
		return
	}
	state.closeWorkspace(workspaceID)
}

func (state *State) closeWorkspace(id layoutcore.WorkspaceID) {
	sessions, err := state.deck.RemoveWorkspace(id, nil)
	if err != nil {
		return
	}
	for _, sessionID := range sessions {
		state.closeSession(uint32(sessionID))
	}
	state.syncActiveSession()
	state.rebuildTabOptions()
	if state.deck.ActiveWorkspace() == 0 {
		state.activeSection = sectionHosts
	} else {
		state.activeSection = sectionTerminal
	}
	state.save()
}

// canReconnect reports whether a tab has nothing live left to talk to.
func (state *State) canReconnect(session *Session) bool {
	return session.restored || session.term != nil && session.term.shell == nil &&
		(session.term.ended || session.phase == sshconn.PhaseFailed || session.phase == sshconn.PhaseClosed)
}

// reconnect dials a tab's host again in the same tab.
func (state *State) reconnect(session *Session) {
	state.startConnection(session, session.host)
	state.status = state.messages.StatusOpened + session.Title
}

// clickHostDialog handles the New Host dialog. Clicks on its fields only move
// focus, which the runtime already did; clicks anywhere else are swallowed.
func (state *State) clickHostDialog(id ui.ID) {
	switch id {
	case hitDialogDismiss, hitDialogClose, hitDialogCancel:
		state.editor.password = text.Editor{}
		state.modal = modalNone
		state.dialogError = ""
		state.status = state.messages.StatusEditCanceled
		state.showNextPrompt()
	case hitDialogSubmit:
		state.submitHostDialog()
	case hitHostProxyConfigure:
		if state.editor.protocol != protocolSerial {
			state.openHostProxyDialog()
		}
	default:
		state.clickHostProtocol(id)
	}
}

// openHostDialog resets the form and shows the modal.
func (state *State) openHostDialog() {
	state.editor.reset()
	state.hostDialogMode = hostDialogAdd
	state.hostDialogID = 0
	state.dialogError = ""
	state.modal = modalNewHost
}

// syncVisible rebuilds the list of hosts the search box lets through when the
// text or the host list changed since it was last built, and does nothing
// otherwise, so painting an unchanged window stays free of work.
func (state *State) syncVisible() {
	query := state.search.String()
	if query == state.visibleQuery && len(state.hosts) == state.visibleHosts && state.visible != nil {
		return
	}
	state.visibleQuery, state.visibleHosts = query, len(state.hosts)
	needle := strings.ToLower(strings.TrimSpace(query))
	state.visible = state.visible[:0]
	if state.visible == nil {
		state.visible = make([]int, 0, len(state.hosts))
	}
	for index, host := range state.hosts {
		if needle == "" || matchesHost(host, needle) {
			state.visible = append(state.visible, index)
		}
	}
	state.refreshHostSelection()
	state.rebuildHostTreeRows()
}

// matchesHost reports whether needle (lower case) occurs in any text the host
// shows.
func matchesHost(host Host, needle string) bool {
	for _, value := range [...]string{host.Label, host.Address, host.User, host.Group, host.Tags, host.Icon.String()} {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

// hostIsVisible reports whether the search box currently lets a host through.
func (state *State) hostIsVisible(id uint32) bool {
	for _, index := range state.visible {
		if state.hosts[index].ID == id {
			return true
		}
	}
	return false
}

// typedQuery is the text in the search box without surrounding blanks.
func (state *State) typedQuery() string {
	return strings.TrimSpace(state.search.String())
}

// connectTyped connects to what the search box holds: a saved host named
// exactly by its label or address, or else a target written as
// [user@]host[:port]. Text that is neither says so in the status line and
// connects nowhere.
func (state *State) connectTyped() {
	query := state.typedQuery()
	if query == "" {
		return
	}
	var named []Host
	for _, host := range state.hosts {
		if strings.EqualFold(host.Label, query) || strings.EqualFold(host.Address, query) {
			named = append(named, host)
		}
	}
	if len(named) > 0 {
		chosen := named[0]
		for _, host := range named {
			if host.ID == state.selectedHostID {
				chosen = host
			}
		}
		state.selectedHostID = chosen.ID
		state.openHostSession(chosen)
		return
	}
	parsed, err := parseConnection(query)
	if err != nil {
		state.status = fmt.Sprintf(state.messages.ErrBadTargetFmt, query)
		return
	}
	state.connectParsed(parsed)
}

// connectParsed opens a target that was typed or came from outside: the saved
// host it spells out, or else a temporary one.
func (state *State) connectParsed(parsed connection) {
	// Typing out a saved host in full is opening that host.
	for _, host := range state.hosts {
		if host.Protocol == parsed.protocol && strings.EqualFold(host.Address, parsed.host) && host.Port == parsed.port &&
			(parsed.user == "" || parsed.user == host.User) {
			state.selectedHostID = host.ID
			state.openHostSession(host)
			return
		}
	}
	adhoc := Host{Label: parsed.host, Address: parsed.host, User: parsed.user, Port: parsed.port, Protocol: parsed.protocol}
	adhoc.finish()
	adhoc.Label = adhoc.target
	state.openHostSession(adhoc)
}

// hostForRow maps a host row's hit identity back to its host.
func (state *State) hostForRow(id ui.ID) (Host, bool) {
	if id.Scope() != hitHostList {
		return Host{}, false
	}
	return state.hostByID(uint32(item(id)))
}

// Pointer handles the pointer events the runtime does not turn into clicks. A
// double click on a host row connects to it.
func (state *State) Pointer(event ui.PointerEvent) (outcome ui.EventResult) {
	defer state.afterEvent(&outcome)
	if state.aiTrackPointer(event) {
		defer func() { outcome.Repaint = true }()
	}
	// The menu opens where the pointer is in the window, which the pane
	// translation below would otherwise hide.
	state.terminalMenu.pointer = ui.Point{X: event.X, Y: event.Y}
	if result, handled := state.menuPointer(event); handled {
		return result
	}
	if result, handled := state.terminalMenuPointer(event); handled {
		return result
	}
	if result, handled := state.tabMenuPointer(event); handled {
		return result
	}
	if result, handled := state.tabDragPointer(event); handled {
		return result
	}
	if result, handled := state.hostMenuPointer(event); handled {
		return result
	}
	if result, handled := state.sftpMenuDismissPointer(event); handled {
		return result
	}
	if result, handled := state.aiPointer(event); handled {
		return result
	}
	if result, handled := state.contextDividerPointer(event); handled {
		return result
	}
	if result, handled := state.paneDividerPointer(event); handled {
		return result
	}
	if session, local, ok := state.decodePaneHit(event.Target); ok {
		mount, _ := tree.MountOf(event.Target.Scope())
		// A pane becomes the active one when it is pressed, not when the pointer
		// merely passes over it.
		changed := false
		if event.Kind == ui.PointerDown {
			changed = state.activeSessionID != session.ID
			state.ActivatePane(session.ID)
		}
		_, origin, viewport, embedded := state.paneEventGeometry(session.ID, event.Viewport)
		if embedded {
			event.X -= origin.X
			event.Y -= origin.Y
			event.Viewport = viewport
		}
		event.Target = local
		result := state.pointer(event)
		result.Focus = tree.MountID(mount, result.Focus)
		result.Repaint = result.Repaint || changed
		if changed && event.Kind == ui.PointerDown {
			state.save()
		}
		return result
	}
	if session := state.terminalSession(event.Target); session != nil {
		_, origin, viewport, embedded := state.paneEventGeometry(session.ID, event.Viewport)
		if embedded {
			event.X -= origin.X
			event.Y -= origin.Y
			event.Viewport = viewport
		}
	}
	return state.pointer(event)
}

func (state *State) pointer(event ui.PointerEvent) ui.EventResult {
	if result, handled := state.terminalFindPointer(event); handled {
		return result
	}
	if session := state.terminalSession(event.Target); session != nil && state.modal == modalNone && state.activeSection == sectionTerminal {
		if session.desk != nil {
			return state.desktopPointer(session, event)
		}
		return state.terminalPointer(session, event)
	}
	if result, handled := state.sftpMenuPointer(event); handled {
		return result
	}
	if result, handled := state.sftpRowPointer(event); handled {
		return result
	}
	return ui.EventResult{}
}

// isDialogField reports whether a hit is one of the New Host dialog's text fields.
func isDialogField(id ui.ID) bool {
	switch id {
	case hitDialogLabel, hitDialogAddress, hitDialogUser, hitDialogPassword, hitDialogGroup, hitDialogPort, hitDialogTags, hitDialogColor:
		return true
	}
	return false
}

// fieldForDialogHit maps a dialog hit id back to its form field.
func fieldForDialogHit(id ui.ID) hostField {
	switch id {
	case hitDialogLabel:
		return fieldLabel
	case hitDialogAddress:
		return fieldAddress
	case hitDialogUser:
		return fieldUser
	case hitDialogPassword:
		return fieldPassword
	case hitDialogGroup:
		return fieldGroup
	case hitDialogTags:
		return fieldTags
	case hitDialogColor:
		return fieldColor
	default:
		return fieldPort
	}
}

// Key provides roving keyboard focus for terminal tabs.
func (state *State) Key(event ui.KeyEvent) (outcome ui.EventResult) {
	defer state.afterEvent(&outcome)
	if state.tabDrag.active && event.Key == ui.KeyEscape {
		state.tabDrag = tabDrag{}
		return ui.EventResult{Handled: true, Repaint: true}
	}
	var mount ui.Scope
	if session, local, ok := state.decodePaneHit(event.Target); ok {
		state.ActivatePane(session.ID)
		mount, _ = tree.MountOf(event.Target.Scope())
		event.Target = local
	}
	result := state.key(event)
	result.Focus = tree.MountID(mount, result.Focus)
	return result
}

func (state *State) key(event ui.KeyEvent) ui.EventResult {
	// A tooltip is the topmost transient thing when no popup menu is open:
	// Escape hides it before it can reach a dialog or the page.
	if event.Key == ui.KeyEscape && state.openMenuScroll() == nil && state.tooltip.Dismiss() {
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if state.terminalMenu.open && event.Key == ui.KeyEscape {
		state.terminalMenu.open = false
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if result, handled := state.menuKey(event); handled {
		return result
	}
	if result, handled := state.selectTypeaheadKey(event); handled {
		return result
	}
	if result, handled := state.restoredPageKey(event); handled {
		return result
	}
	if state.modal == modalShortcutCapture && event.Key == ui.KeyEscape && !event.Repeat {
		state.clickShortcutCapture(hitShortcutCaptureCancel)
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if result, handled := state.aiKey(event); handled {
		return result
	}
	if result, handled := state.commandPaletteKey(event); handled {
		return result
	}
	if result, handled := state.historyKey(event); handled {
		return result
	}
	if result, handled := state.knownHostsKey(event); handled {
		return result
	}
	if result, handled := state.updateKey(event); handled {
		return result
	}
	if result, handled := state.launchKey(event); handled {
		return result
	}
	if result, handled := state.hostManagementKey(event); handled {
		return result
	}
	if state.modal == modalNewHost && event.Key == ui.KeyEnter && event.Modifiers == 0 && state.EditorFor(event.Target) != nil {
		_, composing := state.EditorFor(event.Target).Composition()
		if !event.Repeat && !composing {
			state.submitHostDialog()
		}
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if result, handled := state.aiRenameKey(event); handled {
		return result
	}
	if result, handled := state.tabRenameKey(event); handled {
		return result
	}
	if result, handled := state.closeTabsKey(event); handled {
		return result
	}
	if result, handled := state.terminalFindKey(event); handled {
		return result
	}
	if result, handled := state.sftpKey(event); handled {
		return result
	}
	if result, handled := state.fontPickerKey(event); handled {
		return result
	}
	if result, handled := state.settingsKey(event); handled {
		return result
	}
	if session := state.terminalSession(event.Target); session != nil && state.modal == modalNone {
		if session.desk != nil {
			return state.desktopKey(session, event)
		}
		return state.terminalKey(session, event)
	}
	if result, handled := state.settingsSearchKey(event); handled {
		return result
	}
	if result, handled := state.hostsKey(event); handled {
		return result
	}
	if state.modal == modalNewForward && (event.Target == hitForwardModeLocal || event.Target == hitForwardModeRemote || event.Target == hitForwardModeSOCKS5 || event.Target == hitForwardModeHTTPConnect) {
		options := state.forwardModeOptions()
		next, handled := components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
		if handled {
			state.Click(next)
			return ui.EventResult{Focus: next, FocusVisible: true, Handled: true, Repaint: true}
		}
	}
	if state.modal == modalNewForward && event.Key == ui.KeyEnter && state.EditorFor(event.Target) != nil && !event.Repeat {
		state.submitForwardDialog()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if state.modal == modalPassword && event.Key == ui.KeyEnter && !event.Repeat && event.Target.Scope() == hitPasswordFields {
		state.submitPassword()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if state.modal == modalVaultUnlock && event.Key == ui.KeyEnter && event.Target == hitVaultUnlockField && !event.Repeat {
		state.submitVaultUnlock()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if state.modal == modalOutboundProxy && event.Key == ui.KeyEnter && !event.Repeat &&
		(event.Target == hitOutboundProxyAddress || event.Target == hitOutboundProxyUser || event.Target == hitOutboundProxyPassword || event.Target == hitOutboundProxyBypass) {
		state.submitOutboundProxy()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if state.modal == modalForm && event.Key == ui.KeyEnter && !event.Repeat && state.formEditor(event.Target) != nil {
		state.submitForm()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if state.modal == modalAutofillRule && event.Key == ui.KeyEnter && !event.Repeat &&
		(event.Target == hitAutofillName || event.Target == hitAutofillPattern || event.Target == hitAutofillFixed || event.Target == hitAutofillScope) {
		state.submitAutofillRule()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if state.modal == modalHighlightRule && event.Key == ui.KeyEnter && event.Target == hitHighlightPattern && !event.Repeat {
		state.submitHighlightRule()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if state.modal == modalSnippet && event.Key == ui.KeyEnter && (event.Target == hitSnippetName || event.Target == hitSnippetCommand) && !event.Repeat {
		state.submitSnippet()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if state.modal == modalMasterPassword && event.Key == ui.KeyEnter && (event.Target == hitMasterPasswordNew || event.Target == hitMasterPasswordConfirm) && !event.Repeat {
		state.submitMasterPassword()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if state.modal == modalKeyPassphrase && event.Key == ui.KeyEnter && event.Target == hitKeyPassphraseField && !event.Repeat {
		state.submitKeyPassphrase()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if event.Target.Scope() != hitTabList {
		return ui.EventResult{}
	}
	next, handled := components.TabsKeyDown(
		event.Target,
		state.tabOptions,
		components.TabsHorizontal,
		components.DirectionLTR,
		event.Key,
	)
	if !handled {
		return ui.EventResult{}
	}
	// Base UI tabs move focus on arrows and wait for Enter or Space to activate
	// the focused tab; the runtime's activation delivers that click.
	return ui.EventResult{Focus: next, FocusVisible: true, Handled: true, Repaint: true}
}

func (state *State) openHostSession(host Host) *Session {
	address := state.sessionAddress(host)
	for index := range state.sessions {
		session := &state.sessions[index]
		// A saved host owns its tab; a typed target is reused by its address.
		if session.HostID != host.ID || host.ID == 0 && session.Address != address {
			continue
		}
		state.activeSessionID = session.ID
		state.activeSection = sectionTerminal
		if state.canReconnect(session) {
			state.startConnection(session, host) // reconnect in the same tab
			state.status = state.messages.StatusOpened + session.Title
		} else {
			state.status = state.messages.StatusReused + session.Title
		}
		state.save()
		return session
	}
	return state.createHostSession(host, address)
}

// newHostSession always opens another tab. Host rows use this path because
// choosing an existing session belongs to the tab strip, not the host list.
func (state *State) newHostSession(host Host) *Session {
	address := state.sessionAddress(host)
	return state.createHostSession(host, address)
}

func (state *State) createHostSession(host Host, address string) *Session {
	workspaceID := state.pendingWorkspace
	paneID := state.pendingPane
	state.pendingWorkspace, state.pendingPane = 0, 0
	if pane, ok := state.deck.Pane(workspaceID, paneID); !ok || pane.SessionID != 0 {
		var err error
		workspaceID, paneID, err = state.deck.Create(host.Label)
		if err != nil {
			return nil
		}
	}
	state.sessions = append(state.sessions, Session{
		ID:       state.nextSessionID,
		HostID:   host.ID,
		Title:    host.Label,
		Address:  address,
		paneSlot: state.claimPaneSlot(),
	})
	session := &state.sessions[len(state.sessions)-1]
	if err := state.deck.Bind(workspaceID, paneID, layoutcore.SessionID(session.ID)); err != nil {
		state.sessions = state.sessions[:len(state.sessions)-1]
		return nil
	}
	state.nextSessionID++
	state.activeSessionID = session.ID
	state.activeSection = sectionTerminal
	state.status = state.messages.StatusOpened + session.Title
	state.startConnection(session, host)
	state.fitTerminals()
	state.fitSFTP(state.viewport)
	state.rebuildTabOptions()
	state.save()
	return session
}

func (state *State) rebuildTabOptions() {
	state.workspaceTabs = state.deck.Workspaces(state.workspaceTabs[:0])
	if cap(state.tabOptions) < len(state.workspaceTabs) {
		state.tabOptions = make([]components.TabOption, len(state.workspaceTabs))
	} else {
		state.tabOptions = state.tabOptions[:len(state.workspaceTabs)]
	}
	for index, workspace := range state.workspaceTabs {
		state.tabOptions[index] = components.TabOption{ID: hitTabList.At(int(workspace.ID)), Label: workspace.Title}
	}
}

func (state *State) selectedHost() (Host, bool) {
	return state.hostByID(state.selectedHostID)
}

func (state *State) hostByID(id uint32) (Host, bool) {
	for _, host := range state.hosts {
		if host.ID == id {
			return host, true
		}
	}
	return Host{}, false
}

func (state *State) sessionByID(id uint32) (Session, bool) {
	for _, session := range state.sessions {
		if session.ID == id {
			return session, true
		}
	}
	return Session{}, false
}

func (state *State) hostLabel(id uint32) string {
	host, _ := state.hostByID(id)
	return host.Label
}

func (state *State) sessionTitle(id uint32) string {
	session, _ := state.sessionByID(id)
	return session.Title
}

// hostsKey handles the keys the host list owns: Enter connects, and Escape
// empties the search box. It reports whether it took the key.
func (state *State) hostsKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if state.modal != modalNone || state.activeSection != sectionHosts || event.Repeat {
		return ui.EventResult{}, false
	}
	handled := ui.EventResult{Handled: true, Repaint: true}
	switch event.Key {
	case ui.KeyEscape:
		if event.Target == hitSearch && state.search.RuneCount() > 0 {
			state.search = text.Editor{}
			state.syncVisible()
			return handled, true
		}
	case ui.KeyEnter:
		switch {
		case event.Target == hitSearch:
			state.connectTyped()
			return handled, true
		case state.isHostRow(event.Target):
			host, _ := state.hostForRow(event.Target)
			state.selectedHostID = host.ID
			state.newHostSession(host)
			return handled, true
		case event.Target.IsZero():
			// Nothing is focused: Enter connects the selected host, provided
			// the search box has not hidden it.
			if host, ok := state.selectedHost(); ok && state.hostIsVisible(host.ID) {
				state.newHostSession(host)
				return handled, true
			}
		}
	}
	return ui.EventResult{}, false
}

func (state *State) isHostRow(id ui.ID) bool {
	_, ok := state.hostForRow(id)
	return ok
}
