package workbench

import (
	"strings"
	"sync"
	"testing"
	"time"

	"motty/internal/termsize"
)

// floodChunk is one read's worth of the plain monitoring lines a program like
// codex streams while a multiplexer above it keeps the pane busy. Every line
// scrolls the screen, and a sustained stream of them arrives faster than the
// pump's per-frame budget can consume.
func floodChunk(bytes int) []byte {
	var b strings.Builder
	for b.Len() < bytes {
		b.WriteString("cpu 12% mem 3.2G load 0.42 net rx 12MB/s tx 3MB/s disk 40%\n")
	}
	return []byte(b.String())
}

// floodShell stands in for the shell's output buffer the way sshconn and
// localshell build it: the reader blocks while a megabyte of output is
// pending, and a drain takes all of it. The bound only holds if the pump
// leaves output here when it cannot keep up.
type floodShell struct {
	inner Shell
	mu    sync.Mutex
	space *sync.Cond
	data  []byte
	ended bool
}

func newFloodShell(inner Shell) *floodShell {
	shell := &floodShell{inner: inner}
	shell.space = sync.NewCond(&shell.mu)
	return shell
}

// say is the reader goroutine's put: it blocks while the buffer is full.
func (shell *floodShell) say(p []byte) bool {
	shell.mu.Lock()
	defer shell.mu.Unlock()
	for len(shell.data) >= 1<<20 && !shell.ended {
		shell.space.Wait()
	}
	if shell.ended {
		return false
	}
	shell.data = append(shell.data, p...)
	return true
}

func (shell *floodShell) Drain(dst []byte) []byte {
	shell.mu.Lock()
	dst = append(dst, shell.data...)
	shell.data = shell.data[:0]
	shell.mu.Unlock()
	shell.space.Broadcast()
	return dst
}

func (shell *floodShell) Write(p []byte) bool       { return shell.inner.Write(p) }
func (shell *floodShell) Resize(size termsize.Size) { shell.inner.Resize(size) }
func (shell *floodShell) Done() <-chan struct{}     { return shell.inner.Done() }
func (shell *floodShell) Err() error                { return shell.inner.Err() }
func (shell *floodShell) Close() {
	shell.mu.Lock()
	shell.ended = true
	shell.mu.Unlock()
	shell.space.Broadcast()
	shell.inner.Close()
}

// A shell that keeps printing must not let the pump's unparsed backlog grow
// without bound: the reader blocks on the shell's bounded buffer, so the pump
// has to leave output there instead of pulling all of it into rx every wake.
// Otherwise a program that floods (t8y2/motty#976: herdr redraws while codex
// streams) grows rx until the process is OOM-killed.
func TestPumpBacklogStaysBoundedUnderFlood(t *testing.T) {
	state, _, _, inner := liveSession(t)
	session := &state.sessions[0]
	shell := newFloodShell(inner)
	session.term.shell = shell
	t.Cleanup(func() { shell.Close() })

	chunk := floodChunk(1 << 20)
	const wakes = 120
	shell.say(chunk) // the first read is already pending when the pumping starts
	fed := make(chan struct{})
	go func() {
		defer close(fed)
		for range wakes - 1 {
			if !shell.say(chunk) {
				return
			}
		}
	}()

	const bound = 8 << 20 // a bounded pump keeps rx to a few MB, not tens
	peak := 0
	for range wakes {
		state.pump(session)
		if n := len(session.term.rx); n > peak {
			peak = n
		}
	}
	shell.Close()
	<-fed
	t.Logf("after %d wakes against a flooding reader: rx peak = %d bytes (%.1f MB)",
		wakes, peak, float64(peak)/(1<<20))
	if peak == 0 {
		t.Fatal("the flood never reached the pump; the test proved nothing")
	}
	if peak > bound {
		t.Fatalf("unparsed backlog grew to %d bytes (%.1f MB), want <= %d: the pump drains faster than it parses with no bound",
			peak, float64(peak)/(1<<20), bound)
	}
}

// A frame that leaves output in the shell's bounded buffer must request another
// wake even when it happens to parse all of rx: the shell is not drained on
// that frame, so nothing else would schedule the next one.
func TestABackloggedPumpRequestsAnotherWake(t *testing.T) {
	state, _, _, inner := liveSession(t)
	session := &state.sessions[0]
	shell := newFloodShell(inner)
	session.term.shell = shell
	t.Cleanup(func() { shell.Close() })

	session.term.rx = append(session.term.rx, floodChunk(maxPumpBacklog)...)
	invalidates := 0
	previous := state.options.Invalidate
	state.options.Invalidate = func() {
		invalidates++
		if previous != nil {
			previous()
		}
	}

	// A budget past the whole slice parses all of rx, so the only reason to
	// invalidate is that the drain was skipped and the shell still holds output.
	state.pumpFor(session, time.Hour)
	if len(session.term.rx) != 0 {
		t.Fatalf("rx holds %d bytes after a full budget", len(session.term.rx))
	}
	if invalidates != 1 {
		t.Fatalf("invalidates = %d, want one for the skipped drain", invalidates)
	}
}
