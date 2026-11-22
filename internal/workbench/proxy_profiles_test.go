package workbench

import (
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/text"
	"motty/internal/sshconn"
)

func addProxyProfile(t *testing.T, state *State, name, address, user, password string) string {
	t.Helper()
	state.openProxyProfileDialog("")
	typeInto(state, hitProxyProfileName, name)
	typeInto(state, hitProxyProfileAddress, address)
	if user != "" {
		typeInto(state, hitProxyProfileUser, user)
	}
	if password != "" {
		typeInto(state, hitProxyProfilePassword, password)
	}
	state.Click(hitProxyProfileSubmit)
	if state.modal != modalNone {
		t.Fatalf("profile dialog stayed open: error %q", state.proxyProfileForm.dialogError)
	}
	for _, profile := range state.proxyProfiles {
		if profile.Name == name {
			return profile.ID
		}
	}
	t.Fatalf("profile %q was not saved", name)
	return ""
}

func useProxyProfile(t *testing.T, state *State, hostIndex int, id string) {
	t.Helper()
	state.openEditHostID(state.hosts[hostIndex].ID)
	state.Click(hitHostProxyConfigure)
	if state.modal != modalOutboundProxy {
		t.Fatalf("host proxy dialog modal = %d", state.modal)
	}
	state.proxyForm.profile = id
	if profile, ok := state.proxyProfileByID(id); ok {
		state.loadProxyProfileIntoForm(profile)
	}
	state.Click(hitOutboundProxySubmit)
	if state.modal != modalNewHost {
		t.Fatalf("host proxy dialog did not return to the host form: %d %q", state.modal, state.proxyForm.dialogError)
	}
	state.Click(hitDialogSubmit)
	if state.modal != modalNone {
		t.Fatalf("host dialog stayed open: %q", state.dialogError)
	}
}

func TestProxyProfileCarriesHostsAndEditsReachTheNextConnection(t *testing.T) {
	world, options := proxyWorld(t)
	state := newTestState(t, options)
	id := addProxyProfile(t, state, "Office", "127.0.0.1:1080", "alice", "s3cret")
	if _, found, err := state.secrets.vault.Get(proxyProfileSecretName(id)); err != nil || !found {
		t.Fatalf("profile password in vault: found=%v err=%v", found, err)
	}
	useProxyProfile(t, state, 0, id)
	if state.hosts[0].ProxyProfile != id || state.hosts[0].Proxy != nil {
		t.Fatalf("host proxy = %+v profile %q", state.hosts[0].Proxy, state.hosts[0].ProxyProfile)
	}
	if uses := state.proxyProfileUses(id); uses != 1 {
		t.Fatalf("profile is used by %d hosts", uses)
	}
	connect(t, state, world)
	proxy := world.configs[len(world.configs)-1].Proxy
	if proxy == nil || proxy.Kind != sshconn.ProxySOCKS5 || proxy.Address != "127.0.0.1:1080" || proxy.Username != "alice" || proxy.Password != "s3cret" {
		t.Fatalf("connection proxy = %+v", proxy)
	}

	state.openProxyProfileDialog(id)
	state.proxyProfileForm.address = *text.NewEditor("127.0.0.1:1081")
	state.proxyProfileForm.password = *text.NewEditor("new-secret")
	state.Click(hitProxyProfileSubmit)
	if state.proxyProfiles[0].Address != "127.0.0.1:1081" {
		t.Fatalf("profile address = %q", state.proxyProfiles[0].Address)
	}
	proxy = state.proxyForHost(state.hosts[0])
	if proxy == nil || proxy.Address != "127.0.0.1:1081" || proxy.Password != "new-secret" {
		t.Fatalf("resolution after editing the profile = %+v", proxy)
	}

	restored := newTestState(t, options)
	if len(restored.proxyProfiles) != 1 || restored.proxyProfiles[0].Address != "127.0.0.1:1081" {
		t.Fatalf("restored profiles = %+v", restored.proxyProfiles)
	}
	if restored.hosts[0].ProxyProfile != id {
		t.Fatalf("restored host profile = %q", restored.hosts[0].ProxyProfile)
	}
}

