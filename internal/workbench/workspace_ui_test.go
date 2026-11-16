package workbench

import (
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

func TestClosingSplitRestoresPopulatedTerminal(t *testing.T) {
	for _, test := range []struct {
		name  string
		axis  layoutcore.Axis
		empty bool
	}{
		{"right session", layoutcore.Horizontal, false},
		{"down session", layoutcore.Vertical, false},
		{"right empty", layoutcore.Horizontal, true},
		{"down empty", layoutcore.Vertical, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			openHost(state)
			state.cellWidth, state.cellHeight = 8, 16
			size := gfx.Size{Width: 1200, Height: 800}
			state.Resize(size)
			originalID := state.sessions[0].ID
			terminal := state.sessions[0].term
			shell := &fakeShell{done: make(chan struct{})}
			terminal.shell, terminal.live = shell, true
			state.fitTerminal(&state.sessions[0])
			terminal.screen.Write([]byte(strings.Repeat("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz--RIGHT-EDGE-END\r\n", 80) + "prompt> "))
			before := string(terminal.screen.AppendRetainedText(nil))
			cols, rows := terminal.screen.Size()
			if test.empty {
				if !state.splitActivePane(test.axis) {
					t.Fatal("split did not open")
				}
			} else if test.axis == layoutcore.Horizontal {
				state.Click(hitPaneSplitHorizontal)
			} else {
				state.Click(hitPaneSplitVertical)
			}
			if len(state.Panes(nil)) != 2 {
				t.Fatal("split did not produce two panes")
			}
			state.Click(hitPaneClose)
			if len(state.Panes(nil)) != 1 || state.activeSessionID != originalID {
				t.Fatal("closing the new pane did not restore the original session")
			}
			if c, r := terminal.screen.Size(); c != cols || r != rows {
				t.Fatalf("terminal stayed at %dx%d, want %dx%d", c, r, cols, rows)
			}
			shell.mu.Lock()
			lastResize := shell.resizes[len(shell.resizes)-1]
			shell.mu.Unlock()
			if lastResize != [2]int{cols, rows} {
				t.Fatalf("shell stayed at %v, want %dx%d", lastResize, cols, rows)
			}
			if after := string(terminal.screen.AppendRetainedText(nil)); after != before {
				t.Fatal("split/close permanently changed existing output")
			}
			var canvas gfx.Canvas
			state.Paint(&canvas, size)
			hit, ok := hitByID(&canvas, terminalTestHitByID(state, originalID))
			if !ok || hit.Bounds.Width != 1200-terminalToolRailExtent {
				t.Fatalf("restored terminal hit = %+v", hit)
			}
		})
	}
}

func TestManagementUsesSidebarAndTerminalUsesFullWorkspace(t *testing.T) {
	state := newTestState(t, testOptions(t))
	size := gfx.Size{Width: 1200, Height: 800}
	var management gfx.Canvas
	state.Paint(&management, size)
	hosts, ok := hitByID(&management, hitHosts)
	if !ok || hosts.Bounds.Width < 150 || !hosts.Selected {
		t.Fatalf("management hosts item = %+v", hosts)
	}

	openHost(state)
	state.sessions[0].term.live = true
	var terminal gfx.Canvas
	state.Paint(&terminal, size)
	for _, id := range []gfx.ID{hitHosts, hitKeychain, hitProxies, hitPortForwarding, hitKnownHosts, hitSettings} {
		if hit, ok := hitByID(&terminal, id); ok {
			t.Fatalf("terminal painted management navigation %d: %+v", id, hit)
		}
	}
	if bounds := state.workspacePixelBounds(size); bounds.X != 0 || bounds.Width != size.Width-terminalToolRailExtent {
		t.Fatalf("terminal workspace = %+v, want room for the shared right rail", bounds)
	}
	terminalHit, ok := hitByID(&terminal, terminalTestHit(state, &state.sessions[0]))
	if !ok || terminalHit.Bounds.X != 0 || terminalHit.Bounds.Width != size.Width-terminalToolRailExtent {
		t.Fatalf("single terminal does not use full workspace: %+v", terminalHit)
	}
}

