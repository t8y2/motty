package workbench

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/cli"
	"motty/internal/ai/store"
	"motty/internal/localshell"
	"motty/internal/term"
)

var (
	hitAICLILogin         = tree.ID(aiSettingsHits, "ai-cli-login")
	hitAICLIDevice        = tree.ID(aiSettingsHits, "ai-cli-device-login")
	hitAICLIRecheck       = tree.ID(aiSettingsHits, "ai-cli-recheck")
	hitAICLILoginTerminal = tree.ID(aiSettingsHits, "ai-cli-login-terminal")
)

type aiCLILogin struct {
	provider store.Provider
	term     *terminal
	timer    *time.Timer
	cancel   context.CancelFunc
	release  func()
	cleanup  func()
	dir      string
	finished bool
	geometry terminalGeometry
}
type aiCLIAuthCheck struct {
	provider store.Provider
	cancel   context.CancelFunc
}

func (s *State) aiSetAuth(id string, status cli.AuthState) {
	if s.ai.auth == nil {
		s.ai.auth = make(map[string]cli.AuthState)
	}
	s.ai.auth[id] = status
}

// aiResetCLIAuth cancels an in-flight status check and clears the cached status
// of the old executable. A path edit must not leave "checking" behind, because
// that state disables the Check status action.
func (s *State) aiResetCLIAuth(id string) {
	if check := s.ai.authChecks[id]; check != nil {
		check.cancel()
		delete(s.ai.authChecks, id)
	}
	s.aiSetAuth(id, cli.AuthUnknown)
}

