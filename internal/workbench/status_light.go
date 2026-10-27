package workbench

import (
	"math"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

// lightKind is how a status light looks.
type lightKind uint8

const (
	lightIdle  lightKind = iota // a hollow ring: nothing is connected
	lightBusy                   // a halo that breathes: working on it
	lightOn                     // a solid dot: connected
	lightFault                  // a solid dot: failed
	lightWaiting
)

const (
	lightCore   = float32(7)
	lightHalo   = float32(15)
	pulsePeriod = 1400 * time.Millisecond
	// pulseStep is how often a breathing light is repainted: smooth enough for
	// a slow pulse, a sixth of the frames display cadence would spend.
	pulseStep = 50 * time.Millisecond
)

// paintLight draws a status light centered on (x, y). behind is the color the
// light sits on, which an idle ring needs for its empty middle.
func paintLight(canvas *ui.Canvas, x, y float32, kind lightKind, color, behind ui.Color) {
	core := ui.Rect{X: x - lightCore/2, Y: y - lightCore/2, Width: lightCore, Height: lightCore}
	if kind == lightIdle {
		canvas.RoundedBorder(core, lightCore/2, 1.5, color, behind)
		return
	}
	if kind != lightBusy {
		canvas.Rounded(core, lightCore/2, color)
		return
	}
	alpha := float32(0.30)
	if !canvas.ReduceMotion() {
		phase := float64(canvas.Now()%pulsePeriod) / float64(pulsePeriod)
		alpha = 0.10 + 0.32*float32(0.5-0.5*math.Cos(2*math.Pi*phase))
		canvas.RequestFrameIn(pulseStep)
	}
	halo := color
	halo.Alpha *= alpha
	canvas.Rounded(ui.Rect{X: x - lightHalo/2, Y: y - lightHalo/2, Width: lightHalo, Height: lightHalo}, lightHalo/2, halo)
	canvas.Rounded(core, lightCore/2, color)
}

// sessionLight is the light of a session: what state it is in, and its color.
func (state *State) sessionLight(session *Session) (lightKind, ui.Color) {
	pal := state.pal
	switch {
	case session.term != nil && session.term.ended:
		return lightIdle, pal.faint
	case session.phase == sshconn.PhaseConnected:
		if session.shellPending() {
			if session.term.shell == nil {
				return lightBusy, pal.warning
			}
			return lightWaiting, pal.warning
		}
		return lightOn, pal.success
	case session.phase == sshconn.PhaseFailed:
		return lightFault, pal.danger
	case session.phase == sshconn.PhaseClosed:
		return lightIdle, pal.faint
	}
	return lightBusy, pal.warning
}

// hostLight is the light of a saved host: that of its session, or an idle ring
// when no connection has been made to it.
func (state *State) hostLight(host Host) (lightKind, ui.Color) {
	best, bestColor := lightIdle, state.pal.faint
	for session := range state.connectionSessions() {
		if session.HostID != host.ID {
			continue
		}
		kind, color := state.sessionLight(session)
		switch kind {
		case lightOn:
			return kind, color
		case lightBusy:
			best, bestColor = kind, color
		case lightWaiting:
			if best != lightBusy {
				best, bestColor = kind, color
			}
		case lightFault:
			if best == lightIdle {
				best, bestColor = kind, color
			}
		}
	}
	return best, bestColor
}
