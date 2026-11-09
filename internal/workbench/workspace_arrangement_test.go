package workbench

import (
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

func TestWorkspaceLibraryCardsFollowLayoutAndKeepOpenInHeader(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces
	state.templates = []WorkspaceTemplate{mustWorkspaceTemplate(t, "API", workspaceCatalogIDs(state, 0, 1, 2), 2)}
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	header := panelHit(t, canvas, hitWorkspaceTemplateRowList.At(0)).Bounds
	open := panelHit(t, canvas, hitWorkspaceTemplateOpen.At(0)).Bounds
	if open.Y < header.Y || open.Y+open.Height > header.Y+header.Height {
		t.Fatalf("open button has its own row: header %+v, button %+v", header, open)
	}
	first := panelHit(t, canvas, hitWorkspaceTemplateCards.At(0)).Bounds
	second := panelHit(t, canvas, hitWorkspaceTemplateCards.At(1)).Bounds
	last := panelHit(t, canvas, hitWorkspaceTemplateCards.At(2)).Bounds
	if first.Y != second.Y || first.X >= second.X || last.Y <= first.Y || last.Width < first.Width*1.9 {
		t.Fatalf("cards do not follow the launched layout: %+v %+v %+v", first, second, last)
	}
}

func TestWorkspaceArrangementApplyPreservesSessionsAndTemplate(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	workspace := launchTemporaryWorkspace(t, state, 0, 1, 2)
	sessions := sessionIDs(state.sessions)
	active, dials := state.activeSessionID, len(world.configs)
	before := paneSessionIDs(state.deck.Panes(workspace, nil))
	state.openWorkspaceArrangement()
	state.workspaceLayoutDraft.layout = layoutcore.GridSpec(2)
	moveWorkspaceItem(state.workspaceLayoutDraft.items, 0, 2)
	if !slices.Equal(before, paneSessionIDs(state.deck.Panes(workspace, nil))) {
		t.Fatal("staging a layout changed live panes")
	}
	state.applyWorkspaceArrangement()
	after := paneSessionIDs(state.deck.Panes(workspace, nil))
	if !slices.Equal(after, []uint32{before[1], before[2], before[0]}) || !isGrid(state.managedWorkspaceLayouts[workspace], 2) {
		t.Fatalf("apply order %v, columns %+v", after, state.managedWorkspaceLayouts[workspace])
	}
	if !slices.Equal(sessions, sessionIDs(state.sessions)) || state.activeSessionID != active || len(world.configs) != dials {
		t.Fatal("applying layout changed session identity or reconnected a host")
	}
}

func TestWorkspaceArrangementRejectsChangedRuntime(t *testing.T) {
	state := newTestState(t, testOptions(t))
	workspace := launchTemporaryWorkspace(t, state, 0, 1)
	state.openWorkspaceArrangement()
	state.workspaceLayoutDraft.layout = layoutcore.GridSpec(1)
	panes := state.deck.Panes(workspace, nil)
	order := []layoutcore.PaneID{panes[0].ID, panes[1].ID}
	if err := state.deck.SetLayout(workspace, order, layoutcore.GridSpec(2)); err != nil {
		t.Fatal(err)
	}
	state.applyWorkspaceArrangement()
	if state.workspaceLayoutDraft.err == "" || !isGrid(state.managedWorkspaceLayouts[workspace], 2) {
		t.Fatal("stale runtime layout was applied")
	}
}
