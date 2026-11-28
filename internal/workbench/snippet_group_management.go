package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/workbench/icons"
)

// Groups are named by the snippets in them, so managing one is managing every
// snippet that carries the name: renaming rewrites the name on all of them, and
// deleting it leaves their commands on the page as ungrouped ones. The Snippets
// page opens this manager from the button beside its group filter.

var (
	hitSnippetGroups = tree.ID(snippetsHits, "snippet-groups")

	hitSnippetGroupsClose        = tree.ID(snippetsHits, "snippet-groups-close")
	hitSnippetGroupsDone         = tree.ID(snippetsHits, "snippet-groups-done")
	hitSnippetGroupRenameList    = tree.Sub(snippetsHits, tree.Name("snippet-group-rename"))
	hitSnippetGroupDeleteList    = tree.Sub(snippetsHits, tree.Name("snippet-group-delete"))
	hitSnippetGroupRenameField   = tree.ID(snippetsHits, "snippet-group-rename-field")
	hitSnippetGroupRenameSubmit  = tree.ID(snippetsHits, "snippet-group-rename-submit")
	hitSnippetGroupRenameCancel  = tree.ID(snippetsHits, "snippet-group-rename-cancel")
	hitSnippetGroupRenameClose   = tree.ID(snippetsHits, "snippet-group-rename-close")
	hitSnippetGroupDeleteKeep    = tree.ID(snippetsHits, "snippet-group-delete-keep")
	hitSnippetGroupDeletePurge   = tree.ID(snippetsHits, "snippet-group-delete-purge")
	hitSnippetGroupDeleteConfirm = tree.ID(snippetsHits, "snippet-group-delete-confirm")
	hitSnippetGroupDeleteCancel  = tree.ID(snippetsHits, "snippet-group-delete-cancel")
	hitSnippetGroupDeleteClose   = tree.ID(snippetsHits, "snippet-group-delete-close")
	hitSnippetGroupDeleteDismiss = tree.ID(snippetsHits, "snippet-group-delete-dismiss")
)

type snippetGroupsUI struct {
	// target is the group the rename or delete dialog asks about.
	target string
	// creating switches modalSnippetGroupRename into a create-group dialog.
	creating bool
	// deleteSnippets makes modalSnippetGroupDelete also delete every snippet
	// under target instead of ungrouping them.
	deleteSnippets bool
	// parent is the selected parent group path when creating or renaming a group
	// ("" means top-level).
	parent        string
	parentField   ui.FormFieldState
	parentChoices []string
	editor        text.Editor
	field         dialogField
	error         string
	// returning reopens the manager after the rename or delete dialog closes.
	returning bool
	// bounds is the manager's scroll area, for the wheel.
	bounds gfx.Rect
}

func (state *State) openSnippetGroupsManager() {
	state.snippetGroups = snippetGroupsUI{}
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
	state.openSnippetGroupRenameMode(group, true)
}

func (state *State) openSnippetGroupRenameDirect(group string) {
	state.openSnippetGroupRenameMode(group, false)
}

func (state *State) openSnippetGroupCreate(parent string) {
	form := &state.snippetGroups
	form.target, form.creating, form.error, form.returning = "", true, "", false
	form.parent = cleanSnippetGroup(parent)
	form.parentField = ui.FormFieldState{}
	form.field = dialogField{}
	form.editor = text.Editor{}
	state.modal = modalSnippetGroupRename
}

func (state *State) openSnippetGroupRenameMode(group string, returning bool) {
	form := &state.snippetGroups
	form.target, form.creating, form.error, form.returning = group, false, "", returning
	form.parent = snippetGroupParent(group)
	form.parentField = ui.FormFieldState{}
	form.field = dialogField{}
	form.editor = text.Editor{}
	form.editor.Insert(snippetGroupLeaf(group))
	form.editor.SetSelection(text.Range{End: form.editor.RuneCount()})
	state.modal = modalSnippetGroupRename
}

