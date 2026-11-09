package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
	"motty/internal/term"
	layoutcore "motty/internal/workspace"
)

func TestSharedTerminalToolbarStaysAtWorkspaceOuterEdge(t *testing.T) {
	for _, position := range []terminalToolbarPosition{terminalToolbarLeft, terminalToolbarRight} {
		t.Run(position.String(), func(t *testing.T) {
			state, _ := monitorFixture(t)
			session := &state.sessions[0]
			state.terminalToolbarPosition = position
			id := state.deck.ActiveWorkspace()
			context, _ := state.deck.Context(id)
			context.MonitorOpen = true
			context.MonitorWidth = 320
			context.MonitorLeft = position == terminalToolbarLeft
			context.Open = false
			if err := state.deck.SetContext(id, context); err != nil {
				t.Fatal(err)
			}
			size := gfx.Size{Width: 1200, Height: 800}
			state.Resize(size)
			var canvas gfx.Canvas
			state.Paint(&canvas, size)

			rail, ok := hitByID(&canvas, hitSFTPOpen)
			if !ok || rail.Bounds.Width != terminalBarToolSize {
				t.Fatalf("%s rail Files control=%+v, %v", position, rail.Bounds, ok)
			}
			full := state.fullWorkspacePixelBounds(size)
			inner := state.baseWorkspacePixelBounds(size)
			terminal := state.layoutForSession(session.ID).area
			bar, ok := state.terminalSideRailBounds(size)
			if !ok {
				t.Fatal("shared toolbar bounds are missing")
			}
			switch position {
			case terminalToolbarLeft:
				if session.term.railViewport.X != full.X+4 {
					t.Fatalf("left rail viewport x=%v, want outer x=%v", session.term.railViewport.X, full.X+4)
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
				if session.term.railViewport.X+session.term.railViewport.Width != wantX+terminalToolRailExtent-4 {
					t.Fatalf("right rail viewport=%+v, want its gutter at outer x=%v", session.term.railViewport, wantX)
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
	state.terminalToolbarPosition = terminalToolbarRight
	size := gfx.Size{Width: 1200, Height: 800}

	assertRecordHint := func(session *Session) {
		t.Helper()
		if !state.ActivatePane(session.ID) {
			t.Fatalf("activate session %d", session.ID)
		}
		var canvas gfx.Canvas
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
		if session.term.railCount == 0 {
			t.Fatal("active session does not own the shared rail")
		}
	}

	assertRecordHint(first)
	assertRecordHint(second)
}

func TestActiveSplitPaneBarKeepsConnectionIdentity(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	if len(state.sessions) < 2 {
		t.Fatalf("split created only %d sessions", len(state.sessions))
	}
	size := gfx.Size{Width: 1200, Height: 800}
	state.Resize(size)

	assertPaneBar := func(session *Session) {
		t.Helper()
		if !state.ActivatePane(session.ID) {
			t.Fatalf("activate session %d", session.ID)
		}
		var canvas gfx.Canvas
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
		foundTitle, foundStatus := false, false
		for _, text := range canvas.Texts() {
			if text.Bounds.Y < bounds.Y || text.Bounds.Y >= bounds.Y+infoBarHeight ||
				text.Bounds.X < bounds.X || text.Bounds.X >= bounds.X+bounds.Width {
				continue
			}
			foundTitle = foundTitle || text.Value == title
			foundStatus = foundStatus || text.Value == status
		}
		if !foundTitle || !foundStatus {
			t.Fatalf("active session %d title=%q found=%v status=%q found=%v in pane bar %+v",
				session.ID, title, foundTitle, status, foundStatus, bounds)
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
	size := gfx.Size{Width: 1200, Height: 800}
	state.Resize(size)

	for _, index := range []int{0, 1, 0} {
		if !state.ActivatePane(state.sessions[index].ID) {
			t.Fatalf("activate session %d", state.sessions[index].ID)
		}
		var canvas gfx.Canvas
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
	size := gfx.Size{Width: 1200, Height: 800}
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
	size := gfx.Size{Width: 1200, Height: 800}
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
	size := gfx.Size{Width: 1200, Height: 800}
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
	size := gfx.Size{Width: 1200, Height: 800}
	state.Resize(size)

	for index := range state.sessions {
		session := &state.sessions[index]
		if !state.ActivatePane(session.ID) {
			t.Fatalf("activate session %d", session.ID)
		}
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		hit, ok := hitByID(&canvas, tree.MountID(paneMount(session), hitTerminalList.At(int(session.ID))))
		if !ok {
			t.Fatalf("session %d has no mounted terminal hit", session.ID)
		}
		geometry := state.layoutForSession(session.ID)
		x := geometry.left + state.cellWidth*1.5
		y := geometry.top + state.cellHeight*0.5
		state.Pointer(gfx.PointerEvent{
			Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit.ID,
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
	size := gfx.Size{Width: 1200, Height: 800}
	state.Resize(size)
	session := &state.sessions[1]
	if !state.ActivatePane(session.ID) {
		t.Fatal("activate the second split")
	}
	shell := &fakeShell{}
	session.term.shell = shell
	session.term.screen.Write([]byte("\x1b[?1002h\x1b[?1006h"))

	var canvas gfx.Canvas
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
	result := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit.ID,
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
	size := gfx.Size{Width: 1200, Height: 800}
	state.Resize(size)
	session := &state.sessions[1]
	if !state.ActivatePane(session.ID) {
		t.Fatal("activate the second split")
	}
	for line := 0; line < 100; line++ {
		session.term.screen.Write([]byte("history line\r\n"))
	}

	var canvas gfx.Canvas
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
	result := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		Target: tree.MountID(paneMount(session), hitTerminalBar.At(int(session.ID))),
		X:      x, Y: grab, Viewport: size,
	})
	if !result.Handled || !session.term.barDragging {
		t.Fatalf("scrollbar press=%+v dragging=%v", result, session.term.barDragging)
	}
}

func TestSharedTerminalToolbarPlacesLatencyBeforeActions(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	state.terminalShowLatency = true
	state.terminalToolbarPosition = terminalToolbarRight
	atomic.StoreInt64(&session.rttMs, 42)
	atomic.StoreInt32(&session.rttAvailable, 1)
	size := gfx.Size{Width: 1200, Height: 800}
	state.Resize(size)

	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	if session.term.railCount == 0 {
		t.Fatal("the rail painted no actions")
	}
	first, ok := hitByID(&canvas, session.term.railIDs[0])
	if !ok {
		t.Fatal("first rail action is missing")
	}
	wantTop := session.term.railViewport.Y + 4
	if first.Bounds.Y != wantTop || first.Bounds.Width != terminalBarToolSize {
		t.Fatalf("first action = %+v, want %vpx high below latency at y=%v", first.Bounds, terminalBarToolSize, wantTop)
	}
	found := false
	for _, text := range canvas.Texts() {
		if text.Value == "42ms" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("latency is not painted above the action rail")
	}
}

func TestSharedTerminalToolbarSavesOutput(t *testing.T) {
	state, _, _, shell := liveSession(t)
	shell.say("first line\r\nsecond line\r\n")
	state.Wake()
	saved := filepath.Join(t.TempDir(), "terminal-output.txt")
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) { return saved, nil }
	size := gfx.Size{Width: 1200, Height: 800}
	state.Resize(size)

	var canvas gfx.Canvas
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
	state.Resize(gfx.Size{Width: 1200, Height: 800})
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
	for _, position := range []terminalToolbarPosition{terminalToolbarLeft, terminalToolbarRight} {
		t.Run(position.String(), func(t *testing.T) {
			state, session, _, _ := openTestSFTP(t)
			state.terminalToolbarPosition = position
			state.ai.open = true
			state.ai.panelWidth = 280
			workspace := state.deck.ActiveWorkspace()
			context, _ := state.deck.Context(workspace)
			context.Kind, context.Open, context.Side = layoutcore.ContextSFTP, true, layoutcore.ContextRight
			context.MonitorOpen, context.MonitorLeft = true, position == terminalToolbarLeft
			if err := state.deck.SetContext(workspace, context); err != nil {
				t.Fatal(err)
			}
			size := gfx.Size{Width: 1500, Height: 900}
			state.Resize(size)
			state.aiPanelSync()

			var canvas gfx.Canvas
			state.Paint(&canvas, size)
			rail, ok := state.terminalSideRailBounds(size)
			if !ok {
				t.Fatal("shared toolbar bounds are missing")
			}
			full := state.fullWorkspacePixelBounds(size)
			if position == terminalToolbarLeft {
				if rail.X != full.X {
					t.Fatalf("left rail x=%v, want outer-%v", rail.X, full.X)
				}
			} else if rail.X+rail.Width != full.X+full.Width {
				t.Fatalf("right rail right=%v, want outer-%v", rail.X+rail.Width, full.X+full.Width)
			}
			terminal := state.layoutForSession(session.ID).area
			for name, panel := range map[string]gfx.Rect{
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
	state.terminalToolbarPosition = terminalToolbarLeft
	size := gfx.Size{Width: 900, Height: 300}
	state.Resize(size)

	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	term := session.term
	if term.railCount < 3 || term.railContentExtent <= term.railViewport.Height {
		t.Fatalf("fixture does not overflow: count=%d content=%v viewport=%v", term.railCount, term.railContentExtent, term.railViewport)
	}
	lastID := term.railIDs[term.railCount-1]
	if _, visible := hitByID(&canvas, lastID); visible {
		t.Fatal("the final control unexpectedly begins in the visible rail")
	}

	target := term.railIDs[0]
	for target != lastID {
		result := state.Key(gfx.KeyEvent{Target: target, Key: gfx.KeyDown})
		if !result.Handled || result.Focus.IsZero() || result.Focus == target {
			t.Fatalf("Down from %v returned %+v", target, result)
		}
		target = result.Focus
	}

	canvas.Reset()
	canvas.SetInteraction(gfx.ID{}, gfx.ID{}, lastID, true)
	state.Paint(&canvas, size)
	if term.railScroll <= 0 {
		t.Fatal("keyboard focus did not scroll the overflowed shared rail")
	}
	if _, visible := hitByID(&canvas, lastID); !visible {
		t.Fatal("the keyboard-focused overflow control was not brought into view")
	}
}

func TestSharedTerminalToolbarKeyboardNavigationSkipsDisabledActions(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	size := gfx.Size{Width: 900, Height: 700}
	state.Resize(size)

	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	term := session.term
	disabled := -1
	for index := 0; index < term.railCount; index++ {
		if !term.railEnabled[index] {
			disabled = index
			break
		}
	}
	if disabled <= 0 || disabled >= term.railCount-1 {
		t.Fatalf("fixture has no usable disabled action: count=%d disabled=%d", term.railCount, disabled)
	}
	before, after := disabled-1, disabled+1
	for before >= 0 && !term.railEnabled[before] {
		before--
	}
	for after < term.railCount && !term.railEnabled[after] {
		after++
	}
	if before < 0 || after >= term.railCount {
		t.Fatalf("disabled action is not between enabled actions: before=%d disabled=%d after=%d", before, disabled, after)
	}

	result, handled := state.terminalToolbarKey(session, gfx.KeyEvent{Target: term.railIDs[before], Key: gfx.KeyDown})
	if !handled || !result.Handled || result.Focus != term.railIDs[after] {
		t.Fatalf("Down across disabled action = %+v handled=%v, want focus %v", result, handled, term.railIDs[after])
	}
	result, handled = state.terminalToolbarKey(session, gfx.KeyEvent{Target: term.railIDs[after], Key: gfx.KeyUp})
	if !handled || !result.Handled || result.Focus != term.railIDs[before] {
		t.Fatalf("Up across disabled action = %+v handled=%v, want focus %v", result, handled, term.railIDs[before])
	}
}

func TestWheelOverSharedTerminalToolbarDoesNotScrollTerminalHistory(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	state.terminalToolbarPosition = terminalToolbarRight
	size := gfx.Size{Width: 900, Height: 300}
	state.Resize(size)
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	if session.term.railContentExtent <= session.term.railViewport.Height {
		t.Fatal("fixture does not overflow")
	}
	before := session.term.screen.ViewOffset()
	rail := session.term.railViewport
	result := state.scroll(gfx.ScrollEvent{X: rail.X + 2, Y: rail.Y + 2, DeltaY: 1})
	if !result.Handled || session.term.railScroll <= 0 {
		t.Fatalf("wheel result=%+v offset=%v", result, session.term.railScroll)
	}
	if session.term.screen.ViewOffset() != before {
		t.Fatal("wheel over the shared rail moved terminal history")
	}
}

func TestWheelOverSharedTerminalToolbarGutterStaysInTheRail(t *testing.T) {
	for _, position := range []terminalToolbarPosition{terminalToolbarLeft, terminalToolbarRight} {
		t.Run(position.String(), func(t *testing.T) {
			state, _, _, _ := liveSession(t)
			session := &state.sessions[0]
			state.terminalToolbarPosition = position
			size := gfx.Size{Width: 900, Height: 300}
			state.Resize(size)
			for line := 0; line < 80; line++ {
				session.term.screen.Write([]byte("history line\r\n"))
			}
			var canvas gfx.Canvas
			state.Paint(&canvas, size)
			if session.term.railContentExtent <= session.term.railViewport.Height {
				t.Fatal("fixture does not overflow")
			}
			before := session.term.screen.ViewOffset()
			rail := session.term.railBounds
			x := rail.X + 1
			if position == terminalToolbarRight {
				x = rail.X + rail.Width - 1
			}
			result := state.scroll(gfx.ScrollEvent{X: x, Y: rail.Y + 2, DeltaY: 1})
			if !result.Handled || session.term.railScroll <= 0 {
				t.Fatalf("gutter wheel result=%+v offset=%v", result, session.term.railScroll)
			}
			if session.term.screen.ViewOffset() != before {
				t.Fatal("wheel over the rail gutter moved terminal history")
			}
		})
	}
}

func TestActivatingTerminalWorkspaceFitsShellToSharedRail(t *testing.T) {
	state, _, _, shell := liveSession(t)
	size := gfx.Size{Width: 1200, Height: 800}
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

func TestTerminalToolbarPositionPersistsAndInvalidValuesFallBackToRight(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	if !state.setTerminalToolbarPosition(terminalToolbarLeft) {
		t.Fatal("changing the toolbar position was not reported")
	}
	state.save()
	saved, _, err := loadState(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.TerminalToolbarPosition != terminalToolbarLeft.String() {
		t.Fatalf("saved toolbar position = %q, want %q", saved.TerminalToolbarPosition, terminalToolbarLeft.String())
	}
	if got := terminalToolbarPositionFromString("top"); got != terminalToolbarRight {
		t.Fatalf("unknown saved position became %v, want right", got)
	}
}

func TestTerminalToolbarSettingsApplyImmediatelyAndNavigate(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	state.activeSection = sectionSettings
	session.term.railScroll = 90

	if !state.clickSettings(hitSettingsToolbarLeft) {
		t.Fatal("toolbar position setting was not handled")
	}
	if state.terminalToolbarPosition != terminalToolbarLeft || session.term.railScroll != 0 {
		t.Fatalf("position=%v scroll=%v after immediate setting", state.terminalToolbarPosition, session.term.railScroll)
	}
	geometry := state.layoutForSession(session.ID)
	if cols, rows := session.term.screen.Size(); cols != geometry.cols || rows != geometry.rows {
		t.Fatalf("background terminal screen=%dx%d geometry=%dx%d after toolbar setting", cols, rows, geometry.cols, geometry.rows)
	}

	result, handled := state.settingsKey(gfx.KeyEvent{Target: hitSettingsToolbarLeft, Key: gfx.KeyRight})
	if !handled || !result.Handled || result.Focus != hitSettingsToolbarRight || state.terminalToolbarPosition != terminalToolbarRight {
		t.Fatalf("position keyboard result=%+v handled=%v position=%v", result, handled, state.terminalToolbarPosition)
	}
}

func TestTerminalFindKeepsTheSharedRailWhileSearching(t *testing.T) {
	for _, position := range []terminalToolbarPosition{terminalToolbarLeft, terminalToolbarRight} {
		t.Run(position.String(), func(t *testing.T) {
			state, _, _, _ := liveSession(t)
			session := &state.sessions[0]
			state.terminalToolbarPosition = position
			size := gfx.Size{Width: 900, Height: 700}
			state.Resize(size)

			before := state.layoutForSession(session.ID)
			state.openTerminalFind(session)
			if _, ok := state.terminalSideRailSession(); !ok {
				t.Fatal("find mode hid the shared rail")
			}
			if reserve := state.toolbarDockSideReserveFor(position == terminalToolbarLeft, true); reserve != state.terminalToolbarMetrics().railExtent {
				t.Fatalf("find mode left %v pixels reserved, want the rail extent", reserve)
			}
			// The bar sits above the grid, so the rail and the columns it shares
			// stay put: the pane does not get wider when a search opens.
			if after := state.layoutForSession(session.ID); after.cols != before.cols || after.left != before.left {
				t.Fatalf("opening find changed the columns: %+v, was %+v", after, before)
			}
			geometry := state.layoutForSession(session.ID)
			cols, rows := session.term.screen.Size()
			if cols != geometry.cols || rows != geometry.rows {
				t.Fatalf("find-open terminal=%dx%d geometry=%dx%d", cols, rows, geometry.cols, geometry.rows)
			}

			state.closeTerminalFind(session)
			if _, ok := state.terminalSideRailSession(); !ok {
				t.Fatal("closing find lost the shared rail")
			}
			if layout := state.layoutForSession(session.ID); layout != before {
				t.Fatalf("closing find did not restore the grid: %+v, was %+v", layout, before)
			}
			geometry = state.layoutForSession(session.ID)
			cols, rows = session.term.screen.Size()
			if cols != geometry.cols || rows != geometry.rows {
				t.Fatalf("find-closed terminal=%dx%d geometry=%dx%d", cols, rows, geometry.cols, geometry.rows)
			}
		})
	}
}

// The search bar belongs above the grid, so a search never narrows the pane:
// the columns the user's text was wrapped to stay the same. Rows do change,
// which is why the selection cannot survive — that is the grid following the
// bar, not the rail disappearing.
func TestTerminalFindKeepsTheColumnCount(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	state.Resize(gfx.Size{Width: 900, Height: 700})
	shell.say("keep me\r\n")
	state.Wake()

	cols, _ := session.term.screen.Size()
	before := state.layoutForSession(session.ID)

	state.openTerminalFind(session)
	if gotCols, _ := session.term.screen.Size(); gotCols != cols {
		t.Fatalf("opening find changed the column count: %d, was %d", gotCols, cols)
	}
	if after := state.layoutForSession(session.ID); after.cols != before.cols || after.left != before.left {
		t.Fatalf("opening find moved the grid sideways: %+v, was %+v", after, before)
	}

	state.closeTerminalFind(session)
	if gotCols, _ := session.term.screen.Size(); gotCols != cols {
		t.Fatalf("closing find changed the column count: %d, was %d", gotCols, cols)
	}
	if after := state.layoutForSession(session.ID); after != before {
		t.Fatalf("closing find did not restore the grid: %+v, was %+v", after, before)
	}
}

func TestTerminalFindResizesEveryPaneForTheSharedRail(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	if len(state.sessions) < 2 {
		t.Fatalf("split created only %d sessions", len(state.sessions))
	}
	size := gfx.Size{Width: 1200, Height: 800}
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
	size := gfx.Size{Width: 1200, Height: 800}
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
