package workbench

import (
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/monitoring"
	"motty/internal/sshconn"
	"motty/internal/workbench/icons"
)

// dashboardSectionUI is what the dashboard's declaration leaves for its paint:
// the scroll area and the hand-painted content rect, and the viewport the
// visibility culling reads.
type dashboardSectionUI struct {
	draw   func(*ui.DrawContext)
	scroll ui.Box
}

// dashboardRowControls is where a host card's page controls sit inside its row.
type dashboardRowControls struct {
	tabs, terminal, sudo, retry gfx.Rect
	display                     gfx.Rect
	retryShown                  bool
}

// declareDashboardWorkspace declares the dashboard in the workbench page: its
// header controls, its hand-painted scrolling body, its footer and its first-run
// state. The host cards paint themselves into the scroll slot; their view tabs
// and actions are page controls, so the page owns focus, hits and the keyboard.
func (state *State) declareDashboardWorkspace(c ui.C) {
	if state.activeSection != sectionDashboard {
		return
	}
	p := state.dashboardPanel()
	layout := &state.sectionBodies.dashboard
	*layout = dashboardSectionUI{}
	inner := max(state.catalogSectionBounds().Width-36, 0)
	if p == nil {
		// The icon is painted by hand into the slot the empty state declared, as
		// the other catalog sections do, so a first run shows it too.
		if empty := state.declareDashboardEmpty(c); empty.Valid() {
			empty.DrawOver(func(d *ui.DrawContext) { state.paintEmptyPage(d.Canvas, iconGauge) })
		}
		return
	}
	state.fillDashboardRefreshClock(p, int(inner))
	layout.draw = func(d *ui.DrawContext) { state.paintDashboardBody(d.Canvas, d.Rect) }
	body := ui.Column(c).Fill().PadSides(14, 18, 0, 18)
	body.Children(func() {
		state.declareDashboardHeader(c, p)
		ui.Slot(c).Height(10)
		ui.Slot(c).Height(1).FillWidth().Draw(func(d *ui.DrawContext) {
			d.Canvas.Fill(d.Rect, state.pal.line)
		})
		ui.Slot(c).Height(7)
		state.declareDashboardBody(c, p, inner)
		ui.Slot(c).Height(5)
		state.declareDashboardFooter(c, p, inner)
		ui.Slot(c).Height(2)
	})
	body.Clip()
	body.DrawOver(func(d *ui.DrawContext) { state.syncDashboardScroll() })
}

// fillDashboardRefreshClock records what the sampling clock's hands sweep
// against, as the hand-painted header did: the interval, whether collection is
// paused and whether any host is live.
func (state *State) fillDashboardRefreshClock(p *monitoring.Panel, width int) {
	d, m := &state.dashboard, state.messages.Dashboard
	running := d.running[p.ID]
	d.refresh.interval = p.Interval
	d.refresh.intervalText = fmt.Sprintf(m.Every, p.Interval)
	d.refresh.paused = !running
	d.refresh.count = 0
	for _, id := range p.Hosts {
		r := state.dashboardRow(p, id, width)
		if r.snapshot.Status == monitoring.Live {
			d.refresh.count = 1
			d.refresh.version = uint32(r.snapshot.Updated.UnixMilli())
			break
		}
	}
}

// Dashboard header actions keep one height and one gap, and each is at least as
// wide as its label needs. A single fixed width cannot hold every language:
// Chinese "开始监控" fits 112 and English "Start monitoring" needs more.
const (
	dashboardActionHeight = float32(36)
	dashboardActionGap    = float32(12)
	dashboardActionLeast  = float32(112)
	dashboardActionIcon   = float32(36)
	// The selector's least width, its gap, and the three icon buttons beside
	// it: the room the actions must leave before they are worth their labels.
	dashboardHeaderLeast = float32(120 + 8 + 112 + 12)
)

// dashboardActionWidths is the width of each header action. A label wider than
// the designed width widens its button instead of being clipped.
func dashboardActionWidths(canvas *gfx.Canvas, theme gfx.Theme, newLabel string, newIcon gfx.Icon, toggleLabel string, toggleIcon gfx.Icon, iconOnly bool) (newWidth, toggleWidth float32) {
	if iconOnly {
		return dashboardActionIcon, dashboardActionIcon
	}
	measure := components.BeginButton(canvas, theme, gfx.ID{}, gfx.Rect{})
	return max(dashboardActionLeast, measure.WithIcon(newIcon).SizeToFit(newLabel).BoundsWidth()),
		max(dashboardActionLeast, measure.WithIcon(toggleIcon).SizeToFit(toggleLabel).BoundsWidth())
}

