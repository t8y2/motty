package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/text"
)

func TestAINoteSearchAliasesAndTypeFilters(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.notes.list = []savedNote{{ID: "n", Title: "Docker", Tags: []string{"Docker", encodeNoteHost(noteHost{CatalogID: "h", Label: "Docker"}), encodeNoteHostGroup("Docker")}}}
	s.notes.folders = []savedNoteFolder{{ID: "f", Name: "Docker"}}
	for _, query := range []string{"@notedocker", "@note:DOCKER", "@notecker"} {
		s.ai.input = text.Editor{}
		s.ai.input.Insert(query)
		var buf [16]mentionName
		names, _ := s.mentionSuggestions(buf[:0])
		kinds := map[string]bool{}
		for _, name := range names {
			kinds[name.noteReference.Kind] = true
		}
		for _, kind := range []string{"note", "tag", "folder", "host", "host_group"} {
			if !kinds[kind] {
				t.Fatalf("%s did not find %s: %+v", query, kind, names)
			}
		}
	}
	for _, kind := range []string{"tag", "folder", "host", "host_group"} {
		s.ai.input = text.Editor{}
		s.ai.input.Insert("@note:" + kind + ":docker")
		var buf [16]mentionName
		names, _ := s.mentionSuggestions(buf[:0])
		if len(names) != 1 || names[0].noteReference.Kind != kind {
			t.Fatalf("%s filter leaked types: %+v", kind, names)
		}
	}
	s.ai.input = text.Editor{}
	s.ai.input.Insert("@notedkr")
	var buf [16]mentionName
	if names, _ := s.mentionSuggestions(buf[:0]); len(names) != 0 {
		t.Fatal("unexpected skipped-letter matching")
	}
}

func TestAINoteSearchRanksBeforeLimitingAndInsertsCanonicalToken(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	for i := 0; i < 8; i++ {
		s.notes.list = append(s.notes.list, savedNote{ID: noteID(), Title: "my Docker experience"})
	}
	s.notes.list = append(s.notes.list, savedNote{ID: "prefix", Title: "Docker guide", Tags: []string{"Docker"}})
	s.ai.input.Insert("@notedocker")
	var buf [16]mentionName
	names, _ := s.mentionSuggestions(buf[:0])
	if len(names) < 3 || names[0].name != "note:tag:Docker" || names[1].noteReference.ID != "prefix" || !strings.HasPrefix(names[2].title, "my ") {
		t.Fatalf("incorrect ranking: %+v", names)
	}
	var limited [1]mentionName
	top, _ := s.mentionSuggestions(limited[:0])
	if len(top) != 1 || top[0].name != "note:tag:Docker" {
		t.Fatal("limit hid the exact tag match")
	}
	s.acceptMention(top[0].name)
	if s.ai.input.String() != "@note:tag:Docker " {
		t.Fatalf("noncanonical insertion: %s", s.ai.input.String())
	}
	refs := s.aiNoteReferences(s.ai.input.String())
	if len(refs) != 1 || refs[0].Kind != "tag" || refs[0].ID != "Docker" {
		t.Fatalf("tag identity lost: %+v", refs)
	}
}

func TestAINoteSearchPathsAndNormalizedLabels(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.notes.folders = []savedNoteFolder{{ID: "a", Name: "A"}, {ID: "b", Name: "Docker Guide", Parent: "a"}}
	for _, query := range []string{"@noteA/Docker", "@note:folder:A/Docker-Guide", "@note:folder"} {
		s.ai.input = text.Editor{}
		s.ai.input.Insert(query)
		var buf [8]mentionName
		names, _ := s.mentionSuggestions(buf[:0])
		found := false
		for _, name := range names {
			if name.noteReference.Kind == "folder" && name.noteReference.ID == "b" {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s path search failed: %+v", query, names)
		}
	}
	s.ai.input = text.Editor{}
	s.ai.input.Insert("@terminal/path")
	if _, _, ok := s.mentionQuery(); ok {
		t.Fatal("note alias grammar affected terminal references")
	}
}

func TestAINoteSearchKeepsNotePrefixedTerminals(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.notes.list = []savedNote{{ID: "n", Title: "Docker", Tags: []string{"Docker"}}}
	for i, title := range []string{"notebook", "note-prod", "notedocker", "note:tag:Docker"} {
		s.sessions = append(s.sessions, Session{ID: uint32(i + 1), Title: title, term: &terminal{}})
	}
	for _, name := range s.mentionNames(nil) {
		s.ai.input = text.Editor{}
		s.ai.input.Insert("@" + name.name)
		var buf [8]mentionName
		choices, _ := s.mentionSuggestions(buf[:0])
		if len(choices) == 0 || choices[0].session != name.session {
			t.Fatalf("terminal hidden: %s %+v", name.name, choices)
		}
		if name.name == "notedocker" {
			found := false
			for _, candidate := range choices {
				if candidate.noteReference != nil && candidate.noteReference.Kind == "tag" {
					found = true
				}
			}
			if !found {
				t.Fatal("terminal matching hid fuzzy note results")
			}
		}
		s.acceptMention(choices[0].name)
		ids := findMentions(s.ai.input.String(), s.mentionNames(nil))
		if len(ids) != 1 || ids[0] != name.session || len(s.aiNoteReferences(s.ai.input.String())) != 0 {
			t.Fatalf("terminal reference changed identity: %s", s.ai.input.String())
		}
	}
}