func (state *State) openSnippetGroupDelete(group string) {
	state.openSnippetGroupDeleteMode(group, true)
}

func (state *State) openSnippetGroupDeleteDirect(group string) {
	state.openSnippetGroupDeleteMode(group, false)
}

func (state *State) openSnippetGroupDeleteMode(group string, returning bool) {
	form := &state.snippetGroups
	form.target, form.deleteSnippets, form.error, form.returning = group, false, "", returning
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
	case hitSnippetGroupDeleteKeep:
		form.deleteSnippets = false
	case hitSnippetGroupDeletePurge:
		form.deleteSnippets = true
	case hitSnippetGroupDeleteCancel, hitSnippetGroupDeleteClose, hitSnippetGroupDeleteDismiss:
		state.closeModal()
	case hitSnippetGroupDeleteConfirm:
		if form.deleteSnippets {
			state.flushInlineSnippetSave()
			state.deleteSnippetGroupWithSnippets(form.target)
		} else {
			state.deleteSnippetGroup(form.target)
		}
		state.closeModal()
	}
}

// submitSnippetGroupRename creates or renames the group the dialog asks about;
// a name that cannot be saved keeps the dialog open with the reason.
func (state *State) submitSnippetGroupRename() {
	form := &state.snippetGroups
	if _, composing := form.editor.Composition(); composing {
		return
	}
	raw := strings.TrimSpace(form.editor.String())
	if form.parent != "" && strings.Contains(raw, "/") {
		form.error = state.messages.Tools.SnippetGroupRenameInvalid
		return
	}
	fullPath := raw
	if form.parent != "" && raw != "" {
		fullPath = form.parent + "/" + raw
	}
	if form.creating {
		if !state.createSnippetGroup(fullPath) {
			return
		}
		state.closeModal()
		return
	}
	if !state.renameSnippetGroup(form.target, fullPath) {
		return
	}
	state.closeModal()
}

// createSnippetGroup adds a new explicit (possibly empty) snippet group path so
// it appears immediately in the Snippets directory tree and persists on disk.
func (state *State) createSnippetGroup(name string) bool {
	form, manager, m := &state.snippets, &state.snippetGroups, state.messages.Tools
	normalized := normalizeSnippetGroupPath(name)
	if !validSnippetGroup(normalized) {
		manager.error = m.SnippetGroupRenameInvalid
		return false
	}
	for _, existing := range form.groups {
		if sameSnippetGroup(existing, normalized) {
			manager.error = m.SnippetGroupConflict
			return false
		}
	}
	form.customGroups = append(form.customGroups, normalized)
	form.selectedFolder = normalized
	state.snippetTreeExpandAncestors(normalized)
	manager.error = ""
	state.refreshSnippets()
	state.save()
	return true
}

