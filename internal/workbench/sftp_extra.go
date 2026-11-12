package workbench

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
	"motty/internal/term"
	layoutcore "motty/internal/workspace"
)

// What the file panel does beyond listing, moving and editing: links, bookmarks,
// extracting archives, following the terminal's folder, and the settings that
// shape transfers.

// The panel's ids live in the file panel's range, which clickSFTP serves.
var (
	hitSFTPNewLink         = tree.ID(sftpHits, "sftp-new-link")
	hitSFTPBookmarkToggle  = tree.ID(sftpHits, "sftp-bookmark-toggle")
	hitSFTPBookmarkList    = tree.ID(sftpHits, "sftp-bookmark-list")
	hitSFTPBookmarkDismiss = tree.ID(sftpHits, "sftp-bookmark-dismiss")
	hitSFTPExtract         = tree.ID(sftpHits, "sftp-extract")
	hitSFTPCompress        = tree.ID(sftpHits, "sftp-compress")
	hitSFTPDialogField2    = tree.ID(sftpHits, "sftp-dialog-field2")
)

var (
	hitSettingsParallel1            = tree.ID(sftpHits, "settings-parallel1")
	hitSettingsParallel2            = tree.ID(sftpHits, "settings-parallel2")
	hitSettingsParallel4            = tree.ID(sftpHits, "settings-parallel4")
	hitSettingsKeepTimesToggle      = tree.ID(sftpHits, "settings-keep-times")
	hitSettingsFollowToggle         = tree.ID(sftpHits, "settings-follow")
	hitSettingsTerminalFollowToggle = tree.ID(sftpHits, "settings-terminal-follow")
	hitSettingsPathBreadcrumbs      = tree.ID(sftpHits, "settings-path-breadcrumbs")
	hitSettingsPathInput            = tree.ID(sftpHits, "settings-path-input")
	hitSettingsFollowSetup          = tree.ID(sftpHits, "settings-follow-setup")
	hitSettingsPanelAutoOpen        = tree.ID(sftpHits, "settings-panel-auto-open")
	hitSettingsDirectoryTree        = tree.ID(sftpHits, "settings-directory-tree")
	hitSettingsPanelLeft            = tree.ID(sftpHits, "settings-panel-left")
	hitSettingsPanelRight           = tree.ID(sftpHits, "settings-panel-right")
	hitSettingsWindowLocalLeft      = tree.ID(sftpHits, "settings-window-local-left")
	hitSettingsWindowRemoteLeft     = tree.ID(sftpHits, "settings-window-remote-left")
	hitSettingsOpenWindow           = tree.ID(sftpHits, "settings-open-window")
	hitSettingsOpenTab              = tree.ID(sftpHits, "settings-open-tab")
	hitSettingsCustomEditorChoose   = tree.ID(sftpHits, "settings-custom-editor-choose")
	hitSettingsCustomEditorClear    = tree.ID(sftpHits, "settings-custom-editor-clear")
	hitSettingsConfigureColumns     = tree.ID(sftpHits, "settings-configure-columns")
	hitSettingsDownloadDirectory    = tree.ID(sftpHits, "settings-download-directory")
	hitSettingsDownloadAsk          = tree.ID(sftpHits, "settings-download-ask")
)

var (
	hitSFTPBookmarkItemList   = tree.Sub(sftpHits, tree.Name("sftp-bookmark-item")) // + index of a bookmark of the host
	hitSFTPBookmarkRemoveList = tree.Sub(sftpHits, tree.Name("sftp-bookmark-remove"))
)

const (
	maxBookmarksPerHost = 40
	maxBookmarks        = 500
	maxExtractOutput    = 4096
	extractTimeout      = 10 * time.Minute
	sftpDownloadAsk     = "ask"
)

// sftpBookmark is a folder kept for a host, which the host's catalog id names.
type sftpBookmark struct {
	host, path   string
	name, local  string
	shared, sync bool
}