// declareDashboardHeader declares the panel selector, the manage actions and
// the new/start controls in one row, then the view mode, search and the panel's
// sampling settings in the next.
func (state *State) declareDashboardHeader(c ui.C, p *monitoring.Panel) {
	d, m := &state.dashboard, state.messages.Dashboard
	inner := max(state.catalogSectionBounds().Width-36, 0)
	running := d.running[p.ID]
	toggleLabel, toggleIcon := m.Start, iconPlay
	if running {
		toggleLabel, toggleIcon = m.Stop, iconPause
	}
	actionTheme := state.Theme()
	actionTheme.Metrics.ControlHeight = dashboardActionHeight
	newW, toggleW := dashboardActionWidths(c.Canvas(), actionTheme, m.New, lucideIcons[iconPlus], toggleLabel, lucideIcons[toggleIcon], false)
	// A long label must not push the actions over the panel selector and its
	// three icon buttons; two icon buttons fit wherever the labels do not.
	iconOnly := inner < 720
	if !iconOnly && inner-newW-toggleW-dashboardActionGap < dashboardHeaderLeast {
		iconOnly = true
	}
	if iconOnly {
		newW, toggleW = dashboardActionWidths(c.Canvas(), actionTheme, m.New, lucideIcons[iconPlus], toggleLabel, lucideIcons[toggleIcon], true)
	}
	newBoxX := inner - newW - toggleW - dashboardActionGap
	panelWidth := min(float32(240), max(float32(120), newBoxX-132))
	ui.Row(c).Height(dashboardActionHeight).Items(ui.Start).Children(func() {
		choices := d.panelOrder[:0]
		for index := range d.panels {
			choices = append(choices, index)
		}
		d.panelOrder = choices
		choice := 0
		for index := range d.panels {
			if d.panels[index].ID == p.ID {
				choice = index
			}
		}
		d.panelChoice = choice
		ui.Select(c, &d.panelChoice, d.panelOrder, func(index int) string {
			if index < 0 || index >= len(d.panels) {
				return ""
			}
			return d.panels[index].Name
		}).Label(m.Title).Hint(p.Name).Width(panelWidth).Height(36)
		if d.panelChoice != choice && d.panelChoice >= 0 && d.panelChoice < len(d.panels) {
			d.active = d.panels[d.panelChoice].ID
			d.search = text.Editor{}
			d.scroll = 0
			state.save()
		}
		state.declareIconButton(c, hitDashboardEdit, iconPencil, m.Edit, false, iconButtonQuiet).
			Size(32, 32).MarginLeft(8).MarginTop(2)
		state.declareIconButton(c, hitDashboardSort, iconArrowUpDown, m.Sort, len(p.Hosts) < 2, iconButtonQuiet).
			Size(32, 32).MarginLeft(4).MarginTop(2)
		state.declareIconButton(c, hitDashboardDelete, iconTrash, m.Delete, false, iconButtonQuiet).
			Size(32, 32).MarginLeft(4).MarginTop(2)
		// The collection state sits left of the actions on a wide panel; a narrow
		// one keeps it in the footer.
		if inner >= 1000 {
			_, stateText := state.dashboardLiveState(p, running, int(inner))
			ui.Slot(c).Absolute().Left(newBoxX-174).Top(0).Size(160, 36).Draw(func(draw *ui.DrawContext) {
				monitorRight(draw.Canvas, stateText, draw.Rect, 12, state.dashboardLiveColor(running), gfx.WeightMedium)
			})
		}
		ui.Spacer(c)
		newButton := ui.Button(c, m.New).Hit(hitDashboardNew, m.New).Icon(lucideIcons[iconPlus]).Shrink(0)
		toggle := ui.Button(c, toggleLabel).Hit(hitDashboardToggle, toggleLabel).Icon(lucideIcons[toggleIcon]).Shrink(0)
		if iconOnly {
			newButton.IconOnly()
			toggle.IconOnly()
		}
		newButton.Size(newW, dashboardActionHeight)
		toggle.Size(toggleW, dashboardActionHeight).MarginLeft(dashboardActionGap)
		if running {
			toggle.Outline()
		}
	})
	state.declareDashboardModeAndSearch(c, p, inner)
}

// declareDashboardModeAndSearch declares the view mode, the host search and the
// panel's sampling settings, wrapping the settings onto their own row when the
// panel is too narrow to hold them beside the search.
func (state *State) declareDashboardModeAndSearch(c ui.C, p *monitoring.Panel, inner float32) {
	d, m := &state.dashboard, state.messages.Dashboard
	declareMode := func() {
		mode := 0
		if !p.Simple {
			mode = 1
		}
		d.modeChoice = mode
		ui.Tabs(c, &d.modeChoice, []int{0, 1}, func(index int) string {
			if index == 0 {
				return m.Simple
			}
			return m.Detailed
		}).Size(136, 34)
		if d.modeChoice != mode {
			p.Simple = d.modeChoice == 0
			d.scroll = 0
			state.save()
		}
	}
	declareSearch := func(width float32) ui.Box {
		if !d.searchBound {
			d.searchField.Bind(&d.search)
			d.searchBound = true
		}
		return ui.TextField(c, &d.searchField, m.Search, m.Search, ui.WithID(hitDashboardSearch)).
			Icon(lucideIcons[iconSearch]).Height(34).Width(width)
	}
	if inner < 860 {
		ui.Row(c).Height(42).MarginTop(22).Items(ui.Start).Children(func() {
			declareMode()
			declareSearch(max(inner-152, 0)).MarginLeft(16)
		})
		ui.Row(c).Height(44).Items(ui.Start).Justify(ui.End).Children(func() {
			state.declareDashboardIntervalSelect(c, p)
			state.declareDashboardRetentionSelect(c, p).MarginLeft(10)
		})
		return
	}
	searchW := max(100, inner-136-16-316)
	ui.Row(c).Height(42).MarginTop(22).Items(ui.Start).Children(func() {
		declareMode()
		declareSearch(searchW).MarginLeft(16)
		ui.Spacer(c)
		state.declareDashboardIntervalSelect(c, p)
		state.declareDashboardRetentionSelect(c, p).MarginLeft(10)
	})
}

