package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/desktop"
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
	saved, _, err := loadState(state.options.StatePath)
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

func TestAHostCanInheritDisableOrOverrideTheGlobalProxy(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	state.proxy = outboundProxy{kind: sshconn.ProxySOCKS5, address: "global.proxy:1080", user: "global", bypass: "localhost"}
	if err := state.secrets.vault.Set(proxyPasswordSecret, "global-secret"); err != nil {
		t.Fatal(err)
	}

	inherited := state.proxyForHost(Host{})
	if inherited == nil || inherited.Address != "global.proxy:1080" || inherited.Password != "global-secret" {
		t.Fatalf("inherited proxy = %+v", inherited)
	}
	if direct := state.proxyForHost(Host{Proxy: &outboundProxy{}}); direct != nil {
		t.Fatalf("direct host received proxy %+v", direct)
	}

	host := Host{
		CatalogID: "host_edge", Proxy: &outboundProxy{
			kind: sshconn.ProxyHTTPConnect, address: "edge.proxy:3128", user: "edge", bypass: "*.internal",
		},
	}
	if err := state.secrets.vault.Set(hostProxyPasswordSecret(host.CatalogID, host.Proxy), "edge-secret"); err != nil {
		t.Fatal(err)
	}
	dedicated := state.proxyForHost(host)
	if dedicated == nil || dedicated.Kind != sshconn.ProxyHTTPConnect || dedicated.Address != "edge.proxy:3128" || dedicated.Username != "edge" || dedicated.Password != "edge-secret" {
		t.Fatalf("dedicated proxy = %+v", dedicated)
	}
	if !dedicated.Uses("service.example:443") || dedicated.Uses("api.internal:443") {
		t.Fatalf("dedicated bypass = %v", dedicated.Bypass)
	}
}

func TestHostProxyConfigurationRoundTripsWithoutItsPassword(t *testing.T) {
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	state := newTestState(t, options)
	host := Host{
		ID: 1, CatalogID: "host_edge", Label: "Edge", Address: "edge.example.com", User: "ops", Port: 22,
		Proxy: &outboundProxy{kind: sshconn.ProxySOCKS5, address: "proxy.example.com:1080", user: "proxy-user", bypass: "localhost, *.corp"},
	}
	host.finish()
	state.hosts = append(state.hosts, host)
	state.nextHostID = 2
	if err := state.secrets.vault.Set(hostProxyPasswordSecret(host.CatalogID, host.Proxy), "never-serialize-me"); err != nil {
		t.Fatal(err)
	}
	state.save()

	saved, _, err := loadState(state.options.StatePath)
	if err != nil || len(saved.Hosts) != 1 || saved.Hosts[0].Proxy == nil {
		t.Fatalf("saved host proxy = %+v (%v)", saved.Hosts, err)
	}
	data := string(mustJSON(t, saved.Hosts[0]))
	if strings.Contains(data, "never-serialize-me") || saved.Hosts[0].Proxy.Kind != "socks5" || saved.Hosts[0].Proxy.Address != "proxy.example.com:1080" {
		t.Fatalf("serialized host proxy = %s", data)
	}

	restarted := newTestState(t, state.options)
	restored := restarted.hosts[0]
	if restored.Proxy == nil || restored.Proxy.kind != sshconn.ProxySOCKS5 || restored.Proxy.address != "proxy.example.com:1080" || restored.Proxy.user != "proxy-user" || restored.Proxy.bypass != "localhost, *.corp" {
		t.Fatalf("restored host proxy = %+v", restored.Proxy)
	}
	entry := savedHostFromHost(restored)
	imported, ok := restarted.hostFromCatalogEntry(entry)
	if !ok || imported.Proxy == nil || imported.Proxy.address != restored.Proxy.address || strings.Contains(string(mustJSON(t, entry)), "never-serialize-me") {
		t.Fatalf("catalog round trip = %+v, ok=%t", imported.Proxy, ok)
	}
}

