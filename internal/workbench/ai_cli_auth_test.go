package workbench

import (
	"errors"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/agent"
	"motty/internal/ai/cli"
	"motty/internal/ai/store"
	"motty/internal/localshell"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCLILoginIsExplicitLocalSensitiveAndCancelable(t *testing.T) {
	w := newSettingsWorld(t, newLister("default"))
	s := w.state
	path, _ := os.Executable()
	db, _ := s.aiStore()
	if err := db.SaveProvider(store.Provider{ID: "auth", Name: "Auth", Kind: "codex-cli", CLIPath: path, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	s.reloadAI()
	starts := 0
	shell := &fakeShell{done: make(chan struct{})}
	s.options.CLIAuthStart = func(c localshell.Config) (Shell, error) {
		starts++
		if c.Shell != path || strings.Join(c.Args, " ") != "login" {
			t.Error("wrong local command", c.Shell, c.Args)
		}
		return shell, nil
	}
	w.paint()
	if starts != 0 {
		t.Fatal("paint started login")
	}
	before := len(s.sessions)
	s.aiStartCLILogin("auth", false)
	s.aiStartCLILogin("auth", false)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.Wake()
		if s.ai.login != nil && s.ai.login.term.shell != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if starts != 1 || len(s.sessions) != before || s.ai.login == nil {
		t.Fatal("not isolated/duplicate login", starts)
	}
	shell.say("PRIVATE_AUTH_URL CODE=1234")
	s.Wake()
	if !strings.Contains(s.ai.login.term.screen.Text(), "PRIVATE_AUTH_URL") {
		t.Fatal("login output not displayed")
	}
	if strings.Contains(s.aiContext(), "PRIVATE_AUTH_URL") || strings.Contains(s.aiSessionSummary(), "Auth") {
		t.Fatal("auth output leaked to AI")
	}
	input := s.TerminalInput(app.TextInputContext{ID: hitAICLILoginTerminal, Terminal: true})
	if input == nil {
		t.Fatal("login has no native terminal input")
	}
	input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "code"})
	s.aiSettingsKey(ui.KeyEvent{Target: hitAICLILoginTerminal, Key: ui.KeyEnter})
	if string(shell.input) != "code\r" {
		t.Fatal("interactive input not delivered")
	}
	space := nativeSpaceKey()
	space.Target = hitAICLILoginTerminal
	s.aiSettingsKey(space)
	input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: " "})
	if got := string(shell.input); got != "code\r " {
		t.Fatalf("login Space was delivered twice: %q", got)
	}
	s.aiStopCLILogin()
	if !shell.closed || s.ai.login != nil {
		t.Fatal("login not closed")
	}
	if len(s.ai.history) != 0 || s.ai.run != nil {
		t.Fatal("auth replayed model request")
	}
}
func TestDeletingProviderCancelsSensitiveLogin(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	s := w.state
	path, _ := os.Executable()
	db, _ := s.aiStore()
	db.SaveProvider(store.Provider{ID: "auth", Kind: "codex-cli", CLIPath: path, Enabled: true})
	s.reloadAI()
	shell := &fakeShell{done: make(chan struct{})}
	s.options.CLIAuthStart = func(localshell.Config) (Shell, error) { return shell, nil }
	s.aiStartCLILogin("auth", false)
	for i := 0; i < 1000; i++ {
		s.Wake()
		if s.ai.login != nil && s.ai.login.term.shell != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	s.aiDeleteProvider("auth")
	s.Wake()
	if s.ai.login != nil || !shell.closed {
		t.Fatal("provider deletion left login alive")
	}
}

func TestLoginPasteShortcutCannotReachActiveSSHSession(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	s := w.state
	remote := &fakeShell{done: make(chan struct{})}
	normal := newTerminal(80, 24)
	normal.shell = remote
	s.sessions = append(s.sessions, Session{ID: 919, term: normal})
	s.activeSessionID = 919
	auth := &fakeShell{done: make(chan struct{})}
	local := newTerminal(80, 24)
	local.shell = auth
	s.ai.login = &aiCLILogin{provider: store.Provider{ID: "auth"}, term: local, cancel: func() {}}
	s.ai.cfg.dialog = aiDialogCLILogin
	s.options.ClipboardText = func() (string, error) { return "sensitive-login-code", nil }
	chord := s.shortcuts.chords[shortcutPaste]
	s.Shortcut(ui.KeyEvent{Target: hitAICLILoginTerminal, Modifiers: chord.Modifiers, Text: string(chord.Key)})
	if remote.typed() != "" {
		t.Fatal("login clipboard was sent to the active SSH session")
	}
	if auth.typed() != "sensitive-login-code" {
		t.Fatal("login terminal did not receive clipboard")
	}
}

func TestCLILoginErrorProvidesExplicitProviderActionWithoutAutoLogin(t *testing.T) {
	w := newSettingsWorld(t, newLister("default"))
	s := w.state
	path, _ := os.Executable()
	db, _ := s.aiStore()
	p := store.Provider{ID: "auth", Name: "CLI", Kind: "codex-cli", CLIPath: path, Enabled: true}
	db.SaveProvider(p)
	db.SaveModels(p.ID, []store.Model{{ProviderID: p.ID, Name: "default", Enabled: true}})
	db.SetDefaultModel(p.ID, "default")
	s.reloadAI()
	run := &aiRun{providerID: p.ID, cancel: func() {}, open: -1}
	s.ai.run = run
	s.ai.open = true
	s.aiEvent(run, agent.Event{Kind: agent.EventError, Text: "cli: login_required"})
	s.aiFinished(run, nil, errors.New("cli: login_required"))
	if s.ai.login != nil || s.ai.needLogin != p.ID {
		t.Fatal("login required state is wrong")
	}
	canvas, _ := w.paint()
	if _, ok := hitByID(canvas, hitAISetup); !ok {
		t.Fatal("no explicit login action in assistant")
	}
	s.options.CLIAuthStart = func(localshell.Config) (Shell, error) { return &fakeShell{done: make(chan struct{})}, nil }
	s.Click(hitAISetup)
	if s.ai.login == nil || s.ai.login.provider.ID != p.ID || s.ai.cfg.selected != p.ID {
		t.Fatal("login action did not target failed provider")
	}
}

func TestSyncDeletionCancelsLoginWhileLocalProcessIsStarting(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	s := w.state
	path, _ := os.Executable()
	db, _ := s.aiStore()
	db.SaveProvider(store.Provider{ID: "pending", Kind: "codex-cli", CLIPath: path, Enabled: true})
	s.reloadAI()
	entered := make(chan struct{})
	resume := make(chan struct{})
	defer close(resume)
	shell := &fakeShell{done: make(chan struct{})}
	s.options.CLIAuthStart = func(localshell.Config) (Shell, error) { close(entered); <-resume; return shell, nil }
	s.aiStartCLILogin("pending", false)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("auth did not start")
	}
	db.DeleteProvider("pending")
	s.reloadAI()
	s.Wake()
	if s.ai.login != nil {
		t.Fatal("synced provider deletion left pending login alive")
	}
}

