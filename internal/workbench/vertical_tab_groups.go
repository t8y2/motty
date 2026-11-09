package workbench

import (
	"fmt"
	"hash/fnv"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

const verticalTabIndent = float32(14)

// Grouping is only a view of the deck. It never reorders, merges or reconnects
// sessions; disabling it restores the exact flat tab list.
type verticalTabKey struct {
	protocol           hostProtocol
	address, user      string
	port               uint16
	jump, proxy, shell string
	local              bool
	workspace          layoutcore.WorkspaceID // nonzero: an empty or mixed-host tab
}

type verticalTabGroup struct {
	key         verticalTabKey
	hit         gfx.ID // derived from key, so it outlives the group's first member
	title       string
	label       string
	countText   string
	labelCount  int
	labelFormat string
	members     [layoutcore.MaxWorkspaces]int
	count       int
	active      bool
	badge       ui.Box
}

func (group *verticalTabGroup) id() gfx.ID { return group.hit }

// hash is a stable number for a group's key. The keyboard focus of a group
// header is its hit identity: derived from the first member's workspace it would
// be lost when that tab is closed or moved, while the key lasts as long as the
// group does.
func (key verticalTabKey) hash() int {
	h := fnv.New32a()
	fmt.Fprintf(h, "%d\x00%s\x00%s\x00%d\x00%s\x00%s\x00%s\x00%t", key.protocol, key.address, key.user, key.port, key.jump, key.proxy, key.shell, key.local)
	return int(h.Sum32() & 0x7fffffff)
}

type verticalTabRow struct {
	group    int
	tab      int // -1 is a group header
	indented bool
	number   int // the tab's place in the number row, zero for a header
}

type verticalTabGrouping struct {
	groups    []verticalTabGroup
	rows      []verticalTabRow
	order     []layoutcore.WorkspaceInfo      // all tabs, in grouped visual order
	layout    []layoutcore.WorkspaceInfo      // visible rows for the scroll strip
	options   []components.TabOption          // visible keyboard targets, including headers
	tree      []components.TreeNavigationItem // the same rows as a tree, for the arrow keys
	collapsed map[verticalTabKey]bool
	revealed  layoutcore.WorkspaceID
	compact   bool
}

func (state *State) verticalTabsGrouped() bool {
	return state.tabOrientation == tabsVertical && state.verticalTabs.grouping
}

func (state *State) setVerticalTabsGrouping(enabled bool) bool {
	if state.verticalTabs.grouping == enabled {
		return false
	}
	state.verticalTabs.grouping = enabled
	state.verticalTabs.grouped.revealed = 0
	state.tabStrip.revealed = 0
	return true
}

func (state *State) verticalSessionKey(session *Session) verticalTabKey {
	host := session.host
	if session.isLocal() {
		return verticalTabKey{local: true, shell: host.localShell}
	}
	address := strings.TrimSpace(host.Address)
	user := strings.TrimSpace(host.User)
	if host.isSSH() {
		user = state.login(host)
	}
	// The connection status names whichever endpoint is being dialed now, which
	// is a jump host until the route reaches the target. Only the target's own
	// endpoint may stand for it: otherwise a tab would join its jump host's group
	// first, and stay there if the route failed.
	target := host.isSSH() && session.role == sshconn.EndpointTarget && session.endpoint.Host != ""
	if target {
		address, user = session.endpoint.Host, session.endpoint.User
	}
	if host.Protocol != protocolSerial {
		address = strings.Trim(address, "[]")
		if ip, err := netip.ParseAddr(address); err == nil {
			address = ip.String()
		} else {
			address = strings.TrimSuffix(strings.ToLower(address), ".")
		}
	}
	port := host.Port
	if target && session.endpoint.Port != 0 {
		port = session.endpoint.Port
	}
	if port == 0 && host.Protocol != protocolSerial {
		port = host.defaultPort()
	}
	return verticalTabKey{
		protocol: host.Protocol, address: address, port: port, user: user,
		jump: session.jumpPath, proxy: session.proxyRoute,
	}
}

func (state *State) verticalWorkspaceKey(tab layoutcore.WorkspaceInfo) (verticalTabKey, string) {
	var slots [layoutcore.MaxPanes]layoutcore.Pane
	var key verticalTabKey
	var title string
	found := false
	for _, pane := range state.deck.Panes(tab.ID, slots[:0]) {
		if pane.SessionID == 0 {
			continue
		}
		session, ok := state.sessionByIDPointer(uint32(pane.SessionID))
		if !ok {
			return verticalTabKey{workspace: tab.ID}, tab.Title
		}
		candidate := state.verticalSessionKey(session)
		if candidate.address == "" && !candidate.local || found && candidate != key {
			return verticalTabKey{workspace: tab.ID}, tab.Title
		}
		if !found {
			key, title, found = candidate, session.host.Label, true
			if title == "" {
				title = state.sessionAddress(session.host)
			}
		}
	}
	if !found {
		return verticalTabKey{workspace: tab.ID}, tab.Title
	}
	return key, title
}

func (state *State) rebuildVerticalTabGroups(compact bool) {
	view := &state.verticalTabs.grouped
	view.compact = compact
	view.groups = view.groups[:0]
	active := state.deck.ActiveWorkspace()
	for index, tab := range state.workspaceTabs {
		key, title := state.verticalWorkspaceKey(tab)
		groupIndex := -1
		for i := range view.groups {
			if view.groups[i].key == key {
				groupIndex = i
				break
			}
		}
		if groupIndex < 0 {
			groupIndex = len(view.groups)
			if groupIndex < cap(view.groups) {
				view.groups = view.groups[:groupIndex+1]
			} else {
				view.groups = append(view.groups, verticalTabGroup{})
			}
			group := &view.groups[groupIndex]
			if group.key != key || group.title != title {
				group.label = ""
			}
			group.key, group.title, group.count, group.active, group.badge = key, title, 0, false, ui.Box{}
		}
		group := &view.groups[groupIndex]
		group.members[group.count] = index
		group.count++
		group.active = group.active || tab.ID == active && state.terminalPage()
	}
	for index := range view.groups {
		group := &view.groups[index]
		number := group.key.hash()
		for taken := true; taken; {
			taken = false
			for other := range view.groups[:index] {
				if view.groups[other].hit == hitVerticalTabGroups.At(number) {
					number, taken = (number+1)&0x7fffffff, true
				}
			}
		}
		group.hit = hitVerticalTabGroups.At(number)
	}
	for key := range view.collapsed {
		found := false
		for _, group := range view.groups {
			found = found || group.key == key
		}
		if !found {
			delete(view.collapsed, key)
		}
	}
	// A shortcut or newly created connection must reveal its child. A manual
	// collapse of the current group remains closed until another tab is selected.
	if state.terminalPage() && active != view.revealed {
		for _, group := range view.groups {
			if group.active {
				delete(view.collapsed, group.key)
			}
		}
	}
	view.revealed = active
	previousActiveRow := -1
	for index, row := range view.layout {
		if row.ID == active {
			previousActiveRow = index
		}
	}
	view.order, view.rows, view.layout, view.options, view.tree = view.order[:0], view.rows[:0], view.layout[:0], view.options[:0], view.tree[:0]
	for index := range view.groups {
		group := &view.groups[index]
		if group.label == "" || group.labelCount != group.count || group.labelFormat != state.messages.VerticalTabGroupCountFmt {
			group.label = fmt.Sprintf(state.messages.VerticalTabGroupCountFmt, group.title, group.count)
			group.countText = strconv.Itoa(group.count)
			group.labelCount, group.labelFormat = group.count, state.messages.VerticalTabGroupCountFmt
		}
		if group.count > 1 && !compact {
			id := group.id()
			view.rows = append(view.rows, verticalTabRow{group: index, tab: -1})
			view.options = append(view.options, components.TabOption{ID: id, Label: group.label})
			view.tree = append(view.tree, components.TreeNavigationItem{Branch: true, Expanded: !view.collapsed[group.key]})
			row := layoutcore.WorkspaceInfo{}
			if group.active && view.collapsed[group.key] {
				row.ID = active
			}
			view.layout = append(view.layout, row)
		}
		for _, tabIndex := range group.members[:group.count] {
			tab := state.workspaceTabs[tabIndex]
			// A collapsed group keeps the numbers of the tabs it hides: they
			// still occupy their places in the row the shortcuts count.
			number := len(view.order) + 1
			view.order = append(view.order, tab)
			if group.count > 1 && view.collapsed[group.key] && !compact {
				continue
			}
			indented := group.count > 1 && !compact
			view.rows = append(view.rows, verticalTabRow{group: index, tab: tabIndex, indented: indented, number: number})
			view.layout = append(view.layout, tab)
			view.options = append(view.options, components.TabOption{ID: hitTabList.At(int(tab.ID)), Label: state.tabLabel(tab)})
			item := components.TreeNavigationItem{}
			if indented {
				item.Depth = 1
			}
			view.tree = append(view.tree, item)
		}
	}
	if previousActiveRow >= 0 {
		start := float32(previousActiveRow) * state.tabStrip.pitch
		if start >= state.tabStrip.offset && start+verticalTabHeight <= state.tabStrip.offset+state.tabStrip.area.Height {
			for index, row := range view.layout {
				if row.ID == active && index != previousActiveRow {
					state.tabStrip.revealed = 0
				}
			}
		}
	}
}

func (state *State) declareGroupedVerticalTabs(f *ui.Frame, width float32, compact bool) {
	view := &state.verticalTabs
	for _, row := range view.grouped.rows {
		if row.tab < 0 {
			state.declareVerticalTabGroup(f, row.group, width)
			continue
		}
		indent := float32(0)
		if row.indented {
			indent = verticalTabIndent
		}
		f.Row().Size(width, verticalTabHeight).Shrink(0).MarginSides(0, 0, barGap, 0).Children(func() {
			if indent > 0 {
				f.Slot().Width(indent).Shrink(0)
			}
			view.tabs = append(view.tabs, state.declareWorkspaceTab(f, state.workspaceTabs[row.tab], max(width-indent, 0), verticalTabHeight, 0, true, compact, row.number))
		})
	}
}

func (state *State) declareVerticalTabGroup(f *ui.Frame, index int, width float32) {
	view, pal := &state.verticalTabs.grouped, state.pal
	group := &view.groups[index]
	id := group.id()
	expanded := !view.collapsed[group.key]
	fill := pal.shell
	if group.active {
		fill = pal.surfaceHot
	} else if f.Canvas().Hovered() == id {
		fill = pal.surfaceSoft
	}
	icon := uint8(iconChevronRight)
	if expanded {
		icon = iconChevronDown
	}
	f.Row().Size(width, verticalTabHeight).Shrink(0).MarginSides(0, 0, barGap, 0).PadX(4).Gap(4).Items(ui.Center).
		Radius(radius.item).Background(fill).
		HitWith(id, group.label, gfx.HitOptions{Role: gfx.RoleButton, Expanded: expanded, Expandable: true, Selected: group.active, Hint: group.label}).Children(func() {
		f.Icon(lucideIcons[icon]).Size(12, 12).Shrink(0).Color(pal.muted)
		group.badge = f.Slot().Size(18, 18).Shrink(0)
		f.Text(group.title).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.text).Ellipsis().Grow(1).Basis(0)
		f.Text(group.countText).FontSize(metrics.FontSizeS).Color(pal.muted).Shrink(0)
	})
}

