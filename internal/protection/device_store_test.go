package protection

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"motty/internal/vault"
)

type testDeviceStore struct {
	value                           []byte
	failRead, failStore, failDelete error
}

func (s *testDeviceStore) Load(_, _ string) ([]byte, error) {
	if s.failRead != nil {
		return nil, s.failRead
	}
	if len(s.value) == 0 {
		return nil, vault.ErrKeystoreNotFound
	}
	return append([]byte(nil), s.value...), nil
}

func (s *testDeviceStore) Store(_, _ string, v []byte) error {
	if s.failStore == nil {
		s.value = append([]byte(nil), v...)
	}
	return s.failStore
}

func (s *testDeviceStore) Delete(_, _ string) error {
	if s.failDelete == nil {
		s.value = nil
	}
	return s.failDelete
}

func TestDisableWithUnavailableEnrollmentCommitsOrdinaryProfile(t *testing.T) {
	g, _ := fixture(t)
	store := &testDeviceStore{}
	g.options.DeviceStore = store
	const password = "master password for cleanup"
	if err := g.Enable(password); err != nil {
		t.Fatal(err)
	}
	if err := g.SetDeviceBlob([]byte("native reference")); err != nil {
		t.Fatal(err)
	}
	store.failDelete = errors.New("device cleanup denied")
	if err := g.Disable(password); !errors.Is(err, ErrDeviceCleanup) {
		t.Fatalf("expected a post-commit device cleanup warning: %v", err)
	}
	if g.Enabled() || g.Locked() || g.PendingMigration() {
		t.Fatal("a native cleanup failure changed the committed storage mode")
	}
	cold, err := Open(g.options)
	if err != nil || cold.Enabled() || cold.Locked() || len(cold.DeviceBlob()) != 0 {
		t.Fatalf("cold ordinary profile after device cleanup failure: %v", err)
	}
	v, err := vault.Open(g.options.VaultPath, g.options.Keystore)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok, err := v.Get("host:one:password"); err != nil || !ok || value != "secret-account-password" {
		t.Fatalf("ordinary vault after device cleanup failure: %v", err)
	}
}

func TestDeviceEnrollmentSurvivesTemporaryStoreFailure(t *testing.T) {
	g, _ := fixture(t)
	store := &testDeviceStore{}
	g.options.DeviceStore = store
	const password = "the profile password"
	if err := g.Enable(password); err != nil {
		t.Fatal(err)
	}
	original := []byte("opaque reference")
	offline := errors.New("device store offline")
	store.failStore = offline
	if err := g.SetDeviceBlob(original); !errors.Is(err, offline) {
		t.Fatalf("failed enrollment: %v", err)
	}
	store.failStore = nil
	if err := g.SetDeviceBlob(original); err != nil {
		t.Fatal(err)
	}
	g.Lock()
	store.failRead = offline
	cold, err := Open(g.options)
	if err != nil {
		t.Fatal(err)
	}
	if len(cold.DeviceBlob()) != 0 {
		t.Fatal("unavailable device was offered at startup")
	}
	if err := cold.Unlock(password); err != nil {
		t.Fatal(err)
	}
	if err := cold.SetDeviceBlob([]byte("replacement")); !errors.Is(err, offline) {
		t.Fatalf("enrollment attempted over an unreadable reference: %v", err)
	}
	if _, err := cold.DeviceForRemoval(); !errors.Is(err, offline) {
		t.Fatalf("missing native cleanup warning: %v", err)
	}
	store.failRead = nil
	blob, err := cold.DeviceForRemoval()
	if err != nil || !bytes.Equal(blob, original) {
		t.Fatalf("recovered enrollment: %v", err)
	}
	stored, _, decodeErr := unpackDeviceEntry(store.value)
	if err := cold.SetDeviceBlob([]byte("replacement")); err == nil || decodeErr != nil || !bytes.Equal(stored, original) {
		t.Fatal("re-enrollment replaced an existing native reference")
	}
	if !bytes.Equal(cold.DeviceBlob(), original) {
		t.Fatal("recovered enrollment was not available for system unlock")
	}
	store.failDelete = offline
	if err := cold.SetDeviceBlob(nil); !errors.Is(err, offline) {
		t.Fatalf("missing deletion failure: %v", err)
	}
	store.failDelete = nil
	if err := cold.SetDeviceBlob(nil); err != nil {
		t.Fatal(err)
	}
	if len(store.value) != 0 {
		t.Fatal("native reference was not removed")
	}
	if _, err := os.Stat(cold.path(deviceFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("enrollment marker survived removal: %v", err)
	}
}
