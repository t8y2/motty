package workbench

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"motty/internal/configstore"
)

// linkedFixture is a state with one Markdown file linked as a note.
func linkedFixture(t *testing.T, content string) (*State, string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	s := newTestState(t, options)
	s.Click(hitNotes)
	s.Click(hitNoteLinkFile)
	if len(s.notes.list) != 1 || s.notes.list[0].Link == nil {
		t.Fatalf("the file was not linked: %+v", s.notes.list)
	}
	return s, s.notes.selected, path
}

// settle runs the window's wake until condition holds, as the window does when
// a watch or a background read asks for it.
func settle(t *testing.T, s *State, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s.Wake()
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLinkedNoteReadsAndWritesItsFile(t *testing.T) {
	s, id, path := linkedFixture(t, "# Plan\n")
	if got := s.EditorFor(hitNoteBody).String(); got != "# Plan\n" {
		t.Fatalf("body = %q", got)
	}
	s.EditorFor(hitNoteBody).Insert("more")
	s.save()
	if got := readFile(t, path); got != "# Plan\nmore" {
		t.Fatalf("the file holds %q after an edit", got)
	}
	if s.noteLinks[id].conflict || s.noteLinks[id].problem != "" {
		t.Fatalf("our own write was taken for a change: %+v", s.noteLinks[id])
	}
}

func TestLinkedNotePicksUpAnEditMadeElsewhere(t *testing.T) {
	for name, replace := range map[string]func(path, content string) error{
		"in place": func(path, content string) error { return os.WriteFile(path, []byte(content), 0o600) },
		"by rename": func(path, content string) error {
			temp := path + ".tmp"
			if err := os.WriteFile(temp, []byte(content), 0o600); err != nil {
				return err
			}
			return os.Rename(temp, path)
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, id, path := linkedFixture(t, "one")
			if err := replace(path, "two from outside"); err != nil {
				t.Fatal(err)
			}
			settle(t, s, "the editor to show the new text", func() bool {
				return s.EditorFor(hitNoteBody).String() == "two from outside"
			})
			if s.notes.list[s.noteIndex(id)].Body != "two from outside" || s.noteLinks[id].conflict {
				t.Fatalf("note not updated cleanly: %+v", s.notes.list[0])
			}
		})
	}
}

func TestLinkedNoteWithEditsKeepsBothSidesUntilChosen(t *testing.T) {
	s, id, path := linkedFixture(t, "base")
	s.EditorFor(hitNoteBody).Insert("mine ")
	if err := os.WriteFile(path, []byte("theirs"), 0o600); err != nil {
		t.Fatal(err)
	}
	settle(t, s, "the conflict to be noticed", func() bool { return s.noteLinks[id].conflict })
	s.save()
	if got := readFile(t, path); got != "theirs" {
		t.Fatalf("a conflicted save overwrote the file: %q", got)
	}
	if got := s.EditorFor(hitNoteBody).String(); got != "basemine " {
		t.Fatalf("the user's edit was replaced: %q", got)
	}
	if notice, ok := s.noteLinkNotice(id); !ok || len(notice.actions) != 3 {
		t.Fatalf("no choices offered: %+v", notice)
	}
	t.Run("keep mine", func(t *testing.T) {
		s.Click(hitNoteLinkOverwrite)
		if got := readFile(t, path); got != "basemine " || s.noteLinks[id].conflict {
			t.Fatalf("file %q, state %+v", got, s.noteLinks[id])
		}
	})
}

func TestLinkedNoteConflictLoadDiskAndFork(t *testing.T) {
	for _, choice := range []string{"load", "fork"} {
		t.Run(choice, func(t *testing.T) {
			s, id, path := linkedFixture(t, "base")
			s.EditorFor(hitNoteBody).Insert("mine ")
			if err := os.WriteFile(path, []byte("theirs"), 0o600); err != nil {
				t.Fatal(err)
			}
			settle(t, s, "the conflict", func() bool { return s.noteLinks[id].conflict })
			if choice == "load" {
				s.Click(hitNoteLinkLoad)
			} else {
				s.Click(hitNoteLinkFork)
			}
			if got := s.EditorFor(hitNoteBody).String(); got != "theirs" {
				t.Fatalf("body = %q, want the file's text", got)
			}
			if readFile(t, path) != "theirs" || s.noteLinks[id].conflict {
				t.Fatal("the file changed or the conflict stayed")
			}
			kept := 0
			for _, n := range s.notes.list {
				if n.Link == nil && n.Body == "basemine " {
					kept++
				}
			}
			if want := map[string]int{"load": 0, "fork": 1}[choice]; kept != want {
				t.Fatalf("%d library copies of the user's text, want %d", kept, want)
			}
		})
	}
}

