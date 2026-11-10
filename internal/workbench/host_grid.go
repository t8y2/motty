package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// The hosts page shows its hosts as a list or as cards. Both read the same rows
// (hostTreeRows), use the same ids for selecting, checking and opening a host and
// share one scroll position; only the layout differs.

type hostViewMode uint8

const (
	hostViewList hostViewMode = iota
	hostViewGrid
)

// hostGridHits is the place of the controls this file paints.
var hostGridHits = tree.Feature("host-grid")

var (
	hitHostViewList = tree.ID(hostGridHits, "host-view-list")
	hitHostViewGrid = tree.ID(hostGridHits, "host-view-grid")
	hitHostSort     = tree.ID(hostGridHits, "host-sort")
)

const (
	hostCardMinWidth = float32(280)
	hostCardHeight   = float32(64)
	hostCardGap      = float32(12)
	hostGridHeader   = float32(42)
)

// hostGridDrag is the card grid's drag of a group header onto another group
// header. The list view drags through its Tree; the grid is painted by hand, so
// its headers are declared as a page DragState's sources and targets.
type hostGridDrag struct {
	state  ui.DragState
	accept func(source, target gfx.ID) bool
	drop   func(source, target gfx.ID)
}

// ensureHostGridDrag makes the callbacks the DragState keeps, once.
func (state *State) ensureHostGridDrag() *hostGridDrag {
	drag := &state.hostGridDrag
	if drag.accept == nil {
		drag.accept, drag.drop = state.hostGridDragAccept, state.hostGridDragDrop
	}
	return drag
}

// hostGridPaintState keeps the functions the grid's declaration uses across
// paints: the reorder surface that wraps its items, and the drawers that paint
// them.
type hostGridPaintState struct {
	sortUnder, sortOver func(*ui.DrawContext)
}

// ensureHostGridPaint makes the grid's paint callbacks, once.
func (state *State) ensureHostGridPaint() *hostGridPaintState {
	paint := &state.hostGridPaint
	if paint.sortUnder == nil {
		paint.sortUnder, paint.sortOver = state.drawHostGridSortUnder, state.drawHostGridSortOver
	}
	return paint
}

// drawHostGridSortUnder starts the reorder overlay in the grid's viewport, and
// drawHostGridSortOver ends it once the page has painted the cards.
func (state *State) drawHostGridSortUnder(d *ui.DrawContext) {
	state.beginHostSortPaint(d.Canvas, d.Rect)
}

func (state *State) drawHostGridSortOver(*ui.DrawContext) {
	state.endHostSortPaint()
}

// hostGroupOpenHit maps a grid group header's hit identity back to its group.
func (state *State) hostGroupOpenHit(id gfx.ID) (int, bool) {
	if id.Scope() != hitHostGroupOpenList {
		return 0, false
	}
	index := item(id)
	if index < 0 || index >= len(state.hostGroups) {
		return 0, false
	}
	return index, true
}

// hostGridDragAccept takes a group header only where the list view would: the
// destination is another group, not the source or what is inside it, and the
// move does not meet a group of its own name.
func (state *State) hostGridDragAccept(source, target gfx.ID) bool {
	if state.activeSection != sectionHosts || state.modal != modalNone ||
		state.hostSort.enabled || state.hostView != hostViewGrid {
		return false
	}
	from, ok := state.hostGroupOpenHit(source)
	if !ok {
		return false
	}
	to, ok := state.hostGroupOpenHit(target)
	if !ok {
		return false
	}
	return state.hostGroupMoveAllowed(from, to)
}

// hostGridDragDrop moves the dragged group under the group it was let go on.
func (state *State) hostGridDragDrop(source, target gfx.ID) {
	if state.activeSection != sectionHosts || state.modal != modalNone || state.hostSort.enabled {
		return
	}
	from, ok := state.hostGroupOpenHit(source)
	if !ok {
		return
	}
	to, ok := state.hostGroupOpenHit(target)
	if !ok || !state.hostGroupMoveAllowed(from, to) {
		return
	}
	if !state.moveHostGroup(state.hostGroups[from].Name, state.hostGroups[to].Name) {
		state.status, state.dialogError = state.dialogError, ""
	}
}

