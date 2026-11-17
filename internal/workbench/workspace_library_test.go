package workbench

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

func TestWorkspaceLibraryEmptyStateOffersCreation(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces

	canvas, texts := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	if !hasText(texts, state.messages.WorkspaceLibraryEmpty) || !hasText(texts, state.messages.WorkspaceLibraryEmptySub) {
		t.Fatalf("workspace library empty state is missing: %q", texts)
	}
	for _, id := range []gfx.ID{hitWorkspaceNew} {
		panelHit(t, canvas, id)
	}
	for _, id := range []gfx.ID{hitWorkspaces, hitHosts} {
		panelHit(t, canvas, id)
	}
}

func TestWorkspaceLibraryShowsTemplateFactsAndSelectsRows(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	production := mustWorkspaceTemplate(t, "Production", []string{"host_a", "host_b"}, 2)
	database := mustWorkspaceTemplate(t, "Database", []string{"host_a"}, 1)
	state.templates = []WorkspaceTemplate{production, database}

	canvas, texts := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	panelHit(t, canvas, hitWorkspaceTemplateRowList.At(0))
	panelHit(t, canvas, hitWorkspaceTemplateRowList.At(1))
	for _, want := range []string{production.Name, database.Name, state.messages.WorkspaceColumnsFmt, state.messages.WorkspaceTemplateMissingFmt} {
		if !hasText(texts, fmt.Sprintf(want, 2)) && !hasText(texts, fmt.Sprintf(want, "Database")) && !hasText(texts, want) {
			t.Fatalf("workspace library is missing %q: %q", want, texts)
		}
	}
	if state.selectedWorkspaceTemplateID != production.ID {
		t.Fatalf("initial selection = %q, want %q", state.selectedWorkspaceTemplateID, production.ID)
	}

	state.Click(hitWorkspaceTemplateRowList.At(1))
	if state.selectedWorkspaceTemplateID != database.ID {
		t.Fatalf("selected template = %q, want %q", state.selectedWorkspaceTemplateID, database.ID)
	}
	canvas, _ = paintAt(state, gfx.Size{Width: 1400, Height: 900})
	if !panelHit(t, canvas, hitWorkspaceTemplateRowList.At(1)).Selected {
		t.Fatal("the second template is not painted selected")
	}
	if panelHit(t, canvas, hitWorkspaceTemplateRowList.At(0)).Selected {
		t.Fatal("the first template is painted selected")
	}
	if hit := panelHit(t, canvas, hitWorkspaceTemplateOpen.At(1)); hit.Label != state.messages.WorkspaceEnter {
		t.Fatalf("open action label = %q, want %q", hit.Label, state.messages.WorkspaceEnter)
	}
}

func TestWorkspaceLibraryUsesAReadableCardRowAtEveryWidth(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	state.templates = []WorkspaceTemplate{
		mustWorkspaceTemplate(t, "Production API cluster", workspaceCatalogIDs(state, 0, 1), 2),
	}
	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 960, Height: 600}} {
		canvas, texts := paintAt(state, size)
		row := panelHit(t, canvas, hitWorkspaceTemplateRowList.At(0))
		if row.Role != gfx.RoleButton || !row.Selected || row.Hint != state.messages.WorkspaceOpenHint {
			t.Fatalf("%gx%g row semantics = %+v", size.Width, size.Height, row)
		}
		if row.Bounds.X < 0 || row.Bounds.X+row.Bounds.Width > size.Width {
			t.Fatalf("%gx%g row lies outside the window: %+v", size.Width, size.Height, row.Bounds)
		}
		if !hasText(texts, "Production API cluster") || !hasText(texts, fmt.Sprintf(state.messages.WorkspaceColumnsFmt, 2)) {
			t.Fatalf("%gx%g row text = %q", size.Width, size.Height, texts)
		}
		for _, text := range canvas.Texts() {
			if text.Bounds.X < row.Bounds.X || text.Bounds.X+text.Bounds.Width > row.Bounds.X+row.Bounds.Width ||
				text.Bounds.Y < row.Bounds.Y || text.Bounds.Y+text.Bounds.Height > row.Bounds.Y+row.Bounds.Height {
				continue
			}
			if canvas.TextWidthWeight(text.Value, text.Style.Size, text.Style.Weight) > text.Bounds.Width+0.5 {
				t.Fatalf("%gx%g row overflows at %q: %+v", size.Width, size.Height, text.Value, text.Bounds)
			}
		}
	}
}

