package workbench

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"motty/internal/sessionlog"
	"motty/internal/sshconn"
)

// sessionLogTestOptions is newWorld with a fixed wall clock and a controllable
// monotonic one, so rows and reconnect delays are deterministic.
func sessionLogTestOptions(t *testing.T, now *time.Duration) (*world, Options) {
	t.Helper()
	world, options := newWorld(t)
	options.WallNow = func() time.Time { return time.Date(2026, 10, 30, 8, 30, 0, 0, time.UTC) }
	options.Now = func() time.Duration { return *now }
	return world, options
}

// settleSessionLogs waits until every write submitted so far has run. The queue
// is FIFO, so an acknowledgment for a later operation proves the earlier ones
// finished. Deleting a missing id is not an error and changes nothing.
func settleSessionLogs(t *testing.T, state *State) {
	t.Helper()
	queue := state.sessionLogs.queue
	if queue == nil {
		t.Fatal("the session log queue is not open")
	}
	ack := queue.Submit(sessionlog.Operation{Kind: sessionlog.OpDelete, ID: "00000000000000000000000000000000"})
	select {
	case <-ack:
	case <-time.After(10 * time.Second):
		t.Fatal("the session log queue did not settle")
	}
}

func sessionLogRows(t *testing.T, state *State) []sessionlog.Record {
	t.Helper()
	settleSessionLogs(t, state)
	page, err := state.sessionLogs.store.List(sessionlog.Query{})
	if err != nil {
		t.Fatalf("list session logs: %v", err)
	}
	return page.Records
}

func sessionLogEvents(t *testing.T, state *State, id string) []sessionlog.Event {
	t.Helper()
	settleSessionLogs(t, state)
	events, err := state.sessionLogs.store.Events(id)
	if err != nil {
		t.Fatalf("events of %s: %v", id, err)
	}
	return events
}

func containsEvent(events []sessionlog.Event, kind sessionlog.EventKind) bool {
	for _, event := range events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

// connectForLog dials the first saved host and brings its fake transport up.
func connectForLog(t *testing.T, state *State, world *world) *fakeConn {
	t.Helper()
	connection := connect(t, state, world)
	connection.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseConnected
		status.ServerVersion = "SSH-2.0-Test"
		status.Method = "publickey"
	})
	state.Wake()
	return connection
}

