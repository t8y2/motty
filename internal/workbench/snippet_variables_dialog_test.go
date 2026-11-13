//go:build windows || darwin || linux

package workbench

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestSnippetDynamicVariablesDialogFlow(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	shell := world.conns[0].shell(t)
	state.Wake()
	state.activeSection = sectionTerminal

	// Add two snippets: one plain, one with dynamic variables (one with default, one required).
	state.snippets.list = []snippet{
		{
			Name:    "Plain Ping",
			Command: "ping 127.0.0.1",
			Run:     true,
		},
		{
			Name:    "Deploy App",
			Command: "deploy --env {{env:staging}} --tag {{version}}",
			Run:     true,
		},
	}

	// 1. Plain snippet sends immediately without opening dialog.
	if !state.sendSnippet(0) {
		t.Fatal("sendSnippet(0) failed")
	}
	if state.modal != modalNone {
		t.Fatalf("expected modalNone, got %d", state.modal)
	}
	if got := shell.typed(); got != "ping 127.0.0.1\r" {
		t.Fatalf("shell got %q, want 'ping 127.0.0.1\\r'", got)
	}

	// 2. Snippet with variables opens modalSnippetVariables.
	if !state.sendSnippet(1) {
		t.Fatal("sendSnippet(1) failed")
	}
	if state.modal != modalSnippetVariables {
		t.Fatalf("expected modalSnippetVariables (%d), got %d", modalSnippetVariables, state.modal)
	}
	prompt := &state.snippetPrompt
	if len(prompt.defs) != 2 {
		t.Fatalf("expected 2 defs, got %d", len(prompt.defs))
	}
	if prompt.defs[0].Name != "env" || prompt.editors[0].String() != "staging" {
		t.Fatalf("expected env editor initialized with 'staging', got %q", prompt.editors[0].String())
	}
	if prompt.defs[1].Name != "version" || prompt.editors[1].String() != "" {
		t.Fatalf("expected version editor initialized with '', got %q", prompt.editors[1].String())
	}

	// 3. Submitting with required variable 'version' empty fails and shows error.
	state.Click(hitSnippetVarSubmit)
	if state.modal != modalSnippetVariables {
		t.Fatalf("dialog should stay open when required variable is missing")
	}
	if prompt.dialogError != state.messages.Tools.SnippetVarRequired {
		t.Fatalf("expected error %q, got %q", state.messages.Tools.SnippetVarRequired, prompt.dialogError)
	}

	// 4. Entering value for required variable and pressing Enter submits successfully.
	app.TextEvent{Kind: app.TextInsert, Text: "v1.2.3"}.Apply(state.EditorFor(hitSnippetVarList.At(1)))
	state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitSnippetVarList.At(1)})

	if state.modal != modalNone {
		t.Fatalf("expected modal closed after valid submit, got %d", state.modal)
	}
	expectedTyped := "ping 127.0.0.1\rdeploy --env staging --tag v1.2.3\r"
	if got := shell.typed(); got != expectedTyped {
		t.Fatalf("shell got %q, want %q", got, expectedTyped)
	}

	// 5. Running the snippet again pre-fills previously entered values.
	state.sendSnippet(1)
	if state.modal != modalSnippetVariables {
		t.Fatalf("expected modalSnippetVariables, got %d", state.modal)
	}
	if state.snippetPrompt.editors[1].String() != "v1.2.3" {
		t.Fatalf("expected cached value 'v1.2.3', got %q", state.snippetPrompt.editors[1].String())
	}

	// 6. Canceling closes dialog without sending anything new.
	state.Click(hitSnippetVarCancel)
	if state.modal != modalNone {
		t.Fatalf("expected modal closed after cancel, got %d", state.modal)
	}
	if got := shell.typed(); got != expectedTyped {
		t.Fatalf("shell got %q, want %q after cancel", got, expectedTyped)
	}
}

