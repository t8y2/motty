package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func passwordHintSession(t *testing.T) (*State, *Session, *fakeShell) {
	t.Helper()
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	if err := state.secrets.vault.Set(state.passwordName(session), "hunter2"); err != nil {
		t.Fatal(err)
	}
	return state, session, shell
}

func TestEnterSendsTheSavedPasswordAtASudoPrompt(t *testing.T) {
	state, session, shell := passwordHintSession(t)
	shell.say("$ sudo ls\r\n[sudo] password for dev: ")
	state.Wake()
	if rest, ok := state.passwordHintGhost(session); !ok || rest != state.messages.Tools.PasswordHintGhost {
		t.Fatalf("no offer at the prompt: %q %v", rest, ok)
	}
	if shell.typed() != "" {
		t.Fatalf("the password was sent before Enter: %q", shell.typed())
	}
	state.terminalKey(session, gfx.KeyEvent{Key: gfx.KeyEnter})
	if got := shell.typed(); got != "hunter2\r" {
		t.Fatalf("typed %q", got)
	}
	if _, ok := state.passwordHintGhost(session); ok {
		t.Fatal("the offer outlived the answer")
	}
}

func TestTypingTakesTheSudoOfferBack(t *testing.T) {
	state, session, shell := passwordHintSession(t)
	shell.say("[sudo] password for dev: ")
	state.Wake()
	state.typed(session, []byte("x"))
	state.terminalKey(session, gfx.KeyEvent{Key: gfx.KeyEnter})
	if got := shell.typed(); got != "x\r" {
		t.Fatalf("typed %q: the saved password was added to what the user typed", got)
	}
}

func TestNoOfferWithoutASudoPromptASavedPasswordOrTheSetting(t *testing.T) {
	state, session, shell := passwordHintSession(t)
	shell.say("Password: ")
	state.Wake()
	if _, ok := state.passwordHintGhost(session); ok {
		t.Fatal("a bare Password: prompt was offered the host password")
	}

	state.setPasswordHint(false)
	shell.say("\r\n[sudo] password for dev: ")
	state.Wake()
	if _, ok := state.passwordHintGhost(session); ok {
		t.Fatal("the offer ignored the setting")
	}

	state.setPasswordHint(true)
	if err := state.secrets.vault.Delete(state.passwordName(session)); err != nil {
		t.Fatal(err)
	}
	shell.say("\r\n[sudo] password for dev: ")
	state.Wake()
	if _, ok := state.passwordHintGhost(session); ok {
		t.Fatal("offered a password that is not saved")
	}
}