func TestDeletingAProxyProfileLeavesItsHostsDirect(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	id := addProxyProfile(t, state, "Office", "127.0.0.1:1080", "alice", "s3cret")
	useProxyProfile(t, state, 0, id)

	state.askDeleteProxyProfile(0)
	if state.modal != modalDeleteRule || state.ruleDelete.uses != 1 {
		t.Fatalf("delete dialog = %d uses=%d", state.modal, state.ruleDelete.uses)
	}
	state.Click(hitRuleDeleteConfirm)
	if state.modal != modalNone || len(state.proxyProfiles) != 0 {
		t.Fatalf("profiles after delete = %+v modal %d", state.proxyProfiles, state.modal)
	}
	if _, found, _ := state.secrets.vault.Get(proxyProfileSecretName(id)); found {
		t.Fatal("the profile secret survived the profile")
	}
	host := state.hosts[0]
	if host.ProxyProfile != "" || host.Proxy == nil || host.Proxy.kind != sshconn.ProxyNone {
		t.Fatalf("host after delete = profile %q proxy %+v", host.ProxyProfile, host.Proxy)
	}
	if proxy := state.proxyForHost(host); proxy != nil {
		t.Fatalf("a deleted profile still carried the host: %+v", proxy)
	}

	restored := newTestState(t, options)
	if len(restored.proxyProfiles) != 0 || restored.hosts[0].ProxyProfile != "" {
		t.Fatalf("restored profiles = %+v host profile %q", restored.proxyProfiles, restored.hosts[0].ProxyProfile)
	}
}

func TestProxyProfileIsExportedAsAnIDWithoutItsSecret(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	id := addProxyProfile(t, state, "Office", "127.0.0.1:1080", "alice", "s3cret")
	useProxyProfile(t, state, 0, id)
	state.hosts[0].ProxyBypass = "localhost, *.corp"
	state.save()

	entry := savedHostFromHost(state.hosts[0])
	if entry.ProxyProfile != id || entry.Proxy != nil || entry.ProxyBypass != "localhost, *.corp" {
		t.Fatalf("exported transport = %+v", entry)
	}
	data, err := json.Marshal(entry)
	if err != nil || strings.Contains(string(data), "s3cret") || strings.Contains(string(data), "127.0.0.1:1080") {
		t.Fatalf("exported host = %s (%v)", data, err)
	}
	host, ok := state.hostFromCatalogEntry(entry)
	if !ok || host.ProxyProfile != id || host.ProxyBypass != "localhost, *.corp" {
		t.Fatalf("imported host = %+v ok=%v", host, ok)
	}

	// Imported where the profile is unknown, the host is direct rather than
	// silently using the global proxy.
	other := newTestState(t, testOptions(t))
	other.proxy = outboundProxy{kind: sshconn.ProxySOCKS5, address: "global:1080"}
	if proxy := other.proxyForHost(host); proxy != nil {
		t.Fatalf("an unknown profile fell back to %+v", proxy)
	}
}

func TestProxyProfileRefusesUnusableRecords(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	state.openProxyProfileDialog("")
	typeInto(state, hitProxyProfileName, "Bad")
	typeInto(state, hitProxyProfileAddress, "not-a-proxy")
	state.Click(hitProxyProfileSubmit)
	if state.modal != modalProxyProfile || state.proxyProfileForm.dialogError != state.messages.ProxyProfiles.ErrorAddress {
		t.Fatalf("invalid address: modal %d error %q", state.modal, state.proxyProfileForm.dialogError)
	}
	state.proxyProfileForm.address = *text.NewEditor("127.0.0.1:1080")
	state.Click(hitProxyProfileSubmit)
	if len(state.proxyProfiles) != 1 {
		t.Fatalf("profiles = %+v", state.proxyProfiles)
	}
	state.openProxyProfileDialog("")
	typeInto(state, hitProxyProfileName, "bad")
	typeInto(state, hitProxyProfileAddress, "127.0.0.1:1081")
	state.Click(hitProxyProfileSubmit)
	if state.modal != modalProxyProfile || state.proxyProfileForm.dialogError != state.messages.ProxyProfiles.ErrorNameTaken {
		t.Fatalf("duplicate name: modal %d error %q", state.modal, state.proxyProfileForm.dialogError)
	}
	state.Click(hitProxyProfileCancel)

	if got := decodeProxyProfiles([]savedProxyProfile{{ID: "prof_x", Name: "ok", Kind: "socks5", Address: "127.0.0.1:1080"}, {ID: "prof_y", Name: "bad", Kind: "socks5", Address: "nope"}}); len(got) != 1 || got[0].ID != "prof_x" {
		t.Fatalf("decoded profiles = %+v", got)
	}
}

func TestProxyProfileTestChecksTheEndpoint(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	id := addProxyProfile(t, state, "Local", listener.Addr().String(), "", "")
	state.openProxyProfileDialog(id)
	state.Click(hitProxyProfileTest)
	deadline := time.Now().Add(2 * time.Second)
	for state.proxyProfileForm.testRunning && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		state.drainAI()
	}
	if state.proxyProfileForm.testRunning || state.proxyProfileForm.testMessage != state.messages.ProxyProfiles.TestEndpointOK {
		t.Fatalf("test result = %q running=%v", state.proxyProfileForm.testMessage, state.proxyProfileForm.testRunning)
	}
}

