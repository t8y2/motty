package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/term"
)

func TestShortcutCaptureRejectsConflictsAndPersistsTypedBinding(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	showShortcutSettings(state)
	state.Click(hitShortcutRowList.At(int(shortcutFind - 1)))
	if state.modal != modalShortcutCapture || state.shortcutCapture != shortcutFind {
		t.Fatalf("shortcut row did not open capture: modal=%d action=%d", state.modal, state.shortcutCapture)
	}
	state.clickShortcutCapture(hitShortcutCaptureCancel)

	state.openShortcutCapture(shortcutFind)
	palette := state.shortcuts.chords[shortcutCommandPalette]
	result := state.Shortcut(ui.KeyEvent{Modifiers: palette.Modifiers, Text: string([]byte{palette.Key})})
	if !result.Handled || !result.Repaint || state.modal != modalShortcutCapture || state.shortcutError == "" {
		t.Fatalf("conflict result=%+v modal=%d error=%q", result, state.modal, state.shortcutError)
	}
	if state.shortcuts.chords[shortcutFind] == palette {
		t.Fatal("conflicting shortcut replaced the find binding")
	}

	custom := shortcutChord{Modifiers: defaultShortcutModifier() | ui.ModifierAlt, Key: 'x'}
	result = state.Shortcut(ui.KeyEvent{Modifiers: custom.Modifiers, Text: "x"})
	if !result.Handled || state.modal != modalNone || state.shortcuts.chords[shortcutFind] != custom {
		t.Fatalf("custom result=%+v modal=%d chord=%+v", result, state.modal, state.shortcuts.chords[shortcutFind])
	}

	restored := newTestState(t, options)
	if restored.shortcuts.chords[shortcutFind] != custom || restored.activeSection != sectionSettings || restored.settingsCategory != settingsShortcuts {
		t.Fatalf("restored chord=%+v section=%d", restored.shortcuts.chords[shortcutFind], restored.activeSection)
	}
	if got := restored.Shortcut(ui.KeyEvent{Modifiers: defaultShortcutModifier(), Text: "f"}); got.Handled {
		t.Fatal("old find binding remained active")
	}
	if got := restored.Shortcut(ui.KeyEvent{Modifiers: custom.Modifiers, Text: "x"}); !got.Handled {
		t.Fatal("custom find binding was not active")
	}
}

func TestShortcutCanBeDeletedAndStaysDeletedAcrossRestarts(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	showShortcutSettings(state)
	state.Click(hitShortcutRowList.At(int(shortcutFind - 1)))
	if state.modal != modalShortcutCapture || state.shortcutCapture != shortcutFind {
		t.Fatalf("shortcut row did not open capture: modal=%d action=%d", state.modal, state.shortcutCapture)
	}
	state.Click(hitShortcutCaptureDelete)
	if state.modal != modalNone || state.shortcutCapture != shortcutNone || state.shortcutError != "" {
		t.Fatalf("delete left modal=%d action=%d error=%q", state.modal, state.shortcutCapture, state.shortcutError)
	}
	if state.shortcuts.chords[shortcutFind].valid() {
		t.Fatalf("the deleted shortcut is still bound: %+v", state.shortcuts.chords[shortcutFind])
	}
	if label := state.shortcuts.labels[shortcutFind]; label != "—" {
		t.Fatalf("the deleted shortcut reads %q, want —", label)
	}

	restored := newTestState(t, options)
	if restored.shortcuts.chords[shortcutFind].valid() {
		t.Fatalf("the deleted shortcut came back: %+v", restored.shortcuts.chords[shortcutFind])
	}
	if got := restored.Shortcut(ui.KeyEvent{Modifiers: defaultShortcutModifier(), Text: "f"}); got.Handled {
		t.Fatal("the deleted find chord still runs")
	}
	if got, want := restored.shortcuts.chords[shortcutNewTab], defaultShortcuts().chords[shortcutNewTab]; got != want {
		t.Fatalf("an untouched shortcut = %+v, want its default %+v", got, want)
	}
}

