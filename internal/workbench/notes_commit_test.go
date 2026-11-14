package workbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"motty/internal/ai/tools"
	"motty/internal/configstore"
)

func rejectedNoteMutation(t *testing.T, s *State, a noteMutation, status string) {
	t.Helper()
	before := string(mustNoteJSON(s.notes.list))
	folders := string(mustNoteJSON(s.notes.folders))
	records, err := s.config.Records()
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.commitNoteMutation(a)
	if err == nil || result.Status != status || result.LibrarySaved {
		t.Fatalf("expected %s: %+v err=%v", status, result, err)
	}
	after, err := s.config.Records()
	if err != nil {
		t.Fatal(err)
	}
	if before != string(mustNoteJSON(s.notes.list)) || folders != string(mustNoteJSON(s.notes.folders)) || string(mustNoteJSON(records)) != string(mustNoteJSON(after)) {
		t.Fatal("rejected operation changed memory or storage")
	}
}
func noteString(v string) *string { return &v }

func TestNoteCommitRejectsStaleStoredVersionWithoutFork(t *testing.T) {
	_, options := newWorld(t)
	a := newTestState(t, options)
	aiNotesEditTest(t, a, `{"action":"create","title":"target","body":"base"}`)
	id := a.notes.list[0].ID
	b := newTestState(t, options)
	v := aiNotesVersion(b, id)
	aiNotesEditTest(t, a, aiNotesArgs(t, "update", id, aiNotesVersion(a, id), map[string]any{"body": "remote change"}))
	for _, edit := range []noteMutation{
		{Action: "update", ID: id, Version: v, Body: noteString("stale overwrite")},
		{Action: "update", ID: id, Version: v, HostIDs: &[]string{b.hosts[1].CatalogID}},
	} {
		rejectedNoteMutation(t, b, edit, "version_conflict")
	}
	reopened := newTestState(t, options)
	if len(reopened.notes.list) != 1 || reopened.notes.list[0].Body != "remote change" || len(reopened.aiNoteSnapshot(reopened.notes.list[0]).HostIDs) != 0 {
		t.Fatal("stale edit persisted or forked")
	}
	// A clean read refreshes the snapshot and supplies a usable new token.
	value, err := b.aiQueryNote(tools.NoteQueryArgs{Action: "read", ID: id})
	if err != nil {
		t.Fatal(err)
	}
	var n aiNoteResult
	if err = json.Unmarshal([]byte(value), &n); err != nil {
		t.Fatal(err)
	}
	if n.Body != "remote change" || n.Version == v {
		t.Fatalf("read failed to refresh: %s", value)
	}
	aiNotesEditTest(t, b, aiNotesArgs(t, "update", id, n.Version, map[string]any{"title": "fresh"}))
}

func TestNoteCommitFolderConcurrencyAndUIReferences(t *testing.T) {
	_, options := newWorld(t)
	a := newTestState(t, options)
	aiNotesEditTest(t, a, `{"action":"folder_create","name":"folder"}`)
	folder := a.notes.folders[0].ID
	b := newTestState(t, options)
	aiNotesEditTest(t, a, aiNotesArgs(t, "create", "", "", map[string]any{"title": "child", "body": "data", "folder": folder}))
	rejectedNoteMutation(t, b, noteMutation{Action: "folder_delete", ID: folder}, "validation_error")
	// A stale UI writer also cannot add a note into an already-deleted folder.
	aiNotesEditTest(t, a, aiNotesArgs(t, "update", a.notes.list[0].ID, aiNotesVersion(a, a.notes.list[0].ID), map[string]any{"folder": ""}))
	aiNotesEditTest(t, a, aiNotesArgs(t, "folder_delete", folder, "", map[string]any{}))
	b.notes.list = append(b.notes.list, savedNote{ID: "pending", Title: "draft", Body: "keep me", Folder: folder, Revision: noteID()})
	b.noteBuffer("pending")
	if err := b.saveWithResult(); err == nil {
		t.Fatal("stale UI saved a dangling folder reference")
	}
	reopened := newTestState(t, options)
	for _, n := range reopened.notes.list {
		if n.Folder == folder {
			t.Fatal("dangling folder reference persisted")
		}
	}
	if b.noteBrowserNote(b.notes.list[b.noteIndex("pending")]).Body != "keep me" {
		t.Fatal("UI draft lost on failure")
	}
}

