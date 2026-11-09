package workbench

import (
	"encoding/csv"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/remotetools"
	"motty/internal/sshconn"
)

func TestProcessDetailsKeepsTwoColumnMetadataAndSelectableEnvironment(t *testing.T) {
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

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	for _, value := range []string{
		state.messages.Tools.RemoteProcessEnvironment,
		state.messages.Tools.RemoteProcessEnvironmentName,
		state.messages.Tools.RemoteProcessEnvironmentValue,
		"LANG", "PATH", "SERVICE",
	} {
		if !remoteCanvasHasText(&canvas, value) {
			t.Errorf("process details lack %q", value)
		}
	}
	detailsText := &state.remote.processDetails.text
	result := state.Shortcut(gfx.KeyEvent{
		Text: "a", Modifiers: keymap.Primary, Target: hitRemoteProcessDetailsText,
	})
	if !result.Handled || !result.Repaint {
		t.Fatalf("environment select-all result=%+v", result)
	}
	selected, ok := detailsText.selectedText()
	if !ok || !strings.Contains(selected, "zh_CN.UTF-8") ||
		!strings.Contains(selected, "/usr/local/bin:/usr/bin") ||
		!strings.Contains(selected, "api=blue") {
		t.Fatalf("environment table is not selectable: %q, %v", selected, ok)
	}

	for _, label := range []string{
		state.messages.Tools.RemoteProcessParentPID,
		state.messages.Tools.RemoteProcessState,
		state.messages.Tools.RemoteProcessMemory,
		state.messages.Tools.RemoteProcessVirtualMemory,
		state.messages.Tools.RemoteProcessCollectedAt,
	} {
		if !remoteCanvasHasText(&canvas, label) {
			t.Errorf("two-column metadata lacks %q", label)
		}
	}
	secondColumnX := float32(0)
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
	if _, found := hitByID(&canvas, hitRemoteProcessDetailsText); !found {
		t.Fatal("detail body has no selectable text region")
	}
}

