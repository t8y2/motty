package workbench

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/configstore"
	"motty/internal/i18n"
	"motty/internal/sessionlog"
	"motty/internal/sshconn"
)

// insertSessionLog adds a finished record straight to the journal, for tests
// that need rows without a connection.
func insertSessionLog(t *testing.T, state *State, id, host string, started time.Time, favorite bool) {
	t.Helper()
	record := sessionlog.Record{
		ID:        id,
		RunID:     "run-test",
		Host:      host,
		Address:   host + ".example.com",
		Account:   "deploy",
		Protocol:  sessionlog.ProtocolSSH,
		StartedAt: started,
		Favorite:  favorite,
	}
	if err := state.sessionLogs.store.Start(record); err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	if err := state.sessionLogs.store.Finish(id, sessionlog.StatusEnded, started.Add(time.Minute), sessionlog.ReasonUserClosed); err != nil {
		t.Fatalf("finish %s: %v", id, err)
	}
}

func openSessionLogSettings(t *testing.T, state *State) {
	t.Helper()
	state.activeSection = sectionSettings
	state.settingsCategory = settingsSecurity
	state.modal = modalNone
}

func TestSessionLogsSettingIsLocalOnly(t *testing.T) {
	if syncedSettings["session_logs_off"] {
		t.Fatal("the session-log switch is a synced setting")
	}
	path := filepath.Join(t.TempDir(), "config.db")
	want := richState()
	want.SessionLogsOff = true
	if err := saveState(path, want); err != nil {
		t.Fatal(err)
	}
	got, report, err := loadState(path)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("load: %+v, %v", report, err)
	}
	if !got.SessionLogsOff {
		t.Fatal("the saved switch did not come back")
	}
	store, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entries, local, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if bytes.Contains(entry.Payload, []byte("session_logs_off")) {
			t.Fatalf("the switch became a shared record: %s/%s", entry.Kind, entry.ID)
		}
	}
	if !bytes.Contains(local[localKey], []byte(`"session_logs_off":true`)) {
		t.Fatalf("the switch is not in the local state: %s", local[localKey])
	}
}

func TestSessionLogsSettingsRowPaintsWithoutAVault(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.secrets.vault = nil
	openSessionLogSettings(t, state)
	canvas, texts := paintedTexts(state)

	m := state.messages.SessionLogs
	for _, want := range []string{m.SettingsGroup, m.SettingsCaptureRow, m.SettingsStorageRow, m.ClearUnfavorited, m.ClearAll} {
		if !contains(texts, want) {
			t.Fatalf("the security page does not paint %q", want)
		}
	}
	for _, id := range []gfx.ID{hitSessionLogsSettingsToggle, hitSessionLogsSettingsClear, hitSessionLogsSettingsClearAll} {
		if _, ok := hitByID(canvas, id); !ok {
			t.Fatalf("the security page has no control %d", id)
		}
	}
}

