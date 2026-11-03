package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

const workspaceCardHeight = float32(104)

type workspaceCardView uint8

const (
	workspaceCardsRuntime workspaceCardView = iota + 1
	workspaceCardsTemplate
	workspaceCardsDraft
)

var (
	hitWorkspaceLayoutCards   = tree.Sub(workspaceHits, tree.Name("layout-card"))
	hitWorkspaceTemplateCards = tree.Sub(workspaceHits, tree.Name("template-card"))
	hitWorkspaceDraftCards    = tree.Sub(workspaceHits, tree.Name("draft-card"))
)

type workspaceCardRegion struct {
	view     workspaceCardView
	template string
	index    int
	id       ui.ID
	bounds   ui.Rect
	viewport ui.Rect
}

type workspaceCardDrag struct {
	pressed, active bool
	region          workspaceCardRegion
	start, at       ui.Point
	target          int
	order           []workspaceArrangementItem
}

func workspaceCardContentHeight(count int, columns uint8) float32 {
	return float32((count+int(columns)-1)/max(1, int(columns))) * workspaceCardHeight
}

// Use the launcher's split tree, so incomplete rows occupy the same width in
// the editor and in the running workspace.
func workspaceGridCells(count int, columns uint8) []FractionBounds {
	ids := make([]layoutcore.SessionID, count)
	for index := range ids {
		ids[index] = layoutcore.SessionID(index + 1)
	}
	layout, err := layoutcore.NewGrid(ids, int(columns))
	if err != nil {
		return nil
	}
	return appendWorkspaceGridCells(nil, &layout, layout.Root(), FractionBounds{Width: 1, Height: 1})
}

func appendWorkspaceGridCells(cells []FractionBounds, layout *layoutcore.Layout, id layoutcore.NodeID, bounds FractionBounds) []FractionBounds {
	node, ok := layout.Node(id)
	if !ok {
		return cells
	}
	if node.Kind == layoutcore.KindLeaf {
		return append(cells, bounds)
	}
	first, second := splitFractionBounds(bounds, node.Axis, node.Ratio)
	cells = appendWorkspaceGridCells(cells, layout, node.First, first)
	return appendWorkspaceGridCells(cells, layout, node.Second, second)
}

func (state *State) paintWorkspaceCards(canvas *ui.Canvas, content, viewport ui.Rect, items []workspaceArrangementItem, columns uint8, view workspaceCardView, template string, templateIndex int) {
	cells := workspaceGridCells(len(items), columns)
	for index, cell := range cells {
		box := pixelBounds(content, cell).Inset(ui.All(4))
		if box.Y+box.Height <= viewport.Y || box.Y >= viewport.Y+viewport.Height {
			continue
		}
		id := hitWorkspaceLayoutCards.At(index)
		switch view {
		case workspaceCardsTemplate:
			id = hitWorkspaceTemplateCards.At(templateIndex*layoutcore.MaxPanes + index)
		case workspaceCardsDraft:
			id = hitWorkspaceDraftCards.At(index)
		}
		region := workspaceCardRegion{view: view, template: template, index: index, id: id, bounds: box, viewport: viewport}
		state.workspaceCardRegions = append(state.workspaceCardRegions, region)
		state.paintWorkspaceHostCard(canvas, region, items[index], len(items))
	}
	state.paintWorkspaceCardDrag(canvas, view, template)
}

func (state *State) workspaceCardText(item workspaceArrangementItem) (Host, string, string, string, bool) {
	if item.session != 0 {
		if session, ok := state.sessionByIDPointer(item.session); ok {
			status, _ := state.sessionStatus(session)
			return session.host, session.Title, session.Address, status, true
		}
	}
	if host, ok := state.workspaceHostByID(item.host); ok {
		detail := strings.Trim(strings.Join([]string{host.Group, host.Tags}, " · "), " ·")
		if host.ID == localHostID {
			detail = state.messages.WorkspaceLocalTerminalHint
		}
		return host, host.Label, state.sessionAddress(host), detail, true
	}
	return Host{}, item.host, state.messages.WorkspaceTemplateMissingFmt, "", false
}

