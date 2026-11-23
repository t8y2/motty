package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

func TestTerminalFolderShortcutOpensFocusedLocalDirectory(t *testing.T) {
	state, _, _, firstID, secondID := twoPanes(t)
	first, _ := state.sessionByIDPointer(firstID)
	second, _ := state.sessionByIDPointer(secondID)
	first.HostID, second.HostID = localHostID, localHostID
	first.localDirectory, second.localDirectory = t.TempDir(), t.TempDir()
	want := first.localDirectory
	opened := make(chan string, 1)
	state.options.OpenFolder = func(directory string) error {
		opened <- directory
		return nil
	}
	chord := state.shortcuts.chords.Chord(shortcutTerminalFolder)
	if chord != (keymap.Chord{Modifiers: gfx.ModifierControl | gfx.ModifierShift, Char: 'r'}) {
		t.Fatalf("unexpected default chord: %+v", chord)
	}
	result := state.Shortcut(gfx.KeyEvent{Target: terminalTestHitByID(state, firstID), Modifiers: chord.Modifiers, Text: "R"})
	if !result.Handled || !state.revealBusy {
		t.Fatalf("file manager was not requested: %+v", result)
	}
	waitFor(t, state, func() bool { return !state.revealBusy })
	select {
	case directory := <-opened:
		if directory != want {
			t.Fatalf("opened %q, want focused directory %q", directory, want)
		}
	default:
		t.Fatal("file manager did not receive the directory")
	}
}

func TestTerminalFolderShortcutSettingsPositionAndPersistence(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	showShortcutSettings(state)
	if state.shortcutActionLabel(shortcutTerminalFolder) == "" {
		t.Fatal("missing file manager shortcut label")
	}
	// The page lists the file-manager row above the local-terminal row; assert
	// the order the user sees rather than enum adjacency.
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	folderRow, ok := hitByID(&canvas, hitShortcutRowList.At(int(shortcutTerminalFolder-1)))
	if !ok {
		t.Fatal("file manager shortcut row is not painted")
	}
	localRow, ok := hitByID(&canvas, hitShortcutRowList.At(int(shortcutLocalTerminal-1)))
	if !ok {
		t.Fatal("local terminal shortcut row is not painted")
	}
	if folderRow.Bounds.Y >= localRow.Bounds.Y {
		t.Fatalf("file manager row sits below local terminal: folder=%+v local=%+v", folderRow.Bounds, localRow.Bounds)
	}
	state.Click(hitShortcutRowList.At(int(shortcutTerminalFolder - 1)))
	if state.modal != modalShortcutCapture || state.shortcutCapture != shortcutTerminalFolder {
		t.Fatal("settings row did not open shortcut capture")
	}
	custom := keymap.Chord{Modifiers: gfx.ModifierControl | gfx.ModifierAlt, Char: 'r'}
	pressChord(state, custom)
	restored := newTestState(t, options)
	if restored.shortcuts.chords.Chord(shortcutTerminalFolder) != custom {
		t.Fatal("custom file manager shortcut was not persisted")
	}
}

func TestTerminalFolderMenuOpensRightClickedPaneDirectory(t *testing.T) {
	state, _, _, firstID, secondID := twoPanes(t)
	first, _ := state.sessionByIDPointer(firstID)
	second, _ := state.sessionByIDPointer(secondID)
	first.HostID, second.HostID = localHostID, localHostID
	first.localDirectory, second.localDirectory = t.TempDir(), t.TempDir()
	want := first.localDirectory
	opened := make(chan string, 1)
	state.options.OpenFolder = func(directory string) error {
		opened <- directory
		return nil
	}
	state.terminalMenu = terminalMenu{open: true, session: firstID}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	folder, ok := menuHit(&canvas, state, hitTerminalMenuFolder)
	if !ok || folder.Disabled {
		t.Fatal("folder menu item is missing or disabled")
	}
	files, ok := menuHit(&canvas, state, hitTerminalMenuFiles)
	if !ok || folder.Bounds.Y+folder.Bounds.Height != files.Bounds.Y {
		t.Fatalf("folder item must immediately precede Files: folder=%+v files=%+v", folder.Bounds, files.Bounds)
	}
	chooseMenu(t, state, hitTerminalMenuFolder)
	waitFor(t, state, func() bool { return !state.revealBusy })
	select {
	case directory := <-opened:
		if directory != want {
			t.Fatalf("opened %q, want right-clicked directory %q", directory, want)
		}
	default:
		t.Fatal("file manager did not receive the directory")
	}
}
