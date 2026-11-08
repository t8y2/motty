//go:build darwin || linux || windows

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
)

func TestTransferCenterPopupRoutesClicksAndFitsSmallWindows(t *testing.T) {
	for _, size := range []ui.Size{{Width: 1400, Height: 900}, {Width: 900, Height: 600}} {
		t.Run(fmt.Sprintf("%dx%d", int(size.Width), int(size.Height)), func(t *testing.T) {
			state := transferPreviewState(t)
			if size.Width == 900 {
				state.chooseAppearanceMode(appearancePinnedLight)
			}
			view, err := headless.Open(app.Config{
				Title: "Motty", Width: int(size.Width), Height: int(size.Height), Tree: Tree(),
				Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
			}, nil, headless.Options{Scale: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			click := func(id ui.ID) {
				t.Helper()
				for _, hit := range view.Snapshot().Hits {
					if hit.ID == id && !hit.Block {
						if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
							t.Fatal(err)
						}
						return
					}
				}
				t.Fatalf("missing control %v", id)
			}
			if err := view.Wait(300 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			box := state.transfers.surface
			if box.X < 0 || box.Y < 0 || box.X+box.Width > size.Width || box.Y+box.Height > size.Height {
				t.Fatalf("popover outside viewport: %+v", box)
			}
			if hit, ok := findSnapshotHit(view.Snapshot(), hitTransferCenter); !ok || hit.Bounds.X >= state.chrome.bar.assistant.Rect().X {
				t.Fatal("transfer button is not left of the assistant")
			}
			if dir := os.Getenv("MOTTY_TRANSFER_SCREENSHOTS"); dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("transfers-%dx%d.png", int(size.Width), int(size.Height)))); err != nil {
					t.Fatal(err)
				}
			}
			if err := view.Driver().Key(ui.KeyEnd, 0); err != nil {
				t.Fatal(err)
			}
			if state.transfers.scroll != state.transfers.geometry.MaxOffsetY {
				t.Fatal("End did not reveal the bottom of history")
			}
			reveal, ok := findSnapshotHit(view.Snapshot(), hitTransfersReveal.At(1))
			if !ok {
				t.Fatal("completed download has no reveal control")
			}
			if reveal.Role != ui.RoleButton || reveal.Label != state.messages.Transfers.ShowInFolder ||
				reveal.Hint != state.messages.Transfers.ShowInFolder || !reveal.Focusable {
				t.Fatalf("reveal control lacks button semantics: %+v", reveal)
			}
			remove, ok := findSnapshotHit(view.Snapshot(), hitTransfersRemove.At(1))
			if !ok || reveal.Bounds.X+reveal.Bounds.Width > remove.Bounds.X {
				t.Fatalf("reveal control %+v is not left of remove control %+v", reveal.Bounds, remove.Bounds)
			}
			for _, id := range []int{2, 3, 4} {
				if _, ok := findSnapshotHit(view.Snapshot(), hitTransfersReveal.At(id)); ok {
					t.Fatalf("non-completed-download record %d has a reveal control", id)
				}
			}
			if err := view.Driver().Key(ui.KeyHome, 0); err != nil {
				t.Fatal(err)
			}
			if state.transfers.scroll != 0 {
				t.Fatal("Home did not return to the first transfer")
			}
			click(hitTransfersFilter.At(2))
			if state.transfers.filter != 2 || state.transfers.count != 2 {
				t.Fatalf("finished filter=%d count=%d", state.transfers.filter, state.transfers.count)
			}
			click(hitTransfersRemove.At(2))
			if len(state.transfers.entries) != 3 {
				t.Fatal("remove click did not remove a history entry")
			}
			if err := view.Driver().Key(ui.KeyEscape, 0); err != nil {
				t.Fatal(err)
			}
			if state.transfers.open {
				t.Fatal("Escape did not close popover")
			}
			click(hitTransferCenter)
			if !state.transfers.open {
				t.Fatal("trigger did not reopen popover")
			}
			click(hitTransferCenter)
			if state.transfers.open {
				t.Fatal("trigger did not toggle popover closed")
			}
			click(hitTransferCenter)
			click(hitTransfersClose)
			if state.transfers.open {
				t.Fatal("close button did not close popover")
			}
			click(hitTransferCenter)
			if err := view.Click(size.Width-5, size.Height-5); err != nil {
				t.Fatal(err)
			}
			if state.transfers.open {
				t.Fatal("outside click did not close popover")
			}
		})
	}
}

