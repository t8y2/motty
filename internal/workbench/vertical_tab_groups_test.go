package workbench

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

// Restored, disconnected tabs exercise grouping without a network substitute.
func restoredGroupedTabs(t *testing.T, entries ...savedSession) *State {
	t.Helper()
	state := newTestState(t, Options{})
	sessions := state.restoreSessions(entries, len(entries))
	state.deck = layoutcore.Deck{}
	for index, session := range sessions {
		id, pane, err := state.deck.Create(entries[index].Label)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.deck.Bind(id, pane, layoutcore.SessionID(session)); err != nil {
			t.Fatal(err)
		}
	}
	state.syncActiveSession()
	state.activeSection = sectionTerminal
	state.rebuildTabOptions()
	state.setTabOrientation(tabsVertical)
	state.setVerticalTabsGrouping(true)
	return state
}

func groupingEntries() []savedSession {
	return []savedSession{
		{Label: "Server A", Address: "a.example.com", User: "admin", Port: 22},
		{Label: "Server B", Address: "b.example.com", User: "admin", Port: 22},
		{Label: "Server A logs", Address: "A.EXAMPLE.COM", User: "admin", Port: 22},
		{Label: "Server A other account", Address: "a.example.com", User: "deploy", Port: 22},
	}
}

func TestVerticalTabGroupingIsAnIndependentLocalSetting(t *testing.T) {
	state := newTestState(t, Options{})
	if state.verticalTabs.grouping {
		t.Fatal("grouping must default to off")
	}
	state.Click(hitSettings)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1800})
	if _, ok := hitByID(&canvas, hitSettingsVerticalTabsGrouping); ok {
		t.Fatal("horizontal tabs have nothing to group")
	}
	state.Click(hitSettingsTabsVertical)
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1800})
	if _, ok := hitByID(&canvas, hitSettingsVerticalTabsGrouping); !ok {
		t.Fatal("vertical tabs must expose the grouping switch")
	}
	state.Click(hitSettingsVerticalTabsGrouping)
	state.Click(hitSettingsTabsHorizontal)
	if !state.verticalTabs.grouping || state.verticalTabsGrouped() {
		t.Fatal("the grouping choice is kept, but horizontal tabs are never grouped")
	}
	options := state.options
	state.Close()
	again := newTestState(t, options)
	if !again.verticalTabs.grouping || again.tabOrientation != tabsHorizontal {
		t.Fatal("the independent preference did not survive restart")
	}
}

func TestVerticalTabGroupingCollapsesWithoutChangingTheDeck(t *testing.T) {
	state := restoredGroupedTabs(t, groupingEntries()...)
	before := tabOrder(state)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1200, Height: 760}
	state.Paint(&canvas, size)
	view := &state.verticalTabs.grouped
	if len(view.groups) != 3 || view.groups[0].count != 2 || len(view.rows) != 5 {
		t.Fatalf("expected two Server A connections and two singletons: %+v", view.groups)
	}
	if got := []layoutcore.WorkspaceID{view.order[0].ID, view.order[1].ID, view.order[2].ID, view.order[3].ID}; !reflect.DeepEqual(got, []layoutcore.WorkspaceID{before[0], before[2], before[1], before[3]}) {
		t.Fatalf("grouping did not bring nonadjacent Server A tabs together: %v", got)
	}
	header := view.groups[0].id()
	hit, ok := hitByID(&canvas, header)
	if !ok || !hit.Expandable || !hit.Expanded {
		t.Fatal("the server header must be an accessible disclosure")
	}
	first, _ := hitByID(&canvas, hitTabList.At(int(before[0])))
	third, _ := hitByID(&canvas, hitTabList.At(int(before[2])))
	if first.Bounds.X <= hit.Bounds.X || third.Bounds.Y <= first.Bounds.Y {
		t.Fatal("group members must be indented below their server")
	}
	state.Click(header)
	canvas.Reset()
	state.Paint(&canvas, size)
	if _, ok := hitByID(&canvas, hitTabList.At(int(before[2]))); ok {
		t.Fatal("collapsed children must not retain invisible click targets")
	}
	if !reflect.DeepEqual(tabOrder(state), before) || len(state.sessions) != 4 {
		t.Fatal("collapsing must not reorder the deck or close connections")
	}
	state.setVerticalTabsAutoHide(true)
	canvas.Reset()
	state.Paint(&canvas, size)
	if _, ok := hitByID(&canvas, hitTabList.At(int(before[2]))); !ok {
		t.Fatal("the compact icon rail must keep every connection reachable")
	}
	state.setVerticalTabsAutoHide(false)
	state.activateWorkspace(before[2])
	canvas.Reset()
	state.Paint(&canvas, size)
	if hit, ok := hitByID(&canvas, hitTabList.At(int(before[2]))); !ok || !hit.Selected {
		t.Fatal("activating a hidden connection must expand and reveal it")
	}
	state.closeWorkspace(before[0])
	canvas.Reset()
	state.Paint(&canvas, size)
	for _, row := range state.verticalTabs.grouped.rows {
		if row.tab < 0 {
			t.Fatal("a server with one remaining connection must lose its group header")
		}
	}
	state.setTabOrientation(tabsHorizontal)
	canvas.Reset()
	state.Paint(&canvas, size)
	if len(state.chrome.bar.tabs) != 3 || state.tabStrip.vertical {
		t.Fatal("horizontal mode must ignore the grouping setting")
	}
}

