package workbench

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/crypto/ssh"

	"motty/internal/i18n"
	"motty/internal/sshconn"
)

// writeTestIdentity saves an unencrypted ed25519 private key and returns its
// path and fingerprint.
func writeTestIdentity(t *testing.T) (path, fingerprint string) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(t.TempDir(), "id_test")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	sshPublic, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return path, ssh.FingerprintSHA256(sshPublic)
}

// The window and the headless server must dial a saved host the same way: a
// saved state read by either gives the same connection for every host.
func TestWindowAndHeadlessCatalogResolveEveryHostToTheSameConnection(t *testing.T) {
	keyPath, fingerprint := writeTestIdentity(t)
	hosts := []savedHost{
		{ID: "edge", Label: "Edge", Address: "edge.example.com", User: "ops", Port: 2200,
			savedTransport: savedTransport{Authentication: "password", Algorithms: "compatible"}},
		{ID: "inner", Label: "Inner", Address: "10.0.0.5", User: "deploy", Port: 22,
			savedTransport: savedTransport{Authentication: "key", Identity: fingerprint}},
		{ID: "deep", Label: "Deep", Address: "10.0.1.9", Port: 22,
			savedTransport: savedTransport{Authentication: "agent", Proxy: &savedOutboundProxy{Kind: "http", Address: "proxy.example.com:3128"}}},
		{ID: "plain", Label: "Plain", Address: "plain.example.com", User: "root", Port: 22},
		{ID: "vnc", Label: "Desktop", Address: "desk.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc"}},
	}
	saved := savedState{
		Hosts:         hosts,
		IdentityFiles: []string{keyPath, keyPath, "", filepath.Join(t.TempDir(), "missing")},
		OutboundProxy: &savedOutboundProxy{Kind: "socks5", Address: "global.example.com:1080", Bypass: "*.local"},
		Proxies: []savedProxy{
			{TargetAddress: "10.0.1.9", TargetPort: 22, JumpAddress: "10.0.0.5", JumpUser: "deploy", JumpPort: 22},
			{TargetAddress: "10.0.0.5", TargetUser: "deploy", TargetPort: 22, JumpAddress: "edge.example.com", JumpUser: "ops", JumpPort: 2200},
			{TargetAddress: "edge.example.com", TargetUser: "ops", TargetPort: 2200, JumpAddress: "10.0.1.9", JumpPort: 22}, // closes a loop: dropped
		},
		ConnectionTimeoutSeconds:   30,
		ConnectionKeepAliveSeconds: 60,
	}

	options := testOptions(t)
	state := newTestState(t, options)
	state.restoreSettings(saved)
	catalog := catalogFromSaved(saved, systemLogin(), keyMessagesIn(i18n.For(i18n.English)))

	if len(catalog.hosts) != len(state.hosts) || len(catalog.proxies) != len(state.proxies) || len(catalog.identities) != len(state.identities) {
		t.Fatalf("catalog has %d hosts, %d routes, %d keys; window has %d, %d, %d",
			len(catalog.hosts), len(catalog.proxies), len(catalog.identities), len(state.hosts), len(state.proxies), len(state.identities))
	}
	if len(catalog.proxies) != 2 {
		t.Fatalf("the loop-closing route must be dropped: %+v", catalog.proxies)
	}
	if got := catalog.identityPaths(); len(got) != 1 {
		t.Fatalf("a repeated, blank and missing key must leave one key: %v", got)
	}

	env := connectEnv{KnownHostsPath: options.KnownHostsPath}
	for _, host := range state.hosts {
		if !host.isSSH() {
			continue
		}
		session := &Session{host: host}
		want, wantValid := state.sshConfigFor(session, host)
		want.Notify = nil
		// The window reads stored keys from its vault; this headless catalog has none.
		if want.StoredKey == nil {
			t.Fatalf("%s: the window's connection cannot read stored keys", host.Label)
		}
		want.StoredKey = nil
		other, found := Host{}, false
		for _, candidate := range catalog.hosts {
			if candidate.CatalogID == host.CatalogID {
				other, found = candidate, true
			}
		}
		if !found {
			t.Fatalf("the headless catalog lacks %s", host.Label)
		}
		got, route, gotValid := catalog.sshConfigFor(other, other, env)
		if !reflect.DeepEqual(got, want) || gotValid != wantValid || route.jumpPath != session.jumpPath || route.proxyKey != session.proxyRoute {
			t.Errorf("%s resolves differently:\nwindow   %+v (%q, %q)\nheadless %+v (%q, %q)",
				host.Label, want, session.jumpPath, session.proxyRoute, got, route.jumpPath, route.proxyKey)
		}
	}

	// What the two agree on is also what the host's settings say.
	deep, _ := catalog.hostByConnection("10.0.1.9", "", 22)
	config, route, valid := catalog.sshConfigFor(deep, deep, env)
	if !valid || len(config.ProxyJumps) != 2 || config.ProxyJumps[0].Host != "edge.example.com" || config.ProxyJumps[1].Host != "10.0.0.5" {
		t.Fatalf("the jump chain of Deep = %+v", config.ProxyJumps)
	}
	if route.jumpPath == "" {
		t.Fatal("a host reached through jump hosts must report its path")
	}
	if config.Proxy == nil || config.Proxy.Address != "proxy.example.com:3128" || config.DialTimeout.Seconds() != 30 || config.KeepAlive.Interval.Seconds() != 60 {
		t.Fatalf("Deep leaves by %+v with timeout %v and keep-alive %+v", config.Proxy, config.DialTimeout, config.KeepAlive)
	}
	inner, _ := catalog.hostByConnection("10.0.0.5", "deploy", 22)
	if auth := catalog.authenticationFor(inner); auth.Methods != sshconn.AuthPublicKey || len(auth.IdentityFiles) != 1 || auth.IdentityFiles[0] != keyPath {
		t.Fatalf("Inner signs in with %+v, want the key %s", auth, keyPath)
	}
}