func TestWindowCloseReclaimsQueuedLoginProcess(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	s := w.state
	path, _ := os.Executable()
	db, _ := s.aiStore()
	db.SaveProvider(store.Provider{ID: "queued", Kind: "codex-cli", CLIPath: path, Enabled: true})
	s.reloadAI()
	shell := &fakeShell{done: make(chan struct{})}
	s.options.CLIAuthStart = func(localshell.Config) (Shell, error) { return shell, nil }
	s.aiStartCLILogin("queued", false)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.ai.inbox.mu.Lock()
		queued := len(s.ai.inbox.queue) > 0
		s.ai.inbox.mu.Unlock()
		if queued {
			break
		}
		time.Sleep(time.Millisecond)
	}
	s.Close()
	for time.Now().Before(deadline) {
		shell.mu.Lock()
		closed := shell.closed
		shell.mu.Unlock()
		if closed {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("window close dropped the queued auth callback and leaked its process")
}

func TestAuthTimeoutStopsProcessWithoutUIWake(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	s := w.state
	path, _ := os.Executable()
	db, _ := s.aiStore()
	db.SaveProvider(store.Provider{ID: "timeout", Kind: "codex-cli", CLIPath: path, Enabled: true})
	s.reloadAI()
	shell := &fakeShell{done: make(chan struct{})}
	s.options.CLIAuthStart = func(localshell.Config) (Shell, error) { return shell, nil }
	s.aiStartCLILogin("timeout", false)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.Wake()
		if s.ai.login != nil && s.ai.login.term.shell != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if s.ai.login == nil || s.ai.login.term.shell == nil {
		t.Fatal("auth not adopted")
	}
	s.ai.login.timer.Reset(time.Millisecond)
	for time.Now().Before(deadline) {
		shell.mu.Lock()
		closed := shell.closed
		shell.mu.Unlock()
		if closed {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("auth timeout waited for the UI inbox before stopping the process")
}

func TestCLILoginAnswersTerminalQueries(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	s := w.state
	path, _ := os.Executable()
	db, _ := s.aiStore()
	if err := db.SaveProvider(store.Provider{ID: "auth", Name: "Auth", Kind: "codex-cli", CLIPath: path, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	s.reloadAI()
	shell := &fakeShell{done: make(chan struct{})}
	s.options.CLIAuthStart = func(localshell.Config) (Shell, error) { return shell, nil }
	s.aiStartCLILogin("auth", false)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.Wake()
		if s.ai.login != nil && s.ai.login.term.shell != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if s.ai.login == nil || s.ai.login.term.shell == nil {
		t.Fatal("login did not start")
	}
	shell.say("\x1b[c")
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.Wake()
		if shell.typed() != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got := shell.typed(); got == "" || got == "\x1b[c" {
		t.Fatalf("login terminal did not answer DA1: %q", got)
	}
	s.aiStopCLILogin()
}

func TestCLIAuthActionsStayReachableInNarrowSettings(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	s := w.state
	path, _ := os.Executable()
	db, _ := s.aiStore()
	if err := db.SaveProvider(store.Provider{ID: "auth", Name: "Auth", Kind: "codex-cli", CLIPath: path, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	s.reloadAI()
	s.aiSelectProvider(0)
	for _, width := range []float32{800, 900, 1000, 1200} {
		var canvas ui.Canvas
		s.Paint(&canvas, ui.Size{Width: width, Height: 900})
		for _, id := range []ui.ID{hitAICLIRecheck, hitAICLIDevice, hitAICLILogin} {
			hit, ok := hitByID(&canvas, id)
			if !ok || hit.Bounds.Width < 20 || hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > width {
				t.Fatalf("width %.0f: auth control %d missing or clipped: %+v, %t", width, id, hit.Bounds, ok)
			}
		}
	}
}

func TestResetCLIAuthClearsACheckingState(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	s := w.state
	canceled := false
	s.ai.authChecks = map[string]*aiCLIAuthCheck{"auth": &aiCLIAuthCheck{cancel: func() { canceled = true }}}
	s.aiSetAuth("auth", cli.AuthChecking)
	s.aiResetCLIAuth("auth")
	if !canceled || s.ai.auth["auth"] != cli.AuthUnknown || s.ai.authChecks["auth"] != nil {
		t.Fatalf("reset auth canceled=%t state=%q checks=%v", canceled, s.ai.auth["auth"], s.ai.authChecks)
	}
}
