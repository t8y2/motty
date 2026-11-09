package workbench

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
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
	hitWorkspaceDraftTemporary        = tree.ID(workspaceHits, "draft-temporary")
	hitWorkspaceDraftFixed            = tree.ID(workspaceHits, "draft-fixed")
	hitWorkspaceDraftName             = tree.ID(workspaceHits, "draft-name")
	hitWorkspaceDraftSearch           = tree.ID(workspaceHits, "draft-search")
	hitWorkspaceDraftHostList         = tree.Sub(workspaceHits, tree.Name("draft-host"))
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
	plan, err := PlanWorkspaceLaunch(state.workspaceHosts(), template.HostIDs, template.Layout)
	if err != nil {
		detail := state.messages.WorkspaceTemplateApplyFailed
		if err == ErrWorkspaceLaunchEmpty && len(plan.Missing) > 0 {
			detail = state.messages.WorkspaceTemplateNoHosts
		}
		state.showActionWarning(template.Name, detail)
		return
	}
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

// paintWorkspaceLibrary lists templates as openable catalog rows. The sidebar
// deliberately carries no template count.
func (state *State) paintWorkspaceLibrary(canvas *gfx.Canvas, bounds gfx.Rect) {
	m, pal := state.messages, state.pal
	newWidth, newIcon := toolbarButtonLayout(canvas, m.WorkspaceNew, iconPlus, iconPlus, bounds.Width < 520)

	var content, emptyIcon, emptyButton ui.Box
	var create ui.Box
	f := state.workspaceLibraryLayout.Begin(canvas, state.Theme(), bounds)
	padding := float32(22)
	if bounds.Width < 340 {
		padding = 12
	}
	f.Column().Fill().PadSides(18, padding, 0, padding).Children(func() {
		if bounds.Width < 520 {
			f.Text(m.WorkspaceLibraryTitle).FontSize(20).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(28).Ellipsis()
			f.Row().Gap(8).MarginSides(12, 0, 20, 0).Children(func() {
				create = f.Slot().Size(newWidth, toolbarHeight)
			})
		} else {
			f.Row().MinHeight(74).Gap(10).MarginSides(0, 0, 14, 0).Items(ui.Start).Children(func() {
				f.Column().Grow(1).Gap(6).Children(func() {
					f.Text(m.WorkspaceLibraryTitle).FontSize(20).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(28).Ellipsis()
					f.Paragraph(m.WorkspaceLibraryDescription, 2).FontSize(12).LineHeight(19).Color(pal.muted)
				})
				create = f.Slot().Size(newWidth, toolbarHeight)
			})
		}
		if len(state.templates) == 0 {
			emptyIcon, emptyButton = state.declareEmptyPage(f, bounds, m.WorkspaceLibraryEmpty, m.WorkspaceLibraryEmptySub, m.WorkspaceNew)
		} else {
			content = f.Slot().Grow(1).MarginSides(0, 0, 14, 0)
		}
		f.Divider().Background(pal.line)
	})
	f.End()

	if len(state.templates) == 0 {
		state.paintEmptyPage(canvas, emptyIcon, emptyButton, iconColumns2, hitWorkspaceNew, m.WorkspaceNew, true)
		return
	}

	state.paintToolbarButton(canvas, hitWorkspaceNew, create.Rect().X, create.Rect().Y,
		newWidth, m.WorkspaceNew, newIcon, false, true, bounds.Width < 520)

	state.paintWorkspaceLibraryRows(canvas, content.Rect(), state.expandedWorkspaceTemplateIndex())
}

type workspaceLibraryRow struct {
	index    int
	expanded bool
	bounds   gfx.Rect
}

