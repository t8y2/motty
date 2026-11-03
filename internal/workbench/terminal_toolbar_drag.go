package workbench

import (
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
)

const (
	terminalToolbarDragThreshold = float32(6)
	terminalToolbarDragSwitch    = float32(0.28)
	terminalToolbarDragLayer     = uint16(710)
)

// terminalToolbarDrag follows a press on a rail control. It stays a candidate
// until the pointer moves, so an ordinary click reaches the control unchanged.
// Once active it previews both ordering and side changes without saving either
// until release.
type terminalToolbarRailDrag struct {
	pressed bool
	active  bool

	sessionID uint32
	key       string
	id        ui.ID
	icon      uint8
	kind      barToolKind
	height    float32

	sourceZone terminalToolbarZone
	zone       terminalToolbarZone
	start, at  ui.Point
	// dropIndex is in the destination list after the source is removed;
	// displayIndex is the matching boundary in the rail as currently painted,
	// where the source still occupies its dimmed placeholder.
	dropIndex, displayIndex int
	hasDrop                 bool

	// previewAddsRail says this frame needs the destination rail even when it
	// currently has no controls. previewSide remembers which side caused that
	// layout so moving the preview to the other edge still performs one refit.
	previewAddsRail bool
	previewSide     terminalToolbarSide
	previewSideSet  bool
}

func (state *State) terminalRailDragPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	drag := &state.terminalToolbarDrag
	switch event.Kind {
	case ui.PointerDown:
		*drag = terminalToolbarRailDrag{}
		if state.modal != modalNone || state.activeSection != sectionTerminal ||
			(event.Button != ui.PointerButtonPrimary && event.Button != ui.PointerButtonNone) {
			return ui.EventResult{}, false
		}
		session, ok := state.terminalSideRailSession()
		if !ok {
			return ui.EventResult{}, false
		}
		var toolList [maxBarTools]barTool
		tools := state.activeTerminalTools(session, &toolList)
		var set terminalToolZoneSet
		state.splitTerminalTools(tools, &set)
		sourceID := event.Target
		if event.Target.Scope() == hitTerminalToolbarDrag {
			index := item(event.Target)
			if index < 0 || index >= len(toolbarCatalog[toolbarTerminal]) {
				return ui.EventResult{}, false
			}
			sourceID = toolbarCatalog[toolbarTerminal][index].id
		}
		for zone := range set.zones {
			for _, tool := range set.tools(terminalToolbarZone(zone)) {
				if tool.id != sourceID {
					continue
				}
				key, found := state.terminalToolbarKeyForID(tool.id)
				if !found {
					return ui.EventResult{}, false
				}
				*drag = terminalToolbarRailDrag{
					pressed: true, sessionID: session.ID, key: key, id: tool.id,
					icon: tool.icon, kind: tool.kind, height: tool.height,
					sourceZone: terminalToolbarZone(zone), zone: terminalToolbarZone(zone),
					start: ui.Point{X: event.X, Y: event.Y},
					at:    ui.Point{X: event.X, Y: event.Y},
				}
				return ui.EventResult{}, false
			}
		}
		return ui.EventResult{}, false

	case ui.PointerMove:
		if !drag.pressed {
			return ui.EventResult{}, false
		}
		session, ok := state.terminalRailDragSession()
		if !ok {
			changed := state.cancelTerminalRailDrag(false)
			return ui.EventResult{Handled: changed, Repaint: changed}, changed
		}
		dx, dy := event.X-drag.start.X, event.Y-drag.start.Y
		if !drag.active {
			if dx*dx+dy*dy < terminalToolbarDragThreshold*terminalToolbarDragThreshold {
				return ui.EventResult{}, false
			}
			drag.active = true
		}
		drag.at = ui.Point{X: event.X, Y: event.Y}
		state.updateTerminalRailDragDrop(session, event, true)
		return ui.EventResult{Handled: true, Repaint: true, Focus: drag.id}, true

	case ui.PointerUp:
		if !drag.pressed {
			return ui.EventResult{}, false
		}
		finished := *drag
		*drag = terminalToolbarRailDrag{}
		if !finished.active {
			return ui.EventResult{}, false
		}
		session, ok := state.terminalRailDragSessionFor(finished)
		if ok {
			state.updateTerminalRailDragDropFor(&finished, session, event, false)
		}
		state.terminalToolbarDrag = terminalToolbarRailDrag{}

		moved := false
		if ok && finished.active && finished.hasDrop {
			zone, tools := state.terminalRailToolsForDrag(session, finished.zone)
			visible := terminalRailToolsWithout(tools, finished.id)
			slot := state.terminalRailPersistedDropSlot(finished.zone, visible, finished.dropIndex, finished.key)
			moved = moveToolbarEntryToZone(state, finished.key, zone, slot)
		}
		if !moved && finished.previewAddsRail {
			state.fitTerminalRailDragLayout()
		}
		return ui.EventResult{Handled: true, Repaint: true, Focus: finished.id}, true

	case ui.PointerExit:
		if !drag.pressed {
			return ui.EventResult{}, false
		}
		wasActive := drag.active
		state.cancelTerminalRailDrag(true)
		return ui.EventResult{Handled: wasActive, Repaint: wasActive}, wasActive
	}
	return ui.EventResult{}, false
}

