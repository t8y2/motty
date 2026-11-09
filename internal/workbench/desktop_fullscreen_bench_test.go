package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
)

func TestDesktopRTTDoesNotScheduleIdlePolling(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.terminalShowLatency = true
	source := &measuredDesktop{followingDesktop: newFollowingDesktop(1920, 1080, false), sampled: time.Now(), rtt: 73 * time.Millisecond}
	view := newDesktopView(source, false)
	defer view.close()
	view.fullscreen = true
	session := &Session{ID: 1, Address: "desktop.example", desk: view, phase: sshconn.PhaseConnected}
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1920, Height: 1080}
	state.paintDesktopPage(&canvas, size, session)
	canvas.Reset()
	state.paintDesktopPage(&canvas, size, session)
	if delay, scheduled := canvas.NextFrame(); scheduled {
		t.Fatalf("idle RDP RTT scheduled a frame after %v", delay)
	}
}

func BenchmarkPaintDesktopFullscreen(b *testing.B) {
	state := newTestState(b, testOptions(b))
	state.terminalShowLatency = true
	source := &measuredDesktop{
		followingDesktop: newFollowingDesktop(1920, 1080, false),
		rtt:              73 * time.Millisecond,
		sampled:          time.Now(),
	}
	view := newDesktopView(source, false)
	defer view.close()
	view.fullscreen = true
	session := &Session{ID: 1, Address: "desktop.example", desk: view, phase: sshconn.PhaseConnected}
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1920, Height: 1080}
	b.ReportAllocs()
	for b.Loop() {
		canvas.Reset()
		state.paintDesktopPage(&canvas, size, session)
	}
}
