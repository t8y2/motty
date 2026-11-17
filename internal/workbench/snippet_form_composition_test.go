package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

// A snippet dialog never saves an input method's marked text: not on Enter in
// either field and not through the Save button. The application leaves the
// commit to the input method.
func TestSnippetFormDoesNotSaveAComposition(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("deploy")
	state.snippets.command.Insert("make deploy")
	state.snippets.name.SetMarkedText("部署", text.Range{End: 2})

	saves := []struct {
		name string
		save func()
	}{
		{"Enter in the composing field", func() {
			state.Key(gfx.KeyEvent{Target: hitSnippetName, Key: gfx.KeyEnter})
		}},
		{"Enter in the other field", func() {
			state.Key(gfx.KeyEvent{Target: hitSnippetCommand, Key: gfx.KeyEnter})
		}},
		{"the Save button", func() {
			state.Click(hitSnippetSubmit)
		}},
	}
	for _, save := range saves {
		t.Run(save.name, func(t *testing.T) {
			state.snippets.dialogError = ""
			save.save()
			if state.modal != modalSnippet || len(state.snippets.list) != 0 {
				t.Fatal("a composing field was saved")
			}
			if _, composing := state.snippets.name.Composition(); !composing {
				t.Fatal("the application committed the composition")
			}
		})
	}

	// What the input method does with the Enter: commit the marked text.
	state.snippets.name.Unmark()
	state.Key(gfx.KeyEvent{Target: hitSnippetName, Key: gfx.KeyEnter})
	if state.modal != modalNone || len(state.snippets.list) != 1 {
		t.Fatal("Enter after the commit did not save the snippet")
	}
	if got := state.snippets.list[0]; got.Name != "deploy部署" || got.Command != "make deploy" {
		t.Fatalf("snippet = %+v", got)
	}
}

// Pressing Save while composing keeps the field focused, so the runtime does
// not commit the preedit as a side effect of moving focus to the button.
func TestSnippetFormSaveButtonDoesNotStealAComposition(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("deploy")
	state.snippets.command.Insert("make deploy")
	state.snippets.name.SetMarkedText("部署", text.Range{End: 2})
	result := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Target: hitSnippetSubmit})
	if !result.Handled || result.Focus != hitSnippetName {
		t.Fatalf("Save press result = %+v", result)
	}
	if state.modal != modalSnippet || len(state.snippets.list) != 0 {
		t.Fatal("the composing Save press saved the snippet")
	}
	if _, composing := state.snippets.name.Composition(); !composing {
		t.Fatal("the Save press committed the composition")
	}
}

// A composition in the command field is as much a preedit as one in the name:
// neither field may be saved while it holds marked text.
func TestSnippetFormDoesNotSaveACompositionInTheCommandField(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("deploy")
	state.snippets.command.Insert("make deploy")
	state.snippets.command.SetMarkedText("部署", text.Range{End: 2})
	state.Click(hitSnippetSubmit)
	if state.modal != modalSnippet || len(state.snippets.list) != 0 {
		t.Fatal("a composing command field was saved")
	}
	if _, composing := state.snippets.command.Composition(); !composing {
		t.Fatal("the application committed the composition")
	}
}

// An input method can leave composing true with an empty marked range; there is
// no preedit to protect, so saving must not be blocked.
func TestSnippetFormSavesWhenTheMarkedRangeIsEmpty(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("deploy")
	state.snippets.command.Insert("make deploy")
	state.snippets.name.SetMarkedText("", text.Range{})
	state.Key(gfx.KeyEvent{Target: hitSnippetName, Key: gfx.KeyEnter})
	if state.modal != modalNone || len(state.snippets.list) != 1 {
		t.Fatal("an empty marked range blocked the save")
	}
}

// A composition interrupted by a click on the other field must not keep the
// dialog blocked: the input method drops its marked text there, and the editor
// has to let go of it too.
func TestSnippetFormSavesAfterAnInterruptedComposition(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("deploy")
	state.snippets.command.Insert("make deploy")
	state.snippets.name.SetMarkedText("部署", text.Range{End: 2})

	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Target: hitSnippetCommand})
	if _, composing := state.snippets.name.Composition(); composing {
		t.Fatal("the abandoned field still composes")
	}
	state.Key(gfx.KeyEvent{Target: hitSnippetName, Key: gfx.KeyEnter})
	if state.modal != modalNone || len(state.snippets.list) != 1 {
		t.Fatal("an interrupted composition blocked the save")
	}
	if got := state.snippets.list[0]; got.Name != "deploy部署" {
		t.Fatalf("name = %q, want the text the user saw", got.Name)
	}
}
