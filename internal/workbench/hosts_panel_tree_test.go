package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func hostsPanelGroupRowOf(state *State, name string) (int, bool) {
	for index, entry := range state.hostsPanel.rows {
		if entry.Kind == hostTreeGroup && state.hostGroups[entry.GroupIndex].Name == name {
			return index, true
		}
	}
	return 0, false
}

func TestHostsPanelNamesGroupsByLeafNotPath(t *testing.T) {
	state := hostsPanelFixture(t)
	state.hosts[0].Group = "prod/eu"
	state.refreshHostGroups()
	state.visible = nil
	_, texts := paintedTexts(state)
	if !contains(texts, "eu") {
		t.Error("the panel does not show the child group's own name")
	}
	if contains(texts, "prod/eu") {
		t.Error("the panel shows the child group as its path")
	}
}

func TestHostsPanelGroupNodeOpensItsActions(t *testing.T) {
	state := hostsPanelFixture(t)
	row, ok := hostsPanelGroupRowOf(state, "prod")
	if !ok {
		t.Fatal("prod is not listed")
	}
	target := state.hostsPanel.tree.RowID(row)
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: target,
		X: 40, Y: 200, Viewport: panelWide,
	})
	if !state.groupActions.open || state.groupActions.path != "prod" {
		t.Fatalf("group actions = %+v, want prod", state.groupActions)
	}
	canvas, _ := paintedTexts(state)
	for _, id := range []gfx.ID{hitGroupNewHost, hitGroupNewChild, hitGroupRename, hitGroupMove, hitGroupDelete} {
		if hit, ok := hitByID(canvas, id); !ok || hit.Disabled {
			t.Errorf("group action %s: found=%v disabled=%v", tree.Text(id), ok, hit.Disabled)
		}
	}
}

func TestHostsPanelEmptySpaceCreatesATopLevelGroup(t *testing.T) {
	state := hostsPanelFixture(t)
	body := state.hostsPanel.body
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary,
		X: body.X + 20, Y: body.Y + body.Height - 10, Viewport: panelWide,
	})
	if !state.hostBlankMenu.open {
		t.Fatal("a secondary press on the empty panel opened nothing")
	}
	canvas, _ := paintedTexts(state)
	for _, id := range []gfx.ID{hitHostBlankNewHost, hitHostBlankNewGroup} {
		if _, ok := hitByID(canvas, id); !ok {
			t.Errorf("blank menu row %s is not painted", tree.Text(id))
		}
	}
	chooseMenu(t, state, hitHostBlankNewGroup)
	if state.modal != modalMoveHostGroup || state.groupActions.op != hostGroupCreateRoot {
		t.Fatalf("new root group: modal %v op %v", state.modal, state.groupActions.op)
	}
	state.hostGroupEditor.Insert("ops")
	state.Click(hitMoveGroupSubmit)
	if state.hostGroupIndex("ops") < 0 {
		t.Fatal("the root group was not created")
	}
}

func TestHostsPanelSearchKeepsTheMatchingPath(t *testing.T) {
	state := hostsPanelFixture(t)
	state.hostsPanel.search.SetText("beta")
	state.hostsPanel.stale = true
	state.Paint(&gfx.Canvas{}, panelWide)
	if _, ok := hostsPanelGroupRowOf(state, "prod"); !ok {
		t.Fatal("the search dropped the group the match sits in")
	}
	if _, ok := hostsPanelRowOf(state, "beta"); !ok {
		t.Fatal("the search dropped the host it matches")
	}
	if _, ok := hostsPanelRowOf(state, "alpha"); ok {
		t.Fatal("the search kept a host it does not match")
	}
}

func TestSecondaryPressOnAnOpenMenuBackdropClosesItAndOpensTheHostMenu(t *testing.T) {
	state := hostsPanelFixture(t)
	row, ok := hostsPanelRowOf(state, "alpha")
	if !ok {
		t.Fatal("alpha is not listed")
	}
	target := state.hostsPanel.tree.RowID(row)
	state.tabMenu.open = true
	// The first pass is the press the open menu's backdrop takes; it must close
	// that menu and leave the panel alone.
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitTabMenuDismiss,
		X: 100, Y: 148, Viewport: panelWide,
	})
	if state.tabMenu.open {
		t.Fatal("the open menu did not close")
	}
	if state.hostBlankMenu.open {
		t.Fatal("the backdrop press opened the blank menu")
	}
	// The runtime then delivers the same press to the row under it.
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: target,
		X: 100, Y: 148, Viewport: panelWide,
	})
	if !state.hostMenu.open || state.hostMenu.host != state.hosts[state.hostsPanel.rows[row].HostIndex].ID {
		t.Fatalf("host menu = %+v, want alpha's", state.hostMenu)
	}
	if state.hostBlankMenu.open {
		t.Fatal("the host press opened the blank menu")
	}
}

func TestHostsPanelAcceptsHostDropsOnGroupsAndHosts(t *testing.T) {
	state := hostsPanelFixture(t)
	group, ok := hostsPanelGroupRowOf(state, "prod")
	if !ok {
		t.Fatal("prod is not listed")
	}
	alpha, ok := hostsPanelRowOf(state, "alpha")
	if !ok {
		t.Fatal("alpha is not listed")
	}
	beta, ok := hostsPanelRowOf(state, "beta")
	if !ok {
		t.Fatal("beta is not listed")
	}
	if !state.hostsPanelCanDrop(alpha, group, ui.TreeDropInside) {
		t.Error("a host cannot be dropped into a group")
	}
	if state.hostsPanelCanDrop(alpha, group, ui.TreeDropBefore) {
		t.Error("a host can be dropped beside a group")
	}
	if !state.hostsPanelCanDrop(alpha, beta, ui.TreeDropAfter) {
		t.Error("a host cannot be dropped beside another host")
	}
	if state.hostsPanelCanDrop(alpha, beta, ui.TreeDropInside) {
		t.Error("a host can be dropped into another host")
	}
	if state.hostsPanelCanDrop(group, beta, ui.TreeDropInside) {
		t.Error("a group can be dropped into a host")
	}
	if state.hostsPanelCanDrop(alpha, alpha, ui.TreeDropAfter) {
		t.Error("a host can be dropped on itself")
	}
}

func TestHostDropMovesTheHostIntoTheTargetGroup(t *testing.T) {
	state := hostsPanelFixture(t)
	gamma, ok := hostsPanelRowOf(state, "gamma")
	if !ok {
		t.Fatal("gamma is not listed")
	}
	if !state.moveHostToGroup(state.hosts[state.hostsPanel.rows[gamma].HostIndex].ID, "prod") {
		t.Fatal("moving gamma into prod did nothing")
	}
	for _, host := range state.hosts {
		if host.Label == "gamma" && host.Group != "prod" {
			t.Fatalf("gamma is in %q, want prod", host.Group)
		}
	}
}

func TestHostReorderPlacesTheHostBesideItsTarget(t *testing.T) {
	state := hostsPanelFixture(t)
	gamma := state.hosts[2]
	alpha := state.hosts[0]
	if !state.reorderHostBeside(gamma.ID, alpha.ID, false) {
		t.Fatal("the reorder did nothing")
	}
	if state.hosts[0].Label != "gamma" || state.hosts[1].Label != "alpha" || state.hosts[2].Label != "beta" {
		t.Fatalf("order = %q, %q, %q", state.hosts[0].Label, state.hosts[1].Label, state.hosts[2].Label)
	}
	if state.hosts[0].Group != "prod" {
		t.Fatalf("gamma joined %q, want the target's group", state.hosts[0].Group)
	}
}
