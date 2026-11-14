package workbench

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"motty/internal/ai/mcp"
	"motty/internal/ai/store"
	"motty/internal/ai/tools"
	"motty/internal/configstore"
)

// seedNoteLibrary writes notes and folders to a configuration store the way a
// window's save does.
func seedNoteLibrary(t *testing.T, path string, notes []savedNote, folders []savedNoteFolder) {
	t.Helper()
	entries, local, _, err := splitState(savedState{Notes: notes, NoteFolders: folders}, newConfigBase(), nil)
	if err != nil {
		t.Fatal(err)
	}
	config, err := configstore.OpenShared(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer config.Close()
	if _, err := config.Commit(configstore.Change{Put: entries, Local: map[string][]byte{localKey: local}}); err != nil {
		t.Fatal(err)
	}
}

type noteToolReply struct {
	Items []struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Version string `json:"version"`
	} `json:"items"`
	ID           string `json:"id"`
	Body         string `json:"body"`
	Version      string `json:"version"`
	Status       string `json:"status"`
	LibrarySaved bool   `json:"library_saved"`
	Message      string `json:"message"`
	Note         *struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	} `json:"note"`
	FileStatus string `json:"file_status"`
}

func parseNoteReply(t *testing.T, value string) noteToolReply {
	t.Helper()
	var reply noteToolReply
	if err := json.Unmarshal([]byte(value), &reply); err != nil {
		t.Fatalf("note reply is not JSON: %v: %s", err, value)
	}
	return reply
}

func strPtr(value string) *string { return &value }

func TestHeadlessNotesEditAndVersionConflict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.db")
	seedNoteLibrary(t, path, []savedNote{{
		ID: "runbook", Title: "Deploy", Body: "restart nginx", Revision: "rev-1",
		Created: "2026-01-01T00:00:00Z", Updated: "2026-01-01T00:00:00Z",
	}}, nil)

	notes := newHeadlessNotes(path)
	defer notes.Close()
	found, err := notes.Query(tools.NoteQueryArgs{Action: "search", Query: "restart"})
	if err != nil {
		t.Fatal(err)
	}
	search := parseNoteReply(t, found)
	if len(search.Items) != 1 || search.Items[0].ID != "runbook" || search.Items[0].Version == "" {
		t.Fatalf("search = %s", found)
	}
	version := search.Items[0].Version

	edited, err := notes.Edit(tools.NoteEditArgs{Action: "update", ID: "runbook", Version: version, Body: strPtr("restart nginx\nthen reload")})
	if err != nil {
		t.Fatalf("edit failed: %v: %s", err, edited)
	}
	result := parseNoteReply(t, edited)
	if result.Status != "saved" || result.Note == nil || result.Note.Version == version {
		t.Fatalf("edit = %s", edited)
	}
	read, err := notes.Query(tools.NoteQueryArgs{Action: "read", ID: "runbook"})
	if err != nil || !strings.Contains(read, "then reload") {
		t.Fatalf("read after edit: %v %s", err, read)
	}

	stale, err := notes.Edit(tools.NoteEditArgs{Action: "update", ID: "runbook", Version: version, Body: strPtr("lost")})
	if err == nil {
		t.Fatal("a stale version was accepted")
	}
	if status := parseNoteReply(t, stale).Status; status != "version_conflict" {
		t.Fatalf("stale edit status = %q: %s", status, stale)
	}
	read, _ = notes.Query(tools.NoteQueryArgs{Action: "read", ID: "runbook"})
	if strings.Contains(read, "lost") {
		t.Fatal("the stale edit overwrote the stored note")
	}
}

