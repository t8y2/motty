package workbench

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

// clockedWorld is a world whose clock the test moves.
type clockedWorld struct {
	*world
	now *time.Duration
}

func newClockedWorld(t testing.TB) (clockedWorld, Options) {
	known, options := newWorld(t)
	var now time.Duration
	options.Now = func() time.Duration { return now }
	return clockedWorld{world: known, now: &now}, options
}

func steps(session *Session) [stageCount]stepState {
	var states [stageCount]stepState
	for stage := range states {
		states[stage] = session.stepOf(stage)
	}
	return states
}

func TestTheConnectionPageFollowsTheConnectionStageByStage(t *testing.T) {
	changed := sshconn.HostKeyInfo{Address: "prod.example.com:22", Fingerprint: "SHA256:NEW", Known: []string{"SHA256:OLD"}}
	for _, test := range []struct {
		name     string
		statuses []sshconn.Status
		want     [stageCount]stepState
	}{
		{"dialing", nil, [stageCount]stepState{stepActive, stepPending, stepPending, stepPending}},
		{"asking about a new host key",
			[]sshconn.Status{{Phase: sshconn.PhaseHostKeyPrompt, HostKey: testHostKey}},
			[stageCount]stepState{stepDone, stepWaiting, stepPending, stepPending}},
		{"trying keys",
			[]sshconn.Status{{Phase: sshconn.PhaseAuthenticating}},
			[stageCount]stepState{stepDone, stepDone, stepActive, stepPending}},
		{"asking for a password",
			[]sshconn.Status{{Phase: sshconn.PhasePasswordPrompt, Attempt: 1}},
			[stageCount]stepState{stepDone, stepDone, stepWaiting, stepPending}},
		{"opening the shell",
			[]sshconn.Status{{Phase: sshconn.PhaseConnected, Method: "password"}},
			[stageCount]stepState{stepDone, stepDone, stepDone, stepActive}},
		{"the user rejected the key",
			[]sshconn.Status{{Phase: sshconn.PhaseHostKeyPrompt, HostKey: testHostKey}, {Phase: sshconn.PhaseFailed, Err: sshconn.ErrHostKeyRejected}},
			[stageCount]stepState{stepDone, stepFailed, stepPending, stepPending}},
		{"the key changed",
			[]sshconn.Status{{Phase: sshconn.PhaseFailed, Err: sshconn.ErrHostKeyChanged, HostKey: changed}},
			[stageCount]stepState{stepDone, stepFailed, stepPending, stepPending}},
		{"the credentials were refused",
			[]sshconn.Status{{Phase: sshconn.PhaseAuthenticating}, {Phase: sshconn.PhaseFailed, Err: sshconn.ErrAuthFailed}},
			[stageCount]stepState{stepDone, stepDone, stepFailed, stepPending}},
		{"the server was unreachable",
			[]sshconn.Status{{Phase: sshconn.PhaseFailed, Err: errors.New("connect to x:22: i/o timeout")}},
			[stageCount]stepState{stepFailed, stepPending, stepPending, stepPending}},
	} {
		t.Run(test.name, func(t *testing.T) {
			world, options := newWorld(t)
			state := newTestState(t, options)
			fake := connect(t, state, world)
			for _, status := range test.statuses {
				fake.set(func(s *sshconn.Status) { version := s.Version; *s, s.Version = status, version })
				state.Wake()
			}
			if got := steps(&state.sessions[0]); got != test.want {
				t.Fatalf("rows are %v, want %v", got, test.want)
			}
			if !state.showsProgress(&state.sessions[0]) {
				t.Fatal("the connection page is not up")
			}
		})
	}
}

func TestAShellThatCannotBeOpenedFailsTheLastRow(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.shellErr = errors.New("channel open refused")
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected; s.Method = "password" })
	state.Wake()
	session := &state.sessions[0]
	if want := [stageCount]stepState{stepDone, stepDone, stepDone, stepFailed}; steps(session) != want {
		t.Fatalf("rows are %v, want %v", steps(session), want)
	}
	if !strings.Contains(session.failure, "channel open refused") {
		t.Fatalf("failure line = %q", session.failure)
	}
	if !state.showsProgress(session) || !state.canReconnect(session) {
		t.Fatal("the page does not offer another try")
	}
}