func TestSessionLogsRecordConnectionOutcomes(t *testing.T) {
	t.Run("a successful SSH session is active with a shell", func(t *testing.T) {
		now := time.Duration(0)
		world, options := sessionLogTestOptions(t, &now)
		state := newTestState(t, options)
		connectForLog(t, state, world)

		rows := sessionLogRows(t, state)
		if len(rows) != 1 {
			t.Fatalf("rows = %d, want 1", len(rows))
		}
		row := rows[0]
		if row.Host != "Production" || row.Address != "prod.example.com" || row.Account != "deploy" || row.Protocol != sessionlog.ProtocolSSH {
			t.Fatalf("row metadata = %+v", row)
		}
		if row.Status != sessionlog.StatusActive || !row.StartedAt.Equal(time.Date(2026, 10, 30, 8, 30, 0, 0, time.UTC)) {
			t.Fatalf("row state = status %d started %v", row.Status, row.StartedAt)
		}
		events := sessionLogEvents(t, state, row.ID)
		if !containsEvent(events, sessionlog.EventConnected) || !containsEvent(events, sessionlog.EventShellOpened) {
			t.Fatalf("events = %+v", events)
		}

		state.closeSession(state.sessions[0].ID)
		rows = sessionLogRows(t, state)
		if rows[0].Status != sessionlog.StatusEnded || rows[0].Reason != sessionlog.ReasonUserClosed || rows[0].EndedAt.IsZero() {
			t.Fatalf("closed row = %+v", rows[0])
		}
	})

	t.Run("an authentication failure is failed and stores no error text", func(t *testing.T) {
		now := time.Duration(0)
		world, options := sessionLogTestOptions(t, &now)
		state := newTestState(t, options)
		connection := connect(t, state, world)
		connection.set(func(status *sshconn.Status) {
			status.Phase = sshconn.PhaseFailed
			status.Err = errors.Join(sshconn.ErrAuthFailed, errors.New("password=hunter2 rejected"))
		})
		state.Wake()

		rows := sessionLogRows(t, state)
		if len(rows) != 1 || rows[0].Status != sessionlog.StatusFailed || rows[0].Reason != sessionlog.ReasonAuthFailed {
			t.Fatalf("rows = %+v", rows)
		}
		assertSessionLogFileOmits(t, state, "hunter2", "rejected")
	})

	t.Run("a shell failure after the transport connected is failed, not active", func(t *testing.T) {
		now := time.Duration(0)
		world, options := sessionLogTestOptions(t, &now)
		state := newTestState(t, options)
		connection := connect(t, state, world)
		connection.shellErr = errors.New("shell refused: password=hunter2")
		connection.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
		state.Wake()

		rows := sessionLogRows(t, state)
		if len(rows) != 1 || rows[0].Status != sessionlog.StatusFailed || rows[0].Reason != sessionlog.ReasonShellFailed {
			t.Fatalf("rows = %+v", rows)
		}
		assertSessionLogFileOmits(t, state, "hunter2")
	})

	t.Run("a dial failure is failed with a known reason", func(t *testing.T) {
		now := time.Duration(0)
		world, options := sessionLogTestOptions(t, &now)
		state := newTestState(t, options)
		connection := connect(t, state, world)
		connection.set(func(status *sshconn.Status) {
			status.Phase = sshconn.PhaseFailed
			status.Err = errors.New("dial tcp 10.0.0.1:22: connection refused")
		})
		state.Wake()

		rows := sessionLogRows(t, state)
		if len(rows) != 1 || rows[0].Status != sessionlog.StatusFailed || rows[0].Reason != sessionlog.ReasonConnectFailed {
			t.Fatalf("rows = %+v", rows)
		}
		assertSessionLogFileOmits(t, state, "10.0.0.1", "refused")
	})
}

func TestSessionLogsReconnectStaysOneRow(t *testing.T) {
	now := time.Duration(0)
	world, options := sessionLogTestOptions(t, &now)
	state := newTestState(t, options)
	state.connectionReconnect = reconnectThree
	connection := connectForLog(t, state, world)
	rows := sessionLogRows(t, state)
	id := rows[0].ID

	disconnectEstablished(connection, state)
	rows = sessionLogRows(t, state)
	if len(rows) != 1 || rows[0].ID != id || rows[0].Status != sessionlog.StatusReconnecting {
		t.Fatalf("after the first disconnect rows = %+v", rows)
	}
	if rows[0].ReconnectCount != 1 {
		t.Fatalf("reconnect count = %d", rows[0].ReconnectCount)
	}

	now += time.Second // the first retry is due one second after the close
	state.Wake()
	if len(world.conns) != 2 {
		t.Fatalf("retry dials = %d, want a second connection", len(world.conns))
	}
	world.conns[1].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()

	rows = sessionLogRows(t, state)
	if len(rows) != 1 || rows[0].ID != id || rows[0].Status != sessionlog.StatusActive {
		t.Fatalf("after the retry rows = %+v", rows)
	}
	events := sessionLogEvents(t, state, id)
	for _, kind := range []sessionlog.EventKind{
		sessionlog.EventReconnectScheduled, sessionlog.EventReconnectStarted, sessionlog.EventConnected,
	} {
		if !containsEvent(events, kind) {
			t.Fatalf("events = %+v, missing %d", events, kind)
		}
	}
}

