package workbench

import "testing"

func TestCopiedHostTransferChoiceSurvivesEditingAndRestart(t *testing.T) {
	for _, mode := range []transferMode{transferSFTP, transferSCP} {
		t.Run(Host{Transfer: mode}.savedTransfer(), func(t *testing.T) {
			options := testOptions(t)
			state := newTestState(t, options)
			original := state.hosts[0]
			state.openCopyHostID(original.ID)
			label := state.hostTransferChoiceLabel(mode)
			clickHostChoice(t, state, label)
			state.Click(hitDialogSubmit)
			if state.modal != modalNone {
				t.Fatalf("copy not saved: %q", state.dialogError)
			}
			copied := state.hosts[len(state.hosts)-1]
			if copied.Transfer != mode || copied.CatalogID == original.CatalogID {
				t.Fatal("copy did not retain its own transfer choice")
			}
			state.openEditHostID(copied.ID)
			if state.editor.transfer != mode {
				t.Fatalf("reopened transfer = %v, want %v", state.editor.transfer, mode)
			}
			other := transferSCP
			if mode == transferSCP {
				other = transferSFTP
			}
			clickHostChoice(t, state, state.hostTransferChoiceLabel(other))
			state.Click(hitDialogSubmit)
			if state.modal != modalNone {
				t.Fatalf("edit not saved: %q", state.dialogError)
			}
			restarted := newTestState(t, options)
			restored, ok := restarted.hostByID(copied.ID)
			if !ok || restored.Transfer != other {
				t.Fatalf("restored transfer = %v, want %v", restored.Transfer, other)
			}
			if got := restarted.hostProtocolTagLabel(restored); got != state.hostTransferChoiceLabel(other) {
				t.Errorf("protocol tag = %q, want %q", got, state.hostTransferChoiceLabel(other))
			}
			unchanged, ok := restarted.hostByID(original.ID)
			if !ok || unchanged.Transfer != original.Transfer {
				t.Fatal("editing the copy changed the original transfer choice")
			}
		})
	}
}
