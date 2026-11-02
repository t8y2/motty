package workbench

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"motty/internal/sftpclient"
)

// Searching by name through the folders below the one on show (#323). With the
// filter's "Include subfolders" switch on, the tree under the folder is read
// once, on a goroutine and, on a server, over an SFTP channel of its own, so
// the transfer queue is not held up. The query is matched against what has been
// read so far, and again as more arrives or the query changes.
//
// The matches are rows of the folder's own list: the list paints them, scrolls,
// takes focus and answers keys as it does for the folder's rows. What differs
// is what a row stands for. A result is not in the folder, so a press only
// selects it, opening one leaves the search for the result's folder, and the
// selection, transfers, rename and delete never see it.

const (
	maxSearchEntries = 200_000
	maxSearchDepth   = 32
	maxSearchResults = 5000
)

var hitSFTPSearchToggle = tree.ID(sftpHits, "sftp-search-toggle")

// searchPruned are the folders a search does not go down from the top of a file
// system: they are not the user's files, and /proc and /sys have no end to
// them, so they would use up the whole allowance of entries.
var searchPruned = []string{"/proc", "/sys", "/dev"}

func searchPrune(dir string) bool { return slices.Contains(searchPruned, dir) }

// sftpSearch is one reading of the tree under root, and the query's matches in
// what it has read. It outlives a query: clearing the filter keeps what was
// read, so typing again does not read the tree again.
type sftpSearch struct {
	root string
	// hidden is the folder's showHidden when the search began: hidden entries
	// are read only when they are shown.
	hidden  bool
	cancel  context.CancelFunc
	items   []sftpSearchItem
	running bool
	summary sftpclient.WalkSummary
	failure string // why the search could not start

	query   string
	folded  string
	rows    []sftpRow // the matches, up to maxSearchResults, as rows of the list
	matched int       // matches found, including those past maxSearchResults
	// selected is the result pressed, or -1.
	selected int

	// What the reading goroutine has found and not yet handed to the window.
	// The window takes all of it at once, so however quickly folders are read
	// it is woken once until it has looked.
	mu          sync.Mutex
	pending     []sftpSearchItem
	posted      bool
	done        bool
	doneSummary sftpclient.WalkSummary
	doneFailure string
}

// sftpSearchItem is an entry the search read: only what a result shows, since
// a search keeps up to maxSearchEntries of them. dir is its folder relative to
// the root with "/" between folders, "" for the root itself.
type sftpSearchItem struct {
	dir, name, folded string
	mode              os.FileMode
}

// searching reports whether the folder's list shows search results instead of
// its rows.
func (folder *sftpFolder) searching() bool {
	return folder.deep && folder.search != nil && folder.search.query != ""
}

// shownRows are the rows the folder's list shows: the matches while it is
// searching, else its entries.
func (folder *sftpFolder) shownRows() []sftpRow {
	if folder.searching() {
		return folder.search.rows
	}
	return folder.rows
}

// sftpSearchAvailable reports whether a side can search below its folder: this
// computer always can, a server when its files can be opened a second time.
func sftpSearchAvailable(view *sftpView, local bool) bool {
	return local || view.openFiles != nil
}

// syncSFTPSearch brings the folder's search in line with its switch, query,
// folder and hidden-files setting. The reading starts with the first query,
// goes on to the end of the tree whatever the query becomes, and is dropped
// only when the switch goes off or the folder or the hidden-files setting
// changes. A query that only changed is matched again against what was read.
func (state *State) syncSFTPSearch(view *sftpView, folder *sftpFolder) {
	query := folder.filterInputEditor().String()
	local := folder == &view.lfolder
	// The detached window has lists of its own that do not show results, so a
	// reading on its behalf would only use a channel to show nothing.
	if !folder.deep || folder.path == "" || view.detached != nil || !sftpSearchAvailable(view, local) {
		stopSFTPSearch(folder)
		state.refreshSFTPFilterStatus(view, folder)
		return
	}
	if search := folder.search; search != nil && (search.root != folder.path || search.hidden != folder.showHidden) {
		stopSFTPSearch(folder)
	}
	search := folder.search
	if search == nil && query != "" {
		search = state.startSFTPSearch(view, folder)
	}
	if search != nil && search.query != query {
		search.match(query)
		folder.first, folder.scrollOffset, folder.wheel = 0, 0, 0
	}
	folder.syncSearchList()
	state.refreshSFTPFilterStatus(view, folder)
}

// stopSFTPSearch ends the folder's search and forgets it.
func stopSFTPSearch(folder *sftpFolder) {
	if folder.search != nil {
		folder.search.cancel()
		folder.search = nil
	}
	folder.syncSearchList()
}

