package workbench

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// hostsPanelHits is the place of the controls the session manager declares.
var hostsPanelHits = tree.Feature("hosts-panel")

var (
	hitHostsPanel         = tree.ID(hostsPanelHits, "toggle")
	hitHostsPanelClose    = tree.ID(hostsPanelHits, "close")
	hitHostsPanelNew      = tree.ID(hostsPanelHits, "new-host")
	hitHostsPanelManage   = tree.ID(hostsPanelHits, "manage")
	hitHostsPanelSearch   = tree.ID(hostsPanelHits, "search")
	hitHostsPanelDivider  = tree.ID(hostsPanelHits, "divider")
	hitHostsPanelExpand   = tree.ID(hostsPanelHits, "expand-all")
	hitHostsPanelCollapse = tree.ID(hostsPanelHits, "collapse-all")
)

// The session manager is the saved hosts and their groups as a tree beside the
// terminals, so a host can be opened without leaving the terminal for the hosts
// page. It lives in the window, not in a workspace: the catalog is the same
// whichever tab is on show.
const (
	hostsPanelDefaultWidth = float32(240)
	hostsPanelMinWidth     = float32(180)
	hostsPanelMaxWidth     = float32(420)
	// hostsPanelMaxShare is the most of the content area the panel may take, so
	// the terminals keep their room however wide the user dragged it.
	hostsPanelMaxShare = float32(0.4)
	// hostsPanelMinFrame is the content width below which the panel is not
	// shown, as a window too narrow for it keeps its room for the terminals.
	hostsPanelMinFrame = float32(760)
	hostsPanelHeader   = float32(40)
	hostsPanelSearch   = float32(36)
	hostsPanelGroupRow = float32(30)
	hostsPanelHostRow  = float32(32)
	hostsPanelIndent   = float32(20)
	hostsPanelBadge    = float32(16)
	hostsPanelLight    = float32(10)
	// hostsPanelEdge is the strip the divider takes beside the panel's own line.
	hostsPanelEdge = float32(6)
)

// hostsPanel is the session manager's state. The rows are the group hierarchy
// the hosts page lists, filtered by the panel's own search box and not by the
// page's, and rebuilt when the catalog or the search changes.
type hostsPanel struct {
	open     bool
	width    float32
	resizing bool
	// selected is the host row last chosen; it is the panel's own, as the hosts
	// page keeps its selection for its own list.
	selected uint32
	search   ui.Field
	// body is the room below the header and search box; bounds is the whole
	// panel, where a secondary press off a row offers the catalog's top-level
	// actions.
	body   gfx.Rect
	bounds gfx.Rect
	// stale says the rows no longer follow the catalog or the search.
	stale     bool
	filtering bool
	hosts     []int
	rows      []hostTreeRow

	tree    ui.TreeState
	node    func(int) ui.TreeNode
	label   func(int) string
	key     func(int) int
	content func(int)
	canDrop func(int, int, ui.TreeDrop) bool
	// c is the declaration in progress while the Tree asks for its rows.
	c       ui.C
	drawers []*hostsPanelRowDrawer
	next    int
}

// hostsPanelRowDrawer paints a host row's badge and light into the slots the
// row's declaration leaves for them. It is made once per row on screen and keeps
// its own index, so a paint makes no closure per row.
type hostsPanelRowDrawer struct {
	owner *State
	index int
	fn    func(*ui.DrawContext)
}

func (d *hostsPanelRowDrawer) paint(ctx *ui.DrawContext) { d.owner.paintHostsPanelRow(ctx, d.index) }

// hostsPanelExtent is the width the panel takes from a content area of the
// given width, its edge included; zero when it is closed or the area is too
// narrow to share.
func (state *State) hostsPanelExtent(frameWidth float32) float32 {
	panel := &state.hostsPanel
	if !panel.open || frameWidth < hostsPanelMinFrame {
		return 0
	}
	return state.hostsPanelWidth(frameWidth) + 1
}

