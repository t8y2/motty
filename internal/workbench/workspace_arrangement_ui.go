package workbench

import (
	"fmt"
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

var (
	hitWorkspaceLayoutScroll    = tree.ID(workspaceHeaderHits, "layout-scroll")
	hitWorkspaceLayoutScrollbar = tree.ID(workspaceHeaderHits, "layout-scrollbar")
	hitWorkspaceHeaderPresets   = tree.Sub(workspaceHeaderHits, tree.Name("presets"))
	hitWorkspaceTemplatePresets = tree.Sub(workspaceHits, tree.Name("template-preset"))
	hitWorkspaceDraftPresets    = tree.Sub(workspaceHits, tree.Name("draft-preset"))
)

// declareWorkspaceLayoutPanel declares the arrangement editor under the
// header's layout button: an application overlay (no focus scope of its own, so
// closing it does not take the keyboard from the terminal), the preset strip,
// and the workspace's cards in a page scroll area.
func (state *State) declareWorkspaceLayoutPanel(c ui.C, size gfx.Size, plan workspaceHeaderPlan) {
	draft, m := &state.workspaceLayoutDraft, state.messages
	theme := c.Theme()
	pad, gap := theme.Metrics.PadSurface, theme.Metrics.GapItem
	contentWidth := min(640, max(0, size.Width-24))
	strip := workspacePresetStripHeight(contentWidth)
	cards := workspaceCardContentHeight(len(draft.items), draft.layout)
	lineHeight := theme.Metrics.FontSizeM + 7
	titleHeight := lineHeight
	descriptionHeight := float32(0)
	if m.WorkspaceLayoutHint != "" {
		var scratch [3]string
		lines := c.Canvas().WrapLines(scratch[:0], m.WorkspaceLayoutHint, theme.Metrics.FontSizeM, gfx.WeightRegular, contentWidth, 3)
		descriptionHeight = gap + float32(len(lines))*lineHeight
	}
	errorHeight := float32(0)
	if draft.err != "" {
		errorHeight = gap + 20
	}
	room := max(0, size.Height-windowBarHeight-workspaceHeaderHeight-16)
	scrollHeight := min(cards, max(float32(48), room-2*pad-titleHeight-descriptionHeight-2*gap-strip-errorHeight))
	panelWidth := contentWidth + 2*pad
	panelHeight := 2*pad + titleHeight + descriptionHeight + gap + strip + gap + scrollHeight + errorHeight
	anchor := gfx.Rect{
		X: plan.bounds.X + plan.bounds.Width - 10 - plan.layoutWidth,
		Y: plan.bounds.Y + 4, Width: plan.layoutWidth, Height: 30,
	}
	x := min(max(anchor.X+anchor.Width-panelWidth, 8), max(size.Width-panelWidth-8, 8))
	y := anchor.Y + anchor.Height + 6
	if y+panelHeight > size.Height-8 {
		y = max(anchor.Y-6-panelHeight, 8)
	}
	window := gfx.Rect{Width: size.Width, Height: size.Height}
	overlay := ui.Overlay(c, ui.OverlaySpec{Band: ui.LayerPaneChrome, Bounds: window})
	overlay.Children(func() {
		ui.Slot(c).Absolute().Inset(0).HitWith(hitWorkspaceHeaderLayoutDismiss, m.WorkspaceHeaderLayout,
			gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true, Backdrop: true, PassSecondary: true})
		panel := ui.Column(c).Absolute().Left(x).Top(y).Width(panelWidth).Height(panelHeight).
			Pad(pad).Gap(gap).Background(theme.Popover).Border(1, theme.Border).Radius(theme.Metrics.RadiusControl).Shadow(gfx.ShadowLG)
		panel.Children(func() {
			ui.Text(c, m.WorkspaceHeaderLayout).FontSize(theme.Metrics.FontSizeM).Weight(gfx.WeightSemibold).
				Color(theme.PopoverForeground).LineHeight(lineHeight).Ellipsis()
			if m.WorkspaceLayoutHint != "" {
				ui.Paragraph(c, m.WorkspaceLayoutHint, 3).FontSize(theme.Metrics.FontSizeM).LineHeight(lineHeight).Color(theme.MutedForeground)
			}
			ui.Slot(c).FillWidth().Height(strip).Shrink(0).Draw(func(d *ui.DrawContext) {
				state.paintWorkspaceLayoutPresets(d.Canvas, d.Rect, hitWorkspaceHeaderPresets, 0, draft.layout)
			})
			scroll := ui.Scroll(c).FillWidth().Height(scrollHeight).Shrink(0).AlwaysScrollbars().
				ScrollbarIDs(hitWorkspaceLayoutScrollbar, gfx.ID{}).ScrollbarLabels(m.WorkspaceHeaderLayout, "")
			area := scroll.ScrollState()
			if draft.scroll != draft.scrollSynced {
				area.ScrollTo(draft.scroll)
				draft.scrollSynced = draft.scroll
				c.Canvas().RequestFrame()
			}
			scroll.Children(func() {
				if cards <= 0 {
					return
				}
				ui.Slot(c).FillWidth().Height(cards).Draw(func(d *ui.DrawContext) {
					state.paintWorkspaceCards(d.Canvas, d.Rect, d.Rect, draft.items, draft.layout, workspaceCardsRuntime, "", 0)
				})
			})
			scroll.DrawOver(func(d *ui.DrawContext) {
				draft.bounds = d.Rect
				draft.scroll = area.Offset
				draft.scrollSynced = area.Offset
			})
			if draft.err != "" {
				ui.TextSelectionArea(c, &draft.errSelection).Children(func() {
					ui.Text(c, draft.err).FontSize(12).Color(state.pal.danger).LineHeight(20).Ellipsis()
				})
			}
		})
	})
	if id, ok := c.Canvas().ClickTarget(); ok && id == hitWorkspaceHeaderLayoutDismiss {
		// The dismiss press closes the panel; the keyboard goes back to the
		// terminal it was opened over.
		state.workspaceHeaderFocusPending = state.activeTerminalFocusHit()
	}
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
		// The keyboard goes back to the terminal the editor was opened over.
		state.workspaceHeaderFocusPending = state.activeTerminalFocusHit()
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
	if pointInRect(event.X, event.Y, state.workspaceLayoutDraft.bounds) {
		result := state.windowPage().RouteWheel(event)
		result.Handled = true
		return result, true
	}
	return gfx.EventResult{Handled: true}, true
}
