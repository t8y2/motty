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
	hitHostViewTree = tree.ID(hostGridHits, "host-view-tree")
)

const (
	hostCardMinWidth      = float32(280)
	hostCardHeight        = float32(64)
	hostCardGap           = float32(12)
	hostGridHeader        = float32(42)
	hostGridSectionHeader = float32(30)
)

// hostGridDrag is the card grid's drag of a group header onto another group
// header. The list view drags through its Tree; the grid is painted by hand, so
// its headers are declared as a page DragState's sources and targets.
type hostGridDrag struct {
	state  ui.DragState
	accept func(source, target gfx.ID) bool
	drop   func(source, target gfx.ID)
}

// hostGridDropShare is the part of a group header's height that orders the group
// beside the target instead of inside it: its top and bottom quarters, as a Tree
// row's top and bottom quarters insert beside it.
const hostGridDropShare = float32(0.25)

// The wash and edge that mark a group the drag would join: the share of the
// accent mixed into the row behind the group, and the alpha of its edge.
const (
	hostGridDropWash = float32(0.10)
	hostGridDropEdge = float32(0.5)
)

// ensureHostGridDrag makes the callbacks the DragState keeps, once.
func (state *State) ensureHostGridDrag() *hostGridDrag {
	drag := &state.hostGridDrag
	if drag.accept == nil {
		drag.accept, drag.drop = state.hostGridDragAccept, state.hostGridDragDrop
	}
	return drag
}

// hostGridPaintState keeps the grid control's draw callback across paints. The
// grid is one Control whose Draw paints every visible card and group header, so
// their parts are declared in the page and the page routes their drag.
type hostGridPaintState struct {
	draw func(*ui.DrawContext)
}

// ensureHostGridPaint makes the grid's draw callback, once.
func (state *State) ensureHostGridPaint() *hostGridPaintState {
	paint := &state.hostGridPaint
	if paint.draw == nil {
		paint.draw = state.drawHostGrid
	}
	return paint
}

// hostGroupOpenHit maps a grid group header's identity back to its group. It is
// the drag's source, and the drop that puts a group inside another.
func (state *State) hostGroupOpenHit(id gfx.ID) (int, bool) {
	if id.Scope() != hitHostGroupOpenList {
		return 0, false
	}
	return state.hostGroupHeaderIndex(id)
}

// hostGridDropTarget maps an identity in a group header's drop area back to its
// group and where a drop there lands: its middle joins the group, its top and
// bottom strips place the dragged group before or after it.
func (state *State) hostGridDropTarget(id gfx.ID) (int, ui.TreeDrop, bool) {
	where := ui.TreeDropInside
	switch id.Scope() {
	case hitHostGroupOpenList:
	case hitHostGroupDropBeforeList:
		where = ui.TreeDropBefore
	case hitHostGroupDropAfterList:
		where = ui.TreeDropAfter
	default:
		return 0, 0, false
	}
	index, ok := state.hostGroupHeaderIndex(id)
	return index, where, ok
}

// hostGroupHeaderIndex is the group a numbered group header identity names.
func (state *State) hostGroupHeaderIndex(id gfx.ID) (int, bool) {
	index := item(id)
	if index < 0 || index >= len(state.hostGroups) {
		return 0, false
	}
	return index, true
}

// hostGridDragAccept takes a group header only where the list view would: inside
// another group, not into the source or what is inside it, and not beside a group
// of its own name. A drop beside the target's top or bottom edge is accepted only
// when the manual order can place the group there.
func (state *State) hostGridDragAccept(source, target gfx.ID) bool {
	if state.activeSection != sectionHosts || state.modal != modalNone ||
		state.hostSort.enabled || state.hostView != hostViewGrid {
		return false
	}
	from, ok := state.hostGroupOpenHit(source)
	if !ok {
		return false
	}
	to, where, ok := state.hostGridDropTarget(target)
	if !ok {
		return false
	}
	if where == ui.TreeDropInside {
		return state.hostGroupMoveAllowed(from, to)
	}
	return state.hostGroupMoveBesideAllowed(from, to)
}

