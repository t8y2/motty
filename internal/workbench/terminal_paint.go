package workbench

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	"motty/internal/term"
	layoutcore "motty/internal/workspace"
)

// spinnerStep is how long a spinner spoke stays lit.
const spinnerStep = 125 * time.Millisecond

// barTool is one icon button on the right of an active pane's bar.
type barTool struct {
	id       gfx.ID
	icon     uint8
	hint     string
	disabled bool
	style    iconButtonStyle
	groupEnd bool // the group of pane controls ends here: a wider gap follows
	rect     gfx.Rect
}

// maxBarTools is how many tools the shared rail can hold: transfer, Files, save
// output, export log, export recording, record, the resource monitor, remote
// tools, close, the two splits and broadcast.
const maxBarTools = 32 // includes relocated window actions

const (
	terminalToolRailExtent = float32(40)
	barToolSize            = float32(24)
	barToolGap             = float32(4)
	terminalBarToolSize    = float32(32)
	terminalBarToolGap     = float32(4)
)

// paintTerminalPaneDetails paints what the pane declares but the page does not
// own as elements: the find bar's text surface, the spinner and status light of
// a split bar, the completion popover and the input notice. It runs after the
// page painted the pane, clipped to the pane as the old embedded view was.
//
// Active panes get a configurable tool rail with only connection latency and
// actions. Every split pane reserves the same bar height and identifies its own
// session there, so switching focus cannot move or resize a PTY. Find mode puts
// its search controls in that strip, as a macOS terminal puts its search bar
// above the text rather than over it, and leaves the shared rail where it is.
func (state *State) paintTerminalPaneDetails(canvas *gfx.Canvas, session *Session, bounds gfx.Rect) {
	if session.term == nil {
		return
	}
	state.settleShellResize(canvas, session)
	state.wantLatencyProbe(session, canvas)
	t := session.term
	canvas.Clip(bounds)
	defer canvas.Unclip()
	if t.paintConnectionBar.Valid() {
		state.paintConnectionTopBar(canvas, t.paintConnectionBar.Rect(), session)
	}
	switch {
	case t.paintFinding:
		state.paintFindBarSlots(canvas, session, t.paintFind)
	case t.paintInSplit:
		switch {
		case session.busy:
			phase := components.SpinnerPhase(canvas.Now() / spinnerStep)
			components.PaintSpinner(canvas, state.Theme(), t.paintSpinner.Rect(), phase)
			canvas.RequestFrameIn(spinnerStep)
		case !t.paintInactive:
			// Only the active pane declares the status-light slot. Calling Rect on
			// the empty box used by an inactive pane would dereference nil.
			spot := t.paintLight.Rect()
			kind, dot := state.sessionLight(session)
			paintLight(canvas, spot.X+spot.Width/2, spot.Y+spot.Height/2, kind, dot, t.paintBarBackground)
		}
	}
	if t.paintCommandNoticeShown {
		state.paintWorkspaceCommandNotice(canvas, session, t.paintCommandNotice.Rect())
	}
	state.paintCompletionList(canvas, session, t.paintGeometry)
	state.paintAutofillPick(canvas, session, t.paintGeometry)
	if t.paintNoticeShown {
		state.paintTerminalInputNotice(canvas, session, t.paintNotice.Rect())
	}
}

