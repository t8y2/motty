package workbench

import (
	"fmt"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/sessionlog"
)

// The session-log page's interactions. Everything that changes what the page
// shows happens here or in session_logs.go: a click, a key, a scroll gesture, or
// the dirty-check Wake runs. Paint never starts a query and never writes.

// openSessionLogs shows the journal and loads its first page.
func (state *State) openSessionLogs() {
	if state.activeSection != sectionSessionLogs {
		state.activeSection = sectionSessionLogs
		state.save()
		state.refreshSessionLogFilters()
		return
	}
	view := &state.sessionLogs.view
	if !view.loaded && !view.loading {
		state.refreshSessionLogFilters()
	}
}

// syncSessionLogs runs from Wake, before the repaint, and never from Paint. It
// turns what the user typed into the native search field into a store query,
// picks up a language change, and starts the first page when the page becomes
// visible. Key events are not a reliable trigger for typing: the characters go to
// the platform's text service, so the comparison here is what notices them.
func (state *State) syncSessionLogs() {
	view := &state.sessionLogs.view
	if state.activeSection != sectionSessionLogs {
		state.stopSessionLogCalendar()
		return
	}
	state.updateSessionLogCalendar()
	clockChanged := view.synced && view.clockStamp != state.sessionLogClockStamp()
	if clockChanged && view.timeRange != sessionLogTimeAll {
		state.refreshSessionLogFilters()
		return
	}
	if !view.synced || view.syncedLanguage != state.language || clockChanged {
		view.clockStamp = state.sessionLogClockStamp()
		state.prepareSessionLogsView()
	}
	query := strings.TrimSpace(view.search.String())
	if query != view.appliedSearch {
		view.appliedSearch = query
		view.query.Search = query
		view.query.Cursor = sessionlog.Cursor{}
		state.resetSessionLogsScroll()
		view.detailOpen = false
		view.selected = ""
		state.refreshSessionLogs()
		return
	}
	if state.activeSection == sectionSessionLogs && view.dirty {
		view.dirty = false
		// A record can finish while the list is sorted by duration, which moves
		// it; every other order a settled write leaves alone.
		if view.query.Sort == sessionlog.SortDurationAsc || view.query.Sort == sessionlog.SortDurationDesc {
			state.refreshSessionLogs()
		} else {
			state.refreshSessionLogsKeepingScroll()
		}
		return
	}
	if state.activeSection == sectionSessionLogs && !view.loaded && !view.loading && !view.unavailable {
		state.refreshSessionLogFilters()
	}
}

// sessionLogRange is the query's time window for a filter choice.
func sessionLogRange(now time.Time, span sessionLogTimeRange) (since, until time.Time) {
	local := now
	switch span {
	case sessionLogTimeToday:
		midnight := sessionLogCalendarBoundary(sessionLogCalendarDate(local), local.Location())
		return midnight, time.Time{}
	case sessionLogTimeWeek:
		return now.Add(-7 * 24 * time.Hour), time.Time{}
	case sessionLogTimeMonth:
		return now.Add(-30 * 24 * time.Hour), time.Time{}
	default:
		return time.Time{}, time.Time{}
	}
}

// applySessionLogFilters pushes the filter choices into the query and reloads
// the first page. It runs from a click, never from Paint.
func (state *State) applySessionLogFilters() {
	if state.refreshSessionLogFilters() {
		state.rememberSessionLogsView()
	}
}

