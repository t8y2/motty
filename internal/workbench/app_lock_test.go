package workbench

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/app/systemauth"
	"github.com/vivaui/viva/gfx"
	"motty/internal/configstore"
)

func finishAppLock(t *testing.T, s *State, action appLockAction, current, next string) error {
	t.Helper()
	done := make(chan error, 1)
	s.runAppLockAction(action, current, next, func(err error) { done <- err })
	select {
	case err := <-done:
		return err
	case finish := <-s.appProtection.results:
		finish()
		return <-done
	case <-time.After(10 * time.Second):
		t.Fatal("application lock operation timed out")
		return nil
	}
}

func TestAppLockChangesOnlyLocalVerifierAndRestoresWindows(t *testing.T) {
	opts := testOptions(t)
	opts.Group = NewGroup()
	first := newTestState(t, opts)
	opts.WindowID = 2
	second := newTestState(t, opts)
	config := opts.StatePath
	dir := filepath.Dir(config)
	vault := filepath.Join(dir, "vault.json")
	if err := first.secrets.vault.Set("lock-test", "test credential"); err != nil {
		t.Fatal(err)
	}
	originalConfig, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	originalVault, err := os.ReadFile(vault)
	if err != nil {
		t.Fatal(err)
	}
	const password = "long fixture password"
	if err := finishAppLock(t, first, appLockEnable, "", password); err != nil {
		t.Fatal(err)
	}
	if !second.appLockEnabled() || second.appLocked() {
		t.Fatal("the other window did not share the new setting")
	}
	configAfterSetup, err := os.ReadFile(config)
	if err != nil || !bytes.Equal(originalConfig, configAfterSetup) {
		t.Fatalf("enabling the UI lock rewrote SQLite: %v", err)
	}
	verifierBytes, err := os.ReadFile(first.appProtection.verifierPath)
	if err != nil || bytes.Contains(verifierBytes, []byte(password)) {
		t.Fatalf("expected a password verifier without plaintext: %v", err)
	}
	first.lockApplication()
	if !second.appLocked() {
		t.Fatal("the second window remained visible")
	}
	opts.WindowID = 3
	third := newTestState(t, opts)
	if !third.appLocked() || !third.profileDeferred || third.config != nil {
		t.Fatal("window opened without the profile lock")
	}
	if err := finishAppLock(t, second, appLockUnlock, "incorrect", ""); !errors.Is(err, errLockWrongPassword) || !first.appLocked() {
		t.Fatalf("bad password: %v", err)
	}
	if err := finishAppLock(t, third, appLockUnlock, password, ""); err != nil {
		t.Fatal(err)
	}
	if first.appLocked() || second.appLocked() || third.config == nil {
		t.Fatal("not all windows reopened")
	}
	const next = "different fixture password"
	if err := finishAppLock(t, second, appLockChange, password, next); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if err := finishAppLock(t, second, appLockDisable, password, ""); !errors.Is(err, errLockWrongPassword) {
		t.Fatalf("disable with previous password: %v", err)
	}
	if err := finishAppLock(t, second, appLockDisable, next, ""); err != nil {
		t.Fatal(err)
	}
	if first.appLockEnabled() || second.appLocked() {
		t.Fatal("disable left lock active")
	}
	if _, err := os.Stat(first.appProtection.verifierPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("verifier after disable: %v", err)
	}
	nextVault, err := os.ReadFile(vault)
	if err != nil || !bytes.Equal(originalVault, nextVault) {
		t.Fatalf("vault changed: %v", err)
	}
	entries, _, err := configstore.ReadOnly(config, configFormat)
	if err != nil || len(entries) == 0 {
		t.Fatalf("ordinary SQLite could not read: %v", err)
	}
	nextConfig, err := os.ReadFile(config)
	if err != nil || !bytes.Equal(originalConfig[60:64], nextConfig[60:64]) {
		t.Fatalf("SQLite schema changed: %v", err)
	}
}

func TestAppLockColdAppearance(t *testing.T) {
	for _, mode := range []appearanceMode{appearancePinnedLight, appearancePinnedDark} {
		t.Run(map[appearanceMode]string{appearancePinnedLight: "light", appearancePinnedDark: "dark"}[mode], func(t *testing.T) {
			opts := testOptions(t)
			opts.RememberWindowFrame = true
			first := newTestState(t, opts)
			first.chooseAppearanceMode(mode)
			frame := gfx.Rect{X: 120, Y: 80, Width: 1234, Height: 777}
			first.NoteWindowFrame(frame)
			first.SetWindowMaximized(true)
			first.save()
			const password = "long fixture password"
			if err := finishAppLock(t, first, appLockEnable, "", password); err != nil {
				t.Fatal(err)
			}
			first.Close()
			cold := newTestState(t, opts)
			if !cold.appLocked() || !cold.profileDeferred || cold.appearanceMode != mode {
				t.Fatal("saved appearance was not restored on lock screen")
			}
			if got, ok := cold.RestoredWindowFrame(); !ok || got != frame || !cold.RestoredWindowMaximized() {
				t.Fatalf("locked startup restored frame %+v, maximized=%v", got, cold.RestoredWindowMaximized())
			}
			var canvas gfx.Canvas
			cold.Paint(&canvas, gfx.Size{Width: 1100, Height: 760})
			if _, ok := hitByID(&canvas, hitAppLockPassword); !ok {
				t.Fatal("no password field")
			}
			frame.X += 20
			cold.SetWindowMaximized(false)
			cold.NoteWindowFrame(frame)
			cold.Launch([]string{"ssh://person@example.invalid"})
			if err := finishAppLock(t, cold, appLockUnlock, password, ""); err != nil {
				t.Fatal(err)
			}
			if cold.appLocked() || cold.appearanceMode != mode {
				t.Fatal("unlock discarded appearance")
			}
			if got, ok := cold.RestoredWindowFrame(); !ok || got != frame || cold.RestoredWindowMaximized() {
				t.Fatalf("unlock discarded changes to the window: frame %+v, maximized=%v", got, cold.RestoredWindowMaximized())
			}
			cold.Wake()
			if cold.modal != modalOpenLink {
				t.Fatal("launch request received while locked was lost")
			}
		})
	}
}

