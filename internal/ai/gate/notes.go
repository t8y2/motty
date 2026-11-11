package gate

import (
	"context"
	"encoding/json"
	"motty/internal/ai/tools"
)

// NotesBackend is optional: external MCP backends do not expose a note library.
type NotesBackend interface {
	QueryNote(context.Context, tools.NoteQueryArgs) (string, error)
	EditNote(context.Context, tools.NoteEditArgs) (string, error)
}

func (g Gate) runNotesTool(ctx context.Context, name string, raw json.RawMessage) Result {
	b, ok := g.Backend.(NotesBackend)
	if !g.NotesEnabled || !ok {
		return Result{Text: "note tools are unavailable", IsError: true}
	}
	// Local note edits use explicit user intent in the agent instructions, not
	// the terminal risk classifier or remote-file approval policy.
	var value string
	var err error
	if name == tools.NameNoteQuery {
		a, e := tools.DecodeNoteQuery(raw)
		if e != nil {
			return notesFailure("validation_error", e)
		}
		value, err = b.QueryNote(ctx, a)
	} else {
		a, e := tools.DecodeNoteEdit(raw)
		if e != nil {
			return notesFailure("validation_error", e)
		}
		value, err = b.EditNote(ctx, a)
	}
	var status struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal([]byte(value), &status)
	// Domain failures carry their precise status even alongside a Go error.
	// Only transport failures without a structured result need a fallback.
	if err != nil && status.Status == "" {
		return notesFailure("storage_error", err)
	}
	return Result{Text: value, IsError: err != nil || status.Status != "" && status.Status != "saved"}
}

func notesFailure(status string, err error) Result {
	raw, _ := json.Marshal(map[string]any{"status": status, "library_saved": false, "message": err.Error()})
	return Result{Text: string(raw), IsError: true}
}
