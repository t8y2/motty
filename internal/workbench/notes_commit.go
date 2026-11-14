package workbench

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"motty/internal/configstore"
)

// A note operation has one storage commit. Candidate construction and all
// validation precede changes to editors, the library, or linked files.
type noteMutation struct {
	Action, ID, Version                                 string
	Title, Body, Folder, OldText, NewText, Name, Parent *string
	Tags, HostIDs, HostGroups                           *[]string
}

type noteStoreSnapshot struct {
	entries []configstore.Entry
	local   []byte
	notes   []savedNote
	folders []savedNoteFolder
}

func (s *State) loadNoteStore() (noteStoreSnapshot, error) {
	var out noteStoreSnapshot
	if s.config == nil || s.saveBlocked || s.pauseSave > 0 {
		return out, errors.New("note storage is unavailable")
	}
	entries, local, err := s.config.Load()
	if err != nil {
		return out, err
	}
	return decodeNoteStore(entries, local[localKey])
}
func decodeNoteStore(entries []configstore.Entry, rawLocal []byte) (noteStoreSnapshot, error) {
	var out noteStoreSnapshot
	var err error
	out.entries = entries
	out.local = rawLocal
	for _, e := range entries {
		var p recordPayload
		if e.Kind != "note" && e.Kind != "note-folder" {
			continue
		}
		if err = json.Unmarshal(e.Payload, &p); err != nil {
			return out, err
		}
		if e.Kind == "note" {
			var n savedNote
			if err = json.Unmarshal(p.Data, &n); err != nil {
				return out, err
			}
			out.notes = append(out.notes, n)
		} else {
			var f savedNoteFolder
			if err = json.Unmarshal(p.Data, &f); err != nil {
				return out, err
			}
			out.folders = append(out.folders, f)
		}
	}
	var fields map[string]json.RawMessage
	if len(out.local) > 0 {
		if err = json.Unmarshal(out.local, &fields); err != nil {
			return out, err
		}
	}
	var linked []savedNote
	var folders []savedNoteFolder
	if raw := fields[linkedNotesKey]; len(raw) > 0 {
		if err = json.Unmarshal(raw, &linked); err != nil {
			return out, err
		}
	}
	if raw := fields[linkedNoteFoldersKey]; len(raw) > 0 {
		if err = json.Unmarshal(raw, &folders); err != nil {
			return out, err
		}
	}
	out.notes = append(out.notes, linked...)
	out.folders = append(out.folders, folders...)
	return out, nil
}
func (db noteStoreSnapshot) payload(kind, id string) []byte {
	for _, e := range db.entries {
		if e.Kind == kind && e.ID == id {
			return e.Payload
		}
	}
	return nil
}
func noteKindSnapshot(entries []configstore.Entry, kind string) []configstore.Entry {
	out := []configstore.Entry{}
	for _, e := range entries {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}
func noteContentVersion(n savedNote, raw []byte) string {
	return "n2:" + noteHash(string(raw)) + ":" + noteHash(string(mustNoteJSON(n)))
}
func (s *State) noteBaseline(n savedNote) []byte {
	if n.Link == nil {
		return s.configBase.payloads[configstore.Key{Kind: "note", ID: n.ID}]
	}
	return noteLocalPayload(s.configBase.local, n.ID)
}

// Keep raw linked-note objects: their unknown fields participate in versions
// and must survive edits to this note and to other entries in the array.
func noteLocalPayload(raw []byte, id string) []byte {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	var notes []json.RawMessage
	_ = json.Unmarshal(fields[linkedNotesKey], &notes)
	for _, raw := range notes {
		var n savedNote
		_ = json.Unmarshal(raw, &n)
		if n.ID == id {
			return raw
		}
	}
	return nil
}

// Both inputs come from validated local documents. Replace only the target
// raw object, preserving array order and unrelated objects and local fields.
func setNoteLocalPayload(raw []byte, id string, note []byte) []byte {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	var notes []json.RawMessage
	_ = json.Unmarshal(fields[linkedNotesKey], &notes)
	found := false
	for i, raw := range notes {
		var n savedNote
		_ = json.Unmarshal(raw, &n)
		if n.ID != id {
			continue
		}
		found = true
		if note == nil {
			notes = slices.Delete(notes, i, i+1)
		} else {
			notes[i] = note
		}
		break
	}
	if !found && note != nil {
		notes = append(notes, note)
	}
	fields[linkedNotesKey] = mustNoteJSON(notes)
	return mustNoteJSON(fields)
}

func (db noteStoreSnapshot) notePayload(n savedNote) []byte {
	if n.Link != nil {
		return noteLocalPayload(db.local, n.ID)
	}
	return db.payload("note", n.ID)
}

// Use the same typed projection and lossless patch as ordinary UI saves.
// Expected still binds the entire raw baseline inside the commit transaction.
func (db noteStoreSnapshot) stageRecord(change *configstore.Change, key configstore.Key, data []byte) ([]byte, error) {
	old := db.payload(key.Kind, key.ID)
	var record recordPayload
	if len(old) > 0 {
		if err := json.Unmarshal(old, &record); err != nil {
			return nil, err
		}
	} else {
		for _, e := range db.entries {
			var p recordPayload
			_ = json.Unmarshal(e.Payload, &p)
			record.Ord = max(record.Ord, p.Ord)
		}
		record.Ord++
	}
	record.Data = data
	payload := mustNoteJSON(record)
	change.Expected[key] = old
	if len(old) == 0 {
		change.Put = append(change.Put, configstore.Entry{Kind: key.Kind, ID: key.ID, Payload: payload})
		return payload, nil
	}
	before, err := projectRecord(key, old)
	if err != nil {
		return nil, err
	}
	after, err := projectRecord(key, payload)
	if err != nil {
		return nil, err
	}
	patch := configstore.JSONChange{Before: before, After: after}
	payload, err = patch.Apply(old)
	if err != nil {
		return nil, err
	}
	change.Patch = append(change.Patch, configstore.PatchEntry{Key: key, JSONChange: patch})
	return payload, nil
}

type noteMutationResult struct {
	Status              string
	LibrarySaved        bool
	Note                *savedNote
	Folder              *savedNoteFolder
	FileStatus, Message string
	// Version is the committed note's version, for the tool's reply. It is
	// computed from what was written, not from the caller's view.
	Version string
}

func noteMutationFailure(status string, err error) (noteMutationResult, error) {
	return noteMutationResult{Status: status, Message: err.Error()}, err
}
func noteStorageFailure(err error) (noteMutationResult, error) {
	if errors.Is(err, configstore.ErrConflict) {
		return noteMutationFailure("version_conflict", fmt.Errorf("note library changed; read again: %w", err))
	}
	return noteMutationFailure("storage_error", err)
}

// The editor and tools share body lineage and record-size rules.
func prepareNoteUpdate(previous, next savedNote, now time.Time) (savedNote, error) {
	if previous.ID == "" {
		next.Ancestors = nil
	} else if next.Body != previous.Body {
		next.Revision = noteID()
		next.Ancestors = slices.Clone(previous.Ancestors)
		rev := noteRevision(previous)
		if !slices.Contains(next.Ancestors, rev) {
			next.Ancestors = append(next.Ancestors, rev)
		}
	}
	next.Updated = now.UTC().Format(time.RFC3339Nano)
	if len(mustNoteJSON(recordPayload{Data: mustNoteJSON(next)})) > configstore.MaxPayload {
		return next, errors.New("note exceeds the 1 MiB record limit")
	}
	return next, nil
}
func validateNoteGraph(notes []savedNote, folders []savedNoteFolder) error {
	byID := map[string]savedNoteFolder{}
	for _, f := range folders {
		byID[f.ID] = f
	}
	for _, n := range notes {
		if n.Folder != "" {
			if _, ok := byID[n.Folder]; !ok {
				return fmt.Errorf("note folder no longer exists: %s", n.Folder)
			}
		}
	}
	for _, f := range folders {
		seen := map[string]bool{f.ID: true}
		parent := f.Parent
		for parent != "" {
			if seen[parent] {
				return errors.New("folder cycle")
			}
			seen[parent] = true
			p, ok := byID[parent]
			if !ok {
				return errors.New("parent folder no longer exists")
			}
			parent = p.Parent
		}
	}
	return nil
}
func ordinaryNoteFolder(folders []savedNoteFolder, id string) error {
	if id == "" {
		return nil
	}
	for _, f := range folders {
		if f.ID == id {
			if f.Link != nil {
				return errors.New("linked folders cannot be managed by the agent")
			}
			return nil
		}
	}
	return errors.New("folder not found")
}

func (s *State) acceptNoteCommit(n savedNote, payload, local []byte) {
	i := s.noteIndex(n.ID)
	if i < 0 {
		s.notes.list = append(s.notes.list, n)
	} else {
		s.notes.list[i] = n
	}
	if d := s.notes.drafts[n.ID]; d != nil {
		replaceNoteEditorValue(&d.title, n.Title)
		replaceNoteEditorValue(&d.body, n.Body)
		replaceNoteEditorValue(&d.tags, strings.Join(n.Tags, ", "))
		d.draft = n
		d.problem = ""
	}
	if n.Link == nil {
		key := configstore.Key{Kind: "note", ID: n.ID}
		s.configBase.payloads[key] = payload
		var p recordPayload
		_ = json.Unmarshal(payload, &p)
		s.configBase.ords[key] = p.Ord
		s.configBase.maxOrd = max(s.configBase.maxOrd, p.Ord)
	} else {
		s.acceptLinkedLocal(local, n.ID)
	}
	i = slices.IndexFunc(s.lastSaved.Notes, func(x savedNote) bool { return x.ID == n.ID })
	if i < 0 {
		s.lastSaved.Notes = append(s.lastSaved.Notes, n)
	} else {
		s.lastSaved.Notes[i] = n
	}
}
func (s *State) acceptLinkedLocal(raw []byte, id string) {
	// Advance only this note's raw baseline, preserving unrelated local baselines.
	s.configBase.local = setNoteLocalPayload(s.configBase.local, id, noteLocalPayload(raw, id))
}

// noteLibrary is this window's view of the library. The window owns the drafts,
// editors and watches; the library owns the records and the file policy.
func (s *State) noteLibrary() noteLibrary {
	if s.noteLinks == nil {
		s.noteLinks = map[string]*noteLinkState{}
	}
	return noteLibrary{
		store:        s.config,
		base:         s.configBase,
		notes:        s.notes.list,
		folders:      s.notes.folders,
		drafts:       s.notes.drafts,
		links:        s.noteLinks,
		now:          s.options.WallNow,
		browser:      s.noteBrowserNote,
		baseline:     s.noteBaseline,
		text:         s.noteText,
		loadSnapshot: s.loadNoteStore,
		afterNote:    s.acceptNoteCommit,
		afterFolder:  s.acceptFolderCommit,
		notify:       s.noteCommitNotification,
	}
}

func (s *State) commitNoteMutation(a noteMutation) (noteMutationResult, error) {
	db, err := s.loadNoteStore()
	if err != nil {
		return noteStorageFailure(err)
	}
	lib := s.noteLibrary()
	return lib.edit(db, a)
}

// acceptFolderCommit applies a committed folder to this window's view and base.
func (s *State) acceptFolderCommit(f savedNoteFolder, payload []byte, deleting bool) {
	update := func(list []savedNoteFolder) []savedNoteFolder {
		i := slices.IndexFunc(list, func(x savedNoteFolder) bool { return x.ID == f.ID })
		if deleting {
			if i >= 0 {
				list = slices.Delete(list, i, i+1)
			}
			return list
		}
		if i < 0 {
			return append(list, f)
		}
		list[i] = f
		return list
	}
	s.notes.folders = update(s.notes.folders)
	s.lastSaved.NoteFolders = update(s.lastSaved.NoteFolders)
	key := configstore.Key{Kind: "note-folder", ID: f.ID}
	if deleting {
		delete(s.configBase.payloads, key)
		delete(s.configBase.ords, key)
		if s.notes.folder == f.ID {
			s.notes.folder = ""
		}
		return
	}
	s.configBase.payloads[key] = payload
	var p recordPayload
	_ = json.Unmarshal(payload, &p)
	s.configBase.ords[key] = p.Ord
	s.configBase.maxOrd = max(s.configBase.maxOrd, p.Ord)
}

func (s *State) noteCommitNotification(shared bool) {
	s.configBase.stored = true
	if shared {
		s.scheduleSync()
	}
	if s.options.Group != nil {
		s.options.Group.settingsChanged(s)
	}
	s.invalidateNoteViews()
}

// Refresh only clean note records for queries. Dirty drafts are never rebased
// onto a different stored version by a read operation.
func (s *State) refreshNoteQuery(id string) error {
	db, err := s.loadNoteStore()
	if err != nil {
		return err
	}
	for _, n := range slices.Clone(s.notes.list) {
		if id != "" && id != n.ID {
			continue
		}
		baseline := s.noteBaseline(n)
		if len(baseline) == 0 || slices.ContainsFunc(db.notes, func(x savedNote) bool { return x.ID == n.ID }) {
			continue
		}
		var old savedNote
		if n.Link != nil {
			_ = json.Unmarshal(baseline, &old)
		} else {
			var p recordPayload
			_ = json.Unmarshal(baseline, &p)
			_ = json.Unmarshal(p.Data, &old)
		}
		old = s.normalizeNoteHosts(old)
		if string(mustNoteJSON(s.noteBrowserNote(n))) != string(mustNoteJSON(old)) {
			return fmt.Errorf("%w: note was deleted while a local draft exists", configstore.ErrConflict)
		}
		s.notes.list = slices.DeleteFunc(s.notes.list, func(x savedNote) bool { return x.ID == n.ID })
		s.lastSaved.Notes = slices.DeleteFunc(s.lastSaved.Notes, func(x savedNote) bool { return x.ID == n.ID })
		delete(s.notes.drafts, n.ID)
		if n.Link != nil {
			s.acceptLinkedLocal(db.local, n.ID)
		} else {
			key := configstore.Key{Kind: "note", ID: n.ID}
			delete(s.configBase.payloads, key)
			delete(s.configBase.ords, key)
		}
		if s.notes.selected == n.ID {
			s.selectNoteWithoutSave("")
		}
	}
	for _, n := range db.notes {
		if id != "" && id != n.ID {
			continue
		}
		i := s.noteIndex(n.ID)
		baseline := s.noteBaseline(n)
		raw := db.notePayload(n)
		if noteHash(string(raw)) == noteHash(string(baseline)) {
			continue
		}
		if i >= 0 {
			visible := s.noteBrowserNote(s.notes.list[i])
			var old savedNote
			if n.Link != nil {
				_ = json.Unmarshal(baseline, &old)
			} else {
				var p recordPayload
				_ = json.Unmarshal(baseline, &p)
				_ = json.Unmarshal(p.Data, &old)
			}
			old = s.normalizeNoteHosts(old)
			if string(mustNoteJSON(visible)) != string(mustNoteJSON(old)) {
				return fmt.Errorf("%w: local draft and stored note changed; resolve before reading", configstore.ErrConflict)
			}
			s.notes.list[i] = n
			if d := s.notes.drafts[n.ID]; d != nil {
				replaceNoteEditorValue(&d.title, n.Title)
				replaceNoteEditorValue(&d.body, n.Body)
				replaceNoteEditorValue(&d.tags, strings.Join(n.Tags, ", "))
				d.draft = n
			}
		} else {
			s.notes.list = append(s.notes.list, n)
		}
		if n.Link != nil {
			s.acceptLinkedLocal(db.local, n.ID)
		} else {
			key := configstore.Key{Kind: "note", ID: n.ID}
			s.configBase.payloads[key] = raw
			var p recordPayload
			_ = json.Unmarshal(raw, &p)
			s.configBase.ords[key] = p.Ord
			s.configBase.maxOrd = max(s.configBase.maxOrd, p.Ord)
		}
		j := slices.IndexFunc(s.lastSaved.Notes, func(x savedNote) bool { return x.ID == n.ID })
		if j < 0 {
			s.lastSaved.Notes = append(s.lastSaved.Notes, n)
		} else {
			s.lastSaved.Notes[j] = n
		}
	}
	return nil
}

// All local note writers validate their projected graph against a single
// guarded snapshot, including new members not present in the writer's cache.
func guardNoteGraphChange(store *configstore.Store, change *configstore.Change) error {
	relevant := false
	isNote := func(kind string) bool { return kind == "note" || kind == "note-folder" }
	for _, e := range change.Put {
		relevant = relevant || isNote(e.Kind)
	}
	for _, e := range change.Patch {
		relevant = relevant || isNote(e.Kind)
	}
	for _, e := range change.Delete {
		relevant = relevant || isNote(e.Kind)
	}
	// Detect linked-list changes in the local document as well.
	if p, ok := change.LocalPatch[localKey]; ok {
		var before, after map[string]json.RawMessage
		_ = json.Unmarshal(p.Before, &before)
		_ = json.Unmarshal(p.After, &after)
		for _, key := range []string{linkedNotesKey, linkedNoteFoldersKey} {
			relevant = relevant || string(before[key]) != string(after[key])
		}
	}
	if !relevant {
		return nil
	}
	entries, locals, err := store.Load()
	if err != nil {
		return err
	}
	original, err := decodeNoteStore(entries, locals[localKey])
	if err != nil {
		return err
	}
	rows := map[configstore.Key][]byte{}
	for _, e := range entries {
		rows[configstore.Key{Kind: e.Kind, ID: e.ID}] = e.Payload
	}
	for _, e := range change.Put {
		rows[configstore.Key{Kind: e.Kind, ID: e.ID}] = e.Payload
	}
	for _, e := range change.Patch {
		key := e.Key
		if !isNote(key.Kind) {
			continue
		}
		raw, e2 := e.JSONChange.Apply(rows[key])
		if e2 != nil {
			return e2
		}
		rows[key] = raw
	}
	for _, key := range change.Delete {
		delete(rows, key)
	}
	projected := []configstore.Entry{}
	for key, raw := range rows {
		projected = append(projected, configstore.Entry{Kind: key.Kind, ID: key.ID, Payload: raw})
	}
	local := locals[localKey]
	if raw, ok := change.Local[localKey]; ok {
		local = raw
	}
	if p, ok := change.LocalPatch[localKey]; ok {
		if len(local) == 0 {
			local = []byte("{}")
		}
		local, err = p.Apply(local)
		if err != nil {
			return err
		}
	}
	next, err := decodeNoteStore(projected, local)
	if err != nil {
		return err
	}
	if err = validateNoteGraph(next.notes, next.folders); err != nil {
		// Do not silently normalize imported dangling relationships during a save.
		if validateNoteGraph(original.notes, original.folders) == nil {
			return err
		}
		return fmt.Errorf("repair invalid note folder relationships before saving: %w", err)
	}
	if change.ExpectedKinds == nil {
		change.ExpectedKinds = map[string][]configstore.Entry{}
	}
	for _, kind := range []string{"note", "note-folder"} {
		change.ExpectedKinds[kind] = noteKindSnapshot(entries, kind)
	}
	if change.ExpectedLocal == nil {
		change.ExpectedLocal = map[string][]byte{}
	}
	change.ExpectedLocal[localKey] = locals[localKey]
	return nil
}

func uniqueNoteFragment(body, old string) bool {
	if old == "" {
		return false
	}
	at := strings.Index(body, old)
	return at >= 0 && strings.Index(body[at+1:], old) < 0
}

func (db noteStoreSnapshot) catalog() ([]Host, []savedHostGroup, error) {
	persisted, _, err := joinState(db.entries, db.local)
	if err != nil {
		return nil, nil, err
	}
	hosts, _ := decodeHosts(persisted.Hosts, nil, 1)
	return hosts, persisted.HostGroups, nil
}

func (s *State) refreshNoteFolders() error {
	db, err := s.loadNoteStore()
	if err != nil {
		return err
	}
	// A folder query must not discard pending navigation-tree edits.
	known := []savedNoteFolder{}
	for key, raw := range s.configBase.payloads {
		if key.Kind == "note-folder" {
			var p recordPayload
			var f savedNoteFolder
			if err = json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if err = json.Unmarshal(p.Data, &f); err != nil {
				return err
			}
			known = append(known, f)
		}
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(s.configBase.local, &fields)
	var linked []savedNoteFolder
	_ = json.Unmarshal(fields[linkedNoteFoldersKey], &linked)
	known = append(known, linked...)
	same := len(known) == len(s.notes.folders)
	for _, f := range s.notes.folders {
		same = same && slices.ContainsFunc(known, func(x savedNoteFolder) bool { return string(mustNoteJSON(x)) == string(mustNoteJSON(f)) })
	}
	if !same {
		return fmt.Errorf("%w: save pending folder edits before querying folders", configstore.ErrConflict)
	}
	s.notes.folders = slices.Clone(db.folders)
	s.lastSaved.NoteFolders = slices.Clone(db.folders)
	for key := range s.configBase.payloads {
		if key.Kind == "note-folder" {
			delete(s.configBase.payloads, key)
			delete(s.configBase.ords, key)
		}
	}
	for _, e := range db.entries {
		if e.Kind == "note-folder" {
			key := configstore.Key{Kind: e.Kind, ID: e.ID}
			s.configBase.payloads[key] = e.Payload
			var p recordPayload
			_ = json.Unmarshal(e.Payload, &p)
			s.configBase.ords[key] = p.Ord
			s.configBase.maxOrd = max(s.configBase.maxOrd, p.Ord)
		}
	}
	var actual map[string]json.RawMessage
	_ = json.Unmarshal(db.local, &actual)
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	if raw, ok := actual[linkedNoteFoldersKey]; ok {
		fields[linkedNoteFoldersKey] = raw
	} else {
		delete(fields, linkedNoteFoldersKey)
	}
	s.configBase.local = mustNoteJSON(fields)
	if s.notes.folder != "" && !slices.ContainsFunc(db.folders, func(f savedNoteFolder) bool { return f.ID == s.notes.folder }) {
		s.notes.folder = ""
	}
	return nil
}
