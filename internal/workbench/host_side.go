package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var (
	hostSideHits         = tree.Feature("host-side")
	hitHostSideWidth     = tree.ID(hostSideHits, "width")
	hitHostSideGroupMore = tree.Sub(hostSideHits, tree.Name("group-more"))
)

// The hosts page's geometry. The toolbar spans the page; below it the group
// tree, when the user opens it and the window has room for both panes, stands
// beside the host list or grid.
const (
	hostsToolbarTop       = float32(14)
	hostsBodyGap          = float32(12)
	hostsBodyMargin       = float32(18)
	hostsBodyBottom       = float32(18)
	hostSideHeaderHeight  = float32(28)
	hostSideTreeGap       = float32(6)
	hostSideMinWidth      = float32(160)
	hostSideMaxWidth      = float32(360)
	hostSideDefaultWidth  = float32(220)
	hostSidePaneGap       = float32(12)
	hostListMinWidth      = float32(320)
	hostsTwoPaneWidth     = float32(620)
	hostDragGhostHeight   = float32(24)
	hostDragGhostRadius   = float32(4)
	hostDragGhostPadX     = float32(8)
	hostDragGhostFontSize = float32(13)
	hostDragGhostMaxWidth = float32(220)
	hostDragGhostOffsetX  = float32(8)
	hostDragGhostOffsetY  = float32(12)
	hostSideRowHeight     = float32(34)
	hostSideIndent        = float32(24) // one level's step: the chevron's gutter plus the air after it
	hostSideGutter        = paneDividerHitWidth/2 + 1
	hostsListTopGap       = float32(8)
)

// hostsToolbarHeight is the room the toolbar row keeps: its top margin, the
// row itself and the air before the panes below it.
func hostsToolbarHeight() float32 {
	return hostsToolbarTop + toolbarHeight + hostsBodyGap
}

// hostsLayout places the toolbar, the group tree and the host list inside the
// page. The rectangles are relative to the page's slot; the painters translate
// them by the slot's origin.
type hostsLayout struct {
	pageWidth   float32
	sideVisible bool
	compact     bool
	anySelected bool
	// side, split and main are the two panes and the one-pixel divider between
	// them. side is zero when the tree is not shown.
	side, split, main    gfx.Rect
	sideHeader, sideTree gfx.Rect
	// rightHeader is the row over the list (its controls, filters and batch
	// actions) and list the room left for the rows below it.
	rightHeader       gfx.Rect
	rightHeaderHeight float32
	list              gfx.Rect
}

func (state *State) hostsLayout(width, height float32) hostsLayout {
	layout := hostsLayout{pageWidth: width}
	top := hostsToolbarHeight()
	bodyHeight := max(height-top-hostsBodyBottom, 0)
	inner := max(width-2*hostsBodyMargin, 0)
	if state.hostSide.open && state.hasHostFilter() && width >= hostsTwoPaneWidth {
		sideWidth := state.hostSidePaneWidth()
		sideWidth = min(max(sideWidth, hostSideMinWidth), hostSideMaxWidth)
		// The list keeps its own least width; a window that cannot hold both
		// panes falls back to the group chips over the list.
		if maxSide := inner - 1 - hostSidePaneGap - hostListMinWidth; maxSide >= hostSideMinWidth {
			sideWidth = min(sideWidth, maxSide)
			layout.sideVisible = true
			layout.side = gfx.Rect{X: hostsBodyMargin, Y: top, Width: sideWidth, Height: bodyHeight}
			layout.split = gfx.Rect{X: layout.side.X + sideWidth, Y: top, Width: 1, Height: bodyHeight}
			layout.main = gfx.Rect{X: layout.split.X + 1 + hostSidePaneGap, Y: top, Width: inner - sideWidth - 1 - hostSidePaneGap, Height: bodyHeight}
		}
	}
	if !layout.sideVisible {
		layout.main = gfx.Rect{X: hostsBodyMargin, Y: top, Width: inner, Height: bodyHeight}
	}
	layout.anySelected = len(state.hostSelections) > 0
	layout.compact = layout.main.Width < 450
	headerHeight := float32(hostHeaderRowHeight)
	if layout.compact {
		headerHeight += hostHeaderRowHeight + hostHeaderRowGap
	}
	if layout.anySelected {
		headerHeight += hostHeaderRowHeight + hostHeaderRowGap
	}
	layout.rightHeader = gfx.Rect{X: layout.main.X, Y: layout.main.Y, Width: layout.main.Width, Height: headerHeight}
	layout.rightHeaderHeight = headerHeight
	layout.list = gfx.Rect{
		X: layout.main.X, Y: layout.main.Y + headerHeight + hostsListTopGap,
		Width: layout.main.Width, Height: max(layout.main.Height-headerHeight-hostsListTopGap, 0),
	}
	if layout.sideVisible {
		layout.sideHeader = gfx.Rect{X: layout.side.X, Y: layout.side.Y, Width: layout.side.Width - hostSideGutter, Height: hostSideHeaderHeight}
		layout.sideTree = gfx.Rect{
			X: layout.side.X, Y: layout.side.Y + hostSideHeaderHeight + hostSideTreeGap,
			Width: layout.side.Width - hostSideGutter, Height: max(bodyHeight-hostSideHeaderHeight-hostSideTreeGap, 0),
		}
	}
	return layout
}