// hostGridDragDrop moves the dragged group under the group it was let go on, or,
// in the strips at that group's edges, beside it in the target's folder.
func (state *State) hostGridDragDrop(source, target gfx.ID) {
	if state.activeSection != sectionHosts || state.modal != modalNone || state.hostSort.enabled {
		return
	}
	from, ok := state.hostGroupOpenHit(source)
	if !ok {
		return
	}
	to, where, ok := state.hostGridDropTarget(target)
	if !ok {
		return
	}
	var moved bool
	switch where {
	case ui.TreeDropInside:
		if !state.hostGroupMoveAllowed(from, to) {
			return
		}
		moved = state.moveHostGroup(state.hostGroups[from].Name, state.hostGroups[to].Name)
	case ui.TreeDropBefore, ui.TreeDropAfter:
		if !state.hostGroupMoveBesideAllowed(from, to) {
			return
		}
		moved = state.moveHostGroupBeside(from, to, where == ui.TreeDropAfter)
	default:
		return
	}
	if !moved {
		state.status, state.dialogError = state.dialogError, ""
	}
}

// hostGridDropZones is where a group header's top and bottom strips are: the
// shares of its cell that order the group beside the header. The rest of the
// cell joins the group.
func hostGridDropZones(cell gfx.Rect) (before, after gfx.Rect) {
	height := cell.Height * hostGridDropShare
	before = gfx.Rect{X: cell.X, Y: cell.Y, Width: cell.Width, Height: height}
	after = gfx.Rect{X: cell.X, Y: cell.Y + cell.Height - height, Width: cell.Width, Height: height}
	return before, after
}

// paintHostGridDrop marks where a dragged group would land: a line at the edge it
// would be inserted at, or a wash and edge around the group it would join, as the
// list's Tree marks its rows.
func (state *State) paintHostGridDrop(canvas *gfx.Canvas, cell gfx.Rect, where ui.TreeDrop) {
	if where == ui.TreeDropInside {
		// The row's wash is painted opaque under a translucent edge: a filled
		// translucent border would fill the whole cell with its border colour.
		wash := mixColor(state.pal.surfaceSoft, state.pal.accent, hostGridDropWash)
		canvas.RoundedBorder(cell, radius.control, 2, withAlpha(state.pal.accent, hostGridDropEdge), wash)
		return
	}
	y := cell.Y - 1
	if where == ui.TreeDropAfter {
		y = cell.Y + cell.Height - 1
	}
	canvas.Fill(gfx.Rect{X: cell.X + 8, Y: y, Width: max(cell.Width-16, 0), Height: 2}, state.pal.accent)
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
	if id == hitHostViewTree {
		// The group tree stands beside either layout: it is a pane, not a third
		// view, so the list and grid choices keep working with it open.
		if state.activeSection == sectionHosts && state.modal == modalNone && state.hasHostFilter() {
			state.hostSide.open = !state.hostSide.open
			if state.hostSide.open {
				state.revealHostFilterGroup()
				state.rebuildHostTreeRows()
			}
			state.save()
		}
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

// hostGridWidestRun is the number of hosts in the widest contiguous same-group
// run between group boundaries: the columns a row can actually fill. A grid
// never needs more columns than that, or widening the window past the point
// where an empty column would fit would shrink every card although no card was
// added.
func (state *State) hostGridWidestRun() int {
	widest, run, runGroup := 1, 0, -1
	for _, entry := range state.hostTreeRows {
		if entry.Kind == hostTreeGroup {
			widest, run, runGroup = max(widest, run), 0, -1
			continue
		}
		if entry.GroupIndex != runGroup {
			widest, run, runGroup = max(widest, run), 0, entry.GroupIndex
		}
		run++
	}
	return max(widest, run)
}

// hostGridColumnCount is how many columns the grid shows at width: every column
// that fits, but never more than the hosts to fill a row.
func (state *State) hostGridColumnCount(width float32) int {
	return min(hostGridColumns(width), state.hostGridWidestRun())
}

// hostGridMeasure returns the total card layout height at width when
// targetHostID == 0, or the vertical offset of targetHostID when non-zero.
func (state *State) hostGridMeasure(width float32, targetHostID uint32) (float32, bool) {
	columns := state.hostGridColumnCount(width)
	var height float32
	run, lastHeaderGroup, runGroup := 0, -1, -1
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
			lastHeaderGroup, runGroup = entry.GroupIndex, -1
			continue
		}
		if entry.GroupIndex != runGroup {
			flush()
			runGroup = entry.GroupIndex
			if lastHeaderGroup >= 0 && entry.GroupIndex != lastHeaderGroup {
				height += hostGridSectionHeader
				lastHeaderGroup = entry.GroupIndex
			}
		}
		if targetHostID != 0 && state.hosts[entry.HostIndex].ID == targetHostID {
			return height + float32(run/columns)*(hostCardHeight+hostCardGap), true
		}
		run++
	}
	flush()
	if targetHostID != 0 {
		return 0, false
	}
	return height, true
}

