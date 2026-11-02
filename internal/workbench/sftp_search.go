package workbench

import (
	"context"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

// Searching by name through the folders below the one on show (#323). With the
// filter's "Include subfolders" switch on, the tree under the folder is read
// once, on a goroutine and, on a server, over an SFTP channel of its own, so
// the transfer queue is not held up. The query is matched against what has been
// read so far, and again as more arrives or the query changes. The results are
// a list of their own: selection, transfers, rename and delete act on the
// folder's rows and never see them.

const (
	maxSearchEntries = 200_000
	maxSearchDepth   = 32
	maxSearchResults = 5000
	// searchFlush is how often a running search hands what it read to the
	// window.
	searchFlush = 100 * time.Millisecond
)

var (
	hitSFTPSearchToggle    = tree.ID(sftpHits, "sftp-search-toggle")
	hitSFTPSearchScrollbar = tree.ID(sftpHits, "sftp-search-scrollbar")
	hitSFTPSearchList      = tree.Sub(sftpHits, tree.Name("sftp-search-result"))
)

// sftpSearch is one reading of the tree under root, and the query's matches in
// what it has read.
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
	results []sftpSearchResult
	matched int // matches found, including those past maxSearchResults

	first, selected, visibleRows int
	wheel                        float32
	scroll                       components.ScrollAreaGeometry
	scrollOffset                 float32
	scrollDrag                   bool
	scrollGrab                   float32
}

// sftpSearchItem is an entry the search read: only what a result shows, since
// a search keeps up to maxSearchEntries of them. dir is its folder relative to
// the root, "" for the root itself.
type sftpSearchItem struct {
	dir, name, folded string
	mode              os.FileMode
}

// sftpSearchResult is a match: the item, and the byte range of its name that
// matched.
type sftpSearchResult struct {
	item       int
	start, end int
}

// searching reports whether the folder shows search results instead of its
// rows.
func (folder *sftpFolder) searching() bool {
	return folder.deep && folder.search != nil && folder.filterInputEditor().String() != ""
}

// sftpSearchAvailable reports whether a side can search below its folder: this
// computer always can, a server when its files can be opened a second time.
func sftpSearchAvailable(view *sftpView, local bool) bool {
	return local || view.openFiles != nil
}

// syncSFTPSearch brings the folder's search in line with its switch, query,
// folder and hidden-files setting: it starts, restarts or stops the reading,
// and matches the query again when only that changed.
func (state *State) syncSFTPSearch(view *sftpView, folder *sftpFolder) {
	query := folder.filterInputEditor().String()
	local := folder == &view.lfolder
	if !folder.deep || query == "" || folder.path == "" || !sftpSearchAvailable(view, local) {
		stopSFTPSearch(folder)
		return
	}
	search := folder.search
	if search == nil || search.root != folder.path || search.hidden != folder.showHidden {
		stopSFTPSearch(folder)
		search = state.startSFTPSearch(view, folder)
	}
	if search.query != query {
		search.match(query)
	}
	state.refreshSFTPSearchStatus(view, folder)
}

// stopSFTPSearch ends the folder's search and forgets it.
func stopSFTPSearch(folder *sftpFolder) {
	if folder.search != nil {
		folder.search.cancel()
		folder.search = nil
	}
}

