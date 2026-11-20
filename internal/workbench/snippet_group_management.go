package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// Groups are named by the snippets in them, so managing one is managing every
// snippet that carries the name: renaming rewrites the name on all of them, and
// deleting it leaves their commands on the page as ungrouped ones. The Snippets
// page opens this manager from the button beside its group filter.

var (
	hitSnippetGroups       = tree.ID(snippetsHits, "snippet-groups")
	hitSnippetDialogGroups = tree.ID(snippetsHits, "snippet-dialog-groups")

	hitSnippetGroupsClose        = tree.ID(snippetsHits, "snippet-groups-close")
	hitSnippetGroupsDone         = tree.ID(snippetsHits, "snippet-groups-done")
	hitSnippetGroupRenameList    = tree.Sub(snippetsHits, tree.Name("snippet-group-rename"))
	hitSnippetGroupDeleteList    = tree.Sub(snippetsHits, tree.Name("snippet-group-delete"))
	hitSnippetGroupRenameField   = tree.ID(snippetsHits, "snippet-group-rename-field")
	hitSnippetGroupRenameSubmit  = tree.ID(snippetsHits, "snippet-group-rename-submit")
	hitSnippetGroupRenameCancel  = tree.ID(snippetsHits, "snippet-group-rename-cancel")
	hitSnippetGroupRenameClose   = tree.ID(snippetsHits, "snippet-group-rename-close")
	hitSnippetGroupDeleteConfirm = tree.ID(snippetsHits, "snippet-group-delete-confirm")
	hitSnippetGroupDeleteCancel  = tree.ID(snippetsHits, "snippet-group-delete-cancel")
	hitSnippetGroupDeleteClose   = tree.ID(snippetsHits, "snippet-group-delete-close")
	hitSnippetGroupDeleteDismiss = tree.ID(snippetsHits, "snippet-group-delete-dismiss")
)

type snippetGroupsUI struct {
	// target is the group the rename or delete dialog asks about.
	target string
	editor text.Editor
	field  dialogField
	error  string
	// returning reopens the manager after the rename or delete dialog closes.
	returning bool
	// returnToSnippet reopens the snippet editor the manager was opened from.
	returnToSnippet bool
	// bounds is the manager's scroll area, for the wheel.
	bounds gfx.Rect
}

func (state *State) openSnippetGroupsManager() {
	state.openSnippetGroupsManagerFor(false)
}

// openSnippetGroupsManagerFor opens the manager, remembering whether the
// snippet editor underneath should come back when it closes.
func (state *State) openSnippetGroupsManagerFor(fromSnippet bool) {
	state.snippetGroups = snippetGroupsUI{returnToSnippet: fromSnippet}
	state.modal = modalSnippetGroups
}

// clickSnippetGroups runs a press in the group manager.
func (state *State) clickSnippetGroups(id gfx.ID) {
	form := &state.snippets
	switch {
	case id == hitSnippetGroupsClose, id == hitSnippetGroupsDone:
		state.closeModal()
	case id.Scope() == hitSnippetGroupRenameList:
		if index, ok := id.In(hitSnippetGroupRenameList); ok && index >= 0 && index < len(form.groups) {
			state.openSnippetGroupRename(form.groups[index])
		}
	case id.Scope() == hitSnippetGroupDeleteList:
		if index, ok := id.In(hitSnippetGroupDeleteList); ok && index >= 0 && index < len(form.groups) {
			state.openSnippetGroupDelete(form.groups[index])
		}
	}
}

func (state *State) openSnippetGroupRename(group string) {
	form := &state.snippetGroups
	form.target, form.error, form.returning = group, "", true
	form.field = dialogField{}
	form.editor = text.Editor{}
	form.editor.Insert(group)
	form.editor.SetSelection(text.Range{End: form.editor.RuneCount()})
	state.modal = modalSnippetGroupRename
}

func (state *State) openSnippetGroupDelete(group string) {
	form := &state.snippetGroups
	form.target, form.error, form.returning = group, "", true
	state.modal = modalSnippetGroupDelete
}

