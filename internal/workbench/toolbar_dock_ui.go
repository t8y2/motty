package workbench

import (
	"slices"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

type toolbarDockHit struct {
	tool   barTool
	zone   toolbarDockZone
	bounds gfx.Rect
}

type toolbarDockSlot struct {
	tool barTool
	box  ui.Box
}
type toolbarDockDrop struct {
	valid  bool
	zone   toolbarDockZone
	before gfx.ID
	marker gfx.Rect
}
type toolbarDockDrag struct {
	pressed, active, cancelled bool
	tool                       barTool
	start, at                  gfx.Point
	drop                       toolbarDockDrop
}
type toolbarDockUI struct {
	hits                    [maxBarTools]toolbarDockHit
	count                   int
	topSlots                []toolbarDockSlot
	topArea                 ui.Box
	zones                   [7]gfx.Rect
	drag                    toolbarDockDrag
	menu                    bool
	menuTool                gfx.ID
	menuAt                  gfx.Point
	overflow                bool
	sideScroll              [4]float32
	sideMax                 [4]float32
	sideViewports           [4]gfx.Rect
	bottomScroll, bottomMax [2]float32
	bottomViewports         [2]gfx.Rect
}

func toolbarDockZoneIndex(zone toolbarDockZone) int {
	switch zone {
	case toolbarDockTop:
		return 0
	case toolbarDockSideTop:
		return 1
	case toolbarDockSideBottom:
		return 2
	case toolbarDockBottom:
		return 3
	case toolbarDockLeftTop:
		return 4
	case toolbarDockLeftBottom:
		return 5
	default:
		return 6
	}
}

func (state *State) toolbarDockBounds(id gfx.ID) gfx.Rect {
	for _, hit := range state.toolbarDock.hits[:state.toolbarDock.count] {
		if hit.tool.id == id {
			return hit.bounds
		}
	}
	return gfx.Rect{}
}

func (state *State) registerToolbarDockTool(canvas *gfx.Canvas, tool barTool, zone toolbarDockZone, bounds gfx.Rect) {
	if _, ok := toolbarDockIdentity(tool.id); !ok || bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	dock := &state.toolbarDock
	if dock.count < len(dock.hits) {
		dock.hits[dock.count] = toolbarDockHit{tool, zone, bounds}
		dock.count++
	}
	if tool.id == hitAIToggle && len(state.ai.approvals) > 0 && !state.aiPanelVisible() {
		canvas.Rounded(gfx.Rect{X: bounds.X + bounds.Width - 8, Y: bounds.Y + 2, Width: 6, Height: 6}, 3, state.pal.warning)
	}
	if tool.id == hitTransferCenter {
		state.paintTransferBadge(canvas)
	}
	if tool.id == hitWindowLocalTerminal {
		state.openLocalShellPickerOn(canvas, tool.id, false)
	}
}

func (state *State) declareToolbarDockTop(f *ui.Frame, probe bool) {
	dock := &state.toolbarDock
	var buffer [maxBarTools]barTool
	tools := state.toolbarDockTools(toolbarDockTop, &buffer)
	limit := state.toolbarTopLimit()
	overflow := len(tools) > limit
	if overflow {
		tools = tools[:limit-1]
	}
	if !probe {
		dock.topSlots = dock.topSlots[:0]
	}
	area := f.Row().Shrink(0).MinWidth(barButtonSize).Height(barButtonSize).MarginSides(0, 0, 0, 10).Gap(2)
	if !probe {
		dock.topArea = area
	}
	area.Children(func() {
		for _, tool := range tools {
			box := f.Slot().Size(barButtonSize, barButtonSize).Shrink(0)
			if !probe {
				dock.topSlots = append(dock.topSlots, toolbarDockSlot{tool, box})
				if tool.id == hitAIToggle {
					state.chrome.bar.assistant = box
				}
				if tool.id == hitTransferCenter {
					state.chrome.bar.transfers = box
				}
			}
		}
		if overflow {
			box := state.barButton(f, probe, hitToolbarDockMore, iconEllipsis, state.messages.Toolbars.More)
			if !probe {
				dock.topSlots = append(dock.topSlots, toolbarDockSlot{barTool{id: hitToolbarDockMore}, box})
			}
		}
	})
}

func (state *State) paintToolbarDockTop(canvas *gfx.Canvas) {
	dock := &state.toolbarDock
	dock.zones[0] = dock.topArea.Rect()
	dock.zones[0].Y, dock.zones[0].Height = 0, windowBarHeight
	for _, slot := range dock.topSlots {
		if slot.tool.id == hitToolbarDockMore {
			if event, ok := canvas.PointerOn(hitToolbarDockMore); ok && event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary {
				dock.menu, dock.overflow, dock.menuAt = true, true, gfx.Point{X: event.X, Y: windowBarHeight}
				canvas.Consume()
			}
			continue
		}
		tool, bounds := slot.tool, slot.box.Rect()
		entry, _ := toolbarDockIdentity(tool.id)
		if tool.id == hitToolbarLatency {
			session, ok := state.terminalSideRailSession()
			if !ok || !state.paintToolbarLatency(canvas, bounds, session) {
				continue
			}
		} else {
			// Titlebar window actions must leave typing focused on the terminal.
			state.paintIconButtonWithOptions(canvas, tool.id, tool.icon, bounds, tool.hint, tool.disabled, tool.style, state.pal.shell, 16, gfx.HitOptions{SkipFocus: entry.Group == toolbarWindow})
		}
		state.registerToolbarDockTool(canvas, tool, toolbarDockTop, bounds)
	}
}

// Reserve each physical side independently. Background terminals keep their
// eventual tool widths; ordinary pages only reserve globally available actions.
func (state *State) toolbarDockSideReserveFor(left, includeTerminal bool) float32 {
	session, hasTerminal := state.terminalToolbarSession()
	if !hasTerminal && state.noteSidebarVisible() && (state.terminalToolbarPosition == terminalToolbarLeft) == left {
		return state.terminalToolbarMetrics().railExtent
	}
	if includeTerminal && hasTerminal && state.terminalToolbarShowsOnSide(session, left) {
		return state.terminalToolbarMetrics().railExtent
	}
	if state.toolbars.dock == nil {
		return 0
	}
	for _, entry := range state.toolbars.dock {
		if !toolbarDockVertical(entry.Zone) || toolbarDockLeft(entry.Zone) != left {
			continue
		}
		// Terminal entries were resolved against the active session above.
		if entry.Group == toolbarTerminal {
			continue
		}
		spec, ok := toolbarSpecFor(entry.Group, entry.Key)
		if ok && state.toolbarVisible(entry.Group, spec.id) {
			return state.terminalToolbarMetrics().railExtent
		}
	}
	return 0
}

// terminalToolbarShowsOnSide reports whether this physical side has actual
// content: a tool the bar paints there, or a latency badge backed by an RTT
// sample. It runs on the steady-state paint path, so it scans preferences
// directly; a tool this does not know yet counts as offered rather than
// vanishing from the rail (see terminalToolbarOffers).
func (state *State) terminalToolbarShowsOnSide(session *Session, left bool) bool {
	if session == nil {
		return false
	}
	managed := state.workspaceManaged(state.deck.ActiveWorkspace())
	capacity := workspacePaneToolsCapacity(state.viewport.Width)
	compactCount := 0
	compactMenu := false
	entries := state.toolbars.entries(toolbarTerminal)
	for _, preference := range entries {
		spec, ok := toolbarSpecFor(toolbarTerminal, preference.Key)
		if !ok {
			continue
		}
		visible := preference.Enabled
		if spec.id == hitToolbarLatency {
			visible = state.terminalShowLatency
		}
		if !visible || !state.terminalToolbarOffers(session, spec.id) {
			continue
		}
		// The latency readout is a status, not an action: compaction never moves
		// it into the pane-tools menu.
		if managed && spec.id != hitToolbarLatency {
			if terminalToolbarCompactVisible(spec.id) && compactCount < capacity {
				compactCount++
			} else {
				compactMenu = true
				continue
			}
		}
		zone := state.toolbarDockZoneFor(toolbarTerminal, preference.Key)
		if toolbarDockVertical(zone) && toolbarDockLeft(zone) == left {
			return true
		}
	}
	if managed && compactMenu {
		if zone := state.defaultToolbarDockZone(toolbarTerminal); toolbarDockLeft(zone) == left {
			return true
		}
	}
	if session.showLog && !session.term.live {
		if zone := state.defaultToolbarDockZone(toolbarTerminal); toolbarDockLeft(zone) == left {
			return true
		}
	}
	// A running proxy keeps its button on the default rail even while the
	// tool's preference is off: toolbarDockTools adds it after the catalog.
	if !state.toolbarVisible(toolbarTerminal, hitLocalProxyOpen) {
		if zone := state.defaultToolbarDockZone(toolbarTerminal); toolbarDockLeft(zone) == left {
			switch state.localProxySnapshot(session).Phase {
			case localProxyConnecting, localProxyConnected, localProxyFailed:
				return true
			}
		}
	}
	return false
}

// terminalToolbarOffers mirrors the context filters in activeTerminalTools.
// Disabled actions still count because their icon remains visible. A tool the
// switch does not name is offered: a catalog entry added later must not
// disappear from the rail just because this list was not updated.
func (state *State) terminalToolbarOffers(session *Session, id gfx.ID) bool {
	switch id {
	case hitSFTPDetach:
		return session.host.isSSH() && !session.isLocal()
	case hitSFTPOpen, hitConnExport, hitTerminalRecordingExport, hitTerminalRecord,
		hitTerminalSaveOutput, hitProcessToolsOpen, hitPaneSplitVertical, hitPaneSplitHorizontal,
		hitTerminalSnippets:
		return true
	case hitLocalProxyOpen:
		return state.localProxyActionVisible(session)
	case hitRemoteStats:
		_, _, offered := state.remoteStatsToggle(session)
		return offered
	case hitToolbarLatency:
		return state.latencyAvailable(session)
	case hitPaneClose:
		return state.paneInSplit(session.ID)
	case hitBroadcast:
		return state.paneInSplit(session.ID) && !state.workspaceManaged(state.deck.ActiveWorkspace())
	default:
		return true
	}
}

// These are the actions compactWorkspacePaneTools keeps beside the pane title
// when space permits. Everything else is represented by the synthesized menu.
func terminalToolbarCompactVisible(id gfx.ID) bool {
	return id == hitSFTPOpen || id == hitRemoteStats || id == hitPaneClose
}

func (state *State) toolbarDockSideBoundsFor(size gfx.Size, left bool) gfx.Rect {
	bounds := state.fullWorkspacePixelBounds(size)
	if !state.terminalPage() && state.activeSFTPWindow() == nil {
		bounds.X = 0
	}
	bounds.Height = max(bounds.Height-state.toolbarDockBottomReserve(state.terminalPage()), 0)
	extent := state.terminalToolbarMetrics().railExtent
	bounds.Width = extent
	if !left {
		bounds.X = max(size.Width-extent, 0)
	}
	return bounds
}

func (state *State) toolbarDockSideBounds(size gfx.Size) gfx.Rect {
	return state.toolbarDockSideBoundsFor(size, state.terminalToolbarPosition == terminalToolbarLeft)
}

// Terminal geometry also reserves the bottom while a section covers it, just
// as it does for the existing side rail. Switching pages must not resize a PTY.
func (state *State) toolbarDockBottomReserve(includeTerminal bool) float32 {
	session, hasTerminal := state.terminalToolbarSession()
	for _, entry := range state.toolbars.dock {
		if !toolbarDockAtBottom(entry.Zone) || (entry.Group == toolbarTerminal && (!includeTerminal || !hasTerminal)) {
			continue
		}
		spec, ok := toolbarSpecFor(entry.Group, entry.Key)
		if !ok || !state.toolbarVisible(entry.Group, spec.id) {
			continue
		}
		// A latency entry reserves its strip only while a reading exists.
		if spec.id == hitToolbarLatency && !state.terminalToolbarOffers(session, spec.id) {
			continue
		}
		return state.terminalToolbarMetrics().railExtent
	}
	return 0
}

func (state *State) toolbarDockBottomBounds(size gfx.Size, emptyTarget bool) gfx.Rect {
	bounds := state.fullWorkspacePixelBounds(size)
	extent := state.toolbarDockBottomReserve(state.terminalPage())
	if emptyTarget {
		extent = state.terminalToolbarMetrics().railExtent
	}
	bounds.Y = max(size.Height-extent, windowBarHeight)
	bounds.Height = min(extent, max(size.Height-windowBarHeight, 0))
	return bounds
}

func (state *State) paintToolbarDockBottom(canvas *gfx.Canvas, size gfx.Size) {
	var leftBuffer, rightBuffer [maxBarTools]barTool
	left := state.toolbarDockTools(toolbarDockBottom, &leftBuffer)
	right := state.toolbarDockTools(toolbarDockBottomRight, &rightBuffer)
	if len(left)+len(right) == 0 {
		return
	}
	bounds := state.toolbarDockBottomBounds(size, false)
	dock := &state.toolbarDock
	dock.zones[3], dock.zones[6] = toolbarDockBottomZones(bounds)
	canvas.Fill(bounds, state.pal.shell)
	canvas.Fill(gfx.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: 1}, state.pal.line)
	viewport := bounds.Inset(gfx.All(4))
	leftExtent, rightExtent := state.toolbarDockHorizontalExtent(left), state.toolbarDockHorizontalExtent(right)
	gap := float32(0)
	if len(left) > 0 && len(right) > 0 {
		gap = min(12, viewport.Width)
	}
	available := max(viewport.Width-gap, 0)
	// Let a small group keep its space; share the rest when both overflow.
	leftWidth := min(leftExtent, max(available/2, available-rightExtent))
	rightWidth := min(rightExtent, max(available-leftWidth, 0))
	leftViewport := gfx.Rect{X: viewport.X, Y: viewport.Y, Width: leftWidth, Height: viewport.Height}
	rightViewport := gfx.Rect{X: viewport.X + viewport.Width - rightWidth, Y: viewport.Y, Width: rightWidth, Height: viewport.Height}
	state.paintToolbarDockBottomGroup(canvas, left, toolbarDockBottom, leftViewport)
	state.paintToolbarDockBottomGroup(canvas, right, toolbarDockBottomRight, rightViewport)
}

