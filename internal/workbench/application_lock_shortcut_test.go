package workbench

import (
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

func TestAppLockShortcutDefaultsAndKeepsTerminalControlL(t *testing.T) {
	s, _, _, shell := liveSession(t)
	chord := keymap.Chord{Modifiers: keymap.Primary | gfx.ModifierShift, Char: 'l'}
	if got := s.shortcuts.chords.Chord(shortcutAppLock); got != chord || s.shortcuts.actionFor(chord) != shortcutAppLock {
		t.Fatalf("lock shortcut = %+v, want %+v", got, chord)
	}
	target := terminalTestHit(s, &s.sessions[0])
	event := gfx.KeyEvent{Target: target, Text: "l", Modifiers: chord.Modifiers}
	repeat, release := event, event
	repeat.Repeat = true
	release.Release = true
	for _, stroke := range []gfx.KeyEvent{event, repeat, release} {
		if result := s.Shortcut(stroke); result.Handled || s.appLocked() {
			t.Fatalf("ordinary profile consumed the lock shortcut: %+v", stroke)
		}
	}
	checkControlL := func() {
		t.Helper()
		shell.input = nil
		// A printable Control chord arrives as KeyUnknown with its letter in Text.
		ctrlL := gfx.KeyEvent{Target: target, Text: "l", Modifiers: gfx.ModifierControl}
		if result := s.Shortcut(ctrlL); result.Handled {
			t.Fatal("Ctrl+L was claimed by the application")
		}
		s.Key(ctrlL)
		if got := shell.typed(); got != "\x0c" || s.appLocked() {
			t.Fatalf("terminal Ctrl+L produced %q", got)
		}
	}
	checkControlL()
	if err := finishProtectionAction(t, s, appLockEnable, "", "test master password"); err != nil {
		t.Fatal(err)
	}
	checkControlL()
	shell.input = nil
	if result := s.Shortcut(event); !result.Handled || !s.appLocked() || len(shell.input) != 0 {
		t.Fatal("default lock shortcut was not isolated from the shell")
	}
	event.Repeat = true
	if result := s.Shortcut(event); !result.Handled || !s.appLocked() || len(shell.input) != 0 {
		t.Fatal("held lock shortcut reached the locked session")
	}
}

func TestAppLockShortcutCapturePersistsAndLocksAllWindows(t *testing.T) {
	opts := testOptions(t)
	opts.Group = NewGroup()
	first := newTestState(t, opts)
	if first.shortcutActionLabel(shortcutAppLock) != first.messages.AppLock.Lock {
		t.Fatal("settings row has no lock-screen label")
	}
	showShortcutSettings(first)
	first.Click(hitShortcutRowList.At(int(shortcutAppLock - 1)))
	if first.modal != modalShortcutCapture || first.shortcutCapture != shortcutAppLock {
		t.Fatal("lock shortcut settings row did not open capture")
	}
	pressChord(first, first.shortcuts.chords.Chord(shortcutCommandPalette))
	if first.modal != modalShortcutCapture || first.shortcutError == "" {
		t.Fatal("lock shortcut capture accepted a conflicting action")
	}
	custom := gfx.KeyEvent{Key: gfx.KeyF8}
	first.Key(custom)
	if first.modal != modalNone || first.shortcuts.chords.Chord(shortcutAppLock) != (keymap.Chord{Key: gfx.KeyF8}) {
		t.Fatal("named key was not captured")
	}
	opts.WindowID = 2
	second := newTestState(t, opts)
	if second.shortcuts.chords.Chord(shortcutAppLock) != (keymap.Chord{Key: gfx.KeyF8}) {
		t.Fatal("new window lost the saved lock shortcut")
	}
	if err := finishProtectionAction(t, first, appLockEnable, "", "test master password"); err != nil {
		t.Fatal(err)
	}
	if result := pressChord(second, defaultShortcuts().chords.Chord(shortcutAppLock)); result.Handled || first.appLocked() {
		t.Fatal("old default survived remapping")
	}
	if result := second.Key(custom); !result.Handled || !first.appLocked() || !second.appLocked() {
		t.Fatal("named shortcut did not lock both windows")
	}
	opts.WindowID = 3
	if third := newTestState(t, opts); !third.appLocked() || !third.profileDeferred {
		t.Fatal("window opened after shortcut inherited an unlocked profile")
	}
}

func TestAppLockShortcutUpgradeKeepsExistingActionAndSnippet(t *testing.T) {
	chord := keymap.Chord{Modifiers: keymap.Primary | gfx.ModifierShift, Char: 'l'}
	for _, snippetOwns := range []bool{false, true} {
		name := "action"
		if snippetOwns {
			name = "snippet"
		}
		t.Run(name, func(t *testing.T) {
			opts := freshOptions(t)
			opts.StatePath = filepath.Join(filepath.Dir(opts.KnownHostsPath), "config.db")
			saved := savedState{HostCatalogVersion: hostCatalogSchemaVersion, ShortcutVersion: shortcutSchemaVersion}
			if snippetOwns {
				saved.Snippets = []savedSnippet{{Name: "status", Command: "pwd"}}
				saved.SnippetShortcuts = []savedSnippetKey{{Name: "status", Key: "l", Modifiers: uint8(chord.Modifiers)}}
			} else {
				saved.Shortcuts = []savedShortcut{{Action: shortcutFind.String(), Key: "l", Modifiers: uint8(chord.Modifiers)}}
			}
			saveReleaseState(t, opts.StatePath, saved)
			s := newTestState(t, opts)
			if s.shortcuts.chords.Chord(shortcutAppLock).Valid() {
				t.Fatal("new default claimed an existing user shortcut")
			}
			if snippetOwns {
				if s.snippetNamed(chord) != "status" || s.shortcuts.actionFor(chord) != shortcutNone {
					t.Fatal("saved snippet shortcut lost after upgrade")
				}
			} else if s.shortcuts.actionFor(chord) != shortcutFind {
				t.Fatal("saved application action lost after upgrade")
			}
		})
	}
}

func TestAppLockShortcutRespectsRemoteKeyboardCapture(t *testing.T) {
	s, session := fileDesktopState(t, newFileDesktop(), false, Options{})
	if err := finishProtectionAction(t, s, appLockEnable, "", "test master password"); err != nil {
		t.Fatal(err)
	}
	event := gfx.KeyEvent{Target: s.activeTerminalFocusHit(), Text: "l", Modifiers: keymap.Primary | gfx.ModifierShift}
	if session.desk.released {
		t.Fatal("remote desktop does not own keyboard")
	}
	if result := s.Shortcut(event); result.Handled || s.appLocked() {
		t.Fatal("remote keyboard shortcut locked the local profile")
	}
	s.toggleCapture(session.desk)
	if result := s.Shortcut(event); !result.Handled || !s.appLocked() {
		t.Fatal("released keyboard did not lock the profile")
	}
}

func TestAppLockShortcutInDocumentWindow(t *testing.T) {
	host := &documentTestHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() { host.Quit(); app.UseApplicationRunner(nil) })
	if err := app.RunApplication(func(application *app.Application) error {
		opts := testOptions(t)
		opts.OpenContentWindow = application.OpenContentWindow
		opts.Group = NewGroup()
		s := newTestState(t, opts)
		opts.WindowID = 2
		other := newTestState(t, opts)
		s.notes.list = []savedNote{{ID: "note", Title: "Note", Body: "note text"}}
		s.openNoteWindow("note")
		if len(host.windows) != 1 {
			t.Fatal("note editor did not open")
		}
		editor := host.windows[0]
		event := gfx.KeyEvent{Text: "l", Modifiers: keymap.Primary | gfx.ModifierShift}
		if result := editor.config.OnShortcut(event); result.Handled || s.appLocked() {
			t.Fatal("unprotected editor captured the lock shortcut")
		}
		if result := editor.config.OnShortcut(gfx.KeyEvent{Modifiers: keymap.Primary, Text: "s"}); !result.Handled {
			t.Fatal("unprotected editor lost its own Save shortcut")
		}
		const password = "test master password"
		if err := finishProtectionAction(t, s, appLockEnable, "", password); err != nil {
			return err
		}
		content := s.fileWindows.window
		if content == nil || content.Closed() {
			t.Fatal("note editor disappeared during enablement")
		}
		if result := editor.config.OnShortcut(event); !result.Handled || !s.appLocked() || !other.appLocked() || editor.done {
			t.Fatal("editor shortcut did not lock both windows and preserve the document")
		}
		shows := editor.shows
		content.Show()
		if editor.shows != shows {
			t.Fatal("concealed editor was shown while the application was locked")
		}
		if err := finishProtectionAction(t, s, appLockUnlock, password, ""); err != nil {
			return err
		}
		if s.fileWindows.window != content || editor.shows <= shows {
			t.Fatal("unlock did not restore the same visible editor")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
