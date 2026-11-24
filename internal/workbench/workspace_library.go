package workbench

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var workspaceHits = tree.Feature("workspaces")

var (
	hitWorkspaceTemplateRowList       = tree.Sub(workspaceHits, tree.Name("template-row"))
	hitWorkspaceTemplateOpen          = tree.Sub(workspaceHits, tree.Name("template-open"))
	hitWorkspaceTemplateMore          = tree.Sub(workspaceHits, tree.Name("template-more"))
	hitWorkspaceLibraryScroll         = tree.ID(workspaceHits, "library-scroll")
	hitWorkspaceNew                   = tree.ID(workspaceHits, "new")
	hitWorkspaceDraftDismiss          = tree.ID(workspaceHits, "draft-dismiss")
	hitWorkspaceDraftCancel           = tree.ID(workspaceHits, "draft-cancel")
	hitWorkspaceDraftSubmit           = tree.ID(workspaceHits, "draft-submit")
	hitWorkspaceDraftName             = tree.ID(workspaceHits, "draft-name")
	hitWorkspaceDraftSearch           = tree.ID(workspaceHits, "draft-search")
	hitWorkspaceDraftHostList         = tree.Sub(workspaceHits, tree.Name("draft-host"))
	hitWorkspaceDraftGroupList        = tree.Sub(workspaceHits, tree.Name("draft-group"))
	hitWorkspaceDraftGroupCheckList   = tree.Sub(workspaceHits, tree.Name("draft-group-check"))
	hitWorkspaceDraftScroll           = tree.ID(workspaceHits, "draft-scroll")
	hitWorkspaceDraftScrollbar        = tree.ID(workspaceHits, "draft-scrollbar")
	hitWorkspaceDraftPreviewScroll    = tree.ID(workspaceHits, "draft-preview-scroll")
	hitWorkspaceDraftPreviewScrollbar = tree.ID(workspaceHits, "draft-preview-scrollbar")
	hitWorkspaceDraftRemoveItem       = tree.Sub(workspaceHits, tree.Name("draft-remove-item"))
)

const (
	workspaceLibraryRowGap           = float32(10)
	workspaceLibraryHeaderHeight     = float32(76)
	workspaceLibraryBottomInset      = float32(12)
	workspaceLibraryScrollbarReserve = float32(10)
)

// clickWorkspaceLibrary expands a template, opens its action, or creates a new
// workspace.
// It reports whether id belonged to the library.
func (state *State) clickWorkspaceLibrary(id gfx.ID) bool {
	if state.activeSection != sectionWorkspaces || state.modal != modalNone {
		return false
	}
	switch {
	case id.Scope() == hitWorkspaceTemplateOpen:
		index, ok := id.In(hitWorkspaceTemplateOpen)
		if ok && index >= 0 && index < len(state.templates) {
			state.selectedWorkspaceTemplateID = state.templates[index].ID
			state.workspaceLibraryExpanded = true
			state.applySelectedWorkspaceTemplate()
		}
	case id.Scope() == hitWorkspaceTemplateMore:
		state.clickWorkspaceTemplateManagement(id)
	case id == hitWorkspaceNew:
		state.openWorkspaceDraft(workspaceDraftTemporary)
	case id.Scope() == hitWorkspaceTemplateRowList:
		index, ok := id.In(hitWorkspaceTemplateRowList)
		if ok && index >= 0 && index < len(state.templates) {
			templateID := state.templates[index].ID
			if state.selectedWorkspaceTemplateID == templateID {
				state.workspaceLibraryExpanded = !state.workspaceLibraryExpanded
			} else {
				state.selectedWorkspaceTemplateID = templateID
				state.workspaceLibraryExpanded = true
			}
		}
	default:
		return false
	}
	return true
}

// workspaceLibraryPointer opens the template hit by the first press of a
// double click, before the row's single-click expansion state can replace the
// pointer target.
func (state *State) workspaceLibraryPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if state.activeSection != sectionWorkspaces || state.modal != modalNone ||
		event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonPrimary {
		return gfx.EventResult{}, false
	}
	if event.ClickCount > 1 {
		templateID := state.workspaceLibraryPointerID
		state.workspaceLibraryPointerID = ""
		if templateID == "" || event.Target.Scope() != hitWorkspaceTemplateRowList {
			return gfx.EventResult{}, false
		}
		state.selectedWorkspaceTemplateID = templateID
		state.applySelectedWorkspaceTemplate()
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	state.workspaceLibraryPointerID = ""
	if index, ok := event.Target.In(hitWorkspaceTemplateRowList); ok &&
		index >= 0 && index < len(state.templates) {
		state.workspaceLibraryPointerID = state.templates[index].ID
	}
	return gfx.EventResult{}, false
}