// clickSFTPExtra handles the presses this file adds to the panel.
func (state *State) clickSFTPExtra(session *Session, view *sftpView, id gfx.ID) bool {
	switch {
	case id == hitSFTPFollowToggle:
		state.toggleSFTPFollow(session)
	case id == hitSFTPPaneToggle:
		state.toggleSFTPPane(view)
	case id == hitSFTPDownloadHere && !view.busy:
		state.downloadHere(view)
	case id == hitSFTPNewLink && !view.busy:
		state.openSFTPLinkDialog(view)
	case id == hitSFTPBookmarkToggle:
		sftpPlaceBrowser{owner: state, session: session, view: view}.toggleDirectoryFavorite(false)
	case id == hitSFTPLocalStar:
		sftpPlaceBrowser{owner: state, session: session, view: view}.toggleDirectoryFavorite(true)
	case id == hitSFTPBookmarkList:
		sftpPlaceBrowser{owner: state, session: session, view: view}.openDirectoryPlaces(false)
	case id == hitSFTPLocalPlaces:
		sftpPlaceBrowser{owner: state, session: session, view: view}.openDirectoryPlaces(true)
	case id == hitSFTPBookmarkDismiss:
		view.bookmarksOpen = false
	case id == hitSFTPExtract && !view.busy && !view.extracting:
		state.openSFTPExtractDialog(view)
	case id == hitSFTPCompress && !view.busy && !view.extracting:
		state.openSFTPCompressDialog(view)
	default:
		return false
	}
	return true
}

// ---- symbolic links ----

func (state *State) openSFTPLinkDialog(view *sftpView) {
	view.dialogEditor, view.dialogEditor2 = text.Editor{}, text.Editor{}
	view.dialogEntry = sftpclient.Entry{}
	view.dialogError = ""
	if entry, ok := state.selectedSFTPEntry(view); ok {
		view.dialogEditor2.Insert(entry.Name) // a link to what is selected is the usual wish
	}
	view.dialog = sftpDialogSymlink
}

func (state *State) submitSFTPLink(view *sftpView) bool {
	m := state.messages.Tools
	name := strings.TrimSpace(view.dialogEditor.String())
	target := strings.TrimSpace(view.dialogEditor2.String())
	switch {
	case name == "":
		view.dialogError = m.LinkErrorName
		return false
	case target == "":
		view.dialogError = m.LinkErrorTarget
		return false
	case strings.ContainsRune(name, 0) || strings.ContainsRune(target, 0):
		view.dialogError = m.LinkErrorTarget
		return false
	}
	linkPath := sftpRemotePath(view.path, name)
	if !view.worker.Symlink(linkPath, target) {
		view.dialogError = state.messages.SFTPUnavailable
		return false
	}
	state.closeSFTPDialog(view)
	view.busy = true
	view.operation = sftpclient.OperationSymlink
	view.status = fmt.Sprintf(m.LinkCreatingFmt, name, target)
	view.errorText = ""
	return true
}

// ---- extracting archives ----

// archiveKind is how an archive's name says it is packed.
type archiveKind uint8

const (
	archiveNone archiveKind = iota
	archiveZip
	archiveTar
)

// archiveKindOf recognizes ZIP and tar in its common compressions. The command
// checks that the server has the required unpacker before creating a directory.
func archiveKindOf(name string) archiveKind {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return archiveZip
	case strings.HasSuffix(lower, ".tar"), strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"),
		strings.HasSuffix(lower, ".tar.bz2"), strings.HasSuffix(lower, ".tbz2"), strings.HasSuffix(lower, ".tar.xz"), strings.HasSuffix(lower, ".txz"):
		return archiveTar
	}
	return archiveNone
}

