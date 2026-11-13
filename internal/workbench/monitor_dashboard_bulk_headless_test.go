package workbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func dashboardBulkView(test *testing.T, state *State, width, height int, scale float64) *headless.Session {
	test.Helper()
	view, err := headless.Open(app.Config{
		Title: "Motty", Width: width, Height: height, Tree: Tree(), Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
		OnScroll: state.Scroll, OnResize: state.Resize,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if editor := state.EditorFor(context.ID); editor != nil {
				return &app.TextInput{Editor: editor, NavigationKeys: context.NavigationKeys}
			}
			return nil
		},
	}, nil, headless.Options{Scale: scale})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(view.Close)
	return view
}

func dashboardBulkClick(test *testing.T, view *headless.Session, label string) {
	test.Helper()
	if err := view.ClickLabel(label); err != nil {
		test.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		test.Fatal(err)
	}
}

func dashboardBulkScreenshot(test *testing.T, view *headless.Session, name string) {
	test.Helper()
	if err := view.Rest(); err != nil {
		test.Fatal(err)
	}
	directory := os.Getenv("MOTTY_DASHBOARD_BULK_SCREENSHOTS")
	if directory == "" {
		return
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		test.Fatal(err)
	}
	if err := view.Screenshot(filepath.Join(directory, name+".png")); err != nil {
		test.Fatal(err)
	}
	snapshot, err := json.MarshalIndent(view.Snapshot(), "", "  ")
	if err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name+".json"), snapshot, 0o644); err != nil {
		test.Fatal(err)
	}
}

func TestDashboardBulkHeadlessSelectsOffscreenMatches(test *testing.T) {
	state := dashboardBulkFixture(test)
	for index := 0; index < 93; index++ {
		state.hosts = append(state.hosts, Host{ID: uint32(index + 10), CatalogID: fmt.Sprintf("node-%02d", index), Label: fmt.Sprintf("api-node-%02d", index), Address: fmt.Sprintf("10.20.0.%d", index+1), Group: "Production"})
	}
	state.dashboard.editor.selected = []string{"db", "removed", "web-10"}
	view := dashboardBulkView(test, state, 1440, 1000, 1)
	messages := state.messages.Dashboard
	dashboardBulkClick(test, view, messages.GroupFilter)
	dashboardBulkClick(test, view, "Production")
	check, ok := view.Hit(messages.SelectMatches)
	if !ok || !check.Mixed || check.Checked || check.Role != gfx.RoleCheckbox {
		test.Fatalf("partial selection checkbox = %+v", check)
	}
	dashboardBulkClick(test, view, messages.SelectMatches)
	if len(state.dashboard.editor.selected) != 97 || !slices.Contains(state.dashboard.editor.selected, "node-92") {
		test.Fatalf("all filtered hosts, including offscreen rows, were not selected: %d", len(state.dashboard.editor.selected))
	}
	check, _ = view.Hit(messages.SelectMatches)
	if !check.Checked || check.Mixed {
		test.Fatal("full matching selection was not exposed as checked")
	}
	dashboardBulkScreenshot(test, view, "monitor-select-all-matches")
	viewport := state.dashboard.editor.bounds
	if err := view.Scroll(gfx.ScrollEvent{X: viewport.X + 20, Y: viewport.Y + 20, DeltaY: 5000}); err != nil {
		test.Fatal(err)
	}
	if state.dashboard.editor.scroll <= 0 {
		test.Fatal("host picker did not scroll")
	}
	dashboardBulkClick(test, view, messages.SelectMatches)
	if !slices.Equal(state.dashboard.editor.selected, []string{"db", "removed"}) {
		test.Fatalf("clear scope changed outside selections: %v", state.dashboard.editor.selected)
	}
	dashboardBulkClick(test, view, messages.HostSearch)
	if err := view.Type("no matches"); err != nil {
		test.Fatal(err)
	}
	index := slices.IndexFunc(view.Snapshot().Hits, func(hit gfx.Hit) bool { return hit.ID == hitDashboardSelectMatches })
	if index < 0 || !view.Snapshot().Hits[index].Disabled {
		test.Fatal("empty bulk scope is enabled")
	}
	if err := view.Keys("escape"); err != nil {
		test.Fatal(err)
	}
	if state.modal != modalNone || len(state.dashboard.panels) != 0 || len(state.dashboard.sources) != 0 {
		test.Fatal("canceling the picker saved or connected hosts")
	}
}

