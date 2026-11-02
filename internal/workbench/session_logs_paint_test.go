package workbench

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/sessionlog"
)

// The session-log page's state and paint tests. The page is fed by a real
// private store in a test directory: only the query result path is observed, and
// the store is the same one production opens. A fixed wall clock keeps the time
// filters and the formatted times reproducible.

var sessionLogsTestNow = time.Date(2026, 10, 30, 18, 0, 0, 0, time.Local)

// sessionLogsTestOptions is the workbench with the journal's clock pinned.
func sessionLogsTestOptions(t testing.TB) Options {
	t.Helper()
	options := testOptions(t)
	options.WallNow = func() time.Time { return sessionLogsTestNow }
	return options
}

// sessionLogsInsert writes one finished record, with events, to the state's
// journal the way a live session would.
func sessionLogsInsert(t testing.TB, state *State, record sessionlog.Record, status sessionlog.Status, reason sessionlog.Reason, endedAt time.Time, events ...sessionlog.EventKind) {
	t.Helper()
	store := state.sessionLogs.store
	if store == nil {
		t.Fatal("the test state has no session journal")
	}
	record.Status = sessionlog.StatusConnecting
	if err := store.Start(record); err != nil {
		t.Fatal(err)
	}
	for _, kind := range events {
		if err := store.AppendEvent(sessionlog.Event{RecordID: record.ID, Kind: kind, At: record.StartedAt.Add(time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Finish(record.ID, status, endedAt, reason); err != nil {
		t.Fatal(err)
	}
}

// sessionLogsSeed is a small journal with one record per interesting outcome:
// newest first is log-00, and each row's start time is relative to the pinned
// clock.
func sessionLogsSeed(t testing.TB, state *State) {
	t.Helper()
	const run = "run-test"
	sessionLogsInsert(t, state, sessionlog.Record{
		ID: "log-00", RunID: run, Host: "web-01", Address: "10.0.0.1", Account: "alice",
		Protocol: sessionlog.ProtocolSSH, StartedAt: sessionLogsTestNow.Add(-1 * time.Hour),
	}, sessionlog.StatusEnded, sessionlog.ReasonUserClosed, sessionLogsTestNow.Add(-30*time.Minute),
		sessionlog.EventConnected, sessionlog.EventShellOpened, sessionlog.EventDisconnected)
	sessionLogsInsert(t, state, sessionlog.Record{
		ID: "log-01", RunID: run, Host: "db-01", Address: "10.0.0.2", Account: "bob",
		Protocol: sessionlog.ProtocolSSH, StartedAt: sessionLogsTestNow.Add(-3 * time.Hour),
	}, sessionlog.StatusFailed, sessionlog.ReasonAuthFailed, sessionLogsTestNow.Add(-175*time.Minute),
		sessionlog.EventFailed)
	sessionLogsInsert(t, state, sessionlog.Record{
		ID: "log-02", RunID: run, Host: "lab", Address: "192.168.1.10", Account: "ops",
		Protocol: sessionlog.ProtocolLocal, StartedAt: sessionLogsTestNow.Add(-30 * time.Hour),
	}, sessionlog.StatusEnded, sessionlog.ReasonUserClosed, sessionLogsTestNow.Add(-29*time.Hour),
		sessionlog.EventConnected, sessionlog.EventDisconnected)
	sessionLogsInsert(t, state, sessionlog.Record{
		ID: "log-03", RunID: run, Host: "old-box", Address: "10.0.0.9", Account: "ops",
		Protocol: sessionlog.ProtocolTelnet, StartedAt: sessionLogsTestNow.Add(-8 * 24 * time.Hour),
	}, sessionlog.StatusFailed, sessionlog.ReasonTimeout, sessionLogsTestNow.Add(-8*24*time.Hour+time.Minute),
		sessionlog.EventFailed)
	sessionLogsInsert(t, state, sessionlog.Record{
		ID: "log-04", RunID: run, Host: "archive", Address: "10.0.0.8", Account: "ops",
		Protocol: sessionlog.ProtocolSSH, StartedAt: sessionLogsTestNow.Add(-40 * 24 * time.Hour),
	}, sessionlog.StatusEnded, sessionlog.ReasonUserClosed, sessionLogsTestNow.Add(-40*24*time.Hour+time.Minute))
	sessionLogsInsert(t, state, sessionlog.Record{
		ID: "log-05", RunID: run, Host: "rack-02", Address: "10.0.0.7", Account: "ops",
		Protocol: sessionlog.ProtocolSerial, StartedAt: sessionLogsTestNow.Add(-2 * time.Hour),
	}, sessionlog.StatusInterrupted, sessionlog.ReasonProcessRestart, sessionLogsTestNow.Add(-2*time.Hour+time.Minute),
		sessionlog.EventReconnectScheduled, sessionlog.EventReconnectStarted)
}

// sessionLogsState is a workbench showing the page, with the seed journal when
// seed is true.
func sessionLogsState(t testing.TB, seed bool) *State {
	t.Helper()
	state := newTestState(t, sessionLogsTestOptions(t))
	if seed {
		sessionLogsSeed(t, state)
	}
	state.openSessionLogs()
	sessionLogsWait(t, state)
	return state
}

// sessionLogsWait runs the query completions until the page is idle.
func sessionLogsWait(t testing.TB, state *State) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		state.pumpSessionLogs()
		view := &state.sessionLogs.view
		if !view.loading && !view.loadingMore && !view.eventsLoad {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the session log page did not settle")
		}
		time.Sleep(time.Millisecond)
	}
}

func sessionLogsWaitFor(t testing.TB, state *State, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ready() && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	if !ready() {
		t.Fatal("session log write did not update the page")
	}
	sessionLogsWait(t, state)
}

// sessionLogsType types into the page's native search field and lets the wake
// path turn it into a query, as a window does.
func sessionLogsType(t testing.TB, state *State, text string) {
	t.Helper()
	editor := state.EditorFor(hitSessionLogsSearch)
	if editor == nil {
		t.Fatal("the page has no native search field")
	}
	app.TextEvent{Kind: app.TextInsert, Text: text}.Apply(editor)
	state.Wake()
	sessionLogsWait(t, state)
}

// sessionLogsSearch replaces the search box's text, the way selecting it and
// typing over it does.
func sessionLogsSearch(t testing.TB, state *State, value string) {
	t.Helper()
	editor := state.EditorFor(hitSessionLogsSearch)
	if editor == nil {
		t.Fatal("the page has no native search field")
	}
	*editor = text.Editor{}
	editor.Insert(value)
	state.Wake()
	sessionLogsWait(t, state)
}

// sessionLogLabels is the painted texts of one paint.
func sessionLogLabels(state *State, size ui.Size) []string {
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	return canvasTexts(&canvas)
}

func canvasTexts(canvas *ui.Canvas) []string {
	texts := make([]string, 0, len(canvas.Texts()))
	for _, text := range canvas.Texts() {
		texts = append(texts, text.Value)
	}
	return texts
}

// sessionLogHit finds a painted hit of the page.
func sessionLogHit(canvas *ui.Canvas, id ui.ID) (ui.Hit, bool) {
	for _, hit := range canvas.Hits() {
		if hit.ID == id {
			return hit, true
		}
	}
	return ui.Hit{}, false
}

// sessionLogRowIDs is the record IDs the painted page holds, in order.
func sessionLogRowIDs(state *State) []string {
	ids := make([]string, len(state.sessionLogs.view.page))
	for index := range state.sessionLogs.view.page {
		ids[index] = state.sessionLogs.view.page[index].id
	}
	return ids
}

func TestSessionLogsPageReloadsAfterLeavingAndReturning(t *testing.T) {
	state := sessionLogsState(t, false)
	if len(state.sessionLogs.view.rows) != 0 {
		t.Fatal("expected an initially empty log page")
	}
	state.Click(hitHosts)
	sessionLogsInsert(t, state, sessionlog.Record{
		ID: "new-session", RunID: "run-test", Host: "new-host", Protocol: sessionlog.ProtocolSSH,
		StartedAt: sessionLogsTestNow.Add(-time.Minute),
	}, sessionlog.StatusEnded, sessionlog.ReasonUserClosed, sessionLogsTestNow)
	state.Click(hitSessionLogs)
	sessionLogsWait(t, state)
	if ids := sessionLogRowIDs(state); len(ids) != 1 || ids[0] != "new-session" {
		t.Fatalf("reopened page kept an obsolete query: %v", ids)
	}
}

func TestSessionLogsVisiblePageRefreshesAfterLifecycleWrite(t *testing.T) {
	state := newTestState(t, sessionLogsTestOptions(t))
	state.openSessionLogs()
	sessionLogsWait(t, state)
	session := &Session{}
	state.startSessionLog(session, state.hosts[0])
	state.submitSessionLogEvent(session, sessionlog.EventConnected, 0)
	settleSessionLogs(t, state)
	deadline := time.Now().Add(3 * time.Second)
	for len(state.sessionLogs.view.rows) == 0 && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	sessionLogsWait(t, state)
	if ids := sessionLogRowIDs(state); len(ids) != 1 {
		t.Fatalf("a connection created while the page was open is absent: %v", ids)
	}
}

// A write settles in drainAI, after Wake has already run pumpSessionLogs, so it
// must ask for the repaint that shows its result: without it the page keeps the
// row the write changed until something else happens to wake the window.
func TestSessionLogWriteWakesThePageThatShowsIt(t *testing.T) {
	state := sessionLogsState(t, true)
	state.selectSessionLogRecord("log-00")
	sessionLogsWait(t, state)
	wakes := 0
	state.options.Invalidate = func() { wakes++ }
	state.sessionLogWriteSettled(sessionlog.Operation{Kind: sessionlog.OpSetFavorite, ID: "log-00", Favorite: true}, nil)
	if !state.sessionLogs.view.dirty {
		t.Fatal("the settled write did not mark the page for a refresh")
	}
	if wakes == 0 {
		t.Fatal("the settled write did not wake the page that shows it")
	}
}

// A record deleted between the click and the write is gone, not broken: the page
// refreshes to what the store has instead of claiming the write failed.
func TestSessionLogWriteOfADeletedRecordIsNotAFailure(t *testing.T) {
	state := sessionLogsState(t, true)
	state.selectSessionLogRecord("log-00")
	sessionLogsWait(t, state)
	state.sessionLogWriteSettled(sessionlog.Operation{Kind: sessionlog.OpDelete, ID: "log-00"}, sessionlog.ErrNotFound)
	if state.sessionLogs.view.statusDanger {
		t.Fatal("a record deleted before its write settled was reported as a write failure")
	}
	if state.sessionLogs.view.statusText == state.messages.SessionLogs.WriteFailed {
		t.Fatal("a deleted record still reads as a failed write")
	}
}

func TestSessionLogsFailedFavoriteDoesNotClaimSuccess(t *testing.T) {
	state := sessionLogsState(t, true)
	state.selectSessionLogRecord("log-00")
	sessionLogsWait(t, state)
	if err := state.sessionLogs.store.Close(); err != nil {
		t.Fatal(err)
	}
	state.toggleSessionLogFavorite()
	if state.sessionLogs.view.statusText == state.messages.SessionLogs.FavoritedStatus ||
		state.sessionLogs.view.rows[0].Favorite {
		t.Fatal("favorite was reported as saved before the database accepted it")
	}
	deadline := time.Now().Add(3 * time.Second)
	for !state.sessionLogs.view.statusDanger && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	if state.sessionLogs.view.statusText != state.messages.SessionLogs.WriteFailed {
		t.Fatalf("failed write has no visible error: %q", state.sessionLogs.view.statusText)
	}
}

func TestSessionLogsFavoriteIgnoresRepeatedClickUntilWriteSettles(t *testing.T) {
	state := sessionLogsState(t, true)
	state.selectSessionLogRecord("log-00")
	sessionLogsWait(t, state)
	state.toggleSessionLogFavorite()
	state.toggleSessionLogFavorite()
	if state.sessionLogs.view.favoritePending != "log-00" {
		t.Fatal("favorite write is not marked pending")
	}
	sessionLogsWaitFor(t, state, func() bool {
		return state.sessionLogs.view.favoritePending == "" &&
			state.sessionLogs.view.statusText == state.messages.SessionLogs.FavoritedStatus
	})
	if record := state.sessionLogSelectedRecord(); record == nil || !record.Favorite {
		t.Fatalf("the confirmed favorite is not reflected in the selected row: %+v", record)
	}
	state.toggleSessionLogFavorite()
	sessionLogsWaitFor(t, state, func() bool {
		return state.sessionLogs.view.statusText == state.messages.SessionLogs.UnfavoritedStatus
	})
}

func TestSessionLogsResultIsQueuedBeforeWindowInvalidation(t *testing.T) {
	state := &State{}
	state.sessionLogs.results = make(chan sessionLogsResult, 1)
	state.sessionLogs.done = make(chan struct{})
	state.options.Invalidate = func() {
		if len(state.sessionLogs.results) != 1 {
			t.Error("window was woken before its query result was available")
		}
	}
	state.postSessionLogsResult(sessionLogsResult{generation: 1}, state.options.Invalidate)
}

func TestSessionLogsFailedDeleteAndClearKeepVisibleRows(t *testing.T) {
	for _, action := range []string{"delete", "clear"} {
		t.Run(action, func(t *testing.T) {
			state := sessionLogsState(t, true)
			if err := state.sessionLogs.store.Close(); err != nil {
				t.Fatal(err)
			}
			before := sessionLogRowIDs(state)
			if action == "delete" {
				state.deleteSessionLogRecord(before[0])
			} else {
				state.clearSessionLogRecords(true)
			}
			if ids := sessionLogRowIDs(state); !slices.Equal(ids, before) {
				t.Fatalf("unconfirmed %s removed rows: %v, want %v", action, ids, before)
			}
			if state.sessionLogs.view.statusText == state.messages.SessionLogs.Deleted ||
				state.sessionLogs.view.statusText == state.messages.SessionLogs.ClearedAll {
				t.Fatal("unconfirmed mutation claimed success")
			}
		})
	}
}

func TestSessionLogsUnfavoriteLeavesFavoritesOnlyResults(t *testing.T) {
	state := sessionLogsState(t, true)
	if err := state.sessionLogs.store.SetFavorite("log-00", true); err != nil {
		t.Fatal(err)
	}
	state.sessionLogs.view.favoriteOnly = true
	state.applySessionLogFilters()
	sessionLogsWait(t, state)
	if ids := sessionLogRowIDs(state); len(ids) != 1 || ids[0] != "log-00" {
		t.Fatalf("favorite-only setup: %v", ids)
	}
	state.selectSessionLogRecord("log-00")
	sessionLogsWait(t, state)
	state.toggleSessionLogFavorite()
	sessionLogsWaitFor(t, state, func() bool {
		return len(state.sessionLogs.view.page) == 0 && state.sessionLogs.view.statusText == state.messages.SessionLogs.UnfavoritedStatus
	})
}

// The sidebar row sits right after Monitor, the palette reaches the page
// directly, and the workspace literal brings it back after a restart.
func TestSessionLogsNavigationAndRestore(t *testing.T) {
	options := freshOptions(t)
	options.WallNow = func() time.Time { return sessionLogsTestNow }
	first := newTestState(t, options)

	var canvas ui.Canvas
	first.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	row, ok := sessionLogHit(&canvas, hitSessionLogs)
	if !ok {
		t.Fatal("the sidebar has no session-log row")
	}
	if row.Label != first.messages.SessionLogs.Title || row.Role != ui.RoleMenuItem {
		t.Fatalf("the sidebar row reads %q with role %v", row.Label, row.Role)
	}
	monitor, ok := sessionLogHit(&canvas, hitDashboard)
	if !ok {
		t.Fatal("the sidebar has no monitor row")
	}
	if row.Bounds.Width != monitor.Bounds.Width || row.Bounds.Height != monitor.Bounds.Height {
		t.Fatalf("the session-log row %+v does not match the monitor row %+v", row.Bounds, monitor.Bounds)
	}
	if row.Bounds.Y <= monitor.Bounds.Y {
		t.Fatalf("the session-log row is not below Monitor: %+v vs %+v", row.Bounds, monitor.Bounds)
	}

	first.Click(hitSessionLogs)
	if first.activeSection != sectionSessionLogs {
		t.Fatalf("section after the sidebar click = %d", first.activeSection)
	}
	if first.workspaceName() != "session-logs" {
		t.Fatalf("workspace name = %q", first.workspaceName())
	}
	sessionLogsWait(t, first)
	if hit, ok := sessionLogHit(paintedCanvas(t, first), hitSessionLogs); !ok || !hit.Selected {
		t.Fatalf("the sidebar row is not selected on its own page: %+v", hit)
	}

	// The command palette reaches the same page.
	first.Click(hitHosts)
	first.Click(hitCommandPalette)
	app.TextEvent{Kind: app.TextInsert, Text: "session logs"}.Apply(first.EditorFor(hitCommandPaletteSearch))
	first.refreshCommandPalette()
	if first.commandPalette.visibleCount != 1 {
		t.Fatalf("palette commands for %q = %d", "session logs", first.commandPalette.visibleCount)
	}
	if hit, ok := sessionLogHit(paintedCanvas(t, first), hitCommandPaletteItems.At(0)); !ok || hit.Label != first.messages.SessionLogs.Title {
		t.Fatalf("palette entry = %+v, ok=%v", hit, ok)
	}
	first.Key(ui.KeyEvent{Target: hitCommandPaletteSearch, Key: ui.KeyEnter})
	if first.activeSection != sectionSessionLogs || first.modal != modalNone {
		t.Fatalf("palette execute left section=%d modal=%d", first.activeSection, first.modal)
	}

	// The saved workspace literal restores the page.
	first.save()
	saved, report, err := loadState(first.options.StatePath)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("load saved state: outcome=%d err=%v", report.outcome, err)
	}
	if saved.Workspace != "session-logs" {
		t.Fatalf("saved workspace = %q", saved.Workspace)
	}
	second := newTestState(t, options)
	if second.activeSection != sectionSessionLogs {
		t.Fatalf("restored section = %d", second.activeSection)
	}
}

// paintedCanvas paints the page at the standard size and returns its canvas.
func paintedCanvas(t *testing.T, state *State) *ui.Canvas {
	t.Helper()
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	return &canvas
}

// Search matches only the stored host, address and account, the filters narrow
// the store query, and an answer to an older query never replaces a newer one.
func TestSessionLogsSearchAndStaleResult(t *testing.T) {
	state := sessionLogsState(t, true)
	view := &state.sessionLogs.view
	if got := len(view.page); got != 6 {
		t.Fatalf("rows = %d, want the six seeded records", got)
	}
	newest := []string{"log-00", "log-05", "log-01", "log-02", "log-03", "log-04"}
	if got := sessionLogRowIDs(state); strings.Join(got, ",") != strings.Join(newest, ",") {
		t.Fatalf("row order = %v, want newest first %v", got, newest)
	}

	// Host name, address and account are all searchable; nothing else is.
	sessionLogsType(t, state, "db-01")
	if got := sessionLogRowIDs(state); len(got) != 1 || got[0] != "log-01" {
		t.Fatalf("search by host = %v", got)
	}
	sessionLogsSearch(t, state, "192.168.1.10")
	if got := sessionLogRowIDs(state); len(got) != 1 || got[0] != "log-02" {
		t.Fatalf("search by address = %v", got)
	}
	sessionLogsSearch(t, state, "bob")
	if got := sessionLogRowIDs(state); len(got) != 1 || got[0] != "log-01" {
		t.Fatalf("search by account = %v", got)
	}
	// The text never reaches the store as a protocol or a status word.
	if view.query.Search != "bob" {
		t.Fatalf("applied search = %q", view.query.Search)
	}
	state.clearSessionLogFilters()
	sessionLogsWait(t, state)

	// A late answer to an older query is dropped.
	store := state.sessionLogs.store
	state.sessionLogs.store = nil
	state.refreshSessionLogs()
	staleGeneration := state.sessionLogs.requestGeneration - 1
	state.applySessionLogsResult(sessionLogsResult{generation: staleGeneration, page: sessionlog.Page{
		Records: []sessionlog.Record{{ID: "stale", RunID: "run-test", Host: "stale", Protocol: sessionlog.ProtocolSSH, StartedAt: sessionLogsTestNow}},
	}})
	if len(view.rows) != 0 {
		t.Fatalf("a stale result was applied: %d rows", len(view.rows))
	}
	state.applySessionLogsResult(sessionLogsResult{generation: state.sessionLogs.requestGeneration, page: sessionlog.Page{
		Records: []sessionlog.Record{{ID: "fresh", RunID: "run-test", Host: "fresh", Protocol: sessionlog.ProtocolSSH, StartedAt: sessionLogsTestNow}},
	}})
	if len(view.rows) != 1 || view.rows[0].ID != "fresh" {
		t.Fatalf("the current result was not applied: %+v", view.rows)
	}
	state.sessionLogs.store = store
	state.refreshSessionLogs()
	sessionLogsWait(t, state)

	// Status and time-range filters reach the query.
	state.Click(hitSessionLogsStatus)
	if view.filterMenu != sessionLogFilterStatusMenu {
		t.Fatalf("status filter menu = %d", view.filterMenu)
	}
	canvas := paintedCanvas(t, state)
	if hit, ok := sessionLogHit(canvas, hitSessionLogsFilterList.At(5)); !ok || hit.Label != state.messages.SessionLogs.StatusFailed {
		t.Fatalf("status filter option = %+v, ok=%v", hit, ok)
	}
	state.Click(hitSessionLogsFilterList.At(5))
	sessionLogsWait(t, state)
	if view.query.Status == nil || *view.query.Status != sessionlog.StatusFailed {
		t.Fatalf("status query = %v", view.query.Status)
	}
	if got := sessionLogRowIDs(state); len(got) != 2 || got[0] != "log-01" || got[1] != "log-03" {
		t.Fatalf("failed rows = %v", got)
	}

	// A time range narrows by the record's start, computed from the wall clock.
	state.Click(hitSessionLogsStatus)
	state.Click(hitSessionLogsFilterList.At(0))
	sessionLogsWait(t, state)
	state.Click(hitSessionLogsTime)
	canvas = paintedCanvas(t, state)
	if hit, ok := sessionLogHit(canvas, hitSessionLogsFilterList.At(1)); !ok || hit.Label != state.messages.SessionLogs.TimeToday {
		t.Fatalf("time filter option = %+v, ok=%v", hit, ok)
	}
	state.Click(hitSessionLogsFilterList.At(1))
	sessionLogsWait(t, state)
	if got := sessionLogRowIDs(state); len(got) != 3 {
		t.Fatalf("today's rows = %v, want the three started after local midnight", got)
	}
	if view.query.Since.IsZero() {
		t.Fatal("the time filter did not reach the query")
	}

	// The favorites-only filter, and the favorite action, belong to the page.
	state.clearSessionLogFilters()
	sessionLogsWait(t, state)
	state.selectSessionLogRecord(view.page[0].id)
	sessionLogsWait(t, state)
	state.Click(hitSessionLogsFavorite)
	sessionLogsWait(t, state)
	sessionLogsAwaitStoredFavorite(t, state, view.page[0].id)
	state.Click(hitSessionLogsFavoriteFilter)
	sessionLogsWait(t, state)
	if got := sessionLogRowIDs(state); len(got) != 1 || got[0] != "log-00" {
		t.Fatalf("favorite rows = %v", got)
	}
	if hit, ok := sessionLogHit(paintedCanvas(t, state), hitSessionLogsFavoriteFilter); !ok || !hit.Selected {
		t.Fatalf("the favorite filter does not show its state: %+v", hit)
	}
}

// The page keeps four states apart: nothing recorded, nothing matching, an
// unreadable journal, and a load in progress. A record's detail shows the
// recorded events only, never a replay of bytes that do not exist.
func TestSessionLogsEmptyAndMetadataOnlyStates(t *testing.T) {
	empty := sessionLogsState(t, false)
	labels := sessionLogLabels(empty, ui.Size{Width: 1400, Height: 900})
	if !contains(labels, empty.messages.SessionLogs.Empty) {
		t.Fatalf("the empty page does not say %q: %v", empty.messages.SessionLogs.Empty, labels)
	}
	canvas := paintedCanvas(t, empty)
	if _, ok := sessionLogHit(canvas, hitSessionLogsClearFilters); ok {
		t.Fatal("the empty page offers a clear-filters action without filters")
	}
	if _, ok := sessionLogHit(canvas, hitSessionLogsLoadMore); ok {
		t.Fatal("the empty page offers to load more")
	}

	state := sessionLogsState(t, true)
	sessionLogsType(t, state, "no-such-host")
	labels = sessionLogLabels(state, ui.Size{Width: 1400, Height: 900})
	if !contains(labels, state.messages.SessionLogs.NoMatches) {
		t.Fatalf("the no-match page does not say %q: %v", state.messages.SessionLogs.NoMatches, labels)
	}
	canvas = paintedCanvas(t, state)
	clearAction, ok := sessionLogHit(canvas, hitSessionLogsClearFilters)
	if !ok || clearAction.Label != state.messages.SessionLogs.ClearFilters {
		t.Fatalf("the no-match page has no clear-filters action: %+v, ok=%v", clearAction, ok)
	}
	state.Click(hitSessionLogsClearFilters)
	sessionLogsWait(t, state)
	if len(state.sessionLogs.view.page) != 6 {
		t.Fatalf("clearing the filters left %d rows", len(state.sessionLogs.view.page))
	}

	// The journal could not be opened: the page says so instead of pretending
	// there are no sessions.
	unavailable := sessionLogsState(t, false)
	// Dropped open, the file could not be removed with the test's folder on Windows.
	if err := unavailable.sessionLogs.store.Close(); err != nil {
		t.Fatal(err)
	}
	unavailable.sessionLogs.store = nil
	unavailable.sessionLogs.view.unavailable = true
	unavailable.sessionLogs.view.loaded = true
	unavailable.prepareSessionLogsView()
	labels = sessionLogLabels(unavailable, ui.Size{Width: 1400, Height: 900})
	if !contains(labels, unavailable.messages.SessionLogs.Unavailable) {
		t.Fatalf("the unavailable page does not say %q: %v", unavailable.messages.SessionLogs.Unavailable, labels)
	}

	// A record with events shows them; nothing offers a replay.
	state.selectSessionLogRecord("log-00")
	sessionLogsWait(t, state)
	canvas = paintedCanvas(t, state)
	labels = canvasTexts(canvas)
	for _, want := range []string{
		state.messages.SessionLogs.Details, state.messages.SessionLogs.FieldStarted, state.messages.SessionLogs.FieldResult,
		state.messages.SessionLogs.ReasonUserClosed, state.messages.SessionLogs.EventConnected,
		state.messages.SessionLogs.EventShellOpened, state.messages.SessionLogs.EventDisconnected,
	} {
		if !contains(labels, want) {
			t.Errorf("the detail panel does not show %q", want)
		}
	}
	for _, unwanted := range []string{"Replay", "replay", "Recording file"} {
		if contains(labels, unwanted) {
			t.Errorf("the detail panel offers %q, which does not exist", unwanted)
		}
	}
	for _, hit := range canvas.Hits() {
		if hit.Label == "Replay" || hit.Label == "Play" {
			t.Errorf("a replay control is painted: %+v", hit)
		}
	}
	liveButton, ok := sessionLogHit(canvas, hitSessionLogsLive)
	if !ok {
		t.Fatal("the detail panel has no session-jump action")
	}
	if !liveButton.Disabled {
		t.Fatalf("the session jump is enabled with no open session: %+v", liveButton)
	}
	if liveButton.Label != state.messages.SessionLogs.SessionClosed {
		t.Fatalf("the disabled session jump says %q", liveButton.Label)
	}

	// A record with no events still shows its localized result code.
	state.selectSessionLogRecord("log-05")
	sessionLogsWait(t, state)
	labels = sessionLogLabels(state, ui.Size{Width: 1400, Height: 900})
	if !contains(labels, state.messages.SessionLogs.ReasonProcessRestart) {
		t.Fatalf("the result code is not localized: %v", labels)
	}
	if contains(labels, "ssh:") || contains(labels, "handshake") || contains(labels, "dial tcp") {
		t.Fatalf("a raw error string reached the page: %v", labels)
	}
	// A record with no events at all says so honestly.
	state.selectSessionLogRecord("log-04")
	sessionLogsWait(t, state)
	labels = sessionLogLabels(state, ui.Size{Width: 1400, Height: 900})
	if !contains(labels, state.messages.SessionLogs.NoMoreInformation) {
		t.Fatalf("a record without events does not say %q: %v", state.messages.SessionLogs.NoMoreInformation, labels)
	}
}

// Keyboard activation, the deletion confirmation's scope and focus after a
// delete, and a live-session jump that resolves the stable record ID only.
func TestSessionLogsKeyboardAndDeletionFocus(t *testing.T) {
	state := sessionLogsState(t, true)
	view := &state.sessionLogs.view

	// Enter on a row opens the detail panel; Escape closes it and returns the
	// keyboard to the row.
	if result := state.Key(ui.KeyEvent{Target: hitSessionLogsRowList.At(0), Key: ui.KeyEnter}); !result.Handled {
		t.Fatal("Enter on a row is not handled")
	}
	if view.selected != "log-00" || !view.detailOpen {
		t.Fatalf("Enter selected %q, detail open=%v", view.selected, view.detailOpen)
	}
	sessionLogsWait(t, state)
	result := state.Key(ui.KeyEvent{Target: hitSessionLogsDetailClose, Key: ui.KeyEscape})
	if !result.Handled || view.detailOpen {
		t.Fatalf("Escape did not close the detail panel: handled=%v open=%v", result.Handled, view.detailOpen)
	}
	if result.Focus != hitSessionLogsRowList.At(0) || !result.FocusVisible {
		t.Fatalf("Escape returned focus to %v", result.Focus)
	}

	// Escape in the search box empties it and brings the full list back.
	sessionLogsType(t, state, "db-01")
	if got := len(view.page); got != 1 {
		t.Fatalf("search rows = %d", got)
	}
	result = state.Key(ui.KeyEvent{Target: hitSessionLogsSearch, Key: ui.KeyEscape})
	if !result.Handled {
		t.Fatal("Escape in the search box is not handled")
	}
	sessionLogsWait(t, state)
	if view.search.RuneCount() != 0 || view.appliedSearch != "" || len(view.page) != 6 {
		t.Fatalf("Escape left search=%q rows=%d", view.search.String(), len(view.page))
	}

	// The delete confirmation names the record it deletes.
	state.selectSessionLogRecord("log-01")
	sessionLogsWait(t, state)
	state.Click(hitSessionLogsDelete)
	if state.modal != modalForm {
		t.Fatalf("delete modal = %d", state.modal)
	}
	if state.form.title != state.messages.SessionLogs.DeleteTitle {
		t.Fatalf("delete title = %q", state.form.title)
	}
	if !strings.Contains(state.form.description, "db-01") || !strings.Contains(state.form.description, "bob") {
		t.Fatalf("delete description does not name the record: %q", state.form.description)
	}
	state.Click(hitFormSubmit)
	sessionLogsWaitFor(t, state, func() bool { return len(view.page) == 5 && view.statusText == state.messages.SessionLogs.Deleted })
	if got := sessionLogRowIDs(state); len(got) != 5 {
		t.Fatalf("rows after the delete = %v", got)
	}
	for _, id := range sessionLogRowIDs(state) {
		if id == "log-01" {
			t.Fatal("the deleted record is still on the page")
		}
	}
	if view.statusText != state.messages.SessionLogs.Deleted {
		t.Fatalf("status after the delete = %q", view.statusText)
	}
	if view.selected == "" || view.selected == "log-01" {
		t.Fatalf("selection after the delete = %q", view.selected)
	}
	canvas := paintedCanvas(t, state)
	if got := canvas.FocusRequest(); got.Scope() != hitSessionLogsRowList {
		t.Fatalf("focus after the delete = %v, want a row of the page", got)
	}

	// The clear confirmations state what they clear.
	state.Click(hitSessionLogsClearFavorite)
	if state.modal != modalForm || state.form.description != state.messages.SessionLogs.ClearUnfavoritedPrompt {
		t.Fatalf("clear-unfavorited dialog = %q", state.form.description)
	}
	state.Click(hitFormCancel)
	if state.modal != modalNone {
		t.Fatal("canceling the clear leaves the dialog open")
	}
	state.Click(hitSessionLogsClearAll)
	if state.form.description != state.messages.SessionLogs.ClearAllPrompt {
		t.Fatalf("clear-all dialog = %q", state.form.description)
	}
	if !strings.Contains(strings.ToLower(state.form.description), strings.ToLower(state.messages.SessionLogs.Favorite)) {
		t.Fatal("the clear-all confirmation does not state that favorites go too")
	}
	state.Click(hitFormSubmit)
	sessionLogsWaitFor(t, state, func() bool { return len(view.page) == 0 && view.statusText == state.messages.SessionLogs.ClearedAll })
	if len(view.page) != 0 || view.statusText != state.messages.SessionLogs.ClearedAll {
		t.Fatalf("after clearing all: rows=%d status=%q", len(view.page), view.statusText)
	}

	// A live-session jump uses the stable record ID, never the integer session
	// ID: the tab that owns log-02 is session 7, and session 2 belongs to
	// another record.
	live := sessionLogsState(t, true)
	live.sessions = append(live.sessions,
		Session{ID: 2, Title: "other", logRecordID: "log-04"},
		Session{ID: 7, Title: "moved tab", logRecordID: "log-02"},
	)
	live.ensureWorkspaceDeck()
	live.selectSessionLogRecord("log-02")
	sessionLogsWait(t, live)
	live.Click(hitSessionLogsLive)
	if live.activeSection != sectionTerminal || live.activeSessionID != 7 {
		t.Fatalf("live jump went to section=%d session=%d, want session 7", live.activeSection, live.activeSessionID)
	}

	// A record whose session is gone does not jump at all.
	live.sessions[1].logFinished = true
	live.activeSection = sectionSessionLogs
	live.activeSessionID = 2
	live.selectSessionLogRecord("log-02")
	sessionLogsWait(t, live)
	live.Click(hitSessionLogsLive)
	if live.activeSection != sectionSessionLogs || live.activeSessionID != 2 {
		t.Fatalf("a closed record jumped to section=%d session=%d", live.activeSection, live.activeSessionID)
	}
	if live.sessionLogs.view.statusText != live.messages.SessionLogs.SessionClosed {
		t.Fatalf("status after the closed jump = %q", live.sessionLogs.view.statusText)
	}
}

// A page holds at most 50 records and continues with the keyset cursor, so a
// record inserted between pages cannot shift or duplicate a row.
func TestSessionLogsPagingLoadsMore(t *testing.T) {
	state := newTestState(t, sessionLogsTestOptions(t))
	for index := 0; index < 60; index++ {
		sessionLogsInsert(t, state, sessionlog.Record{
			ID: fmt.Sprintf("page-%02d", index), RunID: "run-test", Host: fmt.Sprintf("host-%02d", index),
			Address: "10.1.0.1", Account: "ops", Protocol: sessionlog.ProtocolSSH,
			StartedAt: sessionLogsTestNow.Add(-time.Duration(index) * time.Minute),
		}, sessionlog.StatusEnded, sessionlog.ReasonUserClosed, sessionLogsTestNow.Add(-time.Duration(index)*time.Minute+time.Second))
	}
	state.openSessionLogs()
	sessionLogsWait(t, state)
	view := &state.sessionLogs.view
	if len(view.page) != sessionLogPageSize {
		t.Fatalf("first page = %d rows, want %d", len(view.page), sessionLogPageSize)
	}
	if !view.more {
		t.Fatal("the page does not say another page exists")
	}
	canvas := paintedCanvas(t, state)
	if hit, ok := sessionLogHit(canvas, hitSessionLogsLoadMore); !ok || hit.Disabled {
		t.Fatalf("load-more action = %+v, ok=%v", hit, ok)
	}
	state.Click(hitSessionLogsLoadMore)
	sessionLogsWait(t, state)
	if len(view.page) != 60 {
		t.Fatalf("after loading more: %d rows", len(view.page))
	}
	if view.more {
		t.Fatal("the page still says another page exists")
	}
	seen := map[string]bool{}
	for _, id := range sessionLogRowIDs(state) {
		if seen[id] {
			t.Fatalf("row %q is listed twice", id)
		}
		seen[id] = true
	}
	if got := sessionLogRowIDs(state)[50]; got != "page-50" {
		t.Fatalf("the second page starts at %q", got)
	}
}

// Warm paint is inert and allocates nothing: no database call, no prepared
// string built, no frame scheduled.
func TestSessionLogsPaintDoesNotAllocateOnceWarm(t *testing.T) {
	state := sessionLogsState(t, true)
	state.selectSessionLogRecord("log-00")
	sessionLogsWait(t, state)
	canvas := paintedCanvas(t, state)
	size := ui.Size{Width: 1400, Height: 900}
	generation := state.sessionLogs.requestGeneration
	loading := state.sessionLogs.view.loading
	if allocations := testing.AllocsPerRun(50, func() {
		canvas.Reset()
		state.Paint(canvas, size)
	}); allocations != 0 {
		t.Fatalf("session-log paint allocations = %v", allocations)
	}
	if state.sessionLogs.requestGeneration != generation || state.sessionLogs.view.loading != loading {
		t.Fatal("paint asked the store for a page")
	}
	select {
	case result := <-state.sessionLogs.results:
		t.Fatalf("paint produced a store result: %+v", result)
	default:
	}
	if _, scheduled := canvas.NextFrame(); scheduled {
		t.Fatal("the idle session-log page scheduled another frame")
	}
}

// sessionLogsAwaitStoredFavorite waits for the ordered writer to persist a
// favorite mark: the page writes through a queue that never blocks a connection,
// so a filter that reads the store right after must wait for the row it changes.
func sessionLogsAwaitStoredFavorite(t testing.TB, state *State, id string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		page, err := state.sessionLogs.store.List(sessionlog.Query{})
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range page.Records {
			if record.ID == id && record.Favorite {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("favorite mark for %q was not written", id)
		}
		time.Sleep(time.Millisecond)
	}
}

// The page at both window sizes in both languages and themes: the same texts
// the state says, laid out inside the card.
func BenchmarkPaintSessionLogs(b *testing.B) {
	state := sessionLogsState(b, true)
	state.selectSessionLogRecord("log-00")
	sessionLogsWait(b, state)
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	b.ReportAllocs()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

// The page's texts follow the language: every localized status and protocol the
// seeded records use is painted in the chosen catalog.
func TestSessionLogsLanguageFollowsTheCatalog(t *testing.T) {
	state := sessionLogsState(t, true)
	state.setLanguage(i18n.Chinese)
	state.Wake()
	sessionLogsWait(t, state)
	labels := sessionLogLabels(state, ui.Size{Width: 1400, Height: 900})
	zh := i18n.For(i18n.Chinese).SessionLogs
	if !contains(labels, zh.Title) || !contains(labels, zh.ColumnTarget) {
		t.Fatalf("the Chinese page does not paint its own title and columns: %v", labels)
	}
	if contains(labels, i18n.For(i18n.English).SessionLogs.Title) {
		t.Fatalf("the English title is still painted: %v", labels)
	}
	if !contains(labels, zh.StatusInterrupted) || !contains(labels, zh.ProtocolSerial) {
		t.Fatalf("the row values are not localized: %v", labels)
	}
}