// workspaceLibraryKey opens the selected template from a row or the page while
// the library is showing. The library owns Enter only when no other editor or
// popup has already claimed it.
func (state *State) workspaceLibraryKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.activeSection != sectionWorkspaces || state.modal != modalNone || event.Release ||
		event.Key != gfx.KeyEnter || event.Repeat || event.Modifiers != 0 {
		return gfx.EventResult{}, false
	}
	if event.Target != (gfx.ID{}) && event.Target.Scope() != hitWorkspaceTemplateRowList {
		return gfx.EventResult{}, false
	}
	index := state.selectedWorkspaceTemplateIndex()
	if index < 0 {
		return gfx.EventResult{}, false
	}
	if event.Target != (gfx.ID{}) {
		row, ok := event.Target.In(hitWorkspaceTemplateRowList)
		if !ok || row < 0 || row >= len(state.templates) {
			return gfx.EventResult{}, false
		}
		state.selectedWorkspaceTemplateID = state.templates[row].ID
	}
	state.applySelectedWorkspaceTemplate()
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

// applySelectedWorkspaceTemplate resolves the saved host references through the
// same launch planner as every other workspace. A partial launch proceeds and
// reports the references it skipped; an empty one changes no tab or session.
func (state *State) applySelectedWorkspaceTemplate() {
	index := state.selectedWorkspaceTemplateIndex()
	if index < 0 {
		return
	}
	template := state.templates[index]
	plan, err := PlanWorkspaceLaunch(state.workspaceLaunchCatalog(template.HostIDs), template.HostIDs, template.Layout)
	if err != nil {
		detail := state.messages.WorkspaceTemplateApplyFailed
		if err == ErrWorkspaceLaunchEmpty && len(plan.Missing) > 0 {
			detail = state.messages.WorkspaceTemplateNoHosts
		}
		state.showActionWarning(template.Name, detail)
		return
	}
	plan.Commands = template.Commands
	plan = orderedWorkspaceLaunch(plan, template.HostIDs)
	workspaceID, _, err := state.launchWorkspaceWithIdentity(plan, template.Name, workspaceLaunchIdentity{
		managedLayout: template.Layout,
	})
	if err != nil {
		state.showActionWarning(template.Name, state.messages.WorkspaceTemplateApplyFailed)
		return
	}
	state.fitWorkspaceTerminals(workspaceID)
	if len(plan.Missing) > 0 {
		state.showActionWarning(template.Name,
			fmt.Sprintf(state.messages.WorkspaceTemplateMissingListFmt, strings.Join(plan.Missing, ", ")))
	}
}

// selectedWorkspaceTemplateIndex keeps the selection valid as templates load
// or change. The library defaults to its first row without persisting that
// choice.
func (state *State) selectedWorkspaceTemplateIndex() int {
	if len(state.templates) == 0 {
		state.selectedWorkspaceTemplateID = ""
		state.workspaceLibraryExpanded = false
		return -1
	}
	for index := range state.templates {
		if state.templates[index].ID == state.selectedWorkspaceTemplateID {
			return index
		}
	}
	state.selectedWorkspaceTemplateID = state.templates[0].ID
	state.workspaceLibraryExpanded = true
	return 0
}

func (state *State) expandedWorkspaceTemplateIndex() int {
	index := state.selectedWorkspaceTemplateIndex()
	if !state.workspaceLibraryExpanded {
		return -1
	}
	return index
}

// workspaceLibrarySectionUI is what the workspace library's declaration leaves
// for its paint.
type workspaceLibrarySectionUI struct {
	create, empty, scroll ui.Box
	newWidth              float32
	newIcon               uint8
	condensed             bool
	contentWidth          float32
	contentHeight         float32
	// emptyDraw paints the icon of the reserved empty page once it is laid out.
	// It is kept so a paint does not build a closure for the hook.
	emptyDraw func(*ui.DrawContext)
}

