package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
	"motty/internal/workbench/icons"
	"time"
)

func (state *State) paintIndependentMonitor(canvas *gfx.Canvas, size gfx.Size) {
	if !state.monitorShown(state.deck.ActiveWorkspace()) {
		return
	}
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	if state.monitorDrawer(size) {
		side := components.DrawerRight
		if context.MonitorLeft {
			side = components.DrawerLeft
		}
		d := components.BeginDrawer(canvas, state.Theme(), state.baseWorkspacePixelBounds(size), components.DrawerOptions{Layer: drawerLayer, Scope: dialogScope, DismissID: hitMonitorClose, Side: side, ContentExtent: min(state.monitorPanelWidth(state.deck.ActiveWorkspace(), size), size.Width*.82), InitialFocusID: hitMonitorClose})
		layout := d.Begin("", "")
		state.paintMonitor(canvas, layout.Content, state.contextSession())
		d.End()
		return
	}
	base := state.baseWorkspacePixelBounds(size)
	reserve := state.independentMonitorReserve(state.deck.ActiveWorkspace(), size)
	if !context.MonitorLeft {
		base.X += base.Width - state.aiReserve(state.deck.ActiveWorkspace(), size) - reserve + 1
	}
	base.Width = reserve - 1
	dividerX := base.X + base.Width
	if !context.MonitorLeft {
		dividerX = base.X - 1
	}
	canvas.Fill(gfx.Rect{X: dividerX, Y: base.Y, Width: 1, Height: base.Height}, state.pal.line)
	state.paintMonitor(canvas, base, state.contextSession())
	divider := gfx.Rect{X: dividerX - paneDividerHitWidth/2, Y: base.Y, Width: paneDividerHitWidth, Height: base.Height}
	if canvas.Hovered() == hitMonitorDivider || state.monitorResizing {
		canvas.Fill(gfx.Rect{X: dividerX - 1, Y: base.Y, Width: 2, Height: base.Height}, state.pal.line)
	}
	canvas.HitWithOptions(hitMonitorDivider, divider, state.messages.Monitor.Title, gfx.HitOptions{Role: gfx.RoleSeparator, Cursor: gfx.PointerCursorResizeHorizontal})
}