// clickSnippetGroupDialog runs the rename or delete dialog.
func (state *State) clickSnippetGroupDialog(id gfx.ID) {
	form := &state.snippetGroups
	if state.modal == modalSnippetGroupRename {
		switch id {
		case hitSnippetGroupRenameCancel, hitSnippetGroupRenameClose:
			state.closeModal()
		case hitSnippetGroupRenameSubmit:
			state.submitSnippetGroupRename()
		}
		return
	}
	switch id {
	case hitSnippetGroupDeleteCancel, hitSnippetGroupDeleteClose, hitSnippetGroupDeleteDismiss:
		state.closeModal()
	case hitSnippetGroupDeleteConfirm:
		state.deleteSnippetGroup(form.target)
		state.closeModal()
	}
}

// submitSnippetGroupRename renames the group the dialog asks about and returns to
// the manager; a name that cannot be saved keeps the dialog open with the reason.
func (state *State) submitSnippetGroupRename() {
	form := &state.snippetGroups
	if _, composing := form.editor.Composition(); composing {
		return
	}
	if !state.renameSnippetGroup(form.target, form.editor.String()) {
		return
	}
	state.closeModal()
}

// renameSnippetGroup gives every snippet that names from the name to, keeping
// the group filter and the page on the renamed group. A name another group
// already has is refused, so two groups never merge by accident.
func (state *State) renameSnippetGroup(from, to string) bool {
	form, manager, m := &state.snippets, &state.snippetGroups, state.messages.Tools
	to = strings.TrimSpace(to)
	if !validSnippetGroup(to) {
		manager.error = m.SnippetGroupRenameInvalid
		return false
	}
	fromAt := -1
	for index, group := range form.groups {
		if sameSnippetGroup(group, from) {
			fromAt = index
			continue
		}
		if sameSnippetGroup(group, to) {
			manager.error = m.SnippetGroupConflict
			return false
		}
	}
	if fromAt < 0 {
		manager.error = m.SnippetGroupMissing
		return false
	}
	for index := range form.list {
		if sameSnippetGroup(form.list[index].Group, from) {
			form.list[index].Group = to
		}
	}
	if form.filter.group != "" && sameSnippetGroup(form.filter.group, from) {
		form.filter.group = to
	}
	manager.error = ""
	state.refreshSnippets()
	if manager.returnToSnippet && sameSnippetGroup(state.snippets.groupPicker.Value(), from) {
		state.snippetGroupPicker(to)
	}
	state.save()
	state.status = fmt.Sprintf(m.StatusGroupRenamedFmt, from, to)
	return true
}

// deleteSnippetGroup clears the group from its snippets: the commands stay on
// the page as ungrouped ones. A filter on the deleted group shows everything
// again, as it does for a group that is gone for any other reason.
func (state *State) deleteSnippetGroup(group string) {
	form, manager, m := &state.snippets, &state.snippetGroups, state.messages.Tools
	count := 0
	for index := range form.list {
		if sameSnippetGroup(form.list[index].Group, group) {
			form.list[index].Group = ""
			count++
		}
	}
	if count == 0 {
		return
	}
	if form.filter.group != "" && sameSnippetGroup(form.filter.group, group) {
		form.filter = snippetFilter{}
	}
	state.refreshSnippets()
	if manager.returnToSnippet && sameSnippetGroup(state.snippets.groupPicker.Value(), group) {
		state.snippetGroupPicker("")
	}
	state.save()
	state.status = fmt.Sprintf(m.StatusGroupDeletedFmt, group, count)
}

// snippetGroupCount is how many snippets the group at name lists, counted when
// the groups were last refreshed.
func (state *State) snippetGroupCount(name string) int {
	for _, group := range state.snippets.groups {
		if sameSnippetGroup(group, name) {
			return state.snippets.groupCounts[group]
		}
	}
	return 0
}

// declareSnippetGroupsDialog lists the groups with the two things a page cannot
// do from the filter alone: rename one, and delete one.
func (state *State) declareSnippetGroupsDialog(c ui.C) {
	manager, form, m := &state.snippetGroups, &state.snippets, state.messages.Tools
	count := len(form.groups)
	viewport := dialogViewport(c)
	body := float32(max(1, min(count, 7))) * 56
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 560, m.SnippetGroupsDescription, body)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.SnippetGroupsManage, Description: m.SnippetGroupsDescription,
		Width: 560, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitSnippetGroupsClose, InitialFocusID: hitSnippetGroupsDone,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().Children(func() {
			list := ui.Scroll(c).FillWidth().Grow(1).Basis(0).PadSides(0, 12, 0, 0).
				AlwaysScrollbars().ScrollbarSize(10).Label(m.SnippetGroupsTitle)
			list.Children(func() {
				if count == 0 {
					ui.Text(c, m.SnippetGroupEmpty).FontSize(metrics.FontSizeM).Color(state.pal.muted).Margin(16)
					return
				}
				for index := range count {
					state.declareSnippetGroupManagerRow(c, index)
				}
			})
			list.DrawOver(func(d *ui.DrawContext) { manager.bounds = d.Rect })
		})
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitSnippetGroupsDone, Label: m.SnippetGroupsClose, Variant: components.ButtonSecondary},
		)
	})
}

