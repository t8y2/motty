package workbench

import (
	"fmt"
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

var (
	hitWorkspaceLayoutScroll    = tree.ID(workspaceHeaderHits, "layout-scroll")
	hitWorkspaceLayoutScrollbar = tree.ID(workspaceHeaderHits, "layout-scrollbar")
	hitWorkspaceTemplatePresets = tree.Sub(workspaceHits, tree.Name("template-preset"))
)

func (state *State) paintWorkspaceLayoutMenu(canvas *ui.Canvas, size ui.Size, anchor ui.Rect) {
	draft, m := &state.workspaceLayoutDraft, state.messages
	popup := components.BeginPopover(canvas, state.Theme(), state.windowRect(), anchor, components.PopoverOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitWorkspaceHeaderLayoutDismiss,
		AnchorDismiss: true, ConsumeOutsidePress: true, Align: components.PopoverAlignEnd, SideOffset: 6,
		ContentSize:    ui.Size{Width: min(640, max(0, size.Width-24)), Height: min(180+workspaceCardContentHeight(len(draft.items), draft.columns), min(540, max(0, size.Height-windowBarHeight-workspaceHeaderHeight-16)))},
		InitialFocusID: hitWorkspaceHeaderColumnsList.At(int(draft.columns)), TitleWeight: ui.WeightSemibold,
	})
	layout := popup.Begin(m.WorkspaceHeaderLayout, m.WorkspaceLayoutHint)
	body := layout.Body
	ids := [3]ui.ID{hitWorkspaceHeaderColumnsList.At(1), hitWorkspaceHeaderColumnsList.At(2), hitWorkspaceHeaderColumnsList.At(3)}
	state.paintWorkspaceLayoutPresets(canvas, ui.Rect{X: body.X, Y: body.Y, Width: min(288, body.Width), Height: 54}, ids, draft.columns)
	draft.bounds = ui.Rect{X: body.X, Y: body.Y + 66, Width: body.Width, Height: max(0, body.Height-66)}
	if draft.err != "" {
		draft.bounds.Height = max(0, draft.bounds.Height-28)
	}
	contentHeight := workspaceCardContentHeight(len(draft.items), draft.columns)
	area := components.BeginScrollArea(canvas, state.Theme(), draft.bounds, ui.Size{Width: max(0, body.Width-10), Height: contentHeight}).
		Offset(0, draft.scroll).ReserveScrollbars().Viewport(hitWorkspaceLayoutScroll, m.WorkspaceHeaderLayout).
		VerticalScrollbar(hitWorkspaceLayoutScrollbar, m.WorkspaceHeaderLayout, "")
	geometry := area.BeginContent()
	draft.scroll = geometry.OffsetY
	state.paintWorkspaceCards(canvas, ui.Rect{X: geometry.Content.X, Y: geometry.Content.Y, Width: max(0, body.Width-10), Height: contentHeight}, geometry.Viewport,
		draft.items, draft.columns, workspaceCardsRuntime, "", 0)
	area.EndContent()
	if draft.err != "" {
		monitorLabel(canvas, elide(canvas, draft.err, 12, ui.WeightRegular, body.Width), ui.Rect{X: body.X, Y: body.Y + body.Height - 20, Width: body.Width, Height: 20}, 12, state.pal.danger, ui.WeightRegular)
	}
	popup.End()
}

func (state *State) paintWorkspaceLayoutPresets(canvas *ui.Canvas, bounds ui.Rect, ids [3]ui.ID, selected uint8) {
	pal := state.pal
	width := max(0, (bounds.Width-16)/3)
	for index, id := range ids {
		columns := uint8(index + 1)
		box := ui.Rect{X: bounds.X + float32(index)*(width+8), Y: bounds.Y, Width: width, Height: bounds.Height}
		background := pal.surfaceSoft
		if columns == selected || canvas.Hovered() == id {
			background = pal.accentSoft
		}
		canvas.Rounded(box, radius.control, background)
		if columns == selected || canvas.FocusVisible() == id {
			canvas.RoundedBorder(box, radius.control, 1, pal.accent, background)
		}
		preview := ui.Rect{X: box.X + 8, Y: box.Y + 6, Width: max(0, box.Width-16), Height: 22}
		for _, cell := range workspaceGridCells(3, columns) {
			canvas.Rounded(pixelBounds(preview, cell).Inset(ui.All(1)), 1, state.workspaceStatusInk(pal.muted))
		}
		label := state.workspaceColumnsLabel(columns)
		visible := label
		if width < 70 {
			visible = fmt.Sprintf("%d", columns)
		}
		monitorLabel(canvas, elide(canvas, visible, 12, ui.WeightMedium, max(0, width-12)), ui.Rect{X: box.X + 6, Y: box.Y + 30, Width: max(0, width-12), Height: 20}, 12, pal.text, ui.WeightMedium)
		canvas.HitWithOptions(id, box, label, ui.HitOptions{Role: ui.RoleRadio, Checked: columns == selected, Selected: columns == selected})
	}
}

