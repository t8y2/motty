package workbench

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
	"motty/internal/term"
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
	hitSFTPDialogField2    = tree.ID(sftpHits, "sftp-dialog-field2")
)

var (
	hitSettingsParallel1       = tree.ID(sftpHits, "settings-parallel1")
	hitSettingsParallel2       = tree.ID(sftpHits, "settings-parallel2")
	hitSettingsParallel4       = tree.ID(sftpHits, "settings-parallel4")
	hitSettingsKeepTimesToggle = tree.ID(sftpHits, "settings-keep-times")
	hitSettingsFollowToggle    = tree.ID(sftpHits, "settings-follow")
	hitSettingsFollowSetup     = tree.ID(sftpHits, "settings-follow-setup")
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
)

// sftpBookmark is a folder kept for a host, which the host's catalog id names.
type sftpBookmark struct {
	host, path string
}

// bookmarksFor lists the bookmarked folders of the session's host, in the order
// they were added.
func (state *State) bookmarksFor(session *Session) []string {
	var paths []string
	for _, bookmark := range state.sftpBookmarks {
		if bookmark.host == session.host.CatalogID {
			paths = append(paths, bookmark.path)
		}
	}
	return paths
}

func (state *State) bookmarked(session *Session, folder string) bool {
	return slices.Contains(state.bookmarksFor(session), folder)
}

// toggleBookmark bookmarks the folder on screen, or removes the bookmark.
func (state *State) toggleBookmark(session *Session, view *sftpView) {
	folder := path.Clean(view.path)
	if folder == "." || session.host.CatalogID == "" {
		return
	}
	if state.removeBookmark(session, folder) {
		state.save()
		return
	}
	if len(state.bookmarksFor(session)) >= maxBookmarksPerHost || len(state.sftpBookmarks) >= maxBookmarks {
		view.errorText = fmt.Sprintf(state.messages.Tools.BookmarkLimitFmt, maxBookmarksPerHost)
		return
	}
	state.sftpBookmarks = append(state.sftpBookmarks, sftpBookmark{host: session.host.CatalogID, path: folder})
	state.save()
}

func (state *State) removeBookmark(session *Session, folder string) bool {
	for index, bookmark := range state.sftpBookmarks {
		if bookmark.host == session.host.CatalogID && bookmark.path == folder {
			state.sftpBookmarks = slices.Delete(state.sftpBookmarks, index, index+1)
			return true
		}
	}
	return false
}

