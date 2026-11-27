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
// picture: the pin, the exit button, the keyboard switch and the room the
// latency badge takes, at the spot along its edge, or the free place, it was
// left at. A bar set to auto-hide that the pointer is not on slides under its
// edge and leaves a translucent marker holding the connection light where it
// sits; only that marker, not the rest of the edge, brings the bar out, so
// content along the edge keeps its clicks. The platform client reveals its
// hidden bar from anywhere along the top edge; the marker trades that for
// knowing where the bar is and what it will cover before it comes out.
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
	barHeight := barToolSize + 8
	width := 2*pad + 3*barToolSize + 3*barToolGap + labelWidth
	if latencyWidth > 0 {
		width += barToolGap + latencyWidth
	}
	area := view.area
	bar := state.desktopBar.rect(area, width, barHeight)
	if state.desktopBar.drag.active {
		// While it is dragged the bar is where the pointer has it.
		bar.X, bar.Y = state.desktopBar.drag.rect.X, state.desktopBar.drag.rect.Y
	}
	// The bar's own rect keeps it out once it is out, and the marker with slack
	// around it is what brings it out: both follow the bar, laid out every paint
	// whether hidden or not, and read by the pointer tracking on its next event.
	marker := state.desktopBar.markerRect(bar, area)
	state.desktopBar.bar, state.desktopBar.marker = bar, marker
	state.desktopBar.zone = inflateRect(marker, fullscreenBarRevealSlack)
	s := state.paneBody(session)
	s.fullscreenText, s.fullscreenColor, s.fullscreenHint = "", gfx.Color{}, ""
	s.fullscreenLatency = gfx.Rect{}
	ids := [4]gfx.ID{
		state.paneIdentity(session, hitDesktopBarPin),
		state.paneIdentity(session, hitDesktopFullscreen),
		state.paneIdentity(session, hitDesktopCapture),
		state.paneIdentity(session, hitDesktopBarDock),
	}
	dragID := state.paneIdentity(session, hitDesktopBarDrag)
	// How far out the bar is animates on its drag surface's identity. The
	// fullscreen page lays the bar out every frame, hidden or not, so a slide
	// starts from where the last one stopped.
	target := float32(1)
	if state.desktopBar.autoHide && !state.desktopBar.over && !view.dockMenu && !state.desktopBar.drag.active && !desktopBarFocused(canvas, ids) {
		target = 0
	}
	shown := canvas.Animate(dragID, desktopBarRevealSlot, target, desktopBarSlideMotion)
	if state.desktopBar.autoHide && shown < 1 {
		state.paintDesktopFullscreenMarker(c, session, marker, 1-shown)
	}
	if shown <= 0 {
		state.desktopBar.dragHit, state.desktopBar.labelHit = gfx.ID{}, gfx.ID{}
		return
	}
	slide := state.desktopBar.slide(bar, area)
	bar.X += slide.X * (1 - shown)
	bar.Y += slide.Y * (1 - shown)
	// An edge-docked bar goes whole under its edge; a free one has nowhere to
	// go and gives way to its marker by fading over a short drop.
	fade := float32(1)
	if state.desktopBar.dock == desktopBarDockFree {
		fade = shown
	}
	if latency != "" {
		s.fullscreenText, s.fullscreenColor, s.fullscreenHint = latency, withAlpha(color, color.Alpha*fade), hint
	}
	if s.fullscreenBarDraw == nil {
		s.fullscreenBarDraw = state.paintDesktopFullscreenBar
	}
	state.desktopBar.dragHit, state.desktopBar.labelHit = dragID, ids[1]
	cursor := gfx.PointerCursorHand
	if state.desktopBar.drag.active {
		cursor = gfx.PointerCursorGrabbing
	}
	ui.Slot(c).Absolute().Left(bar.X).Top(bar.Y).Width(bar.Width).Height(bar.Height).
		Opacity(fade).
		HitWith(dragID, "", gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Cursor: cursor}).
		Draw(s.fullscreenBarDraw)
	at := bar.X + pad
	pinHint, pinIcon := m.FullscreenBarAutoHide, iconPin
	if state.desktopBar.autoHide {
		pinHint, pinIcon = m.FullscreenBarPin, iconPinOff
	}
	state.declareIconButton(c, ids[0], pinIcon, pinHint, false, iconButtonQuiet).
		Absolute().Left(at).Top(bar.Y+pad).Size(barToolSize, barToolSize)
	at += barToolSize + barToolGap
	ui.Row(c).Absolute().Left(at).Top(bar.Y + pad).Width(labelWidth).Height(barToolSize).Items(ui.Center).Justify(ui.End).Opacity(fade).Children(func() {
		ui.Button(c, label).XSmall().Ghost().Hit(ids[1], label)
	})
	at += labelWidth + barToolGap
	captureHint, style := fmt.Sprintf(m.TipCapture, desktopEscapeChord), iconButtonActive
	if view.released {
		captureHint, style = fmt.Sprintf(m.TipRelease, desktopEscapeChord), iconButtonQuiet
	}
	state.declareIconButton(c, ids[2], iconKeyboard, captureHint, view.ended, style).
		Absolute().Left(at).Top(bar.Y+pad).Size(barToolSize, barToolSize).Opacity(fade)
	at += barToolSize + barToolGap
	if latency != "" {
		s.fullscreenLatency = gfx.Rect{X: at, Y: bar.Y + pad, Width: latencyWidth, Height: barToolSize}
		at += latencyWidth + barToolGap
	}
	dockButton := state.declareIconButton(c, ids[3], iconEllipsis, m.FullscreenBarDock, false, iconButtonQuiet).
		Absolute().Left(at).Top(bar.Y+pad).Size(barToolSize, barToolSize).Opacity(fade)
	state.declareDesktopBarDockMenu(c, view, dockButton)
}

