//go:build darwin || linux || windows

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestHeadlessSFTPMultiDeleteThroughContextMenu(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	state.Click(hitSFTPEntryList.At(sftpRowIndex(t, session.files, "notes.txt")))
	state.Click(hitSFTPEntryList.At(sftpRowIndex(t, session.files, "docs")))
	state.sftpMenu = sftpMenu{open: true, session: session.ID, at: gfx.Point{X: 400, Y: 300}}
	preview, err := headless.Open(app.Config{
		Title: "selected files", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	click := func(id gfx.ID) {
		t.Helper()
		if err := preview.Rest(); err != nil {
			t.Fatal(err)
		}
		hit, ok := hitByIDSnapshot(preview.Snapshot().Hits, id)
		if !ok {
			t.Fatalf("missing action %s", tree.Text(id))
		}
		if err := preview.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
	}
	click(hitSFTPMenuDelete)
	if session.files.dialog != sftpDialogDelete || len(session.files.dialogEntries) != 2 {
		t.Fatal("menu did not ask to delete the selected items")
	}
	click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "notes.txt") < 0 && sftpRowIndexOrNegative(view, "docs") < 0
	})
	remote.mu.Lock()
	_, childLeft := remote.files["/home/test/docs/guide.md"]
	remote.mu.Unlock()
	if childLeft {
		t.Fatal("the selected folder's contents remain after confirmation")
	}
}
