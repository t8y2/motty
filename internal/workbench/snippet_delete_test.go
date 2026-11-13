//go:build windows || darwin || linux

package workbench

import "testing"

func TestDeletingASnippetAsksFirst(t *testing.T) {
	state := snippetTestState(t, snippet{Name: "Keep", Command: "echo keep"})
	state.Click(hitSnippetRemove)
	if state.modal != modalSnippetDelete || state.snippets.deleting != 1 || len(state.snippets.list) != 1 {
		t.Fatalf("delete: modal=%d deleting=%d snippets=%d", state.modal, state.snippets.deleting, len(state.snippets.list))
	}
	// Dismissing the confirmation keeps the snippet and forgets what it asked
	// about, so a later confirmation cannot delete something else.
	state.Click(hitSnippetDeleteDismiss)
	if state.modal != modalNone || state.snippets.deleting != 0 || len(state.snippets.list) != 1 {
		t.Fatalf("dismiss: modal=%d deleting=%d snippets=%d", state.modal, state.snippets.deleting, len(state.snippets.list))
	}
	state.Click(hitSnippetRemove)
	state.Click(hitSnippetDeleteConfirm)
	if state.modal != modalNone || state.snippets.deleting != 0 || len(state.snippets.list) != 0 {
		t.Fatalf("confirm: modal=%d deleting=%d snippets=%d", state.modal, state.snippets.deleting, len(state.snippets.list))
	}
}

func TestSnippetDeleteConfirmationNamesTheEntryItAsksAbout(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "First", Command: "echo first"},
		snippet{Name: "Second", Command: "echo second"})
	state.snippets.selected = 1
	state.Click(hitSnippetRemove)
	if state.snippets.deleting != 2 {
		t.Fatalf("the confirmation asks about snippet %d, want the selected second", state.snippets.deleting)
	}
	_, texts := paintedTexts(state)
	if !contains(texts, state.messages.Tools.SnippetDeleteTitle) {
		t.Fatalf("the confirmation title is missing: %v", texts)
	}
	if !contains(texts, "Second") {
		t.Fatalf("the confirmation does not name the snippet: %v", texts)
	}
}