func TestNoteCommitDoesNotSaveUnrelatedSettingsOrDrafts(t *testing.T) {
	_, options := newWorld(t)
	s := newTestState(t, options)
	aiNotesEditTest(t, s, `{"action":"create","title":"A","body":"first"}`)
	aiNotesEditTest(t, s, `{"action":"create","title":"B","body":"second"}`)
	a, b := s.notes.list[0].ID, s.notes.list[1].ID
	d := s.noteBuffer(b)
	d.body.Insert("pending ")
	originalLabel := s.hosts[0].Label
	s.hosts[0].Label = "unsaved setting"
	originalRevision := s.notes.list[0].Revision
	hostIDs := []string{s.hosts[1].CatalogID}
	result, err := s.commitNoteMutation(noteMutation{Action: "update", ID: a, Version: aiNotesVersion(s, a), HostIDs: &hostIDs})
	if err != nil || result.Status != "saved" {
		t.Fatalf("%+v %v", result, err)
	}
	if result.Note.Revision != originalRevision {
		t.Fatal("metadata edit advanced body lineage")
	}
	reopened := newTestState(t, options)
	if reopened.hosts[0].Label != originalLabel || reopened.notes.list[reopened.noteIndex(b)].Body != "second" {
		t.Fatal("tool saved unrelated state")
	}
	if !strings.Contains(d.body.String(), "pending ") || s.hosts[0].Label != "unsaved setting" {
		t.Fatal("tool replaced unrelated pending state")
	}
	// Its later ordinary save must retain the note tool's committed association.
	s.save()
	reopened = newTestState(t, options)
	if !noteHasHost(reopened.notes.list[reopened.noteIndex(a)].Tags, hostIDs[0]) {
		t.Fatal("ordinary save reverted committed metadata")
	}
}

func TestNoteCommitStorageFailureLeavesDraftAndLinkedFileUntouched(t *testing.T) {
	s, id, path := linkedFixture(t, "disk base")
	d := s.noteBuffer(id)
	d.body.Insert("user draft ")
	a := noteMutation{Action: "update", ID: id, Version: aiNotesVersion(s, id), Body: noteString("AI edit")}
	before := d.body.String()
	library := string(mustNoteJSON(s.notes.list))
	if err := s.config.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := s.commitNoteMutation(a)
	if err == nil || result.Status != "storage_error" || d.body.String() != before || string(mustNoteJSON(s.notes.list)) != library || readFile(t, path) != "disk base" {
		t.Fatalf("failed write had side effects: %+v %v", result, err)
	}
}

func TestNoteReadPinsPagesAndBoundsMetadata(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	aiNotesEditTest(t, s, aiNotesArgs(t, "create", "", "", map[string]any{"title": "large", "body": strings.Repeat("中文", 5000)}))
	id := s.notes.list[0].ID
	first, err := s.aiQueryNote(tools.NoteQueryArgs{Action: "read", ID: id})
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Version    string `json:"version"`
		NextOffset int    `json:"next_offset"`
	}
	_ = json.Unmarshal([]byte(first), &page)
	s.noteBuffer(id).body.Insert("user edit")
	result, err := s.aiQueryNote(tools.NoteQueryArgs{Action: "read", ID: id, Offset: page.NextOffset, ExpectedVersion: page.Version})
	if err == nil || !strings.Contains(result, `"status":"version_conflict"`) {
		t.Fatalf("mixed-version pages accepted: %s %v", result, err)
	}
	// Large attributes are valid records but cannot be silently truncated by read.
	title := strings.Repeat("long title", 5000)
	result, err = s.aiEditNote(tools.NoteEditArgs{Action: "create", Title: &title, Body: noteString("text")})
	if err != nil || !json.Valid([]byte(result)) || len(result) > noteToolResultLimit {
		t.Fatalf("unbounded mutation response: %s %v", result, err)
	}
	var created struct {
		Note struct {
			ID string `json:"id"`
		} `json:"note"`
	}
	_ = json.Unmarshal([]byte(result), &created)
	result, err = s.aiQueryNote(tools.NoteQueryArgs{Action: "read", ID: created.Note.ID})
	if err != nil || !json.Valid([]byte(result)) || !strings.Contains(result, `"status":"validation_error"`) {
		t.Fatalf("large metadata read not explicit: %s %v", result, err)
	}
}

