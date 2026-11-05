package workbench

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

func TestSFTPPanelSettingsPersistAndDoNotMoveExistingWorkspace(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	workspace := state.deck.ActiveWorkspace()
	before, _ := state.deck.Context(workspace)
	if state.sftpPanelAutoOpen || state.sftpPanelSide != layoutcore.ContextLeft {
		t.Fatal("legacy settings must default to hidden, left")
	}
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsFiles)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 1100})
	for _, id := range []ui.ID{hitSettingsPanelAutoOpen, hitSettingsPanelLeft, hitSettingsPanelRight} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Fatal("missing file panel setting", id)
		}
	}
	state.Click(hitSettingsPanelAutoOpen)
	result := state.Key(ui.KeyEvent{Target: hitSettingsPanelLeft, Key: ui.KeyRight})
	if !result.Handled || result.Focus != hitSettingsPanelRight || state.sftpPanelSide != layoutcore.ContextRight {
		t.Fatal("panel position does not support arrow-key selection")
	}
	state.Click(hitSettingsPanelRight)
	if current, _ := state.deck.Context(workspace); current != before {
		t.Fatal("editing defaults moved an existing workspace")
	}
	saved, _, err := loadState(state.options.StatePath)
	if err != nil || !saved.SFTPPanelAutoOpen || saved.SFTPPanelSide != "right" {
		t.Fatalf("panel settings were not saved: %+v, %v", saved, err)
	}
	restored := newTestState(t, state.options)
	context, _ := restored.deck.Context(restored.deck.ActiveWorkspace())
	if !restored.sftpPanelAutoOpen || restored.sftpPanelSide != layoutcore.ContextRight || !context.Open || context.Side != layoutcore.ContextRight {
		t.Fatal("startup used the previous left-docked layout instead of the defaults")
	}
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		state.setLanguage(language)
		for _, label := range []string{state.messages.Tools.FilesPanelAutoOpen, state.messages.Tools.FilesPanelPosition} {
			app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextSelectAll}.Apply(&state.settings.search)
			searchSettings(t, state, label)
			if state.settings.count == 0 || state.settings.results[0].category != settingsFiles {
				t.Fatal("panel setting is missing from search", label)
			}
		}
	}
	// Cold restore may hide a formerly open panel, but switching to it at runtime
	// must not undo the header's choice or start a second SFTP worker.
	state.ActivatePane(session.ID)
	view, worker := session.files, session.files.worker
	state.Click(contextID(hitSFTPPanelSide))
	context, _ = state.deck.Context(workspace)
	if context.Side != layoutcore.ContextRight || session.files != view || session.files.worker != worker {
		t.Fatal("moving the panel recreated its file view or worker")
	}
	state.Click(hitSFTPClose)
	state.Click(hitSettingsPanelAutoOpen)
	state.Click(hitSettingsPanelLeft)
	restored = newTestState(t, state.options)
	context, _ = restored.deck.Context(restored.deck.ActiveWorkspace())
	if context.Open || context.Side != layoutcore.ContextLeft {
		t.Fatal("disabled/left startup defaults were not applied")
	}
}

func TestSFTPPanelMovingDoesNotInterruptActiveTransfer(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	files := session.files
	files.worker.Close()
	gated := &centerGatedRemote{workbenchSFTPRemote: remote, gate: make(chan struct{}), opened: make(chan struct{}, 1)}
	files.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return gated, nil }, nil)
	defer files.worker.Close()
	worker := files.worker
	target := filepath.Join(t.TempDir(), "notes.txt")
	state.queueSFTPDownload(files, files.rows[1].entry, target)
	select {
	case <-gated.opened:
	case <-time.After(3 * time.Second):
		t.Fatal("download did not begin")
	}
	state.Click(hitTransfersClose)
	for range 4 {
		state.Click(hitSFTPPanelSide)
	}
	if session.files != files || files.worker != worker {
		t.Fatal("moving replaced an active worker")
	}
	if tasks := worker.Transfers(nil); len(tasks) != 1 || tasks[0].State != sftpclient.TransferRunning {
		t.Fatal("moving interrupted the active download", tasks)
	}
	gated.once.Do(func() { close(gated.gate) })
	waitTransfersDone(t, state, session, 1)
	if data, err := os.ReadFile(target); err != nil || string(data) != "hello world" {
		t.Fatal("download did not finish at its original destination", err)
	}
}

