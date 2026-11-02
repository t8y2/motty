package workbench

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"motty/internal/sessionlog"
)

func TestSessionLogCustomRangeCalendarBoundaries(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		date       string
		start, end time.Time
		hours      time.Duration
	}{
		{"2026-03-08", time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC), time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC), 23},
		{"2026-11-01", time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC), time.Date(2026, 11, 2, 5, 0, 0, 0, time.UTC), 25},
	} {
		start, end, err := sessionLogDateRange(tc.date, tc.date, location)
		if err != nil || !start.Equal(tc.start) || !end.Equal(tc.end) || end.Sub(start) != tc.hours*time.Hour {
			t.Fatalf("%s: [%v,%v) %v", tc.date, start, end, err)
		}
	}
	for _, dates := range [][2]string{{"2026-02-30", "2026-03-01"}, {"2026-11-02", "2026-11-01"}, {"2026-1-1", "2026-01-02"}, {"", "2026-01-01"}} {
		if _, _, err := sessionLogDateRange(dates[0], dates[1], location); err == nil {
			t.Fatalf("invalid calendar range accepted: %v", dates)
		}
	}
}

func TestSessionLogsCustomRangeFormAndSortControls(t *testing.T) {
	state := sessionLogsState(t, true)
	view := &state.sessionLogs.view
	view.filterMenu = sessionLogFilterSortMenu
	state.chooseSessionLogFilter(2)
	sessionLogsWait(t, state)
	if view.query.Sort != sessionlog.SortDurationDesc {
		t.Fatal("sort menu did not apply to the store query")
	}
	view.filterMenu = sessionLogFilterTimeMenu
	state.chooseSessionLogFilter(int(sessionLogTimeCustom))
	if state.modal != modalForm || view.timeRange == sessionLogTimeCustom {
		t.Fatal("opening a custom range changed the query before confirmation")
	}
	state.form.set(0, "2026-11-02")
	state.form.set(1, "2026-11-01")
	state.submitForm()
	if state.modal != modalForm || state.form.dialogError == "" {
		t.Fatal("inverted range did not keep validation visible")
	}
	state.form.set(0, "2026-10-30")
	state.form.set(1, "2026-10-30")
	state.submitForm()
	sessionLogsWait(t, state)
	if view.timeRange != sessionLogTimeCustom || len(view.page) != 3 || view.matched != 3 || view.total != 6 {
		t.Fatalf("custom day query: range=%d loaded=%d matched=%d total=%d", view.timeRange, len(view.page), view.matched, view.total)
	}
	state.clearSessionLogFilters()
	sessionLogsWait(t, state)
	if view.query.Sort != sessionlog.SortDurationDesc || len(view.page) != 6 {
		t.Fatal("clearing filters changed the ordering or retained a range")
	}
}

func TestSessionLogsDateGroupsKeepRecordHitsStable(t *testing.T) {
	state := sessionLogsState(t, true)
	if state.sessionLogs.view.err != "" {
		_, err := state.sessionLogs.store.List(state.sessionLogs.view.query)
		t.Fatalf("query failed before grouping: %v, query=%+v", err, state.sessionLogs.view.query)
	}
	var headings, ids []string
	for _, item := range state.sessionLogs.view.items {
		if item.row < 0 {
			headings = append(headings, item.heading)
		} else {
			ids = append(ids, state.sessionLogs.view.page[item.row].id)
		}
	}
	if !slices.Equal(headings, []string{"Today", "Yesterday", "Earlier"}) || !slices.Equal(ids, sessionLogRowIDs(state)) {
		t.Fatalf("groups/records = %v / %v", headings, ids)
	}
	state.sessionLogs.view.query.Sort = sessionlog.SortDurationDesc
	state.applySessionLogFilters()
	sessionLogsWait(t, state)
	for _, item := range state.sessionLogs.view.items {
		if item.row < 0 {
			t.Fatal("duration ordering contains calendar group headers")
		}
	}
}