// renameSnippetGroup gives every snippet in from (and its descendants) the new
// path under to, keeping the group filter and the page on the renamed group. A
// name another group already has is refused, so two groups never merge by
// accident.
func (state *State) renameSnippetGroup(from, to string) bool {
	form, manager, m := &state.snippets, &state.snippetGroups, state.messages.Tools
	to = normalizeSnippetGroupPath(to)
	if !validSnippetGroup(to) {
		manager.error = m.SnippetGroupRenameInvalid
		return false
	}
	if !sameSnippetGroup(to, from) && snippetGroupWithin(to, from) {
		manager.error = m.SnippetGroupRenameInvalid
		return false
	}
	fromAt := -1
	for index, group := range form.groups {
		if sameSnippetGroup(group, from) {
			fromAt = index
			break
		}
	}
	if fromAt < 0 {
		manager.error = m.SnippetGroupMissing
		return false
	}
	for _, group := range form.groups {
		if !snippetGroupWithin(group, from) {
			continue
		}
		remapped := remapSnippetGroupPath(group, from, to)
		if !validSnippetGroup(remapped) {
			manager.error = m.SnippetGroupRenameInvalid
			return false
		}
		for _, existing := range form.groups {
			if snippetGroupWithin(existing, from) {
				continue
			}
			if sameSnippetGroup(existing, remapped) {
				manager.error = m.SnippetGroupConflict
				return false
			}
		}
	}
	for index := range form.list {
		if snippetGroupWithin(form.list[index].Group, from) {
			form.list[index].Group = remapSnippetGroupPath(form.list[index].Group, from, to)
		}
	}
	for index := range form.customGroups {
		if snippetGroupWithin(form.customGroups[index], from) {
			form.customGroups[index] = remapSnippetGroupPath(form.customGroups[index], from, to)
		}
	}
	if form.filter.group != "" && snippetGroupWithin(form.filter.group, from) {
		form.filter.group = remapSnippetGroupPath(form.filter.group, from, to)
	}
	remapCommandGroupMap(state.commands.hiddenGroups, from, to)
	remapCommandGroupMap(state.commands.collapsedGroups, from, to)
	remapCommandGroupMap(state.snippets.treeCollapsedGroups, from, to)
	if state.snippets.selectedFolder != "" && snippetGroupWithin(state.snippets.selectedFolder, from) {
		state.snippets.selectedFolder = remapSnippetGroupPath(state.snippets.selectedFolder, from, to)
	}
	if form.inline.draft && snippetGroupWithin(form.inline.loadedSnap.Group, from) {
		form.inline.loadedSnap.Group = remapSnippetGroupPath(form.inline.loadedSnap.Group, from, to)
	}
	manager.error = ""
	state.refreshSnippets()
	if form.inline.draft {
		state.refreshInlineGroupOptions(form.inline.loadedSnap.Group)
		state.refreshInlineMeta(form.inline.loadedSnap)
	}
	state.save()
	state.status = fmt.Sprintf(m.StatusGroupRenamedFmt, from, to)
	return true
}

// deleteSnippetGroup clears the group from its snippets (and any descendants):
// the commands stay on the page as ungrouped ones. A filter on the deleted
// group or a descendant shows everything again, as it does for a group that is
// gone for any other reason.
func (state *State) deleteSnippetGroup(group string) {
	form, m := &state.snippets, state.messages.Tools
	count := 0
	for index := range form.list {
		if snippetGroupWithin(form.list[index].Group, group) {
			form.list[index].Group = ""
			count++
		}
	}
	removedCustom := false
	kept := form.customGroups[:0]
	for _, custom := range form.customGroups {
		if snippetGroupWithin(custom, group) {
			removedCustom = true
			continue
		}
		kept = append(kept, custom)
	}
	form.customGroups = kept
	if count == 0 && !removedCustom {
		return
	}
	if form.filter.group != "" && snippetGroupWithin(form.filter.group, group) {
		form.filter = snippetFilter{}
	}
	deleteCommandGroupMap(state.commands.hiddenGroups, group)
	deleteCommandGroupMap(state.commands.collapsedGroups, group)
	deleteCommandGroupMap(state.snippets.treeCollapsedGroups, group)
	if state.snippets.selectedFolder != "" && snippetGroupWithin(state.snippets.selectedFolder, group) {
		state.snippets.selectedFolder = ""
	}
	if form.inline.draft && snippetGroupWithin(form.inline.loadedSnap.Group, group) {
		form.inline.loadedSnap.Group = ""
	}
	state.refreshSnippets()
	if form.inline.draft {
		state.refreshInlineGroupOptions(form.inline.loadedSnap.Group)
		state.refreshInlineMeta(form.inline.loadedSnap)
	}
	state.save()
	state.status = fmt.Sprintf(m.StatusGroupDeletedFmt, group, count)
}