func TestSessionLogsExhaustedReconnectFinishesOnce(t *testing.T) {
	now := time.Duration(0)
	world, options := sessionLogTestOptions(t, &now)
	state := newTestState(t, options)
	state.connectionReconnect = reconnectThree
	connection := connectForLog(t, state, world)

	disconnectEstablished(connection, state)
	id := sessionLogRows(t, state)[0].ID
	for attempt := 0; attempt < 3; attempt++ {
		now += reconnectDelay(state.sessions[0].reconnectAttempt)
		state.Wake()
		current := world.conns[len(world.conns)-1]
		current.set(func(status *sshconn.Status) {
			status.Phase = sshconn.PhaseFailed
			status.Err = errors.Join(sshconn.ErrDisconnected, errors.New("network is unreachable"))
		})
		state.Wake()
	}

	rows := sessionLogRows(t, state)
	if len(rows) != 1 || rows[0].ID != id || rows[0].Status != sessionlog.StatusFailed || rows[0].Reason != sessionlog.ReasonNetworkLost {
		t.Fatalf("exhausted rows = %+v", rows)
	}
	if rows[0].ReconnectCount != 3 || rows[0].EndedAt.IsZero() {
		t.Fatalf("exhausted row = %+v", rows[0])
	}
	assertSessionLogFileOmits(t, state, "unreachable")
}

