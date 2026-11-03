package workbench

import (
	"math"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	"motty/internal/term"
)

// spinnerStep is how long a spinner spoke stays lit.
const spinnerStep = 125 * time.Millisecond

// barTool is one icon button on the right of an active pane's bar.
type barToolKind uint8

const (
	barToolAction barToolKind = iota
	barToolConnectionColor
	barToolLatency
)

type barTool struct {
	id       ui.ID
	icon     uint8
	kind     barToolKind
	hint     string
	disabled bool
	style    iconButtonStyle
	groupEnd bool // the group of pane controls ends here: a wider gap follows
	height   float32
	rect     ui.Rect
}

func (tool barTool) focusable() bool { return tool.kind == barToolAction }

func (tool barTool) railHeight(defaultHeight float32) float32 {
	if tool.height > 0 {
		return tool.height
	}
	return defaultHeight
}

// maxBarTools leaves room for every action plus the two configurable status
// items in the terminal toolbar catalog.
const maxBarTools = 16

const (
	terminalToolRailExtent    = float32(40)
	barToolSize               = float32(24)
	barToolGap                = float32(4)
	terminalBarToolSize       = float32(32)
	terminalBarToolGap        = float32(4)
	terminalRailAccentWidth   = float32(18)
	terminalRailAccentHeight  = float32(5)
	terminalRailCapsuleWidth  = float32(24)
	terminalRailCapsuleHeight = float32(10)
)

// paintTerminalPage draws a session. Active panes get a configurable tool rail
// with only connection latency and actions. Every split pane reserves the same
// bar height and identifies its own session there, so switching focus cannot
// move or resize a PTY. Find mode temporarily replaces the bar with its search
// controls.
func (state *State) paintTerminalPage(canvas *ui.Canvas, size ui.Size, session *Session) {
	state.wantLatencyProbe(session, canvas)
	chrome, ok := state.terminalChromeForSession(session.ID)
	if !ok {
		chrome = terminalChrome{top: infoBarHeight}
	}
	geometry := terminalLayoutWithChrome(size, state.cellWidth, state.cellHeight, chrome, state.padding())
	inactive := state.paneIsInactive(session.ID)
	inSplit := state.paneInSplit(session.ID)
	// A split pane keeps the same neutral chrome in every tab. Connection
	// identity stays on the workspace tab and the rail marker instead of
	// painting another large tinted surface around the terminal.
	barBackground, barBorder := state.pal.workspace, state.pal.line
	finding := !inactive && session.term.find.open
	showingInputNotice := !inactive && !finding && state.terminalInputNoticeVisible(session)
	status, _ := state.sessionStatus(session)
	if inactive || finding {
		state.clearTerminalRail(session)
	}
	title := session.Title
	if title == "" {
		title = session.Address
	}
	var spinner, light, inputNotice flex.Box
	var find findBarSlots

	f := state.terminalPageLayout.Begin(canvas, state.Theme(), ui.Rect{Width: size.Width, Height: size.Height})
	f.Column().Fill().PadSides(windowBarHeight, 0, 0, 0).Children(func() {
		switch {
		case inSplit || finding:
			f.Stack().Height(infoBarHeight).Shrink(0).Background(barBackground).Children(func() {
				switch {
				case finding:
					find = state.declareFindBar(f, session)
				default:
					spinner, light = state.declarePaneBar(f, size.Width, title, status, session, !inactive)
				}
				f.Row().FillWidth().Height(1).Self(flex.End).Background(barBorder)
			})
			f.Column().Grow(1).FillHeight().Children(func() {
				if showingInputNotice {
					inputNotice = f.Slot().Height(inputNoticeHeight).FillWidth().Shrink(0)
				}
				f.Row().Grow(1).Background(state.ground(state.termPal.terminal))
			})
		default:
			f.Column().Grow(1).FillHeight().Children(func() {
				if showingInputNotice {
					inputNotice = f.Slot().Height(inputNoticeHeight).FillWidth().Shrink(0)
				}
				f.Row().Grow(1).Background(state.ground(state.termPal.terminal))
			})
		}
	})
	f.End()

	switch {
	case finding:
		state.paintFindBarSlots(canvas, session, find)
	case inSplit:
		if session.busy {
			phase := components.SpinnerPhase(canvas.Now() / spinnerStep)
			components.PaintSpinner(canvas, state.Theme(), spinner.Rect(), phase)
			canvas.RequestFrameIn(spinnerStep)
		} else if !inactive {
			// Only the active pane declares the status-light slot. Calling Rect on
			// the empty box used by an inactive pane would dereference nil.
			spot := light.Rect()
			kind, dot := state.sessionLight(session)
			paintLight(canvas, spot.X+spot.Width/2, spot.Y+spot.Height/2, kind, dot, barBackground)
		}
	}

	state.paintGrid(canvas, session, geometry)
	state.paintCompletionList(canvas, session, geometry)
	if showingInputNotice {
		state.paintTerminalInputNotice(canvas, session, inputNotice.Rect())
	}
}