// declareWorkspaceLibrary declares the workspace library in the workbench page:
// its header with the create button, its rows or empty state, and its divider.
func (state *State) declareWorkspaceLibrary(c ui.C) {
	m, pal := state.messages, state.pal
	bounds := state.catalogSectionBounds()
	newWidth, newIcon := toolbarButtonLayout(c.Canvas(), m.WorkspaceNew, iconPlus, iconPlus, bounds.Width < 520)
	view := &state.sectionBodies.workspaceLibrary
	*view = workspaceLibrarySectionUI{newWidth: newWidth, newIcon: newIcon, condensed: bounds.Width < 520}
	padding := float32(22)
	if bounds.Width < 340 {
		padding = 12
	}
	body := ui.Column(c).Fill().PadSides(18, padding, 0, padding)
	body.Children(func() {
		if bounds.Width < 520 {
			ui.Text(c, m.WorkspaceLibraryTitle).FontSize(20).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(28).Ellipsis()
			ui.Row(c).Gap(8).MarginSides(12, 0, 20, 0).Children(func() {
				view.create = state.declareButtonSized(c, hitWorkspaceNew, m.WorkspaceNew, newIcon, false, false, true, toolbarFont, bounds.Width < 520).Size(newWidth, toolbarHeight)
			})
		} else {
			ui.Row(c).MinHeight(74).Gap(10).MarginSides(0, 0, 14, 0).Items(ui.Start).Children(func() {
				ui.Column(c).Grow(1).Gap(6).Children(func() {
					ui.Text(c, m.WorkspaceLibraryTitle).FontSize(20).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(28).Ellipsis()
					ui.Paragraph(c, m.WorkspaceLibraryDescription, 2).FontSize(12).LineHeight(19).Color(pal.muted)
				})
				view.create = state.declareButtonSized(c, hitWorkspaceNew, m.WorkspaceNew, newIcon, false, false, true, toolbarFont, bounds.Width < 520).Size(newWidth, toolbarHeight)
			})
		}
		if len(state.templates) == 0 {
			view.empty = state.reserveEmptyPage(c, bounds, m.WorkspaceLibraryEmpty, m.WorkspaceLibraryEmptySub, m.WorkspaceNew, hitWorkspaceNew, iconColumns2, true)
			if view.emptyDraw == nil {
				view.emptyDraw = func(d *ui.DrawContext) { state.drawReservedEmptyPage(d.Canvas, view.empty, iconColumns2) }
			}
		} else {
			state.declareWorkspaceLibraryList(c, view, bounds, padding)
		}
		ui.Divider(c).Background(pal.line)
	})
	if len(state.templates) == 0 {
		body.Clip().DrawOver(view.emptyDraw)
	}
}

// declareWorkspaceLibraryList declares the template rows in a page scroll area:
// the rows and their cards are painted into a slot, while their open buttons and
// the inline command editor are page controls over them.
func (state *State) declareWorkspaceLibraryList(c ui.C, view *workspaceLibrarySectionUI, bounds gfx.Rect, padding float32) {
	contentWidth := max(0, bounds.Width-2*padding-workspaceLibraryScrollbarReserve)
	rows, contentHeight := state.workspaceLibraryRowLayout(contentWidth, state.expandedWorkspaceTemplateIndex())
	view.contentWidth, view.contentHeight = contentWidth, contentHeight
	scroll := ui.Scroll(c).Grow(1).MarginSides(0, 0, 14, 0)
	view.scroll = scroll
	area := scroll.ScrollState()
	if state.workspaceLibraryScroll != state.workspaceLibraryScrollSynced {
		area.ScrollTo(state.workspaceLibraryScroll)
		state.workspaceLibraryScrollSynced = state.workspaceLibraryScroll
		c.Canvas().RequestFrame()
	}
	scroll.Children(func() {
		content := ui.Stack(c).FillWidth().Height(contentHeight)
		content.Children(func() {
			ui.Slot(c).Absolute().Inset(0).Draw(func(d *ui.DrawContext) {
				state.drawWorkspaceLibraryRows(d.Canvas, d.Rect, contentWidth, contentHeight)
			})
			state.declareWorkspaceLibraryRowActions(c, contentWidth, rows)
			state.declareWorkspaceInlineEditor(c, contentWidth, rows)
		})
	})
	scroll.DrawOver(func(d *ui.DrawContext) {
		state.workspaceLibraryBounds = d.Rect
		state.workspaceLibraryContent = contentHeight
		state.workspaceLibraryScroll = area.Offset
		state.workspaceLibraryScrollSynced = area.Offset
	})
}