// startSFTPSearch begins reading the tree under the folder. What the walk finds
// reaches the window in batches; a batch for a search the folder no longer has
// is dropped.
func (state *State) startSFTPSearch(view *sftpView, folder *sftpFolder) *sftpSearch {
	ctx, cancel := context.WithCancel(context.Background())
	search := &sftpSearch{root: folder.path, hidden: folder.showHidden, cancel: cancel, running: true, selected: -1}
	folder.search = search
	// Nothing hidden under the results acts on: the selection bar and the keys
	// that act on selected rows would otherwise reach rows out of sight.
	for index := range folder.allRows {
		folder.allRows[index].selected, folder.allRows[index].cells[0] = false, ""
	}
	for index := range folder.rows {
		folder.rows[index].selected, folder.rows[index].cells[0] = false, ""
	}
	folder.selected, folder.anchor = -1, -1

	local, showHidden, root, open := folder == &view.lfolder, folder.showHidden, folder.path, view.openFiles
	post := func(apply func(*sftpSearch)) {
		state.aiPost(func(state *State) {
			if folder.search != search {
				return
			}
			apply(search)
			state.refreshSFTPSearchStatus(view, folder)
		})
	}
	go func() {
		var reader sftpclient.DirReader = sftpclient.LocalReader{}
		options := sftpclient.WalkOptions{
			Join:       func(dir, name string) string { return filepath.Join(dir, name) },
			MaxEntries: maxSearchEntries, MaxDepth: maxSearchDepth, Parallel: 2,
		}
		if !showHidden {
			options.Skip = func(info os.FileInfo) bool { return localFileHidden(info.Name(), info) }
		}
		if !local {
			remote, err := open()
			if err != nil {
				post(func(search *sftpSearch) { search.running, search.failure = false, err.Error() })
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
		var pending []sftpclient.Found
		last := time.Now()
		summary := sftpclient.Walk(ctx, reader, root, options, func(found []sftpclient.Found) {
			pending = append(pending, found...)
			if time.Since(last) >= searchFlush {
				batch := pending
				pending, last = nil, time.Now()
				post(func(search *sftpSearch) { search.add(batch) })
			}
		})
		if ctx.Err() != nil {
			return
		}
		post(func(search *sftpSearch) {
			search.add(pending)
			search.running, search.summary = false, summary
		})
	}()
	return search
}

// add keeps what the walk read and matches it against the query.
func (search *sftpSearch) add(found []sftpclient.Found) {
	for _, item := range found {
		name := item.Entry.Name
		search.items = append(search.items, sftpSearchItem{dir: item.Dir, name: name, folded: strings.ToLower(name), mode: item.Entry.Mode})
		search.matchItem(len(search.items) - 1)
	}
}

// match matches every item read so far against a new query.
func (search *sftpSearch) match(query string) {
	search.query, search.folded = query, strings.ToLower(query)
	search.results, search.matched = search.results[:0], 0
	search.first, search.selected, search.scrollOffset = 0, -1, 0
	for index := range search.items {
		search.matchItem(index)
	}
}

func (search *sftpSearch) matchItem(index int) {
	item := &search.items[index]
	if search.folded == "" || !strings.Contains(item.folded, search.folded) {
		return
	}
	search.matched++
	if len(search.results) >= maxSearchResults {
		return
	}
	start, end, _ := sftpFilterMatch(item.name, search.query)
	search.results = append(search.results, sftpSearchResult{item: index, start: start, end: end})
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
	parts := []string{fmt.Sprintf(m.FilesSearchDoneFmt, search.matched, len(search.items))}
	if search.running {
		parts[0] = fmt.Sprintf(m.FilesSearchScanningFmt, search.matched, len(search.items))
	}
	if search.matched > len(search.results) {
		parts = append(parts, fmt.Sprintf(m.FilesSearchShownFmt, len(search.results)))
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

// revealSFTPSearchResult leaves the search for a result: a folder is opened, a
// file is shown selected in its folder.
func (state *State) revealSFTPSearchResult(view *sftpView, folder *sftpFolder, index int) {
	search := folder.search
	if search == nil || index < 0 || index >= len(search.results) {
		return
	}
	item := search.items[search.results[index].item]
	local := folder == &view.lfolder
	join := func(dir, name string) string { return path.Join(dir, name) }
	if local {
		join = func(dir, name string) string { return filepath.Join(dir, name) }
	}
	dir := search.root
	if item.dir != "" {
		dir = join(search.root, item.dir)
	}
	target, name := dir, item.name
	if item.mode.IsDir() {
		target, name = join(dir, item.name), ""
	}
	folder.clearFilter()
	folder.reveal = sftpReveal{dir: target, name: name}
	state.applySFTPFilter(view, folder, false)
	if local {
		state.navigateLocalDirectory(view, target)
	} else {
		state.navigateSFTPDirectory(view, target)
	}
}

// sftpReveal is the entry to select once a folder's listing arrives.
type sftpReveal struct {
	dir, name string
}

// applyReveal selects the entry a search result led to, when the listing that
// arrived is the folder it is in, and brings it into view.
func (folder *sftpFolder) applyReveal() {
	reveal := folder.reveal
	folder.reveal = sftpReveal{}
	if reveal.name == "" || reveal.dir != folder.path {
		return
	}
	index := sftpRowNameIndex(folder.rows, reveal.name)
	if index < 0 {
		return
	}
	folder.restorePlace(sftpPlace{current: reveal.name, anchor: reveal.name, selected: []string{reveal.name}, first: index - folder.visibleRows/2})
}

// paintSFTPSearchList shows the results in the list's place: each match with its
// folder relative to where the search began.
func (state *State) paintSFTPSearchList(canvas *ui.Canvas, list ui.Rect, folder *sftpFolder) {
	search := folder.search
	visibleRows := max(0, int(list.Height/sftpRowHeight))
	scrolls := len(search.results) > visibleRows && visibleRows > 0
	rowWidth := list.Width
	if scrolls {
		rowWidth = max(rowWidth-10, 0)
	}
	if !search.scrollDrag {
		if search.scroll.MaxOffsetY <= 0 || search.scrollOffset > search.scroll.MaxOffsetY || int(search.scrollOffset/sftpRowHeight) != search.first {
			search.scrollOffset = float32(search.first) * sftpRowHeight
		}
	}
	area := components.BeginScrollArea(canvas, state.Theme(), list, ui.Size{Width: list.Width, Height: float32(len(search.results)) * sftpRowHeight}).
		Offset(0, search.scrollOffset).
		VerticalScrollbar(hitSFTPSearchScrollbar, state.messages.SFTPFiles, "")
	geometry := area.BeginContent()
	search.scroll, search.scrollOffset = geometry, geometry.OffsetY
	first := min(int(geometry.OffsetY/sftpRowHeight), max(0, len(search.results)-max(visibleRows, 1)))
	search.first = first
	last := min(len(search.results), first+int(math.Ceil(float64(list.Height/sftpRowHeight)))+2)
	f := state.filesList.Begin(canvas, state.Theme(), ui.Rect{
		X: list.X, Y: list.Y - geometry.OffsetY + float32(first)*sftpRowHeight,
		Width: rowWidth, Height: float32(last-first) * sftpRowHeight,
	})
	f.Column().FillWidth().Children(func() {
		for index := first; index < last; index++ {
			state.declareSFTPSearchRow(f, search, index, hitSFTPSearchList.At(index-first))
		}
	})
	f.End()
	area.EndContent()
	search.visibleRows = visibleRows

	if len(search.results) == 0 {
		label := state.messages.Tools.FilesSearchNoMatches
		switch {
		case search.failure != "":
			label = fmt.Sprintf(state.messages.Tools.FilesSearchUnavailableFmt, search.failure)
		case search.running:
			label = state.messages.Tools.FilesSearchSearching
		}
		canvas.LabelCenter(label, list, ui.TextStyle{Size: metrics.FontSizeM, Color: state.pal.muted})
	}
}

// declareSFTPSearchRow declares one result: its icon, its name with the match
// in the accent, then its folder, which gives way first when room is short.
func (state *State) declareSFTPSearchRow(f *flex.Frame, search *sftpSearch, index int, id ui.ID) {
	pal, canvas := state.pal, f.Canvas()
	result := search.results[index]
	item := search.items[result.item]
	name := item.name
	icon, mono := sftpFileIcon(name, item.mode)
	row := sftpRow{entry: sftpclient.Entry{Name: name, Mode: item.mode}, icon: icon, mono: mono}
	tint := ui.Color{Alpha: 1}
	if mono {
		tint = pal.muted
		if item.mode&os.ModeSymlink != 0 {
			tint = pal.accent
		}
	}
	selected := index == search.selected
	var background ui.Color
	switch {
	case selected:
		background = pal.accentSoft
	case canvas.Hovered() == id:
		background = withAlpha(pal.surfaceHot, 0.7)
	}
	where := name
	if item.dir != "" {
		where = item.dir + "/" + name
	}
	box := f.Row().Height(sftpRowHeight).Shrink(0).Items(flex.Center).HitWith(id, name, ui.HitOptions{Role: ui.RoleButton, Selected: selected, Hint: where})
	contents := func() {
		declareSFTPRowIcon(f, &row, tint)
		f.Row().Grow(1).Clip().Items(flex.Center).MarginSides(0, 0, 0, 8).Children(func() {
			if prefix := name[:result.start]; prefix != "" {
				f.Text(prefix).FontSize(metrics.FontSizeM).Color(pal.text).Shrink(0)
			}
			f.Text(name[result.start:result.end]).FontSize(metrics.FontSizeM).Weight(ui.WeightSemibold).Color(pal.accent).Shrink(0)
			if suffix := name[result.end:]; suffix != "" {
				f.Text(suffix).FontSize(metrics.FontSizeM).Color(pal.text).Shrink(0)
			}
			if item.dir != "" {
				f.Text(item.dir).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().Grow(1).MarginSides(0, 0, 0, 8)
			}
		})
	}
	if background.Alpha == 0 && canvas.FocusVisible() != id {
		box.PadX(8).Children(contents)
		return
	}
	box.PadY(1).Children(func() {
		inner := f.Row().Grow(1).Self(flex.Stretch).Items(flex.Center).PadX(8).Background(background).Radius(radius.control)
		if canvas.FocusVisible() == id {
			inner.Border(1.5, pal.accent)
		}
		inner.Children(contents)
	})
}

// scrollSFTPSearch moves the results a row at a time, as scrollFolder moves a
// folder.
func scrollSFTPSearch(search *sftpSearch, event ui.ScrollEvent) ui.EventResult {
	if search.visibleRows <= 0 || len(search.results) <= search.visibleRows {
		return ui.EventResult{Handled: true}
	}
	search.wheel += event.StepY(sftpRowHeight) / sftpRowHeight
	lines := int(search.wheel)
	if lines == 0 {
		return ui.EventResult{Handled: true}
	}
	search.wheel -= float32(lines)
	before := search.first
	search.first = min(max(0, search.first+lines), len(search.results)-search.visibleRows)
	if search.first == before {
		search.wheel = 0
	}
	search.scrollOffset = float32(search.first) * sftpRowHeight
	return ui.EventResult{Handled: true, Repaint: search.first != before}
}

// sftpSearchPointer selects the result pressed and opens one double-clicked, and
// drags the results' scroll bar.
func (state *State) sftpSearchPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || state.activeSection != sectionTerminal || state.modal != modalNone || session.files == nil {
		return ui.EventResult{}, false
	}
	view := session.files
	folder := view.active()
	if !folder.searching() || view.dialog != sftpDialogNone {
		return ui.EventResult{}, false
	}
	search := folder.search
	if search.scrollDrag || event.Kind == ui.PointerDown && event.Button == ui.PointerButtonPrimary && event.Target == hitSFTPSearchScrollbar {
		return state.dragSFTPSearchScrollbar(search, event)
	}
	if event.Kind != ui.PointerDown || event.Button != ui.PointerButtonPrimary || event.Target.Scope() != hitSFTPSearchList {
		return ui.EventResult{}, false
	}
	index := search.first + item(event.Target)
	if index >= len(search.results) {
		return ui.EventResult{Handled: true}, true
	}
	search.selected = index
	if event.ClickCount >= 2 {
		state.revealSFTPSearchResult(view, folder, index)
	}
	return ui.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) dragSFTPSearchScrollbar(search *sftpSearch, event ui.PointerEvent) (ui.EventResult, bool) {
	geometry := search.scroll
	switch event.Kind {
	case ui.PointerDown:
		search.scrollDrag = true
		search.scrollGrab = geometry.VerticalThumb.Height / 2
		if event.Y >= geometry.VerticalThumb.Y && event.Y < geometry.VerticalThumb.Y+geometry.VerticalThumb.Height {
			search.scrollGrab = event.Y - geometry.VerticalThumb.Y
		}
	case ui.PointerMove:
		if !event.Captured {
			return ui.EventResult{}, true
		}
	case ui.PointerUp:
		search.scrollDrag = false
		search.first = min(max(0, int(math.Floor(float64(search.scrollOffset/sftpRowHeight)))), max(0, len(search.results)-max(search.visibleRows, 1)))
		return ui.EventResult{Handled: true, Repaint: true}, true
	case ui.PointerExit:
		if !event.Captured {
			search.scrollDrag = false
		}
		return ui.EventResult{Handled: true}, true
	default:
		return ui.EventResult{}, false
	}
	before := search.scrollOffset
	search.scrollOffset = geometry.VerticalOffsetForThumb(event.Y - search.scrollGrab)
	search.first = min(max(0, int(math.Floor(float64(search.scrollOffset/sftpRowHeight)))), max(0, len(search.results)-max(search.visibleRows, 1)))
	return ui.EventResult{Handled: true, Repaint: before != search.scrollOffset}, true
}