// hostsPanelWidth is the panel's width inside a content area of the given width.
func (state *State) hostsPanelWidth(frameWidth float32) float32 {
	width := state.hostsPanel.width
	if width <= 0 {
		width = hostsPanelDefaultWidth
	}
	return min(max(width, hostsPanelMinWidth), min(hostsPanelMaxWidth, frameWidth*hostsPanelMaxShare))
}

// hostsPanelVisible reports whether the window shows the panel now.
func (state *State) hostsPanelVisible() bool {
	return state.terminalPage() && state.hostsPanelExtent(state.toolbarDockFrame(state.viewport, true).Width) > 0
}

func (state *State) toggleHostsPanel() {
	panel := &state.hostsPanel
	panel.open = !panel.open
	panel.resizing = false
	panel.stale = true
	state.refitForHostsPanel()
	state.save()
}

// refitForHostsPanel gives the terminals the room the panel left them: every
// workspace shares the window's content area, so all of them change.
func (state *State) refitForHostsPanel() {
	state.fitTerminals()
	state.fitSFTP(state.viewport)
}

func (state *State) clickHostsPanel(id gfx.ID) bool {
	if id != hitHostsPanel {
		return false
	}
	state.toggleHostsPanel()
	return true
}

// hostsPanelFns makes the functions the Tree keeps: a method value made in a
// paint allocates, and the Tree asks for each row.
func (state *State) hostsPanelFns() *hostsPanel {
	panel := &state.hostsPanel
	if panel.node != nil {
		return panel
	}
	panel.node = state.hostsPanelNode
	panel.label = state.hostsPanelLabel
	panel.key = state.hostsPanelKey
	panel.content = state.hostsPanelDeclareRow
	panel.canDrop = state.hostsPanelCanDrop
	panel.tree.Node = panel.node
	panel.tree.Key = panel.key
	panel.tree.CanDrop = panel.canDrop
	panel.tree.Indent = hostsPanelIndent
	return panel
}

// rebuildHostsPanelRows lists the rows the panel shows: the hosts the panel's
// search lets through, in the order the user chose for the hosts page, under the
// groups the page has open.
func (state *State) rebuildHostsPanelRows() {
	panel := &state.hostsPanel
	panel.stale = false
	words := strings.Fields(strings.ToLower(panel.search.Text()))
	panel.filtering = len(words) != 0
	panel.hosts = panel.hosts[:0]
	for index, host := range state.hosts {
		if !panel.filtering || matchesHost(host, words) {
			panel.hosts = append(panel.hosts, index)
		}
	}
	state.sortHostIndexes(panel.hosts)
	panel.rows = state.appendHostTreeRows(panel.rows[:0], panel.hosts, words, state.namedHostGroups() == 0, false, "")
}

// hostsPanelExpansionActions says which of expand-all and collapse-all would
// change the panel. A search opens every group it finds a host in, and a catalog
// without named groups lists no group headers, so neither has anything to do.
func (state *State) hostsPanelExpansionActions() (expand, collapse bool) {
	if state.hostsPanel.filtering || state.namedHostGroups() == 0 {
		return false, false
	}
	for _, group := range state.hostGroups {
		expand = expand || !group.Expanded
		collapse = collapse || group.Expanded
	}
	return expand, collapse
}

// hostsPanelEntry is row i of the panel, or false for a row the catalog no
// longer has: a paint may come between a change to the hosts and the rebuild.
func (state *State) hostsPanelEntry(i int) (hostTreeRow, bool) {
	rows := state.hostsPanel.rows
	if i < 0 || i >= len(rows) {
		return hostTreeRow{}, false
	}
	entry := rows[i]
	if entry.Kind == hostTreeGroup {
		return entry, entry.GroupIndex < len(state.hostGroups)
	}
	return entry, entry.HostIndex < len(state.hosts)
}

// hostsPanelKey names a row by its place in the catalog, so the Tree keeps a
// row's focus as rows above it open and close: groups are odd and hosts even.
func (state *State) hostsPanelKey(i int) int {
	entry, ok := state.hostsPanelEntry(i)
	switch {
	case !ok:
		return -1 - i
	case entry.Kind == hostTreeGroup:
		return 2*entry.GroupIndex + 1
	}
	return 2 * entry.HostIndex
}

