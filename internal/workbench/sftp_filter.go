package workbench

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

var (
	sftpHeaderSortHits       = tree.Sub(sftpHits, tree.Name("header-sort"))
	hitSFTPFilterToggle      = tree.ID(sftpHits, "sftp-filter-toggle")
	hitSFTPFilter            = tree.ID(sftpHits, "sftp-filter")
	hitSFTPFilterClear       = tree.ID(sftpHits, "sftp-filter-clear")
	hitSFTPHiddenToggle      = tree.ID(sftpHits, "sftp-hidden-toggle")
	hitSFTPHeaderMore        = tree.ID(sftpHits, "sftp-header-more")
	hitSFTPHeaderFilter      = tree.ID(sftpHits, "sftp-header-filter")
	hitSFTPHeaderHidden      = tree.ID(sftpHits, "sftp-header-hidden")
	hitSFTPHeaderEditor      = tree.ID(sftpHits, "sftp-header-editor")
	hitSFTPHeaderMenuDismiss = tree.ID(sftpHits, "sftp-header-menu-dismiss")
	hitSFTPCoverage          = tree.ID(sftpHits, "sftp-local-coverage")
)

const sftpFilterMaxRunes = 256

func sftpFilterRune(value rune) bool { return value != '\r' && value != '\n' }

func (folder *sftpFolder) filterInputEditor() *text.Editor {
	if !folder.filterReady {
		folder.filterEditor.Restrict(sftpFilterRune, sftpFilterMaxRunes)
		folder.filterReady = true
	}
	return &folder.filterEditor
}

func (folder *sftpFolder) clearFilter() {
	folder.filterEditor = *text.NewEditor("")
	folder.filterEditor.Restrict(sftpFilterRune, sftpFilterMaxRunes)
	folder.filterReady = true
}

// selectFilterQuery selects the whole filter, so the Find shortcut's next
// typed rune or paste replaces it instead of appending to it.
func (folder *sftpFolder) selectFilterQuery() {
	editor := folder.filterInputEditor()
	editor.SetSelection(text.Range{Start: 0, End: editor.RuneCount()})
}

// SFTPInput resolves the native editor used by the current side's file filter.
// The callback derives visible rows after each text-system mutation.
func (state *State) SFTPInput(context app.TextInputContext) *app.TextInput {
	session, localID, ok := state.decodePaneHit(context.ID)
	if ok {
		context.ID = localID
	} else {
		session, ok = state.sessionByIDPointer(state.activeSessionID)
	}
	if !ok || context.ID != hitSFTPFilter || session.files == nil || state.activeSection != sectionTerminal || state.modal != modalNone {
		return nil
	}
	view := session.files
	local := view.local
	folder := view.active()
	if !folder.filterOpen || view.dialog != sftpDialogNone || view.editPath != "" {
		return nil
	}
	input := &folder.filterInput
	input.Editor = nil
	input.State = app.TextStateOf(folder.filterInputEditor())
	input.OnEvent = func(event app.TextEvent) (app.TextState, error) {
		return state.sftpFilterText(session.ID, local, event), nil
	}
	return input
}

func (state *State) sftpFilterText(sessionID uint32, local bool, event app.TextEvent) app.TextState {
	session, ok := state.sessionByIDPointer(sessionID)
	if !ok || session.files == nil {
		return app.TextState{}
	}
	view := session.files
	folder := &view.sftpFolder
	if local {
		folder = &view.lfolder
	}
	return state.applySFTPFilterText(view, folder, event)
}

// Both presentations use the owning folder, not the main window's active tab.
func (state *State) applySFTPFilterText(view *sftpView, folder *sftpFolder, event app.TextEvent) app.TextState {
	editor := folder.filterInputEditor()
	before := editor.String()
	event.Apply(editor)
	if editor.String() != before {
		folder.rememberSelection()
		state.applySFTPFilter(view, folder, false)
	}
	next := app.TextStateOf(folder.filterInputEditor())
	next.Redraw = true
	return next
}

func (state *State) clickSFTPFilter(view *sftpView, id gfx.ID) bool {
	folder := view.active()
	switch id {
	case hitSFTPFilterToggle:
		folder.filterOpen = !folder.filterOpen
		return true
	case hitSFTPFilterClear:
		folder.rememberSelection()
		folder.clearFilter()
		state.applySFTPFilter(view, folder, false)
		return true
	case hitSFTPHiddenToggle:
		folder.rememberSelection()
		folder.showHidden = !folder.showHidden
		state.applySFTPFilter(view, folder, false)
		return true
	case hitSFTPSearchToggle:
		state.toggleSFTPSearch(view)
		return true
	}
	return false
}