func TestVerticalTabGroupingNavigationReorderAndCloseFollowVisualOrder(t *testing.T) {
	state := restoredGroupedTabs(t, groupingEntries()...)
	ids := tabOrder(state)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1200, Height: 760}
	state.Paint(&canvas, size)
	header := state.verticalTabs.grouped.groups[0].id()
	result := state.Key(gfx.KeyEvent{Target: hitTabList.At(int(ids[0])), Key: gfx.KeyDown})
	if !result.Handled || result.Focus != hitTabList.At(int(ids[2])) {
		t.Fatal("Down must follow grouped visual order, not raw deck order")
	}
	state.Key(gfx.KeyEvent{Target: header, Key: gfx.KeyLeft})
	canvas.Reset()
	state.Paint(&canvas, size)
	result = state.Key(gfx.KeyEvent{Target: header, Key: gfx.KeyDown})
	if !result.Handled || result.Focus != hitTabList.At(int(ids[1])) {
		t.Fatal("Down must skip the children of a collapsed group")
	}
	state.Key(gfx.KeyEvent{Target: header, Key: gfx.KeyRight})
	state.activateWorkspace(ids[0])
	state.cycleWorkspace(1)
	if state.deck.ActiveWorkspace() != ids[2] {
		t.Fatal("Ctrl+Tab must follow the grouped order")
	}
	canvas.Reset()
	state.Paint(&canvas, size)
	first, _ := hitByID(&canvas, hitTabList.At(int(ids[0])))
	third, _ := hitByID(&canvas, hitTabList.At(int(ids[2])))
	target := hitTabList.At(int(ids[2]))
	from := gfx.Point{X: third.Bounds.X + third.Bounds.Width/2, Y: third.Bounds.Y + third.Bounds.Height/2}
	to := gfx.Point{X: from.X, Y: first.Bounds.Y + 2}
	press(state, target, from)
	dragTo(state, target, to)
	if state.tabDrag.drop.kind != dropReorder || state.tabDrag.drop.boundary != 0 {
		t.Fatalf("group headers must not be counted as deck tabs: %+v", state.tabDrag.drop)
	}
	release(state, target, to)
	if tabOrder(state)[0] != ids[2] {
		t.Fatal("dragging within a server did not reorder its connections")
	}
	state.tabMenu = tabMenu{open: true, workspace: ids[2]}
	state.clickTabMenu(hitTabMenuCloseRight)
	if state.modal != modalCloseTabs || state.tabsClose.count != 3 {
		t.Fatalf("close below must include all following visual tabs: %+v", state.tabsClose)
	}
	state.clickCloseTabsDialog(hitTabsCloseCancel)
	canvas.Reset()
	state.Paint(&canvas, size)
	first, _ = hitByID(&canvas, hitTabList.At(int(ids[2])))
	third, _ = hitByID(&canvas, hitTabList.At(int(ids[0])))
	target = hitTabList.At(int(ids[2]))
	from = gfx.Point{X: first.Bounds.X + first.Bounds.Width/2, Y: first.Bounds.Y + first.Bounds.Height/2}
	to = gfx.Point{X: from.X, Y: third.Bounds.Y + third.Bounds.Height - 2}
	press(state, target, from)
	dragTo(state, target, to)
	release(state, target, to)
	if got := tabOrder(state); !reflect.DeepEqual(got, ids) {
		t.Fatalf("reordering a child must preserve other servers' slots: %v", got)
	}
	state.tabMenu = tabMenu{open: true, workspace: ids[2]}
	state.clickTabMenu(hitTabMenuCloseRight)
	if state.tabsClose.count != 2 || !reflect.DeepEqual(state.tabsClose.ids, []layoutcore.WorkspaceID{ids[1], ids[3]}) {
		t.Fatalf("close below must include the earlier deck tab displayed below the group: %+v", state.tabsClose)
	}
}

