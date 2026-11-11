package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
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

func (state *State) paintDesktopFullscreenControls(canvas *gfx.Canvas, view *desktopView, session *Session, area gfx.Rect) {
	canvas.PushLayer(popupLayer)
	defer canvas.PopLayer()
	m := state.messages.Desktop
	label := m.ExitFullscreen + " · " + desktopFullscreenChord
	width := canvas.TextWidthWeight(label, metrics.FontSizeS, gfx.WeightMedium) + 24
	bar := gfx.Rect{X: area.X + 8, Y: area.Y + 8, Width: width + barToolSize + 12, Height: barToolSize + 8}
	latency, color, hint := "", gfx.Color{}, ""
	if state.terminalShowLatency {
		latency, color, hint = state.desktopLatency(view)
		if latency != "" {
			bar.Width += canvas.TextWidthWeight(latency, metrics.FontSizeS, gfx.WeightMedium) + 20
		}
	}
	canvas.RoundedBorder(bar, radius.item, 1, state.pal.line, withAlpha(state.pal.shell, 0.95))
	button := gfx.Rect{X: bar.X + 4, Y: bar.Y + 4, Width: width, Height: barToolSize}
	components.BeginButtonRow(canvas, state.theme, button).Size(components.ButtonSizeXSmall).Paint(
		components.ButtonAction{ID: state.paneIdentity(session, hitDesktopFullscreen), Label: label, Variant: components.ButtonGhost})
	capture := gfx.Rect{X: button.X + button.Width + 4, Y: button.Y, Width: barToolSize, Height: barToolSize}
	captureHint, style := fmt.Sprintf(m.TipCapture, desktopEscapeChord), iconButtonActive
	if view.released {
		captureHint, style = fmt.Sprintf(m.TipRelease, desktopEscapeChord), iconButtonQuiet
	}

	state.paintIconButton(canvas, state.paneIdentity(session, hitDesktopCapture), iconKeyboard, capture, captureHint, view.ended, style, state.pal.shell)
	if latency != "" {
		bounds := gfx.Rect{X: capture.X + capture.Width + 4, Y: button.Y, Width: bar.Width - (capture.X - bar.X) - capture.Width - 8, Height: barToolSize}
		canvas.Label(latency, bounds, gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: color})
		canvas.HitWithOptions(state.paneIdentity(session, hitDesktopLatency), bounds, latency, gfx.HitOptions{Hint: hint})
	}
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
