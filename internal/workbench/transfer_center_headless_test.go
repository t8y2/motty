//go:build darwin || linux || windows

package workbench

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
)

func TestTransferCenterPopupRoutesClicksAndFitsSmallWindows(t *testing.T) {
	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 900, Height: 600}} {
		t.Run(fmt.Sprintf("%dx%d", int(size.Width), int(size.Height)), func(t *testing.T) {
			state := transferPreviewState(t)
			extendTransferPreview(state, sftpclient.TransferPaused, 20)
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
			click := func(id gfx.ID) {
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
			// The list clips its overflow inside the popover.
			list := state.transfers.body.list.Rect()
			if list.Y+list.Height > box.Y+box.Height {
				t.Fatalf("transfer list %+v spills out of the popover %+v", list, box)
			}
			if list.Height >= float32(len(state.transfers.rows))*transferRowHeight {
				t.Fatalf("list viewport %v is not shorter than its %d rows", list.Height, len(state.transfers.rows))
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
			if err := view.Driver().Key(gfx.KeyEnd, 0); err != nil {
				t.Fatal(err)
			}
			if state.transfers.scroll != state.transfers.geometry.MaxOffsetY {
				t.Fatal("End did not reveal the bottom of history")
			}
			// The List applies the offset at its next declaration, which it asks for.
			if err := view.Wait(16 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			reveal, ok := findSnapshotHit(view.Snapshot(), hitTransfersReveal.At(1))
			if !ok {
				t.Fatal("completed download has no reveal control")
			}
			if reveal.Role != gfx.RoleButton || reveal.Label != state.messages.Transfers.ShowInFolder ||
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
			if err := view.Driver().Key(gfx.KeyHome, 0); err != nil {
				t.Fatal(err)
			}
			if state.transfers.scroll != 0 {
				t.Fatal("Home did not return to the first transfer")
			}
			click(hitTransfersFilter.At(transferFilterEnded))
			if state.transfers.filter != transferFilterEnded || state.transfers.count != 1 {
				t.Fatalf("finished filter=%d count=%d", state.transfers.filter, state.transfers.count)
			}
			if err := view.Wait(16 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			before := len(state.transfers.entries)
			click(hitTransfersRemove.At(1))
			if len(state.transfers.entries) != before-1 {
				t.Fatal("remove click did not remove a history entry")
			}
			if err := view.Driver().Key(gfx.KeyEscape, 0); err != nil {
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

func extendTransferPreview(state *State, phase sftpclient.TransferState, count int) {
	start := state.transfers.entries[0].snapshot.QueuedAt
	for range count {
		state.transfers.nextID++
		id := state.transfers.nextID
		at := start.Add(time.Duration(id) * time.Second)
		entry := transferEntry{id: id, owner: &sftpView{}, snapshot: sftpclient.TransferSnapshot{
			ID: uint64(id), State: phase, Operation: sftpclient.OperationDownload, Source: fmt.Sprintf("/tmp/task-%d.log", id),
			QueuedAt: at, StartedAt: at,
		}}
		state.describeTransfer(&entry)
		state.transfers.entries = append(state.transfers.entries, entry)
	}
	state.describeTransferSummary()
}

func TestTransferCenterClipsRowActionsToList(t *testing.T) {
	state := transferPreviewState(t)
	// Give the list more active rows than it can show.
	extendTransferPreview(state, sftpclient.TransferRunning, 20)
	for index := range state.transfers.entries {
		entry := &state.transfers.entries[index]
		entry.snapshot.State = sftpclient.TransferRunning
		entry.owner = &sftpView{}
		state.describeTransfer(entry)
	}
	state.describeTransferSummary()

	view, err := headless.Open(app.Config{
		Title: "Motty", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Wait(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	last := hitTransfersCancel.At(1)
	if hit, ok := findSnapshotHit(view.Snapshot(), last); ok {
		t.Fatalf("offscreen Cancel button has a hit region: %+v", hit.Bounds)
	}
	if err := view.Driver().Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(16 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	hit, ok := findSnapshotHit(view.Snapshot(), last)
	list := state.transfers.list
	if !ok || hit.Bounds.Y < list.Y || hit.Bounds.Y+hit.Bounds.Height > list.Y+list.Height {
		t.Fatalf("Cancel button did not scroll into the list viewport: %+v, list %+v", hit.Bounds, list)
	}
}

// An interrupted transfer is visible as such, explains that it continues when
// the host's files come back, and offers no action that pretends it is a
// failure or a user cancellation (ADR 0028).
func TestTransferCenterShowsAnInterruptedTransfer(t *testing.T) {
	state := transferPreviewState(t)
	entry := &state.transfers.entries[1]
	entry.snapshot.State = sftpclient.TransferInterrupted
	entry.snapshot.Error = sftpclient.ErrInterrupted.Error()
	entry.snapshot.Retryable = false
	entry.owner = nil
	state.describeTransfer(entry)
	state.describeTransferSummary()

	view, err := headless.Open(app.Config{
		Title: "Motty", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Wait(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	status, ok := findSnapshotHit(view.Snapshot(), hitTransfersStatus.At(entry.id))
	if !ok || status.Label != state.messages.Transfers.Interrupted || status.Hint != state.messages.Transfers.InterruptedHint {
		t.Fatalf("interrupted status = %+v, want %q with its hint", status, state.messages.Transfers.Interrupted)
	}
	for _, id := range []gfx.ID{
		hitTransfersRetry.At(entry.id), hitTransfersReplace.At(entry.id),
		hitTransfersResume.At(entry.id), hitTransfersSkip.At(entry.id),
	} {
		if _, ok := findSnapshotHit(view.Snapshot(), id); ok {
			t.Fatalf("interrupted transfer offers %v", id)
		}
	}
	if _, ok := findSnapshotHit(view.Snapshot(), hitTransfersRemove.At(entry.id)); !ok {
		t.Fatal("interrupted transfer cannot be removed")
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
	if err := view.Driver().Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	// The List applies the offset at its next declaration, which it asks for.
	if err := view.Wait(16 * time.Millisecond); err != nil {
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
			click := func(id gfx.ID) {
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
			if err := view.Driver().Key(gfx.KeyDown, 0); err != nil {
				t.Fatal(err)
			}
			if err := view.Driver().Key(gfx.KeyEnter, 0); err != nil {
				t.Fatal(err)
			}
			if state.transfers.count != 1 {
				t.Fatal("keyboard host selection did not filter to one host")
			}
			click(bulkAction("host"))
			if err := view.Driver().Key(gfx.KeyEscape, 0); err != nil {
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
			for _, id := range []gfx.ID{hitTransfersFilter.At(3), bulkAction("host"), hitTransfersDirection, hitTransfersSearch, bulkAction("replace-visible"), bulkAction("resume-visible"), hitTransfersCancelAll, hitTransfersClear} {
				hit, ok := findSnapshotHit(view.Snapshot(), id)
				box := state.transfers.surface
				if !ok || hit.Bounds.X < box.X || hit.Bounds.X+hit.Bounds.Width > box.X+box.Width || hit.Bounds.Y < box.Y || hit.Bounds.Y+hit.Bounds.Height > box.Y+box.Height {
					t.Fatalf("native transfer control %v does not fit narrow popover", id)
				}
			}
			if _, ok := findSnapshotHit(view.Snapshot(), bulkAction("retry-failed")); ok {
				t.Fatal("batch Retry is shown without eligible ordinary failures")
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
			if err := view.Driver().Key(gfx.KeyEscape, 0); err != nil {
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
			if err := view.Driver().Key(gfx.KeyEnter, 0); err != nil {
				t.Fatal(err)
			}
			if contents, _ := os.ReadFile(target); string(contents) != "mine" {
				t.Fatal("initial confirmation focus must be Cancel")
			}
			click(bulkAction("replace-visible"))
			if err := view.Driver().Key(gfx.KeyTab, 0); err != nil {
				t.Fatal(err)
			}
			if err := view.Driver().Key(gfx.KeyEnter, 0); err != nil {
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

func TestTransferCenterPreviewAndFilters(t *testing.T) {
	state := transferDesignPreview(t)
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Motty", Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	wait := func(duration time.Duration) {
		t.Helper()
		if err := view.Wait(duration); err != nil {
			t.Fatal(err)
		}
	}
	hit := func(id gfx.ID) gfx.Hit {
		t.Helper()
		out, ok := findSnapshotHit(view.Snapshot(), id)
		if !ok || out.Block || out.Bounds.Width <= 0 || out.Bounds.Height <= 0 {
			t.Fatalf("missing %v", id)
		}
		return out
	}
	click := func(id gfx.ID) {
		t.Helper()
		box := hit(id).Bounds
		if err := view.Click(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
			t.Fatal(err)
		}
	}
	wait(300 * time.Millisecond)
	for group, count := range map[int]string{
		transferFilterAll: "9", transferFilterRunning: "3", transferFilterWaiting: "2",
		transferFilterEnded: "3", transferFilterFailed: "1",
	} {
		if tab := hit(hitTransfersFilter.At(group)); tab.Role != gfx.RoleTab || tab.Value != count {
			t.Fatalf("tab %d: %+v; want count %s", group, tab, count)
		}
		if group != transferFilterAll {
			hit(hitTransfersGroup.At(group))
		}
	}
	writeTransferPreview(t, view, state.transfers.surface, "transfer-center.png")

	for _, preview := range []struct {
		filter, count int
		name          string
	}{
		{transferFilterRunning, 3, "transfer-center-running.png"},
		{transferFilterEnded, 3, "transfer-center-ended.png"},
		{transferFilterFailed, 1, "transfer-center-failed.png"},
	} {
		click(hitTransfersFilter.At(preview.filter))
		wait(16 * time.Millisecond)
		if state.transfers.filter != preview.filter || state.transfers.count != preview.count {
			t.Fatalf("preview filter %d: got filter=%d count=%d", preview.filter, state.transfers.filter, state.transfers.count)
		}
		writeTransferPreview(t, view, state.transfers.surface, preview.name)
	}
	click(hitTransfersFilter.At(transferFilterAll))
	wait(16 * time.Millisecond)

	const redis = 9
	anchor := hit(hitTransfersDetails.At(redis)).Bounds
	if err := view.Move(anchor.X+min(154, anchor.Width/2), anchor.Y+anchor.Height/2); err != nil {
		t.Fatal(err)
	}
	wait(transferDetailDelay + 220*time.Millisecond)
	card := hit(hitTransfersDetailCard)
	if card.Bounds.X < 0 || card.Bounds.Y < 0 || card.Bounds.X+card.Bounds.Width > 1400 || card.Bounds.Y+card.Bounds.Height > 900 {
		t.Fatalf("details outside viewport: %+v", card.Bounds)
	}
	var detailText strings.Builder
	for _, label := range view.Snapshot().Texts {
		if label.Layer == hoverCardLayer {
			detailText.WriteString(label.Value)
			detailText.WriteByte('\n')
		}
	}
	for _, value := range []string{"redis-7.0.14.tar.gz", "root@192.0.2.40:22", "/srv/packages/redis-7.0.14.tar.gz", "开始时间"} {
		if !strings.Contains(detailText.String(), value) {
			t.Fatalf("details missing %q: %s", value, detailText.String())
		}
	}
	writeTransferPreview(t, view, state.transfers.surface, "transfer-center-hover.png")

	click(hitTransfersDirection)
	click(hitTransfersDirectionItems.At(int(transferDownload)))
	if state.transfers.count != 3 {
		t.Fatalf("download filter matched %d entries", state.transfers.count)
	}
	if _, ok := findSnapshotHit(view.Snapshot(), hitTransfersDetailCard); ok && state.transfers.details.showing != 0 {
		t.Fatal("the dropdown left the old hover card open")
	}
	click(hitTransfersGroup.At(transferFilterEnded))
	wait(16 * time.Millisecond)
	if _, ok := findSnapshotHit(view.Snapshot(), hitTransfersDetails.At(2)); ok || state.transfers.count != 3 {
		t.Fatal("collapsing the group must hide its rows without changing counts")
	}
	click(hitTransfersGroup.At(transferFilterEnded))
	click(hitTransfersSearch)
	if err := view.Type("DOCKER"); err != nil {
		t.Fatal(err)
	}
	wait(16 * time.Millisecond)
	if state.transfers.count != 1 || hit(hitTransfersDetails.At(2)).Label != "docker-compose.yml" {
		t.Fatalf("search did not combine with the direction filter: %+v", state.transfers.rows)
	}
	if err := view.Driver().Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if !state.transfers.open || state.transfers.count != 1 {
		t.Fatal("Enter in the filename search closed the center or changed its results")
	}
	click(hitTransfersDetails.At(2))
	hit(hitTransfersDetailCard)
	if err := view.Driver().Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if !state.transfers.open || state.transfers.details.pinned != 0 {
		t.Fatal("Escape should close a pinned preview before the transfer center")
	}
}

func transferDesignPreview(t *testing.T) *State {
	t.Helper()
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.setLanguage(i18n.Chinese)
	state.chooseAppearanceMode(appearancePinnedDark)
	at := time.Now().Truncate(time.Second).Add(-90 * time.Second)
	host := newTransferHost("软件仓库", "root@192.0.2.40:22")
	type sample struct {
		file             string
		state            sftpclient.TransferState
		done, total, bps int64
	}
	const mb = 1 << 20
	for index, item := range []sample{
		{"nginx-1.26.2.tar.gz", sftpclient.TransferCompleted, 12 * mb, 12 * mb, 0},
		{"docker-compose.yml", sftpclient.TransferCompleted, 4968, 4968, 0},
		{"transfer-report.log", sftpclient.TransferCompleted, 5 * mb / 2, 5 * mb / 2, 0},
		{"httpd-2.4.58.tar.gz", sftpclient.TransferFailed, 0, 24 * mb, 0},
		{"zlib-1.3.tar.gz", sftpclient.TransferPaused, 0, 8 * mb, 0},
		{"pcre2-10.37.tar.gz", sftpclient.TransferQueued, 0, 18 * mb, 0},
		{"openssl-1.1.1w.tar.gz", sftpclient.TransferRunning, 79 * mb, 100 * mb, 7 * mb / 2},
		{"perl-5.38.0.tar.gz", sftpclient.TransferRunning, 24 * mb, 100 * mb, mb / 2},
		{"redis-7.0.14.tar.gz", sftpclient.TransferRunning, 36 * mb, 52 * mb, 6 * mb / 5},
	} {
		local, remote := filepath.Join(`D:\发布资料\公共组件`, item.file), "/srv/packages/"+item.file
		queued := at.Add(time.Duration(index) * time.Second)
		job := sftpclient.TransferSnapshot{
			ID: uint64(index + 1), Operation: sftpclient.OperationUpload, State: item.state, Source: local, Destination: remote,
			BytesDone: item.done, BytesTotal: item.total, BytesPerSecond: item.bps, QueuedAt: queued,
		}
		if item.state != sftpclient.TransferQueued {
			job.StartedAt = queued.Add(time.Second)
		}
		if item.state == sftpclient.TransferCompleted {
			job.Operation, job.Source, job.Destination = sftpclient.OperationDownload, remote, local
			job.FinishedAt = job.StartedAt.Add(time.Duration(4+index*3) * time.Second)
		}
		if item.state == sftpclient.TransferFailed {
			job.Error, job.Retryable = "连接超时", true
			job.FinishedAt = at.Add(30 * time.Second)
		}
		entry := transferEntry{id: index + 1, host: host, server: "软件仓库", address: "root@192.0.2.40:22", snapshot: job}
		if transferActive(job) || job.Retryable {
			entry.owner = &sftpView{}
		}
		state.describeTransfer(&entry)
		state.transfers.entries = append(state.transfers.entries, entry)
	}
	state.transfers.nextID = len(state.transfers.entries)
	state.transfers.open = true
	state.describeTransferSummary()
	return state
}

func TestTransferCenterEmptyPreview(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.setLanguage(i18n.Chinese)
	state.chooseAppearanceMode(appearancePinnedDark)
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Motty", Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	trigger, ok := findSnapshotHit(view.Snapshot(), hitTransferCenter)
	if !ok {
		t.Fatal("transfer center trigger missing")
	}
	if err := view.Click(trigger.Bounds.X+trigger.Bounds.Width/2, trigger.Bounds.Y+trigger.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(0, 0); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, label := range view.Snapshot().Texts {
		found = found || label.Value == state.messages.Transfers.Empty
	}
	if !found || state.transfers.hasEntries {
		t.Fatal("fresh transfer center did not show the empty state")
	}
	writeTransferPreview(t, view, state.transfers.surface, "transfer-center-empty.png")
}

// Save the panel's actual pixels without the unrelated workspace around it.
func writeTransferPreview(t *testing.T, view *headless.Session, panel gfx.Rect, name string) {
	t.Helper()
	dir := os.Getenv("MOTTY_TRANSFER_SCREENSHOTS")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	frame, err := view.Image()
	if err != nil {
		t.Fatal(err)
	}
	rect := image.Rect(int(panel.X), int(panel.Y), int(panel.X+panel.Width+0.5), int(panel.Y+panel.Height+0.5))
	file, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, frame.SubImage(rect)); err != nil {
		t.Fatal(err)
	}
}

func findSnapshotHit(snapshot app.Snapshot, id gfx.ID) (gfx.Hit, bool) {
	for _, hit := range snapshot.Hits {
		if hit.ID == id {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}
