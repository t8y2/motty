package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var (
	hostSideHits     = tree.Feature("host-side")
	hitHostSideWidth = tree.ID(hostSideHits, "width")
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
	hostSideRowHeight     = hostsPanelGroupRow
	hostSideHostRowHeight = hostsPanelHostRow
	hostSideIndent        = hostsPanelIndent
	hostSideGutter        = paneDividerHitWidth/2 + 1
	hostsListTopGap       = float32(8)
)

// hostSideRowKind says what one row of the group tree is: the all-hosts row,
// a group, or a host under its group.
type hostSideRowKind uint8

const (
	hostSideRowAll hostSideRowKind = iota
	hostSideRowGroup
	hostSideRowHost
)

type hostSideRow struct {
	Kind       hostSideRowKind
	GroupIndex int
	HostIndex  int
	Depth      uint8
}

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
	drawers  []*hostSideRowDrawer
	next     int
	// width is the width the user chose. visibleWidth is the width the layout
	// could give it, which a narrow window clamps below width; the divider is
	// painted and dragged at the visible one.
	width, visibleWidth float32
}

// hostSideRowDrawer paints a host row's badge and light into the slots its
// declaration leaves, in the same places the session manager puts them.
type hostSideRowDrawer struct {
	owner *State
	index int
	fn    func(*ui.DrawContext)
}