func TestDigitShortcutFollowsTheGroupedTabOrder(t *testing.T) {
	state := restoredGroupedTabs(t, groupingEntries()...)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 760})
	state.setTabNumbers(true)
	visual := state.visualWorkspaceTabs()
	if len(visual) < 3 {
		t.Fatalf("%d visual tabs", len(visual))
	}
	state.Shortcut(gfx.KeyEvent{Target: state.activeTerminalFocusHit(), Modifiers: keymap.Primary, Text: "3"})
	if state.deck.ActiveWorkspace() != visual[2].ID {
		t.Fatal("the digit shortcut must follow the grouped order")
	}
}

func TestVerticalTabGroupingDistinguishesTargetsAndMixedSplits(t *testing.T) {
	entries := groupingEntries()
	// A shell path has to be one the restore accepts on this platform: on
	// Windows /bin/bash is not absolute, so it would be dropped and the shell
	// group would collapse into the default one.
	otherShell := filepath.Join(t.TempDir(), "other-shell")
	entries = append(entries,
		savedSession{Label: "Other port", Address: "a.example.com", User: "admin", Port: 2222},
		savedSession{Label: "VNC", Address: "a.example.com", User: "admin", Port: 22, savedTransport: savedTransport{Protocol: "vnc"}},
		savedSession{Label: "Local", Local: true},
		savedSession{Label: "Local 2", Local: true},
		savedSession{Label: "Another shell", Local: true, LocalShell: otherShell},
	)
	state := restoredGroupedTabs(t, entries...)
	state.rebuildVerticalTabGroups(false)
	if len(state.verticalTabs.grouped.groups) != 7 {
		t.Fatalf("port, protocol, account and shell must remain distinct: %+v", state.verticalTabs.grouped.groups)
	}
	tabs := state.workspaceTabs
	if _, err := state.deck.Dock(tabs[0].ID, tabs[1].ID, tabs[0].ActivePane, layoutcore.Horizontal, layoutcore.After); err != nil {
		t.Fatal(err)
	}
	state.rebuildTabOptions()
	state.rebuildVerticalTabGroups(false)
	key, _ := state.verticalWorkspaceKey(state.workspaceTabs[0])
	if key.workspace != state.workspaceTabs[0].ID {
		t.Fatal("a split across different servers must remain an independent tab")
	}
}

func TestVerticalTabGroupingRevealsActivatedConnectionsAfterOverflow(t *testing.T) {
	entries := groupingEntries()[:2]
	for range 18 {
		entries = append(entries, savedSession{Label: "Server A shell", Address: "a.example.com", User: "admin", Port: 22})
	}
	state := restoredGroupedTabs(t, entries...)
	size := gfx.Size{Width: 1000, Height: 560}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	last := state.deck.ActiveWorkspace()
	hit, ok := hitByID(&canvas, hitTabList.At(int(last)))
	area := state.tabStrip.area
	if !ok || hit.Bounds.Y < area.Y || hit.Bounds.Y+hit.Bounds.Height > area.Y+area.Height+0.5 {
		t.Fatal("active child must scroll into view beneath its server header")
	}
	group := state.verticalTabs.grouped.groups[0]
	state.Click(group.id())
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.tabStrip.offset > state.tabStrip.max {
		t.Fatal("collapsing must clamp the now shorter list's scroll offset")
	}
	state.activateWorkspace(state.workspaceTabs[0].ID)
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.verticalTabs.grouped.collapsed[group.key] {
		t.Fatal("selecting a child must expand its collapsed server")
	}
}

