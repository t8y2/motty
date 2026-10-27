//go:build windows || darwin || linux

package workbench

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

func TestSFTPWindowSortHeadersPreserveSelectionAndRefreshOrder(t *testing.T) {
	state, w, _, host, _ := dualSFTPFixture(t, 1.25)
	view := host.windows[0].view
	entries := []sftpclient.Entry{
		{Name: "z.txt", Size: 10, ModTime: time.Unix(1, 0)},
		{Name: "a.txt", Size: 30, ModTime: time.Unix(3, 0)},
		{Name: "m.txt", Size: 20, ModTime: time.Unix(2, 0)},
		{Name: "folder", Mode: os.ModeDir | 0755},
	}
	for side := range 2 {
		folder := w.folder(side == 0)
		folder.rows = prepareSFTPRows(nil, entries)
		folder.selectRow(2, 0)
		for column := range 3 {
			for click := range 2 {
				view.Rest()
				header := dualHit(t, view, sftpWindowSortHits[side].At(column))
				view.Click(header.X+header.Width/2, header.Y+header.Height/2)
				if folder.rows[0].entry.Name != "folder" {
					t.Fatal("directories must stay first")
				}
				expected := "z.txt"
				if column == 0 {
					expected = "a.txt"
				}
				if w.ui.sort[side].descending {
					if expected == "z.txt" {
						expected = "a.txt"
					} else {
						expected = "z.txt"
					}
				}
				if folder.rows[1].entry.Name != expected {
					t.Fatal("wrong sort", side, column, click, folder.rows[1].entry.Name)
				}
				if folder.rows[folder.selected].entry.Name != "m.txt" || !folder.rows[folder.selected].selected {
					t.Fatal("sort changed selected file")
				}
			}
		}
		if side == 1 {
			order := w.ui.sort[side]
			state.showSFTPListing(w.view, w.view.path, entries)
			if w.ui.sort[side] != order || w.view.rows[1].entry.Name != "a.txt" {
				t.Fatal("refresh lost descending time order")
			}
		}
	}
}

func TestSFTPWindowWheelMatchesPanelAndPreservesPreciseDeltas(t *testing.T) {
	_, w, _, host, _ := dualSFTPFixture(t, 1.5)
	entries := make([]sftpclient.Entry, 1000)
	for i := range entries {
		entries[i].Name = fmt.Sprintf("file-%04d.yml", i)
	}
	w.view.lfolder.rows = prepareSFTPRows(nil, entries)
	w.window.Invalidate()
	host.windows[0].view.Driver().Pump()
	list := w.ui.list[0]
	expected := sftpFolder{rows: w.view.lfolder.rows, visibleRows: w.view.lfolder.visibleRows}
	for _, delta := range []float32{1, 2, 17, 10, 144, -60} {
		event := ui.ScrollEvent{X: list.X + 20, Y: list.Y + 20, DeltaY: delta}
		scrollFolder(&expected, event)
		host.windows[0].view.Scroll(event)
		host.windows[0].view.Rest()
		if w.ui.offset[0] != float32(expected.first)*sftpRowHeight {
			t.Fatal("wheel differs from side panel", delta, w.ui.offset[0], expected.first)
		}
	}
	before := w.ui.offset[0]
	w.scroll(ui.ScrollEvent{X: list.X + 20, Y: list.Y + 20, DeltaY: .25, Precise: true})
	if math.Abs(float64(w.ui.offset[0]-before-.25)) > .001 {
		t.Fatal("precise scroll was rounded")
	}
	w.ui.offset[0] = w.ui.bars[0].MaxOffsetY
	if w.scroll(ui.ScrollEvent{X: list.X + 20, Y: list.Y + 20, DeltaY: 1, Precise: true}).Repaint {
		t.Fatal("edge scroll requested unnecessary repaint")
	}
}