// Opening a page or crossing midnight recalculates relative bounds without
// replacing defaults most recently chosen in another window.
func (state *State) refreshSessionLogFilters() bool {
	view := &state.sessionLogs.view
	state.updateSessionLogCalendar()
	view.query.Status = nil
	if view.statusFilter != 0 {
		status := view.statusFilter
		view.query.Status = &status
	}
	view.query.Protocol = nil
	if view.protocolFilter != 0 {
		protocol := view.protocolFilter
		view.query.Protocol = &protocol
	}
	view.query.Favorite = nil
	if view.favoriteOnly {
		favorite := true
		view.query.Favorite = &favorite
	}
	view.query.Since, view.query.Until = sessionLogRange(state.wallNow().In(state.sessionLogLocation()), view.timeRange)
	if view.timeRange == sessionLogTimeCustom {
		since, until, err := sessionLogDateRange(view.customStartDate, view.customEndDate, state.sessionLogLocation())
		if err != nil {
			view.statusText, view.statusDanger = state.messages.SessionLogs.InvalidDateRange, true
			return false
		}
		view.query.Since, view.query.Until = since, until
	}
	view.query.Cursor = sessionlog.Cursor{}
	state.resetSessionLogsScroll()
	view.filterMenu = sessionLogFilterNone
	view.filterOpen = false
	view.clockStamp = state.sessionLogClockStamp()
	state.refreshSessionLogs()
	return true
}

// chooseSessionLogFilter applies one choice of the open filter menu.
func (state *State) chooseSessionLogFilter(index int) {
	state.chooseSessionLogFilterIn(state.sessionLogs.view.filterMenu, index)
}

// chooseSessionLogFilterIn applies one choice of menu. The menu closes as the
// choice is delivered, so the page menu carries the filter it belonged to
// instead of reading it back after the popup has gone.
func (state *State) chooseSessionLogFilterIn(menu sessionLogFilterMenu, index int) {
	view := &state.sessionLogs.view
	view.filterMenu = sessionLogFilterNone
	view.filterOpen = false
	if index < 0 {
		return
	}
	switch menu {
	case sessionLogFilterSortMenu:
		if index < 4 {
			view.query.Sort = sessionlog.SortOrder(index)
		}
	case sessionLogFilterOutcomeMenu:
		if index < 4 {
			view.query.Outcome = sessionlog.OutcomeFilter(index)
		}
	case sessionLogFilterTimeMenu:
		if index == int(sessionLogTimeCustom) {
			state.askSessionLogCustomRange()
			return
		}
		if index < 4 {
			view.timeRange = sessionLogTimeRange(index)
		}
	case sessionLogFilterStatusMenu:
		if index < len(sessionLogStatusChoices) {
			view.statusFilter = sessionLogStatusChoices[index]
		}
	case sessionLogFilterProtocolMenu:
		if index < len(sessionLogProtocolChoices) {
			view.protocolFilter = sessionLogProtocolChoices[index]
		}
	}
	state.applySessionLogFilters()
}

// toggleSessionLogHeaderSort sorts by the column whose heading was clicked: the
// started column and the duration column, the two orders the store offers. A
// narrow table shows the duration in the started column while the order is one
// of the duration orders, so clicking it keeps sorting by what it shows.
func (state *State) toggleSessionLogHeaderSort(column int) {
	view := &state.sessionLogs.view
	_, _, count := state.sessionLogColumns(view.tableWidth)
	duration := view.query.Sort >= sessionlog.SortDurationDesc
	started := view.query.Sort <= sessionlog.SortStartedAsc
	switch {
	case column == 4:
		if duration && view.query.Sort == sessionlog.SortDurationAsc {
			view.query.Sort = sessionlog.SortDurationDesc
		} else {
			view.query.Sort = sessionlog.SortDurationAsc
		}
	case column == 1 && count < 6 && duration:
		if view.query.Sort == sessionlog.SortDurationAsc {
			view.query.Sort = sessionlog.SortDurationDesc
		} else {
			view.query.Sort = sessionlog.SortDurationAsc
		}
	case column == 1:
		if started && view.query.Sort == sessionlog.SortStartedAsc {
			view.query.Sort = sessionlog.SortStartedDesc
		} else {
			view.query.Sort = sessionlog.SortStartedAsc
		}
	default:
		return
	}
	state.applySessionLogFilters()
}

// clearSessionLogFilters returns the page to its widest query.
func (state *State) clearSessionLogFilters() {
	view := &state.sessionLogs.view
	view.search = text.Editor{}
	view.appliedSearch = ""
	view.query.Search = ""
	view.timeRange = sessionLogTimeAll
	view.statusFilter = 0
	view.protocolFilter = 0
	view.favoriteOnly = false
	view.query.Outcome = sessionlog.OutcomeAll
	view.filterMenu = sessionLogFilterNone
	view.filterOpen = false
	view.statusText = ""
	view.statusDanger = false
	state.applySessionLogFilters()
	view.focusPending = hitSessionLogsSearch
}