// declareDashboardIntervalSelect declares the sampling cadence as a page
// Select, with the refresh globe as the icon of every choice.
func (state *State) declareDashboardIntervalSelect(c ui.C, p *monitoring.Panel) ui.Box {
	d, m := &state.dashboard, state.messages.Dashboard
	choice := p.Interval
	d.intervalChoice = choice
	selector := ui.Select(c, &d.intervalChoice, dashboardIntervals[:], func(seconds int) string {
		return fmt.Sprintf(m.Every, seconds)
	}).Icons(dashboardIntervalIcons).Label(m.Sampling).Height(34).Width(152)
	// The hands of the sampling clock move over the icon the trigger paints, as
	// they did over the hand-painted trigger: a live sweep asks for the next
	// frame, an idle or reduced-motion one does not.
	selector.Draw(func(part *ui.DrawContext) {
		frame := monitorRefreshFrame(part.Canvas, &d.refresh, d.refresh.count > 0)
		part.Canvas.DrawIcon(remoteRefreshHands[frame], gfx.Rect{
			X: part.Rect.X + 11, Y: part.Rect.Y + (part.Rect.Height-16)/2, Width: 16, Height: 16,
		}, part.Theme.Foreground)
	})
	if d.intervalChoice != choice {
		p.Interval = d.intervalChoice
		d.refresh.refreshReset = true
		state.reconcileDashboard()
		state.save()
	}
	return selector
}

// declareDashboardRetentionSelect declares how long samples are kept.
func (state *State) declareDashboardRetentionSelect(c ui.C, p *monitoring.Panel) ui.Box {
	d, m := &state.dashboard, state.messages.Dashboard
	choice := p.Retention
	d.retentionChoice = choice
	selector := ui.Select(c, &d.retentionChoice, dashboardRetentions[:], func(minutes int) string {
		return state.dashboardRetention(minutes)
	}).Label(m.Retention).Hint(m.Retention + ": " + state.dashboardRetention(p.Retention)).Height(34).Width(142)
	if d.retentionChoice != choice {
		p.Retention = d.retentionChoice
		state.reconcileDashboard()
		state.save()
	}
	return selector
}

// declareDashboardBody declares the scrolling host list: the page Scroll owns
// the offset and scrollbars, the slot paints the cards, and page controls sit
// over the visible cards' own actions.
func (state *State) declareDashboardBody(c ui.C, p *monitoring.Panel, inner float32) {
	d, m := &state.dashboard, state.messages.Dashboard
	width := int(max(inner, 0))
	rowW := max(inner, 0)
	if p.Simple {
		rowW = max(rowW, 960)
	}
	query := strings.ToLower(strings.TrimSpace(d.search.String()))
	ids := d.visibleIDs[:0]
	d.content = 0
	if p.Simple {
		// The simple view's column headings are part of the scrolled content, not
		// a fixed band: they move with the rows they head.
		d.content = 38
	}
	for _, id := range p.Hosts {
		r := state.dashboardRow(p, id, width)
		h, exists := state.dashboardHost(id)
		if !exists {
			r.snapshot.Status = monitoring.Missing
		}
		if query == "" || strings.Contains(strings.ToLower(h.Label+" "+h.Address+" "+h.Tags+" "+h.Group+" "+id), query) {
			ids = append(ids, id)
			height := dashboardHostHeight(r, rowW-12)
			if p.Simple {
				height = dashboardSimpleHeight(r)
			}
			d.content += height
		}
	}
	d.visibleIDs = ids
	if len(ids) == 0 {
		// The no-match line is painted inside the scroll content, under the
		// simple view's headings. Give it the room a zero-row list does not.
		d.content += 70
	}
	scroll := ui.Scroll(c).Grow(1).Basis(0).MinHeight(0).Sideways().Scrollbars().ScrollbarSize(catalogBarGutter).
		HitWith(hitDashboardScroll, m.Hosts, gfx.HitOptions{Role: gfx.RoleGroup})
	if area := scroll.ScrollState(); area != nil {
		if d.scroll != d.scrollSynced {
			area.ScrollTo(d.scroll)
			d.scrollSynced = d.scroll
			c.Canvas().RequestFrame()
		}
	}
	state.sectionBodies.dashboard.scroll = scroll
	scroll.Children(func() {
		ui.Column(c).Width(rowW).Height(d.content).Children(func() {
			ui.Slot(c).FillWidth().Height(d.content).Draw(state.sectionBodies.dashboard.draw)
			state.declareDashboardRowControls(c, p, ids, rowW, inner)
		})
	})
}

