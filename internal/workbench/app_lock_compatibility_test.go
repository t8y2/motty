package workbench

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"motty/internal/protection"
	"motty/internal/vault"
)

func TestAppLockWritesAcrossWindowsAndProtectionGenerations(t *testing.T) {
	world, options := newWorld(t)
	group := NewGroup()
	options.Group = group
	first := newTestState(t, options)
	connect(t, first, world)
	if err := first.saveWithResultMode(false); err != nil {
		t.Fatal(err)
	}
	options.WindowID = 2
	older := newTestState(t, options)
	host := first.hosts[0]
	if _, ok := older.hostByCatalogID(host.CatalogID); !ok {
		t.Fatal("both windows must start with the same saved host")
	}
	first.hosts[0].Label = "first window edit"
	if err := first.saveWithResultMode(false); err != nil {
		t.Fatal(err)
	}
	const master = "first generation password"
	if err := finishProtectionAction(t, first, appLockEnable, "", master); err != nil {
		t.Fatal(err)
	}
	root, err := first.appProtection.gate.EnrollmentKey(master)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(root)
	if err := older.saveWithResultMode(false); err != nil {
		t.Fatal(err)
	}
	options.WindowID = 3
	fresh := newTestState(t, options)
	current, ok := fresh.hostByCatalogID(host.CatalogID)
	if !ok || current.Label != "first window edit" {
		t.Fatal("a stale window overwrote the edit after enable")
	}
	fresh.hosts[fresh.hostIndex(current.ID)].Label = "new window edit"
	if err := fresh.saveWithResultMode(false); err != nil {
		t.Fatal(err)
	}
	if err := fresh.secrets.vault.Set(prodPasswordName, "saved after enable"); err != nil {
		t.Fatal(err)
	}
	if err := finishProtectionAction(t, older, appLockDisable, master, ""); err != nil {
		t.Fatal(err)
	}
	if err := first.secrets.vault.Set(prodPasswordName, "saved after disable"); err != nil {
		t.Fatal(err)
	}
	options.WindowID = 4
	ordinary := newTestState(t, options)
	if current, ok := ordinary.hostByCatalogID(host.CatalogID); !ok || current.Label != "new window edit" {
		t.Fatal("newer protected edit did not survive disable")
	}
	const next = "second generation password"
	if err := finishProtectionAction(t, ordinary, appLockEnable, "", next); err != nil {
		t.Fatal(err)
	}
	fresh.lockApplication()
	if err := fresh.appProtection.gate.UnlockWithKey(root); !errors.Is(err, protection.ErrWrongPassword) {
		t.Fatalf("a previous generation's device key was accepted: %v", err)
	}
	if err := finishProtectionAction(t, first, appLockUnlock, next, ""); err != nil {
		t.Fatal(err)
	}
	for _, w := range []*State{first, older, fresh, ordinary} {
		if got, ok, err := w.secrets.vault.Get(prodPasswordName); err != nil || !ok || got != "saved after disable" {
			t.Fatalf("credential lost across window generations: %v", err)
		}
	}
}

func TestAppLockEnablePreservesLegacyPasswordVaultUntilConversion(t *testing.T) {
	options := testOptions(t)
	options.Group = NewGroup()
	first := newTestState(t, options)
	if err := first.secrets.vault.Set("fixture", "existing credential"); err != nil {
		t.Fatal(err)
	}
	const oldPassword = "old vault password"
	if err := first.secrets.vault.LegacyForTests(oldPassword); err != nil {
		t.Fatal(err)
	}
	options.WindowID = 2
	second := newTestState(t, options)
	path := filepath.Join(filepath.Dir(options.StatePath), "vault.json")
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const master = "new workspace password"
	if err := finishProtectionAction(t, first, appLockEnable, "", master); !errors.Is(err, vault.ErrNeedsConversion) {
		t.Fatalf("expected guidance for the legacy vault: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, saved) {
		t.Fatalf("failed enable rewrote the original vault: %v", err)
	}
	if first.appAccessBlocked() || second.appAccessBlocked() || first.appLockEnabled() || !second.secrets.vault.NeedsConversion() {
		t.Fatal("previous ordinary workspace was not accessible in both windows")
	}
	if err := second.secrets.vault.ConvertLegacy(oldPassword); err != nil {
		t.Fatal(err)
	}
	if err := finishProtectionAction(t, first, appLockEnable, "", master); err != nil {
		t.Fatal(err)
	}
	first.lockApplication()
	if err := finishProtectionAction(t, second, appLockUnlock, master, ""); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := first.secrets.vault.Get("fixture"); err != nil || !ok || got != "existing credential" {
		t.Fatalf("converted credential did not survive enable and unlock: %v", err)
	}
}