func (s *State) aiCheckCLIAuth(p store.Provider) {
	if !store.IsCLIKind(p.Kind) {
		return
	}
	if s.ai.authChecks == nil {
		s.ai.authChecks = make(map[string]*aiCLIAuthCheck)
	}
	if old := s.ai.authChecks[p.ID]; old != nil {
		old.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	check := &aiCLIAuthCheck{provider: p, cancel: cancel}
	s.ai.authChecks[p.ID] = check
	s.aiSetAuth(p.ID, cli.AuthChecking)
	go func() {
		status, err := cli.CheckAuth(ctx, p.Kind, p.CLIPath)
		s.aiPost(func(s *State) {
			if s.ai.authChecks[p.ID] != check {
				return
			}
			delete(s.ai.authChecks, p.ID)
			cancel()
			current, ok := s.aiProviderByID(p.ID)
			if !ok {
				return
			}
			if current.Kind != p.Kind || current.CLIPath != p.CLIPath {
				// The executable changed while this check ran. If another check
				// has already started, leave its Checking state alone; otherwise
				// clear the stale state so the action becomes reachable again.
				if s.ai.authChecks[p.ID] == nil {
					s.aiSetAuth(p.ID, cli.AuthUnknown)
				}
				return
			}
			s.aiSetAuth(p.ID, status)
			if err != nil {
				s.ai.cfg.notice = s.aiCLIError(err.Error())
			}
		})
	}()
}
func (s *State) aiStartCLILogin(id string, device bool) {
	if s.ai.login != nil {
		return
	}
	p, ok := s.aiProviderByID(id)
	if !ok || !store.IsCLIKind(p.Kind) {
		return
	}
	s.aiCloseDialog()
	ctx, cancel := context.WithCancel(context.Background())
	login := &aiCLILogin{provider: p, term: newTerminalWithScrollback(90, 16, 200), cancel: cancel}
	s.reportTerminalColors(login.term)
	login.term.screen.SetImageWake(func() {
		if s.options.Invalidate != nil {
			s.options.Invalidate()
		}
	})
	s.ai.login = login
	s.ai.cfg.dialog = aiDialogCLILogin
	s.aiSetAuth(id, cli.AuthSigningIn)
	login.timer = time.AfterFunc(15*time.Minute, func() {
		cancel()
		s.aiPost(func(s *State) {
			if s.ai.login != login || login.finished {
				return
			}
			s.aiStopCLILogin()
			s.aiSetAuth(id, cli.AuthFailed)
			s.ai.cfg.notice = s.messages.AISettings.CLIAuthTimeout
		})
	})
	start := s.options.CLIAuthStart
	go func() {
		release, err := cli.ReserveLogin(p.Kind, p.CLIPath)
		if err != nil {
			s.aiPost(func(s *State) {
				if s.ai.login == login {
					s.aiStopCLILogin()
					s.ai.cfg.notice = s.aiCLIError(err.Error())
				}
			})
			return
		}
		command, err := cli.LoginCommand(p.Kind, p.CLIPath, device)
		var shell Shell
		dir := ""
		if err == nil {
			dir, err = os.MkdirTemp("", "motty-cli-login-")
		}
		if err == nil && ctx.Err() == nil {
			config := localshell.Config{Shell: command.Program, Args: command.Args, Dir: dir, Cols: 90, Rows: 16, Notify: func() { s.aiPost(func(s *State) { s.aiPumpCLILogin() }) }}
			if start != nil {
				shell, err = start(config)
			} else {
				shell, err = localshell.Start(config)
			}
		}
		var cleanupOnce sync.Once
		cleanup := func() {
			cleanupOnce.Do(func() {
				if shell != nil {
					shell.Close()
				}
				if dir != "" {
					_ = os.RemoveAll(dir)
				}
				release()
			})
		}
		// A queued inbox callback may be discarded when its window closes.
		// Cancellation owns the process even before the UI adopts it.
		context.AfterFunc(ctx, cleanup)
		accepted := s.aiPost(func(s *State) {
			current, exists := s.aiProviderByID(p.ID)
			if s.ai.login != login || !exists || current.Kind != p.Kind || current.CLIPath != p.CLIPath || ctx.Err() != nil {
				if s.ai.login == login {
					s.aiStopCLILogin()
				}
				cleanup()
				return
			}
			if err != nil || shell == nil {
				s.aiStopCLILogin()
				s.aiSetAuth(id, cli.AuthFailed)
				s.ai.cfg.notice = s.messages.AISettings.CLIAuthFailed
				cleanup()
				return
			}
			login.dir, login.release = dir, release
			login.cleanup = cleanup
			login.term.shell = shell
			// A login TUI may ask the terminal for DA1, cursor-position or color
			// reports. The normal terminal answers them; this private terminal must
			// too, or the provider waits forever for a reply. Do not wire the
			// clipboard or notification callbacks into a sensitive terminal.
			login.term.screen.SetReply(func(reply []byte) {
				if s.ai.login == login && !login.finished {
					shell.Write(reply)
				}
			})
			s.aiPumpCLILogin()
		})
		if !accepted {
			cancel()
			cleanup()
		}
	}()
}
func (s *State) aiPumpCLILogin() {
	login := s.ai.login
	if login == nil {
		return
	}
	login.term.screen.ApplyImages()
	current, ok := s.aiProviderByID(login.provider.ID)
	if !ok || current.Kind != login.provider.Kind || current.CLIPath != login.provider.CLIPath {
		s.aiStopCLILogin()
		return
	}
	if login.term.shell == nil || login.finished {
		return
	}
	t := login.term
	t.rx = t.shell.Drain(t.rx[:0])
	if len(t.rx) > 0 {
		t.screen.Write(t.rx)
		clear(t.rx)
		t.rx = t.rx[:0]
	}
	select {
	case <-t.shell.Done():
		login.finished = true
		login.timer.Stop()
		if login.release != nil {
			login.release()
		}
		if t.shell.Err() != nil {
			s.aiSetAuth(login.provider.ID, cli.AuthFailed)
			s.ai.cfg.notice = s.messages.AISettings.CLIAuthFailed
		} else {
			s.aiCheckCLIAuth(login.provider)
		}
	default:
	}
}
func (s *State) aiStopCLILogin() {
	login := s.ai.login
	if login == nil {
		return
	}
	s.ai.login = nil
	login.cancel()
	if login.timer != nil {
		login.timer.Stop()
	}
	if login.cleanup != nil {
		login.cleanup()
	} else if login.term.shell != nil {
		login.term.shell.Close()
	}
	if login.release != nil {
		login.release()
	}
	if !login.finished {
		s.aiSetAuth(login.provider.ID, cli.AuthCanceled)
	}
	login.term = nil
	if s.ai.cfg.dialog == aiDialogCLILogin {
		s.ai.cfg.dialog = aiDialogNone
	}
}
func (s *State) aiCLILoginKey(event ui.KeyEvent) ui.EventResult {
	login := s.ai.login
	if login != nil && event.Target == hitAICLILoginTerminal && login.term.shell != nil && !login.finished {
		if event.Modifiers&ui.ModifierMeta != 0 {
			return ui.EventResult{}
		}
		input := term.EncodeKeyEvent(event, login.term.screen.KeyboardFlags(), login.term.screen.Modes().AppCursorKeys)
		login.term.shell.Write(input)
	}
	return ui.EventResult{Handled: true, Repaint: true}
}
func (s *State) aiCLILoginInput(context app.TextInputContext) *app.TextInput {
	login := s.ai.login
	if login == nil || !s.aiSettingsVisible() || s.ai.cfg.dialog != aiDialogCLILogin || login.finished {
		return nil
	}
	t := login.term
	c := t.screen.Cursor()
	t.input.Bounds = ui.Rect{X: login.geometry.left + float32(c.X)*s.cellWidth, Y: login.geometry.top + float32(c.Y)*s.cellHeight, Width: s.cellWidth, Height: s.cellHeight}
	t.input.FontSize = s.terminalFontSize
	t.input.MonoFace = s.terminalFontFace
	t.input.Color = s.termPal.termFG
	t.input.State = t.preedit
	t.input.OnEvent = func(e app.TextEvent) (app.TextState, error) {
		if s.ai.login != login || t.shell == nil || login.finished {
			return app.TextState{}, nil
		}
		switch e.Kind {
		case app.TextInsert, app.TextPaste:
			t.preedit = app.TextState{}
			input := []byte(e.Text)
			if e.Kind == app.TextPaste {
				input = term.EncodePaste(e.Text, t.screen.Modes().BracketedPaste)
			}
			t.shell.Write(input)
		case app.TextSetMarked:
			t.preedit = app.TextState{Value: e.Text, Selection: e.MarkedSelection, Composition: app.TextRange{End: utf8.RuneCountInString(e.Text)}, Composing: e.Text != ""}
		case app.TextUnmark:
			t.preedit = app.TextState{}
		}
		result := t.preedit
		result.Redraw = true
		return result, nil
	}
	return &t.input
}
func (s *State) paintAICLILogin(canvas *ui.Canvas, viewport ui.Rect) {
	login := s.ai.login
	if login == nil {
		return
	}
	m := s.messages.AISettings
	height := min(float32(420), max(float32(180), viewport.Height-230))
	dialog := components.BeginDialog(canvas, s.Theme(), viewport, components.DialogOptions{Layer: dialogLayer, Scope: dialogScope, DismissID: hitAISetFormDismiss, CloseID: hitAISetFormClose, InitialFocusID: hitAICLILoginTerminal, ContentSize: ui.Size{Width: min(float32(920), max(float32(320), viewport.Width-40))}, FitBody: true, BodyHeight: height})
	layout := dialog.Begin(m.CLIAuthTitle+" · "+login.provider.Name, m.CLIAuthSensitive)
	area := layout.Body
	cols := max(20, int(area.Width/max(s.cellWidth, 1)))
	rows := max(5, int(area.Height/max(s.cellHeight, 1)))
	if c, r := login.term.screen.Size(); c != cols || r != rows {
		login.term.screen.Resize(cols, rows)
		if login.term.shell != nil {
			login.term.shell.Resize(cols, rows)
		}
	}
	login.geometry = terminalGeometry{area: area, left: area.X, top: area.Y, cols: cols, rows: rows}
	canvas.Fill(area, s.termPal.terminal)
	s.paintGridWithID(canvas, &Session{Title: m.CLIAuthTitle, Address: m.CLIAuthTitle, term: login.term}, login.geometry, hitAICLILoginTerminal, true)
	label := s.messages.Cancel
	if login.finished {
		label = m.FormSave
	}
	dialog.Actions(layout, components.ButtonAction{ID: hitAISetFormCancel, Label: label, Variant: components.ButtonSecondary})
	dialog.End()
}
func (s *State) paintAICLIAuth(page *settingsPage) {
	p, ok := s.aiSelectedProvider()
	if !ok || !store.IsCLIKind(p.Kind) {
		return
	}
	m := s.messages.AISettings
	status := m.CLIAuthUnknown
	switch s.ai.auth[p.ID] {
	case cli.AuthSignedOut:
		status = m.CLIAuthSignedOut
	case cli.AuthConfigured:
		status = m.CLIAuthConfigured
	case cli.AuthChecking:
		status = m.CLIAuthChecking
	case cli.AuthSigningIn:
		status = m.CLIAuthSigningIn
	case cli.AuthFailed:
		status = m.CLIAuthFailed
	case cli.AuthCanceled:
		status = m.CLIAuthCanceled
	}
	page.group(m.CLIAuthTitle, func() {
		page.aiFrame(settingsRowHeight, p.Name, status, s.pal.muted, func() {
			// The authentication controls are actions, not flexible labels. If
			// they shrink with the card, the primary Sign in button disappears on
			// ordinary narrow windows; keep their intrinsic width and let the
			// status words wrap instead.
			page.f.Row().Gap(8).Shrink(0).Children(func() {
				page.button(hitAICLIRecheck, m.CLIAuthRecheck, aiButtonOutline, s.ai.auth[p.ID] == cli.AuthChecking)
				if p.Kind == store.KindCodexCLI {
					page.button(hitAICLIDevice, m.CLIAuthDevice, aiButtonOutline, s.ai.login != nil)
				}
				page.button(hitAICLILogin, m.CLIAuthLogin, aiButtonPrimary, s.ai.login != nil)
			})
		})
	})
}
func (s *State) aiCLIError(message string) string {
	if !strings.HasPrefix(message, "cli:") {
		return message
	}
	m := s.messages.AISettings
	if diagnostic, ok := cli.ParseCompatibilityDiagnostic(message); ok {
		var reason string
		switch diagnostic.Reason {
		case "unavailable":
			reason = m.CLIErrCheckUnavailable
		case "invalid_output":
			reason = m.CLIErrCheckInvalid
		case "missing_capability":
			reason = fmt.Sprintf(m.CLIErrCapabilityFmt, diagnostic.Detail)
		case "not_applied":
			reason = fmt.Sprintf(m.CLIErrIsolationFmt, diagnostic.Detail)
		}
		if reason != "" {
			if diagnostic.Version == "" {
				diagnostic.Version = m.CLIVersionUnknown
			}
			name := strings.TrimSpace(diagnostic.Program + " " + diagnostic.Version)
			return fmt.Sprintf(m.CLIErrCompatibilityFmt, name, reason, diagnostic.Command)
		}
	}
	switch strings.TrimSpace(strings.TrimPrefix(message, "cli:")) {
	case "not_installed", "invalid_path", "node_required", "unsupported_launcher":
		return m.CLIErrInstall
	case "unsupported_version", "invalid_version", "unsafe_config_unverifiable":
		return m.CLIErrVersion
	case "login_required":
		return m.CLIErrLogin
	default:
		return fmt.Sprintf(m.CLIErrRunFmt, strings.TrimPrefix(message, "cli: "))
	}
}