// clickSFTPExtra handles the presses this file adds to the panel.
func (state *State) clickSFTPExtra(session *Session, view *sftpView, id ui.ID) bool {
	switch {
	case id == hitSFTPPaneToggle:
		state.toggleSFTPPane(view)
	case id == hitSFTPDownloadHere && !view.busy:
		state.downloadHere(view)
	case id == hitSFTPNewLink && !view.busy:
		state.openSFTPLinkDialog(view)
	case id == hitSFTPBookmarkToggle:
		state.toggleBookmark(session, view)
	case id == hitSFTPBookmarkList:
		view.bookmarksOpen = !view.bookmarksOpen
	case id == hitSFTPBookmarkDismiss:
		view.bookmarksOpen = false
	case id.Scope() == hitSFTPBookmarkItemList:
		marks := state.bookmarksFor(session)
		if index := item(id); index < len(marks) {
			view.bookmarksOpen = false
			if !view.busy {
				state.openSFTPDirectory(view, marks[index])
			}
		}
	case id.Scope() == hitSFTPBookmarkRemoveList:
		marks := state.bookmarksFor(session)
		if index := item(id); index < len(marks) {
			state.removeBookmark(session, marks[index])
			state.save()
		}
	case id == hitSFTPExtract && !view.busy:
		state.openSFTPExtractDialog(view)
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

// archiveKindOf recognizes the archives a server can unpack with the tools every
// server has: zip, and tar in its common compressions.
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
// it. The folder is created by the command and the command refuses to go on if
// something of that name is there, so nothing is ever overwritten; the tools
// themselves refuse paths that climb out of the folder.
func extractCommand(kind archiveKind, archive, folder string) string {
	var unpack string
	switch kind {
	case archiveZip:
		unpack = "unzip -q -- " + shellQuote(archive)
	case archiveTar:
		unpack = "tar -xf " + shellQuote(archive)
	default:
		return ""
	}
	return "if [ -e " + shellQuote(folder) + " ]; then echo 'already exists'; exit 17; fi; " +
		"mkdir -- " + shellQuote(folder) + " && cd -- " + shellQuote(folder) + " && " + unpack + " 2>&1"
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
	command := extractCommand(kind, path.Join(view.path, entry.Name), path.Join(view.path, folder))
	if command == "" {
		return false
	}
	state.closeSFTPDialog(view)
	view.busy = true
	view.operation = sftpclient.OperationList // nothing the worker knows; the panel waits on the command
	view.extracting = true
	view.status = fmt.Sprintf(m.ExtractRunningFmt, entry.Name)
	view.errorText = ""
	connection, directory := session.conn, view.path
	ctx, cancel := context.WithTimeout(context.Background(), extractTimeout)
	view.cancelExtract = cancel
	go func() {
		defer cancel()
		output, err := connection.RunCommand(ctx, command, maxExtractOutput)
		state.aiPost(func(state *State) { state.finishExtractView(view, directory, entry.Name, folder, output, err) })
	}()
	return true
}

// finishExtract takes in the outcome of an extraction on the window's thread.
func (state *State) finishExtract(sessionID uint32, directory, archive, folder string, output []byte, err error) {
	session, ok := state.sessionByIDPointer(sessionID)
	if !ok || session.files == nil || !session.files.extracting {
		return
	}
	view := session.files
	state.finishExtractView(view, directory, archive, folder, output, err)
}

func (state *State) finishExtractView(view *sftpView, directory, archive, folder string, output []byte, err error) {
	if !view.extracting {
		return
	}
	defer view.invalidateDetached()
	m := state.messages.Tools
	view.extracting, view.busy, view.cancelExtract = false, false, nil
	if err != nil {
		reason := strings.TrimSpace(string(output))
		if reason == "" {
			reason = err.Error()
		}
		if errors.Is(err, context.Canceled) {
			reason = state.messages.SFTPCanceled
		}
		view.errorText = fmt.Sprintf(m.ExtractFailedFmt, archive, firstLines(reason, 3))
		return
	}
	view.forgetListings()
	view.refreshStatus = fmt.Sprintf(m.ExtractDoneFmt, archive, folder)
	if view.path == directory {
		state.listSFTP(view, directory)
	} else {
		view.status = view.refreshStatus
	}
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

// terminalDirectory is called when a session's shell reports its folder.
func (state *State) terminalDirectory(sessionID uint32, cwd term.Cwd) {
	if state.sftpFollowOff {
		return
	}
	session, ok := state.sessionByIDPointer(sessionID)
	if !ok || session.files == nil || !session.hasServer() {
		return
	}
	view := session.files
	if view.busy || view.dialog != sftpDialogNone || view.editPath != "" || path.Clean(view.path) == path.Clean(cwd.Path) {
		return
	}
	state.openSFTPDirectory(view, cwd.Path)
}

// followSetupCommand is what a shell is given to report its folder at every
// prompt. It is for bash and zsh, which between them are most shells.
const followSetupCommand = `if [ -n "$ZSH_VERSION" ]; then precmd() { printf '\e]7;file://%s%s\a' "$HOST" "$PWD"; }; else PROMPT_COMMAND='printf "\e]7;file://%s%s\a" "$HOSTNAME" "$PWD"'; fi`

// ---- the settings ----

func (state *State) settingsParallelOptions() [3]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsParallel1, Label: "1"},
		{ID: hitSettingsParallel2, Label: "2"},
		{ID: hitSettingsParallel4, Label: "4"},
	}
}

func (state *State) paintFilesSettings(page *settingsPage) {
	m := state.messages.Tools
	parallel := state.settingsParallelOptions()
	selectedParallel := hitSettingsParallel2
	switch state.sftpParallel {
	case 1:
		selectedParallel = hitSettingsParallel1
	case 4:
		selectedParallel = hitSettingsParallel4
	}
	page.groups(func() {
		page.group("", func() {
			page.row(m.FilesParallel, "", parallel[:], selectedParallel)
			page.toggleRow(m.FilesKeepTimes, m.FilesKeepTimesDescription, !state.sftpKeepTimesOff, hitSettingsKeepTimesToggle)
			page.toggleRow(m.FilesFollow, m.FilesFollowDescription, !state.sftpFollowOff, hitSettingsFollowToggle)
			page.actionRow(m.FilesFollowSetup, m.FilesFollowSetupDescription, false,
				components.ButtonAction{ID: hitSettingsFollowSetup, Label: m.FilesFollowSetupCopy, Variant: components.ButtonOutline})
		})
	})
}

