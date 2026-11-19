package workbench

import (
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	"motty/internal/workbench/icons"
)

// monitorClockSteps is how many of the hand's positions a sweep shows. Each step
// repaints the whole window, so at 60 a 2 s interval kept it at 30 frames a
// second while nothing else changed.
const monitorClockSteps = 12

// monitorPageUI is what the monitor panel's page declaration leaves for its
// paint: the scroll area, and the interval control the menu hangs from.
type monitorPageUI struct {
	scroll ui.Box
}

// monitorBaseBounds is the docked monitor's rectangle. Drawer mode uses the
// whole window edge and reports false.
func (state *State) monitorBaseBounds(size gfx.Size) (gfx.Rect, bool) {
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	base := state.baseWorkspacePixelBounds(size)
	reserve := state.independentMonitorReserve(state.deck.ActiveWorkspace(), size)
	if reserve == 0 {
		extent := min(state.monitorPanelWidth(state.deck.ActiveWorkspace(), size), size.Width*.82)
		x := max(size.Width-extent, 0)
		if context.MonitorLeft {
			x = 0
		}
		return gfx.Rect{X: x, Y: 0, Width: extent, Height: size.Height}, true
	}
	if !context.MonitorLeft {
		base.X += base.Width - state.aiReserve(state.deck.ActiveWorkspace(), size) - reserve + 1
	}
	base.Width = reserve - 1
	return base, false
}

// paintIndependentMonitor paints only the divider of a docked monitor; its
// panel is the page's. Drawer mode draws nothing here.
func (state *State) paintIndependentMonitor(canvas *gfx.Canvas, size gfx.Size) {
	if !state.monitorShown(state.deck.ActiveWorkspace()) || state.monitorDrawer(size) {
		return
	}
	base := state.monitorRect
	if base.Width <= 0 {
		return
	}
	dividerX := base.X + base.Width
	if c, _ := state.deck.Context(state.deck.ActiveWorkspace()); !c.MonitorLeft {
		dividerX = base.X - 1
	}
	canvas.Fill(gfx.Rect{X: dividerX, Y: base.Y, Width: 1, Height: base.Height}, state.pal.line)
	divider := gfx.Rect{X: dividerX - paneDividerHitWidth/2, Y: base.Y, Width: paneDividerHitWidth, Height: base.Height}
	if canvas.Hovered() == hitMonitorDivider || state.monitorResizing {
		canvas.Fill(gfx.Rect{X: dividerX - 1, Y: base.Y, Width: 2, Height: base.Height}, state.pal.line)
	}
	canvas.HitWithOptions(hitMonitorDivider, divider, state.messages.Monitor.Title, gfx.HitOptions{Role: gfx.RoleSeparator, Cursor: gfx.PointerCursorResizeHorizontal})
}

// declareIndependentMonitor declares the monitor panel in the workbench page:
// a drawer at narrow widths, otherwise a fixed region beside the panes.
func (state *State) declareIndependentMonitor(c ui.C) {
	if state.activeSection != sectionTerminal || !state.monitorShown(state.deck.ActiveWorkspace()) {
		return
	}
	size := state.viewport
	base, drawer := state.monitorBaseBounds(size)
	state.monitorRect = base
	if drawer {
		context, _ := state.deck.Context(state.deck.ActiveWorkspace())
		side := ui.DrawerRight
		if context.MonitorLeft {
			side = ui.DrawerLeft
		}
		open := true
		ui.Drawer(c, &open, "", ui.DrawerSpec{Side: side, Size: base.Width, InitialFocus: hitMonitorClose}, func() {
			ui.Column(c).Fill().Draw(func(d *ui.DrawContext) {
				// The drawer's content is the panel: its own header, body and
				// footer, not the chrome and padding the drawer adds beside it.
				state.monitorRect = d.Rect
				state.paintMonitorSurface(d.Canvas, d.Rect)
			}).Children(func() {
				state.declareMonitorContent(c, base.Width, base.Height)
			})
		}, nil)
		if !open {
			c.After(func() { state.closeMonitor() })
		}
		return
	}
	ui.Column(c).Absolute().Left(base.X).Top(base.Y).Size(base.Width, base.Height).
		Draw(func(d *ui.DrawContext) {
			state.monitorRect = d.Rect
			state.paintMonitorSurface(d.Canvas, d.Rect)
		}).
		Children(func() { state.declareMonitorContent(c, base.Width, base.Height) })
}

