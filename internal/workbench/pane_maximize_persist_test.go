package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	layoutcore "motty/internal/workspace"
	"reflect"
	"testing"
)

func TestPaneMaximizePersistsIndependentTabs(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	first := state.deck.ActiveWorkspace()
	state.togglePaneMaximized(0)
	panes := state.deck.Panes(first, nil)
	state.selectWorkspaceFocusTab(panes[0].ID, true)
	state.Click(hitLocalTerminal)
	second := state.deck.ActiveWorkspace()
	if first == second {
		t.Fatal("new terminal did not open a workspace")
	}
	state.save()
	before := state.savedWorkspaceDeck()
	restored := newTestState(t, options)
	if !restored.deck.PaneMaximized(first) || restored.deck.PaneMaximized(second) {
		t.Fatal("restored modes were not independent")
	}
	if restored.deck.ActiveWorkspace() != second || restored.deck.ActivePane(first) != panes[0].ID {
		t.Fatal("restored selection differs")
	}
	if !reflect.DeepEqual(before, restored.savedWorkspaceDeck()) {
		t.Fatal("saved workspace state changed across restart")
	}
	restored.activateWorkspace(first)
	if !restored.deck.PaneMaximized(first) {
		t.Fatal("tab switch reset its mode")
	}
}

func TestPaneMaximizeShortcutRebindAndToggle(t *testing.T) {
	state, _, _ := twoLivePanes(t)
	workspace := state.deck.ActiveWorkspace()
	result := state.Shortcut(gfx.KeyEvent{Text: "m", Modifiers: keymap.Primary | gfx.ModifierShift})
	if !result.Handled || !state.deck.PaneMaximized(workspace) {
		t.Fatal("default shortcut did not maximize")
	}
	state.openShortcutCapture(shortcutPaneMaximize)
	custom := keymap.Chord{Key: gfx.KeyF8}
	result = state.Shortcut(gfx.KeyEvent{Key: custom.Key})
	if !result.Handled || state.shortcuts.chords.Chord(shortcutPaneMaximize) != custom {
		t.Fatal("capture did not bind new shortcut")
	}
	if !state.deck.PaneMaximized(workspace) {
		t.Fatal("capture activated the shortcut")
	}
	result = state.Shortcut(gfx.KeyEvent{Key: custom.Key})
	if !result.Handled || state.deck.PaneMaximized(workspace) {
		t.Fatal("custom shortcut did not restore")
	}
}

func TestPaneMaximizeRestoresManagedTabs(t *testing.T) {
	state := newTestState(t, testOptions(t))
	workspace := launchTemporaryWorkspace(t, state, 0, 1, 2)
	state.openWorkspaceArrangement()
	state.workspaceLayoutDraft.layout = layoutcore.LayoutSpec{Kind: layoutcore.LayoutTabs}
	state.applyWorkspaceArrangement()
	state.Resize(gfx.Size{Width: 1400, Height: 900})
	original := state.workspacePixelBounds(state.viewport)
	state.togglePaneMaximized(0)
	maximized := state.workspacePixelBounds(state.viewport)
	if maximized.Y != original.Y-workspacePaneTabsHeight || maximized.Height != original.Height {
		t.Fatal("top and bottom strips reserve space twice")
	}
	state.togglePaneMaximized(0)
	if state.workspacePixelBounds(state.viewport) != original || !state.workspaceTabbed(workspace) {
		t.Fatal("restore changed the base Tabs layout")
	}
}

func TestPaneMaximizeSplitFromTabsShowsNewTree(t *testing.T) {
	state := newTestState(t, testOptions(t))
	workspace := launchTemporaryWorkspace(t, state, 0, 1, 2)
	state.openWorkspaceArrangement()
	state.workspaceLayoutDraft.layout = layoutcore.LayoutSpec{Kind: layoutcore.LayoutTabs}
	state.applyWorkspaceArrangement()
	state.Resize(gfx.Size{Width: 3000, Height: 1800})
	state.togglePaneMaximized(0)
	if !state.splitActivePane(layoutcore.Vertical) {
		t.Fatal("split from maximized Tabs failed")
	}
	if state.deck.PaneMaximized(workspace) || state.workspaceTabbed(workspace) {
		t.Fatal("new split remained hidden in a single-pane view")
	}
	for _, pane := range state.Panes(nil) {
		if pane.Bounds.Width <= 0 || pane.Bounds.Height <= 0 {
			t.Fatal("new split tree was not fully shown")
		}
	}
}