func hostViewFromString(value string) hostViewMode {
	if value == "grid" {
		return hostViewGrid
	}
	return hostViewList
}

func (mode hostViewMode) saved() string {
	if mode == hostViewGrid {
		return "grid"
	}
	return ""
}

func (state *State) clickHostView(id gfx.ID) bool {
	if id == hitHostSort {
		state.toggleHostSort()
		return true
	}
	var mode hostViewMode
	switch id {
	case hitHostViewList:
		mode = hostViewList
	case hitHostViewGrid:
		mode = hostViewGrid
	default:
		return false
	}
	if state.activeSection == sectionHosts && state.modal == modalNone && state.hostView != mode {
		state.hostView, state.hostsScroll = mode, 0
		state.save()
	}
	return true
}

// hostGridColumns is how many cards fit across width.
func hostGridColumns(width float32) int {
	return max(int((width+hostCardGap)/(hostCardMinWidth+hostCardGap)), 1)
}

// hostGridHeight is the height of the whole card layout at width.
func (state *State) hostGridHeight(width float32) float32 {
	columns := hostGridColumns(width)
	var height float32
	run := 0
	flush := func() {
		if run > 0 {
			height += float32((run+columns-1)/columns) * (hostCardHeight + hostCardGap)
			run = 0
		}
	}
	for _, entry := range state.hostTreeRows {
		if entry.Kind == hostTreeGroup {
			flush()
			height += hostGridHeader
		} else {
			run++
		}
	}
	flush()
	return height
}

// hostGridDrawer paints one card or group header the page declared. The Draw of
// an item is kept for the frame; the drawer carries its row so the callback reads
// the right entry, like the list's hostRowDrawer.
type hostGridDrawer struct {
	owner *State
	entry int // the index into hostTreeRows
	fn    func(*ui.DrawContext)
}

func (d *hostGridDrawer) paint(ctx *ui.DrawContext) { d.owner.paintHostGridEntry(ctx, d.entry) }

