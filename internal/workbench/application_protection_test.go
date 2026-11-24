package workbench

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"motty/internal/configstore"
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

// A real worker finished, but the UI thread has not delivered its completion.
// This makes opening/closing windows in that interval deterministic.
func pendingProtectionResult(t *testing.T, s *State, action appLockAction, current, next string) (func(), <-chan error) {
	t.Helper()
	done := make(chan error, 1)
	s.runAppLockAction(action, current, next, func(err error) { done <- err })
	select {
	case err := <-done:
		t.Fatalf("action did not start: %v", err)
	case finish := <-s.appProtection.results:
		finishOnce := sync.OnceFunc(finish)
		t.Cleanup(finishOnce)
		return finishOnce, done
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for the profile operation")
	}
	return nil, nil
}

func TestApplicationProtectionLastWindowDuringEnableAndPasswordChange(t *testing.T) {
	options := testOptions(t)
	options.Group = NewGroup()
	s := newTestState(t, options)
	s.chooseLanguage(i18n.Chinese)
	if err := s.secrets.vault.Set("fixture", "saved credential"); err != nil {
		t.Fatal(err)
	}
	password, replacement := "first master password", "changed master password"
	finish, _ := pendingProtectionResult(t, s, appLockEnable, "", password)
	s.Close()
	options.WindowID++
	fresh := newTestState(t, options)
	if !fresh.profileDeferred || fresh.language != i18n.Chinese {
		t.Fatal("new window opened during migration or lost its cached language")
	}
	finish()
	if !fresh.appLocked() || fresh.config != nil || fresh.secrets.vault != nil {
		t.Fatal("closing the last window during enable left its replacement unlocked")
	}
	if err := finishProtectionAction(t, fresh, appLockUnlock, password, ""); err != nil {
		t.Fatal(err)
	}
	held := fresh.secrets.vault
	finish, _ = pendingProtectionResult(t, fresh, appLockChange, password, replacement)
	fresh.Close()
	if _, _, err := held.Get("fixture"); !errors.Is(err, vault.ErrLocked) {
		t.Fatalf("old handle after last close: %v", err)
	}
	options.WindowID++
	last := newTestState(t, options)
	finish()
	if !last.appLocked() || !last.profileDeferred {
		t.Fatal("a newly opened window inherited the previous unlock")
	}
	if err := finishProtectionAction(t, last, appLockUnlock, replacement, ""); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := last.secrets.vault.Get("fixture"); err != nil || !ok || got != "saved credential" {
		t.Fatal("new window lost the saved credential", err)
	}
}

