package workbench

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/i18n"
	"motty/internal/sessionlog"
	"motty/internal/sshconn"
)

// Session logs are the user's view of "what did I connect to, and how did it
// end": one bounded, local row per logical session, fed from the same lifecycle
// transitions the terminal already follows. The database is a separate private
// SQLite file, never part of the configuration, the command history, a backup or
// a sync; see internal/sessionlog for what may and may not be stored.
//
// Two rules keep the rest of the workbench honest:
//   - no database call happens on the paint path, and
//   - a State built by hand (most tests) simply has no store, and every helper
//     here stays nil-safe.

// sessionLogs is the metadata journal owned by one window.
type sessionLogs struct {
	calendar    sessionLogCalendar
	preferences *savedSessionLogsView
	store       *sessionlog.Store
	queue       *sessionlog.Queue
	// runID names this process run. It is the same for every window of the
	// process, so one window's startup recovery never marks another window's
	// live row interrupted.
	runID   string
	enabled bool
	openErr error
	view    sessionLogsView
	// settings is what Settings > Security shows about the journal. It is
	// refreshed off the paint path, like the page's own view.
	settings sessionLogsSettings
	// results carries query completions from the worker goroutines back to the
	// window thread; done is closed when the window stops caring.
	results chan sessionLogsResult
	done    chan struct{}
	// requestGeneration drops stale query results; the view keeps the version it
	// asked for and ignores everything older.
	requestGeneration uint64
}

// sessionLogsView is what the page paints: the current query, the rows it
// produced, and the detail selection. Paint only reads it; workers rebuild it.
//
// The page part below is the view's prepared projection: page, countText and the
// empty state carry every string the table and the details show, and the
// filters carry the choices the query is built from. See session_logs_paint.go
// for the projection and session_logs_input.go for the interactions.
type sessionLogsView struct {
	query      sessionlog.Query
	rows       []sessionlog.Record
	next       sessionlog.Cursor
	more       bool
	loading    bool
	err        string
	matched    int64
	total      int64
	items      []sessionLogListItem
	groupCount int
	clockStamp sessionLogClockStamp

	unavailable bool

	selected        string
	favoritePending string
	events          []sessionlog.Event
	eventsFor       string
	eventsMore      bool
	eventsErr       string
	eventsLoad      bool
	eventGen        uint64
	selectedGen     uint64
	selectedLive    bool

	// The page.
	search         text.Editor
	appliedSearch  string
	page           []sessionLogRow
	loaded         bool
	dirty          bool
	loadingMore    bool
	countText      string
	statusText     string
	statusDanger   bool
	emptyTitle     string
	emptyText      string
	emptyAction    string
	emptyIcon      uint8
	emptyDanger    bool
	detailOpen     bool
	detail         sessionLogDetail
	synced         bool
	syncedLanguage i18n.Language

	// The filters. query.Status, query.Protocol, query.Favorite, query.Since and
	// query.Until are the applied form of these choices.
	timeRange       sessionLogTimeRange
	statusFilter    sessionlog.Status
	protocolFilter  sessionlog.Protocol
	favoriteOnly    bool
	customStartDate string
	customEndDate   string
	customRangeHint string
	filterMenu      sessionLogFilterMenu
	moreMenu        bool
	moreBounds      gfx.Rect
	// filterBounds are the three filter triggers' places, recorded while the page
	// paints; the popup opens under the one whose menu is open.
	filterBounds [5]gfx.Rect

	// Scrolling and the geometry the wheel is aimed at, recorded while painting.
	scrollRows   float32
	scrollEvents float32
	dragging     sessionLogDragging
	dragGrab     float32
	tableBody    gfx.Rect
	eventsBody   gfx.Rect
	// rowCells holds one row's cell rectangles so painting a row does not
	// allocate: the table appends its geometry to this reused slice.
	rowCells []gfx.Rect
	// focusPending is a focus move requested by an event handler and delivered
	// by the next paint, which is the only place a focus request may be made.
	focusPending gfx.ID
}

// sessionLogsResult is one background query completion. Exactly one of page and
// events is set.
type sessionLogsResult struct {
	generation uint64
	page       sessionlog.Page
	// appendPage makes the page continue with these records instead of
	// replacing what the view holds.
	appendPage bool
	events     []sessionlog.Event
	eventsFor  string
	eventsGen  uint64
	err        error
}

