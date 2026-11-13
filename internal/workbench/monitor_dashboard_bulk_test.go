package workbench

import (
	"fmt"
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
)

func dashboardBulkFixture(test *testing.T) *State {
	test.Helper()
	state, _, _ := dashboardTestState(test)
	state.hosts = []Host{
		{ID: 1, CatalogID: "web-10", Label: "Web Z", Address: "10.0.0.10", Group: "Production"},
		{ID: 2, CatalogID: "web-2", Label: "Web A", Address: "10.0.0.2", Group: "Production"},
		{ID: 3, CatalogID: "db", Label: "Database", Address: "db.internal", Group: "Testing"},
		{ID: 4, CatalogID: "desktop", Label: "Desktop", Address: "10.0.0.3", Protocol: protocolRDP},
		{ID: localHostID, CatalogID: "local", Label: "Local"},
	}
	state.setLanguage(i18n.English)
	state.openDashboardEditor(false)
	return state
}

func TestDashboardBulkSelectionRespectsFilters(test *testing.T) {
	state := dashboardBulkFixture(test)
	state.dashboard.editor.selected = []string{"db", "removed", "web-10"}
	state.dashboard.editor.groupFilter = true
	state.dashboard.editor.group = "Production"
	state.Click(tree.ID(dashboardHits, "select-matches"))
	if want := []string{"db", "removed", "web-10", "web-2"}; !slices.Equal(state.dashboard.editor.selected, want) {
		test.Fatalf("select matching hosts = %v, want %v", state.dashboard.editor.selected, want)
	}
	state.Click(tree.ID(dashboardHits, "select-matches"))
	if want := []string{"db", "removed"}; !slices.Equal(state.dashboard.editor.selected, want) {
		test.Fatalf("clear matching hosts = %v, want %v", state.dashboard.editor.selected, want)
	}
	state.dashboard.editor.query.Insert("Web A")
	state.Click(tree.ID(dashboardHits, "select-matches"))
	if want := []string{"db", "removed", "web-2"}; !slices.Equal(state.dashboard.editor.selected, want) {
		test.Fatalf("searched scope = %v, want %v", state.dashboard.editor.selected, want)
	}
	state.dashboard.editor.selectedOnly = true
	state.Click(tree.ID(dashboardHits, "select-matches"))
	if want := []string{"db", "removed"}; !slices.Equal(state.dashboard.editor.selected, want) {
		test.Fatalf("selected-only scope = %v, want %v", state.dashboard.editor.selected, want)
	}
	state.Click(tree.ID(dashboardHits, "select-matches"))
	if want := []string{"db", "removed"}; !slices.Equal(state.dashboard.editor.selected, want) {
		test.Fatalf("empty selected scope changed selection: %v", state.dashboard.editor.selected)
	}
}

func TestDashboardBulkSelectionExcludesUnsupportedHosts(test *testing.T) {
	state := dashboardBulkFixture(test)
	state.hosts = append(state.hosts, Host{ID: 5, CatalogID: "mosh", Label: "Mosh", Terminal: terminalMosh})
	state.Click(tree.ID(dashboardHits, "select-matches"))
	if want := []string{"web-10", "web-2", "db"}; !slices.Equal(state.dashboard.editor.selected, want) {
		test.Fatalf("eligible selection = %v, want %v", state.dashboard.editor.selected, want)
	}
	state.dashboard.editor.query.Insert("not found")
	state.Click(tree.ID(dashboardHits, "select-matches"))
	if len(state.dashboard.editor.selected) != 3 {
		test.Fatal("empty search changed selection")
	}
}

