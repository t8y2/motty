package workbench

import (
	"strings"
	"testing"
	"time"

	"motty/internal/sessionlog"
)

func TestSessionLogsAuthenticationStagesAreTyped(t *testing.T) {
	now := time.Duration(0)
	_, options := sessionLogTestOptions(t, &now)
	state := newTestState(t, options)
	session := &Session{}
	host := Host{ID: 123, CatalogID: "host-stable", Label: "server", Address: "server.test", User: "root", Port: 2222}
	state.hosts = append(state.hosts, host)
	state.startSessionLog(session, host)
	state.journalConnectionEvent(session, connectionEventHostKeyPrompt)
	state.journalConnectionEvent(session, connectionEventAuthenticating)
	state.journalConnectionEvent(session, connectionEventAuthenticating)
	state.finishSessionLog(session, sessionlog.StatusEnded, sessionlog.ReasonUserClosed)
	rows := sessionLogRows(t, state)
	if len(rows) != 1 || rows[0].CatalogID != "host-stable" || rows[0].Port != 2222 || rows[0].TargetKind != sessionlog.TargetSavedHost {
		t.Fatalf("target snapshot = %+v", rows)
	}
	events := sessionLogEvents(t, state, rows[0].ID)
	want := []sessionlog.EventKind{sessionlog.EventStarted, sessionlog.EventHostKeyPrompt, sessionlog.EventAuthenticating, sessionlog.EventFinished}
	if len(events) != len(want) {
		t.Fatalf("stages = %+v", events)
	}
	for i, kind := range want {
		if events[i].Kind != kind {
			t.Fatalf("stage %d = %v, want %v", i, events[i].Kind, kind)
		}
	}
}

func TestSessionLogsLocalSnapshotOmitsExecutablePath(t *testing.T) {
	now := time.Duration(0)
	_, options := sessionLogTestOptions(t, &now)
	state := newTestState(t, options)
	host := localHost("local")
	host.localShell = "/private/sensitive-tools/zsh"
	state.startSessionLog(&Session{}, host)
	rows := sessionLogRows(t, state)
	if len(rows) != 1 || rows[0].ShellName != "zsh" || rows[0].Port != 0 || rows[0].TargetKind != sessionlog.TargetLocal {
		t.Fatalf("local snapshot = %+v", rows)
	}
	assertSessionLogFileOmits(t, state, "sensitive-tools")
}

func TestSessionLogsOtherWindowRefreshesAfterFinish(t *testing.T) {
	group := NewGroup()
	a, b := sessionLogsState(t, true), sessionLogsState(t, true)
	a.options.Group = group
	group.members = []*State{a, b}
	b.sessionLogs.view.dirty = false
	a.sessionLogWriteSettled(sessionlog.Operation{Kind: sessionlog.OpFinish, ID: "log-00"}, nil)
	if !b.sessionLogs.view.dirty {
		t.Fatal("peer view was not invalidated after a completed write")
	}
}

func TestSessionLogsLegacyTimelineSummaryIsLabeled(t *testing.T) {
	state := sessionLogsState(t, false)
	r := sessionlog.Record{ID: "legacy", Host: "old", Protocol: sessionlog.ProtocolSSH, StartedAt: sessionLogsTestNow.Add(-time.Hour), EndedAt: sessionLogsTestNow, Status: sessionlog.StatusEnded, Reason: sessionlog.ReasonUserClosed}
	state.sessionLogs.view.rows = []sessionlog.Record{r}
	state.sessionLogs.view.selected = r.ID
	state.prepareSessionLogDetail()
	detail := state.sessionLogs.view.detail
	if len(detail.events) != 1 || !strings.Contains(detail.events[0].label, state.messages.SessionLogs.ReasonUserClosed) {
		t.Fatalf("legacy summary missing: %+v", detail)
	}
}

func TestSessionLogsTrackingStoppedNeverClaimsConnectionEnded(t *testing.T) {
	state := sessionLogsState(t, false)
	r := sessionlog.Record{ID: "stopped", Status: sessionlog.StatusTrackingStopped, Reason: sessionlog.ReasonTrackingOff, StartedAt: sessionLogsTestNow.Add(-time.Hour), EndedAt: sessionLogsTestNow}
	events := []sessionlog.Event{{RecordID: r.ID, Kind: sessionlog.EventFinished, Reason: sessionlog.ReasonTrackingOff, At: r.EndedAt}}
	rows := state.sessionLogTimeline(r, events)
	if len(rows) != 1 || strings.Contains(rows[0].label, state.messages.SessionLogs.EventFinished) {
		t.Fatalf("stopping metadata capture claims the connection ended: %+v", rows)
	}
	if summary := sessionLogMetadataSummary(r, events); strings.Contains(summary, "Session ended") || strings.Contains(summary, "\nEnded:") {
		t.Fatalf("copied stopped record claims connection closure: %s", summary)
	}
}

