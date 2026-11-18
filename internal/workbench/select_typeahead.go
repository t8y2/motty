package workbench

import (
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

// selectTypeahead is the typed prefix of one closed select trigger. The owner
// is the trigger whose options the session belongs to; moving to another
// trigger or closing the dialog starts over.
type selectTypeahead struct {
	owner gfx.ID
	state components.MenuTypeahead
}

// selectTypeaheadKey commits the option a closed select trigger types toward,
// as Base UI does: the list does not open, the matching value is chosen, and
// repeating the first letter cycles through the options that share it. The
// caller builds the options from the same data the open popup paints.
func (state *State) selectTypeaheadKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if event.Release || event.Text == "" || event.Target.IsZero() ||
		event.Modifiers&(gfx.ModifierControl|gfx.ModifierAlt|gfx.ModifierMeta) != 0 {
		return gfx.EventResult{}, false
	}
	items, commit, ok := state.selectTypeaheadOptions(event.Target)
	if !ok {
		state.selectTypeahead = selectTypeahead{}
		return gfx.EventResult{}, false
	}
	if state.selectTypeahead.owner != event.Target {
		state.selectTypeahead = selectTypeahead{owner: event.Target}
	}
	id, matched := state.selectTypeahead.state.TypeItems(event.Text, time.Now(), items, gfx.ID{})
	if !matched || !commit(id) {
		if !matched {
			state.selectTypeahead.state.Reset()
		}
		return gfx.EventResult{}, false
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

// selectTypeaheadOptions returns the closed select's choices and the action
// that commits one. It reports false when the target is not a select trigger
// currently showing its value rather than its list.
func (state *State) selectTypeaheadOptions(target gfx.ID) ([]components.MenuTypeaheadItem, func(gfx.ID) bool, bool) {
	switch {
	case state.aiPanelVisible() && target == hitAITarget && !state.ai.panel.targetOpen && state.ai.run == nil:
		options := state.aiTargetOptions()
		items := make([]components.MenuTypeaheadItem, 0, len(options))
		for _, option := range options {
			items = append(items, components.MenuTypeaheadItem{ID: option.ID, Label: option.Label, Disabled: option.Disabled})
		}
		return items, state.aiChooseOnlyTarget, true
	case state.modal == modalNewHost && target == hitDialogPortsTrigger && !state.editor.portsOpen:
		items := make([]components.MenuTypeaheadItem, 0, len(state.editor.ports))
		for index, port := range state.editor.ports {
			items = append(items, components.MenuTypeaheadItem{
				ID: hitDialogPortsItemList.At(index), Label: port.Device,
			})
		}
		return items, func(id gfx.ID) bool {
			index, ok := id.In(hitDialogPortsItemList)
			if !ok || index < 0 || index >= len(state.editor.ports) {
				return false
			}
			state.editor.address = text.Editor{}
			state.editor.address.Insert(state.editor.ports[index].Device)
			return true
		}, true
	}
	return nil, nil, false
}

// proxyTargetSelectable is the rule the open target select paints: a host that
// is SSH and not already the target of another route.
func (state *State) proxyTargetSelectable(id uint32) bool {
	host, ok := state.hostByID(id)
	return ok && host.isSSH() && !state.proxyTargetConfiguredExcluding(id, state.proxyEditor.routeID)
}

// chooseProxyTarget and chooseProxyJump apply the same choices as the open
// popup, including the swap that keeps the two hosts distinct.
func (state *State) chooseProxyTarget(id uint32) bool {
	if !state.proxyTargetSelectable(id) {
		return false
	}
	if id == state.proxyEditor.jumpID {
		state.proxyEditor.jumpID = state.proxyEditor.targetID
	}
	state.proxyEditor.targetID = id
	return true
}

func (state *State) chooseProxyJump(id uint32) bool {
	if _, ok := state.hostByID(id); !ok {
		return false
	}
	if id == state.proxyEditor.targetID {
		state.proxyEditor.targetID = state.proxyEditor.jumpID
	}
	state.proxyEditor.jumpID = id
	return true
}
