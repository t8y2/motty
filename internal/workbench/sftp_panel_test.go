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
