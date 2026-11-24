package workbench

import (
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"motty/internal/i18n"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

func TestLocalDirectoryPickerShortcutHistoryAndBrowse(t *testing.T) {
	skipWithoutUnixShell(t)
	options := freshOptions(t)
	state := newTestState(t, options)
	first, second := t.TempDir(), t.TempDir()
	state.localDirectoryHistory = []string{first, second}
	chord := state.shortcuts.chords.Chord(shortcutAddLocalTerminal)
	if chord != (keymap.Chord{Modifiers: primaryModifier | gfx.ModifierShift, Char: 'o'}) {
		t.Fatalf("chord = %+v", chord)
	}
	pressChord(state, chord)
	if state.modal != modalLocalDirectory {
		t.Fatal("picker did not open")
	}
	state.localDirectoryPicker.search.Insert(second)
	state.refreshLocalDirectories()
	if !slices.Equal(state.localDirectoryPicker.visible, []string{second}) {
		t.Fatal("search did not filter history")
	}
	state.options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return second, nil }
	state.Click(hitLocalDirectoryBrowse)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	if session == nil || !session.isLocal() || session.localDirectory != second {
		t.Fatal("browse did not start local terminal in chosen directory")
	}
	if !slices.Equal(state.localDirectoryHistory, []string{second, first}) {
		t.Fatal("history did not move chosen directory to front")
	}
	restored := newTestState(t, options)
	if !slices.Equal(restored.localDirectoryHistory, state.localDirectoryHistory) {
		t.Fatal("history did not persist")
	}
	state.openLocalDirectoryPicker()
	state.Click(hitLocalDirectoryRemove.At(1))
	restored = newTestState(t, options)
	if !slices.Equal(restored.localDirectoryHistory, []string{second}) {
		t.Fatal("history deletion did not persist")
	}
}

func TestLocalDirectoryPickerAddsAdjacentPane(t *testing.T) {
	skipWithoutUnixShell(t)
	for _, maximized := range []bool{false, true} {
		t.Run(map[bool]string{false: "tiled", true: "maximized"}[maximized], func(t *testing.T) {
			state, _, _ := twoLivePanes(t)
			workspace := state.deck.ActiveWorkspace()
			original := state.activeSessionID
			state.deck.SetPaneMaximized(workspace, maximized)
			directory := t.TempDir()
			state.localDirectoryHistory = []string{directory}
			state.openLocalDirectoryPicker()
			result, handled := state.localDirectoryKey(gfx.KeyEvent{Target: hitLocalDirectorySearch, Key: gfx.KeyEnter})
			if !handled || !result.Handled || state.modal != modalNone {
				t.Fatal("selection did not close picker")
			}
			session, _ := state.sessionByIDPointer(state.activeSessionID)
			if len(state.sessions) != 3 || session.ID == original || !session.isLocal() || session.localDirectory != directory {
				t.Fatal("new local pane did not open")
			}
			if state.deck.ActiveWorkspace() != workspace || len(state.deck.Panes(workspace, nil)) != 3 {
				t.Fatal("new terminal left current workspace")
			}
			if state.deck.PaneMaximized(workspace) != maximized {
				t.Fatal("display mode changed")
			}
			if !maximized {
				first, _ := state.paneForSession(original)
				second, _ := state.paneForSession(session.ID)
				if second.Bounds == first.Bounds {
					t.Fatal("panes are not tiled")
				}
			}
		})
	}
}

func TestLocalDirectoryPickerPaintAndNavigation(t *testing.T) {
	state := newTestState(t, testOptions(t))
	first, second := t.TempDir(), t.TempDir()
	state.localDirectoryHistory = []string{first, second}
	state.openLocalDirectoryPicker()
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1000, Height: 800}
	state.Paint(&canvas, size)
	if canvas.InitialFocus() != hitLocalDirectorySearch {
		t.Fatal("search is not initially focused")
	}
	for _, id := range []gfx.ID{hitLocalDirectorySearch, hitLocalDirectoryBrowse, hitLocalDirectoryRows.At(0), hitLocalDirectoryRemove.At(0)} {
		hit, ok := hitByID(&canvas, id)
		if !ok || hit.Bounds.Width <= 0 || hit.Bounds.Height <= 0 {
			t.Fatalf("missing visible control: %v", id)
		}
	}
	state.localDirectoryKey(gfx.KeyEvent{Key: gfx.KeyDown})
	if state.localDirectoryPicker.selected != 1 {
		t.Fatal("down did not select second directory")
	}
	state.localDirectoryKey(gfx.KeyEvent{Key: gfx.KeyUp})
	if state.localDirectoryPicker.selected != 0 {
		t.Fatal("up did not select first directory")
	}
	canvas.Reset()
	canvas.BeginEventPass(hitLocalDirectoryRemove.At(0), gfx.ClickInfo{})
	state.Paint(&canvas, size)
	canvas.EndEventPass()
	canvas.RunAfterPaint()
	if !slices.Equal(state.localDirectoryHistory, []string{second}) {
		t.Fatal("pointer deletion did not remove history")
	}
	state.localDirectoryKey(gfx.KeyEvent{Key: gfx.KeyEscape})
	if state.modal != modalNone {
		t.Fatal("escape did not close picker")
	}
}