// sessionLogProcessRunID is the process-wide run identity used when Options does
// not inject one. A failure to draw random bytes still yields a distinct value.
var sessionLogProcessRunID = sync.OnceValue(func() string {
	if id, err := sessionlog.NewID(); err == nil {
		return id
	}
	return fmt.Sprintf("run-%d", time.Now().UnixNano())
})

// initSessionLogs opens this window's journal, recovers rows a previous process
// run left open, and starts the ordered writer. A database that cannot be opened
// is remembered and reported, never fatal for connections.
func (state *State) initSessionLogs() {
	defer state.prepareSessionLogsSettings()
	state.sessionLogs.enabled = !state.sessionLogsOff
	state.sessionLogs.runID = state.options.SessionLogRunID
	if state.sessionLogs.runID == "" {
		state.sessionLogs.runID = sessionLogProcessRunID()
	}
	state.sessionLogs.results = make(chan sessionLogsResult, 8)
	state.sessionLogs.done = make(chan struct{})
	store, err := sessionlog.OpenWithClock(state.sessionLogPath(), state.options.WallNow)
	if err != nil {
		state.sessionLogs.openErr = err
		state.sessionLogs.view.unavailable = true
		state.reportSessionLogProblem("open", err)
		state.prepareSessionLogsView()
		return
	}
	state.sessionLogs.store = store
	state.sessionLogs.queue = sessionlog.NewQueue(store)
	if err := store.Recover(state.sessionLogs.runID, state.wallNow()); err != nil {
		state.reportSessionLogProblem("recover", err)
	}
	state.prepareSessionLogsView()
}

// sessionLogPath is the private journal, beside the saved state and apart from
// every other database.
func (state *State) sessionLogPath() string {
	return filepath.Join(filepath.Dir(state.options.StatePath), "session-logs.db")
}

// sessionLogsRecording reports whether new lifecycle rows may be written.
func (state *State) sessionLogsRecording() bool {
	return state.sessionLogs.enabled && state.sessionLogs.store != nil && state.sessionLogs.queue != nil
}

// wallNow is the day-and-time clock, nil-safe for a State built by hand.
func (state *State) wallNow() time.Time {
	if state.options.WallNow == nil {
		return time.Now()
	}
	return state.options.WallNow()
}

// reportSessionLogProblem writes a store failure to the diagnostic log. The
// error text is a database or file-system message, never terminal content or an
// authentication answer; the page shows the user that records are unavailable.
func (state *State) reportSessionLogProblem(stage string, err error) {
	if err == nil || state.options.Log == nil {
		return
	}
	fmt.Fprintf(state.options.Log, "session log %s: %v\n", stage, err)
}

// submitSessionLog hands one write to the ordered writer. The acknowledgement is
// drained on a goroutine; the connection path never waits for SQLite.
func (state *State) submitSessionLog(operation sessionlog.Operation) {
	queue := state.sessionLogs.queue
	if queue == nil {
		return
	}
	ack := queue.Submit(operation)
	invalidate := state.options.Invalidate
	go func() {
		err := <-ack
		state.aiPostWithInvalidator(func(state *State) {
			state.sessionLogWriteSettled(operation, err)
		}, invalidate)
	}()
}

