package workbench

import (
	"errors"
	"testing"

	"github.com/vivaui/viva/app"
	"motty/internal/sshconn"
)

func startupInputSession(t *testing.T) (*State, *Session) {
	t.Helper()
	state := newTestState(t, testOptions(t))
	ids := state.restoreSessions([]savedSession{{Local: true}}, 1)
	state.restoreWorkspaceDeck(nil, ids)
	session := &state.sessions[0]
	conn := newLocalConn("", networkProxyDirect)
	conn.backgroundShell = true
	session.conn = conn
	session.restored = false
	session.phase = sshconn.PhaseConnected
	session.term = newTerminal(80, 24)
	state.configureTerminal(session)
	session.term.opening = make(chan shellResult, 1)
	state.activeSection, state.activeSessionID = sectionTerminal, session.ID
	state.localStartupCommands = map[string]string{localStartupKey(""): "echo ready"}
	return state, session
}

func TestStartupInputDeliversCommittedTextOnceToThePreparedShell(t *testing.T) {
	state, session := startupInputSession(t)
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextSetMarked, Text: "ni"})
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "你"})
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "好"})
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "\r"})
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextPaste, Text: "paste"})
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextSetMarked, Text: "ma"})

	shell := &fakeShell{}
	session.term.opening <- shellResult{shell: shell}
	state.collectShell(session)
	state.collectShell(session)
	if got := shell.typed(); got != "你好" {
		t.Fatalf("shell received %q, want only the two committed characters once", got)
	}
	if preedit := session.term.preedit; !preedit.Composing || preedit.Value != "ma" {
		t.Fatalf("attaching the shell changed the next composition: %+v", preedit)
	}
	if session.term.login.line != "" {
		t.Fatal("early typing must take precedence over the local startup command")
	}
}

func TestStartupInputNeverReachesAReplacementShell(t *testing.T) {
	for _, failed := range []bool{false, true} {
		state, session := startupInputSession(t)
		state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "私"})
		pending := session.term.opening
		if failed {
			pending <- shellResult{err: errors.New("shell did not open")}
			state.collectShell(session)
		} else {
			session.term.abandonShell()
			pending <- shellResult{}
		}
		state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "旧"})
		shell := &fakeShell{}
		state.shellOpened(session, shell, nil)
		if got := shell.typed(); got != "" {
			t.Fatalf("failed=%v: replacement shell received old input %q", failed, got)
		}
	}
}
