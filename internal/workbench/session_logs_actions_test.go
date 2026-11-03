package workbench

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/text"
	"motty/internal/sessionlog"
)

func sessionLogActionState(t *testing.T) (*State, sessionlog.Record) {
	t.Helper()
	now := time.Duration(0)
	_, options := sessionLogTestOptions(t, &now)
	state := newTestState(t, options)
	host := state.hosts[0]
	r := sessionlog.Record{ID: "historical", RunID: "old-run", Host: host.Label, Address: host.Address, Account: state.login(host), CatalogID: host.CatalogID, Port: host.Port, Protocol: sessionlog.ProtocolSSH, TargetKind: sessionlog.TargetSavedHost, StartedAt: options.WallNow().Add(-time.Hour)}
	sessionLogsInsert(t, state, r, sessionlog.StatusEnded, sessionlog.ReasonUserClosed, options.WallNow().Add(-time.Minute))
	state.openSessionLogs()
	sessionLogsWait(t, state)
	state.selectSessionLogRecord(r.ID)
	sessionLogsWait(t, state)
	return state, r
}

func TestSessionLogReopenActivatesOtherWindowWithoutConnecting(t *testing.T) {
	a, record := sessionLogActionState(t)
	b := sessionLogsState(t, false)
	group := NewGroup()
	group.members = []*State{a, b}
	a.options.Group, b.options.Group = group, group
	raised := 0
	invalidated := 0
	b.options.Raise = func() { raised++ }
	b.options.Invalidate = func() { invalidated++ }
	b.sessions = []Session{{ID: 7, Title: "live in other window", logRecordID: record.ID}}
	b.ensureWorkspaceDeck()
	a.reopenSessionLogRecord(record.ID)
	if len(a.sessions) != 0 || b.activeSessionID != 7 || b.activeSection != sectionTerminal || raised != 1 {
		t.Fatalf("live record reconnected instead of activating owner: new=%d peer=%d raised=%d", len(a.sessions), b.activeSessionID, raised)
	}
	if invalidated == 0 {
		t.Fatal("activating the owning window did not repaint its terminal")
	}
}

func TestSessionLogLocateRevealsFilteredCollapsedHost(t *testing.T) {
	for _, mode := range []hostViewMode{hostViewList, hostViewGrid} {
		state, record := sessionLogActionState(t)
		host := state.hosts[0]
		host.Group = "hidden-group/region/production"
		state.hosts[0] = host
		state.refreshHostGroups()
		for i := range state.hostGroups {
			state.hostGroups[i].Expanded = false
		}
		state.search = text.Editor{}
		state.search.Insert("does-not-match-any-host")
		state.syncVisible()
		state.hostView = mode
		state.locateSessionLogHost(record.ID)
		canvas := paintedCanvas(t, state)
		hit, ok := sessionLogHit(canvas, hitHostList.At(int(host.ID)))
		if !state.hostIsVisible(host.ID) || !ok || hit.Bounds.Height <= 0 {
			t.Fatalf("located host still hidden for view %d: visible=%v hit=%+v", mode, state.hostIsVisible(host.ID), hit)
		}
		if canvas.FocusRequest() != hitHostList.At(int(host.ID)) {
			t.Fatalf("located host is not focused: %v", canvas.FocusRequest())
		}
	}
}

func TestSessionLogReopenCreatesNewRecord(t *testing.T) {
	state, original := sessionLogActionState(t)
	state.reopenSessionLogRecord(original.ID)
	if len(state.sessions) != 1 || state.sessions[0].logRecordID == original.ID {
		t.Fatalf("reopen did not create a distinct session: %+v", state.sessions)
	}
	rows := sessionLogRows(t, state)
	if len(rows) != 2 {
		t.Fatalf("reopen replaced history: %+v", rows)
	}
}

func TestSessionLogReopenChangedTargetNeedsConfirmation(t *testing.T) {
	state, original := sessionLogActionState(t)
	state.hosts[0].Address = "changed.test"
	state.reopenSessionLogRecord(original.ID)
	if len(state.sessions) != 0 || state.modal != modalForm || !strings.Contains(state.form.description, "changed.test") {
		t.Fatal("changed target connected without an informative confirmation")
	}
	// A second edit while confirming must not connect an unconfirmed target.
	state.hosts[0].Address = "changed-again.test"
	state.submitForm()
	if len(state.sessions) != 0 {
		t.Fatal("confirmation connected a target different from the one shown")
	}
}

func TestSessionLogReopenMissingLegacyAndSharedTargets(t *testing.T) {
	for _, kind := range []sessionlog.TargetKind{sessionlog.TargetUnknown, sessionlog.TargetShared} {
		state, r := sessionLogActionState(t)
		state.sessionLogs.view.rows[0].TargetKind = kind
		state.reopenSessionLogRecord(r.ID)
		if len(state.sessions) != 0 || state.activeSection != sectionHosts {
			t.Fatal("non-reusable record was guessed into a connection")
		}
	}
}

func TestSessionLogMetadataClipboardFailureIsVisible(t *testing.T) {
	state, r := sessionLogActionState(t)
	state.options.SetClipboardText = func(string) error { return errors.New("clipboard unavailable") }
	state.copySessionLogMetadata(r.ID)
	if !state.sessionLogs.view.statusDanger {
		t.Fatal("clipboard failure was hidden")
	}
}

func TestSessionLogMetadataSummaryOmitsSecretsAndTerminalText(t *testing.T) {
	state, r := sessionLogActionState(t)
	state.hosts[0].LoginCommand = "secret-login-command"
	var copied string
	state.options.SetClipboardText = func(s string) error { copied = s; return nil }
	state.copySessionLogMetadata(r.ID)
	deadline := time.Now().Add(3 * time.Second)
	for copied == "" && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	if copied == "" || !strings.Contains(copied, r.Address) || strings.Contains(copied, "secret-login-command") || strings.Contains(copied, "Terminal retained text") {
		t.Fatalf("metadata summary is missing or includes terminal data: %q", copied)
	}
}
