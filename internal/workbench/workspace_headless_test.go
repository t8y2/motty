//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/sshconn"
)

func TestHeadlessIdleMultiPaneWorkspaceDoesNotRepaint(t *testing.T) {
	state := threeVisiblePaneState(t)
	for index := range state.sessions {
		state.sessions[index].phase = sshconn.PhaseConnected
		state.sessions[index].term.live = true
	}
	paints := 0
	session, err := headless.Open(app.Config{
		Title: "idle workspace", Width: 1400, Height: 900,
		Paint: func(canvas *gfx.Canvas, size gfx.Size) {
			paints++
			state.Paint(canvas, size)
		},
		OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	before := paints
	for range 100 {
		if err := session.Driver().Pump(); err != nil {
			t.Fatal(err)
		}
	}
	if paints != before {
		t.Fatalf("idle workspace repainted %d times", paints-before)
	}
}

func TestHeadlessWorkspaceLaunchAndExplicitBroadcastFlow(t *testing.T) {
	world, options := newWorld(t)
	var invalidator app.Invalidator
	options.Invalidate = invalidator.Invalidate
	state := newTestState(t, options)
	var input app.TextInput
	view, err := headless.Open(app.Config{
		Title: "workspace flow", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, TextInput: func(context app.TextInputContext) *app.TextInput {
			if context.Terminal {
				return state.TerminalInput(context)
			}
			if input.Editor = state.EditorFor(context.ID); input.Editor == nil {
				return nil
			}
			return &input
		},
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
		OnResize: state.Resize, Invalidator: &invalidator, OnWake: state.Wake,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()

	click := func(id gfx.ID) {
		t.Helper()
		hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
		if !ok || hit.Block || hit.Disabled {
			t.Fatalf("control %d is not clickable: %+v", id, hit)
		}
		if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
	}

	click(hitWorkspaces)
	click(hitWorkspaceNew)
	click(workspaceDraftHostID(1))
	click(workspaceDraftHostID(0))
	click(hitWorkspaceDraftSubmit)

	workspaceID := state.deck.ActiveWorkspace()
	if workspaceID == 0 || state.activeSection != sectionTerminal || len(world.conns) != 2 {
		t.Fatalf("launch left workspace=%d section=%d dials=%d", workspaceID, state.activeSection, len(world.conns))
	}
	sessions := state.deck.Panes(workspaceID, nil)
	if len(sessions) != 2 || sessions[0].SessionID == 0 || sessions[1].SessionID == 0 {
		t.Fatalf("launched panes = %+v", sessions)
	}
	if state.broadcast != 0 {
		t.Fatalf("launch enabled broadcast for workspace %d", state.broadcast)
	}

	for index := range world.conns {
		world.conns[index].set(func(status *sshconn.Status) {
			status.Phase = sshconn.PhaseConnected
			status.ServerVersion = "SSH-2.0-Test"
			status.Method = "password"
		})
	}
	invalidator.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	for index := range world.conns {
		world.conns[index].shell(t).say("prompt$ ")
	}
	invalidator.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}

	for index := range world.conns {
		if got := world.conns[index].shell(t).typed(); got != "" {
			t.Fatalf("connection %d received automatic input %q", index, got)
		}
	}
	if hit, ok := hitByIDSnapshot(view.Snapshot().Hits, hitWorkspaceHeaderBroadcast); !ok || hit.Disabled {
		t.Fatalf("broadcast is not available after both shells connect: %+v", hit)
	}

	click(hitWorkspaceHeaderBroadcast)
	if state.broadcast != workspaceID {
		t.Fatalf("broadcast = %d, want workspace %d", state.broadcast, workspaceID)
	}
	sourceID := uint32(sessions[0].SessionID)
	if !state.ActivatePane(sourceID) {
		t.Fatal("activate source pane")
	}
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	source, _ := state.sessionByIDPointer(sourceID)
	terminalLabel := fmt.Sprintf(state.messages.LabelTerminalFmt, source.Address)
	if err := view.ClickLabel(terminalLabel); err != nil {
		t.Fatal(err)
	}
	if err := view.Type("uptime\r"); err != nil {
		t.Fatal(err)
	}

	if got := world.conns[0].shell(t).typed(); got != "uptime\r" {
		t.Fatalf("source shell received %q", got)
	}
	if got := world.conns[1].shell(t).typed(); got != "uptime\r" {
		t.Fatalf("peer shell received %q", got)
	}

	other, _, err := state.deck.Create("Other")
	if err != nil {
		t.Fatal(err)
	}
	if !state.activateWorkspace(other) || state.broadcast != 0 {
		t.Fatalf("tab change left broadcast %d", state.broadcast)
	}
}

func TestHeadlessWorkspaceTemplateKeepsSavedOrderAndMissingHosts(t *testing.T) {
	world, options := newWorld(t)
	var invalidator app.Invalidator
	options.Invalidate = invalidator.Invalidate
	state := newTestState(t, options)
	template := mustWorkspaceTemplate(t, "Mixed", []string{
		workspaceHostCatalogID(t, state, "Staging"),
		"host_deleted",
		workspaceHostCatalogID(t, state, "Production"),
	}, 2)
	state.templates = []WorkspaceTemplate{template}
	view, err := headless.Open(app.Config{
		Title: "workspace template", Width: 1200, Height: 800, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
		OnShortcut: state.Shortcut, OnResize: state.Resize,
		Invalidator: &invalidator, OnWake: state.Wake,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()

	if err := view.ClickLabel(state.messages.NavWorkspaces); err != nil {
		t.Fatal(err)
	}
	if err := view.DoubleClickLabel(template.Name); err != nil {
		t.Fatal(err)
	}
	workspaceID := state.deck.ActiveWorkspace()
	if workspaceID == 0 || len(world.configs) != 2 ||
		world.configs[0].Host != "staging.example.com" || world.configs[1].Host != "prod.example.com" {
		t.Fatalf("template launch workspace=%d dials=%+v", workspaceID, world.configs)
	}
	if len(state.fileNotices.items) != 1 || state.fileNotices.items[0].kind != components.ToastWarning {
		t.Fatalf("missing-host notice = %+v", state.fileNotices.items)
	}
	if len(state.deck.Panes(workspaceID, nil)) != 2 || state.broadcast != 0 {
		t.Fatalf("template panes=%d broadcast=%d", len(state.deck.Panes(workspaceID, nil)), state.broadcast)
	}
}