func (state *State) hostsPanelNode(i int) ui.TreeNode {
	entry, ok := state.hostsPanelEntry(i)
	if !ok {
		return ui.TreeNode{Height: hostsPanelHostRow}
	}
	panel := &state.hostsPanel
	if entry.Kind == hostTreeGroup {
		group := state.hostGroups[entry.GroupIndex]
		return ui.TreeNode{Depth: int(entry.Depth), Branch: true, Expanded: group.Expanded || panel.filtering, Height: hostsPanelGroupRow}
	}
	return ui.TreeNode{Depth: int(entry.Depth), Height: hostsPanelHostRow, Selected: state.hosts[entry.HostIndex].ID == panel.selected}
}

func (state *State) hostsPanelLabel(i int) string {
	entry, ok := state.hostsPanelEntry(i)
	switch {
	case !ok:
		return ""
	case entry.Kind == hostTreeGroup:
		return state.hostGroupLabel(state.hostGroups[entry.GroupIndex])
	}
	return state.hostPrivacySemanticLabel(state.hosts[entry.HostIndex])
}

// hostsPanelDeclareRow declares what is in row i after the Tree's chevron: a
// group's name and count, or a host's badge, name and connection light. The
// badge and the light are painted into their slots by the row's drawer.
func (state *State) hostsPanelDeclareRow(i int) {
	panel := &state.hostsPanel
	c := panel.c
	entry, ok := state.hostsPanelEntry(i)
	if c == nil || !ok {
		return
	}
	pal := state.pal
	if entry.Kind == hostTreeGroup {
		group := state.hostGroups[entry.GroupIndex]
		label := group.Label
		if group.Name == "" {
			label = state.messages.HostGroupDefault
		}
		ui.Row(c).Fill().Gap(6).Items(ui.Center).Children(func() {
			folder := lucideIcons[iconFolderSolid]
			if group.Expanded || panel.filtering {
				folder = lucideIcons[iconFolderSolidOpen]
			}
			ui.Icon(c, folder).Size(16, 16).Shrink(0)
			ui.Text(c, label).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis().Shrink(1)
			ui.Text(c, group.Digits).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).Shrink(0)
		})
		return
	}
	host := state.hosts[entry.HostIndex]
	d := panel.drawer(state, i)
	ui.Row(c).Fill().Gap(8).Items(ui.Center).PadSides(0, 0, 0, 4).Draw(d.fn).Children(func() {
		ui.Slot(c).Size(hostsPanelBadge, hostsPanelBadge).Shrink(0)
		ui.Text(c, host.Label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text).Ellipsis().Shrink(1)
		ui.Spacer(c)
		ui.Slot(c).Size(hostsPanelLight, hostsPanelLight).Shrink(0)
	})
}

func (panel *hostsPanel) drawer(state *State, index int) *hostsPanelRowDrawer {
	if panel.next >= len(panel.drawers) {
		d := &hostsPanelRowDrawer{owner: state}
		d.fn = d.paint
		panel.drawers = append(panel.drawers, d)
	}
	d := panel.drawers[panel.next]
	panel.next++
	d.index = index
	return d
}

// paintHostsPanelRow paints a host row's badge and light at the two ends of the
// row's room.
func (state *State) paintHostsPanelRow(d *ui.DrawContext, index int) {
	entry, ok := state.hostsPanelEntry(index)
	if !ok || entry.Kind != hostTreeHost {
		return
	}
	host := state.hosts[entry.HostIndex]
	room := d.Rect
	pal := state.pal
	badge := gfx.Rect{X: room.X + 4, Y: room.Y + (room.Height-hostsPanelBadge)/2, Width: hostsPanelBadge, Height: hostsPanelBadge}
	state.paintHostCatalogIcon(d.Canvas, host, badge, pal.workspace)
	if kind, color := state.hostLight(host); kind != lightIdle {
		paintLight(d.Canvas, room.X+room.Width-hostsPanelLight/2, room.Y+room.Height/2, kind, color, pal.workspace)
	}
}

