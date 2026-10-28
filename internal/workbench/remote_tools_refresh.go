package workbench

import (
	"fmt"
	"math"
	"time"

	"github.com/vivaui/viva/components"
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
	anchor   ui.Rect
	scroll   components.MenuScroll
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
	if state.modal == modalRemoteTools && !state.remote.loading {
		state.refreshRemoteTools()
	}
}

func (state *State) paintRemoteRefresh(canvas *ui.Canvas, bounds ui.Rect) {
	m, theme, auto := state.messages.Tools, state.Theme(), &state.remote.auto
	auto.now = canvas.Now()
	manual := ui.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Height, Height: bounds.Height}
	state.paintIconButton(canvas, hitRemoteRefresh, iconRefresh, manual, m.RemoteRefresh, state.remote.loading, iconButtonQuiet, theme.Background)
	auto.anchor = ui.Rect{X: bounds.X + 40, Y: bounds.Y, Width: bounds.Width - 40, Height: bounds.Height}
	label, foreground := m.RemoteAutoRefresh, theme.MutedForeground
	if auto.enabled {
		label = fmt.Sprintf(m.RemoteRefreshIntervalFmt, int(auto.period()/time.Second))
	}
	buttonTheme := theme
	buttonTheme.Metrics.ControlHeight = auto.anchor.Height
	button := components.BeginButton(canvas, buttonTheme, ui.ID{}, auto.anchor).WithState(components.ButtonState{
		Hovered: canvas.Hovered() == hitRemoteAutoRefresh, Pressed: canvas.Pressed() == hitRemoteAutoRefresh, Focused: canvas.FocusVisible() == hitRemoteAutoRefresh,
	})
	if auto.enabled {
		button.Secondary("")
		foreground = theme.Primary
	} else {
		button.Outline("")
	}
	canvas.HitWithOptions(hitRemoteAutoRefresh, auto.anchor, m.RemoteAutoRefresh+": "+label, ui.HitOptions{Role: ui.RoleButton, Hint: m.RemoteAutoRefresh, Expanded: auto.open, Expandable: true, Checked: auto.enabled})
	clock := ui.Rect{X: auto.anchor.X + 9, Y: auto.anchor.Y + (auto.anchor.Height-18)/2, Width: 18, Height: 18}
	phase := 0
	if auto.enabled && !canvas.ReduceMotion() {
		phase = int(max(auto.now-auto.started, 0)*60/auto.period()) % 60
	}
	canvas.DrawIcon(remoteRefreshClock, clock, foreground)
	canvas.DrawIcon(remoteRefreshHands[phase], clock, foreground)
	canvas.Label(label, ui.Rect{X: clock.X + 24, Y: auto.anchor.Y, Width: max(auto.anchor.Width-56, 0), Height: auto.anchor.Height}, ui.TextStyle{Size: theme.Metrics.FontSizeM, Weight: ui.WeightMedium, Color: foreground})
	canvas.DrawIcon(icons.ChevronDown, ui.Rect{X: auto.anchor.X + auto.anchor.Width - 20, Y: auto.anchor.Y + (auto.anchor.Height-12)/2, Width: 12, Height: 12}, foreground)
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

func (state *State) paintRemoteRefreshMenu(canvas *ui.Canvas) {
	auto, m := &state.remote.auto, state.messages.Tools
	if !auto.open {
		return
	}
	viewport := ui.Rect{Width: state.viewport.Width, Height: state.viewport.Height}
	menu := components.BeginDropdownMenu(canvas, state.Theme(), ui.Rect{
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

func (state *State) remoteRefreshKey(event ui.KeyEvent) ui.EventResult {
	auto := &state.remote.auto
	if state.modal != modalRemoteTools || event.Release {
		return ui.EventResult{}
	}
	if auto.open {
		if event.Key == ui.KeyEscape {
			auto.open = false
			return ui.EventResult{Focus: hitRemoteAutoRefresh, FocusVisible: true, Handled: true, Repaint: true}
		}
		if event.Key == ui.KeyTab {
			auto.open = false
			return ui.EventResult{Handled: true, Repaint: true, ContinueTraversal: true}
		}
		if id, ok := auto.scroll.Navigate(event.Key, event.Target, true); ok {
			return ui.EventResult{Focus: id, FocusVisible: true, Handled: true, Repaint: true}
		}
	} else if event.Target == hitRemoteAutoRefresh && event.Key == ui.KeyDown {
		auto.open = true
		return ui.EventResult{Focus: hitRemoteAutoToggle, FocusVisible: true, Handled: true, Repaint: true}
	}
	return ui.EventResult{}
}

func (state *State) paintRemoteDockerEmpty(canvas *ui.Canvas, bounds ui.Rect) {
	m := state.messages.Tools
	components.Empty(canvas, state.Theme(), bounds, components.EmptyData{
		Media: components.EmptyMediaDefault, Icon: icons.LinuxDocker,
		Title: m.RemoteDockerMissing, Description: m.RemoteDockerMissingHint,
	})
}

func (state *State) paintRemoteToolsLoading(canvas *ui.Canvas, bounds ui.Rect) {
	center := bounds.Y + bounds.Height/2
	phase := components.SpinnerPhase(0)
	if !canvas.ReduceMotion() {
		phase = components.SpinnerPhase(canvas.Now() / (80 * time.Millisecond) % 8)
		canvas.RequestFrameIn(80 * time.Millisecond)
	}
	components.PaintSpinner(canvas, state.Theme(), ui.Rect{X: bounds.X + bounds.Width/2 - 8, Y: center - 24, Width: 16, Height: 16}, phase)
	canvas.LabelCenter(state.messages.Tools.RemoteLoading, ui.Rect{X: bounds.X, Y: center, Width: bounds.Width, Height: 24}, ui.TextStyle{Size: state.Theme().Metrics.FontSizeM, Color: state.pal.muted})
}

// Small, bounded vector frames keep fractional-DPI rendering crisp and avoid
// rebuilding paths or accumulating tessellation-cache entries during a sweep.
var remoteRefreshClock = ui.Icon{ViewBox: 24, Stroke: 2, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
	ui.MoveTo(10, 2), ui.LineTo(14, 2),
	ui.MoveTo(20, 14), ui.CubicTo(20, 18.418, 16.418, 22, 12, 22), ui.CubicTo(7.582, 22, 4, 18.418, 4, 14),
	ui.CubicTo(4, 9.582, 7.582, 6, 12, 6), ui.CubicTo(16.418, 6, 20, 9.582, 20, 14), ui.ClosePath(),
}}

var remoteRefreshHands = func() (frames [60]ui.Icon) {
	for i := range frames {
		angle := float64(i) * math.Pi / 30
		frames[i] = ui.Icon{ViewBox: 24, Stroke: 2, Cap: ui.CapRound, Commands: []ui.PathCommand{
			ui.MoveTo(12, 14), ui.LineTo(12+float32(math.Sin(angle)*6), 14-float32(math.Cos(angle)*6)),
		}}
	}
	return
}()