// hostGridHeight is the height of the whole card layout at width.
func (state *State) hostGridHeight(width float32) float32 {
	height, _ := state.hostGridMeasure(width, 0)
	return height
}

// declareHostGrid declares the card grid inside the hosts page as one control:
// its Draw paints only what is on screen, scrolled by hostsScroll, as the list's
// Tree declares only its visible rows. The cards and group headers keep their
// application identities, so the central click dispatch still owns them, while
// the control places their parts in the page and routes their drag.
func (state *State) declareHostGrid(c ui.C, bounds gfx.Rect) {
	content := state.hostListHeight(bounds.Width)
	_, geometry := hostListScrollArea(nil, state.Theme(), bounds, content, state.hostsScroll, state.hostListBarRevealed())
	if geometry.MaxOffsetY > 0 {
		// Taking the bar's strip narrows the list; the cards pack to the width they
		// are really given, so read the height again at the reserved width.
		content = state.hostListHeight(geometry.Viewport.Width)
		_, geometry = hostListScrollArea(nil, state.Theme(), bounds, content, state.hostsScroll, state.hostListBarRevealed())
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
		_, geometry = hostListScrollArea(nil, state.Theme(), bounds, content, state.hostsScroll, state.hostListBarRevealed())
	}
	state.hostsContent, state.hostsScroll = content, geometry.OffsetY
	viewport := geometry.Viewport
	// The sort's edge scroll reads the pointer in window coordinates; the last
	// paint's absolute viewport is what the list view uses for the same check.
	if state.hostSortAutoScrollFrame(canvas, state.hostsGridViewport, content) {
		_, geometry = hostListScrollArea(nil, state.Theme(), bounds, content, state.hostsScroll, state.hostListBarRevealed())
		state.hostsScroll = geometry.OffsetY
		viewport = geometry.Viewport
	}
	paint := state.ensureHostGridPaint()
	ui.Control(c, state.messages.NavHosts, &state.hostGridHandlers).
		Absolute().Left(viewport.X).Top(viewport.Y).Width(viewport.Width).Height(viewport.Height).Clip().
		Draw(paint.draw)
}

