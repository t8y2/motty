package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/configstore"
	"motty/internal/sshconn"
	"motty/internal/term"
	layoutcore "motty/internal/workspace"
)

func setAllTerminalToolbarSides(state *State, side terminalToolbarSide) {
	entries := state.toolbars.customize(toolbarTerminal)
	for index := range entries {
		entries[index].Side = side.String()
		entries[index].Zone = terminalToolbarZoneFromParts(side, terminalToolbarDockTop).String()
	}
}

func TestTerminalToolbarsStayAtWorkspaceOuterEdges(t *testing.T) {
	for _, side := range []terminalToolbarSide{terminalToolbarLeft, terminalToolbarRight} {
		t.Run(side.String(), func(t *testing.T) {
			state, _ := monitorFixture(t)
			session := &state.sessions[0]
			setAllTerminalToolbarSides(state, side)
			id := state.deck.ActiveWorkspace()
			context, _ := state.deck.Context(id)
			context.MonitorOpen = true
			context.MonitorWidth = 320
			context.MonitorLeft = side == terminalToolbarLeft
			context.Open = false
			if err := state.deck.SetContext(id, context); err != nil {
				t.Fatal(err)
			}
			size := ui.Size{Width: 1200, Height: 800}
			state.Resize(size)
			var canvas ui.Canvas
			state.Paint(&canvas, size)

			rail, ok := hitByID(&canvas, hitSFTPOpen)
			if !ok || rail.Bounds.Width != terminalBarToolSize {
				t.Fatalf("%s rail Files control=%+v, %v", side, rail.Bounds, ok)
			}
			full := state.fullWorkspacePixelBounds(size)
			inner := state.baseWorkspacePixelBounds(size)
			terminal := state.layoutForSession(session.ID).area
			bar, ok := state.terminalSideRailBoundsFor(size, side)
			if !ok {
				t.Fatal("toolbar bounds are missing")
			}
			viewport := session.term.rails[side].zones[terminalToolbarDockTop].viewport
			switch side {
			case terminalToolbarLeft:
				if viewport.X != full.X+4 {
					t.Fatalf("left rail viewport x=%v, want outer x=%v", viewport.X, full.X+4)
				}
				if state.monitorRect.X < bar.X+bar.Width {
					t.Fatalf("left monitor=%+v overlaps outer rail=%+v", state.monitorRect, bar)
				}
				if terminal.X < state.monitorRect.X+state.monitorRect.Width {
					t.Fatalf("terminal=%+v sits left of monitor=%+v", terminal, state.monitorRect)
				}
				if inner.X != full.X+terminalToolRailExtent {
					t.Fatalf("inner workspace x=%v, want %v", inner.X, full.X+terminalToolRailExtent)
				}
			case terminalToolbarRight:
				wantX := full.X + full.Width - terminalToolRailExtent
				if viewport.X+viewport.Width != wantX+terminalToolRailExtent-4 {
					t.Fatalf("right rail viewport=%+v, want its gutter at outer x=%v", viewport, wantX)
				}
				if state.monitorRect.X+state.monitorRect.Width > bar.X {
					t.Fatalf("right monitor=%+v overlaps outer rail=%+v", state.monitorRect, bar)
				}
				if terminal.X+terminal.Width > state.monitorRect.X {
					t.Fatalf("terminal=%+v sits right of monitor=%+v", terminal, state.monitorRect)
				}
				if inner.X+inner.Width != full.X+full.Width-terminalToolRailExtent {
					t.Fatalf("inner workspace right=%v, want %v", inner.X+inner.Width, rail.Bounds.X)
				}
			}
		})
	}
}

func TestTerminalToolbarSizeChangesRailAndControlGeometry(t *testing.T) {
	state, _ := monitorFixture(t)
	session := &state.sessions[0]
	size := ui.Size{Width: 1200, Height: 800}
	state.Resize(size)
	for _, test := range []struct {
		size   terminalToolbarSize
		extent float32
		tool   float32
		icon   float32
	}{
		{terminalToolbarSmall, 40, 32, 17},
		{terminalToolbarMedium, 48, 40, 20},
		{terminalToolbarLarge, 56, 48, 24},
	} {
		t.Run(test.size.String(), func(t *testing.T) {
			state.terminalToolbarSize = test.size
			rail, ok := state.terminalSideRailBoundsFor(size, terminalToolbarRight)
			if !ok || rail.Width != test.extent {
				t.Fatalf("rail bounds=%+v found=%v, want width %v", rail, ok, test.extent)
			}
			var canvas ui.Canvas
			state.Paint(&canvas, size)
			metrics := state.terminalToolbarMetrics()
			if metrics.railExtent != test.extent || metrics.toolSize != test.tool ||
				metrics.iconSize != test.icon {
				t.Fatalf("metrics=%+v", metrics)
			}
			viewport := session.term.rails[terminalToolbarRight].zones[terminalToolbarDockTop].viewport
			if viewport.Width != test.tool {
				t.Fatalf("rail viewport=%+v, want tool width %v", viewport, test.tool)
			}
		})
	}
}

func TestTerminalToolbarZonesAnchorControlsIndependently(t *testing.T) {
	state, _, _, _ := liveSession(t)
	setAllTerminalToolbarSides(state, terminalToolbarLeft)
	size := ui.Size{Width: 1200, Height: 1000}
	state.Resize(size)

	var canvas ui.Canvas
	state.Paint(&canvas, size)
	topFiles, ok := hitByID(&canvas, hitSFTPOpen)
	if !ok {
		t.Fatal("files control is missing from the top zone")
	}
	if !moveToolbarEntryToZone(state, "files", terminalToolbarZoneLeftBottom, 0) {
		t.Fatal("moving files to the bottom-left zone was not reported")
	}
	canvas.Reset()
	state.Paint(&canvas, size)

	bottomFiles, ok := hitByID(&canvas, hitSFTPOpen)
	if !ok {
		t.Fatal("files control is missing from the bottom zone")
	}
	if bottomFiles.Bounds.Y <= topFiles.Bounds.Y {
		t.Fatalf("bottom-zone files control y=%v did not move below top-zone y=%v", bottomFiles.Bounds.Y, topFiles.Bounds.Y)
	}
	zone := state.sessions[0].term.rails[terminalToolbarLeft].zones[terminalToolbarDockBottom]
	if zone.contentExtent > zone.viewport.Height {
		t.Fatalf("fixture unexpectedly overflows: content=%v viewport=%v", zone.contentExtent, zone.viewport.Height)
	}
	wantBottom := zone.viewport.Y + zone.viewport.Height - state.terminalToolbarMetrics().inset
	if bottomFiles.Bounds.Y+bottomFiles.Bounds.Height != wantBottom {
		t.Fatalf("bottom-zone files control bottom=%v, want %v", bottomFiles.Bounds.Y+bottomFiles.Bounds.Height, wantBottom)
	}
}

