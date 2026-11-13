package workbench

import (
	"errors"
	"fmt"
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/autofill"
	"motty/internal/configstore"
	"motty/internal/vault"
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
	ruleDeleteCredential
	ruleDeleteHostTag
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
	// credential is the saved password to delete, by id; uses is how many hosts
	// take their password from it when the question is asked.
	credential         string
	uses               int
	selectedAutofill   []*autofill.Rule
	selectedHighlights []selectedHighlightRule
}

type selectedHighlightRule struct {
	index int
	rule  highlightRule
}

func (state *State) askDeleteSelectedRules() {
	manager := &state.ruleManager
	ask := ruleDelete{kind: manager.kind}
	for index := range state.ruleManagerCount() {
		if !manager.selected[index] {
			continue
		}
		if manager.kind == ruleDeleteAutofill {
			rule := state.autofillRules.rules[index]
			ask.selectedAutofill = append(ask.selectedAutofill, rule)
			ask.text = ask.text || rule.Send == autofill.SendText
		} else {
			ask.selectedHighlights = append(ask.selectedHighlights, selectedHighlightRule{index: index, rule: state.highlighting.rules[index]})
		}
	}
	if len(ask.selectedAutofill)+len(ask.selectedHighlights) == 0 {
		return
	}
	state.ruleDelete, manager.returning = ask, true
	state.modal = modalDeleteRule
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

func (state *State) askDeleteCredential(index int) {
	if index < 0 || index >= len(state.credentials) {
		return
	}
	entry := state.credentials[index]
	state.ruleDelete = ruleDelete{kind: ruleDeleteCredential, credential: entry.ID, name: entry.Name, uses: state.credentialUses(entry.ID)}
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

func (state *State) clickDeleteRuleDialog(id gfx.ID) {
	ask := state.ruleDelete
	switch id {
	case hitRuleDeleteConfirm:
		state.closeModal()
		if len(ask.selectedAutofill)+len(ask.selectedHighlights) > 0 {
			state.deleteSelectedRules(ask)
			return
		}
		switch ask.kind {
		case ruleDeleteAutofill:
			for index, rule := range state.autofillRules.rules {
				if rule == ask.autofill {
					state.deleteAutofillRule(index)
					break
				}
			}
		case ruleDeleteCredential:
			state.deleteCredential(ask.credential)
		case ruleDeleteHostTag:
			state.deleteHostTag(ask.name)
		case ruleDeleteHighlight:
			if rules := state.highlighting.rules; ask.index < len(rules) && rules[ask.index].Pattern == ask.pattern {
				state.deleteHighlightRule(ask.index)
			}
		}
	case hitRuleDeleteCancel, hitRuleDeleteClose, hitRuleDeleteDismiss:
		state.closeModal()
	}
}

func (state *State) deleteSelectedRules(ask ruleDelete) {
	previousAutofill, previousHighlights := state.autofillRules.rules, state.highlighting.rules
	previousSaved := state.lastSaved
	var changes []vault.Change
	if len(ask.selectedAutofill) > 0 {
		state.autofillRules.rules = slices.DeleteFunc(slices.Clone(previousAutofill), func(rule *autofill.Rule) bool {
			if !slices.Contains(ask.selectedAutofill, rule) {
				return false
			}
			if rule.Send == autofill.SendText {
				changes = append(changes, vault.Change{Name: autofillSecretName(rule), Delete: true})
			}
			return true
		})
	}
	if len(ask.selectedHighlights) > 0 {
		state.highlighting.rules = make([]highlightRule, 0, len(previousHighlights))
		for index, rule := range previousHighlights {
			if !slices.Contains(ask.selectedHighlights, selectedHighlightRule{index: index, rule: rule}) {
				state.highlighting.rules = append(state.highlighting.rules, rule)
			}
		}
	}
	commit := func() error { return state.saveWithResultModeRecovery(false, false, false) }
	var err error
	if len(changes) == 0 {
		err = commit()
	} else if state.secrets.vault == nil {
		err = vault.ErrUnavailable
	} else {
		err = state.secrets.vault.ApplyWithCommit(changes, commit)
	}
	if err != nil {
		// ApplyWithCommit holds the vault lock, so shared state may only be
		// reloaded after it has returned. Reloading on a conflict is what lets
		// the retry build on the store the other window wrote; without it the
		// base stays stale and every retry fails the same way. The reload also
		// replaces both rule lists with the store's current records, so the
		// rolled-back copies must not overwrite them afterwards.
		if errors.Is(err, configstore.ErrConflict) {
			if refreshErr := state.refreshShared(); refreshErr != nil {
				err = errors.Join(err, fmt.Errorf("reload configuration after conflict: %w", refreshErr))
			} else {
				state.status = fmt.Sprintf(state.messages.StatusSaveFailedFmt, err)
				return
			}
		}
		state.autofillRules.rules, state.highlighting.rules = previousAutofill, previousHighlights
		state.status = fmt.Sprintf(state.messages.StatusSaveFailedFmt, err)
		return
	}
	if len(ask.selectedAutofill) > 0 {
		state.refreshAutofillArmed()
	}
	if len(ask.selectedHighlights) > 0 {
		state.recompileHighlights()
	}
	clear(state.ruleManager.selected)
	shared := len(previousAutofill) != len(state.autofillRules.rules) || len(previousHighlights) != len(state.highlighting.rules)
	state.configurationSaved(changedSavedFields(previousSaved, state.lastSaved), shared, true)
	state.sshSyncFlush()
}

func (state *State) paintDeleteRuleDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	m := state.messages.Autofill
	ask := state.ruleDelete
	description := fmt.Sprintf(m.DeleteFmt, ask.name)
	if count := len(ask.selectedAutofill) + len(ask.selectedHighlights); count > 0 {
		description = fmt.Sprintf(state.messages.RuleManagement.DeleteSelectedFmt, count)
	}
	if ask.text {
		description += " " + m.DeleteTextNote
	}
	title := m.DeleteTitle
	if len(ask.selectedAutofill)+len(ask.selectedHighlights) > 0 {
		title = state.messages.RuleManagement.DeleteSelectedTitle
	}
	if ask.kind == ruleDeleteCredential {
		c := state.messages.Credentials
		title, description = c.DeleteTitle, fmt.Sprintf(c.DeleteFmt, ask.name)
		if ask.uses > 0 {
			description += " " + fmt.Sprintf(c.DeleteInUseFmt, ask.uses)
		}
	}
	if ask.kind == ruleDeleteHostTag {
		tags := state.messages.HostTags
		title, description = tags.DeleteTitle, fmt.Sprintf(tags.DeleteFmt, ask.name)
		if ask.uses > 0 {
			description += " " + fmt.Sprintf(tags.DeleteInUseFmt, ask.uses)
		}
	}
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitRuleDeleteDismiss, CloseID: hitRuleDeleteClose, InitialFocusID: hitRuleDeleteCancel,
		ContentSize: gfx.Size{Width: 520}, FitBody: true,
	})
	layout := dialog.Begin(title, description)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitRuleDeleteCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitRuleDeleteConfirm, Label: m.Delete, Variant: components.ButtonDestructive},
	)
	dialog.End()
}