func (state *State) toolbarDockHorizontalExtent(tools []barTool) float32 {
	if len(tools) == 0 {
		return 0
	}
	metrics := state.terminalToolbarMetrics()
	return float32(len(tools))*(metrics.toolSize+metrics.gap) - metrics.gap
}

func toolbarDockBottomZones(bounds gfx.Rect) (gfx.Rect, gfx.Rect) {
	left := bounds
	left.Width = bounds.Width / 2
	right := bounds
	right.X += left.Width
	right.Width -= left.Width
	return left, right
}

func (state *State) paintToolbarDockBottomGroup(canvas *gfx.Canvas, tools []barTool, zone toolbarDockZone, viewport gfx.Rect) {
	dock := &state.toolbarDock
	metrics := state.terminalToolbarMetrics()
	index := 0
	if zone == toolbarDockBottomRight {
		index = 1
	}
	dock.bottomViewports[index] = viewport
	dock.bottomMax[index] = max(state.toolbarDockHorizontalExtent(tools)-viewport.Width, 0)
	scroll := &dock.bottomScroll[index]
	*scroll = min(max(*scroll, 0), dock.bottomMax[index])
	for i, tool := range tools {
		if canvas.FocusVisible() == tool.id {
			start := float32(i) * (metrics.toolSize + metrics.gap)
			*scroll = min(max(*scroll, start+metrics.toolSize-viewport.Width), start)
			*scroll = min(max(*scroll, 0), dock.bottomMax[index])
		}
	}
	canvas.Clip(viewport)
	for i, tool := range tools {
		rect := gfx.Rect{X: viewport.X + float32(i)*(metrics.toolSize+metrics.gap) - *scroll, Y: viewport.Y, Width: metrics.toolSize, Height: metrics.toolSize}
		clipped := intersectRect(rect, viewport)
		if clipped.Width <= 0 || clipped.Height <= 0 {
			continue
		}
		if tool.id == hitToolbarLatency {
			session, ok := state.terminalSideRailSession()
			if !ok || !state.paintToolbarLatency(canvas, rect, session) {
				continue
			}
		} else {
			state.paintIconButtonSized(canvas, tool.id, tool.icon, rect, tool.hint, tool.disabled, tool.style, state.pal.shell, metrics.iconSize)
		}
		state.registerToolbarDockTool(canvas, tool, zone, clipped)
	}
	canvas.Unclip()
}

