//go:build windows || darwin || linux

package workbench

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

func TestValidFileMoveTarget(t *testing.T) {
	fileEntry := sftpclient.Entry{Name: "notes.txt"}
	dirEntry := sftpclient.Entry{Name: "docs", Mode: os.ModeDir | 0o755}
	linkDirEntry := sftpclient.Entry{Name: "linkdir", Mode: os.ModeDir | os.ModeSymlink | 0o777, LinkTarget: "docs"}

	if !validFileMoveTarget(false, "/home/test", []sftpclient.Entry{fileEntry}, "/home/test/sub") {
		t.Fatal("expected remote file move to subfolder to be valid")
	}
	if !validFileMoveTarget(false, "/home/test/sub", []sftpclient.Entry{fileEntry}, "/home/test") {
		t.Fatal("expected remote file move to parent folder to be valid")
	}
	if validFileMoveTarget(false, "/home/test", []sftpclient.Entry{fileEntry}, "/home/test") {
		t.Fatal("expected remote move to same directory to be rejected")
	}
	if validFileMoveTarget(false, "/home/test", []sftpclient.Entry{dirEntry}, "/home/test/docs") {
		t.Fatal("expected moving remote folder into itself to be rejected")
	}
	if validFileMoveTarget(false, "/home/test", []sftpclient.Entry{dirEntry}, "/home/test/docs/nested") {
		t.Fatal("expected moving remote folder into descendant to be rejected")
	}
	if !validFileMoveTarget(false, "/home/test", []sftpclient.Entry{linkDirEntry}, "/home/test/linkdir/child") {
		t.Fatal("expected symlink directory entry to be allowed")
	}

	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	docs := filepath.Join(root, "docs")
	nested := filepath.Join(docs, "nested")
	if !validFileMoveTarget(true, root, []sftpclient.Entry{fileEntry}, sub) {
		t.Fatal("expected local file move to subfolder to be valid")
	}
	if !validFileMoveTarget(true, sub, []sftpclient.Entry{fileEntry}, root) {
		t.Fatal("expected local file move to parent folder to be valid")
	}
	if validFileMoveTarget(true, root, []sftpclient.Entry{fileEntry}, root) {
		t.Fatal("expected local move to same directory to be rejected")
	}
	if validFileMoveTarget(true, root, []sftpclient.Entry{dirEntry}, docs) {
		t.Fatal("expected moving local folder into itself to be rejected")
	}
	if validFileMoveTarget(true, root, []sftpclient.Entry{dirEntry}, nested) {
		t.Fatal("expected moving local folder into descendant to be rejected")
	}
}

func sftpFolderRowIndex(folder *sftpFolder, name string) int {
	for i := range folder.rows {
		if folder.rows[i].entry.Name == name {
			return i
		}
	}
	return -1
}

func rowCenterPoint(listRect gfx.Rect, scrollOffset float32, index int) gfx.Point {
	return gfx.Point{
		X: listRect.X + min(float32(40), listRect.Width/2),
		Y: listRect.Y + (float32(index)+0.5)*sftpRowHeight - scrollOffset,
	}
}

func panelRowHit(t *testing.T, canvas *gfx.Canvas, folder *sftpFolder, name string) gfx.Hit {
	t.Helper()
	idx := sftpFolderRowIndex(folder, name)
	if idx < 0 {
		t.Fatalf("missing row %q", name)
	}
	hit, ok := hitByID(canvas, folder.listRowsScope.At(idx))
	if !ok {
		t.Fatalf("row %q (%v) was not painted as a hit", name, folder.listRowsScope.At(idx))
	}
	return hit
}

func sftpHitCenter(hit gfx.Hit) gfx.Point {
	return gfx.Point{
		X: hit.Bounds.X + hit.Bounds.Width/2,
		Y: hit.Bounds.Y + hit.Bounds.Height/2,
	}
}

func panelDragConfig(state *State) app.Config {
	return state.WindowConfig(app.Config{Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key})
}