// sftpHeaderMenuRequest is the More menu a file panel asked for, waiting for the
// page to paint it. The menu dims the whole window, so it is painted over the
// page rather than inside the panel or the tab: a press on the window bar then
// reaches the menu's backdrop instead of a move region.
type sftpHeaderMenuRequest struct {
	queued bool
	// window is the file tab whose menu it is; nil is the docked panel's menu.
	window *sftpWindow
	side   int
	// anchor is the More button, in the canvas that painted the panel or tab.
	anchor gfx.Rect
	folder *sftpFolder
	view   *sftpView
	// owner is the dismiss identity of the menu as painted. Keeping it here is
	// what lets the keyboard and Escape find the menu again.
	owner gfx.ID
}

// sftpHeaderMenuMine reports whether id is the identity the file panel's More
// menu took.
func (state *State) sftpHeaderMenuMine(id gfx.ID) bool {
	return !id.IsZero() && id == state.sftpHeaderMenu.owner
}

// sftpHeaderMenuOwns reports whether the popup owner is the page-painted More
// menu of a file panel that can still take a choice.
func (state *State) sftpHeaderMenuOwns(id gfx.ID) bool {
	if !state.sftpHeaderMenuMine(id) {
		return false
	}
	if w := state.activeSFTPWindow(); w != nil {
		return state.modal == modalNone && !state.transfers.open && w.view != nil &&
			w.view.dialog == sftpDialogNone && w.view.editPath == ""
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	return ok && state.sftpHeaderMenuAvailable(session.files)
}

// requestSFTPHeaderMenu records the docked panel's More menu for the page.
func (state *State) requestSFTPHeaderMenu(anchor gfx.Rect, folder *sftpFolder, view *sftpView) {
	state.sftpHeaderMenu = sftpHeaderMenuRequest{queued: true, anchor: anchor, folder: folder, view: view}
}

// requestSFTPWindowHeaderMenu records a file tab's More menu for the page.
func (state *State) requestSFTPWindowHeaderMenu(window *sftpWindow, side int, anchor gfx.Rect) {
	state.sftpHeaderMenu = sftpHeaderMenuRequest{queued: true, window: window, side: side, anchor: anchor}
}

// paintQueuedSFTPHeaderMenu paints the menu an embedded file panel asked for,
// over the page, where its backdrop covers the whole window.
func (state *State) paintQueuedSFTPHeaderMenu(canvas *gfx.Canvas) {
	request := state.sftpHeaderMenu
	state.sftpHeaderMenu = sftpHeaderMenuRequest{}
	if !request.queued {
		return
	}
	if request.window == nil {
		if request.anchor.Width == 0 || state.contextRect.Width == 0 {
			return
		}
		// The panel is part of the workbench page, so its anchor is already a
		// window rectangle and its identities need no mount.
		state.sftpHeaderMenu.owner = hitSFTPHeaderMenuDismiss
		state.paintSFTPHeaderMenu(canvas, state.windowRect(), request.anchor, request.folder, request.view)
		return
	}
	if request.window.closed || request.window.window != nil {
		return
	}
	// The tab's view is part of the workbench page, so its More menu is painted
	// at the window's coordinates with the window's own identities: no mount.
	state.sftpHeaderMenu.owner = hitSFTPWindowHeaderDismiss
	request.window.paintHeaderMenu(canvas, request.side, state.windowRect(), &request.window.ui.headerScroll)
}

// paintSFTPHeaderMenu paints the docked panel's More menu over the page. Its
// anchor is already a window rectangle: the panel is part of the workbench page,
// so its identities are its own.
func (state *State) paintSFTPHeaderMenu(canvas *gfx.Canvas, viewport gfx.Rect, anchor gfx.Rect, folder *sftpFolder, view *sftpView) {
	if anchor.Width == 0 || anchor.Height == 0 {
		return
	}
	dismiss := hitSFTPHeaderMenuDismiss
	const menuWidth = float32(280)
	rows := 9 + len(folder.history.recent)
	if label, _ := state.sftpEditorItem(view, folder); label != "" {
		rows++
	}
	menuHeight := float32(rows*32 + 26)
	bounds := state.popupBelow(canvas, anchor, menuWidth, menuHeight, ui.End)
	menu := components.BeginDropdownMenu(canvas, state.Theme(), bounds, components.DropdownMenuOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: dismiss,
		Viewport: viewport, Scroll: state.menuScrollFor(dismiss),
	})
	state.menuPlaced(menu.Bounds())
	state.paintSFTPHistoryMenu(&menu, view, view.local, hitSFTPHistoryBack, hitSFTPHistoryForward, sftpRecentHits)
	menu.AddItem(components.DropdownMenuItem{
		ID: hitSFTPHeaderFilter, Label: state.messages.Tools.FilesFilterLabel, Icon: lucideIcons[iconSearch],
		Selected: folder.filterOpen || folder.filterInputEditor().String() != "",
	})
	hiddenIcon, hiddenLabel := iconEyeOff, state.messages.Tools.FilesShowHidden
	if folder.showHidden {
		hiddenIcon, hiddenLabel = iconEye, state.messages.Tools.FilesHideHidden
	}
	menu.AddItem(components.DropdownMenuItem{
		ID: hitSFTPHeaderHidden, Label: hiddenLabel, Icon: lucideIcons[hiddenIcon],
		Selected: folder.showHidden,
	})
	if label, disabled := state.sftpEditorItem(view, folder); label != "" {
		menu.AddItem(components.DropdownMenuItem{ID: hitSFTPHeaderEditor, Label: label, Icon: lucideIcons[iconExternal], Disabled: disabled})
	}
	menu.AddSeparator()
	menu.AddItem(components.DropdownMenuItem{
		ID: hitSFTPCalculateDirectorySize, Label: state.messages.SFTPWindow.CalculateDirectorySize, Icon: lucideIcons[iconInfo],
		Disabled: folder.path == "" || !view.local && view.openFiles == nil,
	})
	for _, entry := range state.sftpColumns.entries {
		if !entry.Enabled {
			continue
		}
		column, label := int(entry.Key), state.messages.SFTPWindow.Name
		switch entry.Key {
		case sftpColumnSize:
			label = state.messages.SFTPWindow.Size
		case sftpColumnModified:
			label = state.messages.SFTPWindow.Modified
		}
		icon := uint8(iconArrowUp)
		if folder.sortOrder.descending {
			icon = iconChevronDown
		}
		item := components.DropdownMenuItem{ID: sftpHeaderSortHits.At(column), Label: label, Selected: folder.sortOrder.column == column}
		if item.Selected {
			item.Icon = lucideIcons[icon]
		}
		menu.AddItem(item)
	}
	menu.End()
}

