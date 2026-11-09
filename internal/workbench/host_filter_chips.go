package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// hostChipMenu is the menu of nested groups a top-level group's chip opens.
// Its rows are the groups below root, named relative to it.
type hostChipMenu struct {
	open bool
	// root is the top-level group the menu lists the groups of.
	root string
	tree components.HierarchyMenuState
	// anchor is the chip the menu hangs under, popup the box around its panels.
	anchor, popup gfx.Rect
}

// fitChipGroups fills state.hostChips with the groups that get a chip, as
// indexes into hostGroups: the top level, so the row keeps its shape whichever
// group is chosen. Nested groups are reached from the chevron of their top-level
// group's chip.
func (state *State) fitChipGroups() {
	chips := state.hostChips[:0]
	for index, group := range state.hostGroups {
		if state.hostGroupChipParent(group.Name) == "" {
			chips = append(chips, index)
		}
	}
	state.hostChips = chips
}

// hostGroupChipParent is the group a chip's group sits under. A group whose
// parent does not exist as a group is shown with the top level.
func (state *State) hostGroupChipParent(name string) string {
	parent := hostGroupParent(name)
	if parent != "" && state.hostGroupIndex(parent) < 0 {
		return ""
	}
	return parent
}

func (state *State) hostGroupHasChildren(name string) bool {
	if name == "" {
		return false
	}
	for _, group := range state.hostGroups {
		if group.Name != name && state.hostGroupChipParent(group.Name) == name {
			return true
		}
	}
	return false
}

// hostGroupChipLabel is the text of a group's own chip, or of a row that names
// it: the last segment of its path.
func (state *State) hostGroupChipLabel(group hostGroup) string {
	if group.Name == "" {
		return state.messages.HostGroupDefault
	}
	if at := strings.LastIndexByte(group.Name, '/'); at >= 0 {
		if last := strings.TrimSpace(group.Name[at+1:]); last != "" {
			return last
		}
	}
	return group.Name
}

// hostChipView is what the chip of a top-level group shows. When the chosen
// group is nested under it the chip is chosen and names that group after its
// own: "School › Tianshui", with that group's count.
func (state *State) hostChipView(index int) (label, digits string, chosen bool) {
	group := state.hostGroups[index]
	label, digits = state.hostGroupChipLabel(group), group.Digits
	if !state.hostFilterOn {
		return label, digits, false
	}
	if state.hostFilter == group.Name {
		return label, digits, true
	}
	if group.Name == "" || !strings.HasPrefix(state.hostFilter, group.Name+"/") {
		return label, digits, false
	}
	if nested := state.hostGroupIndex(state.hostFilter); nested >= 0 {
		return label + " › " + state.hostGroupChipLabel(state.hostGroups[nested]), state.hostGroups[nested].Digits, true
	}
	return label, digits, true
}

// hostChipHasMenu is whether the chip of the group at index has nested groups to
// open.
func (state *State) hostChipHasMenu(index int) bool {
	return state.hostGroupHasChildren(state.hostGroups[index].Name)
}

// openHostChipMenu opens the menu of the groups nested under the group at index.
func (state *State) openHostChipMenu(index int) {
	if index < 0 || index >= len(state.hostGroups) {
		return
	}
	m, root := &state.chipMenu, state.hostGroups[index].Name
	if m.open && m.root == root {
		m.open = false
		return
	}
	prefix := root + "/"
	selected := ""
	if state.hostFilterOn && strings.HasPrefix(state.hostFilter, prefix) {
		selected = state.hostFilter[len(prefix):]
	}
	var nested []string
	for _, group := range state.hostGroups {
		if strings.HasPrefix(group.Name, prefix) {
			nested = append(nested, group.Name[len(prefix):])
		}
	}
	m.tree.Build("/", selected, nil, len(nested), func(i int) components.HierarchyMenuItem {
		return components.HierarchyMenuItem{Value: nested[i]}
	})
	m.open, m.root = m.tree.Usable(), root
	state.hostFilterMenu = false
}

// clickHostChipMenu handles a press while the chip menu is open, or on a chip's
// chevron. It reports whether the press was the menu's.
func (state *State) clickHostChipMenu(id gfx.ID) bool {
	m := &state.chipMenu
	if id.Scope() == hitHostFilterChevronList {
		state.openHostChipMenu(int(item(id)))
		return true
	}
	if !m.open {
		return false
	}
	switch {
	case id == hitHostChipMenuDismiss:
		m.open = false
	case id.Scope() == hitHostChipMenuList:
		if value, ok := m.tree.Choose(int(item(id))); ok {
			m.open = false
			state.setHostFilter(true, m.root+"/"+value)
		}
	default:
		m.open = false
		return false
	}
	return true
}

// hostChipMenuKey routes keys to the open chip menu.
func (state *State) hostChipMenuKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	m := &state.chipMenu
	if !m.open || event.Release {
		return gfx.EventResult{}, false
	}
	result := m.tree.Key(event, true)
	if result.Picked {
		m.open = false
		state.setHostFilter(true, m.root+"/"+result.Chosen)
	}
	if result.Handled {
		return result.EventResult, true
	}
	if event.Key == gfx.KeyEscape {
		m.open = false
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

// paintHostChipMenu paints the open chip menu under its chip.
func (state *State) paintHostChipMenu(canvas *gfx.Canvas, viewport gfx.Rect) {
	m := &state.chipMenu
	m.popup = components.PaintHierarchyMenu(canvas, state.Theme(), m.anchor, &m.tree, components.HierarchyMenuOptions{
		OptionIDs: hitHostChipMenuList, ScrollID: hitHostChipMenuScroll, Scope: dialogScope, Layer: dialogLayer,
		Label: m.root, EmptyLabel: state.messages.HostGroupsEmpty, Viewport: viewport, Overlay: true, DismissID: hitHostChipMenuDismiss,
	})
}