// archiveFolderName is the folder an archive unpacks into: its name without the
// archive extension.
func archiveFolderName(name string) string {
	lower := strings.ToLower(name)
	for _, suffix := range []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tgz", ".tbz2", ".txz", ".tar", ".zip"} {
		if strings.HasSuffix(lower, suffix) {
			return name[:len(name)-len(suffix)]
		}
	}
	return name
}

// shellQuote makes one word of s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// extractCommand is the command that unpacks archive into a new folder next to
// it. Check the unpacker before creating anything, and refuse an existing
// destination, including a dangling symbolic link. ZIP never prompts for input.
func extractCommand(kind archiveKind, archive, folder string) string {
	var unpack, tool string
	switch kind {
	case archiveZip:
		tool = "unzip"
		unpack = "unzip -q -n -- " + shellQuote(archive)
	case archiveTar:
		tool = "tar"
		unpack = "tar -xf " + shellQuote(archive)
	default:
		return ""
	}
	checks := archiveToolCheck(tool)
	if kind == archiveZip {
		checks += archiveUTF8Locale()
	}
	return "( " + checks + archiveDestinationCheck(folder) +
		"mkdir -- " + shellQuote(folder) + " && cd -- " + shellQuote(folder) + " || exit; " +
		unpack + "; result=$?; if [ \"$result\" -ne 0 ]; then rmdir -- " + shellQuote(folder) +
		" 2>/dev/null || :; fi; exit \"$result\" ) 2>&1"
}

func (state *State) selectedArchive(session *Session, view *sftpView) (sftpclient.Entry, bool) {
	entry, ok := state.selectedSFTPEntry(view)
	if !ok || entry.IsDir() || session.host.System == sshconn.SystemWindows {
		return sftpclient.Entry{}, false
	}
	return entry, archiveKindOf(entry.Name) != archiveNone
}

func (state *State) openSFTPExtractDialog(view *sftpView) {
	session, ok := state.sftpSession(view)
	if !ok {
		return
	}
	entry, ok := state.selectedArchive(session, view)
	if !ok {
		return
	}
	view.dialogEditor, view.dialogEditor2 = text.Editor{}, text.Editor{}
	view.dialogEntry = entry
	view.archiveDirectory = view.path
	view.dialogError = ""
	view.dialogEditor.Insert(archiveFolderName(entry.Name))
	view.dialog = sftpDialogExtract
}

func (state *State) submitSFTPExtract(view *sftpView) bool {
	session, ok := state.sftpSession(view)
	m := state.messages.Tools
	if !ok || session.conn == nil {
		return false
	}
	folder := strings.TrimSpace(view.dialogEditor.String())
	if folder == "" || strings.ContainsAny(folder, "/\x00") || folder == "." || folder == ".." {
		view.dialogError = m.ExtractErrorFolder
		return false
	}
	entry := view.dialogEntry
	kind := archiveKindOf(entry.Name)
	directory := view.archiveDirectory
	if directory == "" {
		directory = view.path
	}
	command := extractCommand(kind, path.Join(directory, entry.Name), path.Join(directory, folder))
	if command == "" {
		return false
	}
	state.startSFTPArchive(session, view, directory, entry.Name, folder, command, false)
	return true
}

// firstLines is the first n lines of s, joined on one line.
func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " · ")
}

// ---- following the terminal's folder ----

func (state *State) toggleSFTPFollow(session *Session) {
	state.sftpFollowOff = !state.sftpFollowOff
	state.save()
	if state.sftpFollowOff {
		return
	}
	if session == nil {
		session = state.chrome.sftpPanel.session
	}
	if session == nil {
		session, _ = state.sessionByIDPointer(state.activeSessionID)
	}
	if session != nil && session.term != nil {
		if cwd, ok := session.term.screen.WorkingDirectory(); ok {
			state.terminalDirectory(session.ID, cwd)
			state.followTerminalDirectory(session)
		} else if session.term.reportedDirectory != (term.Cwd{}) {
			state.terminalDirectory(session.ID, session.term.reportedDirectory)
			state.followTerminalDirectory(session)
		}
	}
}