func TestSFTPPanelInitialDefaultsAreWorkspaceLocalAndNotReappliedOnReconnectOrSplit(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.sftpPanelAutoOpen, state.sftpPanelSide = true, layoutcore.ContextRight
	first := state.newHostSession(state.hosts[0])
	firstID := first.ID
	workspace := state.deck.ActiveWorkspace()
	context, _ := state.deck.Context(workspace)
	if !context.Open || context.Side != layoutcore.ContextRight {
		t.Fatal("new workspace ignored defaults")
	}
	state.Click(hitSFTPPanelSide)
	state.Click(hitSFTPClose)
	want, _ := state.deck.Context(workspace)
	second := state.newHostSession(state.hosts[1])
	if context, _ = state.deck.Context(state.deck.ActiveWorkspace()); !context.Open || context.Side != layoutcore.ContextRight || second.ID == firstID {
		t.Fatal("manual presentation leaked into the next workspace")
	}
	state.activateWorkspace(workspace)
	first, _ = state.sessionByIDPointer(firstID)
	state.startConnection(first, first.host)
	if context, _ = state.deck.Context(workspace); context != want {
		t.Fatal("reconnect reapplied defaults")
	}
	openHostInSplit(t, state, state.hosts[1].ID, layoutcore.Horizontal)
	if context, _ = state.deck.Context(workspace); context != want {
		t.Fatal("splitting reapplied defaults")
	}
	local := state.createHostSessionWithConnection(localHost("Local"), "local", &fakeConn{status: sshconn.Status{Version: 1, Phase: sshconn.PhaseConnected}}, "", nil)
	if !local.isLocal() {
		t.Fatal("test did not open a local terminal")
	}
	if context, _ = state.deck.Context(state.deck.ActiveWorkspace()); !context.Open || context.Side != layoutcore.ContextRight {
		t.Fatal("local files did not use the same defaults")
	}
	unsupported := state.hosts[0]
	unsupported.Protocol = protocolTelnet
	state.createHostSessionWithConnection(unsupported, unsupported.Address, &fakeConn{status: sshconn.Status{Version: 1, Phase: sshconn.PhaseConnecting}}, "", nil)
	if context, _ = state.deck.Context(state.deck.ActiveWorkspace()); context.Open {
		t.Fatal("non-file-capable protocol auto-opened a panel")
	}
}

func TestSFTPToolbarButtonTogglesPanel(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	workspace := state.deck.ActiveWorkspace()
	context, ok := state.deck.Context(workspace)
	if !ok || !context.Open || context.Kind != layoutcore.ContextSFTP {
		t.Fatal("openTestSFTP did not leave the panel open")
	}

	// Clicking hitSFTPOpen while open toggles it closed.
	state.Click(hitSFTPOpen)
	context, ok = state.deck.Context(workspace)
	if !ok || context.Open {
		t.Fatalf("clicking hitSFTPOpen while open did not close the panel: %+v", context)
	}

	// Clicking hitSFTPOpen while closed toggles it open.
	state.Click(hitSFTPOpen)
	context, ok = state.deck.Context(workspace)
	if !ok || !context.Open || context.Kind != layoutcore.ContextSFTP {
		t.Fatalf("clicking hitSFTPOpen while closed did not reopen the panel: %+v", context)
	}
	if session.files == nil {
		t.Fatal("reopening the panel did not restore session.files")
	}
}

// Showing a folder from the terminal menu opens the panel, and a panel that is
// already up keeps the view the navigation runs on instead of toggling shut.
func TestShowInFilesKeepsAnOpenPanelOpen(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test" })
	state.showInFiles(session, "/home/test/docs/guide.md")
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs" })
	if context, _ := state.deck.Context(state.deck.ActiveWorkspace()); !context.Open || context.Kind != layoutcore.ContextSFTP {
		t.Fatalf("showing a folder closed the panel: %+v", context)
	}
}

func TestSFTPPanelRemembersResizedWidthAcrossWorkspacesAndRestores(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	size := ui.Size{Width: 1400, Height: 900}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	divider, ok := hitByID(&canvas, hitContextDivider)
	if !ok {
		t.Fatal("context divider is missing")
	}
	base := state.baseWorkspacePixelBounds(size)
	dragX := base.X + 420 + 0.5
	y := divider.Bounds.Y + divider.Bounds.Height/2
	state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hitContextDivider, X: divider.Bounds.X, Y: y, Viewport: size})
	state.Pointer(ui.PointerEvent{Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Target: hitContextDivider, Captured: true, X: dragX, Y: y, Viewport: size})
	state.Pointer(ui.PointerEvent{Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Target: hitContextDivider, Captured: true, X: dragX, Y: y, Viewport: size})

	if state.sftpPanelWidth < 419 || state.sftpPanelWidth > 421 {
		t.Fatalf("state.sftpPanelWidth = %v, want ~420", state.sftpPanelWidth)
	}

	// Toggling the panel closed and open again in the same workspace remembers the width.
	state.Click(hitSFTPOpen)
	state.Click(hitSFTPOpen)
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	if context.Width < 419 || context.Width > 421 {
		t.Fatalf("reopened panel width = %v, want ~420", context.Width)
	}

	// Opening a new workspace remembers the adjusted width.
	second := state.newHostSession(state.hosts[1])
	secondWorkspace := state.deck.ActiveWorkspace()
	secondContext, _ := state.deck.Context(secondWorkspace)
	if secondContext.Width < 419 || secondContext.Width > 421 {
		t.Fatalf("new workspace panel width = %v, want ~420 (session %v)", secondContext.Width, second.ID)
	}

	// Restoring the app remembers the adjusted width.
	restored := newTestState(t, state.options)
	if restored.sftpPanelWidth < 419 || restored.sftpPanelWidth > 421 {
		t.Fatalf("restored.sftpPanelWidth = %v, want ~420", restored.sftpPanelWidth)
	}
	third := restored.newHostSession(restored.hosts[0])
	thirdContext, _ := restored.deck.Context(restored.deck.ActiveWorkspace())
	if thirdContext.Width < 419 || thirdContext.Width > 421 {
		t.Fatalf("restored new workspace panel width = %v, want ~420 (session %v)", thirdContext.Width, third.ID)
	}
	_ = session
}
