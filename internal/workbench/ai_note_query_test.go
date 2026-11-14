package workbench

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"motty/internal/ai/agent"
	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"motty/internal/ai/tools"
)

func queryNoteTest(t *testing.T, s *State, a tools.NoteQueryArgs) string {
	t.Helper()
	raw, err := s.aiQueryNote(a)
	if err != nil {
		t.Fatalf("query failed: %s %v", raw, err)
	}
	if !json.Valid([]byte(raw)) || len(raw) > noteToolResultLimit {
		t.Fatalf("invalid result: %s", raw)
	}
	return raw
}

func TestNoteQueryReferenceSelectionAndMetadata(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.hosts = []Host{{CatalogID: "h1", Label: "same", Address: "a", Group: "Prod/DB"}, {CatalogID: "h2", Label: "same", Address: "b", Group: "Prod/DB2"}}
	s.save()
	folder := func(name, parent string) string {
		out := aiNotesEditTest(t, s, aiNotesArgs(t, "folder_create", "", "", map[string]any{"name": name, "parent": parent}))
		var f savedNoteFolder
		_ = json.Unmarshal(out["folder"], &f)
		return f.ID
	}
	root := folder("A", "")
	child := folder("B", root)
	empty := folder("empty", "")
	create := func(title string, attrs map[string]any) string {
		attrs["title"] = title
		attrs["body"] = "Docker 中文网络"
		out := aiNotesEditTest(t, s, aiNotesArgs(t, "create", "", "", attrs))
		var n savedNote
		_ = json.Unmarshal(out["note"], &n)
		return n.ID
	}
	a := create("a", map[string]any{"tags": []string{"Docker"}, "host_ids": []string{"h1"}, "folder": child})
	b := create("b", map[string]any{"tags": []string{"Docker"}, "host_groups": []string{"Prod/DB"}})
	c := create("c", map[string]any{"host_groups": []string{"Prod/DB2"}, "host_ids": []string{"h2"}})
	d := create("d", map[string]any{"host_groups": []string{"Prod"}})
	selectIDs := func(refs []tools.NoteReference, query string) []string {
		var page struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
			Next *int `json:"next_offset"`
		}
		raw := queryNoteTest(t, s, tools.NoteQueryArgs{Action: "search", References: refs, Query: query})
		if err := json.Unmarshal([]byte(raw), &page); err != nil {
			t.Fatal(err)
		}
		if page.Next != nil {
			t.Fatalf("unexpected next page: %s", raw)
		}
		ids := []string{}
		for _, n := range page.Items {
			ids = append(ids, n.ID)
		}
		slices.Sort(ids)
		return ids
	}
	for _, tc := range []struct {
		name  string
		refs  []tools.NoteReference
		query string
		want  []string
	}{
		{"whole library", nil, "", []string{a, b, c, d}},
		{"empty references", []tools.NoteReference{}, "", []string{a, b, c, d}},
		{"note ID wins over title", []tools.NoteReference{{Kind: "note", ID: a, Title: "wrong", Token: "wrong"}}, "", []string{a}},
		{"tag", []tools.NoteReference{{Kind: "tag", Title: "Docker"}}, "", []string{a, b}},
		{"folder descendants", []tools.NoteReference{{Kind: "folder", ID: root}}, "", []string{a}},
		{"empty existing folder", []tools.NoteReference{{Kind: "folder", ID: empty}}, "", []string{}},
		{"host ID no alias guessing", []tools.NoteReference{{Kind: "host", ID: "h1", Title: "same-2"}}, "", []string{a}},
		{"group boundary and no inferred host group", []tools.NoteReference{{Kind: "host_group", Title: "Prod/DB"}}, "", []string{b}},
		{"group descendants", []tools.NoteReference{{Kind: "host_group", Title: "Prod"}}, "", []string{b, c, d}},
		{"union deduplicates", []tools.NoteReference{{Kind: "tag", Title: "Docker"}, {Kind: "note", ID: a}, {Kind: "host", ID: "h2"}}, "", []string{a, b, c}},
		{"keyword intersection", []tools.NoteReference{{Kind: "host", ID: "h1"}}, "不存在", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slices.Sort(tc.want)
			if got := selectIDs(tc.refs, tc.query); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	for _, ref := range []tools.NoteReference{{Kind: "note", ID: "missing"}, {Kind: "folder", ID: "missing"}, {Kind: "host", ID: "missing"}, {Kind: "tag", Title: "docker"}, {Kind: "host_group", Title: "Prod/D"}} {
		raw, err := s.aiQueryNote(tools.NoteQueryArgs{Action: "search", References: []tools.NoteReference{{Kind: "note", ID: a}, ref}})
		if err == nil || !strings.Contains(raw, `"status":"invalid_reference"`) {
			t.Fatalf("invalid reference widened search: %s %v", raw, err)
		}
	}
	raw := queryNoteTest(t, s, tools.NoteQueryArgs{Action: "search", References: []tools.NoteReference{{Kind: "note", ID: a}}})
	for _, fragment := range []string{`"host_ids":["h1"]`, `"host_groups":[]`, `"folder_path":"A/B"`, `"tags":["Docker"]`, `"title_truncated":false`} {
		if !strings.Contains(raw, fragment) {
			t.Fatalf("missing %s in %s", fragment, raw)
		}
	}
	tags := queryNoteTest(t, s, tools.NoteQueryArgs{Action: "tags"})
	if strings.Contains(tags, "h1") || strings.Contains(tags, "Prod") || !strings.Contains(tags, "Docker") {
		t.Fatal(tags)
	}
	folders := queryNoteTest(t, s, tools.NoteQueryArgs{Action: "folders", Query: "A/B"})
	if !strings.Contains(folders, child) || !strings.Contains(folders, `"path":"A/B"`) {
		t.Fatal(folders)
	}
	// Refresh from another window, retaining references by ID through renames.
	other := newTestState(t, s.options)
	other.hosts[0].Label = "renamed"
	other.save()
	aiNotesEditTest(t, other, aiNotesArgs(t, "folder_update", root, "", map[string]any{"name": "renamed"}))
	if got := selectIDs([]tools.NoteReference{{Kind: "folder", ID: root, Title: "A"}}, ""); !reflect.DeepEqual(got, []string{a}) {
		t.Fatal(got)
	}
	if got := selectIDs([]tools.NoteReference{{Kind: "host", ID: "h1", Title: "same"}}, ""); !reflect.DeepEqual(got, []string{a}) {
		t.Fatal(got)
	}
	// Read's version is accepted by the unchanged editor and survives reopening.
	var read struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal([]byte(queryNoteTest(t, s, tools.NoteQueryArgs{Action: "read", ID: a})), &read)
	aiNotesEditTest(t, s, aiNotesArgs(t, "update", a, read.Version, map[string]any{"host_ids": []string{"h2"}, "host_groups": []string{"Prod"}}))
	reopened := newTestState(t, s.options)
	saved := queryNoteTest(t, reopened, tools.NoteQueryArgs{Action: "read", ID: a})
	if !strings.Contains(saved, `"host_ids":["h2"]`) || !strings.Contains(saved, `"host_groups":["Prod"]`) {
		t.Fatal(saved)
	}
}

func TestNoteQueryPaginationAndResponseBudget(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	tags := []string{strings.Repeat("长", 80)}
	for i := 0; i < 20; i++ {
		aiNotesEditTest(t, s, aiNotesArgs(t, "create", "", "", map[string]any{"title": strings.Repeat("标题", 100), "body": strings.Repeat("中", 500), "tags": tags}))
	}
	var page struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
		Next  *int              `json:"next_offset"`
	}
	raw := queryNoteTest(t, s, tools.NoteQueryArgs{Action: "search"})
	_ = json.Unmarshal([]byte(raw), &page)
	if len(page.Items) != 10 || page.Total != 20 || page.Next == nil || *page.Next != 10 {
		t.Fatal(raw)
	}
	for _, flag := range []string{"title_truncated", "summary_truncated", "tags_truncated"} {
		if !strings.Contains(string(page.Items[0]), `"`+flag+`":true`) {
			t.Fatal(raw)
		}
	}
	raw = queryNoteTest(t, s, tools.NoteQueryArgs{Action: "search", Offset: *page.Next})
	_ = json.Unmarshal([]byte(raw), &page)
	if len(page.Items) != 10 || page.Next != nil {
		t.Fatal(raw)
	}
	// Full row accounting must stop before the budget, even when limit allows more.
	raw = queryNoteTest(t, s, tools.NoteQueryArgs{Action: "search", Limit: 50})
	_ = json.Unmarshal([]byte(raw), &page)
	if len(page.Items) == 0 || len(page.Items) >= 20 || page.Next == nil {
		t.Fatalf("budget did not end the page: %d items %d bytes", len(page.Items), len(raw))
	}
	// An individual auxiliary row too large to return must produce a valid error.
	s.hosts = []Host{{CatalogID: "big", Label: strings.Repeat("主机", 10000), Address: "a"}}
	s.save()
	raw, err := s.aiQueryNote(tools.NoteQueryArgs{Action: "hosts"})
	if err == nil || !json.Valid([]byte(raw)) || len(raw) > noteToolResultLimit || !strings.Contains(raw, `"status":"validation_error"`) {
		t.Fatalf("invalid oversized response: %s %v", raw, err)
	}
}