func TestAgentAccessIsPerComputerAndOnlyForHostsThatExist(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.restoreSettings(savedState{Hosts: []savedHost{
		{ID: "web", Label: "Web", Address: "web.example.com", User: "ops", Port: 22},
		{ID: "db", Label: "DB", Address: "db.example.com", User: "ops", Port: 22},
		{ID: "desk", Label: "Desk", Address: "desk.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc"}},
	}})
	web, desk := state.hosts[0], state.hosts[2]
	state.setAgentAccess(web, true)
	state.setAgentAccess(desk, true) // a desktop host cannot be opened
	state.setAgentAccess(state.hosts[1], true)
	state.setAgentAccess(state.hosts[1], false)

	saved := state.snapshot()
	if !reflect.DeepEqual(saved.AgentHosts, []string{"web"}) {
		t.Fatalf("saved agent hosts = %v", saved.AgentHosts)
	}

	// Another computer's copy of the same hosts starts closed to agents, and the
	// flag of a deleted host is not kept.
	other := newTestState(t, testOptions(t))
	other.restoreSettings(savedState{Hosts: saved.Hosts})
	if got := other.snapshot().AgentHosts; len(got) != 0 {
		t.Fatalf("hosts that arrived without the flag are open: %v", got)
	}
	other.restoreSettings(savedState{Hosts: saved.Hosts, AgentHosts: []string{"web", "gone"}})
	other.restoreLocal(savedState{AgentHosts: []string{"web", "gone"}})
	if got := other.snapshot().AgentHosts; !reflect.DeepEqual(got, []string{"web"}) {
		t.Fatalf("after restore: %v", got)
	}
}

// What the window saves is what the headless server reads, through the store.
func TestHeadlessServerReadsWhatTheWindowSaved(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.restoreSettings(savedState{Hosts: []savedHost{
		{ID: "web", Label: "Web", Address: "web.example.com", User: "ops", Port: 22},
		{ID: "db", Label: "DB", Address: "db.example.com", User: "ops", Port: 22},
	}})
	state.setAgentAccess(state.hosts[0], true)
	state.save()

	catalog, err := readAgentCatalog(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	allowed := catalog.agentHostList()
	if len(catalog.hosts) != 2 || len(allowed) != 1 || allowed[0].Label != "Web" || allowed[0].Address != "web.example.com" {
		t.Fatalf("hosts %d, opened %+v", len(catalog.hosts), allowed)
	}
}
