//go:build darwin || windows || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
)

func TestNewHostPlainFieldPastesAtCaretFromContextMenu(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openHostDialog()
	state.editor.address.Insert("aXz")
	clipboard, reads := "b\r\nc", 0
	state.options.ClipboardText = func() (string, error) {
		reads++
		return clipboard, nil
	}
	view, err := headless.Open(state.WindowConfig(app.Config{
		Width: 1100, Height: 820, Tree: Tree(), Paint: state.Paint,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnPointer: state.Pointer,
		OnScroll: state.Scroll, OnResize: state.Resize,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if focused := view.Snapshot().Focused; focused == hitDialogAddress {
		t.Fatal("address must be usable without clicking it first")
	}
	state.editor.address.SetSelection(text.Range{Start: 1, End: 1})
	paste := func() {
		t.Helper()
		before := reads
		hostFilterContextClick(t, view, hitDialogAddress, true)
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if reads != before {
			t.Fatal("clipboard read before choosing Paste")
		}
		for _, menu := range view.Snapshot().Hits {
			if menu.Role == gfx.RoleMenuItem && menu.Label == state.messages.MenuPaste {
				hostFilterContextClick(t, view, menu.ID, false)
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatal("right-click did not expose Paste in the New Host address field")
	}
	paste()
	if got, want := state.editor.address.String(), "ab cXz"; got != want {
		t.Fatalf("pasted value = %q, want %q", got, want)
	}
	if got := state.editor.address.Caret(); got != 4 {
		t.Fatalf("paste caret = %d, want 4", got)
	}
	if focused := view.Snapshot().Focused; focused != hitDialogAddress {
		t.Fatalf("focus after paste = %s, want address", view.Snapshot().Tree.Text(focused))
	}
	clipboard = "\r\n\x00"
	state.editor.address.SetSelection(text.Range{Start: 1, End: 4})
	paste()
	if got := state.editor.address.String(); got != "ab cXz" {
		t.Fatalf("empty normalized paste changed selected text: %q", got)
	}
	if reads != 2 {
		t.Fatalf("clipboard reads = %d, want one per Paste", reads)
	}
}

func TestHostFieldPasteTextNormalizesLineEndings(t *testing.T) {
	if got, want := hostFieldPasteText("first\r\nsecond\u2028third\tfourth\x00", false), "first second third fourth"; got != want {
		t.Fatalf("single-line paste = %q, want %q", got, want)
	}
	if got, want := hostFieldPasteText("first\r\nsecond\rthird", true), "first\nsecond\nthird"; got != want {
		t.Fatalf("multiline paste = %q, want %q", got, want)
	}
}

func TestNewHostPasswordDoesNotOfferContextPaste(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openHostDialog()
	state.editor.protocol = protocolRDP
	state.editor.auth = authPassword
	state.editor.password.Insert("keep-private")
	view, err := headless.Open(app.Config{
		Width: 1100, Height: 820, Tree: Tree(), Paint: state.Paint,
		OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, OnResize: state.Resize,
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	hostFilterContextClick(t, view, hitDialogPassword, true)
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if _, ok := view.Hit(state.messages.MenuPaste); ok {
		t.Fatal("password field offered context paste")
	}
	if got := state.editor.password.String(); got != "keep-private" {
		t.Fatal("password changed after right-click")
	}
}
