package workbench

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// hostTreeIndent is the step of one level of the hosts tree, as the list has
// always indented it.
const hostTreeIndent = float32(22)

// hostCatalogList is the hosts list as a framework Tree: the visible rows of the
// group hierarchy, with the row heights, geometry, scrolling and drag and drop
// owned by TreeState. It is declared inside the workbench page, like the terminal
// panes and the docked file panel, so its keys, wheel and ownership are the
// page's. The host cards stay outside it; only the list view uses it.
//
// The functions the Tree keeps are made once here, not in each paint: a method
// value made in a paint allocates, and the Tree asks Node for every row.
type hostCatalogList struct {
	tree    ui.TreeState
	node    func(int) ui.TreeNode
	label   func(int) string
	content func(int)
	key     func(int) int
	canDrop func(int, int, ui.TreeDrop) bool
	// sortUnder and sortOver start and end the hand-drawn reorder overlay
	// around the rows the Tree paints. They are kept, not made in each paint.
	sortUnder, sortOver func(*ui.DrawContext)

	// c is the declaration being used while the Tree asks content for its rows:
	// ui.Tree calls content during the declaration it is part of.
	c ui.C
	// header and area are the rooms the page declared: the header painted by
	// hand, and the Tree that paints the rows. Their rectangles are read after
	// the page is laid out.
	header ui.Box
	area   ui.Box
	// scroll is the Tree's scroll area as of the last list paint; scrollLive says
	// that paint declared it, and pendingLive is that answer for the paint being
	// made, which clears it. They apply an offset another part of the page asked
	// for (the sort drag's edge scroll, or a test) before the Tree lays its rows
	// out again, and only while the state is still the list's.
	scroll      *ui.ScrollState
	scrollLive  bool
	pendingLive bool
	// applied is the offset the last list paint gave the Tree and the window. A
	// hostsScroll that differs from it is a request from another part of the
	// page (a reset, the sort drag's edge scroll, a test); one that matches is
	// just the last paint's value, so a wheel the page handled is not undone.
	applied float32
	// over says the pointer is on the list, which reveals its bar. The bar keeps
	// its reserved strip either way, so the rows never move under it.
	over    bool
	barDrag scrollBarDrag
	// bounds is the room the list occupies in the window, including the strip the
	// scroll bar takes, and viewport the part the rows are painted in, both as of
	// the last paint.
	bounds   gfx.Rect
	viewport gfx.Rect
	// rowWidth is the width the Tree gives each row, as the declaration worked
	// it out from the bar's reserved strip; the row content is laid out to it.
	rowWidth float32
	// drawers are reused row painters, one per row the Tree declares in a
	// paint. They carry their own row index, so the Draw of each row reads the
	// right row after the whole tree has been declared.
	drawers []*hostRowDrawer
	next    int
}

// hostRowDrawer paints one row of the hosts list: the Draw the Tree keeps for
// that row until the frame ends. The boxes are the slots the row's content
// declaration left for the controls painted by hand.
type hostRowDrawer struct {
	owner      *State
	index      int
	fn         func(*ui.DrawContext)
	avatar     ui.Box
	light      ui.Box
	grip       ui.Box
	check      ui.Box
	name       ui.Box
	subtitle   ui.Box
	address    ui.Box
	groupCheck ui.Box
}

func (d *hostRowDrawer) paint(ctx *ui.DrawContext) { d.owner.paintHostListRow(ctx, d) }

// hostListFns makes the functions the Tree keeps. The Tree itself is declared in
// the workbench page, in declareHostsWorkspace.
func (state *State) hostListFns() *hostCatalogList {
	list := &state.hostList
	if list.node != nil {
		return list
	}
	list.node = state.hostListNode
	list.label = state.hostListLabel
	list.key = state.hostListKey
	list.canDrop = state.hostListCanDrop
	list.content = state.hostListDeclareRow
	list.sortUnder, list.sortOver = state.drawHostSortUnder, state.drawHostSortOver
	list.tree.Node = list.node
	list.tree.Key = list.key
	list.tree.CanDrop = list.canDrop
	list.tree.Indent = hostTreeIndent
	return list
}

// hostListKey names a visible row of the hosts tree by its place in the model,
// so the Tree keeps a row's focus as rows above it open and close. Groups are
// odd and hosts even: the two ranges never meet, and the key says which kind a
// row is when a pointer event names it.
func (state *State) hostListKey(i int) int {
	entry := state.hostTreeRows[i]
	if entry.Kind == hostTreeGroup {
		return 2*entry.GroupIndex + 1
	}
	return 2 * entry.HostIndex
}

