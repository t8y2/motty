package tools

import "testing"

func TestNoteToolsByCatalog(t *testing.T) {
	for _, spec := range All() {
		if spec.Name == NameNoteQuery || spec.Name == NameNoteEdit {
			t.Fatal("notes leaked into the window catalog")
		}
	}
	found := 0
	for _, spec := range InApp() {
		if spec.Name == NameNoteQuery || spec.Name == NameNoteEdit {
			found++
		}
	}
	if found != 2 {
		t.Fatal("in-app catalog missing notes")
	}
	read := ExternalNotes(false)
	if len(read) != 1 || read[0].Name != NameNoteQuery || read[0].Kind != Read {
		t.Fatalf("external read-only notes = %+v", read)
	}
	full := ExternalNotes(true)
	if len(full) != 2 || full[1].Name != NameNoteEdit || full[1].Kind != NoteWrite {
		t.Fatalf("external writable notes = %+v", full)
	}
	headless := Headless(HeadlessOptions{Notes: true})
	if !hasTool(headless, NameNoteQuery) || hasTool(headless, NameNoteEdit) {
		t.Fatalf("headless read-only notes = %v", toolNames(headless))
	}
	headless = Headless(HeadlessOptions{Notes: true, NoteWrites: true})
	if !hasTool(headless, NameNoteEdit) {
		t.Fatalf("full-access headless notes = %v", toolNames(headless))
	}
	if hasTool(Headless(HeadlessOptions{NoteWrites: true}), NameNoteQuery) {
		t.Fatal("note_edit was offered without note_query")
	}
}

func hasTool(specs []Spec, name string) bool {
	for _, spec := range specs {
		if spec.Name == name {
			return true
		}
	}
	return false
}

func toolNames(specs []Spec) []string {
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}

func TestNoteQueryActionParameters(t *testing.T) {
	for _, raw := range []string{
		`{"action":"search","references":[{"kind":"note","id":"n","title":"old","token":"note:old"}]}`,
		`{"action":"search","references":[{"kind":"note","id":"n","title":"n"}]}`,
		`{"action":"search","references":[]}`, `{"action":"read","id":"n","offset":1,"expected_version":"v"}`,
		`{"action":"tags","query":"Docker"}`, `{"action":"folders"}`, `{"action":"hosts"}`, `{"action":"host_groups"}`,
	} {
		if _, err := DecodeNoteQuery([]byte(raw)); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{
		`{"action":"search","id":"n"}`, `{"action":"read","id":"n","references":[]}`,
		`{"action":"tags","max_chars":100}`, `{"action":"read"}`, `{"action":"unknown"}`,
		`{"action":"search","references":[{"kind":"host","title":"alias"}]}`,
		`{"action":"search","references":[{"kind":"host_group","title":""}]}`,
		`{"action":"search","references":[{"kind":"other"}]}`, `{"action":"search","limit":51}`, `{"action":"search","limit":0}`, `{"action":"read","id":"n","max_chars":0}`,
		`{"action":"search","references":[{"kind":"tag","title":null,"token":"t"}]}`,
		`{"action":"search","references":[{"kind":"tag","title":"a","extra":true}]}`,
	} {
		if _, err := DecodeNoteQuery([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	a, err := DecodeNoteQuery([]byte(`{"action":"search"}`))
	if err != nil || a.Limit != 10 {
		t.Fatalf("defaults: %+v %v", a, err)
	}
	if _, ok := Lookup("notes_query"); ok {
		t.Fatal("removed tool still registered")
	}
	for _, s := range InApp() {
		if s.Name == "notes_query" {
			t.Fatal("removed tool advertised")
		}
	}
}