func TestWorkspaceLibraryAccordionRowsStayInPlaceAtEveryWidth(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	productionHosts := workspaceCatalogIDs(state, 0, 1)
	apiHosts := workspaceCatalogIDs(state, 2)
	databaseHosts := workspaceCatalogIDs(state, 0)
	state.templates = []WorkspaceTemplate{
		mustWorkspaceTemplate(t, "Production", productionHosts, 2),
		mustWorkspaceTemplate(t, "API", apiHosts, 2),
		mustWorkspaceTemplate(t, "Database", databaseHosts, 1),
	}

	canvas, texts := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	production := panelHit(t, canvas, hitWorkspaceTemplateRowList.At(0))
	api := panelHit(t, canvas, hitWorkspaceTemplateRowList.At(1))
	database := panelHit(t, canvas, hitWorkspaceTemplateRowList.At(2))
	if !production.Expanded {
		t.Fatalf("wide production card = %+v, api card = %+v", production, api)
	}
	if production.Bounds.Width != api.Bounds.Width || api.Bounds.Width != database.Bounds.Width ||
		production.Bounds.X != api.Bounds.X || api.Bounds.X != database.Bounds.X {
		t.Fatalf("wide accordion rows are not full width: %+v %+v %+v", production.Bounds, api.Bounds, database.Bounds)
	}
	if !(production.Bounds.Y < api.Bounds.Y && api.Bounds.Y < database.Bounds.Y) {
		t.Fatalf("wide accordion rows are out of order: %+v %+v %+v", production.Bounds, api.Bounds, database.Bounds)
	}
	for _, host := range productionHosts {
		if !hasText(texts, state.hosts[hostIndexByCatalogID(t, state, host)].Label) {
			t.Fatalf("expanded card is missing host %q: %q", host, texts)
		}
	}
	for index := range productionHosts {
		panelHit(t, canvas, hitWorkspaceTemplateCards.At(index))
	}

	canvas, _ = paintAt(state, gfx.Size{Width: 960, Height: 600})
	production = panelHit(t, canvas, hitWorkspaceTemplateRowList.At(0))
	api = panelHit(t, canvas, hitWorkspaceTemplateRowList.At(1))
	database = panelHit(t, canvas, hitWorkspaceTemplateRowList.At(2))
	if production.Bounds.Width != api.Bounds.Width || api.Bounds.Width != database.Bounds.Width {
		t.Fatalf("compact cards are not equal width: %+v %+v %+v", production.Bounds, api.Bounds, database.Bounds)
	}
	if !(production.Bounds.Y < api.Bounds.Y && api.Bounds.Y < database.Bounds.Y) {
		t.Fatalf("compact accordion rows are out of order: %+v %+v %+v", production.Bounds, api.Bounds, database.Bounds)
	}

	state.Click(hitWorkspaceTemplateRowList.At(1))
	canvas, _ = paintAt(state, gfx.Size{Width: 960, Height: 600})
	if production := panelHit(t, canvas, hitWorkspaceTemplateRowList.At(0)); production.Expanded {
		t.Fatalf("old card stayed expanded: %+v", production)
	}
	if api := panelHit(t, canvas, hitWorkspaceTemplateRowList.At(1)); !api.Expanded || !api.Selected {
		t.Fatalf("clicked card did not expand inline: %+v", api)
	}
	if production := panelHit(t, canvas, hitWorkspaceTemplateRowList.At(0)); production.Bounds.Y >= api.Bounds.Y {
		t.Fatalf("opening a later row moved it out of order: production=%+v api=%+v", production.Bounds, api.Bounds)
	}

	state.Click(hitWorkspaceTemplateRowList.At(1))
	canvas, texts = paintAt(state, gfx.Size{Width: 960, Height: 600})
	if api := panelHit(t, canvas, hitWorkspaceTemplateRowList.At(1)); api.Expanded || api.Selected {
		t.Fatalf("clicking the open row did not collapse it: %+v", api)
	}
	if _, ok := hitByID(canvas, hitWorkspaceTemplateCards.At(0)); ok {
		t.Fatal("collapsed accordion still paints host cards")
	}
}

