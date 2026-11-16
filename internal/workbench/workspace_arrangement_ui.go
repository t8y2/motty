package workbench

import (
	"fmt"
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

var (
	hitWorkspaceLayoutScroll    = tree.ID(workspaceHeaderHits, "layout-scroll")
	hitWorkspaceLayoutScrollbar = tree.ID(workspaceHeaderHits, "layout-scrollbar")
	hitWorkspaceHeaderPresets   = tree.Sub(workspaceHeaderHits, tree.Name("presets"))
	hitWorkspaceTemplatePresets = tree.Sub(workspaceHits, tree.Name("template-preset"))
	hitWorkspaceDraftPresets    = tree.Sub(workspaceHits, tree.Name("draft-preset"))
)

func (state *State) paintWorkspaceLayoutMenu(canvas *gfx.Canvas, size gfx.Size, anchor gfx.Rect) {
	draft, m := &state.workspaceLayoutDraft, state.messages
	popup := components.BeginPopover(canvas, state.Theme(), state.windowRect(), anchor, components.PopoverOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitWorkspaceHeaderLayoutDismiss,
		AnchorDismiss: true, ConsumeOutsidePress: true, Align: components.PopoverAlignEnd, SideOffset: 6,
		ContentSize:    gfx.Size{Width: min(640, max(0, size.Width-24)), Height: min(180+workspacePresetStripHeight(min(640, max(0, size.Width-24))-40)-workspacePresetHeight+workspaceCardContentHeight(len(draft.items), draft.layout), min(540, max(0, size.Height-windowBarHeight-workspaceHeaderHeight-16)))},
		InitialFocusID: hitWorkspaceHeaderPresets.At(max(0, workspacePresetIndex(draft.layout))), TitleWeight: gfx.WeightSemibold,
	})
	layout := popup.Begin(m.WorkspaceHeaderLayout, m.WorkspaceLayoutHint)
	body := layout.Body
	strip := workspacePresetStripHeight(body.Width)
	state.paintWorkspaceLayoutPresets(canvas, gfx.Rect{X: body.X, Y: body.Y, Width: body.Width, Height: strip}, hitWorkspaceHeaderPresets, 0, draft.layout)
	draft.bounds = gfx.Rect{X: body.X, Y: body.Y + strip + 12, Width: body.Width, Height: max(0, body.Height-strip-12)}
	if draft.err != "" {
		draft.bounds.Height = max(0, draft.bounds.Height-28)
	}
	contentHeight := workspaceCardContentHeight(len(draft.items), draft.layout)
	area := components.BeginScrollArea(canvas, state.Theme(), draft.bounds, gfx.Size{Width: max(0, body.Width-10), Height: contentHeight}).
		Offset(0, draft.scroll).ReserveScrollbars().Viewport(hitWorkspaceLayoutScroll, m.WorkspaceHeaderLayout).
		VerticalScrollbar(hitWorkspaceLayoutScrollbar, m.WorkspaceHeaderLayout, "")
	geometry := area.BeginContent()
	draft.scroll = geometry.OffsetY
	state.paintWorkspaceCards(canvas, gfx.Rect{X: geometry.Content.X, Y: geometry.Content.Y, Width: max(0, body.Width-10), Height: contentHeight}, geometry.Viewport,
		draft.items, draft.layout, workspaceCardsRuntime, "", 0)
	area.EndContent()
	if draft.err != "" {
		monitorLabel(canvas, elide(canvas, draft.err, 12, gfx.WeightRegular, body.Width), gfx.Rect{X: body.X, Y: body.Y + body.Height - 20, Width: body.Width, Height: 20}, 12, state.pal.danger, gfx.WeightRegular)
	}
	popup.End()
}

// workspacePresets is every arrangement offered as one click, in the order the
// strip shows them.
var workspacePresets = layoutcore.Presets()

// workspacePresetIndex is the preset a layout is, or -1 for a layout drawn by hand.
func workspacePresetIndex(spec layoutcore.LayoutSpec) int {
	return slices.IndexFunc(workspacePresets, spec.Equal)
}