func TestSFTPWindowToolbarAndNativeFileActions(t *testing.T) {
	state, w, remote, host, _ := dualSFTPFixture(t, 1.5)
	view := host.windows[0].view
	up := dualHit(t, view, sftpWindowUp[1])
	for _, id := range []ui.ID{hitSFTPWindowUpload, hitSFTPWindowDownload} {
		button := dualHit(t, view, id)
		if button.X >= up.X || button.Y != up.Y {
			t.Fatal("transfer buttons must precede remote up button")
		}
	}
	var canvas ui.Canvas
	w.paint(&canvas, w.ui.size)
	for _, text := range canvas.Texts() {
		if text.Value == w.session.Title+" · "+w.session.Address {
			t.Fatal("duplicate connection title is still rendered")
		}
	}
	selectRemote := func(name string) {
		for i := range w.view.rows {
			if w.view.rows[i].entry.Name == name {
				w.view.sftpFolder.selectRow(i, 0)
				return
			}
		}
		t.Fatal("missing", name)
	}
	selectRemote("notes.txt")
	w.fileAction(false, hitSFTPRename)
	view.Driver().Pump()
	view.Rest()
	field := dualHit(t, view, hitSFTPDialogField)
	view.Click(field.X+20, field.Y+10)
	modifier := ui.ModifierControl
	if runtime.GOOS == "darwin" {
		modifier = ui.ModifierMeta
	}
	view.Driver().ShortcutChord(modifier, 'a')
	view.Type("renamed.yml")
	view.Keys("enter")
	dualWait(t, state, w, view, func() bool { return !w.view.busy && remoteText(remote, "/home/test/renamed.yml") == "hello world" })
	selectRemote("renamed.yml")
	w.fileAction(false, hitSFTPChmod)
	app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextSelectAll}.Apply(&w.view.dialogEditor)
	w.view.dialogEditor.Insert("600")
	w.click(hitSFTPDialogSubmit)
	dualWait(t, state, w, view, func() bool { return !w.view.busy })
	selectRemote("renamed.yml")
	if w.view.rows[w.view.selected].entry.Mode.Perm() != 0600 {
		t.Fatal("permissions did not change")
	}
	w.fileAction(false, hitSFTPDelete)
	w.click(hitSFTPDialogSubmit)
	dualWait(t, state, w, view, func() bool { return !w.view.busy && remoteText(remote, "/home/test/renamed.yml") == "" })
	w.session.host.CatalogID = "dual-window-host"
	w.click(hitSFTPBookmarkToggle)
	if !state.bookmarked(&w.session, w.view.path) {
		t.Fatal("bookmark not added")
	}
	w.navigate(false, "/home/test/docs")
	dualWait(t, state, w, view, func() bool { return !w.view.busy && w.view.path == "/home/test/docs" })
	w.click(hitSFTPBookmarkList)
	view.Driver().Pump()
	view.Rest()
	mark := dualHit(t, view, hitSFTPBookmarkItemList.At(0))
	view.Click(mark.X+10, mark.Y+10)
	dualWait(t, state, w, view, func() bool { return !w.view.busy && w.view.path == "/home/test" })
}

