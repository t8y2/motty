package workbench

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// folderFixture is a state with a directory of Markdown files linked.
func folderFixture(t *testing.T, files map[string]string) (*State, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "vault")
	writeTree(t, root, files)
	options := freshOptions(t)
	options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return root, nil }
	s := newTestState(t, options)
	s.Click(hitNotes)
	s.Click(hitNoteLinkFolder)
	settle(t, s, "the folder to be read", func() bool { return len(s.notes.list) >= len(mdCount(files)) })
	return s, root
}

func mdCount(files map[string]string) []string {
	var out []string
	for rel := range files {
		if strings.HasSuffix(rel, ".md") && !strings.HasPrefix(rel, ".") && !strings.HasPrefix(rel, "node_modules/") {
			out = append(out, rel)
		}
	}
	return out
}

func noteByTitle(s *State, title string) (savedNote, bool) {
	for _, n := range s.notes.list {
		if n.Title == title {
			return n, true
		}
	}
	return savedNote{}, false
}

func TestLinkFolderMirrorsMarkdownFilesAndFolders(t *testing.T) {
	s, root := folderFixture(t, map[string]string{
		"a.md":              "alpha",
		"sub/b.md":          "beta",
		"sub/deep/c.md":     "gamma",
		"readme.txt":        "not markdown",
		".git/ignored.md":   "hidden",
		"node_modules/x.md": "dependency",
	})
	if len(s.notes.list) != 3 {
		t.Fatalf("%d notes, want the three Markdown files outside skipped directories", len(s.notes.list))
	}
	rootFolder := s.noteFolderRoot(root)
	if rootFolder == nil || rootFolder.Name != "vault" {
		t.Fatalf("no root folder: %+v", s.notes.folders)
	}
	b, _ := noteByTitle(s, "b")
	sub, ok := s.noteFolder(b.Folder)
	if !ok || sub.Name != "sub" || sub.Parent != rootFolder.ID {
		t.Fatalf("b is in %+v", sub)
	}
	c, _ := noteByTitle(s, "c")
	if deep, _ := s.noteFolder(c.Folder); deep.Name != "deep" || deep.Parent != sub.ID {
		t.Fatalf("c is in %+v", deep)
	}
	if b.Link == nil || b.Link.Tree != root || b.Link.Path != filepath.Join(root, "sub", "b.md") {
		t.Fatalf("link = %+v", b.Link)
	}
}

func TestLinkedFolderFollowsFilesAppearingChangingAndMoving(t *testing.T) {
	s, root := folderFixture(t, map[string]string{"a.md": "alpha", "keep.md": "unchanged text"})
	writeTree(t, root, map[string]string{"new/fresh.md": "just arrived"})
	settle(t, s, "the new file to become a note", func() bool { _, ok := noteByTitle(s, "fresh"); return ok })
	fresh, _ := noteByTitle(s, "fresh")
	if f, _ := s.noteFolder(fresh.Folder); f.Name != "new" {
		t.Fatalf("fresh is in %+v", f)
	}

	writeTree(t, root, map[string]string{"a.md": "alpha edited elsewhere"})
	settle(t, s, "the edit", func() bool { n, _ := noteByTitle(s, "a"); return n.Body == "alpha edited elsewhere" })

	// A rename keeps the note, and with it what Motty holds about it.
	keep, _ := noteByTitle(s, "keep")
	s.notes.list[s.noteIndex(keep.ID)].Tags = []string{"important"}
	if err := os.Rename(filepath.Join(root, "keep.md"), filepath.Join(root, "renamed.md")); err != nil {
		t.Fatal(err)
	}
	settle(t, s, "the note to follow its renamed file", func() bool {
		i := s.noteIndex(keep.ID)
		return i >= 0 && s.notes.list[i].Link.Path == filepath.Join(root, "renamed.md") && !s.noteLinks[keep.ID].missing
	})
	if got := s.notes.list[s.noteIndex(keep.ID)]; !slices.Equal(got.Tags, []string{"important"}) || len(s.notes.list) != 3 {
		t.Fatalf("rename lost metadata or made a copy: %+v (%d notes)", got, len(s.notes.list))
	}
}

func TestLinkedFolderKeepsANoteWhoseFileWasDeleted(t *testing.T) {
	s, root := folderFixture(t, map[string]string{"a.md": "alpha", "b.md": "beta"})
	a, _ := noteByTitle(s, "a")
	if err := os.Remove(filepath.Join(root, "a.md")); err != nil {
		t.Fatal(err)
	}
	settle(t, s, "the deletion to be noticed", func() bool { return s.noteLinks[a.ID] != nil && s.noteLinks[a.ID].missing })
	got := s.notes.list[s.noteIndex(a.ID)]
	if got.Body != "alpha" {
		t.Fatalf("the note lost its text: %q", got.Body)
	}
	if _, ok := noteByTitle(s, "b"); !ok || s.noteLinks[s.notes.list[s.noteIndex(a.ID)+1].ID].missing {
		t.Fatal("an untouched note was affected")
	}
}

func TestLinkedFolderStaysOutOfSharedRecordsAndSurvivesRestart(t *testing.T) {
	s, root := folderFixture(t, map[string]string{"a.md": "alpha", "sub/b.md": "beta"})
	s.save()
	records, err := s.config.Records()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if (r.Kind == "note" || r.Kind == "note-folder") && !r.Deleted {
			t.Fatalf("a linked item became a shared %s record: %s", r.Kind, r.Payload)
		}
		if strings.Contains(string(r.Payload), "vault") {
			t.Fatalf("the path reached a shared %s record", r.Kind)
		}
	}
	// A file added while the app is closed is found at the next start.
	writeTree(t, root, map[string]string{"while-closed.md": "hello"})
	restarted := newTestState(t, s.options)
	restarted.Click(hitNotes)
	if restarted.noteFolderRoot(root) == nil || len(restarted.notes.list) < 2 {
		t.Fatalf("the linked folder did not come back: %+v", restarted.notes.folders)
	}
	settle(t, restarted, "the file added while closed", func() bool { _, ok := noteByTitle(restarted, "while-closed"); return ok })
	if len(restarted.notes.list) != 3 {
		t.Fatalf("%d notes after restart, want 3 without duplicates", len(restarted.notes.list))
	}
}