// declareDashboardRowControls declares the page controls of every host row the
// viewport can show: its view tabs and its terminal, sudo and retry actions.
func (state *State) declareDashboardRowControls(c ui.C, p *monitoring.Panel, ids []string, rowW, inner float32) {
	d := &state.dashboard
	height := d.body.Height
	if height <= 0 {
		height = max(state.catalogSectionBounds().Height-160, 200)
	}
	y := float32(0)
	if p.Simple {
		y = 38
	}
	for _, id := range ids {
		r := state.dashboardRow(p, id, int(inner))
		h, exists := state.dashboardHost(id)
		rowHeight := dashboardHostHeight(r, rowW-12)
		if p.Simple {
			rowHeight = dashboardSimpleHeight(r)
		}
		if y+rowHeight < d.scroll-200 || y > d.scroll+height+200 {
			y += rowHeight
			continue
		}
		row := gfx.Rect{X: 0, Y: y, Width: rowW - 12, Height: rowHeight}
		controls := state.dashboardRowControlRects(c.Canvas(), p, h, r, row, exists, p.Simple)
		state.declareDashboardRowControlBoxes(c, p, h, r, controls, p.Simple, y, row.Width)
		y += rowHeight
	}
}

// declareDashboardRowControlBoxes puts a row's controls at their content
// offsets, over the painted card.
func (state *State) declareDashboardRowControlBoxes(c ui.C, p *monitoring.Panel, h Host, r *dashboardRow, controls dashboardRowControls, simple bool, y, rowWidth float32) {
	m := state.messages.Dashboard
	labels := [4]string{state.messages.Monitor.Overview, state.messages.Monitor.Disks, state.messages.Monitor.Network, state.messages.Monitor.GPU}
	view := func(index int) string { return labels[index] }
	values := dashboardViewValues(r, simple)
	icon := func(index int) gfx.Icon { return dashboardViewIcons(values[index]) }
	if simple {
		// The view column is 40 wide: the strip shows only its icons, and the
		// labels stay its accessible names and hover hints.
		ui.Tabs(c, &r.simplePage, values, view).TabIcons(icon).TabIconOnly().Vertical().
			Absolute().Left(controls.tabs.X).Top(y+controls.tabs.Y).Size(controls.tabs.Width, controls.tabs.Height)
	} else {
		ui.Tabs(c, &r.page, values, view).TabIcons(icon).
			Absolute().Left(controls.tabs.X).Top(y+controls.tabs.Y).Size(controls.tabs.Width, controls.tabs.Height)
	}
	state.declareDashboardDisplay(c, p, h, r, controls.display, y)
	state.declareDashboardGPUControls(c, p, h, r, gfx.Rect{Width: rowWidth}, y)
	if h.ID != 0 {
		state.declareIconButton(c, hitDashboardTerminalList.At(int(h.ID)), iconTerminal, m.Terminal, false, iconButtonQuiet).
			Absolute().Left(controls.terminal.X).Top(y+controls.terminal.Y).Size(controls.terminal.Width, controls.terminal.Height)
		if source := state.dashboard.sources[h.CatalogID]; source != nil && source.session.phase == sshconn.PhaseConnected && source.session.host.System != sshconn.SystemWindows {
			label := state.messages.DashboardSudoEnable
			if source.session.sudoCommands {
				label = state.messages.DashboardSudoDisable
			}
			state.declareIconButton(c, hitDashboardSudoList.At(int(h.ID)), iconLock, label, false, iconButtonQuiet).
				Absolute().Left(controls.sudo.X).Top(y+controls.sudo.Y).Size(controls.sudo.Width, controls.sudo.Height)
		}
	}
	if controls.retryShown {
		ui.Button(c, m.Retry).Hit(hitDashboardRetryList.At(int(h.ID)), m.Retry).Ghost().Small().
			Absolute().Left(controls.retry.X).Top(y+controls.retry.Y).Size(controls.retry.Width, controls.retry.Height)
	}
}