func TestWorkspaceLibraryApplyLaunchesOnlyTheSelectedTemplate(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	production := mustWorkspaceTemplate(t, "Production", []string{workspaceHostCatalogID(t, state, "Production")}, 1)
	database := mustWorkspaceTemplate(t, "Database", []string{workspaceHostCatalogID(t, state, "Staging")}, 2)
	state.templates = []WorkspaceTemplate{production, database}
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	state.Click(hitWorkspaceTemplateRowList.At(1))
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	state.Click(hitWorkspaceTemplateOpen.At(1))

	if len(world.configs) != 1 || world.configs[0].Host != "staging.example.com" {
		t.Fatalf("dials = %+v, want only %s", world.configs, database.Name)
	}
	if state.activeSection != sectionTerminal || state.workspaceTabs[len(state.workspaceTabs)-1].Title != database.Name {
		t.Fatalf("section=%d tabs=%+v", state.activeSection, state.workspaceTabs)
	}
	if state.broadcast != 0 {
		t.Fatalf("applying a template enabled broadcast: %d", state.broadcast)
	}
}

func TestWorkspaceLibraryOpensTemplatesByDoubleClick(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "Production", []string{
		workspaceHostCatalogID(t, state, "Production"),
		workspaceHostCatalogID(t, state, "Staging"),
	}, 2)
	state.templates = []WorkspaceTemplate{template}
	paintAt(state, gfx.Size{Width: 1400, Height: 900})

	first := pointer(state, gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1,
		Target: hitWorkspaceTemplateRowList.At(0),
		X:      100, Y: 220, Viewport: gfx.Size{Width: 1400, Height: 900},
	})
	if first.Handled {
		t.Fatalf("first press = %+v", first)
	}
	state.Click(hitWorkspaceTemplateRowList.At(0))
	if len(world.configs) != 0 {
		t.Fatalf("single click launched %d sessions", len(world.configs))
	}

	result := pointer(state, gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 2,
		Target: hitWorkspaceTemplateRowList.At(0),
		X:      100, Y: 220, Viewport: gfx.Size{Width: 1400, Height: 900},
	})
	if !result.Handled || !result.Repaint {
		t.Fatalf("double click = %+v", result)
	}
	if len(world.configs) != 2 || state.activeSection != sectionTerminal {
		t.Fatalf("double click dialed %d and left section %d", len(world.configs), state.activeSection)
	}
	if state.deck.ActiveWorkspace() == 0 || state.workspaceTabs[len(state.workspaceTabs)-1].Title != template.Name {
		t.Fatalf("double click opened %+v", state.workspaceTabs)
	}
}

