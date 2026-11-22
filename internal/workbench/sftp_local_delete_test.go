package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

// The local walk reports each entry it finishes, so the status line can advance
// past "1 of n" the way the remote queue's count does.
func TestLocalDeleteWalkReportsPerItemProgress(t *testing.T) {
	dir := t.TempDir()
	entries := []sftpclient.Entry{{Name: "aaa.txt"}, {Name: "bbb.txt"}, {Name: "ccc.txt"}}
	for _, name := range []string{"aaa.txt", "bbb.txt", "ccc.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The last entry vanishes before the walk reaches it; a forced walk treats
	// that as removed and still reports it.
	if err := os.Remove(filepath.Join(dir, "ccc.txt")); err != nil {
		t.Fatal(err)
	}
	var seen []int
	result := removeLocalEntries(dir, entries, true, func(done int) { seen = append(seen, done) })
	if want := []int{1, 2, 3}; !slices.Equal(seen, want) {
		t.Fatalf("progress frames = %v, want %v", seen, want)
	}
	if len(result.removed) != 3 || len(result.failed) != 0 || result.err != nil {
		t.Fatalf("forced walk result = %+v", result)
	}
}

// localDeleteSettled reports a finished local deletion: nothing is walking the
// disk and the entry is gone.
func localDeleteSettled(view *sftpView, location string) bool {
	_, err := os.Lstat(location)
	return !view.localBusy && errors.Is(err, os.ErrNotExist)
}

func sftpLocalRowIndexOrNegative(view *sftpView, name string) int {
	for index := range view.lfolder.rows {
		if view.lfolder.rows[index].entry.Name == name {
			return index
		}
	}
	return -1
}

func TestLocalDeleteConfirmsSelectionAndRemovesFilesAndFolders(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localFixture(t)
	view := openLocalPane(t, state, session, dir)
	clickSFTPLocalRow(t, state, view, "report.txt")
	clickSFTPLocalRow(t, state, view, "photos")
	if !sftpMenuRowIDs(state.sftpMenuRows(session, view))[hitSFTPMenuDelete] {
		t.Fatal("a local selection has no delete action")
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, contextID(hitSFTPDelete)); !ok {
		t.Fatal("the local pane's delete button is not painted")
	}
	state.Click(hitSFTPDelete)
	if view.dialog != sftpDialogDelete || len(view.dialogEntries) != 2 || !view.local {
		t.Fatalf("local delete confirmation = dialog %v, entries %d, local %t", view.dialog, len(view.dialogEntries), view.local)
	}
	state.Click(hitSFTPDialogCancel)
	if _, err := os.Stat(filepath.Join(dir, "report.txt")); err != nil {
		t.Fatal("canceling the local delete removed a file")
	}
	if view.dialog != sftpDialogNone || view.localBusy {
		t.Fatalf("cancel left dialog=%v busy=%t", view.dialog, view.localBusy)
	}

	state.Click(hitSFTPDelete)
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.localBusy && sftpLocalRowIndexOrNegative(view, "report.txt") < 0 && sftpLocalRowIndexOrNegative(view, "photos") < 0
	})
	if _, err := os.Lstat(filepath.Join(dir, "report.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted file still on disk: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "photos", "a.jpg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted folder still held its contents: %v", err)
	}
	if view.status == "" || strings.Contains(view.status, "…") {
		t.Fatalf("local delete left the running status %q", view.status)
	}
	if _, err := os.Stat(filepath.Join(dir, "Zeta.txt")); err != nil {
		t.Fatal("an unselected local file was removed")
	}
}

func TestLocalDeleteRemovesASymlinkWithoutFollowingIt(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real", "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "link")); err != nil {
		t.Skipf("this platform cannot make the fixture's symlink: %v", err)
	}
	view := openLocalPane(t, state, session, dir)
	clickSFTPLocalRow(t, state, view, "link")
	state.Click(hitSFTPDelete)
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return localDeleteSettled(view, filepath.Join(dir, "link"))
	})
	if _, err := os.Stat(filepath.Join(dir, "real", "keep.txt")); err != nil {
		t.Fatalf("deleting a link emptied its target: %v", err)
	}
}

func TestLocalForceDeleteContinuesPastAMissingEntry(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := t.TempDir()
	for _, name := range []string{"aaa.txt", "bbb.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	view := openLocalPane(t, state, session, dir)
	clickSFTPLocalRow(t, state, view, "aaa.txt")
	clickSFTPLocalRow(t, state, view, "bbb.txt")
	state.Click(hitSFTPDelete)
	if err := os.Remove(filepath.Join(dir, "aaa.txt")); err != nil {
		t.Fatal(err)
	}
	chooseSFTPDeleteMethod(t, state, state.messages.SFTPDeleteMethodForce)
	state.Click(hitSFTPDialogSubmit)
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.localBusy })
	if _, err := os.Stat(filepath.Join(dir, "bbb.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("force delete stopped at the missing entry: %v", err)
	}
	if view.status == "" || strings.Contains(view.status, "…") {
		t.Fatalf("force local delete left the running status %q", view.status)
	}
}

func TestLocalDeleteStopsAtTheFirstFailureAndSaysSo(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := t.TempDir()
	for _, name := range []string{"aaa.txt", "bbb.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	view := openLocalPane(t, state, session, dir)
	clickSFTPLocalRow(t, state, view, "aaa.txt")
	clickSFTPLocalRow(t, state, view, "bbb.txt")
	state.Click(hitSFTPDelete)
	// The first entry is gone before the confirmation is accepted, so the walk
	// fails at it; what follows it must stay.
	if err := os.Remove(filepath.Join(dir, "aaa.txt")); err != nil {
		t.Fatal(err)
	}
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.localBusy })
	if _, err := os.Stat(filepath.Join(dir, "bbb.txt")); err != nil {
		t.Fatalf("a failed local delete continued past the failure: %v", err)
	}
	if len(state.fileNotices.items) == 0 ||
		!strings.Contains(state.fileNotices.items[0].detail, "aaa.txt") ||
		state.fileNotices.items[0].title != state.messages.SFTPDeleteSelectionTitle {
		t.Fatalf("local delete failure notice = %+v", state.fileNotices.items)
	}
	if strings.Contains(view.status, "…") {
		t.Fatalf("failed local delete left the running status %q", view.status)
	}
}