// declareHostGrid declares the cards of the grid view inside the hosts page:
// only what is on screen, scrolled by hostsScroll, as the list's Tree declares
// only its visible rows. Each item paints itself through the Draw of the page
// element that carries its hit and, for a group header, its drag state.
func (state *State) declareHostGrid(c ui.C, width, height, headerHeight float32) {
	rows := state.hostTreeRows
	bounds := gfx.Rect{X: 18, Y: headerHeight, Width: max(width-36, 0), Height: max(height-headerHeight-18, 0)}
	content := state.hostListHeight(bounds.Width)
	_, geometry := hostListScrollArea(nil, state.Theme(), bounds, content, state.hostsScroll)
	if geometry.MaxOffsetY > 0 {
		// Taking the bar's strip narrows the list; the cards pack to the width they
		// are really given, so read the height again at the reserved width.
		content = state.hostListHeight(geometry.Viewport.Width)
		_, geometry = hostListScrollArea(nil, state.Theme(), bounds, content, state.hostsScroll)
	}
	canvas := c.Canvas()
	if state.hostRevealID != 0 {
		state.hostsScroll = state.sessionLogHostOffset(state.hostRevealID, geometry.Viewport.Width)
		focus := hitHostList.At(int(state.hostRevealID))
		if state.hostSort.enabled {
			focus = hitHostSortHandles.At(int(state.hostRevealID))
		}
		canvas.RequestFocus(focus)
		state.hostRevealID = 0
		_, geometry = hostListScrollArea(nil, state.Theme(), bounds, content, state.hostsScroll)
	}
	state.hostsContent, state.hostsScroll = content, geometry.OffsetY
	viewport := geometry.Viewport
	// The sort's edge scroll reads the pointer in window coordinates; the last
	// paint's absolute viewport is what the list view uses for the same check.
	if state.hostSortAutoScrollFrame(canvas, state.hostsGridViewport, content) {
		_, geometry = hostListScrollArea(nil, state.Theme(), bounds, content, state.hostsScroll)
		state.hostsScroll = geometry.OffsetY
		viewport = geometry.Viewport
	}
	state.hostGridDrawerCount = 0
	paint := state.ensureHostGridPaint()
	drag := state.ensureHostGridDrag()
	columns := hostGridColumns(viewport.Width)
	y := viewport.Y - state.hostsScroll
	bottom := viewport.Y + viewport.Height
	grid := ui.Column(c).Absolute().Left(viewport.X).Top(viewport.Y).Width(viewport.Width).Height(viewport.Height).Clip()
	// The reorder overlay wraps the cards the page paints: its surface starts
	// before the first item and ends after the last.
	grid.Draw(paint.sortUnder).DrawOver(paint.sortOver)
	grid.Children(func() {
		for index := 0; index < len(rows) && y < bottom; {
			if rows[index].Kind == hostTreeGroup {
				// Its text sits on the same line as a group row's in the list view.
				if y+hostGridHeader > viewport.Y {
					// The header's cell is the drag source and drop target; its name
					// toggles the group on a click, as the list's group row does.
					groupIndex := rows[index].GroupIndex
					group := state.hostGroups[groupIndex]
					box := ui.Slot(c).Absolute().Left(0).Top(y - viewport.Y + 4).Width(viewport.Width).Height(hostGridHeader - 8)
					box.HitWith(hitHostGroupOpenList.At(groupIndex), state.hostGroupLabel(group), gfx.HitOptions{
						Role: gfx.RoleButton, Expanded: group.Expanded, Hint: group.Name,
					}).Radius(radius.control).Hover(ui.Look{}.Background(state.pal.surfaceSoft)).
						DragSource(&drag.state).
						DropTarget(&drag.state, drag.accept, drag.drop).
						Draw(state.hostGridDrawerFor(index))
				}
				y += hostGridHeader
				index++
				continue
			}
			end := index
			for end < len(rows) && end-index < columns && rows[end].Kind == hostTreeHost {
				end++
			}
			if y+hostCardHeight >= viewport.Y {
				// Every row has all its columns, so the last card is no wider than the rest.
				row := ui.Row(c).Absolute().Left(0).Top(y - viewport.Y).Width(viewport.Width).Height(hostCardHeight).Gap(hostCardGap)
				row.Children(func() {
					for column := 0; column < columns; column++ {
						card := ui.Slot(c).Grow(1).Basis(0)
						if index+column < end {
							card.Draw(state.hostGridDrawerFor(index + column))
						}
					}
				})
			}
			y += hostCardHeight + hostCardGap
			index = end
		}
	})
}

// paintHostGridEntry paints one card or group header the page declared, scrolled
// by hostsScroll, from its entry into hostTreeRows.
func (state *State) paintHostGridEntry(ctx *ui.DrawContext, entry int) {
	rows := state.hostTreeRows
	if entry < 0 || entry >= len(rows) {
		return
	}
	if rows[entry].Kind == hostTreeGroup {
		groupIndex := rows[entry].GroupIndex
		state.paintHostGridHeader(ctx.Canvas, ctx.Rect, groupIndex, rows[entry].Depth)
		if state.hostGridDrag.state.Target() == hitHostGroupOpenList.At(groupIndex) {
			header := ctx.Rect
			ctx.Canvas.Fill(gfx.Rect{X: header.X, Y: header.Y, Width: header.Width, Height: 2}, state.pal.accent)
		}
		return
	}
	host := state.hosts[rows[entry].HostIndex]
	state.paintHostCard(ctx.Canvas, ctx.Rect, host)
}