// workspaceLibraryOpenWidth is how wide a row's Enter button is: the small
// button's padding and icon around its label, and 28 for the compact
// one-icon form.
func workspaceLibraryOpenWidth(canvas *gfx.Canvas, label string, compact bool) float32 {
	if compact {
		return 28
	}
	return canvas.TextWidthWeight(label, metrics.FontSizeM, gfx.WeightMedium) + 44
}

// workspaceLibraryRowButtons is where a row's open and more buttons go.
func workspaceLibraryRowButtons(bounds gfx.Rect, openWidth float32) (open, more gfx.Rect) {
	more = gfx.Rect{X: bounds.X + bounds.Width - 16 - 28, Y: bounds.Y + 22, Width: 28, Height: 32}
	open = gfx.Rect{X: more.X - 8 - openWidth, Y: bounds.Y + 22, Width: openWidth, Height: 32}
	return open, more
}

// declareWorkspaceLibraryRowActions declares the Enter button of every template
// row; the more button stays painted with the rest of the row.
func (state *State) declareWorkspaceLibraryRowActions(c ui.C, contentWidth float32, rows []workspaceLibraryRow) {
	m := state.messages
	for _, row := range rows {
		bounds := gfx.Rect{Width: contentWidth, Height: row.bounds.Height, Y: row.bounds.Y}
		compact := bounds.Width < 340
		openBounds, _ := workspaceLibraryRowButtons(bounds, workspaceLibraryOpenWidth(c.Canvas(), m.WorkspaceEnter, compact))
		button := ui.Button(c, m.WorkspaceEnter).Small().Icon(lucideIcons[iconPlay]).
			Hit(hitWorkspaceTemplateOpen.At(row.index), m.WorkspaceEnter).
			Absolute().Left(openBounds.X).Top(openBounds.Y)
		if compact {
			button.IconOnly().Size(28, 28)
		} else {
			button.Size(openBounds.Width, 28)
		}
	}
}

// declareWorkspaceInlineEditor declares the command editor of the template card
// it was opened on, over the cards in the scroll content.
func (state *State) declareWorkspaceInlineEditor(c ui.C, contentWidth float32, rows []workspaceLibraryRow) {
	edit, m := &state.workspaceCommandEdit, state.messages
	if edit.template == "" {
		return
	}
	templateIndex := slices.IndexFunc(state.templates, func(template WorkspaceTemplate) bool {
		return template.ID == edit.template
	})
	if templateIndex < 0 || templateIndex >= len(rows) || !rows[templateIndex].expanded {
		return
	}
	template := state.templates[templateIndex]
	hostIndex := slices.Index(template.HostIDs, edit.host)
	cells := workspaceLayoutCells(len(template.HostIDs), template.Layout)
	if hostIndex < 0 || hostIndex >= len(cells) {
		return
	}
	row := rows[templateIndex].bounds
	bounds := gfx.Rect{Width: contentWidth, Height: row.Height, Y: row.Y}
	panel := gfx.Rect{X: bounds.X + 12, Y: bounds.Y + workspaceLibraryHeaderHeight, Width: max(0, bounds.Width-24)}
	content := gfx.Rect{X: panel.X, Y: panel.Y + workspaceLibraryLayoutHeight(bounds.Width), Width: panel.Width, Height: workspaceTemplateCardHeight(template)}
	box := pixelBounds(content, cells[hostIndex]).Inset(gfx.All(4))
	editor := gfx.Rect{X: box.X + 10, Y: box.Y + 88, Width: max(0, box.Width-20), Height: 52}
	edit.field.Invalid = edit.error != ""
	buttons := min(float32(116), editor.Width*.45)
	ui.Row(c).Absolute().Left(editor.X).Top(editor.Y).Width(editor.Width).Height(52).Gap(8).Items(ui.Start).Children(func() {
		ui.TextFieldID(c, hitWorkspaceInlineField, &edit.field, m.WorkspaceCommandField, "").Height(32).Grow(1)
		ui.Row(c).Width(buttons).Height(26).Shrink(0).MarginTop(3).Gap(8).Items(ui.Center).Justify(ui.End).Children(func() {
			ui.Button(c, m.WorkspaceCommandSave).XSmall().Outline().Shrink(0).
				Hit(hitWorkspaceInlineSave, m.WorkspaceCommandSave)
			ui.Button(c, m.Cancel).XSmall().Secondary().Shrink(0).
				Hit(hitWorkspaceInlineCancel, m.Cancel)
		})
	})
}

