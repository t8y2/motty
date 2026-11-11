package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

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
	result := aiNoteResult{savedNote: n, Version: noteContentVersion(n, s.noteBaseline(n))}
	result.Tags = []string{}
	result.HostIDs = []string{}
	result.HostGroups = []string{}
	if n.Folder != "" {
		result.FolderPath, _ = s.noteFolderReferencePath(n.Folder)
	}
	for _, tag := range n.Tags {
		if h, ok := parseNoteHost(tag); ok {
			result.HostIDs = append(result.HostIDs, h.CatalogID)
		} else if g, ok := parseNoteHostGroup(tag); ok {
			result.HostGroups = append(result.HostGroups, g)
		} else {
			result.Tags = append(result.Tags, tag)
		}
	}
	return result
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
	if a.Action == "read" {
		i := s.noteIndex(a.ID)
		if i < 0 {
			return noteQueryError("not_found", "note not found")
		}
		snapshot := s.aiNoteSnapshot(s.notes.list[i])
		if a.Offset > 0 && a.ExpectedVersion == "" || a.ExpectedVersion != "" && a.ExpectedVersion != snapshot.Version {
			return noteQueryError("version_conflict", "note changed between pages; restart reading")
		}
		return aiNotesJSON(aiNotePage(snapshot, a.Offset, a.MaxChars))
	}
	catalog := []Host{}
	groupPaths := map[string]bool{}
	if a.Action == "search" || a.Action == "hosts" || a.Action == "host_groups" {
		db, err := s.loadNoteStore()
		if err != nil {
			return noteStorageError(err)
		}
		hosts, groups, err := db.catalog()
		if err != nil {
			return noteStorageError(err)
		}
		catalog = hosts
		addGroup := func(path string) {
			parts := strings.Split(path, "/")
			for i := range parts {
				if p := strings.Join(parts[:i+1], "/"); p != "" {
					groupPaths[p] = true
				}
			}
		}
		for _, g := range groups {
			addGroup(g.Name)
		}
		for _, h := range hosts {
			addGroup(h.Group)
		}
	}
	// Ordinary labels and structured associations are distinct namespaces.
	tags := []string{}
	seenTags := map[string]bool{}
	if a.Action == "search" || a.Action == "tags" {
		for _, n := range s.notes.list {
			for _, tag := range s.aiNoteSnapshot(n).Tags {
				if !seenTags[tag] {
					seenTags[tag] = true
					tags = append(tags, tag)
				}
			}
		}
		slices.Sort(tags)
	}
	for _, r := range a.References {
		valid := false
		switch r.Kind {
		case "note":
			valid = s.noteIndex(r.ID) >= 0
		case "folder":
			_, valid = s.noteFolder(r.ID)
		case "tag":
			valid = seenTags[r.Title]
		case "host":
			for _, h := range catalog {
				if h.CatalogID == r.ID {
					valid = true
					break
				}
			}
		case "host_group":
			valid = groupPaths[r.Title]
		}
		if !valid {
			return noteQueryError("invalid_reference", "reference no longer exists: "+r.Kind+" "+r.ID+" "+r.Title)
		}
	}
	matches := func(n aiNoteResult) bool {
		if len(a.References) == 0 {
			return true
		}
		for _, r := range a.References {
			switch r.Kind {
			case "note":
				if n.ID == r.ID {
					return true
				}
			case "folder":
				if s.noteFolderWithin(n.Folder, r.ID) {
					return true
				}
			case "tag":
				if slices.Contains(n.Tags, r.Title) {
					return true
				}
			case "host":
				if slices.Contains(n.HostIDs, r.ID) {
					return true
				}
			case "host_group":
				for _, g := range n.HostGroups {
					if g == r.Title || strings.HasPrefix(g, r.Title+"/") {
						return true
					}
				}
			}
		}
		return false
	}
	rows := []any{}
	contains := func(v string) bool { return strings.Contains(strings.ToLower(v), strings.ToLower(a.Query)) }
	switch a.Action {
	case "search":
		for _, n := range s.notes.list {
			item := s.aiNoteSnapshot(n)
			if !matches(item) || !s.noteMatchesSearch(s.noteBrowserNote(n), a.Query) {
				continue
			}
			title := noteSummary(item.Title, 160)
			body := []rune(item.Body)
			summary := string(body[:min(len(body), 300)])
			displayTags := noteSummaryTags(item.Tags)
			rows = append(rows, map[string]any{
				"id": item.ID, "title": title, "summary": summary, "version": item.Version,
				"folder": item.Folder, "folder_path": item.FolderPath, "tags": displayTags,
				"host_ids": item.HostIDs, "host_groups": item.HostGroups,
				"title_truncated": title != item.Title, "summary_truncated": len(body) > 300,
				"tags_truncated": !slices.Equal(displayTags, item.Tags),
			})
		}
	case "tags":
		for _, tag := range tags {
			if contains(tag) {
				rows = append(rows, map[string]string{"id": tag, "name": tag})
			}
		}
	case "folders":
		for _, f := range s.notes.folders {
			path, ok := s.noteFolderReferencePath(f.ID)
			if !ok {
				return noteQueryError("storage_error", "invalid folder hierarchy")
			}
			if contains(path) {
				rows = append(rows, map[string]any{"id": f.ID, "name": f.Name, "parent": f.Parent, "path": path, "linked": f.Link != nil})
			}
		}
	case "hosts":
		for _, h := range catalog {
			if contains(h.Label + "\n" + h.Address) {
				rows = append(rows, map[string]string{"id": h.CatalogID, "name": h.Label, "address": h.Address, "group": h.Group})
			}
		}
	case "host_groups":
		paths := []string{}
		for path := range groupPaths {
			if contains(path) {
				paths = append(paths, path)
			}
		}
		slices.Sort(paths)
		for _, path := range paths {
			rows = append(rows, map[string]string{"id": path, "name": path})
		}
	default:
		return noteQueryError("validation_error", "unknown query action")
	}
	limit := a.Limit
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	start := min(max(a.Offset, 0), len(rows))
	end := start
	// Measure the complete serialized response, including the envelope and escaping.
	response := func(end int) map[string]any {
		var next any
		if end < len(rows) {
			next = end
		}
		return map[string]any{"items": rows[start:end], "total": len(rows), "next_offset": next}
	}
	for end < len(rows) && end-start < limit {
		encoded, err := json.Marshal(response(end + 1))
		if err != nil {
			return "", err
		}
		if len(encoded) > noteToolResultLimit {
			if end == start {
				return noteQueryError("validation_error", "note response exceeds tool budget; narrow the query or reduce requested content")
			}
			break
		}
		end++
	}
	return aiNotesJSON(response(end))
}

func (s *State) aiEditNote(a tools.NoteEditArgs) (string, error) {
	result, err := s.commitNoteMutation(noteMutation{Action: a.Action, ID: a.ID, Version: a.Version, Title: a.Title, Body: a.Body, Tags: a.Tags, HostIDs: a.HostIDs, HostGroups: a.HostGroups, Folder: a.Folder, OldText: a.OldText, NewText: a.NewText, Name: a.Name, Parent: a.Parent})
	output := map[string]any{"status": result.Status, "library_saved": result.LibrarySaved}
	if result.Message != "" {
		output["message"] = result.Message
	}
	if result.FileStatus != "" {
		output["file_status"] = result.FileStatus
	}
	if n := result.Note; n != nil {
		output["note"] = map[string]any{"id": n.ID, "version": noteContentVersion(*n, s.noteBaseline(*n))}
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