func TestProcessEnvironmentReportsEmptyFailureAndUnsupportedStates(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		state, server, _ := openRemoteToolsDialog(t)
		server.environment = "\x00"
		state.Click(hitRemoteRowList.At(0))
		state.Click(hitRemoteProcessDetailsEnv)
		waitFor(t, state, func() bool { return !state.remote.processDetails.loading })
		var canvas gfx.Canvas
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
		if !remoteCanvasHasText(&canvas, state.messages.Tools.RemoteProcessEnvironmentEmpty) {
			t.Fatal("empty environment state is not shown")
		}
		if _, found := hitByID(&canvas, hitRemoteProcessDetailsText); found {
			t.Fatal("empty environment notice still owns a framed text region")
		}
	})

	t.Run("failure", func(t *testing.T) {
		state, server, _ := openRemoteToolsDialog(t)
		server.failures["environment"] = errors.New("exit status 1")
		state.Click(hitRemoteRowList.At(0))
		state.Click(hitRemoteProcessDetailsEnv)
		waitFor(t, state, func() bool { return !state.remote.processDetails.loading })
		var canvas gfx.Canvas
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
		if !state.remote.processDetails.failed ||
			state.remote.processDetails.status == "" ||
			!remoteCanvasHasText(&canvas, state.remote.processDetails.status) {
			t.Fatalf("failure state=%+v", state.remote.processDetails)
		}
		if _, found := hitByID(&canvas, hitRemoteProcessDetailsText); found {
			t.Fatal("failure notice still owns a framed text region")
		}
	})

	t.Run("process exited", func(t *testing.T) {
		state, server, _ := openRemoteToolsDialog(t)
		server.failures["environment"] = errors.New("exit status 1")
		server.environmentFailure = "motty-env-before 12345678-1234-1234-1234-123456789abc 221101\n"
		state.Click(hitRemoteRowList.At(0))
		state.Click(hitRemoteProcessDetailsEnv)
		waitFor(t, state, func() bool { return !state.remote.processDetails.loading })
		if state.remote.processDetails.status != state.messages.Tools.RemoteProcessChanged ||
			strings.Contains(state.remote.processDetails.status, "motty-env-before") {
			t.Fatalf("process exited status=%q", state.remote.processDetails.status)
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

	t.Run("unreadable", func(t *testing.T) {
		state, _, _ := openRemoteToolsDialog(t)
		if got := state.remoteErrorText(&remotetools.Failure{
			Message: "internal marker", Err: remotetools.ErrProcessEnvironmentUnreadable,
		}); got != state.messages.Tools.RemoteProcessEnvironmentUnreadable {
			t.Fatalf("unreadable text = %q", got)
		}
		if got := state.remoteErrorText(remotetools.ErrProcessEnvironmentTooLarge); got != state.messages.Tools.RemoteProcessEnvironmentTooLarge {
			t.Fatalf("too-large text = %q", got)
		}
	})
}

func TestEnvironmentTableCopiesCompleteMarkupLookingValues(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	longValue := strings.Repeat("path-segment/", 24)
	variables := []remotetools.ProcessEnvironmentVariable{
		{Name: "PIPE", Value: "left|right"},
		{Name: "MARKUP", Value: "`value\\with*markup`"},
		{Name: "LONG", Value: longValue},
	}
	header := [2]string{
		state.messages.Tools.RemoteProcessEnvironmentName,
		state.messages.Tools.RemoteProcessEnvironmentValue,
	}
	var text remoteDetailText
	var canvas gfx.Canvas
	state.paintRemoteEnvironmentView(
		&canvas,
		gfx.Rect{Width: 700, Height: 240},
		&text,
		false, false, "", state.messages.Tools.RemoteProcessEnvironmentEmpty,
		variables, header,
		state.messages.Tools.RemoteProcessEnvironment,
		state.messages.Tools.RemoteProcessEnvironment,
		hitRemoteProcessDetailsText, hitRemoteProcessDetailsScroll,
	)
	if text.mode != remoteDetailTable {
		t.Fatalf("environment mode = %v, want table", text.mode)
	}
	text.selection.Select(components.TextPoint{}, components.TextPoint{Offset: text.table.TextLen()})
	selected, ok := text.selectedText()
	want := header[0] + "\t" + header[1] + "\n" +
		"PIPE\tleft|right\n" +
		"MARKUP\t`value\\with*markup`\n" +
		"LONG\t" + longValue
	if !ok || selected != want {
		t.Fatalf("selected environment = %q, %v; want %q", selected, ok, want)
	}
}

func TestEnvironmentTableCopiesDelimitedValuesAsLosslessTSV(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	variables := []remotetools.ProcessEnvironmentVariable{
		{Name: "MULTILINE", Value: "first\nsecond"},
		{Name: "TABBED", Value: "left\tright"},
		{Name: "QUOTED", Value: `"alpha"`},
	}
	header := [2]string{"Variable", "Value"}
	var text remoteDetailText
	var canvas gfx.Canvas
	state.paintRemoteEnvironmentView(
		&canvas, gfx.Rect{Width: 700, Height: 240}, &text,
		false, false, "", state.messages.Tools.RemoteProcessEnvironmentEmpty,
		variables, header,
		state.messages.Tools.RemoteProcessEnvironment,
		state.messages.Tools.RemoteProcessEnvironment,
		hitRemoteProcessDetailsText, hitRemoteProcessDetailsScroll,
	)
	text.selection.Select(components.TextPoint{}, components.TextPoint{Offset: text.table.TextLen()})
	selected, ok := text.selectedText()
	if !ok {
		t.Fatal("environment table produced no selectable text")
	}
	reader := csv.NewReader(strings.NewReader(selected))
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("environment TSV did not parse: %v\n%s", err, selected)
	}
	want := [][]string{
		{"Variable", "Value"},
		{"MULTILINE", "first\nsecond"},
		{"TABBED", "left\tright"},
		{"QUOTED", `"alpha"`},
	}
	if !slices.EqualFunc(records, want, slices.Equal) {
		t.Fatalf("environment records = %#v, want %#v\n%s", records, want, selected)
	}
}

func TestProcessDetailsSupportsSelectingAndCopyingText(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	state.Click(hitRemoteRowList.At(0))
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	textHit, found := hitByID(&canvas, hitRemoteProcessDetailsText)
	if !found {
		t.Fatal("process detail text is not selectable")
	}
	start := gfx.Point{X: textHit.Bounds.X + 4, Y: textHit.Bounds.Y + 4}
	end := gfx.Point{X: textHit.Bounds.X + textHit.Bounds.Width - 8, Y: textHit.Bounds.Bottom() - 4}
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		X: start.X, Y: start.Y, Target: hitRemoteProcessDetailsText,
		Viewport: gfx.Size{Width: 1400, Height: 900},
	})
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Captured: true,
		X: end.X, Y: end.Y, Target: hitRemoteProcessDetailsText,
		Viewport: gfx.Size{Width: 1400, Height: 900},
	})
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Captured: true,
		X: end.X, Y: end.Y, Target: hitRemoteProcessDetailsText,
		Viewport: gfx.Size{Width: 1400, Height: 900},
	})
	selected, ok := state.remote.processDetails.text.selectedText()
	if !ok || !strings.Contains(selected, state.remote.processDetails.process.Command) {
		t.Fatalf("selected process text = %q, %v", selected, ok)
	}
	copied := ""
	state.options.SetClipboardText = func(value string) error {
		copied = value
		return nil
	}
	result := state.Shortcut(gfx.KeyEvent{
		Text: "c", Modifiers: keymap.Primary, Target: hitRemoteProcessDetailsText,
	})
	if !result.Handled || copied != selected {
		t.Fatalf("copy result=%+v copied=%q, want %q", result, copied, selected)
	}
}