func TestThePageSaysWhatEachRowIsDoing(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	session := &state.sessions[0]
	expect := func(stage int, want string) {
		t.Helper()
		if got := session.details[stage]; !strings.Contains(got, want) {
			t.Fatalf("row %d says %q, want it to contain %q", stage, got, want)
		}
	}
	expect(stageConnect, "Waiting for the server") // the address is in the header, not repeated here

	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseHostKeyPrompt; s.HostKey = testHostKey })
	state.Wake()
	expect(stageHostKey, "SHA256:abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG")
	expect(stageHostKey, "ssh-ed25519")

	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseAuthenticating })
	state.Wake()
	expect(stageHostKey, "ssh-ed25519") // the key the user just trusted
	expect(stageLogin, "ssh-agent")

	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhasePasswordPrompt; s.Attempt = 1 })
	state.Wake()
	expect(stageLogin, "password")

	fake.set(func(s *sshconn.Status) {
		s.Phase, s.Method, s.ServerVersion = sshconn.PhaseConnected, "password", "SSH-2.0-OpenSSH_9.7"
	})
	state.Wake()
	expect(stageConnect, "SSH-2.0-OpenSSH_9.7")
	expect(stageLogin, "password")
	expect(stageShell, "first output")
}

func TestAHostAlreadyInKnownHostsIsSaidToMatch(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseAuthenticating }) // no prompt was needed
	state.Wake()
	if got := state.sessions[0].details[stageHostKey]; !strings.Contains(got, "known_hosts") {
		t.Fatalf("host key row says %q", got)
	}
}

func TestTheTerminalTakesOverWhenTheShellFirstSpeaks(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	session := &state.sessions[0]

	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitConnLog); !ok {
		t.Fatal("no connection page while the shell is silent")
	}
	if _, ok := hitByID(canvas, terminalHit(state, 0)); ok {
		t.Fatal("the terminal is painted behind the connection page")
	}

	fake.shell(t).say("welcome\r\n")
	state.Wake()
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, hitConnLog); ok {
		t.Fatal("the connection page stayed up after the shell spoke")
	}
	if hit, ok := hitByID(canvas, terminalHit(state, 0)); !ok || !hit.Autofocus {
		t.Fatalf("terminal hit = %+v, %v; want it painted and taking the keyboard", hit, ok)
	}
	if session.finished == 0 {
		t.Fatal("the elapsed time kept running")
	}
	// The page does not come back by itself, whatever happens to the session.
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseClosed })
	state.Wake()
	if state.showsProgress(session) {
		t.Fatal("the page came back when the session ended")
	}
}

func TestASilentShellIsGivenUpOnAfterAMoment(t *testing.T) {
	world, options := newClockedWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world.world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	*world.now = 4 * time.Second
	state.Wake()
	session := &state.sessions[0]

	*world.now += shellGrace - time.Millisecond
	if !state.showsProgress(session) {
		t.Fatal("the page gave up before the grace period")
	}
	canvas, _ := paintedTexts(state)
	if delay, ok := canvas.NextFrame(); !ok || delay > time.Millisecond+pulseStep {
		t.Fatalf("the page asks for its next frame after %v, %v; it has to come back when the grace ends", delay, ok)
	}
	*world.now += time.Millisecond
	if state.showsProgress(session) {
		t.Fatal("a silent shell kept the user away from its keyboard")
	}
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, terminalHit(state, 0)); !ok {
		t.Fatal("the terminal is not painted once the page gave up")
	}
}

