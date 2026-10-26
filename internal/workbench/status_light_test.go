package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

func TestTheLightFollowsTheSession(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	pal := state.pal
	fake := connect(t, state, world)

	for _, test := range []struct {
		phase sshconn.Phase
		kind  lightKind
		color ui.Color
	}{
		{sshconn.PhaseConnecting, lightBusy, pal.warning},
		{sshconn.PhaseHostKeyPrompt, lightBusy, pal.warning},
		{sshconn.PhaseAuthenticating, lightBusy, pal.warning},
		{sshconn.PhaseConnected, lightOn, pal.success},
		{sshconn.PhaseFailed, lightFault, pal.danger},
		{sshconn.PhaseClosed, lightIdle, pal.faint},
	} {
		fake.set(func(s *sshconn.Status) { s.Phase = test.phase })
		state.Wake()
		if test.phase == sshconn.PhaseConnected {
			fake.shell(t).say("$ ")
			state.Wake()
		}
		kind, color := state.sessionLight(&state.sessions[0])
		if kind != test.kind || color != test.color {
			t.Errorf("phase %v: light %v %+v, want %v %+v", test.phase, kind, color, test.kind, test.color)
		}
	}
	if kind, _ := state.hostLight(state.hosts[1]); kind != lightIdle {
		t.Fatalf("a host with no session shows light %v, want an idle ring", kind)
	}
}

func TestHostLightUsesTheBestStateAcrossItsTabs(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	failed := connect(t, state, world)
	failed.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseFailed })
	state.Wake()
	state.Click(hitHosts)
	connected := connect(t, state, world)
	connected.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	connected.shell(t).say("$ ")
	state.Wake()

	if kind, color := state.hostLight(state.hosts[0]); kind != lightOn || color != state.pal.success {
		t.Fatalf("host light = %v %+v, want connected", kind, color)
	}
	if label, _ := state.hostStatus(state.hosts[0]); label != state.messages.HostStateConnected {
		t.Fatalf("host status = %q", label)
	}
}

// haloOf finds the halo rectangle of the light painted at x, y.
func haloOf(canvas *ui.Canvas, x, y float32) (ui.Rectangle, bool) {
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Bounds.Width == lightHalo && rectangle.Bounds.X == x-lightHalo/2 && rectangle.Bounds.Y == y-lightHalo/2 {
			return rectangle, true
		}
	}
	return ui.Rectangle{}, false
}

func paintOneLight(kind lightKind, now time.Duration, reduceMotion bool) *ui.Canvas {
	var canvas ui.Canvas
	canvas.SetClock(func() time.Duration { return now })
	canvas.SetReduceMotion(reduceMotion)
	canvas.BeginFrame()
	paintLight(&canvas, 50, 50, kind, ui.Color{Green: 1, Alpha: 1}, ui.Color{Alpha: 1})
	canvas.EndFrame()
	return &canvas
}

func TestABusyLightBreathesAtItsOwnPace(t *testing.T) {
	low, _ := haloOf(paintOneLight(lightBusy, 0, false), 50, 50)
	high, _ := haloOf(paintOneLight(lightBusy, pulsePeriod/2, false), 50, 50)
	if !(high.Color.Alpha > low.Color.Alpha+0.2) {
		t.Fatalf("halo alpha %v at the start of the pulse and %v at its middle: it does not breathe", low.Color.Alpha, high.Color.Alpha)
	}
	canvas := paintOneLight(lightBusy, 0, false)
	if delay, ok := canvas.NextFrame(); !ok || delay != pulseStep || canvas.Animating() {
		t.Fatalf("a breathing light asks for its next frame after %v, %v (animating %v)", delay, ok, canvas.Animating())
	}
}

func TestLightsThatDoNotMoveAskForNoFrames(t *testing.T) {
	for name, canvas := range map[string]*ui.Canvas{
		"on":      paintOneLight(lightOn, 0, false),
		"waiting": paintOneLight(lightWaiting, 0, false),
		"fault":   paintOneLight(lightFault, 0, false),
		"idle":    paintOneLight(lightIdle, 0, false),
		"reduce":  paintOneLight(lightBusy, 0, true),
	} {
		if delay, ok := canvas.NextFrame(); ok {
			t.Errorf("a %s light asks for a frame in %v", name, delay)
		}
	}
	// With reduced motion a busy light is steady, not hidden.
	if _, ok := haloOf(paintOneLight(lightBusy, 0, true), 50, 50); !ok {
		t.Fatal("a busy light under reduced motion has no halo")
	}
	for _, kind := range []lightKind{lightIdle, lightOn, lightFault} {
		if _, ok := haloOf(paintOneLight(kind, 0, false), 50, 50); ok {
			t.Fatalf("a settled light %v has a halo", kind)
		}
	}
}
