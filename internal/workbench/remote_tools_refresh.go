package workbench

import (
	"fmt"
	"math"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/workbench/icons"
)

var remoteRefreshIntervals = [...]time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute}

type remoteAutoRefresh struct {
	enabled  bool
	open     bool
	pending  bool
	interval time.Duration
	now      time.Duration
	started  time.Duration
	next     time.Duration

	// The clock animates even when its interval and accessible text are unchanged.
	label, accessibleLabel   string
	labelFormat, labelAction string
	labelSeconds             int
}

func (auto *remoteAutoRefresh) period() time.Duration {
	if auto.interval <= 0 {
		return 10 * time.Second
	}
	return auto.interval
}

func (auto *remoteAutoRefresh) restart() {
	auto.open = false
	auto.started, auto.next = auto.now, auto.now+auto.period()
}

// Refresh is driven by the window's monotonic frame clock, without a polling
// goroutine or a separate timer. Resuming a window
// attempts at most one refresh rather than replaying missed ticks.
func (state *State) tickRemoteRefresh(now time.Duration) {
	auto := &state.remote.auto
	if state.closed || !auto.enabled || now < auto.next {
		return
	}
	auto.now = now
	auto.started, auto.next = now, now+auto.period()
	if state.remoteToolsVisible() && !state.remote.loading {
		state.refreshRemoteTools()
	}
}

// drawRemoteRefreshControl paints the manual refresh button inside its page
// control: the tools window and the workbench modal share it.
func (state *State) drawRemoteRefreshControl(d *ui.DrawContext) {
	canvas, theme := d.Canvas, *d.Theme
	theme.Metrics.ControlHeight = d.Rect.Height
	button := components.BeginButton(canvas, theme, gfx.ID{}, d.Rect).WithState(components.ButtonState{
		Hovered: d.State.Hovered, Pressed: d.State.Pressed, Focused: d.State.Focused,
	})
	if state.remote.loading {
		button = button.Disabled()
	}
	button.Ghost("")
	foreground := state.pal.muted
	if d.State.Hovered || d.State.Pressed {
		foreground = state.pal.text
	}
	if state.remote.loading {
		foreground.Alpha *= 0.4
	}
	size := min(d.Rect.Width, d.Rect.Height) * .375
	canvas.DrawIcon(lucideIcons[iconRefresh], gfx.Rect{
		X: d.Rect.X + (d.Rect.Width-size)/2, Y: d.Rect.Y + (d.Rect.Height-size)/2,
		Width: size, Height: size,
	}, foreground)
}