// drawHostGrid paints the card grid's control: only the cards and group headers
// on screen, each with the application identity it has always had and, for a
// group header, its part in the page's drag.
func (state *State) drawHostGrid(d *ui.DrawContext) {
	rows, canvas, viewport := state.hostTreeRows, d.Canvas, d.Rect
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return
	}
	drag := state.ensureHostGridDrag()
	columns := state.hostGridColumnCount(viewport.Width)
	cardWidth := (viewport.Width - float32(columns-1)*hostCardGap) / float32(columns)
	state.beginHostSortPaint(canvas, viewport)
	y := viewport.Y - state.hostsScroll
	bottom := viewport.Y + viewport.Height
	lastHeaderGroup := -1
	for index := 0; index < len(rows) && y < bottom; {
		if rows[index].Kind == hostTreeGroup {
			// Its text sits on the same line as a group row's in the list view.
			groupIndex := rows[index].GroupIndex
			lastHeaderGroup = groupIndex
			if y+hostGridHeader > viewport.Y {
				// The header's cell is the drag source; its middle joins the group
				// and the strips at its edges order the group beside it. Its name
				// toggles the group on a click, as the list's group row does.
				group := state.hostGroups[groupIndex]
				id := hitHostGroupOpenList.At(groupIndex)
				cell := gfx.Rect{X: viewport.X, Y: y + 4, Width: viewport.Width, Height: hostGridHeader - 8}
				if canvas.Hovered() == id {
					canvas.Rounded(cell, radius.control, state.pal.surfaceSoft)
				}
				canvas.HitWithOptions(id, cell, state.hostGroupLabel(group), gfx.HitOptions{
					Role: gfx.RoleButton, Expanded: group.Expanded, Hint: group.Name,
				})
				d.DragSource(&drag.state, id, cell)
				// The cell is the fallback drop, declared first so the edge strips
				// are asked before it: a drop that cannot order the group there
				// still joins it.
				d.DropTarget(&drag.state, id, cell, drag.accept, drag.drop)
				before, after := hostGridDropZones(cell)
				d.DropTarget(&drag.state, hitHostGroupDropBeforeList.At(groupIndex), before, drag.accept, drag.drop)
				d.DropTarget(&drag.state, hitHostGroupDropAfterList.At(groupIndex), after, drag.accept, drag.drop)
				if target, where, ok := state.hostGridDropTarget(drag.state.Target()); ok && target == groupIndex {
					state.paintHostGridDrop(canvas, cell, where)
				}
				state.paintHostGridHeader(canvas, cell, groupIndex, rows[index].Depth)
			}
			y += hostGridHeader
			index++
			continue
		}
		groupIndex := rows[index].GroupIndex
		if lastHeaderGroup >= 0 && groupIndex != lastHeaderGroup {
			if y+hostGridSectionHeader > viewport.Y {
				cell := gfx.Rect{X: viewport.X, Y: y, Width: viewport.Width, Height: hostGridSectionHeader}
				state.paintHostGridSectionDivider(canvas, cell, groupIndex, rows[index].Depth)
			}
			y += hostGridSectionHeader
			lastHeaderGroup = groupIndex
			if y >= bottom {
				break
			}
		}
		end := index
		for end < len(rows) && end-index < columns && rows[end].Kind == hostTreeHost && rows[end].GroupIndex == groupIndex {
			end++
		}
		if y+hostCardHeight >= viewport.Y {
			// Every row has all its columns, so the last card is no wider than the rest.
			for column := 0; column < columns && index+column < end; column++ {
				card := gfx.Rect{
					X:      viewport.X + float32(column)*(cardWidth+hostCardGap),
					Y:      y,
					Width:  cardWidth,
					Height: hostCardHeight,
				}
				state.paintHostCard(canvas, card, state.hosts[rows[index+column].HostIndex])
			}
		}
		y += hostCardHeight + hostCardGap
		index = end
	}
	state.endHostSortPaint()
}

// paintHostGridSectionDivider separates a parent group's direct host cards from
// the preceding subgroup's cards so they do not visually merge into the subgroup.
func (state *State) paintHostGridSectionDivider(canvas *gfx.Canvas, cell gfx.Rect, groupIndex int, depth uint8) {
	if groupIndex < 0 || groupIndex >= len(state.hostGroups) {
		return
	}
	pal, group := state.pal, state.hostGroups[groupIndex]
	label := group.Label
	if group.Name == "" {
		label = state.messages.HostGroupDefault
	}
	indent := float32(max(0, int(depth)-1)) * 22
	x := cell.X + indent
	right := cell.X + cell.Width - 2
	maxLabel := max(0, right-x)
	style := gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: pal.muted}
	labelWidth := min(canvas.TextWidthWeight(label, style.Size, style.Weight), maxLabel)
	if labelWidth > 0 {
		paintHostGridLine(canvas, label, gfx.Rect{X: x, Y: cell.Y, Width: labelWidth, Height: cell.Height}, 20, style)
	}
	lineX := x + labelWidth + 8
	if lineX < right {
		lineY := cell.Y + cell.Height/2
		canvas.Fill(gfx.Rect{X: lineX, Y: lineY, Width: right - lineX, Height: 1}, withAlpha(pal.line, 0.7))
	}
}