func TestHeadlessNotesWriteLinkedFileAndRefuseAnExternalChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.db")
	file := filepath.Join(dir, "ops.md")
	if err := os.WriteFile(file, []byte("disk body"), 0o600); err != nil {
		t.Fatal(err)
	}
	seedNoteLibrary(t, path, []savedNote{{
		ID: "linked", Title: "Ops", Body: "disk body", Revision: "rev-l",
		Created: "2026-01-01T00:00:00Z", Updated: "2026-01-01T00:00:00Z",
		Link: &noteLink{Path: file},
	}}, nil)

	notes := newHeadlessNotes(path)
	defer notes.Close()
	read, err := notes.Query(tools.NoteQueryArgs{Action: "read", ID: "linked"})
	if err != nil {
		t.Fatal(err)
	}
	version := parseNoteReply(t, read).Version
	for _, body := range []string{"first edit", "second edit"} {
		value, err := notes.Edit(tools.NoteEditArgs{Action: "update", ID: "linked", Version: version, Body: strPtr(body)})
		if err != nil {
			t.Fatalf("edit %q: %v: %s", body, err, value)
		}
		result := parseNoteReply(t, value)
		if result.Status != "saved" || result.Note == nil {
			t.Fatalf("edit %q = %s", body, value)
		}
		version = result.Note.Version
		written, err := os.ReadFile(file)
		if err != nil || string(written) != body {
			t.Fatalf("file after %q = %q, %v", body, written, err)
		}
	}

	// Another program changes the file; the next edit must keep that text and
	// report the conflict instead of writing over it. The library edit itself
	// still succeeds, exactly as it does for the in-app agent.
	if err := os.WriteFile(file, []byte("outside edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, _ := notes.Edit(tools.NoteEditArgs{Action: "update", ID: "linked", Version: version, Body: strPtr("agent edit")})
	result := parseNoteReply(t, value)
	if result.Status != "file_conflict" || result.FileStatus != "conflict" || !result.LibrarySaved {
		t.Fatalf("conflict result = %s", value)
	}
	written, _ := os.ReadFile(file)
	if string(written) != "outside edit" {
		t.Fatalf("external text was overwritten: %q", written)
	}
}

func TestConfigStoreDataVersionSeesAnotherConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	watcher, err := configstore.OpenShared(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()
	before, err := watcher.DataVersion()
	if err != nil {
		t.Fatal(err)
	}
	// A second process is a second connection: Open, not OpenShared.
	writer, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Commit(configstore.Change{Put: []configstore.Entry{{Kind: "note", ID: "n", Payload: []byte(`{"data":{"id":"n"}}`)}}}); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	after, err := watcher.DataVersion()
	if err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Fatal("data_version did not change for another connection's commit")
	}
	// Its own commit must not look external.
	if _, err := watcher.Commit(configstore.Change{Put: []configstore.Entry{{Kind: "note", ID: "m", Payload: []byte(`{"data":{"id":"m"}}`)}}}); err != nil {
		t.Fatal(err)
	}
	own, err := watcher.DataVersion()
	if err != nil {
		t.Fatal(err)
	}
	if own != after {
		t.Fatal("data_version changed for the store's own commit")
	}
}

