package workbench

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/ui"
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
	line string
	last time.Duration
	seen bool
}

func (login *loginCommand) arm(line string) { *login = loginCommand{line: line} }

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

// runLoginCommand types the login command when the shell is ready for it.
func (state *State) runLoginCommand(session *Session) {
	t := session.term
	if t == nil || t.login.line == "" || t.shell == nil || t.ended || t.screen.Modes().AltScreen {
		return
	}
	now := state.now()
	switch {
	case t.promptReady && !t.promptDirty:
	case !t.login.seen || t.folderIntegrationSeen:
		// Nothing has been said yet, or the integration is working and will say
		// when the prompt is ready.
		return
	case t.login.quietWait(now, state.loginQuiet(session)) > 0:
		return
	}
	line := t.login.line
	t.login = loginCommand{}
	t.sendAssistant([]byte(line + "\r"))
}

// requestLoginCommandFrames asks for the frame that lets the quiet rule apply to
// a shell whose output has stopped. A terminal with no login command waiting
// asks for nothing.
func (state *State) requestLoginCommandFrames(canvas *ui.Canvas) {
	for index := range state.sessions {
		session := &state.sessions[index]
		t := session.term
		if t == nil || t.login.line == "" || !t.login.seen || t.folderIntegrationSeen || t.shell == nil {
			continue
		}
		canvas.RequestFrameIn(max(t.login.quietWait(state.now(), state.loginQuiet(session)), time.Millisecond))
	}
}
