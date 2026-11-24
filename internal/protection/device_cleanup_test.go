package protection

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"motty/internal/vault"
)

type profileDeviceStore struct {
	slots   map[string][]byte
	loads   int
	offline bool
}

func (s *profileDeviceStore) Load(_, id string) ([]byte, error) {
	s.loads++
	if s.offline {
		return nil, vault.ErrKeystoreUnavailable
	}
	stored, ok := s.slots[id]
	if !ok {
		return nil, vault.ErrKeystoreNotFound
	}
	return bytes.Clone(stored), nil
}
func (s *profileDeviceStore) Store(_, id string, v []byte) error {
	if s.offline {
		return vault.ErrKeystoreUnavailable
	}
	s.slots[id] = bytes.Clone(v)
	return nil
}
func (s *profileDeviceStore) Delete(_, id string) error {
	if s.offline {
		return vault.ErrKeystoreUnavailable
	}
	delete(s.slots, id)
	return nil
}

func TestDeviceCleanupSurvivesCrashAndIsBoundToDirectoryAndGeneration(t *testing.T) {
	g, _ := fixture(t)
	store := &profileDeviceStore{slots: map[string][]byte{}}
	g.options.DeviceStore = store
	if err := g.RetryDeviceCleanup(); err != nil || store.loads != 0 {
		t.Fatal("ordinary profile touched the OS store")
	}
	const password = "the master password"
	if err := g.Enable(password); err != nil {
		t.Fatal(err)
	}
	oldID := g.ProfileID()
	if err := g.SetDeviceBlob([]byte("old native reference")); err != nil {
		t.Fatal(err)
	}
	oldEnvelope, err := os.ReadFile(g.path("protection.json"))
	if err != nil {
		t.Fatal(err)
	}
	g.options.RevokeDevice = func(string, []byte) error { panic("lost power after commit") }
	func() {
		defer func() {
			if r := recover(); r != "lost power after commit" {
				t.Fatalf("did not interrupt native revoke: %v", r)
			}
		}()
		t.Fatal(g.Disable(password))
	}()
	var revoked []string
	g.options.RevokeDevice = func(id string, blob []byte) error {
		if id != oldID || string(blob) != "old native reference" {
			t.Fatal("attempted to revoke another enrollment")
		}
		revoked = append(revoked, id)
		return nil
	}
	cold, err := Open(g.options)
	if err != nil || cold.Enabled() || !cold.HasDeviceCleanup() {
		t.Fatalf("ordinary restart lost intent: %v", err)
	}
	tag, err := os.ReadFile(cold.cleanupPath(oldID))
	if err != nil || len(tag) != 33 || bytes.Contains(tag, []byte("old native reference")) {
		t.Fatal("invalid cleanup intent")
	}

	otherDir := t.TempDir()
	other, err := Open(Options{Directory: otherDir, DeviceStore: store, RevokeDevice: g.options.RevokeDevice})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(otherDir, cleanupDirectory), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other.cleanupPath(oldID), tag, 0600); err != nil {
		t.Fatal(err)
	}
	// Even a matching generation and OS authorization key cannot reuse another directory's tag.
	store.slots[other.deviceAccount(oldID, 2)] = bytes.Clone(store.slots[g.deviceAccount(oldID, 2)])
	if err := other.RetryDeviceCleanup(); !errors.Is(err, ErrCorrupt) || len(revoked) != 0 {
		t.Fatalf("copied intent triggered a revocation: %v", err)
	}

	// Restoring the original generation must preserve its device enrollment.
	if err := os.WriteFile(cold.path("protection.json"), oldEnvelope, 0600); err != nil {
		t.Fatal(err)
	}
	if err := cold.RetryDeviceCleanup(); err != nil || len(revoked) != 0 {
		t.Fatal("active generation revoked")
	}
	if err := os.Remove(cold.path("protection.json")); err != nil {
		t.Fatal(err)
	}

	if err := cold.Enable(password); err != nil {
		t.Fatal(err)
	}
	newID := cold.ProfileID()
	if err := cold.SetDeviceBlob([]byte("new native reference")); err != nil {
		t.Fatal(err)
	}
	store.offline = true
	if err := cold.RetryDeviceCleanup(); !errors.Is(err, vault.ErrKeystoreUnavailable) || !cold.HasDeviceCleanup() {
		t.Fatalf("offline retry lost intent: %v", err)
	}
	store.offline = false
	if err := cold.RetryDeviceCleanup(); err != nil {
		t.Fatal(err)
	}
	if len(revoked) != 1 || cold.HasDeviceCleanup() || store.slots[g.deviceAccount(oldID, 2)] != nil || store.slots[g.deviceAccount(newID, 2)] == nil {
		t.Fatal("wrong generation retired")
	}
	if blob, err := cold.readDevice(); err != nil || string(blob) != "new native reference" {
		t.Fatal("new enrollment marker lost")
	}
	if err := cold.RetryDeviceCleanup(); err != nil || len(revoked) != 1 {
		t.Fatal("completed native cleanup was repeated")
	}
}