func (state *State) paintVerticalTabGroupIcons(canvas *gfx.Canvas) {
	for _, group := range state.verticalTabs.grouped.groups {
		if group.count < 2 || group.badge == (ui.Box{}) {
			continue
		}
		tab := state.workspaceTabs[group.members[0]]
		if session := state.tabSession(tab); session != nil {
			state.paintHostCatalogIcon(canvas, session.host, group.badge.Rect(), state.pal.shell)
		}
	}
}

func (state *State) clickVerticalTabGroup(id gfx.ID) bool {
	if !state.verticalTabsGrouped() || id.Scope() != hitVerticalTabGroups {
		return false
	}
	for _, group := range state.verticalTabs.grouped.groups {
		if group.count > 1 && group.id() == id {
			state.setVerticalTabGroupCollapsed(group.key, !state.verticalTabs.grouped.collapsed[group.key])
			return true
		}
	}
	return true
}

func (state *State) setVerticalTabGroupCollapsed(key verticalTabKey, collapsed bool) {
	view := &state.verticalTabs.grouped
	if !collapsed {
		delete(view.collapsed, key)
		return
	}
	if view.collapsed == nil {
		view.collapsed = make(map[verticalTabKey]bool)
	}
	view.collapsed[key] = true
}

// verticalTabGroupKey moves the keyboard through the grouped rows as a tree: the arrows step
// through the visible rows without wrapping, Right opens a group or goes into it, Left closes it
// or goes back to its header, and Home, End and the page keys jump. The rows are the ones on
// screen, so the children of a collapsed group are skipped. It owns every key in the list.
func (state *State) verticalTabGroupKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if !state.verticalTabsGrouped() || event.Target.Scope() != hitTabList && event.Target.Scope() != hitVerticalTabGroups {
		return gfx.EventResult{}, false
	}
	view := &state.verticalTabs.grouped
	current := slices.IndexFunc(view.options, func(option components.TabOption) bool { return option.ID == event.Target })
	pageRows := 1
	if pitch := state.tabStrip.pitch; pitch > 0 {
		pageRows = max(int(state.tabStrip.area.Height/pitch), 1)
	}
	next, action, handled := components.NavigateTree(view.tree, current, event.Key, pageRows)
	if !handled {
		return gfx.EventResult{}, true
	}
	switch action {
	case components.TreeNavigationExpand, components.TreeNavigationCollapse:
		state.setVerticalTabGroupCollapsed(view.groups[view.rows[current].group].key, action == components.TreeNavigationCollapse)
		return gfx.EventResult{Focus: event.Target, FocusVisible: true, Handled: true, Repaint: true}, true
	case components.TreeNavigationMove:
		return state.focusVerticalTabRow(view.options[next].ID), true
	}
	return gfx.EventResult{}, true
}

