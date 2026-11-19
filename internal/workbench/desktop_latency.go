package workbench

import (
	"fmt"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/desktop"
)

func (state *State) desktopLatency(view *desktopView) (string, gfx.Color, string) {
	source, ok := view.session.(desktop.LatencySource)
	if view.ended || view.session.Info().Protocol != "RDP" {
		return "", gfx.Color{}, ""
	}
	if !ok {
		return "RTT —", state.pal.faint, state.messages.Desktop.LatencyUnavailable
	}
	rtt, sampled := source.NetworkRTT()
	if sampled.IsZero() {
		return "RTT —", state.pal.faint, state.messages.Desktop.LatencyUnavailable
	}
	ms := rtt.Milliseconds()
	return fmt.Sprintf("RTT %dms", ms), state.latencyColor(ms),
		fmt.Sprintf(state.messages.Desktop.LatencyHintFmt, sampled.Local().Format("15:04:05"))
}

// desktopLatencyUI is one desktop's RTT badge as the status strip shows it: the
// text, its color and hint, and how wide the text is. The strip's Frame reserves
// a slot for it; paintDesktopLatency draws the badge into it. A desktop pane has
// its own badge, so split panes do not share one.
type desktopLatencyUI struct {
	label string
	color gfx.Color
	hint  string
	width float32
}

// desktopLatencyBadge reads what the status strip's badge says and measures it.
// It reserves no element: the strip's layout gives the badge a slot, because the
// strip is laid out by a Frame.
func (state *State) desktopLatencyBadge(canvas *gfx.Canvas, view *desktopView) (desktopLatencyUI, bool) {
	var badge desktopLatencyUI
	if !state.terminalShowLatency {
		return badge, false
	}
	label, color, hint := state.desktopLatency(view)
	if label == "" {
		return badge, false
	}
	badge.label, badge.color, badge.hint = label, color, hint
	badge.width = canvas.TextWidthWeight(label, metrics.FontSizeS, gfx.WeightMedium) + 2
	return badge, true
}

// paintDesktopLatency draws the badge in the rectangle the status strip reserved
// for it: the RTT in the sample's color over a wash of it, with the sample time
// as the hover hint. The same identity the overlay and chromeless presentations
// use names the badge.
func (state *State) paintDesktopLatency(canvas *gfx.Canvas, bounds gfx.Rect, badge desktopLatencyUI, id gfx.ID) {
	canvas.Rounded(bounds, 4, withAlpha(badge.color, 0.14))
	canvas.Label(badge.label, gfx.Rect{X: bounds.X + 6, Y: bounds.Y, Width: badge.width, Height: bounds.Height},
		gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: badge.color})
	canvas.HitWithOptions(id, bounds, badge.label,
		gfx.HitOptions{Role: gfx.RoleStatus, SkipFocus: true, Hint: badge.hint})
}

// declareDesktopFullscreenControls declares the fullscreen view's bar in the
// page: the exit button, the keyboard switch, and the room the latency badge
// takes, over the top-left corner of the picture. The badge's text is painted
// after the page, as the status strip's own badge is.
func (state *State) declareDesktopFullscreenControls(c ui.C, view *desktopView, session *Session) {
	m := state.messages.Desktop
	canvas := c.Canvas()
	label := m.ExitFullscreen + " · " + desktopFullscreenChord
	width := canvas.TextWidthWeight(label, metrics.FontSizeS, gfx.WeightMedium) + 24
	bar := gfx.Rect{X: 8, Y: 8, Width: width + barToolSize + 12, Height: barToolSize + 8}
	latency, color, hint := "", gfx.Color{}, ""
	if state.terminalShowLatency {
		latency, color, hint = state.desktopLatency(view)
		if latency != "" {
			bar.Width += canvas.TextWidthWeight(latency, metrics.FontSizeS, gfx.WeightMedium) + 20
		}
	}
	s := state.paneBody(session)
	s.fullscreenText, s.fullscreenColor, s.fullscreenHint = latency, color, hint
	s.fullscreenLatency = gfx.Rect{}
	if latency != "" {
		x := bar.X + 4 + width + 4 + barToolSize + 4
		s.fullscreenLatency = gfx.Rect{X: x, Y: bar.Y + 4, Width: bar.Width - (x - bar.X) - barToolSize - 8, Height: barToolSize}
	}
	if s.fullscreenBarDraw == nil {
		s.fullscreenBarDraw = state.paintDesktopFullscreenBar
	}
	ui.Slot(c).Absolute().Left(bar.X).Top(bar.Y).Width(bar.Width).Height(bar.Height).Draw(s.fullscreenBarDraw)
	ui.Row(c).Absolute().Left(bar.X + 4).Top(bar.Y + 4).Width(width).Height(barToolSize).Items(ui.Center).Justify(ui.End).Children(func() {
		ui.Button(c, label).XSmall().Ghost().Hit(state.paneIdentity(session, hitDesktopFullscreen), label)
	})
	captureHint, style := fmt.Sprintf(m.TipCapture, desktopEscapeChord), iconButtonActive
	if view.released {
		captureHint, style = fmt.Sprintf(m.TipRelease, desktopEscapeChord), iconButtonQuiet
	}
	state.declareIconButton(c, state.paneIdentity(session, hitDesktopCapture), iconKeyboard, captureHint, view.ended, style).
		Absolute().Left(bar.X+4+width+4).Top(bar.Y+4).Size(barToolSize, barToolSize)
}

// paintDesktopFullscreenBar paints the fullscreen bar's surface under its
// controls: the same rounded border the canvas drew before the controls were
// page elements.
func (state *State) paintDesktopFullscreenBar(d *ui.DrawContext) {
	d.Canvas.RoundedBorder(d.Rect, radius.item, 1, state.pal.line, withAlpha(state.pal.shell, 0.95))
}

// paintDesktopFullscreenLatency paints the fullscreen bar's latency badge in
// the room its declaration reserved, over the page's bar.
func (state *State) paintDesktopFullscreenLatency(canvas *gfx.Canvas, session *Session) {
	s := state.paneBody(session)
	if s.fullscreenText == "" {
		return
	}
	bounds := s.fullscreenLatency
	canvas.Label(s.fullscreenText, bounds, gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: s.fullscreenColor})
	canvas.HitWithOptions(state.paneIdentity(session, hitDesktopLatency), bounds, s.fullscreenText, gfx.HitOptions{Hint: s.fullscreenHint})
}

func (state *State) paintChromelessLatency(canvas *gfx.Canvas, view *desktopView, session *Session, area gfx.Rect) {
	if !state.terminalShowLatency {
		return
	}
	label, color, hint := state.desktopLatency(view)
	if label == "" {
		return
	}
	canvas.PushLayer(popupLayer)
	defer canvas.PopLayer()
	width := canvas.TextWidthWeight(label, metrics.FontSizeS, gfx.WeightMedium) + 16
	bounds := gfx.Rect{X: area.X + 8, Y: area.Y + area.Height - 30, Width: width, Height: 22}
	canvas.RoundedBorder(bounds, radius.item, 1, state.pal.line, withAlpha(state.pal.shell, 0.95))
	canvas.Label(label, gfx.Rect{X: bounds.X + 8, Y: bounds.Y, Width: bounds.Width - 16, Height: bounds.Height},
		gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: color})
	canvas.HitWithOptions(state.paneIdentity(session, hitDesktopLatency), bounds, label, gfx.HitOptions{Role: gfx.RoleStatus, SkipFocus: true, Hint: hint})
}