func (state *State) toolbarDockSideReserve() float32 {
	return state.toolbarDockSideReserveFor(true, false) + state.toolbarDockSideReserveFor(false, false)
}

func (state *State) paintToolbarDockSide(canvas *gfx.Canvas, size gfx.Size) {
	session, terminalVisible := state.terminalSideRailSession()
	for _, left := range []bool{false, true} {
		primary := (state.terminalToolbarPosition == terminalToolbarLeft) == left
		if state.toolbarDockSideReserveFor(left, terminalVisible) == 0 {
			if terminalVisible && primary && session != nil {
				state.clearTerminalRail(session)
			}
			continue
		}
		bounds := state.toolbarDockSideBoundsFor(size, left)
		topZone, bottomZone := toolbarDockSideTop, toolbarDockSideBottom
		if left {
			topZone, bottomZone = toolbarDockLeftTop, toolbarDockLeftBottom
		}
		var topBuffer, bottomBuffer [maxBarTools]barTool
		top := state.toolbarDockTools(topZone, &topBuffer)
		bottom := state.toolbarDockTools(bottomZone, &bottomBuffer)
		dock := &state.toolbarDock
		dock.zones[toolbarDockZoneIndex(topZone)] = gfx.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: bounds.Height / 2}
		dock.zones[toolbarDockZoneIndex(bottomZone)] = gfx.Rect{X: bounds.X, Y: bounds.Y + bounds.Height/2, Width: bounds.Width, Height: bounds.Height / 2}
		background, border := state.pal.shell, state.pal.line
		if terminalVisible {
			background, border = state.pal.workspace, state.pal.line
			if primary {
				state.setTerminalRailTools(session, append(top, bottom...))
				if len(bottom) == 0 {
					state.paintTerminalSideRail(canvas, bounds, session, top, background, border, left)
					continue
				}
			}
		}
		canvas.Fill(bounds, background)
		x := bounds.X
		if left {
			x += bounds.Width - 1
		}
		canvas.Fill(gfx.Rect{X: x, Y: bounds.Y, Width: 1, Height: bounds.Height}, border)
		viewport := bounds.Inset(gfx.All(4))
		if terminalVisible && primary {
			session.term.railBounds = bounds
		}
		bottomHeight := min(state.terminalRailContentExtent(bottom), viewport.Height/2)
		if len(top) == 0 {
			bottomHeight = min(state.terminalRailContentExtent(bottom), viewport.Height)
		}
		if len(bottom) == 0 {
			bottomHeight = 0
		}
		topViewport := viewport
		topViewport.Height = max(viewport.Height-bottomHeight, 0)
		bottomViewport := gfx.Rect{X: viewport.X, Y: viewport.Y + viewport.Height - bottomHeight, Width: viewport.Width, Height: bottomHeight}
		var owner *Session
		if terminalVisible && primary {
			owner = session
		}
		state.paintToolbarDockSideGroup(canvas, top, topZone, topViewport, background, owner)
		state.paintToolbarDockSideGroup(canvas, bottom, bottomZone, bottomViewport, background, owner)
	}
}

