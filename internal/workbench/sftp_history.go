package workbench

import (
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

const maxSFTPHistory, maxSFTPRecent = 50, 10

var (
	hitSFTPHistoryBack       = tree.ID(sftpHits, "history-back")
	hitSFTPHistoryForward    = tree.ID(sftpHits, "history-forward")
	sftpRecentHits           = tree.Sub(sftpHits, tree.Name("recent"))
	sftpWindowHistoryBack    = [2]gfx.ID{tree.ID(sftpWindowLocal, "history-back"), tree.ID(sftpWindowRemote, "history-back")}
	sftpWindowHistoryForward = [2]gfx.ID{tree.ID(sftpWindowLocal, "history-forward"), tree.ID(sftpWindowRemote, "history-forward")}
	sftpWindowRecentHits     = [2]gfx.Scope{tree.Sub(sftpWindowLocal, tree.Name("recent")), tree.Sub(sftpWindowRemote, tree.Name("recent"))}
)

type sftpPlace struct {
	top, current, anchor string
	first                int
	selected             []string
}

type sftpVisit struct {
	path  string
	place sftpPlace
}

type sftpHistory struct {
	entries []sftpVisit
	recent  []string
	cursor  int
}

type sftpSavedHistory struct {
	history sftpHistory
	path    string
}

func (view *sftpView) saveHistory() [2]sftpSavedHistory {
	var saved [2]sftpSavedHistory
	for side, folder := range []*sftpFolder{&view.lfolder, &view.sftpFolder} {
		if len(folder.history.entries) > 0 && folder.path == folder.history.entries[folder.history.cursor].path {
			folder.history.entries[folder.history.cursor].place = folder.place()
		}
		saved[side] = sftpSavedHistory{history: folder.history, path: folder.path}
	}
	return saved
}

func (view *sftpView) adoptHistory(saved [2]sftpSavedHistory) {
	view.lfolder.history, view.sftpFolder.history = saved[0].history, saved[1].history
}

type sftpNavigationKind uint8

const (
	sftpNavigationPassive sftpNavigationKind = iota
	sftpNavigationManual
	sftpNavigationHistory
)

// A listing owns this value from request to accepted rows, including cache,
// background preparation and drag deferral. A later same-path request is distinct.
type sftpNavigation struct {
	id    uint64
	path  string
	kind  sftpNavigationKind
	index int
	// reveal is the entry to select when this navigation lands: it belongs to
	// the navigation that was started for it, not to the folder, so a reading
	// of another folder arriving first, or this one failing, neither spends
	// nor keeps it.
	reveal sftpReveal
}

func (state *State) beginSFTPNavigation(view *sftpView, local bool, directory string, kind sftpNavigationKind, index int) sftpNavigation {
	view.navigationID++
	nav := sftpNavigation{id: view.navigationID, path: directory, kind: kind, index: index}
	folder := &view.sftpFolder
	if local {
		folder = &view.lfolder
	}
	nav.reveal, folder.reveal = folder.reveal, sftpReveal{}
	folder.navigation = nav
	if !local {
		view.remoteSeq++
		view.preparingID = 0
		view.preparingPath, view.followUpPath, view.followUpEntries = "", "", nil
	}
	return nav
}

func (folder *sftpFolder) place() sftpPlace {
	p := sftpPlace{first: folder.first}
	name := func(index int) string {
		if index >= 0 && index < len(folder.rows) {
			return folder.rows[index].entry.Name
		}
		return ""
	}
	p.top, p.current, p.anchor = name(folder.first), name(folder.selected), name(folder.anchor)
	for _, row := range folder.rows {
		if row.selected {
			p.selected = append(p.selected, row.entry.Name)
		}
	}
	return p
}

func (history *sftpHistory) touch(directory string) {
	if i := slices.Index(history.recent, directory); i >= 0 {
		history.recent = slices.Delete(history.recent, i, i+1)
	}
	history.recent = append(history.recent, "")
	copy(history.recent[1:], history.recent[:len(history.recent)-1])
	history.recent[0] = directory
	if len(history.recent) > maxSFTPRecent {
		history.recent = history.recent[:maxSFTPRecent]
	}
}

// Called only after acceptance guards, before replacing the outgoing rows.
func (folder *sftpFolder) acceptNavigation(directory string, nav sftpNavigation) (sftpPlace, bool) {
	h := &folder.history
	if folder.path == "" && len(h.entries) > 0 && h.entries[h.cursor].path == directory && nav.kind == sftpNavigationPassive {
		folder.navigation = sftpNavigation{}
		return h.entries[h.cursor].place, true
	}
	if len(h.entries) > 0 && h.entries[h.cursor].path == folder.path {
		h.entries[h.cursor].place = folder.place()
	}
	folder.navigation = sftpNavigation{}
	if len(h.entries) == 0 {
		h.entries = append(h.entries, sftpVisit{path: directory})
		h.touch(directory)
		return sftpPlace{}, false
	}
	if nav.kind == sftpNavigationHistory && nav.index >= 0 && nav.index < len(h.entries) && h.entries[nav.index].path == directory {
		h.cursor = nav.index
		h.touch(directory)
		return h.entries[h.cursor].place, true
	}
	if nav.kind != sftpNavigationManual || folder.path == directory {
		return sftpPlace{}, false
	}
	clear(h.entries[h.cursor+1:])
	h.entries = append(h.entries[:h.cursor+1], sftpVisit{path: directory})
	if len(h.entries) > maxSFTPHistory {
		h.entries = slices.Delete(h.entries, 0, len(h.entries)-maxSFTPHistory)
	}
	h.cursor = len(h.entries) - 1
	h.touch(directory)
	return sftpPlace{}, false
}

func (folder *sftpFolder) restorePlace(place sftpPlace) {
	folder.first = sftpRowNameIndex(folder.rows, place.top)
	if folder.first < 0 {
		folder.first = place.first
	}
	folder.first = max(0, min(folder.first, max(0, len(folder.rows)-max(folder.visibleRows, 1))))
	folder.selected = sftpRowNameIndex(folder.rows, place.current)
	folder.anchor = sftpRowNameIndex(folder.rows, place.anchor)
	selected := make(map[string]struct{}, len(place.selected))
	for _, name := range place.selected {
		selected[name] = struct{}{}
	}
	for i := range folder.rows {
		_, folder.rows[i].selected = selected[folder.rows[i].entry.Name]
		folder.rows[i].cells[0] = sftpSelectionCell(folder.rows[i].selected)
	}
	folder.rememberSelection()
}

func (state *State) finishSFTPNavigation(view *sftpView, local bool, place sftpPlace, restore bool, nav sftpNavigation) {
	folder := &view.sftpFolder
	if local {
		folder = &view.lfolder
	}
	if restore {
		folder.restorePlace(place)
	}
	folder.applyReveal(nav.reveal)
	if w := view.detached; w != nil {
		side := sideIndex(local)
		// Same-path refresh keeps its precise offset; navigation restores a place.
		if restore || w.shownPaths[side] != folder.path {
			w.ui.offset[side] = float32(folder.first) * sftpRowHeight
		}
		w.shownPaths[side] = folder.path
	}
}

func (folder *sftpFolder) historyTarget(delta int) (int, bool) {
	h := &folder.history
	if len(h.entries) == 0 {
		return 0, false
	}
	index := h.cursor + delta
	if delta < 0 && folder.path != h.entries[h.cursor].path {
		index = h.cursor
	}
	return index, index >= 0 && index < len(h.entries)
}

func (state *State) sftpNavigationReady(view *sftpView, local bool) bool {
	if view == nil || view.dialog != sftpDialogNone || view.editPath != "" {
		return false
	}
	if w := view.detached; w != nil && (w.closed || w.closeConfirm || w.bookmark.open || w.browse.pending != nil || w.externalConflict != 0 || local && w.pendingLocal != "" || !local && !w.connected()) {
		return false
	}
	if local {
		return view.lfolder.navigation.id == 0
	}
	if view.detached == nil {
		session, ok := state.sftpSession(view)
		if !ok || session.phase != sshconn.PhaseConnected || session.conn == nil || session.conn.Status().Phase != sshconn.PhaseConnected {
			return false
		}
	}
	return view.worker != nil && !view.busy
}

func (state *State) navigateSFTPHistory(view *sftpView, local bool, delta int) bool {
	if !state.sftpNavigationReady(view, local) {
		return false
	}
	folder := &view.sftpFolder
	if local {
		folder = &view.lfolder
	}
	index, ok := folder.historyTarget(delta)
	if !ok {
		return false
	}
	state.navigateSFTPFolder(view, local, folder.history.entries[index].path, sftpNavigationHistory, index)
	return true
}

func (state *State) navigateSFTPRecent(view *sftpView, local bool, index int) bool {
	if !state.sftpNavigationReady(view, local) {
		return false
	}
	folder := &view.sftpFolder
	if local {
		folder = &view.lfolder
	}
	if index < 0 || index >= len(folder.history.recent) {
		return false
	}
	state.navigateSFTPFolder(view, local, folder.history.recent[index], sftpNavigationManual, 0)
	return true
}

func (state *State) navigateSFTPFolder(view *sftpView, local bool, directory string, kind sftpNavigationKind, index int) {
	if w := view.detached; w != nil {
		w.cancelDrag()
		w.activeLocal, view.local = local, local
		w.startBrowseNavigationHistory(local, directory, kind, index)
		return
	}
	if local {
		_ = state.listLocalNavigation(view, directory, kind, index)
	} else {
		state.navigateSFTPDirectoryNavigation(view, directory, kind, index)
	}
}

func (state *State) navigateLocalDirectory(view *sftpView, directory string) error {
	return state.listLocalNavigation(view, directory, sftpNavigationManual, 0)
}

func (state *State) cancelSFTPNavigation(view *sftpView) {
	if view.detached != nil {
		view.detached.cancelBrowseNavigation(false, view.navigation.path)
	}
	view.navigation = sftpNavigation{}
	view.remoteSeq++
	view.preparingPath, view.followUpPath, view.followUpEntries = "", "", nil
	if view.waiting != "" {
		delete(view.prefetching, view.waiting)
	}
	view.waiting, view.pathRequest = "", ""
	view.waitingID, view.preparingID = 0, 0
	view.terminalPath = ""
	if view.operation == sftpclient.OperationList {
		view.busy = false
	}
}

func (state *State) cancelSFTPLocalNavigation(view *sftpView) {
	if view.detached != nil {
		view.detached.cancelBrowseNavigation(true, view.lfolder.navigation.path)
	}
	view.localSeq++
	view.lfolder.navigation = sftpNavigation{}
	if view.detached != nil {
		view.detached.pendingLocal = ""
	}
	state.retireLocalListingStatus(view)
}

func sftpHistoryChord(event gfx.KeyEvent) (int, bool) {
	if event.Release || event.Repeat || event.Modifiers != gfx.ModifierAlt {
		return 0, false
	}
	switch event.Key {
	case gfx.KeyLeft:
		return -1, true
	case gfx.KeyRight:
		return 1, true
	}
	return 0, false
}

func (state *State) sftpHistoryKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	delta, chord := sftpHistoryChord(event)
	if !chord || state.activeSection != sectionTerminal || state.modal != modalNone || state.transfers.open {
		return gfx.EventResult{}, false
	}
	target := event.Target
	if target.Scope() != hitSFTPEntryList && target.Scope() != hitSFTPBreadcrumbList && target != hitSFTPPathEdit && target != hitSFTPHeaderMore && target != hitSFTPUp && target != hitSFTPRefresh {
		return gfx.EventResult{}, false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.files == nil || session.files.editPath != "" || session.files.dialog != sftpDialogNone {
		return gfx.EventResult{}, false
	}
	state.navigateSFTPHistory(session.files, session.files.local, delta)
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) sftpHeaderMenuAvailable(view *sftpView) bool {
	return state.modal == modalNone && !state.transfers.open && view != nil && view.dialog == sftpDialogNone && view.editPath == ""
}

func (state *State) paintSFTPHistoryMenu(menu *components.DropdownMenu, view *sftpView, local bool, back, forward gfx.ID, recent gfx.Scope) {
	folder := &view.sftpFolder
	if local {
		folder = &view.lfolder
	}
	ready := state.sftpNavigationReady(view, local)
	_, canBack := folder.historyTarget(-1)
	_, canForward := folder.historyTarget(1)
	m := state.messages.Tools
	menu.AddItem(components.DropdownMenuItem{ID: back, Label: m.FilesBack, Shortcut: "Alt+Left", Disabled: !ready || !canBack})
	menu.AddItem(components.DropdownMenuItem{ID: forward, Label: m.FilesForward, Shortcut: "Alt+Right", Disabled: !ready || !canForward})
	if len(folder.history.recent) > 0 {
		menu.AddLabel(m.FilesRecent, false)
		for i, directory := range folder.history.recent {
			menu.AddItem(components.DropdownMenuItem{ID: recent.At(i), Label: directory, Hint: directory, Disabled: !ready, Selected: directory == folder.path})
		}
	}
	menu.AddSeparator()
}

func (state *State) clickSFTPHistory(view *sftpView, local bool, id, back, forward gfx.ID, recent gfx.Scope) bool {
	switch id {
	case back:
		state.navigateSFTPHistory(view, local, -1)
		return true
	case forward:
		state.navigateSFTPHistory(view, local, 1)
		return true
	}
	if index, ok := id.In(recent); ok {
		state.navigateSFTPRecent(view, local, index)
		return true
	}
	return false
}
