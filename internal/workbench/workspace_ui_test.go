package workbench

import (
	"slices"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

func TestManagementUsesSidebarAndTerminalUsesFullWorkspace(t *testing.T) {
	state := newTestState(t, testOptions(t))
	size := ui.Size{Width: 1200, Height: 800}
	var management ui.Canvas
	state.Paint(&management, size)
	hosts, ok := hitByID(&management, hitHosts)
	if !ok || hosts.Bounds.Width < 150 || !hosts.Selected {
		t.Fatalf("management hosts item = %+v", hosts)
	}

	openHost(state)
	state.sessions[0].term.live = true
	var terminal ui.Canvas
	state.Paint(&terminal, size)
	for _, id := range []ui.ID{hitHosts, hitKeychain, hitProxies, hitPortForwarding, hitKnownHosts, hitSettings} {
		if hit, ok := hitByID(&terminal, id); ok {
			t.Fatalf("terminal painted management navigation %d: %+v", id, hit)
		}
	}
	if bounds := state.workspacePixelBounds(size); bounds.X != 0 || bounds.Width != size.Width {
		t.Fatalf("terminal workspace = %+v, want full width", bounds)
	}
	terminalHit, ok := hitByID(&terminal, hitTerminalList.At(int(state.sessions[0].ID)))
	if !ok || terminalHit.Bounds.X != 0 || terminalHit.Bounds.Width != size.Width {
		t.Fatalf("single terminal does not use full workspace: %+v", terminalHit)
	}
}

func TestOnlyActiveSplitPanePaintsFullTerminalToolbar(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	for index := range state.sessions {
		state.sessions[index].term.live = true
	}
	size := ui.Size{Width: 1200, Height: 800}
	state.Resize(size)

	assertToolbarOwnership := func() {
		t.Helper()
		var canvas ui.Canvas
		state.Paint(&canvas, size)
		frame := state.workspacePixelBounds(size)
		for _, pane := range state.Panes(nil) {
			session, ok := state.sessionByIDPointer(pane.SessionID)
			if !ok {
				t.Fatalf("pane %d has no session", pane.ID)
			}
			_, hasFiles := hitByID(&canvas, tree.MountID(paneMount(session), hitSFTPOpen))
			_, hasClose := hitByID(&canvas, tree.MountID(paneMount(session), hitPaneClose))
			if hasFiles != pane.Active || hasClose != pane.Active {
				t.Fatalf("pane %d active=%v Files=%v Close=%v", pane.ID, pane.Active, hasFiles, hasClose)
			}
			barHeight := infoBarHeight // the same whether or not the pane is active
			bounds := pixelBounds(frame, pane.Bounds)
			if geometry := state.layoutForSession(session.ID); geometry.area.Y != bounds.Y+barHeight {
				t.Fatalf("pane %d terminal y=%v, want %v", pane.ID, geometry.area.Y, bounds.Y+barHeight)
			}
			cols, rows := session.term.screen.Size()
			geometry := state.layoutForSession(session.ID)
			if cols != geometry.cols || rows != geometry.rows {
				t.Fatalf("pane %d screen=%dx%d geometry=%dx%d", pane.ID, cols, rows, geometry.cols, geometry.rows)
			}
		}
	}

	assertToolbarOwnership()
	first := state.sessions[0].ID
	if !state.ActivatePane(first) {
		t.Fatal("activate first pane")
	}
	assertToolbarOwnership()
}

func TestMountedPaneCloseToolbarButtonClosesItsPane(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	closedSession := state.activeSessionID
	size := ui.Size{Width: 1200, Height: 800}
	driver, err := app.NewDriver(app.Config{
		Title: "pane close", Width: int(size.Width), Height: int(size.Height), Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)

	session, _ := state.sessionByIDPointer(closedSession)
	closeID := tree.MountID(paneMount(session), hitPaneClose)
	var closeHit ui.Hit
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == closeID {
			closeHit = hit
			break
		}
	}
	if closeHit.ID.IsZero() {
		t.Fatalf("active pane close hit %d is missing", closeID)
	}
	if err := driver.Click(closeHit.Bounds.X+closeHit.Bounds.Width/2, closeHit.Bounds.Y+closeHit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if _, present := state.sessionByIDPointer(closedSession); present || len(state.sessions) != 1 || len(state.Panes(nil)) != 1 {
		t.Fatalf("toolbar close left sessions=%d panes=%+v closed=%d present=%v", len(state.sessions), state.Panes(nil), closedSession, present)
	}
}

func TestWorkspacePanesPaintDistinctInteractiveViews(t *testing.T) {
	state := threeVisiblePaneState(t)
	size := ui.Size{Width: 1200, Height: 800}
	var canvas ui.Canvas
	state.Paint(&canvas, size)

	for _, id := range []ui.ID{hitPanePrevious, hitPaneNext, hitPaneSplitHorizontal, hitPaneSplitVertical, hitPaneClose} {
		if hit, ok := hitByID(&canvas, id); ok {
			t.Fatalf("pane action %d painted a floating control: %+v", id, hit)
		}
	}
	for index, split := range state.paneSplits {
		divider, ok := hitByID(&canvas, hitPaneDividerList.At(index))
		if !ok {
			t.Fatalf("pane divider %d is not hittable", index)
		}
		want := ui.PointerCursorResizeHorizontal
		if split.Axis == layoutcore.Vertical {
			want = ui.PointerCursorResizeVertical
		}
		if divider.Cursor != want {
			t.Fatalf("pane divider %d cursor = %v, want %v", index, divider.Cursor, want)
		}
	}

	frame := state.workspacePixelBounds(size)
	panes := state.Panes(state.panes[:0])
	for index := range panes {
		session, _ := state.sessionByIDPointer(panes[index].SessionID)
		activateID := hitPaneActivateList.At(int(session.paneSlot) - 1)
		activate, ok := hitByID(&canvas, activateID)
		if !ok || activate.Bounds != pixelBounds(frame, panes[index].Bounds) {
			t.Fatalf("pane %d activation hit = %+v", index, activate)
		}
		terminalID := hitTerminalList.At(int(session.ID))
		terminalID = tree.MountID(paneMount(session), terminalID)
		terminal, ok := hitByID(&canvas, terminalID)
		if !ok {
			t.Fatalf("pane %d terminal hit %d is missing", index, terminalID)
		}
		paneBounds := pixelBounds(frame, panes[index].Bounds)
		if terminal.Bounds.X < paneBounds.X || terminal.Bounds.Y < paneBounds.Y ||
			terminal.Bounds.X+terminal.Bounds.Width > paneBounds.X+paneBounds.Width ||
			terminal.Bounds.Y+terminal.Bounds.Height > paneBounds.Y+paneBounds.Height {
			t.Fatalf("terminal bounds %+v escape pane %+v", terminal.Bounds, paneBounds)
		}
		if !panes[index].Active && terminal.Autofocus {
			t.Fatalf("background pane %d requested autofocus", index)
		}
	}
}

func TestWorkspacePaintsEverySessionPageInsideItsPane(t *testing.T) {
	state := threeVisiblePaneState(t)
	openHostInSplit(t, state, 1, layoutcore.Horizontal)
	for index := range state.sessions {
		state.sessions[index].term.live = true
	}
	state.sessions[1].restored = true
	state.sessions[1].term = nil
	state.sessions[2].files = &sftpView{sftpFolder: sftpFolder{path: "/"}}
	if err := state.deck.SetContext(state.deck.ActiveWorkspace(), layoutcore.ContextState{Kind: layoutcore.ContextSFTP, Open: true, Width: layoutcore.DefaultContextWidth}); err != nil {
		t.Fatal(err)
	}
	state.sessions[3].term.live = false
	state.sessions[3].term.shell = nil
	state.ActivatePane(state.sessions[2].ID)

	size := ui.Size{Width: 1400, Height: 900}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	assertTextInsidePane(t, state, &canvas, size, state.sessions[1].ID, state.messages.SessionRestoredTitle)
	assertTextInsidePane(t, state, &canvas, size, state.sessions[3].ID, state.messages.HostStateConnecting)
	foundSFTP := false
	for _, text := range canvas.Texts() {
		if text.Value == state.messages.SFTPFiles {
			foundSFTP = true
		}
	}
	if !foundSFTP {
		t.Fatal("active pane SFTP context panel is missing")
	}

	terminal := &state.sessions[0]
	terminalID := tree.MountID(paneMount(terminal), hitTerminalList.At(int(terminal.ID)))
	if _, ok := hitByID(&canvas, terminalID); !ok {
		t.Fatalf("terminal page hit %d is missing", terminalID)
	}
	if _, ok := hitByID(&canvas, contextID(hitSFTPClose)); !ok {
		t.Fatal("SFTP context panel controls are missing")
	}
}

func TestWorkspacePaneActionsSplitActivateAndClose(t *testing.T) {
	state := threeVisiblePaneState(t)
	first := state.sessions[0].ID
	state.ActivatePane(first)

	state.Click(hitPaneSplitHorizontal)
	panes := state.Panes(nil)
	if len(panes) != 4 || panes[1].SessionID == 0 || !panes[1].Active || len(state.sessions) != 4 {
		t.Fatalf("split right panes = %+v sessions=%d", panes, len(state.sessions))
	}
	if opened, source := state.sessions[3], state.sessions[0]; opened.HostID != source.HostID || opened.host.Address != source.host.Address {
		t.Fatalf("the split opened %+v, want the host of %+v", opened.host, source.host)
	}
	state.Click(hitPaneClose)
	if len(state.sessions) != 3 || len(state.Panes(nil)) != 3 {
		t.Fatalf("closing the split pane left sessions=%d panes=%+v", len(state.sessions), state.Panes(nil))
	}

	active := state.activeSessionID
	state.Click(hitPaneNext)
	if state.activeSessionID == active {
		t.Fatal("next pane did not activate another session")
	}
	state.Click(hitPanePrevious)
	if state.activeSessionID != active {
		t.Fatalf("previous pane active = %d, want %d", state.activeSessionID, active)
	}

	_, layout := state.activeWorkspaceLayout()
	if err := layout.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceBackgroundPointerActivatesPane(t *testing.T) {
	state := threeVisiblePaneState(t)
	size := ui.Size{Width: 1200, Height: 800}
	var canvas ui.Canvas
	state.Paint(&canvas, size)

	background := state.sessions[0].ID
	if background == state.activeSessionID {
		background = state.sessions[1].ID
	}
	session, _ := state.sessionByIDPointer(background)
	target := tree.MountID(paneMount(session), hitTerminalList.At(int(session.ID)))
	hit, ok := hitByID(&canvas, target)
	if !ok {
		t.Fatalf("background terminal hit %d is missing", target)
	}
	result := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: target,
		X: hit.Bounds.X + 10, Y: hit.Bounds.Y + 10, Viewport: size,
	})
	if state.activeSessionID != background || !result.Handled || !result.Repaint {
		t.Fatalf("activation active=%d result=%+v", state.activeSessionID, result)
	}
}

func TestWorkspaceBackgroundTerminalKeepsFocusThroughActivation(t *testing.T) {
	state := threeVisiblePaneState(t)
	size := ui.Size{Width: 1200, Height: 800}
	driver, err := app.NewDriver(app.Config{
		Title: "panes", Width: int(size.Width), Height: int(size.Height),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
		OnScroll: state.Scroll, OnResize: state.Resize,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if context.Terminal {
				return state.TerminalInput(context)
			}
			return nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)

	background := &state.sessions[0]
	if background.ID == state.activeSessionID {
		background = &state.sessions[1]
	}
	target := tree.MountID(paneMount(background), hitTerminalList.At(int(background.ID)))
	var terminal ui.Hit
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == target {
			terminal = hit
			break
		}
	}
	if terminal.ID.IsZero() {
		t.Fatalf("background terminal hit %d is missing", target)
	}
	if err := driver.Press(terminal.Bounds.X+12, terminal.Bounds.Y+terminal.Bounds.Height-12, ui.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if state.activeSessionID != background.ID || driver.Snapshot().Focused != target {
		t.Fatalf("active=%d focus=%d, want %d/%d", state.activeSessionID, driver.Snapshot().Focused, background.ID, target)
	}
}

func TestNativeTextInputOnlyTargetsTheActivePane(t *testing.T) {
	state, world, _, firstShell := liveSession(t)
	if !state.splitActivePane(layoutcore.Horizontal) {
		t.Fatal("split right")
	}
	state.Click(hitPaneChooseHost)
	state.Click(hitHostList.At(2))
	openHost(state)
	secondConn := world.conns[1]
	secondConn.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	secondShell := secondConn.shell(t)
	secondShell.say("ready")
	state.Wake()

	first := &state.sessions[0]
	second := &state.sessions[1]
	firstTarget := tree.MountID(paneMount(first), hitTerminalList.At(int(first.ID)))
	secondTarget := tree.MountID(paneMount(second), hitTerminalList.At(int(second.ID)))
	if input := state.TerminalInput(app.TextInputContext{ID: firstTarget, Terminal: true}); input != nil {
		t.Fatal("inactive pane exposed a native text receiver")
	}
	input := state.TerminalInput(app.TextInputContext{ID: secondTarget, Terminal: true})
	if input == nil {
		t.Fatal("active pane has no native text receiver")
	}
	if _, err := input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if firstShell.typed() != "" || secondShell.typed() != "x" {
		t.Fatalf("typed first=%q second=%q", firstShell.typed(), secondShell.typed())
	}
}

func TestSFTPContextFollowsActivePaneWithoutReusingAnotherSession(t *testing.T) {
	state := threeVisiblePaneState(t)
	first := &state.sessions[0]
	second := &state.sessions[1]
	first.files = &sftpView{sftpFolder: sftpFolder{path: "/first"}}
	if err := state.deck.SetContext(state.deck.ActiveWorkspace(), layoutcore.ContextState{Kind: layoutcore.ContextSFTP, Open: true, Width: 340}); err != nil {
		t.Fatal(err)
	}
	state.ActivatePane(second.ID)
	size := ui.Size{Width: 1400, Height: 900}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	// The files panel is on the left, ahead of the panes.
	panes := state.workspacePixelBounds(size)
	if panes.X <= 340 {
		t.Fatalf("panes start at %v, want them after the 340 wide files panel", panes.X)
	}
	foundTarget := false
	for _, text := range canvas.Texts() {
		if text.Value == second.Title && text.Bounds.X < panes.X {
			foundTarget = true
		}
	}
	if !foundTarget {
		t.Fatal("context panel did not identify the active pane session")
	}
	if _, ok := hitByID(&canvas, contextID(hitSFTPEntryList.At(0))); ok {
		t.Fatal("context panel reused the inactive pane's SFTP rows")
	}
}

func TestContextPanelCollapseRemovesGapAndNarrowWindowUsesDrawer(t *testing.T) {
	state, _, _, _ := openTestSFTP(t)
	wide := ui.Size{Width: 1400, Height: 900}
	openBounds := state.workspacePixelBounds(wide)
	state.Click(hitSFTPClose)
	closedBounds := state.workspacePixelBounds(wide)
	if closedBounds != state.baseWorkspacePixelBounds(wide) || closedBounds.Width <= openBounds.Width {
		t.Fatalf("collapsed bounds open=%+v closed=%+v base=%+v", openBounds, closedBounds, state.baseWorkspacePixelBounds(wide))
	}
	var closed ui.Canvas
	state.Paint(&closed, wide)
	if _, ok := hitByID(&closed, hitContextDivider); ok {
		t.Fatal("collapsed context left a divider or gap")
	}

	state.Click(hitSFTPOpen)
	narrow := ui.Size{Width: 800, Height: 700}
	var drawer ui.Canvas
	state.Paint(&drawer, narrow)
	if state.workspacePixelBounds(narrow) != state.baseWorkspacePixelBounds(narrow) {
		t.Fatalf("drawer reserved terminal width: %+v", state.workspacePixelBounds(narrow))
	}
	if drawer.TrappedFocusScope() != dialogScope {
		t.Fatalf("drawer focus scope = %d", drawer.TrappedFocusScope())
	}
	if _, ok := hitByID(&drawer, contextID(hitSFTPClose)); !ok {
		t.Fatal("drawer SFTP content is missing")
	}
}

// TestHoveringTheSFTPDrawerBackdropLeavesItsCloseButtonAlone is the drawer's
// half of the rule the dialog follows: the backdrop that dismisses the drawer
// and its corner button are separate targets, so the hover of the one must not
// reach the other.
func TestHoveringTheSFTPDrawerBackdropLeavesItsCloseButtonAlone(t *testing.T) {
	state, _, _, _ := openTestSFTP(t)
	size := ui.Size{Width: 800, Height: 700}
	var quiet ui.Canvas
	state.Paint(&quiet, size)
	dismiss := ui.ID{}
	for _, hit := range quiet.Hits() {
		if hit.Backdrop {
			dismiss = hit.ID
			break
		}
	}
	if dismiss.IsZero() {
		t.Fatal("the drawer has no backdrop to hover")
	}
	paintedWith := func(hovered ui.ID) []ui.Rectangle {
		var canvas ui.Canvas
		canvas.SetInteraction(hovered, ui.ID{}, ui.ID{}, false)
		state.Paint(&canvas, size)
		return dialogLayerRectangles(&canvas)
	}
	if !slices.Equal(paintedWith(ui.ID{}), paintedWith(dismiss)) {
		t.Fatal("hovering the backdrop paints the drawer's controls as hovered")
	}
	// The drawer's own button closes the panel, as the panel's does.
	state.Click(hitSFTPDrawerClose)
	if context, _ := state.deck.Context(state.deck.ActiveWorkspace()); context.Open {
		t.Fatal("the drawer's close button left the panel open")
	}
}

func TestContextDividerPersistsWidthAndRestoresTerminalFocus(t *testing.T) {
	state, _, _, _ := openTestSFTP(t)
	size := ui.Size{Width: 1400, Height: 900}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	divider, ok := hitByID(&canvas, hitContextDivider)
	if !ok {
		t.Fatal("context divider is missing")
	}
	if divider.Cursor != ui.PointerCursorResizeHorizontal {
		t.Fatalf("context divider cursor = %v", divider.Cursor)
	}
	base := state.baseWorkspacePixelBounds(size)
	dragX := base.X + 400 + 0.5
	y := divider.Bounds.Y + divider.Bounds.Height/2
	state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hitContextDivider, X: divider.Bounds.X, Y: y, Viewport: size})
	move := state.Pointer(ui.PointerEvent{Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Target: hitContextDivider, Captured: true, X: dragX, Y: y, Viewport: size})
	release := state.Pointer(ui.PointerEvent{Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Target: hitContextDivider, Captured: true, X: dragX, Y: y, Viewport: size})
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	if !move.Repaint || context.Width < 399 || context.Width > 401 || release.Focus != state.activeTerminalFocusHit() {
		t.Fatalf("drag move=%+v release=%+v context=%+v", move, release, context)
	}
	restored := New(state.options)
	t.Cleanup(restored.Close)
	restoredContext, ok := restored.deck.Context(restored.deck.ActiveWorkspace())
	if !ok || restoredContext.Open != state.sftpPanelAutoOpen || restoredContext.Kind != layoutcore.ContextSFTP || restoredContext.Width != context.Width {
		t.Fatalf("restored context = %+v, %v; want %+v", restoredContext, ok, context)
	}
}