func TestSnippetDynamicVariablesFromPage(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	shell := world.conns[0].shell(t)
	state.Wake()
	state.activeSection = sectionSnippets

	state.snippets.list = []snippet{
		{
			Name:    "Restart",
			Command: "systemctl restart {{service:nginx}}",
			Run:     true,
		},
	}

	state.sendSnippetPicked(0, &state.sessions[0])
	if state.modal != modalSnippetVariables {
		t.Fatalf("expected modalSnippetVariables, got %d", state.modal)
	}
	if !state.snippetPrompt.fromPage {
		t.Fatal("expected fromPage to be true")
	}

	// Submit with default value
	state.Click(hitSnippetVarSubmit)
	if state.modal != modalNone {
		t.Fatalf("expected modalNone, got %d", state.modal)
	}
	if state.activeSection != sectionTerminal {
		t.Fatalf("expected activeSection = sectionTerminal (%d), got %d", sectionTerminal, state.activeSection)
	}
	if !strings.Contains(shell.typed(), "systemctl restart nginx\r") {
		t.Fatalf("expected command to be sent, got %q", shell.typed())
	}
}

// A snippet with more variables than the list holds must keep the preview and
// the actions inside the dialog, and every field reachable: the list scrolls,
// and the field that takes the focus is brought back into it.
func TestSnippetVariablesDialogManyFieldsStayReachable(t *testing.T) {
	state := newTestState(t, testOptions(t))
	var command strings.Builder
	command.WriteString("build")
	for i := 0; i < 14; i++ {
		fmt.Fprintf(&command, " --v%d={{v%d:%d}}", i, i, i)
	}
	state.snippets.list = []snippet{
		{Name: "Batch", Command: command.String(), Run: true},
	}
	state.openSnippetVariablesDialog(0, 0, false)

	view, err := headless.Open(app.Config{
		Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint,
		OnKey: state.Key, OnPointer: state.Pointer, OnShortcut: state.Shortcut,
		OnScroll: state.Scroll, OnResize: state.Resize,
		TextInput: func(c app.TextInputContext) *app.TextInput {
			if e := state.EditorFor(c.ID); e != nil {
				return &app.TextInput{Editor: e}
			}
			return nil
		},
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}

	prompt := &state.snippetPrompt
	if !prompt.listSet {
		t.Fatal("the fields were not declared in a scroll area")
	}
	list := prompt.list.Rect()

	// The list scrolls: fourteen fields no longer fit the height it holds.
	scroll := prompt.list.ScrollState()
	if scroll.Content <= scroll.Viewport {
		t.Fatalf("14 fields did not make the list scrollable: content %v, viewport %v", scroll.Content, scroll.Viewport)
	}

	// The preview and the actions stay inside the window: they were pushed
	// below its edge before the list scrolled.
	window := gfx.Rect{Width: 1400, Height: 900}
	if got := prompt.preview.Rect(); got.Y+got.Height > window.Height || got.Width <= 0 {
		t.Fatalf("preview box %+v is outside the window", got)
	}
	for _, label := range []string{state.messages.Tools.SnippetVarRun, state.messages.Cancel} {
		var hit gfx.Hit
		for _, candidate := range view.Snapshot().Hits {
			if candidate.Label == label && !candidate.IsLabel {
				hit = candidate
				break
			}
		}
		if hit.Bounds.Width <= 0 || hit.Bounds.Y+hit.Bounds.Height > window.Height {
			t.Fatalf("dialog action %q has no place in the window: %+v", label, hit.Bounds)
		}
	}

	// Wheel notches over the list scroll it — through the window's own scroll
	// routing — until the last field is inside the list's viewport: every
	// field is reachable with the wheel and the scrollbar.
	for i := 0; i < len(prompt.defs); i++ {
		if err := view.Scroll(gfx.ScrollEvent{X: list.X + list.Width/2, Y: list.Y + list.Height/2, DeltaY: 48, Precise: true}); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
	}
	if scroll.Offset <= 0 {
		t.Fatalf("the wheel never moved the list: offset %v", scroll.Offset)
	}
	last := len(prompt.defs) - 1
	slot := prompt.slots[last].Rect()
	list = prompt.list.Rect()
	if slot.Y < list.Y || slot.Y+slot.Height > list.Y+list.Height {
		t.Fatalf("the last field %+v is not reachable in the list %+v", slot, list)
	}
}