func (state *State) activeTerminalTools(session *Session, toolList *[maxBarTools]barTool) []barTool {
	if session == nil || session.term == nil {
		return nil
	}
	tools := toolList[:0]
	notesStyle := iconButtonQuiet
	if state.notesPanelOpen() {
		notesStyle = iconButtonActive
	}
	tools = append(tools, barTool{id: hitNotesPanel, icon: iconNotebook, hint: state.noteText("Notes", "笔记"), style: notesStyle})
	if session.hasServer() {
		tools = append(tools, barTool{id: hitSFTPDetach, icon: iconSFTPTransfer, hint: state.messages.SFTPWindow.Open, disabled: !session.hasServer() || session.phase != sshconn.PhaseConnected || !state.sftpManagersAvailable(), style: iconButtonQuiet})
	}
	filesStyle := iconButtonQuiet
	if state.filesPanelShown() {
		filesStyle = iconButtonActive
	}
	filesHint := state.messages.TipFiles
	if session.isLocal() {
		filesHint = state.messages.TipFilesLocal
	}
	tools = append(tools, barTool{id: hitSFTPOpen, icon: iconFolderOpen, hint: filesHint, disabled: !session.canBrowseFiles(), style: filesStyle})
	if state.localProxyActionVisible(session) {
		tool := barTool{id: hitLocalProxyOpen, icon: iconGlobe, hint: state.messages.LocalProxy.Title}
		if snapshot := state.localProxySnapshot(session); snapshot.Phase != localProxyDisconnected {
			tool.style = iconButtonActive
			if snapshot.Phase == localProxyConnected && snapshot.RemotePort != 0 {
				tool.hint = fmt.Sprintf(state.messages.LocalProxy.ConnectedFmt, snapshot.RemotePort)
			}
		}
		tools = append(tools, tool)
	}
	tools = append(tools, barTool{id: hitConnExport, icon: iconFileDown, hint: state.messages.ConnExportLog, style: iconButtonQuiet})
	tools = append(tools, barTool{id: hitTerminalRecordingExport, icon: iconFilm, hint: state.messages.RecordingExport, disabled: len(session.term.recording.events) == 0, style: iconButtonQuiet})
	recordIcon, recordHint, recordStyle := uint8(iconRecord), state.messages.TipRecordStart, iconButtonQuiet
	if session.term.recording.active {
		recordIcon, recordHint, recordStyle = iconRecordStop, state.messages.TipRecordStop, iconButtonDanger
	}
	tools = append(tools, barTool{id: hitTerminalRecord, icon: recordIcon, hint: recordHint, disabled: session.term.shell == nil, style: recordStyle})
	tools = append(tools, barTool{id: hitTerminalSaveOutput, icon: iconSave, hint: state.messages.Tools.MenuSaveOutput, style: iconButtonQuiet})
	if hint, style, offered := state.remoteStatsToggle(session); offered {
		tools = append(tools, barTool{id: hitRemoteStats, icon: iconActivity, hint: hint, style: style})
	}
	tools = append(tools, barTool{id: hitProcessToolsOpen, icon: iconProcess, hint: state.messages.Tools.RemoteMenu, disabled: !state.remoteToolsOffered(session), style: iconButtonQuiet})
	inSplit := state.paneInSplit(session.ID)
	if inSplit {
		tools = append(tools, barTool{id: hitPaneClose, icon: iconClose, hint: state.messages.PaneClose, style: iconButtonQuiet, groupEnd: true})
	}
	managed := state.workspaceManaged(state.deck.ActiveWorkspace())
	verticalDisabled := managed && !state.canSplitActivePane(layoutcore.Vertical)
	horizontalDisabled := managed && !state.canSplitActivePane(layoutcore.Horizontal)
	tools = append(tools, barTool{id: hitTerminalSnippets, icon: iconNote, hint: state.messages.Tools.SnippetTip, disabled: session.term.shell == nil, style: iconButtonQuiet})
	tools = append(tools, barTool{id: hitPaneSplitVertical, icon: iconRows2, hint: state.messages.PaneSplitVertical, disabled: verticalDisabled, style: iconButtonQuiet, groupEnd: !inSplit})
	tools = append(tools, barTool{id: hitPaneSplitHorizontal, icon: iconColumns2, hint: state.messages.PaneSplitHorizontal, disabled: horizontalDisabled, style: iconButtonQuiet})
	if inSplit && !managed {
		hint, style := state.messages.Tools.BroadcastStart, iconButtonQuiet
		enabled := workspaceCanBroadcast(state.workspaceConnectionState(state.deck.ActiveWorkspace()))
		if state.broadcasting(session.ID) {
			hint, style = state.messages.Tools.BroadcastStop, iconButtonActive
			enabled = true
		}
		tools = append(tools, barTool{id: hitBroadcast, icon: iconRadio, hint: hint, disabled: !enabled, style: style})
	}
	ordered := state.orderToolbarTools(toolbarTerminal, tools, false)
	if session.showLog && !session.term.live {
		ordered = append(ordered, barTool{})
		copy(ordered[1:], ordered[:len(ordered)-1])
		ordered[0] = barTool{
			id: hitConnProgress, icon: iconChevronLeft, hint: state.messages.ConnShowProgress,
			style: iconButtonQuiet,
		}
	}
	if managed {
		ordered = state.compactWorkspacePaneTools(ordered, session.ID, state.viewport.Width)
	}
	// The latency readout is a status, not an action: it stays on the shared
	// rail (compaction keeps it visible) and is placed by the toolbar layout.
	// The value is formatted by the badge's own paint, not here: this list is
	// rebuilt several times per frame.
	if state.latencyAvailable(session) {
		ordered = append(ordered, barTool{id: hitToolbarLatency, icon: iconActivity})
	}
	return ordered
}