func (state *State) terminalRailDragKey(event ui.KeyEvent) (ui.EventResult, bool) {
	drag := &state.terminalToolbarDrag
	if !drag.pressed || event.Release || event.Key != ui.KeyEscape {
		return ui.EventResult{}, false
	}
	id := drag.id
	active := drag.active
	state.cancelTerminalRailDrag(true)
	return ui.EventResult{Handled: true, Repaint: true, Focus: id, FocusVisible: active}, true
}

// cancelTerminalRailDrag clears the visual gesture. A revealed empty rail
// changes the pane width, so cancellation restores the terminal fit once after
// the preview has been removed.
func (state *State) cancelTerminalRailDrag(refit bool) bool {
	drag := state.terminalToolbarDrag
	if !drag.pressed {
		return false
	}
	state.terminalToolbarDrag = terminalToolbarRailDrag{}
	if refit && drag.previewAddsRail {
		state.fitTerminalRailDragLayout()
	}
	return drag.active
}

func (state *State) terminalRailDragSession() (*Session, bool) {
	drag := &state.terminalToolbarDrag
	if !drag.pressed {
		return nil, false
	}
	return state.terminalRailDragSessionFor(*drag)
}

func (state *State) terminalRailDragSessionFor(drag terminalToolbarRailDrag) (*Session, bool) {
	if state.modal != modalNone || state.activeSection != sectionTerminal {
		return nil, false
	}
	session, ok := state.terminalSideRailSession()
	if !ok || session.ID != drag.sessionID {
		return nil, false
	}
	return session, true
}

func (state *State) terminalToolbarKeyForID(id ui.ID) (string, bool) {
	for _, spec := range toolbarCatalog[toolbarTerminal] {
		if spec.id == id {
			return spec.key, true
		}
	}
	return "", false
}

func terminalToolbarDragHandle(id ui.ID) ui.ID {
	for index, spec := range toolbarCatalog[toolbarTerminal] {
		if spec.id == id {
			return hitTerminalToolbarDrag.At(index)
		}
	}
	return ui.ID{}
}

// terminalRailDropZone mirrors the split docking gesture horizontally and adds
// a top/bottom boundary vertically. The center preserves the source side so a
// vertical reorder does not unexpectedly reveal the opposite rail.
func terminalRailDropZone(source terminalToolbarZone, x, y float32, bounds ui.Rect) terminalToolbarZone {
	side := source.Side()
	if x < bounds.X+bounds.Width*terminalToolbarDragSwitch {
		side = terminalToolbarLeft
	} else if x > bounds.X+bounds.Width*(1-terminalToolbarDragSwitch) {
		side = terminalToolbarRight
	}
	dock := terminalToolbarDockTop
	if y >= bounds.Y+bounds.Height/2 {
		dock = terminalToolbarDockBottom
	}
	return terminalToolbarZoneFromParts(side, dock)
}

func (state *State) updateTerminalRailDragDrop(session *Session, event ui.PointerEvent, fit bool) {
	state.updateTerminalRailDragDropFor(&state.terminalToolbarDrag, session, event, fit)
}

func (state *State) updateTerminalRailDragDropFor(drag *terminalToolbarRailDrag, session *Session, event ui.PointerEvent, fit bool) {
	bounds := state.fullWorkspacePixelBounds(event.Viewport)
	inside := event.X >= bounds.X && event.X <= bounds.X+bounds.Width &&
		event.Y >= bounds.Y && event.Y <= bounds.Y+bounds.Height
	oldAdds, oldSide, oldSet := drag.previewAddsRail, drag.previewSide, drag.previewSideSet
	drag.hasDrop = inside
	drag.at = ui.Point{X: event.X, Y: event.Y}
	if inside {
		drag.zone = terminalRailDropZone(drag.sourceZone, event.X, event.Y, bounds)
		_, tools := state.terminalRailToolsForDrag(session, drag.zone)
		drag.displayIndex, drag.dropIndex = terminalRailVisibleDropIndex(
			tools, drag.id, event.Y, &session.term.rails[drag.zone.Side()], drag.zone.Dock(), state.terminalToolbarMetrics(),
		)
	}
	configured, _ := state.terminalToolbarConfiguredOccupiedSides()
	drag.previewAddsRail = drag.hasDrop && !configured[drag.zone.Side()]
	drag.previewSide = drag.zone.Side()
	drag.previewSideSet = drag.hasDrop
	changed := oldAdds != drag.previewAddsRail ||
		(drag.previewAddsRail && (!oldSet || oldSide != drag.previewSide))
	if fit && changed {
		state.fitTerminalRailDragLayout()
	}
}