func TestDeleteOrBackspaceRemovesTheShortcutBeingCaptured(t *testing.T) {
	for _, key := range []ui.Key{ui.KeyDelete, ui.KeyBackspace} {
		state := newTestState(t, freshOptions(t))
		state.openShortcutCapture(shortcutFind)
		result := state.Key(ui.KeyEvent{Key: key})
		if !result.Handled || state.modal != modalNone || state.shortcuts.chords[shortcutFind].valid() {
			t.Fatalf("key %v result=%+v modal=%d chord=%+v", key, result, state.modal, state.shortcuts.chords[shortcutFind])
		}
	}
}

func TestRestoreKeepsAnExplicitlyDeletedShortcutUnbound(t *testing.T) {
	primary := defaultShortcutModifier()
	restored := restoreShortcuts([]savedShortcut{
		{Action: "find", Key: ""},
		{Action: "copy", Key: "c", Modifiers: uint8(primary)},
	})
	if restored.chords[shortcutFind].valid() {
		t.Fatalf("an explicitly deleted shortcut took its default back: %+v", restored.chords[shortcutFind])
	}
	if got, want := restored.chords[shortcutCopy], (shortcutChord{Modifiers: primary, Key: 'c'}); got != want {
		t.Fatalf("saved copy shortcut = %+v, want %+v", got, want)
	}
	if got, want := restored.chords[shortcutNewTab], (shortcutChord{Modifiers: primary, Key: 't'}); got != want {
		t.Fatalf("an action the file did not mention = %+v, want its default %+v", got, want)
	}
}

func TestShortcutFilesPathHasPrimaryLDefaultAndPersistsCustomBinding(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	wantDefault := shortcutChord{Modifiers: defaultShortcutModifier(), Key: 'l'}
	if state.shortcuts.chords[shortcutFilesPath] != wantDefault {
		t.Fatalf("default files path shortcut=%+v, want %+v", state.shortcuts.chords[shortcutFilesPath], wantDefault)
	}
	custom := shortcutChord{Modifiers: defaultShortcutModifier() | ui.ModifierAlt, Key: 'l'}
	if conflict, ok := state.shortcuts.bind(shortcutFilesPath, custom); !ok || conflict != shortcutNone {
		t.Fatalf("bind custom files path shortcut conflict=%v ok=%v", conflict, ok)
	}
	state.save()
	restored := newTestState(t, options)
	if restored.shortcuts.chords[shortcutFilesPath] != custom || shortcutActionFromString("files_path") != shortcutFilesPath {
		t.Fatalf("restored files path shortcut=%+v action=%v", restored.shortcuts.chords[shortcutFilesPath], shortcutActionFromString("files_path"))
	}
}

func TestShortcutFilesPathLeavesTerminalCtrlLAloneWithoutAVisibleFilePanel(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	shortcut := state.shortcuts.chords[shortcutFilesPath]
	event := ui.KeyEvent{Target: state.activeTerminalFocusHit(), Modifiers: shortcut.Modifiers, Text: string(shortcut.Key)}
	if result := state.Shortcut(event); result.Handled {
		t.Fatalf("Ctrl/Cmd+L without Files was consumed: %+v", result)
	}

	state.Click(hitSFTPOpen)
	if result := state.Shortcut(event); result.Handled || session.files.sftpFolder.pathEditing {
		t.Fatalf("Ctrl/Cmd+L with terminal focused and visible Files result=%+v editing=%v", result, session.files.sftpFolder.pathEditing)
	}
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	context.Open = false
	if err := state.deck.SetContext(state.deck.ActiveWorkspace(), context); err != nil {
		t.Fatal(err)
	}
	if session.files == nil {
		t.Fatal("test needs retained file state behind a hidden panel")
	}
	if result := state.Shortcut(event); result.Handled || session.files.sftpFolder.pathEditing {
		t.Fatalf("Ctrl/Cmd+L with hidden Files result=%+v editing=%v", result, session.files.sftpFolder.pathEditing)
	}
}

