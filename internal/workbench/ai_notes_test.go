package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"motty/internal/ai/agent"
	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"motty/internal/ai/tools"
)

func aiNotesEditTest(t *testing.T, s *State, raw string) map[string]json.RawMessage {
	t.Helper()
	a, err := tools.DecodeNoteEdit([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.aiEditNote(a)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err = json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatal(err)
	}
	if string(out["status"]) != `"saved"` {
		t.Fatalf("save failed: %s", result)
	}
	return out
}
func aiNotesVersion(s *State, id string) string {
	return s.aiNoteSnapshot(s.notes.list[s.noteIndex(id)]).Version
}
func aiNotesArgs(t *testing.T, action, id, version string, attrs map[string]any) string {
	t.Helper()
	attrs["action"] = action
	if id != "" {
		attrs["id"] = id
	}
	if version != "" {
		attrs["version"] = version
	}
	b, err := json.Marshal(attrs)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAINotesContentMetadataAndDraftVersion(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.hosts = []Host{{CatalogID: "db", Label: "Database", Address: "db.example", Group: "Prod/DB"}}
	s.save()
	aiNotesEditTest(t, s, `{"action":"create","title":"排障","body":"before command after","tags":["数据库"],"host_ids":["db"],"host_groups":["Prod"]}`)
	id := s.notes.list[0].ID
	if !noteHasHost(s.notes.list[0].Tags, "db") || !noteHasGroup(s.notes.list[0].Tags, "Prod") {
		t.Fatal("missing associations")
	}
	result, err := s.aiQueryNote(tools.NoteQueryArgs{Action: "search", Query: "Database"})
	if err != nil || !strings.Contains(result, id) {
		t.Fatalf("host search: %s %v", result, err)
	}
	version := aiNotesVersion(s, id)
	aiNotesEditTest(t, s, aiNotesArgs(t, "replace", id, version, map[string]any{"old_text": "command", "new_text": "fixed"}))
	if got := s.notes.list[0].Body; got != "before fixed after" {
		t.Fatal(got)
	}
	stale, _ := tools.DecodeNoteEdit([]byte(aiNotesArgs(t, "update", id, version, map[string]any{"title": "bad"})))
	if _, err = s.aiEditNote(stale); err == nil {
		t.Fatal("stale version accepted")
	}
	version = aiNotesVersion(s, id)
	s.selectNoteWithoutSave(id)
	s.noteBuffer(id).body.Insert("draft ")
	stale.Version = version
	if _, err = s.aiEditNote(stale); err == nil {
		t.Fatal("shared draft change was overwritten")
	}
	current := aiNotesVersion(s, id)
	aiNotesEditTest(t, s, aiNotesArgs(t, "update", id, current, map[string]any{"title": "更新", "tags": []string{"新标签"}, "host_ids": []string{}}))
	n := s.notes.list[0]
	if n.Title != "更新" || !strings.Contains(n.Body, "draft ") || noteHasHost(n.Tags, "db") || !noteHasGroup(n.Tags, "Prod") {
		t.Fatalf("metadata/draft lost: %+v", n)
	}
	aiNotesEditTest(t, s, aiNotesArgs(t, "replace", id, aiNotesVersion(s, id), map[string]any{"old_text": "fixed", "new_text": ""}))
	for _, old := range []string{"missing", " "} {
		args, _ := tools.DecodeNoteEdit([]byte(aiNotesArgs(t, "replace", id, aiNotesVersion(s, id), map[string]any{"old_text": old, "new_text": "bad"})))
		before := s.notes.list[0].Body
		if _, err = s.aiEditNote(args); err == nil || s.notes.list[0].Body != before {
			t.Fatal("invalid match mutated note")
		}
	}
	reopened := newTestState(t, s.options)
	if reopened.notes.list[0].Title != "更新" || reopened.notes.list[0].Body != s.notes.list[0].Body {
		t.Fatal("changes not persisted")
	}
}

func TestAINotesFoldersAndRestrictions(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	aiNotesEditTest(t, s, `{"action":"folder_create","name":"经验"}`)
	parent := s.notes.folders[0].ID
	aiNotesEditTest(t, s, aiNotesArgs(t, "folder_create", "", "", map[string]any{"name": "数据库", "parent": parent}))
	child := s.notes.folders[1].ID
	reject := func(raw string) {
		t.Helper()
		a, err := tools.DecodeNoteEdit([]byte(raw))
		if err == nil {
			_, err = s.aiEditNote(a)
		}
		if err == nil {
			t.Fatalf("accepted: %s", raw)
		}
	}
	reject(aiNotesArgs(t, "folder_update", parent, "", map[string]any{"parent": child}))
	reject(aiNotesArgs(t, "folder_delete", parent, "", map[string]any{}))
	aiNotesEditTest(t, s, aiNotesArgs(t, "create", "", "", map[string]any{"title": "经验", "body": "内容", "folder": child}))
	id := s.notes.list[0].ID
	reject(aiNotesArgs(t, "folder_delete", child, "", map[string]any{}))
	aiNotesEditTest(t, s, aiNotesArgs(t, "update", id, aiNotesVersion(s, id), map[string]any{"folder": ""}))
	aiNotesEditTest(t, s, aiNotesArgs(t, "folder_update", child, "", map[string]any{"name": "新名称", "parent": ""}))
	aiNotesEditTest(t, s, aiNotesArgs(t, "folder_delete", child, "", map[string]any{}))
	aiNotesEditTest(t, s, aiNotesArgs(t, "folder_delete", parent, "", map[string]any{}))
	s.notes.folders = append(s.notes.folders, savedNoteFolder{ID: "linked", Name: "linked", Link: &noteFolderLink{Path: t.TempDir()}})
	reject(`{"action":"folder_update","id":"linked","name":"bad"}`)
	reject(`{"action":"folder_create","name":"bad","parent":"linked"}`)
	reject(aiNotesArgs(t, "update", id, aiNotesVersion(s, id), map[string]any{"folder": "linked"}))
	reject(aiNotesArgs(t, "update", id, aiNotesVersion(s, id), map[string]any{"host_ids": []string{"unknown"}}))
}

func TestAINotesMentionsFocusAndIdentity(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	for _, raw := range []string{`{"action":"create","title":"同名","body":"one","tags":["ops"]}`, `{"action":"create","title":"同名","body":"two","tags":["db"]}`, `{"action":"create","title":"外部","body":"three","tags":["other"]}`} {
		aiNotesEditTest(t, s, raw)
	}
	names := s.noteMentionNames()
	if names[0].name == names[1].name {
		t.Fatal("duplicate titles have same token")
	}
	s.aiRememberNoteMention(names[0].name)
	token := names[0].name
	s.notes.list[0].Title = "renamed"
	refs := s.aiNoteReferences("@" + token)
	if len(refs) != 1 || refs[0].ID != s.notes.list[0].ID {
		t.Fatalf("identity lost: %+v", refs)
	}
	var tagToken string
	for _, n := range s.noteMentionNames() {
		if n.noteReference.Kind == "tag" && n.noteReference.ID == "db" {
			tagToken = n.name
		}
	}
	refs = s.aiNoteReferences("@" + token + " @" + tagToken)
	if len(refs) != 2 || refs[0].ID != s.notes.list[0].ID || refs[1].ID != "db" {
		t.Fatalf("mixed reference identities lost: %+v", refs)
	}
	s.notes.list = s.notes.list[1:]
	refs = s.aiNoteReferences("@" + token)
	if len(refs) != 1 || refs[0].ID == "" {
		t.Fatalf("deleted note identity lost: %+v", refs)
	}
	context := aiNoteReferenceContext(refs)
	if !strings.Contains(context, refs[0].ID) || stripContext("question"+context) != "question" {
		t.Fatal("structured reference context not preserved/hidden")
	}
}

func TestAINotesLinkedFileResults(t *testing.T) {
	for _, scenario := range []string{"success", "conflict", "missing", "write_failure"} {
		t.Run(scenario, func(t *testing.T) {
			s, id, path := linkedFixture(t, "base")
			version := aiNotesVersion(s, id)
			switch scenario {
			case "conflict":
				if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "write_failure":
				if runtime.GOOS == "windows" || os.Geteuid() == 0 {
					t.Skip("requires POSIX directory permissions")
				}
				dir := filepath.Dir(path)
				if err := os.Chmod(dir, 0500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
			}
			a, _ := tools.DecodeNoteEdit([]byte(aiNotesArgs(t, "update", id, version, map[string]any{"body": "AI result", "title": "new title"})))
			result, err := s.aiEditNote(a)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "success" {
				if !strings.Contains(result, `"status":"saved"`) || readFile(t, path) != "AI result" {
					t.Fatalf("save: %s", result)
				}
			} else if strings.Contains(result, `"status":"saved"`) {
				t.Fatalf("reported false success: %s", result)
			}
			if scenario == "write_failure" && !strings.Contains(result, `"status":"file_write_failed"`) {
				t.Fatalf("write failure not classified: %s", result)
			}
			if s.notes.list[0].Link.Path != path || s.notes.list[0].Body != "AI result" {
				t.Fatal("link changed or memory lost")
			}
			if scenario == "conflict" && readFile(t, path) != "external" {
				t.Fatal("disk conflict overwritten")
			}
		})
	}
}

// Exercise the real send -> agent -> gate -> window backend -> persistence path
// with deterministic model responses, including note tools without a terminal.
func TestAINotesConversationCaptureReferenceAndUpdate(t *testing.T) {
	var id string
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		last := request.Messages[len(request.Messages)-1]
		switch index {
		case 0:
			found := false
			for _, tool := range request.Tools {
				if tool.Name == tools.NameNoteEdit {
					found = true
				}
			}
			if !found {
				t.Error("note tools missing from Agent request")
			}
			return toolTurn(tools.NameNoteQuery, `{"action":"hosts"}`)
		case 1:
			return toolTurn(tools.NameNoteEdit, `{"action":"folder_create","name":"排障经验"}`)
		case 2:
			var result struct {
				Folder savedNoteFolder `json:"folder"`
			}
			if err := json.Unmarshal([]byte(last.Text), &result); err != nil {
				t.Error(err)
			}
			return toolTurn(tools.NameNoteEdit, aiNotesArgs(t, "create", "", "", map[string]any{"title": "数据库排障", "body": "检查连接后重启", "tags": []string{"database"}, "host_ids": []string{"db"}, "host_groups": []string{"Prod"}, "folder": result.Folder.ID}))
		case 3:
			return textTurn("已整理为数据库排障笔记。")
		case 4:
			if !strings.Contains(last.Text, notesReferenceMarker) {
				t.Error("references missing from model context")
			}
			return toolTurn(tools.NameNoteQuery, `{"action":"search"}`)
		case 5:
			if !strings.Contains(last.Text, "secret-unrelated") {
				t.Error("focus hint restricted agent search")
			}
			return toolTurn(tools.NameNoteQuery, aiNotesArgs(t, "read", id, "", map[string]any{}))
		case 6:
			var note aiNoteResult
			if err := json.Unmarshal([]byte(last.Text), &note); err != nil {
				t.Error(err)
			}
			return toolTurn(tools.NameNoteEdit, aiNotesArgs(t, "replace", note.ID, note.Version, map[string]any{"old_text": "重启", "new_text": "确认连接池状态"}))
		default:
			return textTurn("根据数据库排障笔记，先检查连接。已补充验证步骤。")
		}
	}}
	s, _ := assistantTestSession(t, model)
	s.hosts = []Host{{CatalogID: "db", Label: "Database", Address: "db.example", Group: "Prod/DB"}}
	s.save()
	s.activeSection = sectionNotes
	s.ai.targetSet = true
	s.ai.target = gate.SessionInfo{}
	s.ai.settings.keepHistory = true
	ask(s, "将刚才的排障整理为笔记，关联数据库主机并按主题归档", agent.ModeAgent)
	pumpUntil(t, s, "notes captured", func() bool { return s.ai.run == nil })
	if len(s.notes.list) != 1 {
		t.Fatalf("capture failed: %+v", s.ai.items)
	}
	id = s.notes.list[0].ID
	if s.notes.list[0].Folder == "" || !noteHasHost(s.notes.list[0].Tags, "db") {
		t.Fatal("metadata not saved")
	}
	aiNotesEditTest(t, s, `{"action":"create","title":"secret-unrelated","body":"secret-unrelated"}`)
	var token string
	for _, candidate := range s.noteMentionNames() {
		if candidate.noteReference.Kind == "tag" && candidate.noteReference.ID == "database" {
			token = candidate.name
		}
	}
	s.ai.input.Insert("@database")
	s.acceptMention(token)
	ask(s, "参考这个标签的经验，并将重启步骤改为确认连接池状态", agent.ModeAgent)
	pumpUntil(t, s, "note updated", func() bool { return s.ai.run == nil })
	if got := s.notes.list[s.noteIndex(id)].Body; got != "检查连接后确认连接池状态" {
		t.Fatal(got)
	}
	conv := s.ai.conv
	s.aiNewConversation()
	s.aiOpenConversation(conv)
	refs := s.aiNoteReferences("@" + token)
	if len(refs) != 1 || refs[0].ID != "database" {
		t.Fatalf("history lost reference metadata: %+v", refs)
	}
	// Ask can query notes without switching mode, but cannot edit them.
	model.turn = func(_ int, request llm.Request) []llm.Event {
		found := false
		for _, tool := range request.Tools {
			if tool.Name == tools.NameNoteEdit {
				t.Error("Ask received note editing tool")
			}
			if tool.Name == tools.NameNoteQuery {
				found = true
			}
		}
		if !found {
			t.Error("Ask did not receive note_query")
		}
		if !strings.Contains(request.System, "note_query tool needs no connection and can be used in Ask or Agent mode") {
			t.Error("no-connection prompt withheld note queries from Ask")
		}
		last := request.Messages[len(request.Messages)-1]
		if last.Role == llm.RoleUser {
			return toolTurn(tools.NameNoteQuery, aiNotesArgs(t, "read", id, "", map[string]any{}))
		}
		if !strings.Contains(last.Text, "确认连接池状态") {
			t.Errorf("Ask read failed: %s", last.Text)
		}
		return textTurn("参考数据库排障笔记，检查连接池状态。")
	}
	ask(s, "@"+token+" 读取经验", agent.ModeAsk)
	pumpUntil(t, s, "Ask queried and answered", func() bool { return s.ai.run == nil })
	if s.ai.mode != agent.ModeAsk || len(s.ai.approvals) != 0 {
		t.Fatal("Ask note query required escalation")
	}

}

func TestAINotesInvalidReferenceAndPagedRead(t *testing.T) {
	if uniqueNoteFragment("aaa", "aa") {
		t.Fatal("overlapping matches are ambiguous")
	}
	s := newTestState(t, freshOptions(t))
	body := strings.Repeat("中文笔记", 2000)
	aiNotesEditTest(t, s, aiNotesArgs(t, "create", "", "", map[string]any{"title": "large", "body": body}))
	id := s.notes.list[0].ID
	refs := s.aiNoteReferences("@note:tag:deleted")
	if len(refs) != 1 {
		t.Fatal("unresolved reference was dropped")
	}
	if _, err := s.aiQueryNote(tools.NoteQueryArgs{Action: "search"}); err != nil {
		t.Fatal("unresolved focus hint blocked global search")
	}
	offset := 0
	var rebuilt strings.Builder
	for offset < len([]rune(body)) {
		raw, err := s.aiQueryNote(tools.NoteQueryArgs{Action: "read", ID: id, Offset: offset, ExpectedVersion: aiNotesVersion(s, id)})
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > 32<<10 {
			t.Fatal("read exceeds agent result budget")
		}
		var page struct {
			Body       string `json:"body"`
			NextOffset int    `json:"next_offset"`
		}
		if err = json.Unmarshal([]byte(raw), &page); err != nil {
			t.Fatal(err)
		}
		if page.NextOffset <= offset {
			t.Fatal("pagination made no progress")
		}
		rebuilt.WriteString(page.Body)
		offset = page.NextOffset
	}
	if rebuilt.String() != body {
		t.Fatal("paged content lost")
	}
}

func TestAINotesCommitIgnoresUnrelatedSettingsConflict(t *testing.T) {
	for _, action := range []string{"create", "update"} {
		t.Run(action, func(t *testing.T) {
			_, options := newWorld(t)
			first := newTestState(t, options)
			aiNotesEditTest(t, first, `{"action":"create","title":"existing","body":"unchanged"}`)
			id := first.notes.list[0].ID
			second := newTestState(t, options)
			version := aiNotesVersion(second, id)
			hostID := second.hosts[1].CatalogID
			first.hosts[0].Label = "other window"
			first.save()
			second.hosts[0].Label = "pending local change"
			attrs := map[string]any{"host_ids": []string{hostID}}
			target := id
			if action == "create" {
				target = ""
				version = ""
				attrs["title"] = "new AI note"
				attrs["body"] = "must survive"
			}
			out := aiNotesEditTest(t, second, aiNotesArgs(t, action, target, version, attrs))
			var saved savedNote
			if err := json.Unmarshal(out["note"], &saved); err != nil {
				t.Fatal(err)
			}
			if _, err := second.aiQueryNote(tools.NoteQueryArgs{Action: "read", ID: saved.ID}); err != nil {
				t.Fatal(err)
			}
			reopened := newTestState(t, options)
			index := reopened.noteIndex(saved.ID)
			if index < 0 {
				t.Fatal("note missing after restart")
			}
			n := reopened.aiNoteSnapshot(reopened.notes.list[index])
			if len(n.HostIDs) != 1 || n.HostIDs[0] != hostID {
				t.Fatalf("association lost: %+v", n.HostIDs)
			}
			expectedCount := 1
			if action == "create" {
				expectedCount = 2
				if n.Body != "must survive" {
					t.Fatal("created body lost")
				}
			}
			if len(reopened.notes.list) != expectedCount {
				t.Fatal("note forked or lost")
			}
		})
	}
}
