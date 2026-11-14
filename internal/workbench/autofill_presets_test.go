package workbench

import (
	"testing"

	"motty/internal/autofill"
)

func TestThePresetPatternsAreTheirPromptsAndNothingElse(t *testing.T) {
	for pattern, cases := range map[string]map[string]bool{
		sudoPromptPattern: {
			"[sudo] password for alice:":             true,
			"[sudo] password for alice: ":            true,
			"[sudo] alice 的密码：":                      true,
			"[sudo: authenticate] Password:":         true,
			"[sudo: authenticate] Password: ":        true,
			"[sudo: authenticate] 密码：":               true,
			"[sudo: authenticate] 密碼：":               true,
			"[sudo: 验证] 密码：":                         true,
			"[sudo: authenticate] password for dev:": true,
			"[sudo files] password for alice:":       false,
			"Password:":                              false,
			"echo [sudo] password for x:":            false,
			"enter password for the db:":             false,
			"[sudoku] password for alice:":           false,
		},
		suPromptPattern: {
			"Password:":                      true,
			"Password: ":                     true,
			"密码：":                            true,
			"alice@host's password:":         false,
			"Enter password:":                false,
			"[sudo] password for alice:":     false,
			"[sudo: authenticate] Password:": false,
			"Password for alice:":            false,
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

func TestSudoPresetMatchesSudoRsPromptAndAsksToFill(t *testing.T) {
	w := liveAutofillSession(t)
	state := w.state
	if err := state.secrets.vault.Set(state.passwordName(&state.sessions[0]), "dev-password"); err != nil {
		t.Fatal(err)
	}
	state.Click(hitSettings)
	state.Click(hitSettingsAutofillSudo)
	state.Click(hitAutofillSubmit)
	state.Click(hitAutofillToggleList.At(0))
	if !state.autofillRules.rules[0].Enabled {
		t.Fatal("sudo rule was not enabled")
	}

	w.output("$ sudo -i\r\n[sudo: authenticate] Password: ")
	if state.modal != modalAutofillConfirm || w.shell.typed() != "" {
		t.Fatalf("sudo-rs prompt must ask: modal %d, typed %q", state.modal, w.shell.typed())
	}
	state.Click(hitAutofillAllow)
	if got := w.shell.typed(); got != "dev-password\r" {
		t.Fatalf("typed %q, want %q", got, "dev-password\r")
	}
}
