package configstore

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProtectedRecordsKeepLogicalPatchAndConflictSemantics(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "plain.db")
	destination := filepath.Join(dir, "protected.db")
	key := bytes.Repeat([]byte{9}, 32)
	plain, err := Open(source, 1)
	if err != nil {
		t.Fatal(err)
	}
	before := []byte(`{"d":{"address":"classified.example","user":"admin"}}`)
	after := []byte(`{"d":{"address":"updated.example","user":"admin"}}`)
	_, err = plain.Commit(Change{Put: []Entry{{Kind: "group", ID: "Production secret", Payload: before}}, Local: map[string][]byte{"window-1": []byte(`{"sessions":[{"address":"classified.example"}]}`)}})
	if err != nil {
		t.Fatal(err)
	}
	plain.Close()
	if err := BuildCopy(source, destination, 1, key); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"Production secret", "classified.example", "admin", "window-1"} {
		if bytes.Contains(contents, []byte(secret)) {
			t.Fatalf("plaintext %q survived migration", secret)
		}
	}
	if _, err := Open(destination, 1); !errors.Is(err, ErrLocked) {
		t.Fatalf("locked open: %v", err)
	}
	if err := SetProtectionKey(destination, key); err != nil {
		t.Fatal(err)
	}
	store, err := Open(destination, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Commit(Change{Patch: []PatchEntry{{Key: Key{Kind: "group", ID: "Production secret"}, JSONChange: JSONChange{Before: before, After: after}}}, LocalPatch: map[string]JSONChange{"window-1": {Before: []byte(`{"sessions":[{"address":"classified.example"}]}`), After: []byte(`{"sessions":[{"address":"updated.example"}]}`)}}}); err != nil {
		t.Fatal(err)
	}
	entries, local, err := store.Load()
	if err != nil || len(entries) != 1 || !sameJSON(entries[0].Payload, after) || !bytes.Contains(local["window-1"], []byte("updated.example")) {
		t.Fatalf("logical patch failed: %v", err)
	}
	if _, err := store.Commit(Change{ExpectedKinds: map[string][]Entry{"group": {{Kind: "group", ID: "Production secret", Payload: after}}}}); err != nil {
		t.Fatal("logical collection guard", err)
	}
	if _, err := store.Commit(Change{Expected: map[Key][]byte{{Kind: "group", ID: "Production secret"}: before}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale patch was accepted: %v", err)
	}
	LockProtection(destination)
	if _, _, err := store.Load(); !errors.Is(err, ErrLocked) {
		t.Fatalf("live handle bypassed lock: %v", err)
	}
	if _, err := store.Commit(Change{Delete: []Key{{Kind: "group", ID: "Production secret"}}}); !errors.Is(err, ErrLocked) {
		t.Fatalf("locked write accepted: %v", err)
	}
	if err := SetProtectionKey(destination, bytes.Repeat([]byte{1}, 32)); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("wrong key accepted: %v", err)
	}
	if err := SetProtectionKey(destination, key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(Change{Delete: []Key{{Kind: "group", ID: "Production secret"}}}); err != nil {
		t.Fatal(err)
	}
	records, err := store.Records()
	if err != nil || len(records) != 1 || records[0].ID != "Production secret" || !records[0].Deleted || records[0].Payload != nil {
		t.Fatalf("logical tombstone: %#v %v", records, err)
	}
}

func TestProtectedAuthenticationFailureIsNotAnEmptyProfile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	path := filepath.Join(dir, "protected.db")
	key := bytes.Repeat([]byte{8}, 32)
	plain, err := Open(source, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Commit(Change{Put: []Entry{{Kind: "host", ID: "host", Payload: []byte(`{"d":{"address":"server.example"}}`)}}}); err != nil {
		t.Fatal(err)
	}
	plain.Close()
	if err := BuildCopy(source, path, 1, key); err != nil {
		t.Fatal(err)
	}
	if err := SetProtectionKey(path, key); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE records SET payload=randomblob(length(payload))`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, _, err := ReadOnly(path, 1); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("tampered record did not fail closed: %v", err)
	}
}

func TestClosedOrdinaryStoresLeaveNoMigrationRegistryEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	store, err := Open(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureClosed(path); !errors.Is(err, ErrBusy) {
		store.Close()
		t.Fatalf("ordinary handle not tracked: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := EnsureClosed(path); err != nil {
		t.Fatal(err)
	}
	activeStores.Lock()
	_, retained := activeStores.paths[canonicalPath(path)]
	activeStores.Unlock()
	if retained {
		t.Fatal("closed path retained by migration registry")
	}
}
