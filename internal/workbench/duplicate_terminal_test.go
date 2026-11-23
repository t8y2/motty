package workbench

import (
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/sshconn"
)

func TestDuplicateTerminalShortcutStartsBelowWithSameDirectory(t *testing.T) {
	state, world, _, shell := liveSession(t)
	firstID := state.activeSessionID
	reportDirectory(state, &state.sessions[0], shell, "/data/workspace/ai/motty")
	chord := state.shortcuts.chords.Chord(shortcutDuplicateTerminal)
	if chord != (keymap.Chord{Modifiers: gfx.ModifierAlt | gfx.ModifierShift, Char: 't'}) {
		t.Fatalf("unexpected default chord: %+v", chord)
	}
	result := state.Shortcut(gfx.KeyEvent{Target: terminalTestHitByID(state, firstID), Modifiers: chord.Modifiers, Text: "T"})
	if !result.Handled || len(state.sessions) != 2 || result.Focus != state.activeTerminalFocusHit() {
		t.Fatalf("duplicate result=%+v sessions=%d", result, len(state.sessions))
	}
	first, _ := state.paneForSession(firstID)
	second, _ := state.paneForSession(state.activeSessionID)
	if second.Bounds.Y <= first.Bounds.Y || second.Bounds.X != first.Bounds.X || second.Bounds.Width != first.Bounds.Width {
		t.Fatalf("duplicate is not below source: first=%+v second=%+v", first.Bounds, second.Bounds)
	}
	conn := world.conns[1]
	conn.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	conn.shell(t)
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if len(conn.configs) != 1 || conn.configs[0].StartDirectory != "/data/workspace/ai/motty" {
		t.Fatalf("unexpected startup configs: %+v", conn.configs)
	}
}

func TestDuplicateTerminalShortcutPreservesLocalDirectory(t *testing.T) {
	state, first := openLocal(t)
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first.localDirectory = directory
	pressChord(state, state.shortcuts.chords.Chord(shortcutDuplicateTerminal))
	if len(state.sessions) != 2 {
		t.Fatalf("sessions=%d, want 2", len(state.sessions))
	}
	second := &state.sessions[1]
	t.Cleanup(func() {
		if second.term != nil && second.term.shell != nil {
			second.term.shell.Close()
		}
	})
	conn, ok := second.conn.(*localConn)
	if !ok || conn.dir != directory || second.localDirectory != directory {
		t.Fatalf("duplicate did not inherit local directory %q", directory)
	}
}

func TestDuplicateTerminalShortcutSettingsPersist(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	showShortcutSettings(state)
	if state.shortcutActionLabel(shortcutDuplicateTerminal) == "" {
		t.Fatal("missing settings label")
	}
	state.Click(hitShortcutRowList.At(int(shortcutDuplicateTerminal - 1)))
	if state.modal != modalShortcutCapture || state.shortcutCapture != shortcutDuplicateTerminal {
		t.Fatal("settings row did not open shortcut capture")
	}
	custom := keymap.Chord{Modifiers: gfx.ModifierControl | gfx.ModifierAlt, Char: 'y'}
	pressChord(state, custom)
	restored := newTestState(t, options)
	if restored.shortcuts.chords.Chord(shortcutDuplicateTerminal) != custom {
		t.Fatal("custom duplicate shortcut was not persisted")
	}
}

func TestDuplicateTerminalShortcutUsesFocusedPane(t *testing.T) {
	state, _, _, firstID, secondID := twoPanes(t)
	first, _ := state.sessionByIDPointer(firstID)
	second, _ := state.sessionByIDPointer(secondID)
	first.startDirectory = "/srv/first"
	second.startDirectory = "/srv/second"
	chord := state.shortcuts.chords.Chord(shortcutDuplicateTerminal)
	result := state.Shortcut(gfx.KeyEvent{Target: terminalTestHitByID(state, firstID), Modifiers: chord.Modifiers, Text: "t"})
	if !result.Handled || len(state.sessions) != 3 {
		t.Fatalf("duplicate result=%+v sessions=%d", result, len(state.sessions))
	}
	duplicate, _ := state.sessionByIDPointer(state.activeSessionID)
	if duplicate.startDirectory != "/srv/first" {
		t.Fatalf("copied directory %q, want focused pane directory", duplicate.startDirectory)
	}
	sourcePane, _ := state.paneForSession(firstID)
	duplicatePane, _ := state.paneForSession(duplicate.ID)
	if duplicatePane.Bounds.Y <= sourcePane.Bounds.Y || duplicatePane.Bounds.X != sourcePane.Bounds.X {
		t.Fatalf("duplicate is not below focused pane: source=%+v duplicate=%+v", sourcePane.Bounds, duplicatePane.Bounds)
	}
}
