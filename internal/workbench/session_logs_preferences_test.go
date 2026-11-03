package workbench

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"motty/internal/configstore"
	"motty/internal/sessionlog"
)

func TestSessionLogsViewPreferencesAreDeviceLocal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	want := richState()
	want.SessionLogsView = &savedSessionLogsView{TimeRange: "custom", StartDate: "2026-10-01", EndDate: "2026-10-30", Status: "failed", Protocol: "ssh", FavoriteOnly: true, Outcome: "abnormal", Sort: "duration_desc"}
	if err := saveState(path, want); err != nil {
		t.Fatal(err)
	}
	got, _, err := loadState(path)
	if err != nil || got.SessionLogsView == nil || *got.SessionLogsView != *want.SessionLogsView {
		t.Fatalf("preferences did not persist: %+v %v", got.SessionLogsView, err)
	}
	store, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	entries, local, err := store.Load()
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if bytes.Contains(entry.Payload, []byte("session_logs_view")) {
			t.Fatal("view preferences entered a shared record")
		}
	}
	if syncedSettings["session_logs_view"] || !bytes.Contains(local[localKey], []byte("session_logs_view")) {
		t.Fatal("view preferences are not device-local")
	}
	// Since epoch 23 a writer patches the local document field by field, so a
	// build that has never heard of the view defaults keeps them when it saves.
	writer, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.Commit(configstore.Change{
		LocalPatch: map[string]configstore.JSONChange{
			localKey: {Before: []byte(`{"theme":"dark"}`), After: []byte(`{"theme":"light"}`)},
		},
	}); err != nil {
		t.Fatalf("an epoch-23 save was refused: %v", err)
	}
	after, _, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.SessionLogsView == nil || *after.SessionLogsView != *want.SessionLogsView {
		t.Fatalf("an epoch-23 save dropped the view preferences: %+v", after.SessionLogsView)
	}
}

func TestSessionLogsViewPreferencesRoundTrip(t *testing.T) {
	state := sessionLogsState(t, true)
	view := &state.sessionLogs.view
	view.timeRange, view.customStartDate, view.customEndDate = sessionLogTimeCustom, "2026-10-01", "2026-10-30"
	view.statusFilter, view.protocolFilter, view.favoriteOnly = sessionlog.StatusFailed, sessionlog.ProtocolSSH, true
	view.query.Sort, view.query.Outcome = sessionlog.SortDurationAsc, sessionlog.OutcomeAbnormal
	state.applySessionLogFilters()
	sessionLogsWait(t, state)
	view.search.Insert("secret-search")
	view.selected = "secret-selection"
	view.query.Cursor = sessionlog.Cursor{ID: "secret-cursor", StartedAt: sessionLogsTestNow}
	view.scrollRows = 123
	saved := state.snapshot()
	if saved.SessionLogsView == nil || saved.SessionLogsView.Sort != "duration_asc" || saved.SessionLogsView.TimeRange != "custom" {
		t.Fatalf("snapshot omits chosen defaults: %+v", saved.SessionLogsView)
	}
	data, err := json.Marshal(saved.SessionLogsView)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-search", "secret-selection", "secret-cursor"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("transient field persisted: %s", secret)
		}
	}
	saved.SessionLogsView.Sort = "tampered"
	if state.snapshotSessionLogsView().Sort != "duration_asc" {
		t.Fatal("snapshot aliases mutable preference state")
	}
	restored := newTestState(t, sessionLogsTestOptions(t))
	restored.restoreSessionLogsView(state.snapshotSessionLogsView())
	other := &restored.sessionLogs.view
	if other.query.Sort != sessionlog.SortDurationAsc || other.timeRange != sessionLogTimeCustom || other.customStartDate != "2026-10-01" || other.query.Outcome != sessionlog.OutcomeAbnormal || other.statusFilter != sessionlog.StatusFailed || !other.favoriteOnly {
		t.Fatalf("restored view = %+v", other)
	}
	if other.search.RuneCount() != 0 || other.selected != "" || other.query.Cursor.ID != "" || other.scrollRows != 0 {
		t.Fatal("transient state was restored")
	}
}

func TestSessionLogsRelativeRangeRecomputesAfterRestore(t *testing.T) {
	now := sessionLogsTestNow
	options := sessionLogsTestOptions(t)
	options.WallNow = func() time.Time { return now }
	state := newTestState(t, options)
	state.restoreSessionLogsView(&savedSessionLogsView{TimeRange: "today", Sort: "started_desc"})
	state.openSessionLogs()
	sessionLogsWait(t, state)
	state.save()
	now = now.AddDate(0, 0, 1)
	other := newTestState(t, options)
	other.openSessionLogs()
	sessionLogsWait(t, other)
	local := now.In(time.Local)
	want := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	if other.sessionLogs.view.timeRange != sessionLogTimeToday || !other.sessionLogs.view.query.Since.Equal(want) {
		t.Fatalf("relative range used a saved absolute time: %v", other.sessionLogs.view.query.Since)
	}
}

func TestSessionLogsUnknownPreferencesUseDefaults(t *testing.T) {
	state := newTestState(t, sessionLogsTestOptions(t))
	state.restoreSessionLogsView(&savedSessionLogsView{TimeRange: "future-range", Status: "future-status", Protocol: "future-protocol", Outcome: "future-outcome", Sort: "future-sort"})
	view := &state.sessionLogs.view
	if view.timeRange != sessionLogTimeAll || view.statusFilter != 0 || view.protocolFilter != 0 || view.query.Sort != sessionlog.SortStartedDesc || view.query.Outcome != sessionlog.OutcomeAll {
		t.Fatal("unknown preferences did not fall back safely")
	}
	state.restoreSessionLogsView(&savedSessionLogsView{TimeRange: "custom", StartDate: "not-a-date", EndDate: "2026-10-30"})
	if view.timeRange != sessionLogTimeAll {
		t.Fatal("invalid saved calendar range became active")
	}
	state.restoreSessionLogsView(&savedSessionLogsView{TimeRange: "custom", StartDate: "0000-12-31", EndDate: "0000-12-31"})
	if view.timeRange != sessionLogTimeAll {
		t.Fatal("year-zero saved calendar range became active")
	}
}

func TestSessionLogsViewDefaultsDoNotInterruptPeerQueries(t *testing.T) {
	group := NewGroup()
	options := sessionLogsTestOptions(t)
	now := sessionLogsTestNow
	options.WallNow = func() time.Time { return now }
	options.Group = group
	a, b := newTestState(t, options), newTestState(t, options)
	a.openSessionLogs()
	b.openSessionLogs()
	sessionLogsWait(t, a)
	sessionLogsWait(t, b)
	b.sessionLogs.view.timeRange = sessionLogTimeToday
	b.applySessionLogFilters()
	sessionLogsWait(t, b)
	a.sessionLogs.view.query.Sort = sessionlog.SortDurationDesc
	a.applySessionLogFilters()
	sessionLogsWait(t, a)
	if b.sessionLogs.view.query.Sort != sessionlog.SortStartedDesc {
		t.Fatal("a peer's active query was replaced")
	}
	now = now.AddDate(0, 0, 1)
	b.Wake()
	sessionLogsWait(t, b)
	// Saving an unrelated setting must not overwrite the newest remembered defaults.
	b.save()
	saved, _, err := loadState(options.StatePath)
	if err != nil || saved.SessionLogsView == nil || saved.SessionLogsView.Sort != "duration_desc" {
		t.Fatalf("unrelated peer save lost latest defaults: %+v %v", saved.SessionLogsView, err)
	}
}
