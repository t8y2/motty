package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/autofill"
	"motty/internal/highlight"
)

func TestHighlightEditingReplacesOnlyItsRuleAndRejectsInvalidOrDuplicateInput(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	original := highlightRule{Pattern: "ERROR", Regex: true, CaseSensitive: true, Slot: highlight.Green}
	state.highlighting.rules = []highlightRule{original, {Pattern: "WARN", Slot: highlight.Yellow}}
	state.openHighlightEdit(0)
	if state.highlighting.pattern.String() != original.Pattern || !state.highlighting.regex || !state.highlighting.folded || state.highlighting.slot != original.Slot {
		t.Fatal("editor did not retain the complete rule")
	}
	state.highlighting.pattern = text.Editor{}
	state.highlighting.pattern.Insert("[")
	state.Click(hitHighlightSubmit)
	if state.modal != modalHighlightRule || state.highlighting.dialogError == "" || state.highlighting.rules[0] != original {
		t.Fatal("invalid edit changed the rule or closed its editor")
	}
	state.highlighting.pattern = text.Editor{}
	state.highlighting.pattern.Insert("WARN")
	state.highlighting.regex, state.highlighting.folded = false, false
	state.highlighting.slot = highlight.Yellow
	state.Click(hitHighlightSubmit)
	if state.modal != modalHighlightRule || state.highlighting.dialogError == "" || state.highlighting.rules[0] != original {
		t.Fatal("duplicate edit changed the rule")
	}
	state.highlighting.pattern = text.Editor{}
	state.highlighting.pattern.Insert("FAIL")
	state.Click(hitHighlightSubmit)
	if len(state.highlighting.rules) != 2 || state.highlighting.rules[0].Pattern != "FAIL" || state.highlighting.rules[1].Pattern != "WARN" {
		t.Fatalf("edit appended, reordered or changed another rule: %+v", state.highlighting.rules)
	}
	restarted := newTestState(t, options)
	if len(restarted.highlighting.rules) != 2 || restarted.highlighting.rules[0] != state.highlighting.rules[0] {
		t.Fatal("edited rule did not survive restart")
	}
}

func TestRuleManagerBatchHighlightDeletionCancelsAndPreservesUnselectedRules(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.highlighting.rules = []highlightRule{{Pattern: "one", Slot: highlight.Red}, {Pattern: "two", Slot: highlight.Blue}, {Pattern: "three", Slot: highlight.Green}}
	state.Click(hitSettingsHighlightManage)
	state.Click(hitRuleManagerSelect.At(0))
	state.Click(hitRuleManagerSelect.At(2))
	state.Click(hitRuleManagerDelete)
	if state.modal != modalDeleteRule || len(state.highlighting.rules) != 3 {
		t.Fatal("batch deletion did not ask first")
	}
	state.Click(hitRuleDeleteCancel)
	if state.modal != modalRuleManager || len(state.ruleManager.selected) != 2 || len(state.highlighting.rules) != 3 {
		t.Fatal("cancel lost selection, closed the manager or deleted rules")
	}
	state.Click(hitRuleManagerDelete)
	state.Click(hitRuleDeleteConfirm)
	if state.modal != modalRuleManager || len(state.highlighting.rules) != 1 || state.highlighting.rules[0].Pattern != "two" || len(state.ruleManager.selected) != 0 {
		t.Fatalf("batch result: modal %v rules %+v selection %+v", state.modal, state.highlighting.rules, state.ruleManager.selected)
	}
	restarted := newTestState(t, options)
	if len(restarted.highlighting.rules) != 1 || restarted.highlighting.rules[0].Pattern != "two" {
		t.Fatal("batch deletion did not survive restart")
	}
}