func TestSessionLogsRowsExposeKnownAndUnknownReasons(t *testing.T) {
	state := sessionLogsState(t, true)
	state.prepareSessionLogsView()
	if !strings.Contains(state.sessionLogs.view.page[0].cells[3], state.messages.SessionLogs.ReasonUserClosed) {
		t.Fatalf("list hides outcome: %+v", state.sessionLogs.view.page[0])
	}
	view := &state.sessionLogs.view
	view.query.Outcome = sessionlog.OutcomeAbnormal
	state.applySessionLogFilters()
	sessionLogsWait(t, state)
	if len(view.page) != 3 {
		t.Fatalf("abnormal filter returned %d, want 3", len(view.page))
	}
	state.clearSessionLogFilters()
	if view.query.Outcome != sessionlog.OutcomeAll {
		t.Fatal("clearing filters left the outcome filter active")
	}
}

func TestSessionLogsClearConfirmationNamesSnapshotScope(t *testing.T) {
	state := sessionLogsState(t, true)
	state.askClearSessionLogs(true)
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(state.form.description, "6") && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(state.form.description, "6") {
		t.Fatalf("confirmation hides record count: %q", state.form.description)
	}
	// The record created after the preview must survive confirmation.
	sessionLogsInsert(t, state, sessionlog.Record{ID: "after-preview", RunID: "run", Host: "new", Protocol: sessionlog.ProtocolSSH, StartedAt: sessionLogsTestNow}, sessionlog.StatusEnded, sessionlog.ReasonUserClosed, sessionLogsTestNow)
	state.submitForm()
	sessionLogsWaitFor(t, state, func() bool {
		return len(state.sessionLogs.view.page) == 1 && state.sessionLogs.view.page[0].id == "after-preview"
	})
	page, err := state.sessionLogs.store.List(sessionlog.Query{})
	if err != nil || len(page.Records) != 1 || page.Records[0].ID != "after-preview" {
		t.Fatalf("clear affected records outside the confirmation: %+v %v", page, err)
	}
}

func TestSessionLogsFavoriteHitDoesNotChangeSelection(t *testing.T) {
	state := sessionLogsState(t, true)
	state.selectSessionLogRecord("log-00")
	sessionLogsWait(t, state)
	state.clickSessionLogs(hitSessionLogsRowFavorite.At(1))
	sessionLogsWaitFor(t, state, func() bool { r, _ := state.sessionLogRecord("log-05"); return r.Favorite })
	if state.sessionLogs.view.selected != "log-00" {
		t.Fatal("favoriting another row changed the current detail selection")
	}
	r, _ := state.sessionLogRecord("log-05")
	if !r.Favorite {
		t.Fatal("row favorite control did not update its own record")
	}
}

func TestSessionLogsNormalEndDoesNotClaimAnUnknownReason(t *testing.T) {
	state := sessionLogsState(t, false)
	started := sessionLogsTestNow.Add(-time.Hour)
	record := sessionlog.Record{ID: "clean-end", RunID: "run", Host: "api-1", Address: "api-1.example.com", Account: "root", Protocol: sessionlog.ProtocolSSH, TargetKind: sessionlog.TargetUnknown, StartedAt: started}
	sessionLogsInsert(t, state, record, sessionlog.StatusEnded, sessionlog.ReasonNone, started.Add(time.Minute), sessionlog.EventConnected, sessionlog.EventShellOpened)
	state.refreshSessionLogs()
	sessionLogsWait(t, state)
	state.selectSessionLogRecord(record.ID)
	sessionLogsWait(t, state)

	m := state.messages.SessionLogs
	row := state.sessionLogs.view.page[0]
	if row.cells[3] != m.StatusEnded {
		t.Fatalf("normal end row = %q, want %q", row.cells[3], m.StatusEnded)
	}
	if strings.Contains(row.cells[3], m.ReasonUnknown) {
		t.Fatalf("normal end row claims an unavailable reason: %q", row.cells[3])
	}
	detail := state.sessionLogs.view.detail
	result := ""
	for _, field := range detail.fields[:detail.count] {
		if field.label == m.FieldResult {
			result = field.value
		}
	}
	if result != m.ReasonNone {
		t.Fatalf("normal end result = %q, want %q", result, m.ReasonNone)
	}
	if len(detail.events) == 0 || detail.events[len(detail.events)-1].label != m.EventFinished {
		t.Fatalf("normal end timeline = %+v, want final %q", detail.events, m.EventFinished)
	}
	if got := sessionLogResultText(&m, &sessionlog.Record{Status: sessionlog.StatusTrackingStopped, Reason: sessionlog.ReasonTrackingOff}); got != m.StatusTrackingStopped {
		t.Fatalf("tracking stop row = %q, want %q", got, m.StatusTrackingStopped)
	}
}
