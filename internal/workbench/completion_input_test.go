package workbench

import "testing"

// Catalog text is revalidated just before it reaches the shell: a damaged or
// tampered cache must not be able to type a character the importer would have
// refused. Text learned from the user's own history stays unrestricted.
func TestSendSuggestionRevalidatesCatalogTextOnly(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git statu")
	before := w.shell.typed()
	w.state.sendSuggestion(w.session, "safe", true)
	if got := w.shell.typed(); got != before+"safe" {
		t.Fatalf("clean catalog suffix typed %q", got[len(before):])
	}

	before = w.shell.typed()
	w.state.sendSuggestion(w.session, "evil\u202Etext", true)
	if got := w.shell.typed(); got != before {
		t.Fatalf("catalog text with a bidirectional override reached the shell: %q", got[len(before):])
	}
	if w.state.completion.catalog == nil {
		t.Fatal("the test world lost its catalog")
	}

	w.state.sendSuggestion(w.session, "evil\u202Etext", false)
	if got := w.shell.typed(); got != before+"evil\u202Etext" {
		t.Fatalf("history text was restricted: %q", got[len(before):])
	}

	before = w.shell.typed()
	w.state.sendSuggestion(w.session, "--flag", true)
	if got := w.shell.typed(); got != before+"--flag" {
		t.Fatalf("catalog option suffix was restricted: %q", got[len(before):])
	}
	w.state.sendSuggestion(w.session, `it's "quoted"`, false)
	if got := w.shell.typed(); got != before+"--flag"+`it's "quoted"` {
		t.Fatalf("history text with quotes was restricted: %q", got[len(before):])
	}
}
