//go:build darwin || windows || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
)

func hostFormEditMenuView(t *testing.T, state *State) *headless.Session {
	t.Helper()
	view, err := headless.Open(state.WindowConfig(app.Config{
		Width: 1100, Height: 820, Tree: Tree(), Paint: state.Paint,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnPointer: state.Pointer,
		OnScroll: state.Scroll, OnResize: state.Resize,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	return view
}

// The New Host form's text fields take Paste from a right-click, with no click
// into the field first: the system's text menu opens with the catalog's words,
// the field takes the keyboard, and the text goes in at the caret as one line.
func TestNewHostFieldPastesFromTheEditMenu(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openHostDialog()
	state.editor.address.Insert("aXz")
	view := hostFormEditMenuView(t, state)
	if view.Snapshot().Focused == hitDialogAddress {
		t.Fatal("the address must not be focused before the right-click")
	}
	state.editor.address.SetSelection(text.Range{Start: 1, End: 1})
	view.Driver().SetClipboard("b\r\nc")
	hostFilterContextClick(t, view, hitDialogAddress, true)
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	var paste app.TextMenuItem
	for _, item := range view.Driver().TextMenu() {
		if item.Command == app.TextMenuPaste {
			paste = item
		}
	}
	if paste.Title == "" {
		t.Fatal("right-click did not open the text menu on the address field")
	}
	if paste.Title != state.messages.MenuPaste {
		t.Fatalf("Paste reads %q, want the catalog's %q", paste.Title, state.messages.MenuPaste)
	}
	if err := view.Driver().ChooseTextMenu(app.TextMenuPaste); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if got, want := state.editor.address.String(), "ab cXz"; got != want {
		t.Fatalf("pasted value = %q, want %q", got, want)
	}
	if focused := view.Snapshot().Focused; focused != hitDialogAddress {
		t.Fatalf("focus after paste = %s, want the address field", view.Snapshot().Tree.Text(focused))
	}
}
