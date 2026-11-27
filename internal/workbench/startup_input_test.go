package workbench

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
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

func TestStartupBackspaceEditsTheLastCommittedCharacter(t *testing.T) {
	state, session := startupInputSession(t)
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "a你e\u0301"})
	state.terminalKey(session, gfx.KeyEvent{Key: gfx.KeyBackspace})
	state.terminalKey(session, gfx.KeyEvent{Key: gfx.KeyBackspace, Release: true})
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "c"})
	shell := &fakeShell{}
	session.term.opening <- shellResult{shell: shell}
	state.collectShell(session)
	if got := shell.typed(); got != "a你c" {
		t.Fatalf("shell received %q after Backspace, want a你c", got)
	}
}

func TestStartupInputIsDiscardedWhenFindTakesFocus(t *testing.T) {
	state, session := startupInputSession(t)
	driver, err := app.NewDriver(state.WindowConfig(app.Config{
		Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnPointer: state.Pointer,
		TextInput: state.TerminalFindInput,
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	if err := driver.Type("旧"); err != nil {
		t.Fatal(err)
	}
	find := state.shortcuts.chords.Chord(shortcutFind)
	if err := driver.Chord(find.Modifiers, string(find.Char)); err != nil {
		t.Fatal(err)
	}
	if editor := driver.Snapshot().Editor; editor == nil || editor.Terminal {
		t.Fatal("Find did not take keyboard focus")
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	shell := &fakeShell{}
	session.term.opening <- shellResult{shell: shell}
	state.collectShell(session)
	if got := shell.typed(); got != "新" {
		t.Fatalf("shell received %q after focus returned, want only the new text", got)
	}
}

func TestStartupShellFallbackDropsInputForTheMissingProgram(t *testing.T) {
	state := newTabState(t)
	closeShells(t, state)
	state.startupEnabled, state.restorePending = true, true
	state.startupShell = filepath.Join(t.TempDir(), "removed-shell")
	state.localShell, state.localShellMenu.loaded = "", true
	state.localShellMenu.choices = nil
	state.PrepareInitialLocalSessions()
	session := &state.sessions[0]
	session.term.recording.start(state.now())
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "旧"})
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextSetMarked, Text: "ni"})
	state.Wake()
	waitUntil(t, state, "fallback shell attachment", func() bool { return session.term.shell != nil || session.err != nil })
	if session.err != nil {
		t.Fatal(session.err)
	}
	if session.host.localShell != "" || session.term.preedit.Composing {
		t.Errorf("fallback kept the selected program or its composition: %q, %+v", session.host.localShell, session.term.preedit)
	}
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "新"})
	var typed string
	for _, e := range session.term.recording.events {
		if e.direction == terminalRecordingInput {
			typed += string(e.data)
		}
	}
	// The terminal can also answer protocol queries from a real shell.
	if strings.Contains(typed, "旧") || strings.Count(typed, "新") != 1 {
		t.Fatalf("fallback shell input = %q; want the new text once and none of the old text", typed)
	}
}