func TestRuleManagerAutofillBatchDeletionRemovesOnlySelectedVaultText(t *testing.T) {
	w := liveAutofillSession(t)
	state := w.state
	w.addRule(t, "one", `^One:`, "", autofill.SendText, "private-one")
	w.addRule(t, "two", `^Two:`, "", autofill.SendText, "private-two")
	w.addRule(t, "three", `^Three:`, "", autofill.SendText, "private-three")
	first, second, third := state.autofillRules.rules[0], state.autofillRules.rules[1], state.autofillRules.rules[2]
	state.toggleAutofillRule(1)
	state.Click(hitSettingsAutofillManage)
	state.Click(hitRuleManagerSelect.At(0))
	state.Click(hitRuleManagerSelect.At(2))
	state.Click(hitRuleManagerDelete)
	if !state.ruleDelete.text {
		t.Fatal("batch confirmation omitted the vault deletion warning")
	}
	state.Click(hitRuleDeleteCancel)
	if _, found, _ := state.secrets.vault.Get(autofillSecretName(first)); !found {
		t.Fatal("canceled deletion removed vault text")
	}
	state.Click(hitRuleManagerDelete)
	state.Click(hitRuleDeleteConfirm)
	if len(state.autofillRules.rules) != 1 || state.autofillRules.rules[0] != second || !second.Enabled || !state.autofillRules.armed {
		t.Fatal("batch deleted or disarmed an unselected rule")
	}
	if _, found, _ := state.secrets.vault.Get(autofillSecretName(first)); found {
		t.Fatal("deleted rule left fixed text in the vault")
	}
	if _, found, _ := state.secrets.vault.Get(autofillSecretName(third)); found {
		t.Fatal("second selected rule left fixed text in the vault")
	}
	if _, found, _ := state.secrets.vault.Get(autofillSecretName(second)); !found {
		t.Fatal("unselected rule lost fixed text")
	}
	state.Click(hitRuleManagerEdit.At(0))
	if state.autofillRules.fixed.String() != "" || state.autofillRules.editing != second {
		t.Fatal("manager exposed a secret or edited the wrong rule")
	}
	state.Click(hitAutofillSubmit)
	if state.modal != modalRuleManager || state.autofillRules.rules[0].ID != second.ID || !state.autofillRules.rules[0].Enabled {
		t.Fatal("manager edit changed identity or arming")
	}
}

func TestRuleManagerDeleteSnapshotDoesNotDeleteShiftedHighlightRule(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.highlighting.rules = []highlightRule{{Pattern: "one", Slot: highlight.Red}, {Pattern: "two", Slot: highlight.Blue}}
	state.Click(hitSettingsHighlightManage)
	state.Click(hitRuleManagerSelect.At(0))
	state.Click(hitRuleManagerDelete)
	state.highlighting.rules = state.highlighting.rules[1:]
	state.Click(hitRuleDeleteConfirm)
	if len(state.highlighting.rules) != 1 || state.highlighting.rules[0].Pattern != "two" {
		t.Fatal("confirmation deleted a different rule after the list changed")
	}
}

func TestRuleManagerSelectionAndCompactSettings(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection, state.settingsCategory = sectionSettings, settingsTerminal
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	emptyHeight := state.settingsContent
	for index := range maxCustomHighlightRules {
		state.highlighting.rules = append(state.highlighting.rules, highlightRule{Pattern: fmt.Sprintf("rule-%d", index), Slot: highlight.Red})
	}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if state.settingsContent != emptyHeight {
		t.Fatalf("rules grew settings from %v to %v", emptyHeight, state.settingsContent)
	}
	state.Click(hitSettingsHighlightManage)
	state.Click(hitRuleManagerAll)
	if len(state.ruleManager.selected) != maxCustomHighlightRules {
		t.Fatal("select all omitted off-screen rules")
	}
	state.Click(hitRuleManagerAll)
	if len(state.ruleManager.selected) != 0 {
		t.Fatal("clear selection did not clear every rule")
	}
	state.Click(hitRuleManagerSelect.At(0))
	state.Click(hitRuleManagerDelete)
	state.Click(hitRuleDeleteClose)
	if state.modal != modalRuleManager || !state.ruleManager.selected[0] {
		t.Fatal("closing deletion did not preserve manager selection")
	}
	state.Click(hitRuleManagerEdit.At(0))
	state.Click(hitHighlightSubmit)
	if state.modal != modalRuleManager || len(state.highlighting.rules) != maxCustomHighlightRules {
		t.Fatal("editing at capacity was rejected or appended another rule")
	}
}

