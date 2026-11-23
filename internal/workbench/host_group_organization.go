package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text/collation"
)

var (
	hitGroupMove          = tree.ID(hostGroupActionHits, "move")
	hitGroupDelete        = tree.ID(hostGroupActionHits, "delete")
	hitGroupPin           = tree.ID(hostGroupActionHits, "pin")
	hitGroupUp            = tree.ID(hostGroupActionHits, "move-up")
	hitGroupDown          = tree.ID(hostGroupActionHits, "move-down")
	hitGroupDeleteParent  = tree.ID(hostGroupActionHits, "delete-parent")
	hitGroupDeleteUngroup = tree.ID(hostGroupActionHits, "delete-ungroup")
	hitGroupParentDismiss = tree.ID(hostGroupActionHits, "parent-dismiss")
	hitGroupParentRoot    = tree.ID(hostGroupActionHits, "parent-root")
	hitGroupParentItems   = tree.Sub(hostGroupActionHits, tree.Name("parent-item"))
)

func (state *State) moveHostGroup(path, parent string) bool {
	parent = strings.TrimSpace(parent)
	if hostGroupWithin(parent, path) {
		state.dialogError = state.messages.HostGroupActions.Cycle
		return false
	}
	if parent != "" && state.hostGroupIndex(parent) < 0 {
		state.dialogError = state.messages.HostGroupActions.Missing
		return false
	}
	target := joinHostGroup(parent, hostGroupLeaf(path))
	if !state.remapHostGroup(path, target) {
		return false
	}
	// Reveal the destination and the moved subtree without changing child folds.
	for index := range state.hostGroups {
		if hostGroupWithin(target, state.hostGroups[index].Name) && state.hostGroups[index].Name != target {
			state.hostGroups[index].Expanded = true
		}
	}
	state.rebuildHostTreeRows()
	state.save()
	return true
}

// hostGroupMoveBesideAllowed says whether group source may be placed as the
// sibling before or after group target: the target's parent must not be the
// source or inside it, the two must share the pin state the order groups by,
// and the new name must not meet another group. Reordering is only defined for
// the manual order.
func (state *State) hostGroupMoveBesideAllowed(source, target int) bool {
	if state.hostOrder != hostOrderManual {
		return false
	}
	if source < 0 || target < 0 || source >= len(state.hostGroups) || target >= len(state.hostGroups) || source == target {
		return false
	}
	src, tgt := state.hostGroups[source], state.hostGroups[target]
	if src.Name == "" || tgt.Name == "" || src.Pinned != tgt.Pinned {
		return false
	}
	parent := hostGroupParent(tgt.Name)
	if hostGroupWithin(parent, src.Name) {
		return false
	}
	moved := joinHostGroup(parent, hostGroupLeaf(src.Name))
	for index, group := range state.hostGroups {
		if index != source && strings.EqualFold(group.Name, moved) {
			return false
		}
	}
	return true
}

// moveHostGroupBeside puts group source next to group target: under the target's
// parent when the two are in different folders, then before or after the target
// in the manual order.
func (state *State) moveHostGroupBeside(source, target int, after bool) bool {
	if !state.hostGroupMoveBesideAllowed(source, target) {
		return false
	}
	sourceName, targetName := state.hostGroups[source].Name, state.hostGroups[target].Name
	parent := hostGroupParent(targetName)
	moved := joinHostGroup(parent, hostGroupLeaf(sourceName))
	if moved != sourceName && !state.moveHostGroup(sourceName, parent) {
		return false
	}
	state.orderHostGroupBeside(moved, targetName, after)
	return true
}

// orderHostGroupBeside assigns the sibling order with path immediately before or
// after target. The current visible order is the base, so a group dragged from
// elsewhere lands where the line showed it.
func (state *State) orderHostGroupBeside(path, target string, after bool) {
	if state.hostOrder != hostOrderManual {
		return
	}
	targetIndex := state.hostGroupIndex(target)
	if targetIndex < 0 || state.hostGroupIndex(path) < 0 {
		return
	}
	parent, pinned := hostGroupParent(target), state.hostGroups[targetIndex].Pinned
	siblings := make([]int, 0, len(state.hostGroups))
	for index, group := range state.hostGroups {
		if group.Name != "" && hostGroupParent(group.Name) == parent && group.Pinned == pinned {
			siblings = append(siblings, index)
		}
	}
	sourceAt, targetAt := -1, -1
	for position, index := range siblings {
		switch state.hostGroups[index].Name {
		case path:
			sourceAt = position
		case target:
			targetAt = position
		}
	}
	if sourceAt < 0 || targetAt < 0 || sourceAt == targetAt {
		return
	}
	moved := siblings[sourceAt]
	siblings = append(siblings[:sourceAt], siblings[sourceAt+1:]...)
	targetAt = -1
	for position, index := range siblings {
		if state.hostGroups[index].Name == target {
			targetAt = position
			break
		}
	}
	if targetAt < 0 {
		return
	}
	insert := targetAt
	if after {
		insert++
	}
	siblings = append(siblings, 0)
	copy(siblings[insert+1:], siblings[insert:])
	siblings[insert] = moved
	for position, index := range siblings {
		state.hostGroups[index].Order = position + 1
	}
	state.refreshHostGroups()
	state.save()
}

func (state *State) deleteHostGroup(path string, ungroup bool) bool {
	if path == "" || state.hostGroupIndex(path) < 0 {
		state.dialogError = state.messages.HostGroupActions.Missing
		return false
	}
	target := hostGroupParent(path)
	if ungroup {
		target = ""
	}
	for index := range state.hosts {
		if hostGroupWithin(strings.TrimSpace(state.hosts[index].Group), path) {
			state.hosts[index].Group = target
		}
	}
	kept := state.hostGroups[:0]
	for _, group := range state.hostGroups {
		if !hostGroupWithin(group.Name, path) {
			kept = append(kept, group)
		}
	}
	clear(state.hostGroups[len(kept):])
	state.hostGroups = kept
	if state.hostFilterOn && hostGroupWithin(state.hostFilter, path) {
		state.hostFilterOn = false
	}
	state.queueNoteGroupChange(path, "", true)
	state.hostDataChanged()
	state.save()
	return true
}

