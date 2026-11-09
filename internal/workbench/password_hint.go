package workbench

import (
	"regexp"
	"strings"

	"github.com/vivaui/viva/gfx"
)

// Password prompt assist: when sudo asks for the account's password, the saved
// one is offered as dim text after the prompt, and Enter sends it. Nothing is
// sent without that Enter, and anything else the user types takes the offer
// back. su is left out: its prompt is bare and asks for root's password, which
// is not the one saved for the host.
//
// Ownership: the prompt comes from the emulator's screen and the password from
// the vault, both application state. The text is drawn by the same ghost path
// as command suggestions.

var passwordHintHits = tree.Feature("password-hint")

var hitSettingsPasswordHintToggle = tree.ID(passwordHintHits, "settings-password-hint")

var sudoPromptRe = regexp.MustCompile(sudoPromptPattern)

func (state *State) setPasswordHint(on bool) bool {
	if state.passwordHintOff == !on {
		return false
	}
	state.passwordHintOff = !on
	return true
}

// notePasswordPrompt looks at the line under the cursor after output arrived and
// offers the saved password when it is sudo's prompt.
func (state *State) notePasswordPrompt(session *Session) {
	t := session.term
	if state.passwordHintOff || t.shell == nil || t.ended || t.screen == nil || session.endpoint.Host == "" || state.broadcast != 0 {
		t.passwordHint = ""
		return
	}
	cursor := t.screen.Cursor()
	_, rows := t.screen.Size()
	if t.screen.Modes().AltScreen || t.screen.ViewOffset() != 0 || cursor.Y < 0 || cursor.Y >= rows {
		t.passwordHint = ""
		return
	}
	line := t.screen.RowText(cursor.Y)
	if len(line) < len("[sudo]") || !strings.EqualFold(line[:len("[sudo]")], "[sudo]") || !sudoPromptRe.MatchString(line) {
		t.passwordHint = ""
		return
	}
	if line == t.passwordHint {
		return
	}
	if _, result := state.vaultSecret(state.passwordName(session)); result != vaultFound {
		t.passwordHint = ""
		return
	}
	t.passwordHint = line
}

// passwordHintActive reports whether the offer still stands: the prompt is
// still the line under the cursor.
func (state *State) passwordHintActive(session *Session) bool {
	t := session.term
	if t == nil || t.passwordHint == "" || t.screen == nil || state.passwordHintOff {
		return false
	}
	cursor := t.screen.Cursor()
	_, rows := t.screen.Size()
	return cursor.Y >= 0 && cursor.Y < rows && !t.screen.Modes().AltScreen && t.screen.RowText(cursor.Y) == t.passwordHint
}

// passwordHintGhost is the text drawn after the prompt while the offer stands.
func (state *State) passwordHintGhost(session *Session) (string, bool) {
	if !state.passwordHintActive(session) {
		return "", false
	}
	return state.messages.Tools.PasswordHintGhost, true
}

// passwordHintKey takes the Enter that accepts the offer.
func (state *State) passwordHintKey(session *Session, event gfx.KeyEvent) bool {
	if event.Key != gfx.KeyEnter || event.Modifiers != 0 || event.Repeat || !state.passwordHintActive(session) {
		return false
	}
	value, result := state.vaultSecret(state.passwordName(session))
	if result != vaultFound {
		session.term.passwordHint = "" // the Enter is the user's own, as without an offer
		return false
	}
	return session.term.sendSecret(append([]byte(value), '\r'))
}
