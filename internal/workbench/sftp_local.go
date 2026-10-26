package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

// The file panel can show this computer's files in place of the server's, to
// choose what to send and where things come to. Both are the same list: the
// panel swaps which one it shows and the toolbar changes with it.

var (
	hitSFTPPaneToggle   = tree.ID(sftpHits, "sftp-pane-toggle")
	hitSFTPSend         = tree.ID(sftpHits, "sftp-send")
	hitSFTPDownloadHere = tree.ID(sftpHits, "sftp-download-here")
	hitSFTPInsertPath   = tree.ID(sftpHits, "sftp-insert-path")
)

// maxLocalEntries bounds a local listing; a bigger folder shows its first part.
const maxLocalEntries = 20000

// localListingInlineEntries is where reading a local folder moves off the
// window thread. A few hundred entries are cheap enough to list in place, and
// keeping them synchronous keeps the small-folder paths simple; a large folder
// pays tens of milliseconds in stat calls, sorting and row preparation.
const localListingInlineEntries = 512

// localStart is where the local side opens: Downloads if there is one, else home.
func localStart() string {
	if dir := downloadsDirectory(); dir != "" {
		return dir
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return string(filepath.Separator)
}

// localHome is where a local terminal's panel opens: the user's home folder.
func localHome() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return localStart()
}

// toggleSFTPPane switches the panel between the server's files and this
// computer's.
func (state *State) toggleSFTPPane(view *sftpView) {
	view.local = !view.local
	view.bookmarksOpen = false
	view.errorText = ""
	if !view.local {
		view.status = fmt.Sprintf(state.messages.SFTPItemsFmt, len(view.rows))
		return
	}
	dir := view.lfolder.path
	if dir == "" {
		dir = localStart()
	}
	state.listLocal(view, dir)
}

// listLocal reads a local folder onto the panel. Small folders are read in
// place; a large one is scanned, sorted and prepared on a goroutine and applied
// on the window thread when it arrives. localSeq drops an answer that a newer
// listing has overtaken.
func (state *State) listLocal(view *sftpView, dir string) {
	folder := &view.lfolder
	dir = filepath.Clean(dir)
	file, err := os.Open(dir)
	if err != nil {
		view.errorText = fmt.Sprintf(state.messages.SFTPErrorFmt, err)
		if folder.path == "" {
			folder.path = dir // an empty list is better than none on a first visit
		}
		return
	}
	// Read just past the inline limit on this thread. A folder that ends before
	// it is small enough to finish now; a larger one continues on a goroutine,
	// so even the directory enumeration leaves the window thread. os.File keeps
	// the read position across that goroutine.
	first, readErr := file.ReadDir(localListingInlineEntries + 1)
	if readErr != nil && len(first) == 0 {
		file.Close()
		view.errorText = fmt.Sprintf(state.messages.SFTPErrorFmt, readErr)
		if folder.path == "" {
			folder.path = dir
		}
		return
	}
	view.localSeq++
	seq := view.localSeq
	if len(first) <= localListingInlineEntries {
		file.Close()
		state.applyLocalListing(view, scanLocalListing(dir, first), seq)
		return
	}
	view.errorText = ""
	view.status = state.messages.SFTPLoading
	go func() {
		defer file.Close()
		items := first
		for len(items) < maxLocalEntries {
			want := min(4096, maxLocalEntries-len(items))
			more, err := file.ReadDir(want)
			items = append(items, more...)
			if err != nil || len(more) == 0 {
				break
			}
		}
		listing := scanLocalListing(dir, items)
		state.aiPost(func(*State) {
			if seq != view.localSeq {
				return
			}
			state.applyLocalListing(view, listing, seq)
		})
	}()
}

// localListing is a local folder scanned away from the window thread.
type localListing struct {
	dir     string
	entries []sftpclient.Entry
	rows    []sftpRow
}