func TestWorkspaceDividerDragResizesTerminalsAndPersists(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	for index := range state.sessions {
		state.sessions[index].term.live = true
	}
	size := ui.Size{Width: 1000, Height: 700}
	state.Resize(size)
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	builds := state.workspaceViewBuilds
	divider, ok := hitByID(&canvas, hitPaneDividerList.At(0))
	if !ok {
		t.Fatal("divider hit missing")
	}
	y := divider.Bounds.Y + divider.Bounds.Height/2
	frame := state.workspacePixelBounds(size)
	dragX := frame.X + 0.5 + (frame.Width-1)*0.70
	state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hitPaneDividerList.At(0), X: divider.Bounds.X + 2, Y: y, Viewport: size})
	move := state.Pointer(ui.PointerEvent{Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Target: hitPaneDividerList.At(0), Captured: true, X: dragX, Y: y, Viewport: size})
	beforeRelease := newTestState(t, options).PaneSplits(nil)
	if len(beforeRelease) != 1 || beforeRelease[0].Ratio != defaultPaneRatio {
		t.Fatalf("divider move persisted before pointer up: %+v", beforeRelease)
	}
	state.Pointer(ui.PointerEvent{Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Target: hitPaneDividerList.At(0), Captured: true, X: dragX, Y: y, Viewport: size})
	if !move.Handled || !move.Repaint {
		t.Fatalf("divider move = %+v", move)
	}
	if state.workspaceViewBuilds != builds {
		t.Fatalf("splitter drag rebuilt the complete workspace view: %d -> %d", builds, state.workspaceViewBuilds)
	}

	splits := state.PaneSplits(nil)
	if len(splits) != 1 || splits[0].Axis != layoutcore.Horizontal || splits[0].Ratio < 0.69 || splits[0].Ratio > 0.71 {
		t.Fatalf("dragged splits = %+v", splits)
	}
	for index := range state.sessions {
		geometry := state.layoutForSession(state.sessions[index].ID)
		cols, rows := state.sessions[index].term.screen.Size()
		if cols != geometry.cols || rows != geometry.rows {
			t.Fatalf("session %d screen=%dx%d geometry=%dx%d", index, cols, rows, geometry.cols, geometry.rows)
		}
	}

	restored := newTestState(t, options)
	restoredSplits := restored.PaneSplits(nil)
	if len(restoredSplits) != 1 || restoredSplits[0].Ratio != splits[0].Ratio {
		t.Fatalf("restored splits = %+v, want %+v", restoredSplits, splits)
	}
}