func TestTransferCenterNativeRevealUsesVivaWithoutOpeningTheFile(t *testing.T) {
	state := transferPreviewState(t)
	entry := &state.transfers.entries[0]
	if entry.owner != nil {
		t.Fatal("fixture is not closed-connection history")
	}
	file := filepath.Join(t.TempDir(), "download.sh")
	if err := os.WriteFile(file, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	entry.snapshot.Destination = file
	view, err := headless.Open(app.Config{Title: "Motty", Width: 900, Height: 600,
		Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll},
		nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Wait(100 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := view.Driver().Key(ui.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	hit, ok := findSnapshotHit(view.Snapshot(), hitTransfersReveal.At(entry.id))
	if !ok || hit.Disabled {
		t.Fatalf("completed download cannot be revealed: %+v", hit)
	}
	click := func() {
		t.Helper()
		if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
	}
	click()
	waitFor(t, state, func() bool {
		got, ok := view.Driver().LastRevealedPath()
		return ok && got == file && !state.revealBusy
	})
	if _, ok := view.Driver().LastOpenedPath(); ok {
		t.Fatal("revealing the downloaded script opened it instead")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	click()
	waitFor(t, state, func() bool { return state.status == state.messages.Transfers.ShowInFolderMissing })
}

func TestTransferCenterNativeBulkRecoveryAndHostKeyboard(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
		t.Run(fmt.Sprint(language), func(t *testing.T) {
			_, options := newWorld(t)
			state := newTestState(t, options)
			state.setLanguage(language)
			first, remote := bulkTestView(t, state)
			second, other := bulkTestView(t, state)
			_, target := bulkConflict(t, state, first, remote, "first", "mine")
			bulkConflict(t, state, second, other, "other", "keep")
			state.transfers.open = true
			view, err := headless.Open(app.Config{Title: "Motty", Width: 560, Height: 640, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll}, nil, headless.Options{Scale: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			click := func(id ui.ID) {
				t.Helper()
				hit, ok := findSnapshotHit(view.Snapshot(), id)
				if !ok || hit.Block {
					t.Fatalf("missing native control %v", id)
				}
				if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
			}
			if err := view.Wait(250 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			click(hitTransfersFilter.At(3))
			click(bulkAction("host"))
			if _, ok := findSnapshotHit(view.Snapshot(), bulkHost(2)); !ok {
				t.Fatal("host dropdown does not distinguish duplicate connection names")
			}
			if err := view.Driver().Key(ui.KeyDown, 0); err != nil {
				t.Fatal(err)
			}
			if err := view.Driver().Key(ui.KeyEnter, 0); err != nil {
				t.Fatal(err)
			}
			if state.transfers.count != 1 {
				t.Fatal("keyboard host selection did not filter to one host")
			}
			click(bulkAction("host"))
			if err := view.Driver().Key(ui.KeyEscape, 0); err != nil {
				t.Fatal(err)
			}
			if !state.transfers.open {
				t.Fatal("Escape in the host menu closed the transfer center")
			}
			click(bulkAction("host"))
			click(bulkHost(1))
			if state.transfers.count != 1 {
				t.Fatalf("host selection shows %d tasks", state.transfers.count)
			}
			for _, id := range []ui.ID{hitTransfersFilter.At(3), bulkAction("host"), bulkAction("retry-failed"), bulkAction("replace-visible"), bulkAction("resume-visible"), hitTransfersCancelAll, hitTransfersClear} {
				hit, ok := findSnapshotHit(view.Snapshot(), id)
				box := state.transfers.surface
				if !ok || hit.Bounds.X < box.X || hit.Bounds.X+hit.Bounds.Width > box.X+box.Width || hit.Bounds.Y < box.Y || hit.Bounds.Y+hit.Bounds.Height > box.Y+box.Height {
					t.Fatalf("native transfer control %v does not fit narrow popover", id)
				}
			}
			if hit, ok := findSnapshotHit(view.Snapshot(), bulkAction("retry-failed")); !ok || !hit.Disabled {
				t.Fatal("no eligible ordinary failures must disable batch Retry")
			}
			if hit, ok := findSnapshotHit(view.Snapshot(), bulkAction("scope-label")); !ok || hit.Label == "" || hit.Hint == "" {
				t.Fatal("filtered scope has no full semantic label/hint")
			}
			click(bulkAction("replace-visible"))
			if _, ok := findSnapshotHit(view.Snapshot(), bulkAction("bulk-confirm")); !ok {
				t.Fatal("replace has no explicit native confirmation")
			}
			if hit, ok := findSnapshotHit(view.Snapshot(), bulkAction("bulk-description")); !ok || hit.Label == "" || hit.Hint == "" {
				t.Fatal("captured count/scope is not exposed semantically")
			}
			if err := view.Driver().Key(ui.KeyEscape, 0); err != nil {
				t.Fatal(err)
			}
			if !state.transfers.open {
				t.Fatal("Escape should cancel recovery before closing center")
			}
			if contents, _ := os.ReadFile(target); string(contents) != "mine" {
				t.Fatal("Escape confirmed a replacement")
			}
			if dir := os.Getenv("MOTTY_TRANSFER_SCREENSHOTS"); dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("recovery-%v-560.png", language))); err != nil {
					t.Fatal(err)
				}
			}
			click(bulkAction("replace-visible"))
			if dir := os.Getenv("MOTTY_TRANSFER_SCREENSHOTS"); dir != "" {
				if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("confirmation-%v-560.png", language))); err != nil {
					t.Fatal(err)
				}
			}
			click(bulkAction("bulk-cancel"))
			if _, ok := findSnapshotHit(view.Snapshot(), hitTransfersList); !ok {
				t.Fatal("Cancel did not restore transfer list")
			}
			click(bulkAction("replace-visible"))
			if err := view.Driver().Key(ui.KeyEnter, 0); err != nil {
				t.Fatal(err)
			}
			if contents, _ := os.ReadFile(target); string(contents) != "mine" {
				t.Fatal("initial confirmation focus must be Cancel")
			}
			click(bulkAction("replace-visible"))
			if err := view.Driver().Key(ui.KeyTab, 0); err != nil {
				t.Fatal(err)
			}
			if err := view.Driver().Key(ui.KeyEnter, 0); err != nil {
				t.Fatal(err)
			}
			bulkAwait(t, state, first, 1, sftpclient.TransferCompleted)
			bulkAwait(t, state, second, 1, sftpclient.TransferFailed)
		})
	}
}

func TestTransferCenterHostMenuRevealsSelectedHost(t *testing.T) {
	state := transferPreviewState(t)
	state.transfers.entries = nil
	for index := range 15 {
		state.transfers.entries = append(state.transfers.entries, transferEntry{id: index + 1, host: newTransferHost(fmt.Sprintf("host-%d", index), "user@host:22"), snapshot: sftpclient.TransferSnapshot{State: sftpclient.TransferFailed}})
	}
	state.rebuildTransferCenter()
	state.Click(bulkHost(15))
	state.Click(bulkAction("host"))
	view, err := headless.Open(app.Config{Title: "Motty", Width: 560, Height: 640, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Wait(200 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	hit, ok := findSnapshotHit(view.Snapshot(), bulkHost(15))
	if !ok || hit.Bounds.Y < state.popups.surface.Y || hit.Bounds.Y+hit.Bounds.Height > state.popups.surface.Y+state.popups.surface.Height {
		t.Fatal("selected host opens outside the visible dropdown scroll range")
	}
}

func findSnapshotHit(snapshot app.Snapshot, id ui.ID) (ui.Hit, bool) {
	for _, hit := range snapshot.Hits {
		if hit.ID == id {
			return hit, true
		}
	}
	return ui.Hit{}, false
}
