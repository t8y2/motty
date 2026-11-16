//go:build darwin || windows || linux

package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/keymap"
)

func TestHeadlessTabNumberShortcutHasASettingsRow(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	showShortcutSettings(state)
	session, err := headless.Open(app.Config{
		Title: "shortcuts", Width: 1400, Height: 900,
		Paint: state.Paint, OnPointer: state.Pointer,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	name, chord := state.messages.ShortcutTabNumber, state.shortcuts.labels[shortcutTabNumber]
	var foundName, foundChord bool
	for _, text := range session.Snapshot().Texts {
		foundName = foundName || text.Value == name
		foundChord = foundChord || text.Value == chord
	}
	if !foundName || !foundChord {
		t.Fatalf("settings page shows name=%v chord=%v, want %q and %q", foundName, foundChord, name, chord)
	}
}

func TestHeadlessShortcutCaptureClickAndChord(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	showShortcutSettings(state)
	session, err := headless.Open(app.Config{
		Title: "shortcuts", Width: 1400, Height: 900,
		Paint: state.Paint, OnPointer: state.Pointer,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	label := state.shortcuts.labels[shortcutCommandPalette]
	if err := session.ClickLabel(label); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalShortcutCapture || state.shortcutCapture != shortcutCommandPalette {
		t.Fatalf("shortcut click left modal=%d action=%d", state.modal, state.shortcutCapture)
	}

	if err := session.Keys("cmd+alt+x"); err != nil {
		t.Fatal(err)
	}
	want := keymap.Chord{Modifiers: gfx.ModifierMeta | gfx.ModifierAlt, Char: 'x'}
	if state.shortcuts.chords.Chord(shortcutCommandPalette) != want || state.modal != modalNone {
		t.Fatalf("binding=%+v modal=%d, want %+v and closed", state.shortcuts.chords.Chord(shortcutCommandPalette), state.modal, want)
	}
}

func TestHeadlessFlexibleShortcutsRecordAndTriggerThroughKeyboard(t *testing.T) {
	for _, keys := range []string{"alt+j", "shift+enter", "f8", "x", "shift+x", "/", "space", "backspace", "delete"} {
		t.Run(keys, func(t *testing.T) {
			state := newTestState(t, freshOptions(t))
			showShortcutSettings(state)
			session, err := headless.Open(app.Config{
				Title: "shortcuts", Width: 1400, Height: 900,
				Paint: state.Paint, OnPointer: state.Pointer, TextInput: state.TerminalInput,
				OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
			}, nil, headless.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if err := session.ClickLabel(state.shortcuts.labels[shortcutCommandPalette]); err != nil {
				t.Fatal(err)
			}
			if err := session.Keys(keys); err != nil {
				t.Fatal(err)
			}
			if state.modal != modalNone {
				t.Fatalf("recording did not finish: modal=%d error=%q", state.modal, state.shortcutError)
			}
			if err := session.Keys(keys); err != nil {
				t.Fatal(err)
			}
			if state.modal != modalCommandPalette {
				t.Fatalf("binding did not trigger the command palette: modal=%d", state.modal)
			}
		})
	}
}

func TestHeadlessFlexibleShortcutLeavesUnboundKeysWithARealLocalShell(t *testing.T) {
	state, local := openLocal(t)
	if _, ok := state.shortcuts.bind(shortcutZoomIn, keymap.Chord{Char: 'x'}); !ok {
		t.Fatal("could not bind X")
	}
	view, err := headless.Open(app.Config{
		Width: 1400, Height: 900, Paint: state.Paint, TextInput: state.TerminalInput,
		OnKey: state.Key, OnShortcut: state.Shortcut,
		OnPointer: state.Pointer, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	before := state.terminalFontSize
	if err := view.Keys("x"); err != nil {
		t.Fatal(err)
	}
	if state.terminalFontSize != before+1 {
		t.Fatal("the plain X binding did not trigger")
	}
	// If X leaked to the real shell this becomes xprintf and never prints the marker.
	for _, key := range "printf 'shortcut-%s\\n' ok" {
		name := string(key)
		if key == ' ' {
			name = "space"
		}
		if err := view.Keys(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := view.Keys("enter"); err != nil {
		t.Fatal(err)
	}
	localShellOutput(t, state, local, "shortcut-ok")
}

// A bound action can be removed from its capture dialog, and the row then reads
// "—" instead of the chord.
func TestHeadlessShortcutCanBeDeletedFromTheCaptureDialog(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	showShortcutSettings(state)
	session, err := headless.Open(app.Config{
		Title: "shortcuts", Width: 1400, Height: 900,
		Paint: state.Paint, OnPointer: state.Pointer,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	if err := session.ClickLabel(state.shortcuts.labels[shortcutFind]); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalShortcutCapture || state.shortcutCapture != shortcutFind {
		t.Fatalf("shortcut click left modal=%d action=%d", state.modal, state.shortcutCapture)
	}
	if err := session.ClickLabel(state.messages.ShortcutsDelete); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNone || state.shortcuts.chords.Chord(shortcutFind).Valid() {
		t.Fatalf("delete left modal=%d chord=%+v", state.modal, state.shortcuts.chords.Chord(shortcutFind))
	}
	if label := state.shortcuts.labels[shortcutFind]; label != "—" {
		t.Fatalf("the deleted shortcut reads %q, want —", label)
	}
	if err := session.Rest(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, text := range session.Snapshot().Texts {
		found = found || text.Value == "—"
	}
	if !found {
		t.Fatal("the deleted shortcut row does not show —")
	}
}

func TestHeadlessShortcutCaptureRestoresOnlyItsOwnDefault(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		name := "custom binding"
		if deleted {
			name = "deleted binding"
		}
		t.Run(name, func(t *testing.T) {
			options := freshOptions(t)
			state := newTestState(t, options)
			showShortcutSettings(state)
			state.shortcuts.bind(shortcutFind, keymap.Chord{Key: gfx.KeyF8})
			if deleted {
				state.shortcuts.unbind(shortcutFind)
			}
			other := keymap.Chord{Modifiers: gfx.ModifierAlt, Char: 'j'}
			state.shortcuts.bind(shortcutCompletion, other)
			state.save()
			view, err := headless.Open(app.Config{
				Title: "shortcuts", Width: 1060, Height: 700,
				Paint: state.Paint, OnPointer: state.Pointer,
				OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
			}, nil, headless.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			if err := view.ClickLabel(state.shortcuts.labels[shortcutFind]); err != nil {
				t.Fatal(err)
			}
			var reset gfx.Rect
			for _, hit := range view.Snapshot().Hits {
				if hit.ID == hitShortcutCaptureReset {
					reset = hit.Bounds
				}
			}
			if reset.Width == 0 {
				t.Fatal("the capture dialog has no restore-default button")
			}
			if err := view.Click(reset.X+reset.Width/2, reset.Y+reset.Height/2); err != nil {
				t.Fatal(err)
			}
			want := defaultShortcuts().chords.Chord(shortcutFind)
			if state.modal != modalNone || state.shortcuts.chords.Chord(shortcutFind) != want || state.shortcutError != "" {
				t.Fatalf("reset left modal=%d chord=%+v error=%q", state.modal, state.shortcuts.chords.Chord(shortcutFind), state.shortcutError)
			}
			restored := newTestState(t, options)
			if restored.shortcuts.chords.Chord(shortcutFind) != want || restored.shortcuts.chords.Chord(shortcutCompletion) != other {
				t.Fatalf("restored find=%+v completion=%+v", restored.shortcuts.chords.Chord(shortcutFind), restored.shortcuts.chords.Chord(shortcutCompletion))
			}
		})
	}
}

// The backdrop and the close button are different controls: hovering the area
// outside the dialog must not paint the close button as hovered.
func TestHeadlessShortcutCaptureCloseHoversOnlyOnItself(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	showShortcutSettings(state)
	session, err := headless.Open(app.Config{
		Title: "shortcuts", Width: 1400, Height: 900,
		Paint: state.Paint, OnPointer: state.Pointer,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	if err := session.ClickLabel(state.shortcuts.labels[shortcutFind]); err != nil {
		t.Fatal(err)
	}
	var close gfx.Rect
	for _, hit := range session.Snapshot().Hits {
		if hit.ID == hitShortcutCaptureClose {
			close = hit.Bounds
		}
	}
	if close.Width == 0 {
		t.Fatal("the capture dialog has no close button")
	}
	if err := session.Move(20, 20); err != nil {
		t.Fatal(err)
	}
	if got := session.Snapshot().Hovered; got != hitShortcutCaptureDismiss {
		t.Fatalf("the pointer outside the dialog hovers %v, want the dismiss backdrop", got)
	}
	center := gfx.Point{X: close.X + close.Width/2, Y: close.Y + close.Height/2}
	if err := session.Move(center.X, center.Y); err != nil {
		t.Fatal(err)
	}
	if got := session.Snapshot().Hovered; got != hitShortcutCaptureClose {
		t.Fatalf("the pointer on the close button hovers %v, want the close button", got)
	}
	if err := session.Click(center.X, center.Y); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNone || state.shortcutCapture != shortcutNone {
		t.Fatalf("close left modal=%d action=%d", state.modal, state.shortcutCapture)
	}
}

// Reset defaults is a page-wide action in the header, above the rows, and it
// still restores a shortcut the user removed.
func TestHeadlessShortcutResetLivesInThePageHeader(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	showShortcutSettings(state)
	state.openShortcutCapture(shortcutFind)
	state.clearShortcut(shortcutFind)
	session, err := headless.Open(app.Config{
		Title: "shortcuts", Width: 1400, Height: 900,
		Paint: state.Paint, OnPointer: state.Pointer,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	var reset, firstRow gfx.Rect
	for _, hit := range session.Snapshot().Hits {
		switch hit.ID {
		case hitShortcutReset:
			reset = hit.Bounds
		case hitShortcutRowList.At(0):
			firstRow = hit.Bounds
		}
	}
	if reset.Width == 0 || firstRow.Width == 0 {
		t.Fatalf("header reset=%+v first row=%+v, want both painted", reset, firstRow)
	}
	if reset.Y >= firstRow.Y {
		t.Fatalf("reset top %.1f is not above the first row top %.1f", reset.Y, firstRow.Y)
	}
	if err := session.ClickLabel(state.messages.ShortcutsReset); err != nil {
		t.Fatal(err)
	}
	if got, want := state.shortcuts.chords.Chord(shortcutFind), defaultShortcuts().chords.Chord(shortcutFind); got != want {
		t.Fatalf("after the header reset find = %+v, want %+v", got, want)
	}
}

// A shortcut row has no explanation, so its name is alone in the row and must sit
// in the middle of the row, level with the chord it names.
func TestHeadlessShortcutRowNameIsCenteredWithItsChord(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	showShortcutSettings(state)
	session, err := headless.Open(app.Config{
		Title: "shortcuts", Width: 1400, Height: 900,
		Paint: state.Paint, OnPointer: state.Pointer,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	texts := session.Snapshot().Texts
	centerY := func(value string) (float32, bool) {
		for _, text := range texts {
			if text.Value == value {
				return text.Bounds.Y + text.Bounds.Height/2, true
			}
		}
		return 0, false
	}
	name, chord := state.shortcutActionLabel(shortcutCommandPalette), state.shortcuts.labels[shortcutCommandPalette]
	nameY, ok := centerY(name)
	if !ok {
		t.Fatalf("shortcut name %q was not painted", name)
	}
	chordY, ok := centerY(chord)
	if !ok {
		t.Fatalf("shortcut chord %q was not painted", chord)
	}
	if difference := nameY - chordY; difference < -1 || difference > 1 {
		t.Fatalf("name center %.1f, chord center %.1f: the name is not centered in its row", nameY, chordY)
	}
}

// A dialog fades out after it is closed and the window then goes idle: no
// transition is left in flight, and the dialog no longer takes input.
func TestHeadlessClosedDialogLeavesAndTheWindowGoesIdle(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	showShortcutSettings(state)
	session, err := headless.Open(app.Config{
		Title: "shortcuts", Width: 1400, Height: 900,
		Paint: state.Paint, OnPointer: state.Pointer,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	if err := session.ClickLabel(state.shortcuts.labels[shortcutCommandPalette]); err != nil {
		t.Fatal(err)
	}
	if err := session.Rest(); err != nil {
		t.Fatal(err)
	}
	if session.Driver().Transitioning() {
		t.Fatal("an open dialog at rest still asks for frames")
	}
	if err := session.Keys("esc"); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNone {
		t.Fatalf("Escape left modal=%d", state.modal)
	}
	if !session.Driver().Transitioning() {
		t.Fatal("a closed dialog did not leave through a transition")
	}
	for _, hit := range session.Snapshot().Hits {
		if hit.Scope == dialogScope {
			t.Fatalf("the leaving dialog still declares %q", hit.Label)
		}
	}
	if err := session.Wait(time.Second); err != nil {
		t.Fatal(err)
	}
	if session.Driver().Transitioning() {
		t.Fatal("the window is still animating a second after the dialog closed")
	}
}

// The digit reaches the tabs through the real key path, not only State.Shortcut.
func TestHeadlessDigitShortcutSwitchesTabs(t *testing.T) {
	state, world, _, _ := liveSession(t)
	state.Click(hitHosts)
	state.Click(hitHostList.At(2))
	connect(t, state, world)
	first, second := state.sessions[0].ID, state.sessions[1].ID
	state.setTabNumbers(true)
	session, err := headless.Open(app.Config{
		Title: "tabs", Width: 1400, Height: 900,
		Paint: state.Paint, OnPointer: state.Pointer,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
		TextInput: state.TerminalInput,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	modifier := "ctrl"
	if keymap.Primary&gfx.ModifierMeta != 0 {
		modifier = "cmd"
	}
	if err := session.Keys(modifier + "+1"); err != nil {
		t.Fatal(err)
	}
	if state.activeSessionID != first {
		t.Fatal("the first digit did not select the first tab through the keyboard")
	}
	if err := session.Keys(modifier + "+2"); err != nil {
		t.Fatal(err)
	}
	if state.activeSessionID != second {
		t.Fatal("the second digit did not select the second tab through the keyboard")
	}
}

// The chord reaches the application through the real key path, not just
// State.Shortcut, and the window then shows the new local terminal.
func TestHeadlessLocalTerminalShortcutOpensItsTab(t *testing.T) {
	skipWithoutUnixShell(t)
	state := newTestState(t, freshOptions(t))
	state.options.Invalidate = func() {}
	session, err := headless.Open(app.Config{
		Title: "local terminal", Width: 1400, Height: 900,
		Paint: state.Paint, OnPointer: state.Pointer,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
		TextInput: state.TerminalInput,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	modifier := "ctrl"
	if keymap.Primary&gfx.ModifierMeta != 0 {
		modifier = "cmd"
	}
	if err := session.Keys(modifier + "+shift+t"); err != nil {
		t.Fatal(err)
	}
	opened, found := state.sessionByIDPointer(state.activeSessionID)
	if !found || !opened.isLocal() {
		t.Fatalf("session after the shortcut = %+v", opened)
	}
	t.Cleanup(func() {
		if opened.term != nil && opened.term.shell != nil {
			opened.term.shell.Close()
		}
	})
	if err := session.WaitFor(func(snapshot app.Snapshot) bool {
		for _, text := range snapshot.Texts {
			if text.Value == state.messages.LocalTerminalTitle {
				return true
			}
		}
		return false
	}, 3*time.Second); err != nil {
		t.Fatal(err)
	}
}