// clickFilesSettings handles the presses of the Files settings.
func (state *State) clickFilesSettings(id ui.ID) bool {
	switch id {
	case hitSettingsParallel1, hitSettingsParallel2, hitSettingsParallel4:
		count := map[ui.ID]int{hitSettingsParallel1: 1, hitSettingsParallel2: 2, hitSettingsParallel4: 4}[id]
		if state.sftpParallel != count {
			state.sftpParallel = count
			state.applySFTPSettings()
			state.save()
		}
	case hitSettingsKeepTimesToggle:
		state.sftpKeepTimesOff = !state.sftpKeepTimesOff
		state.applySFTPSettings()
		state.save()
	case hitSettingsFollowToggle:
		state.sftpFollowOff = !state.sftpFollowOff
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

// paintSFTPBookmarkPopup draws the list of bookmarked folders over the file
// list, from top down to the panel's bottom margin.
func (state *State) paintSFTPBookmarkPopup(canvas *ui.Canvas, top float32, size ui.Size, session *Session, view *sftpView) {
	pal, m := state.pal, state.messages.Tools
	marks := state.bookmarksFor(session)
	const rowHeight, pad = float32(30), float32(6)
	available := max(size.Height-top-sftpPad, 0)
	height := min(float32(max(len(marks), 1))*rowHeight+pad*2, max(available-8, 0))
	empty := len(marks) == 0
	// A row that does not fit whole is not shown.
	marks = marks[:min(len(marks), max(0, int((height-pad)/rowHeight)))]
	icons, removes := state.filesRowIcons[:0], state.filesRowBoxes[:0]
	panel := ui.Rect{Y: windowBarHeight, Width: size.Width, Height: size.Height - windowBarHeight}
	// Anything else in the panel closes the list; the rows below are declared
	// after this, so they win where the list covers them.
	canvas.PushLayer(popupLayer)
	defer canvas.PopLayer()
	canvas.HitWithOptions(hitSFTPBookmarkDismiss, panel, m.Bookmarks, ui.HitOptions{Role: ui.RoleButton, SkipFocus: true})

	f := state.filesPopup.Begin(canvas, state.Theme(), ui.Rect{Y: top, Width: size.Width, Height: available})
	f.Column().Fill().PadSides(0, sftpPad, 0, sftpPad).Children(func() {
		f.Column().Height(height).Shrink(0).PadSides(pad-1, 3, 0, 3). // inside the 1 pixel border
										Border(1, withAlpha(pal.line, 0.9)).Background(pal.surface.Over(pal.workspace)).Radius(8).Children(func() {
			if empty {
				f.Row().Height(rowHeight).Items(flex.Center).PadSides(0, 8, 0, 8).Children(func() {
					f.Text(m.BookmarksEmpty).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().Grow(1)
				})
				return
			}
			for index, mark := range marks {
				id := hitSFTPBookmarkItemList.At(index)
				background := ui.Color{}
				if canvas.Hovered() == id {
					background = withAlpha(pal.surfaceHot, 0.8)
				}
				f.Row().Height(rowHeight).Shrink(0).Background(background).Radius(6).Children(func() {
					f.Row().Grow(1).Items(flex.Center).Gap(8).PadSides(0, 8, 0, 8).HitWith(id, mark, ui.HitOptions{Role: ui.RoleMenuItem}).Children(func() {
						icons = append(icons, f.Slot().Size(16, 16))
						f.Text(mark).FontSize(metrics.FontSizeS).Color(pal.text).Ellipsis().Grow(1)
					})
					removes = append(removes, f.Slot().Width(sftpButton))
				})
			}
		})
	})
	f.End()
	state.filesRowIcons, state.filesRowBoxes = icons, removes
	for index := range marks {
		paintIcon(canvas, iconFolderOpen, icons[index].Rect(), pal.muted)
		state.paintIconButton(canvas, hitSFTPBookmarkRemoveList.At(index), iconClose, removes[index].Rect(), m.BookmarkRemove, false, iconButtonQuiet, pal.surface)
	}
}

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
	Host string `json:"host"`
	Path string `json:"path"`
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
	state.updateAtStart = saved.UpdateCheck != "off"
	state.update.skipped = saved.UpdateSkipped
	clear(state.sftpBookmarks)
	state.sftpBookmarks = state.sftpBookmarks[:0]
	perHost := map[string]int{}
	for _, entry := range saved.SFTPBookmarks {
		clean := path.Clean(entry.Path)
		if entry.Host == "" || !path.IsAbs(clean) || len(clean) > 2048 || strings.ContainsRune(clean, 0) ||
			perHost[entry.Host] >= maxBookmarksPerHost || len(state.sftpBookmarks) >= maxBookmarks {
			continue
		}
		if slices.Contains(state.sftpBookmarks, sftpBookmark{host: entry.Host, path: clean}) {
			continue
		}
		perHost[entry.Host]++
		state.sftpBookmarks = append(state.sftpBookmarks, sftpBookmark{host: entry.Host, path: clean})
	}
}

// DefaultSFTPParallel is how many transfers the file panel runs at once until
// the user says otherwise.
const DefaultSFTPParallel = 2