func TestAppLockSystemVerificationRequiresLocalEnrollment(t *testing.T) {
	opts := testOptions(t)
	state := newTestState(t, opts)
	promptErr := error(systemauth.ErrCanceled)
	prompts, markerReads := 0, 0
	markers := map[string][]byte{}
	state.appProtection.system = appLockSystem{
		method: func() systemauth.VerificationMethod { return systemauth.VerificationWindowsHello },
		verify: func(context.Context, string) error { prompts++; return promptErr },
		load:   func(_, id string) ([]byte, error) { markerReads++; return markers[id], nil },
		store:  func(_, id string, data []byte) error { markers[id] = data; return nil },
		remove: func(_, id string) error { delete(markers, id); return nil },
	}
	const password = "long fixture password"
	if err := finishAppLock(t, state, appLockEnable, "", password); err != nil {
		t.Fatal(err)
	}
	if err := finishAppLock(t, state, appLockEnroll, password, ""); !errors.Is(err, systemauth.ErrCanceled) || len(markers) != 0 {
		t.Fatalf("canceled enrollment: %v", err)
	}
	if err := finishAppLock(t, state, appLockEnroll, "wrong", ""); !errors.Is(err, errLockWrongPassword) || prompts != 1 {
		t.Fatalf("enrollment did not check current password: %v", err)
	}
	promptErr = nil
	if err := finishAppLock(t, state, appLockEnroll, password, ""); err != nil || !state.appSystemEnabled() {
		t.Fatalf("enrollment: %v", err)
	}
	state.lockApplication()
	if err := finishAppLock(t, state, appLockUnlock, password, ""); err != nil {
		t.Fatalf("password unlock with system unlock configured: %v", err)
	}
	if markerReads != 0 {
		t.Fatal("password unlock depended on a read from the system credential store")
	}
	state.lockApplication()
	promptErr = systemauth.ErrCanceled
	if err := finishAppLock(t, state, appLockSystemUnlock, "", ""); !errors.Is(err, systemauth.ErrCanceled) || !state.appLocked() {
		t.Fatalf("canceled unlock: %v", err)
	}
	promptErr = nil
	if err := finishAppLock(t, state, appLockSystemUnlock, "", ""); err != nil || state.appLocked() {
		t.Fatalf("system unlock: %v", err)
	}
	verified := prompts
	state.clickAppLock(hitAppLockSystemToggle)
	if state.modal == modalAppLock || !state.appLockBusy() {
		t.Fatal("the system unlock switch did not disable directly")
	}
	select {
	case finish := <-state.appProtection.results:
		finish()
	case <-time.After(10 * time.Second):
		t.Fatal("system unlock disable timed out")
	}
	if state.appSystemEnabled() || state.appProtection.record.SystemID != "" || len(markers) != 0 || prompts != verified {
		t.Fatal("disabling system unlock did not remove the marker without prompting")
	}
	state.lockApplication()
	if err := finishAppLock(t, state, appLockSystemUnlock, "", ""); !errors.Is(err, systemauth.ErrUnavailable) || !state.appLocked() {
		t.Fatalf("unregistered unlock: %v", err)
	}
	if err := finishAppLock(t, state, appLockUnlock, password, ""); err != nil {
		t.Fatal(err)
	}
}

func TestAppLockQuitPreservesWindowsWhenItCannotAsk(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.keepWindows = keepWindowsAsk
	const password = "long fixture password"
	if err := finishAppLock(t, state, appLockEnable, "", password); err != nil {
		t.Fatal(err)
	}
	if !state.AsksAboutWindowsOnExit() {
		t.Fatal("unlocked window should ask before quitting")
	}
	state.lockApplication()
	if state.AsksAboutWindowsOnExit() || !state.KeepsWindowsOnExit() {
		t.Fatal("quitting from the lock screen should preserve the saved windows")
	}
}

func TestAppLockPrintableInputDoesNotFallThroughToWorkspace(t *testing.T) {
	s := newTestState(t, testOptions(t))
	s.openAppLockForm(appLockEnable)
	config := s.WindowConfig(app.Config{OnKey: s.Key, OnShortcut: s.Shortcut})
	event := gfx.KeyEvent{Text: "a"}
	if config.OnShortcut(event).Handled {
		t.Fatal("plain printable key was swallowed")
	}
	event.Modifiers = gfx.ModifierControl | gfx.ModifierShift
	if !config.OnShortcut(event).Handled {
		t.Fatal("workspace shortcut was not stopped")
	}
	if config.TextInput(app.TextInputContext{ID: hitSettingsSearch}) != nil {
		t.Fatal("stale workspace focus received input")
	}
}