// hostListNode describes row i for the Tree. Each row carries its own height:
// group rows are compact and host rows full, and the list measures each of them
// and reports the result through RowHeightAt.
func (state *State) hostListNode(i int) ui.TreeNode {
	entry := state.hostTreeRows[i]
	if entry.Kind == hostTreeGroup {
		group := state.hostGroups[entry.GroupIndex]
		return ui.TreeNode{
			Depth: int(entry.Depth), Branch: true, Height: hostGroupRowHeight,
			Expanded: group.Expanded || strings.TrimSpace(state.visibleQuery) != "",
		}
	}
	host := state.hosts[entry.HostIndex]
	return ui.TreeNode{Depth: int(entry.Depth), Height: hostRowHeight, Selected: host.ID == state.selectedHostID}
}

// hostListLabel names row i for assistive technology and for typing ahead.
func (state *State) hostListLabel(i int) string {
	entry := state.hostTreeRows[i]
	if entry.Kind == hostTreeGroup {
		return state.hostGroupLabel(state.hostGroups[entry.GroupIndex])
	}
	return state.hostPrivacySemanticLabel(state.hosts[entry.HostIndex])
}

// hostListCanDrop is the Tree's acceptance of a drop: a group goes into another
// group, never into itself or what is inside it, and never beside a group of its
// own name. A row can only be dropped onto a group, so before and after fall
// back to inside and any point of a group row takes the same move it always did.
func (state *State) hostListCanDrop(from, to int, where ui.TreeDrop) bool {
	if where != ui.TreeDropInside || state.hostSort.enabled ||
		state.activeSection != sectionHosts || state.modal != modalNone {
		return false
	}
	rows := state.hostTreeRows
	if from < 0 || from >= len(rows) || to < 0 || to >= len(rows) {
		return false
	}
	source, target := rows[from], rows[to]
	if source.Kind != hostTreeGroup || target.Kind != hostTreeGroup {
		return false
	}
	return state.hostGroupMoveAllowed(source.GroupIndex, target.GroupIndex)
}

// hostGroupMoveAllowed says whether group source may move under group parent:
// not into itself or what is inside it, and not beside a group of its own name.
func (state *State) hostGroupMoveAllowed(source, parent int) bool {
	if source < 0 || source >= len(state.hostGroups) || parent < 0 || parent >= len(state.hostGroups) {
		return false
	}
	path := state.hostGroups[source].Name
	if path == "" { // the unnamed bucket is not a group to move
		return false
	}
	target := state.hostGroups[parent].Name
	if hostGroupWithin(target, path) {
		return false
	}
	moved := joinHostGroup(target, hostGroupLeaf(path))
	for _, group := range state.hostGroups {
		if strings.EqualFold(group.Name, moved) && group.Name != path {
			return false
		}
	}
	return true
}

// hostListDeclareRow declares what is in row i: the row's content is part of
// the page, and the Draw paints the ground and the controls into the slots its
// declaration left. The painter is reused, so a paint allocates no closure per
// row.
func (state *State) hostListDeclareRow(i int) {
	c := state.hostList.c
	if c == nil {
		return
	}
	d := state.hostList.drawer(state, i)
	d.avatar, d.light, d.grip, d.check, d.name, d.subtitle, d.address, d.groupCheck = ui.Box{}, ui.Box{}, ui.Box{}, ui.Box{}, ui.Box{}, ui.Box{}, ui.Box{}, ui.Box{}
	ui.Slot(c).Fill().Draw(d.fn).Children(func() { state.declareHostListRowContent(c, d) })
}

// drawer returns a painter for the next row the Tree declares. The index of the
// next declared row is assigned by hostListDeclareRow; the pool grows only until
// it covers the rows on screen.
func (list *hostCatalogList) drawer(state *State, index int) *hostRowDrawer {
	if list.next >= len(list.drawers) {
		d := &hostRowDrawer{owner: state}
		d.fn = d.paint
		list.drawers = append(list.drawers, d)
	}
	d := list.drawers[list.next]
	list.next++
	d.index = index
	return d
}