func (state *State) hostSidePaneWidth() float32 {
	if state.hostSide.width <= 0 {
		return hostSideDefaultWidth
	}
	return state.hostSide.width
}

// hostCatalogSide is the group tree beside the host list or grid: the visible
// groups of the hierarchy as a framework Tree, with scrolling and drag and drop
// owned by TreeState. The host list in the right pane keeps its own Tree; the
// grid is painted by hand.
type hostCatalogSide struct {
	tree    ui.TreeState
	node    func(int) ui.TreeNode
	label   func(int) string
	key     func(int) int
	content func(int)
	canDrop func(int, int, ui.TreeDrop) bool

	// c is the declaration being used while the Tree asks content for its rows.
	c ui.C
	// area is the Tree, scroll its scroll state as of the last paint, and
	// bounds the room it occupies in the window.
	area   ui.Box
	scroll *ui.ScrollState
	bounds gfx.Rect
	// splitBounds is the two panes and the divider between them, in the window:
	// the geometry a drag of the divider maps through.
	splitBounds gfx.Rect
	// open is the user's choice of the tree pane; visible says the layout could
	// show it beside the list on this window.
	open     bool
	visible  bool
	dragging bool
	rowRects []gfx.Rect
	drawers  []*hostSideRowDrawer
	next     int
	// width is the width the user chose. visibleWidth is the width the layout
	// could give it, which a narrow window clamps below width; the divider is
	// painted and dragged at the visible one.
	width, visibleWidth float32
}

// hostSideRowDrawer records where a group row was laid out, so the row's
// actions menu can open under its own button.
type hostSideRowDrawer struct {
	owner *State
	index int
	fn    func(*ui.DrawContext)
}

func (d *hostSideRowDrawer) paint(ctx *ui.DrawContext) {
	d.owner.recordHostSideRowRect(d.index, ctx.Rect)
}

func (state *State) hostSideFns() *hostCatalogSide {
	side := &state.hostSide
	if side.node != nil {
		return side
	}
	side.node = state.hostSideNode
	side.label = state.hostSideLabel
	side.key = state.hostSideKey
	side.content = state.hostSideDeclareRow
	side.canDrop = state.hostSideCanDrop
	side.tree.Node = side.node
	side.tree.Key = side.key
	side.tree.CanDrop = side.canDrop
	side.tree.Indent = hostSideIndent
	return side
}