// paintWorkspaceLayoutPresets draws the strip of presets. Their identities are
// the list's entries from base on, so several strips can share one list.
func (state *State) paintWorkspaceLayoutPresets(canvas *gfx.Canvas, bounds gfx.Rect, list gfx.Scope, base int, selected layoutcore.LayoutSpec) {
	pal := state.pal
	chosen := workspacePresetIndex(selected)
	perRow := workspacePresetsPerRow(bounds.Width)
	width := max(0, (min(bounds.Width, workspacePresetStripWidth)-float32(perRow-1)*8)/float32(perRow))
	for index, spec := range workspacePresets {
		id := list.At(base + index)
		box := gfx.Rect{X: bounds.X + float32(index%perRow)*(width+8), Y: bounds.Y + float32(index/perRow)*(workspacePresetHeight+8), Width: width, Height: workspacePresetHeight}
		background := pal.surfaceSoft
		if index == chosen || canvas.Hovered() == id {
			background = pal.accentSoft
		}
		canvas.Rounded(box, radius.control, background)
		if index == chosen || canvas.FocusVisible() == id {
			canvas.RoundedBorder(box, radius.control, 1, pal.accent, background)
		}
		preview := gfx.Rect{X: box.X + 8, Y: box.Y + 6, Width: max(0, box.Width-16), Height: 22}
		ink := state.workspaceStatusInk(pal.muted)
		if spec.Tabbed() {
			// One pane under a row of tabs, not three panes.
			strip := gfx.Rect{X: preview.X, Y: preview.Y, Width: preview.Width, Height: 5}
			for tab := 0; tab < 3; tab++ {
				canvas.Rounded(gfx.Rect{X: strip.X + float32(tab)*(strip.Width/3), Y: strip.Y, Width: strip.Width/3 - 2, Height: strip.Height}, 1, ink)
			}
			canvas.Rounded(gfx.Rect{X: preview.X, Y: preview.Y + 7, Width: preview.Width, Height: preview.Height - 7}, 1, ink)
		} else {
			for _, cell := range workspaceLayoutCells(3, spec) {
				canvas.Rounded(pixelBounds(preview, cell).Inset(gfx.All(1)), 1, ink)
			}
		}
		label := state.workspacePresetLabel(spec)
		monitorLabel(canvas, elide(canvas, label, 12, gfx.WeightMedium, max(0, width-12)), gfx.Rect{X: box.X + 6, Y: box.Y + 30, Width: max(0, width-12), Height: 20}, 12, pal.text, gfx.WeightMedium)
		canvas.HitWithOptions(id, box, label, gfx.HitOptions{Role: gfx.RoleRadio, Checked: index == chosen, Selected: index == chosen})
	}
}

// workspacePresetHeight is one preset button; a strip too narrow for them all
// in a line wraps into rows of three.
const workspacePresetHeight = float32(54)

// workspacePresetsPerRow is how many presets a strip of that width fits a row.
func workspacePresetsPerRow(width float32) int {
	if width >= 640 {
		return len(workspacePresets)
	}
	return 3
}

// workspacePresetStripHeight is the height the strip takes at that width.
func workspacePresetStripHeight(width float32) float32 {
	perRow := workspacePresetsPerRow(width)
	rows := (len(workspacePresets) + perRow - 1) / perRow
	return float32(rows)*workspacePresetHeight + float32(rows-1)*8
}

// workspacePresetStripWidth caps the strip so wide panels do not stretch it.
const workspacePresetStripWidth = float32(640)

func (state *State) clickWorkspaceArrangement(id gfx.ID) bool {
	switch {
	case id == hitWorkspaceHeaderLayoutDismiss:
		state.workspaceHeaderMenu = workspaceHeaderMenuNone
		state.workspaceCardDrag = workspaceCardDrag{}
	case id.Scope() == hitWorkspaceHeaderPresets:
		state.chooseWorkspaceRuntimePreset(id)
	case id.Scope() == hitWorkspaceTemplatePresets:
		state.chooseWorkspaceTemplatePreset(id)
	default:
		return false
	}
	return true
}

