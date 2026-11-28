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
	// contentTop is the y the scrolled content starts at with no offset: the
	// split slots of the process tab each need it to cull against the same
	// viewport.
	contentTop float32
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
			ui.Icon(c, lucideIcons[iconGauge]).Size(16, 16).Color(state.pal.muted).Shrink(0)
			ui.Text(c, m.Title).FontSize(14).Weight(gfx.WeightSemibold).Color(state.pal.text).Shrink(0).MarginLeft(7)
			if session == nil || session.stats == nil {
				ui.Spacer(c)
			} else {
				p := &session.stats.panel
				live := session.stats.state == remoteStatsReady
				interval := state.declareMonitorInterval(c, p, live)
				interval.Height(26).Shrink(0).MarginLeft(12)
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
		state.declareMonitorBody(c, session, p, width, height)
		state.declareMonitorProcessMenu(c, p)
		state.declareMonitorProcessRowMenu(c, p)
		state.declareMonitorProcessDetails(c)
		if monitorShowsBody(session) {
			state.declareMonitorFooter(c, session, p)
		}
	})
}

// declareMonitorProcessSearch declares the search box the process tab carries.
// The rows it narrows are the reading already in hand: typing filters them and
// clearing the query brings them all back without asking the host again.
func (state *State) declareMonitorProcessSearch(c ui.C, p *monitorView) {
	m := state.messages.Monitor
	ui.Row(c).Height(monitorProcessSearchRow).PadSides(0, 10, 0, 10).Items(ui.Center).Children(func() {
		ui.TextField(c, &p.procSearch, m.ProcessSearch, m.ProcessSearchPlaceholder, ui.WithID(hitMonitorProcSearch)).
			Height(28).FillWidth()
	})
}

// declareMonitorNavigation declares the overview/processes/disks/network strip.
// Its labels come from monitorPageOptions, the same list the keyboard moves
// through, so the strip and the keys cannot disagree about the order.
func (state *State) declareMonitorNavigation(c ui.C, p *monitorView, width float32) {
	options := state.monitorPageOptions()
	ui.Tabs(c, &p.page, []int{0, 1, 2, 3}, func(index int) string { return options[index].Label }).Size(max(width, 120), 32)
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
	ids := [...]gfx.ID{hitMonitorEvery1, hitMonitorEvery2, hitMonitorEvery5, hitMonitorEvery10}
	open := true
	ui.Menu(c, anchor, &open, func(menu *ui.MenuItems) {
		for index, seconds := range monitorIntervals {
			item := menu.Item(state.messages.Monitor.IntervalLabels[index]).Chosen(p.interval == seconds)
			if item.Clicked() {
				id := ids[index]
				c.After(func() { state.click(id, true) })
			}
		}
	})
	if !open {
		state.monitorMenu = false
	}
}

// declareMonitorProcessMenu declares the process list's sort popup, hanging it
// off the control that list painted last: choosing a sort re-orders the rows the
// panel already holds, so nothing is asked of the host.
func (state *State) declareMonitorProcessMenu(c ui.C, p *monitorView) {
	if !p.sortMenu || p.sortAnchor.Width <= 0 {
		return
	}
	m := state.messages.Monitor
	ids := [...]gfx.ID{hitMonitorSortCPU, hitMonitorSortMemory, hitMonitorSortPID, hitMonitorSortName}
	labels := [...]string{m.SortCPU, m.SortMemory, m.SortPID, m.SortName}
	open := true
	ui.MenuAt(c, &open, p.sortAnchor, func(menu *ui.MenuItems) {
		for index, label := range labels {
			item := menu.Item(label).Chosen(p.procSort == index)
			if item.Clicked() {
				id := ids[index]
				c.After(func() { state.click(id, true) })
			}
		}
	})
	if !open {
		p.sortMenu = false
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
	// The rows the list draws are the reading narrowed by the search box; it is
	// rebuilt here, once per frame, so a reading or a query that changed since
	// the last paint can never leave the card at the wrong height.
	// The box owns its text; the rows narrow from it before the page is measured,
	// so the card is never a frame too tall or too short for what it shows.
	p.procQuery = p.procSearch.Text()
	p.refilterProcesses()
	p.contentHeight = monitorContentHeight(p, state.monitorCurrentSSHUser(session))
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
		if p.page == 1 {
			state.declareMonitorProcessPage(c, session, p)
			return
		}
		ui.Slot(c).FillWidth().Height(p.contentHeight).Draw(func(d *ui.DrawContext) {
			state.paintMonitorSections(d.Canvas, session, session.stats, d.Rect, state.monitorDrawViewport(d, d.Rect.Y))
		})
	})
	scroll.DrawOver(func(d *ui.DrawContext) {
		if area := scroll.ScrollState(); area != nil {
			p.scroll = area.Offset
			p.scrollSynced = area.Offset
		}
	})
}

