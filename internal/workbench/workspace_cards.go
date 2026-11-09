package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/gfx"
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
	id       gfx.ID
	bounds   gfx.Rect
	viewport gfx.Rect
}

type workspaceCardDrag struct {
	pressed, active bool
	region          workspaceCardRegion
	start, at       gfx.Point
	target          int
	zone            workspaceCardZone
	order           []workspaceArrangementItem
}

// workspaceCardZone is the part of a card a drag is over. The middle swaps the
// two cards' places; an edge docks the dragged card beside that card on that
// side, which is how a layout is drawn rather than picked.
type workspaceCardZone uint8

const (
	zoneSwap workspaceCardZone = iota
	zoneLeft
	zoneRight
	zoneTop
	zoneBottom
)

// workspaceCardEdge is the share of a card's width or height that counts as its edge.
const workspaceCardEdge = float32(0.25)

func workspaceCardZoneAt(box gfx.Rect, at gfx.Point) workspaceCardZone {
	x, y := (at.X-box.X)/max(box.Width, 1), (at.Y-box.Y)/max(box.Height, 1)
	zone, depth := zoneSwap, workspaceCardEdge
	for _, edge := range [...]struct {
		zone     workspaceCardZone
		distance float32
	}{{zoneLeft, x}, {zoneRight, 1 - x}, {zoneTop, y}, {zoneBottom, 1 - y}} {
		if edge.distance < depth {
			zone, depth = edge.zone, edge.distance
		}
	}
	return zone
}

// dock is the split a dock zone makes: the axis, and whether the dragged card
// goes before the target on it.
func (zone workspaceCardZone) dock() (layoutcore.Axis, layoutcore.Placement) {
	switch zone {
	case zoneLeft:
		return layoutcore.Horizontal, layoutcore.Before
	case zoneRight:
		return layoutcore.Horizontal, layoutcore.After
	case zoneTop:
		return layoutcore.Vertical, layoutcore.Before
	}
	return layoutcore.Vertical, layoutcore.After
}

// workspaceCardContentHeight is tall enough for the shortest cell of the
// layout to hold one card, so a grid of N rows is N cards high and any other
// shape follows its own proportions.
func workspaceCardContentHeight(count int, spec layoutcore.LayoutSpec) float32 {
	shortest := float32(1)
	for _, cell := range workspaceLayoutCells(count, spec) {
		shortest = min(shortest, cell.Height)
	}
	return workspaceCardHeight * min(1/shortest, layoutcore.MaxPanes)
}

// Use the launcher's split tree, so incomplete rows occupy the same width in
// the editor and in the running workspace.
func workspaceLayoutCells(count int, spec layoutcore.LayoutSpec) []FractionBounds {
	ids := make([]layoutcore.SessionID, count)
	for index := range ids {
		ids[index] = layoutcore.SessionID(index + 1)
	}
	layout, err := layoutcore.NewLayout(ids, spec)
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

func (state *State) paintWorkspaceCards(canvas *gfx.Canvas, content, viewport gfx.Rect, items []workspaceArrangementItem, spec layoutcore.LayoutSpec, view workspaceCardView, template string, templateIndex int) {
	cells := workspaceLayoutCells(len(items), spec)
	for index, cell := range cells {
		box := pixelBounds(content, cell).Inset(gfx.All(4))
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

func (state *State) paintWorkspaceHostCard(canvas *gfx.Canvas, region workspaceCardRegion, item workspaceArrangementItem, count int) {
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
	icon := gfx.Rect{X: box.X + 10, Y: box.Y + 12, Width: 22, Height: 22}
	if available {
		state.paintHostCatalogIcon(canvas, host, icon, background)
	} else {
		canvas.DrawIcon(lucideIcons[iconAlert], icon, color)
	}
	nameWidth := max(0, box.Width-58)
	monitorLabel(canvas, elide(canvas, name, 13, gfx.WeightSemibold, nameWidth), gfx.Rect{X: box.X + 40, Y: box.Y + 10, Width: nameWidth, Height: 24}, 13, color, gfx.WeightSemibold)
	paintWorkspaceCardHandle(canvas, box, pal.muted)
	ink := state.workspaceStatusInk(pal.muted)
	width := max(0, box.Width-20)
	monitorLabel(canvas, elide(canvas, address, 12, gfx.WeightRegular, width), gfx.Rect{X: box.X + 10, Y: box.Y + 40, Width: width, Height: 20}, 12, ink, gfx.WeightRegular)
	detailWidth := width
	if region.view == workspaceCardsDraft && state.workspaceDraft.mode == workspaceDraftEdit {
		detailWidth = max(0, box.Width-44)
	}
	monitorLabel(canvas, elide(canvas, detail, 11, gfx.WeightRegular, detailWidth), gfx.Rect{X: box.X + 10, Y: box.Y + 66, Width: detailWidth, Height: 18}, 11, ink, gfx.WeightRegular)
	canvas.HitWithOptions(region.id, box, name, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand, Hint: state.messages.WorkspaceCardReorderHint,
		Description: address + " · " + detail, PosInSet: int32(region.index + 1), SetSize: int32(count), Selected: active})
	drag := &state.workspaceCardDrag
	if workspaceCardDropTarget(drag, region) {
		if drag.zone == zoneSwap {
			canvas.RoundedBorder(box, radius.control, 2, pal.accent, background)
		} else {
			canvas.Rounded(workspaceCardZoneBar(box, drag.zone), 2, pal.accent)
		}
	}
}

func (state *State) paintWorkspaceCardDrag(canvas *gfx.Canvas, view workspaceCardView, template string) {
	drag := &state.workspaceCardDrag
	if !drag.active || drag.region.view != view || drag.region.template != template {
		return
	}
	box := gfx.Rect{X: drag.at.X + 12, Y: drag.at.Y + 12, Width: min(176, drag.region.bounds.Width), Height: 32}
	canvas.PushLayer(tabDragLayer)
	canvas.RoundedBorder(box, radius.control, 1, state.pal.accent, state.pal.surface)
	label := fmt.Sprintf("%d", drag.region.index+1)
	if items := state.workspaceItemsForRegion(drag.region); drag.region.index < len(items) {
		_, label, _, _, _ = state.workspaceCardText(items[drag.region.index])
	}
	monitorLabel(canvas, elide(canvas, label, 12, gfx.WeightMedium, max(0, box.Width-16)), box.Inset(gfx.All(8)), 12, state.pal.text, gfx.WeightMedium)
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

func paintWorkspaceCardHandle(canvas *gfx.Canvas, box gfx.Rect, color gfx.Color) {
	for row := 0; row < 3; row++ {
		for column := 0; column < 2; column++ {
			canvas.Rounded(gfx.Rect{X: box.X + box.Width - 18 + float32(column)*4, Y: box.Y + 15 + float32(row)*4, Width: 1.5, Height: 1.5}, .75, color)
		}
	}
}

// workspaceCardZoneBar is the half of the card the docked card would take.
func workspaceCardZoneBar(box gfx.Rect, zone workspaceCardZone) gfx.Rect {
	switch zone {
	case zoneLeft:
		box.Width /= 2
	case zoneRight:
		box.X += box.Width / 2
		box.Width /= 2
	case zoneTop:
		box.Height /= 2
	default:
		box.Y += box.Height / 2
		box.Height /= 2
	}
	return box.Inset(gfx.All(2))
}