// localListing scans, sorts and prepares one local folder. It touches no State
// so a goroutine may run it while the window keeps painting.
func scanLocalListing(dir string, items []os.DirEntry) localListing {
	entries := make([]sftpclient.Entry, 0, min(len(items), maxLocalEntries))
	for _, item := range items {
		if len(entries) == maxLocalEntries {
			break
		}
		info, err := item.Info()
		if err != nil {
			continue
		}
		entry := sftpclient.Entry{Name: item.Name(), Size: info.Size(), Mode: info.Mode(), ModTime: info.ModTime()}
		if info.Mode()&os.ModeSymlink != 0 {
			if target, err := os.Readlink(filepath.Join(dir, item.Name())); err == nil {
				entry.LinkTarget = target
			}
			if resolved, err := os.Stat(filepath.Join(dir, item.Name())); err == nil {
				entry.LinkDir = resolved.IsDir()
			} else {
				entry.LinkBroken = true
			}
		}
		entries = append(entries, entry)
	}
	// Sort indices by a name folded once: lowering both names in every
	// comparison allocated a string pair per comparison, which a large folder
	// pays hundreds of thousands of times.
	folded := make([]string, len(entries))
	order := make([]int, len(entries))
	for index := range entries {
		folded[index] = strings.ToLower(entries[index].Name)
		order[index] = index
	}
	slices.SortFunc(order, func(left, right int) int {
		a, b := entries[left], entries[right]
		if a.IsDir() != b.IsDir() {
			if a.IsDir() {
				return -1
			}
			return 1
		}
		return strings.Compare(folded[left], folded[right])
	})
	sorted := make([]sftpclient.Entry, len(entries))
	for index, source := range order {
		sorted[index] = entries[source]
	}
	return localListing{dir: dir, entries: sorted, rows: prepareSFTPRows(nil, sorted)}
}

// applyLocalListing puts a finished local listing on screen. seq must still be
// the view's current one.
func (state *State) applyLocalListing(view *sftpView, listing localListing, seq uint64) {
	if seq != view.localSeq {
		return
	}
	folder := &view.lfolder
	folder.path = listing.dir
	folder.rows = listing.rows
	folder.crumbs = prepareLocalCrumbs(folder.crumbs, listing.dir)
	folder.selected, folder.first = -1, 0
	view.errorText = ""
	view.status = fmt.Sprintf(state.messages.SFTPItemsFmt, len(folder.rows))
}

