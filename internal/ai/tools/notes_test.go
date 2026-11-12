package tools

import "testing"

func TestNoteToolsStayOutOfExternalCatalog(t *testing.T) {
	for _, spec := range All() {
		if spec.Name == NameNoteQuery || spec.Name == NameNoteEdit {
			t.Fatal("notes leaked into MCP catalog")
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
}

func TestNoteQueryActionParameters(t *testing.T) {
	for _, raw := range []string{
		`{"action":"search","references":[{"kind":"note","id":"n","title":"old","token":"note:old"}]}`,
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
		`{"action":"search","references":[{"kind":"note","id":"n","title":"n"}]}`,
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