func TestDisablingCopiedProtectedDirectoryPreservesOriginalEnrollment(t *testing.T) {
	g, _ := fixture(t)
	store := &profileDeviceStore{slots: map[string][]byte{}}
	g.options.DeviceStore = store
	const password = "the master password"
	if err := g.Enable(password); err != nil {
		t.Fatal(err)
	}
	if err := g.SetDeviceBlob([]byte("original native reference")); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, name := range []string{"config.db", "vault.json", "protection.json", deviceFile} {
		data, err := os.ReadFile(g.path(name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	opts := Options{Directory: dir, ConfigFormat: 28, DeviceStore: store, RevokeDevice: func(string, []byte) error {
		t.Fatal("copy revoked the original enrollment")
		return nil
	}}
	copied, err := Open(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(copied.DeviceBlob()) != 0 {
		t.Fatal("copy inherited original directory's enrollment")
	}
	if err := copied.Unlock(password); err != nil {
		t.Fatal(err)
	}
	if err := copied.Disable(password); err != nil {
		t.Fatal(err)
	}
	if blob, err := g.DeviceForRemoval(); err != nil || string(blob) != "original native reference" || len(store.slots[g.deviceAccount(g.ProfileID(), 2)]) == 0 {
		t.Fatal("original slot lost")
	}
}

func TestDeviceCleanupAfterColdDisableWhileOSStoreOffline(t *testing.T) {
	g, _ := fixture(t)
	store := &profileDeviceStore{slots: map[string][]byte{}}
	g.options.DeviceStore = store
	const password = "the master password"
	if err := g.Enable(password); err != nil {
		t.Fatal(err)
	}
	id := g.ProfileID()
	if err := g.SetDeviceBlob([]byte("native reference")); err != nil {
		t.Fatal(err)
	}
	g.Lock()
	store.offline = true
	cold, err := Open(g.options)
	if err != nil {
		t.Fatal(err)
	}
	if err := cold.Unlock(password); err != nil {
		t.Fatal(err)
	}
	if err := cold.Disable(password); !errors.Is(err, ErrDeviceCleanup) || cold.Enabled() || !cold.HasDeviceCleanup() {
		t.Fatalf("offline disable: %v", err)
	}
	store.offline = false
	cold, err = Open(g.options)
	if err != nil {
		t.Fatal(err)
	}
	fail := errors.New("native unavailable")
	cold.options.RevokeDevice = func(string, []byte) error { return fail }
	if err := cold.RetryDeviceCleanup(); !errors.Is(err, fail) || !cold.HasDeviceCleanup() || store.slots[g.deviceAccount(id, 2)] == nil {
		t.Fatalf("native cleanup failed without preserving alias: %v", err)
	}
	cold.options.RevokeDevice = func(string, []byte) error { return nil }
	if err := cold.RetryDeviceCleanup(); err != nil || cold.HasDeviceCleanup() || store.slots[g.deviceAccount(id, 2)] != nil {
		t.Fatalf("later retry: %v", err)
	}
}
