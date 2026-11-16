package workbench

import (
	"testing"
	"time"
)

func TestSessionLogsCalendarTimerStopsWhenHiddenAndClosed(t *testing.T) {
	options := sessionLogsTestOptions(t)
	options.Invalidate = func() {}
	state := newTestState(t, options)
	state.openSessionLogs()
	sessionLogsWait(t, state)
	if state.sessionLogs.calendar.timer == nil {
		t.Fatal("visible page has no calendar wake")
	}
	state.activeSection = sectionHosts
	state.Wake()
	if state.sessionLogs.calendar.timer != nil {
		t.Fatal("hidden page retained its calendar wake")
	}
	state.openSessionLogs()
	sessionLogsWait(t, state)
	if state.sessionLogs.calendar.timer == nil {
		t.Fatal("reopened page did not resume calendar wakes")
	}
	state.Close()
	if state.sessionLogs.calendar.timer != nil {
		t.Fatal("closed window retained its calendar wake")
	}
}

func TestSessionLogsLocationSourceRetainsLastGoodZone(t *testing.T) {
	t.Setenv("TZ", "America/New_York")
	source, location := loadSessionLogLocation("", nil)
	if _, offset := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC).In(location).Zone(); offset != -5*3600 {
		t.Fatalf("local source offset = %d", offset)
	}
	if same, cached := loadSessionLogLocation(source, location); same != source || cached != location {
		t.Fatal("unchanged source replaced the cached location")
	}
	t.Setenv("TZ", "Not/A/Location")
	if same, cached := loadSessionLogLocation(source, location); same != source || cached != location {
		t.Fatal("failed location lookup discarded the last good zone")
	}
}