func TestADamagedSavedHostProxyFallsBackToInheritance(t *testing.T) {
	_, options := proxyWorld(t)
	if err := saveState(options.StatePath, savedState{Hosts: []savedHost{{
		ID: "host_edge", Label: "Edge", Address: "edge.example.com", Port: 22,
		savedTransport: savedTransport{Proxy: &savedOutboundProxy{Kind: "socks5", Address: "missing-port"}},
	}}}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if len(state.hosts) != 1 || state.hosts[0].Proxy != nil {
		t.Fatalf("damaged host proxy = %+v", state.hosts)
	}
}

func TestTheHostProxyDialogStagesAProxyUntilTheHostIsSaved(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	state.proxy = outboundProxy{kind: sshconn.ProxyHTTPConnect, address: "global.proxy:3128"}
	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, "Edge Proxy")
	typeInto(state, hitDialogAddress, "edge-proxy.example.com")
	typeInto(state, hitDialogUser, "ops")
	state.Click(hitHostProxyConfigure)
	if state.modal != modalOutboundProxy || !state.proxyForm.forHost {
		t.Fatalf("host proxy modal = %d form=%+v", state.modal, state.proxyForm)
	}
	state.Click(hitProxyKindSOCKS5)
	typeInto(state, hitOutboundProxyAddress, "host.proxy:1080")
	typeInto(state, hitOutboundProxyUser, "proxy-user")
	typeInto(state, hitOutboundProxyPassword, "host-secret")
	state.proxyForm.bypass = *text.NewEditor("localhost, *.corp")
	state.Click(hitOutboundProxySubmit)
	if state.modal != modalNewHost || state.editor.proxy == nil || state.editor.proxy.kind != sshconn.ProxySOCKS5 || state.editor.proxy.address != "host.proxy:1080" {
		t.Fatalf("staged host proxy = %+v modal=%d", state.editor.proxy, state.modal)
	}
	if state.proxy.address != "global.proxy:3128" {
		t.Fatalf("the global proxy changed to %+v", state.proxy)
	}
	catalogID := catalogIDFor(Host{Address: "edge-proxy.example.com", User: "ops", Port: 22})
	if _, found, _ := state.secrets.vault.Get(hostProxyPasswordSecret(catalogID, state.editor.proxy)); found {
		t.Fatal("the staged proxy password entered the vault before the host was saved")
	}

	state.Click(hitDialogSubmit)
	added := state.hosts[len(state.hosts)-1]
	if state.modal != modalNone || added.Proxy == nil || added.Proxy.address != "host.proxy:1080" {
		t.Fatalf("saved host proxy = %+v modal=%d error=%q", added.Proxy, state.modal, state.dialogError)
	}
	if state.editor.proxyPassword != "" {
		t.Fatal("the staged proxy password remained in the host editor after save")
	}
	password, found, err := state.secrets.vault.Get(hostProxyPasswordSecret(added.CatalogID, added.Proxy))
	if err != nil || !found || password != "host-secret" {
		t.Fatalf("saved host proxy password = %q, %t, %v", password, found, err)
	}
	if strings.Contains(string(mustJSON(t, state.snapshot())), "host-secret") {
		t.Fatal("the host proxy password entered the saved state")
	}
}

func TestCancelingHostProxyConfigurationReturnsToTheUnchangedHost(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	state.Click(hitAddHost)
	state.Click(hitHostProxyConfigure)
	state.Click(hitProxyKindHTTP)
	typeInto(state, hitOutboundProxyAddress, "proxy.example.com:3128")
	state.Click(hitOutboundProxyCancel)
	if state.modal != modalNewHost || state.editor.proxy != nil {
		t.Fatalf("cancel returned to modal %d with proxy %+v", state.modal, state.editor.proxy)
	}
	state.Click(hitDialogCancel)
	if len(state.hosts) != len(sampleHosts) || state.secrets.vault.Len() != 0 {
		t.Fatalf("cancel changed hosts=%d vault=%d", len(state.hosts), state.secrets.vault.Len())
	}
}

