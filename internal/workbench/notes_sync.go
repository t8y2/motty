package workbench

import (
	"encoding/json"
	"fmt"
	"slices"

	"motty/internal/configstore"
)

func decodeNoteRecord(r configstore.Record) (savedNote, bool) {
	var p recordPayload
	var n savedNote
	if r.Kind != "note" || r.Deleted || json.Unmarshal(r.Payload, &p) != nil || json.Unmarshal(p.Data, &n) != nil || n.ID != r.ID {
		return n, false
	}
	return n, true
}

// Preserve the losing concurrent body before the ordinary stamped merge. The
// deterministic identity makes repeated and reversed exchanges idempotent.
func (s *State) preserveNoteConflicts(remote []configstore.Record) (bool, error) {
	if !s.flushNoteDraft() {
		return false, fmt.Errorf("%s", s.noteText("Finish composing or resolve the note save error before syncing.", "请完成输入法输入或处理笔记保存错误后再同步。"))
	}
	s.save()
	if s.hasUnsavedNotes() {
		return false, fmt.Errorf("notes could not be saved; draft retained")
	}
	local, err := s.config.Records()
	if err != nil {
		return false, err
	}
	byID := map[string]configstore.Record{}
	existing := map[string]bool{}
	for _, r := range local {
		if r.Kind == "note" {
			byID[r.ID] = r
			existing[r.ID] = true
		}
	}
	for _, r := range remote {
		if r.Kind == "note" {
			existing[r.ID] = true
		}
	}
	var put []configstore.Entry
	for _, r := range remote {
		if !s.config.Acceptable(r.Stamp) {
			continue
		}
		other, ok := decodeNoteRecord(r)
		if !ok {
			continue
		}
		held, ok := byID[r.ID]
		if !ok {
			continue
		}
		own, ok := decodeNoteRecord(held)
		if !ok || own.Body == other.Body {
			continue
		}
		if slices.Contains(own.Ancestors, noteRevision(other)) || slices.Contains(other.Ancestors, noteRevision(own)) {
			continue
		}
		loser := other
		if held.Stamp < r.Stamp {
			loser = own
		}
		loser = s.noteConflict(loser)
		id := loser.ID
		if existing[id] {
			continue
		}
		existing[id] = true
		if held.Stamp < r.Stamp && s.notes.selected == own.ID {
			s.notes.followConflict = id
		}

		put = append(put, configstore.Entry{Kind: "note", ID: id, Payload: mustNoteJSON(recordPayload{Data: mustNoteJSON(loser)})})
	}
	if len(put) == 0 {
		return false, nil
	}
	expected := map[configstore.Key][]byte{}
	for _, entry := range put {
		expected[configstore.Key{Kind: entry.Kind, ID: entry.ID}] = nil
	}
	_, err = s.config.Commit(configstore.Change{Writer: configWriter(), Put: put, Expected: expected})
	return err == nil, err
}
func (s *State) hasUnsavedNotes() bool {
	if len(s.notes.groupJobs) > 0 {
		return true
	}
	for id, d := range s.noteDrafts() {
		if s.noteIndex(id) >= 0 && d.changed() {
			return true
		}
	}
	if len(s.notes.list) == 0 && len(s.notes.folders) == 0 && len(s.lastSaved.Notes) == 0 && len(s.lastSaved.NoteFolders) == 0 {
		return false
	}
	if !s.hasSaved {
		return len(s.notes.list) > 0
	}
	return string(mustNoteJSON(s.notes.list)) != string(mustNoteJSON(s.lastSaved.Notes)) || string(mustNoteJSON(s.notes.folders)) != string(mustNoteJSON(s.lastSaved.NoteFolders))
}

func noteRevision(n savedNote) string {
	if n.Revision != "" {
		return n.Revision
	}
	return noteHash(n.Body)
}
func (s *State) noteConflict(n savedNote) savedNote {
	origin := n.ID
	n.ID = noteHash(origin + "\x00" + noteRevision(n))[:32]
	// A fork has its own lineage; old revisions belonged to the original note.
	n.Ancestors = nil
	n.Title += s.noteText(" (conflict ", "（冲突 ") + n.Updated + s.noteText(")", "）")
	return n
}