func toolbarDockSideIndex(zone toolbarDockZone) int {
	switch zone {
	case toolbarDockSideTop:
		return 0
	case toolbarDockSideBottom:
		return 1
	case toolbarDockLeftTop:
		return 2
	default:
		return 3
	}
}

func (state *State) paintToolbarDockSideGroup(canvas *gfx.Canvas, tools []barTool, zone toolbarDockZone, viewport gfx.Rect, background gfx.Color, session *Session) {
	dock := &state.toolbarDock
	metrics := state.terminalToolbarMetrics()
	index := toolbarDockSideIndex(zone)
	dock.sideViewports[index] = viewport
	maximum := max(state.terminalRailContentExtent(tools)-viewport.Height, 0)
	scroll := &dock.sideScroll[index]
	if index%2 == 0 && session != nil {
		scroll = &session.term.railScroll
		session.term.railViewport, session.term.railContentExtent = viewport, state.terminalRailContentExtent(tools)
	}
	dock.sideMax[index] = maximum
	*scroll = min(max(*scroll, 0), maximum)
	for i, tool := range tools {
		if canvas.FocusVisible() == tool.id {
			start := state.terminalRailItemStart(tools, i)
			*scroll = min(max(*scroll, start+metrics.toolSize-viewport.Height), start)
			*scroll = min(max(*scroll, 0), maximum)
		}
	}
	canvas.Clip(viewport)
	for i, tool := range tools {
		tool.rect = gfx.Rect{X: viewport.X, Y: viewport.Y + state.terminalRailItemStart(tools, i) - *scroll, Width: viewport.Width, Height: metrics.toolSize}
		clipped := intersectRect(tool.rect, viewport)
		if clipped.Width <= 0 || clipped.Height <= 0 {
			continue
		}
		if tool.id == hitToolbarLatency {
			terminal, ok := state.terminalSideRailSession()
			if !ok || !state.paintToolbarLatency(canvas, tool.rect, terminal) {
				continue
			}
		} else {
			state.paintIconButtonSized(canvas, tool.id, tool.icon, tool.rect, tool.hint, tool.disabled, tool.style, background, metrics.iconSize)
		}
		state.registerToolbarDockTool(canvas, tool, zone, clipped)
		if tool.id == hitWorkspacePaneTools {
			state.workspacePaneTools.anchor = tool.rect
		}
	}
	canvas.Unclip()
}

