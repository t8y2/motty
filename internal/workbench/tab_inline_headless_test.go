//go:build darwin || windows || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

// openTabInlineSession drives a headless window whose typing goes through the
// same EditorFor resolver the real app installs, so the tab's inline field is
// edited the way a native text field is.
func openTabInlineSession(t *testing.T, state *State) *headless.Session {
	t.Helper()
	var input app.TextInput
	session, err := headless.Open(app.Config{
		Title: "tab inline rename", Width: 1400, Height: 900,
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			editor := state.EditorFor(context.ID)
			if editor == nil {
				return nil
			}
			input = app.TextInput{Editor: editor}
			return &input
		},
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	if err := session.Rest(); err != nil {
		t.Fatal(err)
	}
	return session
}

func tabHitCenter(t *testing.T, session *headless.Session, id gfx.ID) (float32, float32) {
	t.Helper()
	hit, ok := hitByIDSnapshot(session.Snapshot().Hits, id)
	if !ok {
		t.Fatalf("no hit %v", id)
	}
	return hit.Bounds.X + hit.Bounds.Width/2, hit.Bounds.Y + hit.Bounds.Height/2
}

func TestDoubleClickRenamesATabInPlace(t *testing.T) {
	state, tabs := fourTabs(t)
	session := openTabInlineSession(t, state)

	before, ok := hitByIDSnapshot(session.Snapshot().Hits, hitTabList.At(int(tabs[1])))
	if !ok {
		t.Fatal("no tab")
	}
	x, y := tabHitCenter(t, session, hitTabList.At(int(tabs[1])))
	if err := session.DoubleClick(x, y); err != nil {
		t.Fatal(err)
	}
	if !state.tabInlineActive() || state.tabInline.workspace != tabs[1] {
		t.Fatalf("the double click did not open the name field: %+v", state.tabInline)
	}
	if session.Snapshot().Editor == nil {
		t.Fatal("the name field is not edited natively")
	}
	if _, ok := hitByIDSnapshot(session.Snapshot().Hits, hitTabInline); !ok {
		t.Fatal("the name field declares no hit")
	}
	if state.tabDrag.pressed {
		t.Fatal("the double click started a tab drag")
	}

	if err := session.Type("Build box"); err != nil {
		t.Fatal(err)
	}
	// A typed name never narrows the tab it is being written into.
	after, _ := hitByIDSnapshot(session.Snapshot().Hits, hitTabList.At(int(tabs[1])))
	if after.Bounds.Width < before.Bounds.Width {
		t.Fatalf("the tab narrowed while naming it: %v < %v", after.Bounds.Width, before.Bounds.Width)
	}
	if err := session.Keys("Enter"); err != nil {
		t.Fatal(err)
	}
	if state.tabInlineActive() {
		t.Fatal("Enter did not apply the name")
	}
	if got := state.tabLabel(state.workspaceTabs[1]); got != "Build box" {
		t.Fatalf("tab label = %q", got)
	}
	if !state.workspaceTabs[1].Named {
		t.Fatal("the named choice was not recorded")
	}

	restored := newTestState(t, state.options)
	t.Cleanup(restored.Close)
	if got := restored.tabLabel(restored.workspaceTabs[1]); got != "Build box" {
		t.Fatalf("after a restart the tab is %q", got)
	}
}

func TestEscapeKeepsTheTabsName(t *testing.T) {
	state, tabs := fourTabs(t)
	session := openTabInlineSession(t, state)
	before := state.tabLabel(state.workspaceTabs[1])

	x, y := tabHitCenter(t, session, hitTabList.At(int(tabs[1])))
	if err := session.DoubleClick(x, y); err != nil {
		t.Fatal(err)
	}
	if err := session.Type("Scratch"); err != nil {
		t.Fatal(err)
	}
	if err := session.Keys("Escape"); err != nil {
		t.Fatal(err)
	}
	if state.tabInlineActive() {
		t.Fatal("Escape left the name field open")
	}
	if got := state.tabLabel(state.workspaceTabs[1]); got != before {
		t.Fatalf("Escape changed the tab: %q, want %q", got, before)
	}
}

func TestPressingAnotherTabAppliesTheName(t *testing.T) {
	state, tabs := fourTabs(t)
	session := openTabInlineSession(t, state)

	x, y := tabHitCenter(t, session, hitTabList.At(int(tabs[1])))
	if err := session.DoubleClick(x, y); err != nil {
		t.Fatal(err)
	}
	if err := session.Type("Build box"); err != nil {
		t.Fatal(err)
	}
	otherX, otherY := tabHitCenter(t, session, hitTabList.At(int(tabs[2])))
	if err := session.Click(otherX, otherY); err != nil {
		t.Fatal(err)
	}
	if state.tabInlineActive() {
		t.Fatal("a press on another tab left the name field open")
	}
	if got := state.tabLabel(state.workspaceTabs[1]); got != "Build box" {
		t.Fatalf("tab label = %q", got)
	}
	if state.deck.ActiveWorkspace() != tabs[2] {
		t.Fatal("the press on the other tab did not activate it")
	}
}

func TestTheComposingNameIsNotLostToEnter(t *testing.T) {
	state, tabs := fourTabs(t)
	session := openTabInlineSession(t, state)

	x, y := tabHitCenter(t, session, hitTabList.At(int(tabs[1])))
	if err := session.DoubleClick(x, y); err != nil {
		t.Fatal(err)
	}
	if err := session.Compose("shengchan"); err != nil {
		t.Fatal(err)
	}
	if err := session.Keys("Enter"); err != nil {
		t.Fatal(err)
	}
	if !state.tabInlineActive() {
		t.Fatal("Enter committed a composing field")
	}
	if got := state.tabLabel(state.workspaceTabs[1]); got == "shengchan" {
		t.Fatal("the unconfirmed composition became the tab's name")
	}
	state.EditorFor(hitTabInline).Unmark()
	if err := session.Keys("Enter"); err != nil {
		t.Fatal(err)
	}
	if state.tabInlineActive() {
		t.Fatal("Enter did not apply the settled name")
	}
	if got := state.tabLabel(state.workspaceTabs[1]); got != "shengchan" {
		t.Fatalf("tab label = %q, want the settled composition", got)
	}
}

func TestTheTabMenuOpensTheSameInlineField(t *testing.T) {
	state, tabs := fourTabs(t)
	session := openTabInlineSession(t, state)

	x, y := tabHitCenter(t, session, hitTabList.At(int(tabs[2])))
	if err := session.RightClick(x, y); err != nil {
		t.Fatal(err)
	}
	if !state.tabMenu.open {
		t.Fatal("the tab menu did not open")
	}
	if err := session.ClickLabel(state.messages.MenuRenameTab); err != nil {
		t.Fatal(err)
	}
	if !state.tabInlineActive() || state.tabInline.workspace != tabs[2] {
		t.Fatalf("the menu did not open the name field: %+v", state.tabInline)
	}
	if got := state.tabInline.editor.String(); got != state.tabLabel(state.workspaceTabs[2]) {
		t.Fatalf("the field starts with %q, want the tab's label %q", got, state.tabLabel(state.workspaceTabs[2]))
	}
}
