package workbench

import (
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// Playing back a terminal recording. The recording is fed to a terminal of its
// own at the size it was made in, so a program that drew a full screen looks as
// it did. Nothing in that terminal reaches the machine: it has no shell, and its
// clipboard, notification, link and reply paths are left unwired, so a
// recording cannot make Motty write anywhere. The clock runs only while the
// recording plays; a paused or finished playback schedules no frames.

var playbackHits = tree.Feature("recording-playback")

var (
	hitPlaybackDismiss = tree.ID(playbackHits, "dismiss")
	hitPlaybackClose   = tree.ID(playbackHits, "close")
	hitPlaybackToggle  = tree.ID(playbackHits, "toggle")
	hitPlaybackSeek    = tree.ID(playbackHits, "seek")
	hitPlaybackSpeed   = tree.ID(playbackHits, "speed")
)

const (
	playbackScrollback = 2000
	playbackPad        = 8
	// playbackFrame is how often the position moves while it plays and nothing
	// on the screen changes sooner.
	playbackFrame = 100 * time.Millisecond
	// playbackSeekSteps is the resolution of the position slider.
	playbackSeekSteps = 1000
)

// playbackSpeed is a playback rate in quarters of real time.
type playbackSpeed uint8

const (
	playbackHalf      playbackSpeed = 2
	playbackNormal    playbackSpeed = 4
	playbackDouble    playbackSpeed = 8
	playbackQuadruple playbackSpeed = 16
)

var playbackSpeeds = [...]playbackSpeed{playbackHalf, playbackNormal, playbackDouble, playbackQuadruple}

func (speed playbackSpeed) String() string {
	switch speed {
	case playbackHalf:
		return "0.5×"
	case playbackDouble:
		return "2×"
	case playbackQuadruple:
		return "4×"
	}
	return "1×"
}

type recordingPlayback struct {
	title   string
	info    string // when it was made, its size and whether it was cut short
	outputs []terminalRecordingEvent
	cols    int
	rows    int
	length  time.Duration

	term    *terminal
	next    int           // the first output not yet on the screen
	at      time.Duration // the playhead
	playing bool
	anchor  time.Duration // the window clock when this run began
	from    time.Duration // the playhead at anchor
	speed   playbackSpeed

	seek                 ui.SliderState
	choice               playbackSpeed
	label                string // the clock text, rebuilt when its seconds change
	labelAt, labelLength time.Duration
}

// openRecording reads the recording at path and plays it.
func (state *State) openRecording(path string) {
	if state.modal != modalNone {
		return
	}
	title := state.messages.RecordingOpen
	file, err := readRecordingFile(path)
	if err != nil {
		state.showFileReadFailure(title, err, state.messages.RecordingInvalid)
		return
	}
	state.startPlayback(file, recordingTitle(file, path))
}

// chooseRecording asks for a recording and plays it.
func (state *State) chooseRecording() {
	if state.modal != modalNone {
		return
	}
	paths, err := state.options.OpenFiles(app.OpenFileOptions{
		Title:   state.messages.RecordingOpen,
		Filters: []app.FileFilter{{Name: "Terminal recording", Pattern: "*.cast;*.mottyrec"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		return
	}
	if err != nil {
		state.showActionFailure(state.messages.RecordingOpen, err, state.messages.ActionFeedback.FilePickerFailed)
		return
	}
	state.openRecording(paths[0])
}

// recordingDropArea reports whether a file dragged here, over no terminal or
// file panel, is taken as a recording to play. The system only says which files
// are dragged when they are dropped, so the hover accepts on the place alone;
// the file's content then decides, and a file that is not a recording says so.
func (state *State) recordingDropArea() bool { return state.modal == modalNone }

func readRecordingFile(path string) (*recordingFile, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	data, err := io.ReadAll(io.LimitReader(handle, recordingFileMaxBytes+1))
	if err != nil {
		return nil, err
	}
	return decodeRecordingFile(data)
}

func recordingTitle(file *recordingFile, path string) string {
	if title := strings.TrimSpace(file.title); title != "" {
		return title
	}
	name := path
	if slash := strings.LastIndexAny(name, `/\`); slash >= 0 {
		name = name[slash+1:]
	}
	if dot := strings.LastIndexByte(name, '.'); dot > 0 {
		name = name[:dot]
	}
	return name
}

func (state *State) startPlayback(file *recordingFile, title string) {
	play := &recordingPlayback{title: title, cols: file.cols, rows: file.rows, speed: playbackNormal}
	play.choice = play.speed
	for _, event := range file.events {
		if event.direction == terminalRecordingOutput {
			play.outputs = append(play.outputs, event)
		}
	}
	if count := len(play.outputs); count > 0 {
		play.length = play.outputs[count-1].at
	}
	play.info = state.playbackInfo(file)
	play.term = state.newPlaybackTerminal(play)
	play.seekTo(state, 0)
	state.playback = play
	state.modal = modalRecordingPlayback
	if play.length > 0 {
		play.resume(state)
	}
}

// playbackInfo is the line under the title: what the file does not show by
// playing, and a warning when the end of the recording is missing.
func (state *State) playbackInfo(file *recordingFile) string {
	var parts [3]string
	count := 0
	if !file.started.IsZero() {
		parts[count] = file.started.Local().Format("2006-01-02 15:04")
		count++
	}
	parts[count] = strconv.Itoa(file.cols) + "×" + strconv.Itoa(file.rows)
	count++
	if file.truncated {
		parts[count] = state.messages.RecordingTruncated
		count++
	}
	return strings.Join(parts[:count], " · ")
}

func (state *State) newPlaybackTerminal(play *recordingPlayback) *terminal {
	t := newTerminalWithScrollback(play.cols, play.rows, playbackScrollback)
	t.screen.SetCellPixels(state.cellPixelWidth, state.cellPixelHeight)
	state.reportTerminalColors(t)
	return t
}

func (play *recordingPlayback) resume(state *State) {
	if play.at >= play.length {
		play.seekTo(state, 0)
	}
	play.playing, play.anchor, play.from = true, state.now(), play.at
}

func (play *recordingPlayback) pause() { play.playing = false }

// seekTo puts the playhead at at. Going back replays from the start into a
// fresh screen, which is what a terminal needs to show an earlier moment.
func (play *recordingPlayback) seekTo(state *State, at time.Duration) {
	at = min(max(at, 0), play.length)
	if at < play.at || play.term == nil {
		play.term = state.newPlaybackTerminal(play)
		play.next = 0
	}
	play.feedTo(at)
	play.at = at
	if play.playing {
		play.anchor, play.from = state.now(), at
	}
}

func (play *recordingPlayback) feedTo(at time.Duration) {
	for play.next < len(play.outputs) && play.outputs[play.next].at <= at {
		play.term.screen.Write(play.outputs[play.next].data)
		play.next++
	}
}

// advance moves the playhead to the window clock's now and returns how long to
// wait for the next frame, or zero when nothing is playing any more.
func (play *recordingPlayback) advance(now time.Duration) time.Duration {
	if !play.playing {
		return 0
	}
	elapsed := (now - play.anchor) * time.Duration(play.speed) / time.Duration(playbackNormal)
	at := min(play.from+elapsed, play.length)
	play.feedTo(at)
	play.at = at
	if at >= play.length {
		play.playing = false
		return 0
	}
	// The next frame is the next output, or the next tick of the clock.
	wait := playbackFrame
	if play.next < len(play.outputs) {
		due := (play.outputs[play.next].at - at) * time.Duration(playbackNormal) / time.Duration(play.speed)
		wait = min(wait, max(due, time.Millisecond))
	}
	return wait
}

func (play *recordingPlayback) setSpeed(state *State, speed playbackSpeed) {
	if play.playing { // keep the position the old rate reached
		play.anchor, play.from = state.now(), play.at
	}
	play.speed = speed
}

// clock is the position and the length as "0:12 / 1:30".
func (play *recordingPlayback) clock() string {
	at, length := play.at.Truncate(time.Second), play.length.Truncate(time.Second)
	if play.label == "" || play.labelAt != at || play.labelLength != length {
		play.label = playbackClock(at) + " / " + playbackClock(length)
		play.labelAt, play.labelLength = at, length
	}
	return play.label
}

func playbackClock(d time.Duration) string {
	seconds := int(d / time.Second)
	if hours := seconds / 3600; hours > 0 {
		return strconv.Itoa(hours) + ":" + twoDigits(seconds/60%60) + ":" + twoDigits(seconds%60)
	}
	return strconv.Itoa(seconds/60) + ":" + twoDigits(seconds%60)
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// declareRecordingPlayback declares the playback dialog: the recorded screen,
// and under it play or pause, the position and the speed.
func (state *State) declareRecordingPlayback(c ui.C) {
	play := state.playback
	if play == nil {
		return
	}
	m := &state.messages
	if wait := play.advance(state.now()); wait > 0 {
		c.Canvas().RequestFrameIn(wait)
	}
	viewport := dialogViewport(c)
	cellWidth, cellHeight := max(state.cellWidth, 1), max(state.cellHeight, 1)
	wanted := float32(play.cols)*cellWidth + 2*playbackPad
	width := min(max(wanted+48, 520), max(viewport.Width-40, 320))
	screenHeight := min(float32(play.rows)*cellHeight+2*playbackPad, max(viewport.Height-200, 120))
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: play.title, Width: width, Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitPlaybackDismiss, CloseID: hitPlaybackClose, InitialFocusID: hitPlaybackToggle,
	})
	// The header slot lies over the title row, so the facts start where the title
	// ends, at the size and weight the dialog paints it in.
	titleWidth := c.Canvas().TextWidthWeight(play.title, c.Theme().Metrics.FontSizeM+2, gfx.WeightMedium)
	dialog.Header(func() {
		ui.Row(c).Fill().Items(ui.Center).Children(func() {
			ui.Text(c, play.info).FontSize(metrics.FontSizeS).Color(state.pal.muted).Ellipsis().MarginSides(0, 48, 0, titleWidth+14)
		})
	})
	dialog.Body(func() {
		ui.Slot(c).Height(screenHeight).Draw(func(d *ui.DrawContext) { state.paintPlayback(d, play) })
	})
	dialog.Footer(func() {
		ui.Row(c).Grow(1).Height(32).Gap(10).Items(ui.Center).Children(func() {
			icon, hint := uint8(iconPause), m.RecordingPause
			if !play.playing {
				icon, hint = iconPlay, m.RecordingPlay
			}
			state.declareIconButton(c, hitPlaybackToggle, icon, hint, false, iconButtonQuiet).Size(28, 28)
			if !play.seek.Dragging() && play.length > 0 {
				play.seek.Value = float32(play.at) / float32(play.length) * playbackSeekSteps
			}
			seek := ui.Slider(c, &play.seek).Range(0, playbackSeekSteps).Step(1).Label(m.RecordingPosition).Hit(hitPlaybackSeek, m.RecordingPosition).Grow(1).Shrink(1)
			play.seek.ID = hitPlaybackSeek
			if seek.Changed() && play.length > 0 {
				at := time.Duration(float64(play.seek.Value) / playbackSeekSteps * float64(play.length))
				c.After(func() { play.seekTo(state, at) })
			}
			ui.Text(c, play.clock()).FontSize(metrics.FontSizeS).Color(state.pal.muted).Width(96).Shrink(0).TextEnd()
			play.choice = play.speed
			speed := ui.Select(c, &play.choice, playbackSpeeds[:], playbackSpeed.String).Label(m.RecordingSpeed).Width(128).Shrink(0)
			if speed.Changed() {
				next := play.choice
				c.After(func() { play.setSpeed(state, next) })
			}
		})
	})
}

// paintPlayback draws the recorded screen at the corner of its slot. A recording
// larger than the slot is cut at the slot's edge, not reflowed: its lines wrap
// where the program put them.
func (state *State) paintPlayback(d *ui.DrawContext, play *recordingPlayback) {
	if play.term == nil {
		return
	}
	area := d.Rect
	d.Canvas.Fill(area, state.termPal.terminal)
	geometry := terminalGeometry{area: area, left: area.X + playbackPad, top: area.Y + playbackPad, cols: play.cols, rows: play.rows}
	state.paintGridBody(d.Canvas, &Session{Title: play.title, term: play.term}, geometry, gfx.ID{}, true, false, false)
}

func (state *State) clickRecordingPlayback(id gfx.ID) {
	play := state.playback
	switch id {
	case hitPlaybackDismiss, hitPlaybackClose:
		state.closeModal() // takes the playback with it
		state.showNextPrompt()
	case hitPlaybackToggle:
		if play == nil {
			return
		}
		if play.playing {
			play.pause()
		} else {
			play.resume(state)
		}
	}
}

// scrollRecordingPlayback scrolls the recorded screen back through its history.
func (state *State) scrollRecordingPlayback(event gfx.ScrollEvent) gfx.EventResult {
	play := state.playback
	if play == nil || play.term == nil {
		return gfx.EventResult{Handled: true}
	}
	lines := int(event.StepY(state.cellHeight) / max(state.cellHeight, 1))
	return gfx.EventResult{Handled: true, Repaint: play.term.screen.Scroll(lines)}
}
