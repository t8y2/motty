package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

func groupToolbarHit(name string) ui.ID {
	return tree.ID(tree.Feature("host-group-toolbar"), name)
}

func TestHostGroupToolbarExpandCollapseAllPersistsNestedGroups(t *testing.T) {
	for _, mode := range []hostViewMode{hostViewList, hostViewGrid} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			state := nestedHostGroupState(t)
			state.hostView = mode
			state.toggleHostGroup(state.hostGroupIndex("Team/Prod"))
			state.Click(groupToolbarHit("collapse-all"))
			for _, group := range state.hostGroups {
				if group.Expanded {
					t.Fatalf("collapse all left %q expanded", group.Name)
				}
			}
			if len(state.hostTreeRows) != 2 {
				t.Fatalf("collapsed tree has %d rows, want two roots", len(state.hostTreeRows))
			}
			restored := newTestState(t, state.options)
			for _, group := range restored.hostGroups {
				if group.Expanded {
					t.Fatal("collapse all was not persisted")
				}
			}
			state.Click(groupToolbarHit("expand-all"))
			for _, group := range state.hostGroups {
				if !group.Expanded {
					t.Fatalf("expand all left %q collapsed", group.Name)
				}
			}
			if len(state.hostTreeRows) != 8 {
				t.Fatalf("expanded tree has %d rows, want five groups and three hosts", len(state.hostTreeRows))
			}
			state.Click(groupToolbarHit("expand-all"))
			if len(state.hostTreeRows) != 8 {
				t.Fatal("repeated expand changed the tree")
			}
		})
	}
}

func TestHostGroupToolbarSearchAndAvailability(t *testing.T) {
	state := nestedHostGroupState(t)
	typeSearch(state, state.hosts[0].Label)
	state.Click(groupToolbarHit("collapse-all"))
	if len(state.hostTreeRows) != 4 {
		t.Fatal("bulk collapse hid the search match or its ancestors")
	}
	state.search = text.Editor{}
	state.syncVisible()
	if len(state.hostTreeRows) != 2 {
		t.Fatal("clearing search did not restore bulk-collapsed state")
	}
	state.setHostFilter(true, "Team")
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, groupToolbarHit("expand-all")); ok {
		t.Fatal("flat group filter offers group-tree controls")
	}
	state.Click(groupToolbarHit("expand-all"))
	for _, group := range state.hostGroups {
		if group.Expanded {
			t.Fatal("hidden expand action changed filtered group state")
		}
	}
	state.setHostFilter(false, "")
	for index := range state.hosts {
		state.hosts[index].Group = ""
	}
	state.hostDataChanged()
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, groupToolbarHit("collapse-all")); ok {
		t.Fatal("unnamed-only hosts offer group controls")
	}
}

func TestHostGroupToolbarSelectionKeepsFiltersAndAddsOnlyOneRow(t *testing.T) {
	for _, mode := range []hostViewMode{hostViewList, hostViewGrid} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.hostView = mode
			size := ui.Size{Width: 960, Height: 600}
			paintAt(state, size)
			listTop := state.chrome.hostList.Y
			first, second := state.hosts[0].ID, state.hosts[1].ID
			state.Click(hitHostCheckList.At(int(first)))
			for _, count := range []int{1, 2} {
				if count == 2 {
					state.Click(hitHostCheckList.At(int(second)))
				}
				canvas, texts := paintAt(state, size)
				filter, ok := hitByID(canvas, hitHostFilterAll)
				if !ok {
					t.Fatal("checking hosts removed the group filters")
				}
				if !contains(texts, state.hostSelectionText) {
					t.Fatal("selection count is absent")
				}
				if state.chrome.hostList.Y <= listTop {
					t.Fatal("selection toolbar did not reserve its own row")
				}
				for _, id := range []ui.ID{hitExportHosts, hitEditHost, hitCopyHost, hitMoveHostGroup, hitDashboardAddHosts, hitDeleteHost} {
					hit, ok := hitByID(canvas, id)
					if !ok || hit.Bounds.Y < filter.Bounds.Y+filter.Bounds.Height || hit.Bounds.Y+hit.Bounds.Height > state.chrome.hostList.Y {
						t.Fatalf("batch action %s overlaps filters or host list: %+v", tree.Text(id), hit)
					}
					if (id == hitEditHost || id == hitCopyHost) && hit.Disabled != (count > 1) {
						t.Fatal("single-host action enablement regressed")
					}
				}
				if count == 1 {
					oneRowTop := state.chrome.hostList.Y
					state.Click(hitHostCheckList.At(int(second)))
					paintAt(state, size)
					if state.chrome.hostList.Y != oneRowTop {
						t.Fatal("another check added another toolbar row")
					}
					state.Click(hitHostCheckList.At(int(second)))
				}
			}
			state.Click(hitHostCheckList.At(int(first)))
			state.Click(hitHostCheckList.At(int(second)))
			canvas, _ := paintAt(state, size)
			if state.chrome.hostList.Y != listTop {
				t.Fatal("clearing checks did not restore the list viewport")
			}
			if _, ok := hitByID(canvas, hitDeleteHost); ok {
				t.Fatal("empty selection still shows batch actions")
			}
		})
	}
}
