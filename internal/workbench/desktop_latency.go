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
	canvas.Rounded(bounds, cornerPx(4), withAlpha(badge.color, 0.14))
	canvas.Label(badge.label, gfx.Rect{X: bounds.X + 6, Y: bounds.Y, Width: badge.width, Height: bounds.Height},
		gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: badge.color})
	canvas.HitWithOptions(id, bounds, badge.label,
		gfx.HitOptions{Role: gfx.RoleStatus, SkipFocus: true, Hint: badge.hint})
}

// declareDesktopFullscreenControls declares the fullscreen view's bar over the
// top of the picture, centred like the platform remote desktop client's
// connection bar: the pin, the exit button, the keyboard switch and the room the
// latency badge takes. A bar set to auto-hide is declared only while the pointer
// reaches the top edge or the keyboard is on one of its controls; otherwise a
// press there belongs to the remote picture, as it does in the platform client.
func (state *State) declareDesktopFullscreenControls(c ui.C, view *desktopView, session *Session) {
	m := state.messages.Desktop
	canvas := c.Canvas()
	label := m.ExitFullscreen + " · " + desktopFullscreenChord
	labelWidth := canvas.TextWidthWeight(label, metrics.FontSizeS, gfx.WeightMedium) + 24
	latency, color, hint := "", gfx.Color{}, ""
	latencyWidth := float32(0)
	if state.terminalShowLatency {
		latency, color, hint = state.desktopLatency(view)
		if latency != "" {
			latencyWidth = canvas.TextWidthWeight(latency, metrics.FontSizeS, gfx.WeightMedium) + 12
		}
	}
	const pad = float32(4)
	width := 2*pad + 2*barToolSize + 2*barToolGap + labelWidth
	if latencyWidth > 0 {
		width += barToolGap + latencyWidth
	}
	area := view.area
	bar := gfx.Rect{X: max((area.Width-width)/2, 8), Y: 8, Width: width, Height: barToolSize + 8}
	s := state.paneBody(session)
	s.fullscreenText, s.fullscreenColor, s.fullscreenHint = "", gfx.Color{}, ""
	s.fullscreenLatency = gfx.Rect{}
	// The band that reveals an unpinned bar, for the pointer tracking to read on
	// the next event. It follows the bar even while the bar is hidden, so the
	// paint after the pointer arrives can show it.
	state.desktopBar.zone = gfx.Rect{
		X: area.X, Y: area.Y, Width: area.Width,
		Height: bar.Y + bar.Height + fullscreenBarRevealSlack,
	}
	ids := [3]gfx.ID{
		state.paneIdentity(session, hitDesktopBarPin),
		state.paneIdentity(session, hitDesktopFullscreen),
		state.paneIdentity(session, hitDesktopCapture),
	}
	if state.desktopBar.autoHide && !state.desktopBar.over && !desktopBarFocused(canvas, ids) {
		return
	}
	if latency != "" {
		s.fullscreenText, s.fullscreenColor, s.fullscreenHint = latency, color, hint
		left := bar.X + pad + barToolSize + barToolGap + labelWidth + barToolGap + barToolSize + barToolGap
		s.fullscreenLatency = gfx.Rect{X: left, Y: bar.Y + pad, Width: latencyWidth, Height: barToolSize}
	}
	if s.fullscreenBarDraw == nil {
		s.fullscreenBarDraw = state.paintDesktopFullscreenBar
	}
	ui.Slot(c).Absolute().Left(bar.X).Top(bar.Y).Width(bar.Width).Height(bar.Height).Draw(s.fullscreenBarDraw)
	at := bar.X + pad
	pinHint, pinIcon := m.FullscreenBarAutoHide, iconPin
	if state.desktopBar.autoHide {
		pinHint, pinIcon = m.FullscreenBarPin, iconPinOff
	}
	state.declareIconButton(c, ids[0], pinIcon, pinHint, false, iconButtonQuiet).
		Absolute().Left(at).Top(bar.Y+pad).Size(barToolSize, barToolSize)
	at += barToolSize + barToolGap
	ui.Row(c).Absolute().Left(at).Top(bar.Y + pad).Width(labelWidth).Height(barToolSize).Items(ui.Center).Justify(ui.End).Children(func() {
		ui.Button(c, label).XSmall().Ghost().Hit(ids[1], label)
	})
	at += labelWidth + barToolGap
	captureHint, style := fmt.Sprintf(m.TipCapture, desktopEscapeChord), iconButtonActive
	if view.released {
		captureHint, style = fmt.Sprintf(m.TipRelease, desktopEscapeChord), iconButtonQuiet
	}
	state.declareIconButton(c, ids[2], iconKeyboard, captureHint, view.ended, style).
		Absolute().Left(at).Top(bar.Y+pad).Size(barToolSize, barToolSize)
}

// desktopBarFocused reports whether the keyboard is on one of the fullscreen
// bar's controls. An unpinned bar then stays out, as the auto-hidden sidebar
// does while one of its rows has the keyboard.
func desktopBarFocused(canvas *gfx.Canvas, ids [3]gfx.ID) bool {
	if canvas == nil {
		return false
	}
	focus := canvas.FocusVisible()
	return focus == ids[0] || focus == ids[1] || focus == ids[2]
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