func TestCancelingTheOuterHostFormDiscardsAnAcceptedProxyDraft(t *testing.T) {
	t.Run("new host", func(t *testing.T) {
		_, options := proxyWorld(t)
		state := newTestState(t, options)
		state.Click(hitAddHost)
		typeInto(state, hitDialogLabel, "Canceled")
		typeInto(state, hitDialogAddress, "canceled.example.com")
		state.Click(hitHostProxyConfigure)
		state.Click(hitProxyKindSOCKS5)
		typeInto(state, hitOutboundProxyAddress, "draft.proxy:1080")
		typeInto(state, hitOutboundProxyUser, "draft-user")
		typeInto(state, hitOutboundProxyPassword, "draft-secret")
		state.Click(hitOutboundProxySubmit)
		state.Click(hitDialogCancel)
		if len(state.hosts) != len(sampleHosts) || state.secrets.vault.Len() != 0 {
			t.Fatalf("outer cancel changed hosts=%d vault=%d", len(state.hosts), state.secrets.vault.Len())
		}
	})

	t.Run("existing host", func(t *testing.T) {
		_, options := proxyWorld(t)
		state := newTestState(t, options)
		host := &state.hosts[0]
		host.Proxy = &outboundProxy{kind: sshconn.ProxySOCKS5, address: "saved.proxy:1080", user: "saved-user"}
		secretName := hostProxyPasswordSecret(host.CatalogID, host.Proxy)
		if err := state.secrets.vault.Set(secretName, "saved-secret"); err != nil {
			t.Fatal(err)
		}
		state.openEditHostID(host.ID)
		state.Click(hitHostProxyConfigure)
		state.Click(hitProxyKindHTTP)
		state.proxyForm.address = *text.NewEditor("draft.proxy:3128")
		state.proxyForm.user = *text.NewEditor("draft-user")
		state.proxyForm.password = *text.NewEditor("draft-secret")
		state.Click(hitOutboundProxySubmit)
		state.Click(hitDialogCancel)
		if got := state.hosts[0].Proxy; got == nil || got.kind != sshconn.ProxySOCKS5 || got.address != "saved.proxy:1080" || got.user != "saved-user" {
			t.Fatalf("outer cancel changed host proxy to %+v", got)
		}
		if got, found, err := state.secrets.vault.Get(secretName); err != nil || !found || got != "saved-secret" {
			t.Fatalf("outer cancel changed secret to %q, %t, %v", got, found, err)
		}
	})
}

func TestTheHostProxyDialogRejectsAnInvalidDedicatedAddress(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	state.Click(hitAddHost)
	state.Click(hitHostProxyConfigure)
	state.Click(hitProxyKindSOCKS5)
	typeInto(state, hitOutboundProxyAddress, "missing-port")
	state.Click(hitOutboundProxySubmit)
	if state.modal != modalOutboundProxy || state.proxyForm.dialogError != state.messages.Tools.ProxyErrorAddress {
		t.Fatalf("invalid proxy modal=%d error=%q", state.modal, state.proxyForm.dialogError)
	}
	state.Click(hitOutboundProxyCancel)
	if state.modal != modalNewHost {
		t.Fatalf("cancel returned to modal %d", state.modal)
	}
}

func TestTheHostDialogOffersProxyConfigurationOnlyForNetworkHosts(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	state.Click(hitAddHost)
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitHostProxyConfigure); !ok {
		t.Fatal("an SSH host does not offer proxy configuration")
	}
	state.Click(hitDialogProtocolSerial)
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, hitHostProxyConfigure); ok {
		t.Fatal("a serial host offers proxy configuration")
	}
}

