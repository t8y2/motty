package workbench

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"motty/internal/ai/tools"
	"motty/internal/configstore"
)

// headlessNotes is the note library for a server that has no window: it reads
// the saved records at the configuration path and, when full access was
// granted, writes them through the same note library the application uses. It
// never starts synchronization, watches or background work.
type headlessNotes struct {
	path string

	mu    sync.Mutex
	store *configstore.Store
}

func newHeadlessNotes(statePath string) *headlessNotes {
	return &headlessNotes{path: statePath}
}

// writable opens the configuration store once, on the first edit.
func (h *headlessNotes) writable() (*configstore.Store, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.store != nil {
		return h.store, nil
	}
	store, err := configstore.OpenShared(h.path, configFormat)
	if err != nil {
		return nil, err
	}
	h.store = store
	return store, nil
}

// Close releases the writable store, if an edit opened one.
func (h *headlessNotes) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.store != nil {
		h.store.Close()
		h.store = nil
	}
}

func (h *headlessNotes) load(writable bool) (noteStoreSnapshot, error) {
	var entries []configstore.Entry
	var local map[string][]byte
	var err error
	if writable {
		store, openErr := h.writable()
		if openErr != nil {
			return noteStoreSnapshot{}, openErr
		}
		entries, local, err = store.Load()
	} else {
		entries, local, err = configstore.ReadOnly(h.path, configFormat)
	}
	if err != nil {
		return noteStoreSnapshot{}, err
	}
	return decodeNoteStore(entries, local[localKey])
}

func (h *headlessNotes) library(db noteStoreSnapshot, writable bool) *noteLibrary {
	payloads := make(map[configstore.Key][]byte, len(db.entries))
	ords := make(map[configstore.Key]int64, len(db.entries))
	for _, entry := range db.entries {
		key := configstore.Key{Kind: entry.Kind, ID: entry.ID}
		payloads[key] = entry.Payload
		var p recordPayload
		if err := json.Unmarshal(entry.Payload, &p); err == nil {
			ords[key] = p.Ord
		}
	}
	lib := &noteLibrary{
		base:      &configBase{payloads: payloads, ords: ords, local: db.local},
		notes:     db.notes,
		folders:   db.folders,
		links:     map[string]*noteLinkState{},
		now:       time.Now,
		text:      func(en, zh string) string { return en },
		syncLinks: true,
		baseline: func(n savedNote) []byte {
			return db.notePayload(n)
		},
		loadSnapshot: func() (noteStoreSnapshot, error) { return h.load(writable) },
	}
	if writable {
		// The store is opened before the commit; a failure to open is reported
		// by load above, so this cannot change under the caller.
		lib.store, _ = h.writable()
	}
	return lib
}

func (h *headlessNotes) Query(a tools.NoteQueryArgs) (string, error) {
	db, err := h.load(false)
	if err != nil {
		return "", err
	}
	return h.library(db, false).query(a)
}

func (h *headlessNotes) Edit(a tools.NoteEditArgs) (string, error) {
	db, err := h.load(true)
	if err != nil {
		return "", err
	}
	lib := h.library(db, true)
	if lib.store == nil {
		return "", errors.New("note storage is read-only")
	}
	result, err := lib.edit(db, noteMutation{Action: a.Action, ID: a.ID, Version: a.Version, Title: a.Title, Body: a.Body, Tags: a.Tags, HostIDs: a.HostIDs, HostGroups: a.HostGroups, Folder: a.Folder, OldText: a.OldText, NewText: a.NewText, Name: a.Name, Parent: a.Parent})
	return noteEditJSON(result, err)
}