func TestSplitPanesShareOneWorkspaceToolbar(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	for index := range state.sessions {
		state.sessions[index].term.live = true
	}
	size := gfx.Size{Width: 1200, Height: 800}
	state.Resize(size)

	assertToolbarOwnership := func() {
		t.Helper()
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		frame := state.workspacePixelBounds(size)
		if _, ok := hitByID(&canvas, hitSFTPOpen); !ok {
			t.Fatal("shared toolbar Files control is missing")
		}
		for _, pane := range state.Panes(nil) {
			session, ok := state.sessionByIDPointer(pane.SessionID)
			if !ok {
				t.Fatalf("pane %d has no session", pane.ID)
			}
			_, hasFiles := hitByID(&canvas, tree.MountID(paneMount(session), hitSFTPOpen))
			_, hasClose := hitByID(&canvas, tree.MountID(paneMount(session), hitPaneClose))
			if hasFiles || hasClose {
				t.Fatalf("pane %d active=%v Files=%v Close=%v", pane.ID, pane.Active, hasFiles, hasClose)
			}
			bounds := pixelBounds(frame, pane.Bounds)
			geometry := state.layoutForSession(session.ID)
			wantY, wantWidth := bounds.Y+infoBarHeight, bounds.Width
			if geometry.area.Y != wantY || geometry.area.Width != wantWidth || geometry.area.X != bounds.X {
				t.Fatalf("pane %d active=%v terminal area=%+v, want x=%v y=%v width=%v", pane.ID, pane.Active, geometry.area, bounds.X, wantY, wantWidth)
			}
			cols, rows := session.term.screen.Size()
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

func TestSharedToolbarCloseButtonClosesActivePane(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	closedSession := state.activeSessionID
	size := gfx.Size{Width: 1200, Height: 800}
	driver, err := app.NewDriver(app.Config{
		Title: "pane close", Width: int(size.Width), Height: int(size.Height), Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)

	var closeHit gfx.Hit
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == hitPaneClose {
			closeHit = hit
			break
		}
	}
	if closeHit.ID.IsZero() {
		t.Fatal("shared toolbar close hit is missing")
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
	size := gfx.Size{Width: 1200, Height: 800}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	for _, id := range []gfx.ID{hitPaneSplitHorizontal, hitPaneSplitVertical, hitPaneClose} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Fatalf("shared toolbar action %d is missing", id)
		}
	}
	for index, split := range state.paneSplits {
		divider, ok := hitByID(&canvas, hitPaneDividerList.At(index))
		if !ok {
			t.Fatalf("pane divider %d is not hittable", index)
		}
		want := gfx.PointerCursorResizeHorizontal
		if split.Axis == layoutcore.Vertical {
			want = gfx.PointerCursorResizeVertical
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
		terminalID := terminalTestHit(state, session)
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

	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
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
	terminalID := terminalTestHit(state, terminal)
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
	size := gfx.Size{Width: 1200, Height: 800}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	background := state.sessions[0].ID
	if background == state.activeSessionID {
		background = state.sessions[1].ID
	}
	session, _ := state.sessionByIDPointer(background)
	target := terminalTestHit(state, session)
	hit, ok := hitByID(&canvas, target)
	if !ok {
		t.Fatalf("background terminal hit %d is missing", target)
	}
	result := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: target,
		X: hit.Bounds.X + 10, Y: hit.Bounds.Y + 10, Viewport: size,
	})
	if state.activeSessionID != background || !result.Handled || !result.Repaint {
		t.Fatalf("activation active=%d result=%+v", state.activeSessionID, result)
	}
}

func TestWorkspaceBackgroundTerminalKeepsFocusThroughActivation(t *testing.T) {
	state := threeVisiblePaneState(t)
	size := gfx.Size{Width: 1200, Height: 800}
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
	target := terminalTestHit(state, background)
	var terminal gfx.Hit
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == target {
			terminal = hit
			break
		}
	}
	if terminal.ID.IsZero() {
		t.Fatalf("background terminal hit %d is missing", target)
	}
	if err := driver.Press(terminal.Bounds.X+12, terminal.Bounds.Y+terminal.Bounds.Height-12, gfx.PointerButtonPrimary); err != nil {
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
	state.chooseHostSection()
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
	firstTarget := terminalTestHit(state, first)
	secondTarget := terminalTestHit(state, second)
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
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
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
	wide := gfx.Size{Width: 1400, Height: 900}
	openBounds := state.workspacePixelBounds(wide)
	state.Click(hitSFTPClose)
	closedBounds := state.workspacePixelBounds(wide)
	if closedBounds != state.baseWorkspacePixelBounds(wide) || closedBounds.Width <= openBounds.Width {
		t.Fatalf("collapsed bounds open=%+v closed=%+v base=%+v", openBounds, closedBounds, state.baseWorkspacePixelBounds(wide))
	}
	var closed gfx.Canvas
	state.Paint(&closed, wide)
	if _, ok := hitByID(&closed, hitContextDivider); ok {
		t.Fatal("collapsed context left a divider or gap")
	}

	state.Click(hitSFTPOpen)
	narrow := gfx.Size{Width: 800, Height: 700}
	var drawer gfx.Canvas
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
	size := gfx.Size{Width: 800, Height: 700}
	var quiet gfx.Canvas
	state.Paint(&quiet, size)
	dismiss := gfx.ID{}
	for _, hit := range quiet.Hits() {
		if hit.Backdrop {
			dismiss = hit.ID
			break
		}
	}
	if dismiss.IsZero() {
		t.Fatal("the drawer has no backdrop to hover")
	}
	paintedWith := func(hovered gfx.ID) []gfx.Rectangle {
		var canvas gfx.Canvas
		canvas.SetInteraction(hovered, gfx.ID{}, gfx.ID{}, false)
		state.Paint(&canvas, size)
		return dialogLayerRectangles(&canvas)
	}
	if !slices.Equal(paintedWith(gfx.ID{}), paintedWith(dismiss)) {
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
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	divider, ok := hitByID(&canvas, hitContextDivider)
	if !ok {
		t.Fatal("context divider is missing")
	}
	if divider.Cursor != gfx.PointerCursorResizeHorizontal {
		t.Fatalf("context divider cursor = %v", divider.Cursor)
	}
	base := state.baseWorkspacePixelBounds(size)
	dragX := base.X + 400 + 0.5
	y := divider.Bounds.Y + divider.Bounds.Height/2
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hitContextDivider, X: divider.Bounds.X, Y: y, Viewport: size})
	move := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hitContextDivider, Captured: true, X: dragX, Y: y, Viewport: size})
	release := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hitContextDivider, Captured: true, X: dragX, Y: y, Viewport: size})
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
	size := gfx.Size{Width: 1000, Height: 700}
	state.Resize(size)
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	builds := state.workspaceViewBuilds
	divider, ok := hitByID(&canvas, hitPaneDividerList.At(0))
	if !ok {
		t.Fatal("divider hit missing")
	}
	y := divider.Bounds.Y + divider.Bounds.Height/2
	frame := state.workspacePixelBounds(size)
	dragX := frame.X + 0.5 + (frame.Width-1)*0.70
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hitPaneDividerList.At(0), X: divider.Bounds.X + 2, Y: y, Viewport: size})
	move := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hitPaneDividerList.At(0), Captured: true, X: dragX, Y: y, Viewport: size})
	beforeRelease := newTestState(t, options).PaneSplits(nil)
	if len(beforeRelease) != 1 || beforeRelease[0].Ratio != defaultPaneRatio {
		t.Fatalf("divider move persisted before pointer up: %+v", beforeRelease)
	}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hitPaneDividerList.At(0), Captured: true, X: dragX, Y: y, Viewport: size})
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
		size gfx.Size
	}{
		{name: "columns", axis: layoutcore.Horizontal, size: gfx.Size{Width: 1400, Height: 900}},
		{name: "rows", axis: layoutcore.Vertical, size: gfx.Size{Width: 1000, Height: 900}},
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
			var canvas gfx.Canvas
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
			state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: id, X: downX, Y: downY, Viewport: test.size})
			move := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: id, Captured: true, X: moveX, Y: moveY, Viewport: test.size})
			state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: id, Captured: true, X: moveX, Y: moveY, Viewport: test.size})
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

	size := gfx.Size{Width: 1200, Height: 800}
	var canvas gfx.Canvas
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
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hitPaneDividerList.At(0), X: divider.Bounds.X, Y: y, Viewport: size})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hitPaneDividerList.At(0), Captured: true, X: dragX, Y: y, Viewport: size})
	if cols, rows := state.sessions[2].term.screen.Size(); cols != backgroundCols || rows != backgroundRows || len(backgroundShell.resizes) != backgroundResizes {
		t.Fatalf("background workspace resized from %dx%d/%d to %dx%d/%d", backgroundCols, backgroundRows, backgroundResizes, cols, rows, len(backgroundShell.resizes))
	}
}

