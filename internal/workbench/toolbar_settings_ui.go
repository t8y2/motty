package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var toolbarSettingsHits = tree.Feature("toolbar-settings")
var (
	hitToolbarSettingsOpen    = [3]gfx.ID{tree.ID(toolbarSettingsHits, "window"), tree.ID(toolbarSettingsHits, "terminal"), tree.ID(toolbarSettingsHits, "sftp")}
	hitToolbarSettingsClose   = tree.ID(toolbarSettingsHits, "close")
	hitToolbarSettingsDismiss = tree.ID(toolbarSettingsHits, "dismiss")
	hitToolbarEnableAll       = tree.ID(toolbarSettingsHits, "enable-all")
	hitToolbarDisableAll      = tree.ID(toolbarSettingsHits, "disable-all")
	hitToolbarReset           = tree.ID(toolbarSettingsHits, "reset")
	hitToolbarToggles         = tree.Sub(toolbarSettingsHits, tree.Name("toggle"))
	hitToolbarPositions       = tree.Sub(toolbarSettingsHits, tree.Name("position"))
	hitToolbarHandles         = tree.Sub(toolbarSettingsHits, tree.Name("handle"))
)

type toolbarSettingsUI struct {
	group toolbarGroup
	// rows is the entries' list: its rows are dragged, or moved with Alt and the
	// arrows, to reorder them.
	rows ui.ListState
	// body and footer declare the dialog's parts, and current is the C they
	// declare with. They are made once, when the editor opens, so a paint does
	// not allocate them again; current is only read while the page declares.
	body, footer   func()
	current        ui.C
	rowDecl        func(ui.ListRow)
	bodyWidth      float32
	controlsHeight float32
	bodyHeight     float32
	list           ui.Box
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

// reorderHandle draws the six-dot grip a reorder row starts with: a 28px
// button at the row's left, as the painted ReorderList's handle was.
func reorderHandle(c ui.C, id gfx.ID, label string, disabled bool, height float32) ui.Box {
	color := c.Theme().MutedForeground
	if disabled {
		color.Alpha *= .5
	}
	// The grip is drawn from elements rather than a Draw callback: a callback
	// made for each row in each paint allocates.
	handle := ui.Slot(c).Size(28, max(height-8, 0)).Shrink(0).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand, Disabled: disabled}).
		Focus(ui.Look{}.Ring(1, c.Theme().Ring))
	handle.Children(func() {
		ui.Column(c).Fill().Gap(2).Items(ui.Center).Justify(ui.Center).Children(func() {
			for range 3 {
				ui.Row(c).Gap(2).Children(func() {
					ui.Slot(c).Size(2, 2).Radius(1).Background(color)
					ui.Slot(c).Size(2, 2).Radius(1).Background(color)
				})
			}
		})
	})
	return handle
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
	editor := &state.toolbarEditor
	*editor = toolbarSettingsUI{group: group}
	editor.body = func() { state.declareToolbarSettingsBody(editor.current) }
	editor.footer = func() { state.declareToolbarSettingsFooter(editor.current) }
	editor.rowDecl = state.declareToolbarEntry
	editor.rows.Key = func(i int) int { return toolbarEntryIndex(group, state.toolbars.entries(group)[i].Key) }
	editor.rows.Label = func(i int) string {
		spec, _ := toolbarSpecFor(group, state.toolbars.entries(group)[i].Key)
		return state.toolbarLabel(spec.id)
	}
	editor.rows.NoTypeahead = true
	editor.rows.Reorder = func(rows []int, to int) { state.moveToolbarEntry(rows[0], to) }
	editor.rows.Handle = hitToolbarHandles
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

// moveToolbarEntry puts the entry at row from before row to (the number of entries for
// the end), as ListState.Reorder says.
func (state *State) moveToolbarEntry(from, to int) {
	editor := &state.toolbarEditor
	entries := state.toolbars.customize(editor.group)
	if from < 0 || from >= len(entries) || to < 0 || to > len(entries) {
		return
	}
	entry := entries[from]
	if from < to {
		to--
		copy(entries[from:to], entries[from+1:to+1])
	} else {
		copy(entries[to+1:from+1], entries[to:from])
	}
	entries[to] = entry
	state.reorderToolbarDockGroup(editor.group)
	state.toolbarDockChanged()
}