func (state *State) paintWorkspaceHostCard(canvas *ui.Canvas, region workspaceCardRegion, item workspaceArrangementItem, count int) {
	pal, box := state.pal, region.bounds
	host, name, address, detail, available := state.workspaceCardText(item)
	background := pal.surfaceSoft
	if canvas.Hovered() == region.id || canvas.Pressed() == region.id {
		background = pal.surfaceSoft.Mix(pal.accent, .045)
	}
	active := item.session != 0 && item.session == state.activeSessionID
	canvas.Rounded(box, radius.control, background)
	if active || canvas.FocusVisible() == region.id {
		canvas.RoundedBorder(box, radius.control, 1.5, pal.accent, background)
	}
	color := pal.text
	if !available {
		color = state.workspaceStatusInk(pal.warning)
	}
	icon := ui.Rect{X: box.X + 10, Y: box.Y + 12, Width: 22, Height: 22}
	if available {
		state.paintHostCatalogIcon(canvas, host, icon, background)
	} else {
		canvas.DrawIcon(lucideIcons[iconAlert], icon, color)
	}
	nameWidth := max(0, box.Width-58)
	monitorLabel(canvas, elide(canvas, name, 13, ui.WeightSemibold, nameWidth), ui.Rect{X: box.X + 40, Y: box.Y + 10, Width: nameWidth, Height: 24}, 13, color, ui.WeightSemibold)
	paintWorkspaceCardHandle(canvas, box, pal.muted)
	ink := state.workspaceStatusInk(pal.muted)
	width := max(0, box.Width-20)
	monitorLabel(canvas, elide(canvas, address, 12, ui.WeightRegular, width), ui.Rect{X: box.X + 10, Y: box.Y + 40, Width: width, Height: 20}, 12, ink, ui.WeightRegular)
	detailWidth := width
	if region.view == workspaceCardsDraft && state.workspaceDraft.mode == workspaceDraftEdit {
		detailWidth = max(0, box.Width-44)
	}
	monitorLabel(canvas, elide(canvas, detail, 11, ui.WeightRegular, detailWidth), ui.Rect{X: box.X + 10, Y: box.Y + 66, Width: detailWidth, Height: 18}, 11, ink, ui.WeightRegular)
	canvas.HitWithOptions(region.id, box, name, ui.HitOptions{Role: ui.RoleButton, Cursor: ui.PointerCursorHand, Hint: state.messages.WorkspaceCardReorderHint,
		Description: address + " · " + detail, PosInSet: int32(region.index + 1), SetSize: int32(count), Selected: active})
	drag := &state.workspaceCardDrag
	if workspaceCardDropTarget(drag, region) {
		canvas.RoundedBorder(box, radius.control, 2, pal.accent, background)
	}
}

func (state *State) paintWorkspaceCardDrag(canvas *ui.Canvas, view workspaceCardView, template string) {
	drag := &state.workspaceCardDrag
	if !drag.active || drag.region.view != view || drag.region.template != template {
		return
	}
	box := ui.Rect{X: drag.at.X + 12, Y: drag.at.Y + 12, Width: min(176, drag.region.bounds.Width), Height: 32}
	canvas.PushLayer(tabDragLayer)
	canvas.RoundedBorder(box, radius.control, 1, state.pal.accent, state.pal.surface)
	label := fmt.Sprintf("%d", drag.region.index+1)
	if items := state.workspaceItemsForRegion(drag.region); drag.region.index < len(items) {
		_, label, _, _, _ = state.workspaceCardText(items[drag.region.index])
	}
	monitorLabel(canvas, elide(canvas, label, 12, ui.WeightMedium, max(0, box.Width-16)), box.Inset(ui.All(8)), 12, state.pal.text, ui.WeightMedium)
	canvas.PopLayer()
}

func (state *State) workspaceItemsForRegion(region workspaceCardRegion) []workspaceArrangementItem {
	switch region.view {
	case workspaceCardsRuntime:
		return state.workspaceLayoutDraft.items
	case workspaceCardsTemplate:
		for _, template := range state.templates {
			if template.ID == region.template {
				return workspaceTemplateItems(template)
			}
		}
	case workspaceCardsDraft:
		ids := state.workspaceDraftOrderedIDs()
		items := make([]workspaceArrangementItem, len(ids))
		for index, id := range ids {
			items[index].host = id
		}
		return items
	}
	return nil
}

func workspaceCardDropTarget(drag *workspaceCardDrag, region workspaceCardRegion) bool {
	return drag.active && drag.region.view == region.view && drag.region.template == region.template && drag.target == region.index
}

func paintWorkspaceCardHandle(canvas *ui.Canvas, box ui.Rect, color ui.Color) {
	for row := 0; row < 3; row++ {
		for column := 0; column < 2; column++ {
			canvas.Rounded(ui.Rect{X: box.X + box.Width - 18 + float32(column)*4, Y: box.Y + 15 + float32(row)*4, Width: 1.5, Height: 1.5}, .75, color)
		}
	}
}
