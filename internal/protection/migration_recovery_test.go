package protection

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"motty/internal/configstore"
	"motty/internal/vault"
)

// The ordinary vault key is first persisted after the signed migration journal.
// Stop at that boundary and reopen from just the durable profile files.
type interruptDisableStore struct {
	vault.Keystore
	journal string
}

func (s interruptDisableStore) Load(service, account string) ([]byte, error) {
	if _, err := os.Stat(s.journal); err == nil {
		panic("disable interrupted")
	}
	return s.Keystore.Load(service, account)
}

func TestInterruptedDisableUsesSealedStagingAndCanRestoreDeviceAccess(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		name := "finish"
		if corrupt {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			g, dir := fixture(t)
			const password = "master password"
			if err := g.Enable(password); err != nil {
				t.Fatal(err)
			}
			if err := g.SetDeviceBlob([]byte("native enrollment reference")); err != nil {
				t.Fatal(err)
			}
			ordinaryKeys := g.options.Keystore
			g.options.Keystore = interruptDisableStore{ordinaryKeys, g.path("protection-migration.json")}
			func() {
				defer func() {
					if got := recover(); got != "disable interrupted" {
						t.Fatalf("failed to interrupt commit: %v", got)
					}
				}()
				t.Fatal(g.Disable(password))
			}()
			if !g.PendingMigration() || !g.Locked() {
				t.Fatal("pending profile exposed to existing windows")
			}
			if _, _, err := configstore.ReadOnly(g.options.ConfigPath, 28); !errors.Is(err, configstore.ErrLocked) {
				t.Fatalf("existing configuration key survived: %v", err)
			}
			g.options.Keystore = ordinaryKeys
			cold, err := Open(g.options)
			if err != nil {
				t.Fatal(err)
			}
			stage := filepath.Join(dir, cold.pending.Stage)
			var configStep migrationStep
			for _, step := range cold.pending.Steps {
				if step.Target == "config.db" {
					configStep = step
				}
			}
			if !configStep.Sealed {
				t.Fatal("unprotected staged image")
			}
			nextPath := filepath.Join(stage, configStep.Staged)
			data, err := os.ReadFile(nextPath)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.HasPrefix(data, []byte("SQLite format 3")) || bytes.Contains(data, []byte("private.example")) {
				t.Fatal("ordinary database was written during preparation")
			}
			if corrupt {
				data[len(data)-1] ^= 1
				if err := os.WriteFile(nextPath, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := cold.Unlock("incorrect password"); !errors.Is(err, ErrWrongPassword) || !cold.PendingMigration() {
				t.Fatal("incorrect password advanced recovery")
			}
			err = cold.Unlock(password)
			if corrupt {
				if !errors.Is(err, ErrMigrationRolledBack) || !cold.Locked() || !cold.Enabled() || cold.PendingMigration() {
					t.Fatalf("failed to roll back protected state: %v", err)
				}
				if got := string(cold.DeviceBlob()); got != "native enrollment reference" {
					t.Fatal("rollback lost enrollment")
				}
				if _, err := os.Stat(filepath.Join(dir, "vault.key")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("rollback left an ordinary vault key")
				}
				if err := cold.Unlock(password); err != nil {
					t.Fatal(err)
				}
				if err := cold.Disable(password); err != nil {
					t.Fatal(err)
				}
			} else if err != nil || cold.PendingMigration() || cold.Enabled() {
				t.Fatalf("failed to finish disable: %v", err)
			}
			fresh, err := Open(g.options)
			if err != nil || fresh.Enabled() {
				t.Fatalf("ordinary restart: %v", err)
			}
			entries, _, err := configstore.ReadOnly(g.options.ConfigPath, 28)
			if err != nil || len(entries) != 1 {
				t.Fatalf("restored config: %v", err)
			}
			v, err := vault.Open(g.options.VaultPath, ordinaryKeys)
			if err != nil {
				t.Fatal(err)
			}
			if value, ok, err := v.Get("host:one:password"); err != nil || !ok || value != "secret-account-password" {
				t.Fatalf("restored vault: %v", err)
			}
		})
	}
}

type wrongReadbackStore struct {
	vault.Keystore
	saved bool
}

func (s *wrongReadbackStore) Store(service, account string, key []byte) error {
	if err := s.Keystore.Store(service, account, key); err != nil {
		return err
	}
	s.saved = true
	return nil
}

func (s *wrongReadbackStore) Load(service, account string) ([]byte, error) {
	if s.saved {
		return bytes.Repeat([]byte{17}, 32), nil
	}
	return s.Keystore.Load(service, account)
}

func TestDisableRollsBackWhenOrdinaryKeyCannotBeReadBack(t *testing.T) {
	g, dir := fixture(t)
	const password = "master password"
	if err := g.Enable(password); err != nil {
		t.Fatal(err)
	}
	if err := g.SetDeviceBlob([]byte("native reference")); err != nil {
		t.Fatal(err)
	}
	g.options.Keystore = &wrongReadbackStore{Keystore: g.options.Keystore}
	if err := g.Disable(password); err == nil || !g.Enabled() || g.Locked() || g.PendingMigration() {
		t.Fatalf("unexpected state after failed ordinary key verification: %v", err)
	}
	if string(g.DeviceBlob()) != "native reference" {
		t.Fatal("rollback removed the native reference")
	}
	if _, err := os.Stat(filepath.Join(dir, "vault.key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("uncommitted ordinary key remained")
	}
	v, err := vault.Open(g.options.VaultPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok, err := v.Get("host:one:password"); err != nil || !ok || got != "secret-account-password" {
		t.Fatalf("protected vault after rollback: %v", err)
	}
}

func TestDisableRefusesToReplaceMissingProtectedFile(t *testing.T) {
	for _, name := range []string{"config.db", "vault.json"} {
		t.Run(name, func(t *testing.T) {
			g, _ := fixture(t)
			const password = "master password"
			if err := g.Enable(password); err != nil {
				t.Fatal(err)
			}
			path := g.path(name)
			if err := os.Rename(path, path+".away"); err != nil {
				t.Fatal(err)
			}
			if err := g.Disable(password); !errors.Is(err, ErrCorrupt) || !g.Enabled() || !g.Locked() || g.PendingMigration() {
				t.Fatalf("missing protected source at disable: %v", err)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("disable created a replacement file")
			}
			if err := os.Rename(path+".away", path); err != nil {
				t.Fatal(err)
			}
			if err := g.Unlock(password); err != nil {
				t.Fatal(err)
			}
			if err := g.Disable(password); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProtectedVaultCannotBeClearedWhileLocked(t *testing.T) {
	g, _ := fixture(t)
	const password = "master password"
	if err := g.Enable(password); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(g.options.VaultPath, g.options.Keystore)
	if err != nil {
		t.Fatal(err)
	}
	g.Lock()
	if err := v.Clear(); !errors.Is(err, vault.ErrLocked) {
		t.Fatalf("locked Clear: %v", err)
	}
	if err := g.Unlock(password); err != nil {
		t.Fatal(err)
	}
	if value, ok, err := v.Get("host:one:password"); err != nil || !ok || value != "secret-account-password" {
		t.Fatal("secret lost across lock")
	}
}
