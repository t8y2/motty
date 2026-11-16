package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"

	"motty/internal/sessionimport"
	"motty/internal/sshconn"
)

const bastionXshell = "[CONNECTION]\r\nHost=127.0.0.1\r\nPort=63871\r\nProtocol=SSH\r\n" +
	"[CONNECTION:AUTHENTICATION]\r\nUserName=root\r\nPassword=ENCRYPTED\r\nUserKey=fake_rsa\r\n" +
	"[CONNECTION:SSH]\r\nCipherList=aes128-cbc,aes192-cbc,aes256-cbc\r\n"

func TestXshellLaunchPathNeedsOneExistingSessionFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tunnel.xsh")
	if err := os.WriteFile(path, []byte(bastionXshell), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := xshellLaunchPath([]string{path}); !ok || got != path {
		t.Fatalf("existing file: %q %v", got, ok)
	}
	if _, ok := xshellLaunchPath([]string{"--background", path}); !ok {
		t.Fatal("background flag must be ignored")
	}
	if _, ok := xshellLaunchPath([]string{path, "extra"}); ok {
		t.Fatal("two arguments are not a session file launch")
	}
	if _, ok := xshellLaunchPath([]string{filepath.Join(dir, "missing.xsh")}); ok {
		t.Fatal("a missing file must fall through to the target parser")
	}
	if _, ok := xshellLaunchPath([]string{"alice@host"}); ok {
		t.Fatal("a connection target is not a session file")
	}
}

func TestOpeningAnXshellFileConnectsEphemerallyWithLoopbackPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tunnel.xsh")
	if err := os.WriteFile(path, []byte(bastionXshell), 0o600); err != nil {
		t.Fatal(err)
	}
	state, world, raised := launchState(t)
	state.connectionKeepAlive = 60 * time.Second // would otherwise probe the tunnel
	before := len(state.hosts)
	state.Launch([]string{path})
	state.Wake()
	if *raised != 1 {
		t.Fatalf("raised %d times", *raised)
	}
	if len(state.hosts) != before {
		t.Fatalf("ephemeral open wrote %d hosts into the library", len(state.hosts)-before)
	}
	if len(world.configs) != 1 {
		t.Fatalf("dialed %d connections", len(world.configs))
	}
	config := world.configs[0]
	if config.Host != "127.0.0.1" || config.Port != 63871 || config.User != "root" {
		t.Fatalf("dialed %+v", config)
	}
	if config.KeepAlive.Interval != 0 {
		t.Fatalf("loopback tunnel kept keepalive %+v", config.KeepAlive)
	}
	endpoint := sshconn.Endpoint{Host: "127.0.0.1", Port: 63871, User: "root"}
	auth := config.Authentication[endpoint]
	if auth.Methods != sshconn.AuthKeyboardInteractive|sshconn.AuthPassword || auth.IdentityFiles == nil || len(auth.IdentityFiles) != 0 || auth.Agent {
		t.Fatalf("authentication = %+v", auth)
	}
	want := sshconn.CompatibleAlgorithms()
	got := config.Algorithms[sshconn.EndpointKey("127.0.0.1", 63871)]
	if len(got.Ciphers) != len(want.Ciphers) || len(got.KeyExchanges) != len(want.KeyExchanges) {
		t.Fatalf("algorithms = %+v, want compatible", got)
	}
	if len(state.sessions) != 1 || state.sessions[0].host.Auth != authPassword || state.sessions[0].host.algoMode() != algoCompatible {
		t.Fatalf("session host = %+v", state.sessions[0].host)
	}
}

func TestOpeningABrokenXshellFileReportsTheError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.xsh")
	if err := os.WriteFile(path, []byte("nothing"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, world, _ := launchState(t)
	state.Launch([]string{path})
	state.Wake()
	if len(world.configs) != 0 || !strings.Contains(state.status, "bad.xsh") {
		t.Fatalf("status %q dials %d", state.status, len(world.configs))
	}
}

func TestHostFromBastionSessionAppliesTunnelPolicy(t *testing.T) {
	host := hostFromSessionEntry(sessionimport.Entry{
		Name: "tunnel", Host: "127.0.0.1", Port: 63871, User: "root",
	})
	if host.ID != 0 || host.Auth != authPassword || host.algoMode() != algoCompatible || host.Address != "127.0.0.1" || host.Port != 63871 || !host.tunnel {
		t.Fatalf("host = %+v mode=%v", host, host.algoMode())
	}
	// A host the user saved on 127.0.0.1 with password auth is not a tunnel:
	// its connection sharing and keepalive must stay as configured.
	saved := Host{ID: 7, Address: "127.0.0.1", User: "root", Port: 22, Auth: authPassword}
	if loopbackTunnel(saved) {
		t.Fatal("a saved loopback host was treated as a one-shot tunnel")
	}
	remote := hostFromSessionEntry(sessionimport.Entry{
		Name: "web", Host: "web.example.com", Port: 22, User: "admin",
	})
	if remote.Auth != authAutomatic || remote.Algo != nil {
		t.Fatalf("remote host must keep defaults: %+v", remote)
	}
}

func TestImportingALoopbackXshellSessionWarnsAboutOneShotTunnels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tunnel.xsh")
	if err := os.WriteFile(path, []byte(bastionXshell), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	options.OpenFiles = func(asked app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportXshell)
	if state.modal != modalHostImportResult || len(state.hosts) != 1 {
		t.Fatalf("modal %d hosts %d", state.modal, len(state.hosts))
	}
	warnings := strings.Join(state.hostImportResult.warnings[:state.hostImportResult.visible], "\n")
	if !strings.Contains(warnings, "one-shot") && !strings.Contains(warnings, "127.0.0.1") {
		t.Fatalf("warnings = %q", warnings)
	}
	if state.hosts[0].Address != "127.0.0.1" || state.hosts[0].Port != 63871 {
		t.Fatalf("host = %+v", state.hosts[0])
	}
}
