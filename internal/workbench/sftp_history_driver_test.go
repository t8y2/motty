//go:build darwin || linux || windows

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"motty/internal/i18n"
)

func historyDriverClick(t *testing.T, driver *app.Driver, id gfx.ID) {
	t.Helper()
	hit := hitOf(t, driver, id)
	if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
}

func TestSFTPHistoryNativeListAltAndTextPassThrough(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	view := session.files
	state.navigateSFTPDirectory(view, "/home/test/docs")
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test/docs" })
	driver := sftpPathFocusDriver(t, state)
	historyDriverClick(t, driver, contextID(hitSFTPEntryList.At(0)))
	if err := driver.Key(gfx.KeyLeft, gfx.ModifierAlt); err != nil {
		t.Fatal(err)
	}
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test" })
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	for _, filter := range []bool{false, true} {
		id := hitSFTPPath
		if filter {
			view.filterOpen = true
			id = hitSFTPFilter
			*view.filterInputEditor() = *text.NewEditor("alpha beta")
		} else {
			state.sftpPathInput = true
			*view.pathInput() = *text.NewEditor("alpha beta")
		}
		driver.Resize(1401, 900)
		driver.Resize(1400, 900)
		historyDriverClick(t, driver, contextID(id))
		if err := driver.SelectText(10, 10); err != nil {
			t.Fatal(err)
		}
		before := append([]string(nil), view.history.recent...)
		if err := driver.Key(gfx.KeyLeft, gfx.ModifierAlt); err != nil {
			t.Fatal(err)
		}
		if view.path != "/home/test" || !reflect.DeepEqual(before, view.history.recent) {
			t.Fatal("Option word movement navigated history")
		}
		if editor := state.EditorFor(contextID(id)); editor == nil || editor.Caret() >= 10 {
			t.Fatalf("Option word movement did not reach native input: %v", editor)
		}
	}
	for _, target := range []gfx.ID{state.activeTerminalFocusHit(), contextID(hitSFTPEditor)} {
		if result, ok := state.sftpKey(gfx.KeyEvent{Target: target, Key: gfx.KeyLeft, Modifiers: gfx.ModifierAlt}); ok || result.Handled {
			t.Fatal("history stole terminal/editor key")
		}
	}
	historyDriverClick(t, driver, state.activeTerminalFocusHit())
	shell := fake.shell(t)
	shell.mu.Lock()
	inputBefore := len(shell.input)
	shell.mu.Unlock()
	if err := driver.Key(gfx.KeyRight, gfx.ModifierAlt); err != nil {
		t.Fatal(err)
	}
	shell.mu.Lock()
	inputAfter := len(shell.input)
	shell.mu.Unlock()
	if view.path != "/home/test" || inputAfter <= inputBefore {
		t.Fatal("terminal Alt arrow did not stay with terminal")
	}
	state.Click(hitSFTPFilterClear)
	openSFTPTextEditor(t, state, session, "notes.txt")
	driver.Resize(1401, 900)
	historyDriverClick(t, driver, contextID(hitSFTPEditor))
	if err := driver.SelectText(0, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyRight, gfx.ModifierAlt); err != nil {
		t.Fatal(err)
	}
	if view.path != "/home/test" || view.editEditor.Caret() == 0 {
		t.Fatal("remote editor Alt word movement navigated history")
	}
}

func TestSFTPHistoryNativeHideReopenSessionLifetime(t *testing.T) {
	for _, transferred := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "transfer-kept"}[transferred], func(t *testing.T) {
			state, session, _, _ := openTestSFTP(t)
			view := session.files
			localA, localB := t.TempDir(), t.TempDir()
			state.navigateLocalDirectory(view, localA)
			state.navigateLocalDirectory(view, localB)
			state.navigateSFTPDirectory(view, "/home/test/docs")
			waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test/docs" })
			if transferred {
				view.worker.QueueDownload("/home/test/notes.txt", filepath.Join(t.TempDir(), "notes"), false, 0)
				waitTransfersDone(t, state, session, 1)
			}
			before := append([]string(nil), view.history.recent...)
			driver := sftpPathFocusDriver(t, state)
			historyDriverClick(t, driver, contextID(hitSFTPClose))
			if !transferred && session.files != nil {
				t.Fatal("idle worker retained for history")
			}
			if transferred && session.files != view {
				t.Fatal("transfer-owned browser lost")
			}
			historyDriverClick(t, driver, hitSFTPOpen)
			waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy })
			if len(session.files.history.entries) != 2 || !reflect.DeepEqual(before, session.files.history.recent) {
				t.Fatal("same-session pane hide lost browsing history")
			}
			state.toggleSFTPPane(session.files)
			if session.files.lfolder.path != localB || len(session.files.lfolder.history.entries) != 2 {
				t.Fatal("hide/reopen lost independent local path/history")
			}
			state.toggleSFTPPane(session.files)
			if !state.navigateSFTPHistory(session.files, false, -1) {
				t.Fatal("reopened history cannot go Back")
			}
			waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test" })
		})
	}
}