func (state *State) paintWorkspaceLibraryRows(canvas *gfx.Canvas, bounds gfx.Rect, expandedIndex int) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	contentWidth := max(0, bounds.Width-workspaceLibraryScrollbarReserve)
	rows, contentHeight := state.workspaceLibraryRowLayout(contentWidth, expandedIndex)
	scroll := components.BeginScrollArea(canvas, state.Theme(), bounds, gfx.Size{
		Width: contentWidth, Height: contentHeight,
	}).
		Offset(0, state.workspaceLibraryScroll).
		ReserveScrollbars().
		Viewport(hitWorkspaceLibraryScroll, state.messages.WorkspaceLibraryTitle).
		VerticalScrollbar(gfx.ID{}, "", "")
	geometry := scroll.BeginContent()
	state.workspaceLibraryScroll = geometry.OffsetY
	state.workspaceLibraryBounds = bounds
	state.workspaceLibraryContent = contentHeight
	state.workspaceTemplateMoreBounds = state.workspaceTemplateMoreBounds[:0]

	for _, row := range rows {
		rect := row.bounds
		rect.X += geometry.Content.X
		rect.Y += geometry.Content.Y
		if rect.Y+rect.Height < geometry.Viewport.Y || rect.Y > geometry.Viewport.Y+geometry.Viewport.Height {
			continue
		}
		template := state.templates[row.index]
		state.paintWorkspaceLibraryRow(canvas, rect, row.index, template, row.expanded)
	}
	scroll.EndContent()
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
	return workspaceLibraryHeaderHeight + workspaceLibraryLayoutHeight(width) + workspaceCardContentHeight(len(template.HostIDs), template.Layout) + workspaceLibraryBottomInset
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
	transform := gfx.IconTransform{Rotate: canvas.Animate(rowID, 0, target, gfx.Motion{Duration: 160 * time.Millisecond, Easing: gfx.EaseOut})}
	canvas.DrawIconTransformed(lucideIcons[iconChevronDown], gfx.Rect{
		X: bounds.X + 16, Y: bounds.Y + 30, Width: 16, Height: 16,
	}, chevronColor, transform)

	openWidth := components.BeginButton(canvas, state.Theme(), gfx.ID{}, gfx.Rect{}).Small().WithIcon(lucideIcons[iconPlay]).SizeToFit(m.WorkspaceEnter).BoundsWidth()
	compact := bounds.Width < 340
	if compact {
		openWidth = 32
	}
	moreBounds := gfx.Rect{X: bounds.X + bounds.Width - 16 - 28, Y: bounds.Y + 22, Width: 28, Height: 32}
	openBounds := gfx.Rect{X: moreBounds.X - 8 - openWidth, Y: bounds.Y + 22, Width: openWidth, Height: 32}
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
	button := components.BeginButton(canvas, state.Theme(), hitWorkspaceTemplateOpen.At(index), openBounds).Small().WithIcon(lucideIcons[iconPlay])
	if compact {
		button = button.Size(components.ButtonSizeIconSmall)
	}
	button.Default(m.WorkspaceEnter)

	if !expanded {
		return
	}

	state.paintWorkspaceLibraryHosts(canvas, bounds, template, index)
}

func (state *State) paintWorkspaceLibraryHosts(canvas *gfx.Canvas, bounds gfx.Rect, template WorkspaceTemplate, index int) {
	panel := gfx.Rect{X: bounds.X + 12, Y: bounds.Y + workspaceLibraryHeaderHeight, Width: max(0, bounds.Width-24)}
	state.paintWorkspaceLayoutPresets(canvas, gfx.Rect{X: panel.X + 4, Y: panel.Y + 4, Width: max(0, panel.Width-8), Height: workspacePresetStripHeight(max(0, panel.Width-8))}, hitWorkspaceTemplatePresets, index*len(workspacePresets), template.Layout)
	items := workspaceTemplateItems(template)
	height := workspaceCardContentHeight(len(items), template.Layout)
	content := gfx.Rect{X: panel.X, Y: panel.Y + workspaceLibraryLayoutHeight(bounds.Width), Width: panel.Width, Height: height}
	state.paintWorkspaceCards(canvas, content, state.workspaceLibraryBounds, items, template.Layout, workspaceCardsTemplate, template.ID, index)

}

func (state *State) scrollWorkspaceLibrary(event gfx.ScrollEvent) gfx.EventResult {
	if state.activeSection != sectionWorkspaces || state.modal != modalNone ||
		!pointInRect(event.X, event.Y, state.workspaceLibraryBounds) {
		return gfx.EventResult{}
	}
	area := components.BeginScrollArea(nil, state.Theme(), state.workspaceLibraryBounds, gfx.Size{
		Width:  max(0, state.workspaceLibraryBounds.Width-workspaceLibraryScrollbarReserve),
		Height: state.workspaceLibraryContent,
	}).Offset(0, state.workspaceLibraryScroll).ReserveScrollbars()
	geometry := area.Geometry()
	before := state.workspaceLibraryScroll
	state.workspaceLibraryScroll = min(max(0, before+event.StepY(36)), geometry.MaxOffsetY)
	return gfx.EventResult{Handled: true, Repaint: before != state.workspaceLibraryScroll}
}
