package workbench

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

func TestSFTPSidebarHeadersSortEveryColumnWithFoldersFirst(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	remote.mu.Lock()
	remote.entries["/home/test"] = append(remote.entries["/home/test"],
		workbenchFileInfo{name: "alpha.txt", size: 2, mode: 0o644, when: time.Unix(1_700_000_400, 0)},
		workbenchFileInfo{name: "large.bin", size: 900, mode: 0o644, when: time.Unix(1_700_000_200, 0)},
		workbenchFileInfo{name: "logs", mode: os.ModeDir | 0o755, when: time.Unix(1_700_000_050, 0)},
	)
	remote.mu.Unlock()
	state.Click(hitSFTPRefresh)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && len(view.rows) == 5 })
	view := session.files

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	for column, label := range []string{
		state.messages.SFTPWindow.Name,
		state.messages.SFTPWindow.Size,
		state.messages.SFTPWindow.Modified,
	} {
		if _, ok := hitByID(&canvas, contextID(hitSFTPColumnList.At(column))); !ok {
			t.Fatalf("column %d (%q) has no header hit", column, label)
		}
	}

	names := func() []string {
		result := make([]string, len(view.rows))
		for index := range view.rows {
			result[index] = view.rows[index].entry.Name
		}
		return result
	}
	state.Click(hitSFTPColumnList.At(0))
	if got, want := names(), []string{"docs", "logs", "alpha.txt", "large.bin", "notes.txt"}; !slices.Equal(got, want) {
		t.Fatalf("name ascending = %v, want %v", got, want)
	}
	state.Click(hitSFTPColumnList.At(0))
	if got, want := names(), []string{"logs", "docs", "notes.txt", "large.bin", "alpha.txt"}; !slices.Equal(got, want) {
		t.Fatalf("name descending = %v, want %v", got, want)
	}

	state.Click(hitSFTPColumnList.At(1))
	if got := names(); !slices.Equal(got[:2], []string{"docs", "logs"}) ||
		!slices.Equal(got[2:], []string{"alpha.txt", "notes.txt", "large.bin"}) {
		t.Fatalf("size ascending does not sort folders first: %v", got)
	}
	state.Click(hitSFTPColumnList.At(1))
	if got := names(); !slices.Equal(got[:2], []string{"logs", "docs"}) ||
		!slices.Equal(got[2:], []string{"large.bin", "notes.txt", "alpha.txt"}) {
		t.Fatalf("size descending does not sort folders first: %v", got)
	}

	state.Click(hitSFTPColumnList.At(2))
	if got := names(); !slices.Equal(got[:2], []string{"logs", "docs"}) ||
		!slices.Equal(got[2:], []string{"notes.txt", "large.bin", "alpha.txt"}) {
		t.Fatalf("time ascending = %v", got)
	}
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	foundDirection := false
	for _, text := range canvas.Texts() {
		foundDirection = foundDirection || text.Value == state.messages.SFTPWindow.Modified+" ↑"
	}
	if !foundDirection {
		t.Fatal("the active header does not show its sort direction")
	}
}

func TestSFTPSidebarColumnVisibilityTracksSettings(t *testing.T) {
	state, _, _, _ := openTestSFTP(t)
	state.sftpColumns.setEnabled(sftpColumnSize, false)
	state.sftpColumns.setEnabled(sftpColumnModified, false)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, contextID(hitSFTPColumnList.At(0))); !ok {
		t.Fatal("the fixed name header was hidden")
	}
	for column := 1; column < 3; column++ {
		if _, ok := hitByID(&canvas, contextID(hitSFTPColumnList.At(column))); ok {
			t.Fatalf("hidden column %d still has a header hit", column)
		}
	}
}