// hostListHitKey says which row of the list a hit names, if it is one. A row's
// key reaches the row the Tree declared it for, and the hit must be that row's
// own identity or its disclosure's: the controls painted inside a row (a
// checkbox, a button) are named in the workbench's tree instead.
func (state *State) hostListHitKey(id gfx.ID) (int, bool) {
	if state.hostView != hostViewList || id.IsZero() {
		return 0, false
	}
	key, ok := id.Name().Index()
	if !ok {
		return 0, false
	}
	row, ok := state.hostListRowForKey(key)
	if !ok {
		return 0, false
	}
	list := &state.hostList
	if id != list.tree.RowID(row) && id != list.tree.DisclosureID(row) {
		return 0, false
	}
	return key, true
}

// hostListRowForKey finds the visible row the list names key, if it shows it.
func (state *State) hostListRowForKey(key int) (int, bool) {
	for row := range state.hostTreeRows {
		if state.hostListKey(row) == key {
			return row, true
		}
	}
	return 0, false
}

// hostListRowFromHit maps a pointer target back to the visible row it names.
func (state *State) hostListRowFromHit(id gfx.ID) (hostTreeRow, bool) {
	key, ok := state.hostListHitKey(id)
	if !ok {
		return hostTreeRow{}, false
	}
	if key&1 == 1 {
		index := (key - 1) / 2
		if index >= 0 && index < len(state.hostGroups) {
			return hostTreeRow{Kind: hostTreeGroup, GroupIndex: index, HostIndex: -1}, true
		}
		return hostTreeRow{}, false
	}
	index := key / 2
	if index >= 0 && index < len(state.hosts) {
		return hostTreeRow{Kind: hostTreeHost, GroupIndex: -1, HostIndex: index}, true
	}
	return hostTreeRow{}, false
}

// hostListRowIndex finds the visible row of host, or -1 when a group holding it
// is closed.
func (state *State) hostListRowIndex(hostID uint32) int {
	for i, entry := range state.hostTreeRows {
		if entry.Kind == hostTreeHost && state.hosts[entry.HostIndex].ID == hostID {
			return i
		}
	}
	return -1
}

// focusHostListRow focuses the row of host, which the paint just declared. The
// Tree names the row's identity, so the focus goes to that ID; sorting by hand
// keeps its grip as the focus instead.
func (state *State) focusHostListRow(canvas *gfx.Canvas, hostID uint32) {
	if state.hostSort.enabled {
		canvas.RequestFocus(hitHostSortHandles.At(int(hostID)))
		return
	}
	if row := state.hostListRowIndex(hostID); row >= 0 {
		canvas.RequestFocus(state.hostList.tree.RowID(row))
	}
}

// hostListBarRevealed reports whether the hosts list's bar is shown: while the
// pointer is on the list or a press drags the bar.
func (state *State) hostListBarRevealed() bool {
	return state.hostList.over || state.hostList.barDrag.dragging
}

// hostListBarPointer drags the hosts list's scrollbar.
func (state *State) hostListBarPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	list := &state.hostList
	if state.modal != modalNone || !state.paintsHostsPage() {
		return gfx.EventResult{}, false
	}
	_, geometry := hostListScrollArea(nil, state.Theme(), list.bounds, state.hostsContent, state.hostsScroll, state.hostListBarRevealed())
	return list.barDrag.pointer(event, geometry, hitHostListBar, gfx.ID{}, &state.hostsScroll)
}

// trackHostListBar records whether the pointer is over the hosts list, which
// reveals its bar. A dialog owns the pointer while it is open, so the bar hides.
func (state *State) trackHostListBar(event gfx.PointerEvent) bool {
	list := &state.hostList
	if event.Captured || state.modal != modalNone || !state.paintsHostsPage() {
		changed := list.over
		list.over = false
		return changed
	}
	over := event.Kind != gfx.PointerExit && pointInRect(event.X, event.Y, list.bounds)
	changed := list.over != over
	list.over = over
	return changed
}

