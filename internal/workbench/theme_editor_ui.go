package workbench

import (
	"fmt"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (state *State) declareThemeEditor(c ui.C) {
	e := &state.themeEditor
	width := min(float32(1120), c.Bounds().Width-32)
	height := min(float32(760), c.Bounds().Height-40)
	if e.maximized {
		width, height = c.Bounds().Width-32, c.Bounds().Height-32
	}
	title := state.themeText("Theme editor", "主题编辑器", "主題編輯器")
	if e.deleting {
		title = state.themeText("Delete theme", "删除主题", "刪除主題")
		width, height = min(width, 520), 230
	}
	dialog := ui.Dialog(c, ui.DialogSpec{Title: title, Width: width, Height: height,
		Layer: dialogLayer, Scope: dialogScope, CloseID: hitThemeClose, NoClose: !e.deleting, InitialFocusID: hitThemeName})
	if !e.deleting {
		dialog.Header(func() {
			// 标题栏按钮以外框为基准，不继承表单内容的内边距。
			padding := c.Theme().Metrics.PadSurface + c.Theme().Metrics.GapItem
			ui.Row(c).Absolute().Top(4 - padding).Right(4 - padding).Height(36).Children(func() {
				label := state.themeText("Maximize", "最大化", "最大化")
				if e.maximized {
					label = state.themeText("Restore", "还原", "還原")
				}
				mark := themeMaximizeIcon
				if e.maximized {
					mark = themeRestoreIcon
				}
				ui.Button(c, label).Icon(mark).IconOnly().Ghost().Size(36, 36).Hit(hitThemeMaximize, label)
				closeLabel := state.themeText("Close", "关闭", "關閉")
				ui.Button(c, closeLabel).Icon(lucideIcons[iconClose]).IconOnly().Ghost().Size(36, 36).Hit(hitThemeClose, closeLabel)
			})
		})
	}
	dialog.Body(func() {
		if e.deleting {
			ui.Paragraph(c, state.themeText("Delete this custom theme? Terminals using it will return to Default.",
				"删除此自定义主题？正在使用它的终端将恢复默认配色。", "刪除此自訂主題？正在使用它的終端將恢復預設配色。"), 3).Color(state.pal.text)
			ui.Text(c, e.draft.name).Color(state.pal.muted).MarginSides(12, 0, 0, 0)
			if e.error != "" {
				state.problemWrapC(c, e.error)
			}
			return
		}
		ui.Column(c).Fill().Gap(12).Children(func() {
			narrow := width < 820
			if narrow && !e.expanded {
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c, state.themeText("Edit", "编辑", "編輯")).Outline().Clicked() {
						e.previewOnly = false
					}
					if ui.Button(c, state.themeText("Preview", "预览", "預覽")).Outline().Clicked() {
						e.previewOnly = true
					}
				})
			}
			ui.Row(c).Grow(1).Basis(0).Gap(20).Children(func() {
				if !e.expanded && (!narrow || !e.previewOnly) {
					ui.Scroll(c).Grow(1).Basis(0).Scrollbars().Children(func() {
						ui.Column(c).FillWidth().Gap(14).PadRight(10).Children(func() { state.declareThemeEditorForm(c) })
					})
				}
				if e.expanded || !narrow || e.previewOnly {
					ui.Scroll(c).Grow(1).Basis(0).Scrollbars().Children(func() { state.declareThemePreview(c) })
				}
			})
		})
	})
	dialog.Footer(func() {
		actions := []components.ButtonAction{{ID: hitThemeClose, Label: state.messages.Cancel, Variant: components.ButtonSecondary}}
		if !e.deleting {
			actions = append(actions, components.ButtonAction{ID: hitThemeSave, Label: state.themeText("Save", "保存", "儲存"), Variant: components.ButtonOutline})
		}
		label := state.themeText("Save & apply", "保存并应用", "儲存並套用")
		variant := components.ButtonDefault
		if e.deleting {
			label = state.themeText("Delete", "删除", "刪除")
			variant = components.ButtonDestructive
		}
		actions = append(actions, components.ButtonAction{ID: hitThemeApply, Label: label, Variant: variant})
		state.dialogActionFooter(c, actions...)
	})
}

func (state *State) declareThemeEditorForm(c ui.C) {
	e := &state.themeEditor
	label := state.themeText("Theme name", "主题名称", "主題名稱")
	ui.Text(c, label).Color(state.pal.text).FontSize(13)
	ui.TextField(c, &e.name, label, label, ui.WithID(hitThemeName)).FillWidth()
	if e.draft.id == "" {
		ui.Row(c).Gap(8).Children(func() {
			for _, look := range []appearance{appearanceDark, appearanceLight} {
				label := state.themeText("Dark template", "深色模板", "深色範本")
				if look == appearanceLight {
					label = state.themeText("Light template", "浅色模板", "淺色範本")
				}
				if ui.Button(c, label).Outline().Clicked() {
					e.useTemplate(look)
				}
			}
		})
	}
	state.declareThemeColors(c)
	label = state.themeText("Background opacity", "背景不透明度", "背景不透明度")
	ui.Text(c, fmt.Sprintf("%s · %.0f%%", label, e.opacity.Value)).Color(state.pal.text).FontSize(13)
	ui.Slider(c, &e.opacity).Range(0, 100).Step(1).Hit(hitThemeOpacity, label).FillWidth()
	ui.Paragraph(c, state.themeText("Blends the background with the image or application background; text stays opaque.",
		"背景与图片或应用底色混合，文字保持不透明。", "背景與圖片或應用底色混合，文字保持不透明。"), 3).FontSize(12).Color(state.pal.muted).LineHeight(18)
	ui.Row(c).Gap(8).Children(func() {
		reset := state.themeText("Reset changes", "重置修改", "重設修改")
		ui.Button(c, reset).Outline().Hit(hitThemeReset, reset)
		preview := state.themeText("Expand preview", "展开预览", "展開預覽")
		ui.Button(c, preview).Outline().Hit(hitThemePreview, preview)
	})
	if e.error != "" {
		ui.Paragraph(c, e.error, 3).FontSize(12).Color(state.pal.danger).LineHeight(18)
	}
}

var themeMaximizeIcon = gfx.Icon{ViewBox: 24, Stroke: 1.5, Commands: []gfx.PathCommand{
	gfx.MoveTo(5, 5), gfx.LineTo(19, 5), gfx.LineTo(19, 19), gfx.LineTo(5, 19), gfx.LineTo(5, 5),
}}
var themeRestoreIcon = gfx.Icon{ViewBox: 24, Stroke: 1.5, Commands: []gfx.PathCommand{
	gfx.MoveTo(8, 5), gfx.LineTo(19, 5), gfx.LineTo(19, 16),
	gfx.MoveTo(5, 8), gfx.LineTo(16, 8), gfx.LineTo(16, 19), gfx.LineTo(5, 19), gfx.LineTo(5, 8),
}}
