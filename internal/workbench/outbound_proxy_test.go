package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/vivaui/viva/ui"
	"strings"
	"testing"

	"github.com/vivaui/viva/text"
	"motty/internal/sshconn"
)

func proxyWorld(t testing.TB) (*world, Options) {
	return newWorld(t)
}

func configureProxy(t *testing.T, state *State, kind ui.ID, address, user, password, bypass string) {
	t.Helper()
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsConnection)
	state.Click(hitSettingsProxyConfigure)
	if state.modal != modalOutboundProxy {
		t.Fatalf("modal = %d", state.modal)
	}
	state.Click(kind)
	for _, field := range []struct {
		hit   ui.ID
		value string
	}{{hitOutboundProxyAddress, address}, {hitOutboundProxyUser, user}, {hitOutboundProxyPassword, password}} {
		if field.value != "" {
			typeInto(state, field.hit, field.value)
		}
	}
	if bypass != "" {
		state.proxyForm.bypass = *text.NewEditor(bypass)
	}
	state.Click(hitOutboundProxySubmit)
}

func TestAConfiguredProxyCarriesEveryNewConnectionWithItsPasswordFromTheVault(t *testing.T) {
	world, options := proxyWorld(t)
	state := newTestState(t, options)
	configureProxy(t, state, hitProxyKindSOCKS5, "proxy.corp:1080", "me", "s3cret", "")
	if state.modal != modalNone || state.proxy.kind != sshconn.ProxySOCKS5 || state.proxy.address != "proxy.corp:1080" || state.proxy.user != "me" {
		t.Fatalf("modal %d, proxy %+v, error %q", state.modal, state.proxy, state.proxyForm.dialogError)
	}
	if password, found, err := state.secrets.vault.Get(proxyPasswordSecret); err != nil || !found || password != "s3cret" {
		t.Fatalf("the vault holds %q %v %v", password, found, err)
	}
	saved, _, err := loadState(options.StatePath)
	if err != nil || saved.OutboundProxy == nil || saved.OutboundProxy.Kind != "socks5" || strings.Contains(string(mustJSON(t, saved)), "s3cret") {
		t.Fatalf("saved proxy = %+v (the password must not be in the state file) %v", saved.OutboundProxy, err)
	}

	state.Click(hitHosts)
	openHost(state)
	proxy := world.configs[len(world.configs)-1].Proxy
	if proxy == nil || proxy.Kind != sshconn.ProxySOCKS5 || proxy.Address != "proxy.corp:1080" || proxy.Username != "me" || proxy.Password != "s3cret" || proxy.Err != nil {
		t.Fatalf("connection proxy = %+v", proxy)
	}
	if !proxy.Uses("prod.example.com:22") || proxy.Uses("localhost:22") {
		t.Fatal("the default bypass list does not keep localhost direct")
	}
	if restored := newTestState(t, options); restored.proxy != state.proxy {
		t.Fatalf("restored %+v", restored.proxy)
	}
}

func TestTheProxyDialogRefusesWhatCannotWork(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	for _, address := range []string{"", "nohostport", "host:0", "host:99999", "ho st:80", ":80"} {
		state.Click(hitSettings)
		state.Click(hitSettingsProxyConfigure)
		state.Click(hitProxyKindHTTP)
		if address != "" {
			typeInto(state, hitOutboundProxyAddress, address)
		}
		state.Click(hitOutboundProxySubmit)
		if state.modal != modalOutboundProxy || state.proxyForm.dialogError != state.messages.Tools.ProxyErrorAddress {
			t.Fatalf("%q: modal %d error %q", address, state.modal, state.proxyForm.dialogError)
		}
		state.Click(hitOutboundProxyCancel)
	}
	if state.proxy.kind != sshconn.ProxyNone {
		t.Fatalf("a refused form changed the proxy: %+v", state.proxy)
	}
	// "None" needs no address.
	state.Click(hitSettingsProxyConfigure)
	state.Click(hitProxyKindNone)
	state.Click(hitOutboundProxySubmit)
	if state.modal != modalNone {
		t.Fatalf("modal %d, error %q", state.modal, state.proxyForm.dialogError)
	}
}

func TestRemovingTheLoginOrTheProxyForgetsItsPassword(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	configureProxy(t, state, hitProxyKindHTTP, "p:3128", "me", "pw", "")
	if _, found, _ := state.secrets.vault.Get(proxyPasswordSecret); !found {
		t.Fatal("the password was not saved")
	}
	state.Click(hitSettingsProxyConfigure)
	if state.proxyForm.password.String() != "" {
		t.Fatal("the saved password was put back in the form")
	}
	state.proxyForm.user = *text.NewEditor("")
	state.Click(hitOutboundProxySubmit)
	if _, found, _ := state.secrets.vault.Get(proxyPasswordSecret); found {
		t.Fatal("a proxy without a login kept a password")
	}
}

func TestALockedVaultMakesAConnectionThroughTheProxyFailWithTheReason(t *testing.T) {
	world, options := proxyWorld(t)
	state := newTestState(t, options)
	configureProxy(t, state, hitProxyKindSOCKS5, "p:1080", "me", "pw", "")
	if err := state.secrets.vault.SetMasterPassword("a master password"); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.Lock()
	state.Click(hitHosts)
	openHost(state)
	proxy := world.configs[len(world.configs)-1].Proxy
	if proxy == nil || proxy.Err == nil || !strings.Contains(proxy.Err.Error(), "locked") {
		t.Fatalf("proxy = %+v", proxy)
	}
	if _, err := proxy.Dial(context.Background(), 0, "h:22"); !errors.Is(err, proxy.Err) {
		t.Fatalf("dialing gave %v", err)
	}
}

func TestADamagedProxyInTheStateFileIsDropped(t *testing.T) {
	_, options := proxyWorld(t)
	if err := saveState(options.StatePath, savedState{OutboundProxy: &savedOutboundProxy{Kind: "socks5", Address: "no port here", User: "u", Bypass: "a, b"}}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if state.proxy.kind != sshconn.ProxyNone || state.proxy.address != "" || state.proxy.bypass != "a, b" {
		t.Fatalf("proxy = %+v", state.proxy)
	}
	if currentNone := state.currentProxy(); currentNone != nil {
		t.Fatalf("a dropped proxy is still used: %+v", currentNone)
	}
}

func TestBypassEntriesAreSplitOnCommasAndSpaces(t *testing.T) {
	got := splitBypass(" localhost,127.0.0.0/8 ;  *.corp.example\n10.0.0.1 ")
	want := []string{"localhost", "127.0.0.0/8", "*.corp.example", "10.0.0.1"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v", got)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