// declareHostsPanel declares the session manager at the window's left edge of
// the terminals: a header with its actions, the search box and the host tree,
// then the divider the user drags to size it. The terminals are declared before
// it, so the divider's strip takes the presses that fall on it.
func (state *State) declareHostsPanel(c ui.C) {
	panel := state.hostsPanelFns()
	frame := state.toolbarDockFrame(state.viewport, true)
	extent := state.hostsPanelExtent(frame.Width)
	if extent <= 0 || frame.Height <= 0 {
		return
	}
	width := extent - 1
	m, pal := state.messages, state.pal
	c.Part("hosts-panel")
	ui.Column(c).Absolute().Left(frame.X).Top(frame.Y).Size(width, frame.Height).Clip().Background(pal.workspace).Children(func() {
		ui.Row(c).Height(hostsPanelHeader).Shrink(0).FillWidth().PadSides(0, 6, 0, 12).Gap(2).Items(ui.Center).Children(func() {
			ui.Text(c, m.NavHosts).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis().Grow(1).MinWidth(0)
			if ui.Button(c, m.NewHost).Icon(lucideIcons[iconPlus]).IconOnly().Ghost().Size(28, 28).Shrink(0).Tooltip(m.NewHost).Clicked() {
				c.After(state.openHostDialog)
			}
			if ui.Button(c, m.HostsPanel.Manage).Icon(lucideIcons[iconHosts]).IconOnly().Ghost().Size(28, 28).Shrink(0).Tooltip(m.HostsPanel.Manage).Clicked() {
				c.After(state.manageHostsFromPanel)
			}
			if ui.Button(c, m.HostsPanel.Close).Icon(lucideIcons[iconClose]).IconOnly().Ghost().Size(28, 28).Shrink(0).Tooltip(m.HostsPanel.Close).Clicked() {
				c.After(func() {
					state.toggleHostsPanel()
					c.Canvas().RequestFocus(state.activeTerminalFocusHit())
				})
			}
		})
		ui.Row(c).Height(hostsPanelSearch).Shrink(0).FillWidth().PadSides(0, 8, 6, 8).Gap(2).Items(ui.Center).Children(func() {
			input := ui.TextFieldID(c, hitHostsPanelSearch, &panel.search, m.NavSearch, m.SearchPlaceholder).
				Icon(lucideIcons[iconSearch]).FontSize(metrics.FontSizeS).Height(28).Grow(1).MinWidth(0)
			if panel.search.Text() != "" {
				input = input.Action(lucideIcons[iconClose], m.SearchClear)
				if input.ActionClicked() {
					panel.search.SetText("")
					panel.stale = true
				}
			}
			if input.Changed() {
				panel.stale = true
			}
			if input.Submitted() && len(panel.rows) > 0 {
				c.Canvas().RequestFocus(panel.tree.RowID(0))
			}
			// The rows follow this frame's search before the buttons ask whether
			// a search holds the groups open.
			if panel.stale {
				state.rebuildHostsPanelRows()
			}
			expand, collapse := state.hostsPanelExpansionActions()
			if ui.Button(c, m.HostGroupActions.ExpandAll).Hit(hitHostsPanelExpand, m.HostGroupActions.ExpandAll).Icon(lucideIcons[iconChevronsExpand]).IconOnly().Ghost().Size(28, 28).Shrink(0).
				Tooltip(m.HostGroupActions.ExpandAll).Disabled(!expand).Clicked() {
				c.After(func() { state.setHostGroupsExpanded(true) })
			}
			if ui.Button(c, m.HostGroupActions.CollapseAll).Hit(hitHostsPanelCollapse, m.HostGroupActions.CollapseAll).Icon(lucideIcons[iconChevronsCollapse]).IconOnly().Ghost().Size(28, 28).Shrink(0).
				Tooltip(m.HostGroupActions.CollapseAll).Disabled(!collapse).Clicked() {
				c.After(func() { state.setHostGroupsExpanded(false) })
			}
		})
		panel.bounds = gfx.Rect{X: frame.X, Y: frame.Y, Width: width, Height: frame.Height}
		panel.body = gfx.Rect{
			X: frame.X, Y: frame.Y + hostsPanelHeader + hostsPanelSearch,
			Width: width, Height: max(frame.Height-hostsPanelHeader-hostsPanelSearch, 0),
		}
		state.declareHostsPanelBody(c, panel)
	})
	ui.Slot(c).Absolute().Left(frame.X+width).Top(frame.Y).Size(hostsPanelEdge, frame.Height).
		HitWith(hitHostsPanelDivider, m.HostsPanel.Resize, gfx.HitOptions{SkipFocus: true, Cursor: gfx.PointerCursorResizeHorizontal}).
		Draw(state.drawHostsPanelEdge)
}

