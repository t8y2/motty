package workbench

import (
	"encoding/json"
	"github.com/vivaui/viva/text"
	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
	"strings"
	"testing"
)

func TestAINoteTagCandidatesPreserveTypedIdentities(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.notes.list = []savedNote{{ID: "one", Title: "one", Tags: []string{"shared", encodeNoteHost(noteHost{CatalogID: "saved-host", Label: "shared"}), encodeNoteHostGroup("shared"), encodeNoteHostGroup("Prod/DB")}}, {ID: "two", Tags: []string{encodeNoteHost(noteHost{CatalogID: "saved-host", Label: "older label"})}}}
	refs := s.noteTagReferences()
	if len(refs) != 4 {
		t.Fatalf("tags not deduplicated by identity: %+v", refs)
	}
	ordinary := s.noteTagCandidates()
	if len(ordinary) != 1 || ordinary[0] != "shared" {
		t.Fatal("ordinary tag editor candidates changed")
	}
	seen := map[string]bool{}
	for _, candidate := range s.noteMentionNames() {
		r := candidate.noteReference
		if r.Kind == "note" {
			continue
		}
		if !strings.HasPrefix(candidate.name, noteReferencePrefix(r.Kind)) || strings.ContainsAny(candidate.name, "{}") {
			t.Fatalf("invalid tag syntax: %s", candidate.name)
		}
		if seen[candidate.name] {
			t.Fatal("ambiguous candidate tokens")
		}
		seen[candidate.name] = true
		s.aiRememberNoteMention(candidate.name)
		found := s.aiNoteReferences("重点关注 @" + candidate.name)
		if len(found) != 1 || found[0].Kind != r.Kind || found[0].ID != r.ID {
			t.Fatalf("reference identity lost: %+v", found)
		}
	}
	// Types have independent namespaces, so equal labels are unambiguous.
	ordinaryRef := s.aiNoteReferences("@note:tag:shared")
	if len(ordinaryRef) != 1 || ordinaryRef[0].Ambiguous || ordinaryRef[0].Kind != "tag" || ordinaryRef[0].ID != "shared" {
		t.Fatalf("ordinary label misclassified: %+v", ordinaryRef)
	}
	s.notes.list[0].Tags = append(s.notes.list[0].Tags, encodeNoteHost(noteHost{CatalogID: "second-host", Label: "shared"}))
	bound := s.aiNoteReferences("@note:host:shared")
	if len(bound) != 1 || bound[0].ID != "saved-host" {
		t.Fatal("selected host identity changed after a duplicate label appeared")
	}
	s.ai.noteMentions = nil // A fresh, unselected name must not guess between hosts.
	ambiguous := s.aiNoteReferences("@note:host:shared")
	if len(ambiguous) != 1 || !ambiguous[0].Ambiguous || ambiguous[0].ID != "" {
		t.Fatalf("duplicate host name guessed: %+v", ambiguous)
	}

	// Ordinary, host, and group candidates use visibly distinct badges.
	kinds := map[string]string{}
	for _, c := range s.noteMentionNames() {
		kinds[c.noteReference.Kind] = c.detail
	}
	if kinds["tag"] == kinds["host"] || kinds["tag"] == kinds["host_group"] || kinds["host"] == kinds["host_group"] {
		t.Fatal("tag categories not distinguished")
	}
}

func TestAINoteGroupTagPathCompletionAndContext(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.notes.list = []savedNote{{ID: "n", Title: "runbook", Tags: []string{encodeNoteHostGroup("Prod/DB")}}}
	s.ai.input.Insert("@note:host_group:Prod/")
	var buf [8]mentionName
	names, _ := s.mentionSuggestions(buf[:0])
	if len(names) != 1 || names[0].name != "note:host_group:Prod/DB" {
		t.Fatalf("group path suggestions: %+v", names)
	}
	s.acceptMention(names[0].name)
	refs := s.aiNoteReferences(s.ai.input.String())
	if len(refs) != 1 || refs[0].Kind != "host_group" || refs[0].ID != "Prod/DB" {
		t.Fatalf("group path parsing: %+v", refs)
	}
	context := aiNoteReferenceContext(refs)
	if !json.Valid([]byte(strings.TrimPrefix(context, notesReferenceMarker))) || !strings.Contains(context, "Prod/DB") {
		t.Fatal("reference JSON or original group path missing")
	}
	s.ai.input = text.Editor{}
	s.ai.input.Insert("@terminal/path")
	if _, _, ok := s.mentionQuery(); ok {
		t.Fatal("note-tag slash grammar changed terminal references")
	}
}

