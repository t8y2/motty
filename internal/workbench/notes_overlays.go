package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

func (s *State) paintNoteOverlays(c *gfx.Canvas, b gfx.Rect) {
	n, v := s.noteContent(), &s.noteContent().view
	theme := s.Theme()
	viewport := s.windowRect()
	if viewport.Width == 0 {
		viewport = b
	}
	if v.menu != "" {
		actions := []components.ContextMenuItem{}
		add := func(id gfx.ID, label string) {
			actions = append(actions, components.ContextMenuItem{ID: id, Label: label})
		}
		switch v.menu {
		case "tree":
			add(hitNoteSearchToggle, s.noteText("Search notes", "搜索笔记"))
			add(hitNoteAdd, s.noteText("New note", "新建笔记"))
			add(hitNoteFolderAdd, s.noteText("New folder", "新建文件夹"))
			add(hitNoteImport, s.noteText("Import Markdown", "导入 Markdown"))
			add(hitNoteLinkFile, s.noteText("Link Markdown file", "链接 Markdown 文件"))
			add(hitNoteLinkFolder, s.noteText("Link Markdown folder", "链接 Markdown 文件夹"))
			add(hitNoteExportAll, s.noteText("Export ZIP", "导出 ZIP"))
			actions = append(actions, components.ContextMenuItem{ID: hitNoteExpandAll, Label: s.noteExpandLabel(), Disabled: len(n.folders) == 0})
		case "folder":
			add(hitNoteAdd, s.noteText("New note", "新建笔记"))
			add(hitNoteFolderAdd, s.noteText("New folder", "新建文件夹"))
			add(hitNoteRename, s.noteText("Rename", "重命名"))
			add(hitNoteImport, s.noteText("Import Markdown", "导入 Markdown"))
			add(hitNoteLinkFile, s.noteText("Link Markdown file", "链接 Markdown 文件"))
			add(hitNoteLinkFolder, s.noteText("Link Markdown folder", "链接 Markdown 文件夹"))
			if s.noteFolderRoot2(v.menuID) != "" {
				add(hitNoteFolderUnlink, s.noteText("Unlink folder (keep as notes)", "取消文件夹链接（保留为笔记）"))
			}
			add(hitNoteFolderRemove, s.noteText("Delete folder", "删除文件夹"))
		default:
			if s.activeSection == sectionTerminal {
				add(hitNotes, s.noteText("Open library", "打开笔记库"))
			}
			add(hitNoteRename, s.noteText("Rename", "重命名"))
			add(hitNotePin, s.noteText("Pin / unpin", "置顶 / 取消"))
			add(hitNoteExport, s.noteText("Export Markdown", "导出 Markdown"))
			if s.noteLinkPath(v.menuID) != "" {
				add(hitNoteLinkUnlink, s.noteText("Unlink file (keep as note)", "取消文件链接（保留为笔记）"))
			}
			add(hitNoteDelete, s.noteText("Delete note", "删除笔记"))
		}
		anchor := v.overlayAnchorRect(c)
		menu := components.BeginContextMenu(c, theme, gfx.Rect{X: anchor.X, Y: anchor.Y + anchor.Height, Width: 210, Height: float32(len(actions))*32 + 8}, components.ContextMenuOptions{Layer: popupLayer + 2, Scope: hitNoteDismiss, DismissID: hitNoteDismiss, Viewport: viewport, Scroll: s.menuScrollFor(hitNoteDismiss)})
		s.menuPlaced(menu.Bounds())
		for _, a := range actions {
			menu.AddItem(a)
		}
		menu.End()
	}
	if v.popup == "tags" {
		s.paintNoteTagPopup(c, viewport)
	} else if v.popup == "hosts" {
		s.paintNoteHostPopup(c, viewport)
	}
	if i, ok := c.Hovered().In(hitNoteFormats); ok && v.menu == "" && v.popup == "" {
		tooltipAnchor, _ := c.HitBounds(c.Hovered())
		components.BeginTooltip(c, theme, viewport, tooltipAnchor, components.TooltipOptions{Layer: tooltipLayer, Side: components.TooltipTop}).Paint(s.noteFormatName(i))
	}
	if n.confirm != "" {
		dialog := components.BeginDialog(c, theme, viewport, components.DialogOptions{Layer: dialogLayer, Scope: hitNoteConfirm, CloseID: hitNoteCancel, InitialFocusID: hitNoteCancel, ContentSize: gfx.Size{Width: 440}, FitBody: true, BodyHeight: 54})
		description := s.noteText("Delete this note?", "确认删除这篇笔记？")
		if n.confirm == "folder" {
			description = s.noteText("Contents move to the parent folder unless you choose Delete all contents.", "默认将内容移至上一级；也可选择删除全部内容。")
		}
		if s.noteConfirmTouchesFiles() {
			description += s.noteText(" Files on disk are not deleted.", "磁盘上的文件不会被删除。")
		}
		layout := dialog.Begin(s.noteText("Confirm deletion", "确认删除"), description)
		actions := []components.ButtonAction{{ID: hitNoteCancel, Label: s.noteText("Cancel", "取消"), Variant: components.ButtonSecondary}, {ID: hitNoteConfirm, Label: s.noteText("Confirm", "确认"), Variant: components.ButtonDestructive}}
		if n.confirm == "folder" {
			actions = append(actions, components.ButtonAction{ID: hitNoteDeleteAll, Label: s.noteText("Delete all contents", "删除全部内容"), Variant: components.ButtonDestructive})
		}
		dialog.Actions(layout, actions...)
		dialog.End()
	}
}

var hitNotePickerScroll = tree.ID(notesHits, "picker-scroll")