func (state *State) toolbarDockDropZones() [7]gfx.Rect {
	zones := state.toolbarDock.zones
	if zones[3].Height == 0 {
		zones[3], zones[6] = toolbarDockBottomZones(state.toolbarDockBottomBounds(state.viewport, true))
	}
	for _, left := range []bool{false, true} {
		index := 1
		if left {
			index = 4
		}
		if zones[index].Width == 0 {
			bounds := state.toolbarDockSideBoundsFor(state.viewport, left)
			zones[index] = gfx.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: bounds.Height / 2}
			zones[index+1] = gfx.Rect{X: bounds.X, Y: bounds.Y + bounds.Height/2, Width: bounds.Width, Height: bounds.Height / 2}
		}
		for i := index; i <= index+1; i++ {
			zones[i].Height = max(min(zones[i].Y+zones[i].Height, zones[3].Y)-zones[i].Y, 0)
		}
	}
	return zones
}

func (state *State) toolbarDockDropAt(at gfx.Point) toolbarDockDrop {
	dock := &state.toolbarDock
	zones := state.toolbarDockDropZones()
	hoverZone := -1
	for _, hit := range dock.hits[:dock.count] {
		if pointInRect(at.X, at.Y, hit.bounds) {
			hoverZone = toolbarDockZoneIndex(hit.zone)
			break
		}
	}
	for i, bounds := range zones {
		if hoverZone >= 0 && i != hoverZone {
			continue
		}
		if hoverZone == i {
			if toolbarDockVertical(toolbarDockZones[i]) {
				bounds = state.toolbarDockSideBoundsFor(state.viewport, toolbarDockLeft(toolbarDockZones[i]))
			} else if toolbarDockAtBottom(toolbarDockZones[i]) {
				bounds = state.toolbarDockBottomBounds(state.viewport, true)
			}
		}
		if !pointInRect(at.X, at.Y, bounds) {
			continue
		}
		zone := toolbarDockZones[i]
		drop := toolbarDockDrop{valid: true, zone: zone}
		position := bounds.Y + 8
		if !toolbarDockVertical(zone) {
			position = bounds.X + 2
			if zone == toolbarDockBottomRight {
				position = bounds.X + bounds.Width - 6
			}
		}
		for _, hit := range dock.hits[:dock.count] {
			if hit.zone != zone || hit.tool.id == dock.drag.tool.id {
				continue
			}
			start, extent, coordinate := hit.bounds.Y, hit.bounds.Height, at.Y
			if !toolbarDockVertical(zone) {
				start, extent, coordinate = hit.bounds.X, hit.bounds.Width, at.X
			}
			if coordinate < start+extent/2 {
				drop.before, position = hit.tool.id, start-2
				break
			}
			position = start + extent + 2
		}
		if !toolbarDockVertical(zone) {
			drop.marker = gfx.Rect{X: position, Y: bounds.Y + 5, Width: 2, Height: bounds.Height - 10}
		} else {
			drop.marker = gfx.Rect{X: bounds.X + 5, Y: min(position, bounds.Y+bounds.Height-2), Width: bounds.Width - 10, Height: 2}
		}
		return drop
	}
	return toolbarDockDrop{}
}

