package workbench

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
)

// maxLoginCommand bounds the line a host types after login.
const maxLoginCommand = 1000

// validLoginCommand reports whether value can be typed into a shell as one line.
func validLoginCommand(value string) bool {
	if utf8.RuneCountInString(value) > maxLoginCommand {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

// restoredLoginCommand is a saved login command, or none when the saved text
// could not have come from the form.
func restoredLoginCommand(value string) string {
	value = strings.TrimSpace(value)
	if !validLoginCommand(value) {
		return ""
	}
	return value
}

// A host's login command is typed once into each new shell, when the shell is
// ready for input. Readiness is the end of a prompt that Motty's shell
// integration reported; where that never comes (a shell the integration does not
// start, or a server that keeps it from running) the shell is taken as ready once
// its output has been quiet for a while. The line is typed like the user would,
// but it is not the user's own input: it stays out of the command history and
// of the broadcast to other panes.
const (
	// loginCommandQuiet is how long a shell without integration has to be quiet.
	loginCommandQuiet = 400 * time.Millisecond
	// loginCommandGrace is how long a shell that should report its prompt is given
	// to do so before it is treated like one that never will.
	loginCommandGrace = 4 * time.Second
)

// loginCommand is the command a terminal still has to type, and when its shell
// last wrote.
type loginCommand struct {
	line          string
	workspace     string
	paused        bool
	waitPrompt    bool
	loginFirst    bool
	promptVersion uint64
	last          time.Duration
	seen          bool
	// local marks a local shell's startup command. Such a shell reports its
	// prompt from the moment it starts, because the integration is part of its
	// launch, so the first prompt carries no command-start marker and promptReady
	// never turns true before the first command; it is typed at the prompt's end
	// instead.
	local bool
}

func (login *loginCommand) arm(line string) { *login = loginCommand{line: line} }

// armLocal freezes a local shell's startup command.
func (login *loginCommand) armLocal(line string) { *login = loginCommand{line: line, local: true} }

// output notes that the shell wrote something at now, while a command waits.
func (login *loginCommand) output(now time.Duration) {
	if login.line != "" {
		login.last, login.seen = now, true
	}
}

// quietWait reports how long to wait before the quiet rule can apply, or zero when
// it already does.
func (login *loginCommand) quietWait(now, quiet time.Duration) time.Duration {
	return max(login.last+quiet-now, 0)
}

// loginQuiet is the quiet the terminal's shell needs: short when no integration
// was asked of it, longer while the integration may still announce a prompt.
func (state *State) loginQuiet(session *Session) time.Duration {
	if session.term.integrated {
		return loginCommandGrace
	}
	return loginCommandQuiet
}

// runLoginCommand types the initialization command when the shell is ready.
func (state *State) runLoginCommand(session *Session) {
	t := session.term
	if t == nil || t.login.line == "" || t.shell == nil || t.ended {
		return
	}
	if t.screen.Modes().AltScreen {
		if t.login.local {
			t.login = loginCommand{} // startup handed control to a full-screen program
		}
		return
	}
	if t.injection.holding() || t.login.paused {
		return
	}
	if t.login.waitPrompt && (!t.promptReady || t.promptDirty || t.promptVersion <= t.login.promptVersion) {
		return
	}
	now := state.now()
	switch {
	case t.promptReady && !t.promptDirty:
	case t.login.local && t.promptStarted && t.inputLine && !t.promptDirty && t.submittedLines == 0:
		// Locally installed prompt hooks report the first A/B before any C.
		// CMD never reports C. Both are ready for the one startup line, without
		// claiming command-reporting support for other terminal features.
	case !t.login.seen || t.folderIntegrationSeen:
		// Once hooks report, only their prompt end authorizes a command.
		return
	case t.login.quietWait(now, state.loginQuiet(session)) > 0:
		return
	}
	state.sendInitializationCommand(t)
}

// requestLoginCommandFrames schedules the existing quiet fallback only while
// an unreported prompt still has an initialization command waiting.
func (state *State) requestLoginCommandFrames(canvas *gfx.Canvas) {
	for index := range state.sessions {
		session := &state.sessions[index]
		t := session.term
		if t == nil || t.login.line == "" || t.login.paused || t.login.waitPrompt || !t.login.seen || t.folderIntegrationSeen || t.shell == nil || t.ended {
			continue
		}
		canvas.RequestFrameIn(max(t.login.quietWait(state.now(), state.loginQuiet(session)), time.Millisecond))
	}
}

// armInitialization freezes both commands for this shell, independently of later edits.
func (t *terminal) armInitialization(state *State, session *Session) {
	t.login = loginCommand{}
	if session.isLocal() {
		// A local shell's startup command belongs to the shell it runs, whether
		// the tab chose one or the settings' default did; the connection froze
		// that choice when it was made, as it froze the folder.
		if local, ok := session.conn.(*localConn); ok {
			t.login.armLocal(state.localStartupCommand(local.shell))
		}
		return
	}
	if !workspaceCommandHost(session.host) || session.containerCommand != "" {
		return
	}
	command := session.workspaceCommand
	if command.Command == "" {
		t.login.arm(session.host.LoginCommand)
	} else if session.host.LoginCommand != "" {
		t.login.arm(session.host.LoginCommand)
		t.login.workspace = command.Command
		t.login.loginFirst = true
	} else {
		t.login.arm(command.Command)
		t.login.workspace = command.Command
	}
}

func (t *terminal) pauseInitialization() {
	if t.login.local {
		t.login = loginCommand{} // never interleave startup with user/assistant input
	} else if t.login.workspace != "" {
		t.login.paused = true
	}
}

func (state *State) sendInitializationCommand(t *terminal) {
	login := t.login
	if !t.sendAssistant([]byte(login.line + "\r")) {
		t.login.paused = true
		if login.local {
			state.showActionWarning(state.messages.SettingsLocalStartupCommand, state.messages.TerminalInput.SendFailed)
		} else if login.workspace != "" {
			state.showActionWarning(state.messages.WorkspaceCommandField, state.messages.TerminalInput.SendFailed)
		}
		return
	}
	if login.workspace != "" && login.loginFirst {
		t.login = loginCommand{line: login.workspace, workspace: login.workspace, waitPrompt: true, paused: login.paused, promptVersion: t.promptVersion}
	} else {
		t.login = loginCommand{}
	}
}
