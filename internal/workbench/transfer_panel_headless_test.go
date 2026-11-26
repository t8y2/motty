//go:build darwin || linux || windows

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
)

func TestTransferPanelFitsItsRowsAndCapsLongHistory(t *testing.T) {
	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 560, Height: 640}} {
		var shortHeight float32
		for _, count := range []int{1, 30} {
			t.Run(fmt.Sprintf("%dx%d/%d", int(size.Width), int(size.Height), count), func(t *testing.T) {
				state := transferPreviewState(t)
				state.transfers.entries = nil
				for index := range count {
					entry := transferEntry{id: index + 1, snapshot: sftpclient.TransferSnapshot{
						Operation: sftpclient.OperationUpload, State: sftpclient.TransferCompleted,
						Source: fmt.Sprintf("release-%d.zip", index), BytesDone: 1024, BytesTotal: 1024,
					}}
					state.describeTransfer(&entry)
					state.transfers.entries = append(state.transfers.entries, entry)
				}
				state.describeTransferSummary()
				view, err := headless.Open(state.WindowConfig(app.Config{Width: int(size.Width), Height: int(size.Height), Tree: Tree(), Paint: state.Paint,
					OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll}), nil, headless.Options{Scale: 1})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				center := &state.transfers
				if center.surface.Width > 760 || center.surface.Height > 580 || center.surface.X < 0 || center.surface.Y < 0 || center.surface.X+center.surface.Width > size.Width || center.surface.Y+center.surface.Height > size.Height {
					t.Fatalf("panel does not fit: %+v", center.surface)
				}
				scroll := center.listBox.ScrollState()
				if count == 1 {
					shortHeight = center.surface.Height
					if scroll.Max() != 0 {
						t.Fatal("a single task unnecessarily scrolls")
					}
				} else if center.surface.Height <= shortHeight || scroll.Max() <= 0 {
					t.Fatal("a long history did not grow and scroll within the cap")
				}
				writeTransferPreview(t, view, center.surface, fmt.Sprintf("compact-history-%d-%d.png", int(size.Width), count))
			})
		}
	}
}

func TestTransferConflictQuickDialogUploadsAcrossSurfaces(t *testing.T) {
	for _, surface := range []string{"terminal", "tab", "window"} {
		for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
			t.Run(fmt.Sprintf("%s/%s", surface, language), func(t *testing.T) {
				var state *State
				var session *Session
				var browser *sftpView
				var window *sftpWindow
				var remote *workbenchSFTPRemote
				if surface == "terminal" {
					state, session, _, remote = openTestSFTP(t)
					browser = session.files
				} else {
					state, _, window, remote, _ = openSFTPTabFixture(t)
					browser = window.view
					if surface == "window" {
						state.activeSFTPTab = 0
					}
				}
				state.setLanguage(language)
				if language == i18n.Chinese {
					state.chooseAppearanceMode(appearancePinnedDark)
				} else {
					state.chooseAppearanceMode(appearancePinnedLight)
				}
				state.sftpTransferAutoOpenOff = true
				source := filepath.Join(t.TempDir(), "notes.txt")
				if err := os.WriteFile(source, []byte("replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
				state.queueSFTPUploadTo(browser, source, false, "/home/test")
				if window == nil {
					waitSFTP(t, state, session, func(view *sftpView) bool { return len(view.conflictPrompt.targets) == 1 })
				} else {
					waitSFTPTab(t, state, window, func() bool { return len(browser.conflictPrompt.targets) == 1 })
				}
				config := state.WindowConfig(app.Config{Width: 560, Height: 640, Tree: Tree(), Paint: state.Paint,
					OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll})
				if surface == "window" {
					config = window.config()
				}
				view, err := headless.Open(config, nil, headless.Options{Scale: 1})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				lastRight := float32(0)
				for _, id := range []gfx.ID{hitTransferConflictCancel, hitTransferConflictSkipAll, hitTransferConflictReplaceAll, hitTransferConflictReplace} {
					hit, ok := findSnapshotHit(view.Snapshot(), id)
					if !ok || hit.Bounds.Width <= 0 || hit.Bounds.X < lastRight || hit.Bounds.X+hit.Bounds.Width > float32(config.Width) {
						t.Fatalf("dialog action is missing or overlaps: %v, %+v", id, hit.Bounds)
					}
					lastRight = hit.Bounds.X + hit.Bounds.Width
				}
				if state.transfers.open {
					t.Fatal("a conflict opened the full transfer center")
				}
				if dir := os.Getenv("MOTTY_TRANSFER_SCREENSHOTS"); dir != "" {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("quick-conflict-%s-%s.png", surface, language))); err != nil {
						t.Fatal(err)
					}
				}
				if err := view.Driver().Key(gfx.KeyEscape, 0); err != nil {
					t.Fatal(err)
				}
				if len(browser.conflictPrompt.targets) != 0 {
					t.Fatal("Escape did not dismiss the prompt")
				}
				remote.mu.Lock()
				before := string(remote.files["/home/test/notes.txt"])
				remote.mu.Unlock()
				if before != "hello world" {
					t.Fatal("Escape overwrote the file")
				}
				state.showTransferConflict(browser)
				if err := view.Driver().Paint(); err != nil {
					t.Fatal(err)
				}
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				dualClickID(t, view, hitTransferConflictReplace)
				bulkAwait(t, state, browser, 1, sftpclient.TransferCompleted)
				remote.mu.Lock()
				contents := string(remote.files["/home/test/notes.txt"])
				remote.mu.Unlock()
				if contents != "replacement" {
					t.Fatalf("one-click overwrite = %q", contents)
				}
			})
		}
	}
}

func TestTransferConflictPromptWaitsForExistingBatchConfirmation(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	browser, remote := bulkTestView(t, state)
	bulkConflict(t, state, browser, remote, "first", "old")
	state.transfers.open = true
	state.clickTransferBulk(hitTransfersReplaceVisible)
	if len(state.transfers.batch.targets) != 1 {
		t.Fatal("the existing batch confirmation did not open")
	}
	bulkConflict(t, state, browser, remote, "later", "old")
	state.showTransferConflict(browser)
	view, err := headless.Open(state.WindowConfig(app.Config{
		Width: 1100, Height: 760, Tree: Tree(), Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if _, shown := findSnapshotHit(view.Snapshot(), hitTransferConflictReplaceAll); shown {
		t.Fatal("a new conflict prompt interrupted the existing batch confirmation")
	}
	dualClickID(t, view, hitTransfersBulkCancel)
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if _, shown := findSnapshotHit(view.Snapshot(), hitTransferConflictReplaceAll); !shown {
		t.Fatal("the waiting conflict prompt did not appear after the batch was dismissed")
	}
}