// syncSearchList sets the list to what it now shows. The place in it and the
// selection belong to what was shown before, so they go; and nothing of the
// folder stays selected under the results, or the keys and buttons that act on
// the selection would reach rows out of sight.
func (folder *sftpFolder) syncSearchList() {
	shown := folder.searching()
	if shown == folder.searchShown {
		return
	}
	folder.searchShown = shown
	folder.first, folder.scrollOffset, folder.wheel = 0, 0, 0
	folder.selected, folder.anchor = -1, -1
	if shown {
		for index := range folder.allRows {
			folder.allRows[index].selected, folder.allRows[index].cells[0] = false, ""
		}
		for index := range folder.rows {
			folder.rows[index].selected, folder.rows[index].cells[0] = false, ""
		}
	}
}

// startSFTPSearch begins reading the tree under the folder. What the walk finds
// reaches the window as it is read; a delivery for a search the folder no longer
// has is dropped.
func (state *State) startSFTPSearch(view *sftpView, folder *sftpFolder) *sftpSearch {
	ctx, cancel := context.WithCancel(context.Background())
	search := &sftpSearch{root: folder.path, hidden: folder.showHidden, cancel: cancel, running: true, selected: -1}
	folder.search = search

	local, showHidden, root, open := folder == &view.lfolder, folder.showHidden, folder.path, view.openFiles
	go func() {
		var reader sftpclient.DirReader = sftpclient.LocalReader{}
		options := sftpclient.WalkOptions{
			Join:       func(dir, name string) string { return filepath.Join(dir, name) },
			Prune:      searchPrune,
			MaxEntries: maxSearchEntries, MaxDepth: maxSearchDepth, Parallel: 2,
		}
		if !showHidden {
			options.Skip = func(info os.FileInfo) bool { return localFileHidden(info.Name(), info) }
		}
		if !local {
			remote, err := open()
			if err != nil {
				state.handSearch(view, folder, search, nil, true, sftpclient.WalkSummary{}, err.Error())
				return
			}
			var once sync.Once
			release := func() { once.Do(func() { _ = remote.Close() }) }
			defer release()
			// Closing the channel ends a read under way, so a cancelled search
			// does not wait for a slow folder.
			defer context.AfterFunc(ctx, release)()
			reader = remote
			options.Join = func(dir, name string) string { return path.Join(dir, name) }
			options.Parallel = 4
			if sftpclient.IsSCP(remote) {
				options.Parallel = 1 // every folder is a command of its own
			}
			options.Skip = nil
			if !showHidden {
				options.Skip = func(info os.FileInfo) bool { return strings.HasPrefix(info.Name(), ".") }
			}
		}
		summary := sftpclient.Walk(ctx, reader, root, options, func(found []sftpclient.Found) {
			if len(found) == 0 {
				return
			}
			dir := found[0].Dir
			if local {
				dir = filepath.ToSlash(dir)
			}
			items := make([]sftpSearchItem, len(found))
			for index, entry := range found {
				name := entry.Entry.Name
				items[index] = sftpSearchItem{dir: dir, name: name, folded: strings.ToLower(name), mode: entry.Entry.Mode}
			}
			state.handSearch(view, folder, search, items, false, sftpclient.WalkSummary{}, "")
		})
		if ctx.Err() != nil {
			return
		}
		failure := ""
		if summary.RootErr != nil {
			failure = summary.RootErr.Error() // nothing to search: say why
		}
		state.handSearch(view, folder, search, nil, true, summary, failure)
	}()
	return search
}

// handSearch passes what the reading found to the window. The window is woken
// when nothing is waiting for it already, so what a slow folder gave is shown
// as soon as it is read, and a quick tree does not wake the window once per
// folder.
func (state *State) handSearch(view *sftpView, folder *sftpFolder, search *sftpSearch, items []sftpSearchItem, done bool, summary sftpclient.WalkSummary, failure string) {
	search.mu.Lock()
	search.pending = append(search.pending, items...)
	if done {
		search.done, search.doneSummary, search.doneFailure = true, summary, failure
	}
	wake := !search.posted
	search.posted = true
	search.mu.Unlock()
	if !wake {
		return
	}
	state.aiPost(func(state *State) {
		search.mu.Lock()
		items, done, summary, failure := search.pending, search.done, search.doneSummary, search.doneFailure
		search.pending, search.posted = nil, false
		search.mu.Unlock()
		if folder.search != search {
			return
		}
		search.add(items)
		if done {
			search.running, search.summary, search.failure = false, summary, failure
		}
		state.refreshSFTPFilterStatus(view, folder)
	})
}

// add keeps what the walk read and matches it against the query.
func (search *sftpSearch) add(items []sftpSearchItem) {
	first := len(search.items)
	search.items = append(search.items, items...)
	for index := first; index < len(search.items); index++ {
		search.matchItem(&search.items[index])
	}
}

// match matches every item read so far against a new query.
func (search *sftpSearch) match(query string) {
	search.query, search.folded = query, strings.ToLower(query)
	search.rows, search.matched, search.selected = search.rows[:0], 0, -1
	if query == "" {
		return
	}
	for index := range search.items {
		search.matchItem(&search.items[index])
	}
}