// monitorLabel paints a one-line label with a width-aware ellipsis.
func monitorLabel(c *gfx.Canvas, text string, b gfx.Rect, size float32, color gfx.Color, weight float32) {
	if b.Width <= 0 || b.Height <= 0 {
		return
	}
	c.Label(elide(c, text, size, weight, b.Width), b, gfx.TextStyle{Size: size, Color: color, Weight: weight})
}

// paintMonitorSurface paints the monitor's background, header band and footer
// band under its page controls.
func (state *State) paintMonitorSurface(canvas *gfx.Canvas, b gfx.Rect) {
	if b.Width <= 0 || b.Height <= 0 {
		return
	}
	colors := state.monitorColors()
	canvas.Fill(b, colors.background)
	canvas.Fill(gfx.Rect{X: b.X, Y: b.Y, Width: b.Width, Height: infoBarHeight}, state.pal.workspace)
	canvas.Fill(gfx.Rect{X: b.X, Y: b.Y + infoBarHeight - 1, Width: b.Width, Height: 1}, state.pal.line)
	if monitorShowsBody(state.contextSession()) {
		// The footer band is the sections' freshness line. A panel with nothing
		// to report has no footer, as the hand-painted one returned before it.
		canvas.Fill(gfx.Rect{X: b.X, Y: b.Y + b.Height - 24, Width: b.Width, Height: 24}, colors.background)
	}
}

// monitorShowsBody reports whether the panel shows its scrolling sections
// rather than its loading, empty, paused or unsupported state.
func monitorShowsBody(session *Session) bool {
	return session != nil && session.stats != nil &&
		session.stats.state < remoteStatsUnsupported && session.stats.panel.count > 0
}

// declareMonitorContent declares the panel's header, navigation, host
// information, scroll body and footer.
func (state *State) declareMonitorContent(c ui.C, width, height float32) {
	session := state.contextSession()
	m := state.messages.Monitor
	state.monitorUI = monitorPageUI{}
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(infoBarHeight).PadSides(0, 10, 0, 12).Items(ui.Center).Children(func() {
			ui.Icon(c, lucideIcons[iconActivity]).Size(16, 16).Color(state.pal.muted).Shrink(0)
			ui.Text(c, m.Title).FontSize(14).Weight(gfx.WeightSemibold).Color(state.pal.text).Shrink(0).MarginLeft(7)
			if session == nil || session.stats == nil {
				ui.Spacer(c)
			} else {
				p := &session.stats.panel
				live := session.stats.state == remoteStatsReady
				interval := state.declareMonitorInterval(c, p, live)
				interval.Width(82).Height(26).Shrink(0).MarginLeft(12)
				state.declareMonitorMenu(c, interval)
				pause, hint := uint8(iconPause), m.Pause
				if p.paused {
					pause, hint = iconPlay, m.Resume
				}
				state.declareIconButton(c, hitMonitorPause, pause, hint, false, iconButtonQuiet).Size(24, 24).MarginLeft(4)
				ui.Spacer(c)
			}
			context, _ := state.deck.Context(state.deck.ActiveWorkspace())
			moveIcon, moveHint := uint8(iconChevronRight), m.MoveRight
			if !context.MonitorLeft {
				moveIcon, moveHint = iconChevronLeft, m.MoveLeft
			}
			state.declareIconButton(c, hitMonitorSide, moveIcon, moveHint, false, iconButtonQuiet).Size(24, 24)
			state.declareIconButton(c, hitMonitorClose, iconClose, state.messages.LabelCloseTab, false, iconButtonQuiet).
				Size(24, 24).MarginLeft(6)
		})
		if session == nil || session.stats == nil {
			ui.Column(c).Grow(1).Items(ui.Center).Justify(ui.Center).Children(func() {
				ui.Text(c, m.NotConnected).FontSize(13).Color(state.pal.muted)
			})
			return
		}
		p := &session.stats.panel
		system := monitorSystem(session)
		p.loginsUnsupported = system == sshconn.SystemWindows
		if p.language != state.language.String() {
			state.formatMonitor(session.stats)
		}
		ui.Row(c).Height(monitorControlsHeight).PadSides(6, 12, 0, 12).Children(func() {
			state.declareMonitorNavigation(c, p, width-24)
		})
		ui.Slot(c).Height(monitorHostInfoHeight).MarginSides(0, 12, 0, 12).Draw(func(d *ui.DrawContext) {
			state.paintMonitorHostInfo(d.Canvas, gfx.Rect{X: d.Rect.X, Y: d.Rect.Y, Width: d.Rect.Width, Height: monitorHostInfoHeight}, session)
		})
		state.declareMonitorBody(c, session, p, width, height)
		if monitorShowsBody(session) {
			state.declareMonitorFooter(c, session, p)
		}
	})
}