// deleteSnippetGroupWithSnippets removes group, its descendant groups, and
// every snippet contained within that subtree.
func (state *State) deleteSnippetGroupWithSnippets(group string) {
	form, m := &state.snippets, state.messages.Tools
	count := 0
	keptList := form.list[:0]
	for _, entry := range form.list {
		if snippetGroupWithin(entry.Group, group) {
			count++
			continue
		}
		keptList = append(keptList, entry)
	}
	form.list = keptList
	removedCustom := false
	keptGroups := form.customGroups[:0]
	for _, custom := range form.customGroups {
		if snippetGroupWithin(custom, group) {
			removedCustom = true
			continue
		}
		keptGroups = append(keptGroups, custom)
	}
	form.customGroups = keptGroups
	if count == 0 && !removedCustom {
		return
	}
	if form.filter.group != "" && snippetGroupWithin(form.filter.group, group) {
		form.filter = snippetFilter{}
	}
	deleteCommandGroupMap(state.commands.hiddenGroups, group)
	deleteCommandGroupMap(state.commands.collapsedGroups, group)
	deleteCommandGroupMap(state.snippets.treeCollapsedGroups, group)
	if state.snippets.selectedFolder != "" && snippetGroupWithin(state.snippets.selectedFolder, group) {
		state.snippets.selectedFolder = ""
	}
	if form.inline.draft && snippetGroupWithin(form.inline.loadedSnap.Group, group) {
		form.inline.loadedSnap.Group = ""
	}
	form.resetSnippetPreview()
	state.refreshSnippets()
	if form.inline.draft {
		state.refreshInlineGroupOptions(form.inline.loadedSnap.Group)
		state.refreshInlineMeta(form.inline.loadedSnap)
	}
	state.save()
	state.status = fmt.Sprintf(m.StatusGroupPurgedFmt, group, count)
}

func remapCommandGroupMap(entries map[string]bool, from, to string) {
	if len(entries) == 0 {
		return
	}
	var updated map[string]bool
	for key, set := range entries {
		if !set || key == "" || !snippetGroupWithin(key, from) {
			continue
		}
		if updated == nil {
			updated = make(map[string]bool)
		}
		updated[strings.ToLower(remapSnippetGroupPath(key, from, to))] = true
		delete(entries, key)
	}
	for key, set := range updated {
		entries[key] = set
	}
}