func TestAFailedConnectionOffersAnotherTryAndTheLog(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseFailed; s.Err = sshconn.ErrAuthFailed })
	state.Wake()

	canvas, texts := paintedTexts(state)
	for _, id := range []ui.ID{hitConnLog, hitConnClose, hitConnRetry} {
		if _, ok := hitByID(canvas, id); !ok {
			t.Fatalf("button %d is missing from the failed page", id)
		}
	}
	if !contains(texts, "Authentication failed.") {
		t.Fatalf("the page does not say why: %q", texts)
	}

	// The log is the terminal's screen, with the way back in its info bar.
	state.Click(hitConnLog)
	canvas, texts = paintedTexts(state)
	if _, ok := hitByID(canvas, terminalHit(state, 0)); !ok || !contains(texts, "Press Enter to connect again.") {
		t.Fatalf("the log is not up: %q", texts)
	}
	if _, ok := hitByID(canvas, hitConnProgress); !ok {
		t.Fatal("the log has no way back to the connection page")
	}
	state.Click(hitConnProgress)
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, hitConnRetry); !ok {
		t.Fatal("the way back did not return to the connection page")
	}

	// Try again dials once more, in the same tab, and starts the page over.
	dialed := len(world.conns)
	state.Click(hitConnRetry)
	if len(world.conns) != dialed+1 || len(state.sessions) != 1 {
		t.Fatalf("retry dialed %d new connections and left %d tabs", len(world.conns)-dialed, len(state.sessions))
	}
	session := &state.sessions[0]
	if want := [stageCount]stepState{stepActive, stepPending, stepPending, stepPending}; steps(session) != want || session.failure != "" {
		t.Fatalf("after retrying the rows are %v and the failure %q", steps(session), session.failure)
	}
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, hitConnRetry); ok {
		t.Fatal("a connection being made offers a retry")
	}
}

func TestClosingFromTheConnectionPageCancelsAndRemovesTheTab(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	state.Click(hitConnClose)
	if len(state.sessions) != 0 || fake.canceled == 0 || state.activeSection != sectionHosts {
		t.Fatalf("tabs=%d canceled=%d section=%d", len(state.sessions), fake.canceled, state.activeSection)
	}
}

func TestElapsedTimeCountsHonestlyAndStopsWhenTheConnectionEnds(t *testing.T) {
	world, options := newClockedWorld(t)
	state := newTestState(t, options)
	*world.now = 100 * time.Second // the window has been open a while
	fake := connect(t, state, world.world)

	*world.now += 7400 * time.Millisecond
	canvas, texts := paintedTexts(state)
	if !contains(texts, "Elapsed 0:07") {
		t.Fatalf("texts: %q", texts)
	}
	// It asks to be painted again as the next second begins, no sooner.
	if delay, ok := canvas.NextFrame(); !ok || delay > 600*time.Millisecond {
		t.Fatalf("next frame after %v, %v; the counter must tick at 8s", delay, ok)
	}

	*world.now += 2 * time.Second
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseFailed; s.Err = errors.New("boom") })
	state.Wake()
	*world.now += time.Hour
	canvas, texts = paintedTexts(state)
	if !contains(texts, "Elapsed 0:09") {
		t.Fatalf("a failed connection's time kept running: %q", texts)
	}
	if delay, ok := canvas.NextFrame(); ok {
		t.Fatalf("a failed connection keeps asking for frames (next in %v)", delay)
	}
	if got := clockText(3725); got != "1:02:05" {
		t.Fatalf("an hour and more reads %q", got)
	}
}