// paintHostGridHeader draws the row that opens or closes a group of cards. The
// cell is the page hit and drag region; the content is indented by the group's
// depth, and the actions button keeps the 38 pixels reserved at the right.
func (state *State) paintHostGridHeader(canvas *gfx.Canvas, cell gfx.Rect, groupIndex int, depth uint8) {
	pal, group := state.pal, state.hostGroups[groupIndex]
	label := group.Label
	if group.Name == "" {
		label = state.messages.HostGroupDefault
	}
	chevron := iconChevronRight
	if group.Expanded || state.visibleQuery != "" {
		chevron = iconChevronDown
	}
	content := cell
	content.X += float32(depth) * 22
	content.Width = max(0, content.Width-float32(depth)*22-38)
	right := content.X + content.Width - 2 // the row's right padding
	icon := gfx.Rect{X: content.X, Y: content.Y + (content.Height-16)/2, Width: 16, Height: 16}
	groupCheck := gfx.Rect{X: content.X + 24, Y: content.Y + (content.Height-28)/2, Width: 40, Height: 28}
	textX := groupCheck.X + 48
	textWidth := max(right-textX, 0)
	pin := float32(0)
	if group.Pinned {
		pin = 20 // the star and the gap after it
		paintIcon(canvas, uint8(iconStar), gfx.Rect{X: textX, Y: content.Y + (content.Height-14)/2, Width: 14, Height: 14}, pal.accent)
	}
	countWidth := canvas.TextWidthWeight(group.Count, metrics.FontSizeS, gfx.WeightMedium)
	labelWidth := min(canvas.TextWidthWeight(label, metrics.FontSizeS, gfx.WeightSemibold), max(textWidth-pin-6-countWidth, 0))
	textX += pin
	paintHostGridLine(canvas, label, gfx.Rect{X: textX, Y: content.Y, Width: labelWidth, Height: content.Height},
		23, gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightSemibold, Color: pal.text})
	paintHostGridLine(canvas, group.Count, gfx.Rect{X: textX + labelWidth + 6, Y: content.Y, Width: countWidth, Height: content.Height},
		21, gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: pal.faint})
	paintIcon(canvas, uint8(chevron), icon, pal.muted)
	state.hostGroupRowEvents(canvas, hitHostGroupOpenList.At(groupIndex), groupIndex, cell)
	state.paintHostScopeCheckbox(canvas, hitHostGroupCheck.At(groupIndex), groupCheck, state.hostScopeSelection.groups[groupIndex])
}

