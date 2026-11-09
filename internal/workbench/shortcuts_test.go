package workbench

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
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
	palette := state.shortcuts.chords.Chord(shortcutCommandPalette)
	result := state.Shortcut(gfx.KeyEvent{Modifiers: palette.Modifiers, Text: string([]byte{palette.Char})})
	if !result.Handled || !result.Repaint || state.modal != modalShortcutCapture || state.shortcutError == "" {
		t.Fatalf("conflict result=%+v modal=%d error=%q", result, state.modal, state.shortcutError)
	}
	if state.shortcuts.chords.Chord(shortcutFind) == palette {
		t.Fatal("conflicting shortcut replaced the find binding")
	}

	custom := keymap.Chord{Modifiers: keymap.Primary | gfx.ModifierAlt, Char: 'x'}
	result = state.Shortcut(gfx.KeyEvent{Modifiers: custom.Modifiers, Text: "x"})
	if !result.Handled || state.modal != modalNone || state.shortcuts.chords.Chord(shortcutFind) != custom {
		t.Fatalf("custom result=%+v modal=%d chord=%+v", result, state.modal, state.shortcuts.chords.Chord(shortcutFind))
	}

	restored := newTestState(t, options)
	if restored.shortcuts.chords.Chord(shortcutFind) != custom || restored.activeSection != sectionSettings || restored.settingsCategory != settingsShortcuts {
		t.Fatalf("restored chord=%+v section=%d", restored.shortcuts.chords.Chord(shortcutFind), restored.activeSection)
	}
	if got := restored.Shortcut(gfx.KeyEvent{Modifiers: keymap.Primary, Text: "f"}); got.Handled {
		t.Fatal("old find binding remained active")
	}
	if got := restored.Shortcut(gfx.KeyEvent{Modifiers: custom.Modifiers, Text: "x"}); !got.Handled {
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
	if state.shortcuts.chords.Chord(shortcutFind).Valid() {
		t.Fatalf("the deleted shortcut is still bound: %+v", state.shortcuts.chords.Chord(shortcutFind))
	}
	if label := state.shortcuts.labels[shortcutFind]; label != "—" {
		t.Fatalf("the deleted shortcut reads %q, want —", label)
	}

	restored := newTestState(t, options)
	if restored.shortcuts.chords.Chord(shortcutFind).Valid() {
		t.Fatalf("the deleted shortcut came back: %+v", restored.shortcuts.chords.Chord(shortcutFind))
	}
	if got := restored.Shortcut(gfx.KeyEvent{Modifiers: keymap.Primary, Text: "f"}); got.Handled {
		t.Fatal("the deleted find chord still runs")
	}
	if got, want := restored.shortcuts.chords.Chord(shortcutNewTab), defaultShortcuts().chords.Chord(shortcutNewTab); got != want {
		t.Fatalf("an untouched shortcut = %+v, want its default %+v", got, want)
	}
}

func TestDeleteOrBackspaceCanBeBoundAsShortcuts(t *testing.T) {
	for _, key := range []gfx.Key{gfx.KeyDelete, gfx.KeyBackspace} {
		state := newTestState(t, freshOptions(t))
		state.openShortcutCapture(shortcutFind)
		result := state.Key(gfx.KeyEvent{Key: key})
		if !result.Handled || state.modal != modalNone || state.shortcuts.chords.Chord(shortcutFind) != (keymap.Chord{Key: key}) {
			t.Fatalf("key %v result=%+v modal=%d chord=%+v", key, result, state.modal, state.shortcuts.chords.Chord(shortcutFind))
		}
	}
}