func TestChangingAHostProxyLoginRequiresANewPassword(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	host := &state.hosts[0]
	host.Proxy = &outboundProxy{kind: sshconn.ProxySOCKS5, address: "old.proxy:1080", user: "alice"}
	oldSecret := hostProxyPasswordSecret(host.CatalogID, host.Proxy)
	if err := state.secrets.vault.Set(oldSecret, "old-secret"); err != nil {
		t.Fatal(err)
	}
	state.openEditHostID(host.ID)
	state.Click(hitHostProxyConfigure)
	state.proxyForm.address = *text.NewEditor("new.proxy:1080")
	state.Click(hitOutboundProxySubmit)
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.Tools.HostProxyPasswordChanged {
		t.Fatalf("changed login modal=%d error=%q", state.modal, state.dialogError)
	}
	state.Click(hitHostProxyConfigure)
	state.proxyForm.password = *text.NewEditor("new-secret")
	state.Click(hitOutboundProxySubmit)
	state.Click(hitDialogSubmit)
	if state.modal != modalNone || state.dialogError != "" {
		t.Fatalf("saving the new login modal=%d error=%q", state.modal, state.dialogError)
	}
	proxy := state.proxyForHost(state.hosts[0])
	if proxy == nil || proxy.Address != "new.proxy:1080" || proxy.Password != "new-secret" {
		t.Fatalf("new login proxy = %+v", proxy)
	}
	if _, found, _ := state.secrets.vault.Get(oldSecret); found {
		t.Fatal("the old proxy password survived the login change")
	}
}

func TestARepointedProxyCannotReuseTheOldPassword(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	host := &state.hosts[0]
	host.Proxy = &outboundProxy{kind: sshconn.ProxySOCKS5, address: "old.proxy:1080", user: "alice"}
	if err := state.secrets.vault.Set(hostProxyPasswordSecret(host.CatalogID, host.Proxy), "old-secret"); err != nil {
		t.Fatal(err)
	}
	// A synced or imported host record can repoint the proxy without going
	// through the host dialog; the bound secret must not follow it.
	host.Proxy = &outboundProxy{kind: sshconn.ProxySOCKS5, address: "new.proxy:1080", user: "alice"}
	if proxy := state.proxyForHost(*host); proxy == nil || proxy.Password != "" {
		t.Fatalf("repointed proxy reused the old secret: %+v", proxy)
	}
}

func TestAHostProxyDescriptionIsHostScoped(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	state.editor.proxy = &outboundProxy{kind: sshconn.ProxySOCKS5, address: "host.proxy:1080", user: "alice"}
	description := state.hostProxyDescription()
	if !strings.Contains(description, "this host") || strings.Contains(description, "every session") {
		t.Fatalf("host proxy description = %q", description)
	}
}

func TestRemovingAHostProxyLoginOpensTheDefaultVault(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	host := &state.hosts[0]
	host.Proxy = &outboundProxy{kind: sshconn.ProxySOCKS5, address: "old.proxy:1080", user: "alice"}
	secret := hostProxyPasswordSecret(host.CatalogID, host.Proxy)
	if err := state.secrets.vault.Set(secret, "old-secret"); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.Lock()

	state.openEditHostID(host.ID)
	state.Click(hitHostProxyConfigure)
	state.Click(hitProxyKindNone)
	state.Click(hitOutboundProxySubmit)
	state.Click(hitDialogSubmit)
	if state.modal != modalNone || state.dialogError != "" {
		t.Fatalf("removing the login modal=%d error=%q", state.modal, state.dialogError)
	}
	if state.hosts[0].Proxy == nil || state.hosts[0].Proxy.kind != sshconn.ProxyNone {
		t.Fatalf("removed proxy = %+v", state.hosts[0].Proxy)
	}
	if _, found, _ := state.secrets.vault.Get(secret); found {
		t.Fatal("the retired proxy password survived")
	}
}

func TestDeletingAProxyHostOpensTheDefaultVault(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	host := state.hosts[0]
	host.Proxy = &outboundProxy{kind: sshconn.ProxySOCKS5, address: "old.proxy:1080", user: "alice"}
	state.hosts[0].Proxy = host.Proxy
	secret := hostProxyPasswordSecret(host.CatalogID, host.Proxy)
	if err := state.secrets.vault.Set(secret, "old-secret"); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.Lock()
	state.hostActionIDs = []uint32{host.ID}
	state.confirmDeleteHosts()
	if len(state.hosts) != len(sampleHosts)-1 || state.dialogError != "" {
		t.Fatalf("delete hosts=%d error=%q", len(state.hosts), state.dialogError)
	}
	if _, found, _ := state.secrets.vault.Get(secret); found {
		t.Fatal("the deleted host's proxy password survived")
	}
}