func (state *State) focusVerticalTabRow(id gfx.ID) gfx.EventResult {
	for index, option := range state.verticalTabs.grouped.options {
		if option.ID == id {
			start := float32(index) * state.tabStrip.pitch
			state.tabStrip.offset = min(state.tabStrip.offset, start)
			state.tabStrip.offset = max(state.tabStrip.offset, start+verticalTabHeight-state.tabStrip.area.Height)
			state.tabStrip.offset = min(max(state.tabStrip.offset, 0), state.tabStrip.max)
			break
		}
	}
	return gfx.EventResult{Focus: id, FocusVisible: true, Handled: true, Repaint: true}
}

// Grouped rows and deck positions differ, so a drop is resolved to what it does
// to the deck, never read as a tab index. It lands in one of two kinds of place.
//
// Inside a server's group, a connection of that server is reordered among its
// siblings. On the boundary between groups, a lone tab or a whole group (dragged
// by its header, unit) is moved to stand there. A connection cannot leave its
// group, because the host decides the group, so any other drop inside a group
// is refused: it would only snap back. Whether a drop changes anything is judged
// by where things are shown, not by their positions in the deck.
func (state *State) groupedVerticalTabDrop(at gfx.Point, source layoutcore.WorkspaceID, unit []layoutcore.WorkspaceID) tabDrop {
	view := &state.verticalTabs.grouped
	if len(view.rows) == 0 {
		return tabDrop{}
	}
	strip := &state.tabStrip
	position := (at.Y - strip.origin + strip.offset) / strip.pitch
	rowIndex := min(max(int(position), 0), len(view.rows)-1)
	after := position-float32(rowIndex) > 0.5
	g := view.rows[rowIndex].group
	group := &view.groups[g]
	first, last := rowIndex, rowIndex
	for first > 0 && view.rows[first-1].group == g {
		first--
	}
	for last+1 < len(view.rows) && view.rows[last+1].group == g {
		last++
	}
	sourceGroup, sourceSlot := -1, -1
	if source != 0 {
		slot, ok := state.workspaceTab(source)
		if !ok {
			return tabDrop{}
		}
		key, _ := state.verticalWorkspaceKey(state.workspaceTabs[slot])
		for index := range view.groups {
			if view.groups[index].key == key {
				sourceGroup, sourceSlot = index, slot
			}
		}
		if sourceGroup < 0 {
			return tabDrop{}
		}
	}
	rowY := func(row int) float32 {
		return strip.origin - strip.offset + float32(row)*strip.pitch - barGap/2
	}
	lone := len(unit) == 0 && source != 0
	if lone && sourceGroup == g && group.count > 1 {
		// Among the siblings: p is the place before which the connection goes.
		childStart := first
		if view.rows[first].tab < 0 {
			childStart++
		}
		if childStart > last {
			return tabDrop{}
		}
		p := 0
		if rowIndex >= childStart {
			p = rowIndex - childStart
			if after {
				p++
			}
		}
		member := slices.Index(group.members[:group.count], sourceSlot)
		if p == member || p == member+1 {
			return tabDrop{}
		}
		boundary := group.members[group.count-1] + 1
		if p < group.count {
			boundary = group.members[p]
		}
		return tabDrop{kind: dropReorder, boundary: boundary, zone: state.tabInsertion(rowY(childStart + p))}
	}
	if lone && view.groups[sourceGroup].count > 1 || group.count > 1 && !(rowIndex == first && !after || rowIndex == last && after) {
		return tabDrop{}
	}
	gap, row := g, first
	if after {
		gap, row = g+1, last+1
	}
	if sourceGroup >= 0 && (gap == sourceGroup || gap == sourceGroup+1) {
		return tabDrop{}
	}
	boundary := len(state.workspaceTabs)
	if gap < len(view.groups) {
		boundary = view.groups[gap].members[0]
	}
	return tabDrop{kind: dropReorder, boundary: boundary, moveUnit: source != 0, zone: state.tabInsertion(rowY(row))}
}

