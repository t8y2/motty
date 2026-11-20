package workbench

import (
	"regexp"

	"motty/internal/autofill"
)

// Password prompt assist settings and prompt tracking. The shared password
// picker in autofill_picker.go owns confirmation for sudo, su and SSH/SCP.

var passwordHintHits = tree.Feature("password-hint")

var hitSettingsPasswordHintToggle = tree.ID(passwordHintHits, "settings-password-hint")

var accountPasswordPrompts = [...]*regexp.Regexp{
	regexp.MustCompile(autofill.SuPromptPattern),
	regexp.MustCompile(autofill.SudoPromptPattern),
	regexp.MustCompile(autofill.SSHPromptPattern),
}

func isAccountPasswordPrompt(line string) bool {
	for _, pattern := range accountPasswordPrompts {
		if pattern.MatchString(line) {
			return true
		}
	}
	return false
}

// A prompt's row identity distinguishes a fresh authentication attempt from
// styling or repainting the same question. It is independent of the automatic
// rules' protection against repeatedly sending an answer.
type terminalPasswordPrompt struct {
	line       string
	rowID      uint64
	generation uint64
}

func (t *terminal) passwordPromptAtCursor() terminalPasswordPrompt {
	if t.screen == nil || t.screen.Modes().AltScreen || t.screen.ViewOffset() != 0 {
		return terminalPasswordPrompt{}
	}
	line, id, ok := t.screen.LogicalLineText(t.screen.Cursor().Y, 1024)
	if !ok {
		return terminalPasswordPrompt{}
	}
	return terminalPasswordPrompt{line: line, rowID: id, generation: t.screen.BufferGeneration()}
}

func (state *State) setPasswordHint(on bool) bool {
	if state.passwordHintOff == !on {
		return false
	}
	state.passwordHintOff = !on
	if !on {
		state.closeAutofillPick()
	}
	return true
}

// notePasswordPrompt looks at the line under the cursor after output arrived and
// tracks password prompts while they wait for input, including for keepalive.
func (state *State) notePasswordPrompt(session *Session) {
	t := session.term
	if t.shell == nil || t.ended || t.screen == nil {
		t.passwordHint = ""
		return
	}
	prompt := t.passwordPromptAtCursor()
	if isAccountPasswordPrompt(prompt.line) {
		t.passwordHint = prompt.line
		return
	}
	t.passwordHint = ""
}