func (state *State) paintMonitor(canvas *gfx.Canvas, b gfx.Rect, s *Session) {
	if b.Width <= 0 || b.Height <= 0 {
		return
	}
	state.monitorRect = b
	pal, m := state.pal, state.messages.Monitor
	colors := state.monitorColors()
	canvas.Fill(b, colors.background)
	header := gfx.Rect{X: b.X, Y: b.Y, Width: b.Width, Height: infoBarHeight}
	bg, edge := pal.workspace, pal.line
	canvas.Fill(header, bg)
	canvas.DrawIcon(lucideIcons[iconActivity], gfx.Rect{X: b.X + 12, Y: b.Y + (header.Height-16)/2, Width: 16, Height: 16}, pal.muted)
	monitorLabel(canvas, m.Title, gfx.Rect{X: b.X + 35, Y: b.Y, Width: 60, Height: header.Height}, 14, pal.text, gfx.WeightSemibold)
	titleWidth := canvas.TextWidthWeight(m.Title, 14, gfx.WeightSemibold)
	state.monitorMenuRect = gfx.Rect{X: b.X + 35 + titleWidth + 12, Y: b.Y + (header.Height-26)/2, Width: 82, Height: 26}
	state.paintIconButton(canvas, hitMonitorClose, iconClose, gfx.Rect{X: b.X + b.Width - 34, Y: b.Y + (header.Height-24)/2, Width: 24, Height: 24}, state.messages.LabelCloseTab, false, iconButtonQuiet, bg)
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	moveIcon, moveHint := uint8(iconChevronRight), m.MoveRight
	if !context.MonitorLeft {
		moveIcon, moveHint = iconChevronLeft, m.MoveLeft
	}
	state.paintIconButton(canvas, hitMonitorSide, moveIcon, gfx.Rect{X: b.X + b.Width - 64, Y: b.Y + (header.Height-24)/2, Width: 24, Height: 24}, moveHint, false, iconButtonQuiet, bg)
	canvas.Fill(gfx.Rect{X: b.X, Y: b.Y + header.Height - 1, Width: b.Width, Height: 1}, edge)
	y := b.Y + header.Height
	if s == nil || s.stats == nil {
		canvas.LabelCenter(m.NotConnected, gfx.Rect{X: b.X + 16, Y: y + (b.Y+b.Height-y)/2 - 12, Width: b.Width - 32, Height: 24}, gfx.TextStyle{Size: 13, Color: pal.muted})
		return
	}
	v := s.stats
	p := &v.panel
	if p.language != state.language.String() {
		state.formatMonitor(v)
	}
	pause, hint := uint8(iconPause), m.Pause
	if p.paused {
		pause, hint = iconPlay, m.Resume
	}
	state.paintMonitorRefresh(canvas, p, state.monitorMenuRect, v.state == remoteStatsReady)
	state.paintIconButton(canvas, hitMonitorPause, pause, gfx.Rect{X: state.monitorMenuRect.X + state.monitorMenuRect.Width + 4, Y: header.Y + (header.Height-24)/2, Width: 24, Height: 24}, hint, false, iconButtonQuiet, bg)
	state.paintMonitorNavigation(canvas, p, gfx.Rect{X: b.X + 12, Y: y + 6, Width: b.Width - 24, Height: 32})
	y += monitorControlsHeight
	if v.state >= remoteStatsUnsupported || p.count == 0 {
		message := state.remoteStatsText(v)
		unsupportedSystem := !monitorSupported(s.host.System)
		if unsupportedSystem {
			message = m.UnsupportedPlatform
		}
		if p.paused {
			message = m.Paused
		}
		if p.emptyText != message || p.emptyWidth != b.Width-32 {
			p.emptyText, p.emptyWidth = message, b.Width-32
			p.emptyLines = canvas.WrapLines(p.emptyLines[:0], message, 13, gfx.WeightRegular, p.emptyWidth, 4)
		}
		loading := v.state == remoteStatsMeasuring && !p.paused && !unsupportedSystem
		retry := v.state >= remoteStatsUnsupported && !unsupportedSystem
		height := float32(len(p.emptyLines)) * 18
		if loading {
			height += 32
		}
		if retry {
			height += 44
		}
		top := y + max(0, (b.Y+b.Height-y-height)/2)
		if loading {
			phase := components.SpinnerPhase(0)
			if !canvas.ReduceMotion() {
				phase = components.SpinnerPhase(canvas.Now() / (80 * time.Millisecond) % 8)
				canvas.RequestFrameIn(80 * time.Millisecond)
			}
			components.PaintSpinner(canvas, state.Theme(), gfx.Rect{X: b.X + b.Width/2 - 8, Y: top, Width: 16, Height: 16}, phase)
			top += 32
		}
		for _, line := range p.emptyLines {
			canvas.LabelCenter(line, gfx.Rect{X: b.X + 16, Y: top, Width: b.Width - 32, Height: 18}, gfx.TextStyle{Size: 13, Color: colors.muted})
			top += 18
		}
		if retry {
			width := min(b.Width-32, 200)
			components.BeginButton(canvas, state.Theme(), hitMonitorRetry, gfx.Rect{X: b.X + (b.Width-width)/2, Y: top + 12, Width: width, Height: 32}).Secondary(m.Retry)
		}
		return
	}
	body := gfx.Rect{X: b.X, Y: y, Width: b.Width, Height: max(0, b.Y+b.Height-y-24)}
	p.contentHeight = monitorContentHeight(p)
	area := components.BeginScrollArea(canvas, state.Theme(), body, gfx.Size{Width: body.Width, Height: p.contentHeight}).Offset(0, p.scroll).Viewport(hitMonitorScroll, m.Title).VerticalScrollbar(hitMonitorThumb, m.Title, "")
	geo := area.BeginContent()
	p.scroll = geo.OffsetY
	state.paintMonitorSections(canvas, v, geo.Content, geo.Viewport)
	area.EndContent()
	footer := m.Updated
	fc := colors.muted
	if p.paused {
		footer = m.Paused
		fc = pal.warning
	}
	if (p.detailsFailed || p.sample.CPUUnavailable || p.sample.NetworkUnavailable) && !p.paused {
		footer = m.DetailsUnavailable
	}
	canvas.Fill(gfx.Rect{X: b.X, Y: b.Y + b.Height - 24, Width: b.Width, Height: 24}, colors.background)
	canvas.DrawIcon(icons.Clock3, gfx.Rect{X: b.X + 12, Y: b.Y + b.Height - 18, Width: 12, Height: 12}, fc)
	monitorLabel(canvas, footer, gfx.Rect{X: b.X + 30, Y: b.Y + b.Height - 24, Width: b.Width - 42, Height: 24}, 10, fc, gfx.WeightRegular)
}

func monitorLabel(c *gfx.Canvas, text string, b gfx.Rect, size float32, color gfx.Color, weight float32) {
	if b.Width <= 0 || b.Height <= 0 {
		return
	}
	c.Label(elide(c, text, size, weight, b.Width), b, gfx.TextStyle{Size: size, Color: color, Weight: weight})
}

// The existing remote-tools clock frames are shared; only local paint timing
// changes here. Sampling remains owned by the cancellable remote worker.
func (state *State) paintMonitorRefresh(c *gfx.Canvas, p *monitorView, b gfx.Rect, live bool) {
	state.paintIntervalClock(c, p, b, live, hitMonitorInterval, state.messages.Monitor.Interval, state.monitorMenu)
}