func TestSessionLogsOffPropagatesAndLeavesConnectionsRunning(t *testing.T) {
	pair := newTwoWindows(t)
	liveTab(t, pair.first, pair.firstW, 0)
	liveTab(t, pair.second, pair.secondW, 0)
	openSessionLogSettings(t, pair.first)

	pair.first.Click(hitSessionLogsSettingsToggle)
	if !pair.first.sessionLogsOff || !pair.second.sessionLogsOff {
		t.Fatalf("the switch did not reach every window: %v %v", pair.first.sessionLogsOff, pair.second.sessionLogsOff)
	}
	if pair.first.sessionLogs.enabled || pair.second.sessionLogs.enabled {
		t.Fatal("a window kept recording after the switch was turned off")
	}
	for index, state := range []*State{pair.first, pair.second} {
		session := &state.sessions[0]
		if session.phase != sshconn.PhaseConnected || session.term == nil || session.term.shell == nil {
			t.Fatalf("window %d: turning the journal off changed the live connection: %+v", index, session)
		}
	}
	// One journal serves the whole process, so the session each window opened
	// above left one row; turning the journal off stops both. Each window has
	// its own writer, so both queues are drained before the rows are counted.
	for _, state := range []*State{pair.first, pair.second} {
		settleSessionLogs(t, pair.first)
		settleSessionLogs(t, pair.second)
		rows, err := state.sessionLogs.store.List(sessionlog.Query{})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows.Records) != 2 {
			t.Fatalf("tracked rows after turning the journal off = %d, want 2", len(rows.Records))
		}
		for _, row := range rows.Records {
			if row.Status != sessionlog.StatusTrackingStopped || row.Reason != sessionlog.ReasonTrackingOff {
				t.Fatalf("tracked row after turning the journal off = %+v", row)
			}
		}
	}

	pair.first.Click(hitSessionLogsSettingsToggle)
	if pair.first.sessionLogsOff || pair.second.sessionLogsOff {
		t.Fatal("turning the journal back on did not reach every window")
	}
	// Re-enabling records only what is opened afterwards: a new tab in the other
	// window produces a row, the stopped row stays stopped.
	liveTab(t, pair.second, pair.secondW, 1)
	settleSessionLogs(t, pair.second)
	rows, err := pair.second.sessionLogs.store.List(sessionlog.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Records) != 3 {
		t.Fatalf("rows after re-enabling = %d, want 3", len(rows.Records))
	}
	if rows.Records[0].Status != sessionlog.StatusActive ||
		rows.Records[1].Status != sessionlog.StatusTrackingStopped || rows.Records[2].Status != sessionlog.StatusTrackingStopped {
		t.Fatalf("rows after re-enabling = %+v", rows.Records)
	}
}

func TestSessionLogsClearScopeOnlyTouchesItsOwnDatabase(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	_ = world
	base := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	insertSessionLog(t, state, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "favorite", base, true)
	insertSessionLog(t, state, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "plain-one", base.Add(time.Minute), false)
	insertSessionLog(t, state, "cccccccccccccccccccccccccccccccc", "plain-two", base.Add(2*time.Minute), false)
	settleSessionLogs(t, state)

	dir := filepath.Dir(options.StatePath)
	sentinels := map[string][]byte{
		"history.db":      []byte("command history stays"),
		"motty.log":       []byte("diagnostics stay"),
		"manual.mottyrec": []byte("manual recording stays"),
	}
	for name, content := range sentinels {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	openSessionLogSettings(t, state)
	state.Click(hitSessionLogsSettingsClear)
	sessionLogsWaitFor(t, state, func() bool { return state.form.description != state.messages.SessionLogs.Loading })
	if state.modal != modalForm || state.form.title != state.messages.SessionLogs.ClearUnfavoritedTitle {
		t.Fatalf("the unfavorited clear did not confirm its scope: modal %d title %q", state.modal, state.form.title)
	}
	if !strings.Contains(state.form.description, "unfavorited") {
		t.Fatalf("the confirmation does not name its scope: %q", state.form.description)
	}
	state.submitForm()
	settleSessionLogs(t, state)
	rows, err := state.sessionLogs.store.List(sessionlog.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Records) != 1 || !rows.Records[0].Favorite {
		t.Fatalf("rows after clearing unfavorited = %+v", rows.Records)
	}
	assertSessionLogSentinels(t, dir, sentinels)

	state.Click(hitSessionLogsSettingsClearAll)
	sessionLogsWaitFor(t, state, func() bool { return state.form.description != state.messages.SessionLogs.Loading })
	if state.modal != modalForm || !strings.Contains(state.form.description, "favorites") {
		t.Fatalf("the clear-all confirmation does not name its scope: %q", state.form.description)
	}
	state.submitForm()
	settleSessionLogs(t, state)
	rows, err = state.sessionLogs.store.List(sessionlog.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Records) != 0 {
		t.Fatalf("rows after clearing all = %+v", rows.Records)
	}
	assertSessionLogSentinels(t, dir, sentinels)
}

func TestSessionLogsSettingsClearFailureShowsAnError(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openSessionLogSettings(t, state)
	if err := state.sessionLogs.store.Close(); err != nil {
		t.Fatal(err)
	}
	state.clearSessionLogsAndNotify(true)
	deadline := time.Now().Add(3 * time.Second)
	for len(state.fileNotices.items) == 0 && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	requireActionNotice(t, state, state.messages.SessionLogs.WriteFailed, components.ToastError)
}

func TestSessionLogsSettingsSelectionFailureShowsAnError(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openSessionLogSettings(t, state)
	state.askClearSessionLogs(true)
	sessionLogsWaitFor(t, state, func() bool { return state.form.description != state.messages.SessionLogs.Loading })
	if err := state.sessionLogs.store.Close(); err != nil {
		t.Fatal(err)
	}
	state.submitForm()
	deadline := time.Now().Add(time.Second)
	for len(state.fileNotices.items) == 0 && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	requireActionNotice(t, state, state.messages.SessionLogs.WriteFailed, components.ToastError)
}

func assertSessionLogSentinels(t *testing.T, dir string, sentinels map[string][]byte) {
	t.Helper()
	for name, want := range sentinels {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("clearing the session journal changed %s", name)
		}
	}
}