func TestWorkspaceMultiPaneSteadyPaintDoesNotAllocate(t *testing.T) {
	state := threeVisiblePaneState(t)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
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
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
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
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	b.ReportAllocs()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

func BenchmarkWorkspaceSplitterDrag(b *testing.B) {
	state := threeVisiblePaneState(b)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	divider, ok := hitByID(&canvas, hitPaneDividerList.At(0))
	if !ok {
		b.Fatal("divider hit missing")
	}
	y := divider.Bounds.Y + divider.Bounds.Height/2
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hitPaneDividerList.At(0), X: divider.Bounds.X, Y: y, Viewport: size})
	frame := state.workspacePixelBounds(size)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; b.Loop(); index++ {
		ratio := float32(0.45)
		if index&1 != 0 {
			ratio = 0.55
		}
		x := frame.X + 0.5 + (frame.Width-1)*ratio
		state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hitPaneDividerList.At(0), Captured: true, X: x, Y: y, Viewport: size})
	}
}

func BenchmarkContextPanelDividerDrag(b *testing.B) {
	state := threeVisiblePaneState(b)
	if err := state.deck.SetContext(state.deck.ActiveWorkspace(), layoutcore.ContextState{Kind: layoutcore.ContextSFTP, Open: true, Width: 340}); err != nil {
		b.Fatal(err)
	}
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	divider, ok := hitByID(&canvas, hitContextDivider)
	if !ok {
		b.Fatal("context divider hit missing")
	}
	y := divider.Bounds.Y + divider.Bounds.Height/2
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hitContextDivider, X: divider.Bounds.X, Y: y, Viewport: size})
	base := state.baseWorkspacePixelBounds(size)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; b.Loop(); index++ {
		width := float32(330)
		if index&1 != 0 {
			width = 350
		}
		x := base.X + width + 0.5
		state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hitContextDivider, Captured: true, X: x, Y: y, Viewport: size})
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
	state.chooseHostSection()
	state.Click(hitHostList.At(int(hostID)))
	openHost(state)
}