func TestFlexibleShortcutsCapturePersistAndKeepConflicts(t *testing.T) {
	for _, event := range []gfx.KeyEvent{
		{Text: "x"}, {Text: "J", Modifiers: gfx.ModifierShift},
		{Text: "j", Modifiers: gfx.ModifierAlt}, {Text: "/"}, {Text: "+"},
		{Text: ",", Modifiers: gfx.ModifierControl}, {Key: gfx.KeySpace},
		{Key: gfx.KeyEnter, Modifiers: gfx.ModifierShift}, {Key: gfx.KeyUp, Modifiers: gfx.ModifierAlt},
		{Key: gfx.KeyF1}, {Key: gfx.KeyF12}, {Key: gfx.KeyEscape, Modifiers: gfx.ModifierAlt},
	} {
		chord, ok := keymap.FromEvent(event)
		if !ok {
			t.Fatalf("event is not a chord: %+v", event)
		}
		t.Run(chordLabel(chord), func(t *testing.T) {
			options := freshOptions(t)
			state := newTestState(t, options)
			state.openShortcutCapture(shortcutCommandPalette)
			result := state.Key(event)
			if !result.Handled || state.modal != modalNone || state.shortcuts.chords.Chord(shortcutCommandPalette) != chord {
				t.Fatalf("capture result=%+v modal=%d chord=%+v", result, state.modal, state.shortcuts.chords.Chord(shortcutCommandPalette))
			}
			restored := newTestState(t, options)
			if restored.shortcuts.chords.Chord(shortcutCommandPalette) != chord {
				t.Fatalf("restored chord=%+v, want %+v", restored.shortcuts.chords.Chord(shortcutCommandPalette), chord)
			}
			restored.openShortcutCapture(shortcutFind)
			restored.Key(event)
			if restored.modal != modalShortcutCapture || restored.shortcutError == "" {
				t.Fatal("duplicate binding was accepted")
			}
			restored.clickShortcutCapture(hitShortcutCaptureCancel)
			if result := restored.Key(event); !result.Handled || restored.modal != modalCommandPalette {
				t.Fatalf("trigger result=%+v modal=%d", result, restored.modal)
			}
		})
	}
}

func TestFlexibleShortcutsLeaveTextEditingAndEscCancellationAvailable(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.openShortcutCapture(shortcutFind)
	before := state.shortcuts.chords.Chord(shortcutFind)
	state.Key(gfx.KeyEvent{Key: gfx.KeyEscape})
	if state.modal != modalNone || state.shortcuts.chords.Chord(shortcutFind) != before {
		t.Fatal("Escape did not cancel recording")
	}
	state.shortcuts.bind(shortcutCommandPalette, keymap.Chord{Char: 'x'})
	if result := state.Shortcut(gfx.KeyEvent{Target: hitSearch, Text: "x"}); result.Handled || state.modal != modalNone {
		t.Fatal("a plain shortcut stole input from the host search field")
	}
	if chord := (keymap.Chord{Char: 'x', Modifiers: gfx.ModifierAlt}); validForSnippet(chord) {
		t.Fatal("application shortcuts unexpectedly changed snippet validation")
	}
}

func TestFlexibleShortcutLeavesEmbeddedFileFieldsEditable(t *testing.T) {
	state, local := openLocal(t)
	manager := state.newSFTPWindow(local)
	state.sftpTabs = []*sftpWindow{manager}
	state.activeSFTPTab, state.activeSection = local.ID, sectionSFTP
	state.shortcuts.bind(shortcutCommandPalette, keymap.Chord{Char: 'x'})
	manager.view.lfolder.filterOpen = true
	for _, id := range []gfx.ID{sftpWindowPath[0], sftpWindowFilter[0]} {
		if !state.shortcutTextInput(id) {
			t.Fatalf("file field %v was not recognized as native input", id)
		}
		if result := state.Shortcut(gfx.KeyEvent{Text: "x", Target: id}); result.Handled || state.modal != modalNone {
			t.Fatalf("typing in file field %v ran a shortcut: %+v modal=%d", id, result, state.modal)
		}
	}
}

func TestNamedFindBindingFocusesRemoteSearch(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.modal = modalRemoteTools
	for _, chord := range []keymap.Chord{{Key: gfx.KeyF8}, {Modifiers: gfx.ModifierAlt, Char: 'j'}} {
		state.shortcuts.bind(shortcutFind, chord)
		event := gfx.KeyEvent{Key: chord.Key, Modifiers: chord.Modifiers}
		if chord.Char != 0 {
			event.Text = string(chord.Char)
		}
		if result := state.Key(event); !result.Handled || result.Focus != hitRemoteSearch {
			t.Fatalf("Find did not focus remote search: chord=%+v result=%+v", chord, result)
		}
	}
}

