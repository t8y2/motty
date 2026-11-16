package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
)

// fullscreenDesktopState is a state whose one page is a fullscreen desktop: the
// paint path the window uses, with no shell around the picture.
func fullscreenDesktopState(t testing.TB, session Session) *State {
	state := newTestState(t, testOptions(t))
	state.terminalShowLatency = true
	state.activeSection = sectionTerminal
	state.sessions = append(state.sessions, session)
	state.activeSessionID = session.ID
	state.winChrome.fullscreen = true
	state.winChrome.fullscreenSession = session.ID
	return state
}

func TestDesktopRTTDoesNotScheduleIdlePolling(t *testing.T) {
	source := &measuredDesktop{followingDesktop: newFollowingDesktop(1920, 1080, false), sampled: time.Now(), rtt: 73 * time.Millisecond}
	view := newDesktopView(source, false)
	defer view.close()
	view.fullscreen = true
	size := gfx.Size{Width: 1920, Height: 1080}
	state := fullscreenDesktopState(t, Session{ID: 1, Address: "desktop.example", desk: view, phase: sshconn.PhaseConnected})
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	canvas.Reset()
	state.Paint(&canvas, size)
	if delay, scheduled := canvas.NextFrame(); scheduled {
		t.Fatalf("idle RDP RTT scheduled a frame after %v", delay)
	}
}

func BenchmarkPaintDesktopFullscreen(b *testing.B) {
	source := &measuredDesktop{
		followingDesktop: newFollowingDesktop(1920, 1080, false),
		rtt:              73 * time.Millisecond,
		sampled:          time.Now(),
	}
	view := newDesktopView(source, false)
	defer view.close()
	view.fullscreen = true
	size := gfx.Size{Width: 1920, Height: 1080}
	state := fullscreenDesktopState(b, Session{ID: 1, Address: "desktop.example", desk: view, phase: sshconn.PhaseConnected})
	var canvas gfx.Canvas
	b.ReportAllocs()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}