func (state *State) setTerminalRailTools(session *Session, tools []barTool) {
	if session == nil || session.term == nil {
		return
	}
	session.term.railCount = min(len(tools), maxBarTools)
	for index := 0; index < session.term.railCount; index++ {
		session.term.railIDs[index] = tools[index].id
		// The readout has no action: the keyboard visits only real controls.
		session.term.railEnabled[index] = !tools[index].disabled && tools[index].id != hitToolbarLatency
	}
}

func (state *State) clearTerminalRail(session *Session) {
	if session == nil || session.term == nil {
		return
	}
	session.term.railCount = 0
	session.term.railIDs = [maxBarTools]gfx.ID{}
	session.term.railEnabled = [maxBarTools]bool{}
	session.term.railViewport = gfx.Rect{}
	session.term.railBounds = gfx.Rect{}
	session.term.railContentExtent = 0
}

// paintWorkspaceSideRail paints the one shared rail at the workspace's outer
// edge. The active split supplies both the tools and the state they act on.
func (state *State) paintWorkspaceSideRail(canvas *gfx.Canvas, size gfx.Size) {
	state.paintToolbarDockSide(canvas, size)
}

// paintTerminalSideRail draws the vertical tool rail. The latency readout is
// an ordinary layout entry now, so it scrolls and moves with the actions.
func (state *State) paintTerminalSideRail(canvas *gfx.Canvas, bounds gfx.Rect, session *Session, tools []barTool, background gfx.Color) {
	if bounds.Width <= 0 || bounds.Height <= 0 || session.term == nil {
		return
	}
	session.term.railBounds = bounds
	canvas.Fill(bounds, background)
	viewport := gfx.Rect{X: bounds.X + 4, Y: bounds.Y + 4, Width: max(bounds.Width-8, 0), Height: max(bounds.Height-8, 0)}
	state.paintTerminalRailTools(canvas, session, viewport, tools, background)
}

func (state *State) paintTerminalRailTools(canvas *gfx.Canvas, session *Session, viewport gfx.Rect, tools []barTool, background gfx.Color) {
	t := session.term
	metrics := state.terminalToolbarMetrics()
	t.railViewport = viewport
	t.railContentExtent = state.terminalRailContentExtent(tools)
	viewportExtent := viewport.Height
	maxScroll := max(t.railContentExtent-viewportExtent, 0)
	t.railScroll = min(max(t.railScroll, 0), maxScroll)

	// A keyboard move to a tool outside the viewport brings it into view on the
	// next paint. Pointer hover and wheel scrolling never move focus.
	for index := range tools {
		if tools[index].id == canvas.FocusVisible() {
			start := state.terminalRailItemStart(tools, index)
			end := start + metrics.toolSize
			if start-t.railScroll < 0 {
				t.railScroll = min(start, maxScroll)
			} else if end-t.railScroll > viewportExtent {
				t.railScroll = min(end-viewportExtent, maxScroll)
			}
			break
		}
	}

	canvas.Clip(viewport)
	for index := range tools {
		offset := state.terminalRailItemStart(tools, index)
		tool := &tools[index]
		tool.rect = gfx.Rect{X: viewport.X, Y: viewport.Y + offset - t.railScroll, Width: min(metrics.toolSize, viewport.Width), Height: metrics.toolSize}
		if tool.rect.X+tool.rect.Width <= viewport.X || tool.rect.X >= viewport.X+viewport.Width ||
			tool.rect.Y+tool.rect.Height <= viewport.Y || tool.rect.Y >= viewport.Y+viewport.Height {
			continue
		}
		zone := toolbarDockSideTop
		if state.terminalToolbarPosition == terminalToolbarLeft {
			zone = toolbarDockLeftTop
		}
		if tool.id == hitToolbarLatency {
			if !state.paintToolbarLatency(canvas, tool.rect, session) {
				continue
			}
		} else {
			state.paintIconButtonSized(canvas, tool.id, tool.icon, tool.rect, tool.hint, tool.disabled, tool.style, background, metrics.iconSize)
		}
		state.registerToolbarDockTool(canvas, *tool, zone, intersectRect(tool.rect, viewport))
		if tool.id == hitWorkspacePaneTools {
			state.workspacePaneTools.anchor = tool.rect
		}
	}
	canvas.Unclip()
}

func (state *State) terminalRailContentExtent(tools []barTool) float32 {
	if len(tools) == 0 {
		return 8
	}
	return state.terminalRailItemStart(tools, len(tools)-1) + state.terminalToolbarMetrics().toolSize + 4
}