func TestSessionLogsFootprintIsShownInTheSecurityPage(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	_ = world
	base := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	insertSessionLog(t, state, "dddddddddddddddddddddddddddddddd", "one", base, true)
	insertSessionLog(t, state, "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", "two", base.Add(time.Minute), false)
	settleSessionLogs(t, state)
	openSessionLogSettings(t, state)

	deadline := time.Now().Add(5 * time.Second)
	for !state.sessionLogs.settings.hasStats && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(2 * time.Millisecond)
	}
	if !state.sessionLogs.settings.hasStats {
		t.Fatal("the security page never asked for the journal footprint")
	}
	_, texts := paintedTexts(state)
	want := fmt.Sprintf(state.messages.SessionLogs.SettingsStorageFmt, 2, formatBytes(state.sessionLogs.settings.stats.Bytes))
	if !contains(texts, want) {
		t.Fatalf("the security page does not show %q", want)
	}

	// A journal that cannot be read says so instead of showing stale counts.
	if err := state.sessionLogs.store.Close(); err != nil {
		t.Fatal(err)
	}
	state.sessionLogs.store = nil
	state.sessionLogs.settings.hasStats = false
	state.sessionLogs.settings.requested = false
	state.sessionLogs.settings.pageVisible = false
	state.Wake()
	_, texts = paintedTexts(state)
	if !contains(texts, state.messages.SessionLogs.SettingsStorageUnavailable) {
		t.Fatal("an unreadable journal is not reported on the security page")
	}
}

func TestSessionLogsFootprintFollowsTheLanguage(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	_ = world
	insertSessionLog(t, state, "ffffffffffffffffffffffffffffffff", "language", time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC), false)
	settleSessionLogs(t, state)
	openSessionLogSettings(t, state)
	deadline := time.Now().Add(5 * time.Second)
	for !state.sessionLogs.settings.hasStats && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(2 * time.Millisecond)
	}
	if !state.sessionLogs.settings.hasStats {
		t.Fatal("no footprint was read")
	}
	if got, want := state.sessionLogs.settings.text, fmt.Sprintf(state.messages.SessionLogs.SettingsStorageFmt, 1, formatBytes(state.sessionLogs.settings.stats.Bytes)); got != want {
		t.Fatalf("English footprint = %q, want %q", got, want)
	}
	state.chooseLanguage(i18n.Chinese)
	state.Wake()
	if got, want := state.sessionLogs.settings.text, fmt.Sprintf(state.messages.SessionLogs.SettingsStorageFmt, 1, formatBytes(state.sessionLogs.settings.stats.Bytes)); got != want {
		t.Fatalf("Chinese footprint = %q, want %q", got, want)
	}
}