func TestTerminalToolbarUsesTwoIndependentRailsAndOmitsAnEmptySide(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.sessions[0].host.Color = "#3b82f6"
	size := ui.Size{Width: 1200, Height: 800}
	state.Resize(size)

	occupied, ok := state.terminalToolbarOccupiedSides()
	if !ok || occupied[terminalToolbarLeft] || !occupied[terminalToolbarRight] {
		t.Fatalf("default toolbar sides = %v, want right only", occupied)
	}
	if _, ok := state.terminalSideRailBoundsFor(size, terminalToolbarLeft); ok {
		t.Fatal("an empty left rail reserved space")
	}
	full, inner := state.fullWorkspacePixelBounds(size), state.baseWorkspacePixelBounds(size)
	extent := state.terminalToolbarMetrics().railExtent
	if inner.X != full.X || inner.X+inner.Width != full.X+full.Width-extent {
		t.Fatalf("right-only workspace=%+v full=%+v extent=%v", inner, full, extent)
	}

	entries := state.toolbars.customize(toolbarTerminal)
	split := len(entries) / 2
	for index := range entries {
		side := terminalToolbarRight
		if index < split {
			side = terminalToolbarLeft
		}
		entries[index].Side = side.String()
		entries[index].Zone = terminalToolbarZoneFromParts(side, terminalToolbarDockTop).String()
	}
	state.applyTerminalToolbarLayoutChange()

	occupied, ok = state.terminalToolbarOccupiedSides()
	if !ok || !occupied[terminalToolbarLeft] || !occupied[terminalToolbarRight] {
		t.Fatalf("split toolbar sides = %v, want both", occupied)
	}
	left, leftOK := state.terminalSideRailBoundsFor(size, terminalToolbarLeft)
	right, rightOK := state.terminalSideRailBoundsFor(size, terminalToolbarRight)
	if !leftOK || !rightOK || left.X != full.X || right.X+right.Width != full.X+full.Width {
		t.Fatalf("rail bounds left=%+v/%v right=%+v/%v full=%+v", left, leftOK, right, rightOK, full)
	}
	inner = state.baseWorkspacePixelBounds(size)
	occupied, ok = state.terminalToolbarOccupiedSides()
	if inner.X != full.X+extent || inner.Width != full.Width-2*extent {
		t.Fatalf("two-sided workspace=%+v full=%+v extent=%v occupied=%v/%v", inner, full, extent, occupied, ok)
	}

	var canvas ui.Canvas
	state.Paint(&canvas, size)
	if state.sessions[0].term.rails[terminalToolbarLeft].count == 0 ||
		state.sessions[0].term.rails[terminalToolbarRight].count == 0 {
		t.Fatalf("rail counts left=%d right=%d",
			state.sessions[0].term.rails[terminalToolbarLeft].count,
			state.sessions[0].term.rails[terminalToolbarRight].count)
	}
}

func TestSharedTerminalToolbarFollowsActiveSplit(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	if len(state.sessions) < 2 {
		t.Fatalf("split created only %d sessions", len(state.sessions))
	}
	first, second := &state.sessions[0], &state.sessions[1]
	first.term.live, second.term.live = true, true
	first.term.recording.active, second.term.recording.active = true, false
	size := ui.Size{Width: 1200, Height: 800}

	assertRecordHint := func(session *Session) {
		t.Helper()
		if !state.ActivatePane(session.ID) {
			t.Fatalf("activate session %d", session.ID)
		}
		var canvas ui.Canvas
		state.Paint(&canvas, size)
		record, ok := hitByID(&canvas, hitTerminalRecord)
		if !ok {
			t.Fatal("shared rail record control is missing")
		}
		want := state.messages.TipRecordStart
		if session.term.recording.active {
			want = state.messages.TipRecordStop
		}
		if record.Hint != want {
			t.Fatalf("active session %d record hint=%q, want %q", session.ID, record.Hint, want)
		}
		if session.term.rails[terminalToolbarRight].count == 0 {
			t.Fatal("active session does not own the toolbar")
		}
	}

	assertRecordHint(first)
	assertRecordHint(second)
}

func TestActiveSplitPaneBarKeepsConnectionIdentity(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts[0].Color = "#ef4444"
	state.hosts[1].Color = "#3b82f6"
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	if len(state.sessions) < 2 {
		t.Fatalf("split created only %d sessions", len(state.sessions))
	}
	size := ui.Size{Width: 1200, Height: 800}
	state.Resize(size)

	assertPaneBar := func(session *Session) {
		t.Helper()
		if !state.ActivatePane(session.ID) {
			t.Fatalf("activate session %d", session.ID)
		}
		var canvas ui.Canvas
		state.Paint(&canvas, size)

		workspace, paneID, ok := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
		if !ok {
			t.Fatalf("session %d has no pane", session.ID)
		}
		fraction, _, ok := state.paneFractionBounds(workspace, paneID)
		if !ok {
			t.Fatalf("session %d has no pane geometry", session.ID)
		}
		bounds := pixelBounds(state.workspacePixelBounds(size), fraction)
		title := session.Title
		if title == "" {
			title = session.Address
		}
		status, _ := state.sessionStatus(session)
		foundTitle, foundStatus, neutralBackground := false, false, false
		for _, text := range canvas.Texts() {
			if text.Bounds.Y < bounds.Y || text.Bounds.Y >= bounds.Y+infoBarHeight ||
				text.Bounds.X < bounds.X || text.Bounds.X >= bounds.X+bounds.Width {
				continue
			}
			foundTitle = foundTitle || text.Value == title
			foundStatus = foundStatus || text.Value == status
		}
		for _, rectangle := range canvas.Rectangles() {
			if rectangle.Color == state.pal.workspace &&
				rectangle.Bounds.X <= bounds.X && rectangle.Bounds.X+rectangle.Bounds.Width >= bounds.X+bounds.Width &&
				rectangle.Bounds.Y <= bounds.Y && rectangle.Bounds.Y+rectangle.Bounds.Height >= bounds.Y+infoBarHeight {
				neutralBackground = true
				break
			}
		}
		if !foundTitle || !foundStatus {
			t.Fatalf("active session %d title=%q found=%v status=%q found=%v in pane bar %+v",
				session.ID, title, foundTitle, status, foundStatus, bounds)
		}
		if !neutralBackground {
			t.Fatalf("active session %d split bar is not neutral", session.ID)
		}
	}

	assertPaneBar(&state.sessions[0])
	assertPaneBar(&state.sessions[1])
}

func TestIdleSplitPanesPaintIdentityWithoutAStatusLightSlot(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	if len(state.sessions) < 2 {
		t.Fatalf("split created only %d sessions", len(state.sessions))
	}
	for index := range state.sessions {
		state.sessions[index].term.live = true
		state.sessions[index].busy = false
	}
	size := ui.Size{Width: 1200, Height: 800}
	state.Resize(size)

	for _, index := range []int{0, 1, 0} {
		if !state.ActivatePane(state.sessions[index].ID) {
			t.Fatalf("activate session %d", state.sessions[index].ID)
		}
		var canvas ui.Canvas
		state.Paint(&canvas, size)
	}
}

