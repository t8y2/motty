package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestSudoUsesThePasswordPickerWithoutSharedIdentities(t *testing.T) {
	for _, prompt := range []string{"[sudo] password for dev: ", "[sudo: authenticate] Password: "} {
		t.Run(prompt, func(t *testing.T) {
			w := liveAutofillSession(t)
			session := &w.state.sessions[0]
			session.endpoint.User = "dev"
			if err := w.state.secrets.vault.Set(w.state.passwordName(session), "host-secret"); err != nil {
				t.Fatal(err)
			}
			if w.state.passwordAssistWantsFrame(session) {
				t.Fatal("a host password requested a frame without a password prompt")
			}
			w.output(prompt)
			if !w.state.passwordAssistWantsFrame(session) {
				t.Fatal("the host's own password did not request a prompt scan")
			}
			if len(w.state.autofillRules.pick.candidates) != 1 || w.shell.typed() != "" {
				t.Fatalf("pick %+v, typed %q", w.state.autofillRules.pick, w.shell.typed())
			}
			key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyDown})
			key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyEnter})
			if got := w.shell.typed(); got != "host-secret\r" {
				t.Fatalf("typed %q", got)
			}
		})
	}
}

func TestSudoEnterWithoutSelectingDoesNotSendTheHostPassword(t *testing.T) {
	w := savedPasswordWorld(t)
	session := &w.state.sessions[0]
	if err := w.state.secrets.vault.Set(w.state.passwordName(session), "host-secret"); err != nil {
		t.Fatal(err)
	}
	w.output("[sudo: authenticate] Password: ")
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyEnter})
	if got := w.shell.typed(); got != "\r" {
		t.Fatalf("Enter without a selection typed %q", got)
	}
}