func TestAINoteReferenceWireFormatAndFolders(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.notes.list = []savedNote{{ID: "note-id", Title: "笔记名称", Tags: []string{"普通标签", encodeNoteHost(noteHost{CatalogID: "host-id", Label: "主机标签"}), encodeNoteHostGroup("主机/分组")}}}
	s.notes.folders = []savedNoteFolder{{ID: "a", Name: "A"}, {ID: "b", Name: "B", Parent: "a"}, {ID: "c", Name: "C", Parent: "b"}, {ID: "empty", Name: "空文件夹"}, {ID: "linked", Name: "关联目录", Link: &noteFolderLink{Path: "/secret/disk/path"}}}
	expected := map[string]string{"note": "note:笔记名称", "tag": "note:tag:普通标签", "host": "note:host:主机标签", "host_group": "note:host_group:主机/分组", "folder": "note:folder:A/B/C"}
	refs := []aiNoteReference{}
	for kind, token := range expected {
		s.ai.input = text.Editor{}
		s.ai.input.Insert("@" + token)
		var buf [16]mentionName
		suggestions, _ := s.mentionSuggestions(buf[:0])
		selected := ""
		for _, candidate := range suggestions {
			if candidate.name == token {
				selected = candidate.name
				break
			}
		}
		if selected == "" {
			t.Fatalf("%s suggestion missing for %s: %+v", kind, token, suggestions)
		}
		s.acceptMention(selected)
		found := s.aiNoteReferences(s.ai.input.String())
		if len(found) != 1 || found[0].Kind != kind {
			t.Fatalf("%s classified incorrectly: %+v", kind, found)
		}
		refs = append(refs, found[0])
	}
	context := aiNoteReferenceContext(refs)
	raw := strings.TrimPrefix(context, notesReferenceMarker)
	if !json.Valid([]byte(raw)) {
		t.Fatalf("JSON has trailing explanation: %s", context)
	}
	var wire []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire) != 5 {
		t.Fatal("missing references")
	}
	for _, item := range wire {
		var kind string
		_ = json.Unmarshal(item["kind"], &kind)
		_, hasID := item["id"]
		wantID := kind == "note" || kind == "host" || kind == "folder"
		size := 3
		if wantID {
			size = 4
		}
		if hasID != wantID || len(item) != size {
			t.Fatalf("unexpected %s fields: %s", kind, mustNoteJSON(item))
		}
		var token string
		_ = json.Unmarshal(item["token"], &token)
		if token != expected[kind] {
			t.Fatalf("unexpected %s token: %s", kind, token)
		}
	}
	if strings.Contains(context, "/secret/disk/path") {
		t.Fatal("disk path leaked into folder reference")
	}
	names := s.noteMentionNames()
	for _, id := range []string{"empty", "linked"} {
		found := false
		for _, name := range names {
			if name.noteReference.Kind == "folder" && name.noteReference.ID == id {
				found = true
			}
		}
		if !found {
			t.Fatalf("folder candidate missing: %s", id)
		}
	}
}

func TestAINoteFolderReferencesKeepIdentityAfterRenameAndHandleCycles(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.notes.folders = []savedNoteFolder{{ID: "one", Name: "same"}, {ID: "two", Name: "same"}, {ID: "cycle", Name: "bad", Parent: "cycle"}}
	names := s.noteMentionNames()
	if len(names) != 2 || names[0].name == names[1].name {
		t.Fatalf("folder ambiguity or cycle: %+v", names)
	}
	s.aiRememberNoteMention(names[0].name)
	token, id := names[0].name, names[0].noteReference.ID
	s.notes.folders[0].Name = "renamed"
	refs := s.aiNoteReferences("@" + token)
	if len(refs) != 1 || refs[0].Kind != "folder" || refs[0].ID != id {
		t.Fatalf("folder identity lost: %+v", refs)
	}
}

func TestAINoteReferenceMessageEndsWithOnlyJSON(t *testing.T) {
	model := &scriptedModel{turn: func(_ int, request llm.Request) []llm.Event {
		sent := request.Messages[len(request.Messages)-1].Text
		at := strings.LastIndex(sent, notesReferenceMarker)
		if at < 0 {
			t.Error("reference block missing")
			return textTurn("done")
		}
		raw := sent[at+len(notesReferenceMarker):]
		if !json.Valid([]byte(raw)) {
			t.Errorf("reference array has trailing content: %s", raw)
		}
		if !strings.Contains(sent[:at], "[Terminal of ") {
			t.Error("Ask terminal context was not retained before references")
		}
		var refs []map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &refs); err != nil {
			t.Error(err)
		} else if len(refs) != 2 {
			t.Errorf("references=%d", len(refs))
		} else {
			for _, ref := range refs {
				var kind string
				_ = json.Unmarshal(ref["kind"], &kind)
				if _, ok := ref["id"]; ok != (kind == "folder") {
					t.Errorf("incorrect %s id fields", kind)
				}
			}
		}
		return textTurn("done")
	}}
	s, shell := assistantTestSession(t, model)
	s.notes.folders = []savedNoteFolder{{ID: "a", Name: "A"}, {ID: "b", Name: "B", Parent: "a"}}
	s.notes.list = []savedNote{{ID: "n", Tags: []string{"ops"}}}
	s.ai.settings.contextLines = 10
	shell.say("terminal context\r\n")
	s.Wake()
	ask(s, "参考 @note:folder:A/B @note:tag:ops", agent.ModeAsk)
	pumpUntil(t, s, "reference message sent", func() bool { return s.ai.run == nil })
	if len(model.requests) != 1 {
		t.Fatal("model was not called")
	}
}
