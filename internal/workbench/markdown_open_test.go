package workbench

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkdownLaunchPathsNeedExistingMarkdownFiles(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "notes.md")
	second := filepath.Join(dir, "runbook.markdown")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("# Notes\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, ok := markdownLaunchPaths([]string{"--background", first, second})
	if !ok || len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("markdown launch = %q, %v", got, ok)
	}
	if _, ok := markdownLaunchPaths([]string{filepath.Join(dir, "missing.md")}); ok {
		t.Fatal("a missing Markdown file was treated as an open request")
	}
	if _, ok := markdownLaunchPaths([]string{filepath.Join(dir, "notes.txt")}); ok {
		t.Fatal("another file type was treated as a Markdown open request")
	}
	if _, ok := markdownLaunchPaths([]string{"alice@example.com"}); ok {
		t.Fatal("a connection target was treated as a Markdown file")
	}
}

func TestOpeningMarkdownFromTheSystemLinksAndSelectsANote(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "release-notes.md")
	want := "# Release\n\n- fixed\n"
	if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}
	state, world, raised := launchState(t)
	state.Launch([]string{path})
	state.Wake()
	if state.activeSection != sectionNotes || len(state.notes.list) != 1 {
		t.Fatalf("section %v notes %+v", state.activeSection, state.notes.list)
	}
	note := state.notes.list[0]
	if note.Link == nil || note.Link.Path != path || note.Body != want || state.notes.selected != note.ID {
		t.Fatalf("opened note = %+v, selected %q", note, state.notes.selected)
	}
	if len(world.configs) != 0 {
		t.Fatalf("opening a note dialed %d connections", len(world.configs))
	}
	if *raised != 1 {
		t.Fatalf("the window was raised %d times", *raised)
	}

	state.Launch([]string{path})
	state.Wake()
	if len(state.notes.list) != 1 || state.notes.selected != note.ID {
		t.Fatalf("opening the same file made another note: %+v", state.notes.list)
	}
}