// hostGridDrawerFor gives the next declared item a drawer that paints its row.
// Drawers are reused between paints and keep one method value each, so declaring
// a frame's items makes no closure per card.
func (state *State) hostGridDrawerFor(entry int) func(*ui.DrawContext) {
	if state.hostGridDrawerCount == len(state.hostGridDrawers) {
		drawer := &hostGridDrawer{owner: state}
		drawer.fn = drawer.paint
		state.hostGridDrawers = append(state.hostGridDrawers, drawer)
	}
	drawer := state.hostGridDrawers[state.hostGridDrawerCount]
	state.hostGridDrawerCount++
	drawer.entry = entry
	return drawer.fn
}

// paintHostGridHeader draws the row that opens or closes a group of cards.
func (state *State) paintHostGridHeader(canvas *gfx.Canvas, header gfx.Rect, groupIndex int, depth uint8) {
	pal, group := state.pal, state.hostGroups[groupIndex]
	row := header
	header.X += float32(depth) * 22
	header.Width = max(0, header.Width-float32(depth)*22-38)
	label := group.Label
	if group.Name == "" {
		label = state.messages.HostGroupDefault
	}
	chevron := iconChevronRight
	if group.Expanded || state.visibleQuery != "" {
		chevron = iconChevronDown
	}
	var icon, groupCheck ui.Box
	f := state.hostGridInner.Begin(canvas, state.Theme(), header)
	f.Row().Fill().PadSides(0, 0, 0, 2).Gap(8).Items(ui.Center).
		Radius(radius.control).
		Children(func() {
			icon = f.Slot().Size(16, 16)
			groupCheck = f.Slot().Size(40, 28).Shrink(0)
			f.Row().Grow(1).Basis(0).Gap(6).Items(ui.Center).Children(func() {
				if group.Pinned {
					f.Icon(lucideIcons[iconStar]).Size(14, 14).Color(pal.accent)
				}
				f.Text(label).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(23).Ellipsis().Shrink(1)
				f.Text(group.Count).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).LineHeight(21)
			})
		})
	f.End()
	paintIcon(canvas, uint8(chevron), icon.Rect(), pal.muted)
	state.hostGroupRowEvents(canvas, hitHostGroupOpenList.At(groupIndex), groupIndex, row)
	state.paintHostScopeCheckbox(canvas, hitHostGroupCheck.At(groupIndex), groupCheck.Rect(), state.hostScopeSelection.groups[groupIndex])
}