func TestSFTPColumnOrderAppliesToSidebarAndDetachedWindow(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	if !state.sftpColumns.moveOptional(1, 2) {
		t.Fatal("the size column did not move after modified time")
	}
	if got, want := state.sftpColumns.String(), "name:1,modified:1,size:1"; got != want {
		t.Fatalf("column order = %q, want %q", got, want)
	}

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	sizeHeader, sizeOK := hitByID(&canvas, contextID(hitSFTPColumnList.At(int(sftpColumnSize))))
	modifiedHeader, modifiedOK := hitByID(&canvas, contextID(hitSFTPColumnList.At(int(sftpColumnModified))))
	if !sizeOK || !modifiedOK || modifiedHeader.Bounds.X >= sizeHeader.Bounds.X {
		t.Fatalf("sidebar headers size=%+v/%t modified=%+v/%t", sizeHeader, sizeOK, modifiedHeader, modifiedOK)
	}

	w := &sftpWindow{owner: state, session: *session, view: session.files}
	canvas = gfx.Canvas{}
	w.paintPane(&canvas, gfx.Rect{Width: 600, Height: 500}, false)
	sizeHeader, sizeOK = hitByID(&canvas, sftpWindowSortHits[1].At(int(sftpColumnSize)))
	modifiedHeader, modifiedOK = hitByID(&canvas, sftpWindowSortHits[1].At(int(sftpColumnModified)))
	if !sizeOK || !modifiedOK || modifiedHeader.Bounds.X >= sizeHeader.Bounds.X {
		t.Fatalf("window headers size=%+v/%t modified=%+v/%t", sizeHeader, sizeOK, modifiedHeader, modifiedOK)
	}
}

func TestSFTPColumnPreferencesKeepNameFirstAndIgnoreInvalidEntries(t *testing.T) {
	columns := restoreSFTPColumnPreferences("modified:false,size:true,name:false,size:true,unknown:true")
	if got, want := columns.String(), "name:1,modified:0,size:1"; got != want {
		t.Fatalf("normalized columns = %q, want %q", got, want)
	}
	if !columns.enabled(sftpColumnName) || columns.enabled(sftpColumnModified) || !columns.enabled(sftpColumnSize) {
		t.Fatalf("normalized visibility = %+v", columns)
	}
}

func TestSFTPColumnAndDownloadSettingsPersistAndUpgrade(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.sftpColumns.setEnabled(sftpColumnSize, false)
	state.sftpDownloadDirectory = t.TempDir()
	state.save()

	saved, _, err := loadState(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.SFTPColumns != "name:1,size:0,modified:1" || saved.SFTPDownloadDirectory != state.sftpDownloadDirectory {
		t.Fatalf("saved columns=%q directory=%q", saved.SFTPColumns, saved.SFTPDownloadDirectory)
	}
	restored, _, err := loadState(options.StatePath)
	if err != nil {
		t.Fatalf("restore = %+v, %v", restored, err)
	}
	columns := restoreSFTPColumnPreferences(restored.SFTPColumns)
	if columns.enabled(sftpColumnSize) || !columns.enabled(sftpColumnModified) ||
		restoreSFTPDownloadDirectory(restored.SFTPDownloadDirectory) != state.sftpDownloadDirectory {
		t.Fatalf("restored columns=%+v directory=%q", columns, restoreSFTPDownloadDirectory(restored.SFTPDownloadDirectory))
	}

	state.sftpDownloadDirectory = ""
	state.save()
	saved, _, err = loadState(options.StatePath)
	if err != nil || saved.SFTPDownloadDirectory != sftpDownloadAsk {
		t.Fatalf("ask setting saved as %q, %v", saved.SFTPDownloadDirectory, err)
	}
	if columns := restoreSFTPColumnPreferences(""); !columns.enabled(sftpColumnSize) || !columns.enabled(sftpColumnModified) {
		t.Fatalf("an older state did not gain both optional columns: %+v", columns)
	}
	if columns := restoreSFTPColumnPreferences("name,modified"); columns.enabled(sftpColumnSize) || !columns.enabled(sftpColumnModified) {
		t.Fatalf("the legacy column value did not migrate: %+v", columns)
	}
	if columns := restoreSFTPColumnPreferences("bogus"); !columns.enabled(sftpColumnSize) || !columns.enabled(sftpColumnModified) {
		t.Fatalf("a damaged column value hid every optional column: %+v", columns)
	}
	if got := restoreSFTPDownloadDirectory(""); got != defaultDownloadDirectory() {
		t.Fatalf("an older state download directory = %q, want %q", got, defaultDownloadDirectory())
	}
}

func TestSFTPDefaultDownloadDirectoryUsesTheSystemDownloadsPlace(t *testing.T) {
	home := t.TempDir()
	places := []sftpLocalPlace{
		{kind: localPlaceHome, path: home},
		{kind: localPlaceDownloads, path: filepath.Join(home, "重定向下载")},
	}
	if got, want := downloadDirectoryFromPlaces(home, places), filepath.Join(home, "重定向下载"); got != want {
		t.Fatalf("download directory = %q, want %q", got, want)
	}
	if got, want := downloadDirectoryFromPlaces(home, nil), filepath.Join(home, "Downloads"); got != want {
		t.Fatalf("fallback download directory = %q, want %q", got, want)
	}
	if got := downloadDirectoryFromPlaces("", []sftpLocalPlace{{kind: localPlaceDownloads, path: "relative"}}); got != "" {
		t.Fatalf("relative system download path = %q, want empty", got)
	}
}

func TestSFTPDefaultDownloadDirectorySkipsThePicker(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	destination := t.TempDir()
	state.sftpDownloadDirectory = destination
	pickerCalls := 0
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) {
		pickerCalls++
		return "", app.ErrCanceled
	}
	clickSFTPRow(t, state, session.files, "notes.txt")
	state.Click(hitSFTPDownload)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return len(view.transfers) == 1 && view.transfers[0].State == sftpclient.TransferCompleted
	})
	content, err := os.ReadFile(filepath.Join(destination, "notes.txt"))
	if err != nil || string(content) != "hello world" || pickerCalls != 0 {
		t.Fatalf("download=%q, err=%v, picker calls=%d", content, err, pickerCalls)
	}
}