func TestNestedWorkspaceDividerPreservesEveryLeafMinimumSize(t *testing.T) {
	tests := []struct {
		name string
		axis layoutcore.Axis
		size ui.Size
	}{
		{name: "columns", axis: layoutcore.Horizontal, size: ui.Size{Width: 1400, Height: 900}},
		{name: "rows", axis: layoutcore.Vertical, size: ui.Size{Width: 1000, Height: 900}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			openHost(state)
			openHostInSplit(t, state, 2, test.axis)
			openHostInSplit(t, state, 3, test.axis)
			for index := range state.sessions {
				state.sessions[index].term.live = true
			}
			state.Resize(test.size)
			var canvas ui.Canvas
			state.Paint(&canvas, test.size)
			layout, _ := state.deck.Layout(state.deck.ActiveWorkspace())
			rootIndex := -1
			for index, split := range state.paneSplits {
				node, _ := layout.Node(split.NodeID)
				if node.Parent == 0 {
					rootIndex = index
					break
				}
			}
			if rootIndex < 0 {
				t.Fatal("root splitter is missing")
			}
			id := hitPaneDividerList.At(rootIndex)
			divider, ok := hitByID(&canvas, id)
			if !ok {
				t.Fatal("root splitter hit is missing")
			}
			downX, downY := divider.Bounds.X+divider.Bounds.Width/2, divider.Bounds.Y+divider.Bounds.Height/2
			moveX, moveY := downX, downY
			frame := state.workspacePixelBounds(test.size)
			if test.axis == layoutcore.Horizontal {
				moveX = frame.X + frame.Width + 100
			} else {
				moveY = frame.Y + frame.Height + 100
			}
			state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: id, X: downX, Y: downY, Viewport: test.size})
			move := state.Pointer(ui.PointerEvent{Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Target: id, Captured: true, X: moveX, Y: moveY, Viewport: test.size})
			state.Pointer(ui.PointerEvent{Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Target: id, Captured: true, X: moveX, Y: moveY, Viewport: test.size})
			if !move.Repaint {
				t.Fatal("extreme splitter drag did not resize")
			}
			for _, pane := range state.Panes(nil) {
				bounds := pixelBounds(frame, pane.Bounds)
				if bounds.Width+0.01 < minPaneWidth || bounds.Height+0.01 < minPaneHeight {
					t.Fatalf("pane %d became unusable: %+v", pane.ID, bounds)
				}
			}
			if state.splitActivePane(test.axis) {
				t.Fatal("a minimum-sized pane was split again")
			}
		})
	}
}

