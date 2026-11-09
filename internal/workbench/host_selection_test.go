package workbench

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func bulkAllHit() gfx.ID {
	return tree.ID(tree.Feature("host-selection"), "all")
}

func bulkGroupHit(state *State, name string) gfx.ID {
	return tree.Sub(tree.Feature("host-selection"), tree.Name("group")).At(state.hostGroupIndex(name))
}

func requireBulkSelection(t *testing.T, state *State, ids ...uint32) {
	t.Helper()
	got := slices.Clone(state.hostSelections)
	slices.Sort(got)
	want := slices.Clone(ids)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("selected hosts = %v, want %v", got, want)
	}
}

func TestHostBulkAllMixedStateAndClearing(t *testing.T) {
	state := nestedHostGroupState(t)
	canvas, _ := paintedTexts(state)
	all, ok := hitByID(canvas, bulkAllHit())
	if !ok || all.Checked || all.Mixed || all.Disabled {
		t.Fatalf("initial select-all = %+v, found=%v", all, ok)
	}
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	canvas, _ = paintedTexts(state)
	all, _ = hitByID(canvas, bulkAllHit())
	if !all.Mixed {
		t.Fatal("partial selection is not exposed as mixed")
	}
	state.Click(bulkAllHit())
	requireBulkSelection(t, state, state.hosts[0].ID, state.hosts[1].ID, state.hosts[2].ID)
	canvas, _ = paintedTexts(state)
	all, _ = hitByID(canvas, bulkAllHit())
	if !all.Checked || all.Mixed {
		t.Fatal("complete selection is not checked")
	}
	state.Click(bulkAllHit())
	requireBulkSelection(t, state)
}

func TestHostBulkSearchScopePreservesOtherSelections(t *testing.T) {
	state := nestedHostGroupState(t)
	state.Click(hitHostCheckList.At(int(state.hosts[2].ID)))
	typeSearch(state, "Production")
	state.Click(bulkAllHit())
	requireBulkSelection(t, state, state.hosts[0].ID, state.hosts[2].ID)
	state.Click(bulkAllHit())
	requireBulkSelection(t, state, state.hosts[2].ID)
	typeSearch(state, "No matching host")
	canvas, _ := paintedTexts(state)
	all, ok := hitByID(canvas, bulkAllHit())
	if !ok || !all.Disabled || all.Mixed || all.Checked {
		t.Fatalf("empty scope = %+v, found=%v", all, ok)
	}
	state.Click(bulkAllHit())
	requireBulkSelection(t, state, state.hosts[2].ID)
}

func TestHostBulkGroupIncludesCollapsedDescendantsAndNotPrefixSibling(t *testing.T) {
	for _, mode := range []hostViewMode{hostViewList, hostViewGrid} {
		state := nestedHostGroupState(t)
		state.hostView = mode
		state.toggleHostGroup(state.hostGroupIndex("Team"))
		canvas, _ := paintedTexts(state)
		if _, ok := hitByID(canvas, bulkGroupHit(state, "Team")); !ok {
			t.Fatalf("view %d lacks a collapsed group checkbox", mode)
		}
		state.Click(bulkGroupHit(state, "Team"))
		requireBulkSelection(t, state, state.hosts[0].ID, state.hosts[1].ID)
		state.Click(hitHostCheckList.At(int(state.hosts[2].ID)))
		state.Click(bulkGroupHit(state, "Team"))
		requireBulkSelection(t, state, state.hosts[2].ID)
	}
}

func TestHostBulkGroupRespectsSearchAndFilter(t *testing.T) {
	state := nestedHostGroupState(t)
	typeSearch(state, "Production")
	state.Click(bulkGroupHit(state, "Team"))
	requireBulkSelection(t, state, state.hosts[0].ID)
	state.search = text.Editor{}
	state.syncVisible()
	state.setHostFilter(true, "Team/Dev")
	state.Click(bulkAllHit())
	requireBulkSelection(t, state, state.hosts[1].ID)
}

func TestHostBulkUnnamedAndEmptyGroups(t *testing.T) {
	state := nestedHostGroupState(t)
	state.hosts[2].Group = ""
	state.hostGroups = append(state.hostGroups, hostGroup{Name: "Empty", Explicit: true, Expanded: true})
	state.hostDataChanged()
	state.Click(bulkGroupHit(state, ""))
	requireBulkSelection(t, state, state.hosts[2].ID)
	canvas, _ := paintedTexts(state)
	empty, ok := hitByID(canvas, bulkGroupHit(state, "Empty"))
	if !ok || !empty.Disabled {
		t.Fatalf("empty group control = %+v, found=%v", empty, ok)
	}
}

func TestHostBulkKeyboardAndSubsetExport(t *testing.T) {
	state := nestedHostGroupState(t)
	result := state.Key(gfx.KeyEvent{Key: gfx.KeySpace, Target: bulkGroupHit(state, "Team")})
	if !result.Handled {
		t.Fatal("group checkbox Space was not handled")
	}
	exportPath := filepath.Join(t.TempDir(), "selected.json")
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) { return exportPath, nil }
	state.Click(hitExportHosts)
	document, err := readHostCatalog(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, host := range document.Hosts {
		labels = append(labels, host.Label)
	}
	if !slices.Equal(labels, []string{"Production", "Staging"}) {
		t.Fatalf("exported hosts = %v", labels)
	}
	state.Key(gfx.KeyEvent{Key: gfx.KeySpace, Target: bulkAllHit()})
	requireBulkSelection(t, state, state.hosts[0].ID, state.hosts[1].ID, state.hosts[2].ID)
}

func TestHostBulkControlsAreUnavailableDuringManualReorder(t *testing.T) {
	state := nestedHostGroupState(t)
	state.Click(hitHostSort)
	state.Click(bulkAllHit())
	state.Click(bulkGroupHit(state, "Team"))
	requireBulkSelection(t, state)
}
