package gate

import (
	"context"
	"errors"
	"motty/internal/ai/tools"
	"strings"
	"testing"
)

type noteTestBackend struct {
	fakeBackend
	calls       int
	response    string
	responseErr error
}

func (b *noteTestBackend) QueryNote(context.Context, tools.NoteQueryArgs) (string, error) {
	b.calls++
	if b.response != "" {
		return b.response, b.responseErr
	}
	return `{"items":[]}`, b.responseErr
}
func (b *noteTestBackend) EditNote(context.Context, tools.NoteEditArgs) (string, error) {
	b.calls++
	if b.response != "" {
		return b.response, b.responseErr
	}
	return `{"status":"saved"}`, b.responseErr
}
func TestNotesRequireInAppCapabilityAndRespectReadOnly(t *testing.T) {
	b := &noteTestBackend{}
	g := Gate{Backend: b, Approver: func(context.Context, Request) (bool, string, error) {
		t.Fatal("notes used terminal approval")
		return false, "", nil
	}}
	query := []byte(`{"action":"search"}`)
	edit := []byte(`{"action":"create","title":"test","body":"body"}`)
	if r := g.Run(context.Background(), tools.NameNoteQuery, query); !r.IsError || b.calls != 0 {
		t.Fatal("external caller reached notes")
	}
	g.NotesEnabled = true
	if r := g.Run(context.Background(), tools.NameNoteQuery, query); r.IsError {
		t.Fatal(r)
	}
	if r := g.Run(context.Background(), tools.NameNoteEdit, edit); r.IsError {
		t.Fatal(r)
	}
	g.Mode = ModeObserve
	if r := g.Run(context.Background(), tools.NameNoteEdit, edit); !r.IsError || b.calls != 2 {
		t.Fatal("read-only gate allowed edit")
	}
	if r := g.Run(context.Background(), tools.NameNoteQuery, []byte(`{"action":"invalid"}`)); !r.IsError || b.calls != 2 {
		t.Fatal("invalid query reached backend")
	}
}

func TestNoteQueryPreservesFailureStatusAndRemovesOldName(t *testing.T) {
	b := &noteTestBackend{}
	g := Gate{Backend: b, NotesEnabled: true}
	for _, status := range []string{"invalid_reference", "not_found", "version_conflict", "validation_error", "storage_error"} {
		b.response = `{"status":"` + status + `","message":"reason"}`
		b.responseErr = errors.New("domain failure")
		for _, call := range []struct{ name, args string }{
			{tools.NameNoteQuery, `{"action":"search"}`},
			{tools.NameNoteEdit, `{"action":"create","title":"n","body":"b"}`},
		} {
			r := g.Run(context.Background(), call.name, []byte(call.args))
			if !r.IsError || !strings.Contains(r.Text, `"status":"`+status+`"`) {
				t.Fatalf("lost status: %+v", r)
			}
		}
	}
	b.response = ""
	if r := g.Run(context.Background(), tools.NameNoteQuery, []byte(`{"action":"search"}`)); !r.IsError || !strings.Contains(r.Text, `"status":"storage_error"`) {
		t.Fatalf("transport failure lost: %+v", r)
	}
	count := b.calls
	if r := g.Run(context.Background(), "notes_query", []byte(`{"action":"search"}`)); !r.IsError || b.calls != count {
		t.Fatal("old tool executed")
	}
}