func deleteCommandGroupMap(entries map[string]bool, group string) {
	for key := range entries {
		if key != "" && snippetGroupWithin(key, group) {
			delete(entries, key)
		}
	}
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
	padLeft := float32(10 + min(int(snippetGroupDepth(group)), 6)*16)
	ui.Row(c).FillWidth().Height(56).Shrink(0).Items(ui.Center).Gap(12).PadSides(0, 14, 0, padLeft).Children(func() {
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

func (state *State) snippetGroupBreadcrumb(path string) string {
	if path == "" {
		return state.messages.Tools.SnippetGroupParentRoot
	}
	return strings.ReplaceAll(path, "/", "  ›  ")
}

// declareSnippetGroupRenameDialog declares the create or rename group dialog:
// creating offers a Parent group selector (rendered with breadcrumb separators)
// above the Group name input; renaming shows the read-only parent breadcrumb
// when inside a parent group and edits only the leaf group name.
func (state *State) declareSnippetGroupRenameDialog(c ui.C) {
	manager, form, m, pal := &state.snippetGroups, &state.snippets, state.messages.Tools, state.pal
	title, saveLabel := m.SnippetGroupRenameTitle, m.SnippetGroupRenameSave
	if manager.creating {
		saveLabel = state.messages.HostGroupActions.Create
		if manager.parent != "" {
			title = state.messages.HostGroupActions.NewChild
		} else {
			title = state.messages.NewGroup
		}
	}
	showParentSelect := manager.creating
	showParentReadOnly := !manager.creating && manager.parent != ""
	var bodyHeight float32 = 90
	if showParentSelect {
		bodyHeight = 166
	} else if showParentReadOnly {
		bodyHeight = 162
	}
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 460, "", bodyHeight)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Width: 460, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitSnippetGroupRenameClose, InitialFocusID: hitSnippetGroupRenameField,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().Gap(10).Children(func() {
			if showParentSelect {
				choices := manager.parentChoices[:0]
				choices = append(choices, "")
				choices = append(choices, form.groups...)
				manager.parentChoices = choices
				ui.FormField(c, &manager.parentField, m.SnippetGroupParentField).FillWidth().Height(66).Children(func() {
					ui.Select(c, &manager.parent, manager.parentChoices, state.snippetGroupBreadcrumb).
						Label(m.SnippetGroupParentField).FillWidth().Height(36)
				})
			} else if showParentReadOnly {
				segments := strings.Split(manager.parent, "/")
				ui.Column(c).FillWidth().Height(62).Gap(6).Children(func() {
					ui.Text(c, m.SnippetGroupParentField).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text)
					ui.Row(c).FillWidth().Height(34).PadSides(0, 10, 0, 10).Gap(6).Items(ui.Center).
						Background(pal.surfaceSoft).Border(1, pal.line).Radius(radius.control).Children(func() {
						ui.Icon(c, icons.FolderSolid).Size(14, 14).Color(pal.muted).Shrink(0)
						for idx, seg := range segments {
							if idx > 0 {
								ui.Icon(c, lucideIcons[iconChevronRight]).Size(12, 12).Color(pal.faint).Shrink(0)
							}
							ui.Text(c, seg).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Ellipsis()
						}
					})
				})
			}
			manager.field.declare(c, state, &manager.editor, dialogFieldSpec{
				ID: hitSnippetGroupRenameField, Label: m.SnippetGroupRenameField,
				Placeholder: m.SnippetGroupNameHint, Error: manager.error, Height: 90,
			})
		})
	})
	dialog.Footer(func() {
		if dialogButton(c, state.messages.Cancel, components.ButtonSecondary, false).Clicked() {
			c.After(func() { state.clickSnippetGroupDialog(hitSnippetGroupRenameCancel) })
		}
		if dialogButton(c, saveLabel, components.ButtonDefault, false).Clicked() {
			c.After(func() { state.clickSnippetGroupDialog(hitSnippetGroupRenameSubmit) })
		}
	})
}

// declareSnippetGroupDeleteDialog asks before deleting a group, letting the
// user choose whether to keep its commands ungrouped (default) or delete them
// together with the group.
func (state *State) declareSnippetGroupDeleteDialog(c ui.C) {
	manager, m := &state.snippetGroups, state.messages.Tools
	count := state.snippetGroupCount(manager.target)
	description := fmt.Sprintf(m.SnippetGroupDeleteBodyFmt, manager.target, count)
	if manager.deleteSnippets {
		description = fmt.Sprintf(m.SnippetGroupPurgeBodyFmt, manager.target, count)
	}
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 480, description, groupDeleteChoiceRows)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.SnippetGroupDeleteTitle, Description: description,
		Width: 480, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitSnippetGroupDeleteDismiss, CloseID: hitSnippetGroupDeleteClose,
		InitialFocusID: hitSnippetGroupDeleteKeep,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().Children(func() {
			ui.RadioGroup(c).Fill().Children(func() {
				ui.Radio(c, &manager.deleteSnippets, false, m.SnippetGroupDeleteKeep, ui.WithID(hitSnippetGroupDeleteKeep)).Grow(1)
				ui.Radio(c, &manager.deleteSnippets, true, m.SnippetGroupDeletePurge, ui.WithID(hitSnippetGroupDeletePurge)).Grow(1)
			})
		})
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitSnippetGroupDeleteCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitSnippetGroupDeleteConfirm, Label: m.SnippetGroupDeleteConfirm, Variant: components.ButtonDestructive},
		)
	})
}