func TestDockedSFTPPanelDragMoveRemoteFilesAndBreadcrumbs(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	if err := remote.Mkdir("/home/test/target"); err != nil {
		t.Fatal(err)
	}
	remote.mu.Lock()
	remote.files["/home/test/extra.txt"] = []byte("extra")
	remote.putEntryLocked("/home/test", workbenchFileInfo{name: "extra.txt", size: 5, mode: 0o644, when: time.Unix(1_700_000_000, 0)})
	remote.mu.Unlock()
	state.Click(hitSFTPRefresh)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpFolderRowIndex(&view.sftpFolder, "target") >= 0 && sftpFolderRowIndex(&view.sftpFolder, "extra.txt") >= 0
	})

	view := session.files
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	config := panelDragConfig(state)

	srcHit := panelRowHit(t, &canvas, &view.sftpFolder, "notes.txt")
	dstHit := panelRowHit(t, &canvas, &view.sftpFolder, "target")
	srcPt, dstPt := sftpHitCenter(srcHit), sftpHitCenter(dstHit)

	// 1. Drag single file onto subfolder row and verify move overlay paints.
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, X: srcPt.X, Y: srcPt.Y, Viewport: size, Target: srcHit.ID})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, X: dstPt.X, Y: dstPt.Y, Viewport: size, Target: srcHit.ID, Captured: true})
	if !view.panelDrag.state.Dragging() || view.panelDrag.state.Target() != dstHit.ID {
		t.Fatalf("expected panel drag over target %v, dragging=%t target=%v", dstHit.ID, view.panelDrag.state.Dragging(), view.panelDrag.state.Target())
	}
	var dragCanvas gfx.Canvas
	state.Paint(&dragCanvas, size)
	if !canvasHasText(&dragCanvas, state.messages.SFTPWindow.DropMoveHint) {
		t.Fatalf("expected DropMoveHint %q to be painted while hovering over folder row", state.messages.SFTPWindow.DropMoveHint)
	}

	// Release onto subfolder row -> file moves on remote.
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: dstPt.X, Y: dstPt.Y, Viewport: size, Target: srcHit.ID, Captured: true})
	waitSFTP(t, state, session, func(v *sftpView) bool {
		return !v.busy && sftpFolderRowIndex(&v.sftpFolder, "notes.txt") < 0
	})
	remote.mu.Lock()
	_, moved := remote.files["/home/test/target/notes.txt"]
	_, stillOld := remote.files["/home/test/notes.txt"]
	remote.mu.Unlock()
	if !moved || stillOld {
		t.Fatalf("remote file not moved: moved=%t stillOld=%t", moved, stillOld)
	}

	// 2. Dragging a folder onto itself is rejected.
	state.Paint(&canvas, size)
	folderHit := panelRowHit(t, &canvas, &view.sftpFolder, "target")
	folderPt := sftpHitCenter(folderHit)
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, X: folderPt.X, Y: folderPt.Y, Viewport: size, Target: folderHit.ID})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, X: folderPt.X + 12, Y: folderPt.Y, Viewport: size, Target: folderHit.ID, Captured: true})
	if !view.panelDrag.state.Target().IsZero() {
		t.Fatalf("expected dragging folder onto itself to have no target, got %v", view.panelDrag.state.Target())
	}
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: folderPt.X + 12, Y: folderPt.Y, Viewport: size, Target: folderHit.ID, Captured: true})

	// 3. Escape cancels an active drag without moving.
	extraHit := panelRowHit(t, &canvas, &view.sftpFolder, "extra.txt")
	dstHit = panelRowHit(t, &canvas, &view.sftpFolder, "target")
	extraPt, dstPt := sftpHitCenter(extraHit), sftpHitCenter(dstHit)
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, X: extraPt.X, Y: extraPt.Y, Viewport: size, Target: extraHit.ID})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, X: dstPt.X, Y: dstPt.Y, Viewport: size, Target: extraHit.ID, Captured: true})
	if !view.panelDrag.state.Dragging() {
		t.Fatal("expected drag to be active before Escape")
	}
	config.OnKey(gfx.KeyEvent{Key: gfx.KeyEscape, Target: extraHit.ID})
	if view.panelDrag.state.Dragging() {
		t.Fatal("expected Escape to cancel panel drag")
	}
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: dstPt.X, Y: dstPt.Y, Viewport: size, Target: extraHit.ID, Captured: true})
	remote.mu.Lock()
	_, stillThere := remote.files["/home/test/extra.txt"]
	remote.mu.Unlock()
	if !stillThere {
		t.Fatal("canceled drag unexpectedly moved extra.txt")
	}

	// 4. Multi-select drag moves all selected entries into target folder.
	remote.mu.Lock()
	remote.files["/home/test/two.txt"] = []byte("two")
	remote.putEntryLocked("/home/test", workbenchFileInfo{name: "two.txt", size: 3, mode: 0o644, when: time.Unix(1_700_000_000, 0)})
	remote.mu.Unlock()
	state.Click(hitSFTPRefresh)
	waitSFTP(t, state, session, func(v *sftpView) bool {
		return !v.busy && sftpFolderRowIndex(&v.sftpFolder, "two.txt") >= 0
	})
	state.Paint(&canvas, size)
	h1 := panelRowHit(t, &canvas, &view.sftpFolder, "extra.txt")
	h2 := panelRowHit(t, &canvas, &view.sftpFolder, "two.txt")
	dstHit = panelRowHit(t, &canvas, &view.sftpFolder, "target")
	p1, p2, dstPt := sftpHitCenter(h1), sftpHitCenter(h2), sftpHitCenter(dstHit)
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, X: p1.X, Y: p1.Y, Viewport: size, Target: h1.ID})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: p1.X, Y: p1.Y, Viewport: size, Target: h1.ID, Captured: true})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, Modifiers: gfx.ModifierControl, X: p2.X, Y: p2.Y, Viewport: size, Target: h2.ID})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: p2.X, Y: p2.Y, Viewport: size, Target: h2.ID, Captured: true})
	// Now drag from h1 (already selected in multi-selection) to target folder.
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, X: p1.X, Y: p1.Y, Viewport: size, Target: h1.ID})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, X: dstPt.X, Y: dstPt.Y, Viewport: size, Target: h1.ID, Captured: true})
	if entries := state.sftpPanelDraggedEntries(view, h1.ID); len(entries) != 2 {
		t.Fatalf("expected 2 dragged entries in multi-select drag, got %d", len(entries))
	}
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: dstPt.X, Y: dstPt.Y, Viewport: size, Target: h1.ID, Captured: true})
	waitSFTP(t, state, session, func(v *sftpView) bool {
		return !v.busy && sftpFolderRowIndex(&v.sftpFolder, "extra.txt") < 0 && sftpFolderRowIndex(&v.sftpFolder, "two.txt") < 0
	})
	remote.mu.Lock()
	_, moved1 := remote.files["/home/test/target/extra.txt"]
	_, moved2 := remote.files["/home/test/target/two.txt"]
	remote.mu.Unlock()
	if !moved1 || !moved2 {
		t.Fatalf("multi-select move incomplete: extra=%t two=%t", moved1, moved2)
	}

	// 5. Navigate into /home/test/target and drag notes.txt onto parent breadcrumb (/home/test).
	state.listSFTP(view, "/home/test/target")
	waitSFTP(t, state, session, func(v *sftpView) bool {
		return !v.busy && v.path == "/home/test/target" && sftpFolderRowIndex(&v.sftpFolder, "notes.txt") >= 0
	})
	state.Paint(&canvas, size)
	var parentCrumb gfx.Rect
	var parentCrumbID gfx.ID
	for i, crumb := range view.sftpFolder.crumbs {
		if path.Clean(crumb.path) == "/home/test" {
			parentCrumbID = hitSFTPBreadcrumbList.At(i)
			break
		}
	}
	for _, cb := range view.sftpFolder.crumbBoxes {
		if path.Clean(cb.path) == "/home/test" {
			parentCrumb = cb.box.Rect()
			break
		}
	}
	if parentCrumb.Width <= 0 || parentCrumbID.IsZero() {
		t.Fatalf("parent breadcrumb rect not laid out: id=%v boxes=%+v", parentCrumbID, view.sftpFolder.crumbBoxes)
	}
	noteHit := panelRowHit(t, &canvas, &view.sftpFolder, "notes.txt")
	notePt := sftpHitCenter(noteHit)
	crumbPt := gfx.Point{X: parentCrumb.X + parentCrumb.Width/2, Y: parentCrumb.Y + parentCrumb.Height/2}
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, X: notePt.X, Y: notePt.Y, Viewport: size, Target: noteHit.ID})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, X: crumbPt.X, Y: crumbPt.Y, Viewport: size, Target: noteHit.ID, Captured: true})
	if !view.panelDrag.state.Dragging() || view.panelDrag.state.Target() != parentCrumbID {
		t.Fatalf("expected breadcrumb drop target %v, got dragging=%t target=%v", parentCrumbID, view.panelDrag.state.Dragging(), view.panelDrag.state.Target())
	}
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: crumbPt.X, Y: crumbPt.Y, Viewport: size, Target: noteHit.ID, Captured: true})
	waitSFTP(t, state, session, func(v *sftpView) bool {
		return !v.busy && sftpFolderRowIndex(&v.sftpFolder, "notes.txt") < 0
	})
	remote.mu.Lock()
	_, backInParent := remote.files["/home/test/notes.txt"]
	remote.mu.Unlock()
	if !backInParent {
		t.Fatal("expected notes.txt to be moved back to /home/test via breadcrumb drop")
	}
}