func TestRuleManagerVaultSaveFailureKeepsAutofillEditorAndOriginalRule(t *testing.T) {
	w := liveAutofillSession(t)
	state := w.state
	w.addRule(t, "token", `^Token:`, "", autofill.SendText, "private-value")
	original := state.autofillRules.rules[0]
	vault := state.secrets.vault
	state.Click(hitSettingsAutofillManage)
	state.Click(hitRuleManagerEdit.At(0))
	state.secrets.vault = nil
	state.autofillRules.fixed.Insert("replacement")
	state.Click(hitAutofillSubmit)
	if state.modal != modalAutofillRule || state.autofillRules.dialogError == "" || state.autofillRules.rules[0] != original {
		t.Fatal("vault save failure replaced the rule or closed its editor")
	}
	state.Click(hitAutofillCancel)
	state.secrets.vault = vault
	if state.modal != modalRuleManager {
		t.Fatal("cancel after save failure did not return to manager")
	}
	if value, found, _ := vault.Get(autofillSecretName(original)); !found || value != "private-value" {
		t.Fatal("save failure changed existing vault text")
	}
}

func TestRuleManagerBatchDeletionConflictPreservesAutofillRulesAndVault(test *testing.T) {
	world := liveAutofillSession(test)
	state := world.state
	world.addRule(test, "one", `^One:`, "", autofill.SendText, "private-one")
	world.addRule(test, "two", `^Two:`, "", autofill.SendText, "private-two")
	world.addRule(test, "three", `^Three:`, "", autofill.SendText, "private-three")
	state.Click(hitSettingsAutofillManage)
	state.Click(hitRuleManagerSelect.At(0))
	state.Click(hitRuleManagerSelect.At(2))
	state.Click(hitRuleManagerDelete)

	other := newTestState(test, state.options)
	other.autofillRules.rules[2].Name = "changed elsewhere"
	if err := other.saveWithResult(); err != nil {
		test.Fatal(err)
	}
	state.Click(hitRuleDeleteConfirm)
	if len(state.autofillRules.rules) != 3 || !state.ruleManager.selected[0] || !state.ruleManager.selected[2] {
		test.Fatalf("conflicted deletion changed rules or selection: rules=%d selection=%v", len(state.autofillRules.rules), state.ruleManager.selected)
	}
	if !contains([]string{state.status}, "Could not save") {
		test.Fatalf("conflict was not reported: %q", state.status)
	}
	for index, value := range []string{"private-one", "private-two", "private-three"} {
		if got, found, err := state.secrets.vault.Get(autofillSecretName(state.autofillRules.rules[index])); err != nil || !found || got != value {
			test.Fatalf("conflict removed rule %d's vault text: found=%v err=%v", index, found, err)
		}
	}
	restarted := newTestState(test, state.options)
	if len(restarted.autofillRules.rules) != 3 || restarted.autofillRules.rules[2].Name != "changed elsewhere" {
		test.Fatal("conflicted deletion overwrote another window's rules")
	}
}

func TestRuleManagerBatchDeletionRetriesAfterAConflict(test *testing.T) {
	world := liveAutofillSession(test)
	state := world.state
	world.addRule(test, "one", `^One:`, "", autofill.SendText, "private-one")
	world.addRule(test, "two", `^Two:`, "", autofill.SendText, "private-two")
	world.addRule(test, "three", `^Three:`, "", autofill.SendText, "private-three")
	state.Click(hitSettingsAutofillManage)
	state.Click(hitRuleManagerSelect.At(0))
	state.Click(hitRuleManagerSelect.At(2))
	state.Click(hitRuleManagerDelete)

	other := newTestState(test, state.options)
	other.autofillRules.rules[2].Name = "changed elsewhere"
	if err := other.saveWithResult(); err != nil {
		test.Fatal(err)
	}
	state.Click(hitRuleDeleteConfirm)
	if len(state.autofillRules.rules) != 3 {
		test.Fatalf("conflicted deletion changed rules: %d", len(state.autofillRules.rules))
	}

	// The conflict has to refresh the base the next attempt builds on; without
	// it the retry reports the same conflict and deletes nothing.
	state.Click(hitRuleManagerDelete)
	state.Click(hitRuleDeleteConfirm)
	if len(state.autofillRules.rules) != 1 || state.autofillRules.rules[0].Name != "two" {
		test.Fatalf("retry after conflict left %+v, want only the unselected rule", state.autofillRules.rules)
	}
}