// Whatever the stage, language, theme, window size or host name, nothing on
// the page spills out of its card, and nothing overlaps the buttons.
func TestTheConnectionPageStaysInsideItsCard(t *testing.T) {
	statuses := []sshconn.Status{
		{},
		{Phase: sshconn.PhaseHostKeyPrompt, HostKey: testHostKey},
		{Phase: sshconn.PhasePasswordPrompt, Attempt: 1},
		{Phase: sshconn.PhaseFailed, Err: errors.New("connect to a-very-long-host-name.example.com:22: dial tcp 203.0.113.9:22: i/o timeout after waiting a long while")},
		{Phase: sshconn.PhaseFailed, Err: sshconn.ErrHostKeyChanged},
	}
	for _, status := range statuses {
		for _, size := range []ui.Size{{Width: 1400, Height: 900}, {Width: 640, Height: 480}, {Width: 360, Height: 420}} {
			for _, dark := range []bool{true, false} {
				world, options := newWorld(t)
				state := newTestState(t, options)
				if !dark {
					state.Click(hitTheme)
				}
				state.Click(hitLanguage)
				fake := connect(t, state, world)
				state.sessions[0].Title = "Production database primary replica in the Frankfurt datacenter"
				fake.set(func(s *sshconn.Status) { *s = status })
				state.Wake()
				var canvas ui.Canvas
				state.Paint(&canvas, size)

				buttons, ok := hitByID(&canvas, hitConnClose)
				if !ok {
					t.Fatalf("%+v at %v: no close button", status, size)
				}
				for _, text := range canvas.Texts() {
					// Where the window bar's tabs scroll under a clip, text outside the
					// clip is not drawn; only what shows has to be inside the window.
					shown := text.Bounds
					if text.HasClip {
						shown = intersectRect(shown, text.ClipOf)
					}
					if shown.Width > 0 && (shown.X+shown.Width > size.Width+0.5 || shown.X < -0.5) {
						t.Errorf("%+v at %v: %q at %+v leaves the window", status.Phase, size, text.Value, text.Bounds)
					}
					if size.Width >= 640 && text.Layer == buttons.Layer && text.Bounds.Y < buttons.Bounds.Y+buttons.Bounds.Height && text.Bounds.Y+text.Bounds.Height > buttons.Bounds.Y &&
						text.Bounds.X+text.Bounds.Width > buttons.Bounds.X && text.Bounds.X < buttons.Bounds.X+buttons.Bounds.Width &&
						!strings.Contains(text.Value, "日志") && !strings.Contains(text.Value, "关闭") && !strings.Contains(text.Value, "重试") {
						t.Errorf("%+v at %v: %q at %+v overlaps the buttons at %+v", status.Phase, size, text.Value, text.Bounds, buttons.Bounds)
					}
				}
			}
		}
	}
}

func TestPaintingTheConnectionPageAllocatesNothingOnceWarm(t *testing.T) {
	world, options := newWorld(t)
	options.Now = func() time.Duration { return 3 * time.Second }
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhasePasswordPrompt; s.Attempt = 1 })
	state.Wake()
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	paint := func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
	paint()
	if allocations := testing.AllocsPerRun(100, paint); allocations != 0 {
		t.Fatalf("painting the connection page allocated %v times", allocations)
	}
}

// The ball stays at the start of the rail for as long as the connection is
// being made, and goes to the end only when it is made.
func TestTheConnectionBallWaitsAtTheStartUntilConnected(t *testing.T) {
	var now time.Duration
	world, options := newWorld(t)
	options.Now = func() time.Duration { return now }
	state := newTestState(t, options)
	fake := connect(t, state, world)
	session, _ := state.sessionByIDPointer(state.activeSessionID)

	var canvas ui.Canvas
	canvas.SetClock(func() time.Duration { return now })
	paint := func(frames int) {
		for range frames {
			now += 16 * time.Millisecond
			canvas.Reset()
			state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
		}
	}
	paint(300)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhasePasswordPrompt; s.Attempt = 1 })
	state.Wake()
	paint(300)
	if session.railShown != 0 {
		t.Fatalf("after ten seconds of connecting the ball is at %.3f, want it still at the start", session.railShown)
	}
	if connectionRailTarget(stepDone) != 1 {
		t.Fatal("a connected session does not send the ball to the end")
	}
}