// followSetupCommand is what a shell is given to report its folder at every
// prompt. It is for bash and zsh, which between them are most shells.
const followSetupCommand = `if [ -n "$ZSH_VERSION" ]; then precmd() { printf '\e]7;file://%s%s\a' "$HOST" "$PWD"; }; else PROMPT_COMMAND='printf "\e]7;file://%s%s\a" "$HOSTNAME" "$PWD"'; fi`

// ---- the settings ----

// The values behind the Files page's choices; the ...Options functions above
// name them for the widths.
var (
	settingsParallelChoices          = [...]int{1, 2, 4}
	settingsPathChoices              = [...]bool{false, true} // breadcrumbs, then the input
	settingsSFTPPanelPositionChoices = [...]layoutcore.ContextSide{layoutcore.ContextLeft, layoutcore.ContextRight}
	settingsSFTPWindowOrderChoices   = [...]sftpWindowOrder{sftpLocalLeft, sftpRemoteLeft}
	settingsSFTPOpenModeChoices      = [...]sftpOpenMode{sftpOpenWindow, sftpOpenTab}
)

func (state *State) settingsParallelLabel(count int) string { return strconv.Itoa(count) }

func (state *State) settingsPathLabel(input bool) string {
	if input {
		return state.messages.Tools.FilesPathInput
	}
	return state.messages.Tools.FilesPathBreadcrumbs
}

func (state *State) settingsSFTPPanelPositionLabel(side layoutcore.ContextSide) string {
	if side == layoutcore.ContextRight {
		return state.messages.Tools.FilesPanelRight
	}
	return state.messages.Tools.FilesPanelLeft
}

func (state *State) settingsSFTPWindowOrderLabel(order sftpWindowOrder) string {
	if order == sftpRemoteLeft {
		return state.messages.Tools.FilesWindowRemoteLeft
	}
	return state.messages.Tools.FilesWindowLocalLeft
}

func (state *State) settingsSFTPOpenModeLabel(mode sftpOpenMode) string {
	if mode == sftpOpenTab {
		return state.messages.Tools.FilesOpenTab
	}
	return state.messages.Tools.FilesOpenWindow
}