// drawRemoteAutoControl paints the auto-refresh button, including its countdown
// and its clock hands.
func (state *State) drawRemoteAutoControl(d *ui.DrawContext) {
	canvas := d.Canvas
	m, theme, auto := state.messages.Tools, *d.Theme, &state.remote.auto
	auto.now = canvas.Now()
	format, seconds := m.RemoteAutoRefresh, 0
	if auto.enabled {
		format, seconds = m.RemoteRefreshIntervalFmt, int(auto.period()/time.Second)
	}
	if auto.label == "" || auto.labelFormat != format || auto.labelSeconds != seconds || auto.labelAction != m.RemoteAutoRefresh {
		auto.labelFormat, auto.labelSeconds, auto.labelAction = format, seconds, m.RemoteAutoRefresh
		auto.label = format
		if auto.enabled {
			auto.label = fmt.Sprintf(format, seconds)
		}
		auto.accessibleLabel = m.RemoteAutoRefresh + ": " + auto.label
	}
	label, foreground := auto.label, theme.MutedForeground
	buttonTheme := theme
	buttonTheme.Metrics.ControlHeight = d.Rect.Height
	button := components.BeginButton(canvas, buttonTheme, gfx.ID{}, d.Rect).WithState(components.ButtonState{
		Hovered: d.State.Hovered, Pressed: d.State.Pressed, Focused: d.State.Focused,
	})
	if auto.enabled {
		button.Secondary("")
		foreground = theme.Primary
	} else {
		button.Outline("")
	}
	clock := gfx.Rect{X: d.Rect.X + 9, Y: d.Rect.Y + (d.Rect.Height-18)/2, Width: 18, Height: 18}
	phase := 0
	if auto.enabled && !canvas.ReduceMotion() {
		phase = int(max(auto.now-auto.started, 0)*60/auto.period()) % 60
	}
	canvas.DrawIcon(remoteRefreshClock, clock, foreground)
	canvas.DrawIcon(remoteRefreshHands[phase], clock, foreground)
	canvas.Label(label, gfx.Rect{X: clock.X + 24, Y: d.Rect.Y, Width: max(d.Rect.Width-56, 0), Height: d.Rect.Height}, gfx.TextStyle{Size: theme.Metrics.FontSizeM, Weight: gfx.WeightMedium, Color: foreground})
	canvas.DrawIcon(icons.ChevronDown, gfx.Rect{X: d.Rect.X + d.Rect.Width - 20, Y: d.Rect.Y + (d.Rect.Height-12)/2, Width: 12, Height: 12}, foreground)
	if auto.enabled {
		remaining := max(auto.next-auto.now, 0)
		delay := remaining
		if !canvas.ReduceMotion() {
			step := auto.period() / 60
			delay = min(remaining, step-(max(auto.now-auto.started, 0)%step))
		}
		if remaining > 0 {
			canvas.RequestFrameIn(delay)
		}
		if remaining == 0 && !auto.pending {
			// Ordinary animation paints have no AfterPaint event pass. Queue the
			// tick through the owning window so network work starts outside paint.
			auto.pending = true
			now, session, serial := auto.now, state.remote.session, state.remote.serial
			state.postRemoteToolsUpdate(func(s *State) {
				s.remote.auto.pending = false
				if s.remote.session != session || s.remote.serial != serial {
					return
				}
				s.tickRemoteRefresh(now)
			})
		}
	}
}

// remoteAutoAccessibleLabel is the auto-refresh control's name for assistive
// technology. It is cached with the clock's label so a paint does not allocate.
func (state *State) remoteAutoAccessibleLabel() string {
	m, auto := state.messages.Tools, &state.remote.auto
	format, seconds := m.RemoteAutoRefresh, 0
	if auto.enabled {
		format, seconds = m.RemoteRefreshIntervalFmt, int(auto.period()/time.Second)
	}
	if auto.accessibleLabel == "" || auto.labelFormat != format || auto.labelSeconds != seconds || auto.labelAction != m.RemoteAutoRefresh {
		auto.labelFormat, auto.labelSeconds, auto.labelAction = format, seconds, m.RemoteAutoRefresh
		auto.label = format
		if auto.enabled {
			auto.label = fmt.Sprintf(format, seconds)
		}
		auto.accessibleLabel = m.RemoteAutoRefresh + ": " + auto.label
	}
	return auto.accessibleLabel
}

// declareRemoteAutoMenu declares the auto-refresh interval menu: the
// start/stop switch, then one row per interval. The tools window and the
// workbench modal share it.
func (state *State) declareRemoteAutoMenu(menu *ui.MenuItems) {
	m := state.messages.Tools
	auto := &state.remote.auto
	label := m.RemoteAutoStart
	if auto.enabled {
		label = m.RemoteAutoStop
	}
	if menu.Item(label).Checked(auto.enabled).Clicked() {
		auto.enabled = !auto.enabled
		auto.restart()
	}
	for i, period := range remoteRefreshIntervals {
		chosen := auto.enabled && auto.period() == period
		if menu.Item(fmt.Sprintf(m.RemoteRefreshIntervalFmt, int(period/time.Second))).Chosen(chosen).Clicked() {
			auto.interval = remoteRefreshIntervals[i]
			auto.enabled = true
			auto.restart()
		}
	}
}

// remoteAutoControlPointer opens or closes the interval menu on a primary
// release.
func (state *State) remoteAutoControlPointer(event gfx.PointerEvent) gfx.EventResult {
	if event.Kind != gfx.PointerUp || event.Button != gfx.PointerButtonPrimary {
		return gfx.EventResult{}
	}
	state.remote.auto.open = !state.remote.auto.open
	return gfx.EventResult{Handled: true, Repaint: true}
}