func TestDockedLocalTerminalFileBrowserDragMove(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localFixture(t)
	if err := os.Mkdir(filepath.Join(dir, "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	view := openLocalPane(t, state, session, dir)
	view.localOnly = true

	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	config := panelDragConfig(state)

	// 1. Drag local file into subfolder.
	srcHit := panelRowHit(t, &canvas, &view.lfolder, "report.txt")
	dstHit := panelRowHit(t, &canvas, &view.lfolder, "photos")
	srcPt, dstPt := sftpHitCenter(srcHit), sftpHitCenter(dstHit)
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, X: srcPt.X, Y: srcPt.Y, Viewport: size, Target: srcHit.ID})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, X: dstPt.X, Y: dstPt.Y, Viewport: size, Target: srcHit.ID, Captured: true})
	if !view.panelDrag.state.Dragging() || view.panelDrag.state.Target() != dstHit.ID {
		t.Fatalf("expected active local row move drag onto %v, got dragging=%t target=%v", dstHit.ID, view.panelDrag.state.Dragging(), view.panelDrag.state.Target())
	}
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: dstPt.X, Y: dstPt.Y, Viewport: size, Target: srcHit.ID, Captured: true})
	waitSFTP(t, state, session, func(v *sftpView) bool {
		return !v.localBusy && sftpFolderRowIndex(&v.lfolder, "report.txt") < 0
	})
	if _, err := os.Stat(filepath.Join(dir, "photos", "report.txt")); err != nil {
		t.Fatalf("expected report.txt in photos/: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "report.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected report.txt to be gone from source dir: %v", err)
	}

	// 2. Drag local folder into another local folder.
	state.Paint(&canvas, size)
	folderSrc := panelRowHit(t, &canvas, &view.lfolder, "photos")
	folderDst := panelRowHit(t, &canvas, &view.lfolder, "archive")
	pSrc, pDst := sftpHitCenter(folderSrc), sftpHitCenter(folderDst)
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, X: pSrc.X, Y: pSrc.Y, Viewport: size, Target: folderSrc.ID})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, X: pDst.X, Y: pDst.Y, Viewport: size, Target: folderSrc.ID, Captured: true})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: pDst.X, Y: pDst.Y, Viewport: size, Target: folderSrc.ID, Captured: true})
	waitSFTP(t, state, session, func(v *sftpView) bool {
		return !v.localBusy && sftpFolderRowIndex(&v.lfolder, "photos") < 0
	})
	if _, err := os.Stat(filepath.Join(dir, "archive", "photos", "a.jpg")); err != nil {
		t.Fatalf("expected photos/a.jpg under archive/: %v", err)
	}

	// 3. Name collision does not overwrite existing file.
	if err := os.WriteFile(filepath.Join(dir, "archive", "Zeta.txt"), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	state.Paint(&canvas, size)
	zetaHit := panelRowHit(t, &canvas, &view.lfolder, "Zeta.txt")
	archiveHit := panelRowHit(t, &canvas, &view.lfolder, "archive")
	zPt, aPt := sftpHitCenter(zetaHit), sftpHitCenter(archiveHit)
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, X: zPt.X, Y: zPt.Y, Viewport: size, Target: zetaHit.ID})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, X: aPt.X, Y: aPt.Y, Viewport: size, Target: zetaHit.ID, Captured: true})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: aPt.X, Y: aPt.Y, Viewport: size, Target: zetaHit.ID, Captured: true})
	waitSFTP(t, state, session, func(v *sftpView) bool {
		return !v.localBusy && v.errorText != ""
	})
	data, err := os.ReadFile(filepath.Join(dir, "archive", "Zeta.txt"))
	if err != nil || string(data) != "existing" {
		t.Fatalf("existing destination file was overwritten: %q err=%v", string(data), err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Zeta.txt")); err != nil {
		t.Fatalf("source Zeta.txt should remain after collision: %v", err)
	}

	// 4. Drag local file up to parent breadcrumb.
	subDir := filepath.Join(dir, "archive", "photos")
	state.navigateLocalDirectory(view, subDir)
	state.Paint(&canvas, size)
	var archiveCrumb gfx.Rect
	for _, cb := range view.lfolder.crumbBoxes {
		if sameSFTPPath(true, cb.path, filepath.Join(dir, "archive")) {
			archiveCrumb = cb.box.Rect()
			break
		}
	}
	if archiveCrumb.Width <= 0 {
		t.Fatalf("archive breadcrumb not laid out: %+v", view.lfolder.crumbBoxes)
	}
	jpgHit := panelRowHit(t, &canvas, &view.lfolder, "a.jpg")
	jpgPt := sftpHitCenter(jpgHit)
	crumbPt := gfx.Point{X: archiveCrumb.X + archiveCrumb.Width/2, Y: archiveCrumb.Y + archiveCrumb.Height/2}
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, X: jpgPt.X, Y: jpgPt.Y, Viewport: size, Target: jpgHit.ID})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, X: crumbPt.X, Y: crumbPt.Y, Viewport: size, Target: jpgHit.ID, Captured: true})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: crumbPt.X, Y: crumbPt.Y, Viewport: size, Target: jpgHit.ID, Captured: true})
	waitSFTP(t, state, session, func(v *sftpView) bool {
		return !v.localBusy && sftpFolderRowIndex(&v.lfolder, "a.jpg") < 0
	})
	if _, err := os.Stat(filepath.Join(dir, "archive", "a.jpg")); err != nil {
		t.Fatalf("expected a.jpg moved up to archive/: %v", err)
	}
}

