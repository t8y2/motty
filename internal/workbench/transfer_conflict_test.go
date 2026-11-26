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