// Both monitoring surfaces use the same SVG clock frames and scheduling rules.
func (state *State) paintIntervalClock(c *gfx.Canvas, p *monitorView, b gfx.Rect, live bool, id gfx.ID, label string, expanded bool) {
	theme := state.Theme().WithControlHeight(b.Height)
	components.BeginButton(c, theme, gfx.ID{}, b).WithState(components.ButtonState{Hovered: c.Hovered() == id, Pressed: c.Pressed() == id, Focused: c.FocusVisible() == id}).Ghost("")
	c.HitWithOptions(id, b, label, gfx.HitOptions{Role: gfx.RoleButton, Expandable: true, Expanded: expanded, Value: p.intervalText})
	frame := monitorRefreshFrame(c, p, live)
	clock := gfx.Rect{X: b.X + 6, Y: b.Y + (b.Height-16)/2, Width: 16, Height: 16}
	c.DrawIcon(remoteRefreshClock, clock, theme.MutedForeground)
	c.DrawIcon(remoteRefreshHands[frame], clock, theme.MutedForeground)
	monitorLabel(c, p.intervalText, gfx.Rect{X: b.X + 27, Y: b.Y, Width: b.Width - 43, Height: b.Height}, 12, theme.Foreground, gfx.WeightMedium)
	c.DrawIcon(icons.ChevronDown, gfx.Rect{X: b.X + b.Width - 15, Y: b.Y + (b.Height-11)/2, Width: 11, Height: 11}, theme.MutedForeground)
}

func monitorRefreshFrame(c *gfx.Canvas, p *monitorView, live bool) int {
	running := live && !p.paused && p.count > 0
	now := c.Now()
	if p.refreshReset || running != p.refreshRunning || p.refreshVersion != p.version {
		p.refreshStarted, p.refreshVersion, p.refreshRunning, p.refreshReset = now, p.version, running, false
	}
	frame := 0
	if running && !c.ReduceMotion() {
		period := time.Duration(max(p.interval, 1)) * time.Second
		step := period / monitorClockSteps
		elapsed := max(now-p.refreshStarted, 0)
		frame = int(elapsed*monitorClockSteps/period) % monitorClockSteps * (len(remoteRefreshHands) / monitorClockSteps)
		c.RequestFrameIn(step - elapsed%step)
	}
	return frame
}

// monitorClockSteps is how many of the hand's positions a sweep shows. Each step
// repaints the whole window, so at 60 a 2 s interval kept it at 30 frames a
// second while nothing else changed.
const monitorClockSteps = 12

func (state *State) paintMonitorIntervalMenu(c *gfx.Canvas) {
	if !state.monitorMenu {
		return
	}
	if state.modal != modalNone || state.activeSection != sectionTerminal || state.monitorMenuSession != state.activeSessionID || !state.monitorShown(state.deck.ActiveWorkspace()) || state.monitorRect.Width <= 0 {
		state.monitorMenu = false
		return
	}
	session := state.contextSession()
	if session == nil || session.stats == nil {
		return
	}
	p, b := &session.stats.panel, state.monitorMenuRect
	layer := popupLayer
	if state.monitorDrawer(state.viewport) {
		layer = drawerLayer + 1
	}
	menu := components.BeginDropdownMenu(c, state.Theme(), gfx.Rect{X: b.X, Y: b.Y + b.Height + 4, Width: 295, Height: 144}, components.DropdownMenuOptions{Layer: layer, Scope: hitMonitorMenuDismiss, DismissID: hitMonitorMenuDismiss, Viewport: state.windowRect(), Scroll: state.selectScroll(true, hitMonitorMenuDismiss)})
	ids := [...]gfx.ID{hitMonitorEvery1, hitMonitorEvery2, hitMonitorEvery5}
	for i, seconds := range [...]int{1, 2, 5} {
		menu.AddItem(components.DropdownMenuItem{ID: ids[i], Label: state.messages.Monitor.IntervalLabels[i], Kind: components.DropdownMenuRadio, Selected: p.interval == seconds})
	}
	if session.hasServer() && session.host.System != sshconn.SystemWindows {
		label := state.messages.SudoCommandsEnable
		if session.sudoCommands {
			label = state.messages.SudoCommandsDisable
		}
		menu.AddItem(components.DropdownMenuItem{ID: hitMonitorSudo, Label: label})
	}
	menu.End()
}

func (state *State) scrollMonitor(e gfx.ScrollEvent) (gfx.EventResult, bool) {
	if state.activeSection != sectionTerminal || !state.monitorShown(state.deck.ActiveWorkspace()) || !pointInRect(e.X, e.Y, state.monitorRect) {
		return gfx.EventResult{}, false
	}
	s := state.contextSession()
	if s == nil || s.stats == nil {
		return gfx.EventResult{Handled: true}, true
	}
	p := &s.stats.panel
	next := min(max(p.scroll+e.DeltaY, 0), max(p.contentHeight-(state.monitorRect.Height-infoBarHeight-monitorControlsHeight-24), 0))
	changed := next != p.scroll
	p.scroll = next
	return gfx.EventResult{Handled: true, Repaint: changed}, true
}