// dashboardRowControlRects is where a row's controls sit, from the same layout
// its paint uses.
func (state *State) dashboardRowControlRects(canvas *gfx.Canvas, p *monitoring.Panel, h Host, r *dashboardRow, row gfx.Rect, exists, simple bool) dashboardRowControls {
	if simple {
		tabs, _ := dashboardSimpleColumns(row)
		// The strip is inset the same distance at the top and bottom of the
		// heading band, so it sits centered in a row with no table under it.
		controls := dashboardRowControls{
			tabs: gfx.Rect{X: tabs, Y: dashboardSimpleTabInset, Width: 40,
				Height: dashboardSimpleRowLeast - 2*dashboardSimpleTabInset},
			terminal: gfx.Rect{X: row.Width - 38, Y: 64, Width: 32, Height: 32},
			display:  gfx.Rect{X: row.Width - 38, Y: 24, Width: 32, Height: 32},
			sudo:     gfx.Rect{X: row.Width - 74, Y: 24, Width: 32, Height: 32},
		}
		if r.visible[4] {
			controls.tabs.Height = 146 - 2*dashboardSimpleTabInset
		}
		controls.retryShown = dashboardRetryShown(state, p, h, r)
		controls.retry = gfx.Rect{X: 54, Y: 75, Width: max(tabs-64, 40), Height: 24}
		return controls
	}
	// The detailed card's heading rects are the paint layout's, which already
	// carries the row's Y; the controls are placed relative to the row, so ask
	// for them at the row's own origin and add the row's Y once.
	rowForRects := row
	rowForRects.Y = 0
	out := state.dashboardHeadingRects(canvas, h, rowForRects)
	out.retryShown = dashboardRetryShown(state, p, h, r)
	return out
}

// dashboardRetryShown reports whether the card shows the retry action instead
// of its status label.
func dashboardRetryShown(state *State, p *monitoring.Panel, h Host, r *dashboardRow) bool {
	return state.dashboard.running[p.ID] && r.snapshot.Status == monitoring.Failed && h.ID != 0
}

// declareDashboardFooter declares the count under the list and, on a narrow
// panel, the collection state. The wide panel shows the state in the header.
func (state *State) declareDashboardFooter(c ui.C, p *monitoring.Panel, inner float32) {
	d, m := &state.dashboard, state.messages.Dashboard
	running := d.running[p.ID]
	_, stateText := state.dashboardLiveState(p, running, int(inner))
	footer := ui.Row(c).Height(34).FillWidth().Items(ui.Center)
	footer.Draw(func(draw *ui.DrawContext) {
		draw.Canvas.Fill(gfx.Rect{X: draw.Rect.X, Y: draw.Rect.Y, Width: draw.Rect.Width, Height: 1}, state.pal.line)
	})
	footer.Children(func() {
		ui.Text(c, fmt.Sprintf(m.Count, len(p.Hosts))).FontSize(12).Color(state.pal.muted).Grow(1).Ellipsis()
		if inner < 1000 {
			ui.Text(c, stateText).FontSize(12).Color(state.pal.muted).Ellipsis()
		}
	})
}

// dashboardLiveState counts live hosts across the whole panel, before the host
// search filters the cards, and names the collection state as the footer did.
func (state *State) dashboardLiveState(p *monitoring.Panel, running bool, width int) (int, string) {
	m := state.messages.Dashboard
	live := 0
	for _, id := range p.Hosts {
		r := state.dashboardRow(p, id, width)
		if r.snapshot.Status == monitoring.Live && running && p.Metrics(id) != 0 {
			live++
		}
	}
	stateText := m.Waiting
	if running {
		stateText = fmt.Sprintf(m.Collecting, live, len(p.Hosts))
	} else {
		for _, id := range p.Hosts {
			if state.dashboardRow(p, id, width).snapshot.HasSample {
				stateText = m.Stopped
				break
			}
		}
	}
	return live, stateText
}

// paintDashboardBody paints the host cards into the scroll slot: every visible
// row's heading, chart cells, disk grid and simple-view metrics.
func (state *State) paintDashboardBody(canvas *gfx.Canvas, content gfx.Rect) {
	d, m := &state.dashboard, state.messages.Dashboard
	p := state.dashboardPanel()
	if p == nil {
		return
	}
	layout := &state.sectionBodies.dashboard
	if layout.scroll.Valid() {
		scroll := layout.scroll.ScrollState()
		d.body = gfx.Rect{
			X:      content.X + scroll.OffsetX,
			Y:      content.Y + scroll.Offset,
			Width:  scroll.ViewportWidth,
			Height: scroll.Viewport,
		}
		d.content = scroll.Content
		d.scroll = scroll.Offset
		d.scrollSynced = scroll.Offset
	}
	rowW := content.Width
	width := int(max(state.catalogSectionBounds().Width-36, 0))
	y := content.Y
	if p.Simple {
		// The headings scroll with the rows, inside the content.
		state.paintDashboardSimpleHead(canvas, gfx.Rect{X: content.X, Y: content.Y, Width: content.Width - 12, Height: 36})
		y += 38
	}
	for _, id := range d.visibleIDs {
		r := state.dashboardRow(p, id, width)
		host, exists := state.dashboardHost(id)
		if !exists {
			host.Label = m.Missing
			host.CatalogID = id
		}
		h := dashboardHostHeight(r, rowW-12)
		if p.Simple {
			h = dashboardSimpleHeight(r)
		}
		box := gfx.Rect{X: content.X, Y: y, Width: content.Width - 12, Height: h}
		if box.Y+box.Height > d.body.Y && box.Y < d.body.Y+d.body.Height {
			if p.Simple {
				state.paintDashboardSimpleRow(canvas, p, host, r, box)
			} else {
				state.paintDashboardHost(canvas, p, host, r, box)
			}
		}
		y += h
	}
	if len(d.visibleIDs) == 0 {
		monitorLabel(canvas, state.messages.KnownHostsNoMatches, gfx.Rect{X: content.X + 18, Y: y + 30, Width: rowW - 36, Height: 40}, 14, state.pal.muted, gfx.WeightRegular)
	}
}

