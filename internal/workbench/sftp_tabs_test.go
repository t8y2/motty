package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

func openSFTPTabFixture(t *testing.T) (*State, *Session, *sftpWindow, string) {
	t.Helper()
	state, source, _, _ := openTestSFTP(t)
	state.sftpOpenMode = sftpOpenTab
	state.openSFTPWindow(source)
	w := state.sftpWindows[source.ID]
	if w == nil || len(state.sftpTabs) != 1 || state.activeSFTPTab != source.ID {
		t.Fatalf("file tab = manager %v, tabs %d, active %d", w != nil, len(state.sftpTabs), state.activeSFTPTab)
	}
	waitSFTPTab(t, state, w, func() bool { return !w.view.busy && len(w.view.rows) == 2 })

	localDir := t.TempDir()
	localFile := filepath.Join(localDir, "local.yml")
	if err := os.WriteFile(localFile, []byte("name: local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.navigate(true, localDir)
	waitSFTPTab(t, state, w, func() bool {
		return !w.view.busy && w.view.lfolder.path == localDir && len(w.view.lfolder.rows) == 1
	})
	return state, source, w, localDir
}

func waitSFTPTab(t *testing.T, state *State, w *sftpWindow, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if ready() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("file tab did not settle: busy=%t local=%q remote=%q error=%q",
		w.view.busy, w.view.lfolder.path, w.view.path, w.view.errorText)
}

func rowIndexByName(t *testing.T, w *sftpWindow, local bool, name string) int {
	t.Helper()
	for index, row := range w.folder(local).rows {
		if row.entry.Name == name {
			return index
		}
	}
	t.Fatalf("row %q is missing from local=%t", name, local)
	return -1
}

func transferUsesOperation(w *sftpWindow, operation sftpclient.Operation) bool {
	for _, transfer := range w.view.worker.Transfers(nil) {
		if transfer.Operation == operation {
			return true
		}
	}
	return false
}

func TestSFTPWindowOrderChangesPresentationButKeepsLogicalSides(t *testing.T) {
	state, _, w, localDir := openSFTPTabFixture(t)

	state.sftpWindowOrder = sftpLocalLeft
	var localLeftCanvas ui.Canvas
	state.Paint(&localLeftCanvas, ui.Size{Width: 1400, Height: 900})
	localLeft, remoteRight := w.ui.list[0], w.ui.list[1]
	if localLeft.X >= remoteRight.X {
		t.Fatalf("local-left layout: local %+v, remote %+v", localLeft, remoteRight)
	}

	state.sftpWindowOrder = sftpRemoteLeft
	state.invalidateSFTPWindows()
	var remoteLeftCanvas ui.Canvas
	state.Paint(&remoteLeftCanvas, ui.Size{Width: 1400, Height: 900})
	remoteLeft, localRight := w.ui.list[0], w.ui.list[1]
	if remoteLeft.X <= localRight.X {
		t.Fatalf("remote-left layout: remote %+v, local %+v", remoteLeft, localRight)
	}

	// Hit identities remain attached to their logical side when the panes move.
	localRow := rowIndexByName(t, w, true, "local.yml")
	if local, index, ok := w.rowTarget(sftpWindowRows[0].At(localRow)); !ok || !local || index != localRow {
		t.Fatalf("local row hit resolved to local=%t index=%d ok=%t", local, index, ok)
	}
	w.view.lfolder.selectRow(localRow, 0)
	if !w.view.lfolder.rows[localRow].selected {
		t.Fatal("clicking a local row did not select the logical local side")
	}
	remoteRow := rowIndexByName(t, w, false, "notes.txt")
	if local, index, ok := w.rowTarget(sftpWindowRows[1].At(remoteRow)); !ok || local || index != remoteRow {
		t.Fatalf("remote row hit resolved to local=%t index=%d ok=%t", local, index, ok)
	}
	w.view.sftpFolder.selectRow(remoteRow, 0)
	if !w.view.sftpFolder.rows[remoteRow].selected {
		t.Fatal("clicking a remote row did not select the logical remote side")
	}

	// A system drop on the visually left pane is still a remote destination.
	local, target, _, valid := w.dropTarget(localRight.X+24, localRight.Y+localRight.Height-12, true)
	if !valid || local || target != "/home/test" {
		t.Fatalf("system drop target = local %t, path %q, valid %t", local, target, valid)
	}
	local, target, _, valid = w.dropTarget(remoteLeft.X+24, remoteLeft.Y+remoteLeft.Height-12, false)
	if !valid || !local || target != localDir {
		t.Fatalf("remote drag target = local %t, path %q, valid %t", local, target, valid)
	}

	// Transfer buttons continue to use local-to-remote and remote-to-local
	// directions even when the remote pane is painted first.
	w.queueSelection(true)
	waitSFTPTab(t, state, w, func() bool { return transferUsesOperation(w, sftpclient.OperationUpload) })
	waitSFTPTab(t, state, w, func() bool { return !w.view.busy })
	remoteRow = rowIndexByName(t, w, false, "notes.txt")
	w.view.sftpFolder.selectRow(remoteRow, 0)
	w.queueSelection(false)
	waitSFTPTab(t, state, w, func() bool { return transferUsesOperation(w, sftpclient.OperationDownload) })
}

func TestSFTPTabsShareHostEventsAndCollectInTheBackground(t *testing.T) {
	state, source, w, localDir := openSFTPTabFixture(t)
	workspace := state.deck.ActiveWorkspace()
	if workspace == 0 {
		t.Fatal("the fixture needs a terminal workspace tab")
	}

	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1500, Height: 900})
	workspaceHit, workspaceOK := hitByID(&canvas, hitTabList.At(int(workspace)))
	fileHit, fileOK := hitByID(&canvas, sftpFileTabID(source.ID))
	if !workspaceOK || !fileOK || workspaceHit.Bounds.X >= fileHit.Bounds.X {
		t.Fatalf("top tab order: workspace=%+v/%t file=%+v/%t", workspaceHit.Bounds, workspaceOK, fileHit.Bounds, fileOK)
	}

	mount := sftpWindowMount(w)
	inputID := tree.MountID(mount, sftpWindowPath[1])
	if state.SFTPTabInput(app.TextInputContext{ID: inputID}) == nil {
		t.Fatal("the embedded file manager did not resolve a text input")
	}
	remoteRow := rowIndexByName(t, w, false, "notes.txt")
	target := tree.MountID(mount, sftpWindowRows[1].At(remoteRow))
	if result := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: target,
		X: w.ui.list[1].X + 24, Y: w.ui.list[1].Y + 24,
		Viewport: ui.Size{Width: 1500, Height: 900},
	}); !result.Handled {
		t.Fatal("the embedded file manager did not receive pointer input")
	}
	if result := state.Key(ui.KeyEvent{Target: target, Key: ui.KeyDown}); !result.Handled {
		t.Fatal("the embedded file manager did not receive key input")
	}
	scrollRect := w.ui.list[1]
	if result := state.Scroll(ui.ScrollEvent{
		X: state.fileManagerRect.X + scrollRect.X + 24,
		Y: state.fileManagerRect.Y + scrollRect.Y + 24, DeltaY: 10,
		Viewport: ui.Size{Width: 1500, Height: 900},
	}); !result.Handled {
		t.Fatal("the embedded file manager did not receive scrolling")
	}

	external := filepath.Join(t.TempDir(), "dropped.txt")
	if err := os.WriteFile(external, []byte("drop"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The fixture has the remote pane on the left. A system file drop there is
	// still routed to the remote destination and the shared worker.
	state.sftpWindowOrder = sftpRemoteLeft
	w.navigate(false, "/home/test")
	waitSFTPTab(t, state, w, func() bool { return !w.view.busy })
	var remoteLeftCanvas ui.Canvas
	state.Paint(&remoteLeftCanvas, ui.Size{Width: 1500, Height: 900})
	remoteRect := w.ui.list[1]
	if result := state.FileDrop(ui.FileDropEvent{
		Phase: ui.FileDropDrop, X: remoteRect.X + 24, Y: remoteRect.Y + remoteRect.Height - 12,
		Paths: []string{external},
	}); !result.Handled {
		t.Fatal("the embedded file manager did not accept a system file drop")
	}
	waitSFTPTab(t, state, w, func() bool { return transferUsesOperation(w, sftpclient.OperationUpload) })

	// Work continues when the terminal tab is in front.
	state.Click(hitTabList.At(int(workspace)))
	if state.activeSection != sectionTerminal {
		t.Fatal("clicking the workspace tab did not hide the file manager")
	}
	w.navigate(false, "/home/test/docs")
	waitSFTPTab(t, state, w, func() bool { return !w.view.busy && w.view.path == "/home/test/docs" })
	if w.view.worker == nil {
		t.Fatal("hiding the file tab closed its worker")
	}
	localIndex := rowIndexByName(t, w, true, "local.yml")
	w.view.lfolder.selectRow(localIndex, 0)
	w.queueSelection(true)
	waitSFTPTab(t, state, w, func() bool { return transferUsesOperation(w, sftpclient.OperationUpload) })
	waitSFTPTab(t, state, w, func() bool { return !sftpTransfersActive(w.view.worker.Transfers(nil)) })
	if w.view.path != "/home/test/docs" {
		t.Fatalf("background transfer replaced the hidden tab's remote folder: %q", w.view.path)
	}

	state.openSFTPWindow(source)
	if len(state.sftpTabs) != 1 || state.activeSection != sectionSFTP || state.activeSFTPTab != source.ID {
		t.Fatal("opening the same source again did not activate its existing tab")
	}

	if result := state.Key(ui.KeyEvent{Key: ui.KeyTab, Modifiers: ui.ModifierControl}); !result.Handled || state.activeSection != sectionTerminal {
		t.Fatalf("Control+Tab did not move from the file tab to the workspace: %+v, section %d", result, state.activeSection)
	}
	if result := state.Key(ui.KeyEvent{Key: ui.KeyTab, Modifiers: ui.ModifierControl}); !result.Handled || state.activeSection != sectionSFTP {
		t.Fatalf("Control+Tab did not return to the file tab: %+v, section %d", result, state.activeSection)
	}
	if got := w.textInput(app.TextInputContext{ID: sftpWindowPath[1]}); got == nil || got.Editor == nil {
		t.Fatal("the mounted path field lost its editor after tab switching")
	}

	// The side dock remains independent from the tab's source and local folder.
	if w.session.ID != source.ID || w.view.lfolder.path != localDir {
		t.Fatalf("source or local folder changed: session %d/%d, local %q/%q", w.session.ID, source.ID, w.view.lfolder.path, localDir)
	}
	if result := commandW(state); !result.Handled {
		t.Fatal("the close-tab shortcut was not handled")
	}
	if len(state.sftpTabs) != 0 || !w.closed {
		t.Fatal("the close-tab shortcut did not close the file tab")
	}
}

func TestSFTPFileTabRemainsInTheTopBarWithVerticalWorkspaceTabs(t *testing.T) {
	state, source, _, _ := openSFTPTabFixture(t)
	state.setTabOrientation(tabsVertical)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})

	fileHit, ok := hitByID(&canvas, sftpFileTabID(source.ID))
	if !ok || fileHit.Bounds.Y >= windowBarHeight || fileHit.Bounds.Width <= barButtonSize {
		t.Fatalf("vertical layout file tab = %+v, painted=%t", fileHit.Bounds, ok)
	}
	workspace := state.deck.ActiveWorkspace()
	if _, ok := hitByID(&canvas, hitTabList.At(int(workspace))); ok {
		t.Fatal("workspace tabs were duplicated into the horizontal bar in vertical mode")
	}
	if state.fileTabStrip.area.Width <= 0 {
		t.Fatal("the top file-tab strip was not laid out")
	}
	if result := state.Key(ui.KeyEvent{Target: sftpFileTabID(source.ID), Key: ui.KeyRight}); !result.Handled {
		t.Fatal("horizontal file-tab navigation stopped working with vertical workspace tabs")
	}
}

