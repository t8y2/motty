package workbench

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

func openSFTPTabFixture(t *testing.T) (*State, *Session, *sftpWindow, *workbenchSFTPRemote, string) {
	t.Helper()
	state, source, _, remote := openTestSFTP(t)
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
	return state, source, w, remote, localDir
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

func transferCompleted(view *sftpView, operation sftpclient.Operation, destination string) bool {
	for _, transfer := range view.transfers {
		if transfer.Operation == operation && transfer.Destination == destination && transfer.State == sftpclient.TransferCompleted {
			return true
		}
	}
	return false
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

func TestSFTPWindowOrderChangesPresentationButKeepsLogicalSides(t *testing.T) {
	state, _, w, remote, localDir := openSFTPTabFixture(t)

	state.sftpWindowOrder = sftpLocalLeft
	var localLeftCanvas gfx.Canvas
	state.Paint(&localLeftCanvas, gfx.Size{Width: 1400, Height: 900})
	localLeft, remoteRight := w.ui.list[0], w.ui.list[1]
	if localLeft.X >= remoteRight.X {
		t.Fatalf("local-left layout: local %+v, remote %+v", localLeft, remoteRight)
	}

	state.sftpWindowOrder = sftpRemoteLeft
	state.invalidateSFTPWindows()
	var remoteLeftCanvas gfx.Canvas
	state.Paint(&remoteLeftCanvas, gfx.Size{Width: 1400, Height: 900})
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
	waitSFTPTab(t, state, w, func() bool {
		return remoteText(remote, "/home/test/local.yml") == "name: local\n" &&
			transferCompleted(w.view, sftpclient.OperationUpload, "/home/test/local.yml")
	})
	waitSFTPTab(t, state, w, func() bool {
		return !w.view.busy &&
			w.browse.pending == nil &&
			!w.view.refreshAfterTransfers &&
			!sftpTransfersActive(w.view.worker.Transfers(nil))
	})
	remoteRow = rowIndexByName(t, w, false, "notes.txt")
	w.view.sftpFolder.selectRow(remoteRow, 0)
	w.queueSelection(false)
	waitSFTPTab(t, state, w, func() bool {
		content, err := os.ReadFile(filepath.Join(localDir, "notes.txt"))
		return err == nil && string(content) == "hello world" &&
			transferCompleted(w.view, sftpclient.OperationDownload, filepath.Join(localDir, "notes.txt"))
	})
}

func TestSFTPTabsShareHostEventsAndCollectInTheBackground(t *testing.T) {
	state, source, w, remote, localDir := openSFTPTabFixture(t)
	workspace := state.deck.ActiveWorkspace()
	if workspace == 0 {
		t.Fatal("the fixture needs a terminal workspace tab")
	}

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1500, Height: 900})
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
	if result := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: target,
		X: w.ui.list[1].X + 24, Y: w.ui.list[1].Y + 24,
		Viewport: gfx.Size{Width: 1500, Height: 900},
	}); !result.Handled {
		t.Fatal("the embedded file manager did not receive pointer input")
	}
	if result := state.Key(gfx.KeyEvent{Target: target, Key: gfx.KeyDown}); !result.Handled {
		t.Fatal("the embedded file manager did not receive key input")
	}
	scrollRect := w.ui.list[1]
	if result := state.Scroll(gfx.ScrollEvent{
		X: state.fileManagerRect.X + scrollRect.X + 24,
		Y: state.fileManagerRect.Y + scrollRect.Y + 24, DeltaY: 10,
		Viewport: gfx.Size{Width: 1500, Height: 900},
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
	var remoteLeftCanvas gfx.Canvas
	state.Paint(&remoteLeftCanvas, gfx.Size{Width: 1500, Height: 900})
	remoteRect := w.ui.list[1]
	dropX := state.fileManagerRect.X + remoteRect.X + 24
	dropY := state.fileManagerRect.Y + remoteRect.Y + remoteRect.Height - 12
	_, remoteDropDir, _, valid := w.dropTarget(
		dropX-state.fileManagerRect.X,
		dropY-state.fileManagerRect.Y,
		true,
	)
	if !valid {
		t.Fatal("the remote pane did not accept the system file drop")
	}
	if result := state.FileDrop(gfx.FileDropEvent{
		Phase: gfx.FileDropDrop,
		X:     dropX,
		Y:     dropY,
		Paths: []string{external},
	}); !result.Handled {
		t.Fatal("the embedded file manager did not accept a system file drop")
	}
	remoteDropPath := path.Join(remoteDropDir, filepath.Base(external))
	waitSFTPTab(t, state, w, func() bool {
		return remoteText(remote, remoteDropPath) == "drop" &&
			transferCompleted(w.view, sftpclient.OperationUpload, remoteDropPath)
	})
	waitSFTPTab(t, state, w, func() bool {
		return !w.view.busy &&
			w.browse.pending == nil &&
			!w.view.refreshAfterTransfers &&
			!sftpTransfersActive(w.view.worker.Transfers(nil))
	})

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
	waitSFTPTab(t, state, w, func() bool {
		return remoteText(remote, "/home/test/docs/local.yml") == "name: local\n" &&
			transferCompleted(w.view, sftpclient.OperationUpload, "/home/test/docs/local.yml")
	})
	if w.view.path != "/home/test/docs" {
		t.Fatalf("background transfer replaced the hidden tab's remote folder: %q", w.view.path)
	}

	state.openSFTPWindow(source)
	if len(state.sftpTabs) != 1 || state.activeSection != sectionSFTP || state.activeSFTPTab != source.ID {
		t.Fatal("opening the same source again did not activate its existing tab")
	}

	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyTab, Modifiers: gfx.ModifierControl}); !result.Handled || state.activeSection != sectionTerminal {
		t.Fatalf("Control+Tab did not move from the file tab to the workspace: %+v, section %d", result, state.activeSection)
	}
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyTab, Modifiers: gfx.ModifierControl}); !result.Handled || state.activeSection != sectionSFTP {
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

func TestEmbeddedSFTPWakeDoesNotRescanOwnedConnections(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	state.sftpWindows = make(map[uint32]*sftpWindow)
	connections := []*fakeConn{{}, {}}
	for index, conn := range connections {
		id := uint32(index + 2)
		state.sftpWindows[id] = &sftpWindow{
			owner:          state,
			session:        Session{ID: id, conn: conn},
			view:           &sftpView{},
			ownsConnection: true,
		}
	}

	remote.mu.Lock()
	remote.reads = nil
	remote.mu.Unlock()
	if !view.worker.ListWithID("/woken", 0) {
		t.Fatal("the background wake request was not queued")
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) && remote.readCount("/woken") == 0 {
		w := &sftpWindow{owner: state, session: *session, view: view, ownsConnection: true}
		state.sftpWindows[session.ID] = w
		state.wakeEmbeddedSFTPWindows()
		delete(state.sftpWindows, session.ID)
		time.Sleep(time.Millisecond)
	}
	if remote.readCount("/woken") == 0 {
		t.Fatal("wakeEmbeddedSFTPWindows did not pump the tab's own worker")
	}
	for index, conn := range connections {
		conn.mu.Lock()
		reads := conn.statusReads
		conn.mu.Unlock()
		if reads != 0 {
			t.Fatalf("embedded page %d repeated a workbench-wide connection scan: %d reads", index+1, reads)
		}
	}
}

func TestSFTPFileTabRemainsInTheTopBarWithVerticalWorkspaceTabs(t *testing.T) {
	state, source, _, _, _ := openSFTPTabFixture(t)
	state.setTabOrientation(tabsVertical)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})

	fileHit, ok := hitByID(&canvas, sftpFileTabID(source.ID))
	if !ok || fileHit.Bounds.Y >= windowBarHeight || fileHit.Bounds.Width <= barButtonSize {
		t.Fatalf("vertical layout file tab = %+v, painted=%t", fileHit.Bounds, ok)
	}
	workspace := state.deck.ActiveWorkspace()
	if _, ok := hitByID(&canvas, hitTabList.At(int(workspace))); !ok {
		t.Fatal("the workspace tab rail disappeared while a file tab was active")
	}
	if state.fileTabStrip.area.Width <= 0 {
		t.Fatal("the top file-tab strip was not laid out")
	}
	if result := state.Key(gfx.KeyEvent{Target: sftpFileTabID(source.ID), Key: gfx.KeyRight}); !result.Handled {
		t.Fatal("horizontal file-tab navigation stopped working with vertical workspace tabs")
	}
}

