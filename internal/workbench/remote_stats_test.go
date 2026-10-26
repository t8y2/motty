package workbench

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/remotestats"
)

// Two readings a second apart from a server 37.5% busy that uses 3 of 8 GiB and
// moves 128 KiB/s in and 64 KiB/s out.
var statsReadings = []string{
	"gop-stats-v2 375 1000 131072 65536 8388608 5242880 1.00\n",
	"gop-stats-v2 750 2000 262144 131072 8388608 5242880 2.00\n",
}

// streamReadings is a monitor command that prints the readings and then stays
// open, as the real one does, until it is stopped.
func streamReadings(lines ...string) func(context.Context, string, int, func([]byte) bool) error {
	return func(ctx context.Context, _ string, _ int, line func([]byte) bool) error {
		for _, text := range lines {
			if !line([]byte(text)) {
				return nil
			}
		}
		<-ctx.Done()
		return ctx.Err()
	}
}

func waitRemoteStats(t testing.TB, state *State, ready func(*remoteStatsView) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		session, ok := state.sessionByIDPointer(state.activeSessionID)
		if ok && session.stats != nil && ready(session.stats) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	t.Fatalf("remote stats did not settle: %+v", session.stats)
}

func TestRemoteStatsRunOnlyAfterExplicitOptInAndStopCleanly(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	stream := streamReadings(statsReadings...)
	fake.streamCommand = func(ctx context.Context, command string, limit int, line func([]byte) bool) error {
		if !strings.Contains(command, "# motty-stats-v2") {
			t.Errorf("unexpected monitor command %q", command)
		}
		return stream(ctx, command, limit, line)
	}
	state.Wake()
	state.Paint(&ui.Canvas{}, ui.Size{Width: 1400, Height: 900})
	fake.mu.Lock()
	before := len(fake.commandCalls)
	fake.mu.Unlock()
	if before != 0 {
		t.Fatalf("an idle connection ran %d resource commands", before)
	}

	state.Click(hitRemoteStats)
	waitRemoteStats(t, state, func(view *remoteStatsView) bool { return view.state == remoteStatsReady })
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	if got := session.stats.text; got != "CPU 38%  RAM 3.0/8.0G  ↓128.0K/s ↑64.0K/s" {
		t.Fatalf("readout = %q", got)
	}
	worker := session.stats.worker
	state.Click(hitRemoteStats)
	if session.stats != nil {
		t.Fatal("stopping monitoring kept its session state")
	}
	select {
	case <-worker.Done():
	case <-time.After(time.Second):
		t.Fatal("monitor worker did not stop")
	}
}

func TestRemoteStatsFailureStopsUntilTheUserRetries(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	var calls atomic.Int32
	unsupported, ready := streamReadings("gop-stats-unsupported Darwin\n"), streamReadings(statsReadings...)
	fake.streamCommand = func(ctx context.Context, command string, limit int, line func([]byte) bool) error {
		if calls.Add(1) == 1 {
			return unsupported(ctx, command, limit, line)
		}
		return ready(ctx, command, limit, line)
	}
	state.Click(hitRemoteStats)
	waitRemoteStats(t, state, func(view *remoteStatsView) bool {
		return view.state == remoteStatsUnsupported && view.worker == nil
	})
	time.Sleep(5 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("unsupported monitor retried %d times", calls.Load())
	}

	state.Click(hitRemoteStats)
	waitRemoteStats(t, state, func(view *remoteStatsView) bool { return view.state == remoteStatsReady })
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.stopRemoteStats(session)
	if calls.Load() != 2 {
		t.Fatalf("retry ran %d commands", calls.Load())
	}
}

func TestClosingSessionCancelsRemoteStats(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	started := make(chan struct{})
	canceled := make(chan struct{})
	fake.streamCommand = func(ctx context.Context, _ string, _ int, _ func([]byte) bool) error {
		close(started)
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	}
	state.Click(hitRemoteStats)
	<-started
	id := state.activeSessionID
	state.closeSession(id)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("closing the tab did not cancel resource sampling")
	}
}