func TestHostProxyConfigurationFitsEveryNetworkDialogInAShortWindow(t *testing.T) {
	for _, protocol := range []hostProtocol{protocolSSH, protocolTelnet, protocolVNC, protocolRDP} {
		t.Run(protocolName(protocol), func(t *testing.T) {
			_, options := proxyWorld(t)
			state := newTestState(t, options)
			state.openHostDialog()
			state.editor.setProtocol(protocol)
			canvas, _ := paintAt(state, ui.Size{Width: 900, Height: 600})
			state.editor.form.offset = state.editor.form.geometry.MaxOffsetY
			canvas, _ = paintAt(state, ui.Size{Width: 900, Height: 600})
			proxy, ok := hitByID(canvas, hitHostProxyConfigure)
			if !ok {
				t.Fatal("proxy configuration was not painted")
			}
			submit, ok := hitByID(canvas, hitDialogSubmit)
			if !ok || proxy.Bounds.Y+proxy.Bounds.Height > submit.Bounds.Y {
				t.Fatalf("proxy control %+v reaches footer %+v", proxy.Bounds, submit.Bounds)
			}
		})
	}
}

func TestSavingAnInheritedHostDoesNotOpenTheSecretStore(t *testing.T) {
	_, options := proxyWorld(t)
	keys := &memKeys{}
	options.Keystore = keys
	state := newTestState(t, options)
	before := keys.loadCount()
	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, "No Proxy Secret")
	typeInto(state, hitDialogAddress, "no-proxy-secret.example.com")
	state.Click(hitDialogSubmit)
	if state.modal != modalNone || state.dialogError != "" {
		t.Fatalf("saving host modal=%d error=%q", state.modal, state.dialogError)
	}
	if after := keys.loadCount(); after != before {
		t.Fatalf("secret store loads changed from %d to %d", before, after)
	}
}

func TestAnSSHHostCanForceDirectInsteadOfTheGlobalProxy(t *testing.T) {
	world, options := proxyWorld(t)
	state := newTestState(t, options)
	state.proxy = outboundProxy{kind: sshconn.ProxySOCKS5, address: "global.proxy:1080"}
	state.hosts[0].Proxy = &outboundProxy{}
	openHost(state)
	if len(world.configs) != 1 || world.configs[0].Proxy != nil {
		t.Fatalf("SSH config proxy = %+v", world.configs)
	}
}

func TestStreamHostsUseTheirOwnProxyPolicyAndSerialUsesNone(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	state.proxy = outboundProxy{kind: sshconn.ProxySOCKS5, address: "global.proxy:1080"}
	streams := &streamWorld{}
	streams.install(state)
	addStreamHost(state, Host{
		Label: "Switch", Address: "10.0.0.2", Protocol: protocolTelnet,
		Proxy: &outboundProxy{kind: sshconn.ProxyHTTPConnect, address: "host.proxy:3128"},
	})
	openHost(state)
	waitUntil(t, state, "the Telnet request", func() bool { return len(streams.requests) == 1 })
	if proxy := streams.requests[0].Proxy; proxy == nil || proxy.Address != "host.proxy:3128" {
		t.Fatalf("Telnet proxy = %+v", proxy)
	}

	state.Click(hitHosts)
	addStreamHost(state, Host{Label: "Console", Address: "/dev/ttyUSB0", Protocol: protocolSerial})
	openHost(state)
	waitUntil(t, state, "the serial request", func() bool { return len(streams.requests) == 2 })
	if proxy := streams.requests[1].Proxy; proxy != nil {
		t.Fatalf("serial proxy = %+v", proxy)
	}
}