func (d *hostSideRowDrawer) paint(ctx *ui.DrawContext) {
	d.owner.paintHostSideRow(ctx, d.index)
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

// hostSideEntry is row i of the tree.
func (state *State) hostSideEntry(i int) (hostSideRow, bool) {
	if i < 0 || i >= len(state.hostSideRows) {
		return hostSideRow{}, false
	}
	return state.hostSideRows[i], true
}

// hostSideKey names a row by its place in the model: the all-hosts row is 0, a
// group is twice its index plus one and a host twice its index plus two, so
// rows keep their focus as groups open and close above them and the two kinds
// never share a key.
func (state *State) hostSideKey(i int) int {
	row, ok := state.hostSideEntry(i)
	if !ok {
		return 0
	}
	switch row.Kind {
	case hostSideRowGroup:
		return 2*row.GroupIndex + 1
	case hostSideRowHost:
		return 2*row.HostIndex + 2
	}
	return 0
}

func (state *State) hostSideNode(i int) ui.TreeNode {
	row, ok := state.hostSideEntry(i)
	if !ok {
		return ui.TreeNode{Height: hostSideRowHeight}
	}
	switch row.Kind {
	case hostSideRowGroup:
		g := state.hostGroups[row.GroupIndex]
		return ui.TreeNode{
			Depth: int(row.Depth), Branch: true,
			Expanded: g.Expanded || strings.TrimSpace(state.visibleQuery) != "",
			Selected: state.hostFilterOn && state.hostFilter == g.Name, Height: hostSideRowHeight,
		}
	case hostSideRowHost:
		return ui.TreeNode{
			Depth: int(row.Depth), Height: hostSideHostRowHeight,
			Selected: state.selectedHostID == state.hosts[row.HostIndex].ID,
		}
	}
	return ui.TreeNode{Height: hostSideRowHeight, Selected: !state.hostFilterOn}
}

func (state *State) hostSideLabel(i int) string {
	row, ok := state.hostSideEntry(i)
	if !ok {
		return ""
	}
	switch row.Kind {
	case hostSideRowGroup:
		return state.hostGroupLabel(state.hostGroups[row.GroupIndex])
	case hostSideRowHost:
		return state.hostPrivacySemanticLabel(state.hosts[row.HostIndex])
	}
	return state.messages.HostFilterAll
}

func (state *State) hostSideDeclareRow(i int) {
	side := &state.hostSide
	c := side.c
	if c == nil {
		return
	}
	d := side.drawer(state, i)
	row, ok := state.hostSideEntry(i)
	if !ok {
		return
	}
	pal := state.pal
	dragging := false
	if from, _, ok := side.tree.DragSource(); ok && from == i {
		dragging = true
	}
	textColor, countColor := pal.text, pal.faint
	if dragging {
		// The row stays where it is while the ghost carries its name.
		textColor.Alpha *= 0.5
		countColor.Alpha *= 0.5
	}
	height := hostSideRowHeight
	if row.Kind == hostSideRowHost {
		height = hostSideHostRowHeight
	}
	line := ui.Row(c).Fill().Height(height).Gap(6).Items(ui.Center)
	switch row.Kind {
	case hostSideRowAll:
		// The all-hosts row has no chevron or folder, so it keeps their gutter;
		// its name lines up with the groups below it.
		line = line.PadSides(0, 0, 0, 46)
	case hostSideRowHost:
		line = line.PadSides(0, 0, 0, 4).Gap(8).Draw(d.fn)
	}
	line.Children(func() {
		switch row.Kind {
		case hostSideRowAll:
			ui.Text(c, state.messages.HostFilterAll).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(textColor).Ellipsis().Shrink(1)
			ui.Text(c, state.hostTotalDigits).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(countColor).Shrink(0)
		case hostSideRowGroup:
			g := state.hostGroups[row.GroupIndex]
			label := g.Label
			if g.Name == "" {
				label = state.messages.HostGroupDefault
			}
			folder := lucideIcons[iconFolderSolid]
			if g.Expanded || strings.TrimSpace(state.visibleQuery) != "" {
				folder = lucideIcons[iconFolderSolidOpen]
			}
			ui.Icon(c, folder).Size(16, 16).Shrink(0)
			ui.Text(c, label).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(textColor).Ellipsis().Shrink(1)
			ui.Text(c, g.Digits).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(countColor).Shrink(0)
		case hostSideRowHost:
			host := state.hosts[row.HostIndex]
			ui.Slot(c).Size(hostsPanelBadge, hostsPanelBadge).Shrink(0)
			ui.Text(c, state.hostPrivacySemanticLabel(host)).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(textColor).Ellipsis().Shrink(1)
			ui.Spacer(c)
			ui.Slot(c).Size(hostsPanelLight, hostsPanelLight).Shrink(0)
		}
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

// paintHostSideRow paints a host row's badge and light at the two ends of the
// row's content, as the session manager does.
func (state *State) paintHostSideRow(ctx *ui.DrawContext, index int) {
	row, ok := state.hostSideEntry(index)
	if !ok || row.Kind != hostSideRowHost {
		return
	}
	host := state.hosts[row.HostIndex]
	room := ctx.Rect
	badge := gfx.Rect{X: room.X + 4, Y: room.Y + (room.Height-hostsPanelBadge)/2, Width: hostsPanelBadge, Height: hostsPanelBadge}
	state.paintHostCatalogIcon(ctx.Canvas, host, badge, state.pal.workspace)
	if kind, color := state.hostLight(host); kind != lightIdle {
		paintLight(ctx.Canvas, room.X+room.Width-hostsPanelLight/2, room.Y+room.Height/2, kind, color, state.pal.workspace)
	}
}

// rebuildHostSideRows lists the rows the tree shows: the all-hosts row, then
// every open group with the hosts and child groups under it, in the list's
// order. The tree shows the whole catalog, so the group filter does not narrow
// it; the search does, as it narrows the list.
func (state *State) rebuildHostSideRows() {
	rows := state.hostSideRows[:0]
	rows = append(rows, hostSideRow{Kind: hostSideRowAll})
	visible := make([]int, 0, len(state.hosts))
	for index := range state.hosts {
		visible = append(visible, index)
	}
	state.sortHostIndexes(visible)
	words := strings.Fields(strings.ToLower(state.visibleQuery))
	if len(words) > 0 {
		kept := visible[:0]
		for _, index := range visible {
			if matchesHost(state.hosts[index], words) {
				kept = append(kept, index)
			}
		}
		visible = kept
	}
	treeRows := state.appendHostTreeRows(nil, visible, words, false, false, "")
	for _, entry := range treeRows {
		kind := hostSideRowGroup
		if entry.Kind == hostTreeHost {
			kind = hostSideRowHost
		}
		rows = append(rows, hostSideRow{
			Kind: kind, GroupIndex: entry.GroupIndex, HostIndex: entry.HostIndex, Depth: entry.Depth,
		})
	}
	state.hostSideRows = rows
}

// hostSideGroup is the group a row names, or -1 for the all-hosts row and a
// host row.
func (state *State) hostSideGroup(i int) int {
	row, ok := state.hostSideEntry(i)
	if !ok || row.Kind != hostSideRowGroup {
		return -1
	}
	return row.GroupIndex
}

func (state *State) hostSideCanDrop(from, to int, where ui.TreeDrop) bool {
	if state.hostSort.enabled || state.activeSection != sectionHosts || state.modal != modalNone || from == to {
		return false
	}
	source, sourceOK := state.hostSideEntry(from)
	target, targetOK := state.hostSideEntry(to)
	if !sourceOK || !targetOK {
		return false
	}
	switch source.Kind {
	case hostSideRowGroup:
		if target.Kind != hostSideRowGroup {
			return false
		}
		switch where {
		case ui.TreeDropInside:
			return state.hostGroupMoveAllowed(source.GroupIndex, target.GroupIndex)
		case ui.TreeDropBefore, ui.TreeDropAfter:
			return state.hostGroupMoveBesideAllowed(source.GroupIndex, target.GroupIndex)
		}
	case hostSideRowHost:
		switch target.Kind {
		case hostSideRowGroup:
			return where == ui.TreeDropInside
		case hostSideRowHost:
			return where != ui.TreeDropInside
		}
	}
	return false
}

// hostSideChanges applies what the user did to the tree in this declaration:
// choosing a group opens or closes it, choosing a host selects it, and a
// dragged row moves where it was dropped.
func (state *State) hostSideChanges(c ui.C) {
	side := &state.hostSide
	if i, ok := side.tree.Toggled(); ok {
		if row, found := state.hostSideEntry(i); found && row.Kind == hostSideRowGroup {
			group := row.GroupIndex
			c.After(func() { state.toggleHostGroup(group) })
		}
	}
	if i, ok := side.tree.Chosen(); ok {
		if row, found := state.hostSideEntry(i); found {
			switch row.Kind {
			case hostSideRowAll:
				if state.hostFilterOn {
					c.After(func() { state.setHostFilter(false, "") })
				}
			case hostSideRowGroup:
				group := row.GroupIndex
				c.After(func() { state.toggleHostGroup(group) })
			case hostSideRowHost:
				host := state.hosts[row.HostIndex]
				c.After(func() {
					state.selectedHostID = host.ID
					if state.hostFilterOn && !state.inHostFilter(host) {
						state.setHostFilter(false, "")
					}
					state.hostRevealID = host.ID
				})
			}
		}
	}
	if move, ok := side.tree.Dropped(); ok {
		source, sourceOK := state.hostSideEntry(move.From)
		target, targetOK := state.hostSideEntry(move.To)
		if !sourceOK || !targetOK {
			return
		}
		switch source.Kind {
		case hostSideRowGroup:
			if target.Kind != hostSideRowGroup {
				return
			}
			path, parent := state.hostGroups[source.GroupIndex].Name, state.hostGroups[target.GroupIndex].Name
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
		case hostSideRowHost:
			hostID := state.hosts[source.HostIndex].ID
			switch target.Kind {
			case hostSideRowGroup:
				group := state.hostGroups[target.GroupIndex].Name
				c.After(func() {
					if state.activeSection != sectionHosts || state.modal != modalNone {
						return
					}
					state.moveHostToGroup(hostID, group)
				})
			case hostSideRowHost:
				targetID := state.hosts[target.HostIndex].ID
				after := move.Where == ui.TreeDropAfter
				c.After(func() {
					if state.activeSection != sectionHosts || state.modal != modalNone {
						return
					}
					state.reorderHostBeside(hostID, targetID, after)
				})
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

// hostSideRowFromHit maps a pointer target to the row it names: a row of the
// tree or its chevron.
func (state *State) hostSideRowFromHit(id gfx.ID) (hostSideRow, bool) {
	if id.IsZero() {
		return hostSideRow{}, false
	}
	for i := range state.hostSideRows {
		if id == state.hostSide.tree.RowID(i) || id == state.hostSide.tree.DisclosureID(i) {
			return state.hostSideRows[i], true
		}
	}
	return hostSideRow{}, false
}

// hostSidePointer drags the divider, connects a host on a double click, and
// opens a group's actions or a host's menu on a secondary press.
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
	if event.Kind != gfx.PointerDown {
		return gfx.EventResult{}, false
	}
	at := gfx.Point{X: event.X, Y: event.Y}
	row, rowOK := state.hostSideRowFromHit(event.Target)
	if event.Button == gfx.PointerButtonPrimary && event.ClickCount >= 2 {
		if !rowOK || row.Kind != hostSideRowHost {
			return gfx.EventResult{}, false
		}
		host := state.hosts[row.HostIndex]
		state.selectedHostID = host.ID
		if state.hostFilterOn && !state.inHostFilter(host) {
			state.setHostFilter(false, "")
		}
		state.newHostSession(host)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if event.Button != gfx.PointerButtonSecondary {
		return gfx.EventResult{}, false
	}
	if !rowOK {
		// A press on the tree's own ground starts a catalog at the top level;
		// a menu's backdrop carries its own target and stays with it.
		if event.Target.IsZero() {
			if area := side.area.Rect(); pointInRect(event.X, event.Y, area) {
				state.openHostBlankMenu(at)
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
		}
		return gfx.EventResult{}, false
	}
	switch row.Kind {
	case hostSideRowGroup:
		state.openHostGroupActions(state.hostGroups[row.GroupIndex].Name, at)
	case hostSideRowHost:
		host := state.hosts[row.HostIndex]
		state.selectedHostID = host.ID
		state.hostBlankMenu.open = false
		state.hostMenu = hostMenu{open: true, host: host.ID, checked: state.hostSelected(host.ID), at: at}
	case hostSideRowAll:
		return gfx.EventResult{}, false
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

// hostDragGhostSource is the name a drag is carrying and where the pointer is:
// the group tree's row first, then the list's.
func (state *State) hostDragGhostSource() (string, gfx.Point, bool) {
	side := &state.hostSide
	if side.visible && side.tree.Dragging() {
		if row, at, ok := side.tree.DragSource(); ok {
			if entry, found := state.hostSideEntry(row); found {
				switch entry.Kind {
				case hostSideRowGroup:
					return state.hostGroupChipLabel(state.hostGroups[entry.GroupIndex]), at, true
				case hostSideRowHost:
					return state.hostPrivacySemanticLabel(state.hosts[entry.HostIndex]), at, true
				}
				return state.messages.HostFilterAll, at, true
			}
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
