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
// owned by TreeState. The host cards stay outside it; only the list view uses it.
//
// The functions the Tree keeps are made once here, not in each paint: a method
// value made in a paint allocates, and the Tree asks Node for every row.
type hostCatalogList struct {
	tree    ui.TreeState
	page    *ui.Page
	view    func(ui.C)
	node    func(int) ui.TreeNode
	label   func(int) string
	content func(int)
	key     func(int) int
	canDrop func(int, int, ui.TreeDrop) bool

	// c is the paint being declared while the Tree asks content for its rows:
	// ui.Tree calls content during the declaration it is part of.
	c ui.C
	// scroll is the Tree's scroll area as of the last list paint. It is kept
	// only to apply an offset another part of the page asked for (the sort
	// drag's edge scroll) before the Tree lays its rows out again.
	scroll *ui.ScrollState
	// drawers are reused row painters, one per row the Tree declares in a
	// paint. They carry their own row index, so the Draw of each row reads the
	// right row after the whole tree has been declared.
	drawers []*hostRowDrawer
	next    int
}

// hostRowDrawer paints one row of the hosts list: the Draw the Tree keeps for
// that row until the frame ends.
type hostRowDrawer struct {
	owner *State
	index int
	fn    func(*ui.DrawContext)
}

func (d *hostRowDrawer) paint(ctx *ui.DrawContext) { d.owner.paintHostListRow(ctx, d.index) }

// ensureHostList makes the list's page and the functions the Tree keeps. The
// page names its rows in the workbench's own identity tree, under a root of its
// own, as the dialogs of the window do.
func (state *State) ensureHostList() *hostCatalogList {
	list := &state.hostList
	if list.page != nil {
		return list
	}
	list.node = state.hostListNode
	list.label = state.hostListLabel
	list.key = state.hostListKey
	list.canDrop = state.hostListCanDrop
	list.content = state.hostListDeclareRow
	list.view = state.declareHostList
	list.tree.Node = list.node
	list.tree.Key = list.key
	list.tree.CanDrop = list.canDrop
	list.tree.Indent = hostTreeIndent
	list.page = ui.NewPage(ui.PageConfig{Tree: &tree, Root: "host-list", Theme: &state.theme})
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
	return state.hosts[entry.HostIndex].Label
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

// hostListDeclareRow declares what is in row i: a slot the row painter fills.
// The painter is reused, so a paint allocates no closure per row.
func (state *State) hostListDeclareRow(i int) {
	c := state.hostList.c
	if c == nil {
		return
	}
	d := state.hostList.drawer(state, i)
	ui.Slot(c).Fill().Draw(d.fn)
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

// hostListHitKey says which row of the list a hit names, if it is one: the Tree
// names its rows by hostListKey in the list page, and a control drawn in a row
// (a checkbox, a button) is named in the workbench's tree instead.
func (state *State) hostListHitKey(id gfx.ID) (int, bool) {
	list := &state.hostList
	if list.page == nil || state.hostView != hostViewList || !list.page.Owns(id) {
		return 0, false
	}
	return id.Name().Index()
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
// list's rows are named by the Tree, so the target is read from the hits rather
// than named by the workbench.
func (state *State) focusHostListRow(canvas *gfx.Canvas, hostID uint32) {
	row := state.hostListRowIndex(hostID)
	if row >= 0 {
		key := state.hostListKey(row)
		for _, hit := range canvas.Hits() {
			if name, ok := state.hostListHitKey(hit.ID); ok && name == key {
				if state.hostSort.enabled {
					canvas.RequestFocus(hitHostSortHandles.At(int(hostID)))
				} else {
					canvas.RequestFocus(hit.ID)
				}
				return
			}
		}
	}
	if state.hostSort.enabled {
		canvas.RequestFocus(hitHostSortHandles.At(int(hostID)))
	}
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

// declareHostList declares the hosts list. The Tree reports what the user did;
// the changes that rearrange the rows run after the paint that declared them.
func (state *State) declareHostList(c ui.C) {
	list := &state.hostList
	rows := state.hostTreeRows
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
	area := ui.Tree(c, &list.tree, len(rows), list.label, list.content).Fill()
	list.c = nil
	list.scroll = area.ScrollState()

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