// The info bar's right edge holds, from the right, the files button, the state
// in words, its light and, while the log stands in for the connection page, the
// way back. None of them may sit on another.
func TestTheInfoBarsRightEdgeHasRoomForEverything(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseFailed; s.Err = sshconn.ErrAuthFailed })
	state.Wake()
	state.Click(hitConnLog)
	for _, size := range []ui.Size{{Width: 1400, Height: 900}, {Width: 760, Height: 500}} {
		var canvas ui.Canvas
		state.Paint(&canvas, size)
		var spans []ui.Rect
		files, ok := hitByID(&canvas, hitSFTPOpen)
		if !ok {
			t.Fatal("no files button")
		}
		back, ok := hitByID(&canvas, hitConnProgress)
		if !ok {
			t.Fatal("no way back to the connection page")
		}
		spans = append(spans, files.Bounds, back.Bounds)
		for _, text := range canvas.Texts() {
			if text.Value == state.messages.HostStateFailed {
				spans = append(spans, text.Bounds)
			}
		}
		if len(spans) != 3 {
			t.Fatalf("found %d of the info bar's right-edge items", len(spans))
		}
		for first := range spans {
			for second := first + 1; second < len(spans); second++ {
				a, b := spans[first], spans[second]
				if a.X < b.X+b.Width && b.X < a.X+a.Width {
					t.Errorf("at %v: %+v overlaps %+v", size, a, b)
				}
			}
		}
	}
}

func TestALongFailureWrapsInsteadOfBeingCutOff(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	reason := "connect to prod.example.com:22: dial tcp 203.0.113.9:22: connect: network is unreachable while trying every address"
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseFailed; s.Err = errors.New(reason) })
	state.Wake()
	canvas, texts := paintedTexts(state)
	// The message is cut into lines, so look for its pieces.
	joined := strings.Join(texts, " ")
	for _, part := range []string{"connect to prod.example.com:22", "203.0.113.9:22", "unreachable"} {
		if !strings.Contains(joined, part) {
			t.Errorf("the failure is missing %q: %q", part, texts)
		}
	}
	if hit, _ := hitByID(canvas, hitConnClose); hit.Bounds.Y+hit.Bounds.Height > 900 {
		t.Errorf("a long failure pushed the buttons off the window: %+v", hit.Bounds)
	}
}

func TestAFailedRowStopsSayingItIsConnecting(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	session := &state.sessions[0]
	if !strings.Contains(session.details[stageConnect], "Waiting for the server") {
		t.Fatalf("while dialing the row says %q", session.details[stageConnect])
	}
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseFailed; s.Err = errors.New("i/o timeout") })
	state.Wake()
	if got := session.details[stageConnect]; strings.Contains(got, "Connecting") || !strings.Contains(got, "prod.example.com:22") {
		t.Fatalf("after failing the row says %q", got)
	}
}

func TestTheRailFlowsWhileConnectingAndStillsWhenMotionIsReduced(t *testing.T) {
	track := ui.Rect{X: 0, Y: 0, Width: 400, Height: 3}
	frame := func(at time.Duration, reduced bool) []ui.Rectangle {
		var canvas ui.Canvas
		canvas.SetReduceMotion(reduced)
		canvas.SetClock(func() time.Duration { return at })
		canvas.BeginFrame()
		paintConnectionFlow(&canvas, track, ui.Color{Alpha: 1})
		return append([]ui.Rectangle(nil), canvas.Rectangles()...)
	}
	early, later := frame(100*time.Millisecond, false), frame(500*time.Millisecond, false)
	if len(early) == 0 || reflect.DeepEqual(early, later) {
		t.Fatalf("the streaks do not move: %d rectangles, equal=%v", len(early), reflect.DeepEqual(early, later))
	}
	for _, r := range early {
		if r.Bounds.X < track.X || r.Bounds.X+r.Bounds.Width > track.X+track.Width+0.01 {
			t.Fatalf("a streak leaves the rail: %+v", r.Bounds)
		}
	}
	// There is one streak at a time: its slices run on without a gap.
	mid := frame(1500*time.Millisecond, false)
	for i := 1; i < len(mid); i++ {
		if gap := mid[i-1].Bounds.X - (mid[i].Bounds.X + mid[i].Bounds.Width); gap < -0.01 || gap > 0.01 {
			t.Fatalf("two streaks at once: a gap of %v between slices", gap)
		}
	}
	if still := frame(100*time.Millisecond, true); len(still) != 0 {
		t.Errorf("reduced motion still draws %d streaks", len(still))
	}
}