// hostSideKey names a row by its place in the model: the all-hosts row is 0,
// and a group is twice its index plus two, so rows keep their focus as groups
// open and close above them.
func (state *State) hostSideKey(i int) int {
	if i < 0 || i >= len(state.hostSideRows) {
		return 0
	}
	if group := state.hostSideRows[i]; group >= 0 {
		return 2*group + 2
	}
	return 0
}

func (state *State) hostSideNode(i int) ui.TreeNode {
	if i < 0 || i >= len(state.hostSideRows) {
		return ui.TreeNode{Height: hostSideRowHeight}
	}
	group := state.hostSideRows[i]
	if group < 0 {
		return ui.TreeNode{Height: hostSideRowHeight, Selected: !state.hostFilterOn}
	}
	g := state.hostGroups[group]
	return ui.TreeNode{
		Depth: int(g.Depth), Branch: state.hostGroupHasChildren(g.Name), Expanded: g.Expanded,
		Selected: state.hostFilterOn && state.hostFilter == g.Name, Height: hostSideRowHeight,
	}
}

func (state *State) hostSideLabel(i int) string {
	if i < 0 || i >= len(state.hostSideRows) {
		return ""
	}
	group := state.hostSideRows[i]
	if group < 0 {
		return state.messages.HostFilterAll
	}
	return state.hostGroupLabel(state.hostGroups[group])
}

func (state *State) hostSideDeclareRow(i int) {
	side := &state.hostSide
	c := side.c
	if c == nil {
		return
	}
	d := side.drawer(state, i)
	group := -1
	if i >= 0 && i < len(state.hostSideRows) {
		group = state.hostSideRows[i]
	}
	pal := state.pal
	dragging := false
	if from, _, ok := side.tree.DragSource(); ok && from == i {
		dragging = true
	}
	textColor, countColor, pinColor := pal.text, pal.faint, pal.accent
	if dragging {
		// The row stays where it is while the ghost carries its name.
		textColor.Alpha *= 0.5
		countColor.Alpha *= 0.5
		pinColor.Alpha *= 0.5
	}
	row := ui.Row(c).Fill().Height(hostSideRowHeight).PadSides(0, 0, 0, 12).Gap(6).Items(ui.Center).Draw(d.fn)
	row.Children(func() {
		if group < 0 {
			ui.Text(c, state.messages.HostFilterAll).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(textColor).Ellipsis().Shrink(1)
			ui.Text(c, state.hostTotalDigits).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(countColor).Shrink(0)
			return
		}
		g := state.hostGroups[group]
		if g.Pinned {
			ui.Icon(c, lucideIcons[iconStar]).Size(12, 12).Color(pinColor).Shrink(0)
		}
		label := g.Label
		if g.Name == "" {
			label = state.messages.HostGroupDefault
		}
		ui.Text(c, label).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(textColor).Ellipsis().Shrink(1)
		ui.Text(c, g.Digits).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(countColor).Shrink(0)
		ui.Spacer(c)
		state.declareIconButton(c, hitHostSideGroupMore.At(group), iconEllipsis, state.messages.HostGroupActions.Actions, dragging, iconButtonQuiet).Size(22, 22)
	})
}

func (side *hostCatalogSide) drawer(state *State, index int) *hostSideRowDrawer {
	if side.next >= len(side.drawers) {
		d := &hostSideRowDrawer{owner: state}
		d.fn = d.paint
		side.drawers = append(side.drawers, d)
	}
	d := side.drawers[side.next]
	side.next++
	d.index = index
	return d
}

func (state *State) recordHostSideRowRect(i int, rect gfx.Rect) {
	if i < 0 || i >= len(state.hostSideRows) {
		return
	}
	group := state.hostSideRows[i]
	if group >= 0 && group < len(state.hostSide.rowRects) {
		state.hostSide.rowRects[group] = rect
	}
}