func TestSFTPWindowColumnVisibilityMatchesSidebarSettings(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	session.files.sftpFolder.rows = prepareSFTPRows(nil, []sftpclient.Entry{{
		Name: "columns-hidden.txt", Size: 987_654_321, Mode: 0o644, ModTime: time.Unix(1_722_222_222, 0),
	}})
	session.files.selected = -1
	w := &sftpWindow{owner: state, session: *session, view: session.files}
	hiddenValues := []string{session.files.rows[0].cells[2], session.files.rows[0].cells[3]}

	paint := func() (*gfx.Canvas, map[string]bool) {
		var canvas gfx.Canvas
		w.paintPane(&canvas, gfx.Rect{Width: 600, Height: 500}, false)
		texts := make(map[string]bool)
		for _, text := range canvas.Texts() {
			texts[text.Value] = true
		}
		return &canvas, texts
	}

	state.sftpColumns.setEnabled(sftpColumnSize, false)
	state.sftpColumns.setEnabled(sftpColumnModified, false)
	canvas, texts := paint()
	for column := 1; column < 3; column++ {
		if _, ok := hitByID(canvas, sftpWindowSortHits[1].At(column)); ok {
			t.Fatalf("hidden column %d still has a window header", column)
		}
	}
	for _, value := range hiddenValues {
		if texts[value] {
			t.Fatalf("hidden column value %q still appears in a window row", value)
		}
	}

	state.sftpColumns.setEnabled(sftpColumnSize, true)
	state.sftpColumns.setEnabled(sftpColumnModified, true)
	canvas, texts = paint()
	for column := 1; column < 3; column++ {
		if _, ok := hitByID(canvas, sftpWindowSortHits[1].At(column)); !ok {
			t.Fatalf("shown column %d has no window header", column)
		}
	}
	for _, value := range hiddenValues {
		if !texts[value] {
			t.Fatalf("shown column value %q does not appear in the window row", value)
		}
	}
}

func TestSFTPUnavailableDefaultDirectoryFallsBackToThePicker(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	unavailable := filepath.Join(t.TempDir(), "not-a-folder")
	if err := os.WriteFile(unavailable, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.sftpDownloadDirectory = unavailable
	target := filepath.Join(t.TempDir(), "notes.txt")
	pickerCalls := 0
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) {
		pickerCalls++
		return target, nil
	}
	clickSFTPRow(t, state, session.files, "notes.txt")
	state.Click(hitSFTPDownload)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return len(view.transfers) == 1 && view.transfers[0].State == sftpclient.TransferCompleted
	})
	if pickerCalls != 1 {
		t.Fatalf("picker calls = %d", pickerCalls)
	}
	if len(state.fileNotices.items) != 1 || state.fileNotices.items[0].kind != components.ToastWarning {
		t.Fatalf("fallback notice = %+v", state.fileNotices.items)
	}
	if content, err := os.ReadFile(target); err != nil || string(content) != "hello world" {
		t.Fatalf("fallback download = %q, %v", content, err)
	}
}

