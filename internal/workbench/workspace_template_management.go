package workbench

import (
	"fmt"
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

var (
	hitWorkspaceTemplateMenuDismiss  = tree.ID(workspaceHits, "template-menu-dismiss")
	hitWorkspaceTemplateMenuEdit     = tree.ID(workspaceHits, "template-menu-edit")
	hitWorkspaceTemplateMenuDelete   = tree.ID(workspaceHits, "template-menu-delete")
	hitWorkspaceTemplateDeleteClose  = tree.ID(workspaceHits, "template-delete-close")
	hitWorkspaceTemplateDeleteCancel = tree.ID(workspaceHits, "template-delete-cancel")
	hitWorkspaceTemplateDeleteSubmit = tree.ID(workspaceHits, "template-delete-submit")
)

type workspaceTemplateMenu struct {
	open       bool
	templateID string
	at         gfx.Point
}

type workspaceTemplateDelete struct {
	templateID string
	name       string
	error      string
}

func (state *State) workspaceTemplateByID(id string) (WorkspaceTemplate, int, bool) {
	for index, template := range state.templates {
		if template.ID == id {
			return template, index, true
		}
	}
	return WorkspaceTemplate{}, -1, false
}

func (state *State) openWorkspaceTemplateMenu(templateID string, at gfx.Point) {
	state.workspaceTemplateMenu = workspaceTemplateMenu{open: true, templateID: templateID, at: at}
	state.workspaceTemplateFocusPending = hitWorkspaceTemplateMenuEdit
}

func (state *State) workspaceTemplateMenuKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if !state.workspaceTemplateMenu.open || event.Release {
		return gfx.EventResult{}, false
	}
	scroll := state.openMenuScroll()
	if scroll == nil {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEscape {
		templateID := state.workspaceTemplateMenu.templateID
		state.workspaceTemplateMenu.open = false
		state.workspaceTemplateFocusPending = state.workspaceTemplateLauncherFocus(templateID)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if event.Target != hitWorkspaceTemplateMenuEdit && event.Target != hitWorkspaceTemplateMenuDelete {
		return gfx.EventResult{}, false
	}
	switch event.Key {
	case gfx.KeyEnter, gfx.KeySpace:
		if event.Repeat {
			return gfx.EventResult{Handled: true}, true
		}
		state.clickWorkspaceTemplateManagement(event.Target)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	id, ok := scroll.Navigate(event.Key, event.Target, true)
	if !ok {
		return gfx.EventResult{}, false
	}
	return gfx.EventResult{Handled: true, Repaint: true, Focus: id, FocusVisible: true}, true
}

func (state *State) workspaceTemplateLauncherKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.activeSection != sectionWorkspaces || state.modal != modalNone || event.Release ||
		event.Repeat || event.Modifiers != 0 ||
		(event.Key != gfx.KeyEnter && event.Key != gfx.KeySpace) {
		return gfx.EventResult{}, false
	}
	index, ok := event.Target.In(hitWorkspaceTemplateMore)
	if !ok || index < 0 || index >= len(state.templates) {
		return gfx.EventResult{}, false
	}
	state.clickWorkspaceTemplateManagement(event.Target)
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) clickWorkspaceTemplateManagement(id gfx.ID) bool {
	menu := &state.workspaceTemplateMenu
	if menu.open {
		template, index, exists := state.workspaceTemplateByID(menu.templateID)
		menu.open = false
		state.workspaceTemplateFocusPending = gfx.ID{}
		if state.modal != modalNone || state.activeSection != sectionWorkspaces || !exists {
			return true
		}
		switch id {
		case hitWorkspaceTemplateMenuEdit:
			state.openWorkspaceDraftEdit(template.ID)
		case hitWorkspaceTemplateMenuDelete:
			state.openWorkspaceTemplateDelete(template.ID)
		case hitWorkspaceTemplateMenuDismiss:
			state.workspaceTemplateFocusPending = hitWorkspaceTemplateMore.At(index)
		}
		return true
	}
	if id.Scope() != hitWorkspaceTemplateMore || state.activeSection != sectionWorkspaces || state.modal != modalNone {
		return false
	}
	index, ok := id.In(hitWorkspaceTemplateMore)
	if !ok || index < 0 || index >= len(state.templates) {
		state.workspaceTemplateFocusPending = gfx.ID{}
		return true
	}
	state.openWorkspaceTemplateMenu(state.templates[index].ID, state.workspaceTemplateMenuAnchor(index))
	return true
}

func (state *State) workspaceTemplateMenuAnchor(index int) gfx.Point {
	if index < 0 || index >= len(state.workspaceTemplateMoreBounds) {
		return gfx.Point{X: state.workspaceLibraryBounds.X + max(0, state.workspaceLibraryBounds.Width-40), Y: state.workspaceLibraryBounds.Y + 30}
	}
	bounds := state.workspaceTemplateMoreBounds[index]
	return gfx.Point{X: bounds.X + bounds.Width, Y: bounds.Y + bounds.Height}
}

func (state *State) paintWorkspaceTemplateMenu(canvas *gfx.Canvas, viewport gfx.Rect) {
	menu := &state.workspaceTemplateMenu
	if !menu.open {
		return
	}
	if state.activeSection != sectionWorkspaces || state.modal != modalNone {
		menu.open = false
		state.workspaceTemplateFocusPending = gfx.ID{}
		return
	}
	if _, _, exists := state.workspaceTemplateByID(menu.templateID); !exists {
		menu.open = false
		state.workspaceTemplateFocusPending = state.workspaceTemplateFocusAfterDelete(menu.templateID)
		return
	}
	var rows menuRows
	rows.add(components.ContextMenuItem{
		ID: hitWorkspaceTemplateMenuEdit, Label: state.messages.WorkspaceTemplateEdit,
		Icon: lucideIcons[iconPencil],
	})
	rows.separate()
	rows.add(components.ContextMenuItem{
		ID: hitWorkspaceTemplateMenuDelete, Label: state.messages.WorkspaceTemplateDelete,
		Icon: lucideIcons[iconTrash], Variant: components.ContextMenuItemDestructive,
	})
	width, height := rows.size(canvas, state.contextMenuTheme())
	bounds := state.popupAt(canvas, viewport, menu.at, width, height, 6)
	dismiss := state.menuScrollFor(hitWorkspaceTemplateMenuDismiss)
	popup := components.BeginContextMenu(canvas, state.contextMenuTheme(), bounds, components.ContextMenuOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitWorkspaceTemplateMenuDismiss,
		Viewport: viewport, Scroll: dismiss, RowHeight: contextMenuRow,
	})
	state.menuPlaced(popup.Bounds())
	rows.paint(&popup, nil)
	popup.End()
}

func (state *State) openWorkspaceTemplateDelete(templateID string) {
	template, _, ok := state.workspaceTemplateByID(templateID)
	if !ok {
		return
	}
	state.selectedWorkspaceTemplateID = template.ID
	state.workspaceLibraryExpanded = true
	state.workspaceTemplateDelete = workspaceTemplateDelete{templateID: template.ID, name: template.Name}
	state.modal = modalWorkspaceTemplateDelete
	state.workspaceTemplateFocusPending = gfx.ID{}
}

func (state *State) paintWorkspaceTemplateDelete(canvas *gfx.Canvas, viewport gfx.Rect) {
	target := &state.workspaceTemplateDelete
	m := state.messages
	body := float32(0)
	if target.error != "" {
		body = 20
	}
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitWorkspaceTemplateDeleteClose, CloseID: hitWorkspaceTemplateDeleteClose,
		InitialFocusID: hitWorkspaceTemplateDeleteCancel,
		ContentSize:    gfx.Size{Width: 520}, FitBody: true, BodyHeight: body,
	})
	layout := dialog.Begin(m.WorkspaceTemplateDeleteTitle, fmt.Sprintf(
		m.WorkspaceTemplateDeleteDescriptionFmt, target.name))
	if target.error != "" {
		f := state.chrome.dialog.Begin(canvas, state.Theme(), layout.Body)
		f.Column().Fill().Children(func() {
			f.Text(target.error).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).
				Color(state.pal.danger).LineHeight(20)
		})
		f.End()
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitWorkspaceTemplateDeleteCancel, Label: m.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitWorkspaceTemplateDeleteSubmit, Label: m.WorkspaceTemplateDeleteConfirm, Variant: components.ButtonDestructive},
	)
	dialog.End()
}