// prepareLocalCrumbs splits a local path into the links of a breadcrumb.
func prepareLocalCrumbs(crumbs []sftpCrumb, dir string) []sftpCrumb {
	crumbs = crumbs[:0]
	volume := filepath.VolumeName(dir)
	rest := strings.TrimPrefix(dir, volume)
	root := volume + string(filepath.Separator)
	label := string(filepath.Separator)
	if volume != "" {
		label = volume + label
	}
	crumbs = append(crumbs, sftpCrumb{label: label, path: root})
	current := root
	for _, part := range strings.Split(strings.Trim(rest, `/\`), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		crumbs = append(crumbs, sftpCrumb{label: part, path: current})
	}
	return crumbs
}

// clickSFTPLocal handles the presses of the panel while it shows local files. It
// reports whether the press was one of its own.
func (state *State) clickSFTPLocal(session *Session, view *sftpView, id ui.ID) bool {
	folder := &view.lfolder
	switch {
	case id == hitSFTPPaneToggle:
		state.toggleSFTPPane(view)
	case id == hitSFTPRefresh:
		state.listLocal(view, folder.path)
	case id == hitSFTPUp:
		if parent := filepath.Dir(folder.path); parent != folder.path {
			state.listLocal(view, parent)
		}
	case id == hitSFTPNewDirectory:
		state.openSFTPDialog(view, sftpDialogCreateDirectory)
	case id == hitSFTPOpenEntry:
		state.openLocalSelected(view)
	case id == hitSFTPSend && !view.localOnly:
		state.sendLocalSelection(view)
	case id == hitSFTPInsertPath && view.localOnly:
		state.insertLocalPaths(session, view)
	case id.Scope() == hitSFTPBreadcrumbList:
		if index := item(id); index < len(folder.crumbs) {
			state.listLocal(view, folder.crumbs[index].path)
		}
	case id.Scope() == hitSFTPEntryList:
		index := folder.first + item(id)
		if index < len(folder.rows) {
			folder.selected = index
			folder.rows[index].selected = !folder.rows[index].selected
			folder.rows[index].cells[0] = sftpSelectionCell(folder.rows[index].selected)
			view.status = fmt.Sprintf(state.messages.SFTPItemsFmt, selectedSFTPCount(view))
		}
	default:
		return false
	}
	return true
}

// openLocalSelected goes into the selected local folder.
func (state *State) openLocalSelected(view *sftpView) {
	entry, ok := state.selectedSFTPEntry(view)
	if !ok || !entry.IsDir() {
		return
	}
	state.listLocal(view, filepath.Join(view.lfolder.path, entry.Name))
}

// insertLocalPaths types the selected items' paths into the local shell, quoted
// for it, the way dropping the files on the terminal does.
func (state *State) insertLocalPaths(session *Session, view *sftpView) {
	if session.term == nil || session.term.shell == nil {
		return
	}
	var paths []string
	for _, row := range selectedSFTPRows(view) {
		paths = append(paths, filepath.Join(view.lfolder.path, row.entry.Name))
	}
	if len(paths) > 0 {
		session.term.send([]byte(quotedPaths(paths) + " "))
	}
}

// sendLocalSelection uploads the selected local items into the server's folder.
func (state *State) sendLocalSelection(view *sftpView) {
	remoteDir := view.path
	if remoteDir == "" {
		return
	}
	for _, row := range selectedSFTPRows(view) {
		state.queueSFTPUploadTo(view, filepath.Join(view.lfolder.path, row.entry.Name), row.entry.IsDir() && !row.entry.IsLink(), remoteDir)
	}
}

// downloadHere fetches the selected server items into the local folder on show
// in the local side, without asking where.
func (state *State) downloadHere(view *sftpView) {
	if view.lfolder.path == "" {
		return
	}
	for _, row := range selectedSFTPRows(view) {
		state.queueSFTPDownload(view, row.entry, filepath.Join(view.lfolder.path, row.entry.Name))
	}
}

// submitLocalDirectory makes a folder in the local folder on show.
func (state *State) submitLocalDirectory(view *sftpView, name string) bool {
	if name == "" {
		view.dialogError = state.messages.SFTPErrDirectoryRequired
		return false
	}
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		view.dialogError = state.messages.Tools.ExtractErrorFolder
		return false
	}
	target := filepath.Join(view.lfolder.path, name)
	if err := os.Mkdir(target, 0o755); err != nil {
		view.dialogError = err.Error()
		if errors.Is(err, os.ErrExist) {
			view.dialogError = fmt.Sprintf(state.messages.SFTPErrorFmt, err)
		}
		return false
	}
	state.closeSFTPDialog(view)
	state.listLocal(view, view.lfolder.path)
	view.status = fmt.Sprintf(state.messages.SFTPDirectoryCreatedFmt, target)
	return true
}

// scrollFolder is scrollSFTP for whichever folder is on show.
func scrollFolder(folder *sftpFolder, event ui.ScrollEvent) ui.EventResult {
	if folder.visibleRows <= 0 || len(folder.rows) <= folder.visibleRows {
		return ui.EventResult{Handled: true}
	}
	folder.wheel += event.DeltaY / sftpRowHeight
	lines := int(folder.wheel)
	if lines == 0 {
		return ui.EventResult{Handled: true}
	}
	folder.wheel -= float32(lines)
	before := folder.first
	folder.first = min(max(0, folder.first+lines), len(folder.rows)-folder.visibleRows)
	if folder.first == before {
		folder.wheel = 0
	}
	return ui.EventResult{Handled: true, Repaint: folder.first != before}
}