func TestSFTPFileTabRespectsVerticalTabsAutoHide(t *testing.T) {
	state, _, _, _, _ := openSFTPTabFixture(t)
	state.setTabOrientation(tabsVertical)
	state.setVerticalTabsAutoHide(true)

	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	if state.verticalTabs.bounds.Width != verticalTabsRail || state.fileManagerRect.X != verticalTabsRail {
		t.Fatalf("collapsed rail = %+v, file manager starts at %.1f", state.verticalTabs.bounds, state.fileManagerRect.X)
	}

	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerMove, X: 2, Y: windowBarHeight + 2, Viewport: size,
	})
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.verticalTabs.bounds.Width <= verticalTabsRail {
		t.Fatalf("hovering the rail did not expand it: %+v", state.verticalTabs.bounds)
	}
	if state.fileManagerRect.X != verticalTabsRail {
		t.Fatalf("the expanded rail moved page content instead of overlaying it: %.1f", state.fileManagerRect.X)
	}
}

func TestSFTPFileTabVerticalRailOwnsInputWhenExpanded(t *testing.T) {
	state, _, w, _, localDir := openSFTPTabFixture(t)
	state.setTabOrientation(tabsVertical)
	state.setVerticalTabsAutoHide(true)
	state.sftpWindowOrder = sftpRemoteLeft
	state.invalidateSFTPWindows()

	size := gfx.Size{Width: 800, Height: 700}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerMove, X: 2, Y: windowBarHeight + 2, Viewport: size,
	})
	canvas.Reset()
	state.Paint(&canvas, size)

	panel := state.verticalTabs.bounds
	if panel.Width <= verticalTabsRail || state.fileManagerRect.X != verticalTabsRail {
		t.Fatalf("expanded rail = %+v, file manager starts at %.1f", panel, state.fileManagerRect.X)
	}
	remoteList := w.ui.list[1]
	x := state.fileManagerRect.X + remoteList.X + 10
	y := state.fileManagerRect.Y + remoteList.Y + remoteList.Height/2
	if !pointInRect(x, y, panel) {
		t.Fatalf("test point %.1f,%.1f is outside expanded rail %+v", x, y, panel)
	}

	w.ui.bars[1].MaxOffsetY = 120
	w.ui.offset[1] = 20
	result := state.Scroll(gfx.ScrollEvent{
		X: x, Y: y, DeltaY: 30, Precise: true, Viewport: size,
	})
	if !result.Handled || w.ui.offset[1] != 20 {
		t.Fatalf("expanded rail wheel = %+v, file offset %.1f", result, w.ui.offset[1])
	}

	w.view.selected = -1
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		X: x, Y: y, Viewport: size,
	})
	if w.view.selected != -1 {
		t.Fatalf("expanded rail press reached the file row behind it: selected %d", w.view.selected)
	}

	localFile := filepath.Join(localDir, "local.yml")
	over := gfx.FileDropEvent{Phase: gfx.FileDropOver, X: x, Y: y, Paths: []string{localFile}}
	if result := state.FileDrop(over); !result.Handled || w.drag.active {
		t.Fatalf("expanded rail accepted a file hover into the page: %+v, drag=%t", result, w.drag.active)
	}
	drop := over
	drop.Phase = gfx.FileDropDrop
	if result := state.FileDrop(drop); !result.Handled || len(w.view.worker.Transfers(nil)) != 0 {
		t.Fatalf("expanded rail accepted a file drop into the page: %+v, transfers=%d",
			result, len(w.view.worker.Transfers(nil)))
	}
}