func (state *State) clickWorkspaceTemplateDelete(id gfx.ID) {
	switch id {
	case hitWorkspaceTemplateDeleteClose, hitWorkspaceTemplateDeleteCancel:
		state.workspaceTemplateFocusPending = state.workspaceTemplateLauncherFocus(state.workspaceTemplateDelete.templateID)
		state.workspaceTemplateDelete = workspaceTemplateDelete{}
		state.modal = modalNone
	case hitWorkspaceTemplateDeleteSubmit:
		state.submitWorkspaceTemplateDelete()
	}
}

func (state *State) workspaceTemplateDeleteKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalWorkspaceTemplateDelete || event.Release {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEscape {
		state.workspaceTemplateFocusPending = state.workspaceTemplateLauncherFocus(state.workspaceTemplateDelete.templateID)
		state.workspaceTemplateDelete = workspaceTemplateDelete{}
		state.modal = modalNone
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if event.Key == gfx.KeyTab {
		next := hitWorkspaceTemplateDeleteSubmit
		if event.Target == hitWorkspaceTemplateDeleteSubmit {
			next = hitWorkspaceTemplateDeleteCancel
		}
		if event.Modifiers&gfx.ModifierShift != 0 {
			next = hitWorkspaceTemplateDeleteSubmit
			if event.Target == hitWorkspaceTemplateDeleteSubmit {
				next = hitWorkspaceTemplateDeleteCancel
			}
		}
		return gfx.EventResult{Handled: true, Repaint: true, Focus: next, FocusVisible: true}, true
	}
	if (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && !event.Repeat && event.Modifiers == 0 &&
		(event.Target == hitWorkspaceTemplateDeleteCancel || event.Target == hitWorkspaceTemplateDeleteSubmit) {
		state.clickWorkspaceTemplateDelete(event.Target)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

func (state *State) submitWorkspaceTemplateDelete() {
	target := &state.workspaceTemplateDelete
	template, index, ok := state.workspaceTemplateByID(target.templateID)
	if !ok {
		state.workspaceTemplateDelete = workspaceTemplateDelete{}
		state.modal = modalNone
		state.workspaceTemplateFocusPending = state.workspaceTemplateFocusAfterDelete("")
		state.showActionWarning(state.messages.WorkspaceLibraryTitle, state.messages.WorkspaceTemplateDeleteMissing)
		return
	}

	previousTemplates := cloneWorkspaceTemplates(state.templates)
	previousSelected := state.selectedWorkspaceTemplateID
	previousExpanded := state.workspaceLibraryExpanded
	state.templates = slices.Delete(state.templates, index, index+1)
	switch {
	case len(state.templates) == 0:
		state.selectedWorkspaceTemplateID = ""
		state.workspaceLibraryExpanded = false
	case index < len(state.templates):
		state.selectedWorkspaceTemplateID = state.templates[index].ID
		state.workspaceLibraryExpanded = true
	default:
		state.selectedWorkspaceTemplateID = state.templates[index-1].ID
		state.workspaceLibraryExpanded = true
	}
	state.workspaceTemplateMenu = workspaceTemplateMenu{}
	state.workspaceCardDrag = workspaceCardDrag{}
	if !state.saveResult() {
		state.templates = previousTemplates
		state.selectedWorkspaceTemplateID = previousSelected
		state.workspaceLibraryExpanded = previousExpanded
		target.error = state.messages.WorkspaceTemplateDeleteFailed
		return
	}
	state.workspaceTemplateFocusPending = state.workspaceTemplateFocusAfterDelete(template.ID)
	state.workspaceTemplateDelete = workspaceTemplateDelete{}
	state.modal = modalNone
	state.showActionInfo(
		state.messages.WorkspaceLibraryTitle,
		fmt.Sprintf(state.messages.WorkspaceTemplateDeletedFmt, template.Name),
	)
}

func (state *State) workspaceTemplateFocusAfterDelete(templateID string) gfx.ID {
	if len(state.templates) == 0 {
		return hitWorkspaceNew
	}
	if templateID == "" {
		if index := state.selectedWorkspaceTemplateIndex(); index >= 0 {
			return hitWorkspaceTemplateMore.At(index)
		}
		return hitWorkspaceNew
	}
	if index := slices.IndexFunc(state.templates, func(template WorkspaceTemplate) bool {
		return template.ID == templateID
	}); index >= 0 {
		return hitWorkspaceTemplateMore.At(index)
	}
	if index := state.selectedWorkspaceTemplateIndex(); index >= 0 {
		return hitWorkspaceTemplateMore.At(index)
	}
	return hitWorkspaceNew
}
