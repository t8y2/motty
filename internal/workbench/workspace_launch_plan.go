package workbench

import (
	"errors"
	"slices"
	"strings"

	layoutcore "motty/internal/workspace"
)

var (
	ErrWorkspaceLaunchEmpty = errors.New("workbench: workspace launch needs at least one host")
	ErrWorkspaceLaunchFull  = errors.New("workbench: workspace launch supports at most eight hosts")
)

// WorkspaceLaunchPlan is the resolved, ordered input to one workspace launch.
// Missing keeps the template references this catalog no longer has; a launch
// may proceed with Hosts when at least one remains.
type WorkspaceLaunchPlan struct {
	Hosts   []Host
	Missing []string
	Columns uint8
}

// PlanWorkspaceLaunch resolves host catalog ids in the catalog's display order.
// Both temporary selections and saved templates go through this function.
func PlanWorkspaceLaunch(catalog []Host, hostIDs []string, columns uint8) (WorkspaceLaunchPlan, error) {
	if columns < layoutcore.MinGridColumns || int(columns) > layoutcore.MaxGridColumns {
		return WorkspaceLaunchPlan{}, ErrWorkspaceTemplateCols
	}
	selected := make(map[string]struct{}, len(hostIDs))
	for _, id := range hostIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			selected[id] = struct{}{}
		}
	}
	if len(selected) == 0 {
		return WorkspaceLaunchPlan{}, ErrWorkspaceLaunchEmpty
	}
	if len(selected) > layoutcore.MaxPanes {
		return WorkspaceLaunchPlan{}, ErrWorkspaceLaunchFull
	}
	found := make(map[string]struct{}, len(selected))
	plan := WorkspaceLaunchPlan{Columns: columns}
	for _, host := range catalog {
		if _, wanted := selected[host.CatalogID]; !wanted {
			continue
		}
		if _, duplicate := found[host.CatalogID]; duplicate {
			continue
		}
		found[host.CatalogID] = struct{}{}
		plan.Hosts = append(plan.Hosts, host)
	}
	for _, id := range hostIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := found[id]; !exists && !slices.Contains(plan.Missing, id) {
			plan.Missing = append(plan.Missing, id)
		}
	}
	if len(plan.Hosts) == 0 {
		return WorkspaceLaunchPlan{Missing: plan.Missing, Columns: columns}, ErrWorkspaceLaunchEmpty
	}
	return plan, nil
}

// DefaultWorkspaceColumns is the layout a launch uses when none was chosen.
func DefaultWorkspaceColumns(hostCount int) uint8 {
	switch {
	case hostCount <= 1:
		return 1
	case hostCount <= 3:
		return uint8(hostCount)
	default:
		return 2
	}
}