func TestSingleShortcutResetKeepsBindingsWhenItsDefaultIsTaken(t *testing.T) {
	for _, snippetOwner := range []bool{false, true} {
		state := newTestState(t, freshOptions(t))
		custom := keymap.Chord{Key: gfx.KeyF8}
		defaultFind := state.shortcuts.chords.Chord(shortcutFind)
		state.shortcuts.bind(shortcutFind, custom)
		owner := state.shortcutActionLabel(shortcutCompletion)
		if snippetOwner {
			owner = "Snippet"
			state.snippets.list = []snippet{{Name: owner, Command: "pwd", Shortcut: defaultFind}}
		} else {
			state.shortcuts.bind(shortcutCompletion, defaultFind)
		}
		before := state.shortcuts.chords.Clone()
		state.openShortcutCapture(shortcutFind)
		state.Click(hitShortcutCaptureReset)
		if state.modal != modalShortcutCapture || !strings.Contains(state.shortcutError, owner) || !state.shortcuts.chords.Equal(before) {
			t.Fatalf("reset changed a conflicting binding: modal=%d error=%q", state.modal, state.shortcutError)
		}
		if snippetOwner && state.snippets.list[0].Shortcut != defaultFind {
			t.Fatal("reset removed a snippet's binding")
		}
	}
}

func TestRestoreKeepsAnExplicitlyDeletedShortcutUnbound(t *testing.T) {
	primary := keymap.Primary
	restored := restoreShortcuts([]savedShortcut{
		{Action: "find", Key: ""},
		{Action: "copy", Key: "c", Modifiers: uint8(primary)},
	})
	if restored.chords.Chord(shortcutFind).Valid() {
		t.Fatalf("an explicitly deleted shortcut took its default back: %+v", restored.chords.Chord(shortcutFind))
	}
	if got, want := restored.chords.Chord(shortcutCopy), (keymap.Chord{Modifiers: primary, Char: 'c'}); got != want {
		t.Fatalf("saved copy shortcut = %+v, want %+v", got, want)
	}
	if got, want := restored.chords.Chord(shortcutNewTab), (keymap.Chord{Modifiers: primary, Char: 't'}); got != want {
		t.Fatalf("an action the file did not mention = %+v, want its default %+v", got, want)
	}
}

func TestShortcutFilesPathHasPrimaryLDefaultAndPersistsCustomBinding(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	wantDefault := keymap.Chord{Modifiers: keymap.Primary, Char: 'l'}
	if state.shortcuts.chords.Chord(shortcutFilesPath) != wantDefault {
		t.Fatalf("default files path shortcut=%+v, want %+v", state.shortcuts.chords.Chord(shortcutFilesPath), wantDefault)
	}
	custom := keymap.Chord{Modifiers: keymap.Primary | gfx.ModifierAlt, Char: 'l'}
	if conflict, ok := state.shortcuts.bind(shortcutFilesPath, custom); !ok || conflict != shortcutNone {
		t.Fatalf("bind custom files path shortcut conflict=%v ok=%v", conflict, ok)
	}
	state.save()
	restored := newTestState(t, options)
	if restored.shortcuts.chords.Chord(shortcutFilesPath) != custom || shortcutActionFromString("files_path") != shortcutFilesPath {
		t.Fatalf("restored files path shortcut=%+v action=%v", restored.shortcuts.chords.Chord(shortcutFilesPath), shortcutActionFromString("files_path"))
	}
}

func TestShortcutFilesPathLeavesTerminalCtrlLAloneWithoutAVisibleFilePanel(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	shortcut := state.shortcuts.chords.Chord(shortcutFilesPath)
	event := gfx.KeyEvent{Target: state.activeTerminalFocusHit(), Modifiers: shortcut.Modifiers, Text: string(shortcut.Char)}
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

	shortcut := state.shortcuts.chords.Chord(shortcutClearScreen)
	want := keymap.Chord{Modifiers: keymap.Primary, Char: 'k'}
	if shortcut != want {
		t.Fatalf("default clear screen shortcut = %+v, want %+v", shortcut, want)
	}
	result := state.Shortcut(gfx.KeyEvent{Target: state.activeTerminalFocusHit(), Modifiers: shortcut.Modifiers, Text: string(shortcut.Char)})
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
	custom := keymap.Chord{Modifiers: keymap.Primary | gfx.ModifierShift, Char: 'k'}
	if conflict, ok := state.shortcuts.bind(shortcutClearScreen, custom); !ok || conflict != shortcutNone {
		t.Fatalf("bind custom clear screen shortcut conflict=%v ok=%v", conflict, ok)
	}
	state.save()
	restored := newTestState(t, options)
	if restored.shortcuts.chords.Chord(shortcutClearScreen) != custom {
		t.Fatalf("restored clear screen shortcut = %+v, want %+v", restored.shortcuts.chords.Chord(shortcutClearScreen), custom)
	}
	if action := restored.shortcuts.actionFor(custom); action != shortcutClearScreen {
		t.Fatalf("the custom chord belongs to %v, want clear screen", action)
	}
}