func TestSFTPFileTabsScrollAndRemainClickableWithVerticalWorkspaceTabs(t *testing.T) {
	state, _, active, _, _ := openSFTPTabFixture(t)
	state.setTabOrientation(tabsVertical)
	firstID := active.session.ID
	ids := []uint32{firstID}
	for index := range 20 {
		id := firstID + uint32(index) + 1
		manager := &sftpWindow{
			owner: state,
			session: Session{
				ID:      id,
				HostID:  active.session.HostID,
				Title:   "overflow-" + string(rune('A'+index)),
				Address: active.session.Address,
				host:    active.session.host,
			},
			view: &sftpView{},
		}
		state.sftpWindows[id] = manager
		state.sftpTabs = append(state.sftpTabs, manager)
		ids = append(ids, id)
	}
	state.rebuildFileTabOptions()

	size := gfx.Size{Width: 720, Height: 600}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	if state.fileTabStrip.max <= 0 {
		t.Fatal("twenty file tabs did not overflow the narrow top strip")
	}
	start := state.fileTabStrip.offset
	result := state.Scroll(gfx.ScrollEvent{
		X:      state.fileTabStrip.area.X + 24,
		Y:      state.fileTabStrip.area.Y + state.fileTabStrip.area.Height/2,
		DeltaY: 160, Viewport: size,
	})
	if !result.Handled || !result.Repaint || state.fileTabStrip.offset != start+160 {
		t.Fatalf("file-tab wheel = %+v, offset %.1f -> %.1f, want %.1f", result, start, state.fileTabStrip.offset, start+160)
	}

	for range 30 {
		state.Scroll(gfx.ScrollEvent{
			X:      state.fileTabStrip.area.X + 24,
			Y:      state.fileTabStrip.area.Y + state.fileTabStrip.area.Height/2,
			DeltaY: 160, Viewport: size,
		})
	}
	if state.fileTabStrip.offset != state.fileTabStrip.max {
		t.Fatalf("file-tab wheel stopped at %.1f, want the end %.1f", state.fileTabStrip.offset, state.fileTabStrip.max)
	}
	canvas.Reset()
	state.Paint(&canvas, size)
	last := sftpFileTabID(ids[len(ids)-1])
	hit, ok := hitByID(&canvas, last)
	strip := state.fileTabStrip.area
	if !ok || hit.Bounds.X < strip.X || hit.Bounds.Right() > strip.Right() {
		t.Fatalf("last file tab is not visible after scrolling: hit=%+v painted=%t strip=%+v", hit.Bounds, ok, strip)
	}
	state.Click(last)
	if state.activeSection != sectionSFTP || state.activeSFTPTab != ids[len(ids)-1] {
		t.Fatalf("clicking the revealed file tab selected section %d tab %d", state.activeSection, state.activeSFTPTab)
	}

	stale := state.fileTabStrip
	for len(state.sftpTabs) > 0 {
		state.removeSFTPTab(state.sftpTabs[len(state.sftpTabs)-1])
	}
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.fileTabStrip.area != (gfx.Rect{}) || state.fileTabStrip.max != 0 || state.fileTabStrip.offset != 0 {
		t.Fatalf("closing every file tab left a stale strip behind: %+v", state.fileTabStrip)
	}
	if stale.area.Width > 0 {
		state.Scroll(gfx.ScrollEvent{
			X: stale.area.X + 24, Y: stale.area.Y + stale.area.Height/2,
			DeltaY: 160, Viewport: size,
		})
		if state.fileTabStrip.offset != 0 {
			t.Fatalf("a closed file-tab strip still consumed wheel input: offset %.1f", state.fileTabStrip.offset)
		}
	}
}