// monitorDrawViewport is the part of the scrolled content the window shows, in
// the coordinates the sections are painted in: contentTop is the y the content
// starts at when the offset is zero, which only the first slot of a split page
// knows and the page records as it paints.
func (state *State) monitorDrawViewport(d *ui.DrawContext, contentTop float32) gfx.Rect {
	viewport := gfx.Rect{X: d.Rect.X, Y: contentTop, Width: d.Rect.Width, Height: d.Rect.Height}
	if state.monitorUI.scroll.Valid() {
		if area := state.monitorUI.scroll.ScrollState(); area != nil {
			viewport.Y = contentTop + area.Offset
			viewport.Height = area.Viewport
		}
	}
	return viewport
}

// declareMonitorProcessPage declares the process tab in the pieces it scrolls
// as: the meters, the process card, and the line that points at the tools
// window. The card is a page control — its header, its search box and its rows
// are declared together — because a text field cannot live in a painted slot.
func (state *State) declareMonitorProcessPage(c ui.C, session *Session, p *monitorView) {
	ui.Slot(c).FillWidth().Height(monitorProcessTopHeight(p)).Draw(func(d *ui.DrawContext) {
		// The first slot of the page is where the content begins: the split
		// slots below are painted at their own y, so the shared viewport has to
		// be taken from here, not from each slot's own rectangle.
		state.monitorUI.contentTop = d.Rect.Y
		state.paintMonitorProcessTop(d.Canvas, p, d.Rect, state.monitorDrawViewport(d, d.Rect.Y))
	})
	state.declareMonitorProcessCard(c, p)
	ui.Slot(c).FillWidth().Height(monitorProcessHintHeight()).Draw(func(d *ui.DrawContext) {
		state.paintMonitorProcessHint(d.Canvas, d.Rect, state.monitorDrawViewport(d, state.monitorUI.contentTop))
	})
}

// declareMonitorProcessCard declares the process card as one control: its
// surface and border, the header that opens and closes it, and — while open —
// the search box and the painted rows.
func (state *State) declareMonitorProcessCard(c ui.C, p *monitorView) {
	colors := state.monitorColors()
	// The painted cards around this one sit monitorInset in from the content
	// edge; a declared card has to ask for the same margin, or it is the widest
	// thing on the page.
	card := ui.Column(c).FillWidth().Height(monitorProcessHeight(p)).
		MarginSides(0, monitorInset, 0, monitorInset).
		Background(colors.surface).Radius(cornerPx(10)).Border(1, colors.border)
	card.Children(func() {
		ui.Slot(c).FillWidth().Height(32).Draw(func(d *ui.DrawContext) {
			state.paintMonitorProcessHeader(d.Canvas, p, d.Rect)
		})
		if !p.procsOpen {
			return
		}
		state.declareMonitorProcessSearch(c, p)
		ui.Slot(c).FillWidth().Height(monitorProcessRowsHeight(p)).Draw(func(d *ui.DrawContext) {
			state.paintMonitorProcessRows(d.Canvas, p, d.Rect, state.monitorDrawViewport(d, state.monitorUI.contentTop))
		})
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
	// What an action did is newer than any reading: it leads the line until its
	// notice expires, then the freshness line has the footer back.
	glyph := icons.Clock3
	if p.actionStatus != "" && time.Since(p.actionAt) < monitorActionNotice {
		footer = p.actionStatus
		fc = colors.muted
		if p.actionFailed {
			fc, glyph = state.pal.danger, icons.CircleAlert
		}
	}
	ui.Row(c).Height(24).PadSides(0, 12, 0, 12).Gap(6).Items(ui.Center).Children(func() {
		ui.Icon(c, glyph).Size(12, 12).Color(fc).Shrink(0)
		ui.Text(c, footer).FontSize(10).Color(fc).Ellipsis()
	})
}
