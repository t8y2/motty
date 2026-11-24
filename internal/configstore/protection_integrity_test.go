package configstore

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrationCheckpointPreservesRowsCommittedOnlyInWAL(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.db")
	store, err := Open(source, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, query := range []string{"PRAGMA journal_mode=WAL", "PRAGMA wal_autocheckpoint=0", "INSERT INTO local(key,value) VALUES('recent-window','wal-only-data')"} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(t.TempDir(), "target.db")
	for _, suffix := range []string{"", "-wal"} {
		data, err := os.ReadFile(source + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if suffix == "" && bytes.Contains(data, []byte("wal-only-data")) {
			t.Fatal("row already checkpointed")
		}
		if err := os.WriteFile(target+suffix, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := CheckpointForMigration(target); err != nil {
		t.Fatal(err)
	}
	mainFile, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(t.TempDir(), "rollback.db")
	if err := os.WriteFile(restored, mainFile, 0600); err != nil {
		t.Fatal(err)
	}
	_, local, err := ReadOnly(restored, 1)
	if err != nil || string(local["recent-window"]) != "wal-only-data" {
		t.Fatalf("rollback snapshot missed committed WAL: %v", err)
	}
}

func TestTombstoneTamperingFailsBeforeLoadPruneOrUnlock(t *testing.T) {
	dir := t.TempDir()
	source, path := filepath.Join(dir, "source.db"), filepath.Join(dir, "protected.db")
	plain, err := Open(source, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Commit(Change{Put: []Entry{{Kind: "host", ID: "one", Payload: []byte(`{"d":{}}`)}}}); err != nil {
		t.Fatal(err)
	}
	if err := plain.Close(); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{4}, 32)
	if err := BuildCopy(source, path, 1, key); err != nil {
		t.Fatal(err)
	}
	if err := SetProtectionKey(path, key); err != nil {
		t.Fatal(err)
	}
	store, err := OpenShared(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.Exec("UPDATE records SET deleted=1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("load: %v", err)
	}
	if _, err := store.Prune(-time.Hour); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("prune: %v", err)
	}
	LockProtection(path)
	if _, err := OpenShared(path, 1); !errors.Is(err, ErrLocked) {
		t.Fatalf("new shared handle: %v", err)
	}
	if err := SetProtectionKey(path, key); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("unlock: %v", err)
	}
}