func TestDirectAndTunneledDesktopHostsUseTheDesktopHostsProxyPolicy(t *testing.T) {
	world, options := proxyWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	state.proxy = outboundProxy{kind: sshconn.ProxySOCKS5, address: "global.proxy:1080"}
	var requests []desktopRequest
	state.dialDesktop = func(_ context.Context, request desktopRequest) (desktop.Session, error) {
		requests = append(requests, request)
		return newPictureOnly(), nil
	}
	dedicated := &outboundProxy{kind: sshconn.ProxyHTTPConnect, address: "desktop.proxy:3128"}
	addStreamHost(state, Host{Label: "Direct desktop", Address: "direct.example.com", Protocol: protocolRDP, Proxy: dedicated})
	openHost(state)
	waitUntil(t, state, "the direct desktop request", func() bool { return len(requests) == 1 })
	if proxy := requests[0].Proxy; proxy == nil || proxy.Address != "desktop.proxy:3128" {
		t.Fatalf("direct desktop proxy = %+v", proxy)
	}

	state.Click(hitHosts)
	addStreamHost(state, Host{Label: "Tunneled desktop", Address: "tunnel.example.com", User: "ops", Protocol: protocolVNC, ViaSSH: true, Proxy: dedicated})
	openHost(state)
	if len(world.configs) == 0 {
		t.Fatal("tunneled desktop did not create an SSH config")
	}
	if proxy := world.configs[len(world.configs)-1].Proxy; proxy == nil || proxy.Address != "desktop.proxy:3128" {
		t.Fatalf("tunneled desktop SSH proxy = %+v", proxy)
	}
}

func TestANewHostGetsAUniqueCatalogIDBeforeItsProxyPasswordIsSaved(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	originalID := state.hosts[0].CatalogID
	state.hosts[0].Proxy = &outboundProxy{kind: sshconn.ProxySOCKS5, address: "original.proxy:1080", user: "original-user"}
	if err := state.secrets.vault.Set(hostProxyPasswordSecret(originalID, state.hosts[0].Proxy), "original-secret"); err != nil {
		t.Fatal(err)
	}

	state.openEditHostID(state.hosts[0].ID)
	state.editor.address = *text.NewEditor("moved.example.com")
	state.Click(hitDialogSubmit)
	if state.hosts[0].CatalogID != originalID {
		t.Fatalf("editing changed catalog id from %q to %q", originalID, state.hosts[0].CatalogID)
	}

	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, "Replacement")
	typeInto(state, hitDialogAddress, "prod.example.com")
	typeInto(state, hitDialogUser, "deploy")
	state.Click(hitHostProxyConfigure)
	state.Click(hitProxyKindHTTP)
	typeInto(state, hitOutboundProxyAddress, "replacement.proxy:3128")
	typeInto(state, hitOutboundProxyUser, "replacement-user")
	typeInto(state, hitOutboundProxyPassword, "replacement-secret")
	state.Click(hitOutboundProxySubmit)
	state.Click(hitDialogSubmit)
	replacement := state.hosts[len(state.hosts)-1]
	if replacement.CatalogID == originalID {
		t.Fatalf("new host reused catalog id %q", originalID)
	}
	for name, want := range map[string]string{
		hostProxyPasswordSecret(originalID, state.hosts[0].Proxy):         "original-secret",
		hostProxyPasswordSecret(replacement.CatalogID, replacement.Proxy): "replacement-secret",
	} {
		if got, found, err := state.secrets.vault.Get(name); err != nil || !found || got != want {
			t.Fatalf("secret %q = %q, %t, %v; want %q", name, got, found, err, want)
		}
	}

	restarted := newTestState(t, state.options)
	if len(restarted.hosts) != len(state.hosts) {
		t.Fatalf("restart hosts=%d, want %d", len(restarted.hosts), len(state.hosts))
	}
	seen := map[string]bool{}
	for _, host := range restarted.hosts {
		if seen[host.CatalogID] {
			t.Fatalf("duplicate catalog id after restart: %q", host.CatalogID)
		}
		seen[host.CatalogID] = true
	}
}