func TestSwitchingActiveSplitPaneKeepsSizesAndSelection(t *testing.T) {
	state, world, _, firstShell := liveSession(t)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	if len(state.sessions) < 2 || len(world.conns) < 2 {
		t.Fatalf("split has %d sessions and %d connections", len(state.sessions), len(world.conns))
	}
	secondConn := world.conns[1]
	secondConn.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	secondShell := secondConn.shell(t)
	secondShell.say("ready\r\n")
	state.Wake()

	first, second := &state.sessions[0], &state.sessions[1]
	if !state.ActivatePane(first.ID) {
		t.Fatal("activate the first split")
	}
	size := ui.Size{Width: 1200, Height: 800}
	state.Resize(size)
	first.term.write([]byte("select these words\r\n"))
	first.term.selection.selectRange(
		first.term.screen.PointAt(term.Position{Row: 0, Column: 0}),
		first.term.screen.PointAt(term.Position{Row: 0, Column: 5}),
	)

	firstColsBefore, firstRowsBefore := first.term.screen.Size()
	secondColsBefore, secondRowsBefore := second.term.screen.Size()
	firstResizes, secondResizes := len(firstShell.resizes), len(secondShell.resizes)
	selectionBefore := first.term.selection

	if !state.ActivatePane(second.ID) {
		t.Fatal("activate the second split")
	}
	firstColsAfter, firstRowsAfter := first.term.screen.Size()
	secondColsAfter, secondRowsAfter := second.term.screen.Size()
	if firstColsAfter != firstColsBefore || firstRowsAfter != firstRowsBefore ||
		secondColsAfter != secondColsBefore || secondRowsAfter != secondRowsBefore {
		t.Fatalf("screens changed on pane switch: first %dx%d -> %dx%d, second %dx%d -> %dx%d",
			firstColsBefore, firstRowsBefore, firstColsAfter, firstRowsAfter,
			secondColsBefore, secondRowsBefore, secondColsAfter, secondRowsAfter)
	}
	if len(firstShell.resizes) != firstResizes || len(secondShell.resizes) != secondResizes {
		t.Fatalf("pane switch resized a PTY: first %d -> %d, second %d -> %d",
			firstResizes, len(firstShell.resizes), secondResizes, len(secondShell.resizes))
	}
	if !first.term.selection.selected || first.term.selection != selectionBefore {
		t.Fatalf("pane switch changed the inactive selection: before %+v, after %+v",
			selectionBefore, first.term.selection)
	}
}

func TestSwitchingAwayFromAPaneInFindModeKeepsSizesAndSelection(t *testing.T) {
	state, world, _, _ := liveSession(t)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	if len(state.sessions) < 2 || len(world.conns) < 2 {
		t.Fatalf("split has %d sessions and %d connections", len(state.sessions), len(world.conns))
	}
	secondConn := world.conns[1]
	secondConn.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	secondShell := secondConn.shell(t)
	secondShell.say("ready\r\n")
	state.Wake()

	first, second := &state.sessions[0], &state.sessions[1]
	if !state.ActivatePane(first.ID) {
		t.Fatal("activate the first split")
	}
	size := ui.Size{Width: 1200, Height: 800}
	state.Resize(size)

	state.openTerminalFind(first)
	if !state.ActivatePane(second.ID) {
		t.Fatal("activate the second split")
	}
	if first.term.find.open {
		t.Fatal("switching panes left find open on the inactive pane")
	}
	for index := range state.sessions {
		session := &state.sessions[index]
		geometry := state.layoutForSession(session.ID)
		if cols, rows := session.term.screen.Size(); cols != geometry.cols || rows != geometry.rows {
			t.Fatalf("session %d screen=%dx%d geometry=%dx%d", session.ID, cols, rows, geometry.cols, geometry.rows)
		}
	}

	second.term.write([]byte("select these words\r\n"))
	second.term.selection.selectRange(
		second.term.screen.PointAt(term.Position{Row: 0, Column: 0}),
		second.term.screen.PointAt(term.Position{Row: 0, Column: 5}),
	)
	secondColsBefore, secondRowsBefore := second.term.screen.Size()
	secondResizes := len(secondShell.resizes)
	selectionBefore := second.term.selection

	if !state.ActivatePane(first.ID) {
		t.Fatal("activate the first split again")
	}
	if cols, rows := second.term.screen.Size(); cols != secondColsBefore || rows != secondRowsBefore {
		t.Fatalf("switching back resized the inactive pane: %dx%d -> %dx%d",
			secondColsBefore, secondRowsBefore, cols, rows)
	}
	if len(secondShell.resizes) != secondResizes {
		t.Fatalf("switching back resized the PTY: %d -> %d", secondResizes, len(secondShell.resizes))
	}
	if !second.term.selection.selected || second.term.selection != selectionBefore {
		t.Fatalf("switching back changed the inactive selection: before %+v, after %+v",
			selectionBefore, second.term.selection)
	}
}

func TestSwitchingActiveSplitPaneKeepsSizesWithInputNotice(t *testing.T) {
	state, _, _, _ := liveSession(t)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	if len(state.sessions) < 2 {
		t.Fatal("split did not create a second session")
	}
	size := ui.Size{Width: 1200, Height: 800}
	state.Resize(size)
	first, second := &state.sessions[0], &state.sessions[1]
	first.term.commandReporting = true
	first.term.inputGuard.environment.Charset = "other"
	if !state.ActivatePane(first.ID) {
		t.Fatal("activate the first split")
	}
	if !state.terminalInputNoticeVisibleInWorkspace(first) {
		t.Fatal("fixture did not reserve the locale notice in the workspace")
	}
	firstColsBefore, firstRowsBefore := first.term.screen.Size()
	secondColsBefore, secondRowsBefore := second.term.screen.Size()
	if !state.ActivatePane(second.ID) {
		t.Fatal("activate the second split")
	}
	firstColsAfter, firstRowsAfter := first.term.screen.Size()
	secondColsAfter, secondRowsAfter := second.term.screen.Size()
	if firstColsAfter != firstColsBefore || firstRowsAfter != firstRowsBefore ||
		secondColsAfter != secondColsBefore || secondRowsAfter != secondRowsBefore {
		t.Fatalf("notice-driven pane switch resized a PTY: first %dx%d -> %dx%d, second %dx%d -> %dx%d",
			firstColsBefore, firstRowsBefore, firstColsAfter, firstRowsAfter,
			secondColsBefore, secondRowsBefore, secondColsAfter, secondRowsAfter)
	}
}

