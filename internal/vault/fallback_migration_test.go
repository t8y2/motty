package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReturnedSystemStorePreservesExistingFileVaultKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.json")
	system := &memoryKeystore{unavailable: true}
	keys := FallbackKeystore{System: system, File: FileKeystore{Path: filepath.Join(dir, "vault.key")}}
	v, err := Open(path, keys)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Set("credential", "saved locally"); err != nil {
		t.Fatal(err)
	}
	localKey, err := keys.File.Load(KeyService, KeyAccount)
	if err != nil {
		t.Fatal(err)
	}
	system.unavailable = false
	otherKey := bytes.Repeat([]byte{17}, 32)
	if err := system.Store(KeyService, KeyAccount, otherKey); err != nil {
		t.Fatal(err)
	}
	v, err = Open(path, keys)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok, err := v.Get("credential"); err != nil || !ok || got != "saved locally" {
		t.Fatalf("local-key vault: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archiveKey, err := RecoveryKey(data, keys)
	if err != nil || !bytes.Equal(archiveKey, localKey) {
		t.Fatalf("archive received wrong key: %v", err)
	}
	if archiveKey, err := RecoveryKey([]byte("unrelated backup"), keys); err != nil || len(archiveKey) > 0 {
		t.Fatal("unrelated backup received a system key")
	}
	if err := BuildProtectedCopy(path, filepath.Join(dir, "vault.next"), keys, bytes.Repeat([]byte{24}, 32), "profile"); err != nil {
		t.Fatal(err)
	}
	if got, err := system.Load(KeyService, KeyAccount); err != nil || !bytes.Equal(got, otherKey) {
		t.Fatal("shared key was rotated")
	}
	system.denied = true
	v, err = Open(path, keys)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.Get("credential"); !errors.Is(err, ErrKeystoreDenied) {
		t.Fatalf("key store denial: %v", err)
	}
}
