package workbench

import (
	"fmt"
	"os"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

func TestTransferConflictPromptIsFiniteAndScopedToItsConnection(t *testing.T) {
	for _, action := range []gfx.ID{hitTransferConflictReplaceAll, hitTransferConflictSkipAll, hitTransferConflictCancel} {
		t.Run(fmt.Sprint(action), func(t *testing.T) {
			_, options := newWorld(t)
			state := newTestState(t, options)
			view, remote := bulkTestView(t, state)
			other, otherRemote := bulkTestView(t, state)
			id, target := bulkConflict(t, state, view, remote, "first", "old")
			second, secondPath := bulkConflict(t, state, view, remote, "second", "old")
			hidden, hiddenPath := bulkConflict(t, state, other, otherRemote, "hidden", "old")
			state.showTransferConflict(view)
			late, latePath := bulkConflict(t, state, view, remote, "late", "old")
			if state.transfers.open || len(view.conflictPrompt.targets) != 2 {
				t.Fatal("prompt opened full history or captured later tasks")
			}
			state.Click(action)
			phase, want := sftpclient.TransferCompleted, "hello world"
			if action == hitTransferConflictCancel {
				phase = sftpclient.TransferFailed
			}
			if action != hitTransferConflictReplaceAll {
				want = "old"
			}
			for _, id := range []uint64{id, second} {
				bulkAwait(t, state, view, id, phase)
			}
			bulkAwait(t, state, view, late, sftpclient.TransferFailed)
			bulkAwait(t, state, other, hidden, sftpclient.TransferFailed)
			for path, expected := range map[string]string{target: want, secondPath: want, latePath: "old", hiddenPath: "old"} {
				if contents, _ := os.ReadFile(path); string(contents) != expected {
					t.Fatalf("%s=%q, want %q", path, contents, expected)
				}
			}
			if action == hitTransferConflictCancel && len(view.conflictPrompt.targets) != 0 {
				t.Fatal("canceled prompt remains open")
			}
			if action != hitTransferConflictCancel && (len(view.conflictPrompt.targets) != 1 || view.conflictPrompt.targets[0].id != late) {
				t.Fatal("later conflicts did not receive a separate prompt")
			}
		})
	}
}

func TestTransferConflictReplaceHandlesOneFileWithoutASecondConfirmation(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	view, remote := bulkTestView(t, state)
	id, target := bulkConflict(t, state, view, remote, "first", "old")
	second, secondPath := bulkConflict(t, state, view, remote, "second", "old")
	state.showTransferConflict(view)
	state.Click(hitTransferConflictReplace)
	bulkAwait(t, state, view, id, sftpclient.TransferCompleted)
	bulkAwait(t, state, view, second, sftpclient.TransferFailed)
	if contents, _ := os.ReadFile(target); string(contents) != "hello world" {
		t.Fatal("one-click replacement failed")
	}
	if contents, _ := os.ReadFile(secondPath); string(contents) != "old" {
		t.Fatal("single replace overwrote the next file")
	}
	if len(view.conflictPrompt.targets) != 1 || len(state.transfers.batch.targets) != 0 {
		t.Fatal("single replace opened another confirmation or lost the next conflict")
	}
	state.Click(hitTransferConflictReplace)
	bulkAwait(t, state, view, second, sftpclient.TransferCompleted)
}

func TestTransferConflictWaitsForAnotherConnectionsFileDialog(t *testing.T) {
	for _, surface := range []string{"panel", "tab"} {
		t.Run(surface, func(t *testing.T) {
			var state *State
			var active *sftpView
			if surface == "tab" {
				s, _, w, _, _ := openSFTPTabFixture(t)
				state, active = s, w.view
			} else {
				s, session, _, _ := openTestSFTP(t)
				state, active = s, session.files
			}
			background, remote := bulkTestView(t, state)
			bulkConflict(t, state, background, remote, "background", "old")
			state.showTransferConflict(background)
			state.openSFTPDialog(active, sftpDialogCreateDirectory)
			if state.transferConflictView() != nil {
				t.Fatal("a background conflict interrupted the active connection's dialog")
			}
			state.closeSFTPDialog(active)
			if state.transferConflictView() != background {
				t.Fatal("the background conflict was lost while waiting for the dialog")
			}
		})
	}
}

func TestTransferConflictBlocksSFTPWindowBackgroundInteraction(t *testing.T) {
	state, _, w, remote, _ := openSFTPTabFixture(t)
	bulkConflict(t, state, w.view, remote, "exists", "old")
	state.showTransferConflict(w.view)
	if !w.transferConflictOpen() || !w.listBlocked() || w.queueDetailsAllowed() {
		t.Fatal("an open conflict prompt did not block background file-window interactions")
	}
	w.click(hitTransferConflictCancel)
	if w.transferConflictOpen() || w.listBlocked() || !w.queueDetailsAllowed() {
		t.Fatal("canceling the conflict prompt left the file window blocked")
	}
}

func TestNonRetryableConflictSettlesTerminalUploadAndCountsAsFailed(t *testing.T) {
	_, options := newWorld(t)
	options.Invalidate = func() {}
	state := newTestState(t, options)
	view, _ := bulkTestView(t, state)
	snapshot := sftpclient.TransferSnapshot{
		ID:        99,
		Operation: sftpclient.OperationUpload,
		State:     sftpclient.TransferFailed,
		Retryable: false,
		Source:    "/tmp/exists.txt",
		Error:     sftpclient.ErrDestinationExists.Error(),
	}
	view.transfers = []sftpclient.TransferSnapshot{snapshot}
	state.terminalUploads = []terminalUploadBatch{{view: view, total: 1, pending: map[uint64]struct{}{snapshot.ID: {}}}}
	state.settleTerminalUploads(view)
	if len(state.terminalUploads) != 0 {
		t.Fatal("a non-retryable conflict hung terminal upload settlement")
	}

	// An orphaned conflict entry (no live owner) must classify consistently as Failed.
	orphan := transferEntry{
		id: 1,
		snapshot: sftpclient.TransferSnapshot{
			ID:        100,
			Operation: sftpclient.OperationUpload,
			State:     sftpclient.TransferFailed,
			Retryable: true,
			Source:    "/tmp/orphan.txt",
			Error:     sftpclient.ErrDestinationExists.Error(),
		},
	}
	state.describeTransfer(&orphan)
	state.transfers.entries = []transferEntry{orphan}
	state.describeTransferSummary()
	if state.transfers.pending != 0 || state.transfers.failed != 1 ||
		len(state.transfers.groups[transferFilterPending].members) != 0 ||
		len(state.transfers.groups[transferFilterFailed].members) != 1 {
		t.Fatalf("orphaned conflict misclassified: pending=%d failed=%d pendingGroup=%d failedGroup=%d",
			state.transfers.pending, state.transfers.failed,
			len(state.transfers.groups[transferFilterPending].members),
			len(state.transfers.groups[transferFilterFailed].members))
	}
}
