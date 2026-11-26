package workbench

import (
	"os"
	"path/filepath"
	"strings"
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
	// These tests check what recording captures; the multi-line paste review is
	// tested separately in paste_review_test.go, so pastes go straight through.
	state.pasteConfirmOff = true
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
	file, err := decodeRecordingFile(encodeAsciicast(recording, "title"))
	if err != nil {
		t.Fatal(err)
	}
	cols, rows := session.term.screen.Size()
	if file.cols != max(cols, minTerminalCols) || file.rows != max(rows, minTerminalRows) || file.title != "title" || len(file.events) != len(recording.events) {
		t.Fatalf("decoded size %dx%d title %q events %d, screen %dx%d", file.cols, file.rows, file.title, len(file.events), cols, rows)
	}
	for index, event := range recording.events {
		got := file.events[index]
		// The file holds text: the byte no character can start is replaced.
		if got.at != event.at || got.direction != event.direction || string(got.data) != strings.ToValidUTF8(string(event.data), "\uFFFD") {
			t.Fatalf("event %d decoded as %+v, recorded %+v", index, got, event)
		}
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

func TestTerminalRecordingExportWritesAnAsciicastFilePrivately(t *testing.T) {
	now := time.Second
	state, _, _ := liveRecordingSession(t, &now)
	session := &state.sessions[0]
	session.term.recording.start(now)
	session.term.recording.append(now+time.Millisecond, terminalRecordingOutput, []byte("hello\r\n"))
	session.term.recording.stop()
	path := filepath.Join(t.TempDir(), "session.cast")
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) { return path, nil }

	state.exportTerminalRecording(session)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if file, err := decodeRecordingFile(data); err != nil || len(file.events) != 1 {
		t.Fatalf("export decodes as %+v, %v", file, err)
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
