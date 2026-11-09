package workbench

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestHostGroupMoveSubtreeAndRejectCyclesOrCollisions(t *testing.T) {
	for _, destination := range []string{"Team/Prod", "Team/Prod/EU", "Team/Prod/EU/Missing"} {
		state := nestedHostGroupState(t)
		before := append([]Host(nil), state.hosts...)
		chooseHostGroupAction(t, state, "Team/Prod", "move")
		submitHostGroupName(state, destination)
		if state.modal != modalMoveHostGroup || state.dialogError == "" || !reflect.DeepEqual(state.hosts, before) {
			t.Fatalf("invalid destination %q accepted: modal=%d error=%q", destination, state.modal, state.dialogError)
		}
	}
	state := nestedHostGroupState(t)
	chooseHostGroupAction(t, state, "Team/Prod", "move")
	submitHostGroupName(state, "Team2")
	if state.modal != modalNone || state.hosts[0].Group != "Team2/Prod/EU" || state.hosts[1].Group != "Team/Dev" {
		t.Fatalf("move did not keep subtree: error=%q groups=%+v", state.dialogError, state.hostGroups)
	}
	chooseHostGroupAction(t, state, "Team2/Prod", "move")
	submitHostGroupName(state, "")
	if state.hosts[0].Group != "Prod/EU" {
		t.Fatal("move to top level failed")
	}
}

func TestHostGroupMoveRejectsCaseInsensitiveDestinationConflict(t *testing.T) {
	state := nestedHostGroupState(t)
	state.hostGroups = append(state.hostGroups, hostGroup{Name: "Team2/PROD", Expanded: true, Explicit: true})
	state.refreshHostGroups()
	before := append([]Host(nil), state.hosts...)
	chooseHostGroupAction(t, state, "Team/Prod", "move")
	submitHostGroupName(state, "Team2")
	if state.modal != modalMoveHostGroup || state.dialogError == "" || !reflect.DeepEqual(before, state.hosts) {
		t.Fatal("move silently merged sibling groups")
	}
}

func TestHostGroupCatalogKeepsPinOrderAndEmptyGroupsInAnExistingCatalog(t *testing.T) {
	state := nestedHostGroupState(t)
	chooseHostGroupAction(t, state, "Team2", "pin")
	chooseHostGroupAction(t, state, "Team/Prod", "move-up")
	chooseHostGroupAction(t, state, "Team/Prod", "new-child")
	submitHostGroupName(state, "Empty")
	path := filepath.Join(t.TempDir(), "catalog.json")
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) { return path, nil }
	state.exportHostCatalog()
	document, err := readHostCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	imported := newTestState(t, freshOptions(t))
	imported.applyHostCatalog(document)
	if !imported.hostGroups[imported.hostGroupIndex("Team2")].Pinned || imported.hostGroupIndex("Team/Prod") > imported.hostGroupIndex("Team/Dev") {
		t.Fatal("group metadata lost during export/import")
	}
	// An existing implicit parent must gain explicit ownership when that same
	// path is imported as an independently created group.
	imported.applyHostCatalog(hostCatalogDocument{Groups: []savedHostGroup{{Name: "Team", Explicit: true}}})
	imported.hosts = nil
	imported.hostDataChanged()
	if imported.hostGroupIndex("Team") < 0 || !imported.hostGroups[imported.hostGroupIndex("Team")].Explicit {
		t.Fatal("import dropped explicit metadata on an existing group")
	}
}

func TestHostGroupCatalogPreservesEmptyAncestorMetadata(t *testing.T) {
	state := nestedHostGroupState(t)
	chooseHostGroupAction(t, state, "Team", "new-child")
	submitHostGroupName(state, "Empty")
	state.hosts[0].Group = ""
	state.hosts[1].Group = ""
	state.hostDataChanged()
	state.pinHostGroup("Team")
	state.toggleHostGroup(state.hostGroupIndex("Team"))
	path := filepath.Join(t.TempDir(), "catalog.json")
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) { return path, nil }
	state.exportHostCatalog()
	document, err := readHostCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	imported := newTestState(t, freshOptions(t))
	imported.applyHostCatalog(document)
	parent := imported.hostGroups[imported.hostGroupIndex("Team")]
	if !parent.Pinned || parent.Expanded {
		t.Fatal("empty ancestor lost its pin or collapse state during export/import")
	}
}