func TestWorkspaceDividerDragDoesNotResizeBackgroundWorkspace(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.sessions = []Session{
		{ID: 1, Title: "One", Address: "one", term: newTerminal(80, 24)},
		{ID: 2, Title: "Two", Address: "two", term: newTerminal(80, 24)},
		{ID: 3, Title: "Three", Address: "three", term: newTerminal(80, 24)},
	}
	for index := range state.sessions {
		state.sessions[index].term.live = true
		state.sessions[index].term.shell = &fakeShell{}
	}
	firstWorkspace, firstPane, _ := state.deck.Create("First")
	if err := state.deck.Bind(firstWorkspace, firstPane, 1); err != nil {
		t.Fatal(err)
	}
	secondPane, err := state.deck.Split(firstWorkspace, firstPane, layoutcore.Horizontal)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.deck.Bind(firstWorkspace, secondPane, 2); err != nil {
		t.Fatal(err)
	}
	backgroundWorkspace, backgroundPane, _ := state.deck.Create("Background")
	if err := state.deck.Bind(backgroundWorkspace, backgroundPane, 3); err != nil {
		t.Fatal(err)
	}
	if err := state.deck.SetContext(backgroundWorkspace, layoutcore.ContextState{Kind: layoutcore.ContextSFTP, Open: true, Width: 400}); err != nil {
		t.Fatal(err)
	}
	state.deck.ActivateWorkspace(firstWorkspace)
	state.deck.ActivatePane(firstWorkspace, firstPane)
	state.syncActiveSession()
	state.activeSection = sectionTerminal

	size := ui.Size{Width: 1200, Height: 800}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	state.Resize(size)
	backgroundCols, backgroundRows := state.sessions[2].term.screen.Size()
	backgroundShell := state.sessions[2].term.shell.(*fakeShell)
	backgroundResizes := len(backgroundShell.resizes)
	canvas.Reset()
	state.Paint(&canvas, size)
	divider, ok := hitByID(&canvas, hitPaneDividerList.At(0))
	if !ok {
		t.Fatal("active workspace divider missing")
	}
	y := divider.Bounds.Y + divider.Bounds.Height/2
	frame := state.workspacePixelBounds(size)
	dragX := frame.X + 0.5 + (frame.Width-1)*0.65
	state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hitPaneDividerList.At(0), X: divider.Bounds.X, Y: y, Viewport: size})
	state.Pointer(ui.PointerEvent{Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Target: hitPaneDividerList.At(0), Captured: true, X: dragX, Y: y, Viewport: size})
	if cols, rows := state.sessions[2].term.screen.Size(); cols != backgroundCols || rows != backgroundRows || len(backgroundShell.resizes) != backgroundResizes {
		t.Fatalf("background workspace resized from %dx%d/%d to %dx%d/%d", backgroundCols, backgroundRows, backgroundResizes, cols, rows, len(backgroundShell.resizes))
	}
}

