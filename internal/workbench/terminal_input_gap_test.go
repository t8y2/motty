package workbench

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// A keystroke into a tab whose shell is gone used to vanish without a trace,
// which is indistinguishable from a frozen window. The page now says so, and it
// says it once rather than on every key.
func TestTypingIntoATabWithoutAShellSaysTheKeysAreNotSent(t *testing.T) {
	state, _, _, _ := liveSession(t)
	if state.status == state.messages.SessionNoShellInput {
		t.Fatal("the status already names the missing shell before any key")
	}

	session := &state.sessions[0]
	session.term.shell = nil
	session.term.ended = false
	session.term.told = toldNothing

	for _, letter := range []string{"l", "s"} {
		result := key(state, 0, gfx.KeyEvent{Key: gfx.KeyUnknown, Text: letter, Typed: letter})
		if !result.Handled {
			t.Fatalf("%q was not consumed by a tab with no shell", letter)
		}
		if state.status != state.messages.SessionNoShellInput {
			t.Fatalf("typing %q left the status %q, want the missing-shell notice", letter, state.status)
		}
	}
}

// The notice is for the case the screen does not already explain. A shell that
// ended prints its own line, so a key after it must not overwrite that with a
// second explanation.
func TestTypingIntoAnEndedSessionLeavesItsOwnMessageAlone(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	session.term.shell = nil
	session.term.ended = true
	session.term.told = toldEnded
	before := state.status

	key(state, 0, gfx.KeyEvent{Key: gfx.KeyUnknown, Text: "x", Typed: "x"})
	if state.status != before {
		t.Fatalf("a key after the end-of-session line rewrote the status to %q", state.status)
	}
}

// The keyboard dropping out is reported once per cause. A paint loop asks for
// the same surface every frame, and logging each request would push the useful
// lines out of the diagnostics report.
func TestAnInputGapIsLoggedOncePerCause(t *testing.T) {
	state, _, _, _ := liveSession(t)
	var logged bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previous) })

	hit := state.activeTerminalFocusHit()
	session := &state.sessions[0]
	if state.TerminalInput(app.TextInputContext{ID: hit, Terminal: true}) == nil {
		t.Fatal("the live session has no input surface")
	}

	// A session that exists but does not hold the keyboard cannot be typed into.
	active := state.activeSessionID
	state.activeSessionID = session.ID + 1000
	for range 5 {
		if input := state.TerminalInput(app.TextInputContext{ID: hit, Terminal: true}); input != nil {
			t.Fatal("a session that is not the active one was given an input surface")
		}
	}
	if got := strings.Count(logged.String(), "terminal input has no surface"); got != 1 {
		t.Fatalf("the gap was logged %d times, want once:\n%s", got, logged.String())
	}
	if !strings.Contains(logged.String(), "session is not the active one") {
		t.Fatalf("the log does not say why:\n%s", logged.String())
	}
	state.activeSessionID = active

	// A different cause is its own line, so two separate gaps are both visible.
	logged.Reset()
	session.term = nil
	if input := state.TerminalInput(app.TextInputContext{ID: hit, Terminal: true}); input != nil {
		t.Fatal("a session without a terminal was given an input surface")
	}
	if !strings.Contains(logged.String(), "session has no terminal") {
		t.Fatalf("a second cause was not reported:\n%s", logged.String())
	}
}

// A gap that is resolved and then comes back is reported again, so a session
// that recovers and breaks twice leaves two lines behind rather than one.
func TestTheInputGapNoteIsForgottenByTheNextSuccessfulPaint(t *testing.T) {
	state, _, _, _ := liveSession(t)
	var logged bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previous) })

	hit := state.activeTerminalFocusHit()
	original := state.sessions[0].ID
	state.sessions[0].ID = state.activeSessionID + 1000
	state.TerminalInput(app.TextInputContext{ID: hit, Terminal: true})
	state.sessions[0].ID = original
	if input := state.TerminalInput(app.TextInputContext{ID: hit, Terminal: true}); input == nil {
		t.Fatal("the recovered session has no input surface")
	}
	state.sessions[0].ID = state.activeSessionID + 1000
	state.TerminalInput(app.TextInputContext{ID: hit, Terminal: true})

	if got := strings.Count(logged.String(), "terminal input has no surface"); got != 1 {
		t.Fatalf("the same cause was logged %d times across a recovery", got)
	}
}