func (state *State) paintFilesSettings(page *settingsPage) {
	m := state.messages.Tools
	downloadDescription, downloadActions := state.sftpFilesDownloadSetting()
	page.groups(func() {
		page.group("", func() {
			page.toggleRow(m.FilesPanelAutoOpen, m.FilesPanelAutoOpenDescription, state.sftpPanelAutoOpen, hitSettingsPanelAutoOpen)
			settingsChoiceRow(page, m.FilesPanelPosition, m.FilesPanelPositionDescription, &state.sftpPanelSide, settingsSFTPPanelPositionChoices[:], state.settingsSFTPPanelPositionLabel,
				func() { state.save() }, nil)
			settingsChoiceRow(page, m.FilesPathStyle, m.FilesPathDescription, &state.sftpPathInput, settingsPathChoices[:], state.settingsPathLabel,
				func() { state.save() }, nil)
			settingsChoiceRow(page, m.FilesWindowOrder, m.FilesWindowOrderDescription, &state.sftpWindowOrder, settingsSFTPWindowOrderChoices[:], state.settingsSFTPWindowOrderLabel,
				func() { state.invalidateSFTPWindows(); state.save() }, nil)
			settingsChoiceRow(page, m.FilesOpenMode, m.FilesOpenModeDescription, &state.sftpOpenMode, settingsSFTPOpenModeChoices[:], state.settingsSFTPOpenModeLabel,
				func() { state.save() }, nil)
			page.toggleRow(m.FilesDirectoryTree, m.FilesDirectoryTreeDescription, state.sftpDirectoryTree, hitSettingsDirectoryTree)
			settingsChoiceRow(page, m.FilesParallel, "", &state.sftpParallel, settingsParallelChoices[:], state.settingsParallelLabel,
				func() { state.applySFTPSettings(); state.save() }, nil)
			page.actionRow(m.FilesColumns, state.sftpFilesColumnsSummary(), false,
				components.ButtonAction{ID: hitSettingsConfigureColumns, Label: m.FilesColumnsCustomize, Variant: components.ButtonOutline})
			page.actionRow(m.FilesDownloadDirectory, downloadDescription, false, downloadActions...)
			state.paintEditorSetting(page)
			if state.customEditorSettingVisible() {
				path := state.customEditorPath
				description := m.FilesCustomEditorNotSet
				if !customEditorSupported() {
					description = m.FilesCustomEditorUnavailable
				}
				if path != "" {
					description = path
				}
				var actions []components.ButtonAction
				if customEditorSupported() {
					actions = append(actions, components.ButtonAction{
						ID: hitSettingsCustomEditorChoose, Label: m.FilesCustomEditorChoose, Variant: components.ButtonOutline,
					})
				}
				if path != "" {
					actions = append(actions, components.ButtonAction{
						ID: hitSettingsCustomEditorClear, Label: m.FilesCustomEditorClear, Variant: components.ButtonGhost,
					})
				}
				page.actionRow(m.FilesCustomEditor, description, false, actions...)
			}
			page.toggleRow(m.FilesKeepTimes, m.FilesKeepTimesDescription, !state.sftpKeepTimesOff, hitSettingsKeepTimesToggle)
			page.toggleRow(m.FilesFollow, m.FilesFollowDescription, !state.sftpFollowOff, hitSettingsFollowToggle)
			page.toggleRow(m.FilesTerminalFollow, m.FilesTerminalFollowDescription, state.sftpTerminalFollow, hitSettingsTerminalFollowToggle)
			page.actionRow(m.FilesFollowSetup, m.FilesFollowSetupDescription, false,
				components.ButtonAction{ID: hitSettingsFollowSetup, Label: m.FilesFollowSetupCopy, Variant: components.ButtonOutline})
		})
	})
}

func (state *State) customEditorSettingVisible() bool {
	return customEditorSupported() || state.defaultEditor == editorCustom || state.customEditorPath != ""
}

// clickFilesSettings handles the presses of the Files settings.
func (state *State) clickFilesSettings(id gfx.ID) bool {
	switch id {
	case hitSettingsPanelAutoOpen:
		state.sftpPanelAutoOpen = !state.sftpPanelAutoOpen
		state.save()
	case hitSettingsDirectoryTree:
		state.sftpDirectoryTree = !state.sftpDirectoryTree
		state.save()
		for _, window := range state.sftpWindows {
			if window != nil && !window.closed {
				window.setDirectoryTreeDefault(state.sftpDirectoryTree)
			}
		}
	case hitSettingsConfigureColumns:
		state.openSFTPColumnSettings()
	case hitSettingsDownloadDirectory:
		directory, err := state.options.OpenDirectory(app.OpenDirectoryOptions{Title: state.messages.Tools.FilesDownloadChoose})
		if errors.Is(err, app.ErrCanceled) {
			return true
		}
		if err != nil {
			state.status = state.messages.Tools.FilesDownloadChooseFailed
			return true
		}
		directory = filepath.Clean(directory)
		if !validSFTPDownloadDirectory(directory) {
			state.status = fmt.Sprintf(state.messages.Tools.FilesDownloadDirectoryInvalidFmt, directory)
			return true
		}
		state.sftpDownloadDirectory = directory
		state.status = fmt.Sprintf(state.messages.Tools.FilesDownloadDirectorySavedFmt, directory)
		state.save()
	case hitSettingsCustomEditorChoose:
		state.chooseCustomEditor()
	case hitSettingsCustomEditorClear:
		state.clearCustomEditor()
	case hitSettingsDownloadAsk:
		state.sftpDownloadDirectory = ""
		state.status = state.messages.Tools.FilesDownloadAskSaved
		state.save()
	case hitSettingsKeepTimesToggle:
		state.sftpKeepTimesOff = !state.sftpKeepTimesOff
		state.applySFTPSettings()
		state.save()
	case hitSettingsFollowToggle:
		session, _ := state.sessionByIDPointer(state.activeSessionID)
		state.toggleSFTPFollow(session)
	case hitSettingsTerminalFollowToggle:
		state.sftpTerminalFollow = !state.sftpTerminalFollow
		state.save()
	case hitSettingsFollowSetup:
		if err := state.options.SetClipboardText(followSetupCommand); err != nil {
			state.status = fmt.Sprintf(state.messages.StatusClipboardFailedFmt, err)
		} else {
			state.status = state.messages.Tools.FilesFollowSetupCopied
		}
	default:
		return false
	}
	return true
}

