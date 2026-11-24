package workbench

import (
	"errors"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/app/devicekey"
	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
	"motty/internal/protection"
)

func appLockPaintState(t *testing.T, method devicekey.VerificationMethod) *State {
	t.Helper()
	s := newTestState(t, testOptions(t))
	s.setLanguage(i18n.Chinese)
	s.appProtection.enabled = true
	s.appProtection.systemEnabled = method != devicekey.VerificationUnavailable
	s.appProtection.supportChecked, s.appProtection.systemMethod = true, method
	s.appProtection.locked.Store(true)
	return s
}

func TestAppLockPageHidesWorkspaceAndAlwaysOffersPassword(t *testing.T) {
	for _, method := range []devicekey.VerificationMethod{devicekey.VerificationUnavailable, devicekey.VerificationTouchID, devicekey.VerificationWindowsHello, devicekey.VerificationDevicePassword} {
		s := appLockPaintState(t, method)
		canvas, texts := paintedTexts(s)
		if !contains(texts, s.messages.AppLock.Title) || !contains(texts, s.messages.AppLock.UnlockPassword) {
			t.Fatal("lock title or password fallback is missing")
		}
		password, ok := hitByID(canvas, hitAppLockPassword)
		if !ok || !password.Secure {
			t.Fatal("the always-visible password field is missing or revealed")
		}
		if _, ok := hitByID(canvas, hitAppLockSystem); ok != (method != devicekey.VerificationUnavailable) {
			t.Fatal("system action does not follow capability/enrollment")
		}
		for _, hit := range canvas.Hits() {
			if !appLockOwns(hit.ID) {
				t.Fatalf("business control behind the lock: %v", hit.ID)
			}
		}
		for _, host := range s.hosts {
			if host.Label != "" && contains(texts, host.Label) {
				t.Fatal("a saved host is visible on the lock page")
			}
		}
		if contains(texts, s.messages.AppLock.KeepRunning) {
			t.Fatal("empty profile claims that a connection is running")
		}
	}
}

func TestAppLockColdPageDescribesRestoreAndRejectsStaleInput(t *testing.T) {
	s := appLockPaintState(t, devicekey.VerificationUnavailable)
	s.profileDeferred = true
	_, texts := paintedTexts(s)
	if !contains(texts, s.messages.AppLock.ColdDescription) || !contains(texts, s.messages.AppLock.RestoreAfterUnlock) || contains(texts, s.messages.AppLock.KeepRunning) {
		t.Fatal("cold-start copy promises a running session")
	}
	if s.EditorFor(hitSearch) != nil || s.EditorFor(hitAppLockPassword) == nil {
		t.Fatal("editor routing leaks behind lock")
	}
	config := s.WindowConfig(app.Config{OnKey: s.Key, OnShortcut: s.Shortcut, OnPointer: s.Pointer})
	if !config.OnKey(gfx.KeyEvent{Target: hitSearch, Key: gfx.KeyEnter}).Handled {
		t.Fatal("stale host key escaped lock")
	}
	if !config.OnShortcut(gfx.KeyEvent{Target: hitSearch, Key: gfx.KeyEnter}).Handled {
		t.Fatal("stale shortcut escaped lock")
	}
	if !config.OnPointer(gfx.PointerEvent{Target: hitHostList.At(1), Kind: gfx.PointerDown}).Handled {
		t.Fatal("stale host pointer escaped lock")
	}
	s.Click(hitSettings)
	if s.activeSection == sectionSettings {
		t.Fatal("direct click escaped lock")
	}
}

func TestAppLockPasswordDialogValidatesBeforeStartingWorkAndClearsEditors(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	s.openAppLockForm(appLockEnable)
	s.lockUI.formFields[0].editor.Insert("short")
	s.submitAppLockForm()
	if s.lockUI.error != s.messages.AppLock.Minimum || s.lockUI.busy {
		t.Fatal("short password started a migration")
	}
	s.lockUI.formFields[0].editor.Insert("-enough")
	s.lockUI.formFields[1].editor.Insert("different")
	s.submitAppLockForm()
	if s.lockUI.error != s.messages.AppLock.Mismatch || s.lockUI.busy {
		t.Fatal("mismatched confirmation started a migration")
	}
	s.closeModal()
	for _, field := range s.lockUI.formFields {
		if field.editor.String() != "" {
			t.Fatal("closed security form retained a password")
		}
	}
}

func TestAppLockMigrationPageRejectsEveryEditor(t *testing.T) {
	s := appLockPaintState(t, devicekey.VerificationUnavailable)
	s.appProtection.locked.Store(false)
	s.appProtection.migrating.Store(true)
	canvas, texts := paintedTexts(s)
	if !contains(texts, s.messages.AppLock.Updating) {
		t.Fatal("shared migration does not cover the workspace")
	}
	for _, hit := range canvas.Hits() {
		if hit.Role == gfx.RoleTextField && !hit.Disabled {
			t.Fatal("migration allows password input")
		}
	}
	if s.EditorFor(hitAppLockPassword) != nil || s.EditorFor(hitSearch) != nil {
		t.Fatal("migration exposed an editor")
	}
}

func TestAppLockErrorsAreLocalizedAndSystemCancelKeepsFallbackQuiet(t *testing.T) {
	s := newTestState(t, freshOptions(t))
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		s.setLanguage(language)
		if s.appLockErrorText(protection.ErrWrongPassword) != s.messages.AppLock.WrongPassword {
			t.Fatal("wrong-password translation missing")
		}
		if s.appLockErrorText(errors.Join(errors.New("native"), devicekey.ErrCanceled)) != "" {
			t.Fatal("canceling the native dialog is shown as an error")
		}
		if s.appLockErrorText(devicekey.ErrNotFound) != s.messages.AppLock.SystemMissing {
			t.Fatal("missing system credential does not explain the fallback")
		}
	}
}