// The reading sits at the left of the bar, after the connection's light, one
// colored piece per metric, and never reaches the tools on the right.
func TestRemoteStatsReadoutSitsLeftInDistinctColors(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	sample := remotestats.Sample{
		CPUPercent: 37.5, MemoryUsedBytes: 3 << 30, MemoryTotalBytes: 8 << 30,
		ReceiveBytesPerSecond: 128 << 10, SendBytesPerSecond: 64 << 10,
	}
	session.stats = &remoteStatsView{state: remoteStatsReady, text: formatRemoteStats(sample), parts: remoteStatsParts(sample)}
	for _, size := range []ui.Size{{Width: 1400, Height: 900}, {Width: 760, Height: 500}} {
		var canvas ui.Canvas
		state.Paint(&canvas, size)
		splits, ok := hitByID(&canvas, hitPaneSplitHorizontal)
		if !ok {
			t.Fatalf("at %v no split button", size)
		}
		colors := map[ui.Color]bool{}
		previous := float32(0)
		for _, part := range session.stats.parts {
			var found *ui.Text
			for index, text := range canvas.Texts() {
				if text.Value == part {
					found = &canvas.Texts()[index]
				}
			}
			if found == nil {
				t.Fatalf("at %v the piece %q was not painted", size, part)
			}
			if found.Bounds.X < previous || found.Bounds.X+found.Bounds.Width > splits.Bounds.X {
				t.Fatalf("at %v piece %q at %+v is out of order or under the tools (%+v)", size, part, found.Bounds, splits.Bounds)
			}
			previous = found.Bounds.X + found.Bounds.Width
			colors[found.Style.Color] = true
		}
		if len(colors) != int(statsMetricCount) {
			t.Fatalf("at %v the %d metrics use %d colors", size, statsMetricCount, len(colors))
		}
	}
}

func TestPaintingRemoteStatsAllocatesNothingOnceWarm(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	warm := remotestats.Sample{
		CPUPercent: 37.5, MemoryUsedBytes: 3 << 30, MemoryTotalBytes: 8 << 30,
		ReceiveBytesPerSecond: 128 << 10, SendBytesPerSecond: 64 << 10,
	}
	session.stats = &remoteStatsView{state: remoteStatsReady, text: formatRemoteStats(warm), parts: remoteStatsParts(warm)}
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	paint := func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
	paint()
	if allocations := testing.AllocsPerRun(100, paint); allocations != 0 {
		t.Fatalf("painting remote stats allocated %v times", allocations)
	}
}

func TestRemoteStatsCommandErrorsAreNotExposedToTheInfoBar(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	fake.streamCommand = func(context.Context, string, int, func([]byte) bool) error {
		return errors.New("server secret: command denied")
	}
	state.Click(hitRemoteStats)
	waitRemoteStats(t, state, func(view *remoteStatsView) bool { return view.state == remoteStatsFailed })
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	if got := state.remoteStatsText(session.stats); got != state.messages.StatsUnavailable || strings.Contains(got, "secret") {
		t.Fatalf("info bar = %q", got)
	}
}

func BenchmarkPaintRemoteStats(b *testing.B) {
	state, _, _, _ := liveSession(&testing.T{})
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	session.stats = &remoteStatsView{state: remoteStatsReady, text: "CPU 38%  RAM 3.0/8.0G  ↓128.0K/s ↑64.0K/s"}
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

// A server with GPUs gets one extra piece per GPU after the four metrics; one
// without shows none and costs nothing more.
func TestRemoteStatsShowGPUPiecesOnlyWhenTheServerHasGPUs(t *testing.T) {
	gpuPieces := func(lines ...string) []string {
		state, _, fake, _ := liveSession(t)
		fake.streamCommand = streamReadings(lines...)
		state.Click(hitRemoteStats)
		waitRemoteStats(t, state, func(view *remoteStatsView) bool { return view.state == remoteStatsReady })
		var canvas ui.Canvas
		state.Paint(&canvas, ui.Size{Width: 1600, Height: 900})
		var pieces []string
		for _, text := range canvas.Texts() {
			if strings.HasPrefix(text.Value, "GPU") {
				pieces = append(pieces, text.Value)
			}
		}
		return pieces
	}
	if pieces := gpuPieces(statsReadings...); len(pieces) != 0 {
		t.Fatalf("a server without GPUs showed %q", pieces)
	}
	pieces := gpuPieces(statsReadings[0],
		"gop-gpu-nvidia 0, NVIDIA GeForce RTX 4090, 87, 20481, 24564, 71, 312.45, 64\n",
		"gop-gpu-nvidia 1, NVIDIA GeForce RTX 3060, [N/A], 6144, 12288, [N/A], [N/A], [N/A]\n",
		statsReadings[1])
	want := []string{"GPU0 87% 20.0/24.0G 71°C 312W", "GPU1 6.0/12.0G"}
	if strings.Join(pieces, "|") != strings.Join(want, "|") {
		t.Fatalf("GPU pieces = %q, want %q", pieces, want)
	}
}
