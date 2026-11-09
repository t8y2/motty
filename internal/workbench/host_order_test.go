package workbench

import (
	"reflect"
	"slices"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

func chooseDisplayOrder(state *State, order string) {
	feature := tree.Feature("host-order")
	state.Click(tree.ID(feature, "trigger"))
	state.Click(tree.ID(feature, order))
}

func displayHostIDs(state *State) []string {
	var ids []string
	for _, row := range state.hostTreeRows {
		if row.Kind == hostTreeHost {
			ids = append(ids, state.hosts[row.HostIndex].CatalogID)
		}
	}
	return ids
}

func displayOrderState(t *testing.T) *State {
	t.Helper()
	state := newTestState(t, freshOptions(t))
	state.hosts = []Host{
		{ID: 1, CatalogID: "host_z", Label: "zulu", Address: "192.168.1.10", Group: "Servers", Port: 22},
		{ID: 2, CatalogID: "host_b", Label: "Alpha", Address: "192.168.1.2", Group: "Servers", Port: 22},
		{ID: 3, CatalogID: "host_a", Label: "Alpha", Address: "2001:db8::10", Group: "Servers", Port: 22},
		{ID: 4, CatalogID: "host_c", Label: "alpha", Address: "2001:db8::2", Group: "Servers", Port: 22},
		{ID: 5, CatalogID: "host_d", Label: "Domain", Address: "z.example.test", Group: "Servers", Port: 22},
		{ID: 6, CatalogID: "host_e", Label: "Domain", Address: "A.example.test", Group: "Servers", Port: 22},
		{ID: 7, CatalogID: "host_f", Label: "Mapped", Address: "::ffff:192.168.1.1", Group: "Servers", Port: 22},
	}
	state.nextHostID = 8
	for index := range state.hosts {
		state.hosts[index].finish()
	}
	state.hostDataChanged()
	state.save()
	return state
}

func TestHostDisplayOrderNameIsDeterministicAndPreservesManual(t *testing.T) {
	state := displayOrderState(t)
	original := slices.Clone(state.hosts)
	state.Click(hitHostCheckList.At(2))
	chooseDisplayOrder(state, "name")
	want := []string{"host_a", "host_b", "host_c", "host_d", "host_e", "host_f", "host_z"}
	if got := displayHostIDs(state); !slices.Equal(got, want) {
		t.Fatalf("name order = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(state.hosts, original) || !state.hostSelected(2) {
		t.Fatal("display order rewrote the catalog or selection identity")
	}
	slices.Reverse(state.hosts)
	state.hostDataChanged()
	if got := displayHostIDs(state); !slices.Equal(got, want) {
		t.Fatalf("reversed catalog changed display order: %v", got)
	}
	slices.Reverse(state.hosts)
	state.hostDataChanged()
	chooseDisplayOrder(state, "manual")
	if got := displayHostIDs(state); !slices.Equal(got, []string{"host_z", "host_b", "host_a", "host_c", "host_d", "host_e", "host_f"}) {
		t.Fatalf("manual order not restored: %v", got)
	}
}

func TestHostDisplayOrderIPUsesNumericAddressesAndDomainFallback(t *testing.T) {
	state := displayOrderState(t)
	chooseDisplayOrder(state, "ip")
	want := []string{"host_f", "host_b", "host_z", "host_c", "host_a", "host_e", "host_d"}
	if got := displayHostIDs(state); !slices.Equal(got, want) {
		t.Fatalf("IP order = %v, want %v", got, want)
	}
	slices.Reverse(state.hosts)
	state.hostDataChanged()
	if got := displayHostIDs(state); !slices.Equal(got, want) {
		t.Fatalf("IP order depends on input order: %v", got)
	}
	restarted := newTestState(t, state.options)
	if got := displayHostIDs(restarted); !slices.Equal(got, want) {
		t.Fatalf("IP order did not survive restart: %v", got)
	}
}

func TestHostDisplayOrderKeepsNestedSubtreesAndManualGroupMetadata(t *testing.T) {
	state := nestedHostGroupState(t)
	state.pinHostGroup("Team2")
	state.reorderHostGroup("Team/Prod", -1)
	chooseDisplayOrder(state, "name")
	var names []string
	for _, group := range state.hostGroups {
		names = append(names, group.Name)
	}
	if !slices.Equal(names, []string{"Team", "Team/Dev", "Team/Prod", "Team/Prod/EU", "Team2"}) {
		t.Fatalf("automatic group order = %v", names)
	}
	chooseDisplayOrder(state, "manual")
	if state.hostGroups[0].Name != "Team2" || state.hostGroupIndex("Team/Prod/EU") > state.hostGroupIndex("Team/Dev") {
		t.Fatal("automatic ordering changed pin/manual subtree order")
	}
}

func TestHostDisplayOrderWorksWithSearchAndGroupFilter(t *testing.T) {
	state := displayOrderState(t)
	chooseDisplayOrder(state, "name")
	typeSearch(state, "alpha")
	state.setHostFilter(true, "Servers")
	if got := displayHostIDs(state); !slices.Equal(got, []string{"host_a", "host_b", "host_c"}) {
		t.Fatalf("filtered order = %v", got)
	}
	state.Click(bulkAllHit())
	requireBulkSelection(t, state, 2, 3, 4)
}

func TestHostDisplayOrderAutomaticDisablesManualMoves(t *testing.T) {
	state := displayOrderState(t)
	state.Click(hitHostSort)
	if !state.hostSort.enabled {
		t.Fatal("manual reorder did not start")
	}
	chooseDisplayOrder(state, "ip")
	if state.hostSort.enabled {
		t.Fatal("automatic order did not cancel manual reorder")
	}
	state.Click(hitHostSort)
	if state.hostSort.enabled {
		t.Fatal("manual reorder started under automatic display order")
	}
	before := slices.Clone(state.hosts)
	move := components.ReorderGridMove{ID: hitHostSortHandles.At(1), Target: hitHostSortHandles.At(2), From: 0, To: 1}
	state.applyHostSortMove(move)
	if !reflect.DeepEqual(state.hosts, before) {
		t.Fatal("stale reorder gesture rewrote an automatically ordered catalog")
	}
	canvas, _ := paintedTexts(state)
	if hit, ok := hitByID(canvas, hitHostSort); !ok || !hit.Disabled {
		t.Fatal("manual reorder button is not disabled under automatic order")
	}
}

func TestHostDisplayOrderGroupManualActionsAreDisabled(t *testing.T) {
	state := nestedHostGroupState(t)
	chooseDisplayOrder(state, "name")
	state.Click(hitHostGroupMore.At(state.hostGroupIndex("Team/Dev")))
	canvas, _ := paintedTexts(state)
	for _, id := range []gfx.ID{hitGroupUp, hitGroupDown} {
		hit, ok := hitByID(canvas, id)
		if !ok || !hit.Disabled {
			t.Fatalf("manual group action %v is enabled under automatic order", id)
		}
	}
}

func TestHostDisplayOrderMenuIndicatesCurrentChoice(t *testing.T) {
	state := displayOrderState(t)
	for _, choice := range []struct {
		name string
		id   gfx.ID
	}{{"name", hitHostOrderName}, {"ip", hitHostOrderIP}, {"manual", hitHostOrderManual}} {
		chooseDisplayOrder(state, choice.name)
		state.Click(hitHostOrder)
		canvas, _ := paintedTexts(state)
		for _, id := range []gfx.ID{hitHostOrderManual, hitHostOrderName, hitHostOrderIP} {
			hit, ok := hitByID(canvas, id)
			if !ok || hit.Selected != (id == choice.id) {
				t.Fatalf("current %s menu item %s: %+v", choice.name, tree.Text(id), hit)
			}
		}
		state.Click(hitHostOrderDismiss)
	}
}
