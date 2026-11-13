package workbench

import (
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

var (
	hitDashboardSelectMatches = tree.ID(dashboardHits, "select-matches")
	hitDashboardOrderMode     = tree.ID(dashboardHits, "order-mode-trigger")
	hitDashboardOrderDismiss  = tree.ID(dashboardHits, "order-mode-dismiss")
	hitDashboardOrderModes    = tree.Sub(dashboardHits, tree.Name("order-mode"))
)

func (state *State) toggleDashboardMatches() {
	groups, _, _ := state.dashboardPickerGroups()
	matching := make(map[string]bool)
	var missing []string
	for _, group := range groups {
		for _, host := range group.hosts {
			if matching[host.CatalogID] {
				continue
			}
			matching[host.CatalogID] = true
			if !slices.Contains(state.dashboard.editor.selected, host.CatalogID) {
				missing = append(missing, host.CatalogID)
			}
		}
	}
	if len(missing) > 0 {
		state.dashboard.editor.selected = append(state.dashboard.editor.selected, missing...)
		return
	}
	state.dashboard.editor.selected = slices.DeleteFunc(state.dashboard.editor.selected, func(id string) bool { return matching[id] })
}

func (state *State) sortDashboardHosts(mode hostOrderMode) {
	editor := &state.dashboard.editor
	editor.orderMode, editor.orderOpen = mode, false
	editor.order.Cancel()
	editor.scroll = 0
	if mode == hostOrderManual {
		return
	}
	hosts := make(map[string]Host, len(state.hosts))
	for _, host := range state.hosts {
		if dashboardHostEligible(host) {
			hosts[host.CatalogID] = host
		}
	}
	collator := state.hostNameCollator()
	slices.SortStableFunc(editor.selected, func(leftID, rightID string) int {
		left, leftFound := hosts[leftID]
		right, rightFound := hosts[rightID]
		if !leftFound && !rightFound {
			return 0
		}
		if !leftFound {
			return 1
		}
		if !rightFound {
			return -1
		}
		if mode == hostOrderIP {
			return compareHostAddress(collator, left, right)
		}
		return compareHostName(collator, left, right)
	})
}

func (state *State) paintDashboardSelectMatches(canvas *gfx.Canvas, bounds gfx.Rect, groups []dashboardPickerGroup) {
	total, selected := 0, 0
	for _, group := range groups {
		for _, host := range group.hosts {
			total++
			if slices.Contains(state.dashboard.editor.selected, host.CatalogID) {
				selected++
			}
		}
	}
	theme := state.Theme()
	side := components.CheckboxExtent(theme)
	checkbox := components.BeginCheckbox(canvas, theme, hitDashboardSelectMatches, gfx.Rect{X: bounds.X, Y: bounds.Y + (bounds.Height-side)/2}, total > 0 && selected == total).
		Label(state.messages.Dashboard.SelectMatches).HitBounds(bounds)
	if selected > 0 && selected < total {
		checkbox = checkbox.Indeterminate()
	}
	if total == 0 {
		checkbox = checkbox.Disabled()
	}
	checkbox.Paint()
	canvas.Label(state.messages.Dashboard.SelectMatches, gfx.Rect{X: bounds.X + side + 8, Y: bounds.Y, Width: max(0, bounds.Width-side-8), Height: bounds.Height}, gfx.TextStyle{Size: 12, Color: theme.Foreground})
}

func (state *State) paintDashboardOrderMode(canvas *gfx.Canvas, bounds gfx.Rect) {
	editor := &state.dashboard.editor
	labels := []string{state.messages.HostCatalog.Manual, state.messages.HostCatalog.Name, state.messages.HostCatalog.IP}
	content := state.hostAuthListBounds(bounds, len(labels))
	control := components.BeginSelect(canvas, state.Theme(), bounds, components.SelectOptions{
		TriggerID: hitDashboardOrderMode, Label: state.messages.Dashboard.SortBy, Value: labels[editor.orderMode],
		Open: editor.orderOpen, Content: content, Layer: dialogLayer + 1, Scope: dialogScope,
		DismissID: hitDashboardOrderDismiss, Viewport: state.windowRect(), Scroll: state.selectScroll(editor.orderOpen, hitDashboardOrderDismiss),
	})
	for index, label := range labels {
		control.AddItem(components.SelectItem{ID: hitDashboardOrderModes.At(index), Label: label, Selected: int(editor.orderMode) == index})
	}
	if editor.orderOpen {
		state.menuPlaced(control.ContentBounds())
	}
	control.End()
}