func TestHostGroupDeleteKeepsHostsInChosenDestinationAndCanCancel(t *testing.T) {
	for _, ungroup := range []bool{false, true} {
		state := nestedHostGroupState(t)
		before := append([]Host(nil), state.hosts...)
		chooseHostGroupAction(t, state, "Team/Prod", "delete")
		if state.modal != modalMoveHostGroup {
			t.Fatal("delete did not ask for confirmation")
		}
		state.Click(hitMoveGroupCancel)
		if !reflect.DeepEqual(before, state.hosts) {
			t.Fatal("cancelled deletion changed hosts")
		}
		chooseHostGroupAction(t, state, "Team/Prod", "delete")
		if ungroup {
			state.Click(tree.ID(tree.Feature("host-group-actions"), "delete-ungroup"))
		}
		state.Click(hitMoveGroupSubmit)
		want := "Team"
		if ungroup {
			want = ""
		}
		if state.modal != modalNone || len(state.hosts) != 3 || state.hosts[0].Group != want || state.hostGroupIndex("Team/Prod") >= 0 || state.hostGroupIndex("Team/Prod/EU") >= 0 {
			t.Fatalf("delete result hosts=%+v groups=%+v", state.hosts, state.hostGroups)
		}
		before[0].Group = want
		if !reflect.DeepEqual(before, state.hosts) {
			t.Fatal("delete changed unrelated connection fields")
		}
		restarted := newTestState(t, state.options)
		if restarted.hosts[0].Group != want || restarted.hostGroupIndex("Team/Prod") >= 0 {
			t.Fatal("deleted group returned after restart")
		}
	}
}

func TestHostGroupPinAndManualOrderStayWithinSiblingTreesAndPersist(t *testing.T) {
	state := nestedHostGroupState(t)
	chooseHostGroupAction(t, state, "Team2", "pin")
	if state.hostGroups[0].Name != "Team2" {
		t.Fatal("pinned root did not move ahead of unpinned roots")
	}
	chooseHostGroupAction(t, state, "Team2", "pin")
	chooseHostGroupAction(t, state, "Team2", "move-up")
	if state.hostGroups[0].Name != "Team2" {
		t.Fatal("manual sibling order was ignored")
	}
	chooseHostGroupAction(t, state, "Team/Prod", "move-up")
	if state.hostGroupIndex("Team/Prod") > state.hostGroupIndex("Team/Dev") || state.hostGroupIndex("Team/Prod/EU") > state.hostGroupIndex("Team/Dev") {
		t.Fatal("child ordering broke subtree contiguity")
	}
	restarted := newTestState(t, state.options)
	if restarted.hostGroups[0].Name != "Team2" || restarted.hostGroupIndex("Team/Prod") > restarted.hostGroupIndex("Team/Dev") {
		t.Fatal("manual ordering did not persist")
	}
}

func TestHostGroupDragMovesSubtreeAndEscapeCancels(t *testing.T) {
	state := nestedHostGroupState(t)
	canvas, _ := paintedTexts(state)
	source, _ := hitByID(canvas, hitHostGroupRowList.At(state.hostGroupIndex("Team/Prod")))
	target, _ := hitByID(canvas, hitHostGroupRowList.At(state.hostGroupIndex("Team2")))
	down := gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: source.ID, X: source.Bounds.X + 40, Y: source.Bounds.Y + 20}
	move := gfx.PointerEvent{Kind: gfx.PointerMove, Target: target.ID, X: target.Bounds.X + 40, Y: target.Bounds.Y + 20}
	up := move
	up.Kind, up.Button = gfx.PointerUp, gfx.PointerButtonPrimary
	pointer(state, down)
	pointer(state, move)
	state.Key(gfx.KeyEvent{Key: gfx.KeyEscape})
	pointer(state, up)
	if state.hosts[0].Group != "Team/Prod/EU" {
		t.Fatal("Escape did not cancel the drag")
	}
	pointer(state, down)
	pointer(state, move)
	pointer(state, up)
	if state.hosts[0].Group != "Team2/Prod/EU" {
		t.Fatal("drag did not move the entire group subtree")
	}
}

func TestHostGroupDragCannotTargetClippedRows(t *testing.T) {
	state := manyHostsState(t, 40)
	for index := range state.hosts {
		state.hosts[index].Group = "Team2"
	}
	state.hosts[0].Group = "Team/Dev"
	state.hostDataChanged()
	state.hostView = hostViewList
	size := gfx.Size{Width: 900, Height: 600}
	paintAt(state, size)
	group := state.hostGroupIndex("Team/Dev")
	row := state.groupActions.rows[group]
	// Scroll the measured row above the clip; dev builds have a taller sidebar.
	state.hostsScroll = row.Y + row.Height - state.chrome.hostList.Y + 1
	paintAt(state, size)
	row = state.groupActions.rows[group]
	x, y := row.X+row.Width/2, row.Y+row.Height/2
	if row.Width <= 0 || row.Height <= 0 || !pointInRect(x, y, row) || pointInRect(x, y, state.chrome.hostList) {
		t.Fatalf("fixture target is not clipped: row=%+v viewport=%+v scroll=%v", row, state.chrome.hostList, state.hostsScroll)
	}
	state.groupActions.drag = hostGroupDrag{pressed: true, active: true, path: "Team2"}
	state.trackHostGroupDrop(x, y)
	if state.groupActions.drag.valid {
		t.Fatal("drag accepted a group hidden behind the toolbar")
	}
}