func (state *State) sftpFilterShortcut(target gfx.ID) (gfx.EventResult, bool) {
	session, local, ok := state.decodePaneHit(target)
	if !ok {
		if panelSession, panelOK := state.contextPanelSession(target); panelOK {
			session, local, ok = panelSession, target, true
		}
	}
	if !ok || local != hitSFTPFilter && local.Scope() != hitSFTPEntryList && !session.files.active().onListRow(local) {
		return gfx.EventResult{}, false
	}
	if session.files == nil || state.activeSection != sectionTerminal || state.modal != modalNone || session.files.editPath != "" || session.files.dialog != sftpDialogNone {
		return gfx.EventResult{}, false
	}
	active := session.files.active()
	active.filterOpen = true
	active.selectFilterQuery()
	return gfx.EventResult{Handled: true, Repaint: true, Focus: hitSFTPFilter, FocusVisible: true}, true
}

func (state *State) paintSFTPFilter(canvas *gfx.Canvas, bounds gfx.Rect, folder *sftpFolder) {
	state.paintSFTPFilterInput(canvas, bounds, folder, hitSFTPFilter, hitSFTPFilterClear)
}

func (state *State) paintSFTPFilterInput(canvas *gfx.Canvas, bounds gfx.Rect, folder *sftpFolder, id, clearID gfx.ID) {
	value := folder.filterInputEditor().String()
	placeholder := state.messages.Tools.FilesFilterPlaceholder
	if folder.deep {
		placeholder = state.messages.Tools.FilesSearchPlaceholder
	}
	clear := components.InputGroupSlot{}
	if value != "" {
		clear = components.InputGroupSlot{
			Kind: components.InputGroupSlotButton, ID: clearID,
			Icon: lucideIcons[iconClose], Label: state.messages.Tools.FilesFilterClear,
			ButtonSize: components.InputGroupButtonIconXS,
		}
	}
	components.PaintInputGroup(canvas, state.Theme(), bounds, components.InputGroupSnapshot{
		ID: id, Label: state.messages.Tools.FilesFilterLabel, Value: value,
		Placeholder:   placeholder,
		NativeEditing: canvas.Focused() == id && value != "",
		InlineStart:   components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: lucideIcons[iconSearch]},
		InlineEnd:     clear,
	})
}