// rebuildHostSideRows lists the groups the tree shows: the all-hosts row, then
// every group whose ancestors are open.
func (state *State) rebuildHostSideRows() {
	rows := state.hostSideRows[:0]
	rows = append(rows, -1)
	hidden := ""
	for index, group := range state.hostGroups {
		if hidden != "" && hostGroupWithin(group.Name, hidden) {
			continue
		}
		hidden = ""
		rows = append(rows, index)
		if !group.Expanded {
			hidden = group.Name
		}
	}
	state.hostSideRows = rows
}

func (state *State) hostSideGroup(i int) int {
	if i < 0 || i >= len(state.hostSideRows) {
		return -1
	}
	return state.hostSideRows[i]
}

func (state *State) hostSideCanDrop(from, to int, where ui.TreeDrop) bool {
	if state.hostSort.enabled || state.activeSection != sectionHosts || state.modal != modalNone {
		return false
	}
	source, target := state.hostSideGroup(from), state.hostSideGroup(to)
	if source < 0 || target < 0 {
		return false
	}
	switch where {
	case ui.TreeDropInside:
		return state.hostGroupMoveAllowed(source, target)
	case ui.TreeDropBefore, ui.TreeDropAfter:
		return state.hostGroupMoveBesideAllowed(source, target)
	}
	return false
}

// hostSideChanges applies what the user did to the group tree in this
// declaration: choosing a group filters the list, a chevron opens or closes
// it, and a dragged group moves under its new parent.
func (state *State) hostSideChanges(c ui.C) {
	side := &state.hostSide
	if i, ok := side.tree.Toggled(); ok {
		if group := state.hostSideGroup(i); group >= 0 {
			c.After(func() { state.toggleHostGroup(group) })
		}
	}
	if i, ok := side.tree.Chosen(); ok {
		group := state.hostSideGroup(i)
		if group < 0 {
			if state.hostFilterOn {
				c.After(func() { state.setHostFilter(false, "") })
			}
		} else {
			name := state.hostGroups[group].Name
			if !state.hostFilterOn || state.hostFilter != name {
				c.After(func() { state.setHostFilter(true, name) })
			}
		}
	}
	if move, ok := side.tree.Dropped(); ok {
		source, target := state.hostSideGroup(move.From), state.hostSideGroup(move.To)
		if source >= 0 && target >= 0 {
			path, parent := state.hostGroups[source].Name, state.hostGroups[target].Name
			if path != "" {
				switch move.Where {
				case ui.TreeDropInside:
					c.After(func() {
						if state.activeSection != sectionHosts || state.modal != modalNone {
							return
						}
						if !state.moveHostGroup(path, parent) {
							state.status, state.dialogError = state.dialogError, ""
						}
					})
				case ui.TreeDropBefore, ui.TreeDropAfter:
					after := move.Where == ui.TreeDropAfter
					c.After(func() {
						if state.activeSection != sectionHosts || state.modal != modalNone {
							return
						}
						sourceAt, targetAt := state.hostGroupIndex(path), state.hostGroupIndex(parent)
						if sourceAt < 0 || targetAt < 0 || !state.moveHostGroupBeside(sourceAt, targetAt, after) {
							state.status, state.dialogError = state.dialogError, ""
						}
					})
				}
			}
		}
	}
}

