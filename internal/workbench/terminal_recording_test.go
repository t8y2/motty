package workbench

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"motty/internal/sshconn"
)

func liveRecordingSession(t testing.TB, now *time.Duration) (*State, *fakeConn, *fakeShell) {
	t.Helper()
	world, options := newWorld(t)
	options.Invalidate = func() {}
	options.Now = func() time.Duration { return *now }
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	shell := fake.shell(t)
	shell.say("ready\r\n")
	state.Wake()
	return state, fake, shell
}

func TestTerminalRecordingCapturesAcceptedSplitInputAndOutputDeterministically(t *testing.T) {
	now := time.Second
	state, _, shell := liveRecordingSession(t, &now)
	session := &state.sessions[0]
	state.toggleTerminalRecording(session)

	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "ls"})
	now += 50 * time.Millisecond
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextPaste, Text: " one\ntwo"})
	shell.mu.Lock()
	shell.refuse = true
	shell.mu.Unlock()
	now += 25 * time.Millisecond
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "rejected"})
	shell.mu.Lock()
	shell.refuse = false
	shell.mu.Unlock()
	now += 75 * time.Millisecond
	shell.say("remote \xff output\r\n")
	state.Wake()
	state.toggleTerminalRecording(session)

	recording := &session.term.recording
	if recording.active || len(recording.events) != 3 {
		t.Fatalf("recording active=%t events=%d", recording.active, len(recording.events))
	}
	encoded := encodeTerminalRecording(recording)
	var replayed terminalRecording
	replayed.start(0)
	if err := replayTerminalRecording(encoded, func(at time.Duration, direction terminalRecordingDirection, data []byte) error {
		replayed.append(at, direction, data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	replayed.stop()
	if !bytes.Equal(encoded, encodeTerminalRecording(&replayed)) {
		t.Fatal("recording replay was not deterministic")
	}
	if got := string(recording.events[0].data); got != "ls" || recording.events[0].direction != terminalRecordingInput || recording.events[0].at != 0 {
		t.Fatalf("first event=%+v data=%q", recording.events[0], got)
	}
	if got := string(recording.events[1].data); got != " one\rtwo" || recording.events[1].at != 50*time.Millisecond {
		t.Fatalf("second event=%+v data=%q", recording.events[1], got)
	}
	if got := string(recording.events[2].data); got != "remote \xff output\r\n" || recording.events[2].direction != terminalRecordingOutput || recording.events[2].at != 150*time.Millisecond {
		t.Fatalf("output event=%+v data=%q", recording.events[2], got)
	}
}

func TestTerminalRecordingNeverCapturesSensitiveConnectionPrompts(t *testing.T) {
	now := time.Second
	state, _, _ := liveRecordingSession(t, &now)
	session := &state.sessions[0]
	state.toggleTerminalRecording(session)

	state.modalSession = session.ID
	state.modal = modalPassword
	state.passwordFields = []text.Editor{{}}
	app.TextEvent{Kind: app.TextInsert, Text: "password-secret"}.Apply(&state.passwordFields[0])
	state.submitPassword()
	state.modalSession = session.ID
	state.modal = modalKeyPassphrase
	app.TextEvent{Kind: app.TextInsert, Text: "passphrase-secret"}.Apply(&state.keyPassphrase)
	state.submitKeyPassphrase()
	state.modalSession = session.ID
	state.modal = modalHostKey
	state.clickHostKey(hitHostKeyTrust)

	if len(session.term.recording.events) != 0 || session.term.recording.bytes != 0 {
		t.Fatalf("sensitive prompts entered recording: %+v", session.term.recording.events)
	}
}

func TestTerminalRecordingStopsOnDisconnectAndIsBounded(t *testing.T) {
	now := time.Second
	state, _, shell := liveRecordingSession(t, &now)
	session := &state.sessions[0]
	state.toggleTerminalRecording(session)
	shell.end(nil)
	state.Wake()
	if session.term.recording.active {
		t.Fatal("recording remained active after shell disconnect")
	}

	var recording terminalRecording
	recording.start(0)
	recording.bytes = terminalRecordingMaxBytes - 2
	recording.append(time.Second, terminalRecordingInput, []byte("four"))
	if recording.active || !recording.truncated || recording.bytes != terminalRecordingMaxBytes {
		t.Fatalf("bounded recording=%+v", recording)
	}
}

func TestTerminalRecordingExportUsesPrivateVersionedFile(t *testing.T) {
	now := time.Second
	state, _, _ := liveRecordingSession(t, &now)
	session := &state.sessions[0]
	session.term.recording.start(now)
	session.term.recording.append(now+time.Millisecond, terminalRecordingOutput, []byte("hello\r\n"))
	session.term.recording.stop()
	path := filepath.Join(t.TempDir(), "session.mottyrec")
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) { return path, nil }

	state.exportTerminalRecording(session)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := replayTerminalRecording(data, func(time.Duration, terminalRecordingDirection, []byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	requirePrivateFile(t, path)
}

func TestTerminalRecordingDecoderRejectsUnknownMetadata(t *testing.T) {
	recording := terminalRecording{}
	recording.start(0)
	recording.append(time.Millisecond, terminalRecordingOutput, []byte("x"))
	recording.stop()
	encoded := encodeTerminalRecording(&recording)

	tests := []struct {
		name   string
		offset int
		value  byte
	}{
		{name: "flags", offset: 11, value: 0x80},
		{name: "reserved event byte", offset: 25, value: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			corrupt := append([]byte(nil), encoded...)
			corrupt[test.offset] = test.value
			if err := replayTerminalRecording(corrupt, func(time.Duration, terminalRecordingDirection, []byte) error { return nil }); err == nil {
				t.Fatal("corrupt metadata was accepted")
			}
		})
	}
}