func TestSFTPWindowArchivesUseBoundConnectionViewAndNotices(t *testing.T) {
	state, w, remote, host, _ := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	remote.mu.Lock()
	remote.entries[w.view.path] = append(remote.entries[w.view.path], workbenchFileInfo{name: "pack.zip", size: 10, mode: 0644})
	remote.files["/home/test/pack.zip"] = []byte("archive")
	remote.mu.Unlock()
	w.refresh(false)
	dualWait(t, state, w, view, func() bool { return !w.view.busy && len(w.view.rows) == 3 })
	var command string
	conn := w.session.conn.(*fakeConn)
	conn.mu.Lock()
	conn.runCommand = func(_ context.Context, value string, _ int) ([]byte, error) { command = value; return nil, nil }
	conn.mu.Unlock()
	state.activeSessionID = 0
	for i := range w.view.rows {
		if w.view.rows[i].entry.Name == "pack.zip" {
			w.view.sftpFolder.selectRow(i, 0)
		}
	}
	w.fileAction(false, hitSFTPExtract)
	if w.view.dialog != sftpDialogExtract {
		t.Fatal("extract action followed the active tab")
	}
	w.click(hitSFTPDialogSubmit)
	dualWait(t, state, w, view, func() bool { return !w.view.extracting && !w.view.busy })
	if !strings.Contains(command, "'/home/test/pack.zip'") {
		t.Fatal("wrong extraction target", command)
	}

	longDirectory := "/home/test/" + strings.Repeat("very-long-directory/", 20)
	w.view.path = longDirectory
	for i := range w.view.rows {
		w.view.rows[i].selected = w.view.rows[i].entry.Name == "docs"
	}
	w.selectionChanged(false)
	w.activeLocal, w.view.local = false, false
	conn.mu.Lock()
	conn.runCommand = func(_ context.Context, value string, _ int) ([]byte, error) {
		command = value
		return []byte(archiveMissingTool + "gzip\n"), fmt.Errorf("exit 127")
	}
	conn.mu.Unlock()
	w.window.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	view.Rest()
	compress := sftpWindowActions[1].At(sftpWindowActionIndex(hitSFTPCompress))
	button := dualHit(t, view, compress)
	if err := view.Click(button.X+button.Width/2, button.Y+button.Height/2); err != nil {
		t.Fatal(err)
	}
	if w.view.dialog != sftpDialogCompress {
		t.Fatal("compress action followed the active tab")
	}
	w.click(hitSFTPDialogSubmit)
	dualWait(t, state, w, view, func() bool { return !w.view.extracting && !w.view.busy && len(w.notices.items) == 1 })
	if !strings.Contains(command, longDirectory+"docs") {
		t.Fatal("wrong compression target", command)
	}
	detail := w.notices.items[0].detail
	if len(state.fileNotices.items) != 0 || !strings.Contains(detail, "gzip") || !strings.Contains(detail, "docs") || !strings.Contains(detail, "…") {
		t.Fatalf("archive failure escaped its origin window: main=%+v detached=%q", state.fileNotices.items, detail)
	}
}

func TestSFTPWindowExternalEditorConflictAndCloseStayInOrigin(t *testing.T) {
	state, w, remote, host, _ := dualSFTPFixture(t, 1.25)
	view := host.windows[0].view
	state.options.OpenPath = func(string) error { return nil }
	state.activeSessionID = 0
	for i := range w.view.rows {
		if w.view.rows[i].entry.Name == "notes.txt" {
			w.view.sftpFolder.selectRow(i, 0)
		}
	}
	w.fileAction(false, hitSFTPEditExternal)
	dualWait(t, state, w, view, func() bool { return len(state.edits.list) == 1 })
	edit := &state.edits.list[0]
	if edit.origin != w {
		t.Fatal("external edit lost origin")
	}
	remote.mu.Lock()
	remote.files["/home/test/notes.txt"] = []byte("changed on server")
	remote.mu.Unlock()
	if err := os.WriteFile(edit.local, []byte("my edited content"), 0600); err != nil {
		t.Fatal(err)
	}
	state.pollExternalEdits()
	state.pollExternalEdits()
	dualWait(t, state, w, view, func() bool { return edit.conflicted })
	state.pollExternalEdits()
	view.Driver().Pump()
	view.Rest()
	if w.externalConflict == 0 || state.modal != modalNone {
		t.Fatal("conflict appeared outside its native window")
	}
	button := dualHit(t, view, hitSFTPWindowExternalReplace)
	view.Click(button.X+10, button.Y+10)
	dualWait(t, state, w, view, func() bool { return remoteText(remote, "/home/test/notes.txt") == "my edited content" && !edit.saving })
	local := edit.local
	w.window.RequestClose()
	if !w.closeConfirm {
		t.Fatal("external sync stopped without close confirmation")
	}
	w.click(hitSFTPWindowCloseCancel)
	if w.closed {
		t.Fatal("cancel closed editor sync")
	}
	w.window.RequestClose()
	w.click(hitSFTPWindowCloseConfirm)
	if _, err := os.Stat(local); !os.IsNotExist(err) || len(state.edits.list) != 0 {
		t.Fatal("external edit not released", err)
	}
}