// sessionLogWriteSettled turns the writer's answer into the page's state: the
// status line, the row the write changed, and the repaint that shows them. It
// runs from drainAI, and Wake runs pumpSessionLogs before drainAI, so the refresh
// it asks for is a repaint of its own: without it a visible page would keep
// showing the old row until some other event happened to wake the window.
func (state *State) sessionLogWriteSettled(operation sessionlog.Operation, err error) {
	view := &state.sessionLogs.view
	if operation.Kind == sessionlog.OpSetFavorite && view.favoritePending == operation.ID {
		view.favoritePending = ""
	}
	// A record deleted between the click and the write is gone, not broken: the
	// page's next query shows what the store has, so it is not a write failure.
	if err != nil && !errors.Is(err, sessionlog.ErrNotFound) {
		state.reportSessionLogProblem("write", err)
		view.statusText = state.messages.SessionLogs.WriteFailed
		view.statusDanger = true
		if operation.Kind == sessionlog.OpDeleteSelection && state.activeSection != sectionSessionLogs {
			state.showActionError(state.messages.SessionLogs.Title, state.messages.SessionLogs.WriteFailed)
		}
	} else {
		switch operation.Kind {
		case sessionlog.OpSetFavorite:
			view.statusDanger = false
			if operation.Favorite {
				view.statusText = state.messages.SessionLogs.FavoritedStatus
			} else {
				view.statusText = state.messages.SessionLogs.UnfavoritedStatus
			}
		case sessionlog.OpDelete:
			view.statusDanger = false
			view.statusText = state.messages.SessionLogs.Deleted
			if view.selected == operation.ID {
				index := -1
				for i := range view.rows {
					if view.rows[i].ID == operation.ID {
						index = i
						break
					}
				}
				view.selected = ""
				view.focusPending = hitSessionLogsSearch
				if index >= 0 && len(view.rows) > 1 {
					next := min(index, len(view.rows)-2)
					if next == index {
						view.selected = view.rows[index+1].ID
					} else {
						view.selected = view.rows[next].ID
					}
					view.focusPending = hitSessionLogsRowList.At(next)
				}
				view.detailOpen = false
				view.events = nil
				view.eventsFor = ""
				view.eventsLoad = false
			}
		case sessionlog.OpDeleteSelection:
			view.statusDanger = false
			view.statusText = state.messages.SessionLogs.ClearedUnfavorited
			if operation.IncludeFavorites {
				view.statusText = state.messages.SessionLogs.ClearedAll
			}
		}
	}
	state.sessionLogs.settings.requested = false
	if err == nil && state.options.Group != nil {
		state.options.Group.invalidateSessionLogViews()
	}
	if state.activeSection == sectionSessionLogs {
		state.sessionLogs.view.dirty = true
		state.invalidateWindow()
	}
}

// startSessionLog opens a row for a user-dialed session. A manual redial is a
// new logical session, so any row the tab still owns is finalized first.
func (state *State) startSessionLog(session *Session, host Host) {
	if session == nil {
		return
	}
	state.finishSessionLog(session, sessionlog.StatusEnded, sessionlog.ReasonUserClosed)
	if !state.sessionLogsRecording() {
		return
	}
	id, err := sessionlog.NewID()
	if err != nil {
		state.reportSessionLogProblem("id", err)
		return
	}
	record := sessionlog.Record{
		ID:        id,
		RunID:     state.sessionLogs.runID,
		Host:      sessionLogHostName(host),
		Address:   sessionLogText(host.Address),
		Account:   sessionLogText(state.login(host)),
		Protocol:  sessionLogProtocol(host),
		StartedAt: state.wallNow().UTC(),
		Port:      sessionLogPort(host),
	}
	switch {
	case host.ID == localHostID:
		record.TargetKind = sessionlog.TargetLocal
		record.Port = 0
		if host.localShell != "" {
			record.ShellName = sessionLogText(filepath.Base(host.localShell))
		}
	case session.shared || session.containerCommand != "":
		record.TargetKind = sessionlog.TargetShared
	default:
		if saved, ok := state.hostByID(host.ID); ok && !host.tunnel {
			record.CatalogID = saved.CatalogID
			record.TargetKind = sessionlog.TargetSavedHost
		}
	}
	if record.Protocol == sessionlog.ProtocolSerial {
		record.Port = 0
	}
	session.logRecordID = id
	session.logFinished = false
	session.logStatus = sessionlog.StatusConnecting
	session.logLastEvent = sessionlog.EventStarted
	state.submitSessionLog(sessionlog.Operation{Kind: sessionlog.OpStart, Record: record, Wait: session.logBarrier})
}

// startSharedSessionLog records a tab that borrows an already connected
// transport (a container shell): the transport is up when the tab appears.
func (state *State) startSharedSessionLog(session *Session, host Host) {
	state.startSessionLog(session, host)
	if session.logRecordID == "" {
		return
	}
	state.submitSessionLogEvent(session, sessionlog.EventConnected, 0)
	state.submitSessionLogEvent(session, sessionlog.EventShellOpened, 0)
	session.logStatus = sessionlog.StatusActive
}