// verticalTabGroupMembers is the workspaces of the server group whose header has
// that identity, in deck order, and the header's text.
func (state *State) verticalTabGroupMembers(id gfx.ID) ([]layoutcore.WorkspaceID, string, bool) {
	if !state.verticalTabsGrouped() || id.Scope() != hitVerticalTabGroups {
		return nil, "", false
	}
	for _, group := range state.verticalTabs.grouped.groups {
		if group.count > 1 && group.hit == id {
			members := make([]layoutcore.WorkspaceID, 0, group.count)
			for _, slot := range group.members[:group.count] {
				members = append(members, state.workspaceTabs[slot].ID)
			}
			return members, group.label, true
		}
	}
	return nil, "", false
}

// moveWorkspaceUnit puts the tabs of unit, which keep their order, in the deck
// directly before the tab at deck index before (the end when it is the tab count).
func (state *State) moveWorkspaceUnit(unit []layoutcore.WorkspaceID, before int) {
	tabs := state.workspaceTabs
	var anchor layoutcore.WorkspaceID
	if before < len(tabs) {
		anchor = tabs[before].ID
	}
	order := make([]layoutcore.WorkspaceID, 0, len(tabs))
	insertAt := -1
	for _, tab := range tabs {
		if slices.Contains(unit, tab.ID) {
			continue
		}
		if tab.ID == anchor {
			insertAt = len(order)
		}
		order = append(order, tab.ID)
	}
	if insertAt < 0 {
		insertAt = len(order)
	}
	order = slices.Insert(order, insertAt, unit...)
	for index, id := range order {
		_ = state.deck.MoveWorkspace(id, index)
	}
}

