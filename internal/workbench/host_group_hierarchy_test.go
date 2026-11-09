package workbench

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func nestedHostGroupState(t *testing.T) *State {
	t.Helper()
	state := newTestState(t, testOptions(t))
	state.hosts[0].Group = "Team/Prod/EU"
	state.hosts[1].Group = "Team/Dev"
	state.hosts[2].Group = "Team2"
	state.hostDataChanged()
	return state
}

func chooseHostGroupAction(t *testing.T, state *State, path, action string) {
	t.Helper()
	index := state.hostGroupIndex(path)
	if index < 0 {
		t.Fatalf("missing group %q", path)
	}
	feature := tree.Feature("host-group-actions")
	state.Click(tree.Sub(feature, tree.Name("more")).At(index))
	state.Click(tree.ID(feature, action))
}

func submitHostGroupName(state *State, name string) {
	state.hostGroupEditor = text.Editor{}
	state.hostGroupEditor.Insert(name)
	state.Click(hitMoveGroupSubmit)
}

func TestHostGroupRenameUpdatesSubtreeFilterExpansionAndPersistence(t *testing.T) {
	state := nestedHostGroupState(t)
	before := append([]Host(nil), state.hosts...)
	state.toggleHostGroup(state.hostGroupIndex("Team/Prod"))
	state.selectedHostID = state.hosts[2].ID
	state.setHostFilter(true, "Team/Prod/EU")
	chooseHostGroupAction(t, state, "Team", "rename")
	if state.modal != modalMoveHostGroup || state.hostGroupEditor.String() != "Team" {
		t.Fatalf("rename did not open a prefilled dialog: modal=%d text=%q", state.modal, state.hostGroupEditor.String())
	}
	submitHostGroupName(state, "Clients")
	if state.modal != modalNone || state.hosts[0].Group != "Clients/Prod/EU" || state.hosts[1].Group != "Clients/Dev" || state.hosts[2].Group != "Team2" {
		t.Fatalf("renamed subtree = %+v, error=%q", state.hosts, state.dialogError)
	}
	for index, host := range state.hosts {
		old := before[index]
		old.Group = host.Group
		if !reflect.DeepEqual(host, old) {
			t.Fatalf("rename changed connection fields: got %+v, want %+v", host, old)
		}
	}
	if !state.hostFilterOn || state.hostFilter != "Clients/Prod/EU" || state.hostGroups[state.hostGroupIndex("Clients/Prod")].Expanded {
		t.Fatal("rename lost filter or expansion state")
	}
	restarted := newTestState(t, state.options)
	if restarted.hosts[0].Group != "Clients/Prod/EU" || restarted.hostGroups[restarted.hostGroupIndex("Clients/Prod")].Expanded {
		t.Fatal("renamed subtree did not survive restart")
	}
}

func TestHostGroupRenameRejectsInvalidAndConflictingNamesAtomically(t *testing.T) {
	for _, name := range []string{"", "  ", "Bad/Path", "Bad\nName", "Bad\tName", "TEAM2", ".", ".."} {
		t.Run(name, func(t *testing.T) {
			state := nestedHostGroupState(t)
			before := append([]Host(nil), state.hosts...)
			chooseHostGroupAction(t, state, "Team", "rename")
			submitHostGroupName(state, name)
			if state.modal != modalMoveHostGroup || state.dialogError == "" || !reflect.DeepEqual(state.hosts, before) {
				t.Fatalf("invalid rename mutated state: modal=%d error=%q hosts=%+v", state.modal, state.dialogError, state.hosts)
			}
		})
	}
}

func TestHostGroupRenameChildAndCaseOnlyRename(t *testing.T) {
	state := nestedHostGroupState(t)
	chooseHostGroupAction(t, state, "Team/Prod", "rename")
	submitHostGroupName(state, "prod")
	if state.hosts[0].Group != "Team/prod/EU" || state.hosts[1].Group != "Team/Dev" {
		t.Fatalf("case-only child rename = %+v", state.hosts)
	}
	chooseHostGroupAction(t, state, "Team/prod", "rename")
	submitHostGroupName(state, "DEV")
	if state.modal != modalMoveHostGroup || state.dialogError == "" || state.hosts[0].Group != "Team/prod/EU" {
		t.Fatal("sibling conflict was merged or accepted")
	}
}

