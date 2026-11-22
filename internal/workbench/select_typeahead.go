package workbench

import (
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
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
	}
	return nil, nil, false
}

// proxyTargetSelectable is the rule the target select paints and commits: a
// route target that is not already the target of another route. A remote
// desktop is a route target, so the choice the list offers has to land.
func (state *State) proxyTargetSelectable(id uint32) bool {
	host, ok := state.hostByID(id)
	return ok && host.isRouteTarget() && !state.proxyTargetConfiguredExcluding(id, state.proxyEditor.routeID)
}

// chooseProxyTarget and chooseProxyJump apply the same choices as the open
// popup, including the swap that keeps the two hosts distinct and each end in
// a role it can take: only an SSH host can be the jump a route signs in to.
func (state *State) chooseProxyTarget(id uint32) bool {
	if !state.proxyTargetSelectable(id) {
		return false
	}
	if id == state.proxyEditor.jumpID {
		// The old target takes the jump's place when it can be a jump; a
		// remote desktop cannot, so the dialog asks for one instead.
		previous, ok := state.hostByID(state.proxyEditor.targetID)
		if ok && previous.isSSH() {
			state.proxyEditor.jumpID = previous.ID
		} else {
			state.proxyEditor.jumpID = 0
		}
	}
	state.proxyEditor.targetID = id
	return true
}

func (state *State) chooseProxyJump(id uint32) bool {
	host, ok := state.hostByID(id)
	if !ok || !host.isSSH() {
		return false
	}
	if id == state.proxyEditor.targetID {
		// The old jump takes the target's place only when no other route has
		// taken it as a target.
		previous := state.proxyEditor.jumpID
		if !state.proxyTargetSelectable(previous) {
			previous = 0
		}
		state.proxyEditor.targetID = previous
	}
	state.proxyEditor.jumpID = id
	return true
}
