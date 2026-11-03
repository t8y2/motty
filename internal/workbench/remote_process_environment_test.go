package workbench

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/remotetools"
	"motty/internal/sshconn"
)

func TestProcessDetailsAlignsSecondColumnAndShowsEnvironment(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	state.Click(hitRemoteRowList.At(0))
	if state.remote.processDetails.environmentLoaded || state.remote.processDetails.loading {
		t.Fatal("command tab read environment variables eagerly")
	}
	state.Click(hitRemoteProcessDetailsEnv)
	waitFor(t, state, func() bool { return !state.remote.processDetails.loading })
	if state.remote.processDetails.failed || len(state.remote.processDetails.environment) != 3 {
		t.Fatalf("environment=%+v status=%q", state.remote.processDetails.environment, state.remote.processDetails.status)
	}

	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	for _, value := range []string{
		state.messages.Tools.RemoteProcessEnvironment,
		state.messages.Tools.RemoteProcessEnvironmentName,
		state.messages.Tools.RemoteProcessEnvironmentValue,
		"LANG", "zh_CN.UTF-8", "PATH", "/usr/local/bin:/usr/bin", "SERVICE", "api=blue",
	} {
		if !remoteCanvasHasText(&canvas, value) {
			t.Errorf("process details lack %q", value)
		}
	}

	var secondColumnX float32
	for _, label := range []string{
		state.messages.Tools.RemoteProcessParentPID,
		state.messages.Tools.RemoteProcessState,
		state.messages.Tools.RemoteProcessMemory,
		state.messages.Tools.RemoteProcessVirtualMemory,
		state.messages.Tools.RemoteProcessCollectedAt,
	} {
		bounds, ok := remoteCanvasExactTextBounds(&canvas, label)
		if !ok {
			t.Errorf("second-column label %q is not painted", label)
			continue
		}
		if secondColumnX == 0 {
			secondColumnX = bounds.X
		} else if delta := bounds.X - secondColumnX; delta < -0.25 || delta > 0.25 {
			t.Errorf("second-column label %q starts at %.2f, want %.2f", label, bounds.X, secondColumnX)
		}
	}
}

func TestProcessEnvironmentReportsEmptyFailureAndUnsupportedStates(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		state, server, _ := openRemoteToolsDialog(t)
		server.environment = "\x00"
		state.Click(hitRemoteRowList.At(0))
		state.Click(hitRemoteProcessDetailsEnv)
		waitFor(t, state, func() bool { return !state.remote.processDetails.loading })
		var canvas ui.Canvas
		state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
		if !remoteCanvasHasText(&canvas, state.messages.Tools.RemoteProcessEnvironmentEmpty) {
			t.Fatal("empty environment state is not shown")
		}
	})

	t.Run("failure", func(t *testing.T) {
		state, server, _ := openRemoteToolsDialog(t)
		server.failures["environment"] = errors.New("exit status 1")
		state.Click(hitRemoteRowList.At(0))
		state.Click(hitRemoteProcessDetailsEnv)
		waitFor(t, state, func() bool { return !state.remote.processDetails.loading })
		var canvas ui.Canvas
		state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
		if !state.remote.processDetails.failed ||
			!remoteCanvasHasText(&canvas, state.remote.processDetails.status) ||
			!strings.Contains(state.remote.processDetails.status, "permission denied") {
			t.Fatalf("failure state=%+v", state.remote.processDetails)
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		state, _, _ := openRemoteToolsDialog(t)
		session, ok := state.sessionByIDPointer(state.remote.session)
		if !ok {
			t.Fatal("remote session is missing")
		}
		session.host.System = sshconn.SystemWindows
		state.Click(hitRemoteRowList.At(0))
		state.Click(hitRemoteProcessDetailsEnv)
		waitFor(t, state, func() bool { return !state.remote.processDetails.loading })
		if !state.remote.processDetails.failed ||
			state.remote.processDetails.status != state.messages.Tools.RemoteProcessEnvironmentUnsupported {
			t.Fatalf("unsupported state=%+v", state.remote.processDetails)
		}
	})
}

