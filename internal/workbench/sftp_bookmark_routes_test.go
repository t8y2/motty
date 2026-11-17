package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
)

// How the bookmark editor's buttons and keys reach it, through the real window. They hold the
// routes the buttons are declared with, so that how the buttons are painted can change.
func openBookmarkEditor(t *testing.T) (*sftpWindow, *headless.Session) {
	t.Helper()
	_, w, _, host, _ := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	w.session.host.CatalogID = "native-bookmark"
	w.window.Invalidate()
	view.Driver().Pump()
	r := dualHit(t, view, hitSFTPLocationProfiles)
	if err := view.Click(r.X+r.Width/2, r.Y+r.Height/2); err != nil {
		t.Fatal(err)
	}
	if !w.bookmark.open {
		t.Fatal("the bookmark editor did not open")
	}
	return w, view
}

func clickBookmarkButton(t *testing.T, w *sftpWindow, view *headless.Session, id gfx.ID) {
	t.Helper()
	w.window.Invalidate()
	view.Driver().Pump()
	r := dualHit(t, view, id)
	if err := view.Click(r.X+r.Width/2, r.Y+r.Height/2); err != nil {
		t.Fatal(err)
	}
}

func TestBookmarkEditorButtonsReachTheirActions(t *testing.T) {
	t.Run("new clears the name and asks for the focus", func(t *testing.T) {
		w, view := openBookmarkEditor(t)
		w.bookmark.fields[0].Insert("something")
		clickBookmarkButton(t, w, view, hitSFTPProfileNew)
		if w.bookmark.fields[0].String() != "" || w.bookmark.editing {
			t.Fatalf("New left name %q editing=%v", w.bookmark.fields[0].String(), w.bookmark.editing)
		}
	})
	t.Run("cancel closes the editor", func(t *testing.T) {
		w, view := openBookmarkEditor(t)
		clickBookmarkButton(t, w, view, hitSFTPBookmarkCancel)
		if w.bookmark.open {
			t.Fatal("Cancel left the editor open")
		}
	})
	t.Run("save with no name reports the problem and stays open", func(t *testing.T) {
		w, view := openBookmarkEditor(t)
		w.bookmark.fields[0] = text.Editor{}
		clickBookmarkButton(t, w, view, hitSFTPBookmarkSave)
		if !w.bookmark.open || w.bookmark.errorText == "" {
			t.Fatalf("open=%v error=%q", w.bookmark.open, w.bookmark.errorText)
		}
	})
	t.Run("delete is inert while nothing is being edited", func(t *testing.T) {
		w, view := openBookmarkEditor(t)
		before := len(w.visibleProfiles())
		clickBookmarkButton(t, w, view, hitSFTPProfileDelete)
		if len(w.visibleProfiles()) != before {
			t.Fatal("Delete removed a profile that was not being edited")
		}
	})
}

func TestBookmarkEditorKeysReachTheirActions(t *testing.T) {
	t.Run("escape closes", func(t *testing.T) {
		w, view := openBookmarkEditor(t)
		if err := view.Keys("escape"); err != nil {
			t.Fatal(err)
		}
		if w.bookmark.open {
			t.Fatal("Escape left the editor open")
		}
	})
	t.Run("enter in the name field saves, which an empty name refuses", func(t *testing.T) {
		w, view := openBookmarkEditor(t)
		w.bookmark.fields[0] = text.Editor{}
		w.window.Invalidate()
		view.Driver().Pump()
		r := dualHit(t, view, hitSFTPBookmarkFields.At(0))
		if err := view.Click(r.X+r.Width/2, r.Y+r.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := view.Keys("enter"); err != nil {
			t.Fatal(err)
		}
		if !w.bookmark.open || w.bookmark.errorText == "" {
			t.Fatalf("Enter on an empty name: open=%v error=%q", w.bookmark.open, w.bookmark.errorText)
		}
	})
}
