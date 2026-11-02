package remotestats

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// The first reading waits for the shell to start, which a heavy login
	// script can slow; later ones are a second apart.
	firstReadingTimeout = 10 * time.Second
	readingTimeout      = 5 * time.Second
)

// Result is one completed sample. An error is terminal for the worker so a
// refused or unsupported command is never retried in the background.
type Result struct {
	Sample  Sample
	Details *Details
	Err     error
}

// commandKind is which output protocol a worker's command speaks. The POSIX
// one reads Linux procfs; the other two each have their own line.
type commandKind uint8

const (
	kindPOSIX commandKind = iota
	kindWindows
	kindDarwin
)

// Worker owns one goroutine and one remote command only while the user has
// monitoring enabled.
type Worker struct {
	runner  Runner
	notify  func()
	ctx     context.Context
	cancel  context.CancelFunc
	results chan Result
	done    chan struct{}
	close   sync.Once
	command string
	kind    commandKind
	// now is the receiver's clock, which times the macOS interface counters.
	now func() time.Time
}

// Start begins sampling immediately. It is intentionally the constructor:
// merely opening a connection creates no worker, timer, or remote command.
func Start(runner Runner, notify func()) *Worker {
	return start(runner, notify, firstReadingTimeout, readingTimeout)
}

func start(runner Runner, notify func(), first, next time.Duration) *Worker {
	return startCommand(runner, notify, first, next, command, kindPOSIX)
}

// StartWithInterval changes the remote sampling interval without adding a
// second timer or SSH stream. The initial quarter-second warmup stays fast.
func StartWithInterval(runner Runner, notify func(), seconds int) *Worker {
	if !validInterval(seconds) {
		seconds = 2
	}
	script := strings.TrimSuffix(command, "sleep 1\ndone") + "sleep " + strconv.Itoa(seconds) + "\ndone"
	return startCommand(runner, notify, firstReadingTimeout, time.Duration(seconds)*time.Second+readingTimeout, script, kindPOSIX)
}

func startCommand(runner Runner, notify func(), first, next time.Duration, script string, kind commandKind) *Worker {
	worker := newWorker(runner, notify, script, kind)
	go worker.run(first, next)
	return worker
}

func newWorker(runner Runner, notify func(), script string, kind commandKind) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	worker := &Worker{
		runner: runner, notify: notify, ctx: ctx, cancel: cancel, command: script, kind: kind, now: time.Now,
		results: make(chan Result, 1), done: make(chan struct{}),
	}
	return worker
}

func validInterval(seconds int) bool {
	return seconds == 1 || seconds == 2 || seconds == 5 || seconds == 10 || seconds == 30 || seconds == 60
}

// Results is the completion stream drained by the window thread. It holds only
// the latest unread result: a window that is slow to draw skips readings rather
// than making the server's command wait.
func (worker *Worker) Results() <-chan Result { return worker.results }

// Done closes once all sampling work has stopped.
func (worker *Worker) Done() <-chan struct{} { return worker.done }

// Close interrupts the running command and returns immediately.
func (worker *Worker) Close() { worker.close.Do(worker.cancel) }

func (worker *Worker) run(first, next time.Duration) {
	defer close(worker.done)
	defer close(worker.results)

	ctx, cancel := context.WithCancel(worker.ctx)
	defer cancel()
	stalled := make(chan struct{})
	var stall sync.Once
	watchdog := time.AfterFunc(first, func() { stall.Do(func() { close(stalled) }); cancel() })
	defer watchdog.Stop()

	var (
		previous             counters
		have                 bool
		fault                error
		pending              []GPU // the GPU lines that came before the counters line they belong to
		primary              string
		previousPrimary      string
		cores, previousCores []coreCounters
		windowsRead          bool
		startupLines         int
		darwinPrevious       darwinReading
		darwinAt             time.Time
		darwinHave           bool
	)
	limit := maxLineBytes
	if worker.kind == kindWindows {
		limit = maxWindowsLineBytes
	}
	err := worker.runner.StreamCommand(ctx, worker.command, limit, func(line []byte) bool {
		if strings.HasPrefix(string(line), "gop-stats-win-v1 ") {
			result, err := parseWindows(line)
			if err != nil {
				fault = err
				return false
			}
			watchdog.Reset(next)
			worker.publish(result)
			windowsRead = true
			return true
		}
		if strings.HasPrefix(string(line), darwinPrefix) {
			reading, err := parseDarwin(line)
			if err != nil {
				fault = err
				return false
			}
			at := worker.now()
			watchdog.Reset(next)
			if darwinHave && !reading.warm {
				worker.publish(Result{Sample: darwinSample(darwinPrevious, reading, at.Sub(darwinAt))})
			}
			darwinPrevious, darwinAt, darwinHave = reading, at, true
			return true
		}
		if worker.kind == kindWindows && !strings.HasPrefix(string(line), "gop-stats-unsupported") {
			// Some Windows SSH shells print a bounded login banner even for
			// command sessions. Never let it reset the first-reading timeout.
			if !windowsRead && startupLines < 8 && len(line) < 1024 {
				startupLines++
				return true
			}
			fault = ErrMalformed
			return false
		}
		if strings.HasPrefix(string(line), "gop-core ") {
			core, err := parseCore(line)
			if err != nil || len(cores) >= 256 {
				fault = ErrMalformed
				return false
			}
			cores = append(cores, core)
			return true
		}
		if strings.HasPrefix(string(line), "gop-net-primary ") {
			primary = strings.TrimSpace(strings.TrimPrefix(string(line), "gop-net-primary "))
			if primary == "-" {
				primary = ""
			}
			if len(primary) > 64 || strings.ContainsAny(primary, "\r\n\t") {
				fault = ErrMalformed
				return false
			}
			return true
		}
		if isGPULine(line) {
			pending = parseGPULine(pending, line)
			return true
		}
		current, err := parse(line)
		if err != nil {
			fault = err
			return false
		}
		watchdog.Reset(next)
		if have {
			if reading, ok := sample(previous, current); ok {
				reading.GPUs = pending
				reading.Cores = coreUsage(previousCores, cores)
				reading.NetworkInterface = primary
				if primary != previousPrimary {
					reading.ReceiveBytesPerSecond = 0
					reading.SendBytesPerSecond = 0
				}
				worker.publish(Result{Sample: reading})
			}
		}
		previous, have = current, true
		previousPrimary = primary
		previousCores = cores
		cores = nil
		pending = nil
		primary = ""
		return true
	})
	if worker.ctx.Err() != nil {
		return
	}
	switch {
	case fault != nil:
		err = fault
	case isClosed(stalled):
		err = ErrStalled
	case err == nil:
		err = ErrEnded
	}
	worker.publish(Result{Err: err})
}

func isClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// publish keeps the newest result in the one-slot channel. The worker is its
// only sender, so after dropping an unread result the send cannot block.
func (worker *Worker) publish(result Result) {
	for {
		select {
		case worker.results <- result:
			if worker.notify != nil {
				worker.notify()
			}
			return
		default:
		}
		select {
		case <-worker.results:
		default:
		}
	}
}