func (state *State) fitTerminalRailDragLayout() {
	if state.viewport.Width <= 0 || state.viewport.Height <= 0 {
		return
	}
	state.fitWorkspaceTerminals(state.deck.ActiveWorkspace())
	state.fitSFTP(state.viewport)
}

// terminalRailToolsForDrag returns the visible controls that a drop would
// reorder. It is intentionally the same filtered list the rail paints, so
// context-only entries do not create invisible spacing in the gesture.
func (state *State) terminalRailToolsForDrag(session *Session, zone terminalToolbarZone) (terminalToolbarZone, []barTool) {
	var toolList [maxBarTools]barTool
	tools := state.activeTerminalTools(session, &toolList)
	var set terminalToolZoneSet
	state.splitTerminalTools(tools, &set)
	return zone, set.tools(zone)
}

func terminalRailToolsWithout(tools []barTool, excluded ui.ID) []barTool {
	filtered := make([]barTool, 0, len(tools))
	for _, tool := range tools {
		if tool.id != excluded {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

// terminalRailVisibleDropIndex returns both the boundary where the insertion
// line is painted and the index after the dragged source is removed. Keeping
// the dimmed source in the paint geometry avoids a jump when an item is dragged
// over the row immediately after it. Hidden catalog entries are mapped back to
// their persisted slots at drop time by terminalRailPersistedDropSlot.
func terminalRailVisibleDropIndex(tools []barTool, source ui.ID, y float32, rail *terminalRailState, dock terminalToolbarDock, metrics terminalToolbarMetrics) (display, filtered int) {
	zone := &rail.zones[dock]
	viewportY := zone.viewport.Y
	if zone.viewport.Width <= 0 || zone.viewport.Height <= 0 {
		viewportY = y - metrics.inset
	}
	contentY := y - viewportY - zone.itemOffset + zone.scroll
	display = len(tools)
	for index, tool := range tools {
		start := terminalRailItemStart(tools, index, metrics)
		if contentY < start+tool.railHeight(metrics.toolSize)/2 {
			display = index
			break
		}
	}
	filtered = display
	for index, tool := range tools {
		if tool.id == source && display > index {
			filtered--
			break
		}
	}
	return display, max(filtered, 0)
}

func (state *State) terminalRailPersistedDropSlot(zone terminalToolbarZone, visible []barTool, index int, sourceKey string) int {
	full := make([]toolbarEntry, 0, len(toolbarCatalog[toolbarTerminal]))
	for _, entry := range state.toolbars.entries(toolbarTerminal) {
		if entry.Key == sourceKey ||
			terminalToolbarZoneForEntry(entry, terminalToolbarRight) != zone {
			continue
		}
		full = append(full, entry)
	}
	if index < 0 || index >= len(visible) {
		return len(full)
	}
	anchorID := visible[index].id
	for at, entry := range full {
		spec, ok := toolbarSpecFor(toolbarTerminal, entry.Key)
		if ok && spec.id == anchorID {
			return at
		}
	}
	return len(full)
}

// terminalRailDragPreviewSide reports the empty rail that must be painted and
// reserve workspace width for the current drop preview.
func (state *State) terminalRailDragPreviewSide(session *Session) (terminalToolbarSide, bool) {
	drag := &state.terminalToolbarDrag
	if !drag.active || !drag.hasDrop || !drag.previewAddsRail || session == nil ||
		session.ID != drag.sessionID || state.modal != modalNone || state.activeSection != sectionTerminal {
		return terminalToolbarRight, false
	}
	return drag.zone.Side(), true
}

func (state *State) terminalRailDragTargets(session *Session, side terminalToolbarSide) bool {
	drag := &state.terminalToolbarDrag
	return drag.active && drag.hasDrop && session != nil && session.ID == drag.sessionID && drag.zone.Side() == side
}

func (state *State) terminalRailDragTarget(session *Session, side terminalToolbarSide, dock terminalToolbarDock) bool {
	drag := &state.terminalToolbarDrag
	return drag.active && drag.hasDrop && session != nil && session.ID == drag.sessionID &&
		drag.zone == terminalToolbarZoneFromParts(side, dock)
}

func (state *State) terminalRailDragSource(session *Session, id ui.ID) bool {
	drag := &state.terminalToolbarDrag
	return drag.active && drag.hasDrop && session != nil && session.ID == drag.sessionID && drag.id == id
}

func (state *State) paintTerminalRailDragSource(canvas *ui.Canvas, tool barTool, metrics terminalToolbarMetrics) {
	pal := state.pal
	if tool.kind == barToolConnectionColor || tool.kind == barToolLatency {
		color := withAlpha(pal.accent, 0.12)
		if tool.kind == barToolLatency {
			color = withAlpha(pal.surfaceHot, 0.45)
		}
		canvas.Rounded(tool.rect, min(radius.control, tool.rect.Height/2), color)
		return
	}
	f := state.chrome.widget.Begin(canvas, state.Theme(), tool.rect)
	f.Stack().Fill().Center().Children(func() {
		f.Row().Fill().Radius(radius.control).Background(withAlpha(pal.surfaceHot, 0.45))
		if int(tool.icon) < len(lucideIcons) {
			f.Icon(lucideIcons[tool.icon]).Size(metrics.iconSize, metrics.iconSize).
				Color(withAlpha(pal.muted, 0.32))
		}
	})
	f.End()
}

// paintTerminalToolbarDrag draws the destination line and the floating source
// above the terminal. It declares no hit: the pointer remains captured by the
// original rail control for the duration of the gesture.
func (state *State) paintTerminalToolbarDrag(canvas *ui.Canvas, viewport ui.Rect) {
	drag := &state.terminalToolbarDrag
	session, ok := state.terminalRailDragSession()
	if !ok || !drag.active {
		return
	}
	canvas.PushLayer(terminalToolbarDragLayer)
	defer canvas.PopLayer()

	if drag.hasDrop {
		_, tools := state.terminalRailToolsForDrag(session, drag.zone)
		metrics := state.terminalToolbarMetrics()
		rail := &session.term.rails[drag.zone.Side()]
		zone := &rail.zones[drag.zone.Dock()]
		railViewport := zone.viewport
		if railViewport.Width > 0 && railViewport.Height > 0 {
			offset := terminalRailItemStart(tools, min(max(drag.displayIndex, 0), len(tools)), metrics)
			if drag.displayIndex > 0 && drag.displayIndex <= len(tools) {
				previous := drag.displayIndex - 1
				offset = terminalRailItemStart(tools, previous, metrics) +
					tools[previous].railHeight(metrics.toolSize) + metrics.toolGap
				if tools[previous].groupEnd {
					offset += metrics.toolGap
				}
			}
			y := min(max(railViewport.Y+zone.itemOffset+offset-zone.scroll, railViewport.Y+2), railViewport.Y+railViewport.Height-2)
			glow := ui.Rect{X: railViewport.X + 1, Y: y - 3, Width: max(railViewport.Width-2, 0), Height: 6}
			line := ui.Rect{X: railViewport.X + 2, Y: y - 1, Width: max(railViewport.Width-4, 0), Height: 2}
			canvas.Rounded(glow, 3, withAlpha(state.pal.accent, 0.22))
			canvas.Rounded(line, 1, state.pal.accent)
		}
	}

	metrics := state.terminalToolbarMetrics()
	width := metrics.toolSize
	height := metrics.toolSize
	ghost := ui.Rect{
		X:      min(max(drag.at.X-width/2, viewport.X+4), max(viewport.X+viewport.Width-width-4, viewport.X)),
		Y:      min(max(drag.at.Y-height/2, viewport.Y+4), max(viewport.Y+viewport.Height-height-4, viewport.Y)),
		Width:  width,
		Height: height,
	}
	canvas.Rounded(ui.Rect{X: ghost.X - 3, Y: ghost.Y + 4, Width: ghost.Width + 6, Height: ghost.Height + 3}, radius.control+3, withAlpha(state.pal.shell, 0.34))
	f := state.chrome.widget.Begin(canvas, state.Theme(), ghost)
	f.Row().Fill().Items(flex.Center).Justify(flex.Center).
		Radius(radius.control).Border(1, withAlpha(state.pal.accent, 0.82)).
		Background(state.pal.tabActive.Over(state.pal.shell)).Children(func() {
		if int(drag.icon) < len(lucideIcons) {
			f.Icon(lucideIcons[drag.icon]).Size(metrics.iconSize, metrics.iconSize).Color(state.pal.text)
		}
	})
	f.End()
}