// declareHostsSide declares the group tree and its header inside the hosts
// page. The tree's rows are the page's, so its keys, wheel and ownership are
// the page's.
func (state *State) declareHostsSide(c ui.C, layout hostsLayout) {
	side := state.hostSideFns()
	side.visible = layout.sideVisible
	side.visibleWidth = layout.side.Width
	m, pal := state.messages, state.pal
	ui.Row(c).Absolute().Left(layout.sideHeader.X).Top(layout.sideHeader.Y).
		Width(layout.sideHeader.Width).Height(layout.sideHeader.Height).PadSides(0, 4, 0, 8).Gap(6).Items(ui.Center).Children(func() {
		ui.Text(c, m.HostCatalog.Groups).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(pal.muted).Ellipsis().Shrink(1)
		ui.Spacer(c)
		expand, collapse := state.hostGroupsExpansionActions()
		state.declareIconButton(c, hitHostGroupsExpandAll, iconChevronsExpand, m.HostGroupActions.ExpandAll, !expand, iconButtonQuiet).Size(24, 24)
		state.declareIconButton(c, hitHostGroupsCollapseAll, iconChevronsCollapse, m.HostGroupActions.CollapseAll, !collapse, iconButtonQuiet).Size(24, 24)
	})
	if cap(side.rowRects) < len(state.hostGroups) {
		side.rowRects = make([]gfx.Rect, len(state.hostGroups))
	} else {
		side.rowRects = side.rowRects[:len(state.hostGroups)]
		clear(side.rowRects)
	}
	side.next = 0
	side.c = c
	side.tree.RowHeight = hostSideRowHeight
	if state.hostSort.enabled {
		side.tree.CanDrop = nil
	} else {
		side.tree.CanDrop = side.canDrop
	}
	area := ui.Tree(c, &side.tree, len(state.hostSideRows), side.label, side.content).
		Absolute().Left(layout.sideTree.X).Top(layout.sideTree.Y).
		Width(layout.sideTree.Width).Height(layout.sideTree.Height).
		Scrollbars().ScrollbarSize(hostListBarWidth)
	side.c = nil
	side.area = area
	side.scroll = area.ScrollState()
	state.hostSideChanges(c)
}

// hostSideResizable is the divider between the group tree and the list. Its
// bounds are the two panes and the handle, in the window.
func (state *State) hostSideResizable(canvas *gfx.Canvas) components.Resizable {
	bounds := state.hostSide.splitBounds
	usable := max(1, bounds.Width-1)
	width := state.hostSide.visibleWidth
	if width <= 0 {
		width = state.hostSidePaneWidth()
	}
	fraction := width / usable
	maximum := min(hostSideMaxWidth, max(usable-hostListMinWidth, hostSideMinWidth)) / usable
	return components.BeginResizable(canvas, state.Theme(), hitHostSideWidth, bounds, fraction).
		Limits(hostSideMinWidth/usable, maximum).
		LimitsPx(hostSideMinWidth, hostListMinWidth).HandleSize(paneDividerHitWidth)
}

func (state *State) paintHostsSideSplit(canvas *gfx.Canvas) {
	state.hostSideResizable(canvas).Paint(state.messages.HostCatalog.ResizeGroups)
}

// hostSideGroupFromHit maps a pointer target to the group row it names: a row
// of the tree, its chevron, or the row's actions button.
func (state *State) hostSideGroupFromHit(id gfx.ID) (int, bool) {
	if id.IsZero() {
		return 0, false
	}
	if id.Scope() == hitHostSideGroupMore {
		group := item(id)
		if group >= 0 && group < len(state.hostGroups) {
			return group, true
		}
		return 0, false
	}
	for i := range state.hostSideRows {
		if id == state.hostSide.tree.RowID(i) || id == state.hostSide.tree.DisclosureID(i) {
			return state.hostSideRows[i], true
		}
	}
	return 0, false
}