func TestASavedShortcutOutranksADefaultAddedByALaterBuild(t *testing.T) {
	primary := keymap.Primary
	restored := restoreShortcuts([]savedShortcut{{Action: "copy", Key: "k", Modifiers: uint8(primary)}})
	if got, want := restored.chords.Chord(shortcutCopy), (keymap.Chord{Modifiers: primary, Char: 'k'}); got != want {
		t.Fatalf("saved copy shortcut = %+v, want %+v", got, want)
	}
	// Clear screen's default would take that chord; the saved binding keeps it,
	// and the new action reads "—" until the user gives it one.
	if restored.chords.Chord(shortcutClearScreen).Valid() {
		t.Fatalf("clear screen took the saved chord: %+v", restored.chords.Chord(shortcutClearScreen))
	}
	if action := restored.actionFor(keymap.Chord{Modifiers: primary, Char: 'k'}); action != shortcutCopy {
		t.Fatalf("primary+K belongs to %v, want copy", action)
	}
	if got, want := restored.chords.Chord(shortcutNewTab), (keymap.Chord{Modifiers: primary, Char: 't'}); got != want {
		t.Fatalf("an action the file did not mention = %+v, want its default %+v", got, want)
	}
}

func TestShortcutSettingsSteadyPaintDoesNotAllocate(t *testing.T) {
	state := newTestState(t, testOptions(t))
	showShortcutSettings(state)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
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
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
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

// Shift+Insert and Ctrl+Insert are the Windows and Linux clipboard chords. They
// stand for Paste and Copy whatever those are bound to, and yield to an action
// the user binds to the same chord.
func TestClipboardInsertChordsAliasPasteAndCopy(t *testing.T) {
	shiftInsert := keymap.Chord{Modifiers: gfx.ModifierShift, Key: gfx.KeyInsert}
	ctrlInsert := keymap.Chord{Modifiers: gfx.ModifierControl, Key: gfx.KeyInsert}
	shortcuts := defaultShortcuts()
	if got := shortcuts.actionFor(shiftInsert); got != shortcutPaste {
		t.Fatalf("Shift+Insert = %v, want paste", got)
	}
	if got := shortcuts.actionFor(ctrlInsert); got != shortcutCopy {
		t.Fatalf("Ctrl+Insert = %v, want copy", got)
	}
	shortcuts.unbind(shortcutPaste)
	if got := shortcuts.actionFor(shiftInsert); got != shortcutPaste {
		t.Fatalf("Shift+Insert stopped pasting once Paste was unbound: %v", got)
	}
	// Another action may take the chord; the alias is not a conflict.
	if conflict, ok := shortcuts.bind(shortcutClearScreen, shiftInsert); !ok {
		t.Fatalf("binding Shift+Insert was refused by %v", conflict)
	}
	if got := shortcuts.actionFor(shiftInsert); got != shortcutClearScreen {
		t.Fatalf("explicit binding lost to the alias: %v", got)
	}
}

// The split defaults must be chords no other action holds, or one of the two
// would be shadowed by whichever the table happens to reach first. They must
// also have left the macOS menu bar's chord: Command+Alt+H is Hide Others, which
// a key equivalent never reaches the application for.
func TestSplitDefaultsAreUnclaimedAndOffTheMenuBarChord(t *testing.T) {
	shortcuts := defaultShortcuts()
	for _, action := range []shortcutAction{shortcutPaneSplitHorizontal, shortcutPaneSplitVertical} {
		chord := shortcuts.chords.Chord(action)
		if !chord.Valid() {
			t.Fatalf("%v has no default", action)
		}
		for other := shortcutAction(1); other < shortcutActionCount; other++ {
			if other != action && shortcuts.chords.Chord(other) == chord {
				t.Fatalf("%v and %v share %s", action, other, chordLabel(chord))
			}
		}
	}
	legacyHorizontal, _ := legacyPaneSplitChords()
	if shortcuts.chords.Chord(shortcutPaneSplitHorizontal) == legacyHorizontal {
		t.Fatalf("the split default is still %s", chordLabel(legacyHorizontal))
	}
}

// Migration moves the bindings still holding the split default an earlier build
// shipped, and only those: a chord the user chose, and a chord a snippet sends,
// keep their owner.
func TestOnlyUntouchedSplitDefaultsMigrate(t *testing.T) {
	legacyHorizontal, legacyVertical := legacyPaneSplitChords()
	horizontal, vertical := defaultPaneSplitChords()

	shortcuts := defaultShortcuts()
	if conflict, ok := shortcuts.bind(shortcutPaneSplitHorizontal, legacyHorizontal); !ok {
		t.Fatalf("saving the old split default was refused by %v", conflict)
	}
	if conflict, ok := shortcuts.bind(shortcutPaneSplitVertical, legacyVertical); !ok {
		t.Fatalf("saving the old split default was refused by %v", conflict)
	}
	shortcuts.migrateChangedDefaults(nil)
	if got := shortcuts.chords.Chord(shortcutPaneSplitHorizontal); got != horizontal {
		t.Fatalf("split right = %s, want the new default %s", chordLabel(got), chordLabel(horizontal))
	}
	if got := shortcuts.chords.Chord(shortcutPaneSplitVertical); got != vertical {
		t.Fatalf("split down = %s, want the new default %s", chordLabel(got), chordLabel(vertical))
	}

	// A chord the user picked is not a default gone stale.
	custom := keymap.Chord{Modifiers: keymap.Primary | gfx.ModifierShift, Char: 'x'}
	shortcuts = defaultShortcuts()
	if conflict, ok := shortcuts.bind(shortcutPaneSplitHorizontal, custom); !ok {
		t.Fatalf("binding a custom split chord was refused by %v", conflict)
	}
	shortcuts.migrateChangedDefaults(nil)
	if got := shortcuts.chords.Chord(shortcutPaneSplitHorizontal); got != custom {
		t.Fatalf("a custom split chord was migrated away to %s", chordLabel(got))
	}

	// A snippet that sends the new chord keeps it; the split waits for the user.
	shortcuts = defaultShortcuts()
	if conflict, ok := shortcuts.bind(shortcutPaneSplitVertical, legacyVertical); !ok {
		t.Fatalf("saving the old split default was refused by %v", conflict)
	}
	shortcuts.migrateChangedDefaults(func(chord keymap.Chord) bool { return chord == vertical })
	if got := shortcuts.chords.Chord(shortcutPaneSplitVertical); got != legacyVertical {
		t.Fatalf("split down took a chord a snippet sends: %s", chordLabel(got))
	}
}

// A start reconciles a stale table once and records that it did, so a chord the
// user later sets back to the old default stays where they put it.
func TestSplitDefaultsMigrateOncePerDevice(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	legacyHorizontal, _ := legacyPaneSplitChords()
	primary := keymap.Primary
	saveReleaseState(t, options.StatePath, savedState{
		HostCatalogVersion: hostCatalogSchemaVersion,
		ShortcutVersion:    shortcutSchemaVersion,
		Shortcuts: []savedShortcut{{
			Action: shortcutPaneSplitHorizontal.String(), Key: string([]byte{legacyHorizontal.Char}), Modifiers: uint8(primary | gfx.ModifierAlt),
		}},
	})

	state := newTestState(t, options)
	horizontal, _ := defaultPaneSplitChords()
	if got := state.shortcuts.chords.Chord(shortcutPaneSplitHorizontal); got != horizontal {
		t.Fatalf("a stale split default survived the start: %s", chordLabel(got))
	}

	// The user puts it back. Nothing may move it again.
	if conflict, ok := state.shortcuts.bind(shortcutPaneSplitHorizontal, legacyHorizontal); !ok {
		t.Fatalf("binding the old default back was refused by %v", conflict)
	}
	state.save()
	restarted := newTestState(t, options)
	if got := restarted.shortcuts.chords.Chord(shortcutPaneSplitHorizontal); got != legacyHorizontal {
		t.Fatalf("a chord the user set was migrated again to %s", chordLabel(got))
	}
}

func TestShiftInsertPastesIntoLiveTerminal(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.options.ClipboardText = func() (string, error) { return "echo hi", nil }
	event := gfx.KeyEvent{Target: terminalHit(state, 0), Key: gfx.KeyInsert, Modifiers: gfx.ModifierShift}
	if result := state.Key(event); !result.Handled {
		t.Fatal("Shift+Insert reached the terminal as a key instead of pasting")
	}
	if got := shell.typed(); !strings.Contains(got, "echo hi") || strings.Contains(got, "\x1b[2") {
		t.Fatalf("terminal received %q", got)
	}
}
