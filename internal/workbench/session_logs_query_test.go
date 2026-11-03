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
	for _, dates := range [][2]string{{"2026-02-30", "2026-03-01"}, {"2026-11-02", "2026-11-01"}, {"2026-1-1", "2026-01-02"}, {"", "2026-01-01"}, {"0000-12-31", "0000-12-31"}} {
		if _, _, err := sessionLogDateRange(dates[0], dates[1], location); err == nil {
			t.Fatalf("invalid calendar range accepted: %v", dates)
		}
	}
	if _, _, err := sessionLogDateRange("0001-01-01", "0001-01-01", time.UTC); err == nil {
		t.Fatal("zero-time lower bound accepted")
	}
}

func TestSessionLogCustomRangeMidnightTransitions(t *testing.T) {
	for _, tc := range []struct {
		zone, date, from, until string
	}{
		{"America/Santiago", "2026-09-05", "2026-09-05T04:00:00Z", "2026-09-06T04:00:00Z"},
		{"America/Santiago", "2026-09-06", "2026-09-06T04:00:00Z", "2026-09-07T03:00:00Z"},
		{"Pacific/Apia", "2011-12-29", "2011-12-29T10:00:00Z", "2011-12-30T10:00:00Z"},
		// A past fall-back: tzdata keeps past transitions, while a future rule
		// can change under the test.
		{"America/Havana", "2023-11-05", "2023-11-05T04:00:00Z", "2023-11-06T05:00:00Z"},
	} {
		location, err := time.LoadLocation(tc.zone)
		if err != nil {
			t.Fatal(err)
		}
		from, until, err := sessionLogDateRange(tc.date, tc.date, location)
		if err != nil || from.Format(time.RFC3339) != tc.from || until.Format(time.RFC3339) != tc.until {
			t.Fatalf("%s %s = [%v,%v), %v", tc.zone, tc.date, from, until, err)
		}
		if since, _ := sessionLogRange(from.In(location).Add(time.Hour), sessionLogTimeToday); !since.Equal(from) {
			t.Fatalf("%s Today = %v, want %v", tc.zone, since, from)
		}
	}
	location, _ := time.LoadLocation("Pacific/Apia")
	if _, _, err := sessionLogDateRange("2011-12-30", "2011-12-30", location); err == nil {
		t.Fatal("nonexistent local date accepted")
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
	state.form.set(0, "0000-12-31")
	state.form.set(1, "0000-12-31")
	state.submitForm()
	if state.modal != modalForm || state.form.dialogError == "" {
		t.Fatal("year-zero range did not keep validation visible")
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

func TestSessionLogsTimezoneChangeRebindsCustomRange(t *testing.T) {
	t.Setenv("TZ", "UTC")
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	options := sessionLogsTestOptions(t)
	options.WallNow = func() time.Time { return now }
	state := newTestState(t, options)
	for i, stamp := range []string{"2026-11-01T02:00:00Z", "2026-11-01T05:00:00Z", "2026-11-02T04:30:00Z", "2026-11-02T06:00:00Z"} {
		at, _ := time.Parse(time.RFC3339, stamp)
		sessionLogsInsert(t, state, sessionlog.Record{ID: fmt.Sprintf("zone-%d", i), RunID: "run", Host: "host", Protocol: sessionlog.ProtocolSSH, StartedAt: at}, sessionlog.StatusEnded, sessionlog.ReasonUserClosed, at.Add(time.Minute))
	}
	view := &state.sessionLogs.view
	view.timeRange, view.customStartDate, view.customEndDate = sessionLogTimeCustom, "2026-11-01", "2026-11-01"
	state.openSessionLogs()
	sessionLogsWait(t, state)
	if !slices.Equal(sessionLogRowIDs(state), []string{"zone-1", "zone-0"}) {
		t.Fatalf("initial UTC calendar day = %v", sessionLogRowIDs(state))
	}
	generation := state.sessionLogs.requestGeneration
	t.Setenv("TZ", "America/New_York")
	now = now.Add(61 * time.Second)
	// An unrelated projection must not acknowledge a clock change on behalf of
	// the query, even when it happens before the next wake.
	state.prepareSessionLogsView()
	state.Wake()
	sessionLogsWait(t, state)
	if !slices.Equal(sessionLogRowIDs(state), []string{"zone-2", "zone-1"}) || state.sessionLogs.requestGeneration <= generation || view.query.Until.Sub(view.query.Since) != 25*time.Hour {
		t.Fatalf("changed local calendar day = %v [%v,%v), generation %d", sessionLogRowIDs(state), view.query.Since, view.query.Until, state.sessionLogs.requestGeneration)
	}
	state.applySessionLogsResult(sessionLogsResult{generation: generation, page: sessionlog.Page{Records: []sessionlog.Record{{ID: "old-zone"}}, Matched: 1, Total: 1}})
	if !slices.Equal(sessionLogRowIDs(state), []string{"zone-2", "zone-1"}) || view.total != 4 {
		t.Fatal("old-zone response replaced the rebound custom range")
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
