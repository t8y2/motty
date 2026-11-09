package workbench

import (
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

func openContainerDetailsForTest(t *testing.T) (*State, *remoteServer) {
	t.Helper()
	state, server, _ := openRemoteToolsDialog(t)
	state.Click(hitRemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 2 })
	selectRemoteToolsRow(state, 0)
	state.Click(hitRemoteContainerDetails)
	if !state.remote.containerDetails.open || state.remote.containerDetails.container.Name != "web" {
		t.Fatalf("container details did not open: %+v", state.remote.containerDetails)
	}
	return state, server
}

func TestContainerDetailsLoadsSelectableEnvironment(t *testing.T) {
	state, server := openContainerDetailsForTest(t)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	for _, value := range []string{
		state.messages.Tools.RemoteContainerDetailsTitle,
		state.messages.Tools.RemoteContainerOverview,
		"web", "nginx:latest", "running",
	} {
		if !remoteCanvasHasText(&canvas, value) {
			t.Errorf("container overview lacks %q", value)
		}
	}

	state.Click(hitRemoteContainerDetailsEnvironment)
	waitFor(t, state, func() bool { return !state.remote.containerDetails.loading })
	if server.ran("docker inspect --format '{{json .Config.Env}}'") != 1 ||
		state.remote.containerDetails.failed || len(state.remote.containerDetails.environment) != 3 {
		t.Fatalf("environment=%+v status=%q commands=%q",
			state.remote.containerDetails.environment, state.remote.containerDetails.status, server.commands)
	}
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	for _, value := range []string{"PATH", "SERVICE"} {
		if !remoteCanvasHasText(&canvas, value) {
			t.Errorf("container environment lacks %q", value)
		}
	}
	detailsText := &state.remote.containerDetails.text
	result := state.Shortcut(gfx.KeyEvent{
		Text: "a", Modifiers: keymap.Primary, Target: hitRemoteContainerDetailsText,
	})
	if !result.Handled || !result.Repaint {
		t.Fatalf("container environment select-all result=%+v", result)
	}
	selected, ok := detailsText.selectedText()
	if !ok || !strings.Contains(selected, "/usr/local/sbin:/usr/local/bin") ||
		!strings.Contains(selected, "web") {
		t.Fatalf("container environment table is not selectable: %q, %v", selected, ok)
	}

	textHit, found := hitByID(&canvas, hitRemoteContainerDetailsText)
	if !found {
		t.Fatal("container detail text is not selectable")
	}
	start := gfx.Point{X: textHit.Bounds.X + 4, Y: textHit.Bounds.Y + 4}
	end := gfx.Point{X: textHit.Bounds.X + textHit.Bounds.Width - 8, Y: textHit.Bounds.Bottom() - 4}
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		X: start.X, Y: start.Y, Target: hitRemoteContainerDetailsText,
		Viewport: gfx.Size{Width: 1400, Height: 900},
	})
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Captured: true,
		X: end.X, Y: end.Y, Target: hitRemoteContainerDetailsText,
		Viewport: gfx.Size{Width: 1400, Height: 900},
	})
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Captured: true,
		X: end.X, Y: end.Y, Target: hitRemoteContainerDetailsText,
		Viewport: gfx.Size{Width: 1400, Height: 900},
	})
	selected, ok = state.remote.containerDetails.text.selectedText()
	if !ok || !strings.Contains(selected, "PATH") {
		t.Fatalf("selected container text = %q, %v", selected, ok)
	}
	copied := ""
	state.options.SetClipboardText = func(value string) error {
		copied = value
		return nil
	}
	result = state.Shortcut(gfx.KeyEvent{
		Text: "c", Modifiers: keymap.Primary, Target: hitRemoteContainerDetailsText,
	})
	if !result.Handled || copied != selected {
		t.Fatalf("copy result=%+v copied=%q, want %q", result, copied, selected)
	}
}

func TestContainerDetailsReportsEnvironmentFailureAndClosesWithEscape(t *testing.T) {
	state, server := openContainerDetailsForTest(t)
	server.failures["docker inspect --format '{{json .Config.Env}}'"] = errors.New("exit status 1")
	state.Click(hitRemoteContainerDetailsEnvironment)
	waitFor(t, state, func() bool { return !state.remote.containerDetails.loading })
	if !state.remote.containerDetails.failed ||
		!strings.Contains(state.remote.containerDetails.status, "denied by the test") {
		t.Fatalf("container environment failure = %+v", state.remote.containerDetails)
	}
	result := state.Key(gfx.KeyEvent{Key: gfx.KeyEscape})
	if !result.Handled || state.remote.containerDetails.open || state.modal != modalRemoteTools {
		t.Fatalf("Escape result=%+v details=%+v modal=%d", result, state.remote.containerDetails, state.modal)
	}
}

func TestContainerEnvironmentCanRetryAfterFailure(t *testing.T) {
	state, server := openContainerDetailsForTest(t)
	server.failures["docker inspect --format '{{json .Config.Env}}'"] = errors.New("exit status 1")
	state.Click(hitRemoteContainerDetailsEnvironment)
	waitFor(t, state, func() bool { return !state.remote.containerDetails.loading })
	if !state.remote.containerDetails.failed {
		t.Fatal("first container environment read did not fail")
	}
	delete(server.failures, "docker inspect --format '{{json .Config.Env}}'")
	state.Click(hitRemoteContainerDetailsEnvironment)
	waitFor(t, state, func() bool {
		return !state.remote.containerDetails.loading && !state.remote.containerDetails.failed
	})
	if got := len(state.remote.containerDetails.environment); got != 3 {
		t.Fatalf("retry loaded %d variables, want 3", got)
	}
}

func TestClosingTheWorkbenchCancelsContainerEnvironment(t *testing.T) {
	state, _ := openContainerDetailsForTest(t)
	cancelled := false
	state.remote.containerDetails.environmentCancel = func() { cancelled = true }
	state.Close()
	if !cancelled || state.remote.containerDetails.open {
		t.Fatalf("container environment survived workbench close: cancelled=%v details=%+v",
			cancelled, state.remote.containerDetails)
	}
}