func (search *sftpSearch) matchItem(item *sftpSearchItem) {
	if search.folded == "" || !strings.Contains(item.folded, search.folded) {
		return
	}
	search.matched++
	if len(search.rows) >= maxSearchResults {
		return
	}
	start, end, _ := sftpFilterMatch(item.name, search.query)
	icon, mono := sftpFileIcon(item.name, item.mode)
	search.rows = append(search.rows, sftpRow{
		entry: sftpclient.Entry{Name: item.name, Mode: item.mode}, icon: icon, mono: mono,
		matchStart: start, matchEnd: end, where: item.dir,
		cells: [5]string{1: item.name},
	})
}

// selectRow marks one result as the selected one.
func (search *sftpSearch) selectRow(index int) {
	if search.selected >= 0 && search.selected < len(search.rows) {
		search.rows[search.selected].selected = false
	}
	search.selected = -1
	if index >= 0 && index < len(search.rows) {
		search.rows[index].selected = true
		search.selected = index
	}
}

// refreshSFTPSearchStatus puts the search's progress in the panel's status
// line while its results are on show.
func (state *State) refreshSFTPSearchStatus(view *sftpView, folder *sftpFolder) {
	search := folder.search
	if search == nil || !folder.searching() || view.active() != folder {
		return
	}
	m := state.messages.Tools
	if search.failure != "" {
		view.status = fmt.Sprintf(m.FilesSearchUnavailableFmt, search.failure)
		return
	}
	count := fmt.Sprintf(m.FilesSearchDoneFmt, search.matched, len(search.items))
	if search.running {
		count = fmt.Sprintf(m.FilesSearchScanningFmt, search.matched, len(search.items))
	}
	parts := []string{count}
	if search.matched > len(search.rows) {
		parts = append(parts, fmt.Sprintf(m.FilesSearchShownFmt, len(search.rows)))
	}
	if search.summary.Truncated {
		parts = append(parts, m.FilesSearchTruncated)
	}
	if search.summary.Failed > 0 {
		parts = append(parts, fmt.Sprintf(m.FilesSearchFailedFmt, search.summary.Failed))
	}
	view.status = strings.Join(parts, " · ")
}

// toggleSFTPSearch turns "Include subfolders" on or off for the folder on show.
func (state *State) toggleSFTPSearch(view *sftpView) {
	folder := view.active()
	folder.deep = !folder.deep
	folder.filterOpen = true
	folder.rememberSelection()
	state.applySFTPFilter(view, folder, false)
}

// pressSFTPSearchResult is a press on result index: it selects it, and a second
// press of a double click leaves the search for it. A server that is busy is
// left alone, as it is for the folder's rows.
func (state *State) pressSFTPSearchResult(view *sftpView, folder *sftpFolder, index, clicks int) {
	if view.busy && !view.local || index < 0 || index >= len(folder.search.rows) {
		return
	}
	folder.search.selectRow(index)
	if clicks >= 2 {
		state.revealSFTPSearchResult(view, folder, index)
	}
}

// revealSFTPSearchResult leaves the search for a result: a folder is opened, a
// file is shown selected in its folder.
func (state *State) revealSFTPSearchResult(view *sftpView, folder *sftpFolder, index int) {
	search := folder.search
	if search == nil || index < 0 || index >= len(search.rows) || view.busy && !view.local {
		return
	}
	row := search.rows[index]
	local := folder == &view.lfolder
	join := func(dir, name string) string { return path.Join(dir, name) }
	if local {
		join = func(dir, name string) string { return filepath.Join(dir, filepath.FromSlash(name)) }
	}
	dir := join(search.root, row.where)
	target, name := dir, row.entry.Name
	if row.entry.Mode.IsDir() {
		target, name = join(dir, row.entry.Name), ""
	}
	// The result is in another folder; the search is over, and a new one starts
	// from wherever the person goes next.
	folder.clearFilter()
	stopSFTPSearch(folder)
	state.applySFTPFilter(view, folder, false)
	folder.reveal = sftpReveal{dir: target, name: name}
	if local {
		_ = state.navigateLocalDirectory(view, target)
	} else {
		state.navigateSFTPDirectory(view, target)
	}
	// A navigation that was not started leaves nothing behind to claim it.
	folder.reveal = sftpReveal{}
}

// sftpReveal is the entry to select once the folder a result led to is listed.
type sftpReveal struct {
	dir, name string
}

// applyReveal selects the entry a search result led to, when the listing that
// arrived is the folder it is in, and brings it into view.
func (folder *sftpFolder) applyReveal(reveal sftpReveal) {
	if reveal.name == "" || reveal.dir != folder.path {
		return
	}
	index := sftpRowNameIndex(folder.rows, reveal.name)
	if index < 0 {
		return
	}
	folder.restorePlace(sftpPlace{current: reveal.name, anchor: reveal.name, selected: []string{reveal.name}, first: index - folder.visibleRows/2})
}