func (state *State) terminalRailItemStart(tools []barTool, index int) float32 {
	metrics := state.terminalToolbarMetrics()
	start := float32(4)
	for previous := 0; previous < index; previous++ {
		start += metrics.toolSize + metrics.gap
		if tools[previous].groupEnd {
			start += metrics.gap
		}
	}
	return start
}

// paintRailLatency paints the RTT badge as a dockable status control. The
// target is the same id the dock uses for its drag and context menu.
func (state *State) paintRailLatency(canvas *gfx.Canvas, text string, color gfx.Color, bounds gfx.Rect, compact bool) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	state.paintRailLatencyVisual(canvas, text, color, bounds, compact)
	canvas.HitWithOptions(hitToolbarLatency, bounds, text, gfx.HitOptions{
		Role: gfx.RoleStatus, SkipFocus: true, Value: text, Hint: text,
	})
}

// paintRailLatencyVisual paints only the badge. The drag preview uses it so the
// floating copy does not declare a second target under the pointer.
func (state *State) paintRailLatencyVisual(canvas *gfx.Canvas, text string, color gfx.Color, bounds gfx.Rect, compact bool) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	label := text
	if compact {
		label = strings.TrimPrefix(text, "RTT ")
	}
	canvas.Rounded(bounds, min(radius.control, bounds.Height/2), withAlpha(color, 0.14))
	state.paintRailLabelCenter(canvas, label, bounds, color, compact)
}

// paintToolbarLatency paints a docked latency readout and reports whether a
// sample existed. A reading that went away between layout and paint leaves no
// empty slot behind.
func (state *State) paintToolbarLatency(canvas *gfx.Canvas, bounds gfx.Rect, session *Session) bool {
	text, color, ok := state.latencyInfo(session)
	if !ok {
		return false
	}
	state.paintRailLatency(canvas, text, color, bounds, true)
	return true
}

func (state *State) paintRailLabelCenter(canvas *gfx.Canvas, text string, bounds gfx.Rect, color gfx.Color, compact bool) {
	size := metrics.FontSizeS
	available := max(bounds.Width-4, 0)
	if compact {
		size = 10
		// When the slot is narrow, step down the font size to keep the number
		// readable instead of eliding it.
		for size > 8 && canvas.TextWidthWeight(text, size, gfx.WeightMedium) > available {
			size--
		}
	}
	fitted := elide(canvas, text, size, gfx.WeightMedium, available)
	canvas.LabelCenter(fitted, bounds, gfx.TextStyle{Size: size, Weight: gfx.WeightMedium, Color: color})
}