func TestWorkspaceMultiPaneSteadyPaintDoesNotAllocate(t *testing.T) {
	state := threeVisiblePaneState(t)
	size := ui.Size{Width: 1400, Height: 900}
	var canvas ui.Canvas
	for range 3 {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
	allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("multi-pane steady paint allocations = %v", allocations)
	}
}

func TestTerminalOutputReusesCachedWorkspaceGeometry(t *testing.T) {
	state := threeVisiblePaneState(t)
	size := ui.Size{Width: 1400, Height: 900}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	builds := state.workspaceViewBuilds
	state.sessions[0].term.screen.Write([]byte("output"))
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.workspaceViewBuilds != builds {
		t.Fatalf("terminal output rebuilt workspace geometry: %d -> %d", builds, state.workspaceViewBuilds)
	}
}

func BenchmarkPaintWorkspacePanes(b *testing.B) {
	state := threeVisiblePaneState(b)
	size := ui.Size{Width: 1400, Height: 900}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	b.ReportAllocs()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

func BenchmarkWorkspaceSplitterDrag(b *testing.B) {
	state := threeVisiblePaneState(b)
	size := ui.Size{Width: 1400, Height: 900}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	divider, ok := hitByID(&canvas, hitPaneDividerList.At(0))
	if !ok {
		b.Fatal("divider hit missing")
	}
	y := divider.Bounds.Y + divider.Bounds.Height/2
	state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hitPaneDividerList.At(0), X: divider.Bounds.X, Y: y, Viewport: size})
	frame := state.workspacePixelBounds(size)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; b.Loop(); index++ {
		ratio := float32(0.45)
		if index&1 != 0 {
			ratio = 0.55
		}
		x := frame.X + 0.5 + (frame.Width-1)*ratio
		state.Pointer(ui.PointerEvent{Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Target: hitPaneDividerList.At(0), Captured: true, X: x, Y: y, Viewport: size})
	}
}

