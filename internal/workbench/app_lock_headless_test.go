//go:build windows || darwin || linux

package workbench

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/app/devicekey"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"motty/internal/i18n"
)

func TestAppLockHeadlessVisualStates(t *testing.T) {
	for _, name := range []string{"system-dark", "password-dark", "password-light", "cold-dark", "settings-dark", "settings-disabled-dark", "change-dialog-dark", "revoke-dialog-dark", "migration-dark"} {
		t.Run(name, func(t *testing.T) {
			s := appLockPaintState(t, devicekey.VerificationWindowsHello)
			if name == "password-dark" || name == "password-light" || name == "cold-dark" {
				s.appProtection.systemEnabled = false
			}
			if name == "password-light" {
				s.setAppearance(appearanceLight)
			}
			if name == "cold-dark" {
				s.profileDeferred = true
			}
			if name == "migration-dark" {
				s.appProtection.migrating.Store(true)
			}
			settings := name == "settings-dark" || name == "settings-disabled-dark" || name == "change-dialog-dark" || name == "revoke-dialog-dark"
			if settings {
				s.appProtection.locked.Store(false)
				if name == "settings-disabled-dark" {
					s.appProtection.enabled, s.appProtection.systemEnabled = false, false
				}
				s.openSettingsCategory(settingsSecurity)
				if name == "change-dialog-dark" {
					s.openAppLockForm(appLockChange)
				}
				if name == "revoke-dialog-dark" {
					s.openAppLockForm(appLockRevoke)
				}
			}
			s.setLanguage(i18n.Chinese)
			view, err := headless.Open(s.WindowConfig(app.Config{Title: "Motty", Width: 1200, Height: 840, Tree: Tree(), Paint: s.Paint, OnWake: s.Wake, OnKey: s.Key, OnShortcut: s.Shortcut, OnPointer: s.Pointer, OnScroll: s.Scroll, OnResize: s.Resize}), nil, headless.Options{Scale: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			if name == "change-dialog-dark" || name == "revoke-dialog-dark" {
				if s.modal != modalAppLock {
					t.Fatalf("security dialog closed during initial paint: modal=%d locked=%v", s.modal, s.appLocked())
				}
				if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitAppLockFormFields.At(0)); !ok {
					t.Fatalf("security dialog did not declare its password field: modal=%d count=%d", s.modal, s.lockUI.formCount)
				}
			}
			if !settings && name != "migration-dark" {
				field, ok := hitByIDSnapshot(view.Snapshot().Hits, hitAppLockPassword)
				if !ok || field.Bounds.Width < 250 || field.Bounds.Height < 32 {
					t.Fatal("password control is clipped")
				}
				if field.Bounds.Y+field.Bounds.Height > 775 {
					t.Fatal("password overlaps the footer")
				}
				if err := view.Click(field.Bounds.X+20, field.Bounds.Y+field.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				if err := view.Type("sample-password"); err != nil {
					t.Fatal(err)
				}
				if s.lockUI.password.String() != "sample-password" {
					t.Fatal("native field did not receive text")
				}
				for _, label := range view.Snapshot().Labels {
					if label.Value == "sample-password" {
						t.Fatal("password is visible before reveal")
					}
				}
				s.lockUI.password = text.Editor{}
				if err := view.Frame(); err != nil {
					t.Fatal(err)
				}
				submit, ok := hitByIDSnapshot(view.Snapshot().Hits, hitAppLockSubmit)
				if !ok {
					t.Fatal("password submit button missing")
				}
				if err := view.Click(submit.Bounds.X+submit.Bounds.Width/2, submit.Bounds.Y+submit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				if s.lockUI.error != s.messages.AppLock.Required {
					t.Fatal("the real password button click did not reach lock dispatch")
				}
				s.lockUI.error = ""
				if err := view.Frame(); err != nil {
					t.Fatal(err)
				}
			}
			if dir := os.Getenv("MOTTY_APP_LOCK_SCREENSHOTS"); dir != "" {
				// Dialogs enter with a fade; capture the settled native frame.
				if err := view.Wait(250 * time.Millisecond); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := view.Screenshot(filepath.Join(dir, name+".png")); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestAppLockHeadlessEnableLockAndPasswordUnlock(t *testing.T) {
	_, options := newWorld(t)
	options.Group = NewGroup()
	s := newTestState(t, options)
	s.chooseLanguage(i18n.Chinese)
	s.appProtection.supportChecked = true
	s.appProtection.systemMethod = devicekey.VerificationUnavailable
	// Use the same invalidator for background KDF completion and the driver,
	// so completion is adopted by Wake on the UI thread, as in a real window.
	s.options.Invalidate = s.options.Invalidator.Invalidate
	view, err := headless.Open(s.WindowConfig(app.Config{
		Title: "Motty", Width: 1200, Height: 840, Tree: Tree(), Invalidator: s.options.Invalidator,
		Paint: s.Paint, OnWake: s.Wake, OnKey: s.Key, OnShortcut: s.Shortcut, OnPointer: s.Pointer, OnScroll: s.Scroll, OnResize: s.Resize,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	click := func(id gfx.ID) {
		t.Helper()
		hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
		if !ok {
			t.Fatalf("missing UI control %v", id)
		}
		if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
	}
	typeField := func(id gfx.ID, value string) {
		t.Helper()
		click(id)
		if err := view.Type(value); err != nil {
			t.Fatal(err)
		}
	}
	waitOperation := func() {
		t.Helper()
		if err := view.WaitFor(func(app.Snapshot) bool { return !s.appLockBusy() }, 15*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	click(hitSettings)
	click(hitSettingsCategoryList.At(int(settingsSecurity)))
	change, ok := hitByIDSnapshot(view.Snapshot().Hits, hitAppLockChange)
	if !ok || !change.Disabled {
		t.Fatal("change-password row is not visibly disabled before enrollment")
	}
	click(hitAppLockToggle)
	if s.appLockEnabled() {
		t.Fatal("the switch enabled protection before password confirmation")
	}
	if err := view.Wait(250 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	const password = "native UI fixture password"
	typeField(hitAppLockFormFields.At(0), password)
	typeField(hitAppLockFormFields.At(1), password)
	click(hitAppLockFormSubmit)
	waitOperation()
	if !s.appLockEnabled() || s.appLocked() || s.modal != modalNone {
		t.Fatalf("enable did not complete: enabled=%v locked=%v modal=%d error=%q", s.appLockEnabled(), s.appLocked(), s.modal, s.lockUI.error)
	}
	click(hitAppLock)
	if !s.appLocked() {
		t.Fatal("sidebar button did not lock the profile")
	}
	s.appProtection.supportChecked = true
	s.appProtection.systemMethod = devicekey.VerificationUnavailable
	typeField(hitAppLockPassword, "incorrect password")
	click(hitAppLockSubmit)
	waitOperation()
	if !s.appLocked() || s.lockUI.error != s.messages.AppLock.WrongPassword {
		t.Fatalf("wrong password did not stay locked: locked=%v error=%q", s.appLocked(), s.lockUI.error)
	}
	typeField(hitAppLockPassword, password)
	click(hitAppLockSubmit)
	waitOperation()
	if s.appLocked() || s.lockUI.error != "" {
		t.Fatalf("password unlock failed: locked=%v error=%q", s.appLocked(), s.lockUI.error)
	}
	if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitAppLockPassword); ok {
		t.Fatal("unlock left the lock page on screen")
	}
	click(hitAppLockToggle)
	if !s.appLockEnabled() || s.lockUI.formAction != appLockDisable {
		t.Fatal("disable switch changed committed protection before confirmation")
	}
	click(hitAppLockFormClose)
	if !s.appLockEnabled() {
		t.Fatal("canceling disable changed protection")
	}
	click(hitAppLockToggle)
	if err := view.Wait(250 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	typeField(hitAppLockFormFields.At(0), password)
	click(hitAppLockFormSubmit)
	waitOperation()
	if s.appLockEnabled() || s.modal != modalNone {
		t.Fatalf("disable did not finish: enabled=%v error=%q", s.appLockEnabled(), s.lockUI.error)
	}
	if change, ok := hitByIDSnapshot(view.Snapshot().Hits, hitAppLockChange); !ok || !change.Disabled {
		t.Fatal("change-password control did not return to disabled state")
	}
}