// clearSessionLogSearch empties the native search field and reloads, which is
// what Escape in the box does. The editor text is the authority; the query
// follows it on the next Wake.
func (state *State) clearSessionLogSearch() {
	view := &state.sessionLogs.view
	view.search = text.Editor{}
	view.appliedSearch = ""
	view.query.Search = ""
	view.query.Cursor = sessionlog.Cursor{}
	state.resetSessionLogsScroll()
	view.detailOpen = false
	view.selected = ""
	state.refreshSessionLogs()
}

// resetSessionLogsScroll asks the record list to show its first row at the next
// declaration and forgets the offset the paint last recorded.
func (state *State) resetSessionLogsScroll() {
	view := &state.sessionLogs.view
	view.rowsList.ScrollTo(0, ui.ListStart)
	view.scrollRows = 0
}

// selectSessionLogRecord opens one record: it becomes the selected row and its
// events load in the detail panel. Only the stable record ID is kept.
func (state *State) selectSessionLogRecord(id string) {
	view := &state.sessionLogs.view
	if id == "" {
		return
	}
	view.selected = id
	view.detailOpen = true
	view.resetEvents = true
	view.statusText = ""
	view.statusDanger = false
	state.loadSessionLogEvents(id)
	state.prepareSessionLogDetail()
	view.focusPending = gfx.ID{}
}

// closeSessionLogDetail closes the detail panel and gives the keyboard back to
// the row it was opened from, or to the search box when there is none.
func (state *State) closeSessionLogDetail() {
	view := &state.sessionLogs.view
	view.detailOpen = false
	view.focusPending = hitSessionLogsSearch
	if item := state.sessionLogSelectedItemIndex(); item >= 0 {
		if id := view.rowsList.RowID(item); !id.IsZero() {
			view.focusPending = id
		}
	}
}

// sessionLogSelectedItemIndex is the record list's row for the selected record,
// or -1: the list interleaves the date headings with the records, so a record's
// index in the page is not its row.
func (state *State) sessionLogSelectedItemIndex() int {
	view := &state.sessionLogs.view
	if view.selected == "" {
		return -1
	}
	for index, item := range view.items {
		if item.row >= 0 && item.row < len(view.page) && view.page[item.row].id == view.selected {
			return index
		}
	}
	return -1
}

// toggleSessionLogFavorite waits for the writer before claiming success. A
// failed write leaves the currently painted favorite unchanged.
func (state *State) toggleSessionLogFavorite() {
	state.toggleSessionLogFavoriteID(state.sessionLogs.view.selected)
}

func (state *State) toggleSessionLogFavoriteID(id string) {
	view := &state.sessionLogs.view
	if view.favoritePending != "" {
		return
	}
	record, ok := state.sessionLogRecord(id)
	if !ok {
		return
	}
	view.favoritePending = record.ID
	state.setSessionLogFavorite(record.ID, !record.Favorite)
	view.statusDanger = false
	view.statusText = state.messages.SessionLogs.Loading
	state.prepareSessionLogsView()
}

// askDeleteSessionLog asks before deleting the selected record. The confirmation
// names the record, so its scope is visible in the dialog.
func (state *State) askDeleteSessionLog() {
	record := state.sessionLogSelectedRecord()
	if record == nil {
		return
	}
	m := &state.messages.SessionLogs
	id := record.ID
	target := sessionLogTargetText(record)
	state.openForm(formDialog{
		title: m.DeleteTitle, description: fmt.Sprintf(m.DeletePromptFmt, target),
		submit: m.DeleteConfirm, danger: true,
		run: func(state *State, _ *formDialog) string {
			state.deleteSessionLogRecord(id)
			return ""
		},
	})
}