func (state *State) visualWorkspaceTabs() []layoutcore.WorkspaceInfo {
	if state.verticalTabsGrouped() {
		state.rebuildVerticalTabGroups(state.verticalTabs.grouped.compact)
		return state.verticalTabs.grouped.order
	}
	return state.workspaceTabs
}

// Reorder only the member slots of this server. Moving its first child past
// another server in the raw deck would otherwise unexpectedly move the group.
func (state *State) reorderVerticalTabGroup(source layoutcore.WorkspaceID, boundary int) bool {
	for _, group := range state.verticalTabs.grouped.groups {
		from := -1
		to := 0
		var members [layoutcore.MaxWorkspaces]layoutcore.WorkspaceID
		for index, slot := range group.members[:group.count] {
			members[index] = state.workspaceTabs[slot].ID
			if members[index] == source {
				from = index
			}
			if slot < boundary {
				to++
			}
		}
		if from < 0 || group.count < 2 {
			continue
		}
		if to > from {
			to--
		}
		if to < from {
			copy(members[to+1:from+1], members[to:from])
		} else {
			copy(members[from:to], members[from+1:to+1])
		}
		members[to] = source
		var order [layoutcore.MaxWorkspaces]layoutcore.WorkspaceID
		for index, tab := range state.workspaceTabs {
			order[index] = tab.ID
		}
		for index, slot := range group.members[:group.count] {
			order[slot] = members[index]
		}
		for index, id := range order[:len(state.workspaceTabs)] {
			_ = state.deck.MoveWorkspace(id, index)
		}
		return true
	}
	return false
}

func (state *State) workspaceTabsAfter(workspace layoutcore.WorkspaceID) []layoutcore.WorkspaceInfo {
	return tabsAfter(state.visualWorkspaceTabs(), workspace)
}

// paintedWorkspaceTabsAfter is workspaceTabsAfter for painting: the sidebar has
// already put the tabs in order this frame, so they are not grouped again.
func (state *State) paintedWorkspaceTabsAfter(workspace layoutcore.WorkspaceID) []layoutcore.WorkspaceInfo {
	if state.verticalTabsGrouped() {
		return tabsAfter(state.verticalTabs.grouped.order, workspace)
	}
	return tabsAfter(state.workspaceTabs, workspace)
}

func tabsAfter(tabs []layoutcore.WorkspaceInfo, workspace layoutcore.WorkspaceID) []layoutcore.WorkspaceInfo {
	for index, tab := range tabs {
		if tab.ID == workspace {
			return tabs[index+1:]
		}
	}
	return nil
}
