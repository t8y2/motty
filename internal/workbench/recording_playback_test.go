package workbench

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func castFile(events ...terminalRecordingEvent) []byte {
	recording := terminalRecording{cols: 100, rows: 30, events: events}
	for _, event := range events {
		recording.bytes += len(event.data)
	}
	return encodeAsciicast(&recording, "")
}

func outputEvent(at time.Duration, text string) terminalRecordingEvent {
	return terminalRecordingEvent{at: at, direction: terminalRecordingOutput, data: []byte(text)}
}

func TestAsciicastKeepsACharacterSplitAcrossEventsWhole(t *testing.T) {
	// The recorder cuts events at a fixed size, which can land inside a character.
	han := []byte("中")
	file, err := decodeRecordingFile(castFile(
		terminalRecordingEvent{at: time.Second, direction: terminalRecordingOutput, data: append([]byte("a"), han[:2]...)},
		terminalRecordingEvent{at: 2 * time.Second, direction: terminalRecordingInput, data: []byte("x")},
		terminalRecordingEvent{at: 3 * time.Second, direction: terminalRecordingOutput, data: append(append([]byte(nil), han[2:]...), 'b')},
	))
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	for _, event := range file.events {
		if event.direction == terminalRecordingOutput {
			output.Write(event.data)
		}
	}
	if output.String() != "a中b" {
		t.Fatalf("output = %q, want the character whole", output.String())
	}
	for _, event := range file.events {
		if string(event.data) != strings.ToValidUTF8(string(event.data), "�") {
			t.Fatalf("event %q is not valid text", event.data)
		}
	}
}

func TestAsciicastRoundTripKeepsOrderTimesAndTruncation(t *testing.T) {
	recording := terminalRecording{cols: 132, rows: 43, truncated: true, wall: time.Unix(1_700_000_000, 0)}
	recording.events = []terminalRecordingEvent{
		outputEvent(0, "first\r\n"),
		outputEvent(1500*time.Microsecond, "<b>&\x1b[31mred\x1b[0m\r\n"),
		{at: 90*time.Minute + 5*time.Second + 123456*time.Microsecond, direction: terminalRecordingInput, data: []byte("q\r")},
	}
	data := encodeAsciicast(&recording, "a \"quoted\" title")
	if !bytes.HasPrefix(data, []byte(`{"version":2,"width":132,"height":43,"timestamp":1700000000,`)) {
		t.Fatalf("header = %q", bytes.SplitN(data, []byte{'\n'}, 2)[0])
	}
	file, err := decodeRecordingFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if file.cols != 132 || file.rows != 43 || !file.truncated || file.title != `a "quoted" title` || len(file.events) != 3 {
		t.Fatalf("file = %+v", file)
	}
	for index, event := range recording.events {
		got := file.events[index]
		if got.at != event.at || got.direction != event.direction || !bytes.Equal(got.data, event.data) {
			t.Fatalf("event %d = %+v, want %+v", index, got, event)
		}
	}
}

func TestAsciicastReadsVersionThree(t *testing.T) {
	file, err := decodeRecordingFile([]byte(`{"version":3,"term":{"cols":90,"rows":28,"type":"xterm"},"title":"v3"}
# a comment line
[0.5,"o","one"]
[0.25,"m","chapter"]
[0.25,"o","two"]
[1.0,"r","100x30"]
[0.1,"x",0]
`))
	if err != nil {
		t.Fatal(err)
	}
	// Version 3 times are the gaps between events; markers, resizes and the exit
	// status are not part of what is shown.
	if file.cols != 90 || file.rows != 28 || file.title != "v3" || len(file.events) != 2 {
		t.Fatalf("file = %+v", file)
	}
	if file.events[0].at != 500*time.Millisecond || file.events[1].at != time.Second {
		t.Fatalf("times = %v, %v", file.events[0].at, file.events[1].at)
	}
}