func TestNoteCommitValidatesCurrentCatalog(t *testing.T) {
	_, o := newWorld(t)
	a := newTestState(t, o)
	aiNotesEditTest(t, a, `{"action":"create","title":"note","body":"body"}`)
	b := newTestState(t, o)
	id := b.notes.list[0].ID
	host := b.hosts[0].CatalogID
	a.hosts = a.hosts[1:]
	a.save()
	ids := []string{host}
	rejectedNoteMutation(t, b, noteMutation{Action: "update", ID: id, Version: aiNotesVersion(b, id), HostIDs: &ids}, "validation_error")
}

func TestNoteFolderCommitDoesNotDependOnUnrelatedSaveConflicts(t *testing.T) {
	for _, action := range []string{"folder_create", "folder_update", "folder_delete"} {
		t.Run(action, func(t *testing.T) {
			_, o := newWorld(t)
			a := newTestState(t, o)
			aiNotesEditTest(t, a, `{"action":"folder_create","name":"original"}`)
			id := a.notes.folders[0].ID
			b := newTestState(t, o)
			a.hosts[0].Label = "remote"
			a.save()
			b.hosts[0].Label = "pending local"
			m := noteMutation{Action: action, ID: id}
			if action == "folder_create" {
				m.ID = ""
				m.Name = noteString("created")
			}
			if action == "folder_update" {
				m.Name = noteString("renamed")
			}
			result, err := b.commitNoteMutation(m)
			if err != nil || !result.LibrarySaved {
				t.Fatalf("%+v %v", result, err)
			}
			restarted := newTestState(t, o)
			f, ok := restarted.noteFolder(result.Folder.ID)
			if action == "folder_delete" {
				if ok {
					t.Fatal("deleted folder retained")
				}
			} else if !ok || f.Name != *m.Name {
				t.Fatal("folder mutation not persisted")
			}
			if b.hosts[0].Label != "pending local" || restarted.hosts[0].Label != "remote" {
				t.Fatal("folder commit changed unrelated hosts")
			}
		})
	}
}