// deleteSessionLogRecord keeps the row visible until the database confirms
// deletion, so a write failure cannot appear to succeed.
func (state *State) deleteSessionLogRecord(id string) {
	view := &state.sessionLogs.view
	state.deleteSessionLog(id)
	view.statusDanger = false
	view.statusText = state.messages.SessionLogs.Loading
	state.prepareSessionLogsView()
}

// askClearSessionLogs asks before clearing part of the journal. The prompt says
// what the confirmation covers: unfavorited records only, or every record.
func (state *State) askClearSessionLogs(includeFavorites bool) {
	m := &state.messages.SessionLogs
	title, scope, submit := m.ClearUnfavoritedTitle, m.ClearUnfavorited, m.ClearUnfavoritedConfirm
	if includeFavorites {
		title, scope, submit = m.ClearAllTitle, m.ClearAll, m.ClearAllConfirm
	}
	store := state.sessionLogs.store
	var selection sessionlog.ClearSelection
	ready := false
	state.openForm(formDialog{
		title: title, description: m.Loading, submit: submit, danger: true,
		cancel: func(state *State) { state.sessionLogs.view.focusPending = hitSessionLogsMore },
		run: func(state *State, _ *formDialog) string {
			if !ready {
				return m.Loading
			}
			state.submitSessionLog(sessionlog.Operation{Kind: sessionlog.OpDeleteSelection, IDs: selection.IDs, IncludeFavorites: includeFavorites})
			state.sessionLogs.view.statusText = m.Loading
			state.sessionLogs.view.statusDanger = false
			return ""
		},
	})
	serial := state.formSerial
	if store == nil {
		state.form.dialogError = m.Unavailable
		return
	}
	invalidate := state.options.Invalidate
	go func() {
		preview, err := store.PreviewClear(includeFavorites)
		state.aiPostWithInvalidator(func(state *State) {
			if state.modal != modalForm || state.formSerial != serial {
				return
			}
			if err != nil {
				state.form.dialogError = m.ListFailedHint
				return
			}
			selection, ready = preview, true
			state.form.description = fmt.Sprintf(m.ClearSelectionFmt, scope, len(preview.IDs), preview.Favorites)
			state.form.dialogError = ""
		}, invalidate)
	}()
}

// clearSessionLogRecords clears this journal only; command history, manual
// recordings and the diagnostic log are other files and stay untouched.
func (state *State) clearSessionLogRecords(includeFavorites bool) {
	view := &state.sessionLogs.view
	state.clearSessionLogsAndNotify(includeFavorites)
	view.statusDanger = false
	view.statusText = state.messages.SessionLogs.Loading
	state.prepareSessionLogsView()
}

// jumpToLiveSession opens the tab that still owns the selected record. The
// record ID is stable across pane and window moves; the integer session ID is
// not, so it is resolved here and never stored.
func (state *State) jumpToLiveSession() {
	view := &state.sessionLogs.view
	record := state.sessionLogSelectedRecord()
	if record == nil {
		return
	}
	if !state.jumpToLogSession(record.ID) {
		view.statusDanger = false
		view.statusText = state.messages.SessionLogs.SessionClosed
		state.prepareSessionLogDetail()
	}
}

func (state *State) jumpToLogSession(id string) bool {
	owner, session := state.sessionLogOwner(id)
	if session == nil {
		return false
	}
	owner.ActivatePane(session.ID)
	owner.status = owner.messages.StatusOpened + session.Title
	owner.save()
	if owner != state && owner.options.Raise != nil {
		owner.options.Raise()
	}
	owner.invalidateWindow()
	view := &state.sessionLogs.view
	view.statusText = ""
	view.statusDanger = false
	return true
}

