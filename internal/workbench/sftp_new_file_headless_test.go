//go:build darwin || linux || windows

package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestSFTPNewFileHeadlessInteractionAndScreenshots(t *testing.T) {
	state, _, w, _, _ := openSFTPTabFixture(t)
	state.setLanguage(i18n.Chinese)

	var invalidator app.Invalidator
	config := app.Config{
		Title: "SFTP New File", Width: 1100, Height: 720, Tree: Tree(),
		Invalidator: &invalidator, Paint: state.Paint, OnPointer: state.Pointer, OnResize: state.Resize,
	}
	preview, err := headless.Open(config, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	if err := preview.Rest(); err != nil {
		t.Fatal(err)
	}

	shot := func(name string) {
		t.Helper()
		if dir := os.Getenv("MOTTY_SFTP_SCREENSHOT_DIR"); dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := preview.Screenshot(filepath.Join(dir, name)); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
				t.Fatal(err)
			}
		}
	}

	// 1. Right click in remote pane list to open context menu showing "新建文件"
	list := w.ui.list[1] // remote pane
	if err := preview.RightClick(list.X+100, list.Y+120); err != nil {
		t.Fatal(err)
	}
	if err := preview.Rest(); err != nil {
		t.Fatal(err)
	}

	// Screenshot 1: Context menu with "新建文件"
	shot("01-sftp-context-menu.png")

	// Find the "新建文件" menu item
	label := w.windowMenuRowLabel(sftpWindowMenuActions.At(sftpWindowActionIndex(hitSFTPNewFile)))
	if label == "" {
		label = state.messages.SFTPNewFile
	}
	menuHit, ok := snapshotMenuHit(preview, label)
	if !ok {
		t.Fatalf("could not find menu hit for %q", label)
	}

	// Click "新建文件"
	if err := preview.Click(menuHit.Bounds.X+menuHit.Bounds.Width/2, menuHit.Bounds.Y+menuHit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := preview.Rest(); err != nil {
		t.Fatal(err)
	}

	if w.view.dialog != sftpDialogCreateFile {
		t.Fatalf("dialog = %v, want %v", w.view.dialog, sftpDialogCreateFile)
	}

	w.view.dialogEditor.Insert("config.json")
	invalidator.Invalidate()
	if err := preview.Rest(); err != nil {
		t.Fatal(err)
	}

	// Screenshot 2: Create file dialog
	shot("02-sftp-new-file-dialog.png")

	// Click "创建" (hitSFTPDialogSubmit)
	var submitHitFound bool
	for _, hit := range preview.Snapshot().Hits {
		if hit.ID == hitSFTPDialogSubmit {
			submitHitFound = true
			if err := preview.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if !submitHitFound {
		w.click(hitSFTPDialogSubmit)
	}

	waitSFTPTab(t, state, w, func() bool {
		return !w.view.busy && len(w.view.rows) == 3
	})
	time.Sleep(50 * time.Millisecond)
	invalidator.Invalidate()
	if err := preview.Rest(); err != nil {
		t.Fatal(err)
	}

	// Screenshot 3: File list with newly created file
	shot("03-sftp-file-created.png")
}