// declareSnippetGroupManagerRow is one group in the manager: its name, how many
// commands it holds, and the two actions.
func (state *State) declareSnippetGroupManagerRow(c ui.C, index int) {
	form, m := &state.snippets, state.messages.Tools
	group := form.groups[index]
	ui.Row(c).FillWidth().Height(56).Shrink(0).Items(ui.Center).Gap(12).PadSides(0, 14, 0, 10).Children(func() {
		ui.Icon(c, lucideIcons[iconFolderOpen]).Size(16, 16).Color(state.pal.muted).Shrink(0)
		ui.Column(c).Grow(1).Basis(0).Gap(2).Children(func() {
			ui.Text(c, group).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(state.pal.text).MaxLines(1).Ellipsis()
			ui.Text(c, fmt.Sprintf(m.SnippetGroupCountFmt, state.snippetGroupCount(group))).
				FontSize(metrics.FontSizeS).Color(state.pal.muted).MaxLines(1)
		})
		state.declareIconButton(c, hitSnippetGroupRenameList.At(index), iconPencil, m.SnippetGroupRename+": "+group, false, iconButtonQuiet).Size(32, 32)
		state.declareIconButton(c, hitSnippetGroupDeleteList.At(index), iconTrash, m.SnippetGroupDelete+": "+group, false, iconButtonQuiet).Size(32, 32)
	})
}

// scrollSnippetGroups keeps the wheel over the manager's list on that list, and
// stops it from reaching the page behind the dialog.
func (state *State) scrollSnippetGroups(event gfx.ScrollEvent) gfx.EventResult {
	result := gfx.EventResult{Handled: true}
	if pointInRect(event.X, event.Y, state.snippetGroups.bounds) {
		result = state.windowPage().RouteWheel(event)
		result.Handled = true
	}
	return result
}

// declareSnippetGroupRenameDialog declares the one-field rename dialog, which
// reopens the manager when it closes.
func (state *State) declareSnippetGroupRenameDialog(c ui.C) {
	manager, m := &state.snippetGroups, state.messages.Tools
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 460, "", 90)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.SnippetGroupRenameTitle, Width: 460, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitSnippetGroupRenameClose, InitialFocusID: hitSnippetGroupRenameField,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().Children(func() {
			manager.field.declare(c, state, &manager.editor, dialogFieldSpec{
				ID: hitSnippetGroupRenameField, Label: m.SnippetGroupRenameField,
				Placeholder: state.messages.FieldGroupPlaceholder, Error: manager.error, Height: 90,
			})
		})
	})
	dialog.Footer(func() {
		if dialogButton(c, state.messages.Cancel, components.ButtonSecondary, false).Clicked() {
			c.After(func() { state.clickSnippetGroupDialog(hitSnippetGroupRenameCancel) })
		}
		if dialogButton(c, m.SnippetGroupRenameSave, components.ButtonDefault, false).Clicked() {
			c.After(func() { state.clickSnippetGroupDialog(hitSnippetGroupRenameSubmit) })
		}
	})
}

// declareSnippetGroupDeleteDialog asks before the group stops naming the
// commands in it.
func (state *State) declareSnippetGroupDeleteDialog(c ui.C) {
	manager, m := &state.snippetGroups, state.messages.Tools
	description := fmt.Sprintf(m.SnippetGroupDeleteBodyFmt, manager.target, state.snippetGroupCount(manager.target))
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 480, description, 0)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.SnippetGroupDeleteTitle, Description: description,
		Width: 480, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitSnippetGroupDeleteDismiss, CloseID: hitSnippetGroupDeleteClose,
		InitialFocusID: hitSnippetGroupDeleteCancel,
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitSnippetGroupDeleteCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitSnippetGroupDeleteConfirm, Label: m.SnippetGroupDeleteConfirm, Variant: components.ButtonDestructive},
		)
	})
}