// drawHostsPanelEdge paints the line between the panel and the terminals, in the
// first pixel of the divider's strip.
func (state *State) drawHostsPanelEdge(d *ui.DrawContext) {
	d.Canvas.Fill(gfx.Rect{X: d.Rect.X, Y: d.Rect.Y, Width: 1, Height: d.Rect.Height}, state.pal.line)
}

// declareHostsPanelBody declares the host tree, or what stands in its place
// while the catalog is empty or the search finds nothing.
func (state *State) declareHostsPanelBody(c ui.C, panel *hostsPanel) {
	m, pal := state.messages, state.pal
	if len(panel.rows) == 0 {
		text := m.NoMatches
		if len(state.hosts) == 0 {
			text = m.HostsEmpty
		}
		ui.Column(c).Grow(1).FillWidth().Items(ui.Center).PadSides(24, 16, 0, 16).Children(func() {
			ui.Text(c, text).FontSize(metrics.FontSizeS).Color(pal.muted)
		})
		return
	}
	panel.next = 0
	panel.c = c
	panel.tree.RowHeight = hostsPanelHostRow
	panel.tree.CanDrop = panel.canDrop
	ui.Tree(c, &panel.tree, len(panel.rows), panel.label, panel.content).Grow(1).FillWidth().Scrollbars().ScrollbarSize(hostListBarWidth)
	panel.c = nil
	state.hostsPanelChanges(c, panel)
}

// hostsPanelChanges applies what the user did to the tree in this declaration:
// a chevron or a click on a group opens or closes it, and a click on a host
// selects it. Connecting is a double click or Enter, handled with the pointer
// and the keys, so a stray click never starts a connection. The changes that
// rearrange the rows run after the paint that declared them.
func (state *State) hostsPanelChanges(c ui.C, panel *hostsPanel) {
	if i, ok := panel.tree.Toggled(); ok {
		if entry, found := state.hostsPanelEntry(i); found && entry.Kind == hostTreeGroup {
			group := entry.GroupIndex
			c.After(func() { state.toggleHostGroup(group) })
		}
	}
	if i, ok := panel.tree.Chosen(); ok {
		if entry, found := state.hostsPanelEntry(i); found {
			if entry.Kind == hostTreeGroup {
				group := entry.GroupIndex
				c.After(func() { state.toggleHostGroup(group) })
			} else {
				panel.selected = state.hosts[entry.HostIndex].ID
			}
		}
	}
	if move, ok := panel.tree.Dropped(); ok {
		state.hostsPanelDrop(c, panel, move)
	}
}

// hostsPanelCanDrop accepts the drags the panel shows: a group into a group, a
// host into a group, and a host beside another host. Dropping a group beside a
// host would mix the two kinds in the row order, so only groups order against
// groups.
func (state *State) hostsPanelCanDrop(from, to int, where ui.TreeDrop) bool {
	panel := &state.hostsPanel
	if state.modal != modalNone || !state.terminalPage() || !panel.open {
		return false
	}
	source, sourceOK := state.hostsPanelEntry(from)
	target, targetOK := state.hostsPanelEntry(to)
	if !sourceOK || !targetOK || from == to {
		return false
	}
	if source.Kind == hostTreeGroup {
		if target.Kind != hostTreeGroup {
			return false
		}
		switch where {
		case ui.TreeDropInside:
			return state.hostGroupMoveAllowed(source.GroupIndex, target.GroupIndex)
		case ui.TreeDropBefore, ui.TreeDropAfter:
			return state.hostGroupMoveBesideAllowed(source.GroupIndex, target.GroupIndex)
		}
		return false
	}
	switch target.Kind {
	case hostTreeGroup:
		return where == ui.TreeDropInside
	case hostTreeHost:
		return where != ui.TreeDropInside
	}
	return false
}