func assertTextInsidePane(t testing.TB, state *State, canvas *gfx.Canvas, size gfx.Size, sessionID uint32, value string) {
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
	size := gfx.Size{Width: 1200, Height: 800}
	state.Resize(size)
	state.Paint(&gfx.Canvas{}, size)
	active := state.activeSessionID
	var other *Session
	for index := range state.sessions {
		if state.sessions[index].ID != active {
			other = &state.sessions[index]
		}
	}
	target := terminalTestHit(state, other)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: target, X: 20, Y: 80, Viewport: size})
	state.Scroll(gfx.ScrollEvent{Target: target, X: 20, Y: 80, DeltaY: 40, Viewport: size})
	if state.activeSessionID != active {
		t.Fatalf("hover or wheel moved the active pane to %d", state.activeSessionID)
	}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: target, X: 20, Y: 80, Viewport: size})
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

// The terminal panes are components of the workbench page now, not pages of
// their own. A press, a key and typed text in either pane must reach that pane's
// terminal through the page, not through a per-session page or a mount.
func TestSplitPaneEventsGoThroughTheWorkbenchPage(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	if len(state.sessions) < 2 {
		t.Fatalf("split created only %d sessions", len(state.sessions))
	}
	size := gfx.Size{Width: 1200, Height: 800}
	state.Resize(size)
	shells := make([]*fakeShell, len(state.sessions))
	for index := range state.sessions {
		shells[index] = &fakeShell{}
		state.sessions[index].term.shell = shells[index]
		state.sessions[index].term.live = true
	}
	second := &state.sessions[1]
	if !state.ActivatePane(second.ID) {
		t.Fatal("activate the second pane")
	}

	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	page := state.chrome.windowPage
	if page == nil {
		t.Fatal("the workbench page was not created")
	}
	for index := range state.sessions {
		session := &state.sessions[index]
		hit := terminalTestHit(state, session)
		if !windowPageOwns(hit) {
			t.Fatalf("session %d terminal is not in the workbench page: %v", session.ID, hit)
		}
		if _, ok := hitByID(&canvas, hit); !ok {
			t.Fatalf("session %d terminal was not painted", session.ID)
		}
	}

	// A press in the second pane goes through the page to its pointer handler.
	hit := terminalTestHit(state, second)
	geometry := state.layoutForSession(second.ID)
	result := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit, ClickCount: 1,
		X: geometry.left + state.cellWidth*1.5, Y: geometry.top + state.cellHeight*0.5, Viewport: size,
	})
	if !result.Handled || !second.term.selection.dragging {
		t.Fatalf("press through the page = %+v, dragging=%v", result, second.term.selection.dragging)
	}
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true,
		X: geometry.left + state.cellWidth*1.5, Y: geometry.top + state.cellHeight*0.5, Viewport: size,
	})

	// Typed text and a key reach the second pane's shell through the page's
	// control routing.
	input := state.TerminalInput(app.TextInputContext{ID: hit, Terminal: true})
	if input == nil || input.OnEvent == nil {
		t.Fatal("the page gave the terminal no text surface")
	}
	if _, err := input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if got := shells[1].typed(); got != "x" {
		t.Fatalf("typed text reached %q, want the second pane's shell", got)
	}
	if result := windowKeyRoutes(state, gfx.KeyEvent{Target: hit, Key: gfx.KeyEnter}); !result.Handled {
		t.Fatalf("key through the page = %+v", result)
	}
	if got := shells[1].typed(); got != "x\r" {
		t.Fatalf("key reached %q, want the second pane's shell", got)
	}
}