func (state *State) chooseWorkspaceRuntimePreset(id gfx.ID) {
	index, ok := id.In(hitWorkspaceHeaderPresets)
	if !ok || index >= len(workspacePresets) || state.workspaceHeaderMenu != workspaceHeaderMenuLayout ||
		workspacePresets[index].Equal(state.workspaceLayoutDraft.layout) {
		return
	}
	state.workspaceLayoutDraft.layout, state.workspaceLayoutDraft.scroll = workspacePresets[index], 0
	state.applyWorkspaceArrangement()
}

func (state *State) chooseWorkspaceTemplatePreset(id gfx.ID) {
	index, ok := id.In(hitWorkspaceTemplatePresets)
	if !ok || index/len(workspacePresets) >= len(state.templates) || state.activeSection != sectionWorkspaces {
		return
	}
	template := state.templates[index/len(workspacePresets)]
	if spec := workspacePresets[index%len(workspacePresets)]; !template.Layout.Equal(spec) {
		state.setWorkspaceTemplateLayout(template.ID, spec)
	}
}

// setWorkspaceTemplateLayout records a template's layout and saves it.
func (state *State) setWorkspaceTemplateLayout(id string, spec layoutcore.LayoutSpec) {
	for index := range state.templates {
		if state.templates[index].ID == id {
			state.templates[index].Layout = spec
			state.workspaceCardDrag = workspaceCardDrag{}
			state.save()
			return
		}
	}
}

// workspacePresetLabel names a preset in the strip.
func (state *State) workspacePresetLabel(spec layoutcore.LayoutSpec) string {
	m := state.messages
	switch spec.Kind {
	case layoutcore.LayoutMainRight:
		return m.WorkspaceLayoutMainRight
	case layoutcore.LayoutMainBottom:
		return m.WorkspaceLayoutMainBottom
	case layoutcore.LayoutTabs:
		return m.WorkspaceLayoutTabs
	}
	if spec.Columns == 1 {
		return m.WorkspaceSingleColumn
	}
	return fmt.Sprintf(m.WorkspaceColumnsFmt, spec.Columns)
}

func (state *State) workspaceArrangementKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if event.Release {
		return gfx.EventResult{}, false
	}
	return state.workspacePresetKey(event)
}

func (state *State) workspacePresetKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if event.Modifiers != 0 {
		return gfx.EventResult{}, false
	}
	var list gfx.Scope
	var base int
	switch {
	case event.Target.Scope() == hitWorkspaceHeaderPresets:
		list = hitWorkspaceHeaderPresets
	case event.Target.Scope() == hitWorkspaceDraftPresets:
		list = hitWorkspaceDraftPresets
	case event.Target.Scope() == hitWorkspaceTemplatePresets:
		list = hitWorkspaceTemplatePresets
		index, _ := event.Target.In(list)
		base = index / len(workspacePresets) * len(workspacePresets)
	default:
		return gfx.EventResult{}, false
	}
	options := make([]components.SegmentOption, len(workspacePresets))
	for index := range options {
		options[index] = components.SegmentOption{ID: list.At(base + index)}
	}
	if next, ok := components.SegmentedControlKeyDown(event.Target, options, components.DirectionLTR, event.Key); ok {
		state.Click(next)
		return gfx.EventResult{Handled: true, Repaint: true, Focus: next, FocusVisible: true}, true
	}
	return gfx.EventResult{}, false
}

func (state *State) workspaceArrangementScroll(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	if state.workspaceHeaderMenu != workspaceHeaderMenuLayout {
		return gfx.EventResult{}, false
	}
	draft := &state.workspaceLayoutDraft
	if pointInRect(event.X, event.Y, draft.bounds) {
		area := components.BeginScrollArea(nil, state.Theme(), draft.bounds, gfx.Size{Width: draft.bounds.Width, Height: workspaceCardContentHeight(len(draft.items), draft.layout)}).Offset(0, draft.scroll)
		before := draft.scroll
		draft.scroll = min(max(0, before+event.StepY(32)), area.Geometry().MaxOffsetY)
		return gfx.EventResult{Handled: true, Repaint: draft.scroll != before}, true
	}
	return gfx.EventResult{Handled: true}, true
}