func TestDashboardBulkHeadlessOrdersAndPersists(test *testing.T) {
	state := dashboardBulkFixture(test)
	view := dashboardBulkView(test, state, 1440, 1000, 1)
	messages := state.messages.Dashboard
	dashboardBulkClick(test, view, messages.Name)
	if err := view.Type("Operations"); err != nil {
		test.Fatal(err)
	}
	dashboardBulkClick(test, view, messages.SelectMatches)
	dashboardBulkClick(test, view, messages.OrderTab)
	dashboardBulkClick(test, view, messages.SortBy)
	if err := view.Keys("escape"); err != nil {
		test.Fatal(err)
	}
	if state.modal != modalDashboard || state.dashboard.editor.orderOpen {
		test.Fatal("Escape closed the dialog instead of the order menu")
	}
	if err := view.Keys("down"); err != nil {
		test.Fatal(err)
	}
	if !state.dashboard.editor.orderOpen {
		test.Fatal("Down did not reopen the order select")
	}
	if err := view.Keys("end"); err != nil {
		test.Fatal(err)
	}
	if err := view.Keys("enter"); err != nil {
		test.Fatal(err)
	}
	if want := []string{"web-2", "web-10", "db"}; !slices.Equal(state.dashboard.editor.selected, want) {
		test.Fatalf("keyboard IP order = %v, want %v", state.dashboard.editor.selected, want)
	}
	dashboardBulkScreenshot(test, view, "monitor-order-by-ip")
	dashboardBulkClick(test, view, fmt.Sprintf(messages.SortPanel, "Web A"))
	if err := view.Keys("end"); err != nil {
		test.Fatal(err)
	}
	if state.dashboard.editor.orderMode != hostOrderManual || !slices.Equal(state.dashboard.editor.selected, []string{"web-10", "db", "web-2"}) {
		test.Fatal("manual keyboard reorder did not retain the draft and reset manual mode")
	}
	dashboardBulkClick(test, view, messages.SortBy)
	dashboardBulkClick(test, view, state.messages.HostCatalog.Name)
	dashboardBulkClick(test, view, messages.Create)
	if state.modal != modalNone || !slices.Equal(state.dashboardPanel().Hosts, []string{"db", "web-2", "web-10"}) || len(state.dashboard.sources) != 0 {
		test.Fatal("save lost the name order or started collection")
	}
	restored := newTestState(test, testOptions(test))
	restored.restoreLocal(state.snapshot())
	if !slices.Equal(restored.dashboardPanel().Hosts, []string{"db", "web-2", "web-10"}) {
		test.Fatal("saved ordering did not survive reload")
	}
}

func TestDashboardBulkHeadlessSelectionResetsOrderMode(test *testing.T) {
	state := dashboardBulkFixture(test)
	state.dashboard.editor.selected = []string{"web-10"}
	view := dashboardBulkView(test, state, 1440, 1000, 1)
	messages := state.messages.Dashboard
	dashboardBulkClick(test, view, messages.OrderTab)
	dashboardBulkClick(test, view, messages.SortBy)
	dashboardBulkClick(test, view, state.messages.HostCatalog.IP)
	tabs := view.Snapshot().Hits
	index := slices.IndexFunc(tabs, func(hit gfx.Hit) bool { return hit.ID == hitDashboardPickTab })
	if index < 0 {
		test.Fatal("host selection tab is missing")
	}
	tab := tabs[index].Bounds
	if err := view.Click(tab.X+tab.Width/2, tab.Y+tab.Height/2); err != nil {
		test.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		test.Fatal(err)
	}
	dashboardBulkClick(test, view, "Web A")
	dashboardBulkClick(test, view, messages.OrderTab)
	if state.dashboard.editor.orderMode != hostOrderManual || !slices.Equal(state.dashboard.editor.selected, []string{"web-10", "web-2"}) {
		test.Fatal("selection changed without resetting the sort label to manual")
	}
	dashboardBulkClick(test, view, messages.SortBy)
	manual, found := view.Hit(state.messages.HostCatalog.Manual)
	if !found || !manual.Selected {
		test.Fatalf("manual order is not selected in the order menu: %+v", manual)
	}
	if err := view.Keys("escape"); err != nil {
		test.Fatal(err)
	}
	dashboardBulkScreenshot(test, view, "monitor-order-after-selection")
}

func TestDashboardBulkHeadlessLayoutAcrossLanguagesAndScales(test *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English, i18n.TraditionalChinese} {
		for _, scale := range []float64{1, 1.5, 2} {
			test.Run(fmt.Sprintf("%s-%g", language, scale), func(test *testing.T) {
				state := dashboardBulkFixture(test)
				state.setLanguage(language)
				state.setAppearance(appearanceLight)
				view := dashboardBulkView(test, state, 700, 650, scale)
				messages := state.messages.Dashboard
				checkLayout := func() {
					test.Helper()
					for _, problem := range headless.CheckLayout(view.Snapshot()) {
						if problem.Text == messages.SelectMatches || problem.Text == messages.SortBy || strings.Contains(problem.Text, " / ") || problem.Kind == headless.LayoutCollide && (problem.Text == messages.SelectMatches || problem.Text == state.messages.HostCatalog.Manual) {
							test.Errorf("bulk control layout: %+v", problem)
						}
					}
				}
				checkLayout()
				dashboardBulkClick(test, view, messages.SelectMatches)
				checkLayout()
				if language == i18n.Chinese && scale == 1 {
					dashboardBulkScreenshot(test, view, "monitor-narrow-selected")
				}
				dashboardBulkClick(test, view, messages.OrderTab)
				dashboardBulkClick(test, view, messages.SortBy)
				checkLayout()
				dashboardBulkClick(test, view, state.messages.HostCatalog.IP)
				checkLayout()
			})
		}
	}
}
