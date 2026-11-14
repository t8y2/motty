package workbench

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"motty/internal/ai/tools"
	"motty/internal/configstore"
)

// This file is the note library without a window: the persisted records, the
// versions, the compare-and-swap commit and the linked Markdown files. The
// window (State) supplies its view, its drafts and its editors; a headless MCP
// server supplies a snapshot of the store and no view. Both go through the same
// validation and commit code, so a note means the same thing on every path.

// noteLibrary is one caller's view of the library plus everything a mutation
// needs. The window fills the view hooks; a headless caller leaves them at
// their defaults.
type noteLibrary struct {
	store *configstore.Store // nil for a read-only headless caller
	base  *configBase
	// notes and folders are the caller's view. A window's list may differ from
	// the store in notes it is editing; a headless caller passes the snapshot.
	notes   []savedNote
	folders []savedNoteFolder
	drafts  map[string]*noteDraft
	links   map[string]*noteLinkState
	// now and newID make tests deterministic; nil uses the wall clock and the
	// ordinary random identity.
	now   func() time.Time
	newID func() string
	// browse applies the caller's unsaved text to a note before it is read.
	browser  func(savedNote) savedNote
	baseline func(savedNote) []byte
	text     func(en, zh string) string
	// syncLinks reads a linked file before an edit instead of relying on a
	// window's watch having made the same comparison. The headless server has no
	// watch; a window keeps its in-memory link state.
	syncLinks bool

	loadSnapshot func() (noteStoreSnapshot, error)
	// afterNote and afterFolder update the caller's view after a commit. A
	// headless caller has no view and leaves them nil.
	afterNote   func(savedNote, []byte, []byte)
	afterFolder func(savedNoteFolder, []byte, bool)
	notify      func(shared bool)
}

func (lib *noteLibrary) browseNote(n savedNote) savedNote {
	if lib.browser != nil {
		return lib.browser(n)
	}
	return n
}

func (lib *noteLibrary) nowTime() time.Time {
	if lib.now != nil {
		return lib.now()
	}
	return time.Now()
}

func (lib *noteLibrary) newNoteID() string {
	if lib.newID != nil {
		return lib.newID()
	}
	return noteID()
}

func (lib *noteLibrary) localText(en, zh string) string {
	if lib.text != nil {
		return lib.text(en, zh)
	}
	return en
}

func (lib *noteLibrary) noteIndex(id string) int {
	return slices.IndexFunc(lib.notes, func(n savedNote) bool { return n.ID == id })
}

func (lib *noteLibrary) noteFolder(id string) (savedNoteFolder, bool) {
	for _, f := range lib.folders {
		if f.ID == id {
			return f, true
		}
	}
	return savedNoteFolder{}, false
}

func (lib *noteLibrary) noteSnapshot(n savedNote) aiNoteResult {
	n = lib.browseNote(n)
	var baseline []byte
	if lib.baseline != nil {
		baseline = lib.baseline(n)
	}
	return noteSnapshotResult(n, baseline, lib.folders)
}

func (lib *noteLibrary) load() (noteStoreSnapshot, error) {
	if lib.loadSnapshot == nil {
		return noteStoreSnapshot{}, errors.New("note storage is unavailable")
	}
	return lib.loadSnapshot()
}

