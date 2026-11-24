package workbench

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

// TestDraggingARemoteRowOutPromisesItsContent is the feature the issue asks for:
// a remote row taken past the window becomes a promise whose content is
// produced when the target asks, not a download that started at the press.
func TestDraggingARemoteRowOutPromisesItsContent(t *testing.T) {
	_, w, _, host, _ := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	file := dualHit(t, view, dualRow(t, w, false, "notes.txt"))
	view.Press(file.X+20, file.Y+10)
	view.Move(file.X+40, file.Y+10)
	view.Move(1400, 400)
	promises := view.Driver().DraggedPromises()
	if len(promises) != 1 || promises[0].Name != "notes.txt" || promises[0].Size != 11 || promises[0].Directory {
		t.Fatalf("the drag offered %+v, want notes.txt of 11 bytes", promises)
	}
	reader, err := promises[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || string(body) != "hello world" {
		t.Fatalf("the promised file read %q, %v", body, err)
	}
}

// TestDraggingARemoteFolderOutListsItsChildren checks the tree the target walks
// when a folder is dragged out.
func TestDraggingARemoteFolderOutListsItsChildren(t *testing.T) {
	_, w, _, host, _ := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	folder := dualHit(t, view, dualRow(t, w, false, "docs"))
	view.Press(folder.X+20, folder.Y+10)
	view.Move(folder.X+40, folder.Y+10)
	view.Move(1400, 400)
	promises := view.Driver().DraggedPromises()
	if len(promises) != 1 || promises[0].Name != "docs" || !promises[0].Directory {
		t.Fatalf("the drag offered %+v, want the docs folder", promises)
	}
	children, err := promises[0].List()
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].Name != "guide.md" || children[0].Size != 7 {
		t.Fatalf("the folder listed %+v, want guide.md", children)
	}
	reader, err := children[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(reader)
	reader.Close()
	if string(body) != "guide\n" {
		t.Fatalf("the child read %q", body)
	}
}

// TestDraggingALocalRowOutOffersItsPath keeps the local side of the transfer
// window on the same gesture: the file already exists, so no content is
// produced, and its real path is what the target copies.
func TestDraggingALocalRowOutOffersItsPath(t *testing.T) {
	_, w, _, host, localDir := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	file := dualHit(t, view, dualRow(t, w, true, "中文上传.yml"))
	view.Press(file.X+20, file.Y+10)
	view.Move(file.X+40, file.Y+10)
	view.Move(1400, 400)
	paths, _ := view.Driver().DraggedFiles()
	want := filepath.Join(localDir, "中文上传.yml")
	if !slices.Equal(paths, []string{want}) {
		t.Fatalf("the drag offered %q, want %q", paths, want)
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatalf("the path offered does not exist: %v", err)
	}
}

// TestDraggingAPanelRowOutOfTheWindowPromisesIt is the docked panel's half of
// the gesture: the panel owns the press through its DragState, so the move out
// of the window is what tells the page the drag left the application.
func TestDraggingAPanelRowOutOfTheWindowPromisesIt(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	preview, err := headless.Open(state.WindowConfig(app.Config{
		Title: "File panel", Width: 1200, Height: 800, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
	}), nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	if err := preview.Rest(); err != nil {
		t.Fatal(err)
	}
	folder := session.files.active()
	index := sftpFolderRowIndex(folder, "notes.txt")
	if index < 0 {
		t.Fatal("missing notes.txt")
	}
	row, ok := findSnapshotHit(preview.Snapshot(), folder.listRowsScope.At(index))
	if !ok {
		t.Fatal("the row was not painted")
	}
	x, y := row.Bounds.X+row.Bounds.Width/2, row.Bounds.Y+row.Bounds.Height/2
	if err := preview.Press(x, y); err != nil {
		t.Fatal(err)
	}
	if err := preview.Move(x+10, y); err != nil {
		t.Fatal(err)
	}
	if !session.files.panelDrag.state.Dragging() {
		t.Fatal("the panel drag did not start")
	}
	if err := preview.Move(1400, 400); err != nil {
		t.Fatal(err)
	}
	promises := preview.Driver().DraggedPromises()
	if len(promises) != 1 || promises[0].Name != "notes.txt" || promises[0].Size != 11 {
		t.Fatalf("the panel drag offered %+v, want notes.txt", promises)
	}
	reader, err := promises[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(reader)
	reader.Close()
	if string(body) != "hello world" {
		t.Fatalf("the promised file read %q", body)
	}
}

// TestOwnWindowAtKnowsTheProcessWindows keeps an OS drag from starting merely
// because the pointer is over another Motty window with no drop target: the
// panes own that gesture.
func TestOwnWindowAtKnowsTheProcessWindows(t *testing.T) {
	state, _, _, _ := openTestSFTP(t)
	visible := true
	frame := gfx.Rect{X: 100, Y: 100, Width: 800, Height: 600}
	state.SetWindow(func() (gfx.Rect, bool) { return frame, true }, func() bool { return visible })
	if !state.ownWindowAt(gfx.Point{X: 200, Y: 200}, nil) {
		t.Fatal("a point inside the window is not the process's")
	}
	if state.ownWindowAt(gfx.Point{X: 1000, Y: 200}, nil) {
		t.Fatal("a point outside every window is the process's")
	}
	visible = false
	if state.ownWindowAt(gfx.Point{X: 200, Y: 200}, nil) {
		t.Fatal("a hidden window took a drag")
	}
}