func TestTerminalPointerCellsFollowEachPaintedSplit(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	if len(state.sessions) < 2 {
		t.Fatalf("split created only %d sessions", len(state.sessions))
	}
	size := ui.Size{Width: 1200, Height: 800}
	state.Resize(size)

	for index := range state.sessions {
		session := &state.sessions[index]
		if !state.ActivatePane(session.ID) {
			t.Fatalf("activate session %d", session.ID)
		}
		var canvas ui.Canvas
		state.Paint(&canvas, size)
		hit, ok := hitByID(&canvas, tree.MountID(paneMount(session), hitTerminalList.At(int(session.ID))))
		if !ok {
			t.Fatalf("session %d has no mounted terminal hit", session.ID)
		}
		geometry := state.layoutForSession(session.ID)
		x := geometry.left + state.cellWidth*1.5
		y := geometry.top + state.cellHeight*0.5
		state.Pointer(ui.PointerEvent{
			Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hit.ID,
			X: x, Y: y, Viewport: size,
		})
		if want := session.term.screen.PointAt(term.Position{Row: 0, Column: 1}); session.term.selection.anchor != want {
			t.Fatalf("session %d active=%v point (%v,%v) mapped to %+v, want %+v", session.ID, session.ID == state.activeSessionID, x, y, session.term.selection.anchor, want)
		}
	}
}

func TestSplitMouseReportingUsesPaintedPaneCoordinates(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	if len(state.sessions) < 2 {
		t.Fatalf("split created only %d sessions", len(state.sessions))
	}
	size := ui.Size{Width: 1200, Height: 800}
	state.Resize(size)
	session := &state.sessions[1]
	if !state.ActivatePane(session.ID) {
		t.Fatal("activate the second split")
	}
	shell := &fakeShell{}
	session.term.shell = shell
	session.term.screen.Write([]byte("\x1b[?1002h\x1b[?1006h"))

	var canvas ui.Canvas
	state.Paint(&canvas, size)
	hit, ok := hitByID(&canvas, tree.MountID(paneMount(session), hitTerminalList.At(int(session.ID))))
	if !ok {
		t.Fatal("the second split has no painted terminal hit")
	}
	bounds, _, viewport, embedded := state.paneEventGeometry(session.ID, size)
	if !embedded {
		t.Fatal("the second split did not report embedded geometry")
	}
	geometry := state.layoutForSession(session.ID)
	windowX := geometry.left + state.cellWidth*4.5
	windowY := geometry.top + state.cellHeight*2.5
	result := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hit.ID,
		X: windowX, Y: windowY, Viewport: size,
	})
	if !result.Handled || shell.typed() != "\x1b[<0;5;3M" {
		t.Fatalf("pane=%+v viewport=%v result=%+v input=%q", bounds, viewport, result, shell.typed())
	}
}

func TestSplitScrollbarUsesPaintedPaneCoordinates(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	if len(state.sessions) < 2 {
		t.Fatalf("split created only %d sessions", len(state.sessions))
	}
	size := ui.Size{Width: 1200, Height: 800}
	state.Resize(size)
	session := &state.sessions[1]
	if !state.ActivatePane(session.ID) {
		t.Fatal("activate the second split")
	}
	for line := 0; line < 100; line++ {
		session.term.screen.Write([]byte("history line\r\n"))
	}

	var canvas ui.Canvas
	state.Paint(&canvas, size)
	bar, ok := hitByID(&canvas, tree.MountID(paneMount(session), hitTerminalBar.At(int(session.ID))))
	if !ok {
		t.Fatal("the second split has no painted scrollbar")
	}
	_, origin, _, embedded := state.paneEventGeometry(session.ID, size)
	if !embedded {
		t.Fatal("the second split did not report embedded geometry")
	}
	geometry := state.terminalScrollArea(nil, session, state.layoutForSession(session.ID).area, 1).Geometry()
	x := bar.Bounds.X + bar.Bounds.Width/2 + origin.X
	grab := origin.Y + geometry.VerticalThumb.Y + geometry.VerticalThumb.Height/2
	result := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonPrimary,
		Target: tree.MountID(paneMount(session), hitTerminalBar.At(int(session.ID))),
		X:      x, Y: grab, Viewport: size,
	})
	if !result.Handled || !session.term.barDragging {
		t.Fatalf("scrollbar press=%+v dragging=%v", result, session.term.barDragging)
	}
}

func TestSharedTerminalToolbarPlacesLatencyAsAnOrderedEntry(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	state.toolbars.setTerminalEntryEnabled("latency", true)
	state.syncTerminalStatusEntries()
	atomic.StoreInt64(&session.rttMs, 42)
	atomic.StoreInt32(&session.rttAvailable, 1)
	size := ui.Size{Width: 1200, Height: 800}
	state.Resize(size)

	var canvas ui.Canvas
	state.Paint(&canvas, size)
	files, ok := hitByID(&canvas, hitSFTPOpen)
	if !ok {
		t.Fatal("files action is missing")
	}
	var latency ui.Text
	found := false
	for _, text := range canvas.Texts() {
		if text.Value == "42ms" {
			latency = text
			found = true
			break
		}
	}
	if !found {
		t.Fatal("latency is not painted in the ordered rail")
	}
	if latency.Bounds.Y >= files.Bounds.Y {
		t.Fatalf("latency=%+v is not ordered before files=%+v", latency.Bounds, files.Bounds)
	}
}

func TestSharedTerminalToolbarSavesOutput(t *testing.T) {
	state, _, _, shell := liveSession(t)
	shell.say("first line\r\nsecond line\r\n")
	state.Wake()
	saved := filepath.Join(t.TempDir(), "terminal-output.txt")
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) { return saved, nil }
	size := ui.Size{Width: 1200, Height: 800}
	state.Resize(size)

	var canvas ui.Canvas
	state.Paint(&canvas, size)
	if _, ok := hitByID(&canvas, hitTerminalSaveOutput); !ok {
		t.Fatal("shared toolbar has no save-output action")
	}
	state.Click(hitTerminalSaveOutput)
	data, err := os.ReadFile(saved)
	if err != nil || !strings.Contains(string(data), "first line\nsecond line") {
		t.Fatalf("saved %q, %v", data, err)
	}
}

func TestSharedSidePanelsFollowActiveSplit(t *testing.T) {
	state, first, _, _ := openTestSFTP(t)
	state.Resize(ui.Size{Width: 1200, Height: 800})
	firstID := first.ID
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	if len(state.sessions) < 2 {
		t.Fatalf("split created only %d sessions", len(state.sessions))
	}
	first, ok := state.sessionByIDPointer(firstID)
	if !ok {
		t.Fatal("the original split session disappeared")
	}
	second, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || second == first {
		t.Fatal("the split did not activate its new session")
	}

	first.files = &sftpView{sftpFolder: sftpFolder{path: "/first"}}
	second.files = &sftpView{sftpFolder: sftpFolder{path: "/second"}}
	first.stats = &remoteStatsView{state: remoteStatsReady}
	second.stats = &remoteStatsView{state: remoteStatsReady}
	first.stats.panel.cpuText, second.stats.panel.cpuText = "first", "second"
	workspace := state.deck.ActiveWorkspace()
	context, _ := state.deck.Context(workspace)
	context.Kind, context.Open, context.MonitorOpen = layoutcore.ContextSFTP, true, true
	if err := state.deck.SetContext(workspace, context); err != nil {
		t.Fatal(err)
	}

	for _, want := range []*Session{first, second, first} {
		if !state.ActivatePane(want.ID) {
			t.Fatalf("activate session %d", want.ID)
		}
		active := state.contextSession()
		if active != want {
			t.Fatalf("active context is session %d, want %d", active.ID, want.ID)
		}
		if active.files != want.files {
			t.Fatalf("files panel follows session %p, want %p for %d", active.files, want.files, want.ID)
		}
		if active.stats != want.stats {
			t.Fatalf("monitor follows session %p, want %p for %d", active.stats, want.stats, want.ID)
		}
	}
}

