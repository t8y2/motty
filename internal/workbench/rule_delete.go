package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/autofill"
)

// Deleting a rule from Settings asks first. An autofill rule that sends a fixed
// text takes that text out of the vault with it, and there is no getting it back.

var ruleDeleteHits = tree.Feature("rule-delete")

var (
	hitRuleDeleteDismiss = tree.ID(ruleDeleteHits, "dismiss")
	hitRuleDeleteClose   = tree.ID(ruleDeleteHits, "close")
	hitRuleDeleteCancel  = tree.ID(ruleDeleteHits, "cancel")
	hitRuleDeleteConfirm = tree.ID(ruleDeleteHits, "confirm")
)

type ruleDeleteKind uint8

const (
	ruleDeleteAutofill ruleDeleteKind = iota + 1
	ruleDeleteHighlight
)

// ruleDelete is the rule the open dialog would delete.
type ruleDelete struct {
	kind ruleDeleteKind
	// rule identifies an autofill rule by pointer, so a list that changed under
	// the dialog cannot make it delete another one; a highlight rule has no
	// identity of its own and is found by its index and pattern.
	autofill *autofill.Rule
	index    int
	pattern  string
	name     string
	text     bool
}

func (state *State) askDeleteAutofillRule(index int) {
	rules := state.autofillRules.rules
	if index < 0 || index >= len(rules) {
		return
	}
	rule := rules[index]
	state.ruleDelete = ruleDelete{kind: ruleDeleteAutofill, autofill: rule, name: rule.Name, text: rule.Send == autofill.SendText}
	state.modal = modalDeleteRule
}

func (state *State) askDeleteHighlightRule(index int) {
	rules := state.highlighting.rules
	if index < 0 || index >= len(rules) {
		return
	}
	state.ruleDelete = ruleDelete{kind: ruleDeleteHighlight, index: index, pattern: rules[index].Pattern, name: rules[index].Pattern}
	state.modal = modalDeleteRule
}

func (state *State) clickDeleteRuleDialog(id ui.ID) {
	ask := state.ruleDelete
	switch id {
	case hitRuleDeleteConfirm:
		state.closeModal()
		switch ask.kind {
		case ruleDeleteAutofill:
			for index, rule := range state.autofillRules.rules {
				if rule == ask.autofill {
					state.deleteAutofillRule(index)
					break
				}
			}
		case ruleDeleteHighlight:
			if rules := state.highlighting.rules; ask.index < len(rules) && rules[ask.index].Pattern == ask.pattern {
				state.deleteHighlightRule(ask.index)
			}
		}
	case hitRuleDeleteCancel, hitRuleDeleteClose, hitRuleDeleteDismiss:
		state.closeModal()
	}
}

func (state *State) paintDeleteRuleDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m := state.messages.Autofill
	ask := state.ruleDelete
	description := fmt.Sprintf(m.DeleteFmt, ask.name)
	if ask.text {
		description += " " + m.DeleteTextNote
	}
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitRuleDeleteDismiss, CloseID: hitRuleDeleteClose, InitialFocusID: hitRuleDeleteCancel,
		ContentSize: ui.Size{Width: 520}, FitBody: true,
	})
	layout := dialog.Begin(m.DeleteTitle, description)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitRuleDeleteCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitRuleDeleteConfirm, Label: m.Delete, Variant: components.ButtonDestructive},
	)
	dialog.End()
}