func TestRemoteProcessEnvironmentScrollsIndependently(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	state.Click(hitRemoteRowList.At(0))
	state.Click(hitRemoteProcessDetailsEnv)
	waitFor(t, state, func() bool { return !state.remote.processDetails.loading })
	state.remote.processDetails.environment = make([]remotetools.ProcessEnvironmentVariable, 80)
	for index := range state.remote.processDetails.environment {
		state.remote.processDetails.environment[index] = remotetools.ProcessEnvironmentVariable{
			Name: fmt.Sprintf("VARIABLE_%02d", index), Value: fmt.Sprintf("value-%02d", index),
		}
	}
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if state.remote.processDetails.environmentScrollBar.MaxOffsetY <= 0 {
		t.Fatal("long environment table has no independent scroll range")
	}
	bounds := state.remote.processDetails.environmentBounds
	before := state.remote.processDetails.environmentScroll
	result := state.Scroll(ui.ScrollEvent{
		X: bounds.X + bounds.Width/2, Y: bounds.Y + min(bounds.Height/2, 40),
		DeltaY: 20, Precise: true, Viewport: ui.Size{Width: 1400, Height: 900},
	})
	if !result.Handled || state.remote.processDetails.environmentScroll <= before {
		t.Fatalf("environment wheel did not move independently: handled=%v before=%g after=%g",
			result.Handled, before, state.remote.processDetails.environmentScroll)
	}
}

func TestCommandTabDoesNotScrollTheHiddenEnvironmentTable(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	state.Click(hitRemoteRowList.At(0))
	state.Click(hitRemoteProcessDetailsEnv)
	waitFor(t, state, func() bool { return !state.remote.processDetails.loading })
	state.remote.processDetails.environment = make([]remotetools.ProcessEnvironmentVariable, 80)
	for index := range state.remote.processDetails.environment {
		state.remote.processDetails.environment[index] = remotetools.ProcessEnvironmentVariable{
			Name: fmt.Sprintf("VARIABLE_%02d", index), Value: fmt.Sprintf("value-%02d", index),
		}
	}

	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	bounds := state.remote.processDetails.environmentBounds
	before := state.remote.processDetails.environmentScroll
	state.Click(hitRemoteProcessDetailsCommand)
	result := state.Scroll(ui.ScrollEvent{
		X: bounds.X + bounds.Width/2, Y: bounds.Y + min(bounds.Height/2, 40),
		DeltaY: 20, Precise: true, Viewport: ui.Size{Width: 1400, Height: 900},
	})
	if !result.Handled || state.remote.processDetails.environmentScroll != before {
		t.Fatalf("hidden environment table moved: handled=%v before=%g after=%g",
			result.Handled, before, state.remote.processDetails.environmentScroll)
	}
}

func TestReopeningToolsDiscardsEnvironmentFromThePreviousSurface(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	session, ok := state.sessionByIDPointer(state.remote.session)
	if !ok {
		t.Fatal("remote session is missing")
	}
	state.Click(hitRemoteRowList.At(0))
	oldDetails := state.remote.processDetails
	cancelled := false
	state.remote.processDetails.environmentCancel = func() { cancelled = true }

	state.closeRemoteToolsSurface()
	if !cancelled || state.remote.processDetails.open || state.modal != modalNone {
		t.Fatalf("closing tools did not dispose the request: cancelled=%v details=%+v modal=%d",
			cancelled, state.remote.processDetails, state.modal)
	}

	state.openRemoteToolsTab(session, remoteProcesses, false)
	waitFor(t, state, func() bool { return !state.remote.loading })
	state.Click(hitRemoteRowList.At(0))
	current := state.remote.processDetails
	if !current.open || current.serial == oldDetails.serial {
		t.Fatalf("reopened details reused generation %d", current.serial)
	}

	state.remote.processDetails.environment = []remotetools.ProcessEnvironmentVariable{{
		Name: "CURRENT", Value: "keep",
	}}
	state.remote.processDetails.environmentRequest = 1
	state.remote.processDetails.loading = true
	state.applyRemoteProcessEnvironment(
		oldDetails.serial, 1, session.ID, oldDetails.process,
		[]remotetools.ProcessEnvironmentVariable{{Name: "STALE", Value: "discard"}}, nil,
	)
	if !state.remote.processDetails.loading ||
		len(state.remote.processDetails.environment) != 1 ||
		state.remote.processDetails.environment[0].Name != "CURRENT" {
		t.Fatalf("stale request changed the reopened details: %+v", state.remote.processDetails)
	}
}

func remoteCanvasExactTextBounds(canvas *ui.Canvas, value string) (ui.Rect, bool) {
	for _, painted := range canvas.Texts() {
		if painted.Value == value {
			return painted.Bounds, true
		}
	}
	return ui.Rect{}, false
}