func TestSFTPWindowSamePaneDragMoveRemoteLocalAndTree(t *testing.T) {
	state, w, remote, _, localDir := dualSFTPFixture(t, 1)
	if err := os.Mkdir(filepath.Join(localDir, "local_sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "local_file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := remote.Mkdir("/home/test/remote_sub"); err != nil {
		t.Fatal(err)
	}
	state.listLocal(w.view, localDir)
	state.listSFTP(w.view, "/home/test")
	waitWindowSFTP(t, w, func() bool {
		return !w.view.busy && sftpFolderRowIndex(&w.view.sftpFolder, "remote_sub") >= 0 && sftpFolderRowIndex(&w.view.lfolder, "local_sub") >= 0
	})

	size := gfx.Size{Width: 1200, Height: 800}
	var canvas gfx.Canvas
	w.paint(&canvas, size)

	// 1. Remote pane same-side drag-move: notes.txt -> remote_sub.
	rSrc := sftpFolderRowIndex(&w.view.sftpFolder, "notes.txt")
	rDst := sftpFolderRowIndex(&w.view.sftpFolder, "remote_sub")
	rSrcPt := rowCenterPoint(w.ui.list[1], w.ui.offset[1], rSrc)
	rDstPt := rowCenterPoint(w.ui.list[1], w.ui.offset[1], rDst)
	w.pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, X: rSrcPt.X, Y: rSrcPt.Y, Viewport: size, Target: w.rowID(1, rSrc)})
	w.pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, X: rDstPt.X, Y: rDstPt.Y, Viewport: size, Captured: true})
	if !w.drag.active || w.drag.target.kind != sftpDragMove || w.drag.target.dir != "/home/test/remote_sub" {
		t.Fatalf("expected remote same-side move target, got %+v", w.drag.target)
	}
	var dragCanvas gfx.Canvas
	w.paint(&dragCanvas, size)
	if !canvasHasText(&dragCanvas, state.messages.SFTPWindow.DropMoveHint) {
		t.Fatalf("expected DropMoveHint %q in sftpWindow paintDrag", state.messages.SFTPWindow.DropMoveHint)
	}
	w.pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: rDstPt.X, Y: rDstPt.Y, Viewport: size, Captured: true})
	waitWindowSFTP(t, w, func() bool {
		return !w.view.busy && sftpFolderRowIndex(&w.view.sftpFolder, "notes.txt") < 0
	})
	remote.mu.Lock()
	_, remoteMoved := remote.files["/home/test/remote_sub/notes.txt"]
	remote.mu.Unlock()
	if !remoteMoved {
		t.Fatal("expected notes.txt to be moved into /home/test/remote_sub")
	}

	// 2. Local pane same-side drag-move: local_file.txt -> local_sub.
	w.paint(&canvas, size)
	lSrc := sftpFolderRowIndex(&w.view.lfolder, "local_file.txt")
	lDst := sftpFolderRowIndex(&w.view.lfolder, "local_sub")
	lSrcPt := rowCenterPoint(w.ui.list[0], w.ui.offset[0], lSrc)
	lDstPt := rowCenterPoint(w.ui.list[0], w.ui.offset[0], lDst)
	w.pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, X: lSrcPt.X, Y: lSrcPt.Y, Viewport: size, Target: w.rowID(0, lSrc)})
	w.pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, X: lDstPt.X, Y: lDstPt.Y, Viewport: size, Captured: true})
	if !w.drag.active || w.drag.target.kind != sftpDragMove || !sameSFTPPath(true, w.drag.target.dir, filepath.Join(localDir, "local_sub")) {
		t.Fatalf("expected local same-side move target, got %+v", w.drag.target)
	}
	w.pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: lDstPt.X, Y: lDstPt.Y, Viewport: size, Captured: true})
	waitWindowSFTP(t, w, func() bool {
		return !w.view.localBusy && sftpFolderRowIndex(&w.view.lfolder, "local_file.txt") < 0
	})
	if _, err := os.Stat(filepath.Join(localDir, "local_sub", "local_file.txt")); err != nil {
		t.Fatalf("expected local_file.txt moved into local_sub/: %v", err)
	}

	// 3. Local pane directory tree drop target: enable tree, drag file onto tree node.
	if err := os.WriteFile(filepath.Join(localDir, "tree_file.txt"), []byte("tree"), 0o644); err != nil {
		t.Fatal(err)
	}
	state.listLocal(w.view, localDir)
	w.enableDirectoryTree(0, localDir)
	treeRowIdx := -1
	waitWindowSFTP(t, w, func() bool {
		w.paint(&canvas, size)
		for i, row := range w.directoryTree[0].visible {
			if row.node != nil && sameSFTPPath(true, row.node.path, filepath.Join(localDir, "local_sub")) {
				treeRowIdx = i
				return true
			}
		}
		return false
	})
	w.directoryTree[0].tree.ScrollIntoView(treeRowIdx)
	w.paint(&canvas, size)
	tSrc := sftpFolderRowIndex(&w.view.lfolder, "tree_file.txt")
	tSrcPt := rowCenterPoint(w.ui.list[0], w.ui.offset[0], tSrc)
	treeBounds := w.directoryTree[0].bounds
	treePt := gfx.Point{
		X: treeBounds.X + treeBounds.Width/2,
		Y: treeBounds.Y + (float32(treeRowIdx)+0.5)*sftpDirectoryTreeRowHeight - w.directoryTree[0].scrollOffset,
	}
	w.pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, X: tSrcPt.X, Y: tSrcPt.Y, Viewport: size, Target: w.rowID(0, tSrc)})
	w.pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, X: treePt.X, Y: treePt.Y, Viewport: size, Captured: true})
	if !w.drag.active || w.drag.target.kind != sftpDragMove || w.drag.target.move.kind != sftpMoveTargetTree {
		t.Fatalf("expected directory tree move target, active=%t navBusy=%t treeBounds=%+v scroll=%v rowIdx=%d pt=%+v target=%+v",
			w.drag.active, w.navigationBusy(), treeBounds, w.directoryTree[0].scrollOffset, treeRowIdx, treePt, w.drag.target)
	}
	w.pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, X: treePt.X, Y: treePt.Y, Viewport: size, Captured: true})
	waitWindowSFTP(t, w, func() bool {
		return !w.view.localBusy && sftpFolderRowIndex(&w.view.lfolder, "tree_file.txt") < 0
	})
	if _, err := os.Stat(filepath.Join(localDir, "local_sub", "tree_file.txt")); err != nil {
		t.Fatalf("expected tree_file.txt moved via directory tree drop: %v", err)
	}
}

func waitWindowSFTP(t *testing.T, w *sftpWindow, done func() bool) {
	t.Helper()
	// Windows CI can start from a slow drive root when disclosing the local
	// directory tree down to a temporary directory.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		w.wake()
		w.owner.Wake()
		if done() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	tree := &w.directoryTree[0]
	t.Fatalf("timed out waiting for sftpWindow operation: busy=%t localBusy=%t status=%q errorText=%q localPath=%q remotePath=%q localRows=%d remoteRows=%d treeEnabled=%t treeVisible=%d treeNodes=%d",
		w.view.busy, w.view.localBusy, w.view.status, w.view.errorText, w.view.lfolder.path, w.view.path,
		len(w.view.lfolder.rows), len(w.view.sftpFolder.rows), tree.enabled, len(tree.visible), len(tree.nodes))
}
