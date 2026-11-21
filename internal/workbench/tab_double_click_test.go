package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

func doubleClickTab(state *State, workspace layoutcore.WorkspaceID) gfx.EventResult {
	return pointer(state, gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		Target: hitTabList.At(int(workspace)), ClickCount: 2, X: 100, Y: 18,
	})
}

func TestDoubleClickOnATabDuplicatesItBesideTheSource(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.tabDoubleClick = tabDoubleClickDuplicate
	connect(t, state, world)
	source := state.deck.ActiveWorkspace()
	sourceSession := state.sessions[0]
	openEmptyTab(state)
	openEmptyTab(state)
	state.Click(hitTabList.At(int(source)))

	result := doubleClickTab(state, source)
	if !result.Handled || !result.Repaint {
		t.Fatalf("double click = %+v", result)
	}

	tabs := state.deck.Workspaces(nil)
	if len(tabs) != 4 || tabs[0].ID != source {
		t.Fatalf("tabs = %+v, want the source first", tabs)
	}
	duplicateWorkspace := state.deck.ActiveWorkspace()
	if duplicateWorkspace == source || tabs[1].ID != duplicateWorkspace {
		t.Fatalf("duplicate tab = %d, tabs = %+v; want it right of the source", duplicateWorkspace, tabs)
	}
	if len(state.sessions) != 2 {
		t.Fatalf("sessions = %d, want the source and one copy", len(state.sessions))
	}
	duplicate := state.sessions[1]
	if duplicate.HostID != sourceSession.HostID || duplicate.Address != sourceSession.Address || duplicate.host.Label != sourceSession.host.Label {
		t.Fatalf("duplicate = %+v, source = %+v", duplicate.host, sourceSession.host)
	}
	if duplicate.term == nil || sourceSession.term == nil || duplicate.term == sourceSession.term || duplicate.term.screen == sourceSession.term.screen {
		t.Fatal("the duplicate shares the source terminal")
	}
	// The two empty tabs follow the copy.
	for _, tab := range tabs[2:] {
		paneID := state.deck.ActivePane(tab.ID)
		if pane, ok := state.deck.Pane(tab.ID, paneID); !ok || pane.SessionID != 0 {
			t.Fatalf("tab %d = %+v, want it left empty", tab.ID, pane)
		}
	}
}

func TestTabMenuDuplicateAlsoPutsTheCopyBesideItsSource(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)
	source := state.deck.ActiveWorkspace()
	openEmptyTab(state)
	openEmptyTab(state)
	state.Click(hitTabList.At(int(source)))
	rightClickTab(state, hitTabList.At(int(source)))
	chooseMenu(t, state, hitTabMenuDuplicate)

	tabs := state.deck.Workspaces(nil)
	if len(tabs) != 4 || tabs[0].ID != source || tabs[1].ID != state.deck.ActiveWorkspace() {
		t.Fatalf("tabs = %+v, active = %d; want the copy right of the source", tabs, state.deck.ActiveWorkspace())
	}
}

func TestDoubleClickCanKeepTheInlineRename(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.tabDoubleClick = tabDoubleClickRename
	connect(t, state, world)
	workspace := state.workspaceTabs[0].ID
	before := len(state.sessions)

	result := doubleClickTab(state, workspace)
	if !result.Handled || !state.tabInlineActive() || state.tabInline.workspace != workspace {
		t.Fatalf("double click = %+v, inline = %+v", result, state.tabInline)
	}
	if len(state.sessions) != before {
		t.Fatal("the rename setting duplicated the session")
	}
}

func TestDoubleClickCanDoNothing(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.tabDoubleClick = tabDoubleClickNone
	connect(t, state, world)
	workspace := state.workspaceTabs[0].ID

	result := doubleClickTab(state, workspace)
	if !result.Handled {
		t.Fatal("the double click was left to the tab underneath")
	}
	if state.tabInlineActive() || len(state.workspaceTabs) != 1 || len(state.sessions) != 1 {
		t.Fatalf("do nothing: inline=%v tabs=%d sessions=%d", state.tabInlineActive(), len(state.workspaceTabs), len(state.sessions))
	}
}

func TestTabDoubleClickSettingIsSavedOnThisDevice(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	if state.tabDoubleClick != tabDoubleClickRename {
		t.Fatalf("default double click = %q, want rename", state.tabDoubleClick)
	}
	state.Click(hitSettings)
	if !contains(paintedAt(state, 1400, 4000), state.messages.SettingsTabDoubleClick) {
		t.Fatal("the double-click row is missing from settings")
	}
	clickSettingsChoiceInRow(t, state, state.messages.SettingsTabDoubleClick, state.messages.MenuDuplicateTab)
	if state.tabDoubleClick != tabDoubleClickDuplicate {
		t.Fatalf("chosen double click = %q, want duplicate", state.tabDoubleClick)
	}
	state.save()

	restored := newTestState(t, state.options)
	if restored.tabDoubleClick != tabDoubleClickDuplicate {
		t.Fatalf("restored double click = %q, want duplicate", restored.tabDoubleClick)
	}
	for _, test := range []struct {
		saved string
		want  tabDoubleClickAction
	}{
		{"", tabDoubleClickRename},
		{"duplicate", tabDoubleClickDuplicate},
		{"rename", tabDoubleClickRename},
		{"none", tabDoubleClickNone},
		{"nonsense", tabDoubleClickRename},
	} {
		if got := tabDoubleClickFromString(test.saved); got != test.want {
			t.Errorf("tabDoubleClickFromString(%q) = %q, want %q", test.saved, got, test.want)
		}
	}
}

func TestDuplicateTabShortcutOpensACopyBesideTheSource(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)
	source := state.deck.ActiveWorkspace()
	openEmptyTab(state)
	state.Click(hitTabList.At(int(source)))

	chord := state.shortcuts.chords.Chord(shortcutDuplicateTab)
	if !chord.Valid() {
		t.Fatal("the duplicate tab shortcut has no default")
	}
	result := pressChord(state, chord)
	if !result.Handled || !result.Repaint {
		t.Fatalf("shortcut = %+v", result)
	}
	tabs := state.deck.Workspaces(nil)
	if len(tabs) != 3 || tabs[0].ID != source || tabs[1].ID != state.deck.ActiveWorkspace() {
		t.Fatalf("tabs = %+v, active = %d; want the copy right of the source", tabs, state.deck.ActiveWorkspace())
	}
	if state.activeSessionID == 0 || result.Focus.IsZero() {
		t.Fatal("the shortcut did not focus the duplicate terminal")
	}
}

func TestTheDuplicateTabShortcutRowIsInSettings(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	showShortcutSettings(state)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 3400})
	row, ok := hitByID(&canvas, hitShortcutRowList.At(int(shortcutDuplicateTab-1)))
	if !ok {
		t.Fatal("the duplicate tab shortcut row is missing")
	}
	if row.Label != state.shortcuts.labels[shortcutDuplicateTab] {
		t.Fatalf("shortcut button = %q, want %q", row.Label, state.shortcuts.labels[shortcutDuplicateTab])
	}
	var texts []string
	for _, text := range canvas.Texts() {
		texts = append(texts, text.Value)
	}
	if !contains(texts, state.messages.MenuDuplicateTab) {
		t.Fatalf("the shortcut row does not name the action: %q", texts)
	}
}