func (state *State) applySFTPFilter(view *sftpView, folder *sftpFolder, clearSelection bool) {
	query := folder.filterInputEditor().String()
	// While the list shows search results its place is a place among them, not
	// among the folder's rows below; it comes back after the rows are rebuilt.
	shownFirst, shownOffset := folder.first, folder.scrollOffset
	if folder.searchShown {
		folder.first, folder.anchor, folder.selected = 0, -1, -1
	}
	oldFirst, oldAnchor, oldSelected := folder.first, folder.anchor, folder.selected
	topName, anchorName, selectedName := "", "", ""
	if !clearSelection {
		if oldFirst >= 0 && oldFirst < len(folder.rows) {
			topName = folder.rows[oldFirst].entry.Name
		}
		if oldAnchor >= 0 && oldAnchor < len(folder.rows) {
			anchorName = folder.rows[oldAnchor].entry.Name
		}
		if oldSelected >= 0 && oldSelected < len(folder.rows) {
			selectedName = folder.rows[oldSelected].entry.Name
		}
	}
	if clearSelection {
		for index := range folder.allRows {
			folder.allRows[index].selected = false
			folder.allRows[index].cells[0] = ""
		}
	}
	rows := folder.rows[:0]
	if cap(rows) < len(folder.allRows) {
		rows = make([]sftpRow, 0, len(folder.allRows))
	}
	shown := make([]bool, len(folder.allRows))
	folder.hiddenMatch = false
	for index := range folder.allRows {
		row := folder.allRows[index]
		row.matchStart, row.matchEnd = 0, 0
		if query == "" {
			if row.hidden && !folder.showHidden {
				continue
			}
		} else {
			start, end, ok := sftpFilterMatch(row.entry.Name, query)
			if row.hidden && !folder.showHidden {
				folder.hiddenMatch = folder.hiddenMatch || ok
				continue
			}
			if !ok {
				continue
			}
			row.matchStart, row.matchEnd = start, end
		}
		shown[index] = true
		rows = append(rows, row)
	}
	if !clearSelection {
		// A filtered-out row cannot stay selected, or a later transfer would
		// act on something the list does not show; a row still in the list
		// keeps its checkmark.
		for index := range folder.allRows {
			if !shown[index] {
				folder.allRows[index].selected = false
				folder.allRows[index].cells[0] = ""
			}
		}
	}
	folder.rows = rows
	if clearSelection || len(rows) == 0 {
		folder.selected, folder.anchor, folder.first = -1, -1, 0
	} else {
		folder.first = sftpRowNameIndex(rows, topName)
		if folder.first < 0 {
			folder.first = min(oldFirst, max(0, len(rows)-max(folder.visibleRows, 1)))
		}
		folder.anchor = sftpRowNameIndex(rows, anchorName)
		if folder.anchor < 0 && oldAnchor >= 0 {
			folder.anchor = min(oldAnchor, len(rows)-1)
		}
		folder.selected = sftpRowNameIndex(rows, selectedName)
		if folder.selected < 0 && oldSelected >= 0 {
			folder.selected = min(oldSelected, len(rows)-1)
		}
	}
	view.sortFilteredFolder(folder == &view.lfolder)
	if clearSelection {
		folder.first = 0
	}
	state.refreshLocalCoverage(folder)
	if folder.searchShown {
		folder.first, folder.scrollOffset = shownFirst, shownOffset
	}
	folder.listNeedsSeed = true
	folder.listPendingFirst = folder.first
	state.syncSFTPSearch(view, folder)
}

func (state *State) refreshLocalCoverage(folder *sftpFolder) {
	folder.coverageLabel, folder.coverageText, folder.coverageEmpty = "", "", ""
	coverage := folder.localCoverage
	if !coverage.incomplete() {
		return
	}
	m := state.messages.Tools
	folder.coverageLabel = fmt.Sprintf(m.FilesLoadedIncompleteFmt, len(folder.allRows))
	folder.coverageText = folder.coverageLabel + ". " + m.FilesLoadedFilterOnly
	if coverage.truncated {
		folder.coverageText += " " + fmt.Sprintf(m.FilesLoadedLimitFmt, maxLocalEntries)
	}
	if coverage.skipped > 0 {
		folder.coverageText += " " + fmt.Sprintf(m.FilesLoadedSkippedFmt, coverage.skipped)
	}
	if coverage.readError != "" {
		folder.coverageText += " " + fmt.Sprintf(m.FilesLoadedReadErrorFmt, coverage.readError)
	}
	folder.coverageEmpty = m.FilesLoadedEmpty
	if len(folder.allRows) > 0 && len(folder.rows) == 0 {
		folder.coverageEmpty = m.FilesLoadedNoneVisible
	}
	if folder.filterInputEditor().String() != "" {
		folder.coverageEmpty = m.FilesLoadedNoMatches
		if folder.hiddenMatch {
			folder.coverageEmpty = m.FilesLoadedMatchesHidden
		}
	}
}