// remoteAutoControlKey opens the interval menu with Down or toggles it with
// Enter and Space.
func (state *State) remoteAutoControlKey(event gfx.KeyEvent) gfx.EventResult {
	if event.Release || event.Repeat {
		return gfx.EventResult{}
	}
	switch event.Key {
	case gfx.KeyDown:
		state.remote.auto.open = true
	case gfx.KeyEnter, gfx.KeySpace:
		state.remote.auto.open = !state.remote.auto.open
	default:
		return gfx.EventResult{}
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

// refreshRemoteToolsExplicit reads the current tab again, rechecking Docker
// availability too. The tools window and the workbench modal share it.
func (state *State) refreshRemoteToolsExplicit() {
	if state.remote.loading {
		return
	}
	state.remote.noDocker = false
	state.refreshRemoteTools()
}

// remoteRefreshControlPointer runs the explicit refresh on a primary release.
func (state *State) remoteRefreshControlPointer(event gfx.PointerEvent) gfx.EventResult {
	if event.Kind != gfx.PointerUp || event.Button != gfx.PointerButtonPrimary {
		return gfx.EventResult{}
	}
	state.refreshRemoteToolsExplicit()
	return gfx.EventResult{Handled: true, Repaint: true}
}

// remoteRefreshControlKey runs the explicit refresh from the keyboard.
func (state *State) remoteRefreshControlKey(event gfx.KeyEvent) gfx.EventResult {
	if event.Release || event.Repeat {
		return gfx.EventResult{}
	}
	if event.Key != gfx.KeyEnter && event.Key != gfx.KeySpace {
		return gfx.EventResult{}
	}
	state.refreshRemoteToolsExplicit()
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (state *State) paintRemoteDockerEmpty(canvas *gfx.Canvas, bounds gfx.Rect) {
	m := state.messages.Tools
	components.Empty(canvas, state.Theme(), bounds, components.EmptyData{
		Media: components.EmptyMediaDefault, Icon: icons.LinuxDocker,
		Title: m.RemoteDockerMissing, Description: m.RemoteDockerMissingHint,
	})
}

func (state *State) paintRemoteToolsLoading(canvas *gfx.Canvas, bounds gfx.Rect) {
	center := bounds.Y + bounds.Height/2
	phase := components.SpinnerPhase(0)
	if !canvas.ReduceMotion() {
		phase = components.SpinnerPhase(canvas.Now() / (80 * time.Millisecond) % 8)
		canvas.RequestFrameIn(80 * time.Millisecond)
	}
	components.PaintSpinner(canvas, state.Theme(), gfx.Rect{X: bounds.X + bounds.Width/2 - 8, Y: center - 24, Width: 16, Height: 16}, phase)
	canvas.LabelCenter(state.messages.Tools.RemoteLoading, gfx.Rect{X: bounds.X, Y: center, Width: bounds.Width, Height: 24}, gfx.TextStyle{Size: state.Theme().Metrics.FontSizeM, Color: state.pal.muted})
}

// Small, bounded vector frames keep fractional-DPI rendering crisp and avoid
// rebuilding paths or accumulating tessellation-cache entries during a sweep.
var remoteRefreshClock = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(10, 2), gfx.LineTo(14, 2),
	gfx.MoveTo(20, 14), gfx.CubicTo(20, 18.418, 16.418, 22, 12, 22), gfx.CubicTo(7.582, 22, 4, 18.418, 4, 14),
	gfx.CubicTo(4, 9.582, 7.582, 6, 12, 6), gfx.CubicTo(16.418, 6, 20, 9.582, 20, 14), gfx.ClosePath(),
}}

var remoteRefreshHands = func() (frames [60]gfx.Icon) {
	for i := range frames {
		angle := float64(i) * math.Pi / 30
		frames[i] = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Commands: []gfx.PathCommand{
			gfx.MoveTo(12, 14), gfx.LineTo(12+float32(math.Sin(angle)*6), 14-float32(math.Cos(angle)*6)),
		}}
	}
	return
}()