// paintDesktopFullscreenMarker declares the tab a hidden bar leaves at its
// place: the bar's translucent surface, with the session's connection light in
// its middle. Its painter is bound to the session once, like the picture's.
func (state *State) paintDesktopFullscreenMarker(c ui.C, session *Session, marker gfx.Rect, opacity float32) {
	s := state.paneBody(session)
	s.fullscreenMarkerOpacity = opacity
	if s.fullscreenMarkerDraw == nil {
		id := session.ID
		s.fullscreenMarkerDraw = func(d *ui.DrawContext) {
			if session, ok := state.sessionByIDPointer(id); ok && session.desk != nil {
				state.paintDesktopFullscreenMarkerSurface(d, session, state.paneBody(session).fullscreenMarkerOpacity)
			}
		}
	}
	ui.Slot(c).Absolute().Left(marker.X).Top(marker.Y).Width(marker.Width).Height(marker.Height).Draw(s.fullscreenMarkerDraw)
}

func (state *State) paintDesktopFullscreenMarkerSurface(d *ui.DrawContext, session *Session, opacity float32) {
	pal := state.pal
	d.Canvas.RoundedBorder(d.Rect, desktopBarMarkerDepth/2, 1, withAlpha(pal.line, pal.line.Alpha*opacity), withAlpha(pal.shell, 0.7*opacity))
	kind, light := state.sessionLight(session)
	behind := withAlpha(pal.shell, 0.7*opacity)
	paintLight(d.Canvas, d.Rect.X+d.Rect.Width/2, d.Rect.Y+d.Rect.Height/2, kind, withAlpha(light, light.Alpha*opacity), behind)
}

// desktopBarFocused reports whether the keyboard is on one of the fullscreen
// bar's controls. An unpinned bar then stays out, as the auto-hidden sidebar
// does while one of its rows has the keyboard.
func desktopBarFocused(canvas *gfx.Canvas, ids [4]gfx.ID) bool {
	if canvas == nil {
		return false
	}
	focus := canvas.FocusVisible()
	return focus == ids[0] || focus == ids[1] || focus == ids[2] || focus == ids[3]
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