// submitSessionLogEvent appends one typed event to the session's timeline.
func (state *State) submitSessionLogEvent(session *Session, kind sessionlog.EventKind, attempt int) {
	if session == nil || session.logRecordID == "" || session.logFinished || !state.sessionLogsRecording() {
		return
	}
	if (kind == sessionlog.EventHostKeyPrompt || kind == sessionlog.EventAuthenticating) && session.logLastEvent == kind {
		return
	}
	session.logLastEvent = kind
	state.submitSessionLog(sessionlog.Operation{
		Kind: sessionlog.OpAppendEvent,
		Wait: session.logBarrier,
		Event: sessionlog.Event{
			RecordID: session.logRecordID,
			Kind:     kind,
			At:       state.wallNow().UTC(),
			Attempt:  attempt,
		},
	})
}

// finishSessionLog writes the terminal status once. Repeated and late calls do
// nothing, so every close path may call it without coordinating.
func (state *State) finishSessionLog(session *Session, status sessionlog.Status, reason sessionlog.Reason) {
	if session == nil || session.logRecordID == "" || session.logFinished {
		return
	}
	session.logFinished = true
	session.logStatus = status
	if !state.sessionLogsRecording() {
		return
	}
	state.submitSessionLog(sessionlog.Operation{
		Kind:    sessionlog.OpFinish,
		Wait:    session.logBarrier,
		ID:      session.logRecordID,
		Status:  status,
		EndedAt: state.wallNow().UTC(),
		Reason:  reason,
	})
}

// journalConnectionEvent mirrors the in-memory connection story into the typed
// timeline. The story keeps its existing contract; this only adds a known event
// kind, never the free-text detail.
func (state *State) journalConnectionEvent(session *Session, kind connectionEventKind) {
	if session == nil || session.logRecordID == "" || session.logFinished {
		return
	}
	switch kind {
	case connectionEventHostKeyPrompt:
		state.submitSessionLogEvent(session, sessionlog.EventHostKeyPrompt, 0)
	case connectionEventAuthenticating:
		state.submitSessionLogEvent(session, sessionlog.EventAuthenticating, 0)
	case connectionEventConnected:
		// journalConnectionOutcome records it once the endpoint role is known.
	case connectionEventClosed:
		state.submitSessionLogEvent(session, sessionlog.EventDisconnected, 0)
	case connectionEventFailed:
		state.submitSessionLogEvent(session, sessionlog.EventFailed, 0)
	case connectionEventReconnectWaiting:
		session.logStatus = sessionlog.StatusReconnecting
		state.submitSessionLogEvent(session, sessionlog.EventReconnectScheduled, session.reconnectAttempt)
	case connectionEventReconnectStarted:
		session.logStatus = sessionlog.StatusReconnecting
		state.submitSessionLogEvent(session, sessionlog.EventReconnectStarted, session.reconnectAttempt)
	case connectionEventReconnectStopped:
		state.submitSessionLogEvent(session, sessionlog.EventReconnectExhausted, session.reconnectAttempt)
	case connectionEventReconnectCanceled:
		state.submitSessionLogEvent(session, sessionlog.EventReconnectCanceled, session.reconnectAttempt)
		state.finishSessionLog(session, sessionlog.StatusFailed, sessionlog.ReasonCanceled)
	}
}

// journalConnectionOutcome decides, after the reconnect policy has run, whether
// a status is final. A transient close with a retry pending keeps the row alive
// as reconnecting; everything else ends it truthfully.
func (state *State) journalConnectionOutcome(session *Session, status sshconn.Status) {
	if session == nil || session.logRecordID == "" {
		return
	}
	switch status.Phase {
	case sshconn.PhaseConnected:
		if status.EndpointRole == sshconn.EndpointProxyJump || session.logFinished {
			return
		}
		if session.logStatus == sessionlog.StatusActive {
			return
		}
		state.submitSessionLogEvent(session, sessionlog.EventConnected, 0)
		session.logStatus = sessionlog.StatusActive
	case sshconn.PhaseClosed, sshconn.PhaseFailed:
		if session.logFinished {
			return
		}
		if session.reconnectPending {
			session.logStatus = sessionlog.StatusReconnecting
			return
		}
		outcome, reason := sessionLogOutcome(status)
		if session.disconnected {
			outcome, reason = sessionlog.StatusEnded, sessionlog.ReasonUserClosed
		}
		state.finishSessionLog(session, outcome, reason)
	}
}

