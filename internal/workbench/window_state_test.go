package workbench

import (
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func windowStateOptions(dir string) Options {
	return Options{
		KnownHostsPath:      filepath.Join(dir, "known_hosts"),
		StatePath:           filepath.Join(dir, "config.db"),
		RememberWindowFrame: true,
	}
}

// TestWindowFrameSurvivesRestart proves that where the first window was is
// written before the state closes and read back when the next window opens.
func TestWindowFrameSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	options := windowStateOptions(dir)
	frame := gfx.Rect{X: 120, Y: 80, Width: 1234, Height: 777}

	state := newTestState(t, options)
	state.NoteWindowFrame(frame)
	state.closeWindowChrome()
	state.Close()

	reopened := newTestState(t, options)
	got, ok := reopened.RestoredWindowFrame()
	if !ok || got != frame {
		t.Fatalf("restored frame = %+v, %t; want %+v", got, ok, frame)
	}
}

// TestMinimizedWindowHidesRemoteDesktops proves the minimized state reaches the
// visibility rule that stops a remote desktop's stream.
func TestMinimizedWindowHidesRemoteDesktops(t *testing.T) {
	state := newTestState(t, Options{})
	invalidations := 0
	state.options.Invalidate = func() { invalidations++ }
	state.activeSection = sectionTerminal
	session := &Session{ID: 1}

	state.SetWindowMinimized(true)
	if !state.WindowMinimized() {
		t.Fatal("WindowMinimized did not follow the callback")
	}
	if invalidations != 1 {
		t.Fatalf("invalidations = %d, want 1", invalidations)
	}
	if state.desktopVisible(session) {
		t.Fatal("a minimized window's desktop is visible")
	}
	state.SetWindowMinimized(false)
	if state.WindowMinimized() {
		t.Fatal("WindowMinimized did not follow the restore")
	}
}
