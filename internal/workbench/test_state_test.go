package workbench

import (
	"context"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/localshell"
)

// newTestState is New for a test that may open the assistant's database: the
// state is closed when the test ends, because Windows cannot delete an open file.
func newTestState(t testing.TB, options Options) *State {
	t.Helper()
	if options.KnownHostsPath == "" {
		// Left empty, it and an empty StatePath default to the user's own Motty
		// folder: a test would open the real config.db and stamp it with this
		// build's format.
		options.KnownHostsPath = filepath.Join(t.TempDir(), "known_hosts")
	}
	if options.PreferredLanguages == nil {
		// The tests read English; the machine running them may not speak it.
		options.PreferredLanguages = func() []string { return nil }
	}
	if options.SystemAppearance == nil {
		// So are the tests' looks: the machine running them may be light.
		options.SystemAppearance = func() app.Appearance { return app.AppearanceUnknown }
	}
	if options.SetNativeAppearance == nil {
		options.SetNativeAppearance = func(app.Appearance) {}
	}
	if options.WatchSystemAppearance == nil {
		options.WatchSystemAppearance = func(func()) func() { return func() {} }
	}
	if options.SetInterfaceFont == nil {
		// Nor may a test change the process's interface font, or read the fonts of
		// the machine it runs on.
		options.SetInterfaceFont = func(string) bool { return true }
	}
	if options.FontFamilies == nil {
		options.FontFamilies = func() []string { return nil }
	}
	if options.ClipboardText == nil {
		// The clipboard a test reads is its own: the real one is asked on the
		// window's main thread, which a test does not have.
		options.ClipboardText = func() (string, error) { return "", nil }
	}
	if options.ClipboardFiles == nil {
		// Nor does a test read the files the developer last copied.
		options.ClipboardFiles = func() ([]string, error) { return nil, app.ErrNoFiles }
	}
	if options.Keystore == nil {
		// Left nil, the vault's key store is the machine's own: a test binary is
		// not the signed application, so macOS asks for the login keychain's
		// password every time a rebuilt binary opens the vault. A test keeps its
		// keys in memory; the ones that exercise a missing or denied store pass
		// their own.
		options.Keystore = &memKeys{}
	}
	state := New(options)
	// The shells a Windows computer offers are whatever is installed on it, and
	// naming WSL distributions starts wsl.exe: a test sees one shell and no
	// distributions, the way a computer with only a default shell would. On a
	// Unix test binary the Windows executable cannot run, so the fixture leaves
	// detection to the platform default.
	state.localShellMenu.discover = func() []localshell.Choice {
		if runtime.GOOS != "windows" {
			return nil
		}
		return []localshell.Choice{{Name: "CMD", Path: `C:\Windows\System32\cmd.exe`, Default: true}}
	}
	state.localShellMenu.discoverWSL = func(context.Context) []localshell.Choice { return nil }
	// A test never waits for the automatic check; the ones that exercise it set
	// these back to something short.
	state.update.firstDelay, state.update.every = time.Hour, time.Hour
	t.Cleanup(func() {
		state.Close()
		eventCanvasesMu.Lock()
		delete(eventCanvases, state)
		eventCanvasesMu.Unlock()
	})
	return state
}

// eventCanvases keeps, per state, the canvas the pointer helper paints events into.
// A real window has one; a test that calls state.Pointer directly has none, so the
// helper makes it. It lives in the test, not in State: production code never holds
// a canvas.
var eventCanvases = map[*State]*gfx.Canvas{}
var eventCanvasesMu sync.Mutex

// pointer sends a pointer event the way a window does: to the state's Pointer
// first and, if nothing there took it, to the controls that declared themselves
// to it while painting (the event pass). The state is painted once before its first
// event so that the controls have declared what they listen to.
func pointer(state *State, event gfx.PointerEvent) gfx.EventResult {
	if event.Viewport == (gfx.Size{}) {
		event.Viewport = gfx.Size{Width: 1400, Height: 900}
	}
	eventCanvasesMu.Lock()
	canvas, seen := eventCanvases[state]
	if !seen {
		canvas = &gfx.Canvas{}
		eventCanvases[state] = canvas
	}
	eventCanvasesMu.Unlock()
	if !seen {
		canvas.BeginFrame()
		state.Paint(canvas, event.Viewport)
		canvas.EndFrame()
	}
	result := state.Pointer(event)
	if result.Handled {
		return result
	}
	if canvas.DeliverPointer(event, event.Viewport, state.Paint) {
		result.Handled, result.Repaint = true, true
	}
	return result
}