// sessionLogOutcome maps a terminal connection status to a stored status and a
// known reason code. It never returns the connection's error text.
func sessionLogOutcome(status sshconn.Status) (sessionlog.Status, sessionlog.Reason) {
	switch {
	case status.Err == nil:
		return sessionlog.StatusEnded, sessionlog.ReasonNone
	case errors.Is(status.Err, sshconn.ErrAuthFailed):
		return sessionlog.StatusFailed, sessionlog.ReasonAuthFailed
	case errors.Is(status.Err, sshconn.ErrHostKeyRejected):
		return sessionlog.StatusFailed, sessionlog.ReasonHostKeyRejected
	case errors.Is(status.Err, sshconn.ErrCanceled):
		return sessionlog.StatusEnded, sessionlog.ReasonCanceled
	case errors.Is(status.Err, sshconn.ErrKeepAliveTimeout):
		return sessionlog.StatusFailed, sessionlog.ReasonTimeout
	case errors.Is(status.Err, sshconn.ErrDisconnected):
		return sessionlog.StatusFailed, sessionlog.ReasonNetworkLost
	case errors.Is(status.Err, sshconn.ErrHostKeyChanged):
		return sessionlog.StatusFailed, sessionlog.ReasonConnectFailed
	default:
		return sessionlog.StatusFailed, sessionlog.ReasonConnectFailed
	}
}

// setSessionLogsEnabled turns metadata capture on or off. Disabling never
// touches a live connection: rows of sessions that are still open are closed as
// "tracking stopped", which says the capture ended, not the session. Enabling
// again only records sessions opened afterwards.
func (state *State) setSessionLogsEnabled(on bool) {
	if state.sessionLogs.enabled == on {
		return
	}
	if !on {
		// Close the open rows while the writer still accepts them, then stop
		// taking new ones.
		for index := range state.sessions {
			session := &state.sessions[index]
			if session.logRecordID == "" || session.logFinished {
				continue
			}
			state.submitSessionLogEvent(session, sessionlog.EventTrackingStopped, 0)
			state.finishSessionLog(session, sessionlog.StatusTrackingStopped, sessionlog.ReasonTrackingOff)
		}
	}
	state.sessionLogs.enabled = on
}

// closeSessionLogs flushes pending rows and closes the journal. Call it after
// the window's sessions are finalized: the queue drains before its store closes.
func (state *State) closeSessionLogs() {
	state.stopSessionLogCalendar()
	if state.sessionLogs.done != nil {
		select {
		case <-state.sessionLogs.done:
		default:
			close(state.sessionLogs.done)
		}
	}
	if state.sessionLogs.queue != nil {
		if err := state.sessionLogs.queue.Close(); err != nil {
			state.reportSessionLogProblem("flush", err)
		}
		state.sessionLogs.queue = nil
	}
	if state.sessionLogs.store != nil {
		if err := state.sessionLogs.store.Close(); err != nil {
			state.reportSessionLogProblem("close", err)
		}
		state.sessionLogs.store = nil
	}
}

// refreshSessionLogs asks a worker for the page the view currently describes.
// Late answers are dropped by generation, so typing or filtering never lets an
// older result replace a newer one.
func (state *State) refreshSessionLogs() {
	state.refreshSessionLogsAt(true)
}

// refreshSessionLogsKeepingScroll refreshes a result whose order the change
// cannot alter, such as a favourite or a delete, so the list does not jump back
// to the top under the reader.
func (state *State) refreshSessionLogsKeepingScroll() {
	state.refreshSessionLogsAt(false)
}

func (state *State) refreshSessionLogsAt(resetScroll bool) {
	view := &state.sessionLogs.view
	if resetScroll {
		view.scrollRows = 0
	}
	view.dirty = false
	state.sessionLogs.requestGeneration++
	view.loading = true
	view.loadingMore = false
	view.err = ""
	// A new query starts at the newest record again; a page cursor belongs to
	// the query that produced it.
	view.query.Cursor = sessionlog.Cursor{}
	view.next = sessionlog.Cursor{}
	view.more = false
	store := state.sessionLogs.store
	if store == nil {
		view.unavailable = true
		view.rows = nil
		view.page = view.page[:0]
		view.loading = false
		view.loaded = true
		state.prepareSessionLogsView()
		return
	}
	generation := state.sessionLogs.requestGeneration
	query := view.query
	invalidate := state.options.Invalidate
	go func() {
		page, err := store.List(query)
		state.postSessionLogsResult(sessionLogsResult{generation: generation, page: page, err: err}, invalidate)
	}()
	state.prepareSessionLogsView()
}

