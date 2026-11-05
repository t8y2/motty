package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

var (
	hostGroupActionHits   = tree.Feature("host-group-actions")
	hitHostGroupMore      = tree.Sub(hostGroupActionHits, tree.Name("more"))
	hitGroupActionDismiss = tree.ID(hostGroupActionHits, "dismiss")
	hitGroupRename        = tree.ID(hostGroupActionHits, "rename")
	hitGroupNewChild      = tree.ID(hostGroupActionHits, "new-child")
	hitGroupNewHost       = tree.ID(hostGroupActionHits, "new-host")
)

type hostGroupOperation uint8

const (
	hostGroupMoveHosts hostGroupOperation = iota
	hostGroupRename
	hostGroupCreateChild
	hostGroupRelocate
	hostGroupDelete
)

type hostGroupActions struct {
	open       bool
	path       string
	at         ui.Point
	op         hostGroupOperation
	rows       []ui.Rect
	ungroup    bool
	parentOpen bool
	drag       hostGroupDrag
}

func hostGroupParent(path string) string {
	if at := strings.LastIndex(path, "/"); at >= 0 {
		return path[:at]
	}
	return ""
}

func joinHostGroup(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

func validHostGroupSegment(name string) bool {
	return validHostGroup(name) && !strings.Contains(name, "/") && name != "." && name != ".."
}

func (state *State) openHostGroupActions(path string, at ui.Point) {
	state.hostMenu.open = false
	state.hostGroupMenu, state.hostFilterMenu, state.importMenu = false, false, false
	state.groupActions.open, state.groupActions.path, state.groupActions.at = true, path, at
}

func (state *State) clickHostGroupActions(id ui.ID) bool {
	a := &state.groupActions
	if !a.open {
		if id.Scope() != hitHostGroupMore || state.activeSection != sectionHosts || state.modal != modalNone {
			return false
		}
		index := item(id)
		if index < 0 || index >= len(state.hostGroups) {
			return true
		}
		at := ui.Point{X: state.hostsBounds.X + 30, Y: state.hostsBounds.Y + hostListTop}
		if index < len(a.rows) {
			row := a.rows[index]
			at = ui.Point{X: row.X + row.Width - 32, Y: row.Y + row.Height}
		}
		state.openHostGroupActions(state.hostGroups[index].Name, at)
		return true
	}
	a.open = false
	if state.activeSection != sectionHosts || state.modal != modalNone || state.hostGroupIndex(a.path) < 0 {
		return true
	}
	switch id {
	case hitGroupNewHost:
		state.openHostDialog()
		state.editor.group.Insert(a.path)
	case hitGroupRename, hitGroupNewChild:
		if a.path == "" && id == hitGroupRename {
			return true
		}
		a.op = hostGroupCreateChild
		state.hostGroupEditor = text.Editor{}
		if id == hitGroupRename {
			a.op = hostGroupRename
			state.hostGroupEditor.Insert(hostGroupLeaf(a.path))
		}
		state.dialogError = ""
		state.modal = modalMoveHostGroup
	case hitGroupMove, hitGroupDelete:
		if a.path == "" {
			return true
		}
		a.op, a.ungroup, a.parentOpen = hostGroupRelocate, false, false
		state.hostGroupEditor = text.Editor{}
		state.hostGroupEditor.Insert(hostGroupParent(a.path))
		if id == hitGroupDelete {
			a.op = hostGroupDelete
		}
		state.dialogError = ""
		state.modal = modalMoveHostGroup
	case hitGroupPin:
		state.pinHostGroup(a.path)
	case hitGroupUp:
		state.reorderHostGroup(a.path, -1)
	case hitGroupDown:
		state.reorderHostGroup(a.path, 1)
	}
	return true
}

func (state *State) paintHostGroupActions(canvas *ui.Canvas, viewport ui.Rect) {
	a := &state.groupActions
	if !a.open {
		return
	}
	if state.activeSection != sectionHosts || state.modal != modalNone || state.hostGroupIndex(a.path) < 0 {
		a.open = false
		return
	}
	m := state.messages.HostGroupActions
	var rows menuRows
	rows.add(components.ContextMenuItem{ID: hitGroupNewHost, Label: state.messages.NewHost, Icon: lucideIcons[iconPlus]})
	rows.add(components.ContextMenuItem{ID: hitGroupNewChild, Label: m.NewChild, Icon: lucideIcons[iconFolderOpen]})
	rows.add(components.ContextMenuItem{ID: hitGroupRename, Label: m.Rename, Icon: lucideIcons[iconPencil], Disabled: a.path == ""})
	rows.add(components.ContextMenuItem{ID: hitGroupMove, Label: m.Move, Icon: lucideIcons[iconFolderOpen], Disabled: a.path == ""})
	rows.separate()
	pin := m.Pin
	if state.hostGroups[state.hostGroupIndex(a.path)].Pinned {
		pin = m.Unpin
	}
	rows.add(components.ContextMenuItem{ID: hitGroupPin, Label: pin, Icon: lucideIcons[iconStar], Disabled: a.path == ""})
	rows.add(components.ContextMenuItem{ID: hitGroupUp, Label: m.MoveUp, Icon: lucideIcons[iconArrowUp], Disabled: state.hostGroupOrderNeighbor(a.path, -1) < 0})
	rows.add(components.ContextMenuItem{ID: hitGroupDown, Label: m.MoveDown, Icon: lucideIcons[iconChevronDown], Disabled: state.hostGroupOrderNeighbor(a.path, 1) < 0})
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitGroupDelete, Label: m.Delete, Icon: lucideIcons[iconTrash], Disabled: a.path == "", Variant: components.ContextMenuItemDestructive})
	width, height := rows.size(canvas, state.contextMenuTheme())
	bounds := state.popupAt(canvas, viewport, a.at, width, height, 6)
	menu := components.BeginContextMenu(canvas, state.contextMenuTheme(), bounds, components.ContextMenuOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitGroupActionDismiss, Viewport: viewport,
		Scroll: state.menuScrollFor(hitGroupActionDismiss), RowHeight: contextMenuRow,
	})
	state.menuPlaced(menu.Bounds())
	rows.paint(&menu, nil)
	menu.End()
}