func TestProcessDetailsLoadsTheCompleteCommandAndSupportsKeyboardSelection(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	fullCommand := "/usr/bin/python3 /srv/application.py --config \"" +
		strings.Repeat("nested/", 180) + "config.yml\" --serve"
	server.processCommand = fullCommand
	state.Click(hitRemoteRowList.At(0))
	waitFor(t, state, func() bool { return state.remote.processDetails.commandLoaded })
	if server.ran("# motty-process-command\n") != 1 ||
		state.remote.processDetails.commandValue != fullCommand ||
		state.remote.processDetails.commandFailed {
		t.Fatalf("command load state=%+v commands=%q",
			state.remote.processDetails, server.commands)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	result := state.Shortcut(gfx.KeyEvent{
		Text: "a", Modifiers: keymap.Primary, Target: hitRemoteProcessDetailsText,
	})
	if !result.Handled || !result.Repaint {
		t.Fatalf("select-all result=%+v", result)
	}
	selected, ok := state.remote.processDetails.text.selectedText()
	if !ok || !strings.Contains(selected, strings.Repeat("nested/", 180)+"config.yml") {
		t.Fatalf("selected complete command = %q, %v", selected, ok)
	}
}

func TestProcessCommandFailureShowsSummaryAndCanRetry(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	server.failures["command"] = errors.New("exit status 1")
	state.Click(hitRemoteRowList.At(0))
	waitFor(t, state, func() bool {
		return state.remote.processDetails.commandLoaded && !state.remote.processDetails.commandLoading
	})
	if !state.remote.processDetails.commandFailed ||
		state.remote.processDetails.commandStatus != state.messages.Tools.RemoteProcessCommandSummary {
		t.Fatalf("failed command state=%+v", state.remote.processDetails)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if !remoteCanvasHasText(&canvas, state.messages.Tools.RemoteProcessCommandSummary) {
		t.Fatal("command summary was not disclosed in the dialog")
	}
	delete(server.failures, "command")
	state.Click(hitRemoteProcessDetailsCommand)
	waitFor(t, state, func() bool {
		return state.remote.processDetails.commandLoaded &&
			!state.remote.processDetails.commandLoading &&
			!state.remote.processDetails.commandFailed
	})
	if state.remote.processDetails.commandStatus != "" ||
		!strings.Contains(state.remote.processDetails.commandValue, "nginx") {
		t.Fatalf("retried command state=%+v", state.remote.processDetails)
	}
}

func TestProcessEnvironmentCanRetryAfterFailure(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	server.failures["environment"] = errors.New("exit status 1")
	state.Click(hitRemoteRowList.At(0))
	state.Click(hitRemoteProcessDetailsEnv)
	waitFor(t, state, func() bool { return !state.remote.processDetails.loading })
	if !state.remote.processDetails.failed {
		t.Fatal("first environment read did not fail")
	}
	delete(server.failures, "environment")
	state.Click(hitRemoteProcessDetailsEnv)
	waitFor(t, state, func() bool {
		return !state.remote.processDetails.loading && !state.remote.processDetails.failed
	})
	if got := len(state.remote.processDetails.environment); got != 3 {
		t.Fatalf("retry loaded %d variables, want 3", got)
	}
}

func TestRemoteDetailTextRelayoutsWhenAsynchronousSourceChanges(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	var text remoteDetailText
	bounds := gfx.Rect{X: 100, Y: 100, Width: 700, Height: 420}
	var canvas gfx.Canvas
	state.paintRemoteDetailText(
		&canvas, bounds, &text,
		state.messages.Tools.RemoteProcessEnvironmentLoading,
		state.messages.Tools.RemoteProcessDetailsTitle,
		state.messages.Tools.RemoteProcessEnvironment,
		hitRemoteProcessDetailsText,
		hitRemoteProcessDetailsScroll,
	)
	if !remoteCanvasHasText(&canvas, state.messages.Tools.RemoteProcessEnvironmentLoading) {
		t.Fatal("loading state is not painted")
	}

	canvas.Reset()
	state.paintRemoteDetailText(
		&canvas, bounds, &text,
		"PATH=/usr/local/bin\nSERVICE=web",
		state.messages.Tools.RemoteProcessDetailsTitle,
		state.messages.Tools.RemoteProcessEnvironment,
		hitRemoteProcessDetailsText,
		hitRemoteProcessDetailsScroll,
	)
	if !remoteCanvasHasText(&canvas, "PATH=/usr/local/bin") ||
		remoteCanvasHasText(&canvas, state.messages.Tools.RemoteProcessEnvironmentLoading) {
		t.Fatal("asynchronous detail text was not relaid out after the source changed")
	}
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
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if state.remote.processDetails.text.scrollBar.MaxOffsetY <= 0 {
		t.Fatal("long environment table has no independent scroll range")
	}
	bounds := state.remote.processDetails.text.bounds
	before := state.remote.processDetails.text.scroll
	result := state.Scroll(gfx.ScrollEvent{
		X: bounds.X + bounds.Width/2, Y: bounds.Y + min(bounds.Height/2, 40),
		DeltaY: 20, Precise: true, Viewport: gfx.Size{Width: 1400, Height: 900},
	})
	if !result.Handled || state.remote.processDetails.text.scroll <= before {
		t.Fatalf("environment wheel did not move independently: handled=%v before=%g after=%g",
			result.Handled, before, state.remote.processDetails.text.scroll)
	}
	before = state.remote.processDetails.text.scroll
	keyResult := state.Key(gfx.KeyEvent{
		Key: gfx.KeyPageDown, Target: hitRemoteProcessDetailsText,
	})
	if !keyResult.Handled || state.remote.processDetails.text.scroll <= before {
		t.Fatalf("environment keyboard scroll did not move: handled=%v before=%g after=%g",
			keyResult.Handled, before, state.remote.processDetails.text.scroll)
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

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	bounds := state.remote.processDetails.text.bounds
	before := state.remote.processDetails.text.scroll
	state.Click(hitRemoteProcessDetailsCommand)
	result := state.Scroll(gfx.ScrollEvent{
		X: bounds.X + bounds.Width/2, Y: bounds.Y + min(bounds.Height/2, 40),
		DeltaY: 20, Precise: true, Viewport: gfx.Size{Width: 1400, Height: 900},
	})
	if !result.Handled || state.remote.processDetails.text.scroll != before {
		t.Fatalf("hidden environment text moved: handled=%v before=%g after=%g",
			result.Handled, before, state.remote.processDetails.text.scroll)
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
	environmentCancelled, commandCancelled := false, false
	state.remote.processDetails.environmentCancel = func() { environmentCancelled = true }
	state.remote.processDetails.commandCancel = func() { commandCancelled = true }

	state.closeRemoteToolsSurface()
	if !environmentCancelled || !commandCancelled || state.remote.processDetails.open || state.modal != modalNone {
		t.Fatalf("closing tools did not dispose requests: env=%v command=%v details=%+v modal=%d",
			environmentCancelled, commandCancelled, state.remote.processDetails, state.modal)
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

func remoteCanvasExactTextBounds(canvas *gfx.Canvas, value string) (gfx.Rect, bool) {
	for _, painted := range canvas.Texts() {
		if painted.Value == value {
			return painted.Bounds, true
		}
	}
	return gfx.Rect{}, false
}