// hostListPointer opens the group actions or the host menu on a secondary press
// on a row of the list, and connects the host on a double click. The Tree names
// its rows itself, so the row is read from the pointer's target instead of a
// workbench hit declared over it.
func (state *State) hostListPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if state.activeSection != sectionHosts || state.modal != modalNone || state.hostSort.enabled {
		return gfx.EventResult{}, false
	}
	if event.Kind != gfx.PointerDown {
		return gfx.EventResult{}, false
	}
	if event.Button == gfx.PointerButtonPrimary && event.ClickCount >= 2 {
		if host, ok := state.hostForRow(event.Target); ok {
			state.selectedHostID = host.ID
			state.newHostSession(host)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		return gfx.EventResult{}, false
	}
	if event.Button != gfx.PointerButtonSecondary {
		return gfx.EventResult{}, false
	}
	entry, ok := state.hostListRowFromHit(event.Target)
	if !ok {
		// The cards and the other pages name their rows with hitHostList; the
		// workbench maps those to the host as it always did.
		host, hostOK := state.hostForRow(event.Target)
		if !hostOK {
			return gfx.EventResult{}, false
		}
		state.selectedHostID = host.ID
		state.hostMenu = hostMenu{open: true, host: host.ID, checked: state.hostSelected(host.ID), at: gfx.Point{X: event.X, Y: event.Y}}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	switch entry.Kind {
	case hostTreeGroup:
		state.openHostGroupActions(state.hostGroups[entry.GroupIndex].Name, gfx.Point{X: event.X, Y: event.Y})
	case hostTreeHost:
		host := state.hosts[entry.HostIndex]
		state.selectedHostID = host.ID
		state.hostMenu = hostMenu{open: true, host: host.ID, checked: state.hostSelected(host.ID), at: gfx.Point{X: event.X, Y: event.Y}}
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

// declareHostsWorkspace declares the hosts page inside the workbench page: the
// header the window paints by hand, and, in the list view, the Tree that paints
// the rows. Both are placed by the workspace slot's edges, so their geometry is
// the slot's and not a copy of it.
func (state *State) declareHostsWorkspace(c ui.C) {
	list := state.hostListFns()
	if cap(state.groupActions.rows) < len(state.hostGroups) {
		state.groupActions.rows = make([]gfx.Rect, len(state.hostGroups))
	} else {
		state.groupActions.rows = state.groupActions.rows[:len(state.hostGroups)]
		clear(state.groupActions.rows)
	}
	width, height := state.hostsWorkspaceSize()
	headerHeight := state.hostHeaderHeight(width, len(state.hostSelections) > 0)
	list.header = ui.Slot(c).Absolute().Left(0).Right(0).Top(0).Height(headerHeight)
	list.header.Children(func() {
		state.declareHostsHeader(c, gfx.Rect{Width: width, Height: headerHeight})
	})
	if len(state.hostTreeRows) == 0 {
		// The page owns the empty state too: the window paints only its icon and
		// action after the page has laid them out.
		m, bounds := state.messages, gfx.Rect{Width: width, Height: height}
		switch {
		case len(state.hosts) == 0:
			state.declareEmptyPageRegion(c, bounds, m.HostsEmpty, m.HostsEmptySub, m.NewHost, state.pal.muted, hitAddHostEmpty, iconHosts, true)
		case len(state.visible) == 0:
			state.declareEmptyPageRegion(c, bounds, m.NoMatches, m.NoMatchesSub, "", state.pal.muted, gfx.ID{}, 0, false)
		}
	}
	if state.hostView == hostViewGrid && len(state.hostTreeRows) > 0 {
		state.declareHostGrid(c, width, height, headerHeight)
		list.scrollLive = false
		return
	}
	if len(state.hostTreeRows) == 0 {
		list.scrollLive = false
		return
	}
	// The bar keeps its strip out of the rows when the content overflows; the
	// viewport is the rest of the list's room.
	room := gfx.Rect{Y: headerHeight, Width: max(width-36, 0), Height: max(height-headerHeight-18, 0)}
	content := state.hostListHeight(room.Width)
	_, geometry := hostListScrollArea(nil, state.Theme(), room, content, state.hostsScroll, state.hostListBarRevealed())
	right := float32(18) + room.Width - geometry.Viewport.Width
	list.rowWidth = geometry.Viewport.Width
	state.applyHostListScroll(c.Canvas(), content)
	list.tree.RowHeight = hostRowHeight
	// The rows are not draggable while they are being sorted by hand: the sort
	// overlay owns the pointer then.
	if state.hostSort.enabled {
		list.tree.CanDrop = nil
	} else {
		list.tree.CanDrop = list.canDrop
	}
	list.next = 0
	list.c = c
	area := ui.Tree(c, &list.tree, len(state.hostTreeRows), list.label, list.content).
		Absolute().Left(18).Right(right).Top(headerHeight).Bottom(18)
	list.c = nil
	// The sort overlay is not a page element: it starts before the rows paint
	// their handles and ends after them, as the list has always painted it.
	area.Draw(list.sortUnder).DrawOver(list.sortOver)
	list.area = area
	list.scroll = area.ScrollState()
	list.scrollLive = true
	state.hostListChanges(c, list)
}

// drawHostSortUnder starts the reorder overlay in the Tree's room, and
// drawHostSortOver ends it once the rows have declared their handles.
func (state *State) drawHostSortUnder(d *ui.DrawContext) {
	state.beginHostSortPaint(d.Canvas, d.Rect)
}

func (state *State) drawHostSortOver(*ui.DrawContext) {
	state.endHostSortPaint()
}

// beginHostsPaint remembers whether the last paint declared the list's scroll
// area, so this paint may apply a requested offset to it; a paint that did not
// declare the list drops the state, and its slot may be another area's now.
func (state *State) beginHostsPaint() {
	list := &state.hostList
	list.pendingLive = list.scrollLive
	list.scrollLive = false
}

// hostsWorkspaceSize is the room the workspace slot gives the hosts page: the
// window less the bar, the docks and the navigation rail, and the card's margins
// and border. declareShell lays the slot out from the same state; the size here
// only decides the header's rows and the bar's strip.
func (state *State) hostsWorkspaceSize() (width, height float32) {
	width = state.viewport.Width - state.toolbarDockSideReserveFor(true, false) - navigationWidth - state.chrome.shellCardRight - 2*workspaceBorder
	height = state.viewport.Height - windowBarHeight - state.toolbarDockBottomReserve(false) - sectionCardMargin - 2*workspaceBorder
	return max(width, 0), max(height, 0)
}

// hostHeaderHeight is the height of the toolbar, the filters and, while hosts are
// checked, the batch actions: the rows the header lays out below its top.
func (state *State) hostHeaderHeight(width float32, anySelected bool) float32 {
	height := float32(hostListTop)
	if width < 450 {
		height += hostHeaderRowHeight + hostHeaderRowGap
	}
	if anySelected {
		height += hostHeaderRowHeight + hostHeaderRowGap
	}
	return height
}

// applyHostListScroll applies an offset another part of the page asked for, and
// a row to reveal, to the Tree's scroll state before the Tree lays its rows out
// again. scrollLive says the state is the one the last paint declared.
func (state *State) applyHostListScroll(canvas *gfx.Canvas, content float32) {
	list := &state.hostList
	live := list.pendingLive
	list.pendingLive = false
	if live && list.scroll != nil && state.hostsScroll != list.applied {
		list.scroll.ScrollTo(state.hostsScroll)
	}
	if state.hostRevealID != 0 {
		if row := state.hostListRowIndex(state.hostRevealID); row >= 0 {
			list.tree.ScrollIntoView(row)
		}
	}
	if live && list.scroll != nil && state.hostSortAutoScrollFrame(canvas, list.viewport, content) {
		list.scroll.ScrollTo(state.hostsScroll)
	}
}

// hostListChanges applies what the user did to the Tree in this declaration: the
// changes that rearrange the rows run after the paint that declared them.
func (state *State) hostListChanges(c ui.C, list *hostCatalogList) {
	rows := state.hostTreeRows
	if i, ok := list.tree.Toggled(); ok && i >= 0 && i < len(rows) && rows[i].Kind == hostTreeGroup {
		group := rows[i].GroupIndex
		c.After(func() { state.toggleHostGroup(group) })
	}
	if i, ok := list.tree.Chosen(); ok && i >= 0 && i < len(rows) {
		entry := rows[i]
		if entry.Kind == hostTreeGroup {
			group := entry.GroupIndex
			c.After(func() { state.toggleHostGroup(group) })
		} else {
			host := state.hosts[entry.HostIndex]
			state.selectedHostID = host.ID
			state.status = state.messages.StatusSelected + host.Label
		}
	}
	if move, ok := list.tree.Dropped(); ok && move.From >= 0 && move.From < len(rows) && move.To >= 0 && move.To < len(rows) {
		source, target := rows[move.From], rows[move.To]
		if source.Kind == hostTreeGroup && target.Kind == hostTreeGroup {
			path := state.hostGroups[source.GroupIndex].Name
			parent := state.hostGroups[target.GroupIndex].Name
			if path != "" {
				c.After(func() {
					if state.activeSection != sectionHosts || state.modal != modalNone {
						return
					}
					if !state.moveHostGroup(path, parent) {
						state.status, state.dialogError = state.dialogError, ""
					}
				})
			}
		}
	}
}
