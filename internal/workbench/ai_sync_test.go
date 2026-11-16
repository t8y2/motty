package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"motty/internal/ai/store"
	"motty/internal/configstore"
)

// addProviderTo puts a provider with a key and two models where the assistant
// keeps them, the way the settings page does.
func addProviderTo(t *testing.T, state *State, id string) {
	t.Helper()
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := state.secrets.vault.Set(aiKeySecretName(id), "sk-"+id); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveProvider(store.Provider{ID: id, Name: "Work " + id, Kind: store.KindOpenAI, BaseURL: "https://api.example.com/v1", HasKey: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveModels(id, []store.Model{{ProviderID: id, Name: "big", Enabled: true}, {ProviderID: id, Name: "small"}}); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
}

func providerNames(state *State) []string {
	var names []string
	for _, provider := range state.ai.providers {
		names = append(names, provider.Name)
	}
	return names
}

func TestAProviderWithItsModelsAndKeyReachesTheOtherComputer(t *testing.T) {
	server, _ := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	second, _, _ := backupTestState(t)
	configureSync(t, first, server)
	configureSync(t, second, server)
	done := first.messages.Tools.SyncDone

	addProviderTo(t, first, "p1")
	syncAndWait(t, first, hitSyncNow, done)
	syncAndWait(t, second, hitSyncNow, done)

	if got := providerNames(second); len(got) != 1 || got[0] != "Work p1" {
		t.Fatalf("the other computer has providers %v", got)
	}
	provider := second.ai.providers[0]
	if !provider.HasKey || provider.BaseURL != "https://api.example.com/v1" || second.aiKeyOf(provider) != "sk-p1" {
		t.Fatalf("the provider arrived as %+v", provider)
	}
	var enabled []string
	for _, model := range second.ai.models {
		if model.Enabled {
			enabled = append(enabled, model.Name)
		}
	}
	if len(second.ai.models) != 2 || len(enabled) != 1 || enabled[0] != "big" {
		t.Fatalf("models %+v", second.ai.models)
	}
	if key, found, _ := second.secrets.vault.Get(aiKeySecretName("p1")); !found || key != "sk-p1" {
		t.Fatalf("the key did not arrive: %q %v", key, found)
	}
}

func TestAProviderKeepsItsAuthStyleAcrossTheOtherComputer(t *testing.T) {
	server, _ := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	second, _, _ := backupTestState(t)
	configureSync(t, first, server)
	configureSync(t, second, server)
	done := first.messages.Tools.SyncDone

	db, err := first.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := first.secrets.vault.Set(aiKeySecretName("relay"), "relay-key"); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveProvider(store.Provider{
		ID: "relay", Name: "Relay", Kind: store.KindAnthropic, BaseURL: "https://relay.example.com",
		AuthStyle: store.AuthStyleBearer, HasKey: true, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	first.reloadAI()
	syncAndWait(t, first, hitSyncNow, done)
	syncAndWait(t, second, hitSyncNow, done)

	if got := providerNames(second); len(got) != 1 || got[0] != "Relay" {
		t.Fatalf("the other computer has providers %v", got)
	}
	if got := second.ai.providers[0].AuthStyle; got != store.AuthStyleBearer {
		t.Fatalf("auth style = %q, want %q", got, store.AuthStyleBearer)
	}
}

func TestAProviderKeepsItsRequestFormatAcrossTheOtherComputer(t *testing.T) {
	server, _ := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	second, _, _ := backupTestState(t)
	configureSync(t, first, server)
	configureSync(t, second, server)
	done := first.messages.Tools.SyncDone

	db, err := first.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveProvider(store.Provider{
		ID: "relay", Name: "Relay", Kind: store.KindOpenAI, BaseURL: "https://relay.example.com/v1",
		APIFormat: store.APIFormatResponses, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	first.reloadAI()
	syncAndWait(t, first, hitSyncNow, done)
	syncAndWait(t, second, hitSyncNow, done)

	if len(second.ai.providers) != 1 || second.ai.providers[0].APIFormat != store.APIFormatResponses {
		t.Fatalf("the other computer has %#v", second.ai.providers)
	}
}

// A provider record written before authentication styles existed has no field;
// reading it keeps the historical x-api-key behavior.
func TestAProviderRecordWithoutAuthStyleReadsAsTheDefault(t *testing.T) {
	state, _, _ := backupTestState(t)
	state.importAIProviderCollections(&[]savedAIProvider{{
		ID: "old", Name: "Old", Kind: store.KindAnthropic, BaseURL: "https://old.example.com", Enabled: true,
	}}, nil)
	if len(state.ai.providers) != 1 || state.ai.providers[0].AuthStyle != "" || state.ai.providers[0].APIFormat != "" {
		t.Fatalf("providers = %#v", state.ai.providers)
	}
}

func TestDeletingAProviderDeletesItOnTheOtherComputerToo(t *testing.T) {
	server, _ := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	second, _, _ := backupTestState(t)
	configureSync(t, first, server)
	configureSync(t, second, server)
	done := first.messages.Tools.SyncDone
	addProviderTo(t, first, "p1")
	syncAndWait(t, first, hitSyncNow, done)
	syncAndWait(t, second, hitSyncNow, done)
	if len(second.ai.providers) != 1 {
		t.Fatal("the provider did not arrive")
	}

	second.aiDeleteProvider("p1")
	syncAndWait(t, second, hitSyncNow, done)
	syncAndWait(t, first, hitSyncNow, done)
	if len(first.ai.providers) != 0 {
		t.Fatalf("the provider is still on the first computer: %v", providerNames(first))
	}
	if _, found, _ := first.secrets.vault.Get(aiKeySecretName("p1")); found {
		t.Fatal("its key is still in the first computer's vault")
	}
}

// An assistant database that is lost must not look like the providers were all
// deleted: that would delete them everywhere.
func TestALostAssistantDatabaseDoesNotDeleteTheProvidersEverywhere(t *testing.T) {
	first, options, _ := backupTestState(t)
	addProviderTo(t, first, "p1")
	if len(first.ai.providers) != 1 {
		t.Fatal("no provider")
	}
	first.Close()
	if err := os.Remove(filepath.Join(filepath.Dir(options.StatePath), "ai.db")); err != nil {
		t.Fatal(err)
	}

	again := newTestState(t, options)
	if _, err := again.aiStore(); err != nil {
		t.Fatal(err)
	}
	if got := providerNames(again); len(got) != 1 || got[0] != "Work p1" {
		t.Fatalf("the providers were not brought back from the records: %v", got)
	}
	store, err := configstore.Open(options.StatePath, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	records, _ := store.Records()
	live := 0
	for _, record := range records {
		if record.Kind == kindAIProvider && !record.Deleted {
			live++
		}
	}
	if live != 1 {
		t.Fatalf("%d provider records are live", live)
	}
}

func TestAWindowThatHasNotLookedAtTheAssistantDoesNotDeleteItsProviders(t *testing.T) {
	first, options, _ := backupTestState(t)
	addProviderTo(t, first, "p1")
	other := newTestState(t, options) // has not opened the assistant's database
	other.terminalScrollback = 10000
	other.save()
	store, err := configstore.Open(options.StatePath, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	records, _ := store.Records()
	for _, record := range records {
		if record.Kind == kindAIProvider && record.Deleted {
			t.Fatalf("a window that never read the providers deleted one: %v", record)
		}
	}
}
