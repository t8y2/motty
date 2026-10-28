package workbench

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestSFTPWindowFooterKeepsFeedbackAcrossListings(t *testing.T) {
	state, w, _, host, localDir := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	shows := func(value string) bool {
		for _, text := range view.Snapshot().Texts {
			if text.Value == value {
				return true
			}
		}
		return false
	}
	paint := func() {
		w.window.Invalidate()
		view.Driver().Pump()
	}

	// Idle, the footer offers the drag hint.
	paint()
	if !shows(state.messages.SFTPWindow.DragHint) {
		t.Fatal("an idle window does not show the drag hint")
	}

	// A listing count must not hide what the operation just reported.
	delivered := fmt.Sprintf(state.messages.SFTPDownloadedFmt, "11 B", filepath.Join(localDir, "notes-out.txt"))
	w.view.status = delivered
	w.view.setItemCountStatus(state.messages.SFTPItemsFmt, 3)
	paint()
	if !shows(delivered) {
		t.Fatal("a listing count replaced the operation feedback")
	}

	// A listing that had nothing else to say retires its own message.
	w.view.startListingStatus(state.messages.SFTPLoading)
	w.view.setItemCountStatus(state.messages.SFTPItemsFmt, len(w.view.rows))
	paint()
	if shows(state.messages.SFTPLoading) {
		t.Fatal("a completed listing kept its loading message")
	}
	if !shows(state.messages.SFTPWindow.DragHint) {
		t.Fatal("the idle drag hint did not come back")
	}
}

func TestSFTPWindowCountsApproximateListingsAsSuch(t *testing.T) {
	state, w, _, host, _ := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	shows := func(value string) bool {
		for _, text := range view.Snapshot().Texts {
			if text.Value == value {
				return true
			}
		}
		return false
	}
	w.view.approximate = true
	w.window.Invalidate()
	view.Driver().Pump()

	rows := len(w.folder(false).rows)
	if want := fmt.Sprintf(state.messages.SCPItemsFmt, rows); !shows(want) {
		t.Fatalf("an approximate listing does not say so: want %q", want)
	}
	if exact := fmt.Sprintf(state.messages.SFTPItemsFmt, rows); exact != fmt.Sprintf(state.messages.SCPItemsFmt, rows) && shows(exact) {
		t.Fatalf("an approximate listing reported an exact count: %q", exact)
	}
}