// applySFTPSettings gives every open file panel the current transfer settings.
func (state *State) applySFTPSettings() {
	for _, window := range state.sftpWindows {
		if !window.closed && window.view.worker != nil {
			state.configureSFTPWorker(window.view)
		}
	}
	for index := range state.sessions {
		if view := state.sessions[index].files; view != nil && view.worker != nil {
			state.configureSFTPWorker(view)
		}
	}
}

func (state *State) configureSFTPWorker(view *sftpView) {
	view.worker.SetParallel(state.sftpParallel)
	view.worker.SetPreserveTimes(!state.sftpKeepTimesOff)
}

// ---- painting ----

// sftpDetails caches the line describing the selected item, so painting a
// panel that did not change builds no text.
type sftpDetails struct {
	entry sftpclient.Entry
	words string // the language the line was made in
	line  string
	ready bool
}

func (state *State) sftpDetailsLine(view *sftpView, entry sftpclient.Entry) string {
	cache := &view.details
	if !cache.ready || cache.entry != entry || cache.words != state.messages.Tools.LinkBroken {
		cache.entry, cache.words, cache.ready = entry, state.messages.Tools.LinkBroken, true
		cache.line = state.sftpDetails(entry)
	}
	return cache.line
}

// sftpDetails is the one line under the list describing the selected item.
func (state *State) sftpDetails(entry sftpclient.Entry) string {
	var parts [6]string
	count := 0
	add := func(value string) {
		if value != "" {
			parts[count] = value
			count++
		}
	}
	add(entry.Mode.String())
	if entry.HasOwner {
		owner, group := entry.Owner, entry.Group
		if owner == "" {
			owner = fmt.Sprint(entry.UID)
		}
		if group == "" {
			group = fmt.Sprint(entry.GID)
		}
		add(owner + ":" + group)
	}
	if !entry.IsDir() {
		add(formatBytes(entry.Size))
	}
	if entry.IsLink() {
		switch {
		case entry.LinkBroken:
			add("→ " + entry.LinkTarget + " (" + state.messages.Tools.LinkBroken + ")")
		case entry.LinkTarget != "":
			add("→ " + entry.LinkTarget)
		}
	}
	return strings.Join(parts[:count], "  ·  ")
}

// savedBookmark is a bookmark in the state file.
type savedBookmark struct {
	Host   string `json:"host"`
	Path   string `json:"path"`
	Name   string `json:"name,omitempty"`
	Local  string `json:"local,omitempty"`
	Shared bool   `json:"shared,omitempty"`
	Sync   bool   `json:"sync,omitempty"`
}

