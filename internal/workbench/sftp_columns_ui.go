package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

var sftpColumnSettingsHits = tree.Feature("sftp-column-settings")

var (
	hitSFTPColumnSettingsClose   = tree.ID(sftpColumnSettingsHits, "close")
	hitSFTPColumnSettingsDismiss = tree.ID(sftpColumnSettingsHits, "dismiss")
	hitSFTPColumnSettingsReset   = tree.ID(sftpColumnSettingsHits, "reset")
	hitSFTPColumnSettingsHandles = tree.Sub(sftpColumnSettingsHits, tree.Name("handle"))
	hitSFTPColumnSettingsToggles = tree.Sub(sftpColumnSettingsHits, tree.Name("toggle"))
)

type sftpColumnSettingsUI struct {
	list         components.ReorderListState
	handleLabels [sftpColumnCount]string
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
	editor, m := &state.sftpColumnEditor, state.messages.Tools
	for key := sftpColumnKey(0); key < sftpColumnCount; key++ {
		editor.handleLabels[key] = fmt.Sprintf(m.FilesColumnDragFmt, state.sftpColumnLabel(key))
	}
	state.modal = modalSFTPColumns
}

func (state *State) clickSFTPColumnSettings(id gfx.ID) {
	editor := &state.sftpColumnEditor
	switch {
	case id == hitSFTPColumnSettingsClose || id == hitSFTPColumnSettingsDismiss:
		state.closeModal()
		return
	case id == hitSFTPColumnSettingsReset:
		state.sftpColumns.reset()
		editor.list.Cancel()
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

func (state *State) moveSFTPColumn(move components.ReorderMove) {
	if !move.Valid() || !state.sftpColumns.moveOptional(move.From, move.To) {
		return
	}
	state.save()
}

func (state *State) sftpColumnSettingsPointer(event gfx.PointerEvent) gfx.EventResult {
	if state.modal != modalSFTPColumns {
		return gfx.EventResult{}
	}
	move, result := state.sftpColumnEditor.list.Pointer(event)
	if move.Valid() {
		state.moveSFTPColumn(move)
	}
	return result
}

func (state *State) sftpColumnSettingsKey(event gfx.KeyEvent) gfx.EventResult {
	if state.modal != modalSFTPColumns {
		return gfx.EventResult{}
	}
	move, result := state.sftpColumnEditor.list.Key(event)
	if result.Handled {
		if move.Valid() {
			state.moveSFTPColumn(move)
		}
		return result
	}
	if event.Key == gfx.KeyEscape && !event.Release && !event.Repeat {
		state.closeModal()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

func (state *State) paintSFTPColumnSettings(canvas *gfx.Canvas, viewport gfx.Rect) {
	editor, m, theme := &state.sftpColumnEditor, state.messages.Tools, state.Theme()
	const rowHeight = float32(48)
	height := float32(len(state.sftpColumns.entries))*rowHeight - 4
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitSFTPColumnSettingsDismiss, CloseID: hitSFTPColumnSettingsClose,
		ContentSize: gfx.Size{Width: 560}, FitBody: true, BodyHeight: height,
	})
	layout := dialog.Begin(m.FilesColumnsTitle, m.FilesColumnsHint)
	list := components.BeginReorderList(canvas, theme, layout.Body, &editor.list).RowHeight(rowHeight)
	for _, entry := range state.sftpColumns.entries {
		index := int(entry.Key)
		label := state.sftpColumnLabel(entry.Key)
		fixed := entry.Key == sftpColumnName
		enabled := entry.Enabled || fixed
		row := list.Row(hitSFTPColumnSettingsHandles.At(index), editor.handleLabels[index], fixed)
		color := theme.Foreground
		if !enabled {
			color = theme.MutedForeground
		}
		caption := gfx.Rect{
			X: row.Content.X, Y: row.Content.Y,
			Width: max(row.Content.Width-58, 0), Height: row.Content.Height,
		}
		canvas.Label(canvas.FitLine(label, theme.Metrics.FontSizeM, gfx.WeightRegular, caption.Width), caption, gfx.TextStyle{Size: theme.Metrics.FontSizeM, Color: color})
		toggle := components.BeginSwitch(canvas, state.switchTheme(), hitSFTPColumnSettingsToggles.At(index),
			gfx.Rect{X: row.Content.X + row.Content.Width - 38, Y: row.Content.Y + (row.Content.Height-22)/2, Width: 38, Height: 22}, enabled).Label(label)
		if fixed {
			toggle = toggle.Disabled()
		}
		toggle.Paint()
	}
	list.End()
	dialog.Actions(layout,
		components.ButtonAction{ID: hitSFTPColumnSettingsReset, Label: m.FilesColumnsReset, Variant: components.ButtonOutline},
		components.ButtonAction{ID: hitSFTPColumnSettingsClose, Label: m.FilesColumnsClose, Variant: components.ButtonSecondary},
	)
	dialog.End()
}
