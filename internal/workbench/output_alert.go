package workbench

import (
	"math"
	"time"

	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

var hitSettingsOutputAlerts = tree.ID(settingsHits, "background-output-alerts")

type outputAlertKind uint8

const (
	outputAlertNone outputAlertKind = iota
	outputAlertQuiet
	outputAlertRunning
	outputAlertError
)

const outputQuietDelay = 2 * time.Second

type terminalOutputAlert struct {
	kind            outputAlertKind
	lastOutput      time.Duration
	commandRunning  bool
	completionKnown bool
}

// All panes of the active workspace count as viewed, including unfocused ones.
// Connection pages and fullscreen desktops do not expose the terminal underneath.
func (state *State) outputTerminalVisible(session *Session) bool {
	return state.desktopVisible(session) && session.term != nil && session.term.live &&
		!session.restored && !state.showsProgress(session) && session.desk == nil
}

func (state *State) clearOutputAlerts() {
	for i := range state.sessions {
		state.sessions[i].outputAlert.kind = outputAlertNone
	}
}

func (state *State) receiveBackgroundOutput(session *Session) {
	alert := &session.outputAlert
	if !state.backgroundOutputAlerts || state.outputTerminalVisible(session) {
		alert.kind = outputAlertNone
		return
	}
	alert.lastOutput = state.now()
	if alert.kind != outputAlertError {
		alert.kind = outputAlertRunning
	}
}

func (state *State) finishBackgroundCommand(session *Session, code int) {
	alert := &session.outputAlert
	running := alert.commandRunning
	alert.commandRunning, alert.completionKnown = false, true
	// The initial prompt also reports a status; it is not a completed command.
	if !running || !state.backgroundOutputAlerts || state.outputTerminalVisible(session) {
		return
	}
	if code != 0 {
		alert.kind = outputAlertError
	} else if alert.kind != outputAlertError && alert.kind != outputAlertNone {
		alert.kind = outputAlertQuiet
	}
}

// Only the silence deadline needs a wake when a title is not being painted.
// Animated frames are requested by visible titles alone.
func (state *State) refreshOutputAlerts(canvas *ui.Canvas) {
	now := state.now()
	for i := range state.sessions {
		session := &state.sessions[i]
		alert := &session.outputAlert
		if !state.backgroundOutputAlerts || state.outputTerminalVisible(session) {
			alert.kind = outputAlertNone
		}
		if session.term != nil && session.term.ended {
			alert.commandRunning = false
		}
		if alert.kind != outputAlertRunning || alert.completionKnown && alert.commandRunning {
			continue
		}
		wait := outputQuietDelay - (now - alert.lastOutput)
		if wait <= 0 {
			alert.kind = outputAlertQuiet
		} else if canvas != nil {
			canvas.RequestFrameIn(wait)
		}
	}
}

func (state *State) workspaceOutputAlert(id layoutcore.WorkspaceID) outputAlertKind {
	if !state.backgroundOutputAlerts {
		return outputAlertNone
	}
	kind := outputAlertNone
	for i := range state.sessions {
		session := &state.sessions[i]
		owner, _, ok := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
		if ok && owner == id && !state.outputTerminalVisible(session) {
			kind = max(kind, session.outputAlert.kind)
		}
	}
	return kind
}

func (state *State) paintOutputAlertTitle(canvas *ui.Canvas, tab *barTab, strip ui.Rect) {
	bounds := tab.title.Rect()
	if bounds.Width <= 0 || bounds.Height <= 0 || bounds.X >= strip.X+strip.Width || bounds.X+bounds.Width <= strip.X || bounds.Y >= strip.Y+strip.Height || bounds.Y+bounds.Height <= strip.Y {
		return
	}
	style := ui.TextStyle{Size: metrics.FontSizeS, Weight: ui.WeightMedium, Color: state.pal.success}
	label := canvas.FitLine(tab.label, style.Size, style.Weight, bounds.Width)
	blue, highlight := ui.RGB24(0x75b8ff), ui.RGB24(0xedfaff)
	if !state.pal.dark {
		blue, highlight = ui.RGB24(0x2465a8), ui.RGB24(0x659bd4)
	}
	switch tab.output {
	case outputAlertRunning:
		style.Color = blue
	case outputAlertError:
		style.Color = state.pal.warning
	}
	if canvas.ReduceMotion() || tab.output == outputAlertQuiet {
		canvas.Label(label, bounds, style)
		return
	}
	canvas.RequestFrameIn(pulseStep)
	if tab.output == outputAlertError {
		phase := float64(canvas.Now()%(2400*time.Millisecond)) / float64(2400*time.Millisecond)
		style.Color.Alpha = .55 + .45*float32(.5-.5*math.Cos(2*math.Pi*phase))
		canvas.Label(label, bounds, style)
		return
	}
	// Clip repeated copies of the same shaped line: only the ink changes color,
	// never its position or glyph spacing. Narrow bands approximate a smooth gradient.
	width := min(bounds.Width, canvas.TextWidthWeight(label, style.Size, style.Weight))
	if width <= 0 {
		return
	}
	phase := float32(canvas.Now()%(1800*time.Millisecond)) / float32(1800*time.Millisecond)
	center, band := (phase*1.6-.3)*width, max(width*.24, 12)
	origin := canvas.LabelOrigin(label, bounds, style, ui.AlignStart)
	for x := float32(0); x < width; x += 3 {
		distance := float32(math.Abs(float64((x + 1.5 - center) / band)))
		amount := max(1-distance, 0)
		amount = amount * amount * (3 - 2*amount)
		style.Color = blue.Mix(highlight, amount)
		canvas.Clip(ui.Rect{X: origin + x, Y: bounds.Y, Width: min(3, width-x), Height: bounds.Height})
		canvas.Label(label, bounds, style)
		canvas.Unclip()
	}
}