// startSOCKS5 runs a minimal no-authentication SOCKS5 proxy that accepts one
// CONNECT, so the profile dialog's test button can carry a real connection.
func startSOCKS5(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				header := make([]byte, 2)
				if _, err := io.ReadFull(conn, header); err != nil || header[0] != 5 {
					return
				}
				methods := make([]byte, int(header[1]))
				if _, err := io.ReadFull(conn, methods); err != nil {
					return
				}
				_, _ = conn.Write([]byte{5, 0})
				request := make([]byte, 4)
				if _, err := io.ReadFull(conn, request); err != nil {
					return
				}
				switch request[3] {
				case 1:
					_, _ = io.ReadFull(conn, make([]byte, 4))
				case 3:
					length := make([]byte, 1)
					_, _ = io.ReadFull(conn, length)
					_, _ = io.ReadFull(conn, make([]byte, int(length[0])))
				case 4:
					_, _ = io.ReadFull(conn, make([]byte, 16))
				}
				_, _ = io.ReadFull(conn, make([]byte, 2))
				_, _ = conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
			}(conn)
		}
	}()
	return listener.Addr().String()
}

func TestProxyProfileTestCarriesARealConnection(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	address := startSOCKS5(t)
	id := addProxyProfile(t, state, "Local SOCKS", address, "", "")
	state.openProxyProfileDialog(id)
	typeInto(state, hitProxyProfileTarget, "example.com:443")
	state.Click(hitProxyProfileTest)
	deadline := time.Now().Add(2 * time.Second)
	for state.proxyProfileForm.testRunning && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		state.drainAI()
	}
	want := state.messages.ProxyProfiles.TestOKFmt
	if state.proxyProfileForm.testRunning || !state.proxyProfileForm.testOK ||
		!strings.Contains(state.proxyProfileForm.testMessage, "example.com:443") {
		t.Fatalf("test result = %q ok=%v running=%v want %q", state.proxyProfileForm.testMessage, state.proxyProfileForm.testOK, state.proxyProfileForm.testRunning, want)
	}
}

func TestSwitchingAHostToAProfileRetiresItsOwnProxyPassword(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	host := &state.hosts[0]
	host.Proxy = &outboundProxy{kind: sshconn.ProxySOCKS5, address: "own.proxy:1080", user: "old"}
	own := hostProxyPasswordSecret(host.CatalogID, host.Proxy)
	if err := state.secrets.vault.Set(own, "old-secret"); err != nil {
		t.Fatal(err)
	}
	state.save()
	id := addProxyProfile(t, state, "Office", "127.0.0.1:1080", "alice", "s3cret")
	useProxyProfile(t, state, 0, id)
	if _, found, err := state.secrets.vault.Get(own); err != nil || found {
		t.Fatalf("the host's own proxy secret survived: found=%v err=%v", found, err)
	}
	if password, found, err := state.secrets.vault.Get(proxyProfileSecretName(id)); err != nil || !found || password != "s3cret" {
		t.Fatalf("profile password = %q found=%v err=%v", password, found, err)
	}
}

func TestANewProfileFromTheHostProxyDialogReturnsChosen(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, "Inline")
	typeInto(state, hitDialogAddress, "inline.example.com")
	typeInto(state, hitDialogUser, "root")
	state.Click(hitHostProxyConfigure)
	if state.modal != modalOutboundProxy {
		t.Fatalf("host proxy modal = %d", state.modal)
	}
	state.openProxyProfileDialogOverHost()
	typeInto(state, hitProxyProfileName, "New profile")
	typeInto(state, hitProxyProfileAddress, "127.0.0.1:1080")
	state.Click(hitProxyProfileSubmit)
	if state.modal != modalOutboundProxy {
		t.Fatalf("after save modal = %d, want host proxy", state.modal)
	}
	profile, ok := state.proxyProfileByID(state.proxyForm.profile)
	if !ok || profile.Name != "New profile" || state.proxyForm.inherit {
		t.Fatalf("form profile = %+v ok=%v inherit=%v", profile, ok, state.proxyForm.inherit)
	}
	state.Click(hitOutboundProxySubmit)
	if state.editor.proxyProfile != profile.ID || state.editor.proxy != nil {
		t.Fatalf("staged host profile = %q proxy %+v", state.editor.proxyProfile, state.editor.proxy)
	}
}

func TestCancelingANewProfileFromTheHostProxyDialogKeepsTheManualRoute(t *testing.T) {
	_, options := proxyWorld(t)
	state := newTestState(t, options)
	state.Click(hitAddHost)
	state.Click(hitHostProxyConfigure)
	state.openProxyProfileDialogOverHost()
	state.Click(hitProxyProfileCancel)
	if state.modal != modalOutboundProxy || state.proxyForm.profile != "" {
		t.Fatalf("after cancel modal = %d profile = %q", state.modal, state.proxyForm.profile)
	}
	if len(state.proxyProfiles) != 0 {
		t.Fatalf("cancel added profiles: %+v", state.proxyProfiles)
	}
}
