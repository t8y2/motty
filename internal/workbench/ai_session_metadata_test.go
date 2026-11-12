package workbench

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
	"motty/internal/ai/tools"
	"motty/internal/sshconn"
)

func TestAISessionMetadataAliasesReconnectAndCatalog(t *testing.T) {
	world, options := newWorld(t)
	s := newTestState(t, options)
	t.Cleanup(s.Close)
	host := s.hosts[0]
	first := s.newHostSession(host)
	firstID := first.ID
	second := s.newHostSession(host)
	secondID := second.ID
	for _, id := range []uint32{firstID, secondID} {
		session, _ := s.sessionByIDPointer(id)
		info := s.aiSessionInfo(session)
		if info.HostID != host.CatalogID || info.HostGroup != host.Group || info.HostName != host.Label {
			t.Fatalf("metadata: %+v", info)
		}
	}
	second, _ = s.sessionByIDPointer(secondID)
	s.renameSession(second, "yicai-2")
	s.reconnect(second)
	fake := world.conns[len(world.conns)-1]
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	s.Wake()
	fake.shell(t).say("ready")
	s.Wake()
	second, _ = s.sessionByIDPointer(secondID)
	info := s.aiSessionInfo(second)
	if info.HostID != host.CatalogID || info.HostGroup != host.Group {
		t.Fatal("reconnect lost catalog identity")
	}
	backend, err := s.aiTargetBackend(info)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.closeAll()
	s.hosts[0].Group = "New/Group"
	if s.aiSessionInfo(second).HostGroup != "New/Group" || backend.bindings[0].info.HostGroup != host.Group {
		t.Fatal("catalog should refresh between requests, remain fixed within a request")
	}
	context := s.aiMentionContext("@"+sessionMentionName(second), 0)
	if !strings.Contains(context, fmt.Sprintf("host_id=%q", host.CatalogID)) || !strings.Contains(context, `host_group="New/Group"`) {
		t.Fatalf("mention missing metadata: %s", context)
	}
	s.ai.target, s.ai.targetSet = info, true
	s.ai.settings.contextLines = 10
	if !strings.Contains(s.aiContext(), `host_group="New/Group"`) || !strings.Contains(s.aiSessionSummary(), fmt.Sprintf("host_id=%q", host.CatalogID)) {
		t.Fatal("default context missing catalog identity")
	}
	local := Session{HostID: localHostID, host: localHost("Local")}
	if got := s.aiSessionInfo(&local); got.HostID != "" || got.HostGroup != "" {
		t.Fatal("local terminal acquired catalog identity")
	}
	s.hosts[0].Group = ""
	if got := s.aiSessionInfo(second); got.HostID != host.CatalogID || got.HostGroup != "" {
		t.Fatal("saved root host lost identity")
	}
	// Never infer identity from matching addresses, including deleted hosts.
	temporary := *second
	temporary.HostID = 0
	if got := s.aiSessionInfo(&temporary); got.HostID != "" || got.HostGroup != "" {
		t.Fatal("temporary target acquired saved identity")
	}
	s.hosts = s.hosts[1:]
	if got := s.aiSessionInfo(second); got.HostID != "" || got.HostGroup != "" {
		t.Fatal("deleted host identity retained")
	}
}

func TestAISessionMetadataUsedForNoteAssociations(t *testing.T) {
	var hostID, group, noteID string
	model := &scriptedModel{turn: func(index int, req llm.Request) []llm.Event {
		if index == 0 {
			if !strings.Contains(req.System, fmt.Sprintf("host_id=%q", hostID)) || !strings.Contains(req.System, fmt.Sprintf("host_group=%q", group)) {
				t.Error("system context lacks host metadata")
			}
			return toolTurn(tools.NameListSessions, `{}`)
		}
		last := req.Messages[len(req.Messages)-1].Text
		if index == 1 {
			if !strings.Contains(last, fmt.Sprintf("host_id=%q", hostID)) || !strings.Contains(last, fmt.Sprintf("host_group=%q", group)) {
				t.Error("list_sessions lacks host metadata")
			}
			return toolTurn(tools.NameNoteEdit, aiNotesArgs(t, "create", "", "", map[string]any{"title": "alias runbook", "body": "steps", "host_ids": []string{hostID}, "host_groups": []string{group}}))
		}
		if index == 2 {
			var result struct {
				Note struct {
					ID string `json:"id"`
				} `json:"note"`
			}
			if err := json.Unmarshal([]byte(last), &result); err != nil {
				t.Error(err)
			}
			noteID = result.Note.ID
			return toolTurn(tools.NameNoteQuery, aiNotesArgs(t, "read", noteID, "", map[string]any{}))
		}
		var result aiNoteResult
		if err := json.Unmarshal([]byte(last), &result); err != nil {
			t.Error(err)
		}
		if len(result.HostIDs) != 1 || result.HostIDs[0] != hostID || len(result.HostGroups) != 1 || result.HostGroups[0] != group {
			t.Errorf("saved associations: %+v", result)
		}
		return textTurn("saved")
	}}
	s, _ := assistantTestSession(t, model)
	session := &s.sessions[0]
	hostID, group = session.host.CatalogID, session.host.Group
	s.renameSession(session, "yicai-2")
	ask(s, "将这次排障整理成笔记并关联本主机及分组", agent.ModeAgent)
	pumpUntil(t, s, "note associations verified", func() bool { return s.ai.run == nil })
	if noteID == "" {
		t.Fatal("note was not created")
	}
	reopened := newTestState(t, s.options)
	result, err := reopened.aiQueryNote(tools.NoteQueryArgs{Action: "read", ID: noteID})
	if err != nil {
		t.Fatal(err)
	}
	var note aiNoteResult
	if err = json.Unmarshal([]byte(result), &note); err != nil {
		t.Fatal(err)
	}
	if len(note.HostIDs) != 1 || note.HostIDs[0] != hostID || len(note.HostGroups) != 1 || note.HostGroups[0] != group {
		t.Fatalf("associations not persisted: %s", result)
	}
}