func BenchmarkSFTPWindowScroll(b *testing.B) {
	for _, count := range []int{1000, 20000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			state, session, _, _ := openTestSFTP(b)
			w := &sftpWindow{owner: state, session: *session, view: session.files}
			w.ui.fraction = .5
			entries := make([]sftpclient.Entry, count)
			for i := range entries {
				entries[i] = sftpclient.Entry{Name: fmt.Sprintf("配置文件-%05d.yml", i), Size: 123, ModTime: time.Unix(100, 0)}
			}
			w.view.rows = prepareSFTPRows(nil, entries)
			w.view.lfolder.rows = prepareSFTPRows(nil, entries)
			w.view.lfolder.path = filepath.Clean(os.TempDir())
			var canvas ui.Canvas
			w.paint(&canvas, ui.Size{Width: 1200, Height: 800})
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				w.ui.offset[0] = float32(i%64) * sftpRowHeight
				w.wake()
				canvas.Reset()
				w.paint(&canvas, ui.Size{Width: 1200, Height: 800})
			}
		})
	}
}

func TestSFTPWindowDropOverlayKeepsTargetsAndShowsDirection(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		for _, light := range []bool{false, true} {
			t.Run(fmt.Sprintf("%.2f-light-%t", scale, light), func(t *testing.T) {
				state, w, _, host, _ := dualSFTPFixture(t, scale)
				view := host.windows[0].view
				if light {
					state.setAppearance(appearanceLight)
					w.window.Invalidate()
					view.Driver().Pump()
				}
				before := len(view.Snapshot().Hits)
				list := w.ui.list[1]
				accepted, err := view.Drop(ui.FileDropEvent{Phase: ui.FileDropOver, X: list.X + 30, Y: list.Y + list.Height - 20})
				if err != nil || !accepted {
					t.Fatal("system upload hover refused", err)
				}
				if len(view.Snapshot().Hits) != before {
					t.Fatal("overlay changed file hit regions")
				}
				contains := func(label string) bool {
					for _, text := range view.Snapshot().Texts {
						if text.Value == label {
							return true
						}
					}
					return false
				}
				if !contains(state.messages.SFTPWindow.DropUploadHint) || !contains(w.view.path) {
					t.Fatal("upload overlay lost direction or target")
				}
				if dir := os.Getenv("MOTTY_SFTP_WINDOW_SCREENSHOT_DIR"); dir != "" {
					os.MkdirAll(dir, 0700)
					view.Screenshot(filepath.Join(dir, fmt.Sprintf("overlay-%.2f-light-%t.png", scale, light)))
				}
				view.Drop(ui.FileDropEvent{Phase: ui.FileDropLeave})
				if w.drag.active {
					t.Fatal("leaving did not clear overlay")
				}
				list = w.ui.list[0]
				w.drag = sftpWindowDrag{active: true, at: ui.Point{X: list.X + 30, Y: list.Y + list.Height - 20}}
				w.window.Invalidate()
				view.Driver().Pump()
				if !contains(state.messages.SFTPWindow.DropDownloadHint) {
					t.Fatal("download overlay has wrong direction")
				}
				view.Keys("escape")
				if w.drag.active || len(w.view.transfers) != 0 {
					t.Fatal("cancelled hover queued a transfer")
				}
			})
		}
	}
}

func TestSFTPWindowGlobalHistoryRemovalRepaintsNativeQueue(t *testing.T) {
	state, w, _, host, dir := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	w.queueEntries(true, []sftpclient.Entry{{Name: "中文上传.yml"}}, dir, w.view.path)
	dualWait(t, state, w, view, func() bool { return len(w.view.transfers) == 1 && !sftpTransfersActive(w.view.transfers) })
	if w.ui.queueBounds.Width == 0 {
		t.Fatal("queue not visible")
	}
	index := state.transfers.byTask[transferKey{w.view, w.view.transfers[0].ID}]
	state.removeTransfer(index)
	view.Driver().Pump()
	if w.queueCount() != 0 || w.ui.queueBounds.Width != 0 {
		t.Fatal("global removal left a stale native queue")
	}
}
