package workbench

import (
	"errors"
	layoutcore "motty/internal/workspace"
	"slices"
	"testing"
)

func TestPlanWorkspaceLaunchUsesCatalogOrderAndReportsMissingHosts(t *testing.T) {
	catalog := []Host{
		{ID: 1, CatalogID: "host_c", Label: "C"},
		{ID: 2, CatalogID: "host_a", Label: "A"},
		{ID: 3, CatalogID: "host_d", Label: "D"},
		{ID: 4, CatalogID: "host_b", Label: "B"},
	}
	plan, err := PlanWorkspaceLaunch(catalog, []string{"host_d", "host_b", "host_missing", "host_a", "host_b"}, layoutcore.GridSpec(2))
	if err != nil {
		t.Fatal(err)
	}
	if got := hostLabels(plan.Hosts); !slices.Equal(got, []string{"host_a", "host_d", "host_b"}) {
		t.Fatalf("hosts = %v", got)
	}
	if !slices.Equal(plan.Missing, []string{"host_missing"}) {
		t.Fatalf("missing = %v", plan.Missing)
	}
	if !isGrid(plan.Layout, 2) {
		t.Fatalf("layout = %+v", plan.Layout)
	}
}

func TestPlanWorkspaceLaunchRejectsInvalidCounts(t *testing.T) {
	catalog := []Host{
		{ID: 1, CatalogID: "a"}, {ID: 2, CatalogID: "b"}, {ID: 3, CatalogID: "c"},
	}
	tests := []struct {
		name    string
		hostIDs []string
		columns uint8
		want    error
	}{
		{"empty", nil, 1, ErrWorkspaceLaunchEmpty},
		{"blank ids only", []string{" ", ""}, 1, ErrWorkspaceLaunchEmpty},
		{"all missing", []string{"x", "y"}, 1, ErrWorkspaceLaunchEmpty},
		{"too many unique", hostIDs(13), 2, ErrWorkspaceLaunchFull},
		{"zero columns", []string{"a"}, 0, ErrWorkspaceTemplateLayout},
		{"too many columns", []string{"a"}, 4, ErrWorkspaceTemplateLayout},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, err := PlanWorkspaceLaunch(catalog, test.hostIDs, columnsSpec(test.columns))
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if len(plan.Hosts) != 0 {
				t.Fatalf("invalid plan has hosts %v", hostLabels(plan.Hosts))
			}
		})
	}
}

func TestPlanWorkspaceLaunchKeepsOneAvailableHostAndReportsTheRest(t *testing.T) {
	catalog := []Host{{ID: 1, CatalogID: "host_a"}, {ID: 2, CatalogID: "host_b"}}
	plan, err := PlanWorkspaceLaunch(catalog, []string{"gone_1", "host_b", "gone_2"}, layoutcore.GridSpec(1))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(hostLabels(plan.Hosts), []string{"host_b"}) {
		t.Fatalf("hosts = %v", hostLabels(plan.Hosts))
	}
	if !slices.Equal(plan.Missing, []string{"gone_1", "gone_2"}) {
		t.Fatalf("missing = %v", plan.Missing)
	}
}

func TestDefaultWorkspaceLayout(t *testing.T) {
	tests := map[int]uint8{0: 1, 1: 1, 2: 2, 3: 3, 4: 2, 6: 2, 7: 2, 8: 2}
	for hostCount, want := range tests {
		if got := DefaultWorkspaceLayout(hostCount); !isGrid(got, want) {
			t.Fatalf("%d hosts = %+v, want %d columns", hostCount, got, want)
		}
	}
}

func hostLabels(hosts []Host) []string {
	labels := make([]string, len(hosts))
	for index := range hosts {
		labels[index] = hosts[index].CatalogID
	}
	return labels
}