func TestSessionLogsMidnightRebuildsRelativeLabels(t *testing.T) {
	now := sessionLogsTestNow
	options := sessionLogsTestOptions(t)
	options.WallNow = func() time.Time { return now }
	state := newTestState(t, options)
	sessionLogsSeed(t, state)
	state.openSessionLogs()
	sessionLogsWait(t, state)
	generation := state.sessionLogs.requestGeneration
	if len(state.sessionLogs.view.items) == 0 || state.sessionLogs.view.items[0].heading != "Today" {
		t.Fatal("initial today group missing")
	}
	now = now.AddDate(0, 0, 1)
	state.Wake()
	if state.sessionLogs.view.items[0].heading != "Yesterday" || state.sessionLogs.requestGeneration != generation {
		t.Fatal("midnight did not update labels without querying the unchanged all-time range")
	}
	state.sessionLogs.view.timeRange = sessionLogTimeToday
	state.applySessionLogFilters()
	sessionLogsWait(t, state)
	if len(state.sessionLogs.view.page) != 0 {
		t.Fatal("relative today range did not recompute")
	}
}

func TestSessionLogsLoadedMatchedTotalNotCursorLimited(t *testing.T) {
	state := sessionLogsState(t, false)
	for i := range 60 {
		sessionLogsInsert(t, state, sessionlog.Record{ID: fmt.Sprintf("count-%02d", i), RunID: "run", Host: "host", Protocol: sessionlog.ProtocolSSH, StartedAt: sessionLogsTestNow.Add(-time.Duration(i) * time.Minute)}, sessionlog.StatusEnded, sessionlog.ReasonUserClosed, sessionLogsTestNow)
	}
	state.refreshSessionLogs()
	sessionLogsWait(t, state)
	view := &state.sessionLogs.view
	if view.matched != 60 || view.total != 60 || !strings.Contains(view.countText, "50 / 60") {
		t.Fatalf("count summary = %q (matched=%d total=%d)", view.countText, view.matched, view.total)
	}
	state.loadMoreSessionLogs()
	sessionLogsWait(t, state)
	if len(view.page) != 60 || view.matched != 60 || !strings.Contains(view.countText, "60 / 60") {
		t.Fatalf("count changed with cursor: %q", view.countText)
	}
}

func TestSessionLogsDurationSortResetsAfterActiveRecordFinishes(t *testing.T) {
	state := sessionLogsState(t, false)
	for i := range 51 {
		sessionLogsInsert(t, state, sessionlog.Record{ID: fmt.Sprintf("done-%02d", i), RunID: "run", Host: "done", Protocol: sessionlog.ProtocolSSH, StartedAt: sessionLogsTestNow.Add(-time.Hour)}, sessionlog.StatusEnded, sessionlog.ReasonUserClosed, sessionLogsTestNow.Add(-59*time.Minute))
	}
	if err := state.sessionLogs.store.Start(sessionlog.Record{ID: "active", RunID: "run", Host: "active", Protocol: sessionlog.ProtocolSSH, StartedAt: sessionLogsTestNow.Add(-48 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	state.sessionLogs.view.query.Sort = sessionlog.SortDurationDesc
	state.applySessionLogFilters()
	sessionLogsWait(t, state)
	state.loadMoreSessionLogs()
	sessionLogsWait(t, state)
	view := &state.sessionLogs.view
	if len(view.page) != 52 || view.page[51].id != "active" {
		t.Fatal("active record was not last across pages")
	}
	oldGeneration := state.sessionLogs.requestGeneration
	view.scrollRows = 100
	state.submitSessionLog(sessionlog.Operation{Kind: sessionlog.OpFinish, ID: "active", Status: sessionlog.StatusEnded, Reason: sessionlog.ReasonUserClosed, EndedAt: sessionLogsTestNow})
	sessionLogsWaitFor(t, state, func() bool { return len(view.page) == 50 && view.page[0].id == "active" })
	if view.scrollRows != 0 || !view.query.Cursor.StartedAt.IsZero() {
		t.Fatal("changed sort key kept old scroll or cursor")
	}
	state.applySessionLogsResult(sessionLogsResult{generation: oldGeneration, appendPage: true, page: sessionlog.Page{Records: []sessionlog.Record{{ID: "stale"}}, Matched: 1, Total: 1}})
	if len(view.page) != 50 || view.total != 52 {
		t.Fatal("stale page overwrote the new duration ordering")
	}
}