func TestLocalDirectoryPickerLongPathsAndLanguages(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		state := newTestState(t, testOptions(t))
		state.messages = i18n.For(language)
		directory := "/workspace/" + strings.Repeat("long-folder/", 40)
		state.localDirectoryHistory = []string{directory}
		state.openLocalDirectoryPicker()
		var canvas gfx.Canvas
		state.Paint(&canvas, gfx.Size{Width: 800, Height: 700})
		row, ok := hitByID(&canvas, hitLocalDirectoryRows.At(0))
		if !ok || row.Label != directory {
			t.Fatal("full directory missing from row")
		}
		remove, ok := hitByID(&canvas, hitLocalDirectoryRemove.At(0))
		if !ok || remove.Bounds.X+remove.Bounds.Width > 800 || remove.Bounds.X < row.Bounds.X || remove.Bounds.X+remove.Bounds.Width > row.Bounds.X+row.Bounds.Width {
			t.Fatal("long path displaced delete button")
		}
		if state.shortcutActionLabel(shortcutAddLocalTerminal) != state.messages.AddLocalTerminal {
			t.Fatal("shortcut label is not localized")
		}
	}
}

func TestLocalDirectoryPickerShortcutCustomizationPersists(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.openShortcutCapture(shortcutAddLocalTerminal)
	chord := keymap.Chord{Modifiers: gfx.ModifierControl | gfx.ModifierAlt, Char: 'n'}
	state.captureShortcut(gfx.KeyEvent{}, chord)
	restored := newTestState(t, options)
	if restored.shortcuts.chords.Chord(shortcutAddLocalTerminal) != chord {
		t.Fatal("custom shortcut did not persist")
	}
}

func TestLocalDirectoryPickerMenuAndPalette(t *testing.T) {
	state := newTestState(t, testOptions(t))
	label := state.messages.AddLocalTerminal
	found := false
	for _, menu := range state.MenuBar() {
		for _, item := range menu.Items {
			if item.Title == label {
				found = true
				item.OnSelect()
				if state.modal != modalLocalDirectory {
					t.Fatal("menu did not open directory picker")
				}
			}
		}
	}
	if !found {
		t.Fatal("directory picker missing from menu")
	}
	state.modal = modalNone
	state.openCommandPalette()
	for _, entry := range state.commandPalette.entries {
		if entry.label == label && entry.target == hitLocalDirectoryOpen {
			return
		}
	}
	t.Fatal("directory picker missing from command palette")
}

func TestLocalDirectoryPickerRestoresPreparedLayout(t *testing.T) {
	for _, maximized := range []bool{false, true} {
		state, _, _ := twoLivePanes(t)
		workspace := state.deck.ActiveWorkspace()
		state.deck.SetPaneMaximized(workspace, maximized)
		before := state.deck.DTO()
		session, _ := state.sessionByIDPointer(state.activeSessionID)
		rollback := state.localDirectoryLayoutRollback(session)
		if !state.prepareAdjacentTerminal(session, state.messages.AddLocalTerminal) {
			t.Fatal("could not prepare adjacent pane")
		}
		rollback()
		if !reflect.DeepEqual(state.deck.DTO(), before) {
			t.Fatal("rollback changed layout, focus, or maximize state")
		}
	}
}

func TestLocalDirectoryHistoryRestoreKeepsRecentUniquePaths(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	root := t.TempDir()
	for i := range localDirectoryHistoryLimit + 10 {
		directory := filepath.Join(root, fmt.Sprint(i))
		state.localDirectoryHistory = append(state.localDirectoryHistory, directory, directory)
	}
	state.save()
	restored := newTestState(t, options)
	if len(restored.localDirectoryHistory) != localDirectoryHistoryLimit {
		t.Fatalf("restored %d paths", len(restored.localDirectoryHistory))
	}
	for i, directory := range restored.localDirectoryHistory {
		if directory != filepath.Join(root, fmt.Sprint(i)) {
			t.Fatalf("recent path order changed at %d", i)
		}
	}
}

func TestLocalDirectoryPickerScrollKeepsSelectionVisible(t *testing.T) {
	state := newTestState(t, testOptions(t))
	for i := range 30 {
		state.localDirectoryHistory = append(state.localDirectoryHistory, filepath.Join(t.TempDir(), fmt.Sprint(i)))
	}
	state.openLocalDirectoryPicker()
	var canvas gfx.Canvas
	size := gfx.Size{Width: 900, Height: 700}
	state.Paint(&canvas, size)
	picker := &state.localDirectoryPicker
	for i := 0; i < len(picker.visible)-1; i++ {
		state.localDirectoryKey(gfx.KeyEvent{Key: gfx.KeyDown})
		canvas.Reset()
		state.Paint(&canvas, size)
		top := float32(picker.selected) * (picker.rowHeight + picker.rowGap)
		if top < picker.scroll.Offset-1 || top+picker.rowHeight > picker.scroll.Offset+picker.scroll.Viewport+1 {
			t.Fatalf("selected row %d is outside viewport", picker.selected)
		}
	}
	for i := len(picker.visible) - 1; i > 0; i-- {
		state.localDirectoryKey(gfx.KeyEvent{Key: gfx.KeyUp})
		canvas.Reset()
		state.Paint(&canvas, size)
	}
	if picker.scroll.Offset != 0 {
		t.Fatalf("first row did not return to top: %v", picker.scroll.Offset)
	}
}
