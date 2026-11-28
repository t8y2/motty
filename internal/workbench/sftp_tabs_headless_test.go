//go:build darwin || linux || windows

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/sftpclient"
)

func TestSFTPBackgroundMenuCreatesFolderInClickedPane(t *testing.T) {
	for _, tab := range []bool{false, true} {
		t.Run(fmt.Sprintf("tab-%t", tab), func(t *testing.T) {
			state, _, w, _, _ := openSFTPTabFixture(t)
			emptyDir := t.TempDir()
			w.navigate(true, emptyDir)
			waitSFTPTab(t, state, w, func() bool {
				return w.view.lfolder.path == emptyDir && len(w.view.lfolder.rows) == 0
			})

			config := w.config()
			if tab {
				config = app.Config{
					Title: "SFTP background menu", Width: 1100, Height: 720, Tree: Tree(),
					Paint: state.Paint, OnPointer: state.Pointer, OnResize: state.Resize,
				}
			}
			preview, err := headless.Open(config, nil, headless.Options{Scale: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer preview.Close()
			if err := preview.Rest(); err != nil {
				t.Fatal(err)
			}

			actionHit := func(action gfx.ID) (gfx.Hit, bool) {
				label := w.windowMenuRowLabel(sftpWindowMenuActions.At(sftpWindowActionIndex(action)))
				return snapshotMenuHit(preview, label)
			}
			for side, local := range []bool{true, false} {
				folder := w.folder(local)
				if !local {
					folder.selectRow(0, 0)
					w.selectionChanged(local)
				}
				list := w.ui.list[side]
				if err := preview.RightClick(list.X+24, list.Y+list.Height-12); err != nil {
					t.Fatal(err)
				}
				if err := preview.Rest(); err != nil {
					t.Fatal(err)
				}
				mkdir, ok := actionHit(hitSFTPNewDirectory)
				if !ok || mkdir.Disabled {
					t.Fatalf("local=%t: New Folder is missing or disabled (menu open=%t)", local, w.ui.menuOpen)
				}
				if _, ok := actionHit(hitSFTPDelete); ok {
					t.Fatalf("local=%t: blank-space menu offered Delete", local)
				}
				if _, ok := actionHit(hitSFTPUpload); ok != !local {
					t.Fatalf("local=%t: Upload present=%t", local, ok)
				}
				if err := preview.Click(mkdir.Bounds.X+mkdir.Bounds.Width/2, mkdir.Bounds.Y+mkdir.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				if w.view.dialog != sftpDialogCreateDirectory || w.view.local != local {
					t.Fatalf("local=%t: dialog=%v, active local=%t", local, w.view.dialog, w.view.local)
				}
				w.view.dialogEditor.Insert("from-context-menu")
				w.click(hitSFTPDialogSubmit)
				waitSFTPTab(t, state, w, func() bool {
					for _, row := range folder.rows {
						if row.entry.Name == "from-context-menu" && row.entry.IsDir() {
							return true
						}
					}
					return false
				})
			}
		})
	}
}

func TestSFTPFileTabHeadlessCompactThemesAndNativeInput(t *testing.T) {
	for _, light := range []bool{false, true} {
		for _, remoteLeft := range []bool{false, true} {
			t.Run(fmt.Sprintf("light-%t-remote-left-%t", light, remoteLeft), func(t *testing.T) {
				state, source, w, _, _ := openSFTPTabFixture(t)
				source.Title = strings.Repeat("long-host-name-", 12)
				w.session.Title = source.Title
				state.rebuildFileTabOptions()
				if light {
					state.setAppearance(appearanceLight)
				}
				if remoteLeft {
					state.sftpWindowOrder = sftpRemoteLeft
				}

				var invalidator app.Invalidator
				preview, err := headless.Open(app.Config{
					Title: "File manager tab", Width: 720, Height: 520, Tree: Tree(),
					Invalidator: &invalidator, Paint: state.Paint,
					OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
					OnScroll: state.Scroll, OnFileDrop: state.FileDrop, OnResize: state.Resize,
					TextInput: state.SFTPTabInput,
				}, nil, headless.Options{Scale: 1})
				if err != nil {
					t.Fatal(err)
				}
				defer preview.Close()
				if err := preview.Driver().Pump(); err != nil {
					t.Fatal(err)
				}

				snapshot := preview.Snapshot()
				if state.fileManagerRect.Width <= 0 || state.fileManagerRect.Height <= 0 {
					t.Fatal("the file manager tab was not painted")
				}
				localText, localOK := textBoundsFromSnapshot(snapshot, "local.yml")
				remoteText, remoteOK := textBoundsFromSnapshot(snapshot, "notes.txt")
				if !localOK || !remoteOK {
					t.Fatalf("compact file manager rows: local=%t remote=%t", localOK, remoteOK)
				}
				if remoteLeft && remoteText.X >= localText.X || !remoteLeft && localText.X >= remoteText.X {
					t.Fatalf("pane order did not match setting: local=%+v remote=%+v remoteLeft=%t", localText, remoteText, remoteLeft)
				}
				tabHit, ok := hitByIDFromSnapshot(snapshot, sftpFileTabID(source.ID))
				if !ok || tabHit.X < 0 || tabHit.X+tabHit.Width > float32(snapshot.Width) {
					t.Fatalf("long file tab escaped the compact title bar: %+v, hit=%t", tabHit, ok)
				}

				modeHit, ok := hitByIDFromSnapshot(snapshot, hitSFTPWindowSudoMode)
				if !ok || modeHit.Width <= 0 || modeHit.Y+modeHit.Height > w.ui.list[1].Y || modeHit.X < 0 || modeHit.X+modeHit.Width > float32(snapshot.Width) {
					t.Fatalf("remote mode control escaped compact header: %+v, hit=%t", modeHit, ok)
				}
				dualClickID(t, preview, sftpWindowPath[0])
				if err := preview.Driver().Pump(); err != nil {
					t.Fatal(err)
				}
				editor := preview.Snapshot().Editor
				if editor == nil {
					t.Fatal("the local path field did not open a native editor")
				}
				if editor.Value != w.view.lfolder.path {
					t.Fatalf("native editor value = %q, want local path %q", editor.Value, w.view.lfolder.path)
				}
				if editor.Bounds.X < 0 || editor.Bounds.Y < 0 ||
					editor.Bounds.X+editor.Bounds.Width > float32(snapshot.Width) ||
					editor.Bounds.Y+editor.Bounds.Height > float32(snapshot.Height) {
					t.Fatalf("native editor escaped the main window: %+v", editor.Bounds)
				}

				if directory := os.Getenv("MOTTY_SFTP_TAB_SCREENSHOT_DIR"); directory != "" {
					if err := os.MkdirAll(directory, 0o755); err != nil {
						t.Fatal(err)
					}
					name := "file-tab-dark-local-left-compact.png"
					if light {
						name = "file-tab-light-local-left-compact.png"
					}
					if remoteLeft {
						name = strings.Replace(name, "local-left", "remote-left", 1)
					}
					if err := preview.Screenshot(filepath.Join(directory, name)); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestSFTPFileTabWheelAndScrollbarStayUnderThePointer(t *testing.T) {
	state, _, w, _, _ := openSFTPTabFixture(t)
	entries := make([]sftpclient.Entry, 240)
	for index := range entries {
		entries[index] = sftpclient.Entry{
			Name: fmt.Sprintf("entry-%03d.yml", index), Size: int64(index),
			ModTime: time.Unix(1_700_000_000+int64(index), 0),
		}
	}
	w.view.lfolder.rows = prepareSFTPRows(nil, entries)

	var invalidator app.Invalidator
	base := app.Config{
		Title: "File manager scrolling", Width: 1100, Height: 720, Tree: Tree(),
		Invalidator: &invalidator, Paint: state.Paint, OnWake: state.Wake,
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
		OnScroll: state.Scroll, OnFileDrop: state.FileDrop, OnResize: state.Resize,
		TextInput: state.SFTPTabInput,
	}
	preview, err := headless.Open(state.WindowConfig(base), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}

	list := w.ui.list[0]
	for _, delta := range []float32{24, 48, 72, -24} {
		before := w.ui.offset[0]
		if err := preview.Scroll(gfx.ScrollEvent{
			X:      list.X + 24,
			Y:      list.Y + 24,
			DeltaY: delta, Precise: true,
		}); err != nil {
			t.Fatal(err)
		}
		if delta > 0 && w.ui.offset[0] <= before {
			t.Fatalf("wheel delta %.0f did not move the list down: %.1f -> %.1f", delta, before, w.ui.offset[0])
		}
		if delta < 0 && w.ui.offset[0] >= before {
			t.Fatalf("wheel delta %.0f did not move the list up: %.1f -> %.1f", delta, before, w.ui.offset[0])
		}
	}

	// The List draws its own scrollbar; hovering reveals it, and a press on the
	// thumb drags the rows, as every page ScrollArea does.
	if err := preview.Move(list.X+list.Width-4, list.Y+24); err != nil {
		t.Fatal(err)
	}
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	var bar gfx.Rect
	for _, hit := range preview.Snapshot().Hits {
		if hit.Role == gfx.RoleSeparator && hit.Bounds.X+hit.Bounds.Width >= list.X+list.Width-1 && pointInRect(hit.Bounds.X+1, hit.Bounds.Y+1, list) {
			bar = hit.Bounds
			break
		}
	}
	if bar.Width <= 0 {
		t.Fatal("the file list has no vertical scrollbar")
	}
	x := bar.X + bar.Width/2
	y := bar.Y + bar.Height/2
	before := w.ui.offset[0]
	if err := preview.Press(x, y); err != nil {
		t.Fatal(err)
	}
	last := w.ui.offset[0]
	for step := float32(1); step <= 4; step++ {
		if err := preview.Move(x, y+step*28); err != nil {
			t.Fatal(err)
		}
		if w.ui.offset[0] <= last {
			t.Fatalf("scrollbar drag step %.0f did not follow the pointer: %.1f -> %.1f", step, last, w.ui.offset[0])
		}
		last = w.ui.offset[0]
	}
	if err := preview.Release(x, y+4*28); err != nil {
		t.Fatal(err)
	}
	if w.ui.offset[0] <= before {
		t.Fatal("dragging the scrollbar did not move the list")
	}
}

// A file tab's view is part of the workbench page, so a click completes in its
// List, the keys aimed at a row are the page's, and typing reaches the field's
// editor through the host. None of these identities is mounted.
func TestSFTPFileTabEventsRouteThroughTheWorkbenchPage(t *testing.T) {
	state, _, w, _, _ := openSFTPTabFixture(t)
	var invalidator app.Invalidator
	preview, err := headless.Open(app.Config{
		Title: "File tab events", Width: 1100, Height: 720, Tree: Tree(),
		Invalidator: &invalidator, Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
		OnScroll: state.Scroll, OnFileDrop: state.FileDrop, OnResize: state.Resize,
		TextInput: state.SFTPTabInput,
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}

	row := w.view.sftpFolder.listRowsScope.At(0)
	if row.IsZero() || !state.fileTabOwns(row) {
		t.Fatalf("remote row identity %v is not the workbench page's", tree.Text(row))
	}
	snapshot := preview.Snapshot()
	hit, ok := hitByIDFromSnapshot(snapshot, row)
	if !ok {
		t.Fatalf("the workbench page did not paint remote row %v", tree.Text(row))
	}
	if err := preview.Click(hit.X+hit.Width/2, hit.Y+hit.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	if w.view.sftpFolder.selected != 0 || !w.view.sftpFolder.rows[0].selected {
		t.Fatalf("a press on a page-declared row did not choose it: selected=%d rows=%v", w.view.sftpFolder.selected, w.view.sftpFolder.rows[0].selected)
	}
	if result := state.Key(gfx.KeyEvent{Target: row, Key: gfx.KeyDown}); !result.Handled {
		t.Fatal("the workbench page did not take a key aimed at its row")
	}
	if w.view.sftpFolder.selected != 1 {
		t.Fatalf("the page's List did not move the row on Down: selected=%d", w.view.sftpFolder.selected)
	}

	dualClickID(t, preview, sftpWindowPath[0])
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	if preview.Snapshot().Editor == nil {
		t.Fatal("the tab's path field did not open the host editor")
	}
	before := w.view.lfolder.pathInput().String()
	if err := preview.Type("x"); err != nil {
		t.Fatal(err)
	}
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	after := w.view.lfolder.pathInput().String()
	if after == before || !strings.Contains(after, "x") {
		t.Fatalf("typing did not reach the tab's path editor: %q -> %q", before, after)
	}

	// The tab's More menu is painted by the workbench page over the window, with
	// the window's own identities: a press on one of its rows reaches the window
	// with no mount between them.
	snapshot = preview.Snapshot()
	more, ok := hitByIDFromSnapshot(snapshot, sftpWindowMore[0])
	if !ok {
		t.Fatal("the local pane has no More button")
	}
	if err := preview.Click(more.X+more.Width/2, more.Y+more.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	if !w.view.lfolder.headerMenuOpen {
		t.Fatal("the tab's More menu did not open")
	}
	hidden, ok := snapshotMenuHit(preview, w.headerMenuRowLabel(0, sftpWindowHeaderHidden[0]))
	if !ok {
		t.Fatal("the tab's More menu is not painted by the workbench page")
	}
	wasHidden := w.view.lfolder.showHidden
	if err := preview.Click(hidden.Bounds.X+hidden.Bounds.Width/2, hidden.Bounds.Y+hidden.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	if w.view.lfolder.showHidden == wasHidden || w.view.lfolder.headerMenuOpen {
		t.Fatalf("the menu row did not act on the tab: hidden=%t->%t open=%t", wasHidden, w.view.lfolder.showHidden, w.view.lfolder.headerMenuOpen)
	}
}

// The properties dialog a file-manager tab declares keeps the same identities
// as the docked panel's, so the workbench page must route its close actions and
// keys to the window that opened it: both close controls (the title bar's and
// the footer's) and the backdrop, as well as Escape, must close it without
// acting on the file list behind it.
func TestSFTPFileTabPropertiesDialogClosesFromItsOwnControls(t *testing.T) {
	state, _, w, _, _ := openSFTPTabFixture(t)
	preview, err := headless.Open(app.Config{
		Title: "File tab properties", Width: 1100, Height: 720, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize,
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}

	// open declares the dialog on the tab and returns the controls the page
	// painted for closing it.
	open := func() []gfx.Hit {
		t.Helper()
		w.view.sftpFolder.selectRow(0, 0)
		w.selectionChanged(false)
		w.fileAction(false, hitSFTPProperties)
		if !w.fileProperties.open {
			t.Fatal("the file-manager properties dialog did not open")
		}
		// The action only queues a repaint; the tab is declared by the page,
		// which has no window of its own to paint it.
		if err := preview.Driver().Paint(); err != nil {
			t.Fatal(err)
		}
		var closes []gfx.Hit
		for _, hit := range preview.Snapshot().Hits {
			if hit.ID == hitFilePropertiesClose && !hit.Block {
				closes = append(closes, hit)
			}
		}
		return closes
	}

	closes := open()
	if len(closes) < 2 {
		t.Fatalf("the properties dialog painted %d close controls, want the title bar's and the footer's", len(closes))
	}
	for index, hit := range closes {
		if err := preview.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
		if w.fileProperties.open {
			t.Fatalf("close control %d did not close the tab's properties dialog", index)
		}
		open()
	}

	// A press on the dialog's backdrop closes it, and does not reach the list
	// selection behind it.
	selected := w.view.sftpFolder.selected
	if err := preview.Click(10, 700); err != nil {
		t.Fatal(err)
	}
	if w.fileProperties.open {
		t.Fatal("the backdrop did not close the tab's properties dialog")
	}
	if w.view.sftpFolder.selected != selected {
		t.Fatalf("a press on the backdrop changed the list selection: %d -> %d", selected, w.view.sftpFolder.selected)
	}

	open()
	if err := preview.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	if w.fileProperties.open {
		t.Fatal("Escape did not close the tab's properties dialog")
	}

	open()
	rows := len(w.view.rows)
	if result := state.Key(gfx.KeyEvent{Target: hitFilePropertiesClose, Key: gfx.KeyDelete}); !result.Handled {
		t.Fatal("a key escaped the tab's properties dialog")
	}
	if w.view.dialog != sftpDialogNone || len(w.view.rows) != rows {
		t.Fatal("a key acted on the file list behind the tab's properties dialog")
	}
}

// While a full-page SFTP file tab is active in vertical-tabs mode, interactive
// controls on the left vertical sidebar (workspace tabs, New Tab, and right-click
// context menus) must still respond to clicks, while blank rail surface over an
// expanded auto-hide sidebar still blocks presses from reaching the file list.
func TestSFTPFileTabVerticalTabsSidebarClicksAndContextMenu(t *testing.T) {
	state, source, w, _, _ := openSFTPTabFixture(t)
	state.setTabOrientation(tabsVertical)
	workspace := state.deck.ActiveWorkspace()
	if state.activeSection != sectionSFTP || state.activeSFTPWindow() != w {
		t.Fatalf("expected active SFTP file tab, got section=%v window=%v", state.activeSection, state.activeSFTPWindow())
	}

	preview, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Vertical tabs on SFTP file tab", Width: 1100, Height: 720, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll, OnResize: state.Resize,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}

	// Right-clicking a workspace tab in the vertical sidebar opens its tab menu.
	tabID := hitTabList.At(int(workspace))
	tabRect, ok := hitByIDFromSnapshot(preview.Snapshot(), tabID)
	if !ok {
		t.Fatalf("vertical sidebar did not paint workspace tab %v", tabID)
	}
	if err := preview.RightClick(tabRect.X+tabRect.Width/2, tabRect.Y+tabRect.Height/2); err != nil {
		t.Fatal(err)
	}
	if !state.tabMenu.open || state.tabMenu.workspace != workspace {
		t.Fatalf("right-clicking vertical tab on SFTP file tab did not open tab menu: %+v", state.tabMenu)
	}
	state.tabMenu = tabMenu{}
	if err := preview.Driver().Paint(); err != nil {
		t.Fatal(err)
	}

	// Clicking New Tab in the vertical sidebar opens the connect picker.
	newTabRect, ok := hitByIDFromSnapshot(preview.Snapshot(), hitVerticalTabsNew)
	if !ok {
		t.Fatal("vertical sidebar did not paint New Tab button")
	}
	if err := preview.Click(newTabRect.X+newTabRect.Width/2, newTabRect.Y+newTabRect.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalCommandPalette || !state.commandPalette.connect {
		t.Fatalf("clicking New Tab in vertical sidebar while on SFTP file tab did not open connect picker: modal=%v connect=%t", state.modal, state.commandPalette.connect)
	}
	state.closeCommandPalette()
	if err := preview.Driver().Paint(); err != nil {
		t.Fatal(err)
	}

	// Clicking a workspace tab in the vertical sidebar switches back to the terminal.
	tabRect, ok = hitByIDFromSnapshot(preview.Snapshot(), tabID)
	if !ok {
		t.Fatalf("vertical sidebar did not paint workspace tab %v", tabID)
	}
	if err := preview.Click(tabRect.X+tabRect.Width/2, tabRect.Y+tabRect.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.activeSection != sectionTerminal || state.deck.ActiveWorkspace() != workspace {
		t.Fatalf("clicking vertical tab did not switch back to terminal: section=%v workspace=%v", state.activeSection, state.deck.ActiveWorkspace())
	}

	// Switch back to the SFTP file tab with auto-hide enabled and expanded over
	// the file list: blank rail clicks must not reach the file list underneath,
	// while clicking a workspace tab on the expanded rail still switches back.
	state.activateSFTPTab(source.ID)
	state.setVerticalTabsAutoHide(true)
	if err := preview.Driver().Paint(); err != nil {
		t.Fatal(err)
	}
	if err := preview.Move(8, windowBarHeight+20); err != nil {
		t.Fatal(err)
	}
	if err := preview.Driver().Advance(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if state.verticalTabs.bounds.Width <= verticalTabsRail {
		t.Fatalf("auto-hide vertical rail did not expand: %+v", state.verticalTabs.bounds)
	}
	w.view.selected = -1
	blankX := state.verticalTabs.bounds.X + state.verticalTabs.bounds.Width - 10
	blankY := state.verticalTabs.bounds.Y + state.verticalTabs.bounds.Height - verticalTabsFooter - 20
	if err := preview.Click(blankX, blankY); err != nil {
		t.Fatal(err)
	}
	if state.activeSection != sectionSFTP || w.view.selected != -1 {
		t.Fatalf("blank expanded rail click leaked to SFTP page: section=%v selected=%d", state.activeSection, w.view.selected)
	}

	tabRect, ok = hitByIDFromSnapshot(preview.Snapshot(), tabID)
	if !ok {
		t.Fatalf("expanded auto-hide sidebar did not paint workspace tab %v", tabID)
	}
	if err := preview.Click(tabRect.X+tabRect.Width/2, tabRect.Y+tabRect.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.activeSection != sectionTerminal {
		t.Fatalf("clicking workspace tab in expanded auto-hide sidebar did not switch back to terminal: section=%v", state.activeSection)
	}
}

func textBoundsFromSnapshot(snapshot app.Snapshot, value string) (gfx.Rect, bool) {
	for _, text := range snapshot.Texts {
		if text.Value == value {
			return text.Bounds, true
		}
	}
	return gfx.Rect{}, false
}

func hitByIDFromSnapshot(snapshot app.Snapshot, id gfx.ID) (gfx.Rect, bool) {
	for _, hit := range snapshot.Hits {
		if hit.ID == id && !hit.Block {
			return hit.Bounds, true
		}
	}
	return gfx.Rect{}, false
}