func TestHostGroupEditingEnterDoesNotSubmitIMEComposition(t *testing.T) {
	for _, action := range []string{"rename", "new-child"} {
		state := nestedHostGroupState(t)
		before := append([]Host(nil), state.hosts...)
		groups := len(state.hostGroups)
		chooseHostGroupAction(t, state, "Team", action)
		state.hostGroupEditor.SetMarkedText("中文", text.Range{Start: 2, End: 2})
		state.Key(gfx.KeyEvent{Target: hitMoveGroupField, Key: gfx.KeyEnter})
		if state.modal != modalMoveHostGroup || !reflect.DeepEqual(before, state.hosts) || len(state.hostGroups) != groups {
			t.Fatalf("%s submitted an IME candidate", action)
		}
	}
}

func TestHostGroupChildCreationAndCatalogRoundTrip(t *testing.T) {
	state := nestedHostGroupState(t)
	chooseHostGroupAction(t, state, "Team/Prod", "new-child")
	submitHostGroupName(state, "APAC")
	path := "Team/Prod/APAC"
	if state.hostGroupIndex(path) < 0 {
		t.Fatalf("child group not created: error=%q", state.dialogError)
	}
	state.hostDataChanged()
	restarted := newTestState(t, state.options)
	if restarted.hostGroupIndex(path) < 0 {
		t.Fatal("empty child disappeared on refresh/restart")
	}
	exportPath := filepath.Join(t.TempDir(), "groups.json")
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) { return exportPath, nil }
	state.exportHostCatalog()
	document, err := readHostCatalog(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	imported := newTestState(t, freshOptions(t))
	imported.applyHostCatalog(document)
	if imported.hostGroupIndex(path) < 0 {
		t.Fatal("empty child disappeared during catalog round trip")
	}
}

func TestExplicitHostGroupsRemainUsableWithoutHosts(t *testing.T) {
	state := nestedHostGroupState(t)
	chooseHostGroupAction(t, state, "Team", "new-child")
	submitHostGroupName(state, "Empty")
	state.hosts = nil
	state.hostDataChanged()
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitHostGroupMore.At(state.hostGroupIndex("Team/Empty"))); !ok {
		t.Fatal("empty groups have no actions after the last host is removed")
	}
	typeSearch(state, "Empty")
	if len(state.hostTreeRows) != 2 {
		t.Fatal("search omitted the empty group and its ancestor")
	}
	state.search = text.Editor{}
	typeSearch(state, "TEAM  EMPTY")
	if len(state.hostTreeRows) != 2 {
		t.Fatal("multi-word search omitted the empty group and its ancestor")
	}
}

func TestHostGroupRightClickNewHostPrefillsClickedGroup(t *testing.T) {
	for _, view := range []hostViewMode{hostViewList, hostViewGrid} {
		state := nestedHostGroupState(t)
		state.hostView = view
		index := state.hostGroupIndex("Team/Prod/EU")
		row := hitHostGroupRowList.At(index)
		if view == hostViewGrid {
			row = hitHostGroupOpenList.At(index)
		}
		pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: row, X: 400, Y: 300})
		canvas, _ := paintedTexts(state)
		newHost := tree.ID(tree.Feature("host-group-actions"), "new-host")
		if _, ok := hitByID(canvas, newHost); !ok {
			t.Fatalf("view %d did not offer New Host on group right click", view)
		}
		state.Click(newHost)
		if state.modal != modalNewHost || state.editor.value(fieldGroup) != "Team/Prod/EU" {
			t.Fatalf("group not prefilled: modal=%d group=%q", state.modal, state.editor.value(fieldGroup))
		}
		typeInto(state, hitDialogLabel, "New child host")
		typeInto(state, hitDialogAddress, "new.example.test")
		state.Click(hitDialogSubmit)
		if state.modal != modalNone || len(state.hosts) != 4 || state.hosts[3].Group != "Team/Prod/EU" {
			t.Fatalf("new host not saved into group: error=%q hosts=%+v", state.dialogError, state.hosts)
		}
	}
}

func TestNestedHostGroupsSynthesizeAncestorsAndCountSubtrees(t *testing.T) {
	state := nestedHostGroupState(t)
	var names []string
	for _, group := range state.hostGroups {
		names = append(names, group.Name)
	}
	if want := []string{"Team", "Team/Dev", "Team/Prod", "Team/Prod/EU", "Team2"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("groups = %v, want %v", names, want)
	}
	if got := state.hostGroups[state.hostGroupIndex("Team")].Digits; got != "2" {
		t.Fatalf("parent subtree count = %s, want 2", got)
	}
	var rows []string
	for _, row := range state.hostTreeRows {
		if row.Kind == hostTreeGroup {
			rows = append(rows, state.hostGroups[row.GroupIndex].Name)
		} else {
			rows = append(rows, state.hosts[row.HostIndex].Label)
		}
	}
	want := []string{"Team", "Team/Dev", state.hosts[1].Label, "Team/Prod", "Team/Prod/EU", state.hosts[0].Label, "Team2", state.hosts[2].Label}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("tree rows = %v, want %v", rows, want)
	}
}