func (state *State) hostGroupRowEvents(canvas *ui.Canvas, id ui.ID, index int, bounds ui.Rect) {
	state.groupActions.rows[index] = bounds
	for at := len(canvas.Hits()) - 1; at >= 0; at-- {
		hit := &canvas.Hits()[at]
		if hit.ID == id {
			hit.Hint = state.hostGroups[index].Name
			break
		}
	}
	state.openHostGroupActionsOn(canvas, id, index)
	button := ui.Rect{X: bounds.X + bounds.Width - 34, Y: bounds.Y + (bounds.Height-28)/2, Width: 28, Height: 28}
	state.paintIconButton(canvas, hitHostGroupMore.At(index), iconEllipsis, button, state.messages.HostGroupActions.Actions, false, iconButtonQuiet, state.pal.workspace)
}

func (state *State) openHostGroupActionsOn(canvas *ui.Canvas, id ui.ID, index int) {
	event, ok := canvas.PointerOn(id)
	if ok && event.Kind == ui.PointerDown && event.Button == ui.PointerButtonSecondary && state.modal == modalNone && state.activeSection == sectionHosts {
		state.openHostGroupActions(state.hostGroups[index].Name, ui.Point{X: event.X, Y: event.Y})
		canvas.Consume()
	}
}

func (state *State) renameHostGroup(path, name string) bool {
	name = strings.TrimSpace(name)
	if !validHostGroupSegment(name) {
		state.dialogError = state.messages.HostGroupActions.InvalidName
		return false
	}
	return state.remapHostGroup(path, joinHostGroup(hostGroupParent(path), name))
}

func (state *State) remapHostGroup(path, target string) bool {
	if path == "" || state.hostGroupIndex(path) < 0 {
		state.dialogError = state.messages.HostGroupActions.Missing
		return false
	}
	for _, group := range state.hostGroups {
		if !hostGroupWithin(group.Name, path) && strings.EqualFold(group.Name, target) {
			state.dialogError = state.messages.HostGroupActions.Conflict
			return false
		}
	}
	for index := range state.hosts {
		group := strings.TrimSpace(state.hosts[index].Group)
		if hostGroupWithin(group, path) {
			state.hosts[index].Group = target + strings.TrimPrefix(group, path)
		}
	}
	for index := range state.hostGroups {
		group := &state.hostGroups[index]
		if hostGroupWithin(group.Name, path) {
			group.Name = target + strings.TrimPrefix(group.Name, path)
		}
	}
	if state.hostFilterOn && hostGroupWithin(state.hostFilter, path) {
		state.hostFilter = target + strings.TrimPrefix(state.hostFilter, path)
	}
	state.hostGroupDataChanged()
	state.save()
	return true
}

func (state *State) createHostChildGroup(parent, name string) bool {
	name = strings.TrimSpace(name)
	if !validHostGroupSegment(name) {
		state.dialogError = state.messages.HostGroupActions.InvalidName
		return false
	}
	if state.hostGroupIndex(parent) < 0 {
		state.dialogError = state.messages.HostGroupActions.Missing
		return false
	}
	path := joinHostGroup(parent, name)
	for _, group := range state.hostGroups {
		if strings.EqualFold(group.Name, path) {
			state.dialogError = state.messages.HostGroupActions.Conflict
			return false
		}
	}
	state.hostGroups = append(state.hostGroups, hostGroup{Name: path, Expanded: true, Explicit: true})
	for index := range state.hostGroups {
		if hostGroupWithin(path, state.hostGroups[index].Name) {
			state.hostGroups[index].Expanded = true
		}
	}
	state.hostGroupDataChanged()
	state.save()
	return true
}

func (state *State) hostGroupDataChanged() {
	filterOn, filter := state.hostFilterOn, state.hostFilter
	state.hostDataChanged()
	if filterOn && state.hostGroupIndex(filter) >= 0 {
		state.hostFilterOn, state.hostFilter = true, filter
		state.pruneHostSelections()
		state.rebuildHostTreeRows()
	}
}
