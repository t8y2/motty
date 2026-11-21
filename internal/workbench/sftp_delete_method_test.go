package workbench

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// chooseSFTPDeleteMethod selects one segment of the delete confirmation's
// method choice. The choice is a page control, so the press goes through the
// event pass where the Choice writes the value.
func chooseSFTPDeleteMethod(t *testing.T, state *State, label string) {
	t.Helper()
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1200})
	hit, ok := hostChoiceIn(&canvas, label)
	if !ok {
		t.Fatalf("no delete method choice labeled %q", label)
	}
	activateHostChoice(t, state, hit.ID)
}

// The delete confirmation now names both methods. The safe one stays the
// default, so a user who only presses the destructive button keeps the old
// behavior.
func TestSFTPDeleteOffersNormalAndForceMethods(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files

	clickSFTPRow(t, state, view, "notes.txt")
	state.Click(hitSFTPDelete)
	if view.deleteMethod != sftpDeleteNormal {
		t.Fatalf("default delete method = %v", view.deleteMethod)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1200})
	for _, want := range []string{
		state.messages.SFTPDeleteMethodLabel,
		state.messages.SFTPDeleteMethodNormal,
		state.messages.SFTPDeleteMethodForce,
	} {
		if !canvasHasText(&canvas, want) {
			t.Fatalf("delete confirmation lacks %q", want)
		}
	}
	// The method choice must still fit the smallest window the app supports.
	var small gfx.Canvas
	state.Paint(&small, gfx.Size{Width: 900, Height: 600})
	if !canvasHasText(&small, state.messages.SFTPDeleteMethodLabel) ||
		!canvasHasText(&small, state.messages.SFTPDeleteMethodForce) {
		t.Fatal("the delete method choice is clipped at the minimum window size")
	}

	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "notes.txt") < 0
	})
	remote.mu.Lock()
	_, exists := remote.files["/home/test/notes.txt"]
	remote.mu.Unlock()
	if exists {
		t.Fatal("normal delete did not remove the file")
	}
}

// Force is the risky half, so it gets a second confirmation of its own. The
// file must still be there until that second question is answered.
func TestSFTPForceDeleteAsksOnceMoreBeforeRemoving(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files

	clickSFTPRow(t, state, view, "notes.txt")
	for attempt := 0; attempt < 2; attempt++ {
		state.Click(hitSFTPDelete)
		chooseSFTPDeleteMethod(t, state, state.messages.SFTPDeleteMethodForce)
		if view.deleteMethod != sftpDeleteForce {
			t.Fatalf("delete method = %v after choosing force", view.deleteMethod)
		}
		state.Click(hitSFTPDialogSubmit)
		if view.dialog != sftpDialogDeleteForce {
			t.Fatalf("force delete dialog = %v, want the second confirmation", view.dialog)
		}
		remote.mu.Lock()
		_, exists := remote.files["/home/test/notes.txt"]
		remote.mu.Unlock()
		if !exists {
			t.Fatal("choosing force removed the file before the second confirmation")
		}

		var canvas gfx.Canvas
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1200})
		if canvas.InitialFocus() != hitSFTPDialogCancel {
			t.Fatalf("force delete initial focus = %d, want cancel", canvas.InitialFocus())
		}
		if !canvasHasText(&canvas, state.messages.SFTPDeleteForceTitle) {
			t.Fatal("the second confirmation does not name force deletion")
		}
		if attempt == 0 {
			state.Click(hitSFTPDialogCancel)
			remote.mu.Lock()
			_, exists = remote.files["/home/test/notes.txt"]
			remote.mu.Unlock()
			if !exists || view.dialog != sftpDialogNone {
				t.Fatal("canceling the force confirmation removed the file")
			}
			continue
		}
		state.Click(hitSFTPDialogSubmit)
	}

	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "notes.txt") < 0
	})
	remote.mu.Lock()
	_, exists := remote.files["/home/test/notes.txt"]
	remote.mu.Unlock()
	if exists {
		t.Fatal("confirmed force delete did not remove the file")
	}
}

// Force keeps going after one refused entry. What it deleted leaves the list,
// what it could not delete stays, and the notice says both happened.
func TestSFTPForceDeleteOfManyDropsRowsWithoutReloading(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	before := remote.readCount("/home/test")

	clickSFTPRow(t, state, view, "notes.txt")
	clickSFTPRow(t, state, view, "docs")
	state.Click(hitSFTPDelete)
	chooseSFTPDeleteMethod(t, state, state.messages.SFTPDeleteMethodForce)
	state.Click(hitSFTPDialogSubmit)
	state.Click(hitSFTPDialogSubmit)

	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && len(view.rows) == 0 && !view.deleteForce
	})
	if reads := remote.readCount("/home/test"); reads != before {
		t.Fatalf("a successful forced delete listed the folder again: %d reads, want %d", reads, before)
	}
	remote.mu.Lock()
	_, notesExist := remote.files["/home/test/notes.txt"]
	_, docsExist := remote.entries["/home/test/docs"]
	remote.mu.Unlock()
	if notesExist || docsExist {
		t.Fatalf("forced delete left content behind: notes=%v docs=%v", notesExist, docsExist)
	}
}

func TestSFTPForceDeleteContinuesPastAFailure(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	remote.mu.Lock()
	remote.removeErrAt = map[string]error{"/home/test/notes.txt": os.ErrPermission}
	remote.mu.Unlock()

	clickSFTPRow(t, state, view, "notes.txt")
	clickSFTPRow(t, state, view, "docs")
	state.Click(hitSFTPDelete)
	chooseSFTPDeleteMethod(t, state, state.messages.SFTPDeleteMethodForce)
	state.Click(hitSFTPDialogSubmit)
	state.Click(hitSFTPDialogSubmit)

	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && view.dialog == sftpDialogNone && !view.deleteForce
	})
	remote.mu.Lock()
	_, notesExist := remote.files["/home/test/notes.txt"]
	_, docsExist := remote.entries["/home/test/docs"]
	remote.mu.Unlock()
	if !notesExist {
		t.Fatal("the refused file disappeared")
	}
	if docsExist {
		t.Fatal("force delete stopped before the later folder")
	}
	if len(state.fileNotices.items) != 1 || state.fileNotices.items[0].kind != components.ToastWarning ||
		!strings.Contains(state.fileNotices.items[0].detail, fmt.Sprintf(state.messages.SFTPDeleteForcePartialFmt, 2, 1)) {
		t.Fatalf("force partial notice = %+v", state.fileNotices.items)
	}
}