// hostsPanelDrop applies a drop to the catalog after the paint that declared
// the rows: the row indices and the hosts are read before the move, which may
// rebuild them.
func (state *State) hostsPanelDrop(c ui.C, panel *hostsPanel, move ui.TreeMove) {
	source, sourceOK := state.hostsPanelEntry(move.From)
	target, targetOK := state.hostsPanelEntry(move.To)
	if !sourceOK || !targetOK {
		return
	}
	if source.Kind == hostTreeGroup {
		path := state.hostGroups[source.GroupIndex].Name
		if path == "" || target.Kind != hostTreeGroup {
			return
		}
		parent := state.hostGroups[target.GroupIndex].Name
		switch move.Where {
		case ui.TreeDropInside:
			c.After(func() {
				if !state.hostsPanelLive() {
					return
				}
				if !state.moveHostGroup(path, parent) {
					state.status, state.dialogError = state.dialogError, ""
				}
			})
		case ui.TreeDropBefore, ui.TreeDropAfter:
			after := move.Where == ui.TreeDropAfter
			c.After(func() {
				if !state.hostsPanelLive() {
					return
				}
				sourceAt, targetAt := state.hostGroupIndex(path), state.hostGroupIndex(parent)
				if sourceAt < 0 || targetAt < 0 || !state.moveHostGroupBeside(sourceAt, targetAt, after) {
					state.status, state.dialogError = state.dialogError, ""
				}
			})
		}
		return
	}
	hostID := state.hosts[source.HostIndex].ID
	switch target.Kind {
	case hostTreeGroup:
		group := state.hostGroups[target.GroupIndex].Name
		c.After(func() {
			if !state.hostsPanelLive() {
				return
			}
			state.moveHostToGroup(hostID, group)
		})
	case hostTreeHost:
		targetID := state.hosts[target.HostIndex].ID
		after := move.Where == ui.TreeDropAfter
		c.After(func() {
			if !state.hostsPanelLive() {
				return
			}
			state.reorderHostBeside(hostID, targetID, after)
		})
	}
}

// hostsPanelLive says the session manager is still the page's to change when a
// drop's deferred move runs: a modal or another page took over in between.
func (state *State) hostsPanelLive() bool {
	return state.terminalPage() && state.hostsPanel.open && state.modal == modalNone
}

// manageHostsFromPanel leaves the terminals for the hosts page, where the
// catalog is edited as a whole.
func (state *State) manageHostsFromPanel() {
	state.activeSection = sectionHosts
	state.save()
}

// hostsPanelHostFromHit maps a pointer target to the host row it names.
func (state *State) hostsPanelHostFromHit(id gfx.ID) (Host, bool) {
	panel := &state.hostsPanel
	if id.IsZero() || !panel.open {
		return Host{}, false
	}
	for i := range panel.rows {
		if id != panel.tree.RowID(i) {
			continue
		}
		if entry, ok := state.hostsPanelEntry(i); ok && entry.Kind == hostTreeHost {
			return state.hosts[entry.HostIndex], true
		}
		return Host{}, false
	}
	return Host{}, false
}

// hostsPanelGroupFromHit maps a pointer target to the group row it names.
func (state *State) hostsPanelGroupFromHit(id gfx.ID) (int, bool) {
	panel := &state.hostsPanel
	if id.IsZero() || !panel.open {
		return 0, false
	}
	for i := range panel.rows {
		if id != panel.tree.RowID(i) && id != panel.tree.DisclosureID(i) {
			continue
		}
		if entry, ok := state.hostsPanelEntry(i); ok && entry.Kind == hostTreeGroup {
			return entry.GroupIndex, true
		}
		return 0, false
	}
	return 0, false
}