// The docked files panel is a component of the workbench page now: its List's
// rows are the page's identities, not a mount of a panel page. A click, a key
// and typed text must reach it through the page and the window's own ids.
func TestDockedFilesPanelEventsGoThroughTheWorkbenchPage(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	page := state.chrome.windowPage
	if page == nil {
		t.Fatal("the workbench page was not created")
	}
	view := session.files
	folder := view.active()
	row := sftpRowIndex(t, view, "notes.txt")
	rowID := sftpListRowID(t, folder, row)
	if !windowPageOwns(rowID) || !state.contextPanelOwns(rowID) {
		t.Fatalf("the files list row is not in the workbench page: %v", rowID)
	}
	hit, ok := hitByID(&canvas, rowID)
	if !ok {
		t.Fatal("the files list row was not painted")
	}

	// A press on the row selects it. The page declared the row, so the event
	// carries the window's identity and is not translated by a mount.
	result := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1,
		Target: rowID, X: hit.Bounds.X + hit.Bounds.Width/2, Y: hit.Bounds.Y + hit.Bounds.Height/2, Viewport: size,
	})
	if !result.Handled || !folder.rows[row].selected {
		t.Fatalf("press through the page = %+v, selected=%v", result, folder.rows[row].selected)
	}

	// The arrows are the List's, routed by the workbench page.
	if result := windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyUp, Target: rowID}); !result.Handled {
		t.Fatalf("key through the page = %+v", result)
	}
	if folder.list.Selected() != 0 {
		t.Fatalf("arrow moved the List to row %d, want 0", folder.list.Selected())
	}

	// Typing reaches the filter by the identity the panel paints in the window,
	// with no embedded page to translate it.
	state.Click(hitSFTPFilterToggle)
	state.Paint(&canvas, size)
	if !state.contextPanelHit(hitSFTPFilter) {
		t.Fatal("the filter is not part of the docked panel")
	}
	input := state.SFTPInput(app.TextInputContext{ID: hitSFTPFilter})
	if input == nil || input.OnEvent == nil {
		t.Fatal("the docked filter has no native text input")
	}
	if _, err := input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "note"}); err != nil {
		t.Fatal(err)
	}
	if got := folder.filterInputEditor().String(); got != "note" {
		t.Fatalf("typed filter = %q, want note", got)
	}
}