// declareMonitorNavigation declares the overview/disks/network strip.
func (state *State) declareMonitorNavigation(c ui.C, p *monitorView, width float32) {
	labels := [3]string{state.messages.Monitor.Overview, state.messages.Monitor.Disks, state.messages.Monitor.Network}
	ui.Tabs(c, &p.page, []int{0, 1, 2}, func(index int) string { return labels[index] }).Size(max(width, 120), 32)
}

// declareMonitorInterval declares the refresh interval as a page button with
// the animated clock in it; its menu hangs from the same box.
func (state *State) declareMonitorInterval(c ui.C, p *monitorView, live bool) ui.Box {
	m := state.messages.Monitor
	hint := m.Interval
	// The interval was a ghost button: the recipe owns its hover, press and
	// focus ring, exactly as components.BeginButton drew it.
	button := ui.ButtonBase(c, hint).Height(26).PadX(4).Gap(4).Radius(radius.control).Ghost().
		HitWith(hitMonitorInterval, hint, gfx.HitOptions{Role: gfx.RoleButton, Expandable: true, Expanded: state.monitorMenu, Value: p.intervalText}).
		Shrink(0)
	button.Children(func() {
		ui.Slot(c).Size(16, 16).Draw(func(d *ui.DrawContext) {
			state.paintMonitorRefreshClock(d.Canvas, p, d.Rect, live)
		})
		ui.Text(c, p.intervalText).FontSize(12).Weight(gfx.WeightMedium).Color(state.Theme().Foreground).Ellipsis().Shrink(1)
		ui.Icon(c, lucideIcons[iconChevronDown]).Size(11, 11).Color(state.Theme().MutedForeground).Shrink(0)
	})
	return button
}

// paintMonitorRefreshClock draws the animated hands of the interval control and
// schedules the next sweep while collection is live.
func (state *State) paintMonitorRefreshClock(c *gfx.Canvas, p *monitorView, b gfx.Rect, live bool) {
	if b.Width <= 0 || b.Height <= 0 {
		return
	}
	b = gfx.Rect{X: b.X + (b.Width-16)/2, Y: b.Y + (b.Height-16)/2, Width: 16, Height: 16}
	state.paintMonitorRefreshHands(c, p, b, live)
}

func (state *State) paintMonitorRefreshHands(c *gfx.Canvas, p *monitorView, b gfx.Rect, live bool) {
	frame := monitorRefreshFrame(c, p, live)
	theme := state.Theme()
	c.DrawIcon(remoteRefreshClock, b, theme.MutedForeground)
	c.DrawIcon(remoteRefreshHands[frame], b, theme.MutedForeground)
}

// monitorRefreshFrame is the hand position of the refresh clock, and the frame
// request that moves it on: a live sweep repaints one step at a time, an idle or
// reduced-motion one does not.
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