// query answers note_query. The caller has already refreshed its view.
func (lib *noteLibrary) query(a tools.NoteQueryArgs) (string, error) {
	if a.Action == "read" {
		i := lib.noteIndex(a.ID)
		if i < 0 {
			return noteQueryError("not_found", "note not found")
		}
		snapshot := lib.noteSnapshot(lib.notes[i])
		if a.Offset > 0 && a.ExpectedVersion == "" || a.ExpectedVersion != "" && a.ExpectedVersion != snapshot.Version {
			return noteQueryError("version_conflict", "note changed between pages; restart reading")
		}
		return aiNotesJSON(aiNotePage(snapshot, a.Offset, a.MaxChars))
	}
	var db noteStoreSnapshot
	var err error
	catalog := []Host{}
	groupPaths := map[string]bool{}
	if a.Action == "search" || a.Action == "hosts" || a.Action == "host_groups" {
		db, err = lib.load()
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
		for _, n := range lib.notes {
			for _, tag := range lib.noteSnapshot(n).Tags {
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
			valid = lib.noteIndex(r.ID) >= 0
		case "folder":
			_, valid = lib.noteFolder(r.ID)
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
				if noteFolderWithin(lib.folders, n.Folder, r.ID) {
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
		for _, n := range lib.notes {
			item := lib.noteSnapshot(n)
			if !matches(item) || !noteMatchesSearch(n, a.Query, lib.browseNote) {
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
		for _, f := range lib.folders {
			path, ok := noteFolderPath(lib.folders, f.ID)
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

// edit performs one note or folder mutation against db. The caller loaded db
// immediately before this call; a change committed by another process in
// between fails the compare-and-swap instead of overwriting.
func (lib *noteLibrary) edit(db noteStoreSnapshot, a noteMutation) (noteMutationResult, error) {
	switch a.Action {
	case "create", "update", "replace", "folder_create", "folder_update", "folder_delete":
	default:
		return noteMutationFailure("validation_error", errors.New("unknown note operation"))
	}
	if strings.HasPrefix(a.Action, "folder_") {
		return lib.editFolder(db, a)
	}
	creating := a.Action == "create"
	var previous, n savedNote
	var err error
	if creating {
		if a.Title == nil || strings.TrimSpace(*a.Title) == "" || a.Body == nil {
			return noteMutationFailure("validation_error", errors.New("create requires title and body"))
		}
		now := lib.nowTime().UTC().Format(time.RFC3339Nano)
		n = savedNote{ID: lib.newNoteID(), Revision: lib.newNoteID(), Created: now, Order: len(db.notes)}
	} else {
		i := lib.noteIndex(a.ID)
		if i < 0 {
			return noteMutationFailure("version_conflict", errors.New("note not found; read again"))
		}
		n = lib.browseNote(lib.notes[i])
		if a.Version == "" || a.Version != noteContentVersion(n, lib.baselineOf(n)) || noteHash(string(db.notePayload(n))) != noteHash(string(lib.baselineOf(n))) {
			return noteMutationFailure("version_conflict", errors.New("stale note version; read the note again"))
		}
		storedIndex := slices.IndexFunc(db.notes, func(x savedNote) bool { return x.ID == n.ID })
		if storedIndex < 0 {
			return noteMutationFailure("version_conflict", errors.New("note was deleted; read again"))
		}
		previous = db.notes[storedIndex]
		if d := lib.drafts[n.ID]; d != nil && (d.composing() || d.conflictSuffix != "") {
			return noteMutationFailure("version_conflict", errors.New("note has active composition or conflict"))
		}
	}
	if a.Title != nil {
		n.Title = *a.Title
	}
	if a.Body != nil {
		n.Body = *a.Body
	}
	if a.Action == "replace" {
		if a.OldText == nil || a.NewText == nil || !uniqueNoteFragment(n.Body, *a.OldText) {
			return noteMutationFailure("validation_error", errors.New("old_text must match exactly once; read again"))
		}
		n.Body = strings.Replace(n.Body, *a.OldText, *a.NewText, 1)
	}
	if a.Folder != nil {
		if n.Link != nil {
			return noteMutationFailure("validation_error", errors.New("linked notes cannot be moved"))
		}
		if err = ordinaryNoteFolder(db.folders, *a.Folder); err != nil {
			return noteMutationFailure("validation_error", err)
		}
		n.Folder = *a.Folder
	}
	var catalog []Host
	var groups []savedHostGroup
	if a.HostIDs != nil || a.HostGroups != nil {
		catalog, groups, err = db.catalog()
		if err != nil {
			return noteStorageFailure(err)
		}
	}
	groupExists := func(path string) bool {
		return path != "" && (slices.ContainsFunc(groups, func(g savedHostGroup) bool { return hostGroupWithin(g.Name, path) }) || slices.ContainsFunc(catalog, func(h Host) bool { return hostGroupWithin(h.Group, path) }))
	}
	n.Tags = slices.Clone(n.Tags)
	if a.Tags != nil {
		tags := []string{}
		for _, tag := range *a.Tags {
			tag = strings.TrimSpace(tag)
			if tag == "" || strings.Contains(tag, ",") || strings.HasPrefix(tag, "{@") {
				return noteMutationFailure("validation_error", errors.New("invalid ordinary tag"))
			}
			if !slices.Contains(tags, tag) {
				tags = append(tags, tag)
			}
		}
		for _, tag := range n.Tags {
			if noteSpecialTag(tag) {
				tags = append(tags, tag)
			}
		}
		n.Tags = tags
	}
	if a.HostIDs != nil {
		n.Tags = slices.DeleteFunc(n.Tags, func(tag string) bool { _, ok := parseNoteHost(tag); return ok })
		for _, id := range *a.HostIDs {
			index := slices.IndexFunc(catalog, func(h Host) bool { return h.CatalogID == id })
			if index < 0 {
				return noteMutationFailure("validation_error", fmt.Errorf("associated host no longer exists: %s", id))
			}
			tag := encodeNoteHost(noteHostFor(catalog[index]))
			if !slices.Contains(n.Tags, tag) {
				n.Tags = append(n.Tags, tag)
			}
		}
	}
	if a.HostGroups != nil {
		n.Tags = slices.DeleteFunc(n.Tags, func(tag string) bool { _, ok := parseNoteHostGroup(tag); return ok })
		for _, id := range *a.HostGroups {
			if !groupExists(id) {
				return noteMutationFailure("validation_error", fmt.Errorf("unknown host group: %s", id))
			}
			tag := encodeNoteHostGroup(id)
			if !slices.Contains(n.Tags, tag) {
				n.Tags = append(n.Tags, tag)
			}
		}
	}
	if lib.syncLinks && n.Link != nil {
		lib.linkReconcile(n, previous)
	}
	if n.Link != nil && n.Body != previous.Body {
		link := *n.Link
		link.WriteBase = noteHash(previous.Body)
		if previous.Link != nil && previous.Link.WriteBase != "" {
			link.WriteBase = previous.Link.WriteBase
		}
		if st := lib.links[n.ID]; st != nil && st.bodyHash != "" {
			link.WriteBase = st.bodyHash
		}
		n.Link = &link
	}
	n, err = prepareNoteUpdate(previous, n, lib.nowTime())
	if err != nil {
		return noteMutationFailure("validation_error", err)
	}
	// Project the committed graph, not a potentially stale UI copy.
	projected := slices.Clone(db.notes)
	index := slices.IndexFunc(projected, func(x savedNote) bool { return x.ID == n.ID })
	if index < 0 {
		if !creating {
			return noteMutationFailure("version_conflict", errors.New("note was deleted"))
		}
		projected = append(projected, n)
	} else {
		projected[index] = n
	}
	if err = validateNoteGraph(projected, db.folders); err != nil {
		return noteMutationFailure("validation_error", err)
	}
	change := configstore.Change{Writer: configWriter(), Expected: map[configstore.Key][]byte{}, ExpectedKinds: map[string][]configstore.Entry{}}
	kinds := []string{"note", "note-folder"}
	if a.HostIDs != nil || a.HostGroups != nil {
		kinds = append(kinds, "host", "group")
	}
	for _, kind := range kinds {
		change.ExpectedKinds[kind] = noteKindSnapshot(db.entries, kind)
	}
	var payload []byte
	if n.Link == nil {
		key := configstore.Key{Kind: "note", ID: n.ID}
		old := db.payload("note", n.ID)
		if creating && len(old) > 0 {
			return noteMutationFailure("version_conflict", errors.New("new note identity already exists"))
		}
		payload, err = db.stageRecord(&change, key, mustNoteJSON(n))
		if err != nil {
			return noteStorageFailure(err)
		}
		if len(payload) > configstore.MaxPayload {
			return noteMutationFailure("validation_error", errors.New("note exceeds record limit"))
		}
	} else {
		patch := configstore.JSONChange{Before: mustNoteJSON(previous), After: mustNoteJSON(n)}
		raw, err := patch.Apply(db.notePayload(previous))
		if err != nil {
			return noteStorageFailure(err)
		}
		if len(raw) > configstore.MaxPayload {
			return noteMutationFailure("validation_error", errors.New("note exceeds record limit"))
		}
		change.Local = map[string][]byte{localKey: setNoteLocalPayload(db.local, n.ID, raw)}
		change.ExpectedLocal = map[string][]byte{localKey: db.local}
	}
	// Local linked records also participate in folder membership guards.
	if change.ExpectedLocal == nil {
		change.ExpectedLocal = map[string][]byte{localKey: db.local}
	}
	if lib.store == nil {
		return noteStorageFailure(errors.New("note storage is read-only"))
	}
	if _, err = lib.store.Commit(change); err != nil {
		return noteStorageFailure(err)
	}
	if lib.afterNote != nil {
		lib.afterNote(n, payload, change.Local[localKey])
	}
	version := ""
	if n.Link == nil {
		version = noteContentVersion(n, payload)
	} else {
		version = noteContentVersion(n, noteLocalPayload(change.Local[localKey], n.ID))
	}
	fileStatus := "not_linked"
	status := "saved"
	message := ""
	if n.Link != nil {
		if lib.links[n.ID] == nil {
			st := lib.linkInfo(n.ID)
			st.bodyHash = n.Link.WriteBase
			if st.bodyHash == "" {
				st.bodyHash = noteHash(previous.Body)
			}
			st.bom = readLinkedFile(n.Link.Path).bom
		}
		fileStatus = "saved"
		if n.Body != previous.Body {
			lib.linkWrite(n)
		} else if lib.linkCheck(n) && noteHash(n.Body) != lib.linkInfo(n.ID).bodyHash {
			lib.linkInfo(n.ID).problem = lib.localText("File save is pending; committed note text is retained.", "文件尚未保存，已提交的笔记正文已保留。")
		}
		st := lib.linkInfo(n.ID)
		switch {
		case st.conflict:
			fileStatus = "conflict"
		case st.missing:
			fileStatus = "missing"
		case st.problem != "":
			fileStatus = "write_failed"
			message = st.problem
		}
		if fileStatus != "saved" {
			status = "file_" + fileStatus
		}
	}
	if lib.notify != nil {
		lib.notify(n.Link == nil)
	}
	return noteMutationResult{Status: status, LibrarySaved: true, FileStatus: fileStatus, Note: &n, Message: message, Version: version}, nil
}

func (lib *noteLibrary) editFolder(db noteStoreSnapshot, a noteMutation) (noteMutationResult, error) {
	folders := slices.Clone(db.folders)
	var f savedNoteFolder
	index := -1
	if a.Action == "folder_create" {
		f = savedNoteFolder{ID: lib.newNoteID(), Order: len(folders)}
	} else {
		index = slices.IndexFunc(folders, func(f savedNoteFolder) bool { return f.ID == a.ID })
		if index < 0 {
			return noteMutationFailure("version_conflict", errors.New("folder no longer exists"))
		}
		f = folders[index]
		old := lib.base.payloads[configstore.Key{Kind: "note-folder", ID: f.ID}]
		if noteHash(string(old)) != noteHash(string(db.payload("note-folder", f.ID))) {
			return noteMutationFailure("version_conflict", errors.New("folder changed; query folders again"))
		}
		// Do not replace pending folder edits from another view.
		if local, ok := lib.noteFolder(f.ID); !ok || string(mustNoteJSON(local)) != string(mustNoteJSON(f)) {
			return noteMutationFailure("version_conflict", errors.New("folder has pending changes"))
		}
		if err := ordinaryNoteFolder(folders, a.ID); err != nil {
			return noteMutationFailure("validation_error", err)
		}
	}
	if a.Name != nil {
		f.Name = strings.TrimSpace(*a.Name)
	}
	if f.Name == "" {
		return noteMutationFailure("validation_error", errors.New("folder name is required"))
	}
	if a.Parent != nil {
		if err := ordinaryNoteFolder(folders, *a.Parent); err != nil {
			return noteMutationFailure("validation_error", err)
		}
		f.Parent = *a.Parent
	}
	deleting := a.Action == "folder_delete"
	if deleting {
		for _, n := range db.notes {
			if n.Folder == f.ID {
				return noteMutationFailure("validation_error", errors.New("folder is not empty"))
			}
		}
		for _, child := range folders {
			if child.Parent == f.ID {
				return noteMutationFailure("validation_error", errors.New("folder is not empty"))
			}
		}
		// Include unsaved local members as well as persisted members.
		for _, n := range lib.notes {
			if n.Folder == f.ID {
				return noteMutationFailure("validation_error", errors.New("folder contains an unsaved note"))
			}
		}
		for _, child := range lib.folders {
			if child.Parent == f.ID {
				return noteMutationFailure("validation_error", errors.New("folder contains an unsaved folder"))
			}
		}
		folders = slices.Delete(folders, index, index+1)
	} else if index < 0 {
		folders = append(folders, f)
	} else {
		folders[index] = f
	}
	if err := validateNoteGraph(db.notes, folders); err != nil {
		return noteMutationFailure("validation_error", err)
	}
	// No management of ordinary ancestors may relocate linked directories.
	if !deleting && index >= 0 {
		for _, child := range db.folders {
			if child.Link != nil {
				parent := child.Parent
				for parent != "" {
					if parent == f.ID {
						return noteMutationFailure("validation_error", errors.New("folder contains a linked directory"))
					}
					j := slices.IndexFunc(db.folders, func(x savedNoteFolder) bool { return x.ID == parent })
					if j < 0 {
						break
					}
					parent = db.folders[j].Parent
				}
			}
		}
	}
	key := configstore.Key{Kind: "note-folder", ID: f.ID}
	change := configstore.Change{Writer: configWriter(), Expected: map[configstore.Key][]byte{key: db.payload(key.Kind, key.ID)}, ExpectedKinds: map[string][]configstore.Entry{}, ExpectedLocal: map[string][]byte{localKey: db.local}}
	for _, kind := range []string{"note", "note-folder"} {
		change.ExpectedKinds[kind] = noteKindSnapshot(db.entries, kind)
	}
	var payload []byte
	if deleting {
		change.Delete = []configstore.Key{key}
	} else {
		if a.Action == "folder_create" && len(db.payload(key.Kind, key.ID)) > 0 {
			return noteMutationFailure("version_conflict", errors.New("new folder identity already exists"))
		}
		var err error
		payload, err = db.stageRecord(&change, key, mustNoteJSON(f))
		if err != nil {
			return noteStorageFailure(err)
		}
		if len(payload) > configstore.MaxPayload {
			return noteMutationFailure("validation_error", errors.New("folder exceeds record limit"))
		}
	}
	if lib.store == nil {
		return noteStorageFailure(errors.New("note storage is read-only"))
	}
	if _, err := lib.store.Commit(change); err != nil {
		return noteStorageFailure(err)
	}
	if lib.afterFolder != nil {
		lib.afterFolder(f, payload, deleting)
	}
	if lib.notify != nil {
		lib.notify(true)
	}
	return noteMutationResult{Status: "saved", LibrarySaved: true, Folder: &f}, nil
}

func (lib *noteLibrary) baselineOf(n savedNote) []byte {
	if lib.baseline != nil {
		return lib.baseline(n)
	}
	return nil
}

func (lib *noteLibrary) linkInfo(id string) *noteLinkState {
	if lib.links == nil {
		lib.links = map[string]*noteLinkState{}
	}
	st := lib.links[id]
	if st == nil {
		st = &noteLinkState{}
		lib.links[id] = st
	}
	return st
}

// linkReconcile compares the file with the committed note before an edit. It is
// the headless counterpart of a window's watch-driven adoption: a file that
// matches the committed text (or the recorded write baseline) is current, a
// file that matches neither is an external change and is never overwritten.
func (lib *noteLibrary) linkReconcile(n, previous savedNote) {
	st := lib.linkInfo(n.ID)
	if st.conflict || st.missing {
		return
	}
	if st.bodyHash == "" {
		st.bodyHash = n.Link.WriteBase
		if st.bodyHash == "" {
			st.bodyHash = noteHash(previous.Body)
		}
	}
	disk := readLinkedFile(n.Link.Path)
	switch {
	case disk.missing:
		st.missing = true
	case disk.err != nil:
		st.problem = noteLinkMessage(disk.err, lib.localText)
	case noteHash(disk.body) == noteHash(previous.Body), noteHash(disk.body) == noteHash(n.Body):
		st.bodyHash, st.bom, st.problem, st.conflict = noteHash(disk.body), disk.bom, "", false
	case noteHash(disk.body) == st.bodyHash:
		st.bom = disk.bom
	default:
		st.bom, st.conflict = disk.bom, true
	}
}

func (lib *noteLibrary) linkCheck(note savedNote) bool {
	return noteLinkCheckFile(lib.linkInfo(note.ID), note.Link.Path, func(err error) string {
		return noteLinkMessage(err, lib.localText)
	})
}

func (lib *noteLibrary) linkWrite(note savedNote) {
	noteLinkWriteFile(lib.linkInfo(note.ID), note.Link.Path, note.Body, lib.localText, func(err error) string {
		return noteLinkMessage(err, lib.localText)
	})
}

// noteSnapshotResult is the tool-facing note with its version and identities.
// browse is already applied.
func noteSnapshotResult(n savedNote, baseline []byte, folders []savedNoteFolder) aiNoteResult {
	result := aiNoteResult{savedNote: n, Version: noteContentVersion(n, baseline)}
	result.Tags = []string{}
	result.HostIDs = []string{}
	result.HostGroups = []string{}
	if n.Folder != "" {
		result.FolderPath, _ = noteFolderPath(folders, n.Folder)
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

// noteFolderPath is the slash-separated path of a folder id.
func noteFolderPath(folders []savedNoteFolder, id string) (string, bool) {
	parts := []string{}
	seen := map[string]bool{}
	for id != "" {
		if seen[id] {
			return "", false
		}
		seen[id] = true
		folder, ok := noteFolderByID(folders, id)
		if !ok {
			return "", false
		}
		parts = append(parts, folder.Name)
		id = folder.Parent
	}
	slices.Reverse(parts)
	return strings.Join(parts, "/"), len(parts) > 0
}

func noteFolderByID(folders []savedNoteFolder, id string) (savedNoteFolder, bool) {
	for _, f := range folders {
		if f.ID == id {
			return f, true
		}
	}
	return savedNoteFolder{}, false
}

// noteFolderWithin says whether id is ancestor or a descendant of it.
func noteFolderWithin(folders []savedNoteFolder, id, ancestor string) bool {
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		if id == ancestor {
			return true
		}
		seen[id] = true
		parent := ""
		for _, f := range folders {
			if f.ID == id {
				parent = f.Parent
				break
			}
		}
		id = parent
	}
	return false
}

// noteMatchesSearch matches a note's title, body and tags. browse applies the
// caller's unsaved text first.
func noteMatchesSearch(note savedNote, query string, browse func(savedNote) savedNote) bool {
	query = strings.ToLower(query)
	if query == "" {
		return true
	}
	terms := []string{query}
	if strings.Contains(query, "|") {
		terms = nil
		for _, term := range strings.Split(query, "|") {
			if term = strings.TrimSpace(term); term != "" {
				terms = append(terms, term)
			}
		}
		if len(terms) == 0 {
			return true
		}
	}
	note = browse(note)
	contains := func(value string) bool {
		value = strings.ToLower(value)
		for _, term := range terms {
			if strings.Contains(value, term) {
				return true
			}
		}
		return false
	}
	if contains(note.Title + "\n" + note.Body) {
		return true
	}
	for _, tag := range note.Tags {
		if host, ok := parseNoteHost(tag); ok {
			if contains(host.Label) || contains(host.Address) || contains(host.User) {
				return true
			}
		} else if path, ok := parseNoteHostGroup(tag); ok {
			if contains(path) {
				return true
			}
		} else if contains(tag) {
			return true
		}
	}
	return false
}

// noteLinkCheckFile validates the disk baseline without writing or adopting
// text. message turns a read error into the caller's wording.
func noteLinkCheckFile(st *noteLinkState, path string, message func(error) string) bool {
	if st.conflict || st.missing {
		return false
	}
	disk := readLinkedFile(path)
	switch {
	case disk.missing:
		st.missing = true
		return false
	case disk.err != nil:
		st.problem = message(disk.err)
		return false
	case noteHash(disk.body) != st.bodyHash:
		st.bom, st.conflict = disk.bom, true
		return false
	}
	st.problem = ""
	return true
}

// noteLinkWriteFile saves a linked note's text to its file. A file that cannot
// be written, or that changed meanwhile, leaves the text in the note and says
// so in its notice.
func noteLinkWriteFile(st *noteLinkState, path, body string, text func(en, zh string) string, message func(error) string) {
	if !noteLinkCheckFile(st, path, message) {
		return
	}
	if err := writeLinkedFile(path, body, st.bom); err != nil {
		st.problem = text("Could not write the file: ", "无法写入文件：") + err.Error()
		return
	}
	st.bodyHash, st.problem = noteHash(body), ""
}

func noteLinkMessage(err error, text func(en, zh string) string) string {
	switch {
	case errors.Is(err, errLinkNotUTF8):
		return text("Markdown must be UTF-8.", "Markdown 文件必须使用 UTF-8 编码。")
	case errors.Is(err, errLinkTooLarge):
		return text("The file exceeds 1 MiB.", "文件超过 1 MiB。")
	case errors.Is(err, errLinkNotRegular):
		return text("The linked path is not a regular file.", "链接的路径不是普通文件。")
	}
	return err.Error()
}