// clickSessionLogs handles one click on the page or its sidebar row.
func (state *State) clickSessionLogs(id gfx.ID) bool {
	if id == hitSessionLogs {
		if state.modal != modalNone {
			return false
		}
		state.openSessionLogs()
		return true
	}
	view := &state.sessionLogs.view
	if state.activeSection != sectionSessionLogs || state.modal != modalNone {
		return false
	}
	if view.moreMenu && id != hitSessionLogsMore {
		view.moreMenu = false
		if id != hitSessionLogsClearFavorite && id != hitSessionLogsClearAll {
			return true
		}
	}
	switch id {
	case hitSessionLogsMore:
		view.moreMenu = !view.moreMenu
		view.filterMenu = sessionLogFilterNone
		view.filterOpen = false
		return true
	case hitSessionLogsSearch:
		return true
	case hitSessionLogsTime, hitSessionLogsStatus, hitSessionLogsProtocol, hitSessionLogsOutcome, hitSessionLogsSort:
		view.moreMenu = false
		filter := sessionLogFilterFor(id)
		if view.filterMenu == filter && view.filterOpen {
			view.filterMenu = sessionLogFilterNone
			view.filterOpen = false
		} else {
			view.filterMenu = filter
			view.filterOpen = true
		}
		return true
	case hitSessionLogsFavoriteFilter:
		view.favoriteOnly = !view.favoriteOnly
		state.applySessionLogFilters()
		return true
	case hitSessionLogsFavorite:
		state.toggleSessionLogFavorite()
		return true
	case hitSessionLogsDelete:
		state.askDeleteSessionLog()
		return true
	case hitSessionLogsClearFavorite:
		state.askClearSessionLogs(false)
		return true
	case hitSessionLogsClearAll:
		state.askClearSessionLogs(true)
		return true
	case hitSessionLogsLoadMore:
		state.loadMoreSessionLogs()
		return true
	case hitSessionLogsClearFilters:
		state.clearSessionLogFilters()
		return true
	case hitSessionLogsDetailClose:
		state.closeSessionLogDetail()
		return true
	case hitSessionLogsLive:
		state.reopenSessionLogRecord(view.selected)
		return true
	case hitSessionLogsCopy:
		state.copySessionLogMetadata(view.selected)
		return true
	case hitSessionLogsLocate:
		state.locateSessionLogHost(view.selected)
		return true
	}
	if column, ok := id.In(hitSessionLogsSortHeader); ok {
		state.toggleSessionLogHeaderSort(column)
		return true
	}
	if index, ok := id.In(hitSessionLogsRowFavorite); ok {
		if index >= 0 && index < len(view.page) {
			state.toggleSessionLogFavoriteID(view.page[index].id)
			return true
		}
	}
	if view.filterMenu != sessionLogFilterNone {
		if index, ok := id.In(hitSessionLogsFilterList); ok {
			state.chooseSessionLogFilter(index)
			return true
		}
		// A press anywhere else belongs to the page again; a page menu closes
		// itself on a press away, and the declaration clears the filter.
		view.filterMenu = sessionLogFilterNone
		view.filterOpen = false
		return true
	}
	return false
}

// sessionLogsKey handles the page's keys: Escape, and Delete on a row or on the
// open record. The record list and the event list are page controls, so their
// arrows, page keys, Home and End are the page's, routed before this handler.
func (state *State) sessionLogsKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	view := &state.sessionLogs.view
	if state.modal != modalNone || state.activeSection != sectionSessionLogs || event.Repeat {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEscape {
		switch {
		case view.moreMenu:
			view.moreMenu = false
			return gfx.EventResult{Handled: true, Repaint: true, Focus: hitSessionLogsMore, FocusVisible: true}, true
		case event.Target == hitSessionLogsSearch && view.search.RuneCount() > 0:
			state.clearSessionLogSearch()
			return gfx.EventResult{Handled: true, Repaint: true}, true
		case view.detailOpen:
			state.closeSessionLogDetail()
			return gfx.EventResult{Handled: true, Repaint: true, Focus: view.focusPending, FocusVisible: true}, true
		}
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyDelete {
		if index, ok := event.Target.In(view.rowScope); ok && view.rowScope != (gfx.Scope(0)) &&
			index >= 0 && index < len(view.items) && view.items[index].row >= 0 {
			state.selectSessionLogRecord(view.page[view.items[index].row].id)
			state.askDeleteSessionLog()
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		if view.selected != "" {
			state.askDeleteSessionLog()
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	}
	return gfx.EventResult{}, false
}