func TestARejectedHostPasswordDoesNotPartiallySaveTheProxyPassword(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	existing := addStreamHost(state, Host{Label: "Existing VNC", Address: "vnc.example.com", Protocol: protocolVNC})
	passwordName, ok := state.hostPasswordSecretName(existing)
	if !ok || state.secrets.vault.Set(passwordName, "existing-password") != nil {
		t.Fatal("could not seed the existing VNC password")
	}

	state.Click(hitAddHost)
	state.Click(hitDialogProtocolVNC)
	typeInto(state, hitDialogLabel, "Conflicting VNC")
	typeInto(state, hitDialogAddress, "vnc.example.com")
	typeInto(state, hitDialogPassword, "different-password")
	state.Click(hitDialogViaSSH) // distinct host endpoint, same VNC password key
	state.Click(hitHostProxyConfigure)
	state.Click(hitProxyKindSOCKS5)
	typeInto(state, hitOutboundProxyAddress, "private.proxy:1080")
	typeInto(state, hitOutboundProxyUser, "proxy-user")
	typeInto(state, hitOutboundProxyPassword, "proxy-secret")
	state.Click(hitOutboundProxySubmit)
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.HostPasswordShared {
		t.Fatalf("rejected save modal=%d error=%q", state.modal, state.dialogError)
	}
	names, err := state.secrets.vault.Names()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if strings.HasPrefix(name, hostProxyPasswordPrefix) {
			t.Fatalf("rejected host save left proxy secret %q", name)
		}
	}
	if state.editor.proxyPassword != "proxy-secret" {
		t.Fatal("rejected host save cleared the proxy password draft")
	}
}

func TestCopyingAHostCarriesItsDedicatedProxyPassword(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	source := &state.hosts[0]
	source.Proxy = &outboundProxy{kind: sshconn.ProxyHTTPConnect, address: "shared.proxy:3128", user: "proxy-user"}
	if err := state.secrets.vault.Set(hostProxyPasswordSecret(source.CatalogID, source.Proxy), "proxy-secret"); err != nil {
		t.Fatal(err)
	}
	state.openCopyHostID(source.ID)
	state.editor.address = *text.NewEditor("prod-copy.example.com")
	state.Click(hitDialogSubmit)
	copy := state.hosts[len(state.hosts)-1]
	if state.modal != modalNone || copy.Proxy == nil || copy.Proxy.address != source.Proxy.address {
		t.Fatalf("copy proxy = %+v modal=%d error=%q", copy.Proxy, state.modal, state.dialogError)
	}
	if got, found, err := state.secrets.vault.Get(hostProxyPasswordSecret(copy.CatalogID, copy.Proxy)); err != nil || !found || got != "proxy-secret" {
		t.Fatalf("copied proxy password = %q, %t, %v", got, found, err)
	}
}

func TestDeletingAHostRetiresItsProxyPasswordBeforeItsCatalogIDCanBeReused(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	deleted := state.hosts[0]
	state.hosts[0].Proxy = &outboundProxy{kind: sshconn.ProxySOCKS5, address: "old.proxy:1080", user: "proxy-user"}
	secretName := hostProxyPasswordSecret(deleted.CatalogID, state.hosts[0].Proxy)
	if err := state.secrets.vault.Set(secretName, "old-secret"); err != nil {
		t.Fatal(err)
	}
	state.hostActionIDs = []uint32{deleted.ID}
	state.confirmDeleteHosts()
	if _, found, err := state.secrets.vault.Get(secretName); err != nil || found {
		t.Fatalf("deleted host proxy password found=%t err=%v", found, err)
	}

	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, "Replacement")
	typeInto(state, hitDialogAddress, deleted.Address)
	typeInto(state, hitDialogUser, deleted.User)
	state.Click(hitHostProxyConfigure)
	state.Click(hitProxyKindHTTP)
	typeInto(state, hitOutboundProxyAddress, "new.proxy:3128")
	typeInto(state, hitOutboundProxyUser, "proxy-user")
	state.Click(hitOutboundProxySubmit)
	state.Click(hitDialogSubmit)
	replacement := state.hosts[len(state.hosts)-1]
	if replacement.CatalogID != deleted.CatalogID {
		t.Fatalf("replacement catalog id = %q, want reused %q", replacement.CatalogID, deleted.CatalogID)
	}
	if proxy := state.proxyForHost(replacement); proxy == nil || proxy.Password != "" {
		t.Fatalf("replacement inherited deleted password: %+v", proxy)
	}
}