// syncDashboardScroll copies the ScrollArea's settled offset back into the
// model, so page keys, tests and the collector see the same position.
func (state *State) syncDashboardScroll() {
	d := &state.dashboard
	layout := &state.sectionBodies.dashboard
	if layout.scroll.Valid() {
		scroll := layout.scroll.ScrollState()
		d.scroll = scroll.Offset
		d.scrollSynced = scroll.Offset
	}
}

// declareDashboardAddMenu declares the add-hosts popup of the hosts page.
func (state *State) declareDashboardAddMenu(c ui.C) {
	if state.modal != modalDashboardAdd {
		return
	}
	d, m := &state.dashboard, state.messages.Dashboard
	var rows menuRows
	hint := fmt.Sprintf(m.AddHint, len(d.addHosts))
	rows.addContent(28, func(canvas *gfx.Canvas, bounds gfx.Rect, theme gfx.Theme) {
		monitorLabel(canvas, hint, bounds, 11, state.pal.muted, gfx.WeightRegular)
	})
	rows.add(components.ContextMenuItem{ID: hitDashboardAddNew, Label: m.New, Icon: icons.Plus})
	rows.separate()
	for index, p := range d.panels {
		rows.add(components.ContextMenuItem{ID: hitDashboardAddPanelList.At(index), Label: p.Name, Icon: icons.Gauge})
	}
	open := true
	state.declareMenuAt(c, &open, d.addAnchor, false, &rows, func(id gfx.ID) {
		state.click(id, true)
	})
	if !open {
		c.After(func() {
			// A choice may have opened the editor or moved to the panel; only a
			// real dismiss (Escape, a press beside it) closes the menu's modal.
			if state.modal == modalDashboardAdd {
				state.closeModal()
			}
		})
	}
}

// declareDashboardDialog declares the panel editor: its tabs, fields, host
// picker or order list, and its save/cancel footer.
func (state *State) declareDashboardDialog(c ui.C) {
	if state.modal != modalDashboard {
		return
	}
	e, m := &state.dashboard.editor, state.messages.Dashboard
	title, submit := m.New, m.Create
	if e.id != "" {
		title, submit = m.Edit, m.Save
	}
	initial := hitDashboardName
	if e.page == 1 {
		initial = hitDashboardDialogSave
	}
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Width: 780, Height: min(620, max(420, dialogViewport(c).Height-160)),
		Layer: dialogLayer, Scope: dialogScope, NoClose: true, CloseID: hitDashboardDialogCancel,
		InitialFocusID: initial,
	})
	dialog.Body(func() {
		page := e.page
		ui.Tabs(c, &e.page, []int{0, 1}, func(index int) string {
			if index == 0 {
				return m.Hosts
			}
			return m.OrderTab
		}).Size(260, 36)
		if e.page != page {
			e.scroll = 0
		}
		groups, count, shown := state.dashboardPickerGroups()
		countText := fmt.Sprintf(m.PickerCount, len(e.selected), shown, count)
		if e.page == 1 {
			countText = fmt.Sprintf(m.Selected, len(e.selected))
		}
		ui.Row(c).Height(32).Shrink(0).FillWidth().Items(ui.Center).Children(func() {
			if e.page == 0 {
				state.declareDashboardSelectMatches(c, groups)
			}
			ui.Spacer(c)
			ui.Text(c, countText).FontSize(12).Color(state.pal.muted).Ellipsis()
		})
		if e.page == 0 {
			e.nameField.declare(c, state, &e.name, dialogFieldSpec{ID: hitDashboardName, Label: m.Name, Placeholder: m.NamePlaceholder, Height: 64})
			e.noteField.declare(c, state, &e.note, dialogFieldSpec{ID: hitDashboardNote, Label: m.Note, Placeholder: m.NotePlaceholder, Height: 64})
			state.declareDashboardPickerFilters(c, groups)
			state.declareDashboardPickerList(c, groups)
		} else {
			state.declareDashboardOrderMode(c)
			state.declareDashboardOrderList(c)
		}
		if e.error != "" {
			ui.Text(c, e.error).FontSize(12).Color(state.pal.danger).Height(20).LineHeight(20).Shrink(0).Ellipsis()
		} else {
			ui.Text(c, m.PickerHint).FontSize(12).Color(state.pal.muted).Height(20).LineHeight(20).Shrink(0).Ellipsis()
		}
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitDashboardDialogCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitDashboardDialogSave, Label: submit},
		)
	})
}

