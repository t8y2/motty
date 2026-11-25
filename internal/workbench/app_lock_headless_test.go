//go:build windows || darwin || linux

package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/app/systemauth"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"motty/internal/i18n"
)

func TestAppLockHeadlessEnableLockAndPasswordUnlock(t *testing.T) {
	_, options := newWorld(t)
	options.Group = NewGroup()
	s := newTestState(t, options)
	s.chooseLanguage(i18n.Chinese)
	s.appProtection.supportChecked = true
	s.appProtection.method = systemauth.VerificationUnavailable
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
		editor := s.appLockEditor(id)
		if editor == nil {
			t.Fatal("missing password editor")
		}
		editor.SetSelection(text.Range{Start: 0, End: editor.RuneCount()})
		// Deliver key events through OnShortcut before the text event, as
		// a Windows keyboard does. Type inserts text directly and bypasses it.
		for _, r := range value {
			key := string(r)
			if r == ' ' {
				key = "space"
			}
			if err := view.Keys(key); err != nil {
				t.Fatal(err)
			}
		}
		if editor.String() != value {
			t.Fatal("password field did not receive keyboard input")
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
	const password = "test12"
	typeField(hitAppLockFormFields.At(0), "test1")
	typeField(hitAppLockFormFields.At(1), "test1")
	click(hitAppLockFormSubmit)
	if s.appLockBusy() || s.lockUI.error != s.messages.AppLock.Minimum {
		t.Fatal("accepted a five-character password")
	}
	typeField(hitAppLockFormFields.At(0), password)
	typeField(hitAppLockFormFields.At(1), password)
	click(hitAppLockFormSubmit)
	waitOperation()
	if !s.appLockEnabled() || s.appLocked() || s.modal != modalNone {
		t.Fatalf("enable did not complete: enabled=%v locked=%v modal=%d error=%q", s.appLockEnabled(), s.appLocked(), s.modal, s.lockUI.error)
	}
	click(hitAppLockChange)
	if err := view.Wait(250 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	const changedPassword = "Changed fixture password 2"
	const incorrect = "incorrect"
	typeField(hitAppLockFormFields.At(0), incorrect)
	typeField(hitAppLockFormFields.At(1), changedPassword)
	typeField(hitAppLockFormFields.At(2), changedPassword)
	click(hitAppLockFormSubmit)
	waitOperation()
	if s.modal != modalAppLock || s.lockUI.error != s.messages.AppLock.WrongPassword {
		t.Fatal("wrong current password did not leave the change form open")
	}
	for i, want := range []string{incorrect, changedPassword, changedPassword} {
		if s.lockUI.formFields[i].editor.String() != want {
			t.Fatalf("password field %d was cleared after failed verification", i)
		}
	}
	typeField(hitAppLockFormFields.At(0), password)
	click(hitAppLockFormSubmit)
	waitOperation()
	if s.modal != modalNone {
		t.Fatal("password change did not finish")
	}

	// Open the enrollment dialog without invoking a real system authenticator.
	s.appProtection.method = systemauth.VerificationWindowsHello
	s.appProtection.supportChecked = true
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	click(hitAppLockSystemToggle)
	if err := view.Wait(250 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if s.modal != modalAppLock || s.lockUI.formAction != appLockEnroll {
		t.Fatal("system enrollment dialog did not open")
	}
	typeField(hitAppLockFormFields.At(0), changedPassword)
	click(hitAppLockFormClose)

	click(hitAppLock)
	if !s.appLocked() {
		t.Fatal("sidebar button did not lock the profile")
	}
	s.appProtection.supportChecked = true
	s.appProtection.method = systemauth.VerificationUnavailable
	typeField(hitAppLockPassword, "incorrect password")
	click(hitAppLockSubmit)
	waitOperation()
	if !s.appLocked() || s.lockUI.error != s.messages.AppLock.WrongPassword {
		t.Fatalf("wrong password did not stay locked: locked=%v error=%q", s.appLocked(), s.lockUI.error)
	}
	typeField(hitAppLockPassword, changedPassword)
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
	typeField(hitAppLockFormFields.At(0), changedPassword)
	click(hitAppLockFormSubmit)
	waitOperation()
	if s.appLockEnabled() || s.modal != modalNone {
		t.Fatalf("disable did not finish: enabled=%v error=%q", s.appLockEnabled(), s.lockUI.error)
	}
	if change, ok := hitByIDSnapshot(view.Snapshot().Hits, hitAppLockChange); !ok || !change.Disabled {
		t.Fatal("change-password control did not return to disabled state")
	}
}
