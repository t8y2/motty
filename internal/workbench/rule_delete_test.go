package workbench

import (
	"testing"

	"motty/internal/autofill"
)

func TestDeletingAnAutofillRuleAsksFirstAndTakesItsTextFromTheVault(t *testing.T) {
	w := liveAutofillSession(t)
	state := w.state
	w.addRule(t, "token", `^Token:`, "", autofill.SendText, "s3cret")
	rule := state.autofillRules.rules[0]

	state.Click(hitAutofillDeleteList.At(0))
	if state.modal != modalDeleteRule || len(state.autofillRules.rules) != 1 {
		t.Fatalf("delete did not ask: modal %d, rules %d", state.modal, len(state.autofillRules.rules))
	}
	state.Click(hitRuleDeleteCancel)
	if state.modal != modalNone || len(state.autofillRules.rules) != 1 {
		t.Fatalf("cancel deleted: modal %d, rules %d", state.modal, len(state.autofillRules.rules))
	}
	if _, found, _ := state.secrets.vault.Get(autofillSecretName(rule)); !found {
		t.Fatal("cancel removed the saved text")
	}

	state.Click(hitAutofillDeleteList.At(0))
	state.Click(hitRuleDeleteConfirm)
	if state.modal != modalNone || len(state.autofillRules.rules) != 0 {
		t.Fatalf("confirm did not delete: modal %d, rules %d", state.modal, len(state.autofillRules.rules))
	}
	if _, found, _ := state.secrets.vault.Get(autofillSecretName(rule)); found {
		t.Fatal("the deleted rule's text is still in the vault")
	}
}

func TestDeleteDialogNamesTheRuleAndWarnsAboutSavedText(t *testing.T) {
	w := liveAutofillSession(t)
	state := w.state
	w.addRule(t, "token", `^Token:`, "", autofill.SendText, "s3cret")
	state.Click(hitAutofillDeleteList.At(0))
	if _, texts := paintedTexts(state); !contains(texts, `"token"`) || !contains(texts, "removed from") {
		t.Fatalf("dialog text = %q", texts)
	}
}