func BenchmarkContextPanelDividerDrag(b *testing.B) {
	state := threeVisiblePaneState(b)
	if err := state.deck.SetContext(state.deck.ActiveWorkspace(), layoutcore.ContextState{Kind: layoutcore.ContextSFTP, Open: true, Width: 340}); err != nil {
		b.Fatal(err)
	}
	size := ui.Size{Width: 1400, Height: 900}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	divider, ok := hitByID(&canvas, hitContextDivider)
	if !ok {
		b.Fatal("context divider hit missing")
	}
	y := divider.Bounds.Y + divider.Bounds.Height/2
	state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hitContextDivider, X: divider.Bounds.X, Y: y, Viewport: size})
	base := state.baseWorkspacePixelBounds(size)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; b.Loop(); index++ {
		width := float32(330)
		if index&1 != 0 {
			width = 350
		}
		x := base.X + width + 0.5
		state.Pointer(ui.PointerEvent{Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Target: hitContextDivider, Captured: true, X: x, Y: y, Viewport: size})
	}
}

func threeVisiblePaneState(t testing.TB) *State {
	t.Helper()
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	openHostInSplit(t, state, 3, layoutcore.Vertical)
	for index := range state.sessions {
		state.sessions[index].term.live = true
	}
	state.fitTerminals()
	return state
}

