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

// TestMinimizedWindowFrameDoesNotReplaceTheLastUsableFrame proves the window
// manager's small off-screen rectangle, reported when the window is minimized,
// never becomes the frame the next launch restores. Windows can deliver the
// move before it tells the application the window is minimized.
func TestMinimizedWindowFrameDoesNotReplaceTheLastUsableFrame(t *testing.T) {
	dir := t.TempDir()
	options := windowStateOptions(dir)
	frame := gfx.Rect{X: 120, Y: 80, Width: 1234, Height: 777}
	minimized := gfx.Rect{X: -32000, Y: -32000, Width: 160, Height: 28}

	state := newTestState(t, options)
	state.NoteWindowFrame(frame)
	state.NoteWindowFrame(minimized)
	state.closeWindowChrome()
	state.Close()

	reopened := newTestState(t, options)
	got, ok := reopened.RestoredWindowFrame()
	if !ok || got != frame {
		t.Fatalf("restored frame = %+v, %t; want the last usable frame %+v", got, ok, frame)
	}
}

// TestRestoredUnusableFrameIsIgnored proves a record an older build filled with
// the minimized rectangle does not open the window in the corner at a size it
// could never have had.
func TestRestoredUnusableFrameIsIgnored(t *testing.T) {
	state := newTestState(t, Options{})
	state.restoreWindowFrame(gfx.Rect{X: -32000, Y: -32000, Width: 160, Height: 28})
	if got, ok := state.RestoredWindowFrame(); ok {
		t.Fatalf("restored frame = %+v, want no frame", got)
	}
}

// TestWindowMaximizedSurvivesRestart proves that maximizing a remembered window
// persists alongside its floating frame, and unmaximizing clears it without
// losing the floating frame.
func TestWindowMaximizedSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	options := windowStateOptions(dir)
	frame := gfx.Rect{X: 120, Y: 80, Width: 1234, Height: 777}

	state := newTestState(t, options)
	state.NoteWindowFrame(frame)
	state.SetWindowMaximized(true)
	state.closeWindowChrome()
	state.Close()

	reopened := newTestState(t, options)
	got, ok := reopened.RestoredWindowFrame()
	if !ok || got != frame {
		t.Fatalf("restored frame = %+v, %t; want %+v", got, ok, frame)
	}
	if !reopened.RestoredWindowMaximized() {
		t.Fatal("RestoredWindowMaximized = false, want true")
	}

	reopened.SetWindowMaximized(false)
	reopened.closeWindowChrome()
	reopened.Close()

	restored := newTestState(t, options)
	got, ok = restored.RestoredWindowFrame()
	if !ok || got != frame {
		t.Fatalf("frame after unmaximize = %+v, %t; want %+v", got, ok, frame)
	}
	if restored.RestoredWindowMaximized() {
		t.Fatal("RestoredWindowMaximized = true after unmaximize, want false")
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
