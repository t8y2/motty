package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

var toolbarSettingsHits = tree.Feature("toolbar-settings")
var (
	hitToolbarSettingsOpen    = [3]gfx.ID{tree.ID(toolbarSettingsHits, "window"), tree.ID(toolbarSettingsHits, "terminal"), tree.ID(toolbarSettingsHits, "sftp")}
	hitToolbarSettingsClose   = tree.ID(toolbarSettingsHits, "close")
	hitToolbarSettingsDismiss = tree.ID(toolbarSettingsHits, "dismiss")
	hitToolbarEnableAll       = tree.ID(toolbarSettingsHits, "enable-all")
	hitToolbarDisableAll      = tree.ID(toolbarSettingsHits, "disable-all")
	hitToolbarReset           = tree.ID(toolbarSettingsHits, "reset")
	hitToolbarScroll          = tree.ID(toolbarSettingsHits, "scroll")
	hitToolbarHandles         = tree.Sub(toolbarSettingsHits, tree.Name("handle"))
	hitToolbarToggles         = tree.Sub(toolbarSettingsHits, tree.Name("toggle"))
	hitToolbarPositions       = tree.Sub(toolbarSettingsHits, tree.Name("position"))
	hitToolbarSizeSmall       = tree.ID(toolbarSettingsHits, "size-small")
	hitToolbarSizeMedium      = tree.ID(toolbarSettingsHits, "size-medium")
	hitToolbarSizeLarge       = tree.ID(toolbarSettingsHits, "size-large")
)

