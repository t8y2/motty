package workbench

import (
	"errors"
	"testing"

	"motty/internal/ai/store"
)

func addProxyProvider(t *testing.T, world *settingsWorld, proxy string) store.Provider {
	t.Helper()
	state := world.state
	state.Click(hitAISetProviderAdd)
	world.typeInto(t, hitAISetFormName, "Proxy")
	world.typeInto(t, hitAISetFormURL, "https://api.example.com/v1")
	world.typeInto(t, hitAISetFormProxy, proxy)
	world.typeInto(t, hitAISetFormKey, testSecretKey)
	state.Click(hitAISetFormSubmit)
	if state.ai.cfg.dialog != aiDialogNone {
		t.Fatalf("the proxy form stayed open: %q", state.ai.cfg.formErr)
	}
	state.cancelAIFetch()
	if len(state.ai.providers) != 1 {
		t.Fatalf("providers = %v", state.ai.providers)
	}
	return state.ai.providers[0]
}

func TestAIProviderProxyIsSavedWithoutItsPassword(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	saved := addProxyProvider(t, world, "socks5://alice:s3cret@127.0.0.1:1080")

	if saved.ProxyURL != "socks5://alice@127.0.0.1:1080" || !saved.HasProxyPassword {
		t.Fatalf("saved proxy = %q, password saved = %t", saved.ProxyURL, saved.HasProxyPassword)
	}
	if saved.ProxyPassword != "" {
		t.Fatal("the provider carries a plaintext proxy password")
	}
	if password, found, err := state.secrets.vault.Get(aiProxyPasswordSecretName(saved.ID)); err != nil || !found || password != "s3cret" {
		t.Fatalf("vault proxy password = %q, found %t, err %v", password, found, err)
	}
	fromDisk, err := world.db(t).Providers()
	if err != nil || len(fromDisk) != 1 {
		t.Fatalf("providers on disk = %v, err %v", fromDisk, err)
	}
	if fromDisk[0].ProxyURL != "socks5://alice@127.0.0.1:1080" || !fromDisk[0].HasProxyPassword || fromDisk[0].ProxyPassword != "" {
		t.Fatalf("provider on disk = %#v", fromDisk[0])
	}

	keyed, ready := state.aiKeyedProvider(saved, func() {})
	if !ready || keyed.ProxyPassword != "s3cret" {
		t.Fatalf("request provider = %#v, ready %t", keyed, ready)
	}
	if keyed.APIKey != testSecretKey {
		t.Fatalf("request API key = %q", keyed.APIKey)
	}
}

func TestAIProviderProxyPasswordFollowsTheSavedAddress(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	saved := addProxyProvider(t, world, "http://alice:s3cret@127.0.0.1:7890")

	// Saving an unrelated edit with the same address keeps the password.
	state.Click(hitAISetProviderEdit.At(0))
	state.Click(hitAISetFormSubmit)
	state.cancelAIFetch()
	if got := state.ai.providers[0]; got.ProxyURL != saved.ProxyURL || !got.HasProxyPassword {
		t.Fatalf("unchanged proxy = %#v", got)
	}

	// A different address without a password drops the old secret.
	state.Click(hitAISetProviderEdit.At(0))
	for range saved.ProxyURL {
		state.EditorFor(hitAISetFormProxy).DeleteBackward()
	}
	world.typeInto(t, hitAISetFormProxy, "http://127.0.0.1:7890")
	state.Click(hitAISetFormSubmit)
	state.cancelAIFetch()
	if got := state.ai.providers[0]; got.ProxyURL != "http://127.0.0.1:7890" || got.HasProxyPassword {
		t.Fatalf("changed proxy = %#v", got)
	}
	if _, found, err := state.secrets.vault.Get(aiProxyPasswordSecretName(saved.ID)); err != nil || found {
		t.Fatalf("old proxy password found %t, err %v", found, err)
	}
}

func TestDeletingSecretsStopsAtTheFirstFailure(t *testing.T) {
	failure := errors.New("vault write failed")
	var tried []string
	err := deleteSecretsUntilError([]string{"key", "proxy"}, func(name string) error {
		tried = append(tried, name)
		if name == "key" {
			return failure
		}
		return nil
	})
	if !errors.Is(err, failure) {
		t.Fatalf("err = %v, want the first failure to survive a later success", err)
	}
	if len(tried) != 1 {
		t.Fatalf("tried %v after the first deletion failed", tried)
	}
}