// restoreSFTPSettings reads the file panel's settings and bookmarks from a state.
func (state *State) restoreSFTPSettings(saved savedState) {
	state.sftpParallel = DefaultSFTPParallel
	switch saved.SFTPParallel {
	case 1, 2, 4:
		state.sftpParallel = saved.SFTPParallel
	}
	state.sftpKeepTimesOff = saved.SFTPKeepTimes == "off"
	state.sftpFollowOff = saved.SFTPFollow == "off"
	state.sftpTerminalFollow = saved.SFTPTerminalFollow
	state.sftpPathInput = saved.SFTPPathInput
	state.sftpPanelAutoOpen = saved.SFTPPanelAutoOpen
	state.sftpDirectoryTree = saved.SFTPDirectoryTree
	if saved.SFTPPanelWidth >= layoutcore.MinContextWidth && saved.SFTPPanelWidth <= layoutcore.MaxContextWidth {
		state.sftpPanelWidth = saved.SFTPPanelWidth
	}
	state.sftpColumns = restoreSFTPColumnPreferences(saved.SFTPColumns)
	state.sftpDownloadDirectory = restoreSFTPDownloadDirectory(saved.SFTPDownloadDirectory)
	state.sftpPanelSide = layoutcore.ContextLeft
	if saved.SFTPPanelSide == "right" {
		state.sftpPanelSide = layoutcore.ContextRight
	}
	state.sftpWindowOrder = sftpWindowOrderFromString(saved.SFTPWindowOrder)
	state.sftpOpenMode = sftpOpenModeFromString(saved.SFTPOpenMode)
	state.customEditorPath = strings.TrimSpace(saved.CustomEditorPath)
	if !validCustomEditorPath(state.customEditorPath) {
		state.customEditorPath = ""
	}
	if state.defaultEditor == editorCustom && state.customEditorPath == "" {
		state.defaultEditor = editorNone
	}
	state.updateAtStart = saved.UpdateCheck != "off"
	state.closeAction = closeActionFromString(saved.CloseAction)
	state.relaunchAction = relaunchActionFromString(saved.RelaunchAction)
	state.trayHintShown = saved.TrayHintShown
	state.update.skipped = saved.UpdateSkipped
	clear(state.sftpBookmarks)
	state.sftpBookmarks = state.sftpBookmarks[:0]
	perHost := map[string]int{}
	for _, entry := range saved.SFTPBookmarks {
		clean := path.Clean(entry.Path)
		if entry.Shared {
			entry.Host = ""
		}
		if entry.Host == "" && !entry.Shared || !path.IsAbs(clean) || len(clean) > 2048 || strings.ContainsRune(clean, 0) ||
			perHost[entry.Host] >= maxBookmarksPerHost || len(state.sftpBookmarks) >= maxBookmarks {
			continue
		}
		bookmark := sftpBookmark{host: entry.Host, path: clean, name: cleanBookmarkName(entry.Name), local: entry.Local, shared: entry.Shared, sync: entry.Sync}
		if !validSavedBookmarkLocal(bookmark.local) {
			bookmark.local = ""
		}
		if bookmark.local == "" {
			bookmark.sync = false
		}
		if slices.Contains(state.sftpBookmarks, bookmark) {
			continue
		}
		perHost[entry.Host]++
		state.sftpBookmarks = append(state.sftpBookmarks, bookmark)
	}
}

func savedSFTPDownloadDirectory(value string) string {
	if value == "" {
		return sftpDownloadAsk
	}
	return filepath.Clean(value)
}

func restoreSFTPDownloadDirectory(value string) string {
	switch value {
	case "", sftpDownloadAsk:
		if value == sftpDownloadAsk {
			return ""
		}
		return defaultDownloadDirectory()
	default:
		value = filepath.Clean(value)
		if validSFTPDownloadDirectory(value) {
			return value
		}
		return defaultDownloadDirectory()
	}
}

func validSFTPDownloadDirectory(value string) bool {
	return value != "" && len(value) <= 4096 && filepath.IsAbs(value) && !strings.ContainsRune(value, 0)
}

// DefaultSFTPParallel is how many transfers the file panel runs at once until
// the user says otherwise.
const DefaultSFTPParallel = 2
