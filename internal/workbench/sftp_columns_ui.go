package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var sftpColumnSettingsHits = tree.Feature("sftp-column-settings")

var (
	hitSFTPColumnSettingsClose   = tree.ID(sftpColumnSettingsHits, "close")
	hitSFTPColumnSettingsDismiss = tree.ID(sftpColumnSettingsHits, "dismiss")
	hitSFTPColumnSettingsReset   = tree.ID(sftpColumnSettingsHits, "reset")
	hitSFTPColumnSettingsToggles = tree.Sub(sftpColumnSettingsHits, tree.Name("toggle"))
	hitSFTPColumnSettingsHandles = tree.Sub(sftpColumnSettingsHits, tree.Name("handle"))
)

type sftpColumnSettingsUI struct {
	rows ui.ListState
}

func (state *State) sftpColumnLabel(key sftpColumnKey) string {
	m := state.messages.Tools
	switch key {
	case sftpColumnName:
		return m.FilesColumnName
	case sftpColumnSize:
		return m.FilesColumnSize
	case sftpColumnModified:
		return m.FilesColumnModified
	}
	return ""
}

func (state *State) openSFTPColumnSettings() {
	state.sftpColumnEditor = sftpColumnSettingsUI{}
	state.modal = modalSFTPColumns
}

func (state *State) clickSFTPColumnSettings(id gfx.ID) {
	switch {
	case id == hitSFTPColumnSettingsClose || id == hitSFTPColumnSettingsDismiss:
		state.closeModal()
		return
	case id == hitSFTPColumnSettingsReset:
		state.sftpColumns.reset()
	case id.Scope() == hitSFTPColumnSettingsToggles:
		key := sftpColumnKey(item(id))
		if key <= sftpColumnName || key >= sftpColumnCount ||
			!state.sftpColumns.setEnabled(key, !state.sftpColumns.enabled(key)) {
			return
		}
	default:
		return
	}
	state.save()
}

// moveSFTPColumn puts the column at row from before row to, as ListState.Reorder says;
// the name column stays first.
func (state *State) moveSFTPColumn(from, to int) {
	if to > from {
		to--
	}
	if !state.sftpColumns.moveOptional(from, to) {
		return
	}
	state.save()
}

func (state *State) sftpColumnSettingsKey(event gfx.KeyEvent) gfx.EventResult {
	if state.modal != modalSFTPColumns {
		return gfx.EventResult{}
	}
	// The columns are a page List: it routes the arrows and reorders with Alt and
	// the arrows. It has no plain-key reorder or handle-only drag yet.
	if event.Key == gfx.KeyEscape && !event.Release && !event.Repeat {
		state.closeModal()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

// declareSFTPColumnSettings declares the columns editor as a dialog of the page: the
// columns are a List whose rows are dragged, or moved with Alt and the arrows, and each
// has a switch; the name column is fixed in front.
func (state *State) declareSFTPColumnSettings(c ui.C) {
	editor, m := &state.sftpColumnEditor, state.messages.Tools
	editor.rows.Key = func(i int) int { return int(state.sftpColumns.entries[i].Key) }
	editor.rows.Label = func(i int) string { return state.sftpColumnLabel(state.sftpColumns.entries[i].Key) }
	editor.rows.NoTypeahead = true
	editor.rows.Reorder = func(rows []int, to int) {
		if rows[0] > 0 {
			state.moveSFTPColumn(rows[0], to)
		}
	}
	editor.rows.Handle = hitSFTPColumnSettingsHandles
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.FilesColumnsTitle, Description: m.FilesColumnsHint, Width: 560, Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitSFTPColumnSettingsDismiss, CloseID: hitSFTPColumnSettingsClose,
	})
	dialog.Body(func() {
		ui.List(c, &editor.rows, len(state.sftpColumns.entries), state.declareSFTPColumn).
			FillWidth().Height(float32(len(state.sftpColumns.entries))*48 - 4).Shrink(0).Label(m.FilesColumnsTitle)
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitSFTPColumnSettingsReset, Label: m.FilesColumnsReset, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitSFTPColumnSettingsClose, Label: m.FilesColumnsClose, Variant: components.ButtonSecondary},
		)
	})
}

// declareSFTPColumn declares one column's row: its name and its switch.
func (state *State) declareSFTPColumn(row ui.ListRow) {
	c, theme := row.C, state.Theme()
	entry := state.sftpColumns.entries[row.Index]
	index := int(entry.Key)
	label := state.sftpColumnLabel(entry.Key)
	fixed := entry.Key == sftpColumnName
	enabled := entry.Enabled || fixed
	color := theme.Foreground
	if !enabled {
		color = theme.MutedForeground
	}
	row.Element.Height(48).MarginBottom(4).PadSides(0, 7, 0, 3).Gap(8).Items(ui.Center).
		Radius(theme.Metrics.RadiusControl).Border(1, theme.Border).Background(theme.Background).Children(func() {
		// The handle is named by its row, which is what the list's Handle scope
		// maps back to; the switch and position keep the column's key.
		reorderHandle(c, hitSFTPColumnSettingsHandles.At(row.Index), label, fixed, 48)
		ui.Row(c).Grow(1).Gap(10).Items(ui.Center).Children(func() {
			ui.Text(c, label).FontSize(theme.Metrics.FontSizeM).Color(color).Ellipsis().Grow(1)
			toggle := ui.Switch(c, state.pageFlag(enabled), "").Hit(hitSFTPColumnSettingsToggles.At(index), label).Shrink(0)
			if fixed {
				toggle.Disabled(true)
			}
		})
	})
}
