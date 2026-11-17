package workbench

import (
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/workbench/icons"
)

var (
	hitDashboardGroupFilter  = tree.ID(dashboardHits, "group-filter")
	hitDashboardGroupDismiss = tree.ID(dashboardHits, "group-dismiss")
	hitDashboardSelectedOnly = tree.ID(dashboardHits, "selected-only")
	hitDashboardGroupList    = tree.Sub(dashboardHits, tree.Name("group-option"))
)

type dashboardPickerGroup struct {
	name    string
	total   int
	hosts   []Host
	missing []int
}

// Filtering changes only the displayed catalog; the selection and its order
// remain shared with the order tab, including hosts outside the active filter.
func (s *State) dashboardPickerGroups() ([]dashboardPickerGroup, int, int) {
	e := &s.dashboard.editor
	query := strings.ToLower(strings.TrimSpace(e.query.String()))
	byName := make(map[string]*dashboardPickerGroup)
	total, shown := 0, 0
	selected := make(map[string]bool, len(e.selected))
	known := make(map[string]bool, len(s.hosts))
	for _, id := range e.selected {
		selected[id] = true
	}
	for _, h := range s.hosts {
		known[h.CatalogID] = dashboardHostEligible(h)
		if !dashboardHostEligible(h) {
			continue
		}
		name := strings.TrimSpace(h.Group)
		group := byName[name]
		if group == nil {
			group = &dashboardPickerGroup{name: name}
			byName[name] = group
		}
		group.total++
		total++
		if e.groupFilter && name != e.group {
			continue
		}
		if e.selectedOnly && !selected[h.CatalogID] {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(h.Label+" "+h.Address+" "+h.User+" "+h.Group+" "+h.Tags), query) {
			continue
		}
		group.hosts = append(group.hosts, h)
		shown++
	}
	e.groups = e.groups[:0]
	for name := range byName {
		e.groups = append(e.groups, name)
	}
	slices.Sort(e.groups)
	groups := make([]dashboardPickerGroup, 0, len(e.groups)+1)
	for _, name := range e.groups {
		if g := byName[name]; len(g.hosts) > 0 {
			groups = append(groups, *g)
		}
	}
	missing := dashboardPickerGroup{name: s.messages.Dashboard.UnavailableHosts}
	for i, id := range e.selected {
		if known[id] {
			continue
		}
		total++
		missing.total++
		searchText := s.messages.Dashboard.Missing + " " + s.messages.Dashboard.UnavailableHosts + " " + id
		if host, exists := s.dashboardHost(id); exists {
			searchText += " " + host.Label + " " + host.Address + " " + host.User + " " + host.Group + " " + host.Tags
		}
		if !e.groupFilter && (query == "" || strings.Contains(strings.ToLower(searchText), query)) {
			missing.missing = append(missing.missing, i)
			shown++
		}
	}
	if len(missing.missing) > 0 {
		groups = append(groups, missing)
	}
	return groups, total, shown
}

func dashboardPickerColumns(width float32) int {
	return max(1, min(3, int((width+8)/218)))
}

func dashboardPickerHeight(groups []dashboardPickerGroup, columns int) float32 {
	var height float32
	for _, g := range groups {
		rows := (len(g.hosts) + len(g.missing) + columns - 1) / columns
		height += 28 + float32(rows)*44
	}
	return max(0, height-8)
}

func (s *State) dashboardGroupLabel(name string) string {
	if name == "" {
		return s.messages.Dashboard.Ungrouped
	}
	return name
}