func openHostInSplit(t testing.TB, state *State, hostID uint32, axis layoutcore.Axis) {
	t.Helper()
	if !state.splitActivePane(axis) {
		t.Fatal("could not create an empty split pane")
	}
	state.Click(hitPaneChooseHost)
	state.Click(hitHostList.At(int(hostID)))
	openHost(state)
}

func assertTextInsidePane(t testing.TB, state *State, canvas *ui.Canvas, size ui.Size, sessionID uint32, value string) {
	t.Helper()
	pane, ok := state.paneForSession(sessionID)
	if !ok {
		t.Fatalf("session %d has no pane", sessionID)
	}
	bounds := pixelBounds(state.workspacePixelBounds(size), pane.Bounds)
	found := false
	for _, text := range canvas.Texts() {
		if text.Value != value {
			continue
		}
		found = true
		if text.Bounds.X >= bounds.X && text.Bounds.Y >= bounds.Y && text.Bounds.X < bounds.X+bounds.Width && text.Bounds.Y < bounds.Y+bounds.Height {
			return
		}
	}
	if found {
		t.Fatalf("text %q is painted, but not inside pane %+v", value, bounds)
	}
	t.Fatalf("text %q is not painted", value)
}

// A pane becomes the active one when it is pressed. Hovering it, moving over it
// or wheeling over it does not take the focus from the pane the user is in.
func TestPanesActivateOnPressNotOnHover(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	for index := range state.sessions {
		state.sessions[index].term.live = true
	}
	size := ui.Size{Width: 1200, Height: 800}
	state.Resize(size)
	state.Paint(&ui.Canvas{}, size)
	active := state.activeSessionID
	var other *Session
	for index := range state.sessions {
		if state.sessions[index].ID != active {
			other = &state.sessions[index]
		}
	}
	target := tree.MountID(paneMount(other), hitTerminalList.At(int(other.ID)))
	state.Pointer(ui.PointerEvent{Kind: ui.PointerMove, Target: target, X: 20, Y: 80, Viewport: size})
	state.Scroll(ui.ScrollEvent{Target: target, X: 20, Y: 80, DeltaY: 40, Viewport: size})
	if state.activeSessionID != active {
		t.Fatalf("hover or wheel moved the active pane to %d", state.activeSessionID)
	}
	state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: target, X: 20, Y: 80, Viewport: size})
	if state.activeSessionID != other.ID {
		t.Fatalf("pressing the pane left %d active, want %d", state.activeSessionID, other.ID)
	}
}