func (state *State) pinHostGroup(path string) {
	if index := state.hostGroupIndex(path); index >= 0 && path != "" {
		state.hostGroups[index].Pinned = !state.hostGroups[index].Pinned
		state.refreshHostGroups()
		state.save()
	}
}

func (state *State) hostGroupOrderNeighbor(path string, step int) int {
	if state.hostOrder != hostOrderManual {
		return -1
	}
	index := state.hostGroupIndex(path)
	if index < 0 || path == "" {
		return -1
	}
	group := state.hostGroups[index]
	for candidate := index + step; candidate >= 0 && candidate < len(state.hostGroups); candidate += step {
		sibling := state.hostGroups[candidate]
		if sibling.Name != "" && hostGroupParent(sibling.Name) == hostGroupParent(path) && sibling.Pinned == group.Pinned {
			return candidate
		}
	}
	return -1
}

func (state *State) reorderHostGroup(path string, step int) {
	index := state.hostGroupIndex(path)
	neighbor := state.hostGroupOrderNeighbor(path, step)
	if neighbor < 0 {
		return
	}
	group := state.hostGroups[index]
	order := 1
	for at := range state.hostGroups {
		sibling := &state.hostGroups[at]
		if sibling.Name != "" && hostGroupParent(sibling.Name) == hostGroupParent(path) && sibling.Pinned == group.Pinned {
			sibling.Order = order
			order++
		}
	}
	state.hostGroups[index].Order, state.hostGroups[neighbor].Order = state.hostGroups[neighbor].Order, state.hostGroups[index].Order
	state.refreshHostGroups()
	state.save()
}

// The first differing sibling decides the order of two entire subtrees.
func orderedHostGroupLess(collator *collation.Collator, left, right string, groups map[string]hostGroup) bool {
	lpath, rpath := left, right
	for {
		l, lrest, lmore := strings.Cut(left, "/")
		r, rrest, rmore := strings.Cut(right, "/")
		if l != r {
			lp, rp := lpath, rpath
			if lmore {
				lp = lpath[:len(lpath)-len(lrest)-1]
			}
			if rmore {
				rp = rpath[:len(rpath)-len(rrest)-1]
			}
			lg, rg := groups[lp], groups[rp]
			if lp == "" || rp == "" {
				return lp == ""
			}
			if lg.Pinned != rg.Pinned {
				return lg.Pinned
			}
			if lg.Order != rg.Order {
				if lg.Order == 0 {
					return false
				}
				if rg.Order == 0 {
					return true
				}
				return lg.Order < rg.Order
			}
			return hostGroupLess(collator, left, right)
		}
		if !lmore || !rmore {
			return !lmore && rmore
		}
		left, right = lrest, rrest
	}
}

func (state *State) groupDeleteChoices() [2]string {
	m := state.messages.HostGroupActions
	parent := hostGroupParent(state.groupActions.path)
	if parent == "" {
		parent = state.messages.HostGroupDefault
	}
	return [2]string{fmt.Sprintf(m.KeepParentFmt, parent), m.KeepUngrouped}
}

// paintHostGroupParentSelect paints the move dialog's destination as the
// hierarchy select it has always been. A page Select cannot keep the dialog's
// initial focus on the field's own identity, so this stays with components.
func (state *State) paintHostGroupParentSelect(canvas *gfx.Canvas, bounds, viewport gfx.Rect) {
	a, m := &state.groupActions, state.messages.HostGroupActions
	value := state.hostGroupEditor.String()
	if value == "" {
		value = m.TopLevel
	}
	count := 1
	for _, group := range state.hostGroups {
		if group.Name != "" && !hostGroupWithin(group.Name, a.path) {
			count++
		}
	}
	control := components.BeginSelect(canvas, state.Theme(), bounds, components.SelectOptions{
		TriggerID: hitMoveGroupField, Label: m.Parent, Value: value, Open: a.parentOpen,
		Content: hostGroupParentList(bounds, count, viewport),
		Layer:   dialogLayer + 1, Scope: dialogScope, DismissID: hitGroupParentDismiss, Viewport: viewport,
		Scroll: state.selectScroll(a.parentOpen, hitGroupParentDismiss),
	})
	defer control.End()
	if !a.parentOpen {
		return
	}
	state.menuPlaced(control.ContentBounds())
	control.AddItem(components.SelectItem{ID: hitGroupParentRoot, Label: m.TopLevel, Selected: state.hostGroupEditor.String() == ""})
	for index, group := range state.hostGroups {
		if group.Name == "" || hostGroupWithin(group.Name, a.path) {
			continue
		}
		control.AddItem(components.SelectItem{ID: hitGroupParentItems.At(index), Label: group.Name, Selected: state.hostGroupEditor.String() == group.Name})
	}
}

// hostGroupParentList is where the parent select's list goes: under the field, or
// above it with more room there, shortened to the room it has.
func hostGroupParentList(field gfx.Rect, count int, viewport gfx.Rect) gfx.Rect {
	list, _ := components.PlaceOverlay(field, gfx.Size{Width: field.Width, Height: float32(8 + min(count, 8)*32)}, viewport,
		components.OverlayPlacement{Side: components.OverlayBottom, Align: components.OverlayAlignStart, SideOffset: 4, Margin: 8, Fit: true})
	return list
}
