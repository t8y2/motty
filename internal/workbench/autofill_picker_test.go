package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/autofill"
)

// savedPasswordWorld is a live session with the terminal in front again, the
// way it is after Settings was used to save a password.
func savedPasswordWorld(t *testing.T) *autofillWorld {
	t.Helper()
	w := liveAutofillSession(t)
	addCredential(t, w.state, "root@cloudpivot", "root-secret")
	addCredential(t, w.state, "deploy", "deploy-secret")
	w.state.activeSection = sectionTerminal
	return w
}

func TestAPasswordPromptOffersSavedPasswordsWithoutARule(t *testing.T) {
	w := savedPasswordWorld(t)
	w.output("Password: ")
	if len(w.state.autofillRules.pick.candidates) == 0 || w.state.modal != modalNone {
		t.Fatalf("the prompt did not offer saved passwords: pick %+v, modal %d", w.state.autofillRules.pick, w.state.modal)
	}
	if got := w.shell.typed(); got != "" {
		t.Fatalf("the offer answered by itself: %q", got)
	}

	// Two Down presses choose the second saved password; Enter sends it.
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyDown})
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyDown})
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyEnter})
	if got := w.shell.typed(); got != "deploy-secret\r" {
		t.Fatalf("typed %q, want the chosen password", got)
	}
	if len(w.state.autofillRules.pick.candidates) != 0 {
		t.Fatal("the offer stayed open after a password was sent")
	}
}

func TestTheHostPasswordIsOfferedFirst(t *testing.T) {
	w := savedPasswordWorld(t)
	session := &w.state.sessions[0]
	if err := w.state.secrets.vault.Set(w.state.passwordName(session), "host-secret"); err != nil {
		t.Fatal(err)
	}

	w.output("Password: ")
	pick := w.state.autofillRules.pick
	if len(pick.candidates) < 2 || !pick.candidates[0].host || pick.candidates[0].name != session.host.Label {
		t.Fatalf("candidates = %+v", pick.candidates)
	}
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyDown})
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyEnter})
	if got := w.shell.typed(); got != "host-secret\r" {
		t.Fatalf("typed %q", got)
	}
}

func TestThePasswordOfferSendsNothingWhenClosed(t *testing.T) {
	w := savedPasswordWorld(t)
	w.output("Password: ")
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyEscape})
	if len(w.state.autofillRules.pick.candidates) != 0 || w.shell.typed() != "" {
		t.Fatalf("Escape sent something: pick %+v, typed %q", w.state.autofillRules.pick, w.shell.typed())
	}

	// Output that does not change the prompt must not offer it again: the
	// answer it was opened for was dismissed.
	w.output("\x1b[0m")
	if len(w.state.autofillRules.pick.candidates) != 0 || w.shell.typed() != "" {
		t.Fatalf("the offer reopened on the same prompt: pick %+v, typed %q", w.state.autofillRules.pick, w.shell.typed())
	}
}

func TestTypingAtAPasswordPromptTakesTheOfferBack(t *testing.T) {
	w := savedPasswordWorld(t)
	w.output("Password: ")
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyUnknown, Text: "r", Typed: "r"})
	if len(w.state.autofillRules.pick.candidates) != 0 {
		t.Fatal("typing kept the offer open")
	}
	if got := w.shell.typed(); got != "r" {
		t.Fatalf("typed %q, want the typed character to reach the shell", got)
	}
}

func TestSuppliedTextTakesTheOfferBack(t *testing.T) {
	for _, event := range []app.TextEvent{
		{Kind: app.TextInsert, Text: "by-hand"},
		{Kind: app.TextPaste, Text: "by-hand"},
	} {
		w := savedPasswordWorld(t)
		w.output("Password: ")
		w.state.terminalText(w.state.sessions[0].ID, event)
		if len(w.state.autofillRules.pick.candidates) != 0 {
			t.Fatalf("%v kept the offer open", event.Kind)
		}
		if got := w.shell.typed(); got != "by-hand" {
			t.Fatalf("%v typed %q, want the supplied text to reach the shell", event.Kind, got)
		}
	}
}

func TestThePasswordOfferWaitsForTheTerminalTheUserIsLookingAt(t *testing.T) {
	w := liveAutofillSession(t)
	addCredential(t, w.state, "root", "root-secret")
	w.state.activeSection = sectionSettings
	w.output("Password: ")
	if len(w.state.autofillRules.pick.candidates) != 0 || w.shell.typed() != "" {
		t.Fatalf("an offer opened behind Settings: pick %+v, typed %q", w.state.autofillRules.pick, w.shell.typed())
	}
}

func TestClickingASavedPasswordSendsIt(t *testing.T) {
	w := savedPasswordWorld(t)
	w.output("Password: ")
	w.state.Click(hitAutofillPickItem.At(1))
	if got := w.shell.typed(); got != "deploy-secret\r" {
		t.Fatalf("typed %q", got)
	}
	if len(w.state.autofillRules.pick.candidates) != 0 {
		t.Fatal("the offer stayed open after a click")
	}
}

func TestThePasswordOfferStaysOffWithTheAssistSwitch(t *testing.T) {
	w := savedPasswordWorld(t)
	w.state.setPasswordHint(false)
	w.output("Password: ")
	if len(w.state.autofillRules.pick.candidates) != 0 || w.shell.typed() != "" {
		t.Fatalf("the offer ignored the switch: pick %+v, typed %q", w.state.autofillRules.pick, w.shell.typed())
	}
}

func TestNoSavedPasswordMeansNoOffer(t *testing.T) {
	w := liveAutofillSession(t)
	w.output("Password: ")
	if len(w.state.autofillRules.pick.candidates) != 0 || w.shell.typed() != "" {
		t.Fatalf("an empty offer opened: pick %+v, typed %q", w.state.autofillRules.pick, w.shell.typed())
	}
}

func TestAConfiguredRuleStillAnswersBeforeTheOffer(t *testing.T) {
	w := liveAutofillSession(t)
	addCredential(t, w.state, "root", "root-secret")
	w.addRule(t, "token", `^Password:`, "", autofill.SendText, "rule-secret")
	w.state.Click(hitAutofillToggleList.At(0))
	w.state.activeSection = sectionTerminal

	w.output("Password: ")
	if w.state.modal != modalAutofillConfirm || len(w.state.autofillRules.pick.candidates) != 0 {
		t.Fatalf("the rule did not answer first: modal %d, pick %+v", w.state.modal, w.state.autofillRules.pick)
	}
}