type toolbarSettingsUI struct {
	group        toolbarGroup
	list         components.ReorderListState
	scroll       float32
	geometry     components.ScrollAreaGeometry
	scrollDrag   bool
	thumbOffset  float32
	handleLabels []string
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
			spec, ok := toolbarSpecFor(group, entry.Key)
			if ok && state.toolbarVisible(group, spec.id) {
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
	state.toolbarEditor = toolbarSettingsUI{group: group, handleLabels: make([]string, len(toolbarCatalog[group]))}
	for i, spec := range toolbarCatalog[group] {
		state.toolbarEditor.handleLabels[i] = fmt.Sprintf(state.messages.Toolbars.DragFmt, state.toolbarLabel(spec.id))
	}
	state.modal = modalToolbarSettings
}

func (state *State) clickToolbarSettings(id gfx.ID) {
	editor := &state.toolbarEditor
	switch {
	case id == hitToolbarSettingsClose || id == hitToolbarSettingsDismiss:
		state.closeModal()
		return
	case id == hitToolbarReset:
		state.toolbars.layouts[editor.group] = nil
		state.resetToolbarDockGroup(editor.group)
		if editor.group == toolbarTerminal {
			state.terminalToolbarSize = terminalToolbarSmall
		}
		editor.list.Cancel()
	case editor.group == toolbarTerminal && (id == hitToolbarSizeSmall || id == hitToolbarSizeMedium || id == hitToolbarSizeLarge):
		size := terminalToolbarSmall
		if id == hitToolbarSizeMedium {
			size = terminalToolbarMedium
		} else if id == hitToolbarSizeLarge {
			size = terminalToolbarLarge
		}
		state.setTerminalToolbarSize(size)
		return
	case id.Scope() == hitToolbarPositions && editor.group <= toolbarTerminal:
		index := item(id)
		for _, spec := range toolbarCatalog[editor.group] {
			if toolbarEntryIndex(editor.group, spec.key) != index {
				continue
			}
			zone := state.toolbarDockZoneFor(editor.group, spec.key)
			next := toolbarDockSideTop
			if zone == toolbarDockSideTop {
				next = toolbarDockSideBottom
			} else if zone == toolbarDockSideBottom {
				next = toolbarDockBottom
			} else if zone == toolbarDockBottom {
				next = toolbarDockBottomRight
			} else if zone == toolbarDockBottomRight {
				next = toolbarDockLeftTop
			} else if zone == toolbarDockLeftTop {
				next = toolbarDockLeftBottom
			} else if zone == toolbarDockLeftBottom {
				next = toolbarDockTop
			}
			state.moveToolbarDockTool(spec.id, next, gfx.ID{})
			return
		}
	case id == hitToolbarEnableAll || id == hitToolbarDisableAll:
		entries := state.toolbars.customize(editor.group)
		for i := range entries {
			entries[i].Enabled = id == hitToolbarEnableAll
		}
		// The latency row's switch is the Show latency preference itself.
		if editor.group == toolbarTerminal {
			state.terminalShowLatency = id == hitToolbarEnableAll
		}
	case id.Scope() == hitToolbarToggles:
		index := item(id)
		first := toolbarEntryIndex(editor.group, toolbarCatalog[editor.group][0].key)
		if index < first || index >= first+len(toolbarCatalog[editor.group]) {
			return
		}
		key := toolbarCatalog[editor.group][index-first].key
		entries := state.toolbars.customize(editor.group)
		if editor.group == toolbarTerminal && key == "latency" {
			// The switch shows the Show latency preference, so it toggles that
			// preference rather than a possibly stale mirror in the layout.
			state.terminalShowLatency = !state.terminalShowLatency
			for i := range entries {
				if entries[i].Key == key {
					entries[i].Enabled = state.terminalShowLatency
					break
				}
			}
			break
		}
		for i := range entries {
			if entries[i].Key == key {
				entries[i].Enabled = !entries[i].Enabled
				break
			}
		}
	default:
		return
	}
	state.toolbarDockChanged()
}

func (state *State) moveToolbarEntry(move components.ReorderMove) {
	if !move.Valid() {
		return
	}
	editor := &state.toolbarEditor
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
	state.reorderToolbarDockGroup(editor.group)
	// Keep the row the move landed on in view. The list knows the row geometry
	// and the viewport it was painted in, so the caller keeps no stride here.
	editor.scroll = max(editor.scroll+editor.list.RevealScroll(move.To), 0)
	state.toolbarDockChanged()
}

func (state *State) toolbarSettingsPointer(event gfx.PointerEvent) gfx.EventResult {
	if state.modal != modalToolbarSettings {
		return gfx.EventResult{}
	}
	editor := &state.toolbarEditor
	if event.Target == hitToolbarScroll || editor.scrollDrag {
		if event.Kind == gfx.PointerExit {
			editor.scrollDrag = false
			return gfx.EventResult{Handled: true}
		}
		switch event.Kind {
		case gfx.PointerDown:
			if event.Button != gfx.PointerButtonPrimary {
				return gfx.EventResult{}
			}
			editor.scrollDrag = true
			editor.thumbOffset = event.Y - editor.geometry.VerticalThumb.Y
			if editor.thumbOffset < 0 || editor.thumbOffset > editor.geometry.VerticalThumb.Height {
				editor.thumbOffset = editor.geometry.VerticalThumb.Height / 2
			}
		case gfx.PointerUp:
			editor.scrollDrag = false
		}
		editor.scroll = editor.geometry.VerticalOffsetForThumb(event.Y - editor.thumbOffset)
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	move, result := editor.list.Pointer(event)
	state.moveToolbarEntry(move)
	return result
}

func (state *State) toolbarSettingsKey(event gfx.KeyEvent) gfx.EventResult {
	if state.modal != modalToolbarSettings {
		return gfx.EventResult{}
	}
	if state.toolbarEditor.group == toolbarTerminal && !event.Release &&
		(event.Target == hitToolbarSizeSmall || event.Target == hitToolbarSizeMedium || event.Target == hitToolbarSizeLarge) {
		options := state.settingsToolbarSizeOptions()
		if next, handled := components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key); handled {
			state.clickToolbarSettings(next)
			return gfx.EventResult{Handled: true, Repaint: true, Focus: next, FocusVisible: true}
		}
	}
	move, result := state.toolbarEditor.list.Key(event)
	if result.Handled {
		state.moveToolbarEntry(move)
		return result
	}
	if event.Key == gfx.KeyEscape && !event.Release && !event.Repeat {
		state.closeModal()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

func (state *State) toolbarSettingsScroll(event gfx.ScrollEvent) gfx.EventResult {
	editor := &state.toolbarEditor
	if !pointInRect(event.X, event.Y, editor.geometry.Viewport) {
		return gfx.EventResult{Handled: true}
	}
	before := editor.scroll
	editor.scroll = min(max(before+event.DeltaY, 0), editor.geometry.MaxOffsetY)
	return gfx.EventResult{Handled: true, Repaint: before != editor.scroll}
}

func (state *State) paintToolbarSettings(canvas *gfx.Canvas, viewport gfx.Rect) {
	editor, m, theme := &state.toolbarEditor, state.messages.Toolbars, state.Theme()
	entries := state.toolbars.entries(editor.group)
	height := float32(len(entries))*48 - 4
	controlsHeight := float32(0)
	if editor.group == toolbarTerminal {
		controlsHeight = 48
	}
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitToolbarSettingsDismiss, CloseID: hitToolbarSettingsClose,
		ContentSize: gfx.Size{Width: 560}, FitBody: true, BodyHeight: min(height+controlsHeight, max(viewport.Height-250, 96)),
	})
	layout := dialog.Begin(state.toolbarGroupName(editor.group), m.Hint)
	body := layout.Body
	if editor.group == toolbarTerminal {
		label := gfx.Rect{X: body.X, Y: body.Y, Width: min(200, body.Width/2), Height: 32}
		canvas.Label(canvas.FitLine(state.messages.SettingsToolbarSize, theme.Metrics.FontSizeM, gfx.WeightRegular, label.Width), label, gfx.TextStyle{Size: theme.Metrics.FontSizeM, Color: theme.MutedForeground})
		options := state.settingsToolbarSizeOptions()
		selected := options[state.terminalToolbarSize].ID
		components.BeginSegmentedControl(canvas, theme, gfx.Rect{
			X: label.X + label.Width + 12, Y: body.Y, Width: max(body.Width-label.Width-12, 0), Height: 32,
		}, options[:], selected).Paint()
		body.Y += controlsHeight
		body.Height = max(body.Height-controlsHeight, 0)
	}
	area := components.BeginScrollArea(canvas, theme, body, gfx.Size{Width: body.Width, Height: height}).Offset(0, editor.scroll).VerticalScrollbar(hitToolbarScroll, "", "")
	geometry := area.BeginContent()
	editor.geometry, editor.scroll = geometry, geometry.OffsetY
	bounds := geometry.Content
	if geometry.MaxOffsetY > 0 {
		bounds.Width = max(bounds.Width-14, 0)
	}
	list := components.BeginReorderList(canvas, theme, bounds, &editor.list).Viewport(geometry.Viewport)
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
		canvas.DrawIcon(lucideIcons[spec.icon], gfx.Rect{X: row.Content.X, Y: row.Content.Y + (row.Content.Height-18)/2, Width: 18, Height: 18}, color)
		caption := gfx.Rect{X: row.Content.X + 28, Y: row.Content.Y, Width: max(row.Content.Width-80, 0), Height: row.Content.Height}
		zoneLabel, zoneWidth := "", float32(0)
		if editor.group <= toolbarTerminal {
			zoneLabel = state.toolbarDockZoneLabel(state.toolbarDockZoneFor(editor.group, entry.Key))
			zoneWidth = components.BeginButton(canvas, theme, gfx.ID{}, gfx.Rect{}).SizeToFit(zoneLabel).BoundsWidth()
			caption.Width = max(caption.Width-zoneWidth-2, 0)
		}
		canvas.Label(canvas.FitLine(label, theme.Metrics.FontSizeM, gfx.WeightRegular, caption.Width), caption, gfx.TextStyle{Size: theme.Metrics.FontSizeM, Color: color})
		if editor.group <= toolbarTerminal {
			components.BeginButton(canvas, theme, hitToolbarPositions.At(index), gfx.Rect{X: row.Content.X + row.Content.Width - 52 - zoneWidth, Y: row.Content.Y + (row.Content.Height-28)/2, Width: zoneWidth}).Outline(zoneLabel)
		}
		checked := entry.Enabled
		if editor.group == toolbarTerminal && entry.Key == "latency" {
			// This switch is the same preference as Settings > Show latency.
			checked = state.terminalShowLatency
		}
		components.BeginSwitch(canvas, state.switchTheme(), hitToolbarToggles.At(index), gfx.Rect{X: row.Content.X + row.Content.Width - 42, Y: row.Content.Y + (row.Content.Height-22)/2, Width: 38, Height: 22}, checked).Label(label).Paint()
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