func TestClearScreenShortcutEmptiesTheWindowAndKeepsThePromptLine(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	shell.say(strings.Repeat("old output\r\n", 200))
	shell.say("prompt$ ")
	state.Wake()
	screen := session.term.screen
	if screen.Scrollback() == 0 {
		t.Fatal("the test needs history to clear")
	}
	cols, rows := screen.Size()
	session.term.selection.selectRange(screen.PointAt(term.Position{}), screen.PointAt(term.Position{Row: rows - 1, Column: cols - 1}))

	shortcut := state.shortcuts.chords[shortcutClearScreen]
	want := shortcutChord{Modifiers: defaultShortcutModifier(), Key: 'k'}
	if shortcut != want {
		t.Fatalf("default clear screen shortcut = %+v, want %+v", shortcut, want)
	}
	result := state.Shortcut(ui.KeyEvent{Target: state.activeTerminalFocusHit(), Modifiers: shortcut.Modifiers, Text: string(shortcut.Key)})
	if !result.Handled {
		t.Fatalf("the clear screen chord was not handled: %+v", result)
	}
	if screen.Scrollback() != 0 {
		t.Fatalf("scrollback = %d after the clear", screen.Scrollback())
	}
	if got := screen.RowText(0); !strings.Contains(got, "prompt$") {
		t.Fatalf("first row = %q, want the prompt line", got)
	}
	for row := 1; row < rows; row++ {
		if got := screen.RowText(row); got != "" {
			t.Fatalf("row %d = %q, want it cleared", row, got)
		}
	}
	if cursor := screen.Cursor(); cursor.Y != 0 {
		t.Fatalf("cursor = %+v, want it on the first row", cursor)
	}
	if session.term.selection.selected {
		t.Fatal("the selection outlived the rows it was pointing at")
	}
}

func TestTheClearScreenShortcutIsCustomisableAndPersists(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	custom := shortcutChord{Modifiers: defaultShortcutModifier() | ui.ModifierShift, Key: 'k'}
	if conflict, ok := state.shortcuts.bind(shortcutClearScreen, custom); !ok || conflict != shortcutNone {
		t.Fatalf("bind custom clear screen shortcut conflict=%v ok=%v", conflict, ok)
	}
	state.save()
	restored := newTestState(t, options)
	if restored.shortcuts.chords[shortcutClearScreen] != custom {
		t.Fatalf("restored clear screen shortcut = %+v, want %+v", restored.shortcuts.chords[shortcutClearScreen], custom)
	}
	if action := restored.shortcuts.actionFor(custom); action != shortcutClearScreen {
		t.Fatalf("the custom chord belongs to %v, want clear screen", action)
	}
}

func TestASavedShortcutOutranksADefaultAddedByALaterBuild(t *testing.T) {
	primary := defaultShortcutModifier()
	restored := restoreShortcuts([]savedShortcut{{Action: "copy", Key: "k", Modifiers: uint8(primary)}})
	if got, want := restored.chords[shortcutCopy], (shortcutChord{Modifiers: primary, Key: 'k'}); got != want {
		t.Fatalf("saved copy shortcut = %+v, want %+v", got, want)
	}
	// Clear screen's default would take that chord; the saved binding keeps it,
	// and the new action reads "—" until the user gives it one.
	if restored.chords[shortcutClearScreen].valid() {
		t.Fatalf("clear screen took the saved chord: %+v", restored.chords[shortcutClearScreen])
	}
	if action := restored.actionFor(shortcutChord{Modifiers: primary, Key: 'k'}); action != shortcutCopy {
		t.Fatalf("primary+K belongs to %v, want copy", action)
	}
	if got, want := restored.chords[shortcutNewTab], (shortcutChord{Modifiers: primary, Key: 't'}); got != want {
		t.Fatalf("an action the file did not mention = %+v, want its default %+v", got, want)
	}
}

func TestShortcutSettingsSteadyPaintDoesNotAllocate(t *testing.T) {
	state := newTestState(t, testOptions(t))
	showShortcutSettings(state)
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("shortcut settings paint allocations = %v", allocations)
	}
}

func BenchmarkPaintShortcutSettings(b *testing.B) {
	state := newTestState(b, testOptions(b))
	showShortcutSettings(state)
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	b.ReportAllocs()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

// showShortcutSettings opens the Keyboard shortcuts category of the settings page.
func showShortcutSettings(state *State) {
	state.activeSection = sectionSettings
	state.settingsCategory = settingsShortcuts
}