func TestApplicationProtectionDisableReopensEveryWindowAndRetriesIntactFiles(t *testing.T) {
	options := testOptions(t)
	options.Group = NewGroup()
	s := newTestState(t, options)
	otherOptions := options
	otherOptions.WindowID = 2
	other := newTestState(t, otherOptions)
	if err := s.secrets.vault.Set("fixture", "saved credential"); err != nil {
		t.Fatal(err)
	}
	password := "test master password"
	if err := finishProtectionAction(t, s, appLockEnable, "", password); err != nil {
		t.Fatal(err)
	}
	finish, done := pendingProtectionResult(t, s, appLockDisable, password, "")
	baseline := other.configBase
	options.WindowID = 3
	during := newTestState(t, options)
	if !during.profileDeferred || during.config != nil {
		t.Fatal("new window opened its store while the migration held the lease")
	}
	path := filepath.Join(filepath.Dir(options.StatePath), "vault.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("temporary fixture read failure"), 0600); err != nil {
		t.Fatal(err)
	}
	finish()
	if err := <-done; err == nil || !s.appProtection.retryOpen || !other.appLocked() || !during.appLocked() {
		t.Fatalf("window accepted an unreadable ordinary vault: %v", err)
	}
	if matches, _ := filepath.Glob(path + ".bad-*"); len(matches) != 0 {
		t.Fatal("migration recovery replaced the previous vault")
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	// A temporarily unavailable database must not become a fresh empty one on retry.
	aside := options.StatePath + ".temporarily-unavailable"
	if err := os.Rename(options.StatePath, aside); err != nil {
		t.Fatal(err)
	}
	if err := finishProtectionAction(t, during, appLockUnlock, "", ""); err == nil || !during.appLocked() {
		t.Fatal("a missing ordinary database was accepted during retry")
	}
	if _, err := os.Stat(options.StatePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("retry created a replacement database", err)
	}
	if err := os.Rename(aside, options.StatePath); err != nil {
		t.Fatal(err)
	}
	if err := finishProtectionAction(t, during, appLockUnlock, "", ""); err != nil {
		t.Fatal(err)
	}
	for _, w := range []*State{s, other, during} {
		if w.appLocked() || w.appLockEnabled() || w.config == nil || w.profileDeferred {
			t.Fatal("ordinary profile did not reopen in every window")
		}
		if got, ok, err := w.secrets.vault.Get("fixture"); err != nil || !ok || got != "saved credential" {
			t.Fatal("credential missing after retry", err)
		}
	}
	if other.configBase != baseline {
		t.Fatal("migration replaced the live window's change-tracking baseline")
	}
}

func TestApplicationProtectionNewWindowReadErrorSealsPeers(t *testing.T) {
	options := testOptions(t)
	options.Group = NewGroup()
	existing := newTestState(t, options)
	password := "test master password"
	if err := finishProtectionAction(t, existing, appLockEnable, "", password); err != nil {
		t.Fatal(err)
	}
	existing.lockUI.shown = true
	configstore.LockProtection(options.StatePath)
	options.WindowID = 2
	fresh := newTestState(t, options)
	if !fresh.profileDeferred || !existing.appLocked() || existing.lockUI.shown {
		t.Fatal("failed opening a peer without concealing the existing workspace")
	}
	if err := finishProtectionAction(t, fresh, appLockUnlock, password, ""); err != nil {
		t.Fatal(err)
	}
	if fresh.config == nil || existing.appLocked() {
		t.Fatal("windows did not resume together")
	}
}

func TestApplicationProtectionWindowClosedDuringWrongPasswordDisable(t *testing.T) {
	options := testOptions(t)
	options.Group = NewGroup()
	first := newTestState(t, options)
	options.WindowID = 2
	second := newTestState(t, options)
	if err := first.secrets.vault.Set("fixture", "saved credential"); err != nil {
		t.Fatal(err)
	}
	password := "test master password"
	if err := finishProtectionAction(t, first, appLockEnable, "", password); err != nil {
		t.Fatal(err)
	}
	record := second.configBase.windowName
	finish, done := pendingProtectionResult(t, first, appLockDisable, "incorrect password", "")
	second.CloseKeepingRecord(false)
	options.WindowID = 3
	third := newTestState(t, options)
	finish()
	if err := <-done; err == nil {
		t.Fatal("accepted a wrong password for disable")
	}
	for _, w := range []*State{first, third} {
		if !w.appLockEnabled() || w.appAccessBlocked() || w.config == nil {
			t.Fatal("failed disable did not reopen the original protected workspace")
		}
		if got, ok, err := w.secrets.vault.Get("fixture"); err != nil || !ok || got != "saved credential" {
			t.Fatal("saved credential missing after failed disable", err)
		}
	}
	_, local, err := first.config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, restored := local[record]; restored {
		t.Fatal("closed window would return on the next restart")
	}
	first.Close()
	lastRecord := third.configBase.windowName
	third.CloseKeepingRecord(false)
	options.WindowID = 4
	reopened := newTestState(t, options)
	if !reopened.appLocked() {
		t.Fatal("last close left the profile unlocked")
	}
	if err := finishProtectionAction(t, reopened, appLockUnlock, password, ""); err != nil {
		t.Fatal(err)
	}
	_, local, err = reopened.config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, restored := local[lastRecord]; restored {
		t.Fatal("last window record was retained after the user discarded it")
	}
}

func TestApplicationProtectionRetriesColdWindowRestore(t *testing.T) {
	options := testOptions(t)
	options.Group = NewGroup()
	first := newTestState(t, options)
	password := "test master password"
	if err := finishProtectionAction(t, first, appLockEnable, "", password); err != nil {
		t.Fatal(err)
	}
	first.Close()
	options.Group = NewGroup()
	calls := 0
	options.ProfileUnlocked = func() error {
		calls++
		if calls == 1 {
			return errors.New("temporary failure reading window records")
		}
		return nil
	}
	cold := newTestState(t, options)
	if err := finishProtectionAction(t, cold, appLockUnlock, password, ""); err == nil || !cold.appLocked() || !cold.profileDeferred {
		t.Fatalf("a failed cold window restore was accepted: %v", err)
	}
	if err := finishProtectionAction(t, cold, appLockUnlock, password, ""); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || cold.profileDeferred || cold.appLocked() {
		t.Fatal("cold window records were never retried")
	}
}