func sessionOfTab(t *testing.T, state *State, tab layoutcore.WorkspaceID) *Session {
	t.Helper()
	pane, ok := state.deck.Pane(tab, 0)
	if !ok {
		var slots [layoutcore.MaxPanes]layoutcore.Pane
		panes := state.deck.Panes(tab, slots[:0])
		if len(panes) == 0 {
			t.Fatal("tab without a pane")
		}
		pane = panes[0]
	}
	session, ok := state.sessionByIDPointer(uint32(pane.SessionID))
	if !ok {
		t.Fatal("pane without a session")
	}
	return session
}

// While a route is dialed the connection status names the jump host. The tab
// belongs to the server it is going to, from the first moment and when the route
// fails halfway.
func TestVerticalTabGroupingKeepsTheTargetWhileAJumpHostIsDialed(t *testing.T) {
	state := restoredGroupedTabs(t, groupingEntries()...)
	ids := tabOrder(state)
	before, _ := state.verticalWorkspaceKey(state.workspaceTabs[2])
	session := sessionOfTab(t, state, ids[2])
	session.endpoint = sshconn.Endpoint{Host: "jump.example.com", Port: 2200, User: "hop"}
	session.role = sshconn.EndpointProxyJump
	if got, _ := state.verticalWorkspaceKey(state.workspaceTabs[2]); got != before {
		t.Fatalf("a jump host moved the tab to another group: %+v, was %+v", got, before)
	}
	state.rebuildVerticalTabGroups(false)
	if group := state.verticalTabs.grouped.groups[0]; group.count != 2 {
		t.Fatalf("the tab left its server's group while its jump host was dialed: %d members", group.count)
	}
	session.endpoint = sshconn.Endpoint{Host: "a.example.com", Port: 22, User: "admin"}
	session.role = sshconn.EndpointTarget
	if got, _ := state.verticalWorkspaceKey(state.workspaceTabs[2]); got != before {
		t.Fatalf("reaching the target changed the group: %+v", got)
	}
}

func TestVerticalTabGroupHeaderKeepsItsIdentityWhenItsFirstConnectionCloses(t *testing.T) {
	entries := append(groupingEntries(), savedSession{Label: "Server A third", Address: "a.example.com", User: "admin", Port: 22})
	state := restoredGroupedTabs(t, entries...)
	ids := tabOrder(state)
	state.rebuildVerticalTabGroups(false)
	header := state.verticalTabs.grouped.groups[0].id()
	state.closeWorkspace(ids[0])
	state.rebuildTabOptions()
	state.rebuildVerticalTabGroups(false)
	for _, group := range state.verticalTabs.grouped.groups {
		if group.count == 2 && group.id() == header {
			return
		}
	}
	t.Fatal("the header's identity, and the focus on it, must outlive the connection that came first")
}