func TestSFTPHistoryMoreMenusExposeRecentFullPathHints(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.navigateSFTPDirectory(session.files, "/home/test/docs")
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test/docs" })
	driver := sftpPathFocusDriver(t, state)
	historyDriverClick(t, driver, contextID(hitSFTPHeaderMore))
	foundBack, foundRecent := false, false
	for _, hit := range driver.Snapshot().Hits {
		foundBack = foundBack || hit.Label == "Back"
		foundRecent = foundRecent || hit.Hint == "/home/test"
	}
	if !foundBack || !foundRecent {
		t.Fatalf("history More menu missing Back/full recent Hint: %v/%v", foundBack, foundRecent)
	}
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if driver.Snapshot().Focused != contextID(sftpHeaderSortHits.At(2)) {
		t.Fatalf("sidebar history More menu lost native keyboard scrolling/focus: got %v want %v owner %v", driver.Snapshot().Focused, contextID(sftpHeaderSortHits.At(2)), state.popups.owner)
	}
}

func TestSFTPWindowHistoryNativeRestoreAndIndependentMenus(t *testing.T) {
	state, w, _, host, dir := dualSFTPFixture(t, 1.25)
	preview := host.windows[0].view
	state.activeSessionID = 0
	for i := 0; i < 70; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("row-%03d", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	w.refresh(true)
	folder := &w.view.lfolder
	folder.selectRow(28, 0)
	folder.first = 20
	w.ui.offset[0] = 20 * sftpRowHeight
	name, top, anchor := folder.rows[28].entry.Name, folder.rows[20].entry.Name, folder.rows[folder.anchor].entry.Name
	away := t.TempDir()
	w.navigate(true, away)
	preview.Resize(900, 600)
	dualClickID(t, preview, sftpWindowMore[0])
	dualClickID(t, preview, sftpWindowHistoryBack[0])
	dualWait(t, state, w, preview, func() bool { return w.view.lfolder.path == dir && w.pendingLocal == "" })
	w.wake()
	preview.Rest()
	if folder.rows[folder.first].entry.Name != top || folder.selected < 0 || folder.rows[folder.selected].entry.Name != name || folder.anchor < 0 || folder.rows[folder.anchor].entry.Name != anchor || !folder.rows[folder.selected].selected || w.ui.offset[0] != float32(folder.first)*sftpRowHeight {
		t.Fatal("detached wake/paint lost named place restoration")
	}
	remoteBefore := append([]string(nil), w.view.history.recent...)
	dualClickID(t, preview, dualRow(t, w, true, name))
	if err := preview.Driver().Key(gfx.KeyRight, gfx.ModifierAlt); err != nil {
		t.Fatal(err)
	}
	dualWait(t, state, w, preview, func() bool { return folder.path == away })
	if !reflect.DeepEqual(remoteBefore, w.view.history.recent) {
		t.Fatal("local native history changed remote side")
	}
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
		state.setLanguage(language)
		preview.Resize(640, 600)
		dualClickID(t, preview, sftpWindowMore[0])
		if err := preview.Rest(); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, hit := range preview.Snapshot().Hits {
			found = found || hit.Hint == dir
		}
		if !found {
			t.Fatal("detached narrow menu lost full path hint")
		}
		if output := os.Getenv("MOTTY_SFTP_HISTORY_SCREENSHOTS"); output != "" {
			os.MkdirAll(output, 0700)
			if err := preview.Screenshot(filepath.Join(output, fmt.Sprintf("history-window-%s-narrow.png", language))); err != nil {
				t.Fatal(err)
			}
		}
		if err := preview.Driver().Key(gfx.KeyEscape, 0); err != nil {
			t.Fatal(err)
		}
		if w.headerMenuOpen() {
			t.Fatal("Escape did not close history More menu")
		}
	}
}