func TestWorkspaceTabsRemainScrollableBesideFileTabs(t *testing.T) {
	state, _, _, _, _ := openSFTPTabFixture(t)
	for range 20 {
		openEmptyTab(state)
	}
	state.setTabOrientation(tabsVertical)
	size := gfx.Size{Width: 720, Height: 600}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	if state.tabStrip.max <= 0 || state.tabStrip.offset != state.tabStrip.max {
		t.Fatalf("workspace tabs did not overflow and reveal the active tab: max=%.1f offset=%.1f", state.tabStrip.max, state.tabStrip.offset)
	}

	area := state.tabStrip.area
	result := state.Scroll(gfx.ScrollEvent{
		X: area.X + 10, Y: area.Y + 10, DeltaY: -10000, Viewport: size,
	})
	if !result.Handled || !result.Repaint || state.tabStrip.offset != 0 {
		t.Fatalf("workspace rail wheel = %+v, offset %.1f", result, state.tabStrip.offset)
	}

	canvas.Reset()
	state.Paint(&canvas, size)
	first := state.workspaceTabs[0].ID
	if hit, ok := hitByID(&canvas, hitTabList.At(int(first))); !ok || hit.Bounds.Y < area.Y || hit.Bounds.Bottom() > area.Bottom() {
		t.Fatalf("first workspace tab is not visible after scrolling: hit=%+v painted=%t area=%+v", hit.Bounds, ok, area)
	}
	state.Click(hitTabList.At(int(first)))
	if state.activeSection != sectionTerminal || state.deck.ActiveWorkspace() != first {
		t.Fatalf("clicking the revealed workspace tab selected section %d workspace %d", state.activeSection, state.deck.ActiveWorkspace())
	}
}