// A lone connection and a whole server can move to the boundaries between
// groups; a connection cannot be dropped into another server's group, which
// would only snap back.
func TestVerticalTabGroupingMovesLoneTabsAndWholeGroupsBetweenGroups(t *testing.T) {
	size := gfx.Size{Width: 1200, Height: 760}
	setup := func() (*State, []layoutcore.WorkspaceID, *gfx.Canvas) {
		state := restoredGroupedTabs(t, groupingEntries()...)
		canvas := &gfx.Canvas{}
		state.Paint(canvas, size)
		return state, tabOrder(state), canvas
	}
	drag := func(state *State, canvas *gfx.Canvas, from gfx.ID, to gfx.Point) tabDrop {
		hit, ok := hitByID(canvas, from)
		if !ok {
			t.Fatalf("no control %v", from)
		}
		start := gfx.Point{X: hit.Bounds.X + hit.Bounds.Width/2, Y: hit.Bounds.Y + hit.Bounds.Height/2}
		press(state, from, start)
		dragTo(state, from, to)
		offered := state.tabDrag.drop
		release(state, from, to)
		return offered
	}
	// Visual order: A header, A, A logs, B, A other account.
	t.Run("a lone tab goes before a server group", func(t *testing.T) {
		state, ids, canvas := setup()
		header := state.verticalTabs.grouped.groups[0].id()
		hit, _ := hitByID(canvas, header)
		drag(state, canvas, hitTabList.At(int(ids[1])), gfx.Point{X: hit.Bounds.X + 20, Y: hit.Bounds.Y + 2})
		if got, want := tabOrder(state), []layoutcore.WorkspaceID{ids[1], ids[0], ids[2], ids[3]}; !reflect.DeepEqual(got, want) {
			t.Fatalf("order %v, want %v", got, want)
		}
	})
	t.Run("a lone tab goes after a server group", func(t *testing.T) {
		state, ids, canvas := setup()
		last, _ := hitByID(canvas, hitTabList.At(int(ids[2])))
		drag(state, canvas, hitTabList.At(int(ids[3])), gfx.Point{X: last.Bounds.X + 20, Y: last.Bounds.Y + last.Bounds.Height - 2})
		state.rebuildVerticalTabGroups(false)
		order := state.verticalTabs.grouped.order
		if order[0].ID != ids[0] || order[1].ID != ids[2] || order[2].ID != ids[3] || order[3].ID != ids[1] {
			t.Fatalf("visual order %v", order)
		}
	})
	t.Run("a whole group follows its header", func(t *testing.T) {
		state, ids, canvas := setup()
		header := state.verticalTabs.grouped.groups[0].id()
		bottom, _ := hitByID(canvas, hitTabList.At(int(ids[3])))
		drag(state, canvas, header, gfx.Point{X: bottom.Bounds.X + 20, Y: bottom.Bounds.Y + bottom.Bounds.Height - 2})
		if got, want := tabOrder(state), []layoutcore.WorkspaceID{ids[1], ids[3], ids[0], ids[2]}; !reflect.DeepEqual(got, want) {
			t.Fatalf("order %v, want %v", got, want)
		}
		if len(state.sessions) != 4 {
			t.Fatal("moving a group must not close connections")
		}
	})
	t.Run("a drop that leaves the order as it is changes nothing", func(t *testing.T) {
		state, ids, canvas := setup()
		drop := func(from gfx.ID, to gfx.Point) {
			if offered := drag(state, canvas, from, to); offered.kind != dropNone {
				t.Fatalf("a no-op drop was offered: %+v", offered)
			}
		}
		next, _ := hitByID(canvas, hitTabList.At(int(ids[1])))
		drop(hitTabList.At(int(ids[3])), gfx.Point{X: next.Bounds.X, Y: next.Bounds.Y + next.Bounds.Height - 2}) // just after B
		drop(hitTabList.At(int(ids[1])), gfx.Point{X: next.Bounds.X, Y: next.Bounds.Y + 2})                      // just before B
		if got := tabOrder(state); !reflect.DeepEqual(got, ids) {
			t.Fatalf("order %v", got)
		}
	})
	t.Run("a connection cannot join another server's group", func(t *testing.T) {
		state, ids, canvas := setup()
		inside, _ := hitByID(canvas, hitTabList.At(int(ids[2])))
		if offered := drag(state, canvas, hitTabList.At(int(ids[1])), gfx.Point{X: inside.Bounds.X + 20, Y: inside.Bounds.Y + 2}); offered.kind != dropNone {
			t.Fatalf("inside another server's group: %+v", offered)
		}
		other, _ := hitByID(canvas, hitTabList.At(int(ids[1])))
		if offered := drag(state, canvas, hitTabList.At(int(ids[2])), gfx.Point{X: other.Bounds.X + 20, Y: other.Bounds.Y + 2}); offered.kind != dropNone {
			t.Fatalf("a child of a group left it: %+v", offered)
		}
	})
}

