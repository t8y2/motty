package workbench

import (
	"testing"

	"motty/internal/ai/store"
)

// aiModelScopeState is a window with one enabled provider carrying two enabled
// models and one disabled one, plus a disabled provider whose model is enabled.
func aiModelScopeState(t *testing.T) (*State, *store.Store) {
	t.Helper()
	state, _ := panelState(t, quietModel{"ok"})
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveProvider(store.Provider{ID: "p", Name: "Primary", Kind: store.KindOpenAI, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveProvider(store.Provider{ID: "q", Name: "Off", Kind: store.KindOpenAI, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveModels("p", []store.Model{
		{ProviderID: "p", Name: "alpha", Enabled: true},
		{ProviderID: "p", Name: "bravo", Enabled: true},
		{ProviderID: "p", Name: "charlie", Enabled: false},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveModels("q", []store.Model{{ProviderID: "q", Name: "delta", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDefaultModel("p", "alpha"); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	return state, db
}

// aiScopeModelIndex finds a model in the chip's offer.
func aiScopeModelIndex(t *testing.T, state *State, model string) (int, int) {
	t.Helper()
	for providerIndex, provider := range state.aiMenuProviders() {
		for modelIndex, entry := range provider.models {
			if entry.name == model {
				return providerIndex, modelIndex
			}
		}
	}
	t.Fatalf("model %q is not offered by the chip", model)
	return -1, -1
}

// The chip offers the models switched on in the settings, not every model that
// was ever fetched, and not the models of a provider that is switched off.
func TestAIModelMenuOffersOnlyEnabledModels(t *testing.T) {
	state, _ := aiModelScopeState(t)
	providers := state.aiMenuProviders()
	if len(providers) != 1 || providers[0].id != "p" {
		t.Fatalf("providers = %+v, want only the enabled one", providers)
	}
	var names []string
	for _, model := range providers[0].models {
		names = append(names, model.name)
	}
	if len(names) != 2 || names[0] != "alpha" || names[1] != "bravo" {
		t.Fatalf("models = %v, want the enabled alpha and bravo", names)
	}
}

// Choosing a model in the workspace changes that conversation only: the global
// default and the enabled switches stay as the settings left them.
func TestAIModelChoiceChangesOnlyTheConversation(t *testing.T) {
	state, db := aiModelScopeState(t)
	providerIndex, modelIndex := aiScopeModelIndex(t, state, "bravo")
	state.aiChooseModel(providerIndex, modelIndex)

	providerID, name, ok, err := db.DefaultModel()
	if err != nil || !ok || providerID != "p" || name != "alpha" {
		t.Fatalf("global default = %q/%q ok=%v err=%v, want the setting's alpha", providerID, name, ok, err)
	}
	models, err := db.Models()
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range models {
		if model.Name == "charlie" && model.Enabled {
			t.Fatal("choosing a workspace model switched on a disabled setting")
		}
	}
	provider, model, err := state.aiModel()
	if err != nil || provider.ID != "p" || model != "bravo" {
		t.Fatalf("request model = %q/%q err=%v, want the conversation's bravo", provider.ID, model, err)
	}
	for _, entry := range state.aiMenuProviders()[0].models {
		if entry.name == "bravo" && !entry.current {
			t.Fatal("the chip does not mark the conversation's choice")
		}
		if entry.name == "alpha" && entry.current {
			t.Fatal("the chip still marks the global default as current")
		}
	}
}

// Until the user picks a model, the conversation follows the setting's default.
// A pick stays through a setting change, and a pick that is switched off falls
// back to the default. A new conversation follows the setting again.
func TestAIModelSelectionFollowsSettingsUntilPinned(t *testing.T) {
	state, db := aiModelScopeState(t)
	if err := db.SetDefaultModel("p", "bravo"); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	if _, model, err := state.aiModel(); err != nil || model != "bravo" {
		t.Fatalf("unpinned conversation = %q err=%v, want the new default bravo", model, err)
	}

	if err := db.SetDefaultModel("p", "alpha"); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	providerIndex, modelIndex := aiScopeModelIndex(t, state, "bravo")
	state.aiChooseModel(providerIndex, modelIndex)

	if err := db.SetDefaultModel("p", "alpha"); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	if _, model, err := state.aiModel(); err != nil || model != "bravo" {
		t.Fatalf("pinned conversation = %q err=%v, want its own bravo", model, err)
	}

	if err := db.SetModelEnabled("p", "bravo", false); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	if _, model, err := state.aiModel(); err != nil || model != "alpha" {
		t.Fatalf("conversation with a disabled pick = %q err=%v, want the default alpha", model, err)
	}

	if err := db.SetModelEnabled("p", "bravo", true); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	providerIndex, modelIndex = aiScopeModelIndex(t, state, "bravo")
	state.aiChooseModel(providerIndex, modelIndex)
	state.aiNewConversation()
	if err := db.SetDefaultModel("p", "alpha"); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	if _, model, err := state.aiModel(); err != nil || model != "alpha" {
		t.Fatalf("new conversation = %q err=%v, want to follow the default alpha", model, err)
	}
}

// The model chip's menu carries a settings shortcut at the providers' level.
func TestAIModelMenuOffersAShortcutToSettings(t *testing.T) {
	state, _ := aiModelScopeState(t)
	state.ai.open = true
	state.ai.panel.modelMenu = true
	canvas, _ := paintAt(state, panelWide)
	row := aiMenuRow(t, canvas, "Manage models…")
	clickMenuItem(t, state, row)
	if state.ai.panel.modelMenu {
		t.Fatal("the settings shortcut left the model menu open")
	}
	if state.activeSection != sectionSettings || state.settingsCategory != settingsAssistant {
		t.Fatalf("shortcut opened section %v category %v, want Settings > Assistant", state.activeSection, state.settingsCategory)
	}
}
