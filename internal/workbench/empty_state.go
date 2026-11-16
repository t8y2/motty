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
)

// sectionBodies holds what each catalog section's last declaration left for its
// paint. Only one catalog section is on show at a time, so one body is declared
// per paint and the rest keep their last, stale slots, which nothing reads.
type sectionBodies struct {
	forwards         forwardSectionUI
	keys             keysSectionUI
	knownHosts       knownHostsSectionUI
	proxies          proxySectionUI
	snippets         snippetsSectionUI
	sessionLogs      sessionLogsSectionUI
	workspaceLibrary workspaceLibrarySectionUI
}

// catalogSectionBounds is the room the workspace slot gives a catalog section:
// the same rectangle hosts and dashboard compute before the page lays the slot
// out. A section body only needs its width and height to decide its rows and
// padding; its slots carry their own place once the page has laid them out.
func (state *State) catalogSectionBounds() gfx.Rect {
	width, height := state.hostsWorkspaceSize()
	return gfx.Rect{Width: width, Height: height}
}

// declareCatalogSection declares the active catalog section's body in the
// workbench page's workspace slot. The sections are exclusive; a section that
// does not use this path (hosts, the dashboard and notes) declares itself.
func (state *State) declareCatalogSection(c ui.C) {
	switch state.activeSection {
	case sectionWorkspaces:
		state.declareWorkspaceLibrary(c)
	case sectionKeys:
		state.declareKeysWorkspace(c)
	case sectionForwards:
		state.declareForwardsWorkspace(c)
	case sectionProxies:
		state.declareProxiesWorkspace(c)
	case sectionKnownHosts:
		state.declareKnownHostsWorkspace(c)
	case sectionSnippets:
		state.declareSnippetsWorkspace(c)
	case sectionSessionLogs:
		state.declareSessionLogsWorkspace(c)
	}
}

// emptyStateUI holds what an empty state's last declaration left for its icon
// and action, which are painted by hand. The empty state itself is declared in
// the section's body: it is a region of a section, not a page of its own.
type emptyStateUI struct {
	bounds  gfx.Rect
	title   string
	sub     string
	add     string
	color   gfx.Color
	icon    uint8
	action  gfx.ID
	enabled bool
	iconBox ui.Box
	button  ui.Box
}

// reserveEmptyPage declares the empty state as the growing region of the
// section's body. The icon sits a little above the middle of the region,
// followed by one title, one description and, when add is not empty, one
// primary action, which is a page button. The caller paints the icon once the
// body has laid out, with drawReservedEmptyPage.
func (state *State) reserveEmptyPage(c ui.C, bounds gfx.Rect, title, sub, add string, action gfx.ID, icon uint8, enabled bool) ui.Box {
	return state.reserveEmptyPageWithColor(c, bounds, title, sub, add, state.pal.muted, action, icon, enabled)
}

func (state *State) reserveEmptyPageWithColor(c ui.C, bounds gfx.Rect, title, sub, add string, descriptionColor gfx.Color, action gfx.ID, icon uint8, enabled bool) ui.Box {
	state.prepareEmptyPage(bounds, title, sub, add, descriptionColor, action, icon, enabled)
	body := max(0, bounds.Height-142)
	column := ui.Column(c).Grow(1).FillWidth().MarginSides(0, 0, 9, 0).
		PadSides(max(20, body*0.40-48), 0, 0, 0).Items(ui.Center)
	column.Children(func() { state.declareEmptyPageContent(c) })
	return column
}

// declareEmptyPageRegion declares the empty state over a region that holds
// nothing else, at the region's own origin. The caller paints the icon once the
// page has laid out, with paintEmptyPage.
func (state *State) declareEmptyPageRegion(c ui.C, bounds gfx.Rect, title, sub, add string, descriptionColor gfx.Color, action gfx.ID, icon uint8, enabled bool) {
	state.prepareEmptyPage(bounds, title, sub, add, descriptionColor, action, icon, enabled)
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
		e.iconBox = ui.Slot(c).Size(emptyStateIconSize, emptyStateIconSize)
	})
	ui.Text(c, e.title).FontSize(emptyStateTitleSize).Weight(gfx.WeightSemibold).Color(pal.text).Width(contentWidth).TextCenter().Ellipsis().MarginSides(20, 0, 0, 0)
	ui.Paragraph(c, e.sub, 4).FontSize(metrics.FontSizeM).Color(e.color).LineHeight(23).Width(contentWidth).TextCenter().MarginSides(8, 0, 0, 0)
	if e.add != "" {
		e.button = state.declareButtonSized(c, e.action, e.add, iconPlus, true, false, e.enabled, toolbarFont, false).
			Size(emptyStateActionWidth, toolbarHeight).MarginSides(20, 0, 0, 0)
	}
}

// prepareEmptyPage records what the empty state's declaration leaves for its
// icon and the action it already declared.
func (state *State) prepareEmptyPage(bounds gfx.Rect, title, sub, add string, descriptionColor gfx.Color, action gfx.ID, icon uint8, enabled bool) {
	state.empty.bounds = bounds
	state.empty.title, state.empty.sub, state.empty.add, state.empty.color = title, sub, add, descriptionColor
	state.empty.action, state.empty.icon, state.empty.enabled = action, icon, enabled
	state.empty.iconBox, state.empty.button = ui.Box{}, ui.Box{}
}

// drawReservedEmptyPage paints the icon the empty state's declaration laid out.
// The box is the region's declaration; its action is already part of the page.
func (state *State) drawReservedEmptyPage(canvas *gfx.Canvas, _ ui.Box, icon uint8) {
	state.paintEmptyPage(canvas, icon)
}

// paintEmptyPage paints the icon declared by the empty state. An empty label
// leaves the page without an action.
func (state *State) paintEmptyPage(canvas *gfx.Canvas, icon uint8) {
	paintIcon(canvas, icon, state.empty.iconBox.Rect(), state.pal.muted)
}