func (state *State) declareLocalCoverage(c ui.C, folder *sftpFolder, id gfx.ID) {
	if folder.coverageText == "" {
		return
	}
	ui.Column(c).Height(38).Shrink(0).HitWith(id, folder.coverageText, gfx.HitOptions{Role: gfx.RoleStatus, SkipFocus: true, Hint: folder.coverageText}).Children(func() {
		ui.Text(c, folder.coverageLabel).FontSize(metrics.FontSizeS).Color(state.pal.warning).Ellipsis().Height(18).Shrink(0)
		ui.Text(c, state.messages.Tools.FilesLoadedFilterOnly).FontSize(metrics.FontSizeS).Color(state.pal.muted).Ellipsis().Height(18).Shrink(0)
	})
}

func sftpRowNameIndex(rows []sftpRow, name string) int {
	if name == "" {
		return -1
	}
	for index := range rows {
		if rows[index].entry.Name == name {
			return index
		}
	}
	return -1
}

func (state *State) refreshSFTPFilterStatus(view *sftpView, folder *sftpFolder) {
	if view.detached != nil {
		if folder == &view.sftpFolder && view.approximate {
			view.setItemCountStatus(state.messages.SCPItemsFmt, len(folder.rows))
		} else {
			view.setItemCountStatus(state.messages.SFTPItemsFmt, len(folder.rows))
		}
		return
	}
	if folder.searching() {
		// The reading's progress, and what it could not read, stay on show
		// whatever else asks for the status to be written again.
		state.refreshSFTPSearchStatus(view, folder)
		return
	}
	if folder.filterInputEditor().String() != "" {
		view.status = fmt.Sprintf(state.messages.Tools.FilesFilterCountFmt, len(folder.rows), sftpVisibleRowCount(folder))
		return
	}
	if folder == &view.sftpFolder && view.approximate {
		view.status = fmt.Sprintf(state.messages.SCPItemsFmt, len(folder.rows))
		return
	}
	view.status = fmt.Sprintf(state.messages.SFTPItemsFmt, len(folder.rows))
}

func sftpVisibleRowCount(folder *sftpFolder) int {
	if folder.showHidden {
		return len(folder.allRows)
	}
	count := 0
	for index := range folder.allRows {
		if !folder.allRows[index].hidden {
			count++
		}
	}
	return count
}

func sftpFilterMatch(name, query string) (int, int, bool) {
	foldedName, foldedQuery := strings.ToLower(name), strings.ToLower(query)
	index := strings.Index(foldedName, foldedQuery)
	if index < 0 {
		return 0, 0, false
	}
	startRunes := utf8.RuneCountInString(foldedName[:index])
	matchRunes := utf8.RuneCountInString(foldedQuery)
	start := sftpRuneByteOffset(name, startRunes)
	end := sftpRuneByteOffset(name, startRunes+matchRunes)
	if end < start {
		return 0, 0, false
	}
	return start, end, true
}

func sftpRuneByteOffset(value string, runes int) int {
	if runes <= 0 {
		return 0
	}
	count := 0
	for offset := range value {
		if count == runes {
			return offset
		}
		count++
	}
	return len(value)
}

// The visible copy owns user selection; retain it when rebuilding from the
// complete listing, and never resurrect a checkmark hidden by an earlier query.
func (folder *sftpFolder) rememberSelection() {
	var selected map[string]struct{}
	for _, row := range folder.rows {
		if row.selected {
			if selected == nil {
				selected = make(map[string]struct{})
			}
			selected[row.entry.Name] = struct{}{}
		}
	}
	for index := range folder.allRows {
		_, folder.allRows[index].selected = selected[folder.allRows[index].entry.Name]
		folder.allRows[index].cells[0] = sftpSelectionCell(folder.allRows[index].selected)
	}
}