func TestAINoteQueryReferencesThroughConversation(t *testing.T) {
	var id string
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		last := request.Messages[len(request.Messages)-1].Text
		switch index {
		case 0:
			at := strings.LastIndex(last, notesReferenceMarker)
			if at < 0 {
				t.Error("missing references")
				return textTurn("failed")
			}
			var refs []tools.NoteReference
			if err := json.Unmarshal([]byte(last[at+len(notesReferenceMarker):]), &refs); err != nil || len(refs) != 4 {
				t.Errorf("references: %+v %v", refs, err)
			}
			raw, _ := json.Marshal(map[string]any{"action": "search", "references": refs})
			return toolTurn(tools.NameNoteQuery, string(raw))
		case 1:
			var page struct {
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
			}
			if err := json.Unmarshal([]byte(last), &page); err != nil || len(page.Items) != 1 || page.Items[0].ID != id {
				t.Errorf("filtered search: %s %v", last, err)
			}
			return toolTurn(tools.NameNoteQuery, aiNotesArgs(t, "read", id, "", map[string]any{}))
		case 2:
			var n aiNoteResult
			if err := json.Unmarshal([]byte(last), &n); err != nil {
				t.Error(err)
			}
			return toolTurn(tools.NameNoteEdit, aiNotesArgs(t, "update", id, n.Version, map[string]any{"host_ids": []string{"h2"}, "host_groups": []string{"Prod"}}))
		case 3:
			if !strings.Contains(last, `"status":"saved"`) {
				t.Error(last)
			}
			return toolTurn(tools.NameNoteQuery, aiNotesArgs(t, "read", id, "", map[string]any{}))
		default:
			if !strings.Contains(last, `"host_ids":["h2"]`) || !strings.Contains(last, `"host_groups":["Prod"]`) {
				t.Error(last)
			}
			return textTurn("已参考笔记并更新关联")
		}
	}}
	s, _ := assistantTestSession(t, model)
	s.hosts = []Host{{CatalogID: "h1", Label: "alias", Address: "a", Group: "Prod/DB"}, {CatalogID: "h2", Label: "other", Address: "b", Group: "Prod"}}
	s.save()
	out := aiNotesEditTest(t, s, `{"action":"folder_create","name":"经验"}`)
	var f savedNoteFolder
	_ = json.Unmarshal(out["folder"], &f)
	out = aiNotesEditTest(t, s, aiNotesArgs(t, "create", "", "", map[string]any{"title": "Docker", "body": "中文经验", "tags": []string{"Docker"}, "host_ids": []string{"h1"}, "host_groups": []string{"Prod/DB"}, "folder": f.ID}))
	var n savedNote
	_ = json.Unmarshal(out["note"], &n)
	id = n.ID
	aiNotesEditTest(t, s, `{"action":"create","title":"unrelated","body":"unrelated"}`)
	input := ""
	for _, name := range s.noteMentionNames() {
		if name.noteReference.Kind == "note" {
			continue
		}
		s.aiRememberNoteMention(name.name)
		input += "@" + name.name + " "
	}
	s.ai.targetSet = true
	s.ai.target = gate.SessionInfo{}
	ask(s, input+"参考这些笔记，更新主机和分组关联", agent.ModeAgent)
	pumpUntil(t, s, "note references queried and edited", func() bool { return s.ai.run == nil })
	reopened := newTestState(t, s.options)
	raw := queryNoteTest(t, reopened, tools.NoteQueryArgs{Action: "read", ID: id})
	if !strings.Contains(raw, `"host_ids":["h2"]`) || !strings.Contains(raw, `"host_groups":["Prod"]`) {
		t.Fatal(raw)
	}
}