func (state *State) scrollTerminalToolbar(session *Session, event gfx.ScrollEvent) (gfx.EventResult, bool) {
	if session == nil || session.term == nil ||
		session.term.railBounds.Width <= 0 || session.term.railBounds.Height <= 0 ||
		!pointInRect(event.X, event.Y, session.term.railBounds) {
		return gfx.EventResult{}, false
	}
	t := session.term
	viewportExtent := t.railViewport.Height
	metrics := state.terminalToolbarMetrics()
	delta := event.StepY(metrics.toolSize + metrics.gap)
	maxScroll := max(t.railContentExtent-viewportExtent, 0)
	next := min(max(t.railScroll+delta, 0), maxScroll)
	if next == t.railScroll {
		return gfx.EventResult{Handled: true}, true
	}
	t.railScroll = next
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

// paintAIMarking draws the strip that says this line's command was started by
// the assistant. It sits in the terminal's left padding, outside the grid, so it
// is not part of the copied command or the emulator's screen cells.
func (state *State) paintAIMarking(canvas *gfx.Canvas, geometry terminalGeometry, y, cellHeight float32) {
	x := geometry.area.X + 4
	canvas.Fill(gfx.Rect{X: x, Y: y, Width: 3, Height: cellHeight}, withAlpha(state.pal.accent, 0.85))
}

// declarePaneBar identifies a pane that shares its workspace. The active pane
// starts with a status light; inactive panes use the terminal icon so the light
// remains a reliable focus marker. Both show the name and connection state, and
// the active bar is the only one that can carry a busy spinner in that slot.
// Resource readings stay in the dedicated monitor surface.
func (state *State) declarePaneBar(c ui.C, width float32, status string, session *Session, active bool) (spinner, light ui.Box) {
	pal := state.pal
	readout := status
	if state.workspaceManaged(state.deck.ActiveWorkspace()) && session.quietOK() {
		readout = ""
	}
	ui.Row(c).Fill().PadX(termPadX).Items(ui.Center).Children(func() {
		if active {
			light = ui.Slot(c).Size(16, 16).Shrink(0).MarginSides(0, 7, 0, 0)
		} else {
			// The idle marker keeps the light's 16 point box, so the connection
			// bar beside it does not move when the pane gains or loses the
			// keyboard.
			ui.Row(c).Size(16, 16).Shrink(0).MarginSides(0, 7, 0, 0).Center().Children(func() {
				ui.Icon(c, lucideIcons[iconTerminal]).Size(14, 14).Color(pal.faint)
			})
		}
		// The pane's own bar is the connection bar: address, actions and, when
		// the setting is on, the six figures.
		session.term.paintConnectionBar = ui.Slot(c).Grow(1).Shrink(1).MinWidth(0).FillHeight()
		if !state.connectionStatsBarShown(session) {
			state.declareLatencyTag(c, session)
		}
		if session.busy {
			spinner = ui.Slot(c).Size(14, 14).MarginSides(0, 9, 0, 0)
		}
		readoutColor := pal.faint
		if active {
			readoutColor = pal.muted
		}
		ui.Text(c, readout).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(readoutColor).LineHeight(20).
			MaxWidth(width * 0.42).Ellipsis().Shrink(1)
		if state.broadcasting(session.ID) {
			// Typing goes to this pane too: the one mark an inactive bar carries.
			ui.Icon(c, lucideIcons[iconRadio]).Size(14, 14).Color(pal.accent).MarginSides(0, 0, 0, 8)
		}
		state.declarePaneBarClose(c, session)
	})
	return spinner, light
}

// declarePaneBarClose is the close control at a split pane's bar: every pane
// carries one, whichever pane has the keyboard, so closing a pane never requires
// finding it in the shared rail first. The control closes the pane its bar
// belongs to, not whichever pane has the keyboard; a press on it is a hit of its
// own, so it neither switches panes nor starts a pane drag.
func (state *State) declarePaneBarClose(c ui.C, session *Session) {
	state.declareIconButtonWithOptions(c, hitPaneBarCloseList.At(int(session.ID)), iconClose, state.messages.PaneClose, false, iconButtonQuiet, 14, gfx.HitOptions{}).
		Size(barToolSize, barToolSize).MarginSides(0, 0, 0, 6)
}

// paintGrid draws a session's character grid: for each row its background
// spans and one fixed-pitch label, then the cursor. Rows are prepared once per
// change of their content; painting a screen that did not change builds
// nothing.
func (state *State) paintGrid(canvas *gfx.Canvas, session *Session, geometry terminalGeometry) {
	session.term.pageIdentity(session.ID)
	state.paintGridWithID(canvas, session, geometry, session.term.controlID, false)
	state.paintTerminalScrollbar(canvas, session, geometry)
}

// paintGridControl paints the grid of a terminal control: the control already
// declares the hit that focuses it, so only the cells are painted.
func (state *State) paintGridControl(canvas *gfx.Canvas, session *Session, geometry terminalGeometry, focused bool) {
	state.paintGridBody(canvas, session, geometry, gfx.ID{}, false, focused, false)
	state.paintTerminalScrollbar(canvas, session, geometry)
}

func (state *State) paintGridWithID(canvas *gfx.Canvas, session *Session, geometry terminalGeometry, id gfx.ID, sensitive bool) {
	state.paintGridBody(canvas, session, geometry, id, sensitive, terminalGridFocused(canvas, session, canvas.Focused() == id), true)
}

func (state *State) paintGridBody(canvas *gfx.Canvas, session *Session, geometry terminalGeometry, id gfx.ID, sensitive, focused, declareHit bool) {
	t, pal := session.term, &state.termPal
	inactive := state.paneIsInactive(session.ID)
	t.expireSync()
	cols, rows := t.presentedSize()
	cellWidth, cellHeight := state.cellWidth, state.cellHeight
	cursor := t.presentedCursor()
	composing := t.preedit.Composing
	predictionText, predictionRuns, predictionCells, predicting := t.prediction.overlay(state.now())
	predicting = predicting && !composing && cursor.Visible && t.sync == nil
	if remaining, ok := t.prediction.expiry(state.now()); ok && predicting {
		// A guess the server never echoes is taken back at its deadline, not at
		// the next key.
		canvas.RequestFrameIn(max(remaining, time.Millisecond))
	}
	if t.sync != nil && t.clock != nil {
		canvas.RequestFrameIn(t.syncRemaining())
	}
	// caretX is where the next character goes: past the text typed ahead of the
	// echo, so every cursor shape keeps showing an insertion point while the
	// server catches up. It is the emulator's own column once nothing is pending.
	caretX := cursor.X
	if predicting {
		caretX += predictionCells
	}
	// The cursor blinks while the terminal has the keyboard and something is
	// happening; it is solid from the moment it moves or the pane is entered.
	blinkOn := true
	if cursor.Visible && focused && !composing && !state.terminalCursorSteady && !cursor.Steady {
		blinkOn = t.blink.on(canvas, caretX, cursor.Y)
	} else {
		t.blink.rest(caretX, cursor.Y)
	}
	if t.sync != nil {
		blinkOn = true
	}
	// A block cursor is an inverted cell prepared by the row cache, so it follows
	// the caret rather than being dropped while a guess is on screen.
	block := cursor.Visible && cursor.Style == term.CursorBlock && focused && !composing && blinkOn
	// The input method draws its composition over the cells from the cursor on,
	// where it replaces whatever the program put there. A program that draws its
	// own cursor (a prompt with placeholder text) hides the real one, so the
	// cells are covered whether the cursor shows or not.
	var covered cellSpan
	if composing && cursor.Y < rows {
		covered = cellSpan{from: cursor.X, to: cursor.X + displayCells(t.preedit.Value)}
	}
	style := gfx.TextStyle{Size: state.terminalFontSize, Color: pal.termFG, MonoFace: state.terminalFontFace}
	// The strip is empty on the alternate screen, whose program owns the grid.
	gutterShown := !sensitive && state.terminalGutter != gutterOff && !t.presentedAltScreen() && t.sync == nil
	gutterNumberStyle, gutterTimeStyle := gutterStyles(state.terminalFontSize, state.terminalFontFace, pal)
	labelWidth := float32(math.Ceil(float64(float32(cols)*cellWidth))) + 1

	// A find scan deferred by the refresh interval gets the frame that runs it.
	if t.find.pending {
		delay := terminalFindRefreshInterval
		if t.find.lastScan > 0 {
			delay = max(terminalFindRefreshInterval-(state.now()-t.find.lastScan), 0)
		}
		canvas.RequestFrameIn(delay)
	}
	if t.selection.dragging && t.selection.edge.direction != 0 {
		canvas.RequestFrameIn(selectionScrollInterval)
	}
	canvas.Clip(geometry.area)
	t.cache.begin()
	if cap(t.rowIDs) < rows {
		t.rowIDs = make([]uint64, rows)
	} else {
		t.rowIDs = t.rowIDs[:rows]
	}
	firstFindMatch := 0
	finding := t.sync == nil && t.find.open && len(t.find.matches) > 0
	var findInk, findBackground gfx.Color
	if finding {
		findInk, findBackground = state.findHighlightColors(false)
	}
	for row := 0; row < rows; row++ {
		column := -1
		if block && row == cursor.Y {
			column = caretX
		}
		line := t.presentedRow(row)
		t.rowIDs[row] = line.ID
		entry := t.cache.row(line, cols, column, coveredOn(covered, row == cursor.Y), pal, inactive)
		y := geometry.top + float32(row)*cellHeight
		for _, span := range entry.spans {
			from := snap(geometry.left + float32(span.from)*cellWidth)
			to := snap(geometry.left + float32(span.to)*cellWidth)
			canvas.Fill(gfx.Rect{X: from, Y: y, Width: to - from, Height: cellHeight}, span.color)
		}
		if finding {
			firstFindMatch = state.paintTerminalFindMatches(canvas, &t.find, geometry, row, t.screen.ViewLine(row), firstFindMatch, findInk, findBackground)
		}
	}
	if t.sync == nil {
		state.paintTerminalImages(canvas, t, geometry, true, t.rowIDs)
	}
	if gutterShown {
		// The strip sits on its own faintly lifted panel, edge to rule.
		edge := geometry.left - gutterRuleGap/2
		canvas.Fill(gfx.Rect{X: geometry.area.X, Y: geometry.area.Y, Width: edge - geometry.area.X, Height: geometry.area.Height}, withAlpha(pal.termFG, 0.035))
	}
	firstFindMatch = 0
	for row := 0; row < rows; row++ {
		column := -1
		if block && row == cursor.Y {
			column = caretX
		}
		entry := t.cache.row(t.presentedRow(row), cols, column, coveredOn(covered, row == cursor.Y), pal, inactive)
		y := geometry.top + float32(row)*cellHeight
		if from, to, selected := t.selection.span(t.screen.LineNumber(row), t.presentedRow(row), cols); selected {
			left := snap(geometry.left + float32(from)*cellWidth)
			right := snap(geometry.left + float32(to)*cellWidth)
			canvas.Fill(gfx.Rect{X: left, Y: y, Width: right - left, Height: cellHeight}, withAlpha(pal.termFG, 0.25))
		}
		if gutterShown && row <= cursor.Y {
			// Rows below the cursor have not been written yet.
			state.paintGutter(canvas, entry, t, geometry, row, y, gutterNumberStyle, gutterTimeStyle)
		}
		if !t.presentedAltScreen() && row <= cursor.Y {
			if _, ok := t.aiRows[t.rowIDs[row]]; ok {
				state.paintAIMarking(canvas, geometry, y, cellHeight)
			}
		}
		if len(entry.runs) > 0 {
			last := entry.runs[len(entry.runs)-1]
			rowCols := min(int(last.Column+last.Cells)+1, cols)
			rowWidth := min(float32(math.Ceil(float64(float32(rowCols)*cellWidth)))+1, labelWidth)
			identity := gfx.TextIdentity{}
			if t.rowIDs[row] != 0 {
				identity = gfx.TextIdentity{Owner: session.ID, Item: t.rowIDs[row]}
			}
			runs := entry.runs
			if finding {
				t.find.paintRuns, firstFindMatch = terminalFindRuns(t.find.paintRuns, entry, &t.find, t.screen.ViewLine(row), cols, firstFindMatch, findInk)
				if len(t.find.paintRuns) != 0 {
					runs = t.find.paintRuns
				}
			}
			canvas.LabelRunsWithIdentity(identity, entry.text, gfx.Rect{X: geometry.left, Y: y, Width: rowWidth, Height: cellHeight}, style, cellWidth, runs)
		}
		if t.sync == nil {
			state.paintTerminalLink(canvas, t, geometry, row)
		}
	}
	if gutterShown {
		canvas.Fill(gfx.Rect{X: geometry.left - gutterRuleGap/2, Y: geometry.top, Width: 1, Height: float32(rows) * cellHeight}, withAlpha(pal.termFG, 0.08))
	}
	if t.sync == nil {
		state.paintTerminalImages(canvas, t, geometry, false, t.rowIDs)
	}
	t.cache.end(rows)
	if predicting && cursor.Y < rows {
		// Typed text the server has not echoed yet, drawn at once over the cursor
		// cell and the cells after it in the terminal's own color, so the echo
		// that replaces it changes nothing a reader could see. The block cursor
		// stays visible past the text. It never enters the terminal's own buffer.
		x := geometry.left + float32(cursor.X)*cellWidth
		y := geometry.top + float32(cursor.Y)*cellHeight
		width := float32(predictionCells) * cellWidth
		canvas.Fill(gfx.Rect{X: x, Y: y, Width: width, Height: cellHeight}, pal.terminal)
		for index := range predictionRuns {
			predictionRuns[index].Color = pal.termFG
		}
		canvas.LabelRuns(predictionText, gfx.Rect{X: x, Y: y, Width: width, Height: cellHeight}, style, cellWidth, predictionRuns)
	}

	if cursor.Visible && cursor.Y < rows {
		cursorColor := pal.cursor
		if inactive {
			cursorColor.Alpha *= 0.45
		}
		x := geometry.left + float32(caretX)*cellWidth
		y := geometry.top + float32(cursor.Y)*cellHeight
		switch {
		case !blinkOn:
			// the dark half of a blink
		case composing:
			// The input method draws over these cells, which the rows left empty.
		case cursor.Style == term.CursorUnderline:
			underline := state.terminalCursorThickness.underline()
			canvas.Fill(gfx.Rect{X: x, Y: y + cellHeight - underline, Width: cellWidth, Height: underline}, cursorColor)
		case cursor.Style == term.CursorBar:
			canvas.Fill(gfx.Rect{X: x, Y: y, Width: state.terminalCursorThickness.bar(), Height: cellHeight}, cursorColor)
		case !focused:
			// A block cursor outlines its cell while the terminal does not have the keyboard.
			canvas.Fill(gfx.Rect{X: x, Y: y, Width: cellWidth, Height: 1}, cursorColor)
			canvas.Fill(gfx.Rect{X: x, Y: y + cellHeight - 1, Width: cellWidth, Height: 1}, cursorColor)
			canvas.Fill(gfx.Rect{X: x, Y: y, Width: 1, Height: cellHeight}, cursorColor)
			canvas.Fill(gfx.Rect{X: x + cellWidth - 1, Y: y, Width: 1, Height: cellHeight}, cursorColor)
		}
	}
	if covered != (cellSpan{}) {
		x := geometry.left + float32(covered.from)*cellWidth
		y := geometry.top + float32(cursor.Y)*cellHeight
		canvas.Fill(gfx.Rect{X: x, Y: y, Width: float32(covered.to-covered.from) * cellWidth, Height: cellHeight}, pal.terminal)
	}
	var hintRect gfx.Rect
	hintShown := false
	// The suggestion and its badge stay while their own menu has the keyboard,
	// as the reference keeps them behind the menu; the other overlays still
	// need the terminal itself focused.
	suggesting := focused || state.ghostMenu.open && state.ghostMenu.session == session.ID
	if !sensitive && suggesting && !composing && !predicting && t.sync == nil {
		if rest, ok := state.passwordHintGhost(session); ok {
			state.paintGhost(canvas, t, geometry, cursor, rest, block)
		} else if rest, ok := state.commandGhost(session); ok {
			if used, limit, painted := state.paintGhost(canvas, t, geometry, cursor, rest, block); painted {
				hintRect, hintShown = state.paintGhostHint(canvas, session, geometry, cursor, used, limit)
			}
		}
	}
	canvas.Unclip()

	if declareHit {
		canvas.HitWithOptions(id, geometry.area, t.accessibleName(state.messages.LabelTerminalFmt, session.Address), gfx.HitOptions{
			Role: gfx.RoleTerminal, Autofocus: session.ID == state.activeSessionID,
		})
	}
	if hintShown {
		// Declared after the terminal's own region: hit testing takes the last
		// region painted, as it does for the scrollbar.
		state.declareGhostHintHit(canvas, session, hintRect)
	}
}

func (state *State) paintTerminalImages(canvas *gfx.Canvas, terminal *terminal, geometry terminalGeometry, behind bool, visible []uint64) {
	placements := terminal.screen.Images()
	if len(placements) == 0 {
		return
	}
	for _, placement := range placements {
		if placement.Image == nil || behind != (placement.Z < 0) {
			continue
		}
		row, ok := visibleRow(visible, placement.LineID)
		if !ok {
			// An image anchored off-screen may still reach into the viewport;
			// resolving that rare case keeps its old cost.
			row, ok = terminal.screen.ImageRow(placement.LineID)
		}
		if !ok {
			continue
		}
		sourceWidth, sourceHeight := placement.SourceWidth, placement.SourceHeight
		if sourceWidth <= 0 || sourceHeight <= 0 {
			continue
		}
		width, height := float32(sourceWidth), float32(sourceHeight)
		if placement.WidthCells > 0 {
			width = float32(placement.WidthCells) * state.cellWidth
		} else if placement.WidthPixels > 0 {
			width = float32(placement.WidthPixels)
		}
		if placement.HeightCells > 0 {
			height = float32(placement.HeightCells) * state.cellHeight
		} else if placement.HeightPixels > 0 {
			height = float32(placement.HeightPixels)
		}
		if placement.WidthCells > 0 || placement.WidthPixels > 0 {
			if placement.HeightCells == 0 && placement.HeightPixels == 0 {
				height = width * float32(sourceHeight) / float32(sourceWidth)
			}
		} else if placement.HeightCells > 0 || placement.HeightPixels > 0 {
			width = height * float32(sourceWidth) / float32(sourceHeight)
		}
		bounds := gfx.Rect{
			X:     geometry.left + float32(placement.Column)*state.cellWidth,
			Y:     geometry.top + float32(row)*state.cellHeight,
			Width: width, Height: height,
		}
		canvas.DrawBitmapRegion(gfx.Bitmap{
			Pix: placement.Image.Pix, Width: placement.Image.Width, Height: placement.Image.Height,
			Stride: placement.Image.Stride, ID: placement.Image.ID,
		}, gfx.Rect{
			X: float32(placement.SourceX), Y: float32(placement.SourceY),
			Width: float32(sourceWidth), Height: float32(sourceHeight),
		}, bounds)
	}
}

// visibleRow finds a line identity among the visible rows. There are only a
// screenful of entries, so a linear scan beats allocating a map per paint.
func visibleRow(visible []uint64, id uint64) (int, bool) {
	for row, candidate := range visible {
		if candidate == id {
			return row, true
		}
	}
	return 0, false
}

// coveredOn is covered when it is on the row being prepared, else nothing.
func coveredOn(covered cellSpan, onRow bool) cellSpan {
	if onRow {
		return covered
	}
	return cellSpan{}
}