// declareDashboardSelectMatches declares the scope-wide select-all checkbox.
func (state *State) declareDashboardSelectMatches(c ui.C, groups []dashboardPickerGroup) {
	e, m := &state.dashboard.editor, state.messages.Dashboard
	total, selected := 0, 0
	for _, group := range groups {
		for _, host := range group.hosts {
			total++
			if slices.Contains(e.selected, host.CatalogID) {
				selected++
			}
		}
	}
	full := total > 0 && selected == total
	flag := state.pageFlag(full)
	box := ui.Checkbox(c, flag, m.SelectMatches).Hit(hitDashboardSelectMatches, m.SelectMatches).
		Mixed(selected > 0 && selected < total)
	if total == 0 {
		box.Disabled(true)
	}
}

// declareDashboardPickerFilters declares the host search, group filter and
// selected-only switch above the picker.
func (state *State) declareDashboardPickerFilters(c ui.C, groups []dashboardPickerGroup) {
	e, m := &state.dashboard.editor, state.messages.Dashboard
	ui.Row(c).Height(36).Shrink(0).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		if !e.queryBound {
			e.queryField.Bind(&e.query)
			e.queryBound = true
		}
		ui.TextField(c, &e.queryField, m.HostSearch, m.HostSearch, ui.WithID(hitDashboardHostQuery)).
			Icon(lucideIcons[iconSearch]).Height(34).MinWidth(80).Grow(1)
		choices := e.groupChoices[:0]
		for index := range e.groups {
			choices = append(choices, index)
		}
		e.groupChoices = choices
		choice := -1
		if e.groupFilter {
			for index, name := range e.groups {
				if name == e.group {
					choice = index
				}
			}
		}
		e.groupChoice = choice
		value := func(index int) string {
			if index < 0 {
				return m.AllGroups
			}
			return state.dashboardGroupLabel(e.groups[index])
		}
		ui.Select(c, &e.groupChoice, append([]int{-1}, e.groupChoices...), value).
			Label(m.GroupFilter).Hint(m.GroupFilter).Height(34).Width(min(170, 120+float32(len(e.groups))*2))
		if e.groupChoice != choice {
			e.groupFilter = e.groupChoice >= 0
			if e.groupChoice >= 0 && e.groupChoice < len(e.groups) {
				e.group = e.groups[e.groupChoice]
			}
			e.scroll = 0
		}
		ui.Switch(c, state.pageFlag(e.selectedOnly), m.SelectedOnly).Hit(hitDashboardSelectedOnly, m.SelectedOnly)
	})
}

// declareDashboardPickerList declares the grouped host cards as page
// checkboxes in a scrolling flow.
func (state *State) declareDashboardPickerList(c ui.C, groups []dashboardPickerGroup) {
	e, m := &state.dashboard.editor, state.messages.Dashboard
	inner := max(min(780, dialogViewport(c).Width-32)-2*c.Theme().Metrics.PadSurface, 200)
	columns := dashboardPickerColumns(inner - 12)
	cardW := (inner - float32(columns-1)*8) / float32(columns)
	scroll := ui.Scroll(c).Grow(1).Basis(0).MinHeight(0).Scrollbars().ScrollbarSize(catalogBarGutter)
	if area := scroll.ScrollState(); area != nil {
		if e.scroll != e.scrollSynced {
			area.ScrollTo(e.scroll)
			e.scrollSynced = e.scroll
			c.Canvas().RequestFrame()
		}
	}
	scroll.Children(func() {
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			for _, group := range groups {
				n := len(group.hosts) + len(group.missing)
				ui.Textf(c, "%s  %d / %d", state.dashboardGroupLabel(group.name), n, group.total).
					FontSize(12).Color(state.pal.muted).Weight(gfx.WeightMedium).Height(24).Shrink(0)
				ui.Flow(c).Gap(8).Children(func() {
					for _, host := range group.hosts {
						state.declareDashboardHostChoice(c, host, hitDashboardHostList.At(int(host.ID)), slices.Contains(e.selected, host.CatalogID), false, cardW)
					}
					for _, at := range group.missing {
						host, exists := state.dashboardHost(e.selected[at])
						if !exists {
							host = Host{Label: m.Missing, Address: e.selected[at]}
						}
						state.declareDashboardHostChoice(c, host, hitDashboardMissingList.At(at), true, true, cardW)
					}
				})
			}
		})
	})
	scroll.DrawOver(func(d *ui.DrawContext) {
		e.bounds = scroll.Rect()
		if area := scroll.ScrollState(); area != nil {
			e.scroll = area.Offset
			e.scrollSynced = area.Offset
			e.content = area.Content
		}
	})
}

// declareDashboardHostChoice declares one host card as a page checkbox over a
// painted card surface.
func (state *State) declareDashboardHostChoice(c ui.C, host Host, id gfx.ID, selected, missing bool, width float32) {
	background := state.pal.surface
	if selected {
		background = state.pal.accentSoft
	}
	color := state.pal.text
	if missing {
		color = state.pal.warning
	}
	row := ui.Row(c).Height(36).Shrink(0).Width(width).PadX(8).Gap(8).Items(ui.Center).
		Radius(radius.control).Border(1, state.pal.line).
		Background(tintHostSurface(background, host.Color, .10)).
		HitWith(id, host.Label, gfx.HitOptions{Role: gfx.RoleCheckbox, Checked: selected})
	row.Children(func() {
		ui.Slot(c).Size(16, 16).Shrink(0).Draw(func(d *ui.DrawContext) {
			paintDashboardCardCheck(d.Canvas, d.Rect, selected)
		})
		ui.Text(c, host.Label).FontSize(13).Weight(gfx.WeightMedium).Color(color).Ellipsis().Grow(1)
	})
}