// loadMoreSessionLogs continues the page with the next key cursor. The keyset
// cursor means a row inserted while the page is open cannot shift the rest.
func (state *State) loadMoreSessionLogs() {
	view := &state.sessionLogs.view
	store := state.sessionLogs.store
	if store == nil || view.loading || view.loadingMore || !view.more {
		return
	}
	state.sessionLogs.requestGeneration++
	generation := state.sessionLogs.requestGeneration
	view.loadingMore = true
	view.err = ""
	query := view.query
	query.Cursor = view.next
	invalidate := state.options.Invalidate
	go func() {
		page, err := store.List(query)
		state.postSessionLogsResult(sessionLogsResult{generation: generation, page: page, err: err, appendPage: true}, invalidate)
	}()
	state.prepareSessionLogsView()
}

// loadSessionLogEvents asks a worker for the selected record's timeline.
func (state *State) loadSessionLogEvents(id string) {
	view := &state.sessionLogs.view
	store := state.sessionLogs.store
	view.selected = id
	view.events = nil
	view.eventsMore = false
	view.eventsErr = ""
	view.eventsLoad = store != nil
	view.eventGen++
	if store == nil || id == "" {
		view.eventsLoad = false
		return
	}
	generation := view.eventGen
	invalidate := state.options.Invalidate
	go func() {
		events, err := store.Events(id)
		state.postSessionLogsResult(sessionLogsResult{eventsFor: id, eventsGen: generation, events: events, err: err}, invalidate)
	}()
}

// postSessionLogsResult hands a worker's answer to the window thread.
func (state *State) postSessionLogsResult(result sessionLogsResult, invalidate func()) {
	select {
	case <-state.sessionLogs.done:
		return
	case state.sessionLogs.results <- result:
	}
	if invalidate != nil {
		invalidate()
	}
}

// pumpSessionLogs runs completed queries. Wake calls it; Paint never does. The
// dirty-check that starts a query runs here too, so typing in the native search
// box becomes a store query before the repaint that shows its result.
func (state *State) pumpSessionLogs() {
	state.pumpSessionLogsSettings()
	state.syncSessionLogs()
	if state.sessionLogs.results == nil {
		return
	}
	for {
		select {
		case result := <-state.sessionLogs.results:
			state.applySessionLogsResult(result)
		default:
			return
		}
	}
}

func (state *State) applySessionLogsResult(result sessionLogsResult) {
	view := &state.sessionLogs.view
	if result.eventsFor != "" || result.eventsGen != 0 {
		if result.eventsGen != view.eventGen || result.eventsFor != view.selected {
			return
		}
		view.eventsLoad = false
		view.eventsFor = result.eventsFor
		view.events = result.events
		view.eventsMore = false
		if record, ok := state.sessionLogRecord(view.selected); ok {
			view.eventsMore = record.Truncated
		}
		if result.err != nil {
			view.eventsErr = "unavailable"
			state.reportSessionLogProblem("events", result.err)
		}
		state.prepareSessionLogDetail()
		return
	}
	if result.generation != state.sessionLogs.requestGeneration {
		return
	}
	view.loading = false
	view.loadingMore = false
	view.loaded = true
	if result.err != nil {
		view.err = "unavailable"
		if !result.appendPage {
			view.rows = nil
			view.more = false
		}
		state.reportSessionLogProblem("list", result.err)
		state.prepareSessionLogsView()
		return
	}
	if result.appendPage {
		view.rows = append(view.rows, result.page.Records...)
	} else {
		view.rows = result.page.Records
	}
	view.next = result.page.Next
	view.more = result.page.More
	view.matched, view.total = result.page.Matched, result.page.Total
	view.unavailable = false
	view.err = ""
	state.prepareSessionLogsView()
	if view.detailOpen && view.selected != "" {
		state.loadSessionLogEvents(view.selected)
		state.prepareSessionLogDetail()
	}
}

func (group *Group) invalidateSessionLogViews() {
	group.mu.Lock()
	members := append([]*State(nil), group.members...)
	group.mu.Unlock()
	for _, member := range members {
		member.sessionLogs.view.dirty = true
		member.sessionLogs.settings.requested = false
		member.invalidateWindow()
	}
}

