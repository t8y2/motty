package workbench

import (
	"fmt"
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

var toolbarSettingsHits = tree.Feature("toolbar-settings")
var (
	hitToolbarSettingsOpen    = [3]ui.ID{tree.ID(toolbarSettingsHits, "window"), tree.ID(toolbarSettingsHits, "terminal"), tree.ID(toolbarSettingsHits, "sftp")}
	hitToolbarSettingsClose   = tree.ID(toolbarSettingsHits, "close")
	hitToolbarSettingsDismiss = tree.ID(toolbarSettingsHits, "dismiss")
	hitToolbarEnableAll       = tree.ID(toolbarSettingsHits, "enable-all")
	hitToolbarDisableAll      = tree.ID(toolbarSettingsHits, "disable-all")
	hitToolbarReset           = tree.ID(toolbarSettingsHits, "reset")
	hitToolbarScroll          = tree.ID(toolbarSettingsHits, "scroll")
	hitToolbarSizeSmall       = tree.ID(toolbarSettingsHits, "size-small")
	hitToolbarSizeMedium      = tree.ID(toolbarSettingsHits, "size-medium")
	hitToolbarSizeLarge       = tree.ID(toolbarSettingsHits, "size-large")
	hitToolbarConnectionColor = tree.ID(toolbarSettingsHits, "connection-color")
	hitToolbarLatency         = tree.ID(toolbarSettingsHits, "latency")
	hitToolbarHandles         = tree.Sub(toolbarSettingsHits, tree.Name("handle"))
	hitToolbarToggles         = tree.Sub(toolbarSettingsHits, tree.Name("toggle"))
)

type toolbarSettingsUI struct {
	group        toolbarGroup
	list         components.ReorderListState
	zoneLists    [4]components.ReorderListState
	scroll       float32
	geometry     components.ScrollAreaGeometry
	scrollDrag   bool
	thumbOffset  float32
	handleLabels []string
	zoneKeys     [4][]string
	zoneViewport [4]ui.Rect
	zoneContent  [4]ui.Rect
	drag         terminalToolbarDrag
}

type terminalToolbarDrag struct {
	active         bool
	key            string
	id             ui.ID
	zone           terminalToolbarZone
	startX, startY float32
	dragging       bool
	dropZone       terminalToolbarZone
	dropIndex      int
	hasDrop        bool
}

func (state *State) toolbarGroupName(group toolbarGroup) string {
	m := state.messages.Toolbars
	return [3]string{m.Window, m.Terminal, m.SFTP}[group]
}

func toolbarEntryIndex(group toolbarGroup, key string) int {
	offset := 0
	for _, catalog := range toolbarCatalog[:group] {
		offset += len(catalog)
	}
	for i, spec := range toolbarCatalog[group] {
		if spec.key == key {
			return offset + i
		}
	}
	return -1
}

func (state *State) paintToolbarSettingsRows(page *settingsPage) {
	m := state.messages.Toolbars
	if state.toolbars.summaryFormat != m.EnabledFmt {
		state.toolbars.summary = [3]string{}
		state.toolbars.summaryFormat = m.EnabledFmt
	}
	for group := toolbarWindow; group <= toolbarSFTP; group++ {
		entries := state.toolbars.entries(group)
		enabled := 0
		for _, entry := range entries {
			if entry.Enabled {
				enabled++
			}
		}
		if state.toolbars.summary[group] == "" || state.toolbars.summaryCounts[group] != enabled || state.toolbars.summarySizes[group] != len(entries) {
			state.toolbars.summary[group] = fmt.Sprintf(m.EnabledFmt, enabled, len(entries))
			state.toolbars.summaryCounts[group], state.toolbars.summarySizes[group] = enabled, len(entries)
		}
		page.actionRow(state.toolbarGroupName(group), state.toolbars.summary[group], false,
			components.ButtonAction{ID: hitToolbarSettingsOpen[group], Label: m.Customize, Variant: components.ButtonOutline})
	}
}

func (state *State) openToolbarSettings(group toolbarGroup) {
	state.toolbarEditor = toolbarSettingsUI{
		group:        group,
		handleLabels: make([]string, len(toolbarCatalog[group])),
	}
	for zone := range state.toolbarEditor.zoneKeys {
		state.toolbarEditor.zoneKeys[zone] = make([]string, 0, len(toolbarCatalog[group]))
	}
	for i, spec := range toolbarCatalog[group] {
		state.toolbarEditor.handleLabels[i] = fmt.Sprintf(state.messages.Toolbars.DragFmt, state.toolbarLabel(spec.id))
	}
	state.modal = modalToolbarSettings
}

func (state *State) clickToolbarSettings(id ui.ID) {
	editor := &state.toolbarEditor
	changed, layoutChanged := false, false
	switch {
	case id == hitToolbarSettingsClose || id == hitToolbarSettingsDismiss:
		state.closeModal()
		return
	case id == hitToolbarSizeSmall:
		changed = state.setTerminalToolbarSize(terminalToolbarSmall)
		layoutChanged = changed
	case id == hitToolbarSizeMedium:
		changed = state.setTerminalToolbarSize(terminalToolbarMedium)
		layoutChanged = changed
	case id == hitToolbarSizeLarge:
		changed = state.setTerminalToolbarSize(terminalToolbarLarge)
		layoutChanged = changed
	case id == hitToolbarConnectionColor:
		changed = state.toggleTerminalStatusEntry("connection-color")
		layoutChanged = changed
	case id == hitToolbarLatency:
		changed = state.toggleTerminalStatusEntry("latency")
		layoutChanged = changed
	case id == hitToolbarReset:
		state.toolbars.layouts[editor.group] = nil
		editor.list.Cancel()
		for zone := range editor.zoneLists {
			editor.zoneLists[zone].Cancel()
		}
		editor.drag = terminalToolbarDrag{}
		if editor.group == toolbarTerminal {
			state.terminalToolbarSize = terminalToolbarSmall
			state.syncTerminalStatusEntries()
			state.applyTerminalToolbarLayoutChange()
		}
		changed = true
	case id == hitToolbarEnableAll || id == hitToolbarDisableAll:
		entries := state.toolbars.customize(editor.group)
		for i := range entries {
			enabled := id == hitToolbarEnableAll
			if entries[i].Enabled != enabled {
				entries[i].Enabled = enabled
				changed = true
			}
		}
		if editor.group == toolbarTerminal {
			state.syncTerminalStatusEntries()
		}
		layoutChanged = changed
	case id.Scope() == hitToolbarToggles:
		index := item(id)
		first := toolbarEntryIndex(editor.group, toolbarCatalog[editor.group][0].key)
		if index < first || index >= first+len(toolbarCatalog[editor.group]) {
			return
		}
		key := toolbarCatalog[editor.group][index-first].key
		entries := state.toolbars.customize(editor.group)
		for i := range entries {
			if entries[i].Key == key {
				entries[i].Enabled = !entries[i].Enabled
				if editor.group == toolbarTerminal {
					state.syncTerminalStatusEntries()
				}
				changed = true
				layoutChanged = true
				break
			}
		}
	default:
		return
	}
	if !changed {
		return
	}
	if layoutChanged && editor.group == toolbarTerminal {
		state.applyTerminalToolbarLayoutChange()
	}
	state.save()
}

func (state *State) moveToolbarEntry(move components.ReorderMove) {
	if !move.Valid() {
		return
	}
	editor := &state.toolbarEditor
	if editor.group == toolbarTerminal {
		key, ok := toolbarKeyForHandle(editor.group, move.ID)
		if !ok {
			return
		}
		zone := terminalToolbarZoneRightTop
		for _, entry := range state.toolbars.entries(toolbarTerminal) {
			if entry.Key == key {
				zone = terminalToolbarZoneForEntry(entry, terminalToolbarRight)
				break
			}
		}
		moveToolbarEntryToZone(state, key, zone, move.To)
		return
	}
	entries := state.toolbars.customize(editor.group)
	if move.From >= len(entries) || move.To >= len(entries) || hitToolbarHandles.At(toolbarEntryIndex(editor.group, entries[move.From].Key)) != move.ID {
		return
	}
	entry := entries[move.From]
	if move.From < move.To {
		copy(entries[move.From:move.To], entries[move.From+1:move.To+1])
	} else {
		copy(entries[move.To+1:move.From+1], entries[move.To:move.From])
	}
	entries[move.To] = entry
	// Keep the row the move landed on in view. The list knows the row geometry
	// and the viewport it was painted in, so the caller keeps no stride here.
	editor.scroll = max(editor.scroll+editor.list.RevealScroll(move.To), 0)
	state.save()
}

func toolbarKeyForHandle(group toolbarGroup, id ui.ID) (string, bool) {
	index := item(id)
	first := toolbarEntryIndex(group, toolbarCatalog[group][0].key)
	if index < first || index >= first+len(toolbarCatalog[group]) {
		return "", false
	}
	return toolbarCatalog[group][index-first].key, true
}

func (state *State) toggleTerminalStatusEntry(key string) bool {
	entries := state.toolbars.entries(toolbarTerminal)
	enabled, ok := toolbarEntryEnabled(entries, key)
	if !ok || !state.toolbars.setTerminalEntryEnabled(key, !enabled) {
		return false
	}
	state.syncTerminalStatusEntries()
	return true
}

// moveToolbarEntryToZone changes one item's quadrant and inserts it at slot in
// the target column's visible order. The persisted list is rebuilt in visible
// quadrant order so dragging matches the four-column editor exactly.
func moveToolbarEntryToZone(state *State, key string, targetZone terminalToolbarZone, slot int) bool {
	before, beforeKnown := state.terminalToolbarConfiguredOccupiedSides()
	entries := state.toolbars.customize(toolbarTerminal)
	source := toolbarEntry{}
	found := false
	var zones [4][]toolbarEntry
	for _, entry := range entries {
		if entry.Key == key {
			source, found = entry, true
			continue
		}
		zone := terminalToolbarZoneForEntry(entry, terminalToolbarRight)
		zones[zone] = append(zones[zone], entry)
	}
	if !found {
		return false
	}
	source.Side = targetZone.Side().String()
	source.Zone = targetZone.String()
	target := &zones[targetZone]
	slot = min(max(slot, 0), len(*target))
	*target = append(*target, toolbarEntry{})
	copy((*target)[slot+1:], (*target)[slot:])
	(*target)[slot] = source
	combined := make([]toolbarEntry, 0, len(entries))
	for zone := range zones {
		combined = append(combined, zones[zone]...)
	}
	if slices.Equal(entries, combined) {
		return false
	}
	state.toolbars.layouts[toolbarTerminal] = combined
	state.syncTerminalStatusEntries()
	after, afterKnown := state.terminalToolbarConfiguredOccupiedSides()
	// Reordering within the visible layout does not change the workspace
	// width. Preserve its rail scroll and terminal selection; only a rail
	// appearing or disappearing needs the full fit/reset path.
	if !beforeKnown || !afterKnown || before != after {
		state.applyTerminalToolbarLayoutChange()
	}
	state.save()
	return true
}

func moveToolbarEntryToSide(state *State, key string, targetSide terminalToolbarSide, slot int) bool {
	return moveToolbarEntryToZone(state, key, terminalToolbarZoneFromParts(targetSide, terminalToolbarDockTop), slot)
}

func (state *State) toolbarSettingsPointer(event ui.PointerEvent) ui.EventResult {
	if state.modal != modalToolbarSettings {
		return ui.EventResult{}
	}
	editor := &state.toolbarEditor
	if event.Target == hitToolbarScroll || editor.scrollDrag {
		if event.Kind == ui.PointerExit {
			editor.scrollDrag = false
			return ui.EventResult{Handled: true}
		}
		switch event.Kind {
		case ui.PointerDown:
			if event.Button != ui.PointerButtonPrimary {
				return ui.EventResult{}
			}
			editor.scrollDrag = true
			editor.thumbOffset = event.Y - editor.geometry.VerticalThumb.Y
			if editor.thumbOffset < 0 || editor.thumbOffset > editor.geometry.VerticalThumb.Height {
				editor.thumbOffset = editor.geometry.VerticalThumb.Height / 2
			}
		case ui.PointerUp:
			editor.scrollDrag = false
		}
		editor.scroll = editor.geometry.VerticalOffsetForThumb(event.Y - editor.thumbOffset)
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if editor.group == toolbarTerminal {
		return state.terminalToolbarSettingsPointer(event)
	}
	move, result := editor.list.Pointer(event)
	state.moveToolbarEntry(move)
	return result
}

func (state *State) terminalToolbarSettingsPointer(event ui.PointerEvent) ui.EventResult {
	editor := &state.toolbarEditor
	if editor.drag.active {
		list := &editor.zoneLists[editor.drag.zone]
		switch event.Kind {
		case ui.PointerMove:
			deltaX, deltaY := event.X-editor.drag.startX, event.Y-editor.drag.startY
			if deltaX >= 3 || deltaX <= -3 || deltaY >= 3 || deltaY <= -3 {
				editor.drag.dragging = true
			}
			state.updateTerminalToolbarDrop(event.X, event.Y)
			return ui.EventResult{Handled: true, Repaint: true, Focus: editor.drag.id}
		case ui.PointerUp:
			drag := editor.drag
			list.Cancel()
			editor.drag = terminalToolbarDrag{}
			if drag.dragging && drag.hasDrop {
				slot := editor.terminalToolbarDropSlot(drag)
				moveToolbarEntryToZone(state, drag.key, drag.dropZone, slot)
				editor.scroll = max(editor.scroll+editor.zoneLists[drag.dropZone].RevealScroll(slot), 0)
			}
			// Keep the handle keyboard-focusable after a mouse drop without
			// leaving the keyboard focus ring painted over it.
			return ui.EventResult{Handled: true, Repaint: true, Focus: drag.id}
		case ui.PointerExit:
			id := editor.drag.id
			list.Cancel()
			editor.drag = terminalToolbarDrag{}
			return ui.EventResult{Handled: true, Repaint: true, Focus: id}
		default:
			return ui.EventResult{Handled: true}
		}
	}
	if event.Kind != ui.PointerDown || event.Button != ui.PointerButtonPrimary {
		return ui.EventResult{}
	}
	for zone := range editor.zoneLists {
		list := &editor.zoneLists[zone]
		_, result := list.Pointer(event)
		if !result.Handled {
			continue
		}
		if key, ok := toolbarKeyForHandle(toolbarTerminal, event.Target); ok {
			editor.drag = terminalToolbarDrag{
				active: true,
				key:    key, id: event.Target, zone: terminalToolbarZone(zone),
				startX: event.X, startY: event.Y,
			}
		}
		return result
	}
	return ui.EventResult{}
}

func (editor *toolbarSettingsUI) terminalToolbarDropSlot(drag terminalToolbarDrag) int {
	if drag.zone != drag.dropZone {
		return drag.dropIndex
	}
	for index, key := range editor.zoneKeys[drag.dropZone] {
		if key == drag.key && index < drag.dropIndex {
			return drag.dropIndex - 1
		}
	}
	return drag.dropIndex
}

func (state *State) updateTerminalToolbarDrop(x, y float32) {
	editor := &state.toolbarEditor
	editor.drag.hasDrop = false
	for zone := range editor.zoneLists {
		viewport := editor.zoneViewport[zone]
		if !pointInRect(x, y, viewport) {
			continue
		}
		editor.drag.dropZone = terminalToolbarZone(zone)
		editor.drag.dropIndex = editor.terminalToolbarDropIndex(terminalToolbarZone(zone), y)
		editor.drag.hasDrop = true
		return
	}
}

func (editor *toolbarSettingsUI) terminalToolbarDropIndex(zone terminalToolbarZone, y float32) int {
	keys := editor.zoneKeys[zone]
	if len(keys) == 0 {
		return 0
	}
	list := &editor.zoneLists[zone]
	for index, key := range keys {
		handle := hitToolbarHandles.At(toolbarEntryIndex(toolbarTerminal, key))
		bounds, ok := list.RowBounds(handle)
		if ok && y < bounds.Y+bounds.Height/2 {
			return index
		}
	}
	// A pointer below the last row inserts after it. This includes the row's
	// midpoint, which makes dropping anywhere in the lower half traverse the
	// complete list ending at "append".
	return len(keys)
}

func (state *State) terminalToolbarSettingsKey(event ui.KeyEvent) ui.EventResult {
	editor := &state.toolbarEditor
	if key, ok := toolbarKeyForHandle(toolbarTerminal, event.Target); ok && !event.Release && event.Modifiers == 0 {
		zone := terminalToolbarZoneRightTop
		for _, entry := range state.toolbars.entries(toolbarTerminal) {
			if entry.Key == key {
				zone = terminalToolbarZoneForEntry(entry, terminalToolbarRight)
				break
			}
		}
		switch event.Key {
		case ui.KeyLeft, ui.KeyRight:
			side := zone.Side()
			if event.Key == ui.KeyLeft {
				side = terminalToolbarLeft
			} else {
				side = terminalToolbarRight
			}
			target := terminalToolbarZoneFromParts(side, zone.Dock())
			if target != zone {
				slot := len(editor.zoneKeys[target])
				if moveToolbarEntryToZone(state, key, target, slot) {
					return ui.EventResult{Handled: true, Repaint: true, Focus: event.Target, FocusVisible: true}
				}
			}
		case ui.KeyPageUp, ui.KeyPageDown:
			dock := terminalToolbarDockTop
			if event.Key == ui.KeyPageDown {
				dock = terminalToolbarDockBottom
			}
			target := terminalToolbarZoneFromParts(zone.Side(), dock)
			if target != zone {
				slot := len(editor.zoneKeys[target])
				if moveToolbarEntryToZone(state, key, target, slot) {
					return ui.EventResult{Handled: true, Repaint: true, Focus: event.Target, FocusVisible: true}
				}
			}
		case ui.KeyUp, ui.KeyDown, ui.KeyHome, ui.KeyEnd:
			list := &editor.zoneLists[zone]
			move, result := list.Key(event)
			if result.Handled {
				if move.Valid() {
					moveToolbarEntryToZone(state, key, zone, move.To)
				}
				return result
			}
		}
	}
	if !event.Release && event.Modifiers == 0 {
		switch event.Target {
		case hitToolbarSizeSmall, hitToolbarSizeMedium, hitToolbarSizeLarge:
			options := state.settingsToolbarSizeOptions()
			next, handled := components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
			if handled {
				state.clickToolbarSettings(next)
				return ui.EventResult{Focus: next, FocusVisible: true, Handled: true, Repaint: true}
			}
		}
	}
	if event.Key == ui.KeyEscape && !event.Release && !event.Repeat {
		state.closeModal()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	return ui.EventResult{}
}

func (state *State) toolbarSettingsKey(event ui.KeyEvent) ui.EventResult {
	if state.modal != modalToolbarSettings {
		return ui.EventResult{}
	}
	if state.toolbarEditor.group == toolbarTerminal {
		return state.terminalToolbarSettingsKey(event)
	}
	move, result := state.toolbarEditor.list.Key(event)
	if result.Handled {
		state.moveToolbarEntry(move)
		return result
	}
	var options []components.SegmentOption
	if !event.Release && event.Modifiers == 0 {
		switch event.Target {
		case hitToolbarSizeSmall, hitToolbarSizeMedium, hitToolbarSizeLarge:
			size := state.settingsToolbarSizeOptions()
			options = size[:]
		}
	}
	if len(options) > 0 {
		next, handled := components.SegmentedControlKeyDown(event.Target, options, components.DirectionLTR, event.Key)
		if handled {
			state.clickToolbarSettings(next)
			return ui.EventResult{Focus: next, FocusVisible: true, Handled: true, Repaint: true}
		}
	}
	if event.Key == ui.KeyEscape && !event.Release && !event.Repeat {
		state.closeModal()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	return ui.EventResult{}
}

func (state *State) toolbarSettingsScroll(event ui.ScrollEvent) ui.EventResult {
	editor := &state.toolbarEditor
	if !pointInRect(event.X, event.Y, editor.geometry.Viewport) {
		return ui.EventResult{Handled: true}
	}
	before := editor.scroll
	editor.scroll = min(max(before+event.DeltaY, 0), editor.geometry.MaxOffsetY)
	return ui.EventResult{Handled: true, Repaint: before != editor.scroll}
}

func (state *State) paintToolbarSettings(canvas *ui.Canvas, viewport ui.Rect) {
	editor, m, theme := &state.toolbarEditor, state.messages.Toolbars, state.Theme()
	entries := state.toolbars.entries(editor.group)
	const rowHeight float32 = 48
	headerHeight := float32(0)
	columnHeaderHeight := float32(0)
	if editor.group == toolbarTerminal {
		headerHeight = 48
		columnHeaderHeight = 34
	}
	rowCount := len(entries)
	if editor.group == toolbarTerminal {
		counts := [4]int{}
		for _, entry := range entries {
			counts[terminalToolbarZoneForEntry(entry, terminalToolbarRight)]++
		}
		rowCount = max(counts[0], counts[1], counts[2], counts[3])
	}
	listHeight := float32(max(rowCount, 1))*(rowHeight+4) - 4
	contentHeight := headerHeight + columnHeaderHeight + listHeight
	bodyHeight := min(contentHeight, max(viewport.Height-200, headerHeight+columnHeaderHeight+64))
	contentWidth := float32(680)
	if editor.group == toolbarTerminal {
		// Four independent drop zones need more room on a normal desktop, but
		// the dialog still has to remain usable in a narrow window.
		contentWidth = min(1120, max(viewport.Width-48, 360))
	}
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitToolbarSettingsDismiss, CloseID: hitToolbarSettingsClose,
		ContentSize: ui.Size{Width: contentWidth}, FitBody: true, BodyHeight: bodyHeight,
	})
	hint := m.Hint
	if editor.group == toolbarTerminal {
		hint = m.TerminalHint
	}
	layout := dialog.Begin(state.toolbarGroupName(editor.group), hint)
	listBounds := layout.Body
	if editor.group == toolbarTerminal {
		state.paintTerminalToolbarControls(canvas, ui.Rect{
			X: layout.Body.X, Y: layout.Body.Y, Width: layout.Body.Width, Height: headerHeight,
		})
		listBounds.Y += headerHeight
		listBounds.Height = max(listBounds.Height-headerHeight, 0)
		state.paintTerminalToolbarColumnHeaders(canvas, ui.Rect{
			X: listBounds.X, Y: listBounds.Y, Width: listBounds.Width, Height: columnHeaderHeight,
		})
		listBounds.Y += columnHeaderHeight
		listBounds.Height = max(listBounds.Height-columnHeaderHeight, 0)
	}
	area := components.BeginScrollArea(canvas, theme, listBounds, ui.Size{Width: listBounds.Width, Height: listHeight}).Offset(0, editor.scroll).VerticalScrollbar(hitToolbarScroll, "", "")
	geometry := area.BeginContent()
	editor.geometry, editor.scroll = geometry, geometry.OffsetY
	bounds := geometry.Content
	if geometry.MaxOffsetY > 0 {
		bounds.Width = max(bounds.Width-14, 0)
	}
	if editor.group == toolbarTerminal {
		state.paintTerminalToolbarColumns(canvas, theme, geometry, bounds, rowHeight)
		area.EndContent()
		dialog.Actions(layout,
			components.ButtonAction{ID: hitToolbarEnableAll, Label: m.EnableAll, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitToolbarDisableAll, Label: m.DisableAll, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitToolbarReset, Label: m.Reset, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitToolbarSettingsClose, Label: m.Close, Variant: components.ButtonSecondary},
		)
		dialog.End()
		return
	}
	list := components.BeginReorderList(canvas, theme, bounds, &editor.list).Viewport(geometry.Viewport).RowHeight(rowHeight)
	first := toolbarEntryIndex(editor.group, toolbarCatalog[editor.group][0].key)
	for _, entry := range entries {
		spec, _ := toolbarSpecFor(editor.group, entry.Key)
		label := state.toolbarLabel(spec.id)
		index := toolbarEntryIndex(editor.group, entry.Key)
		row := list.Row(hitToolbarHandles.At(index), editor.handleLabels[index-first], false)
		color := theme.Foreground
		if !entry.Enabled {
			color = theme.MutedForeground
		}
		canvas.DrawIcon(lucideIcons[spec.icon], ui.Rect{X: row.Content.X, Y: row.Content.Y + (row.Content.Height-18)/2, Width: 18, Height: 18}, color)
		switchX := row.Content.X + row.Content.Width - 42
		captionWidth := max(switchX-row.Content.X-36, 0)
		caption := ui.Rect{X: row.Content.X + 28, Y: row.Content.Y, Width: captionWidth, Height: row.Content.Height}
		canvas.Label(canvas.FitLine(label, theme.Metrics.FontSizeM, ui.WeightRegular, caption.Width), caption, ui.TextStyle{Size: theme.Metrics.FontSizeM, Color: color})
		components.BeginSwitch(canvas, state.switchTheme(), hitToolbarToggles.At(index), ui.Rect{X: switchX, Y: row.Content.Y + (row.Content.Height-22)/2, Width: 38, Height: 22}, entry.Enabled).Label(label).Paint()
	}
	list.End()
	area.EndContent()
	dialog.Actions(layout,
		components.ButtonAction{ID: hitToolbarEnableAll, Label: m.EnableAll, Variant: components.ButtonOutline},
		components.ButtonAction{ID: hitToolbarDisableAll, Label: m.DisableAll, Variant: components.ButtonOutline},
		components.ButtonAction{ID: hitToolbarReset, Label: m.Reset, Variant: components.ButtonOutline},
		components.ButtonAction{ID: hitToolbarSettingsClose, Label: m.Close, Variant: components.ButtonSecondary},
	)
	dialog.End()
}

func (state *State) paintTerminalToolbarColumnHeaders(canvas *ui.Canvas, bounds ui.Rect) {
	theme, m := state.Theme(), state.messages.Toolbars
	gap := float32(10)
	width := max((bounds.Width-gap*3)/4, 0)
	style := ui.TextStyle{Size: theme.Metrics.FontSizeS, Weight: ui.WeightSemibold, Color: theme.MutedForeground}
	for index, label := range [4]string{m.LeftTop, m.LeftBottom, m.RightTop, m.RightBottom} {
		x := bounds.X + float32(index)*(width+gap)
		canvas.Label(canvas.FitLine(label, theme.Metrics.FontSizeS, ui.WeightSemibold, width), ui.Rect{
			X: x + 4, Y: bounds.Y, Width: width - 8, Height: bounds.Height,
		}, style)
	}
}

func (state *State) paintTerminalToolbarColumns(canvas *ui.Canvas, theme ui.Theme, geometry components.ScrollAreaGeometry, bounds ui.Rect, rowHeight float32) {
	editor := &state.toolbarEditor
	entries := state.toolbars.entries(toolbarTerminal)
	gap := float32(10)
	columnWidth := max((bounds.Width-gap*3)/4, 0)
	first := toolbarEntryIndex(toolbarTerminal, toolbarCatalog[toolbarTerminal][0].key)
	for zone := range editor.zoneLists {
		column := ui.Rect{
			X: bounds.X + float32(zone)*(columnWidth+gap), Y: bounds.Y,
			Width: columnWidth, Height: bounds.Height,
		}
		editor.zoneContent[zone] = column
		editor.zoneViewport[zone] = ui.Rect{
			X: column.X, Y: geometry.Viewport.Y, Width: column.Width, Height: geometry.Viewport.Height,
		}
		editor.zoneKeys[zone] = editor.zoneKeys[zone][:0]
		list := components.BeginReorderList(canvas, theme, column, &editor.zoneLists[zone]).
			Viewport(editor.zoneViewport[zone]).RowHeight(rowHeight)
		for _, entry := range entries {
			if terminalToolbarZoneForEntry(entry, terminalToolbarRight) != terminalToolbarZone(zone) {
				continue
			}
			editor.zoneKeys[zone] = append(editor.zoneKeys[zone], entry.Key)
			state.paintTerminalToolbarRow(canvas, theme, &list, entry, first)
		}
		list.End()
	}

	if editor.drag.dragging && editor.drag.hasDrop {
		drawZone := editor.drag.dropZone
		drawBounds := editor.zoneContent[drawZone]
		keys := editor.zoneKeys[drawZone]
		dropIndex := editor.terminalToolbarDropSlot(editor.drag)
		y := drawBounds.Y - 2
		if dropIndex >= len(keys) {
			y = drawBounds.Y + float32(len(keys))*(rowHeight+4) - 2
		} else if len(keys) > 0 {
			slot := max(dropIndex, 0)
			y = drawBounds.Y + float32(slot)*(rowHeight+4)
		}
		canvas.Clip(state.dropViewport(drawZone))
		canvas.Rounded(ui.Rect{X: drawBounds.X + 2, Y: y, Width: max(drawBounds.Width-4, 0), Height: 3}, 1.5, theme.Primary)
		canvas.Unclip()
	}
	for zone := range editor.zoneLists {
		state.paintTerminalToolbarEmptyColumn(canvas, theme, editor.zoneViewport[zone], len(editor.zoneKeys[zone]) == 0)
	}
}

func (state *State) paintTerminalToolbarRow(canvas *ui.Canvas, theme ui.Theme, list *components.ReorderList, entry toolbarEntry, first int) {
	spec, _ := toolbarSpecFor(toolbarTerminal, entry.Key)
	label := state.toolbarLabel(spec.id)
	index := toolbarEntryIndex(toolbarTerminal, entry.Key)
	row := list.Row(hitToolbarHandles.At(index), state.toolbarEditor.handleLabels[index-first], false)
	color := theme.Foreground
	if !entry.Enabled {
		color = theme.MutedForeground
	}
	canvas.DrawIcon(lucideIcons[spec.icon], ui.Rect{X: row.Content.X, Y: row.Content.Y + (row.Content.Height-18)/2, Width: 18, Height: 18}, color)
	switchX := row.Content.X + row.Content.Width - 42
	caption := ui.Rect{X: row.Content.X + 28, Y: row.Content.Y, Width: max(switchX-row.Content.X-36, 0), Height: row.Content.Height}
	canvas.Label(canvas.FitLine(label, theme.Metrics.FontSizeM, ui.WeightRegular, caption.Width), caption, ui.TextStyle{Size: theme.Metrics.FontSizeM, Color: color})
	switchID := hitToolbarToggles.At(index)
	switch entry.Key {
	case "connection-color":
		switchID = hitToolbarConnectionColor
	case "latency":
		switchID = hitToolbarLatency
	}
	components.BeginSwitch(canvas, state.switchTheme(), switchID, ui.Rect{
		X: switchX, Y: row.Content.Y + (row.Content.Height-22)/2, Width: 38, Height: 22,
	}, entry.Enabled).Label(label).Paint()
}

func (state *State) paintTerminalToolbarEmptyColumn(canvas *ui.Canvas, theme ui.Theme, viewport ui.Rect, empty bool) {
	if !empty || viewport.Width <= 0 || viewport.Height <= 0 {
		return
	}
	hint := state.messages.Toolbars.EmptyColumn
	canvas.LabelCenter(canvas.FitLine(hint, theme.Metrics.FontSizeS, ui.WeightRegular, max(viewport.Width-16, 0)), ui.Rect{
		X: viewport.X, Y: viewport.Y, Width: viewport.Width, Height: min(viewport.Height, 64),
	}, ui.TextStyle{Size: theme.Metrics.FontSizeS, Color: theme.MutedForeground})
}

func (state *State) dropViewport(zone terminalToolbarZone) ui.Rect {
	return state.toolbarEditor.zoneViewport[zone]
}

func (state *State) paintTerminalToolbarControls(canvas *ui.Canvas, bounds ui.Rect) {
	theme, m := state.Theme(), state.messages.Toolbars
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	labelStyle := ui.TextStyle{Size: theme.Metrics.FontSizeM, Color: theme.MutedForeground}

	sizeLabel := ui.Rect{X: bounds.X, Y: bounds.Y, Width: 92, Height: 32}
	canvas.Label(canvas.FitLine(m.Size, theme.Metrics.FontSizeM, ui.WeightRegular, sizeLabel.Width), sizeLabel, labelStyle)
	sizeOptions := state.settingsToolbarSizeOptions()
	selectedSize := hitToolbarSizeSmall
	switch state.terminalToolbarSize {
	case terminalToolbarMedium:
		selectedSize = hitToolbarSizeMedium
	case terminalToolbarLarge:
		selectedSize = hitToolbarSizeLarge
	}
	sizeWidth := min(float32(246), max(bounds.Width-112, 0))
	components.BeginSegmentedControl(canvas, theme, ui.Rect{
		X: bounds.X + 100, Y: bounds.Y, Width: sizeWidth, Height: 32,
	}, sizeOptions[:], selectedSize).Paint()

	canvas.Fill(ui.Rect{
		X: bounds.X, Y: bounds.Y + bounds.Height - 1, Width: bounds.Width, Height: 1,
	}, theme.Border)
}