// connectHostsPanelHost opens a host the same way the hosts page does.
func (state *State) connectHostsPanelHost(host Host) gfx.EventResult {
	state.hostsPanel.selected = host.ID
	state.selectedHostID = host.ID
	state.pendingWorkspace, state.pendingPane = 0, 0
	state.openHostSession(host)
	return gfx.EventResult{Handled: true, Repaint: true, Focus: state.activeTerminalFocusHit()}
}

// hostsPanelPointer sizes the panel by its divider, connects a host on a double
// click and opens its menu on a secondary press.
func (state *State) hostsPanelPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	panel := &state.hostsPanel
	if !panel.open {
		return gfx.EventResult{}, false
	}
	if state.modal != modalNone || !state.terminalPage() {
		panel.resizing = false
		return gfx.EventResult{}, false
	}
	primary := event.Button == gfx.PointerButtonPrimary || event.Button == gfx.PointerButtonNone
	if event.Kind == gfx.PointerDown && event.Target == hitHostsPanelDivider && primary {
		panel.resizing = true
	}
	if panel.resizing {
		frame := state.toolbarDockFrame(event.Viewport, true)
		switch event.Kind {
		case gfx.PointerDown, gfx.PointerMove, gfx.PointerUp:
			state.resizeHostsPanel(frame, event.X-frame.X)
		}
		if event.Kind == gfx.PointerUp || event.Kind == gfx.PointerExit {
			panel.resizing = false
			state.save()
		}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if event.Kind != gfx.PointerDown {
		return gfx.EventResult{}, false
	}
	at := gfx.Point{X: event.X, Y: event.Y}
	if host, ok := state.hostsPanelHostFromHit(event.Target); ok {
		switch {
		case event.Button == gfx.PointerButtonPrimary && event.ClickCount >= 2:
			return state.connectHostsPanelHost(host), true
		case event.Button == gfx.PointerButtonSecondary:
			panel.selected = host.ID
			state.selectedHostID = host.ID
			state.hostBlankMenu.open = false
			state.hostMenu = hostMenu{open: true, host: host.ID, checked: state.hostSelected(host.ID), at: at}
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		return gfx.EventResult{}, false
	}
	if group, ok := state.hostsPanelGroupFromHit(event.Target); ok {
		if event.Button == gfx.PointerButtonSecondary {
			state.openHostGroupActions(state.hostGroups[group].Name, at)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		return gfx.EventResult{}, false
	}
	// A secondary press on the panel's own background starts a catalog at the
	// top level. A press with any other target belongs to a control, a row, or
	// an open menu's backdrop: the backdrop's own handler must dismiss that menu
	// first, and the runtime delivers the press again to the row underneath.
	if event.Button == gfx.PointerButtonSecondary && event.Target.IsZero() && pointInRect(event.X, event.Y, panel.bounds) {
		state.openHostBlankMenu(at)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

// resizeHostsPanel sets the panel's width to the pointer's distance from the
// content area's edge, within what the panel and the terminals can spare.
func (state *State) resizeHostsPanel(frame gfx.Rect, width float32) {
	panel := &state.hostsPanel
	width = min(max(width, hostsPanelMinWidth), min(hostsPanelMaxWidth, frame.Width*hostsPanelMaxShare))
	if width == panel.width {
		return
	}
	panel.width = width
	state.refitForHostsPanel()
}

// hostsPanelKeyPress connects the host row the keyboard is on when Enter is pressed;
// the Tree itself only selects on Enter.
func (state *State) hostsPanelKeyPress(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if event.Key != gfx.KeyEnter || event.Modifiers != 0 || event.Release || event.Repeat || state.modal != modalNone || !state.terminalPage() {
		return gfx.EventResult{}, false
	}
	host, ok := state.hostsPanelHostFromHit(event.Target)
	if !ok {
		return gfx.EventResult{}, false
	}
	return state.connectHostsPanelHost(host), true
}