// startNoteHTTPHub creates the AI database the hub reads, starts the server with
// the given mode, and returns the token.
func startNoteHTTPHub(t *testing.T, options Options, mode string) (string, *AIHub) {
	t.Helper()
	db, err := store.Open(options.AIStorePath)
	if err != nil {
		t.Fatal(err)
	}
	token := mcp.GenerateToken()
	for key, value := range map[string]string{"ai.mcp.enabled": "on", "ai.mcp.mode": mode, "ai.mcp.token": token} {
		if err := db.SetSetting(key, value); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	hub := NewAIHub(options.AIStorePath)
	t.Cleanup(hub.Close)
	return token, hub
}

func TestMCPHTTPNotesFollowTheAccessLevel(t *testing.T) {
	_, options := newWorld(t)
	options.AIStorePath = filepath.Join(t.TempDir(), "ai.db")
	state := newTestState(t, options)
	t.Cleanup(state.Close)
	title, body := "Runbook", "restart nginx"
	if _, err := state.commitNoteMutation(noteMutation{Action: "create", Title: &title, Body: &body}); err != nil {
		t.Fatal(err)
	}
	state.save()

	token, hub := startNoteHTTPHub(t, options, "allow-all")
	hub.Add(state, nil)
	if err := hub.Reconfigure(); err != nil {
		t.Fatal(err)
	}
	query := mcpHTTPToolCall(t, state, hub.Address(), token, tools.NameNoteQuery, `{"action":"search","query":"nginx"}`)
	if query.IsError || !strings.Contains(query.Content[0].Text, "Runbook") {
		t.Fatalf("note_query = %+v", query)
	}
	var found noteToolReply
	if err := json.Unmarshal([]byte(query.Content[0].Text), &found); err != nil || len(found.Items) != 1 {
		t.Fatalf("note_query text = %s", query.Content[0].Text)
	}
	edit := mcpHTTPToolCall(t, state, hub.Address(), token, tools.NameNoteEdit,
		`{"action":"update","id":"`+found.Items[0].ID+`","version":"`+found.Items[0].Version+`","body":"restarted"}`)
	if edit.IsError || !strings.Contains(edit.Content[0].Text, `"status":"saved"`) {
		t.Fatalf("note_edit = %+v", edit)
	}
	read := mcpHTTPToolCall(t, state, hub.Address(), token, tools.NameNoteQuery, `{"action":"read","id":"`+found.Items[0].ID+`"}`)
	if read.IsError || !strings.Contains(read.Content[0].Text, "restarted") {
		t.Fatalf("note after edit = %+v", read)
	}
	db, err := store.Open(options.AIStorePath)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := db.Audit(5)
	db.Close()
	foundAudit := false
	for _, entry := range entries {
		if entry.Tool == tools.NameNoteEdit {
			foundAudit = true
			if strings.Contains(entry.Summary, "restarted") {
				t.Fatal("the audit recorded the note body")
			}
		}
	}
	if !foundAudit {
		t.Fatal("the HTTP note edit was not audited")
	}
}

// mcpHTTPToolNames lists the catalog the running HTTP server advertises.
func mcpHTTPToolNames(t *testing.T, state *State, address, token string) []string {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	done := make(chan []string, 1)
	go func() {
		request, err := http.NewRequest(http.MethodPost, address, strings.NewReader(body))
		if err != nil {
			done <- nil
			return
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		client := http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
		response, err := client.Do(request)
		if err != nil {
			done <- nil
			return
		}
		defer response.Body.Close()
		var out struct {
			Result struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
		}
		if json.NewDecoder(response.Body).Decode(&out) != nil {
			done <- nil
			return
		}
		names := make([]string, 0, len(out.Result.Tools))
		for _, tool := range out.Result.Tools {
			names = append(names, tool.Name)
		}
		done <- names
	}()
	deadline := time.NewTimer(6 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case names := <-done:
			if names == nil {
				t.Fatal("tools/list did not answer")
			}
			return names
		case <-ticker.C:
			state.Wake()
		case <-deadline.C:
			t.Fatal("tools/list did not finish")
		}
	}
}

func TestHubReloadsAWindowAfterAnotherProcessWrites(t *testing.T) {
	_, options := newWorld(t)
	options.AIStorePath = filepath.Join(t.TempDir(), "ai.db")
	state := newTestState(t, options)
	t.Cleanup(state.Close)
	hub := NewAIHub(options.AIStorePath)
	t.Cleanup(hub.Close)
	hub.Add(state, nil)

	// A second connection is another process for SQLite. notesWake is what the
	// directory watch calls after config.db changed.
	second, err := configstore.Open(options.StatePath, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	entries, local, _, err := splitState(savedState{Notes: []savedNote{{
		ID: "outside", Title: "Outside", Body: "written by another process", Revision: "rev-o",
		Created: "2026-01-01T00:00:00Z", Updated: "2026-01-01T00:00:00Z",
	}}}, newConfigBase(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Commit(configstore.Change{Put: entries, Local: map[string][]byte{localKey: local}}); err != nil {
		t.Fatal(err)
	}
	second.Close()

	// notesWake is the watch's goroutine in production; it posts to the window
	// thread, which this test drives with Wake.
	done := make(chan struct{})
	go func() {
		hub.notesWake()
		close(done)
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for finished := false; !finished; {
		state.Wake()
		select {
		case <-done:
			finished = true
		case <-deadline.C:
			t.Fatal("the external write did not reach the window")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	state.Wake()
	if !slices.ContainsFunc(state.notes.list, func(n savedNote) bool { return n.ID == "outside" && n.Body == "written by another process" }) {
		t.Fatalf("the window did not reload the external note: %+v", state.notes.list)
	}
}

func TestMCPHTTPNoteEditRequiresFullAccess(t *testing.T) {
	_, options := newWorld(t)
	options.AIStorePath = filepath.Join(t.TempDir(), "ai.db")
	state := newTestState(t, options)
	t.Cleanup(state.Close)
	title, body := "Runbook", "restart nginx"
	if _, err := state.commitNoteMutation(noteMutation{Action: "create", Title: &title, Body: &body}); err != nil {
		t.Fatal(err)
	}
	state.save()

	token, hub := startNoteHTTPHub(t, options, "ask")
	hub.Add(state, nil)
	if err := hub.Reconfigure(); err != nil {
		t.Fatal(err)
	}
	names := mcpHTTPToolNames(t, state, hub.Address(), token)
	hasQuery, hasEdit := false, false
	for _, name := range names {
		hasQuery = hasQuery || name == tools.NameNoteQuery
		hasEdit = hasEdit || name == tools.NameNoteEdit
	}
	if !hasQuery || hasEdit {
		t.Fatalf("ask-mode catalog = %v", names)
	}
	query := mcpHTTPToolCall(t, state, hub.Address(), token, tools.NameNoteQuery, `{"action":"search"}`)
	if query.IsError {
		t.Fatalf("note_query in ask mode = %+v", query)
	}
}

func TestServeAgentMCPNoteToolsFollowFullAccessAndAreAudited(t *testing.T) {
	world := newAgentWorld(t)
	saveAgentState(t, world, true)
	seedNoteLibrary(t, world.options.StatePath, []savedNote{{
		ID: "runbook", Title: "Deploy", Body: "restart nginx", Revision: "rev-1",
		Created: "2026-01-01T00:00:00Z", Updated: "2026-01-01T00:00:00Z",
	}}, nil)
	setMode := func(value string) {
		db, err := store.Open(world.options.AIDatabasePath)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if err := db.SetSetting(agentModeSetting, value); err != nil {
			t.Fatal(err)
		}
	}
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	query := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"note_query","arguments":{"action":"search","query":"nginx"}}}`

	setMode(AgentModeReadOnly)
	replies := serveAgent(t, world.options, list, query)
	if !strings.Contains(replies[1], "note_query") || strings.Contains(replies[1], "note_edit") {
		t.Fatalf("read-only note catalog = %s", replies[1])
	}
	if strings.Contains(replies[2], `"isError":true`) || !strings.Contains(replies[2], "Deploy") {
		t.Fatalf("read-only note_query = %s", replies[2])
	}

	setMode(AgentModeAllowAll)
	replies = serveAgent(t, world.options, list)
	if !strings.Contains(replies[1], "note_edit") {
		t.Fatalf("full-access note catalog = %s", replies[1])
	}
	// Read the version through the tool, then update it.
	replies = serveAgent(t, world.options, query)
	var envelope struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(replies[2]), &envelope); err != nil {
		t.Fatal(err)
	}
	found := parseNoteReply(t, envelope.Result.Content[0].Text)
	if len(found.Items) != 1 {
		t.Fatalf("note_query result = %s", envelope.Result.Content[0].Text)
	}
	edit := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"note_edit","arguments":{"action":"update","id":"` + found.Items[0].ID + `","version":"` + found.Items[0].Version + `","body":"restarted by the agent"}}}`
	replies = serveAgent(t, world.options, edit)
	if strings.Contains(replies[3], `"isError":true`) || !strings.Contains(replies[3], `\"status\":\"saved\"`) {
		t.Fatalf("note_edit = %s", replies[3])
	}
	audit, err := store.Open(world.options.AIDatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer audit.Close()
	entries, _ := audit.Audit(10)
	foundAudit := false
	for _, entry := range entries {
		if entry.Tool == tools.NameNoteEdit {
			foundAudit = true
			if strings.Contains(entry.Summary, "agent") {
				t.Fatal("the audit recorded the note body")
			}
		}
	}
	if !foundAudit {
		t.Fatal("the standalone note edit was not audited")
	}
}
