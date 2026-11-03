//go:build darwin || windows || linux

package workbench

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
)

func TestHeadlessTerminalRecordingExportsReplayableShellIO(t *testing.T) {
	now := time.Second
	state, _, shell := liveRecordingSession(t, &now)
	var input app.TextInput
	resolveInput := func(context app.TextInputContext) *app.TextInput {
		if context.Terminal {
			return state.TerminalInput(context)
		}
		input.Editor = state.EditorFor(context.ID)
		if input.Editor == nil {
			return nil
		}
		return &input
	}
	session, err := headless.Open(app.Config{
		Title: "recording", Width: 1400, Height: 900,
		Paint: state.Paint, TextInput: resolveInput,
		OnPointer: state.Pointer, OnKey: state.Key,
		OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	if err := session.ClickLabel(state.messages.RecordingStart); err != nil {
		t.Fatal(err)
	}
	terminalLabel := fmt.Sprintf(state.messages.LabelTerminalFmt, state.sessions[0].Address)
	if err := session.ClickLabel(terminalLabel); err != nil {
		t.Fatal(err)
	}
	if err := session.Type("printf headless"); err != nil {
		t.Fatal(err)
	}
	now += 25 * time.Millisecond
	shell.say("headless output\r\n")
	state.Wake()
	if err := session.ClickLabel(state.messages.RecordingStop); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "headless.mottyrec")
	if err := session.Driver().QueueSaveFile(path, nil); err != nil {
		t.Fatal(err)
	}
	if err := session.ClickLabel(state.messages.RecordingExport); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var replayed terminalRecording
	replayed.start(0)
	if err := replayTerminalRecording(data, func(at time.Duration, direction terminalRecordingDirection, data []byte) error {
		replayed.append(at, direction, data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	replayed.stop()
	if !bytes.Equal(data, encodeTerminalRecording(&replayed)) {
		t.Fatal("headless export did not replay deterministically")
	}
	if shell.typed() != "printf headless" || len(replayed.events) != 2 {
		t.Fatalf("typed=%q events=%d", shell.typed(), len(replayed.events))
	}
	if event := replayed.events[0]; event.direction != terminalRecordingInput || string(event.data) != "printf headless" || event.at != 0 {
		t.Fatalf("input event=%+v data=%q", event, event.data)
	}
	if event := replayed.events[1]; event.direction != terminalRecordingOutput || string(event.data) != "headless output\r\n" || event.at != 25*time.Millisecond {
		t.Fatalf("output event=%+v data=%q", event, event.data)
	}
	requirePrivateFile(t, path)
}