func (state *State) activeTerminalTools(session *Session, toolList *[maxBarTools]barTool) []barTool {
	if session == nil || session.term == nil || state.paneIsInactive(session.ID) || session.term.find.open {
		return nil
	}
	tools := toolList[:0]
	if accent := hostAccent(session.host.Color); !state.terminalHideConnectionColor && accent.Alpha > 0 {
		tools = append(tools, barTool{
			id: hitTerminalConnectionColor, icon: iconPipette, kind: barToolConnectionColor,
			hint: state.messages.Toolbars.ConnectionColor, height: terminalRailCapsuleHeight,
		})
	}
	tools = append(tools, barTool{
		id: hitTerminalLatency, icon: iconClock, kind: barToolLatency, hint: state.messages.Toolbars.Latency,
	})
	if session.host.isSSH() && !session.isLocal() {
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
	tools = append(tools, barTool{id: hitRemoteToolsOpen, icon: iconWrench, hint: state.messages.Tools.RemoteMenu, disabled: !state.remoteToolsOffered(session), style: iconButtonQuiet})
	inSplit := state.paneInSplit(session.ID)
	if inSplit {
		tools = append(tools, barTool{id: hitPaneClose, icon: iconClose, hint: state.messages.PaneClose, style: iconButtonQuiet, groupEnd: true})
	}
	tools = append(tools, barTool{id: hitPaneSplitVertical, icon: iconRows2, hint: state.messages.PaneSplitVertical, style: iconButtonQuiet, groupEnd: !inSplit})
	tools = append(tools, barTool{id: hitPaneSplitHorizontal, icon: iconColumns2, hint: state.messages.PaneSplitHorizontal, style: iconButtonQuiet})
	if inSplit {
		hint, style := state.messages.Tools.BroadcastStart, iconButtonQuiet
		if state.broadcasting(session.ID) {
			hint, style = state.messages.Tools.BroadcastStop, iconButtonActive
		}
		tools = append(tools, barTool{id: hitBroadcast, icon: iconRadio, hint: hint, style: style})
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
	return ordered
}

type terminalToolZoneSet struct {
	zones [4]struct {
		tools [maxBarTools]barTool
		count int
	}
}

func (set *terminalToolZoneSet) tools(zone terminalToolbarZone) []barTool {
	return set.zones[zone].tools[:set.zones[zone].count]
}

func (state *State) splitTerminalTools(tools []barTool, set *terminalToolZoneSet) {
	for _, tool := range tools {
		zone := int(state.terminalToolbarZoneFor(tool.id))
		count := set.zones[zone].count
		if count >= maxBarTools {
			continue
		}
		set.zones[zone].tools[count] = tool
		set.zones[zone].count++
	}
}

func (state *State) setTerminalRailTools(session *Session, zones *terminalToolZoneSet) {
	if session == nil || session.term == nil {
		return
	}
	for side := range session.term.rails {
		rail := &session.term.rails[side]
		rail.side = terminalToolbarSide(side)
		rail.count = 0
		for _, dock := range []terminalToolbarDock{terminalToolbarDockTop, terminalToolbarDockBottom} {
			tools := zones.tools(terminalToolbarZoneFromParts(terminalToolbarSide(side), dock))
			for index := 0; index < len(tools); index++ {
				at := rail.count
				rail.ids[at] = tools[index].id
				rail.enabled[at] = !tools[index].disabled
				rail.focusable[at] = tools[index].focusable()
				rail.zoneFor[at] = dock
				rail.count++
			}
		}
	}
}

func (state *State) clearTerminalRail(session *Session) {
	if session == nil || session.term == nil {
		return
	}
	for side := range session.term.rails {
		session.term.rails[side] = terminalRailState{}
	}
}

// paintWorkspaceSideRail paints the configured left and right rails at the
// workspace's outer edges. The active split supplies the tools and state for
// both rails.
func (state *State) paintWorkspaceSideRail(canvas *ui.Canvas, size ui.Size) {
	session, ok := state.terminalSideRailSession()
	if !ok {
		return
	}
	var toolList [maxBarTools]barTool
	tools := state.activeTerminalTools(session, &toolList)
	var zoneSet terminalToolZoneSet
	state.splitTerminalTools(tools, &zoneSet)
	state.setTerminalRailTools(session, &zoneSet)
	// The rail remains neutral so the connection color is a single compact
	// identity marker rather than a second full-height tinted surface.
	background, border := state.pal.workspace, state.pal.line
	primary := terminalToolbarRight
	if len(zoneSet.tools(terminalToolbarZoneLeftTop))+len(zoneSet.tools(terminalToolbarZoneLeftBottom)) > 0 {
		primary = terminalToolbarLeft
	}
	for _, side := range []terminalToolbarSide{terminalToolbarLeft, terminalToolbarRight} {
		topTools := zoneSet.tools(terminalToolbarZoneFromParts(side, terminalToolbarDockTop))
		bottomTools := zoneSet.tools(terminalToolbarZoneFromParts(side, terminalToolbarDockBottom))
		target := state.terminalRailDragTargets(session, side)
		if len(topTools) == 0 && len(bottomTools) == 0 && !target {
			continue
		}
		bounds, ok := state.terminalSideRailBoundsFor(size, side)
		if !ok {
			continue
		}
		state.paintTerminalSideRail(canvas, bounds, session, topTools, bottomTools, background, border, side, side == primary)
	}
}

// paintTerminalSideRail draws the top and bottom docking zones on one edge.
// Each zone owns its scroll position so a crowded quadrant never moves the
// controls in the other quadrant.
func (state *State) paintTerminalSideRail(canvas *ui.Canvas, bounds ui.Rect, session *Session, topTools, bottomTools []barTool, background, border ui.Color, side terminalToolbarSide, _ bool) {
	if bounds.Width <= 0 || bounds.Height <= 0 || session.term == nil {
		return
	}
	metrics := state.terminalToolbarMetrics()
	fullBounds := bounds
	rail := &session.term.rails[side]
	rail.side = side
	rail.bounds = bounds
	canvas.Fill(fullBounds, background)
	viewport := ui.Rect{X: bounds.X + metrics.inset, Y: bounds.Y + metrics.inset, Width: max(bounds.Width-metrics.inset*2, 0), Height: max(bounds.Height-metrics.inset*2, 0)}
	zoneViewports := terminalRailZoneViewports(
		viewport, topTools, bottomTools,
		state.terminalRailDragTarget(session, side, terminalToolbarDockTop),
		state.terminalRailDragTarget(session, side, terminalToolbarDockBottom),
		metrics,
	)
	zoneTools := [2][]barTool{topTools, bottomTools}
	for _, dock := range []terminalToolbarDock{terminalToolbarDockTop, terminalToolbarDockBottom} {
		target := state.terminalRailDragTarget(session, side, dock)
		if !target {
			continue
		}
		zoneViewport := zoneViewports[dock]
		canvas.Fill(zoneViewport, withAlpha(state.pal.accent, 0.045))
		border = withAlpha(state.pal.accent, 0.55)
		if len(zoneTools[dock]) == 0 {
			lane := ui.Rect{
				X: zoneViewport.X, Y: zoneViewport.Y,
				Width:  min(metrics.toolSize, zoneViewport.Width),
				Height: min(max(metrics.toolSize*2, 56), zoneViewport.Height),
			}
			canvas.Rounded(lane, radius.control, withAlpha(state.pal.accent, 0.10))
		}
	}
	state.paintTerminalRailTools(canvas, session, rail, terminalToolbarDockTop, zoneViewports[terminalToolbarDockTop], 0, topTools, background, metrics)
	state.paintTerminalRailTools(canvas, session, rail, terminalToolbarDockBottom, zoneViewports[terminalToolbarDockBottom], len(topTools), bottomTools, background, metrics)
	x := bounds.X
	if side == terminalToolbarLeft {
		x += bounds.Width - 1
	}
	canvas.Fill(ui.Rect{X: x, Y: fullBounds.Y, Width: 1, Height: fullBounds.Height}, border)
}

// terminalRailZoneViewports gives an otherwise empty quadrant no height. A
// lone top or bottom group can use the whole rail, preserving the pre-quadrant
// behavior for default layouts in short windows. When both quadrants contain
// controls, each is sized to its content if possible and shares the viewport
// when both overflow. During a drag, an empty destination keeps enough height
// to expose its drop lane.
func terminalRailZoneViewports(viewport ui.Rect, topTools, bottomTools []barTool, topTarget, bottomTarget bool, metrics terminalToolbarMetrics) [2]ui.Rect {
	var result [2]ui.Rect
	topPresent := len(topTools) > 0 || topTarget
	bottomPresent := len(bottomTools) > 0 || bottomTarget
	if !topPresent && !bottomPresent {
		return result
	}
	if topPresent && !bottomPresent {
		result[terminalToolbarDockTop] = viewport
		return result
	}
	if bottomPresent && !topPresent {
		result[terminalToolbarDockBottom] = viewport
		return result
	}

	const zoneGap = float32(8)
	available := max(viewport.Height-zoneGap, 0)
	topRequested := terminalRailContentExtent(topTools, metrics)
	bottomRequested := terminalRailContentExtent(bottomTools, metrics)
	previewExtent := min(max(metrics.toolSize*2, 56), available)
	if topTarget && len(topTools) == 0 {
		topRequested = max(topRequested, previewExtent)
	}
	if bottomTarget && len(bottomTools) == 0 {
		bottomRequested = max(bottomRequested, previewExtent)
	}
	if topRequested+bottomRequested <= available {
		result[terminalToolbarDockTop] = ui.Rect{
			X: viewport.X, Y: viewport.Y, Width: viewport.Width, Height: topRequested,
		}
		result[terminalToolbarDockBottom] = ui.Rect{
			X: viewport.X, Y: viewport.Y + viewport.Height - bottomRequested,
			Width: viewport.Width, Height: bottomRequested,
		}
		return result
	}

	minZoneExtent := min(metrics.toolSize+metrics.inset*2, available/2)
	topMinimum := min(topRequested, minZoneExtent)
	bottomMinimum := min(bottomRequested, minZoneExtent)
	if topTarget && len(topTools) == 0 {
		topMinimum = min(topRequested, available)
	}
	if bottomTarget && len(bottomTools) == 0 {
		bottomMinimum = min(bottomRequested, available)
	}
	if topMinimum+bottomMinimum > available {
		total := topMinimum + bottomMinimum
		if total <= 0 {
			topMinimum, bottomMinimum = available/2, available/2
		} else {
			topMinimum = available * topMinimum / total
			bottomMinimum = available - topMinimum
		}
	}

	topExtra, bottomExtra := topRequested-topMinimum, bottomRequested-bottomMinimum
	extraAvailable := max(available-topMinimum-bottomMinimum, 0)
	extraTotal := topExtra + bottomExtra
	topHeight, bottomHeight := topRequested, bottomRequested
	if extraTotal > 0 {
		topHeight = topMinimum + extraAvailable*topExtra/extraTotal
		bottomHeight = bottomMinimum + extraAvailable*bottomExtra/extraTotal
	} else {
		topHeight, bottomHeight = topMinimum, bottomMinimum
	}
	bottomHeight = min(bottomHeight, available)
	topHeight = min(topHeight, max(available-bottomHeight, 0))
	result[terminalToolbarDockTop] = ui.Rect{
		X: viewport.X, Y: viewport.Y, Width: viewport.Width, Height: topHeight,
	}
	result[terminalToolbarDockBottom] = ui.Rect{
		X: viewport.X, Y: viewport.Y + viewport.Height - bottomHeight,
		Width: viewport.Width, Height: bottomHeight,
	}
	return result
}

func paintTerminalRailAccentMarker(canvas *ui.Canvas, bounds ui.Rect, accent ui.Color) {
	centerX := bounds.X + bounds.Width/2
	centerY := bounds.Y + bounds.Height/2
	canvas.Rounded(ui.Rect{
		X:      centerX - terminalRailCapsuleWidth/2,
		Y:      centerY - terminalRailCapsuleHeight/2,
		Width:  terminalRailCapsuleWidth,
		Height: terminalRailCapsuleHeight,
	}, terminalRailCapsuleHeight/2, withAlpha(accent, 0.16))
	canvas.Rounded(ui.Rect{
		X:      centerX - terminalRailAccentWidth/2,
		Y:      centerY - terminalRailAccentHeight/2,
		Width:  terminalRailAccentWidth,
		Height: terminalRailAccentHeight,
	}, terminalRailAccentHeight/2, accent)
}

func (state *State) paintTerminalRailTools(canvas *ui.Canvas, session *Session, rail *terminalRailState, dock terminalToolbarDock, viewport ui.Rect, rectOffset int, tools []barTool, background ui.Color, metrics terminalToolbarMetrics) {
	zone := &rail.zones[dock]
	zone.viewport = viewport
	zone.contentExtent = terminalRailContentExtent(tools, metrics)
	viewportExtent := viewport.Height
	maxScroll := max(zone.contentExtent-viewportExtent, 0)
	zone.scroll = min(max(zone.scroll, 0), maxScroll)
	zone.itemOffset = 0
	if dock == terminalToolbarDockBottom {
		zone.itemOffset = max(viewportExtent-zone.contentExtent, 0)
	}

	// A keyboard move to a tool outside the viewport brings it into view on the
	// next paint. Pointer hover and wheel scrolling never move focus.
	for index := range tools {
		if tools[index].id == canvas.FocusVisible() {
			start := terminalRailItemStart(tools, index, metrics)
			end := start + tools[index].railHeight(metrics.toolSize)
			if start-zone.scroll < 0 {
				zone.scroll = min(start, maxScroll)
			} else if end-zone.scroll > viewportExtent {
				zone.scroll = min(end-viewportExtent, maxScroll)
			}
			break
		}
	}

	canvas.Clip(viewport)
	for index := range tools {
		itemStart := terminalRailItemStart(tools, index, metrics)
		tool := &tools[index]
		tool.rect = ui.Rect{
			X: viewport.X, Y: viewport.Y + zone.itemOffset + itemStart - zone.scroll,
			Width: min(metrics.toolSize, viewport.Width), Height: tool.railHeight(metrics.toolSize),
		}
		rail.rects[rectOffset+index] = tool.rect
		if tool.rect.X+tool.rect.Width <= viewport.X || tool.rect.X >= viewport.X+viewport.Width ||
			tool.rect.Y+tool.rect.Height <= viewport.Y || tool.rect.Y >= viewport.Y+viewport.Height {
			continue
		}
		if state.terminalRailDragSource(session, tool.id) {
			state.paintTerminalRailDragSource(canvas, *tool, metrics)
			continue
		}
		switch tool.kind {
		case barToolConnectionColor:
			if accent := hostAccent(session.host.Color); !state.terminalHideConnectionColor && accent.Alpha > 0 {
				paintTerminalRailAccentMarker(canvas, tool.rect, accent)
				canvas.ContainerWithOptions(tool.rect, ui.RoleStatus, tool.hint, ui.HitOptions{Value: tool.hint})
			}
		case barToolLatency:
			if text, color, ok := state.latencyInfo(session); ok {
				state.paintRailLatency(canvas, text, color, tool.rect, true)
			} else {
				// Keeping a visible placeholder makes the configurable item
				// draggable before the first RTT sample arrives and prevents a
				// latency-only rail from appearing as an unexplained blank side.
				state.paintRailLatency(canvas, "—", state.pal.muted, tool.rect, true)
			}
		default:
			state.paintIconButtonSized(canvas, tool.id, tool.icon, tool.rect, tool.hint, tool.disabled, tool.style, background, metrics.iconSize)
		}
		// Status chips have no button target of their own, and disabled buttons
		// deliberately skip pointer presses. Both still need a stable drag hit:
		// this transparent layer keeps them reorderable without enabling their
		// underlying action or adding another keyboard stop.
		if tool.disabled || tool.kind != barToolAction {
			if id := terminalToolbarDragHandle(tool.id); !id.IsZero() {
				canvas.HitWithOptions(id, tool.rect, tool.hint, ui.HitOptions{
					Role: ui.RoleButton, Cursor: ui.PointerCursorHand, SkipFocus: true,
				})
			}
		}
	}
	canvas.Unclip()
}

func terminalRailContentExtent(tools []barTool, metrics terminalToolbarMetrics) float32 {
	if len(tools) == 0 {
		return metrics.inset * 2
	}
	last := len(tools) - 1
	return terminalRailItemStart(tools, last, metrics) + tools[last].railHeight(metrics.toolSize) + metrics.inset
}

func terminalRailItemStart(tools []barTool, index int, metrics terminalToolbarMetrics) float32 {
	start := metrics.inset
	for previous := 0; previous < index; previous++ {
		start += tools[previous].railHeight(metrics.toolSize) + metrics.toolGap
		if tools[previous].groupEnd {
			start += metrics.toolGap
		}
	}
	return start
}

func (state *State) paintRailLatency(canvas *ui.Canvas, text string, color ui.Color, bounds ui.Rect, compact bool) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	accessibilityText := text
	if compact {
		text = strings.TrimPrefix(text, "RTT ")
	}
	canvas.Rounded(bounds, min(radius.control, bounds.Height/2), withAlpha(color, 0.14))
	state.paintRailLabelCenter(canvas, text, bounds, color, compact)
	canvas.ContainerWithOptions(bounds, ui.RoleStatus, accessibilityText, ui.HitOptions{Value: accessibilityText})
}

func (state *State) paintRailLabelCenter(canvas *ui.Canvas, text string, bounds ui.Rect, color ui.Color, compact bool) {
	size := metrics.FontSizeS
	if compact {
		size = 10
	}
	fitted := elide(canvas, text, size, ui.WeightMedium, bounds.Width-4)
	canvas.LabelCenter(fitted, bounds, ui.TextStyle{Size: size, Weight: ui.WeightMedium, Color: color})
}

func (state *State) scrollTerminalToolbar(session *Session, event ui.ScrollEvent) (ui.EventResult, bool) {
	if session == nil || session.term == nil {
		return ui.EventResult{}, false
	}
	metrics := state.terminalToolbarMetrics()
	delta := event.StepY(metrics.toolSize + metrics.toolGap)
	for side := range session.term.rails {
		rail := &session.term.rails[side]
		if rail.bounds.Width <= 0 || rail.bounds.Height <= 0 || !pointInRect(event.X, event.Y, rail.bounds) {
			continue
		}
		dock := terminalToolbarDockTop
		if !pointInRect(event.X, event.Y, rail.zones[dock].viewport) {
			if pointInRect(event.X, event.Y, rail.zones[terminalToolbarDockBottom].viewport) {
				dock = terminalToolbarDockBottom
			} else if event.Y >= rail.bounds.Y+rail.bounds.Height/2 {
				// The insets and the splitter belong to the nearest half of
				// the rail. Wheel input there must not leak to terminal history.
				dock = terminalToolbarDockBottom
			}
		}
		zone := &rail.zones[dock]
		maxScroll := max(zone.contentExtent-zone.viewport.Height, 0)
		next := min(max(zone.scroll+delta, 0), maxScroll)
		if next == zone.scroll {
			return ui.EventResult{Handled: true}, true
		}
		zone.scroll = next
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	return ui.EventResult{}, false
}

// paintAIMarking draws the strip that says this line's command was started by
// the assistant. It sits in the terminal's left padding, outside the grid, so it
// is not part of the copied command or the emulator's screen cells.
func (state *State) paintAIMarking(canvas *ui.Canvas, geometry terminalGeometry, y, cellHeight float32) {
	x := geometry.area.X + 4
	canvas.Fill(ui.Rect{X: x, Y: y, Width: 3, Height: cellHeight}, withAlpha(state.pal.accent, 0.85))
}

// declarePaneBar identifies a pane that shares its workspace. The active pane
// starts with a status light; inactive panes use the terminal icon so the light
// remains a reliable focus marker. Both show the name and connection state, and
// the active bar is the only one that can carry a busy spinner in that slot.
// Resource readings stay in the dedicated monitor surface.
func (state *State) declarePaneBar(f *flex.Frame, width float32, title, status string, session *Session, active bool) (spinner, light flex.Box) {
	pal, canvas := state.pal, f.Canvas()
	readout := status
	gap := float32(29) // from the name to the reading
	if session.busy {
		gap = 6
	}
	f.Row().Fill().PadX(termPadX).Items(flex.Center).Children(func() {
		if active {
			light = f.Slot().Size(16, 16).Shrink(0).MarginSides(0, 7, 0, 0)
		} else {
			f.Icon(lucideIcons[iconTerminal]).Size(14, 14).Color(pal.faint).MarginSides(0, 7, 0, 0)
		}
		titleColor := pal.muted
		if active {
			titleColor = pal.text
		}
		f.Text(title).FontSize(barTitleSize).Weight(ui.WeightSemibold).Color(titleColor).LineHeight(22).
			Ellipsis().Grow(1).Shrink(1).MinWidth(0).MarginSides(0, gap, 0, 0)
		state.declareLatencyTag(f, canvas, session)
		if session.busy {
			spinner = f.Slot().Size(14, 14).MarginSides(0, 9, 0, 0)
		}
		readoutColor := pal.faint
		if active {
			readoutColor = pal.muted
		}
		f.Text(readout).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(readoutColor).LineHeight(20).
			MaxWidth(width * 0.42).Ellipsis().Shrink(1)
		if state.broadcasting(session.ID) {
			// Typing goes to this pane too: the one mark an inactive bar carries.
			f.Icon(lucideIcons[iconRadio]).Size(14, 14).Color(pal.accent).MarginSides(0, 0, 0, 8)
		}
	})
	return spinner, light
}

// paintGrid draws a session's character grid: for each row its background
// spans and one fixed-pitch label, then the cursor. Rows are prepared once per
// change of their content; painting a screen that did not change builds
// nothing.
func (state *State) paintGrid(canvas *ui.Canvas, session *Session, geometry terminalGeometry) {
	state.paintGridWithID(canvas, session, geometry, hitTerminalList.At(int(session.ID)), false)
	state.paintTerminalScrollbar(canvas, session, geometry)
}

func (state *State) paintGridWithID(canvas *ui.Canvas, session *Session, geometry terminalGeometry, id ui.ID, sensitive bool) {
	t, pal := session.term, &state.termPal
	inactive := state.paneIsInactive(session.ID)
	focused := canvas.Focused() == id
	t.expireSync()
	cols, rows := t.presentedSize()
	cellWidth, cellHeight := state.cellWidth, state.cellHeight
	cursor := t.presentedCursor()
	composing := t.preedit.Composing
	predictionText, predictionRuns, predictionCells, predicting := t.prediction.overlay()
	predicting = predicting && !composing && cursor.Visible && t.sync == nil
	if t.sync != nil && t.clock != nil {
		canvas.RequestFrameIn(t.syncRemaining())
	}
	// The cursor blinks while the terminal has the keyboard and something is
	// happening; it is solid from the moment it moves or the pane is entered.
	blinkOn := true
	if cursor.Visible && focused && !composing && !state.terminalCursorSteady && !cursor.Steady {
		blinkOn = t.blink.on(canvas, cursor.X, cursor.Y)
	} else {
		t.blink.rest(cursor.X, cursor.Y)
	}
	if t.sync != nil {
		blinkOn = true
	}
	block := cursor.Visible && cursor.Style == term.CursorBlock && focused && !composing && !predicting && blinkOn
	// The input method draws its composition over the cells from the cursor on,
	// where it replaces whatever the program put there. A program that draws its
	// own cursor (a prompt with placeholder text) hides the real one, so the
	// cells are covered whether the cursor shows or not.
	var covered cellSpan
	if composing && cursor.Y < rows {
		covered = cellSpan{from: cursor.X, to: cursor.X + displayCells(t.preedit.Value)}
	}
	style := ui.TextStyle{Size: state.terminalFontSize, Color: pal.termFG, MonoFace: state.terminalFontFace}
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
	for row := 0; row < rows; row++ {
		column := -1
		if block && row == cursor.Y {
			column = cursor.X
		}
		line := t.presentedRow(row)
		t.rowIDs[row] = line.ID
		entry := t.cache.row(line, cols, column, coveredOn(covered, row == cursor.Y), pal, inactive)
		y := geometry.top + float32(row)*cellHeight
		for _, span := range entry.spans {
			from := snap(geometry.left + float32(span.from)*cellWidth)
			to := snap(geometry.left + float32(span.to)*cellWidth)
			canvas.Fill(ui.Rect{X: from, Y: y, Width: to - from, Height: cellHeight}, span.color)
		}
		if t.sync == nil && t.find.open && len(t.find.matches) > 0 {
			firstFindMatch = state.paintTerminalFindMatches(canvas, session, geometry, row, t.screen.ViewLine(row), firstFindMatch)
		}
	}
	if t.sync == nil {
		state.paintTerminalImages(canvas, t, geometry, true, t.rowIDs)
	}
	if gutterShown {
		// The strip sits on its own faintly lifted panel, edge to rule.
		edge := geometry.left - gutterRuleGap/2
		canvas.Fill(ui.Rect{X: geometry.area.X, Y: geometry.area.Y, Width: edge - geometry.area.X, Height: geometry.area.Height}, withAlpha(pal.termFG, 0.035))
	}
	for row := 0; row < rows; row++ {
		column := -1
		if block && row == cursor.Y {
			column = cursor.X
		}
		entry := t.cache.row(t.presentedRow(row), cols, column, coveredOn(covered, row == cursor.Y), pal, inactive)
		y := geometry.top + float32(row)*cellHeight
		if from, to, selected := t.selection.span(t.screen.LineNumber(row), t.presentedRow(row), cols); selected {
			left := snap(geometry.left + float32(from)*cellWidth)
			right := snap(geometry.left + float32(to)*cellWidth)
			canvas.Fill(ui.Rect{X: left, Y: y, Width: right - left, Height: cellHeight}, withAlpha(pal.termFG, 0.25))
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
			canvas.LabelRuns(entry.text, ui.Rect{X: geometry.left, Y: y, Width: labelWidth, Height: cellHeight}, style, cellWidth, entry.runs)
		}
		if t.sync == nil {
			state.paintTerminalLink(canvas, t, geometry, row)
		}
	}
	if gutterShown {
		canvas.Fill(ui.Rect{X: geometry.left - gutterRuleGap/2, Y: geometry.top, Width: 1, Height: float32(rows) * cellHeight}, withAlpha(pal.termFG, 0.08))
	}
	if t.sync == nil {
		state.paintTerminalImages(canvas, t, geometry, false, t.rowIDs)
	}
	t.cache.end(rows)
	if predicting && cursor.Y < rows {
		// Typed text the server has not echoed yet, drawn at once over the cursor
		// cell and the cells after it, muted so it reads as provisional. It is not
		// underlined: on a slow link a held key keeps that mark on screen. It never
		// enters the terminal's own buffer.
		x := geometry.left + float32(cursor.X)*cellWidth
		y := geometry.top + float32(cursor.Y)*cellHeight
		width := float32(predictionCells) * cellWidth
		canvas.Fill(ui.Rect{X: x, Y: y, Width: width, Height: cellHeight}, pal.terminal)
		for index := range predictionRuns {
			predictionRuns[index].Color = pal.muted
		}
		canvas.LabelRuns(predictionText, ui.Rect{X: x, Y: y, Width: width, Height: cellHeight}, style, cellWidth, predictionRuns)
		cursor.X += predictionCells
	}

	if cursor.Visible && cursor.Y < rows {
		cursorColor := pal.cursor
		if inactive {
			cursorColor.Alpha *= 0.45
		}
		x := geometry.left + float32(cursor.X)*cellWidth
		y := geometry.top + float32(cursor.Y)*cellHeight
		switch {
		case !blinkOn:
			// the dark half of a blink
		case composing:
			// The input method draws over these cells, which the rows left empty.
		case cursor.Style == term.CursorUnderline:
			canvas.Fill(ui.Rect{X: x, Y: y + cellHeight - 2, Width: cellWidth, Height: 2}, cursorColor)
		case cursor.Style == term.CursorBar:
			canvas.Fill(ui.Rect{X: x, Y: y, Width: 3, Height: cellHeight}, cursorColor)
		case !focused:
			// A block cursor outlines its cell while the terminal does not have the keyboard.
			canvas.Fill(ui.Rect{X: x, Y: y, Width: cellWidth, Height: 1}, cursorColor)
			canvas.Fill(ui.Rect{X: x, Y: y + cellHeight - 1, Width: cellWidth, Height: 1}, cursorColor)
			canvas.Fill(ui.Rect{X: x, Y: y, Width: 1, Height: cellHeight}, cursorColor)
			canvas.Fill(ui.Rect{X: x + cellWidth - 1, Y: y, Width: 1, Height: cellHeight}, cursorColor)
		}
	}
	if covered != (cellSpan{}) {
		x := geometry.left + float32(covered.from)*cellWidth
		y := geometry.top + float32(cursor.Y)*cellHeight
		canvas.Fill(ui.Rect{X: x, Y: y, Width: float32(covered.to-covered.from) * cellWidth, Height: cellHeight}, pal.terminal)
	}
	if !sensitive && focused && !composing && !predicting && t.sync == nil {
		if rest, ok := state.commandGhost(session); ok {
			state.paintGhost(canvas, t, geometry, cursor, rest, block)
		}
	}
	canvas.Unclip()

	canvas.HitWithOptions(id, geometry.area, t.accessibleName(state.messages.LabelTerminalFmt, session.Address), ui.HitOptions{
		Role: ui.RoleTerminal, Autofocus: session.ID == state.activeSessionID,
	})
}

func (state *State) paintTerminalImages(canvas *ui.Canvas, terminal *terminal, geometry terminalGeometry, behind bool, visible []uint64) {
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
		bounds := ui.Rect{
			X:     geometry.left + float32(placement.Column)*state.cellWidth,
			Y:     geometry.top + float32(row)*state.cellHeight,
			Width: width, Height: height,
		}
		canvas.DrawBitmapRegion(ui.Bitmap{
			Pix: placement.Image.Pix, Width: placement.Image.Width, Height: placement.Image.Height,
			Stride: placement.Image.Stride, ID: placement.Image.ID,
		}, ui.Rect{
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