func (state *State) paintHostCard(canvas *gfx.Canvas, card gfx.Rect, host Host) {
	pal := state.pal
	id := hitHostList.At(int(host.ID))
	selected := host.ID == state.selectedHostID
	background, edge, edgeWidth := pal.surface, withAlpha(pal.line, 0.8), float32(1)
	switch {
	case state.hostSortDragging(host.ID):
		background, edge, edgeWidth = pal.surfaceSoft, pal.accent, 1.5
	case state.hostSortTarget(host.ID):
		edge, edgeWidth = pal.accent, 2
	case selected:
		background, edge, edgeWidth = pal.accentSoft, pal.accent, 1.5
	case state.hostSortHovered(canvas, host.ID):
		background, edge, edgeWidth = pal.surfaceSoft, pal.accent, 1.5
	case canvas.Hovered() == id:
		background, edge = pal.surfaceSoft, withAlpha(pal.line, 0.9)
	}
	// The icon badge picks its fill from the exact card background
	// (hostBadgeFill); give it the pre-tint one so a selected card keeps the
	// contrasting badge instead of accentSoft-on-accentSoft.
	badgeBackground := background
	background = tintHostSurface(background, host.Color, .10)
	if state.hostSortDragging(host.ID) {
		canvas.Elevate(card, radius.surface, gfx.ShadowMD)
	}
	canvas.RoundedBorder(card, radius.surface, edgeWidth, edge, background)
	paintHostColorMark(canvas, card, host.Color)
	if !state.hostSort.enabled {
		canvas.HitWithOptions(id, card, host.Label, gfx.HitOptions{Role: gfx.RoleButton, Selected: selected})
		state.hostRowEvents(canvas, id, host)
	}
	if canvas.FocusVisible() == id {
		canvas.RoundedBorder(card, radius.surface, 1.5, pal.accent, gfx.Color{})
	}

	// The avatar and the two lines of text share the card's vertical center with
	// the checkbox. The connection's state is a light on the avatar's corner.
	_, dot := state.hostStatus(host)
	kind, _ := state.hostLight(host)
	// Keep the account and the destination on one line, each named once, then
	// show as many whole tag pills as fit before the checkbox's full interaction
	// region.
	subtitle := state.hostCardConnection(host)
	checkWidth, checkHeight := components.CheckboxHitExtent(state.Theme())
	textWidth := max(card.Width-14-12-36-12*2-checkWidth, 0)
	tagCount := 0
	var tagList [maxHostTags]string
	subtitleWidth := min(canvas.TextWidth(subtitle, metrics.FontSizeS)+1, textWidth)
	if room := textWidth - subtitleWidth - 8; room >= 40 {
		tagCount = state.fitHostTags(canvas, host, room, &tagList)
	}
	var grip, avatar, light ui.Box
	f := state.hostGridInner.Begin(canvas, state.Theme(), card)
	f.Row().Fill().PadSides(0, 12, 0, 14).Gap(12).Items(ui.Center).Children(func() {
		f.Stack().Size(36, 36).Shrink(0).Children(func() {
			avatar = f.Slot()
			if kind != lightIdle {
				light = f.Slot().Size(14, 14).Self(ui.End).MarginSides(0, -5, -5, 0)
			}
		})
		f.Column().Grow(1).Basis(0).Children(func() {
			f.Text(host.Label).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(22).Ellipsis()
			f.Row().Gap(8).Items(ui.Center).MarginSides(1, 0, 0, 0).Children(func() {
				f.Text(subtitle).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(19).Width(subtitleWidth).Shrink(0).Ellipsis()
				if tagCount > 0 {
					f.Row().Gap(5).Shrink(0).Items(ui.Center).Children(func() {
						for _, tag := range tagList[:tagCount] {
							f.Row().Height(18).PadX(7).Shrink(0).Items(ui.Center).Radius(9).Background(pal.surfaceSoft).Children(func() {
								f.Text(tag).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Width(canvas.TextWidthWeight(tag, metrics.FontSizeS, gfx.WeightMedium) + 1).Shrink(0)
							})
						}
					})
				}
			})
		})
		if state.hostSort.enabled {
			// Match the card's ordinary checkbox exactly: sorting replaces it in
			// place, while the six-dot glyph is the same size as the list view.
			grip = f.Slot().Size(checkWidth, checkHeight).Shrink(0)
		} else {
			f.Checkbox(hitHostCheckList.At(int(host.ID)), state.hostSelected(host.ID)).Label(host.Label)
		}
	})
	f.End()
	if state.hostSort.enabled {
		state.declareHostSortItem(host, card, grip.Rect())
	}
	state.paintHostCatalogIcon(canvas, host, avatar.Rect(), badgeBackground)
	if kind != lightIdle {
		corner := light.Rect()
		canvas.Rounded(corner, corner.Width/2, background)
		paintLight(canvas, corner.X+corner.Width/2, corner.Y+corner.Height/2, kind, dot, background)
	}
}

// hostCardConnection is the card's second line: the account and the destination,
// each named once. A desktop target already spells its protocol, and a VNC host
// reached over SSH already carries its account, so those take the target as it
// is; a serial line has no account at all and its target holds the device and
// its settings. Only a bare destination, an SSH or telnet address, takes the
// login in front of it.
func (state *State) hostCardConnection(host Host) string {
	switch {
	case host.target == "":
		return state.hostSubtitle(host)
	case host.Protocol == protocolSerial, host.Protocol == protocolVNC:
		return host.target
	case host.Protocol == protocolRDP:
		if host.User == "" {
			return host.target
		}
		return host.User + " · " + host.target
	}
	return state.hostSubtitle(host) + " · " + host.target
}
