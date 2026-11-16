package workbench

import (
	"github.com/vivaui/viva/gfx"
	"testing"
)

func TestNotesSearchTagsAndDraft(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	note := savedNote{ID: "saved", Title: "Release", Body: "Rollback instructions", Tags: []string{"Operations", encodeNoteHost(noteHost{CatalogID: "internal-id", Label: "Database", Address: "db.example", User: "Operator"}), encodeNoteHostGroup("生产环境/数据库"), "{@HostGroup:invalid"}}
	for _, q := range []string{"release", "ROLLBACK", "OPERATIONS", "database", "DB.EXAMPLE", "operator", "生产环境/数据库", "{@HostGroup:invalid", ""} {
		if !s.noteMatchesSearch(note, q) {
			t.Fatalf("missing match %q", q)
		}
	}
	for _, q := range []string{"internal-id", "@Address:", "%E7%94%9F", "no-match"} {
		if s.noteMatchesSearch(note, q) {
			t.Fatalf("unexpected match %q", q)
		}
	}
	s.notes.selected = note.ID
	s.notes.title.Insert("Draft title")
	s.notes.body.Insert("Draft body")
	s.replaceNoteTagEditor([]string{"Unsaved"})
	for _, q := range []string{"draft title", "draft body", "unsaved"} {
		if !s.noteMatchesSearch(note, q) {
			t.Fatalf("draft not matched %q", q)
		}
	}
	for _, q := range []string{"release", "rollback", "operations"} {
		if s.noteMatchesSearch(note, q) {
			t.Fatalf("old value still matched %q", q)
		}
	}
	s.replaceNoteTagEditor(nil)
	if s.noteMatchesSearch(note, "unsaved") {
		t.Fatal("removed draft tag still matched")
	}
}

func TestNotesSearchOrTerms(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	note := savedNote{ID: "saved", Title: "Release", Body: "Rollback instructions", Tags: []string{"Operations", encodeNoteHost(noteHost{CatalogID: "internal-id", Label: "Database"}), encodeNoteHostGroup("生产环境/数据库")}}
	for _, test := range []struct {
		query string
		want  bool
	}{
		{"missing|RELEASE|other", true},
		{"missing | Rollback instructions |", true},
		{"missing|operations", true},
		{"missing|database", true},
		{"missing|生产环境", true},
		{"missing|internal-id", false},
		{"missing|other", false},
		{"missing|| ", false},
		{"| |", true},
		{"rollback release|missing", false},
		{" release ", false},
	} {
		if got := s.noteMatchesSearch(note, test.query); got != test.want {
			t.Errorf("query %q: got %v, want %v", test.query, got, test.want)
		}
	}
}

func TestNotesTagSearchKeepsAncestorsAndOrder(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.activeSection = sectionNotes
	s.restoreNotes([]savedNote{{ID: "first", Title: "First", Folder: "child", Tags: []string{"needle"}, Order: 2}, {ID: "pinned", Title: "Pinned", Folder: "child", Tags: []string{"NEEDLE"}, Pinned: true}, {ID: "other", Title: "Other"}}, []savedNoteFolder{{ID: "parent", Name: "Parent"}, {ID: "child", Name: "Child", Parent: "parent"}})
	s.notes.query.Insert("needle")
	s.notes.view.searchOpen = true
	var c gfx.Canvas
	s.Paint(&c, gfx.Size{Width: 1400, Height: 800})
	var labels []string
	for _, h := range c.Hits() {
		if _, ok := h.ID.In(hitNoteRows); ok {
			labels = append(labels, h.Label)
		}
	}
	if len(labels) != 2 || labels[0] != "Pinned" || labels[1] != "First" {
		t.Fatalf("search order %v", labels)
	}
	for _, id := range []string{"parent", "child"} {
		if _, ok := hitByID(&c, hitNoteFolderRows.At(s.notes.view.folderTokens[id])); !ok {
			t.Fatalf("missing ancestor %s", id)
		}
	}
	h, ok := hitByID(&c, hitNoteSearch)
	if !ok || h.Label != s.noteText("Search title, body and tags", "搜索标题、正文和标签") {
		t.Fatalf("search placeholder %+v", h)
	}
}