func TestSFTPSidebarScrollbarDragReachesTheWholeListAtDifferentWindowSizes(t *testing.T) {
	state, session := openLargeSFTP(t, 500)
	view := session.files
	for _, size := range []gfx.Size{{Width: 900, Height: 600}, {Width: 1400, Height: 900}, {Width: 1900, Height: 1200}} {
		view.first, view.scrollOffset = 0, 0
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		geometry := view.scroll
		if geometry.VerticalTrack.Width != 10 || geometry.VerticalThumb.Height < 18 || geometry.MaxOffsetY <= 0 {
			t.Fatalf("%.0fx%.0f scrollbar geometry = %+v", size.Width, size.Height, geometry)
		}
		down := gfx.PointerEvent{
			Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hitSFTPListScrollbar,
			Y: geometry.VerticalThumb.Y + geometry.VerticalThumb.Height/2,
		}
		if result, handled := state.sftpListPointer(down); !handled || !result.Handled {
			t.Fatalf("%.0fx%.0f scrollbar press = %+v, handled=%t", size.Width, size.Height, result, handled)
		}
		move := down
		move.Kind, move.Captured = gfx.PointerMove, true
		move.Y = geometry.VerticalTrack.Y + geometry.VerticalTrack.Height/2
		if result, handled := state.sftpListPointer(move); !handled || !result.Handled {
			t.Fatalf("%.0fx%.0f scrollbar drag = %+v, handled=%t", size.Width, size.Height, result, handled)
		}
		up := move
		up.Kind, up.Captured = gfx.PointerUp, false
		if result, handled := state.sftpListPointer(up); !handled || !result.Handled {
			t.Fatalf("%.0fx%.0f scrollbar release = %+v, handled=%t", size.Width, size.Height, result, handled)
		}
		if view.first <= 0 || view.first >= len(view.rows)-view.visibleRows {
			t.Fatalf("%.0fx%.0f midpoint drag reached row %d of %d with %d visible", size.Width, size.Height, view.first, len(view.rows), view.visibleRows)
		}
	}
}

func TestSFTPSaveRefreshesTheShownDirectoryAndReportsSuccess(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := openSFTPTextEditor(t, state, session, "notes.txt")
	readsBefore := remote.readCount("/home/test")
	const saved = "saved and refreshed\n"
	replaceSFTPText(t, state, saved)
	state.Click(hitSFTPEditSave)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		if view.busy || sftpEditorDirty(view) || view.errorText != "" || len(state.fileNotices.items) != 1 {
			return false
		}
		// Listing completion may replace the transient status with a count.
		// Wait for the saved metadata, not just for the read to start.
		for _, row := range view.rows {
			if row.entry.Name == "notes.txt" {
				return row.entry.Size == int64(len(saved))
			}
		}
		return false
	})
	if remote.readCount("/home/test") <= readsBefore {
		t.Fatal("save did not refresh the shown directory")
	}
	notice := state.fileNotices.items[0]
	if notice.title != "notes.txt" || notice.detail != state.messages.SFTPEditSaved || notice.kind != components.ToastSuccess {
		t.Fatalf("save notice = %+v", notice)
	}
	if view.selected < 0 || view.selected >= len(view.rows) || view.rows[view.selected].entry.Name != "notes.txt" {
		t.Fatalf("refresh lost the selected file: selected=%d rows=%v", view.selected, view.rows)
	}
}

func TestSFTPRefreshAfterSaveWaitsForTheListingInFlight(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	// Keep a listing that started before the save in flight: its answer is
	// older than the save and must not be the last thing on screen.
	state.listSFTP(view, "/home/test")
	if !view.busy || view.navigation.path != "/home/test" {
		t.Fatalf("listing did not start: busy=%t navigation=%q", view.busy, view.navigation.path)
	}
	state.refreshSFTPAfterRemoteSave(view, "/home/test/notes.txt")
	if view.refreshAfterListing != "/home/test" {
		t.Fatalf("refresh was not deferred: %q", view.refreshAfterListing)
	}
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && view.refreshAfterListing == ""
	})
	if view.path != "/home/test" || len(view.rows) != 2 {
		t.Fatalf("view = %q rows=%d", view.path, len(view.rows))
	}
}
