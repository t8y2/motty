package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

func TestTerminalStatusFollowsShellStartup(test *testing.T) {
	state, connection := connectSlow(test)
	defer func() {
		select {
		case <-connection.release:
		default:
			close(connection.release)
		}
	}()
	state.Wake()
	session := &state.sessions[0]
	label, _ := state.sessionStatus(session)
	if label != state.messages.ConnDetailShellOpening {
		test.Fatalf("pending shell status = %q", label)
	}
	if kind, _ := state.sessionLight(session); kind == lightOn {
		test.Fatal("the tab reports success before a shell has opened")
	}
	close(connection.release)
	deadline := time.Now().Add(2 * time.Second)
	for session.term.shell == nil && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	if session.term.shell == nil {
		test.Fatal("the shell never opened")
	}
	label, _ = state.sessionStatus(session)
	if label != state.messages.ConnDetailShellWaiting {
		test.Fatalf("silent shell status = %q", label)
	}
	if kind, _ := state.sessionLight(session); kind == lightOn {
		test.Fatal("the tab reports readiness before any shell output")
	}
	session.HostID = localHostID
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitConnProgress); ok {
		test.Fatal("a local shell offers a network connection progress page")
	}
	if _, ok := hitByID(&canvas, terminalHit(state, 0)); !ok {
		test.Fatal("a silent local shell cannot accept input")
	}
	if _, scheduled := canvas.NextFrame(); scheduled {
		test.Fatal("waiting for shell output scheduled idle redraws")
	}
	connection.fakeConn.shell(test).say("$ ")
	state.Wake()
	label, _ = state.sessionStatus(session)
	if label != state.messages.HostStateConnected {
		test.Fatalf("live shell status = %q", label)
	}
	if kind, _ := state.sessionLight(session); kind != lightOn {
		test.Fatal("the live shell does not report readiness")
	}
}

func BenchmarkShellStartupPaint(bench *testing.B) {
	for _, mode := range []string{"waiting-output", "live"} {
		bench.Run(mode, func(bench *testing.B) {
			world, options := newWorld(bench)
			state := newTestState(bench, options)
			connection := connect(bench, state, world)
			connection.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
			state.Wake()
			state.sessions[0].HostID = localHostID
			if mode == "live" {
				connection.shell(bench).say("$ ")
				state.Wake()
			}
			var canvas ui.Canvas
			size := ui.Size{Width: 1400, Height: 900}
			state.Paint(&canvas, size)
			bench.ReportAllocs()
			bench.ResetTimer()
			for bench.Loop() {
				canvas.Reset()
				state.Paint(&canvas, size)
			}
		})
	}
}