func (state *State) toolbarDockPointer(event gfx.PointerEvent) gfx.EventResult {
	dock := &state.toolbarDock
	if dock.menu {
		if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonSecondary && event.Target == hitToolbarDockDismiss {
			dock.menu = false
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return gfx.EventResult{}
	}
	if state.modal != modalNone {
		dock.drag = toolbarDockDrag{}
		return gfx.EventResult{}
	}
	drag := &dock.drag
	switch event.Kind {
	case gfx.PointerDown:
		*drag = toolbarDockDrag{}
		for _, hit := range dock.hits[:dock.count] {
			if event.Target != hit.tool.id {
				continue
			}
			if event.Button == gfx.PointerButtonSecondary {
				dock.menu, dock.overflow, dock.menuTool, dock.menuAt = true, false, hit.tool.id, gfx.Point{X: event.X, Y: event.Y}
				return gfx.EventResult{Handled: true, Repaint: true}
			}
			if event.Button == gfx.PointerButtonPrimary {
				*drag = toolbarDockDrag{pressed: true, tool: hit.tool, start: gfx.Point{X: event.X, Y: event.Y}}
			}
			break
		}
	case gfx.PointerMove:
		if !drag.pressed {
			return gfx.EventResult{}
		}
		if drag.cancelled {
			return gfx.EventResult{Handled: true}
		}
		drag.at = gfx.Point{X: event.X, Y: event.Y}
		dx, dy := event.X-drag.start.X, event.Y-drag.start.Y
		if !drag.active && dx*dx+dy*dy < tabDragThreshold*tabDragThreshold {
			return gfx.EventResult{}
		}
		drag.active = true
		drag.drop = state.toolbarDockDropAt(drag.at)
		return gfx.EventResult{Handled: true, Repaint: true}
	case gfx.PointerExit:
		if drag.pressed {
			drag.cancelled = true
			drag.active = false
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	case gfx.PointerUp:
		finished := *drag
		*drag = toolbarDockDrag{}
		if finished.cancelled {
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		if !finished.active {
			return gfx.EventResult{}
		}
		// Keep the source identity available while resolving the final insertion.
		dock.drag = finished
		drop := state.toolbarDockDropAt(gfx.Point{X: event.X, Y: event.Y})
		dock.drag = toolbarDockDrag{}
		if drop.valid {
			state.moveToolbarDockTool(finished.tool.id, drop.zone, drop.before)
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

func (state *State) toolbarDockKey(event gfx.KeyEvent) gfx.EventResult {
	dock := &state.toolbarDock
	if event.Release {
		return gfx.EventResult{}
	}
	if event.Key != gfx.KeyEscape {
		if dock.menu || dock.drag.pressed || state.modal != modalNone || event.Modifiers != 0 {
			return gfx.EventResult{}
		}
		for _, hit := range dock.hits[:dock.count] {
			if hit.tool.id != event.Target {
				continue
			}
			var buffer [maxBarTools]barTool
			tools := state.toolbarDockTools(hit.zone, &buffer)
			if hit.zone == toolbarDockTop && len(tools) > state.toolbarTopLimit() {
				tools = tools[:state.toolbarTopLimit()-1]
			}
			vertical := toolbarDockVertical(hit.zone)
			if vertical {
				topZone, bottomZone := toolbarDockSideTop, toolbarDockSideBottom
				if toolbarDockLeft(hit.zone) {
					topZone, bottomZone = toolbarDockLeftTop, toolbarDockLeftBottom
				}
				tools = state.toolbarDockTools(topZone, &buffer)
				var bottom [maxBarTools]barTool
				tools = append(tools, state.toolbarDockTools(bottomZone, &bottom)...)
			}
			if toolbarDockAtBottom(hit.zone) {
				tools = state.toolbarDockTools(toolbarDockBottom, &buffer)
				var right [maxBarTools]barTool
				tools = append(tools, state.toolbarDockTools(toolbarDockBottomRight, &right)...)
			}
			at := slices.IndexFunc(tools, func(tool barTool) bool { return tool.id == event.Target })
			delta := 0
			switch {
			case event.Key == gfx.KeyLeft && !vertical || event.Key == gfx.KeyUp && vertical:
				delta = -1
			case event.Key == gfx.KeyRight && !vertical || event.Key == gfx.KeyDown && vertical:
				delta = 1
			case event.Key == gfx.KeyHome:
				at, delta = -1, 1
			case event.Key == gfx.KeyEnd:
				at, delta = len(tools), -1
			default:
				return gfx.EventResult{}
			}
			for next := at + delta; next >= 0 && next < len(tools); next += delta {
				// The latency readout is a status, not a keyboard stop.
				if !tools[next].disabled && tools[next].id != hitToolbarLatency {
					return gfx.EventResult{Handled: true, Repaint: true, Focus: tools[next].id, FocusVisible: true}
				}
			}
			return gfx.EventResult{Handled: true}
		}
		return gfx.EventResult{}
	}
	if dock.drag.pressed {
		dock.drag.active, dock.drag.cancelled = false, true
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if dock.menu {
		dock.menu = false
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

func (state *State) paintToolbarDockDrag(canvas *gfx.Canvas) {
	drag := &state.toolbarDock.drag
	if !drag.active {
		return
	}
	canvas.PushLayer(tabDragLayer + 1)
	defer canvas.PopLayer()
	for _, bounds := range state.toolbarDockDropZones() {
		canvas.Rounded(bounds.Inset(gfx.All(1)), 4, withAlpha(state.pal.accent, 0.06))
	}
	if drag.drop.valid {
		bounds := state.toolbarDockDropZones()[toolbarDockZoneIndex(drag.drop.zone)]
		if bounds.Width == 0 {
			if toolbarDockAtBottom(drag.drop.zone) {
				bounds = state.toolbarDockBottomBounds(state.viewport, true)
			} else {
				bounds = state.toolbarDockSideBounds(state.viewport)
			}
		}
		canvas.Rounded(bounds.Inset(gfx.All(1)), 4, withAlpha(state.pal.accent, 0.10))
		canvas.Rounded(drag.drop.marker, 1, state.pal.accent)
	}
	box := gfx.Rect{X: drag.at.X + 12, Y: drag.at.Y + 12, Width: 36, Height: 36}
	box.X = min(box.X, state.viewport.Width-box.Width-4)
	box.Y = min(box.Y, state.viewport.Height-box.Height-4)
	canvas.RoundedBorder(box, 7, 1, state.pal.accent, state.pal.workspace)
	if drag.tool.id == hitToolbarLatency {
		if session, ok := state.terminalSideRailSession(); ok {
			if text, color, ok := state.latencyInfo(session); ok {
				state.paintRailLatencyVisual(canvas, text, color, box, true)
				return
			}
		}
	}
	canvas.DrawIcon(lucideIcons[drag.tool.icon], box.Inset(gfx.All(9)), state.pal.text)
}

func (state *State) toolbarDockScroll(event gfx.ScrollEvent) gfx.EventResult {
	if state.modal != modalNone {
		return gfx.EventResult{}
	}
	dock := &state.toolbarDock
	metrics := state.terminalToolbarMetrics()
	for i := range dock.bottomViewports {
		if !pointInRect(event.X, event.Y, dock.bottomViewports[i]) {
			continue
		}
		delta := event.DeltaX
		if delta == 0 {
			delta = event.StepY(metrics.toolSize + metrics.gap)
		}
		before := dock.bottomScroll[i]
		dock.bottomScroll[i] = min(max(before+delta, 0), dock.bottomMax[i])
		return gfx.EventResult{Handled: true, Repaint: before != dock.bottomScroll[i]}
	}
	if pointInRect(event.X, event.Y, dock.zones[3]) || pointInRect(event.X, event.Y, dock.zones[6]) {
		return gfx.EventResult{Handled: true}
	}
	for i, viewport := range dock.sideViewports {
		bounds := viewport
		bounds.X -= 4
		bounds.Width += 8
		if !pointInRect(event.X, event.Y, bounds) {
			continue
		}
		scroll := &dock.sideScroll[i]
		if i%2 == 0 && (i >= 2) == (state.terminalToolbarPosition == terminalToolbarLeft) {
			if session, ok := state.terminalSideRailSession(); ok {
				scroll = &session.term.railScroll
			}
		}
		before := *scroll
		*scroll = min(max(before+event.StepY(metrics.toolSize+metrics.gap), 0), dock.sideMax[i])
		return gfx.EventResult{Handled: true, Repaint: *scroll != before}
	}
	return gfx.EventResult{}
}
