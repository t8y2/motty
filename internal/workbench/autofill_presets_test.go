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

func TestTheSudoPresetFillsTheRuleDialogForTheUserToSave(t *testing.T) {
	w := liveAutofillSession(t)
	state := w.state
	state.Click(hitSettings)

	state.Click(hitSettingsAutofillSudo)
	if state.modal != modalAutofillRule || state.autofillRules.send != autofill.SendPassword ||
		state.autofillRules.pattern.String() != sudoPromptPattern {
		t.Fatalf("sudo preset: modal %d, send %v, pattern %q", state.modal, state.autofillRules.send, state.autofillRules.pattern.String())
	}
	state.Click(hitAutofillSubmit)
	if state.modal != modalNone || len(state.autofillRules.rules) != 1 || state.autofillRules.rules[0].Send != autofill.SendPassword {
		t.Fatalf("the sudo rule was not saved: modal %d, rules %+v", state.modal, state.autofillRules.rules)
	}
}