func TestDeletingAnAuthenticatedProxyHostWaitsForTheLockedVault(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	host := &state.hosts[0]
	host.Proxy = &outboundProxy{kind: sshconn.ProxySOCKS5, address: "private.proxy:1080", user: "proxy-user"}
	secretName := hostProxyPasswordSecret(host.CatalogID, host.Proxy)
	if err := state.secrets.vault.Set(secretName, "proxy-secret"); err != nil {
		t.Fatal(err)
	}
	if err := state.secrets.vault.SetMasterPassword("a master password"); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.Lock()
	state.hostActionIDs = []uint32{host.ID}
	state.confirmDeleteHosts()
	if len(state.hosts) != len(sampleHosts) || state.dialogError != state.messages.Tools.ProxyPasswordLocked {
		t.Fatalf("locked delete hosts=%d error=%q", len(state.hosts), state.dialogError)
	}
	if err := state.secrets.vault.Unlock("a master password"); err != nil {
		t.Fatal(err)
	}
	if got, found, err := state.secrets.vault.Get(secretName); err != nil || !found || got != "proxy-secret" {
		t.Fatalf("blocked delete changed secret to %q, %t, %v", got, found, err)
	}
}

func TestRemovingAHostProxyLoginWaitsForTheLockedVault(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	host := &state.hosts[0]
	host.Proxy = &outboundProxy{kind: sshconn.ProxySOCKS5, address: "old.proxy:1080", user: "old-user"}
	secretName := hostProxyPasswordSecret(host.CatalogID, host.Proxy)
	if err := state.secrets.vault.Set(secretName, "old-secret"); err != nil {
		t.Fatal(err)
	}
	if err := state.secrets.vault.SetMasterPassword("a master password"); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.Lock()

	state.openEditHostID(host.ID)
	state.Click(hitHostProxyConfigure)
	state.Click(hitProxyKindNone)
	state.Click(hitOutboundProxySubmit)
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.Tools.ProxyPasswordLocked {
		t.Fatalf("removal modal=%d error=%q", state.modal, state.dialogError)
	}
	if state.hosts[0].Proxy == nil || state.hosts[0].Proxy.kind != sshconn.ProxySOCKS5 {
		t.Fatalf("locked removal changed saved proxy to %+v", state.hosts[0].Proxy)
	}
	if err := state.secrets.vault.Unlock("a master password"); err != nil {
		t.Fatal(err)
	}
	if got, found, err := state.secrets.vault.Get(secretName); err != nil || !found || got != "old-secret" {
		t.Fatalf("locked removal changed secret to %q, %t, %v", got, found, err)
	}
}

func TestSwitchingANewHostToSerialIgnoresItsHiddenProxyDraft(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, "Serial console")
	typeInto(state, hitDialogAddress, "/dev/tty-test")
	state.Click(hitHostProxyConfigure)
	state.Click(hitProxyKindSOCKS5)
	typeInto(state, hitOutboundProxyAddress, "unused.proxy:1080")
	typeInto(state, hitOutboundProxyUser, "unused-user")
	typeInto(state, hitOutboundProxyPassword, "unused-secret")
	state.Click(hitOutboundProxySubmit)
	if err := state.secrets.vault.SetMasterPassword("a master password"); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.Lock()

	state.Click(hitDialogProtocolSerial)
	state.Click(hitDialogSubmit)
	added := state.hosts[len(state.hosts)-1]
	if state.modal != modalNone || state.dialogError != "" || added.Protocol != protocolSerial || added.Proxy != nil {
		t.Fatalf("serial save modal=%d error=%q host=%+v", state.modal, state.dialogError, added)
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