func TestDashboardOrderModes(test *testing.T) {
	state := dashboardBulkFixture(test)
	state.hosts = append(state.hosts,
		Host{ID: 6, CatalogID: "ipv6", Label: "Web A", Address: "2001:db8::1"},
		Host{ID: 7, CatalogID: "web-tie", Label: "Web A", Address: "10.0.0.2"})
	state.dashboard.editor.selected = []string{"removed", "web-10", "db", "web-tie", "ipv6", "web-2"}
	state.dashboard.editor.page = 1
	state.Click(tree.Sub(dashboardHits, tree.Name("order-mode")).At(int(hostOrderIP)))
	if want := []string{"web-2", "web-tie", "web-10", "ipv6", "db", "removed"}; !slices.Equal(state.dashboard.editor.selected, want) {
		test.Fatalf("IP order = %v, want %v", state.dashboard.editor.selected, want)
	}
	state.Click(tree.Sub(dashboardHits, tree.Name("order-mode")).At(int(hostOrderName)))
	if want := []string{"db", "ipv6", "web-2", "web-tie", "web-10", "removed"}; !slices.Equal(state.dashboard.editor.selected, want) {
		test.Fatalf("name order = %v, want %v", state.dashboard.editor.selected, want)
	}
	state.Click(tree.Sub(dashboardHits, tree.Name("order-mode")).At(int(hostOrderManual)))
	if want := []string{"db", "ipv6", "web-2", "web-tie", "web-10", "removed"}; !slices.Equal(state.dashboard.editor.selected, want) {
		test.Fatalf("manual mode discarded arranged order: %v", state.dashboard.editor.selected)
	}
	state.dashboard.editor.name.Insert("Production")
	state.submitDashboard()
	before := slices.Clone(state.dashboardPanel().Hosts)
	state.openDashboardEditor(true)
	state.dashboard.editor.page = 1
	state.Click(tree.Sub(dashboardHits, tree.Name("order-mode")).At(int(hostOrderIP)))
	state.Click(hitDashboardDialogCancel)
	if !slices.Equal(state.dashboardPanel().Hosts, before) {
		test.Fatal("cancel changed the saved order")
	}
	state.openDashboardEditor(true)
	state.dashboard.editor.page = 1
	state.Click(tree.Sub(dashboardHits, tree.Name("order-mode")).At(int(hostOrderIP)))
	state.submitDashboard()
	restored := newTestState(test, testOptions(test))
	restored.restoreLocal(state.snapshot())
	if want := []string{"web-2", "web-tie", "web-10", "ipv6", "db", "removed"}; !slices.Equal(restored.dashboardPanel().Hosts, want) {
		test.Fatalf("restored order = %v, want %v", restored.dashboardPanel().Hosts, want)
	}
}

func TestDashboardOrderKeepsUnavailableHostsLast(test *testing.T) {
	state := dashboardBulkFixture(test)
	state.dashboard.editor.page = 1
	state.dashboard.editor.selected = []string{"desktop", "removed", "web-10", "web-2"}
	state.Click(tree.Sub(dashboardHits, tree.Name("order-mode")).At(int(hostOrderName)))
	if want := []string{"web-2", "web-10", "desktop", "removed"}; !slices.Equal(state.dashboard.editor.selected, want) {
		test.Fatalf("unavailable host order = %v, want %v", state.dashboard.editor.selected, want)
	}
}

func TestDashboardSelectionChangesResetOrderMode(test *testing.T) {
	for _, mode := range []hostOrderMode{hostOrderName, hostOrderIP} {
		for _, scenario := range []struct {
			name     string
			selected []string
			hit      gfx.ID
		}{
			{name: "add host", selected: []string{"web-10"}, hit: hitDashboardHostList.At(2)},
			{name: "remove host", selected: []string{"web-10", "web-2"}, hit: hitDashboardHostList.At(2)},
			{name: "remove unavailable host", selected: []string{"removed"}, hit: hitDashboardMissingList.At(0)},
			{name: "select matches", selected: []string{"web-10"}, hit: hitDashboardSelectMatches},
			{name: "clear matches", selected: []string{"web-10", "web-2", "db"}, hit: hitDashboardSelectMatches},
		} {
			test.Run(fmt.Sprintf("%s/%d", scenario.name, mode), func(test *testing.T) {
				state := dashboardBulkFixture(test)
				state.dashboard.editor.selected = slices.Clone(scenario.selected)
				state.sortDashboardHosts(mode)
				before := slices.Clone(state.dashboard.editor.selected)
				state.Click(scenario.hit)
				if slices.Equal(state.dashboard.editor.selected, before) {
					test.Fatal("fixture did not change the selection")
				}
				if state.dashboard.editor.orderMode != hostOrderManual {
					test.Fatalf("selection changed but order mode is %v, want manual", state.dashboard.editor.orderMode)
				}
			})
		}
	}
}

func TestDashboardEmptySelectionScopeKeepsOrderMode(test *testing.T) {
	state := dashboardBulkFixture(test)
	state.dashboard.editor.selected = []string{"web-10", "web-2"}
	state.sortDashboardHosts(hostOrderIP)
	state.dashboard.editor.query.Insert("no matches")
	state.Click(hitDashboardSelectMatches)
	if state.dashboard.editor.orderMode != hostOrderIP || !slices.Equal(state.dashboard.editor.selected, []string{"web-2", "web-10"}) {
		test.Fatal("empty matching scope changed the order or mode")
	}
}