// paintHostGridLine paints one line of the grid, cut to its slot's width and
// centered in it at leading as a Text element would be.
func paintHostGridLine(canvas *gfx.Canvas, value string, slot gfx.Rect, leading float32, style gfx.TextStyle) {
	if value == "" {
		return
	}
	line := canvas.FitLine(value, style.Size, style.Weight, slot.Width)
	canvas.Label(line, gfx.Rect{
		X: slot.X, Y: slot.Y + max((slot.Height-leading)/2, 0), Width: slot.Width, Height: leading,
	}, style)
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
		canvas.HitWithOptions(id, card, state.hostPrivacySemanticLabel(host), gfx.HitOptions{Role: gfx.RoleButton, Selected: selected})
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
	left := card.X + 14
	right := card.X + card.Width - 12
	avatar := gfx.Rect{X: left, Y: card.Y + (card.Height-36)/2, Width: 36, Height: 36}
	check := gfx.Rect{X: right - checkWidth, Y: card.Y + (card.Height-checkHeight)/2, Width: checkWidth, Height: checkHeight}
	textX := left + 36 + 12
	textWidth := max(check.X-12-textX, 0)
	tagCount := 0
	var tagList [maxHostTags]string
	protocolLabel := state.hostProtocolTagLabel(host)
	protocolWidth := canvas.TextWidthWeight(protocolLabel, metrics.FontSizeS, gfx.WeightMedium) + 15
	subtitleLimit := textWidth
	if protocolWidth+8 <= textWidth {
		// Reserve the protocol tag first: the connection line gives way to it
		// rather than hiding the tag on a long account and destination.
		subtitleLimit = textWidth - protocolWidth - 8
	}
	subtitleWidth := min(canvas.TextWidth(subtitle, metrics.FontSizeS)+1, subtitleLimit)
	if state.hostPrivacy.enabled {
		subtitleWidth = state.hostPrivacyConnectionWidth(canvas, host, subtitleLimit)
	}
	room := textWidth - subtitleWidth - 8
	showProtocol := room >= protocolWidth
	if showProtocol {
		tagCount = state.fitHostTags(canvas, host, max(room-protocolWidth-5, 0), &tagList)
	} else if room >= 40 {
		tagCount = state.fitHostTags(canvas, host, room, &tagList)
	}
	// The title and the connection line are 22 and 1+19 tall, centered as the
	// column was in the card's row.
	columnTop := card.Y + (card.Height-42)/2
	nameBounds := gfx.Rect{X: textX, Y: columnTop, Width: textWidth, Height: 22}
	connectionBounds := gfx.Rect{X: textX, Y: columnTop + 23, Width: subtitleWidth, Height: 19}
	if state.hostPrivacy.enabled {
		if state.hostPrivacy.nameHidden() {
			state.paintHostPrivacyMask(canvas, nameBounds, host.Label, gfx.TextStyle{Size: metrics.FontSizeM, Weight: gfx.WeightSemibold, Color: pal.text})
		} else {
			paintHostGridLine(canvas, host.Label, nameBounds, 22, gfx.TextStyle{Size: metrics.FontSizeM, Weight: gfx.WeightSemibold, Color: pal.text})
		}
		state.paintHostPrivacyConnection(canvas, connectionBounds, host)
	} else {
		paintHostGridLine(canvas, host.Label, nameBounds, 22, gfx.TextStyle{Size: metrics.FontSizeM, Weight: gfx.WeightSemibold, Color: pal.text})
		paintHostGridLine(canvas, subtitle, connectionBounds, 19, gfx.TextStyle{Size: metrics.FontSizeS, Color: pal.muted})
	}
	if showProtocol || tagCount > 0 {
		pillX := textX + subtitleWidth + 8
		pillY := columnTop + 23 + (19-18)/2
		if showProtocol {
			pill := gfx.Rect{X: pillX, Y: pillY, Width: protocolWidth, Height: 18}
			paintHostProtocolTag(canvas, pill, protocolLabel, hostProtocolTagColor(pal, host.Protocol))
			pillX += pill.Width + 5
		}
		for _, tag := range tagList[:tagCount] {
			width := canvas.TextWidthWeight(tag, metrics.FontSizeS, gfx.WeightMedium)
			pill := gfx.Rect{X: pillX, Y: pillY, Width: width + 15, Height: 18}
			canvas.Rounded(pill, 9, pal.surfaceSoft)
			canvas.Label(tag, gfx.Rect{X: pill.X + 7, Y: pill.Y, Width: width + 1, Height: 18},
				gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: pal.muted})
			pillX += pill.Width + 5
		}
	}
	if state.hostSort.enabled {
		state.declareHostSortItem(host, card, check)
	} else {
		side := components.CheckboxExtent(state.Theme())
		bounds := gfx.Rect{X: check.X + (check.Width-side)/2, Y: check.Y + (check.Height-side)/2, Width: side, Height: side}
		components.BeginCheckbox(canvas, state.Theme(), hitHostCheckList.At(int(host.ID)), bounds, state.hostSelected(host.ID)).
			Label(state.hostPrivacySemanticLabel(host)).Paint()
	}
	state.paintHostCatalogIcon(canvas, host, avatar, badgeBackground)
	if kind != lightIdle {
		corner := gfx.Rect{X: avatar.X + 27, Y: avatar.Y + 27, Width: 14, Height: 14}
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
