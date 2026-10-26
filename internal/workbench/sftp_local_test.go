package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

func localFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "photos"), 0o755)
	os.WriteFile(filepath.Join(dir, "photos", "a.jpg"), []byte("jpg"), 0o644)
	os.WriteFile(filepath.Join(dir, "report.txt"), []byte("local report"), 0o644)
	os.WriteFile(filepath.Join(dir, "Zeta.txt"), []byte("z"), 0o644)
	return dir
}

func openLocalPane(t *testing.T, state *State, session *Session, dir string) *sftpView {
	t.Helper()
	view := session.files
	view.lfolder.path = dir
	state.Click(hitSFTPPaneToggle)
	if !view.local || view.lfolder.path != dir {
		t.Fatalf("local %v path %q", view.local, view.lfolder.path)
	}
	return view
}

func TestLocalPaneListsThisComputerFoldersFirstAndKeepsTheServersListing(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localFixture(t)
	view := openLocalPane(t, state, session, dir)
	names := []string{}
	for _, row := range view.lfolder.rows {
		names = append(names, row.entry.Name)
	}
	if len(names) != 3 || names[0] != "photos" || names[1] != "report.txt" || names[2] != "Zeta.txt" {
		t.Fatalf("local rows = %v", names)
	}
	if len(view.rows) != 2 || view.path != "/home/test" {
		t.Fatalf("the server's listing changed: %d rows at %q", len(view.rows), view.path)
	}
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, contextID(hitSFTPPaneToggle)); !ok {
		t.Fatal("no way back to the server's files")
	}
	if _, ok := hitByID(&canvas, contextID(hitSFTPUpload)); ok {
		t.Fatal("the remote-only upload button is shown for local files")
	}
	// Back again: the server's list is as it was.
	state.Click(hitSFTPPaneToggle)
	if view.local || len(view.rows) != 2 {
		t.Fatalf("local %v rows %d", view.local, len(view.rows))
	}
}

func TestLocalPaneBrowsesIntoFoldersAndUp(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localFixture(t)
	view := openLocalPane(t, state, session, dir)
	clickSFTPLocalRow(t, state, view, "photos")
	state.Click(hitSFTPOpenEntry)
	if view.lfolder.path != filepath.Join(dir, "photos") || len(view.lfolder.rows) != 1 || view.lfolder.rows[0].entry.Name != "a.jpg" {
		t.Fatalf("after opening: %q %v", view.lfolder.path, view.lfolder.rows)
	}
	if len(view.lfolder.crumbs) == 0 || view.lfolder.crumbs[len(view.lfolder.crumbs)-1].label != "photos" {
		t.Fatalf("crumbs = %+v", view.lfolder.crumbs)
	}
	state.Click(hitSFTPUp)
	if view.lfolder.path != dir {
		t.Fatalf("up went to %q", view.lfolder.path)
	}
	state.Click(hitSFTPBreadcrumbList.At(0)) // the root crumb
	if view.lfolder.path != filepath.VolumeName(dir)+string(filepath.Separator) {
		t.Fatalf("root crumb went to %q", view.lfolder.path)
	}
}

func clickSFTPLocalRow(t *testing.T, state *State, view *sftpView, name string) {
	t.Helper()
	for index := range view.lfolder.rows {
		if view.lfolder.rows[index].entry.Name == name {
			state.Click(hitSFTPEntryList.At(index - view.lfolder.first))
			return
		}
	}
	t.Fatalf("no local row %q", name)
}

func TestLocalSelectionUploadsIntoTheServersCurrentFolder(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	dir := localFixture(t)
	view := openLocalPane(t, state, session, dir)
	clickSFTPLocalRow(t, state, view, "report.txt")
	clickSFTPLocalRow(t, state, view, "photos")
	state.Click(hitSFTPSend)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		if len(view.transfers) != 2 {
			return false
		}
		for _, transfer := range view.transfers {
			if transfer.State != sftpclient.TransferCompleted {
				return false
			}
		}
		return true
	})
	remote.mu.Lock()
	report, photo := string(remote.files["/home/test/report.txt"]), string(remote.files["/home/test/photos/a.jpg"])
	remote.mu.Unlock()
	if report != "local report" || photo != "jpg" {
		t.Fatalf("remote has report %q, photo %q", report, photo)
	}
	// Back on the server's side the new files are listed.
	state.Click(hitSFTPPaneToggle)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		for _, row := range view.rows {
			if row.entry.Name == "report.txt" {
				return true
			}
		}
		return false
	})
}

func TestServerSelectionDownloadsIntoTheLocalFolderOnShow(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localFixture(t)
	view := openLocalPane(t, state, session, dir)
	state.Click(hitSFTPPaneToggle) // back to the server, whose download can now go "here"
	clickSFTPRow(t, state, view, "notes.txt")
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, contextID(hitSFTPDownloadHere)); !ok {
		t.Fatal("no Download here button though the local side has been opened")
	}
	state.Click(hitSFTPDownloadHere)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return len(view.transfers) == 1 && view.transfers[0].State == sftpclient.TransferCompleted
	})
	if got, _ := os.ReadFile(filepath.Join(dir, "notes.txt")); string(got) != "hello world" {
		t.Fatalf("downloaded %q", got)
	}
}

