package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const (
	emptyStateTileSize    = float32(62)
	emptyStateTileRadius  = float32(15)
	emptyStateIconSize    = float32(24)
	emptyStateTitleSize   = float32(17)
	emptyStateActionWidth = float32(160)

	// catalogSectionRoot names the shared page a catalog section declares its
	// body in. One page is enough for the same reason: the sections are
	// exclusive.
	catalogSectionRoot = "catalog-section"
)

// emptyStateUI holds what an empty state's last declaration left for its icon
// and action, which are painted by hand, and the page a catalog section declares
// its own body in. The empty state itself is declared in that body: it is a
// region of a section, not a page of its own.
type emptyStateUI struct {
	bounds gfx.Rect
	title  string
	sub    string
	add    string
	color  gfx.Color
	icon   ui.Box
	button ui.Box
	// bodyPage is the shared page the catalog sections declare their own body
	// in, made the first time one paints.
	bodyPage *ui.Page
}

// drawSectionBody draws a catalog section's body in bounds, declaring it with a
// page so the section can reserve its empty-state slot with ui.Slot instead of
// a Frame. Only one catalog section is painted at a time, so one page serves
// them all, as it does for the empty state.
func (state *State) drawSectionBody(canvas *gfx.Canvas, bounds gfx.Rect, view func(ui.C)) {
	e := &state.empty
	if e.bodyPage == nil {
		e.bodyPage = ui.NewPage(ui.PageConfig{Root: catalogSectionRoot, Theme: &state.theme})
	}
	e.bodyPage.Draw(canvas, bounds, view)
}

// reserveEmptyPage declares the empty state as the growing region of the
// section's body. The icon sits a little above the middle of the region,
// followed by one title, one description and, when add is not empty, one
// primary action. The caller paints the icon and action once the body has laid
// out, with drawReservedEmptyPage.
func (state *State) reserveEmptyPage(c ui.C, bounds gfx.Rect, title, sub, add string) ui.Box {
	return state.reserveEmptyPageWithColor(c, bounds, title, sub, add, state.pal.muted)
}

func (state *State) reserveEmptyPageWithColor(c ui.C, bounds gfx.Rect, title, sub, add string, descriptionColor gfx.Color) ui.Box {
	state.prepareEmptyPage(bounds, title, sub, add, descriptionColor)
	body := max(0, bounds.Height-142)
	column := ui.Column(c).Grow(1).FillWidth().MarginSides(0, 0, 9, 0).
		PadSides(max(20, body*0.40-48), 0, 0, 0).Items(ui.Center)
	column.Children(func() { state.declareEmptyPageContent(c) })
	return column
}

// declareEmptyPageRegion declares the empty state over a region that holds
// nothing else, at the region's own origin. The caller paints the icon and
// action once the page has laid out, with paintEmptyPage.
func (state *State) declareEmptyPageRegion(c ui.C, bounds gfx.Rect, title, sub, add string, descriptionColor gfx.Color) {
	state.prepareEmptyPage(bounds, title, sub, add, descriptionColor)
	body := max(0, bounds.Height-142)
	column := ui.Column(c).Absolute().Left(0).Top(0).Width(bounds.Width).Height(bounds.Height).
		MarginSides(0, 0, 9, 0).PadSides(max(20, body*0.40-48), 0, 0, 0).Items(ui.Center)
	column.Children(func() { state.declareEmptyPageContent(c) })
}

// declareEmptyPageContent declares the empty state's icon tile, title,
// description and, when there is one, its action, inside the region
// reserveEmptyPage returned.
func (state *State) declareEmptyPageContent(c ui.C) {
	e := &state.empty
	pal := state.pal
	contentWidth := min(max(e.bounds.Width-44, 0), 560)
	ui.Column(c).Size(emptyStateTileSize, emptyStateTileSize).Center().Radius(emptyStateTileRadius).Background(pal.surface).Children(func() {
		e.icon = ui.Slot(c).Size(emptyStateIconSize, emptyStateIconSize)
	})
	ui.Text(c, e.title).FontSize(emptyStateTitleSize).Weight(gfx.WeightSemibold).Color(pal.text).Width(contentWidth).TextCenter().Ellipsis().MarginSides(20, 0, 0, 0)
	ui.Paragraph(c, e.sub, 4).FontSize(metrics.FontSizeM).Color(e.color).LineHeight(23).Width(contentWidth).TextCenter().MarginSides(8, 0, 0, 0)
	if e.add != "" {
		e.button = ui.Slot(c).Size(emptyStateActionWidth, toolbarHeight).MarginSides(20, 0, 0, 0)
	}
}

// prepareEmptyPage records what the empty state's declaration leaves for the
// icon and the action.
func (state *State) prepareEmptyPage(bounds gfx.Rect, title, sub, add string, descriptionColor gfx.Color) {
	state.empty.bounds = bounds
	state.empty.title, state.empty.sub, state.empty.add, state.empty.color = title, sub, add, descriptionColor
	state.empty.icon, state.empty.button = ui.Box{}, ui.Box{}
}

// drawReservedEmptyPage paints the icon and action the empty state's
// declaration laid out. The box is the region's declaration; its content is
// already part of the page.
func (state *State) drawReservedEmptyPage(canvas *gfx.Canvas, _ ui.Box, icon uint8, id gfx.ID, label string, enabled bool) {
	state.paintEmptyPage(canvas, icon, id, label, enabled)
}

// paintEmptyPage paints the icon and action declared by the empty state. An
// empty label leaves the page without an action, which is useful for data that is
// populated by another workflow rather than created on the page.
func (state *State) paintEmptyPage(canvas *gfx.Canvas, icon uint8, id gfx.ID, label string, enabled bool) {
	paintIcon(canvas, icon, state.empty.icon.Rect(), state.pal.muted)
	if label == "" {
		return
	}
	button := state.empty.button.Rect()
	if button.Width == 0 {
		return
	}
	state.paintToolbarButton(canvas, id, button.X, button.Y, button.Width, label, iconPlus, true, enabled, false)
}
