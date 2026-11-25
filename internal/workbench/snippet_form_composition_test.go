package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

// The inline snippet editor never saves an input method's marked text: neither
// for a new draft nor while editing an existing snippet. The application leaves
// the commit to the input method.
func TestSnippetInlineEditorDoesNotSaveAComposition(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.startInlineSnippetDraft("")
	state.snippets.inline.name.Insert("deploy")
	state.snippets.inline.command.Insert("make deploy")
	state.snippets.inline.name.SetMarkedText("部署", text.Range{End: 2})

	state.applyInlineSnippetEdits()
	if !state.snippets.inline.draft || len(state.snippets.list) != 0 {
		t.Fatal("a composing name field was saved")
	}
	if _, composing := state.snippets.inline.name.Composition(); !composing {
		t.Fatal("the application committed the composition")
	}

	// What the input method does on commit: unmark the text.
	state.snippets.inline.name.Unmark()
	state.applyInlineSnippetEdits()
	if state.snippets.inline.draft || len(state.snippets.list) != 1 {
		t.Fatal("edits after commit did not save the snippet")
	}
	if got := state.snippets.list[0]; got.Name != "deploy部署" || got.Command != "make deploy" {
		t.Fatalf("snippet = %+v", got)
	}
}

// A composition in the command field is as much a preedit as one in the name:
// edits must wait until the input method commits it.
func TestSnippetInlineEditorDoesNotSaveACompositionInTheCommandField(t *testing.T) {
	state := snippetTestState(t, snippet{Name: "deploy", Command: "make deploy"})
	state.snippets.inline.command.SetMarkedText("部署", text.Range{End: 2})
	state.applyInlineSnippetEdits()
	if state.snippets.list[0].Command != "make deploy" || state.snippets.inline.dirty {
		t.Fatalf("a composing command field was applied: %+v dirty=%v", state.snippets.list[0], state.snippets.inline.dirty)
	}
	if _, composing := state.snippets.inline.command.Composition(); !composing {
		t.Fatal("the application committed the composition")
	}
}

// A composition in the inline group picker must not be committed on Enter or
// when sending the snippet.
func TestSnippetInlineEditorDoesNotCommitComposingGroup(t *testing.T) {
	state := snippetTestState(t, snippet{Name: "deploy", Command: "make deploy", Group: "Ops"})
	state.snippets.inline.groupPicker.Query.SetMarkedText("生产", text.Range{End: 2})

	state.Key(gfx.KeyEvent{Target: hitSnippetInlineGroup, Key: gfx.KeyEnter})
	state.commitInlineSnippetPendingGroup()
	if state.snippets.list[0].Group != "Ops" {
		t.Fatalf("composing group was committed: %q", state.snippets.list[0].Group)
	}
	if _, composing := state.snippets.inline.groupPicker.Query.Composition(); !composing {
		t.Fatal("the application committed the group composition")
	}

	state.snippets.inline.groupPicker.Query.Unmark()
	state.Key(gfx.KeyEvent{Target: hitSnippetInlineGroup, Key: gfx.KeyEnter})
	if state.snippets.list[0].Group != "生产" {
		t.Fatalf("committed group = %q, want 生产", state.snippets.list[0].Group)
	}
}

// An input method can leave composing true with an empty marked range; there is
// no preedit to protect, so saving must not be blocked.
func TestSnippetInlineEditorSavesWhenTheMarkedRangeIsEmpty(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.startInlineSnippetDraft("")
	state.snippets.inline.name.Insert("deploy")
	state.snippets.inline.command.Insert("make deploy")
	state.snippets.inline.name.SetMarkedText("", text.Range{})
	state.applyInlineSnippetEdits()
	if state.snippets.inline.draft || len(state.snippets.list) != 1 {
		t.Fatal("an empty marked range blocked the save")
	}
}

// A composition interrupted by a click on another inline field must not keep
// saving blocked: the input method drops its marked text there, and the editor
// has to let go of it too.
func TestSnippetInlineEditorSavesAfterAnInterruptedComposition(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.startInlineSnippetDraft("")
	state.snippets.inline.name.Insert("deploy")
	state.snippets.inline.command.Insert("make deploy")
	state.snippets.inline.name.SetMarkedText("部署", text.Range{End: 2})

	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Target: hitSnippetInlineCommand})
	if _, composing := state.snippets.inline.name.Composition(); composing {
		t.Fatal("the abandoned field still composes")
	}
	state.applyInlineSnippetEdits()
	if state.snippets.inline.draft || len(state.snippets.list) != 1 {
		t.Fatal("an interrupted composition blocked the save")
	}
	if got := state.snippets.list[0]; got.Name != "deploy部署" {
		t.Fatalf("name = %q, want the text the user saw", got.Name)
	}
}
