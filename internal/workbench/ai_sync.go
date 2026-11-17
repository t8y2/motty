package workbench

import (
	"encoding/json"

	"motty/internal/ai/store"
)

// The assistant's providers are shared between computers like the hosts are: the
// name, address and kind of each, which of its models are on, and that it has a
// key. The key itself is a saved password (aikey:<id>) and travels with the
// others. The assistant's own database stays what the assistant reads; the
// records are a copy kept in step with it, written when it changes and written
// back into it when a sync brings some in.

// savedAIProvider is a provider as a record.
type savedAIProvider struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	BaseURL   string `json:"base_url,omitempty"`
	ModelsURL string `json:"models_url,omitempty"`
	// AuthStyle is empty for the kind's default. It stays out of the payload
	// when it is empty so records written before bearer authentication read
	// back unchanged.
	AuthStyle string `json:"auth_style,omitempty"`
	// APIFormat is empty for Chat Completions, on the same terms.
	APIFormat string         `json:"api_format,omitempty"`
	Enabled   bool           `json:"enabled,omitempty"`
	HasKey    bool           `json:"has_key,omitempty"`
	Models    []savedAIModel `json:"models,omitempty"`
}

type savedAIModel struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled,omitempty"`
}

// savedAIProviders is the HTTP providers for the state, or nil while the assistant's
// database has not been read: a window that has not looked must not be taken to
// say there are none.
func (state *State) savedAIProviders() *[]savedAIProvider {
	return state.savedAIProviderCollection(false)
}

func (state *State) savedAICLIProviders() *[]savedAIProvider {
	return state.savedAIProviderCollection(true)
}

func (state *State) savedAIProviderCollection(cli bool) *[]savedAIProvider {
	ai := &state.ai
	if ai.store == nil || ai.storeErr != nil {
		return nil
	}
	list := make([]savedAIProvider, 0, len(ai.providers))
	for _, provider := range ai.providers {
		if store.IsCLIKind(provider.Kind) != cli {
			continue
		}
		entry := savedAIProvider{
			ID: provider.ID, Name: provider.Name, Kind: provider.Kind, BaseURL: provider.BaseURL,
			ModelsURL: provider.ModelsURL,
			AuthStyle: provider.AuthStyle, APIFormat: provider.APIFormat, Enabled: provider.Enabled, HasKey: provider.HasKey,
		}
		for _, model := range ai.models {
			if model.ProviderID == provider.ID {
				entry.Models = append(entry.Models, savedAIModel{Name: model.Name, Enabled: model.Enabled})
			}
		}
		list = append(list, entry)
	}
	return &list
}

// importAIProviderCollections adds, updates and removes recorded providers.
// Collections are independently authoritative. In particular an old backup or
// HTTP-only update cannot imply that CLI providers were deleted.
func (state *State) importAIProviderCollections(http, cli *[]savedAIProvider) {
	// Opening the database reads it, and a read saves; the records this window is
	// bringing in are not in its base yet, so that save would stamp them anew.
	knownHTTP := state.recordedProviderIDs(kindAIProvider)
	knownCLI := state.recordedProviderIDs(kindAICLIProvider)
	state.pauseSave++
	db, err := state.aiStore()
	state.pauseSave--
	if err != nil || db == nil {
		return
	}
	current, err := db.Providers()
	if err != nil {
		return
	}
	var list []savedAIProvider
	for _, collection := range []struct {
		entries *[]savedAIProvider
		cli     bool
	}{{http, false}, {cli, true}} {
		if collection.entries == nil {
			continue
		}
		for _, entry := range *collection.entries {
			if store.IsCLIKind(entry.Kind) == collection.cli {
				list = append(list, entry)
			}
		}
	}
	wanted := make(map[string]bool, len(list))
	for _, entry := range list {
		if entry.ID == "" || !store.ValidProviderKind(entry.Kind) {
			continue
		}
		wanted[entry.ID] = true
		provider := store.Provider{ID: entry.ID, Name: entry.Name, Kind: entry.Kind, BaseURL: entry.BaseURL, ModelsURL: entry.ModelsURL, AuthStyle: entry.AuthStyle, APIFormat: entry.APIFormat, HasKey: entry.HasKey, Enabled: entry.Enabled}
		for _, local := range current {
			if local.ID == entry.ID && local.Kind == entry.Kind {
				provider.CLIPath = local.CLIPath
			}
		}
		if db.SaveProvider(provider) != nil {
			continue
		}
		models := make([]store.Model, 0, len(entry.Models))
		for _, model := range entry.Models {
			models = append(models, store.Model{ProviderID: entry.ID, Name: model.Name, Enabled: model.Enabled})
		}
		_ = db.SaveModels(entry.ID, models)
	}
	for _, provider := range current {
		// Only what the records once held can have been deleted elsewhere; a provider
		// here that was never recorded is new, not deleted.
		known, imported := knownHTTP, http != nil
		if store.IsCLIKind(provider.Kind) {
			known, imported = knownCLI, cli != nil
		}
		if imported && known[provider.ID] && !wanted[provider.ID] {
			_ = db.DeleteProvider(provider.ID)
		}
	}
	state.pauseSave++
	state.reloadAI()
	state.pauseSave--
}

// adoptRecordedProviders restores recorded providers the assistant's database
// is missing. A database that was lost or set aside must not make the providers
// look deleted to every other computer, and a database that still holds another
// provider (for example HTTP) must not hide a recorded CLI provider.
func (state *State) adoptRecordedProviders(db *store.Store) {
	if state.configBase == nil {
		return
	}
	current, err := db.Providers()
	if err != nil {
		return
	}
	present := make(map[string]bool, len(current))
	for _, provider := range current {
		present[provider.ID] = true
	}
	var recorded []savedAIProvider
	for key, payload := range state.configBase.payloads {
		if key.Kind != kindAIProvider && key.Kind != kindAICLIProvider {
			continue
		}
		var wrapper recordPayload
		var entry savedAIProvider
		if json.Unmarshal(payload, &wrapper) == nil && json.Unmarshal(wrapper.Data, &entry) == nil {
			recorded = append(recorded, entry)
		}
	}
	for _, entry := range recorded {
		if entry.ID == "" || present[entry.ID] || !store.ValidProviderKind(entry.Kind) {
			continue
		}
		present[entry.ID] = true
		_ = db.SaveProvider(store.Provider{ID: entry.ID, Name: entry.Name, Kind: entry.Kind, BaseURL: entry.BaseURL, AuthStyle: entry.AuthStyle, APIFormat: entry.APIFormat, HasKey: entry.HasKey, Enabled: entry.Enabled})
		models := make([]store.Model, 0, len(entry.Models))
		for _, model := range entry.Models {
			models = append(models, store.Model{ProviderID: entry.ID, Name: model.Name, Enabled: model.Enabled})
		}
		_ = db.SaveModels(entry.ID, models)
	}
}

const (
	kindAIProvider    = "ai-provider"
	kindAICLIProvider = "ai-cli-provider"
)

// recordedProviderIDs are the providers the records of this window's base hold.
func (state *State) recordedProviderIDs(kind string) map[string]bool {
	ids := map[string]bool{}
	if state.configBase != nil {
		for key := range state.configBase.payloads {
			if key.Kind == kind {
				ids[key.ID] = true
			}
		}
	}
	return ids
}

func (state *State) recordedProviders(kind string) int { return len(state.recordedProviderIDs(kind)) }
