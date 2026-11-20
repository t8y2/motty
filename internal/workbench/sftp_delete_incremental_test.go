package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A confirmed delete already says which entries left the folder, so the list
// drops those rows instead of reading the folder again: the re-read used to
// replace every row under the user's place and scroll position (issue #1104).
func TestSFTPDeleteDropsTheRowWithoutListingTheFolderAgain(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	before := remote.readCount("/home/test")

	clickSFTPRow(t, state, view, "notes.txt")
	state.Click(hitSFTPDelete)
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "notes.txt") < 0
	})
	if reads := remote.readCount("/home/test"); reads != before {
		t.Fatalf("delete listed the folder again: %d reads, want %d", reads, before)
	}
	if len(view.rows) != 1 || view.rows[0].entry.Name != "docs" {
		t.Fatalf("rows after delete = %+v", view.rows)
	}
	if selectedSFTPCount(view) != 0 {
		t.Fatalf("delete left %d rows selected", selectedSFTPCount(view))
	}
	if !strings.Contains(view.status, "/home/test/notes.txt") {
		t.Fatalf("delete status = %q", view.status)
	}
}

// A multi-delete drops every row its results confirmed; the folder is read
// again only when a removal stopped part-way, because then no result describes
// what the interruption left behind.
func TestSFTPMultiDeleteDropsTheRowsWithoutListingTheFolderAgain(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	before := remote.readCount("/home/test")

	clickSFTPRow(t, state, view, "notes.txt")
	clickSFTPRow(t, state, view, "docs")
	state.Click(hitSFTPDelete)
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && len(view.rows) == 0 && sftpRowIndexOrNegative(view, "docs") < 0
	})
	if reads := remote.readCount("/home/test"); reads != before {
		t.Fatalf("multi-delete listed the folder again: %d reads, want %d", reads, before)
	}
	if view.status == "" {
		t.Fatal("multi-delete left no status")
	}
}

// The local side behaves the same: the rows leave in place, and a local read
// that started before the deletion is too old to put them back.
func TestLocalDeleteDropsTheRowsWithoutListingTheFolderAgain(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localFixture(t)
	view := openLocalPane(t, state, session, dir)
	before := view.localSeq

	clickSFTPLocalRow(t, state, view, "report.txt")
	clickSFTPLocalRow(t, state, view, "photos")
	state.Click(hitSFTPDelete)
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.localBusy && sftpLocalRowIndexOrNegative(view, "report.txt") < 0 &&
			sftpLocalRowIndexOrNegative(view, "photos") < 0
	})
	// The delete retires an older read with one more sequence number; a
	// listing of the folder would start its own navigation and add another.
	if view.localSeq != before+1 {
		t.Fatalf("local delete read the folder again: seq %d, want %d", view.localSeq, before+1)
	}
	if len(view.lfolder.rows) != 1 || view.lfolder.rows[0].entry.Name != "Zeta.txt" {
		t.Fatalf("local rows after delete = %+v", view.lfolder.rows)
	}
	if _, err := os.Lstat(filepath.Join(dir, "report.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted file still on disk: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "photos")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted folder still on disk: %v", err)
	}
}