// The grouped rows are a tree to the keyboard: the visible order is a header A, its two connections,
// then the lone connections B and A-other. The arrows stop at the ends instead of wrapping, the
// page keys jump by a viewport, and Right and Left open, enter, close and leave a group.
func TestVerticalTabGroupingKeysWalkTheRowsAsATree(t *testing.T) {
	state := restoredGroupedTabs(t, groupingEntries()...)
	ids := tabOrder(state)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 760})
	header := state.verticalTabs.grouped.groups[0].id()
	row := func(i int) gfx.ID { return hitTabList.At(int(ids[i])) }
	// Visible rows: header, ids[0], ids[2] (the group), ids[1], ids[3].
	order := []gfx.ID{header, row(0), row(2), row(1), row(3)}
	press := func(target gfx.ID, key gfx.Key) gfx.EventResult {
		t.Helper()
		return state.Key(gfx.KeyEvent{Target: target, Key: key})
	}
	if got := press(header, gfx.KeyRight); got.Focus != order[1] || !got.Handled {
		t.Errorf("Right on an open header focused %v, want its first connection", got.Focus)
	}
	if got := press(order[2], gfx.KeyLeft); got.Focus != header || !got.Handled {
		t.Errorf("Left on a connection focused %v, want its group's header", got.Focus)
	}
	if got := press(order[1], gfx.KeyRight); got.Focus != (gfx.ID{}) {
		t.Errorf("Right on a connection moved the focus to %v, want it to stay", got.Focus)
	}
	if got := press(order[3], gfx.KeyLeft); got.Focus != (gfx.ID{}) {
		t.Errorf("Left on a lone connection moved the focus to %v, want it to stay: it has no group", got.Focus)
	}
	for i := 0; i+1 < len(order); i++ {
		if got := press(order[i], gfx.KeyDown); got.Focus != order[i+1] {
			t.Errorf("Down from row %d focused %v, want row %d", i, got.Focus, i+1)
		}
		if got := press(order[i+1], gfx.KeyUp); got.Focus != order[i] {
			t.Errorf("Up from row %d focused %v, want row %d", i+1, got.Focus, i)
		}
	}
	// A tree does not wrap: the arrows stop at its ends.
	if got := press(header, gfx.KeyUp); got.Focus != (gfx.ID{}) {
		t.Errorf("Up from the first row moved the focus to %v, want it to stay", got.Focus)
	}
	if got := press(order[4], gfx.KeyDown); got.Focus != (gfx.ID{}) {
		t.Errorf("Down from the last row moved the focus to %v, want it to stay", got.Focus)
	}
	if got := press(order[2], gfx.KeyHome); got.Focus != header {
		t.Errorf("Home focused %v, want the first row", got.Focus)
	}
	if got := press(order[2], gfx.KeyEnd); got.Focus != order[4] {
		t.Errorf("End focused %v, want the last row", got.Focus)
	}
	// All five rows fit the strip, so a page is the whole list.
	if got := press(header, gfx.KeyPageDown); got.Focus != order[4] {
		t.Errorf("PageDown focused %v, want the last row of a short list", got.Focus)
	}
	if got := press(order[4], gfx.KeyPageUp); got.Focus != header {
		t.Errorf("PageUp focused %v, want the first row of a short list", got.Focus)
	}
}

// Left closes an open header and Right opens it again, keeping the keyboard on the header; a closed
// header's connections are not rows, so the arrows pass over them. Left on a closed header and Right
// on a lone connection change nothing.
func TestVerticalTabGroupingKeysCollapseAndExpandWithoutMovingTheFocus(t *testing.T) {
	state := restoredGroupedTabs(t, groupingEntries()...)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1200, Height: 760}
	state.Paint(&canvas, size)
	header := state.verticalTabs.grouped.groups[0].id()
	collapse := state.Key(gfx.KeyEvent{Target: header, Key: gfx.KeyLeft})
	if !collapse.Handled || collapse.Focus != header || !collapse.Repaint {
		t.Fatalf("Left on an open header = %+v, want it handled, repainted and still on the header", collapse)
	}
	canvas.Reset()
	state.Paint(&canvas, size)
	if rows := len(state.verticalTabs.grouped.rows); rows != 3 {
		t.Fatalf("a collapsed group left %d rows, want its header and the two lone connections", rows)
	}
	if again := state.Key(gfx.KeyEvent{Target: header, Key: gfx.KeyLeft}); again.Repaint || again.Focus != (gfx.ID{}) {
		t.Errorf("Left on a closed header = %+v, want nothing to happen", again)
	}
	expand := state.Key(gfx.KeyEvent{Target: header, Key: gfx.KeyRight})
	if !expand.Handled || expand.Focus != header || !expand.Repaint {
		t.Fatalf("Right on a closed header = %+v, want it opened with the focus kept", expand)
	}
	canvas.Reset()
	state.Paint(&canvas, size)
	if rows := len(state.verticalTabs.grouped.rows); rows != 5 {
		t.Errorf("an opened group shows %d rows, want 5", rows)
	}
}
