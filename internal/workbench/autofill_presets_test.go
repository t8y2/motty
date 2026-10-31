package workbench

import (
	"testing"

	"motty/internal/autofill"
)

func TestThePresetPatternsAreTheirPromptsAndNothingElse(t *testing.T) {
	for pattern, cases := range map[string]map[string]bool{
		sudoPromptPattern: {
			"[sudo] password for alice:":  true,
			"[sudo] password for alice: ": true,
			"[sudo] alice 的密码：":           true,
			"Password:":                   false,
			"echo [sudo] password for x:": false,
			"enter password for the db:":  false,
		},
		suPromptPattern: {
			"Password:":                  true,
			"Password: ":                 true,
			"密码：":                        true,
			"alice@host's password:":     false,
			"Enter password:":            false,
			"[sudo] password for alice:": false,
			"Password for alice:":        false,
		},
	} {
		rule := &autofill.Rule{Pattern: pattern}
		if err := rule.Compile(); err != nil {
			t.Fatalf("%q does not compile: %v", pattern, err)
		}
		for line, want := range cases {
			if got := rule.Matches(line); got != want {
				t.Errorf("%q matches %q = %v, want %v", pattern, line, got, want)
			}
		}
	}
}

func TestThePresetsFillTheRuleDialogForTheUserToSave(t *testing.T) {
	w := liveAutofillSession(t)
	state := w.state
	state.Click(hitSettings)

	state.Click(hitSettingsAutofillSu)
	if state.modal != modalAutofillRule || state.autofillRules.send != autofill.SendText ||
		state.autofillRules.pattern.String() != suPromptPattern {
		t.Fatalf("su preset: modal %d, send %v, pattern %q", state.modal, state.autofillRules.send, state.autofillRules.pattern.String())
	}
	typeInto(state, hitAutofillFixed, "root-secret")
	state.Click(hitAutofillSubmit)
	if state.modal != modalNone || len(state.autofillRules.rules) != 1 || state.autofillRules.rules[0].Enabled {
		t.Fatalf("the su rule was not saved, off: modal %d, rules %+v", state.modal, state.autofillRules.rules)
	}

	// Turned on, it answers su's prompt once the user allows it.
	state.Click(hitAutofillToggleList.At(0))
	w.output("Password: ")
	if state.modal != modalAutofillConfirm {
		t.Fatalf("su's prompt did not ask: modal %d", state.modal)
	}
	state.Click(hitAutofillAllow)
	if got := w.shell.typed(); got != "root-secret\r" {
		t.Fatalf("typed %q", got)
	}

	state.Click(hitSettingsAutofillSudo)
	if state.autofillRules.send != autofill.SendPassword || state.autofillRules.pattern.String() != sudoPromptPattern {
		t.Fatalf("sudo preset: send %v, pattern %q", state.autofillRules.send, state.autofillRules.pattern.String())
	}
	state.Click(hitAutofillSubmit)
	if len(state.autofillRules.rules) != 2 || state.autofillRules.rules[1].Send != autofill.SendPassword {
		t.Fatalf("the sudo rule was not saved: %+v", state.autofillRules.rules)
	}
}