func TestRecordingFileRejectsWhatIsNotARecording(t *testing.T) {
	for name, data := range map[string]string{
		"empty":           "",
		"text":            "hello\n",
		"unknown version": `{"version":9}` + "\n",
		"bad event":       `{"version":2,"width":80,"height":24}` + "\n" + `[0,"o"]` + "\n",
		"negative time":   `{"version":2,"width":80,"height":24}` + "\n" + `[-1,"o","x"]` + "\n",
		"text time":       `{"version":2,"width":80,"height":24}` + "\n" + `["soon","o","x"]` + "\n",
		"huge time":       `{"version":2,"width":80,"height":24}` + "\n" + `[1e300,"o","x"]` + "\n",
		"number data":     `{"version":2,"width":80,"height":24}` + "\n" + `[0,"o",5]` + "\n",
		"truncated magic": "GOPREC\r\n",
	} {
		if file, err := decodeRecordingFile([]byte(data)); err == nil {
			t.Errorf("%s was accepted as %+v", name, file)
		}
	}
	// A size outside what a terminal can show is brought inside it, and a time
	// that steps back keeps the order.
	file, err := decodeRecordingFile([]byte(`{"version":2,"width":100000,"height":1}` + "\n" + `[2,"o","a"]` + "\n" + `[1,"o","b"]` + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if file.cols != castMaxCols || file.rows != minTerminalRows || file.events[1].at != 2*time.Second {
		t.Fatalf("file = %+v", file)
	}
}

func TestEarlierBinaryRecordingsStillOpen(t *testing.T) {
	var recording terminalRecording
	recording.start(0)
	recording.append(time.Second, terminalRecordingOutput, []byte("old"))
	recording.stop()
	recording.truncated = true
	file, err := decodeRecordingFile(encodeTerminalRecording(&recording))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.events) != 1 || string(file.events[0].data) != "old" || file.events[0].at != time.Second || !file.truncated {
		t.Fatalf("file = %+v", file)
	}
}

func playbackState(t *testing.T, now *time.Duration) *State {
	t.Helper()
	_, options := newWorld(t)
	options.Invalidate = func() {}
	options.Now = func() time.Duration { return *now }
	return newTestState(t, options)
}

func playbackRow(play *recordingPlayback, row int) string {
	return strings.TrimRight(play.term.screen.RowText(row), " ")
}

func TestPlaybackSeekingBackRebuildsTheScreenAsItWas(t *testing.T) {
	now := time.Second
	state := playbackState(t, &now)
	state.startPlayback(&recordingFile{cols: 40, rows: 8, events: []terminalRecordingEvent{
		outputEvent(time.Second, "one\r\n"),
		{at: 1500 * time.Millisecond, direction: terminalRecordingInput, data: []byte("typed")},
		outputEvent(2*time.Second, "two\r\n"),
		outputEvent(3*time.Second, "\x1b[2J\x1b[Hthree"),
	}}, "recording")
	play := state.playback
	if state.modal != modalRecordingPlayback || play == nil || play.length != 3*time.Second || len(play.outputs) != 3 {
		t.Fatalf("modal=%v playback=%+v", state.modal, play)
	}
	play.pause()

	play.seekTo(state, 2500*time.Millisecond)
	if playbackRow(play, 0) != "one" || playbackRow(play, 1) != "two" {
		t.Fatalf("at 2.5s: %q, %q", playbackRow(play, 0), playbackRow(play, 1))
	}
	play.seekTo(state, 3*time.Second)
	if playbackRow(play, 0) != "three" || playbackRow(play, 1) != "" {
		t.Fatalf("at the end: %q, %q", playbackRow(play, 0), playbackRow(play, 1))
	}
	// Back: the screen is built again from the start, not patched.
	play.seekTo(state, 1200*time.Millisecond)
	if playbackRow(play, 0) != "one" || playbackRow(play, 1) != "" || play.at != 1200*time.Millisecond {
		t.Fatalf("at 1.2s: %q, %q at=%v", playbackRow(play, 0), playbackRow(play, 1), play.at)
	}
	play.seekTo(state, time.Hour)
	if play.at != 3*time.Second {
		t.Fatalf("a seek past the end stopped at %v", play.at)
	}
}

func TestPlaybackClockFollowsTheSpeedAndStopsAtTheEnd(t *testing.T) {
	now := time.Second
	state := playbackState(t, &now)
	state.startPlayback(&recordingFile{cols: 40, rows: 8, events: []terminalRecordingEvent{
		outputEvent(0, "a"),
		outputEvent(4*time.Second, "b"),
		outputEvent(10*time.Second, "c"),
	}}, "recording")
	play := state.playback
	if !play.playing {
		t.Fatal("a recording does not start playing")
	}

	now += 2 * time.Second
	wait := play.advance(now)
	if play.at != 2*time.Second || play.next != 1 || wait <= 0 || wait > playbackFrame {
		t.Fatalf("after 2s: at=%v next=%d wait=%v", play.at, play.next, wait)
	}
	// A faster rate keeps the position it had reached.
	play.setSpeed(state, playbackDouble)
	now += 2 * time.Second
	play.advance(now)
	if play.at != 6*time.Second || play.next != 2 {
		t.Fatalf("after 2s at double speed: at=%v next=%d", play.at, play.next)
	}
	// The wait for the next output is real time, not recording time.
	if wait := play.advance(now); wait != playbackFrame {
		t.Fatalf("wait = %v, want the clock tick", wait)
	}
	now += time.Hour
	if wait := play.advance(now); wait != 0 || play.playing || play.at != 10*time.Second || playbackRow(play, 0) != "abc" {
		t.Fatalf("at the end: wait=%v playing=%t at=%v row=%q", wait, play.playing, play.at, playbackRow(play, 0))
	}
	// A paused or finished playback asks for no more frames, and Play from the
	// end starts over.
	if wait := play.advance(now + time.Second); wait != 0 {
		t.Fatalf("a finished playback wants a frame in %v", wait)
	}
	play.resume(state)
	if !play.playing || play.at != 0 || playbackRow(play, 0) != "a" {
		t.Fatalf("restart: playing=%t at=%v row=%q", play.playing, play.at, playbackRow(play, 0))
	}
}

func TestOpeningARecordingPlaysItAndClosingReturnsTheWindow(t *testing.T) {
	now := time.Second
	state := playbackState(t, &now)
	path := filepath.Join(t.TempDir(), "demo.cast")
	if err := os.WriteFile(path, castFile(outputEvent(time.Second, "hello\r\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	state.options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state.chooseRecording()
	if state.modal != modalRecordingPlayback || state.playback == nil || state.playback.title != "demo" || state.playback.cols != 100 {
		t.Fatalf("modal=%v playback=%+v", state.modal, state.playback)
	}
	// What is played has no shell to write to: it is a screen and nothing else.
	if state.playback.term.shell != nil {
		t.Fatal("playback has a shell")
	}
	state.clickRecordingPlayback(hitPlaybackClose)
	if state.modal != modalNone || state.playback != nil {
		t.Fatalf("after close modal=%v playback=%v", state.modal, state.playback)
	}

	// A file that is not a recording says so and opens nothing.
	bad := filepath.Join(t.TempDir(), "notes.cast")
	if err := os.WriteFile(bad, []byte("not a recording"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.openRecording(bad)
	if state.modal != modalNone || state.playback != nil {
		t.Fatalf("a bad file opened modal=%v", state.modal)
	}
}

func TestADraggedFileIsAcceptedOnHoverAndPlayedOnDrop(t *testing.T) {
	now := time.Second
	state := playbackState(t, &now)
	path := filepath.Join(t.TempDir(), "demo.cast")
	if err := os.WriteFile(path, castFile(outputEvent(time.Second, "hello\r\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	// The system names the files only at the drop, so a hover over a page carries
	// no path and must be accepted all the same, or the drop never arrives.
	if over := state.FileDrop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: 500, Y: 400}); !over.Handled {
		t.Fatal("a hover over a page that would play a dropped recording is refused")
	}
	if drop := state.FileDrop(gfx.FileDropEvent{Phase: gfx.FileDropDrop, X: 500, Y: 400, Paths: []string{path}}); !drop.Handled || state.playback == nil {
		t.Fatalf("drop handled=%t playback=%v", drop.Handled, state.playback)
	}
	// With the dialog open, files are not taken again.
	if over := state.FileDrop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: 500, Y: 400}); over.Handled {
		t.Fatal("a hover is accepted over the playback dialog")
	}
}