func (state *State) clickWorkspaceArrangement(id ui.ID) bool {
	switch {
	case id == hitWorkspaceHeaderLayoutDismiss:
		state.workspaceHeaderMenu = workspaceHeaderMenuNone
		state.workspaceCardDrag = workspaceCardDrag{}
	case id.Scope() == hitWorkspaceHeaderColumnsList:
		state.chooseWorkspaceRuntimePreset(id)
	case id.Scope() == hitWorkspaceTemplatePresets:
		state.chooseWorkspaceTemplatePreset(id)
	default:
		return false
	}
	return true
}

func (state *State) chooseWorkspaceRuntimePreset(id ui.ID) {
	count, ok := id.In(hitWorkspaceHeaderColumnsList)
	if !ok || count < 1 || count > 3 || state.workspaceHeaderMenu != workspaceHeaderMenuLayout || uint8(count) == state.workspaceLayoutDraft.columns {
		return
	}
	state.workspaceLayoutDraft.columns, state.workspaceLayoutDraft.scroll = uint8(count), 0
	state.applyWorkspaceArrangement()
}

func (state *State) chooseWorkspaceTemplatePreset(id ui.ID) {
	index, ok := id.In(hitWorkspaceTemplatePresets)
	if !ok || index/3 >= len(state.templates) || state.activeSection != sectionWorkspaces {
		return
	}
	template := &state.templates[index/3]
	columns := uint8(index%3 + 1)
	if template.Columns != columns {
		template.Columns = columns
		state.workspaceCardDrag = workspaceCardDrag{}
		state.save()
	}
}

func (state *State) workspaceColumnsLabel(columns uint8) string {
	if columns == 1 {
		return state.messages.WorkspaceSingleColumn
	}
	return fmt.Sprintf(state.messages.WorkspaceColumnsFmt, columns)
}

func (state *State) workspaceArrangementKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if event.Release {
		return ui.EventResult{}, false
	}
	return state.workspacePresetKey(event)
}

func (state *State) workspacePresetKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if event.Modifiers != 0 {
		return ui.EventResult{}, false
	}
	ids := [3]ui.ID{hitWorkspaceDraftColumns1, hitWorkspaceDraftColumns2, hitWorkspaceDraftColumns3}
	if index, ok := event.Target.In(hitWorkspaceHeaderColumnsList); ok && index >= 1 && index <= 3 {
		ids = [3]ui.ID{hitWorkspaceHeaderColumnsList.At(1), hitWorkspaceHeaderColumnsList.At(2), hitWorkspaceHeaderColumnsList.At(3)}
	} else if index, ok := event.Target.In(hitWorkspaceTemplatePresets); ok {
		base := index / 3 * 3
		ids = [3]ui.ID{hitWorkspaceTemplatePresets.At(base), hitWorkspaceTemplatePresets.At(base + 1), hitWorkspaceTemplatePresets.At(base + 2)}
	}
	if !slices.Contains(ids[:], event.Target) {
		return ui.EventResult{}, false
	}
	options := make([]components.SegmentOption, len(ids))
	for index, id := range ids {
		options[index] = components.SegmentOption{ID: id}
	}
	if next, ok := components.SegmentedControlKeyDown(event.Target, options, components.DirectionLTR, event.Key); ok {
		state.Click(next)
		return ui.EventResult{Handled: true, Repaint: true, Focus: next, FocusVisible: true}, true
	}
	return ui.EventResult{}, false
}

func (state *State) workspaceArrangementScroll(event ui.ScrollEvent) (ui.EventResult, bool) {
	if state.workspaceHeaderMenu != workspaceHeaderMenuLayout {
		return ui.EventResult{}, false
	}
	draft := &state.workspaceLayoutDraft
	if pointInRect(event.X, event.Y, draft.bounds) {
		area := components.BeginScrollArea(nil, state.Theme(), draft.bounds, ui.Size{Width: draft.bounds.Width, Height: workspaceCardContentHeight(len(draft.items), draft.columns)}).Offset(0, draft.scroll)
		before := draft.scroll
		draft.scroll = min(max(0, before+event.StepY(32)), area.Geometry().MaxOffsetY)
		return ui.EventResult{Handled: true, Repaint: draft.scroll != before}, true
	}
	return ui.EventResult{Handled: true}, true
}