func TestSFTPFileTabCloseConfirmationAndWindowModeOpening(t *testing.T) {
	state, source, _, _ := openTestSFTP(t)
	windowCalls := 0
	state.options.OpenContentWindow = func(app.Config, *ui.Canvas) (*app.ContentWindow, error) {
		windowCalls++
		return nil, errors.New("test stops before creating a native window")
	}

	state.sftpOpenMode = sftpOpenWindow
	state.openSFTPWindow(source)
	if windowCalls != 1 || len(state.sftpTabs) != 0 {
		t.Fatalf("window mode calls=%d tabs=%d", windowCalls, len(state.sftpTabs))
	}

	state.sftpOpenMode = sftpOpenTab
	state.openSFTPWindow(source)
	w := state.sftpWindows[source.ID]
	if w == nil || windowCalls != 1 || len(state.sftpTabs) != 1 {
		t.Fatalf("tab mode calls=%d manager=%v tabs=%d", windowCalls, w != nil, len(state.sftpTabs))
	}
	waitSFTPTab(t, state, w, func() bool { return !w.view.busy })

	w.view.extracting = true
	state.requestCloseSFTPTab(w)
	if !w.closeConfirm || w.closed || len(state.sftpTabs) != 1 {
		t.Fatal("closing an active file tab skipped confirmation")
	}
	w.click(hitSFTPWindowCloseCancel)
	if w.closeConfirm || w.closed {
		t.Fatal("canceling close confirmation closed the file tab")
	}
	w.view.extracting = false
	state.requestCloseSFTPTab(w)
	if !w.closed || len(state.sftpTabs) != 0 {
		t.Fatal("the confirmed file tab did not close")
	}
}

func TestSFTPFileTabIsRuntimeOnlyAndSourceCloseRemovesIt(t *testing.T) {
	state, source, w, _ := openSFTPTabFixture(t)

	// The state snapshot has no file-tab collection, so saving and reloading a
	// running tab cannot restore it as a fake connected session.
	before := state.snapshot()
	if before.SFTPOpenMode != "tab" || before.SFTPWindowOrder != "local-left" {
		t.Fatalf("snapshot settings = %q/%q", before.SFTPWindowOrder, before.SFTPOpenMode)
	}
	if len(state.sftpTabs) != 1 || w.closed {
		t.Fatal("the fixture did not keep a live runtime-only file tab")
	}

	state.closeSession(source.ID)
	source.files = nil // openTestSFTP's cleanup must not close the deleted source twice
	if len(state.sftpTabs) != 0 || !w.closed {
		t.Fatal("closing the source terminal did not close its runtime file tab")
	}
}