func TestSharedToolbarAndPanelsStayInTheirOuterBands(t *testing.T) {
	for _, side := range []terminalToolbarSide{terminalToolbarLeft, terminalToolbarRight} {
		t.Run(side.String(), func(t *testing.T) {
			state, session, _, _ := openTestSFTP(t)
			setAllTerminalToolbarSides(state, side)
			state.ai.open = true
			state.ai.panelWidth = 280
			workspace := state.deck.ActiveWorkspace()
			context, _ := state.deck.Context(workspace)
			context.Kind, context.Open, context.Side = layoutcore.ContextSFTP, true, layoutcore.ContextRight
			context.MonitorOpen, context.MonitorLeft = true, side == terminalToolbarLeft
			if err := state.deck.SetContext(workspace, context); err != nil {
				t.Fatal(err)
			}
			size := ui.Size{Width: 1500, Height: 900}
			state.Resize(size)
			state.aiPanelSync()

			var canvas ui.Canvas
			state.Paint(&canvas, size)
			rail, ok := state.terminalSideRailBoundsFor(size, side)
			if !ok {
				t.Fatal("shared toolbar bounds are missing")
			}
			full := state.fullWorkspacePixelBounds(size)
			if side == terminalToolbarLeft {
				if rail.X != full.X {
					t.Fatalf("left rail x=%v, want outer-%v", rail.X, full.X)
				}
			} else if rail.X+rail.Width != full.X+full.Width {
				t.Fatalf("right rail right=%v, want outer-%v", rail.X+rail.Width, full.X+full.Width)
			}
			terminal := state.layoutForSession(session.ID).area
			for name, panel := range map[string]ui.Rect{
				"monitor":   state.monitorRect,
				"files":     state.contextRect,
				"assistant": state.ai.panel.rect,
			} {
				if panel.Width <= 0 || panel.Height <= 0 {
					t.Fatalf("%s panel has no geometry: %+v", name, panel)
				}
				if overlap := intersectRect(panel, rail); overlap.Width > 0 && overlap.Height > 0 {
					t.Fatalf("%s panel=%+v overlaps shared rail=%+v", name, panel, rail)
				}
			}
			if overlap := intersectRect(terminal, rail); overlap.Width > 0 && overlap.Height > 0 {
				t.Fatalf("terminal=%+v overlaps shared rail=%+v", terminal, rail)
			}
		})
	}
}

func TestSharedTerminalToolbarKeyboardNavigationReachesOverflow(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	setAllTerminalToolbarSides(state, terminalToolbarLeft)
	size := ui.Size{Width: 900, Height: 300}
	state.Resize(size)

	var canvas ui.Canvas
	state.Paint(&canvas, size)
	rail := &session.term.rails[terminalToolbarLeft]
	zone := &rail.zones[terminalToolbarDockTop]
	if rail.count < 3 || zone.contentExtent <= zone.viewport.Height {
		t.Fatalf("fixture does not overflow: count=%d content=%v viewport=%v", rail.count, zone.contentExtent, zone.viewport)
	}
	lastID := rail.ids[rail.count-1]
	if _, visible := hitByID(&canvas, lastID); visible {
		t.Fatal("the final control unexpectedly begins in the visible rail")
	}

	target := rail.ids[0]
	for target != lastID {
		result := state.Key(ui.KeyEvent{Target: target, Key: ui.KeyDown})
		if !result.Handled || result.Focus.IsZero() || result.Focus == target {
			t.Fatalf("Down from %v returned %+v", target, result)
		}
		target = result.Focus
	}

	canvas.Reset()
	canvas.SetInteraction(ui.ID{}, ui.ID{}, lastID, true)
	state.Paint(&canvas, size)
	if zone.scroll <= 0 {
		t.Fatal("keyboard focus did not scroll the overflowed shared rail")
	}
	if _, visible := hitByID(&canvas, lastID); !visible {
		t.Fatal("the keyboard-focused overflow control was not brought into view")
	}
}

func TestSharedTerminalToolbarKeyboardNavigationSkipsDisabledActions(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	size := ui.Size{Width: 900, Height: 700}
	state.Resize(size)

	var canvas ui.Canvas
	state.Paint(&canvas, size)
	rail := &session.term.rails[terminalToolbarRight]
	disabled := -1
	for index := 0; index < rail.count; index++ {
		if !rail.enabled[index] {
			disabled = index
			break
		}
	}
	if disabled <= 0 || disabled >= rail.count-1 {
		t.Fatalf("fixture has no usable disabled action: count=%d disabled=%d", rail.count, disabled)
	}
	before, after := disabled-1, disabled+1
	for before >= 0 && !rail.enabled[before] {
		before--
	}
	for after < rail.count && !rail.enabled[after] {
		after++
	}
	if before < 0 || after >= rail.count {
		t.Fatalf("disabled action is not between enabled actions: before=%d disabled=%d after=%d", before, disabled, after)
	}

	result, handled := state.terminalToolbarKey(session, ui.KeyEvent{Target: rail.ids[before], Key: ui.KeyDown})
	if !handled || !result.Handled || result.Focus != rail.ids[after] {
		t.Fatalf("Down across disabled action = %+v handled=%v, want focus %v", result, handled, rail.ids[after])
	}
	result, handled = state.terminalToolbarKey(session, ui.KeyEvent{Target: rail.ids[after], Key: ui.KeyUp})
	if !handled || !result.Handled || result.Focus != rail.ids[before] {
		t.Fatalf("Up across disabled action = %+v handled=%v, want focus %v", result, handled, rail.ids[before])
	}
}

func TestWheelOverSharedTerminalToolbarDoesNotScrollTerminalHistory(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	size := ui.Size{Width: 900, Height: 300}
	state.Resize(size)
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	railState := &session.term.rails[terminalToolbarRight]
	zone := &railState.zones[terminalToolbarDockTop]
	if zone.contentExtent <= zone.viewport.Height {
		t.Fatal("fixture does not overflow")
	}
	before := session.term.screen.ViewOffset()
	rail := zone.viewport
	result := state.scroll(ui.ScrollEvent{X: rail.X + 2, Y: rail.Y + 2, DeltaY: 1})
	if !result.Handled || zone.scroll <= 0 {
		t.Fatalf("wheel result=%+v offset=%v", result, zone.scroll)
	}
	if session.term.screen.ViewOffset() != before {
		t.Fatal("wheel over the shared rail moved terminal history")
	}
}