func TestSFTPHistorySteadyPaintBudget(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	for i := 0; i < 10; i++ {
		session.files.history.touch(fmt.Sprintf("/history/path-%d", i))
	}
	for _, open := range []bool{false, true} {
		session.files.headerMenuOpen = open
		var canvas gfx.Canvas
		size := gfx.Size{Width: 1400, Height: 900}
		state.Paint(&canvas, size)
		allocs := testing.AllocsPerRun(30, func() { canvas.Reset(); state.Paint(&canvas, size) })
		if allocs != 0 {
			t.Fatalf("sidebar history menu open=%v allocations=%v", open, allocs)
		}
	}
	_, w, _, _, _ := dualSFTPFixture(t, 1)
	for i := 0; i < 10; i++ {
		w.view.lfolder.history.touch(fmt.Sprintf("/history/path-%d", i))
	}
	w.view.lfolder.headerMenuOpen = true
	var canvas gfx.Canvas
	w.paint(&canvas, gfx.Size{Width: 900, Height: 640})
	// Isolate the added menu: existing pane fmt pools are randomly dropped by
	// race builds, so the whole-window allocation count has no fixed ceiling.
	allocs := testing.AllocsPerRun(30, func() {
		canvas.Reset()
		w.paintHeaderMenu(&canvas, 0, gfx.Point{}, 0, gfx.Rect{Width: 900, Height: 640}, &w.ui.headerScroll)
	})
	if allocs != 0 {
		t.Fatalf("detached history menu allocations=%v, want zero", allocs)
	}
}

func TestSFTPHistoryBackgroundMenuCannotStealModalInput(t *testing.T) {
	state, _, _, _ := openTestSFTP(t)
	driver := sftpPathFocusDriver(t, state)
	historyDriverClick(t, driver, contextID(hitSFTPHeaderMore))
	state.openHostDialog()
	driver.Resize(1401, 900)
	historyDriverClick(t, driver, hitDialogLabel)
	if err := driver.Key(gfx.KeyDown, 0); err != nil {
		t.Fatal(err)
	}
	if driver.Snapshot().Focused != hitDialogLabel {
		t.Fatal("background history menu stole modal arrow focus")
	}
	if result := state.Key(gfx.KeyEvent{Target: hitDialogLabel, Text: "f"}); !result.Focus.IsZero() {
		t.Fatal("background history menu captured modal typed key")
	}
	if err := driver.Type("f"); err != nil {
		t.Fatal(err)
	}
	if state.editor.label.String() != "f" {
		t.Fatal("background history menu stole modal typing")
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNone {
		t.Fatal("background history menu stole modal Escape")
	}
}

func TestSFTPHistoryBackgroundMenuCannotStealTransferCenterInput(t *testing.T) {
	state, _, _, _ := openTestSFTP(t)
	driver := sftpPathFocusDriver(t, state)
	historyDriverClick(t, driver, contextID(hitSFTPHeaderMore))
	state.Click(hitTransferCenter)
	driver.Resize(1401, 900)
	historyDriverClick(t, driver, hitTransfersFilter.At(0))
	for _, event := range []gfx.KeyEvent{{Key: gfx.KeyDown}, {Text: "f"}} {
		if event.Key != 0 {
			driver.Key(event.Key, 0)
		} else {
			if result := state.Key(gfx.KeyEvent{Target: driver.Snapshot().Focused, Text: event.Text}); !result.Focus.IsZero() {
				t.Fatal("background history menu captured transfer center typed key")
			}
		}
		if driver.Snapshot().Focused != hitTransfersFilter.At(0) {
			t.Fatal("background history menu stole transfer center input")
		}
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.transfers.open {
		t.Fatal("background history menu stole transfer center Escape")
	}
}

func TestSFTPHistorySidebarNarrowVisual(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
		t.Run(string(language), func(t *testing.T) {
			state, session, _, _ := openTestSFTP(t)
			state.setLanguage(language)
			state.setAppearance(appearanceLight)
			state.navigateSFTPDirectory(session.files, "/home/test/docs")
			waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy })
			preview, err := headless.Open(app.Config{Title: "History", Width: 1100, Height: 640, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize}, nil, headless.Options{Scale: 1.25})
			if err != nil {
				t.Fatal(err)
			}
			defer preview.Close()
			dualClickID(t, preview, contextID(hitSFTPHeaderMore))
			if err := preview.Rest(); err != nil {
				t.Fatal(err)
			}
			dualHit(t, preview, contextID(hitSFTPHistoryBack))
			if output := os.Getenv("MOTTY_SFTP_HISTORY_SCREENSHOTS"); output != "" {
				os.MkdirAll(output, 0700)
				if err := preview.Screenshot(filepath.Join(output, fmt.Sprintf("history-sidebar-%s-light-narrow.png", language))); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
