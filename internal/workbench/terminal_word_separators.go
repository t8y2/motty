package workbench

import (
	"fmt"
	"strings"
	"unicode"
)

// normalizeWordSeparators keeps one of each character. Blanks always end a word
// and control characters cannot be shown, so neither is kept: a value of only
// those means the built-in rule.
func normalizeWordSeparators(value string) string {
	var kept strings.Builder
	seen := map[rune]bool{}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == unicode.ReplacementChar || seen[r] {
			continue
		}
		seen[r] = true
		kept.WriteRune(r)
	}
	return kept.String()
}

func (state *State) setTerminalWordSeparators(value string) bool {
	value = normalizeWordSeparators(value)
	if state.terminalWordSeparators == value {
		return false
	}
	state.terminalWordSeparators = value
	return true
}

func (state *State) wordSeparatorsDescription() string {
	tools := state.messages.Tools
	if state.terminalWordSeparators == "" {
		return tools.WordSeparatorsDefault
	}
	return fmt.Sprintf(tools.WordSeparatorsCustomFmt, state.terminalWordSeparators)
}

func (state *State) openWordSeparatorsForm() {
	tools := state.messages.Tools
	form := state.openForm(formDialog{
		title: tools.WordSeparatorsRow, description: tools.WordSeparatorsDescription, submit: state.messages.KeySavePassphraseSubmit,
		run: func(state *State, form *formDialog) string {
			if state.setTerminalWordSeparators(form.value(0)) {
				state.save()
			}
			return ""
		},
	}, formField{label: tools.WordSeparatorsLabel, placeholder: tools.WordSeparatorsPlaceholder})
	form.set(0, state.terminalWordSeparators)
}
