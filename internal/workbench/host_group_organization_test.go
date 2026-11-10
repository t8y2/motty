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
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	source, ok := hostListGroupHit(canvas, state, state.hostGroupIndex("Team/Prod"))
	if !ok {
		t.Fatal("the source group row is not painted")
	}
	target, ok := hostListGroupHit(canvas, state, state.hostGroupIndex("Team2"))
	if !ok {
		t.Fatal("the target group row is not painted")
	}
	dragged := dragHostGroup(state, size, source, target)
	windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyEscape, Target: source.ID})
	releaseHostGroup(state, size, dragged, source, target)
	if state.hosts[0].Group != "Team/Prod/EU" {
		t.Fatal("Escape did not cancel the drag")
	}
	dragged = dragHostGroup(state, size, source, target)
	releaseHostGroup(state, size, dragged, source, target)
	if state.hosts[0].Group != "Team2/Prod/EU" {
		t.Fatal("drag did not move the entire group subtree")
	}
}

func TestHostGroupDragRejectsSelfDescendantAndNameCollision(t *testing.T) {
	state := nestedHostGroupState(t)
	state.hostGroups = append(state.hostGroups, hostGroup{Name: "Team2/Prod", Expanded: true, Explicit: true})
	state.refreshHostGroups()
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	source, ok := hostListGroupHit(canvas, state, state.hostGroupIndex("Team/Prod"))
	if !ok {
		t.Fatal("the source group row is not painted")
	}
	for _, targetPath := range []string{"Team/Prod", "Team/Prod/EU", "Team2"} {
		target, ok := hostListGroupHit(canvas, state, state.hostGroupIndex(targetPath))
		if !ok {
			t.Fatalf("target group %q is not painted", targetPath)
		}
		dragged := dragHostGroup(state, size, source, target)
		releaseHostGroup(state, size, dragged, source, target)
		if state.hosts[0].Group != "Team/Prod/EU" || state.hostGroupIndex("Team2/Prod") < 0 {
			t.Fatalf("drop on %q was accepted: hosts=%+v groups=%+v", targetPath, state.hosts, state.hostGroups)
		}
	}
}

func TestHostGroupDragPaintsTheDropIndicator(t *testing.T) {
	state := nestedHostGroupState(t)
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	source, ok := hostListGroupHit(canvas, state, state.hostGroupIndex("Team/Prod"))
	if !ok {
		t.Fatal("the source group row is not painted")
	}
	target, ok := hostListGroupHit(canvas, state, state.hostGroupIndex("Team2"))
	if !ok {
		t.Fatal("the target group row is not painted")
	}
	dragged := dragHostGroup(state, size, source, target)
	primary := state.Theme().Primary
	found := false
	for _, triangle := range dragged.Triangles() {
		if triangle.Color != primary {
			continue
		}
		inside := true
		for _, point := range triangle.Points {
			if point.X < target.Bounds.X-2 || point.X > target.Bounds.X+target.Bounds.Width+2 ||
				point.Y < target.Bounds.Y-2 || point.Y > target.Bounds.Y+target.Bounds.Height+2 {
				inside = false
			}
		}
		if inside {
			found = true
		}
	}
	if !found {
		t.Fatal("a drag over a group did not paint its drop indicator")
	}
	// Let the drag go without moving anything, so the test leaves no press behind.
	windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyEscape, Target: source.ID})
}