func TestNestedHostGroupCollapseSearchAndParentFilter(t *testing.T) {
	state := nestedHostGroupState(t)
	parent := state.hostGroupIndex("Team")
	if parent < 0 {
		t.Fatal("missing implicit parent")
	}
	state.toggleHostGroup(parent)
	if got := len(state.hostTreeRows); got != 3 {
		t.Fatalf("collapsed parent leaves %d rows, want parent and unrelated group/host", got)
	}
	typeSearch(state, state.hosts[0].Label)
	if got := len(state.hostTreeRows); got != 4 {
		t.Fatalf("search should reveal all ancestors and the match, got %d rows", got)
	}
	if state.hostGroups[parent].Expanded {
		t.Fatal("search changed saved expansion")
	}
	state.search = text.Editor{}
	state.syncVisible()
	state.setHostFilter(true, "Team")
	state.syncVisible()
	if len(state.hostTreeRows) != 5 || !state.inHostFilter(state.hosts[0]) || state.inHostFilter(state.hosts[2]) {
		t.Fatalf("parent filter visible=%v rows=%+v, leaked sibling prefix or omitted descendant", state.visible, state.hostTreeRows)
	}
}

func TestFilteredHostGroupKeepsChildHierarchy(t *testing.T) {
	for _, mode := range []hostViewMode{hostViewList, hostViewGrid} {
		state := nestedHostGroupState(t)
		state.hostView = mode
		state.hosts[1].Group = "Team"
		state.hostDataChanged()
		state.toggleHostGroup(state.hostGroupIndex("Team"))
		state.setHostFilter(true, "Team")
		if state.hostsFlat() {
			t.Fatalf("view %d flattened a parent group", mode)
		}
		checkRows := func(want []hostTreeRow) {
			t.Helper()
			if !reflect.DeepEqual(state.hostTreeRows, want) {
				t.Fatalf("view %d filtered rows = %+v, want %+v", mode, state.hostTreeRows, want)
			}
		}
		prod := state.hostGroupIndex("Team/Prod")
		eu := state.hostGroupIndex("Team/Prod/EU")
		checkRows([]hostTreeRow{
			{Kind: hostTreeHost, GroupIndex: state.hostGroupIndex("Team"), HostIndex: 1, Depth: 0},
			{Kind: hostTreeGroup, GroupIndex: prod, HostIndex: -1, Depth: 0},
			{Kind: hostTreeGroup, GroupIndex: eu, HostIndex: -1, Depth: 1},
			{Kind: hostTreeHost, GroupIndex: eu, HostIndex: 0, Depth: 2},
		})
		canvas, _ := paintedTexts(state)
		if _, ok := hitByID(canvas, hitHostGroupOpenList.At(prod)); !ok {
			t.Fatalf("view %d did not render the filtered child group", mode)
		}
		state.toggleHostGroup(prod)
		checkRows([]hostTreeRow{
			{Kind: hostTreeHost, GroupIndex: state.hostGroupIndex("Team"), HostIndex: 1, Depth: 0},
			{Kind: hostTreeGroup, GroupIndex: prod, HostIndex: -1, Depth: 0},
		})
		state.setHostFilter(true, "Team/Prod")
		checkRows([]hostTreeRow{
			{Kind: hostTreeGroup, GroupIndex: eu, HostIndex: -1, Depth: 0},
			{Kind: hostTreeHost, GroupIndex: eu, HostIndex: 0, Depth: 1},
		})
		state.setHostFilter(true, "Team/Prod/EU")
		checkRows([]hostTreeRow{{Kind: hostTreeHost, GroupIndex: eu, HostIndex: 0, Depth: 0}})
	}
}

func TestNestedHostGroupOrderingKeepsPunctuationSiblingOutsideSubtree(t *testing.T) {
	state := nestedHostGroupState(t)
	state.hosts[2].Group = "Team-Other"
	state.hostDataChanged()
	if state.hostGroupIndex("Team-Other") < state.hostGroupIndex("Team/Prod/EU") {
		t.Fatal("a punctuation sibling interrupted the parent subtree")
	}
}
