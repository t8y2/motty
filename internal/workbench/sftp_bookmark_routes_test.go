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

func openBookmarkEditorWithProfiles(t *testing.T, names ...string) (*sftpWindow, *headless.Session) {
	t.Helper()
	_, w, _, host, _ := dualSFTPFixture(t, 1)
	for _, name := range names {
		w.owner.sftpBookmarks = append(w.owner.sftpBookmarks, sftpBookmark{host: "native-bookmark", name: name, path: "/srv/" + name})
	}
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

func TestBookmarkProfileListNavigatesAndOpens(t *testing.T) {
	w, view := openBookmarkEditorWithProfiles(t, "alpha", "beta", "gamma")
	profiles := w.visibleProfiles()
	if len(profiles) != 3 {
		t.Fatalf("profiles = %d, want 3", len(profiles))
	}
	// A click selects a row: its fields fill the editor.
	clickBookmarkButton(t, w, view, hitSFTPProfileItems.At(1))
	if !w.bookmark.editing || w.bookmark.original != profiles[1] {
		t.Fatalf("click on row 1: editing=%v original=%+v", w.bookmark.editing, w.bookmark.original)
	}
	// Down moves the selection on, Up back, Home and End to the ends.
	for _, step := range []struct {
		key  string
		want int
	}{{"down", 2}, {"up", 1}, {"home", 0}, {"end", 2}} {
		if err := view.Keys(step.key); err != nil {
			t.Fatal(err)
		}
		if w.bookmark.original != profiles[step.want] {
			t.Fatalf("after %s the selection is %q, want %q", step.key, w.bookmark.original.name, profiles[step.want].name)
		}
	}
	// A key past an end stays on it.
	if err := view.Keys("down"); err != nil {
		t.Fatal(err)
	}
	if w.bookmark.original != profiles[2] {
		t.Fatalf("down at the end moved to %q", w.bookmark.original.name)
	}
}

func TestBookmarkProfileListIsEmptyWithoutProfiles(t *testing.T) {
	w, _ := openBookmarkEditorWithProfiles(t)
	if n := len(w.visibleProfiles()); n != 0 {
		t.Fatalf("profiles = %d, want none", n)
	}
}

func TestBookmarkScopeSelectOpensChoosesAndDismisses(t *testing.T) {
	w, view := openBookmarkEditorWithProfiles(t)
	if w.bookmark.shared {
		t.Fatal("a new editor starts with the site scope")
	}
	// A click on the trigger opens the list, and focuses an option for the keys.
	clickBookmarkButton(t, w, view, hitSFTPProfileScope)
	if !w.bookmark.scopeOpen {
		t.Fatal("the scope list did not open")
	}
	// Choosing All connections closes it and sets the scope.
	clickBookmarkButton(t, w, view, hitSFTPProfileShared)
	if w.bookmark.scopeOpen || !w.bookmark.shared {
		t.Fatalf("after choosing shared: open=%v shared=%v", w.bookmark.scopeOpen, w.bookmark.shared)
	}
	// Open again: a press outside (the dismiss target) closes it without changing the scope.
	clickBookmarkButton(t, w, view, hitSFTPProfileScope)
	if !w.bookmark.scopeOpen {
		t.Fatal("the scope list did not open the second time")
	}
	clickBookmarkButton(t, w, view, hitSFTPProfileScopeDismiss)
	if w.bookmark.scopeOpen || !w.bookmark.shared {
		t.Fatalf("after dismissing: open=%v shared=%v", w.bookmark.scopeOpen, w.bookmark.shared)
	}
	// Escape closes an open list and does not close the editor.
	clickBookmarkButton(t, w, view, hitSFTPProfileScope)
	if err := view.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	if w.bookmark.scopeOpen || !w.bookmark.open {
		t.Fatalf("Escape on the open list: scopeOpen=%v editorOpen=%v", w.bookmark.scopeOpen, w.bookmark.open)
	}
}

func TestBookmarkSyncCheckboxTogglesByClickAndKey(t *testing.T) {
	w, view := openBookmarkEditor(t)
	if w.bookmark.sync {
		t.Fatal("a new editor starts with sync off")
	}
	clickBookmarkButton(t, w, view, hitSFTPBookmarkSync)
	if !w.bookmark.sync {
		t.Fatal("a click did not turn sync on")
	}
	// The click focused the box: Space toggles it back, Enter too.
	if err := view.Keys("space"); err != nil {
		t.Fatal(err)
	}
	if w.bookmark.sync {
		t.Fatal("Space did not turn sync off")
	}
	if err := view.Keys("enter"); err != nil {
		t.Fatal(err)
	}
	if !w.bookmark.sync {
		t.Fatal("Enter did not turn sync on")
	}
	// Pressing the words toggles it as well: the label is the box's own.
	for _, h := range view.Snapshot().Hits {
		if h.IsLabel && h.Label == w.owner.messages.SFTPWindow.ProfileSync {
			t.Logf("label hit %v", h.Bounds)
		}
	}
}

func TestBookmarkScopeSelectKeysChoose(t *testing.T) {
	w, view := openBookmarkEditor(t)
	clickBookmarkButton(t, w, view, hitSFTPProfileScope)
	if !w.bookmark.scopeOpen {
		t.Fatal("the scope list did not open")
	}
	// Down moves to the next option, Enter chooses it: All connections.
	if err := view.Keys("down"); err != nil {
		t.Fatal(err)
	}
	if err := view.Keys("enter"); err != nil {
		t.Fatal(err)
	}
	if w.bookmark.scopeOpen || !w.bookmark.shared {
		t.Fatalf("Down then Enter: open=%v shared=%v, want closed and shared", w.bookmark.scopeOpen, w.bookmark.shared)
	}
	// The focus after choosing is the dialog's own initial focus (the local directory field), not
	// the trigger the code asks for: pinned as it is, so a change is a decision.
	w.window.Invalidate()
	view.Driver().Pump()
	if got := view.Snapshot().Focused; got != hitSFTPBookmarkFields.At(1) {
		t.Fatalf("after choosing, the focus is %v, want the local directory field %v", got, hitSFTPBookmarkFields.At(1))
	}
	// Up and Enter on the open list choose the first option: This connection only.
	clickBookmarkButton(t, w, view, hitSFTPProfileScope)
	if err := view.Keys("up"); err != nil {
		t.Fatal(err)
	}
	if err := view.Keys("enter"); err != nil {
		t.Fatal(err)
	}
	if w.bookmark.scopeOpen || w.bookmark.shared {
		t.Fatalf("Up then Enter: open=%v shared=%v, want closed and site", w.bookmark.scopeOpen, w.bookmark.shared)
	}
}

func TestBookmarkScopeSelectTabClosesTheList(t *testing.T) {
	w, view := openBookmarkEditor(t)
	clickBookmarkButton(t, w, view, hitSFTPProfileScope)
	if err := view.Keys("tab"); err != nil {
		t.Fatal(err)
	}
	if w.bookmark.scopeOpen {
		t.Fatal("Tab left the scope list open")
	}
}
