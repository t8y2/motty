package workbench

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"motty/internal/i18n"
	"motty/internal/vault"
)

func finishProtectionAction(t *testing.T, s *State, action appLockAction, current, next string) error {
	t.Helper()
	done := make(chan error, 1)
	s.runAppLockAction(action, current, next, func(err error) { done <- err })
	timeout := time.NewTimer(15 * time.Second)
	defer timeout.Stop()
	select {
	case err := <-done:
		return err
	case finish := <-s.appProtection.results:
		finish()
		return <-done
	case <-timeout.C:
		t.Fatal("timed out waiting for profile operation")
	}
	return nil
}

func TestApplicationProtectionExistingPortableProfile(t *testing.T) {
	world, options := newWorld(t)
	keyPath := filepath.Join(filepath.Dir(options.StatePath), "vault.key")
	options.Keystore = vault.FileKeystore{Path: keyPath}
	options.Group = NewGroup()
	s := newTestState(t, options)
	connect(t, s, world)
	s.chooseLanguage(i18n.Chinese)
	s.save()
	if err := s.secrets.vault.Set(prodPasswordName, "portable-fixture-secret"); err != nil {
		t.Fatal(err)
	}
	if s.appLocked() || s.appLockEnabled() {
		t.Fatal("unexpected opt-in")
	}
	options.WindowID = 2
	other := newTestState(t, options)
	firstPassword, secondPassword := "original fixture password", "replacement fixture password"
	if err := finishProtectionAction(t, s, appLockEnable, "", firstPassword); err != nil {
		t.Fatal(err)
	}
	if !other.appLockEnabled() {
		t.Fatal("protection was not shared")
	}
	raw, err := os.ReadFile(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("prod.example.com")) {
		t.Fatal("address remained in config database")
	}
	if _, err := os.Stat(keyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old portable key: %v", err)
	}
	s.lockApplication()
	if !other.appLocked() {
		t.Fatal("second window was not locked")
	}
	if _, err := other.backupDocument(); !errors.Is(err, errAppLocked) {
		t.Fatalf("export while locked: %v", err)
	}
	if s.ai.inbox != nil {
		hub := NewAIHub(filepath.Join(filepath.Dir(options.StatePath), "ai.db"))
		hub.Add(s, nil)
		if r := hub.dispatch(context.Background(), "list_hosts", nil); !r.Denied {
			t.Fatal("locked hub served host list")
		}
		hub.Close()
	}
	if err := finishProtectionAction(t, other, appLockUnlock, "incorrect", ""); err == nil || !s.appLocked() {
		t.Fatal("wrong password unlocked the profile")
	}
	if err := finishProtectionAction(t, other, appLockUnlock, firstPassword, ""); err != nil {
		t.Fatal(err)
	}
	if len(s.sessions) == 0 {
		t.Fatal("live session was discarded")
	}
	if err := finishProtectionAction(t, s, appLockChange, firstPassword, secondPassword); err != nil {
		t.Fatal(err)
	}
	s.lockApplication()
	if err := finishProtectionAction(t, s, appLockUnlock, firstPassword, ""); err == nil {
		t.Fatal("old password still accepted")
	}
	other.Close()
	s.Close()

	options.Group = NewGroup()
	options.WindowID = 1
	reopened := newTestState(t, options)
	before := len(world.conns)
	reopened.Wake()
	if reopened.language != i18n.Chinese {
		t.Fatal("cold lock screen lost language preference")
	}
	if !reopened.appLockCold() || len(reopened.hosts) != 0 || len(world.conns) != before {
		t.Fatal("restored profile before authentication")
	}
	if err := finishProtectionAction(t, reopened, appLockUnlock, secondPassword, ""); err != nil {
		t.Fatal(err)
	}
	if len(reopened.hosts) == 0 {
		t.Fatal("unlock did not restore hosts")
	}
	if secret, ok, err := reopened.secrets.vault.Get(prodPasswordName); err != nil || !ok || secret != "portable-fixture-secret" {
		t.Fatalf("secret did not survive restart: %v", err)
	}
	if err := finishProtectionAction(t, reopened, appLockDisable, secondPassword, ""); err != nil {
		t.Fatal(err)
	}
	if reopened.appLockEnabled() {
		t.Fatal("protection still enabled")
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatal("portable key not restored", err)
	}
}

func TestApplicationProtectionPreservesOtherDataDirectories(t *testing.T) {
	first, other := testOptions(t), testOptions(t)
	other.Keystore = first.Keystore
	current := newTestState(t, first)
	neighbor := newTestState(t, other)
	if err := neighbor.secrets.vault.Set("neighbor", "neighbor value"); err != nil {
		t.Fatal(err)
	}
	if err := current.secrets.vault.Set("current", "current value"); err != nil {
		t.Fatal(err)
	}
	assertNeighborReadable := func() {
		t.Helper()
		fresh, err := vault.Open(filepath.Join(filepath.Dir(other.StatePath), "vault.json"), other.Keystore)
		if err != nil {
			t.Fatal(err)
		}
		if value, ok, err := fresh.Get("neighbor"); err != nil || !ok || value != "neighbor value" {
			t.Fatalf("other data directory lost its saved credential: %v", err)
		}
	}
	password := "test master password"
	if err := finishProtectionAction(t, current, appLockEnable, "", password); err != nil {
		t.Fatal(err)
	}
	assertNeighborReadable()
	if err := finishProtectionAction(t, current, appLockDisable, password, ""); err != nil {
		t.Fatal(err)
	}
	assertNeighborReadable()
}

func TestProfileLeaseExcludesStandaloneAgentDuringMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	agent, err := profileLease(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if migration, err := profileLease(path, true); err == nil {
		migration.Close()
		t.Fatal("migration raced standalone reader")
	}
	agent.Close()
	migration, err := profileLease(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer migration.Close()
	if agent, err := profileLease(path, false); err == nil {
		agent.Close()
		t.Fatal("reader opened during migration")
	}
}