func TestSessionLogsManualReconnectStartsANewRow(t *testing.T) {
	now := time.Duration(0)
	world, options := sessionLogTestOptions(t, &now)
	state := newTestState(t, options)
	connection := connect(t, state, world)
	connection.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseFailed
		status.Err = sshconn.ErrAuthFailed
	})
	state.Wake()
	first := sessionLogRows(t, state)
	if len(first) != 1 {
		t.Fatalf("rows after the failure = %d", len(first))
	}
	session := &state.sessions[0]
	state.options.WallNow = func() time.Time { return time.Date(2026, 10, 30, 8, 31, 0, 0, time.UTC) }
	state.reconnect(session)
	if session.logRecordID == first[0].ID {
		t.Fatal("a manual reconnect reused the row of the failed attempt")
	}

	rows := sessionLogRows(t, state)
	if len(rows) != 2 {
		t.Fatalf("rows after a manual reconnect = %d, want 2", len(rows))
	}
	// The newest row is the retry; the failed attempt stays terminal.
	if rows[0].Status != sessionlog.StatusConnecting || rows[1].Status != sessionlog.StatusFailed {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestSessionLogsCloseFlushesPendingEvents(t *testing.T) {
	now := time.Duration(0)
	world, options := sessionLogTestOptions(t, &now)
	state := newTestState(t, options)
	connectForLog(t, state, world)
	session := &state.sessions[0]
	if session.logRecordID == "" {
		t.Fatal("a connected session has no log record")
	}
	id := session.logRecordID

	state.Close()
	if state.sessionLogs.queue != nil || state.sessionLogs.store != nil {
		t.Fatal("closing the window left the journal open")
	}

	store, err := sessionlog.Open(state.sessionLogPath())
	if err != nil {
		t.Fatalf("reopen journal: %v", err)
	}
	defer store.Close()
	records, err := store.List(sessionlog.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(records.Records) != 1 || records.Records[0].ID != id || records.Records[0].Status != sessionlog.StatusEnded || records.Records[0].Reason != sessionlog.ReasonUserClosed {
		t.Fatalf("flushed records = %+v", records.Records)
	}
	events, err := store.Events(id)
	if err != nil || len(events) == 0 {
		t.Fatalf("flushed events = %+v, err %v", events, err)
	}
}

func TestSessionLogsDisabledDoesNotAffectSSH(t *testing.T) {
	now := time.Duration(0)
	world, options := sessionLogTestOptions(t, &now)
	options.SessionLogRunID = "disabled-run"
	state := newTestState(t, options)
	state.setSessionLogsEnabled(false)
	connectForLog(t, state, world)

	if state.sessions[0].term == nil || state.sessions[0].term.shell == nil {
		t.Fatal("disabling metadata capture prevented the shell from opening")
	}
	if len(sessionLogRows(t, state)) != 0 {
		t.Fatal("a disabled journal recorded a row")
	}

	// Turning capture off for an open session marks it tracking-stopped without
	// touching the connection.
	state.setSessionLogsEnabled(true)
	state.setSessionLogsEnabled(false)
	if state.sessions[0].phase != sshconn.PhaseConnected {
		t.Fatalf("disabling capture changed the connection phase to %v", state.sessions[0].phase)
	}
}

func TestSessionLogsDisableMarksOpenRowsTrackingStopped(t *testing.T) {
	now := time.Duration(0)
	world, options := sessionLogTestOptions(t, &now)
	state := newTestState(t, options)
	connectForLog(t, state, world)
	id := state.sessions[0].logRecordID

	state.setSessionLogsEnabled(false)
	rows := sessionLogRows(t, state)
	if len(rows) != 1 || rows[0].ID != id || rows[0].Status != sessionlog.StatusTrackingStopped || rows[0].Reason != sessionlog.ReasonTrackingOff {
		t.Fatalf("rows after disabling = %+v", rows)
	}
	if state.sessions[0].term.shell == nil {
		t.Fatal("disabling capture closed the shell")
	}

	// Re-enabling only affects sessions opened afterwards.
	state.setSessionLogsEnabled(true)
	state.options.WallNow = func() time.Time { return time.Date(2026, 10, 30, 8, 31, 0, 0, time.UTC) }
	before := len(world.conns)
	state.newHostSession(state.hosts[0])
	if len(world.conns) != before+1 {
		t.Fatalf("opening a second tab dialed %d connections, want one more", len(world.conns)-before)
	}
	second := world.conns[len(world.conns)-1]
	second.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	rows = sessionLogRows(t, state)
	if len(rows) != 2 {
		t.Fatalf("rows after re-enabling = %d, want 2", len(rows))
	}
	if rows[0].Status != sessionlog.StatusActive || rows[1].Status != sessionlog.StatusTrackingStopped {
		t.Fatalf("rows after re-enabling = %+v", rows)
	}
}

func TestSessionLogsWindowMoveKeepsTheRecord(t *testing.T) {
	now := time.Duration(0)
	dir := t.TempDir()
	group := NewGroup()
	firstWorld := &world{}
	secondWorld := &world{}
	optionsFor := func(w *world) Options {
		return Options{
			KnownHostsPath:        filepath.Join(dir, "known_hosts"),
			StatePath:             filepath.Join(dir, "state.json"),
			Keystore:              &memKeys{},
			Group:                 group,
			PreferredLanguages:    func() []string { return nil },
			SystemAppearance:      func() app.Appearance { return app.AppearanceUnknown },
			WatchSystemAppearance: func(func()) func() { return func() {} },
			SetNativeAppearance:   func(app.Appearance) {},
			WallNow:               func() time.Time { return time.Date(2026, 10, 30, 8, 30, 0, 0, time.UTC) },
			Now:                   func() time.Duration { return now },
			Dial: func(config sshconn.Config) Connection {
				fake := &fakeConn{}
				w.conns = append(w.conns, fake)
				return fake
			},
		}
	}
	if err := saveState(filepath.Join(dir, "state.json"), savedState{Hosts: sampleHosts}); err != nil {
		t.Fatal(err)
	}
	first := newTestState(t, optionsFor(firstWorld))
	second := newTestState(t, optionsFor(secondWorld))
	gate := make(chan struct{}, 1)
	defer close(gate)
	first.sessionLogs.queue.Submit(sessionlog.Operation{Kind: sessionlog.OpWait, Wait: gate})

	connection := connectForLog(t, first, firstWorld)
	id := first.sessions[0].logRecordID
	workspaceID := first.deck.ActiveWorkspace()
	payload, ok := first.detachWorkspace(workspaceID)
	if !ok || len(payload.sessions) != 1 {
		t.Fatalf("detach = %v, sessions %d", ok, len(payload.sessions))
	}
	if payload.sessions[0].logRecordID != id {
		t.Fatalf("the payload lost the record id: %q, want %q", payload.sessions[0].logRecordID, id)
	}
	if !second.attachPayload(payload, tabDrop{kind: dropReorder}) {
		t.Fatal("attach failed")
	}
	if second.sessions[0].logRecordID != id {
		t.Fatalf("the moved tab's record id = %q, want %q", second.sessions[0].logRecordID, id)
	}
	second.closeSession(second.sessions[0].ID)
	// The destination must not finalize before the source's queued Start lands.
	barrier := second.sessionLogs.queue.Submit(sessionlog.Operation{Kind: sessionlog.OpWait})
	select {
	case <-barrier:
		t.Fatal("destination finished while the source journal was still blocked")
	case <-time.After(30 * time.Millisecond):
	}
	gate <- struct{}{}
	settleSessionLogs(t, first)
	settleSessionLogs(t, second)
	store, err := sessionlog.Open(filepath.Join(dir, "session-logs.db"))
	if err != nil {
		t.Fatalf("reopen journal: %v", err)
	}
	defer store.Close()
	page, err := store.List(sessionlog.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Records) != 1 || page.Records[0].ID != id || page.Records[0].Status != sessionlog.StatusEnded {
		t.Fatalf("rows after the moved tab closed = %+v", page.Records)
	}
	if connection.shell(t) == nil {
		t.Fatal("the moved session lost its shell")
	}
}

func TestSessionLogsRecoverOnlyPriorRun(t *testing.T) {
	dir := t.TempDir()
	options := func(runID string) Options {
		return Options{
			KnownHostsPath:        filepath.Join(dir, "known_hosts"),
			StatePath:             filepath.Join(dir, "state.json"),
			Keystore:              &memKeys{},
			SessionLogRunID:       runID,
			PreferredLanguages:    func() []string { return nil },
			SystemAppearance:      func() app.Appearance { return app.AppearanceUnknown },
			WatchSystemAppearance: func(func()) func() { return func() {} },
			SetNativeAppearance:   func(app.Appearance) {},
			WallNow:               func() time.Time { return time.Date(2026, 10, 30, 8, 30, 0, 0, time.UTC) },
		}
	}
	previous := newTestState(t, options("run-previous"))
	started := time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC)
	if err := previous.sessionLogs.store.Start(sessionlog.Record{
		ID: "11111111111111111111111111111111", RunID: "run-previous", Host: "Previous",
		Protocol: sessionlog.ProtocolSSH, StartedAt: started,
	}); err != nil {
		t.Fatal(err)
	}
	if err := previous.sessionLogs.store.Start(sessionlog.Record{
		ID: "22222222222222222222222222222222", RunID: "run-previous", Host: "Previous too",
		Protocol: sessionlog.ProtocolSSH, StartedAt: started.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	previous.Close()

	current := newTestState(t, options("run-current"))
	settleSessionLogs(t, current)
	rows := sessionLogRows(t, current)
	if len(rows) != 2 {
		t.Fatalf("rows after recovery = %d", len(rows))
	}
	for _, row := range rows {
		if row.Status != sessionlog.StatusInterrupted || row.Reason != sessionlog.ReasonProcessRestart || row.EndedAt.IsZero() {
			t.Fatalf("recovered row = %+v", row)
		}
	}

	// A row of the current run survives a second recovery in the same process,
	// as another window opening would perform.
	if err := current.sessionLogs.store.Start(sessionlog.Record{
		ID: "33333333333333333333333333333333", RunID: "run-current", Host: "Current",
		Protocol: sessionlog.ProtocolLocal, StartedAt: started.Add(2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := current.sessionLogs.store.Recover("run-current", started.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	latest, err := current.sessionLogs.store.List(sessionlog.Query{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range latest.Records {
		if row.ID == "33333333333333333333333333333333" && row.Status != sessionlog.StatusConnecting {
			t.Fatalf("a same-run row was recovered: %+v", row)
		}
	}
}

func TestSessionLogProtocolMetadata(t *testing.T) {
	tests := []struct {
		name     string
		host     Host
		protocol sessionlog.Protocol
	}{
		{"ssh", Host{Label: "Server", Address: "server.example.com", User: "deploy"}, sessionlog.ProtocolSSH},
		{"local shell", Host{ID: localHostID, Label: "This computer", Address: "mac"}, sessionlog.ProtocolLocal},
		{"telnet", Host{Label: "Switch", Address: "switch.example.com", Protocol: protocolTelnet}, sessionlog.ProtocolTelnet},
		{"serial", Host{Label: "Console", Address: "/dev/ttyUSB0", Protocol: protocolSerial}, sessionlog.ProtocolSerial},
		{"vnc", Host{Label: "Desktop", Address: "desk.example.com", Protocol: protocolVNC}, sessionlog.ProtocolVNC},
		{"rdp", Host{Label: "Windows", Address: "win.example.com", Protocol: protocolRDP}, sessionlog.ProtocolRDP},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sessionLogProtocol(test.host); got != test.protocol {
				t.Fatalf("protocol = %d, want %d", got, test.protocol)
			}
			if name := sessionLogHostName(test.host); name != test.host.Label {
				t.Fatalf("host name = %q, want %q", name, test.host.Label)
			}
		})
	}
	if got := sessionLogHostName(Host{Address: "bare.example.com"}); got != "bare.example.com" {
		t.Fatalf("a host without a label = %q", got)
	}
	if got := sessionLogText("bad\x00name\r\nwith\x1bbytes"); strings.ContainsAny(got, "\x00\r\n\x1b") {
		t.Fatalf("stored display text keeps control characters: %q", got)
	}
}

// assertSessionLogFileOmits checks the database file on disk: nothing the
// connection reported may be stored in it.
func assertSessionLogFileOmits(t *testing.T, state *State, secrets ...string) {
	t.Helper()
	data, err := os.ReadFile(state.sessionLogPath())
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	for _, secret := range secrets {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("the journal contains %q", secret)
		}
	}
}

func TestSessionLogsUnavailableDatabaseDoesNotBlockConnections(t *testing.T) {
	dir := t.TempDir()
	// A directory where the journal file should be makes Open fail the same way a
	// permission or filesystem problem would.
	if err := os.Mkdir(filepath.Join(dir, "session-logs.db"), 0o700); err != nil {
		t.Fatal(err)
	}
	options := Options{
		KnownHostsPath:        filepath.Join(dir, "known_hosts"),
		StatePath:             filepath.Join(dir, "state.json"),
		Keystore:              &memKeys{},
		PreferredLanguages:    func() []string { return nil },
		SystemAppearance:      func() app.Appearance { return app.AppearanceUnknown },
		WatchSystemAppearance: func(func()) func() { return func() {} },
		SetNativeAppearance:   func(app.Appearance) {},
		WallNow:               func() time.Time { return time.Date(2026, 10, 30, 8, 30, 0, 0, time.UTC) },
	}
	world := &world{}
	options.Dial = func(config sshconn.Config) Connection {
		fake := &fakeConn{}
		world.conns = append(world.conns, fake)
		return fake
	}
	if err := saveState(options.StatePath, savedState{Hosts: sampleHosts}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if state.sessionLogs.store != nil || !state.sessionLogs.view.unavailable {
		t.Fatalf("a journal that cannot open is still available: store %v view %v", state.sessionLogs.store, state.sessionLogs.view.unavailable)
	}
	connection := connect(t, state, world)
	connection.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	if state.sessions[0].term == nil || state.sessions[0].term.shell == nil {
		t.Fatal("an unreadable journal blocked the SSH session")
	}
	state.activeSection = sectionSessionLogs
	state.openSessionLogs()
	_, texts := paintedTexts(state)
	if !contains(texts, state.messages.SessionLogs.Unavailable) {
		t.Fatal("the page does not say that records are unavailable")
	}
	state.closeSession(state.sessions[0].ID)
}

func TestSessionLogsContainerTabIsOneActiveRow(t *testing.T) {
	now := time.Duration(0)
	_, options := sessionLogTestOptions(t, &now)
	state := newTestState(t, options)
	host := state.hosts[0]
	connection := &fakeConn{}
	connection.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	session := state.createHostSessionWithConnection(host, host.Address, connection, "docker exec -it app sh", nil)
	if session == nil {
		t.Fatal("the container tab was not created")
	}
	rows := sessionLogRows(t, state)
	if len(rows) != 1 || rows[0].Protocol != sessionlog.ProtocolSSH || rows[0].Status != sessionlog.StatusActive {
		t.Fatalf("container rows = %+v", rows)
	}
	state.closeSession(session.ID)
	rows = sessionLogRows(t, state)
	if len(rows) != 1 || rows[0].Status != sessionlog.StatusEnded {
		t.Fatalf("container row after closing = %+v", rows)
	}
}