func TestDeletingAMemberNoteLeavesItsFileAndItStaysGone(t *testing.T) {
	s, root := folderFixture(t, map[string]string{"a.md": "alpha", "b.md": "beta"})
	a, _ := noteByTitle(s, "a")
	s.selectNote(a.ID)
	s.Click(hitNoteDelete)
	s.Click(hitNoteConfirm)
	if _, ok := noteByTitle(s, "a"); ok {
		t.Fatal("the note was not deleted")
	}
	if _, err := os.Stat(filepath.Join(root, "a.md")); err != nil {
		t.Fatalf("the file was deleted: %v", err)
	}
	s.probeNoteFolder(root, nil, true)
	settle(t, s, "the directory to be read again", func() bool { return !s.noteScans[root].running })
	if _, ok := noteByTitle(s, "a"); ok {
		t.Fatal("the deleted note came back")
	}
}

func TestNewNoteInALinkedFolderMakesAFile(t *testing.T) {
	s, root := folderFixture(t, map[string]string{"a.md": "alpha"})
	s.notes.folder = s.noteFolderRoot(root).ID
	s.Click(hitNoteAdd)
	n := s.notes.list[s.noteIndex(s.notes.selected)]
	if n.Link == nil || n.Link.Path != filepath.Join(root, "Untitled.md") {
		t.Fatalf("no file for the new note: %+v", n.Link)
	}
	if _, err := os.Stat(n.Link.Path); err != nil {
		t.Fatal(err)
	}
	s.EditorFor(hitNoteBody).Insert("typed in Motty")
	s.save()
	if got := readFile(t, n.Link.Path); got != "typed in Motty" {
		t.Fatalf("the file holds %q", got)
	}
	s.Click(hitNoteAdd)
	if second := s.notes.list[s.noteIndex(s.notes.selected)]; second.Link == nil || second.Link.Path != filepath.Join(root, "Untitled 2.md") {
		t.Fatalf("second new note: %+v", second.Link)
	}
}

func TestUnlinkFolderKeepsEverythingAsLibraryNotes(t *testing.T) {
	s, root := folderFixture(t, map[string]string{"a.md": "alpha", "sub/b.md": "beta"})
	s.noteFolderUnlink(s.noteFolderRoot(root).ID)
	for _, n := range s.notes.list {
		if n.Link != nil {
			t.Fatalf("%q is still linked", n.Title)
		}
	}
	for _, f := range s.notes.folders {
		if f.Link != nil {
			t.Fatalf("folder %q is still linked", f.Name)
		}
	}
	if len(s.notes.list) != 2 || len(s.noteTreeWatches) != 0 {
		t.Fatalf("%d notes, %d tree watches", len(s.notes.list), len(s.noteTreeWatches))
	}
	writeTree(t, root, map[string]string{"after.md": "x"})
	s.probeNoteFolder(root, nil, true)
	if _, ok := noteByTitle(s, "after"); ok {
		t.Fatal("an unlinked folder still adds notes")
	}
}

func TestLinkFolderRefusesOverlapAndUnreadableFiles(t *testing.T) {
	s, root := folderFixture(t, map[string]string{"good.md": "ok"})
	if err := os.WriteFile(filepath.Join(root, "binary.md"), []byte{0xff, 0xfe, 'x'}, 0o600); err != nil {
		t.Fatal(err)
	}
	s.probeNoteFolder(root, nil, true)
	settle(t, s, "the read", func() bool { return !s.noteScans[root].running && s.notes.error != "" })
	if len(s.notes.list) != 1 || !strings.Contains(s.notes.error, "left out") {
		t.Fatalf("%d notes, notice %q", len(s.notes.list), s.notes.error)
	}
	s.notes.error = ""
	s.options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return filepath.Join(root, "inner"), nil }
	os.MkdirAll(filepath.Join(root, "inner"), 0o700)
	s.Click(hitNoteLinkFolder)
	if s.notes.error == "" || len(s.noteFoldersLinked()) != 1 {
		t.Fatalf("an overlapping folder was linked (%q)", s.notes.error)
	}
}

func (s *State) noteFoldersLinked() []savedNoteFolder {
	var roots []savedNoteFolder
	for _, f := range s.notes.folders {
		if f.Link != nil && f.Link.Root {
			roots = append(roots, f)
		}
	}
	return roots
}

func TestLinkedFolderMemberEditsWriteBackAndConflictLikeLinkedFiles(t *testing.T) {
	s, root := folderFixture(t, map[string]string{"sub/a.md": "base"})
	a, _ := noteByTitle(s, "a")
	s.selectNote(a.ID)
	s.EditorFor(hitNoteBody).Insert("mine ")
	if err := os.WriteFile(filepath.Join(root, "sub", "a.md"), []byte("theirs"), 0o600); err != nil {
		t.Fatal(err)
	}
	settle(t, s, "the conflict", func() bool { return s.noteLinks[a.ID].conflict })
	s.Click(hitNoteLinkLoad)
	if got := s.EditorFor(hitNoteBody).String(); got != "theirs" {
		t.Fatalf("body = %q", got)
	}
}