// setSessionLogFavorite marks one record, without touching any other history.
func (state *State) setSessionLogFavorite(id string, on bool) {
	if state.sessionLogs.queue == nil {
		return
	}
	state.submitSessionLog(sessionlog.Operation{Kind: sessionlog.OpSetFavorite, ID: id, Favorite: on})
}

// deleteSessionLog removes one record and its timeline.
func (state *State) deleteSessionLog(id string) {
	if state.sessionLogs.queue == nil {
		return
	}
	state.submitSessionLog(sessionlog.Operation{Kind: sessionlog.OpDelete, ID: id})
}

// clearSessionLogs removes this journal only: command history, manual terminal
// recordings and the diagnostic log are separate files and stay untouched.
func (state *State) clearSessionLogs(includeFavorites bool) {
	if state.sessionLogs.queue == nil {
		return
	}
	state.submitSessionLog(sessionlog.Operation{Kind: sessionlog.OpClear, IncludeFavorites: includeFavorites})
}

// sessionLogRecord finds a row already loaded into the view.
func (state *State) sessionLogRecord(id string) (sessionlog.Record, bool) {
	for _, record := range state.sessionLogs.view.rows {
		if record.ID == id {
			return record, true
		}
	}
	return sessionlog.Record{}, false
}

// sessionForLogRecord resolves a still-open tab by its stable record ID. A
// closed or moved tab whose integer session ID was reused is never matched.
func (state *State) sessionForLogRecord(id string) *Session {
	if id == "" {
		return nil
	}
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.logRecordID == id && (!session.logFinished || session.logStatus == sessionlog.StatusTrackingStopped && session.phase == sshconn.PhaseConnected) {
			return &state.sessions[index]
		}
	}
	return nil
}

func (state *State) sessionLogOwner(id string) (*State, *Session) {
	if session := state.sessionForLogRecord(id); session != nil {
		return state, session
	}
	if group := state.options.Group; group != nil {
		group.mu.Lock()
		defer group.mu.Unlock()
		for _, member := range group.members {
			if member != state {
				if session := member.sessionForLogRecord(id); session != nil {
					return member, session
				}
			}
		}
	}
	return nil, nil
}

// sessionLogHostName is the bounded display name of a host: its label, or the
// address when it has none.
func sessionLogHostName(host Host) string {
	switch {
	case strings.TrimSpace(host.Label) != "":
		return sessionLogText(host.Label)
	case strings.TrimSpace(host.Address) != "":
		return sessionLogText(host.Address)
	default:
		return "session"
	}
}

// sessionLogText makes a display snapshot safe to store: valid UTF-8 without
// control characters, which is also how terminal bytes and multi-line server
// text would look.
func sessionLogText(value string) string {
	value = strings.ToValidUTF8(value, "\uFFFD")
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
}

// sessionLogProtocol maps the workbench's host protocol to the stored enum.
func sessionLogProtocol(host Host) sessionlog.Protocol {
	if host.ID == localHostID {
		return sessionlog.ProtocolLocal
	}
	switch host.Protocol {
	case protocolTelnet:
		return sessionlog.ProtocolTelnet
	case protocolSerial:
		return sessionlog.ProtocolSerial
	case protocolVNC:
		return sessionlog.ProtocolVNC
	case protocolRDP:
		return sessionlog.ProtocolRDP
	default:
		return sessionlog.ProtocolSSH
	}
}

func sessionLogPort(host Host) uint16 {
	if host.ID == localHostID || host.Protocol == protocolSerial {
		return 0
	}
	if host.Port == 0 {
		return host.defaultPort()
	}
	return host.Port
}

// broadcastSessionLogsEnabled tells every window of the process about a capture
// switch. All windows run on the event-loop thread, so the peers are mutated
// directly, as the other cross-window settings do.
func (group *Group) broadcastSessionLogsEnabled(enabled bool) {
	group.mu.Lock()
	members := append([]*State(nil), group.members...)
	group.mu.Unlock()
	for _, member := range members {
		// The peers hold the same device-local choice. None of them saves here:
		// their next save writes the same value, and the switching window's save
		// is the one that lands now.
		member.sessionLogsOff = !enabled
		member.setSessionLogsEnabled(enabled)
		member.invalidateWindow()
	}
}

// invalidateWindow asks the runtime for one repaint when there is a window.
func (state *State) invalidateWindow() {
	if state.options.Invalidate != nil {
		state.options.Invalidate()
	}
}
