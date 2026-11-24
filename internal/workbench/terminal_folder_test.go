package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/localshell"
	"motty/internal/term"
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

func TestTerminalFolderFallsBackWhenLocalDirectoryUnreported(t *testing.T) {
	reported := t.TempDir()
	startup := t.TempDir()
	remembered := t.TempDir()

	session := &Session{ID: 1, HostID: localHostID, conn: newLocalConn("", networkProxyDirect), term: &terminal{screen: term.New(80, 24, 100)}}
	session.term.reportedDirectory = term.Cwd{Path: reported}
	if got := localSessionDirectory(session); got != filepath.Clean(reported) {
		t.Fatalf("reported directory: got %q, want %q", got, filepath.Clean(reported))
	}

	session.term.reportedDirectory = term.Cwd{}
	session.localDirectory = remembered
	if got := localSessionDirectory(session); got != filepath.Clean(remembered) {
		t.Fatalf("remembered directory: got %q, want %q", got, filepath.Clean(remembered))
	}

	session.localDirectory = ""
	session.conn.(*localConn).dir = startup
	if got := localSessionDirectory(session); got != filepath.Clean(startup) {
		t.Fatalf("startup directory: got %q, want %q", got, filepath.Clean(startup))
	}

	session.conn.(*localConn).dir = ""
	if got := localSessionDirectory(session); got != filepath.Clean(localHome()) {
		t.Fatalf("home fallback: got %q, want %q", got, filepath.Clean(localHome()))
	}

	wsl := &Session{ID: 2, HostID: localHostID, conn: newLocalConn(localshell.WSLShell("Ubuntu"), networkProxyDirect), term: &terminal{screen: term.New(80, 24, 100)}}
	if canOpenSessionFolder(wsl) {
		t.Fatalf("WSL session without a reported path must not fall back to host home: %q", localSessionDirectory(wsl))
	}

	remote := &Session{ID: 3, host: Host{Protocol: protocolSSH, Address: "box"}}
	if canOpenSessionFolder(remote) {
		t.Fatal("remote session must not offer local folder opening")
	}
}

// A name in the terminal is only resolved against a folder the shell is known
// to be in; the open-folder shortcut's home fallback must not make an unrelated
// file of the same name in the home directory the one that is shown.
func TestRevealTargetIsNotResolvedAgainstTheHomeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.WriteFile(filepath.Join(home, "same.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := &State{}
	session := &Session{ID: 1, HostID: localHostID, conn: newLocalConn("", networkProxyDirect), term: &terminal{screen: term.New(80, 24, 100)}}
	if !canOpenSessionFolder(session) {
		t.Skip("no home directory to fall back to on this machine")
	}
	if path, ok := state.localRevealTarget(session, "same.txt"); ok {
		t.Fatalf("a name with no known shell directory resolved to %q", path)
	}
	session.term.reportedDirectory = term.Cwd{Path: home}
	if _, ok := state.localRevealTarget(session, "same.txt"); !ok {
		t.Fatal("a name in the reported directory must resolve")
	}
}