// Splitting an empty pane has no host to repeat: the new pane stays empty for
// the user to choose one.
func TestSplittingAnEmptyPaneLeavesTheNewPaneEmpty(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	state.Click(hitPaneSplitHorizontal) // a second shell on the same host
	state.Click(hitPaneClose)
	openEmptyTab(state)
	state.Click(hitPaneSplitHorizontal)
	panes := state.Panes(nil)
	if len(panes) != 2 || panes[0].SessionID != 0 || panes[1].SessionID != 0 {
		t.Fatalf("panes = %+v", panes)
	}
}

// A split opens a second shell on a host that is already working, so its pane
// shows the terminal straight away rather than the connection page; the page
// comes back when the connection needs the user or fails.
func TestSplitPaneConnectsBehindItsTerminal(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)
	state.Click(hitPaneSplitHorizontal)
	if len(world.conns) != 2 {
		t.Fatalf("the split dialed %d connections, want a second", len(world.conns))
	}
	opened, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || opened.phase != sshconn.PhaseConnecting {
		t.Fatalf("split session = %+v, %v", opened, ok)
	}
	if state.showsProgress(opened) {
		t.Fatal("a pane of a split shows the progress page")
	}

	fake := world.conns[1]
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseHostKeyPrompt })
	state.Wake()
	if !state.showsProgress(opened) {
		t.Fatal("a connection waiting for the user must show its page")
	}

	// An ordinary new tab keeps its page throughout. (The host-key prompt above
	// is still up, so only the pointer reaches the host row.)
	openHost(state)
	tab, _ := state.sessionByIDPointer(state.activeSessionID)
	if !state.showsProgress(tab) {
		t.Fatal("a new tab lost its progress page")
	}
}
