package workbench

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"motty/internal/ai/store"

	_ "modernc.org/sqlite"
)

// lockedVaultWorld is a settings window with a provider whose key is in a vault
// that has a master password and is locked again.
func lockedVaultWorld(t *testing.T, fake *lister) (*settingsWorld, store.Provider) {
	t.Helper()
	world := newSettingsWorld(t, fake)
	provider := world.addOpenAI(t, "Work", "", testSecretKey)
	if err := world.state.secrets.vault.LegacyForTests("master password"); err != nil {
		t.Fatal(err)
	}
	world.state.secrets.declined = true
	world.state.secrets.vault.DropKeyForTests()
	world.state.reloadAI()
	if got := world.state.ai.providers[0]; got.APIKey != "" || !got.HasKey {
		t.Fatalf("a locked vault should leave the key out of memory: %#v", got)
	}
	return world, provider
}

func TestAIKeyIsInTheVaultAndNeverInTheDatabaseFile(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	provider := world.addOpenAI(t, "Work", "", testSecretKey)
	if err := world.db(t).Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(world.options.AIStorePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(testSecretKey)) {
		t.Fatal("the API key reached ai.db")
	}
	key, found, err := world.state.secrets.vault.Get(aiKeySecretName(provider.ID))
	if err != nil || !found || key != testSecretKey {
		t.Fatalf("vault holds %q found=%v err=%v", key, found, err)
	}
}

func TestAILegacyKeysMoveIntoTheVaultOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"pold", "pnew"} {
		if err := db.SaveProvider(store.Provider{ID: id, Name: id, Kind: store.KindOpenAI, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	// What an older version left behind: a plaintext key in the file.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`UPDATE providers SET api_key = ?, has_key = 1 WHERE id = 'pold'`, testSecretKey); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	options := freshOptions(t)
	options.AIStorePath = path
	options.Invalidate = func() {}
	options.Keystore = &memKeys{}
	state := newTestState(t, options)
	if err := state.secrets.vault.LegacyForTests("master password"); err != nil {
		t.Fatal(err)
	}
	state.secrets.declined = true
	state.secrets.vault.DropKeyForTests()

	// Locked: the key cannot be moved, but it keeps working and stays for later.
	db, err = state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	if got := state.ai.providers[1]; got.ID != "pold" || got.APIKey != testSecretKey {
		t.Fatalf("a legacy key must still work while the vault is locked: %#v", got)
	}
	if legacy, _ := db.LegacyKeys(); legacy["pold"] != testSecretKey {
		t.Fatalf("the legacy key was dropped without being saved: %v", legacy)
	}

	if err := state.secrets.vault.ConvertLegacy("master password"); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	if legacy, _ := db.LegacyKeys(); len(legacy) != 0 {
		t.Fatalf("plaintext still in ai.db: %v", legacy)
	}
	key, found, err := state.secrets.vault.Get(aiKeySecretName("pold"))
	if err != nil || !found || key != testSecretKey {
		t.Fatalf("vault holds %q found=%v err=%v", key, found, err)
	}
	for _, provider := range state.ai.providers {
		if provider.ID == "pold" && (provider.APIKey != testSecretKey || !provider.HasKey) {
			t.Fatalf("migrated provider = %#v", provider)
		}
	}
	if _, found, _ := state.secrets.vault.Get(aiKeySecretName("pnew")); found {
		t.Fatal("a provider without a key got a vault secret")
	}
}

func TestAILockedVaultAsksForThePasswordInsteadOfSendingWithoutAKey(t *testing.T) {
	fake := newLister("gpt-a")
	world, provider := lockedVaultWorld(t, fake)
	state := world.state
	fake.mu.Lock()
	callsBefore := fake.calls
	fake.mu.Unlock()

	state.Click(hitAISetFetchModels)
	if state.modal != modalVaultUnlock {
		t.Fatalf("modal = %d, want the unlock dialog", state.modal)
	}
	fake.mu.Lock()
	made := fake.calls - callsBefore
	fake.mu.Unlock()
	if made != 0 || state.ai.cfg.fetch != nil {
		t.Fatal("a request was made without the key")
	}

	typeInto(state, hitVaultUnlockField, "master password")
	state.Click(hitVaultUnlockSubmit)
	if state.modal != modalNone {
		t.Fatalf("modal = %d after unlocking", state.modal)
	}
	pumpSettings(t, state, "the fetch", func() bool { return state.ai.cfg.fetch == nil && state.ai.cfg.fetched })
	fake.mu.Lock()
	key := fake.key
	fake.mu.Unlock()
	if key != testSecretKey || state.aiModelList(provider.ID) == nil {
		t.Fatalf("after unlocking the request used key %q, models %v", key, state.aiModelList(provider.ID))
	}
}

func TestAIQuestionWaitsForTheVaultAndIsAskedAfterUnlocking(t *testing.T) {
	fake := newLister()
	world, provider := lockedVaultWorld(t, fake)
	state := world.state
	db := world.db(t)
	if err := db.SaveModels(provider.ID, []store.Model{{ProviderID: provider.ID, Name: "m", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDefaultModel(provider.ID, "m"); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()

	state.ai.input.Insert("hello")
	state.aiSend()
	if state.modal != modalVaultUnlock || state.ai.run != nil || len(state.ai.items) != 0 {
		t.Fatalf("modal=%d run=%v items=%v: nothing may be sent while the key is locked", state.modal, state.ai.run, state.ai.items)
	}
	if state.ai.input.String() != "hello" {
		t.Fatal("the question was lost")
	}
	typeInto(state, hitVaultUnlockField, "master password")
	state.Click(hitVaultUnlockSubmit)
	if len(state.ai.items) == 0 || state.ai.items[0].text != "hello" {
		t.Fatalf("the question was not sent after unlocking: %v", state.ai.items)
	}
	fake.mu.Lock()
	key := fake.key
	fake.mu.Unlock()
	if key != testSecretKey {
		t.Fatalf("the request used key %q", key)
	}
}

func TestAIDeletingAProviderDeletesItsVaultSecret(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	provider := world.addOpenAI(t, "Work", "", testSecretKey)

	state.Click(hitAISetProviderDelete.At(0))
	state.Click(hitAISetConfirmAction)
	if len(state.ai.providers) != 0 {
		t.Fatalf("providers after delete = %v", state.ai.providers)
	}
	if _, found, err := state.secrets.vault.Get(aiKeySecretName(provider.ID)); err != nil || found {
		t.Fatalf("the key outlived its provider: found=%v err=%v", found, err)
	}
}

// aiKeyOf is the key a request to provider would carry.
func (state *State) aiKeyOf(provider store.Provider) string {
	keyed, _ := state.aiKeyedProvider(provider, func() {})
	return keyed.APIKey
}

func TestOpeningTheSettingsLeavesSavedAIKeysInTheVault(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	world.addOpenAI(t, "Work", "", testSecretKey)
	reopened := world.open(t)
	if got := reopened.ai.providers[0]; got.APIKey != "" || !got.HasKey {
		t.Fatalf("a saved key should stay in the vault until a request needs it: %#v", got)
	}
	if reopened.aiKeyOf(reopened.ai.providers[0]) != testSecretKey {
		t.Fatal("the key is not found when a request needs it")
	}
}