func TestRuleManagerBatchDeletionSaveFailureKeepsRulesAndSelection(test *testing.T) {
	for _, kind := range []ruleDeleteKind{ruleDeleteAutofill, ruleDeleteHighlight} {
		test.Run(fmt.Sprint(kind), func(test *testing.T) {
			world := liveAutofillSession(test)
			state := world.state
			world.addRule(test, "one", `^One:`, "", autofill.SendText, "private-one")
			world.addRule(test, "two", `^Two:`, "", autofill.SendText, "private-two")
			state.highlighting.rules = []highlightRule{{Pattern: "one", Slot: highlight.Red}, {Pattern: "two", Slot: highlight.Blue}}
			if err := state.saveWithResult(); err != nil {
				test.Fatal(err)
			}
			state.openRuleManager(kind)
			state.Click(hitRuleManagerAll)
			state.Click(hitRuleManagerDelete)
			state.saveBlocked = true
			state.Click(hitRuleDeleteConfirm)
			if state.ruleManagerCount() != 2 || len(state.ruleManager.selected) != 2 {
				test.Fatalf("failed deletion changed rules or selection: count=%d selection=%v", state.ruleManagerCount(), state.ruleManager.selected)
			}
			for index, value := range []string{"private-one", "private-two"} {
				if got, found, err := state.secrets.vault.Get(autofillSecretName(state.autofillRules.rules[index])); err != nil || !found || got != value {
					test.Fatalf("failed deletion removed rule %d's vault text: found=%v err=%v", index, found, err)
				}
			}
			state.saveBlocked = false
			state.Click(hitRuleManagerDelete)
			state.Click(hitRuleDeleteConfirm)
			if state.ruleManagerCount() != 0 || len(state.ruleManager.selected) != 0 {
				test.Fatal("retry did not delete all selected rules")
			}
		})
	}
}

func TestRuleManagerBatchDeletionVaultFailureKeepsRulesAndSelection(test *testing.T) {
	world := liveAutofillSession(test)
	state := world.state
	world.addRule(test, "one", `^One:`, "", autofill.SendText, "private-one")
	world.addRule(test, "two", `^Two:`, "", autofill.SendText, "private-two")
	state.openRuleManager(ruleDeleteAutofill)
	state.Click(hitRuleManagerAll)
	state.Click(hitRuleManagerDelete)
	vaultFile := state.secrets.vault
	state.secrets.vault = nil
	state.Click(hitRuleDeleteConfirm)
	state.secrets.vault = vaultFile
	if len(state.autofillRules.rules) != 2 || len(state.ruleManager.selected) != 2 {
		test.Fatal("unavailable vault removed rules or cleared the selection")
	}
	restarted := newTestState(test, state.options)
	if len(restarted.autofillRules.rules) != 2 {
		test.Fatal("unavailable vault persisted the deletion")
	}
}

func TestRuleManagerBatchDeletionUpdatesOtherWindowsAfterVaultCommit(test *testing.T) {
	pair := newWindowsOnOneStore(test)
	state := pair.first
	rules := []*autofill.Rule{
		{ID: "one", Name: "one", Pattern: "^One:", Send: autofill.SendText},
		{ID: "two", Name: "two", Pattern: "^Two:", Send: autofill.SendText},
		{ID: "three", Name: "three", Pattern: "^Three:", Send: autofill.SendText},
	}
	for _, rule := range rules {
		if err := rule.Compile(); err != nil {
			test.Fatal(err)
		}
		if err := state.secrets.vault.Set(autofillSecretName(rule), "synthetic-"+rule.ID); err != nil {
			test.Fatal(err)
		}
	}
	state.autofillRules.rules = rules
	if err := state.saveWithResult(); err != nil {
		test.Fatal(err)
	}
	if state.secrets.vault != pair.second.secrets.vault {
		test.Fatal("fixture does not share the vault between windows")
	}
	state.openRuleManager(ruleDeleteAutofill)
	state.Click(hitRuleManagerSelect.At(0))
	state.Click(hitRuleManagerSelect.At(2))
	state.Click(hitRuleManagerDelete)
	state.Click(hitRuleDeleteConfirm)
	for _, window := range []*State{state, pair.second} {
		if len(window.autofillRules.rules) != 1 || window.autofillRules.rules[0].ID != "two" {
			test.Fatalf("window retained selected rules or lost an unselected rule: %+v", window.autofillRules.rules)
		}
	}
	for _, rule := range rules {
		_, found, err := state.secrets.vault.Get(autofillSecretName(rule))
		if err != nil || found != (rule.ID == "two") {
			test.Fatalf("wrong vault ownership after deletion for %s: found=%v err=%v", rule.ID, found, err)
		}
	}
}
