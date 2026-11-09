package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

var (
	hostSelectionHits = tree.Feature("host-selection")
	hitHostSelectAll  = tree.ID(hostSelectionHits, "all")
	hitHostGroupCheck = tree.Sub(hostSelectionHits, tree.Name("group"))
)

type hostSelectionCount struct {
	total, selected int
	label           string
}

type hostScopeSelection struct {
	all    hostSelectionCount
	groups []hostSelectionCount
}

func (state *State) refreshHostScopeSelection() {
	selection := &state.hostScopeSelection
	selection.all = hostSelectionCount{label: state.messages.HostCatalog.SelectAll}
	if cap(selection.groups) < len(state.hostGroups) {
		selection.groups = make([]hostSelectionCount, len(state.hostGroups))
	} else {
		selection.groups = selection.groups[:len(state.hostGroups)]
		clear(selection.groups)
	}
	selected := make(map[uint32]bool, len(state.hostSelections))
	for _, id := range state.hostSelections {
		selected[id] = true
	}
	for index, group := range state.hostGroups {
		selection.groups[index].label = fmt.Sprintf(state.messages.HostCatalog.SelectGroupFmt, state.hostGroupLabel(group))
	}
	for _, index := range state.visible {
		if index < 0 || index >= len(state.hosts) || !state.inHostFilter(state.hosts[index]) {
			continue
		}
		host := state.hosts[index]
		selection.all.total++
		if selected[host.ID] {
			selection.all.selected++
		}
		for groupIndex, group := range state.hostGroups {
			if hostGroupWithin(strings.TrimSpace(host.Group), group.Name) {
				selection.groups[groupIndex].total++
				if selected[host.ID] {
					selection.groups[groupIndex].selected++
				}
			}
		}
	}
}

func (state *State) toggleHostScope(group string, grouped bool) {
	if state.hostSort.enabled {
		return
	}
	scope := make(map[uint32]bool)
	selected := make(map[uint32]bool, len(state.hostSelections))
	for _, id := range state.hostSelections {
		selected[id] = true
	}
	all := true
	for _, index := range state.visible {
		host := state.hosts[index]
		if !state.inHostFilter(host) || grouped && !hostGroupWithin(strings.TrimSpace(host.Group), group) {
			continue
		}
		scope[host.ID] = true
		all = all && selected[host.ID]
	}
	if len(scope) == 0 {
		return
	}
	if all {
		kept := state.hostSelections[:0]
		for _, id := range state.hostSelections {
			if !scope[id] {
				kept = append(kept, id)
			}
		}
		state.hostSelections = kept
	} else {
		for _, index := range state.visible {
			host := state.hosts[index]
			if scope[host.ID] && !selected[host.ID] {
				state.hostSelections = append(state.hostSelections, host.ID)
				state.selectedHostID = host.ID
			}
		}
	}
	state.refreshHostSelection()
}

func (state *State) clickHostScopeSelection(id gfx.ID) bool {
	switch {
	case id == hitHostSelectAll:
		state.toggleHostScope("", false)
	case id.Scope() == hitHostGroupCheck:
		index := item(id)
		if index >= 0 && index < len(state.hostGroups) {
			state.toggleHostScope(state.hostGroups[index].Name, true)
		}
	default:
		return false
	}
	return true
}

func (state *State) paintHostScopeCheckbox(canvas *gfx.Canvas, id gfx.ID, bounds gfx.Rect, count hostSelectionCount) {
	theme := state.Theme()
	side := components.CheckboxExtent(theme)
	checkbox := components.BeginCheckbox(canvas, theme, id, gfx.Rect{
		X: bounds.X + (bounds.Width-side)/2, Y: bounds.Y + (bounds.Height-side)/2,
	}, count.total > 0 && count.total == count.selected).Label(count.label).HitBounds(bounds)
	if count.selected > 0 && count.selected < count.total {
		checkbox = checkbox.Indeterminate()
	}
	if count.total == 0 || state.hostSort.enabled {
		checkbox = checkbox.Disabled()
	}
	checkbox.Paint()
	if id.Scope() == hitHostGroupCheck {
		event, ok := canvas.PointerOn(id)
		if ok && event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonSecondary {
			state.openHostGroupActions(state.hostGroups[item(id)].Name, gfx.Point{X: event.X, Y: event.Y})
			canvas.Consume()
		}
	}
}