// declareMonitorMenu declares the interval popup from the interval control.
func (state *State) declareMonitorMenu(c ui.C, anchor ui.Box) {
	if !state.monitorMenu || !anchor.Valid() {
		return
	}
	session := state.contextSession()
	if session == nil || session.stats == nil {
		return
	}
	p := &session.stats.panel
	ids := [...]gfx.ID{hitMonitorEvery1, hitMonitorEvery2, hitMonitorEvery5}
	open := true
	ui.Menu(c, anchor, &open, func(menu *ui.MenuItems) {
		for index, seconds := range [...]int{1, 2, 5} {
			item := menu.Item(state.messages.Monitor.IntervalLabels[index]).Chosen(p.interval == seconds)
			if item.Clicked() {
				id := ids[index]
				c.After(func() { state.click(id, true) })
			}
		}
		if session.hasServer() && session.host.System != sshconn.SystemWindows {
			label := state.messages.SudoCommandsEnable
			if session.sudoCommands {
				label = state.messages.SudoCommandsDisable
			}
			item := menu.Item(label)
			if item.Clicked() {
				c.After(func() { state.click(hitMonitorSudo, true) })
			}
		}
	})
	if !open {
		state.monitorMenu = false
	}
}

// declareMonitorBody declares the scrollable sections, or the loading, empty
// and unsupported states in their place.
func (state *State) declareMonitorBody(c ui.C, session *Session, p *monitorView, width, height float32) {
	m := state.messages.Monitor
	system := monitorSystem(session)
	if session.stats.state >= remoteStatsUnsupported || p.count == 0 {
		message := state.remoteStatsText(session.stats)
		unsupported := !monitorSupported(system)
		if unsupported {
			message = m.UnsupportedPlatform
		}
		if p.paused {
			message = m.Paused
		}
		loading := session.stats.state == remoteStatsMeasuring && !p.paused && !unsupported
		retry := session.stats.state >= remoteStatsUnsupported && !unsupported
		ui.Column(c).Grow(1).Basis(0).Items(ui.Center).Justify(ui.Center).Children(func() {
			if loading {
				ui.Spinner(c).Size(16, 16)
			}
			line := ui.Paragraph(c, message, 4).FontSize(13).LineHeight(18).Width(max(width-32, 0)).
				TextCenter().Color(state.monitorColors().muted)
			if loading {
				line.MarginTop(16)
			}
			if retry {
				ui.Button(c, m.Retry).Hit(hitMonitorRetry, m.Retry).Secondary().Size(min(width-32, 200), 32).MarginTop(12)
			}
		})
		return
	}
	p.contentHeight = monitorContentHeight(p)
	scroll := ui.Scroll(c).Grow(1).Basis(0).MinHeight(0).Scrollbars().ScrollbarSize(catalogBarGutter).
		HitWith(hitMonitorScroll, m.Title, gfx.HitOptions{Role: gfx.RoleGroup})
	if area := scroll.ScrollState(); area != nil {
		if p.scroll != p.scrollSynced {
			area.ScrollTo(p.scroll)
			p.scrollSynced = p.scroll
			c.Canvas().RequestFrame()
		}
	}
	state.monitorUI.scroll = scroll
	scroll.Children(func() {
		ui.Slot(c).FillWidth().Height(p.contentHeight).Draw(func(d *ui.DrawContext) {
			viewport := d.Rect
			if state.monitorUI.scroll.Valid() {
				if area := state.monitorUI.scroll.ScrollState(); area != nil {
					viewport = gfx.Rect{X: d.Rect.X, Y: d.Rect.Y + area.Offset, Width: d.Rect.Width, Height: area.Viewport}
				}
			}
			state.paintMonitorSections(d.Canvas, session.stats, d.Rect, viewport)
		})
	})
	scroll.DrawOver(func(d *ui.DrawContext) {
		if area := scroll.ScrollState(); area != nil {
			p.scroll = area.Offset
			p.scrollSynced = area.Offset
		}
	})
}

// declareMonitorFooter declares the freshness line under the body.
func (state *State) declareMonitorFooter(c ui.C, session *Session, p *monitorView) {
	m, colors := state.messages.Monitor, state.monitorColors()
	footer := m.Updated
	fc := colors.muted
	if p.paused {
		footer = m.Paused
		fc = state.pal.warning
	}
	if (p.detailsFailed || p.sample.CPUUnavailable || p.sample.NetworkUnavailable) && !p.paused {
		footer = m.DetailsUnavailable
	}
	ui.Row(c).Height(24).PadSides(0, 12, 0, 12).Gap(6).Items(ui.Center).Children(func() {
		ui.Icon(c, icons.Clock3).Size(12, 12).Color(fc).Shrink(0)
		ui.Text(c, footer).FontSize(10).Color(fc).Ellipsis()
	})
}
