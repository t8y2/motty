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

	// emptyStateRoot names the shared empty-state page in the window's Tree. One
	// page is enough: the catalog sections are exclusive, so one is painted at a
	// time.
	emptyStateRoot = "empty-state"
)

// emptyStateUI is the shared empty-state page and what its last paint left for
// the icon and the action, which are painted by hand.
type emptyStateUI struct {
	page   *ui.Page
	view   func(ui.C)
	bounds gfx.Rect
	title  string
	sub    string
	add    string
	color  gfx.Color
	icon   ui.Box
	button ui.Box
}

// declareEmptyPage declares the shared empty-state page in f, a frame that keeps
// a region of a larger layout. The icon sits a little above the middle of the
// region, followed by one title, one description and, when add is not empty, one
// primary action. paintEmptyPage paints the icon and action once f has laid out.
func (state *State) declareEmptyPage(f *ui.Frame, bounds gfx.Rect, title, sub, add string) {
	state.declareEmptyPageWithColor(f, bounds, title, sub, add, state.pal.muted)
}

func (state *State) declareEmptyPageWithColor(f *ui.Frame, bounds gfx.Rect, title, sub, add string, descriptionColor gfx.Color) {
	state.empty.bounds = bounds
	state.empty.title, state.empty.sub, state.empty.add, state.empty.color = title, sub, add, descriptionColor
	state.empty.icon, state.empty.button = ui.Box{}, ui.Box{}
	state.emptyPage()
	f.Page(state.empty.page, state.empty.view).Grow(1)
}

// drawEmptyPage paints the shared empty state as a page filling bounds, for a
// region that holds nothing else.
func (state *State) drawEmptyPage(canvas *gfx.Canvas, bounds gfx.Rect, title, sub, add string, descriptionColor gfx.Color, icon uint8, id gfx.ID, label string, enabled bool) {
	state.empty.bounds = bounds
	state.empty.title, state.empty.sub, state.empty.add, state.empty.color = title, sub, add, descriptionColor
	state.empty.icon, state.empty.button = ui.Box{}, ui.Box{}
	state.emptyPage()
	state.empty.page.Draw(canvas, bounds, state.empty.view)
	state.paintEmptyPage(canvas, icon, id, label, enabled)
}

// emptyPage makes the page and its declaration once.
func (state *State) emptyPage() {
	if state.empty.page == nil {
		state.empty.page = ui.NewPage(ui.PageConfig{Root: emptyStateRoot, Theme: &state.theme})
		state.empty.view = state.declareEmptyPageView
	}
}

// declareEmptyPageView declares the empty state inside the page: the icon tile,
// the title and the description, and the action when there is one. The top
// padding follows the region the caller gave, not the page's own slot, so a
// shared frame places the block where the pane's height says.
func (state *State) declareEmptyPageView(c ui.C) {
	e := &state.empty
	pal := state.pal
	body := max(0, e.bounds.Height-142)
	contentWidth := min(max(e.bounds.Width-44, 0), 560)
	ui.Column(c).Fill().MarginSides(0, 0, 9, 0).PadSides(max(20, body*0.40-48), 0, 0, 0).Items(ui.Center).Children(func() {
		ui.Column(c).Size(emptyStateTileSize, emptyStateTileSize).Center().Radius(emptyStateTileRadius).Background(pal.surface).Children(func() {
			e.icon = ui.Slot(c).Size(emptyStateIconSize, emptyStateIconSize)
		})
		ui.Text(c, e.title).FontSize(emptyStateTitleSize).Weight(gfx.WeightSemibold).Color(pal.text).Width(contentWidth).TextCenter().Ellipsis().MarginSides(20, 0, 0, 0)
		ui.Paragraph(c, e.sub, 4).FontSize(metrics.FontSizeM).Color(e.color).LineHeight(23).Width(contentWidth).TextCenter().MarginSides(8, 0, 0, 0)
		if e.add != "" {
			e.button = ui.Slot(c).Size(emptyStateActionWidth, toolbarHeight).MarginSides(20, 0, 0, 0)
		}
	})
}

// paintEmptyPage paints the icon and action declared by the empty-state page. An
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