func TestWheelOverSharedTerminalToolbarGutterStaysInTheRail(t *testing.T) {
	for _, side := range []terminalToolbarSide{terminalToolbarLeft, terminalToolbarRight} {
		t.Run(side.String(), func(t *testing.T) {
			state, _, _, _ := liveSession(t)
			session := &state.sessions[0]
			setAllTerminalToolbarSides(state, side)
			size := ui.Size{Width: 900, Height: 300}
			state.Resize(size)
			for line := 0; line < 80; line++ {
				session.term.screen.Write([]byte("history line\r\n"))
			}
			var canvas ui.Canvas
			state.Paint(&canvas, size)
			railState := &session.term.rails[side]
			zone := &railState.zones[terminalToolbarDockTop]
			if zone.contentExtent <= zone.viewport.Height {
				t.Fatal("fixture does not overflow")
			}
			before := session.term.screen.ViewOffset()
			rail := railState.bounds
			x := rail.X + 1
			if side == terminalToolbarRight {
				x = rail.X + rail.Width - 1
			}
			result := state.scroll(ui.ScrollEvent{X: x, Y: rail.Y + 2, DeltaY: 1})
			if !result.Handled || zone.scroll <= 0 {
				t.Fatalf("gutter wheel result=%+v offset=%v", result, zone.scroll)
			}
			if session.term.screen.ViewOffset() != before {
				t.Fatal("wheel over the rail gutter moved terminal history")
			}
		})
	}
}

func TestActivatingTerminalWorkspaceFitsShellToSharedRail(t *testing.T) {
	state, _, _, shell := liveSession(t)
	size := ui.Size{Width: 1200, Height: 800}
	state.viewport = size
	workspace := state.deck.ActiveWorkspace()
	state.activeSection = sectionHosts
	geometry := state.layoutForSession(state.sessions[0].ID)
	state.fitWorkspaceTerminals(workspace)
	cols, rows := state.sessions[0].term.screen.Size()
	if cols != geometry.cols || rows != geometry.rows {
		t.Fatalf("fixture before activation screen=%dx%d geometry=%dx%d", cols, rows, geometry.cols, geometry.rows)
	}
	resizes := len(shell.resizes)
	state.sessions[0].term.screen.Resize(cols+5, rows)

	if !state.activateWorkspace(workspace) {
		t.Fatal("activate the terminal workspace")
	}
	geometry = state.layoutForSession(state.sessions[0].ID)
	cols, rows = state.sessions[0].term.screen.Size()
	if cols != geometry.cols || rows != geometry.rows {
		t.Fatalf("activated terminal screen=%dx%d geometry=%dx%d", cols, rows, geometry.cols, geometry.rows)
	}
	if len(shell.resizes) != resizes+1 {
		t.Fatalf("activation made %d resize calls, want exactly the final fit", len(shell.resizes)-resizes)
	}
}