type workspaceLibraryRow struct {
	index    int
	expanded bool
	bounds   gfx.Rect
}

func (state *State) drawWorkspaceLibraryRows(canvas *gfx.Canvas, content gfx.Rect, contentWidth, contentHeight float32) {
	if contentWidth <= 0 || contentHeight <= 0 {
		return
	}
	rows, _ := state.workspaceLibraryRowLayout(contentWidth, state.expandedWorkspaceTemplateIndex())
	// The cards are painted from this paint's slot: give them the content rect
	// as the viewport so the whole grid is drawn, not only what the previous
	// paint's scroll bounds said. The rows themselves are culled to the scroll
	// area's viewport, as the hand-painted list culled them.
	viewport := state.workspaceLibraryBounds
	if viewport.Width <= 0 || viewport.Height <= 0 {
		viewport = content
	}
	state.workspaceLibraryBounds, state.workspaceLibraryContent = content, contentHeight
	state.workspaceTemplateMoreBounds = state.workspaceTemplateMoreBounds[:0]
	for _, row := range rows {
		rect := row.bounds
		rect.X += content.X
		rect.Y += content.Y
		if rect.Y+rect.Height < viewport.Y || rect.Y >= viewport.Y+viewport.Height {
			continue
		}
		template := state.templates[row.index]
		state.paintWorkspaceLibraryRow(canvas, rect, row.index, template, row.expanded)
	}
}

func (state *State) workspaceLibraryRowLayout(width float32, expandedIndex int) ([]workspaceLibraryRow, float32) {
	width = max(0, width)
	rows := make([]workspaceLibraryRow, 0, len(state.templates))
	y := float32(0)
	for index := range state.templates {
		if index > 0 {
			y += workspaceLibraryRowGap
		}
		expanded := index == expandedIndex
		template := state.templates[index]
		height := workspaceLibraryRowHeight(template, width, expanded)
		rows = append(rows, workspaceLibraryRow{
			index: index, expanded: expanded,
			bounds: gfx.Rect{
				X: 0, Y: y, Width: width, Height: height,
			},
		})
		y += height
	}
	return rows, y
}

// workspaceLibraryLayoutHeight is the room above a template's cards for its
// strip of layouts, at the row's width.
func workspaceLibraryLayoutHeight(width float32) float32 {
	return workspacePresetStripHeight(max(0, width-32)) + 14
}

func workspaceLibraryRowHeight(template WorkspaceTemplate, width float32, expanded bool) float32 {
	if !expanded {
		return workspaceLibraryHeaderHeight
	}
	return workspaceLibraryHeaderHeight + workspaceLibraryLayoutHeight(width) + workspaceTemplateCardHeight(template) + workspaceLibraryBottomInset
}