// hostSidePointer drags the divider and opens a group's actions on a secondary
// press over a row of the tree.
func (state *State) hostSidePointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	side := &state.hostSide
	if state.activeSection != sectionHosts || state.modal != modalNone || !side.visible {
		return gfx.EventResult{}, false
	}
	if event.Kind == gfx.PointerDown && (event.Button == gfx.PointerButtonPrimary || event.Button == gfx.PointerButtonNone) {
		side.dragging = event.Target == hitHostSideWidth
	}
	if side.dragging {
		if bounds := side.splitBounds; bounds.Width > 1 {
			switch event.Kind {
			case gfx.PointerDown, gfx.PointerMove, gfx.PointerUp:
				side.width = state.hostSideResizable(nil).FractionAt(event.X) * (bounds.Width - 1)
			}
		}
		if event.Kind == gfx.PointerUp || event.Kind == gfx.PointerExit {
			side.dragging = false
			state.save()
		}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonSecondary {
		return gfx.EventResult{}, false
	}
	group, ok := state.hostSideGroupFromHit(event.Target)
	if !ok {
		return gfx.EventResult{}, false
	}
	if group >= 0 {
		state.openHostGroupActions(state.hostGroups[group].Name, gfx.Point{X: event.X, Y: event.Y})
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

// hostDragGhostSource is the name a drag is carrying and where the pointer is:
// the group tree's row first, then the list's.
func (state *State) hostDragGhostSource() (string, gfx.Point, bool) {
	side := &state.hostSide
	if side.visible && side.tree.Dragging() {
		if row, at, ok := side.tree.DragSource(); ok && row >= 0 && row < len(state.hostSideRows) {
			group := state.hostSideRows[row]
			if group < 0 {
				return state.messages.HostFilterAll, at, true
			}
			return state.hostGroupChipLabel(state.hostGroups[group]), at, true
		}
	}
	if state.hostView != hostViewGrid && state.hostList.tree.Dragging() {
		if row, at, ok := state.hostList.tree.DragSource(); ok && row >= 0 && row < len(state.hostTreeRows) {
			entry := state.hostTreeRows[row]
			if entry.Kind == hostTreeGroup {
				return state.hostGroupChipLabel(state.hostGroups[entry.GroupIndex]), at, true
			}
			return state.hosts[entry.HostIndex].Label, at, true
		}
	}
	return "", gfx.Point{}, false
}

// paintHostDragGhost is DBX's drag chip: the carried row's name on a raised
// surface beside the pointer, clamped inside the hosts page.
func (state *State) paintHostDragGhost(canvas *gfx.Canvas, bounds gfx.Rect) {
	label, at, ok := state.hostDragGhostSource()
	if !ok {
		return
	}
	maxText := hostDragGhostMaxWidth - 2*hostDragGhostPadX
	textWidth := canvas.TextWidth(label, hostDragGhostFontSize)
	if textWidth > maxText {
		label, textWidth = cutHostDragLabel(canvas, label, maxText)
	}
	width := textWidth + 2*hostDragGhostPadX
	rect := gfx.Rect{X: at.X + hostDragGhostOffsetX, Y: at.Y - hostDragGhostOffsetY, Width: width, Height: hostDragGhostHeight}
	rect.X = min(max(rect.X, bounds.X+8), max(bounds.X+bounds.Width-width-8, bounds.X+8))
	rect.Y = min(max(rect.Y, bounds.Y+8), max(bounds.Y+bounds.Height-hostDragGhostHeight-8, bounds.Y+8))
	canvas.Elevate(rect, hostDragGhostRadius, gfx.ShadowMD)
	canvas.RoundedBorder(rect, hostDragGhostRadius, 1, state.pal.line, state.pal.surface)
	canvas.Label(label, gfx.Rect{X: rect.X + hostDragGhostPadX, Y: rect.Y, Width: rect.Width - 2*hostDragGhostPadX, Height: hostDragGhostHeight},
		gfx.TextStyle{Size: hostDragGhostFontSize, Color: state.pal.text})
}

// cutHostDragLabel shortens label with an ellipsis until it fits maxWidth.
func cutHostDragLabel(canvas *gfx.Canvas, label string, maxWidth float32) (string, float32) {
	runes := []rune(label)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		cut := string(runes) + "…"
		if width := canvas.TextWidth(cut, hostDragGhostFontSize); width <= maxWidth {
			return cut, width
		}
	}
	const ellipsis = "…"
	return ellipsis, canvas.TextWidth(ellipsis, hostDragGhostFontSize)
}

// scrollHostSideTree moves the group tree with the wheel over its rows.
func (state *State) scrollHostSideTree(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	side := &state.hostSide
	if !side.visible || side.scroll == nil || !pointInRect(event.X, event.Y, side.bounds) {
		return gfx.EventResult{}, false
	}
	return side.scroll.Wheel(event), true
}
