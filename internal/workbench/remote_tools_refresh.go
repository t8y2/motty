package workbench

import (
	"fmt"
	"math"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
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
	anchor   gfx.Rect
	scroll   components.MenuScroll

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

func (state *State) paintRemoteRefresh(canvas *gfx.Canvas, bounds gfx.Rect) {
	m, theme, auto := state.messages.Tools, state.Theme(), &state.remote.auto
	auto.now = canvas.Now()
	manual := gfx.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Height, Height: bounds.Height}
	state.paintIconButton(canvas, hitRemoteRefresh, iconRefresh, manual, m.RemoteRefresh, state.remote.loading, iconButtonQuiet, theme.Background)
	auto.anchor = gfx.Rect{X: bounds.X + 40, Y: bounds.Y, Width: bounds.Width - 40, Height: bounds.Height}
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
	buttonTheme.Metrics.ControlHeight = auto.anchor.Height
	button := components.BeginButton(canvas, buttonTheme, gfx.ID{}, auto.anchor).WithState(components.ButtonState{
		Hovered: canvas.Hovered() == hitRemoteAutoRefresh, Pressed: canvas.Pressed() == hitRemoteAutoRefresh, Focused: canvas.FocusVisible() == hitRemoteAutoRefresh,
	})
	if auto.enabled {
		button.Secondary("")
		foreground = theme.Primary
	} else {
		button.Outline("")
	}
	canvas.HitWithOptions(hitRemoteAutoRefresh, auto.anchor, auto.accessibleLabel, gfx.HitOptions{Role: gfx.RoleButton, Hint: m.RemoteAutoRefresh, Expanded: auto.open, Expandable: true, Checked: auto.enabled})
	clock := gfx.Rect{X: auto.anchor.X + 9, Y: auto.anchor.Y + (auto.anchor.Height-18)/2, Width: 18, Height: 18}
	phase := 0
	if auto.enabled && !canvas.ReduceMotion() {
		phase = int(max(auto.now-auto.started, 0)*60/auto.period()) % 60
	}
	canvas.DrawIcon(remoteRefreshClock, clock, foreground)
	canvas.DrawIcon(remoteRefreshHands[phase], clock, foreground)
	canvas.Label(label, gfx.Rect{X: clock.X + 24, Y: auto.anchor.Y, Width: max(auto.anchor.Width-56, 0), Height: auto.anchor.Height}, gfx.TextStyle{Size: theme.Metrics.FontSizeM, Weight: gfx.WeightMedium, Color: foreground})
	canvas.DrawIcon(icons.ChevronDown, gfx.Rect{X: auto.anchor.X + auto.anchor.Width - 20, Y: auto.anchor.Y + (auto.anchor.Height-12)/2, Width: 12, Height: 12}, foreground)
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

func (state *State) paintRemoteRefreshMenu(canvas *gfx.Canvas) {
	auto, m := &state.remote.auto, state.messages.Tools
	if !auto.open {
		return
	}
	viewport := gfx.Rect{Width: state.viewport.Width, Height: state.viewport.Height}
	menu := components.BeginDropdownMenu(canvas, state.Theme(), gfx.Rect{
		X: auto.anchor.X + auto.anchor.Width - 188, Y: auto.anchor.Y + auto.anchor.Height + 4,
		Width: 188, Height: float32(1+len(remoteRefreshIntervals))*32 + 8,
	}, components.DropdownMenuOptions{Layer: dialogLayer + 1, Scope: remoteAutoScope, DismissID: hitRemoteAutoDismiss, Viewport: viewport, Scroll: &auto.scroll})
	canvas.SetInitialFocus(hitRemoteAutoToggle)
	label := m.RemoteAutoStart
	if auto.enabled {
		label = m.RemoteAutoStop
	}
	menu.AddItem(components.DropdownMenuItem{ID: hitRemoteAutoToggle, Label: label, Kind: components.DropdownMenuCheckbox, Checked: auto.enabled})
	for i, period := range remoteRefreshIntervals {
		menu.AddItem(components.DropdownMenuItem{ID: hitRemoteIntervals.At(i), Label: fmt.Sprintf(m.RemoteRefreshIntervalFmt, int(period/time.Second)), Kind: components.DropdownMenuRadio, Selected: auto.enabled && auto.period() == period})
	}
	menu.End()
}

func (state *State) remoteRefreshKey(event gfx.KeyEvent) gfx.EventResult {
	auto := &state.remote.auto
	if !state.remoteToolsVisible() || event.Release {
		return gfx.EventResult{}
	}
	if auto.open {
		if event.Key == gfx.KeyEscape {
			auto.open = false
			return gfx.EventResult{Focus: hitRemoteAutoRefresh, FocusVisible: true, Handled: true, Repaint: true}
		}
		if event.Key == gfx.KeyTab {
			auto.open = false
			return gfx.EventResult{Handled: true, Repaint: true, ContinueTraversal: true}
		}
		if id, ok := auto.scroll.Navigate(event.Key, event.Target, true); ok {
			return gfx.EventResult{Focus: id, FocusVisible: true, Handled: true, Repaint: true}
		}
	} else if event.Target == hitRemoteAutoRefresh && event.Key == gfx.KeyDown {
		auto.open = true
		return gfx.EventResult{Focus: hitRemoteAutoToggle, FocusVisible: true, Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
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