func TestNoteReadDoesNotResurrectDeletedRecord(t *testing.T) {
	_, o := newWorld(t)
	a := newTestState(t, o)
	aiNotesEditTest(t, a, `{"action":"create","title":"removed","body":"base"}`)
	id := a.notes.list[0].ID
	b := newTestState(t, o)
	if _, err := a.config.Commit(configstore.Change{Delete: []configstore.Key{{Kind: "note", ID: id}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.aiQueryNote(tools.NoteQueryArgs{Action: "read", ID: id}); err == nil {
		t.Fatal("deleted note still readable")
	}
	if b.noteIndex(id) >= 0 {
		t.Fatal("deleted clean note retained in cache")
	}
	if err := b.saveWithResult(); err != nil {
		t.Fatal(err)
	}
	if reopened := newTestState(t, o); reopened.noteIndex(id) >= 0 {
		t.Fatal("query/save resurrected deleted note")
	}
}

func TestNoteCommitConsumesSharedDraftWithCorrectLineageAndFile(t *testing.T) {
	for _, linked := range []bool{false, true} {
		t.Run(fmt.Sprint(linked), func(t *testing.T) {
			var s *State
			var id, path string
			if linked {
				s, id, path = linkedFixture(t, "base")
			} else {
				s = newTestState(t, freshOptions(t))
				aiNotesEditTest(t, s, `{"action":"create","title":"draft","body":"base"}`)
				id = s.notes.list[0].ID
			}
			old := s.notes.list[s.noteIndex(id)].Revision
			d := s.noteBuffer(id)
			d.body.Insert("user edit")
			body := d.body.String()
			result, err := s.commitNoteMutation(noteMutation{Action: "update", ID: id, Version: aiNotesVersion(s, id), Title: noteString("renamed")})
			if err != nil || !result.LibrarySaved {
				t.Fatalf("%+v %v", result, err)
			}
			if result.Note.Body != body || result.Note.Revision == old || !slices.Contains(result.Note.Ancestors, old) || d.changed() {
				t.Fatal("draft content or body lineage not committed correctly")
			}
			if linked && readFile(t, path) != body {
				t.Fatal("metadata update did not flush committed draft to linked file")
			}
			reopened := newTestState(t, s.options)
			n := reopened.notes.list[reopened.noteIndex(id)]
			if n.Body != body || n.Revision != result.Note.Revision {
				t.Fatal("shared draft commit not persisted")
			}
		})
	}
}

func TestNoteCommitLinkedFileConflictRetainsCommittedBodyAfterRestart(t *testing.T) {
	s, id, path := linkedFixture(t, "base")
	if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := s.commitNoteMutation(noteMutation{Action: "update", ID: id, Version: aiNotesVersion(s, id), Body: noteString("committed text")})
	if err != nil || !result.LibrarySaved || result.FileStatus != "conflict" {
		t.Fatalf("%+v %v", result, err)
	}
	reopened := newTestState(t, s.options)
	settle(t, reopened, "file conflict after restart", func() bool { return reopened.noteLinkInfo(id).conflict })
	if reopened.notes.list[reopened.noteIndex(id)].Body != "committed text" || readFile(t, path) != "external" {
		t.Fatal("restart discarded committed text or changed external file")
	}
}

func TestNoteMetadataQueriesRefreshBeforeMutation(t *testing.T) {
	_, o := newWorld(t)
	a := newTestState(t, o)
	b := newTestState(t, o)
	aiNotesEditTest(t, a, `{"action":"folder_create","name":"remote folder"}`)
	folder := a.notes.folders[0].ID
	a.hosts[0].Label = "current catalog name"
	a.save()
	folders, err := b.aiQueryNote(tools.NoteQueryArgs{Action: "folders"})
	if err != nil || !strings.Contains(folders, folder) {
		t.Fatalf("folder query stayed stale: %s %v", folders, err)
	}
	result, err := b.commitNoteMutation(noteMutation{Action: "folder_update", ID: folder, Name: noteString("renamed")})
	if err != nil || !result.LibrarySaved {
		t.Fatalf("refreshed folder cannot be edited: %+v %v", result, err)
	}
	hosts, err := b.aiQueryNote(tools.NoteQueryArgs{Action: "hosts"})
	if err != nil || !strings.Contains(hosts, "current catalog name") {
		t.Fatalf("catalog query stayed stale: %s %v", hosts, err)
	}
	ids := []string{a.hosts[0].CatalogID}
	result, err = b.commitNoteMutation(noteMutation{Action: "create", Title: noteString("associated"), Body: noteString("steps"), HostIDs: &ids})
	if err != nil || !result.LibrarySaved {
		t.Fatalf("catalog identity rejected: %+v %v", result, err)
	}
	h, ok := parseNoteHost(result.Note.Tags[0])
	if !ok || h.Label != "current catalog name" {
		t.Fatal("association used stale catalog metadata")
	}
}

func TestNoteCommitFileWriteFailureRetainsTextAfterRestart(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires POSIX directory permissions")
	}
	s, id, path := linkedFixture(t, "base")
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	result, err := s.commitNoteMutation(noteMutation{Action: "update", ID: id, Version: aiNotesVersion(s, id), Body: noteString("pending file save")})
	if err != nil || !result.LibrarySaved || result.FileStatus != "write_failed" {
		t.Fatalf("%+v %v", result, err)
	}
	reopened := newTestState(t, s.options)
	settle(t, reopened, "pending file notice after restart", func() bool { return reopened.noteLinkInfo(id).problem != "" })
	if reopened.notes.list[reopened.noteIndex(id)].Body != "pending file save" || readFile(t, path) != "base" {
		t.Fatal("failed file write was silently reverted on restart")
	}
	result, err = reopened.commitNoteMutation(noteMutation{Action: "update", ID: id, Version: aiNotesVersion(reopened, id), Title: noteString("renamed")})
	if err != nil || !result.LibrarySaved || result.FileStatus != "write_failed" || readFile(t, path) != "base" {
		t.Fatalf("metadata edit hid or retried pending file write: %+v %v", result, err)
	}
}

func TestNoteCommitPreservesUnknownRecordFields(t *testing.T) {
	for _, kind := range []string{"note", "note-folder"} {
		t.Run(kind, func(t *testing.T) {
			s := newTestState(t, freshOptions(t))
			action, query := "create", "read"
			attrs := map[string]any{"title": "before", "body": "base"}
			if kind == "note-folder" {
				action, query = "folder_create", "folders"
				attrs = map[string]any{"name": "before"}
			}
			aiNotesEditTest(t, s, aiNotesArgs(t, action, "", "", attrs))
			id := ""
			if kind == "note" {
				id = s.notes.list[0].ID
			} else {
				id = s.notes.folders[0].ID
			}
			db, err := s.loadNoteStore()
			if err != nil {
				t.Fatal(err)
			}
			var envelope, data map[string]json.RawMessage
			_ = json.Unmarshal(db.payload(kind, id), &envelope)
			_ = json.Unmarshal(envelope["d"], &data)
			envelope["future_envelope"] = json.RawMessage(`{"value":1}`)
			data["future_data"] = json.RawMessage(`{"nested":{"value":2}}`)
			envelope["d"] = mustNoteJSON(data)
			if _, err = s.config.Commit(configstore.Change{Put: []configstore.Entry{{Kind: kind, ID: id, Payload: mustNoteJSON(envelope)}}}); err != nil {
				t.Fatal(err)
			}
			q := tools.NoteQueryArgs{Action: query}
			if query == "read" {
				q.ID = id
			}
			if _, err = s.aiQueryNote(q); err != nil {
				t.Fatal(err)
			}
			edit := noteMutation{Action: "update", ID: id, Title: noteString("after")}
			if kind == "note" {
				edit.Version = aiNotesVersion(s, id)
			} else {
				edit.Action = "folder_update"
				edit.Title = nil
				edit.Name = noteString("after")
			}
			result, err := s.commitNoteMutation(edit)
			if err != nil || result.Status != "saved" {
				t.Fatalf("commit: %+v %v", result, err)
			}
			// A later ordinary UI save must not treat the kept fields as deletions.
			if err = s.saveWithResult(); err != nil {
				t.Fatal(err)
			}
			reopened := newTestState(t, s.options)
			stored, err := reopened.loadNoteStore()
			if err != nil {
				t.Fatal(err)
			}
			payload := string(stored.payload(kind, id))
			if !strings.Contains(payload, `"future_envelope":{"value":1}`) || !strings.Contains(payload, `"future_data":{"nested":{"value":2}}`) || !strings.Contains(payload, "after") {
				t.Fatalf("lossy update: %s", payload)
			}
		})
	}
}

func TestNoteCommitPreservesRawLinkedNotesAndVersions(t *testing.T) {
	s, id, path := linkedFixture(t, "base")
	db, err := s.loadNoteStore()
	if err != nil {
		t.Fatal(err)
	}
	var fields, note, link map[string]json.RawMessage
	_ = json.Unmarshal(db.local, &fields)
	_ = json.Unmarshal(db.notePayload(s.notes.list[0]), &note)
	_ = json.Unmarshal(note["link"], &link)
	note["future_note"] = json.RawMessage(`{"value":1}`)
	link["future_link"] = json.RawMessage(`{"value":2}`)
	note["link"] = mustNoteJSON(link)
	sibling := s.notes.list[0]
	sibling.ID = "untouched"
	sibling.Title = "sibling"
	sibling.Link = &noteLink{Path: filepath.Join(t.TempDir(), "sibling.md")}
	if err = os.WriteFile(sibling.Link.Path, []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	var siblingFields map[string]json.RawMessage
	_ = json.Unmarshal(mustNoteJSON(sibling), &siblingFields)
	siblingFields["future_sibling"] = json.RawMessage(`{"value":3}`)
	siblingRaw := mustNoteJSON(siblingFields)
	fields[linkedNotesKey] = mustNoteJSON([]json.RawMessage{mustNoteJSON(note), siblingRaw})
	fields["future_local"] = json.RawMessage(`{"value":4}`)
	if _, err = s.config.Commit(configstore.Change{Local: map[string][]byte{localKey: mustNoteJSON(fields)}}); err != nil {
		t.Fatal(err)
	}
	if err = s.refreshNoteQuery(""); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"first edit", "second edit"} {
		edit := noteMutation{Action: "update", ID: id, Version: aiNotesVersion(s, id), Title: noteString(title)}
		if title == "second edit" {
			edit.Body = noteString("edited")
		}
		result, err := s.commitNoteMutation(edit)
		if err != nil || result.Status != "saved" {
			t.Fatalf("commit: %+v %v", result, err)
		}
		db, err = s.loadNoteStore()
		if err != nil {
			t.Fatal(err)
		}
		raw := string(noteLocalPayload(db.local, id))
		if !strings.Contains(raw, `"future_note":{"value":1}`) || !strings.Contains(raw, `"future_link":{"value":2}`) || string(noteLocalPayload(db.local, sibling.ID)) != string(siblingRaw) || !strings.Contains(string(db.local), `"future_local":{"value":4}`) {
			t.Fatalf("lossy linked edit: %s", db.local)
		}
		if string(noteLocalPayload(s.configBase.local, id)) != raw {
			t.Fatal("accepted baseline lost raw fields")
		}
	}
	// Unknown-field changes are also part of the version read by the Agent.
	version := aiNotesVersion(s, id)
	note["future_note"] = json.RawMessage(`{"value":5}`)
	var current map[string]json.RawMessage
	_ = json.Unmarshal(noteLocalPayload(db.local, id), &current)
	current["future_note"] = note["future_note"]
	changed := setNoteLocalPayload(db.local, id, mustNoteJSON(current))
	if _, err = s.config.Commit(configstore.Change{Local: map[string][]byte{localKey: changed}}); err != nil {
		t.Fatal(err)
	}
	rejectedNoteMutation(t, s, noteMutation{Action: "update", ID: id, Version: version, Title: noteString("stale")}, "version_conflict")
	after, err := s.loadNoteStore()
	if err != nil {
		t.Fatal(err)
	}
	if string(after.local) != string(changed) || readFile(t, path) != "edited" {
		t.Fatal("stale linked edit changed storage or disk")
	}
	reopened := newTestState(t, s.options)
	if reopened.notes.list[reopened.noteIndex(id)].Title != "second edit" {
		t.Fatal("linked edit lost after reopen")
	}
}

func TestNoteCommitMetadataChecksActualLinkedFile(t *testing.T) {
	for _, state := range []string{"unchanged", "recovered", "missing", "conflict"} {
		t.Run(state, func(t *testing.T) {
			s, id, path := linkedFixture(t, "base")
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			switch state {
			case "recovered":
				s.noteLinkInfo(id).problem = "temporary file read error"
			case "missing":
				if err = os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "conflict":
				if err = os.WriteFile(path, []byte("external"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			result, err := s.commitNoteMutation(noteMutation{Action: "update", ID: id, Version: aiNotesVersion(s, id), Title: noteString("renamed")})
			want := state
			status := "file_" + state
			if state == "unchanged" || state == "recovered" {
				want, status = "saved", "saved"
			}
			if err != nil || !result.LibrarySaved || result.FileStatus != want || result.Status != status {
				t.Fatalf("inaccurate file result: %+v %v", result, err)
			}
			switch state {
			case "unchanged", "recovered":
				after, err := os.Stat(path)
				if err != nil || !after.ModTime().Equal(before.ModTime()) || readFile(t, path) != "base" {
					t.Fatal("metadata edit wrote the file")
				}
			case "missing":
				if _, err = os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("metadata edit recreated missing file")
				}
			case "conflict":
				if readFile(t, path) != "external" {
					t.Fatal("metadata edit overwrote external content")
				}
			}
		})
	}
}