func TestLocalNewFolderIsMadeOnThisComputer(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	dir := localFixture(t)
	view := openLocalPane(t, state, session, dir)
	state.Click(hitSFTPNewDirectory)
	if view.dialog != sftpDialogCreateDirectory {
		t.Fatalf("dialog = %d", view.dialog)
	}
	replaceSFTPDialogText(t, state, "../escape")
	state.Click(hitSFTPDialogSubmit)
	if view.dialog == sftpDialogNone {
		t.Fatal("a name with a slash was accepted")
	}
	replaceSFTPDialogText(t, state, "fresh")
	state.Click(hitSFTPDialogSubmit)
	if info, err := os.Stat(filepath.Join(dir, "fresh")); err != nil || !info.IsDir() {
		t.Fatalf("no local folder: %v", err)
	}
	if view.dialog != sftpDialogNone || len(view.lfolder.rows) != 4 {
		t.Fatalf("dialog %d rows %d", view.dialog, len(view.lfolder.rows))
	}
	remote.mu.Lock()
	_, onServer := remote.entries["/home/test/fresh"]
	remote.mu.Unlock()
	if onServer {
		t.Fatal("the folder was made on the server")
	}
}

func TestLocalPaneSteadyPaintDoesNotAllocate(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := openLocalPane(t, state, session, localFixture(t))
	clickSFTPLocalRow(t, state, view, "report.txt")
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	if allocations := testingAllocs(func() { canvas.Reset(); state.Paint(&canvas, size) }); allocations != 0 {
		t.Fatalf("local pane paint allocations = %v", allocations)
	}
}

func testingAllocs(fn func()) float64 { return testing.AllocsPerRun(100, fn) }

func TestLocalPaneSortsNamesWithoutRegardToCase(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := t.TempDir()
	for _, name := range []string{"b.txt", "A.txt", "C.txt", "d.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	view := openLocalPane(t, state, session, dir)
	names := make([]string, 0, len(view.lfolder.rows))
	for _, row := range view.lfolder.rows {
		names = append(names, row.entry.Name)
	}
	want := []string{"A.txt", "b.txt", "C.txt", "d.txt"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("local rows = %v, want %v", names, want)
	}
}

func localManyFiles(t *testing.T, count int) string {
	t.Helper()
	dir := t.TempDir()
	for index := 0; index < count; index++ {
		name := filepath.Join(dir, fmt.Sprintf("file-%05d.txt", index))
		if err := os.WriteFile(name, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func waitLocalRows(t *testing.T, state *State, view *sftpView, count int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(view.lfolder.rows) != count && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	if len(view.lfolder.rows) != count {
		t.Fatalf("local rows = %d, want %d (status %q)", len(view.lfolder.rows), count, view.status)
	}
}

func TestALargeLocalListingIsScannedInTheBackground(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localManyFiles(t, localListingInlineEntries+64)
	view := session.files
	view.lfolder.path = dir
	state.Click(hitSFTPPaneToggle)
	if !view.local {
		t.Fatal("the panel did not switch to local files")
	}
	if view.status != state.messages.SFTPLoading {
		t.Fatalf("status = %q, want the loading state", view.status)
	}
	if len(view.lfolder.rows) != 0 {
		t.Fatalf("large listing was already applied: %d rows", len(view.lfolder.rows))
	}
	waitLocalRows(t, state, view, localListingInlineEntries+64)
	if want := fmt.Sprintf(state.messages.SFTPItemsFmt, localListingInlineEntries+64); view.status != want {
		t.Fatalf("status = %q, want %q", view.status, want)
	}
	if got := view.lfolder.rows[0].entry.Name; got != "file-00000.txt" {
		t.Fatalf("first row = %q", got)
	}
}

func TestAnOvertakenLocalListingIsDropped(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	big := localManyFiles(t, localListingInlineEntries+64)
	small := localManyFiles(t, 2)
	view := session.files
	view.lfolder.path = big
	state.Click(hitSFTPPaneToggle)
	if view.status != state.messages.SFTPLoading {
		t.Fatalf("status = %q, want loading", view.status)
	}
	// A newer, small listing finishes synchronously and must win over the large
	// one still being scanned.
	state.listLocal(view, small)
	if view.lfolder.path != small || len(view.lfolder.rows) != 2 {
		t.Fatalf("small listing = %q with %d rows", view.lfolder.path, len(view.lfolder.rows))
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
		if len(view.lfolder.rows) > 2 || view.lfolder.path != small {
			t.Fatalf("the overtaken listing landed: %q with %d rows", view.lfolder.path, len(view.lfolder.rows))
		}
	}
}
