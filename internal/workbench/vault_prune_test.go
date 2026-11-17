package workbench

import (
	"path/filepath"
	"testing"
	"time"

	"motty/internal/configstore"
	"motty/internal/vault"
)

// A computer that never syncs still has to let old deletions go: they count
// toward the vault file, and sync, the other place that prunes them, never runs
// there.
func TestStartPrunesSecretTombstonesPastTheSyncWindow(t *testing.T) {
	dir := t.TempDir()
	options := testOptions(t)
	options.StatePath = filepath.Join(dir, "state.json")
	options.KnownHostsPath = filepath.Join(dir, "known_hosts")
	keys := &memKeys{}
	options.Keystore = keys
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	options.WallNow = func() time.Time { return now }

	path := filepath.Join(dir, "vault.json")
	saved, err := vault.Open(path, keys)
	if err != nil {
		t.Fatal(err)
	}
	old := string(configstore.CutoffStamp(now.Add(-100 * 24 * time.Hour)))
	saved.SetStamper(func() (string, error) { return old, nil })
	if err := saved.Set("kept", "value"); err != nil {
		t.Fatal(err)
	}
	if err := saved.Set("gone", "value"); err != nil {
		t.Fatal(err)
	}
	if err := saved.Delete("gone"); err != nil {
		t.Fatal(err)
	}

	state := newTestState(t, options)
	if state.secrets.vault == nil {
		t.Fatal("the vault did not open")
	}

	onDisk, err := vault.Open(path, keys)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := onDisk.Export()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets {
		if secret.Name == "gone" {
			t.Fatalf("a deletion past the sync window survived start: %+v", secrets)
		}
	}
	if len(secrets) != 1 || secrets[0].Name != "kept" || secrets[0].Deleted {
		t.Fatalf("start left %+v", secrets)
	}
}
