//go:build windows || darwin || linux

package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
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
