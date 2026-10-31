//go:build darwin || linux

package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

func TestLocalTerminalWaitsForRealShellOutput(test *testing.T) {
	home := test.TempDir()
	program := filepath.Join(home, "startup-shell")
	started := filepath.Join(home, "started")
	script := "#!/bin/sh\nstty -echo\n: > \"$MOTTY_SHELL_STARTED\"\nIFS= read -r answer\nprintf 'local-ready-%s\\r\\n' \"$answer\"\nexec /bin/sh\n"
	if err := os.WriteFile(program, []byte(script), 0o700); err != nil {
		test.Fatal(err)
	}
	test.Setenv("HOME", home)
	test.Setenv("SHELL", program)
	test.Setenv("MOTTY_SHELL_STARTED", started)
	var invalidator app.Invalidator
	options := testOptions(test)
	options.Invalidate = invalidator.Invalidate
	state := newTestState(test, options)
	state.Click(hitLocalTerminal)
	local := &state.sessions[0]
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			test.Fatal("the real local shell did not reach its silent input wait")
		}
		time.Sleep(time.Millisecond)
	}
	paints := 0
	view, err := headless.Open(app.Config{
		Width: 1400, Height: 900, Invalidator: &invalidator, OnWake: state.Wake,
		Paint: func(canvas *ui.Canvas, size ui.Size) {
			paints++
			state.Paint(canvas, size)
		},
		TextInput: state.TerminalInput,
		OnKey:     state.Key, OnPointer: state.Pointer, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		test.Fatal(err)
	}
	defer view.Close()
	if local.term.shell == nil || local.term.live {
		test.Fatal("the test did not start a real shell without output")
	}
	label, _ := state.sessionStatus(local)
	if label != state.messages.ConnDetailShellWaiting {
		test.Fatalf("a real silent shell reports %q", label)
	}
	if _, ok := view.Hit(state.messages.ConnShowProgress); ok {
		test.Fatal("local startup offers remote connection progress")
	}
	// Let the window settle into its opening state before counting frames: the
	// click starts work of its own — the session journal writes its row, the
	// completion catalog loads — and each result wakes the window once for the
	// frame that shows it. The new terminal also gets the keyboard, so its
	// cursor blinks until cursorBlinkSpan after the last thing that happened.
	// Wait until a whole second passes with no paint at all; from then on a
	// window left alone must not ask for a single frame until the user does
	// something.
	if err := view.Settle(250*time.Millisecond, time.Minute); err != nil {
		test.Fatal(err)
	}
	for settling := time.Duration(0); settling < 3*cursorBlinkSpan; settling += time.Second {
		before := paints
		if err := view.Wait(time.Second); err != nil {
			test.Fatal(err)
		}
		if paints == before {
			break
		}
	}
	before := paints
	if err := view.Wait(time.Minute); err != nil {
		test.Fatal(err)
	}
	if paints != before {
		test.Fatalf("a silent local shell schedules idle frames (%d in a minute)", paints-before)
	}
	if err := view.Type("probe"); err != nil {
		test.Fatal(err)
	}
	if err := view.Keys("Enter"); err != nil {
		test.Fatal(err)
	}
	if err := view.WaitFor(func(snapshot app.Snapshot) bool {
		for _, text := range snapshot.Texts {
			if strings.Contains(text.Value, "local-ready-probe") {
				return true
			}
		}
		return false
	}, 3*time.Second); err != nil {
		test.Fatal(err)
	}
	label, _ = state.sessionStatus(local)
	if label != state.messages.HostStateConnected {
		test.Fatalf("the local shell spoke but still reports %q", label)
	}
	if kind, _ := state.sessionLight(local); kind != lightOn {
		test.Fatal("the local shell output did not update the readiness light")
	}
}
