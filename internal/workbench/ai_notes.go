package workbench

import (
	"context"
	"encoding/json"
	"errors"

	"motty/internal/ai/tools"
)

// References capture user focus and stable identities; they do not restrict tools.
type aiNoteReference struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	Token     string `json:"token"`
	Ambiguous bool   `json:"ambiguous,omitempty"`
}

type aiNoteResult struct {
	savedNote
	Version    string   `json:"version"`
	HostIDs    []string `json:"host_ids"`
	HostGroups []string `json:"host_groups"`
	FolderPath string   `json:"folder_path"`
}

const noteToolResultLimit = 28 << 10

func aiNotesJSON(v any) (string, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	if len(b) > noteToolResultLimit {
		return `{"status":"validation_error","message":"note response exceeds tool budget; narrow the query or reduce requested content"}`, nil
	}
	return string(b), nil
}

func (s *State) aiNoteSnapshot(n savedNote) aiNoteResult {
	n = s.noteBrowserNote(n)
	return noteSnapshotResult(n, s.noteBaseline(n), s.notes.folders)
}

func (b *aiBackend) QueryNote(ctx context.Context, a tools.NoteQueryArgs) (string, error) {
	return aiCall(b.state, ctx, func() (string, error) { return b.state.aiQueryNote(a) })
}
func (b *aiBackend) EditNote(ctx context.Context, a tools.NoteEditArgs) (string, error) {
	return aiCall(b.state, ctx, func() (string, error) { return b.state.aiEditNote(a) })
}

func noteQueryError(status, message string) (string, error) {
	value, err := aiNotesJSON(map[string]any{"status": status, "message": message})
	if err != nil {
		return "", err
	}
	return value, errors.New(message)
}

func (s *State) aiQueryNote(a tools.NoteQueryArgs) (string, error) {
	// Reading first brings this window's view to the store's records. A dirty
	// draft is never rebased onto a different stored version by a read.
	if a.Action == "read" || a.Action == "search" || a.Action == "tags" {
		id := ""
		if a.Action == "read" {
			id = a.ID
		}
		if err := s.refreshNoteQuery(id); err != nil {
			return noteStorageError(err)
		}
	}
	if a.Action == "search" || a.Action == "read" || a.Action == "folders" {
		if err := s.refreshNoteFolders(); err != nil {
			return noteStorageError(err)
		}
	}
	lib := s.noteLibrary()
	return lib.query(a)
}

func (s *State) aiEditNote(a tools.NoteEditArgs) (string, error) {
	result, err := s.commitNoteMutation(noteMutation{Action: a.Action, ID: a.ID, Version: a.Version, Title: a.Title, Body: a.Body, Tags: a.Tags, HostIDs: a.HostIDs, HostGroups: a.HostGroups, Folder: a.Folder, OldText: a.OldText, NewText: a.NewText, Name: a.Name, Parent: a.Parent})
	return noteEditJSON(result, err)
}

// noteEditJSON is the tool result both the in-app agent and the external
// servers return from note_edit.
func noteEditJSON(result noteMutationResult, err error) (string, error) {
	output := map[string]any{"status": result.Status, "library_saved": result.LibrarySaved}
	if result.Message != "" {
		output["message"] = result.Message
	}
	if result.FileStatus != "" {
		output["file_status"] = result.FileStatus
	}
	if n := result.Note; n != nil {
		output["note"] = map[string]any{"id": n.ID, "version": result.Version}
	}
	if f := result.Folder; f != nil {
		output["folder"] = map[string]any{"id": f.ID, "parent": f.Parent}
	}
	value, encodeErr := aiNotesJSON(output)
	if encodeErr != nil {
		return "", encodeErr
	}
	return value, err
}
func noteError(status string, err error) (string, error) {
	value, e := aiNotesJSON(map[string]any{"status": status, "library_saved": false, "message": err.Error()})
	if e != nil {
		return "", e
	}
	return value, err
}
func noteStorageError(err error) (string, error) {
	result, _ := noteStorageFailure(err)
	return noteError(result.Status, err)
}

// Keep note JSON below the agent's tool-output budget. Versions always cover
// the complete shared draft, not just the returned body chunk.
func aiNotePage(n aiNoteResult, offset, limit int) any {
	if limit <= 0 || limit > 4096 {
		limit = 4096
	}
	body := []rune(n.Body)
	start := min(max(offset, 0), len(body))
	end := min(start+limit, len(body))
	n.Body = string(body[start:end])
	return struct {
		aiNoteResult
		BodyOffset   int  `json:"body_offset"`
		BodyTotal    int  `json:"body_total"`
		NextOffset   int  `json:"next_offset"`
		BodyComplete bool `json:"body_complete"`
	}{n, start, len(body), end, end == len(body)}
}

func noteSummary(v string, limit int) string {
	r := []rune(v)
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return v
}
func noteSummaryTags(tags []string) []string {
	out := []string{}
	for _, t := range tags[:min(len(tags), 10)] {
		out = append(out, noteSummary(t, 60))
	}
	return out
}