func (s *State) paintDashboardPickerFilters(c *gfx.Canvas, b gfx.Rect) {
	e, m, theme := &s.dashboard.editor, s.messages.Dashboard, s.Theme()
	groupW, toggleW := min(float32(170), b.Width*.29), float32(82)
	searchW := max(80, b.Width-groupW-toggleW-16)
	search := gfx.Rect{X: b.X, Y: b.Y, Width: searchW, Height: 34}
	components.PaintInputGroup(c, theme, search, components.InputGroupSnapshot{ID: hitDashboardHostQuery, Label: m.HostSearch, Placeholder: m.HostSearch, Value: e.query.String(), NativeEditing: c.Focused() == hitDashboardHostQuery && e.query.String() != "", InlineStart: components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: icons.Search}})
	trigger := gfx.Rect{X: b.X + searchW + 8, Y: b.Y, Width: groupW, Height: 34}
	value := m.AllGroups
	if e.groupFilter {
		value = s.dashboardGroupLabel(e.group)
	}
	list := s.hostAuthListBounds(trigger, min(8, len(e.groups)+1))
	list.Width = max(220, list.Width)
	selectControl := components.BeginSelect(c, theme, trigger, components.SelectOptions{TriggerID: hitDashboardGroupFilter, Label: m.GroupFilter, Value: value, Open: e.groupOpen, Content: list, Layer: dialogLayer + 1, Scope: dialogScope, DismissID: hitDashboardGroupDismiss, Viewport: s.windowRect(), Scroll: s.selectScroll(e.groupOpen, hitDashboardGroupDismiss)})
	selectControl.AddItem(components.SelectItem{ID: hitDashboardGroupList.At(0), Label: m.AllGroups, Selected: !e.groupFilter})
	for i, group := range e.groups {
		selectControl.AddItem(components.SelectItem{ID: hitDashboardGroupList.At(i + 1), Label: s.dashboardGroupLabel(group), Selected: e.groupFilter && group == e.group})
	}
	if e.groupOpen {
		s.menuPlaced(selectControl.ContentBounds())
	}
	selectControl.End()
	components.BeginToggle(c, theme, hitDashboardSelectedOnly, gfx.Rect{X: trigger.X + trigger.Width + 8, Y: b.Y, Width: toggleW, Height: 34}, e.selectedOnly).Outline(m.SelectedOnly)
}

func (s *State) paintDashboardPickerGroups(c *gfx.Canvas, geo components.ScrollAreaGeometry, groups []dashboardPickerGroup, columns int) {
	e, theme := &s.dashboard.editor, s.Theme()
	width := geo.Viewport.Width - 12
	cardW := (width - float32(columns-1)*8) / float32(columns)
	y := geo.Content.Y
	for _, group := range groups {
		name := s.dashboardGroupLabel(group.name)
		n := len(group.hosts) + len(group.missing)
		if y+28 > geo.Viewport.Y && y < geo.Viewport.Y+geo.Viewport.Height {
			c.DrawIcon(lucideIcons[iconFolderOpen], gfx.Rect{X: geo.Content.X + 2, Y: y + 6, Width: 14, Height: 14}, theme.MutedForeground)
			monitorLabel(c, fmt.Sprintf("%s  %d / %d", name, n, group.total), gfx.Rect{X: geo.Content.X + 24, Y: y, Width: width - 24, Height: 26}, 12, theme.MutedForeground, gfx.WeightMedium)
		}
		y += 28
		for i := 0; i < n; i++ {
			box := gfx.Rect{X: geo.Content.X + float32(i%columns)*(cardW+8), Y: y + float32(i/columns)*44, Width: cardW, Height: 36}
			if box.Y+box.Height < geo.Viewport.Y || box.Y > geo.Viewport.Y+geo.Viewport.Height {
				continue
			}
			if i < len(group.hosts) {
				h := group.hosts[i]
				s.paintDashboardHostChoice(c, h, box, hitDashboardHostList.At(int(h.ID)), slices.Contains(e.selected, h.CatalogID), false)
			} else {
				index := group.missing[i-len(group.hosts)]
				host, exists := s.dashboardHost(e.selected[index])
				if !exists {
					host = Host{Label: s.messages.Dashboard.Missing, Address: e.selected[index]}
				}
				s.paintDashboardHostChoice(c, host, box, hitDashboardMissingList.At(index), true, true)
			}
		}
		y += float32((n+columns-1)/columns) * 44
	}
	if len(groups) == 0 {
		components.Empty(c, theme, geo.Viewport, components.EmptyData{Title: s.messages.KnownHostsNoMatches})
	}
}