func TestLinkedNoteSurvivesRestartAndStaysOutOfSharedRecords(t *testing.T) {
	s, id, path := linkedFixture(t, "text")
	s.EditorFor(hitNoteBody).Insert("kept ")
	s.save()
	records, err := s.config.Records()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Kind == "note" && record.ID == id {
			t.Fatal("a linked note was written as a shared record")
		}
		if bytes.Contains(record.Payload, []byte(filepath.Base(path))) {
			t.Fatalf("the path reached a shared %s record", record.Kind)
		}
	}
	// The file changed while the app was not running.
	if err := os.WriteFile(path, []byte("changed while closed"), 0o600); err != nil {
		t.Fatal(err)
	}
	restarted := newTestState(t, s.options)
	restarted.Click(hitNotes)
	if i := restarted.noteIndex(id); i < 0 || restarted.notes.list[i].Link == nil || restarted.notes.list[i].Link.Path != path {
		t.Fatalf("the link did not survive: %+v", restarted.notes.list)
	}
	settle(t, restarted, "the file's text", func() bool {
		i := restarted.noteIndex(id)
		return i >= 0 && restarted.notes.list[i].Body == "changed while closed"
	})
}

func TestLinkedNoteWhoseFileIsGoneCanBeLocatedOrConverted(t *testing.T) {
	s, id, path := linkedFixture(t, "precious")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	settle(t, s, "the missing file to be noticed", func() bool { return s.noteLinks[id].missing })
	if got := s.EditorFor(hitNoteBody).String(); got != "precious" {
		t.Fatalf("the note lost its text: %q", got)
	}
	s.EditorFor(hitNoteBody).Insert("edited ")
	s.save()
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a missing file was recreated behind the user's back")
	}
	moved := filepath.Join(t.TempDir(), "moved.md")
	if err := os.WriteFile(moved, []byte("preciousedited "), 0o600); err != nil {
		t.Fatal(err)
	}
	s.options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{moved}, nil }
	s.Click(hitNoteLinkLocate)
	if s.noteLinks[id].missing || s.noteLinkPath(id) != moved {
		t.Fatalf("not relocated: %+v path %q", s.noteLinks[id], s.noteLinkPath(id))
	}
	s.Click(hitNoteLinkUnlink)
	if s.noteLinkPath(id) != "" || s.notes.list[s.noteIndex(id)].Body != "preciousedited " {
		t.Fatalf("not converted with its text: %+v", s.notes.list)
	}
	s.save()
	records, err := s.config.Records()
	if err != nil {
		t.Fatal(err)
	}
	if !hasNoteRecord(records, id) {
		t.Fatal("a converted note is not a shared record")
	}
}

func hasNoteRecord(records []configstore.Record, id string) bool {
	for _, record := range records {
		if record.Kind == "note" && record.ID == id && !record.Deleted {
			return true
		}
	}
	return false
}

func TestLinkingTheSameFileTwiceSelectsTheExistingNote(t *testing.T) {
	s, id, _ := linkedFixture(t, "x")
	s.Click(hitNoteLinkFile)
	if len(s.notes.list) != 1 || s.notes.selected != id {
		t.Fatalf("a second note was made: %d notes", len(s.notes.list))
	}
}

func TestLinkingRefusesWhatItCannotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "binary.md")
	if err := os.WriteFile(bad, []byte{0xff, 0xfe, 'x'}, 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{bad}, nil }
	s := newTestState(t, options)
	s.Click(hitNotes)
	s.Click(hitNoteLinkFile)
	if len(s.notes.list) != 0 || s.notes.error == "" {
		t.Fatalf("an invalid file was linked: %+v / %q", s.notes.list, s.notes.error)
	}
}

func TestWriteLinkedFileKeepsBOMAndPermissionsAndLeavesNoTemporaries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.md")
	if err := os.WriteFile(path, []byte("\ufeffold"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := writeLinkedFile(path, "new\r\nline", true); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "\ufeffnew\r\nline" {
		t.Fatalf("content %q", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o640 {
			t.Fatalf("mode %v, %v", info.Mode().Perm(), err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("directory holds %v (%v)", entries, err)
	}
	file := readLinkedFile(path)
	if file.err != nil || !file.bom || strings.HasPrefix(file.body, "\ufeff") {
		t.Fatalf("read back %+v", file)
	}
}

func TestWriteLinkedFileFollowsASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symbolic links needs a privilege on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real.md")
	link := filepath.Join(dir, "link.md")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := writeLinkedFile(link, "new", false); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was replaced by a file")
	}
	if readFile(t, target) != "new" {
		t.Fatal("the file behind the link was not written")
	}
}

func TestUnlinkFromTheNoteMenuKeepsTheTextAndStopsFollowingTheFile(t *testing.T) {
	s, id, path := linkedFixture(t, "kept")
	s.notes.view.menu, s.notes.view.menuID, s.notes.view.menuFolder = "note", id, false
	s.Click(hitNoteLinkUnlink)
	if s.noteLinkPath(id) != "" || s.notes.list[s.noteIndex(id)].Body != "kept" {
		t.Fatalf("not unlinked with its text: %+v", s.notes.list)
	}
	if len(s.noteWatches) != 0 {
		t.Fatal("the directory is still watched")
	}
	s.EditorFor(hitNoteBody).Insert(" more")
	s.save()
	if got := readFile(t, path); got != "kept" {
		t.Fatalf("an unlinked note still wrote its file: %q", got)
	}
}
