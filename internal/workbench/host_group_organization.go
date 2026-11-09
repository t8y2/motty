package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
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
func orderedHostGroupLess(left, right string, groups map[string]hostGroup) bool {
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
			return hostGroupLess(left, right)
		}
		if !lmore || !rmore {
			return !lmore && rmore
		}
		left, right = lrest, rrest
	}
}

func (state *State) groupDeleteChoices() [2]components.RadioOption {
	m := state.messages.HostGroupActions
	parent := hostGroupParent(state.groupActions.path)
	if parent == "" {
		parent = state.messages.HostGroupDefault
	}
	return [2]components.RadioOption{
		{ID: hitGroupDeleteParent, Label: fmt.Sprintf(m.KeepParentFmt, parent)},
		{ID: hitGroupDeleteUngroup, Label: m.KeepUngrouped},
	}
}

func (state *State) paintHostGroupDeleteDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	m := state.messages.HostGroupActions
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitMoveGroupDismiss, CloseID: hitMoveGroupClose,
		InitialFocusID: hitGroupDeleteParent, ContentSize: gfx.Size{Width: 480}, FitBody: true, BodyHeight: 130,
	})
	description := elide(canvas, state.groupActions.path, state.Theme().Metrics.FontSizeM, gfx.WeightRegular, dialog.Layout("").Header.Width)
	layout := dialog.Begin(m.Delete, description)
	choices := state.groupDeleteChoices()
	fullLabels := [2]string{choices[0].Label, choices[1].Label}
	for index := range choices {
		choices[index].Label = elide(canvas, choices[index].Label, state.Theme().Metrics.FontSizeM, gfx.WeightMedium, layout.Body.Width-32)
	}
	selected := hitGroupDeleteParent
	if state.groupActions.ungroup {
		selected = hitGroupDeleteUngroup
	}
	components.BeginRadioGroup(canvas, state.Theme(), layout.Body, choices[:], selected).Paint()
	for index := range canvas.Hits() {
		hit := &canvas.Hits()[index]
		for option := range choices {
			if hit.ID == choices[option].ID {
				hit.Label, hit.Value, hit.Hint = fullLabels[option], fullLabels[option], fullLabels[option]
			}
		}
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitMoveGroupCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitMoveGroupSubmit, Label: m.Delete, Variant: components.ButtonDestructive},
	)
	dialog.End()
}

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
		Content: gfx.Rect{X: bounds.X, Y: bounds.Y + bounds.Height + 4, Width: bounds.Width, Height: float32(8 + min(count, 8)*32)},
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