func (state *State) toolbarSettingsKey(event gfx.KeyEvent) gfx.EventResult {
	if state.modal != modalToolbarSettings {
		return gfx.EventResult{}
	}
	// The size switch is a page Choice and the entries a page List: the page
	// routes their keys. The List owns the arrows rows and reorders with Alt and
	// the arrows; it has no plain-key reorder or handle-only drag yet.
	if event.Key == gfx.KeyEscape && !event.Release && !event.Repeat {
		state.closeModal()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

// toolbarSettingsScroll gives the wheel over the entries to the page's list and keeps
// it from reaching what lies behind the dialog.
func (state *State) toolbarSettingsScroll(event gfx.ScrollEvent) gfx.EventResult {
	result := gfx.EventResult{Handled: true}
	if pointInRect(event.X, event.Y, state.toolbarEditor.list.Rect()) {
		result = state.windowPage().RouteWheel(event)
		result.Handled = true
	}
	return result
}

// toolbarSizeChoices are the three sizes the switch offers.
var toolbarSizeChoices = [...]terminalToolbarSize{terminalToolbarSmall, terminalToolbarMedium, terminalToolbarLarge}

func (state *State) toolbarSizeLabel(size terminalToolbarSize) string {
	switch size {
	case terminalToolbarMedium:
		return state.messages.SettingsToolbarSizeMedium
	case terminalToolbarLarge:
		return state.messages.SettingsToolbarSizeLarge
	}
	return state.messages.SettingsToolbarSizeSmall
}

// declareToolbarSize declares the terminal toolbar's size switch in the dialog
// body. It is a page Choice, so its arrows move between the options.
func (state *State) declareToolbarSize(c ui.C, bodyWidth float32) {
	theme := c.Theme()
	labelWidth := min(200, bodyWidth/2)
	ui.Text(c, state.messages.SettingsToolbarSize).Width(labelWidth).FontSize(theme.Metrics.FontSizeM).Color(theme.MutedForeground).Ellipsis()
	if ui.Choice(c, &state.terminalToolbarSize, toolbarSizeChoices[:], state.toolbarSizeLabel).Grow(1).Height(32).Changed() {
		state.applyTerminalToolbarSize()
	}
}

// declareToolbarSettings declares the toolbar editor as a dialog of the
// workbench page: the size switch is a page Choice and the reorder list is
// painted into the room the page leaves for it.
func (state *State) declareToolbarSettings(c ui.C) {
	if state.modal != modalToolbarSettings {
		return
	}
	editor, m := &state.toolbarEditor, state.messages.Toolbars
	entries := state.toolbars.entries(editor.group)
	listHeight := float32(len(entries))*48 - 4
	editor.controlsHeight = 0
	if editor.group == toolbarTerminal {
		editor.controlsHeight = 48
	}
	viewport := dialogViewport(c)
	bodyHeight := min(listHeight+editor.controlsHeight, max(viewport.Height-250, 96))
	editor.bodyHeight = bodyHeight
	editor.bodyWidth = 560 - 2*c.Theme().Metrics.PadSurface
	editor.current = c
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: state.toolbarGroupName(editor.group), Description: m.Hint,
		Width: 560, Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitToolbarSettingsDismiss, CloseID: hitToolbarSettingsClose,
	})
	dialog.Body(editor.body)
	dialog.Footer(editor.footer)
}

// declareToolbarSettingsBody declares the dialog's body: the terminal size
// switch over the room the reorder list is painted into.
func (state *State) declareToolbarSettingsBody(c ui.C) {
	editor := &state.toolbarEditor
	ui.Column(c).FillWidth().Height(editor.bodyHeight).Shrink(0).Children(func() {
		if editor.group == toolbarTerminal {
			ui.Row(c).Height(editor.controlsHeight).Shrink(0).Gap(12).Items(ui.Center).Children(func() {
				state.declareToolbarSize(c, editor.bodyWidth)
			})
		}
		entries := state.toolbars.entries(editor.group)
		editor.list = ui.List(c, &editor.rows, len(entries), editor.rowDecl).Grow(1).MinHeight(0).FillWidth().
			PadSides(0, 14, 0, 0).AlwaysScrollbars().ScrollbarSize(14).
			Label(state.toolbarGroupName(editor.group))
	})
}

// declareToolbarSettingsFooter declares the dialog's actions in its footer.
func (state *State) declareToolbarSettingsFooter(c ui.C) {
	m := state.messages.Toolbars
	state.dialogActionFooter(c,
		components.ButtonAction{ID: hitToolbarEnableAll, Label: m.EnableAll, Variant: components.ButtonOutline},
		components.ButtonAction{ID: hitToolbarDisableAll, Label: m.DisableAll, Variant: components.ButtonOutline},
		components.ButtonAction{ID: hitToolbarReset, Label: m.Reset, Variant: components.ButtonOutline},
		components.ButtonAction{ID: hitToolbarSettingsClose, Label: m.Close, Variant: components.ButtonSecondary},
	)
}

// declareToolbarEntry declares one entry of the editor's list: its icon, name, dock
// position and switch, in a row that is dragged to move it.
func (state *State) declareToolbarEntry(row ui.ListRow) {
	c, editor, theme := row.C, &state.toolbarEditor, state.Theme()
	entry := state.toolbars.entries(editor.group)[row.Index]
	spec, _ := toolbarSpecFor(editor.group, entry.Key)
	label := state.toolbarLabel(spec.id)
	index := toolbarEntryIndex(editor.group, entry.Key)
	color := theme.Foreground
	if !entry.Enabled {
		color = theme.MutedForeground
	}
	row.Element.Height(44).MarginBottom(4).PadSides(0, 7, 0, 3).Gap(8).Items(ui.Center).
		Radius(theme.Metrics.RadiusControl).Border(1, theme.Border).Background(theme.Background).Children(func() {
		// The handle is named by its row: the list starts a drag from the handle
		// the row's index names (ListState.Handle).
		reorderHandle(c, hitToolbarHandles.At(row.Index), label, false, 44)
		ui.Row(c).Grow(1).Gap(10).Items(ui.Center).Children(func() {
			ui.Icon(c, lucideIcons[spec.icon]).Size(18, 18).Color(color).Shrink(0)
			ui.Text(c, label).FontSize(theme.Metrics.FontSizeM).Color(color).Ellipsis().Grow(1)
			if editor.group <= toolbarTerminal {
				zone := state.toolbarDockZoneLabel(state.toolbarDockZoneFor(editor.group, entry.Key))
				ui.Button(c, zone).Hit(hitToolbarPositions.At(index), zone).Outline().Shrink(0)
			}
			checked := entry.Enabled
			if editor.group == toolbarTerminal && entry.Key == "latency" {
				// This switch is the same preference as Settings > Show latency.
				checked = state.terminalShowLatency
			}
			ui.Switch(c, state.pageFlag(checked), "").Hit(hitToolbarToggles.At(index), label).Shrink(0)
		})
	})
}
