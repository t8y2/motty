package workbench

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
)

// typingRig is a connected terminal driven through the framework's real
// painter, router and text-input path, with a shell that echoes what it hears.
type typingRig struct {
	driver *app.Driver
	state  *State
	shell  *fakeShell
	frames int // paints the driver ran
}

func newTypingRig(t testing.TB) *typingRig {
	t.Helper()
	var invalidator app.Invalidator
	world, options := newWorld(t)
	options.Invalidate = invalidator.Invalidate
	state := newTestState(t, options)
	rig := &typingRig{state: state}
	window := state.WindowColor()
	config := app.Config{
		Title: "latency", Width: 1400, Height: 900,
		ClearColor:  app.Color{Red: float64(window.Red), Green: float64(window.Green), Blue: float64(window.Blue), Alpha: 1},
		Invalidator: &invalidator,
		OnWake:      state.Wake,
		Paint: func(canvas *gfx.Canvas, size gfx.Size) {
			rig.frames++
			state.Paint(canvas, size)
		},
		OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll, OnResize: state.Resize,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if context.Terminal {
				return state.TerminalInput(context)
			}
			return nil
		},
	}
	driver, err := app.NewDriver(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	rig.driver = driver

	openHost(state)
	fake := world.conns[0]
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	invalidator.Invalidate()
	driver.Pump()
	rig.shell = fake.shell(t)
	rig.shell.say("\x1b[32muser@host\x1b[0m:~$ ")
	invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	if !driver.Snapshot().Editor.Composing && driver.Snapshot().Focused.IsZero() {
		t.Fatal("the terminal does not have the keyboard")
	}
	return rig
}

// keystroke types one character and delivers the echo, as the network would.
func (rig *typingRig) keystroke(character string) error {
	if err := rig.driver.Type(character); err != nil {
		return err
	}
	rig.shell.say(character)
	rig.state.options.Invalidate()
	return rig.driver.Pump()
}

func TestTypingEchoesThroughTheRealPainterPath(t *testing.T) {
	rig := newTypingRig(t)
	for _, character := range []string{"l", "s", " ", "-", "l"} {
		if err := rig.keystroke(character); err != nil {
			t.Fatal(err)
		}
	}
	if got := rig.shell.typed(); got != "ls -l" {
		t.Fatalf("the shell heard %q", got)
	}
	if text := rig.state.sessions[0].term.screen.RowText(0); text == "" {
		t.Fatal("nothing on the screen")
	}
	found := false
	for _, text := range rig.driver.Snapshot().Texts {
		if text.Value == "user@host:~$ ls -l" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the echo is not painted: %d texts", len(rig.driver.Snapshot().Texts))
	}
}

// One keystroke, from the text system to the echo on screen, through the
// painter and its change detection. This is everything on the window thread
// except the platform's own drawing.
func BenchmarkKeystrokeToEcho(b *testing.B) {
	rig := newTypingRig(b)
	b.ReportAllocs()
	column := 0
	for b.Loop() {
		if err := rig.keystroke("x"); err != nil {
			b.Fatal(err)
		}
		if column++; column == 100 { // keep the line from wrapping forever
			rig.shell.say("\r\n$ ")
			rig.state.options.Invalidate()
			rig.driver.Pump()
			column = 0
		}
	}
}

// floodOutput is what `ls -R --color` or a build log looks like to the parser:
// short lines with a color pair every few bytes.
func floodOutput(size int) string {
	const line = "\x1b[01;34mdirectory\x1b[0m  \x1b[01;32mexecutable\x1b[0m  plain.txt  \x1b[38;5;208marchive.tar.gz\x1b[0m\r\n"
	var out strings.Builder
	out.Grow(size + len(line))
	for out.Len() < size {
		out.WriteString(line)
	}
	return out.String()
}

// floodWakes plays a flood arriving at once and counts the frames the window
// takes to put all of it on screen.
func (rig *typingRig) floodWakes(b testing.TB, flood string) int {
	rig.shell.say(flood)
	rig.state.options.Invalidate()
	wakes := 0
	for term := rig.state.sessions[0].term; wakes == 0 || len(term.rx) > 0; wakes++ {
		if err := rig.driver.Pump(); err != nil {
			b.Fatal(err)
		}
		if wakes > 10000 {
			b.Fatal("the flood is still queued after 10000 frames")
		}
	}
	return wakes
}

// The rate at which a burst of output reaches the screen, and the frames it
// takes: bytes per second is the figure `cat` of a large file is felt by.
func BenchmarkFloodToScreen(b *testing.B) {
	rig := newTypingRig(b)
	flood := floodOutput(4 << 20)
	b.SetBytes(int64(len(flood)))
	b.ReportAllocs()
	wakes := 0
	for b.Loop() {
		wakes += rig.floodWakes(b, flood)
	}
	b.ReportMetric(float64(wakes)/float64(b.N), "wakes/op")
}

// A key struck while a flood is still queued drains the output first; this is
// the time that key takes, and so how far behind the keyboard falls.
func BenchmarkKeystrokeDuringFlood(b *testing.B) {
	rig := newTypingRig(b)
	flood := floodOutput(4 << 20)
	b.ReportAllocs()
	var typing time.Duration
	for b.Loop() {
		b.StopTimer()
		rig.shell.say(flood)
		b.StartTimer()
		started := time.Now()
		if err := rig.driver.Type("x"); err != nil {
			b.Fatal(err)
		}
		typing += time.Since(started)
		b.StopTimer()
		rig.floodWakes(b, "")
		b.StartTimer()
	}
	b.ReportMetric(float64(typing.Nanoseconds())/float64(b.N), "key-ns/op")
}
