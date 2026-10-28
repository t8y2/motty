package workbench

import (
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

// selectTypeahead is the typed prefix of one closed select trigger. The owner
// is the trigger whose options the session belongs to; moving to another
// trigger or closing the dialog starts over.
type selectTypeahead struct {
	owner ui.ID
	state components.MenuTypeahead
}

// selectTypeaheadKey commits the option a closed select trigger types toward,
// as Base UI does: the list does not open, the matching value is chosen, and
// repeating the first letter cycles through the options that share it. The
// caller builds the options from the same data the open popup paints.
func (state *State) selectTypeaheadKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if event.Release || event.Text == "" || event.Target.IsZero() ||
		event.Modifiers&(ui.ModifierControl|ui.ModifierAlt|ui.ModifierMeta) != 0 {
		return ui.EventResult{}, false
	}
	items, commit, ok := state.selectTypeaheadOptions(event.Target)
	if !ok {
		state.selectTypeahead = selectTypeahead{}
		return ui.EventResult{}, false
	}
	if state.selectTypeahead.owner != event.Target {
		state.selectTypeahead = selectTypeahead{owner: event.Target}
	}
	id, matched := state.selectTypeahead.state.TypeItems(event.Text, time.Now(), items, ui.ID{})
	if !matched || !commit(id) {
		if !matched {
			state.selectTypeahead.state.Reset()
		}
		return ui.EventResult{}, false
	}
	return ui.EventResult{Handled: true, Repaint: true}, true
}

// selectTypeaheadOptions returns the closed select's choices and the action
// that commits one. It reports false when the target is not a select trigger
// currently showing its value rather than its list.
func (state *State) selectTypeaheadOptions(target ui.ID) ([]components.MenuTypeaheadItem, func(ui.ID) bool, bool) {
	if items, commit, ok := state.devSelectTypeahead(target); ok {
		return items, commit, ok
	}
	switch {
	case state.aiPanelVisible() && target == hitAITarget && !state.ai.panel.targetOpen && state.ai.run == nil:
		options := state.aiTargetOptions()
		items := make([]components.MenuTypeaheadItem, 0, len(options))
		for _, option := range options {
			items = append(items, components.MenuTypeaheadItem{ID: option.ID, Label: option.Label, Disabled: option.Disabled})
		}
		return items, state.aiChooseTarget, true
	case state.activeSection == sectionSettings && state.settingsCategory == settingsGeneral && state.modal == modalNone && target == hitSettingsLanguageSelect && !state.languagePicker.open:
		items := make([]components.MenuTypeaheadItem, 0, len(i18n.Supported()))
		for _, locale := range i18n.Supported() {
			items = append(items, components.MenuTypeaheadItem{ID: languageOptionID(locale.Language), Label: locale.Label})
		}
		return items, state.clickSettings, true

	case state.aiSettingsVisible() && state.settingsCategory == settingsMCP && target == hitAISetMCPSelect && !state.ai.cfg.mcpSelectOpen:
		options := aiMCPClientOptions()
		items := make([]components.MenuTypeaheadItem, 0, len(options))
		for _, option := range options {
			items = append(items, components.MenuTypeaheadItem{ID: option.ID, Label: option.Label})
		}
		return items, state.aiSettingsClick, true

	case state.modal == modalNewForward && target == hitForwardDialogHost && !state.forwardEditor.hostOpen:
		items := make([]components.MenuTypeaheadItem, 0, len(state.hosts))
		for _, host := range state.hosts {
			items = append(items, components.MenuTypeaheadItem{
				ID: hitForwardHostItemList.At(int(host.ID)), Label: host.Label, Disabled: !host.isSSH(),
			})
		}
		return items, func(id ui.ID) bool {
			hostID, ok := id.In(hitForwardHostItemList)
			if !ok {
				return false
			}
			if _, exists := state.hostByID(uint32(hostID)); !exists {
				return false
			}
			state.forwardEditor.hostID = uint32(hostID)
			return true
		}, true

	case state.modal == modalNewProxy && target == hitProxyDialogTarget && !state.proxyEditor.targetOpen:
		items := make([]components.MenuTypeaheadItem, 0, len(state.hosts))
		for _, host := range state.hosts {
			items = append(items, components.MenuTypeaheadItem{
				ID: hitProxyTargetItemList.At(int(host.ID)), Label: host.Label,
				Disabled: !host.isSSH() || state.proxyTargetConfigured(host.ID),
			})
		}
		return items, func(id ui.ID) bool {
			hostID, ok := id.In(hitProxyTargetItemList)
			if !ok || !state.proxyTargetSelectable(uint32(hostID)) {
				return false
			}
			return state.chooseProxyTarget(uint32(hostID))
		}, true

	case state.modal == modalNewProxy && target == hitProxyDialogJump && !state.proxyEditor.jumpOpen:
		items := make([]components.MenuTypeaheadItem, 0, len(state.hosts))
		for _, host := range state.hosts {
			items = append(items, components.MenuTypeaheadItem{
				ID: hitProxyJumpItemList.At(int(host.ID)), Label: host.Label, Disabled: !host.isSSH(),
			})
		}
		return items, func(id ui.ID) bool {
			hostID, ok := id.In(hitProxyJumpItemList)
			if !ok {
				return false
			}
			if _, exists := state.hostByID(uint32(hostID)); !exists {
				return false
			}
			return state.chooseProxyJump(uint32(hostID))
		}, true

	case target == hitAISetFormPreset && !state.ai.cfg.presetOpen:
		items := make([]components.MenuTypeaheadItem, 0, len(aiPresets))
		for index := range aiPresets {
			items = append(items, components.MenuTypeaheadItem{
				ID: hitAISetFormPresetItems.At(index), Label: state.aiPresetLabel(index),
			})
		}
		return items, func(id ui.ID) bool {
			index, ok := id.In(hitAISetFormPresetItems)
			if !ok || index < 0 || index >= len(aiPresets) {
				return false
			}
			state.ai.cfg.preset = index
			return true
		}, true

	case state.modal == modalNewHost && target == hitDialogPortsTrigger && !state.editor.portsOpen:
		items := make([]components.MenuTypeaheadItem, 0, len(state.editor.ports))
		for index, port := range state.editor.ports {
			items = append(items, components.MenuTypeaheadItem{
				ID: hitDialogPortsItemList.At(index), Label: port.Device,
			})
		}
		return items, func(id ui.ID) bool {
			index, ok := id.In(hitDialogPortsItemList)
			if !ok || index < 0 || index >= len(state.editor.ports) {
				return false
			}
			state.editor.address = text.Editor{}
			state.editor.address.Insert(state.editor.ports[index].Device)
			return true
		}, true

	case state.modal == modalNewHost && target == hitDialogIcon && !state.editor.iconOpen:
		choices := hostIconChoices()
		items := make([]components.MenuTypeaheadItem, 0, len(choices))
		for _, choice := range choices {
			items = append(items, components.MenuTypeaheadItem{
				ID: hitDialogIconItemList.At(int(choice)), Label: state.hostIconLabel(choice),
			})
		}
		return items, func(id ui.ID) bool {
			index, ok := id.In(hitDialogIconItemList)
			choice := hostIcon(index)
			if !ok || !choice.valid() {
				return false
			}
			state.editor.icon = choice
			return true
		}, true
	}
	return nil, nil, false
}

// proxyTargetSelectable is the rule the open target select paints: a host that
// is SSH and not already the target of another route.
func (state *State) proxyTargetSelectable(id uint32) bool {
	host, ok := state.hostByID(id)
	return ok && host.isSSH() && !state.proxyTargetConfigured(id)
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
