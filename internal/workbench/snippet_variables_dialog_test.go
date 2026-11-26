//go:build windows || darwin || linux

package workbench

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"

	"motty/internal/i18n"
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

// The help beside the Command title is where the editor tells how a variable is
// written (#1647): a user who wrote another client's syntax got no prompt and no
// hint. It sits in the title row, after the title and clear of the command area,
// for a saved snippet as much as for a new one.
func TestSnippetEditorTellsHowAVariableIsWritten(t *testing.T) {
	state := snippetTestState(t, snippet{Name: "deploy", Command: "make deploy"})
	check := func(t *testing.T) {
		t.Helper()
		m := state.messages.Tools
		canvas, _ := paintedTexts(state)
		help, ok := hitByID(canvas, hitSnippetInlineCommandHelp)
		if !ok {
			t.Fatalf("the Command title has no help beside it: hits=%+v", canvas.Hits())
		}
		if help.Hint != m.SnippetVarNote {
			t.Fatalf("help hint = %q, want %q", help.Hint, m.SnippetVarNote)
		}
		control, ok := hitByID(canvas, hitSnippetInlineCommand)
		if !ok {
			t.Fatal("the command area is not on the page")
		}
		var title gfx.Rect
		for _, text := range canvas.Texts() {
			if text.Value == m.SnippetCommand {
				title = text.Bounds
			}
		}
		place := help.Bounds
		if title.Width <= 0 || place.Width <= 0 || place.X < title.X+title.Width ||
			place.Y < title.Y || place.Y+place.Height > title.Y+title.Height {
			t.Fatalf("help %+v is not beside the title %+v", place, title)
		}
		if place.Y+place.Height > control.Bounds.Y {
			t.Fatalf("help %+v lies over the command area %+v", place, control.Bounds)
		}
	}
	check(t)
	state.startInlineSnippetDraft("")
	check(t)
}

// What the note shows has to be what the parser reads, in every catalog: one
// variable, and one with a default.
func TestSnippetVariableNoteShowsSyntaxTheParserReads(t *testing.T) {
	example := regexp.MustCompile(`\{\{[^{}]*\}\}`)
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		note := i18n.For(language).Tools.SnippetVarNote
		plain, withDefault := false, false
		for _, token := range example.FindAllString(note, -1) {
			variables := parseSnippetVariables(token)
			if len(variables) != 1 {
				t.Fatalf("%v: %q in %q is not read as one variable: %+v", language, token, note, variables)
			}
			plain = plain || !variables[0].HasDefault
			withDefault = withDefault || variables[0].HasDefault
		}
		if !plain || !withDefault {
			t.Fatalf("%v: note %q does not show a variable (%v) and one with a default (%v)", language, note, plain, withDefault)
		}
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
	list = prompt.list.Rect()
	var slot gfx.Rect
	for _, candidate := range view.Snapshot().Hits {
		if candidate.ID == hitSnippetVarList.At(last) {
			slot = candidate.Bounds
		}
	}
	if slot.Width == 0 || slot.Y < list.Y || slot.Y+slot.Height > list.Y+list.Height {
		t.Fatalf("the last field %+v is not reachable in the list %+v", slot, list)
	}
}

// A long command wraps inside the preview and scrolls there, instead of
// running off the right edge where only part of it can be read.
func TestSnippetVariablesPreviewWrapsAndScrolls(t *testing.T) {
	state := newTestState(t, testOptions(t))
	command := "deploy " + strings.Repeat("--set=some.really.long.value ", 12) + "{{target}}"
	state.snippets.list = []snippet{
		{Name: "Long", Command: command, Run: true},
	}
	state.openSnippetVariablesDialog(0, 0, false)

	view, err := headless.Open(app.Config{
		Width: 900, Height: 800, Tree: Tree(), Paint: state.Paint,
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
	if !prompt.previewSet {
		t.Fatal("the preview was not declared in a scroll area")
	}
	if got := prompt.preview.Rect().Height; got != snippetVarPreviewHeight {
		t.Fatalf("the preview box is %v tall, want the %v the body reserves", got, snippetVarPreviewHeight)
	}
	if prompt.previewScroll == nil {
		t.Fatal("the preview scroll state was not kept")
	}
	scroll := *prompt.previewScroll
	if scroll.Content <= scroll.Viewport {
		t.Fatalf("a long command did not wrap: preview content %v, viewport %v", scroll.Content, scroll.Viewport)
	}
}