func (state *State) paintWorkspaceLibraryRow(canvas *gfx.Canvas, bounds gfx.Rect, index int,
	template WorkspaceTemplate, expanded bool) {
	pal, m := state.pal, state.messages
	rowID := hitWorkspaceTemplateRowList.At(index)
	headerBounds := bounds
	headerBounds.Height = min(headerBounds.Height, workspaceLibraryHeaderHeight)
	hovered := canvas.Hovered() == rowID
	pressed := canvas.Pressed() == rowID
	focused := canvas.FocusVisible() == rowID
	background, border := pal.surface, pal.line
	switch {
	case pressed:
		background, border = pal.surfaceHot, pal.windowStroke
	case hovered:
		background, border = pal.surfaceSoft, pal.windowStroke
	case expanded:
		border = pal.windowStroke
	}
	canvas.RoundedBorder(bounds, radius.control, 1, border, background)
	if focused {
		canvas.RoundedBorder(bounds, radius.control, 1.5, pal.accent, background)
	}

	hostLabels := make([]string, 0, len(template.HostIDs))
	missing := 0
	for _, catalogID := range template.HostIDs {
		if host, ok := state.workspaceHostByID(catalogID); ok {
			hostLabels = append(hostLabels, host.Label)
		} else {
			hostLabels = append(hostLabels, catalogID)
			missing++
		}
	}
	hostCount := fmt.Sprintf(m.WorkspaceTemplateHostCountFmt, len(template.HostIDs))
	if len(template.HostIDs) == 1 {
		hostCount = m.WorkspaceSingleHost
	}
	label := template.Name + ", " + hostCount + ", " + state.workspacePresetLabel(template.Layout)
	if len(hostLabels) > 0 {
		label += ", " + strings.Join(hostLabels, ", ")
	}
	canvas.HitWithOptions(rowID, headerBounds, label, gfx.HitOptions{
		Role: gfx.RoleButton, Hint: m.WorkspaceOpenHint,
		Selected: expanded, Expanded: expanded, Expandable: true,
	})

	chevronColor := pal.muted
	if hovered || focused || expanded {
		chevronColor = pal.text
	}
	target := float32(0)
	if expanded {
		target = float32(math.Pi)
	}
	transform := gfx.IconTransform{Rotate: canvas.Animate(rowID, 0, target, gfx.SpringMotion(220*time.Millisecond, 0))}
	canvas.DrawIconTransformed(lucideIcons[iconChevronDown], gfx.Rect{
		X: bounds.X + 16, Y: bounds.Y + 30, Width: 16, Height: 16,
	}, chevronColor, transform)

	compact := bounds.Width < 340
	openBounds, moreBounds := workspaceLibraryRowButtons(bounds, workspaceLibraryOpenWidth(canvas, m.WorkspaceEnter, compact))
	for len(state.workspaceTemplateMoreBounds) <= index {
		state.workspaceTemplateMoreBounds = append(state.workspaceTemplateMoreBounds, gfx.Rect{})
	}
	state.workspaceTemplateMoreBounds[index] = moreBounds
	state.paintIconButton(canvas, hitWorkspaceTemplateMore.At(index), iconEllipsis, moreBounds,
		m.WorkspaceTemplateActions, false, iconButtonQuiet, background)
	titleX := bounds.X + 48
	titleWidth := max(0, openBounds.X-titleX-12)
	canvas.Label(elide(canvas, template.Name, 16, gfx.WeightSemibold, titleWidth), gfx.Rect{X: titleX, Y: bounds.Y + 11, Width: titleWidth, Height: 24}, gfx.TextStyle{Size: 16, Weight: gfx.WeightSemibold, Color: pal.text})
	meta, metaColor := hostCount, pal.muted
	if missing > 0 {
		meta += " · " + m.WorkspaceTemplateMissingFmt
		metaColor = state.workspaceStatusInk(pal.warning)
	}
	canvas.Label(elide(canvas, meta, 12, gfx.WeightRegular, titleWidth), gfx.Rect{X: titleX, Y: bounds.Y + 39, Width: titleWidth, Height: 20}, gfx.TextStyle{Size: 12, Color: metaColor})
	// The row's Enter button is a page control declared over the painted row.

	if !expanded {
		return
	}

	state.paintWorkspaceLibraryHosts(canvas, bounds, template, index)
}

func (state *State) paintWorkspaceLibraryHosts(canvas *gfx.Canvas, bounds gfx.Rect, template WorkspaceTemplate, index int) {
	panel := gfx.Rect{X: bounds.X + 12, Y: bounds.Y + workspaceLibraryHeaderHeight, Width: max(0, bounds.Width-24)}
	state.paintWorkspaceLayoutPresets(canvas, gfx.Rect{X: panel.X + 4, Y: panel.Y + 4, Width: max(0, panel.Width-8), Height: workspacePresetStripHeight(max(0, panel.Width-8))}, hitWorkspaceTemplatePresets, index*len(workspacePresets), template.Layout)
	items := workspaceTemplateItems(template)
	height := workspaceTemplateCardHeight(template)
	content := gfx.Rect{X: panel.X, Y: panel.Y + workspaceLibraryLayoutHeight(bounds.Width), Width: panel.Width, Height: height}
	state.paintWorkspaceCards(canvas, content, state.workspaceLibraryBounds, items, template.Layout, workspaceCardsTemplate, template.ID, index)

}

func (state *State) scrollWorkspaceLibrary(event gfx.ScrollEvent) gfx.EventResult {
	if state.activeSection != sectionWorkspaces || state.modal != modalNone ||
		!pointInRect(event.X, event.Y, state.workspaceLibraryBounds) {
		return gfx.EventResult{}
	}
	result := state.windowPage().RouteWheel(event)
	result.Handled = true
	return result
}