func TestSFTPFileTabLeavesRoomForTheAssistantAndKeepsWheelInput(t *testing.T) {
	state, _, _, _, _ := openSFTPTabFixture(t)
	state.setAIPanelOpen(true)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1500, Height: 900})

	panel := state.ai.panel.rect
	if panel.Width <= 0 {
		t.Fatal("the docked assistant panel was not painted")
	}
	if state.fileManagerRect.Right() > panel.X {
		t.Fatalf("file manager %+v overlaps assistant %+v", state.fileManagerRect, panel)
	}
	if result := state.Scroll(gfx.ScrollEvent{
		X: panel.X + 24, Y: panel.Y + 80, DeltaY: 24,
		Viewport: gfx.Size{Width: 1500, Height: 900},
	}); !result.Handled {
		t.Fatal("a wheel over the assistant panel was not handled by the assistant")
	}
}

func TestSFTPFileTabYieldsToTheNarrowAssistantDrawer(t *testing.T) {
	state, _, w, _, _ := openSFTPTabFixture(t)
	state.setAIPanelOpen(true)
	size := gfx.Size{Width: 800, Height: 600}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	panel := state.ai.panel.rect
	if panel.Width <= 0 || state.fileManagerRect.Right() <= panel.X {
		t.Fatalf("the drawer did not overlay the full-page file tab: panel=%+v file=%+v", panel, state.fileManagerRect)
	}
	input, ok := hitByID(&canvas, hitAIInput)
	if !ok {
		t.Fatal("the drawer input was not painted")
	}
	fileOffset := w.ui.offset[0]
	result := state.Scroll(gfx.ScrollEvent{
		X: panel.X + panel.Width/2, Y: panel.Y + panel.Height/2,
		DeltaY: 120, Precise: true, Viewport: size,
	})
	if !result.Handled || w.ui.offset[0] != fileOffset {
		t.Fatalf("the drawer's wheel reached the file list: %+v, offset %.1f -> %.1f", result, fileOffset, w.ui.offset[0])
	}

	event := gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		Target: input.ID, X: input.Bounds.X + 20, Y: input.Bounds.Y + 10,
		Viewport: size,
	}
	result = state.Pointer(event)
	if result.Handled {
		t.Fatalf("the file tab swallowed the drawer's press: %+v", result)
	}
}

func TestSFTPFileTabCloseConfirmationAndWindowModeOpening(t *testing.T) {
	state, source, _, _ := openTestSFTP(t)
	windowCalls := 0
	state.options.OpenContentWindow = func(app.Config, *gfx.Canvas) (*app.ContentWindow, error) {
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
	var canvas gfx.Canvas
	w.paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if got := w.closeDescription(); got != state.messages.SFTPWindow.CloseTabDescription {
		t.Fatalf("file tab close description = %q", got)
	}
	painted := make([]string, 0, len(canvas.Texts()))
	for _, value := range canvas.Texts() {
		if value.Value == state.messages.SFTPWindow.CloseDescription {
			t.Fatal("a file tab reused the independent window's close description")
		}
		painted = append(painted, value.Value)
	}
	if !strings.Contains(strings.Join(painted, " "), state.messages.SFTPWindow.CloseTabDescription) {
		t.Fatal("the file tab close confirmation did not name the tab")
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

func TestMiddleClickFileTabUsesItsCloseConfirmation(t *testing.T) {
	state, source, _, _ := openTestSFTP(t)
	state.sftpOpenMode = sftpOpenTab
	state.openSFTPWindow(source)
	w := state.sftpWindows[source.ID]
	w.view.extracting = true
	press := gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonMiddle, Target: sftpFileTabID(source.ID)}
	result := pointer(state, press)
	if !result.Handled || !result.Repaint || !w.closeConfirm || w.closed || len(state.sftpTabs) != 1 {
		t.Fatalf("middle press skipped file-tab close confirmation: result=%+v confirm=%v closed=%v", result, w.closeConfirm, w.closed)
	}
	w.click(hitSFTPWindowCloseCancel)
	w.view.extracting = false
	press.Target = sftpFileTabCloseID(source.ID)
	pointer(state, press)
	if !w.closed || len(state.sftpTabs) != 0 {
		t.Fatal("middle press did not close the file tab")
	}
}

func TestSFTPFileTabIsRuntimeOnlyAndSourceCloseRemovesIt(t *testing.T) {
	state, source, w, _, _ := openSFTPTabFixture(t)

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
