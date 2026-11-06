package workbench

import (
	"slices"
	"testing"

	"github.com/vivaui/viva/components"
)

func TestHostDisplayOrderWebDAVSharesPreferenceAndPreservesManualOrder(t *testing.T) {
	server, _ := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	second, _, _ := backupTestState(t)
	fixture := displayOrderState(t)
	for _, state := range []*State{first, second} {
		state.hosts = slices.Clone(fixture.hosts)
		state.nextHostID = 8
		for index := range state.hosts {
			switch {
			case index < 2:
				state.hosts[index].Group = "Team/Prod/EU"
			case index < 5:
				state.hosts[index].Group = "Team/Dev"
			default:
				state.hosts[index].Group = "Team2"
			}
		}
		state.hostGroups = nil
		state.hostDataChanged()
		state.save()
		configureSync(t, state, server)
	}
	done := first.messages.Tools.SyncDone
	syncAndWait(t, first, hitSyncNow, done)
	syncAndWait(t, second, hitSyncNow, done)
	first.activeSection = sectionHosts
	second.activeSection = sectionHosts
	second.toggleHostGroup(second.hostGroupIndex("Team/Prod"))
	first.reorderHostGroup("Team/Prod", -1)
	if !first.applyHostSortMove(components.ReorderGridMove{ID: hitHostSortHandles.At(1), Target: hitHostSortHandles.At(2), From: 0, To: 1}) {
		t.Fatal("manual host move was refused")
	}
	first.save()
	chooseDisplayOrder(first, "ip")
	syncAndWait(t, first, hitSyncNow, done)
	syncAndWait(t, second, hitSyncNow, done)
	if second.hostOrder != hostOrderIP {
		t.Fatal("display preference did not reach the second device")
	}
	if second.hostGroups[second.hostGroupIndex("Team/Prod")].Expanded {
		t.Fatal("sync overwrote device-local fold state")
	}
	if !first.hostGroups[first.hostGroupIndex("Team/Prod")].Expanded {
		t.Fatal("second device's folds reached the first device")
	}
	second.setHostGroupsExpanded(true)
	if got, want := displayHostIDs(second), []string{"host_c", "host_a", "host_d", "host_b", "host_z", "host_f", "host_e"}; !slices.Equal(got, want) {
		t.Fatalf("synced IP display = %v, want %v", got, want)
	}
	chooseDisplayOrder(first, "name")
	syncAndWait(t, first, hitSyncNow, done)
	syncAndWait(t, second, hitSyncNow, done)
	if got, want := displayHostIDs(second), []string{"host_a", "host_c", "host_d", "host_b", "host_z", "host_e", "host_f"}; !slices.Equal(got, want) {
		t.Fatalf("synced name display = %v, want %v", got, want)
	}
	chooseDisplayOrder(first, "manual")
	syncAndWait(t, first, hitSyncNow, done)
	syncAndWait(t, second, hitSyncNow, done)
	if second.hostOrder != hostOrderManual || second.hostGroupIndex("Team/Prod/EU") > second.hostGroupIndex("Team/Dev") {
		t.Fatal("return to Manual lost synced group ordering")
	}
	if got, want := displayHostIDs(second), []string{"host_b", "host_z", "host_a", "host_c", "host_d", "host_e", "host_f"}; !slices.Equal(got, want) {
		t.Fatalf("synced manual display = %v, want %v", got, want)
	}
	restarted := newTestState(t, second.options)
	if got, want := displayHostIDs(restarted), displayHostIDs(second); !slices.Equal(got, want) {
		t.Fatalf("synced manual order changed after restart: %v, want %v", got, want)
	}
}
