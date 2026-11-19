package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// declareNoteOverlays declares the menus, popovers and question of one notes
// view: they are page overlays, over the section and over the note window.
func (s *State) declareNoteOverlays(c ui.C, n *notesUI) {
	v := &n.view
	if v.menu != "" {
		s.declareNoteMenu(c, n)
	}
	if v.popup == "tags" {
		s.declareNoteTagPopup(c, n)
	} else if v.popup == "hosts" {
		s.declareNoteHostPopup(c, n)
	}
	if n.confirm != "" {
		s.declareNoteConfirm(c, n)
	}
}

// declareNoteMenu declares the open tree or note menu under its trigger.
func (s *State) declareNoteMenu(c ui.C, n *notesUI) {
	v := &n.view
	var rows menuRows
	add := func(id gfx.ID, label string) { rows.add(components.ContextMenuItem{ID: id, Label: label}) }
	switch v.menu {
	case "tree":
		add(hitNoteSearchToggle, s.noteText("Search notes", "搜索笔记"))
		add(hitNoteAdd, s.noteText("New note", "新建笔记"))
		add(hitNoteFolderAdd, s.noteText("New folder", "新建文件夹"))
		add(hitNoteImport, s.noteText("Import Markdown", "导入 Markdown"))
		add(hitNoteLinkFile, s.noteText("Link Markdown file", "链接 Markdown 文件"))
		add(hitNoteLinkFolder, s.noteText("Link Markdown folder", "链接 Markdown 文件夹"))
		add(hitNoteExportAll, s.noteText("Export ZIP", "导出 ZIP"))
		rows.add(components.ContextMenuItem{ID: hitNoteExpandAll, Label: s.noteExpandLabel(), Disabled: len(n.folders) == 0})
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
	if rows.count == 0 {
		v.menu = ""
		return
	}
	anchor, ok := n.page.anchorBox(n)
	if !ok {
		v.menu = ""
		return
	}
	open := true
	ui.Menu(c, anchor, &open, func(menu *ui.MenuItems) {
		menu.WidthRange(210, 210)
		rows.declare(c, menu, func(id gfx.ID) { s.click(id, true) })
	})
	if !open {
		v.menu = ""
	}
}

// declareNoteConfirm declares the question before a note or folder is deleted.
func (s *State) declareNoteConfirm(c ui.C, n *notesUI) {
	description := s.noteText("Delete this note?", "确认删除这篇笔记？")
	if n.confirm == "folder" {
		description = s.noteText("Contents move to the parent folder unless you choose Delete all contents.", "默认将内容移至上一级；也可选择删除全部内容。")
	}
	if s.noteConfirmTouchesFiles() {
		description += s.noteText(" Files on disk are not deleted.", "磁盘上的文件不会被删除。")
	}
	open := true
	_ = open
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: s.noteText("Confirm deletion", "确认删除"), Description: description, Width: 440, Layer: dialogLayer,
		Scope: hitNoteConfirm, DismissID: hitNoteCancel, CloseID: hitNoteCancel, InitialFocusID: hitNoteCancel,
	})
	dialog.Footer(func() {
		s.dialogActionFooter(c,
			components.ButtonAction{ID: hitNoteCancel, Label: s.noteText("Cancel", "取消"), Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitNoteConfirm, Label: s.noteText("Confirm", "确认"), Variant: components.ButtonDestructive},
		)
		if n.confirm == "folder" {
			label := s.noteText("Delete all contents", "删除全部内容")
			dialogButton(c, label, components.ButtonDestructive, false).Hit(hitNoteDeleteAll, label)
		}
	})
}