func TestTerminalToolbarSidesPersistPerActionAndInvalidValuesFallBackToRight(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	entries := state.toolbars.customize(toolbarTerminal)
	entries[0].Side = terminalToolbarLeft.String()
	state.save()
	saved, _, err := loadState(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Toolbars == nil || len(saved.Toolbars.Terminal) == 0 ||
		saved.Toolbars.Terminal[0].Side != terminalToolbarLeft.String() {
		t.Fatalf("saved terminal toolbar = %+v, want first action on the left", saved.Toolbars)
	}
	if got := terminalToolbarSideFromString("top", terminalToolbarRight); got != terminalToolbarRight {
		t.Fatalf("unknown saved position became %v, want right", got)
	}
}

func TestTerminalToolbarSizePersistsAndInvalidValuesFallBackToSmall(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	if state.terminalToolbarSize != terminalToolbarSmall {
		t.Fatalf("default toolbar size = %v, want small", state.terminalToolbarSize)
	}
	if !state.setTerminalToolbarSize(terminalToolbarLarge) {
		t.Fatal("changing the toolbar size was not reported")
	}
	state.save()
	saved, _, err := loadState(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.TerminalToolbarSize != terminalToolbarLarge.String() {
		t.Fatalf("saved toolbar size = %q, want %q", saved.TerminalToolbarSize, terminalToolbarLarge.String())
	}
	if got := terminalToolbarSizeFromString("huge"); got != terminalToolbarSmall {
		t.Fatalf("unknown saved size became %v, want small", got)
	}
}

func TestTerminalToolbarZonesPersistPerControlAndInvalidValuesFallBackToTop(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	if !moveToolbarEntryToZone(state, "files", terminalToolbarZoneLeftBottom, 0) {
		t.Fatal("left-bottom zone change was not reported")
	}
	if !moveToolbarEntryToZone(state, "latency", terminalToolbarZoneRightBottom, 0) {
		t.Fatal("right-bottom zone change was not reported")
	}
	state.save()
	saved, _, err := loadState(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	savedZones := map[string]string{}
	if saved.Toolbars != nil {
		for _, entry := range saved.Toolbars.Terminal {
			savedZones[entry.Key] = entry.Zone
		}
	}
	if savedZones["files"] != terminalToolbarZoneLeftBottom.String() ||
		savedZones["latency"] != terminalToolbarZoneRightBottom.String() ||
		savedZones["close-pane"] != terminalToolbarZoneRightTop.String() {
		t.Fatalf("saved zones = %v", savedZones)
	}
	restarted := newTestState(t, options)
	if restarted.terminalToolbarZoneFor(hitSFTPOpen) != terminalToolbarZoneLeftBottom ||
		restarted.terminalToolbarZoneFor(hitTerminalLatency) != terminalToolbarZoneRightBottom ||
		restarted.terminalToolbarZoneFor(hitPaneClose) != terminalToolbarZoneRightTop {
		t.Fatal("per-control zones did not survive restart")
	}
	if zone, ok := terminalToolbarZoneFromString("sideways"); ok || zone != terminalToolbarZoneRightTop {
		t.Fatalf("unknown saved zone became %v/%v, want invalid then right-top", zone, ok)
	}
}

func TestTerminalConnectionColorTogglePersists(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.openToolbarSettings(toolbarTerminal)
	state.Click(hitToolbarConnectionColor)
	if !state.terminalHideConnectionColor {
		t.Fatal("connection-color switch did not hide the capsule")
	}
	restarted := newTestState(t, options)
	if !restarted.terminalHideConnectionColor {
		t.Fatal("connection-color switch did not survive restart")
	}
}

func TestFormat19StateDefaultsTerminalToolbarSize(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "state.json")
	options.Keystore = &memKeys{}
	store, err := configstore.Open(options.StatePath, 19)
	if err != nil {
		t.Fatal(err)
	}
	entries, local, err := splitState(savedState{
		Version: stateFileVersion, HostCatalogVersion: hostCatalogSchemaVersion,
		ShortcutVersion: shortcutSchemaVersion,
	}, newConfigBase(), nil)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := store.Commit(configstore.Change{
		Put: entries, Local: map[string][]byte{localKey: local},
	}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restored := newTestState(t, options)
	if restored.terminalToolbarSize != terminalToolbarSmall {
		t.Fatalf("format 19 toolbar size = %v, want small", restored.terminalToolbarSize)
	}
}

func TestFormat20StateDefaultsTerminalToolbarZonesToRightTop(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "state.json")
	options.Keystore = &memKeys{}
	store, err := configstore.Open(options.StatePath, 20)
	if err != nil {
		t.Fatal(err)
	}
	entries, local, err := splitState(savedState{
		Version: stateFileVersion, HostCatalogVersion: hostCatalogSchemaVersion,
		ShortcutVersion: shortcutSchemaVersion,
	}, newConfigBase(), nil)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := store.Commit(configstore.Change{
		Put: entries, Local: map[string][]byte{localKey: local},
	}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restored := newTestState(t, options)
	for _, entry := range restored.toolbars.entries(toolbarTerminal) {
		if zone := terminalToolbarZoneForEntry(entry, terminalToolbarRight); zone != terminalToolbarZoneRightTop {
			t.Fatalf("format 20 entry %q zone=%v, want right-top", entry.Key, zone)
		}
	}
}

func TestLegacyDockMigrationPreservesValidPerControlZones(t *testing.T) {
	state := newTestState(t, testOptions(t))
	savedToolbars := &savedToolbars{Terminal: []toolbarEntry{
		{Key: "latency", Enabled: true, Side: "right", Zone: terminalToolbarZoneRightBottom.String()},
		{Key: "files", Enabled: true, Side: "left", Zone: terminalToolbarZoneLeftBottom.String()},
		{Key: "record", Enabled: true, Side: "left"},
	}}
	state.toolbars.restore(savedToolbars, terminalToolbarRight)
	state.restoreLegacyTerminalToolbarDocks(savedState{
		Toolbars:                 savedToolbars,
		TerminalToolbarLeftDock:  terminalToolbarDockTop.String(),
		TerminalToolbarRightDock: terminalToolbarDockBottom.String(),
	})

	if got := state.terminalToolbarZoneFor(hitTerminalLatency); got != terminalToolbarZoneRightBottom {
		t.Fatalf("valid right-bottom zone migrated to %v", got)
	}
	if got := state.terminalToolbarZoneFor(hitSFTPOpen); got != terminalToolbarZoneLeftBottom {
		t.Fatalf("valid left-bottom zone migrated to %v", got)
	}
	if got := state.terminalToolbarZoneFor(hitTerminalRecord); got != terminalToolbarZoneLeftTop {
		t.Fatalf("legacy left dock became %v, want left-top", got)
	}
	if got := state.terminalToolbarZoneFor(hitPaneClose); got != terminalToolbarZoneRightBottom {
		t.Fatalf("legacy right dock became %v, want right-bottom", got)
	}
}

func TestTerminalToolbarLoneQuadrantUsesTheFullShortRail(t *testing.T) {
	state, _, _, _ := liveSession(t)
	size := ui.Size{Width: 760, Height: 500}
	state.Resize(size)

	var canvas ui.Canvas
	state.Paint(&canvas, size)
	rail := state.sessions[0].term.rails[terminalToolbarRight]
	if rail.zones[terminalToolbarDockBottom].viewport.Height != 0 {
		t.Fatalf("empty bottom viewport has height %v", rail.zones[terminalToolbarDockBottom].viewport.Height)
	}
	if rail.zones[terminalToolbarDockTop].viewport.Height < 400 {
		t.Fatalf("lone top quadrant height=%v, want the usable rail height", rail.zones[terminalToolbarDockTop].viewport.Height)
	}
	if _, ok := hitByID(&canvas, hitTerminalRecord); !ok {
		t.Fatal("record control is unreachable in a short, single-quadrant rail")
	}

	entries := state.toolbars.customize(toolbarTerminal)
	for index := range entries {
		entries[index].Side = terminalToolbarRight.String()
		entries[index].Zone = terminalToolbarZoneRightBottom.String()
	}
	state.applyTerminalToolbarLayoutChange()
	canvas.Reset()
	state.Paint(&canvas, size)
	rail = state.sessions[0].term.rails[terminalToolbarRight]
	if rail.zones[terminalToolbarDockTop].viewport.Height != 0 {
		t.Fatalf("empty top viewport has height %v", rail.zones[terminalToolbarDockTop].viewport.Height)
	}
	if rail.zones[terminalToolbarDockBottom].viewport.Height < 400 {
		t.Fatalf("lone bottom quadrant height=%v, want the usable rail height", rail.zones[terminalToolbarDockBottom].viewport.Height)
	}
}

func TestTerminalToolbarLatencyWithoutASampleUsesAVisiblePlaceholder(t *testing.T) {
	state, _, _, _ := liveSession(t)
	entries := state.toolbars.customize(toolbarTerminal)
	for index := range entries {
		entries[index].Enabled = entries[index].Key == "latency"
	}
	state.syncTerminalStatusEntries()
	size := ui.Size{Width: 900, Height: 700}
	state.Resize(size)

	canvas, texts := paintAt(state, size)
	handle := terminalToolbarDragHandle(hitTerminalLatency)
	if _, ok := hitByID(canvas, handle); !ok {
		var painted []ui.ID
		for _, hit := range canvas.Hits() {
			if hit.ID.Scope() == hitTerminalToolbarDrag {
				painted = append(painted, hit.ID)
			}
		}
		t.Fatalf("latency placeholder is not draggable; painted handles=%v want=%v", painted, handle)
	}
	found := false
	for _, value := range texts {
		found = found || value == "—"
	}
	if !found {
		t.Fatal("latency placeholder was painted as an invisible empty slot")
	}
}

func TestTerminalToolbarFourColumnsNavigateAndPaint(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.openToolbarSettings(toolbarTerminal)

	key := state.toolbars.entries(toolbarTerminal)[0].Key
	handle := hitToolbarHandles.At(toolbarEntryIndex(toolbarTerminal, key))
	spec, _ := toolbarSpecFor(toolbarTerminal, key)
	result := state.toolbarSettingsKey(ui.KeyEvent{Target: handle, Key: ui.KeyLeft})
	if !result.Handled || state.terminalToolbarZoneFor(spec.id) != terminalToolbarZoneLeftTop {
		t.Fatalf("left keyboard result=%+v zone=%v", result, state.terminalToolbarZoneFor(spec.id))
	}
	result = state.toolbarSettingsKey(ui.KeyEvent{Target: handle, Key: ui.KeyRight})
	if !result.Handled || state.terminalToolbarZoneFor(spec.id) != terminalToolbarZoneRightTop {
		t.Fatalf("right keyboard result=%+v zone=%v", result, state.terminalToolbarZoneFor(spec.id))
	}
	result = state.toolbarSettingsKey(ui.KeyEvent{Target: handle, Key: ui.KeyPageDown})
	if !result.Handled || state.terminalToolbarZoneFor(spec.id) != terminalToolbarZoneRightBottom {
		t.Fatalf("page down keyboard result=%+v zone=%v", result, state.terminalToolbarZoneFor(spec.id))
	}
	result = state.toolbarSettingsKey(ui.KeyEvent{Target: handle, Key: ui.KeyLeft})
	if !result.Handled || state.terminalToolbarZoneFor(spec.id) != terminalToolbarZoneLeftBottom {
		t.Fatalf("left-bottom keyboard result=%+v zone=%v", result, state.terminalToolbarZoneFor(spec.id))
	}

	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1200, Height: 900})
	for zone := range state.toolbarEditor.zoneViewport {
		viewport := state.toolbarEditor.zoneViewport[zone]
		if viewport.Width <= 0 || viewport.Height <= 0 {
			t.Fatalf("zone %d viewport=%+v", zone, viewport)
		}
		if zone > 0 && viewport.X <= state.toolbarEditor.zoneViewport[zone-1].X {
			t.Fatalf("zone %d x=%v is not after zone %d x=%v", zone, viewport.X, zone-1, state.toolbarEditor.zoneViewport[zone-1].X)
		}
	}
}

func TestTerminalToolbarSettingsApplyImmediatelyAndNavigate(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	state.activeSection = sectionSettings
	session.term.rails[terminalToolbarRight].zones[terminalToolbarDockTop].scroll = 90
	state.openToolbarSettings(toolbarTerminal)

	state.Click(hitToolbarSizeMedium)
	if state.terminalToolbarSize != terminalToolbarMedium || session.term.rails[terminalToolbarRight].zones[terminalToolbarDockTop].scroll != 0 {
		t.Fatalf("size=%v scroll=%v after immediate setting", state.terminalToolbarSize, session.term.rails[terminalToolbarRight].zones[terminalToolbarDockTop].scroll)
	}
	geometry := state.layoutForSession(session.ID)
	if cols, rows := session.term.screen.Size(); cols != geometry.cols || rows != geometry.rows {
		t.Fatalf("background terminal screen=%dx%d geometry=%dx%d after toolbar size", cols, rows, geometry.cols, geometry.rows)
	}

	result := state.toolbarSettingsKey(ui.KeyEvent{Target: hitToolbarSizeSmall, Key: ui.KeyRight})
	if !result.Handled || result.Focus != hitToolbarSizeMedium || state.terminalToolbarSize != terminalToolbarMedium {
		t.Fatalf("size keyboard result=%+v size=%v", result, state.terminalToolbarSize)
	}

	key := state.toolbars.entries(toolbarTerminal)[0].Key
	handle := hitToolbarHandles.At(toolbarEntryIndex(toolbarTerminal, key))
	result = state.toolbarSettingsKey(ui.KeyEvent{Target: handle, Key: ui.KeyLeft})
	spec, _ := toolbarSpecFor(toolbarTerminal, key)
	if !result.Handled || result.Focus != handle || state.terminalToolbarSideFor(spec.id) != terminalToolbarLeft {
		t.Fatalf("side keyboard result=%+v key=%q", result, key)
	}
	result = state.toolbarSettingsKey(ui.KeyEvent{Target: handle, Key: ui.KeyRight})
	if !result.Handled || state.terminalToolbarSideFor(spec.id) != terminalToolbarRight {
		t.Fatalf("side keyboard return result=%+v key=%q", result, key)
	}

	state.Click(hitToolbarConnectionColor)
	state.Click(hitToolbarLatency)
	if !state.terminalHideConnectionColor || !state.terminalShowLatency {
		t.Fatalf("toolbar visibility switches colorOff=%v latency=%v", state.terminalHideConnectionColor, state.terminalShowLatency)
	}
}

func TestTerminalFindRemovesTheSharedRailWhileSearching(t *testing.T) {
	for _, side := range []terminalToolbarSide{terminalToolbarLeft, terminalToolbarRight} {
		t.Run(side.String(), func(t *testing.T) {
			state, _, _, _ := liveSession(t)
			session := &state.sessions[0]
			setAllTerminalToolbarSides(state, side)
			size := ui.Size{Width: 900, Height: 700}
			state.Resize(size)

			state.openTerminalFind(session)
			if _, ok := state.terminalSideRailSession(); ok {
				t.Fatal("find mode kept the shared rail visible")
			}
			geometry := state.layoutForSession(session.ID)
			cols, rows := session.term.screen.Size()
			if cols != geometry.cols || rows != geometry.rows {
				t.Fatalf("find-open terminal=%dx%d geometry=%dx%d", cols, rows, geometry.cols, geometry.rows)
			}

			state.closeTerminalFind(session)
			if _, ok := state.terminalSideRailSession(); !ok {
				t.Fatal("closing find did not restore the shared rail")
			}
			geometry = state.layoutForSession(session.ID)
			cols, rows = session.term.screen.Size()
			if cols != geometry.cols || rows != geometry.rows {
				t.Fatalf("find-closed terminal=%dx%d geometry=%dx%d", cols, rows, geometry.cols, geometry.rows)
			}
		})
	}
}

func TestTerminalFindResizesEveryPaneForTheSharedRail(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	if len(state.sessions) < 2 {
		t.Fatalf("split created only %d sessions", len(state.sessions))
	}
	size := ui.Size{Width: 1200, Height: 800}
	state.Resize(size)
	active, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("the active split session is missing")
	}

	assertPaneSizes := func(stage string) {
		t.Helper()
		for index := range state.sessions {
			session := &state.sessions[index]
			geometry := state.layoutForSession(session.ID)
			if cols, rows := session.term.screen.Size(); cols != geometry.cols || rows != geometry.rows {
				t.Fatalf("%s: session %d screen=%dx%d geometry=%dx%d", stage, session.ID, cols, rows, geometry.cols, geometry.rows)
			}
		}
	}

	state.openTerminalFind(active)
	assertPaneSizes("find open")
	state.closeTerminalFind(active)
	assertPaneSizes("find closed")
}

func TestClickingFindCloseResizesEveryPaneForTheSharedRail(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	if len(state.sessions) < 2 {
		t.Fatalf("split created only %d sessions", len(state.sessions))
	}
	size := ui.Size{Width: 1200, Height: 800}
	state.Resize(size)
	active, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("the active split session is missing")
	}
	state.openTerminalFind(active)
	if !state.clickTerminalFind(hitTerminalFindClose) || active.term.find.open {
		t.Fatal("the stable find close control did not close the bar")
	}
	assertPaneSizes := func(stage string) {
		t.Helper()
		for index := range state.sessions {
			session := &state.sessions[index]
			geometry := state.layoutForSession(session.ID)
			if cols, rows := session.term.screen.Size(); cols != geometry.cols || rows != geometry.rows {
				t.Fatalf("%s: session %d screen=%dx%d geometry=%dx%d", stage, session.ID, cols, rows, geometry.cols, geometry.rows)
			}
		}
	}
	assertPaneSizes("stable click close")
}