func TestWorkspaceLibraryDoubleClickKeepsTheFirstHitAfterCardsMove(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	state.templates = []WorkspaceTemplate{
		mustWorkspaceTemplate(t, "Production", workspaceCatalogIDs(state, 0, 1), 2),
		mustWorkspaceTemplate(t, "API", workspaceCatalogIDs(state, 2), 1),
		mustWorkspaceTemplate(t, "Database", workspaceCatalogIDs(state, 0), 1),
	}
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	api := panelHit(t, canvas, hitWorkspaceTemplateRowList.At(1))
	point := gfx.Point{X: api.Bounds.X + api.Bounds.Width/2, Y: api.Bounds.Y + api.Bounds.Height/2}

	first := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1,
		Target: hitWorkspaceTemplateRowList.At(1), X: point.X, Y: point.Y,
		Viewport: gfx.Size{Width: 1400, Height: 900},
	})
	if first.Handled {
		t.Fatalf("first press opened a workspace: %+v", first)
	}
	state.Click(hitWorkspaceTemplateRowList.At(1))
	paintAt(state, gfx.Size{Width: 1400, Height: 900})

	second := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 2,
		Target: hitWorkspaceTemplateRowList.At(2), X: point.X, Y: point.Y,
		Viewport: gfx.Size{Width: 1400, Height: 900},
	})
	if !second.Handled || len(world.configs) != 1 || world.configs[0].Host != "192.168.1.1" {
		t.Fatalf("second press = %+v, dials = %+v", second, world.configs)
	}
}

func TestWorkspaceLibraryOpensTheSelectedTemplateWithEnter(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "Production", []string{
		workspaceHostCatalogID(t, state, "Production"),
	}, 1)
	state.templates = []WorkspaceTemplate{template}
	paintAt(state, gfx.Size{Width: 1400, Height: 900})

	for _, target := range []gfx.ID{hitWorkspaceTemplateRowList.At(0), gfx.ID{}} {
		world.configs = nil
		state.activeSection = sectionWorkspaces
		result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: target})
		if !result.Handled || len(world.configs) != 1 || state.activeSection != sectionTerminal {
			t.Fatalf("Enter on %v = %+v dials=%d", target, result, len(world.configs))
		}
	}
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitWorkspaceTemplateRowList.At(0), Repeat: true}); result.Handled {
		t.Fatal("a repeated Enter launched another workspace")
	}
}

func TestWorkspaceLibraryApplyWithEveryHostMissingDoesNothing(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "Gone", []string{"host_deleted"}, 1)
	state.templates = []WorkspaceTemplate{template}
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	workspaces, sessions, dials := len(state.deck.Workspaces(nil)), len(state.sessions), len(world.configs)

	state.Click(hitWorkspaceTemplateOpen.At(0))

	if len(state.deck.Workspaces(nil)) != workspaces || len(state.sessions) != sessions || len(world.configs) != dials {
		t.Fatalf("missing template changed tabs=%d sessions=%d dials=%d", len(state.deck.Workspaces(nil)), len(state.sessions), len(world.configs))
	}
	notice := state.fileNotices.items
	if len(notice) != 1 || notice[0].kind != components.ToastWarning || !strings.Contains(notice[0].detail, state.messages.WorkspaceTemplateNoHosts) {
		t.Fatalf("missing template notice = %+v", notice)
	}
}

func TestWorkspaceLibraryApplyWithPartialHostsLaunchesTheRest(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "Mixed", []string{workspaceHostCatalogID(t, state, "Production"), "host_deleted"}, 2)
	state.templates = []WorkspaceTemplate{template}
	paintAt(state, gfx.Size{Width: 1400, Height: 900})

	state.Click(hitWorkspaceTemplateOpen.At(0))

	if len(world.configs) != 1 || world.configs[0].Host != "prod.example.com" {
		t.Fatalf("dials = %+v, want the one catalog host", world.configs)
	}
	notice := state.fileNotices.items
	if len(notice) != 1 || notice[0].kind != components.ToastWarning || !strings.Contains(notice[0].detail, "host_deleted") {
		t.Fatalf("partial missing notice = %+v", notice)
	}
}

func workspaceHostCatalogID(t testing.TB, state *State, label string) string {
	t.Helper()
	for _, host := range state.hosts {
		if host.Label == label {
			return host.CatalogID
		}
	}
	t.Fatalf("test host %q is not loaded", label)
	return ""
}

func hostIndexByCatalogID(t testing.TB, state *State, catalogID string) int {
	t.Helper()
	for index, host := range state.hosts {
		if host.CatalogID == catalogID {
			return index
		}
	}
	t.Fatalf("test host %q is not loaded", catalogID)
	return -1
}