// paintDashboardCardCheck draws the small checkbox of a host card.
func paintDashboardCardCheck(canvas *gfx.Canvas, b gfx.Rect, checked bool) {
	side := min(b.Width, b.Height)
	box := gfx.Rect{X: b.X, Y: b.Y + (b.Height-side)/2, Width: side, Height: side}
	if checked {
		canvas.Rounded(box, cornerPx(4), gfx.RGB(99, 180, 248))
		canvas.DrawIcon(lucideIcons[iconCheck], gfx.Rect{X: box.X + 3, Y: box.Y + 3, Width: side - 6, Height: side - 6}, gfx.RGB(255, 255, 255))
		return
	}
	canvas.RoundedBorder(box, cornerPx(4), 1.5, gfx.RGB(150, 158, 170), gfx.Color{})
}

// declareDashboardOrderMode declares the sort-mode select of the order tab.
func (state *State) declareDashboardOrderMode(c ui.C) {
	e, m := &state.dashboard.editor, state.messages.Dashboard
	labels := []string{state.messages.HostCatalog.Manual, state.messages.HostCatalog.Name, state.messages.HostCatalog.IP}
	e.orderModeChoice = int(e.orderMode)
	ui.Select(c, &e.orderModeChoice, []int{0, 1, 2}, func(index int) string { return labels[index] }).
		Label(m.SortBy).Hint(labels[e.orderMode]).Height(34).Width(min(220, max(160, min(780, dialogViewport(c).Width-32)-60)))
	if e.orderModeChoice != int(e.orderMode) {
		state.sortDashboardHosts(hostOrderMode(e.orderModeChoice))
	}
}

// declareDashboardOrderList declares the selected hosts as a reorderable page
// List: a drag moves a row, and Alt with the arrows moves the focused one.
func (state *State) declareDashboardOrderList(c ui.C) {
	e, m := &state.dashboard.editor, state.messages.Dashboard
	if e.orderSelected == nil || cap(e.orderSelected) < len(e.selected) {
		e.orderSelected = make([]string, len(e.selected))
	}
	e.orderSelected = e.orderSelected[:len(e.selected)]
	copy(e.orderSelected, e.selected)
	if e.orderKeys == nil {
		e.orderKeys = make(map[string]int)
	}
	for _, host := range e.selected {
		if _, ok := e.orderKeys[host]; !ok {
			e.orderKeys[host] = e.orderKeyNext
			e.orderKeyNext++
		}
	}
	if !e.orderInit {
		e.orderInit = true
		e.orderList.Key = func(index int) int {
			if index < 0 || index >= len(e.orderSelected) {
				return index
			}
			return e.orderKeys[e.orderSelected[index]]
		}
		e.orderList.Label = func(index int) string {
			if index < 0 || index >= len(e.orderSelected) {
				return ""
			}
			if host, exists := state.dashboardHost(e.orderSelected[index]); exists {
				return fmt.Sprintf(m.SortPanel, host.Label)
			}
			return fmt.Sprintf(m.SortPanel, m.Missing)
		}
		e.orderList.NoTypeahead = true
	}
	e.orderList.Reorder = func(rows []int, to int) {
		state.moveDashboardHost(rows, to)
	}
	ui.Column(c).Grow(1).Basis(0).MinHeight(0).Children(func() {
		ui.List(c, &e.orderList, len(e.selected), state.declareDashboardOrderRow).
			Grow(1).Basis(0).MinHeight(0).Scrollbars().ScrollbarSize(catalogBarGutter).Label(m.OrderTab)
	}).DrawOver(func(d *ui.DrawContext) {
		e.bounds = d.Rect
	})
	if len(e.selected) == 0 {
		ui.Text(c, m.HostsRequired).FontSize(12).Color(state.pal.muted)
	}
}

// declareDashboardOrderRow declares one selected host in the order list.
func (state *State) declareDashboardOrderRow(row ui.ListRow) {
	e, m := &state.dashboard.editor, state.messages.Dashboard
	if row.Index < 0 || row.Index >= len(e.selected) {
		return
	}
	id := e.selected[row.Index]
	host, exists := state.dashboardHost(id)
	label := host.Label
	address := host.Address
	color := state.Theme().Foreground
	if !exists {
		label, address, color = m.Missing, id, state.pal.warning
	}
	row.Element.Height(44).MarginBottom(4).PadX(12).Gap(10).Items(ui.Center).
		Radius(state.Theme().Metrics.RadiusControl).Border(1, state.Theme().Border).
		Background(state.Theme().Background).Children(func() {
		ui.Textf(row.C, "%s  %s", label, address).FontSize(13).Color(color).Ellipsis().Grow(1)
	})
}
