package workbench

import (
	"fmt"

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
	hitToolbarHandles         = tree.Sub(toolbarSettingsHits, tree.Name("handle"))
	hitToolbarToggles         = tree.Sub(toolbarSettingsHits, tree.Name("toggle"))
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
	state.toolbarEditor = toolbarSettingsUI{group: group, handleLabels: make([]string, len(toolbarCatalog[group]))}
	for i, spec := range toolbarCatalog[group] {
		state.toolbarEditor.handleLabels[i] = fmt.Sprintf(state.messages.Toolbars.DragFmt, state.toolbarLabel(spec.id))
	}
	state.modal = modalToolbarSettings
}

func (state *State) clickToolbarSettings(id ui.ID) {
	editor := &state.toolbarEditor
	switch {
	case id == hitToolbarSettingsClose || id == hitToolbarSettingsDismiss:
		state.closeModal()
		return
	case id == hitToolbarReset:
		state.toolbars.layouts[editor.group] = nil
		editor.list.Cancel()
	case id == hitToolbarEnableAll || id == hitToolbarDisableAll:
		entries := state.toolbars.customize(editor.group)
		for i := range entries {
			entries[i].Enabled = id == hitToolbarEnableAll
		}
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
				break
			}
		}
	default:
		return
	}
	state.save()
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
	top, bottom := float32(move.To)*48, float32(move.To)*48+44
	if top < editor.scroll {
		editor.scroll = top
	} else if bottom > editor.scroll+editor.geometry.Viewport.Height {
		editor.scroll = bottom - editor.geometry.Viewport.Height
	}
	state.save()
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
	move, result := editor.list.Pointer(event)
	state.moveToolbarEntry(move)
	return result
}

func (state *State) toolbarSettingsKey(event ui.KeyEvent) ui.EventResult {
	if state.modal != modalToolbarSettings {
		return ui.EventResult{}
	}
	move, result := state.toolbarEditor.list.Key(event)
	if result.Handled {
		state.moveToolbarEntry(move)
		return result
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
	height := float32(len(entries))*48 - 4
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitToolbarSettingsDismiss, CloseID: hitToolbarSettingsClose,
		ContentSize: ui.Size{Width: 560}, FitBody: true, BodyHeight: min(height, max(viewport.Height-250, 96)),
	})
	layout := dialog.Begin(state.toolbarGroupName(editor.group), m.Hint)
	area := components.BeginScrollArea(canvas, theme, layout.Body, ui.Size{Width: layout.Body.Width, Height: height}).Offset(0, editor.scroll).VerticalScrollbar(hitToolbarScroll, "", "")
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
		canvas.DrawIcon(lucideIcons[spec.icon], ui.Rect{X: row.Content.X, Y: row.Content.Y + (row.Content.Height-18)/2, Width: 18, Height: 18}, color)
		caption := ui.Rect{X: row.Content.X + 28, Y: row.Content.Y, Width: max(row.Content.Width-80, 0), Height: row.Content.Height}
		canvas.Label(canvas.FitLine(label, theme.Metrics.FontSizeM, ui.WeightRegular, caption.Width), caption, ui.TextStyle{Size: theme.Metrics.FontSizeM, Color: color})
		components.BeginSwitch(canvas, state.switchTheme(), hitToolbarToggles.At(index), ui.Rect{X: row.Content.X + row.Content.Width - 42, Y: row.Content.Y + (row.Content.Height-22)/2, Width: 38, Height: 22}, entry.Enabled).Label(label).Paint()
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
