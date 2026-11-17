package workbench

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"motty/internal/keyidentity"
	"motty/internal/sshconfig"
	"motty/internal/sshconn"
)

func TestSSHConfigImportRetainsConnectionOptionsAcrossRestart(t *testing.T) {
	key := writeWorkbenchIdentity(t, "configured", "")
	other := writeWorkbenchIdentity(t, "unrelated", "")
	path := writeWorkbenchSSHConfig(t, fmt.Sprintf(`
Host configured
    HostName target.example
    User deploy
    ServerAliveInterval 1m
    ServerAliveCountMax 4
    IdentitiesOnly yes
    IdentityFile %q
    ProxyJump disabled
Host disabled
    HostName jump.example
    User gateway
    ServerAliveInterval 0
    ServerAliveCountMax 0
    IdentitiesOnly no
    IdentityFile %q
`, key, other))
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSSHConfig)
	if state.hostImportResult.visible != 0 {
		t.Fatalf("unexpected warnings: %+v", state.hostImportResult)
	}
	restarted := newTestState(t, options)
	host := mustHostLabel(t, restarted, "configured")
	config, _, valid := restarted.hostCatalog.sshConfigFor(host, host, connectEnv{KnownHostsPath: options.KnownHostsPath})
	if !valid || config.KeepAlive.Interval != time.Minute || config.KeepAlive.CountMax == nil || *config.KeepAlive.CountMax != 4 {
		t.Fatalf("restored keepalive = %+v, valid=%v", config.KeepAlive, valid)
	}
	endpoint := sshconn.Endpoint{Host: host.Address, Port: host.Port, User: host.User}
	auth := config.Authentication[endpoint]
	if !auth.IdentitiesOnly || !auth.Agent || !slices.Equal(auth.IdentityFiles, []string{key}) || len(auth.AdditionalIdentityFiles) != 0 {
		t.Fatalf("restored authentication = %+v", auth)
	}
	if len(config.ProxyJumps) != 1 || config.EndpointKeepAlive[config.ProxyJumps[0]].Interval != 0 {
		t.Fatalf("disabled jump keepalive = %+v", config.EndpointKeepAlive)
	}
	restarted.selectedHostID = host.ID
	restarted.openEditHost()
	restarted.Click(hitDialogSubmit)
	if !mustHostLabel(t, restarted, "configured").SSHOptions.Equal(host.SSHOptions) {
		t.Fatal("saving the host editor discarded imported SSH options")
	}
}

func TestSSHConfigImportAllowsPublicIdentityForAgentAuthentication(t *testing.T) {
	path := writeWorkbenchIdentity(t, "agent-identity", "")
	info, err := keyidentity.Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".pub", []byte(info.PublicKey), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	config := writeWorkbenchSSHConfig(t, fmt.Sprintf("Host agent\n    HostName agent.example\n    IdentitiesOnly yes\n    IdentityFile %q\n", path))
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{config}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSSHConfig)
	if state.hostImportResult.visible != 0 || len(state.hosts) != 1 || len(state.identities) != 0 {
		t.Fatalf("public identity import: hosts=%d keys=%d result=%+v", len(state.hosts), len(state.identities), state.hostImportResult)
	}
	if host := state.hosts[0]; host.SSHOptions == nil || !slices.Equal(host.SSHOptions.IdentityFiles, []string{path}) {
		t.Fatalf("agent identity was not retained: %+v", host)
	}
}

func TestChangedSSHIdentityPolicyDoesNotReuseAuthenticatedTransport(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	only := true
	state.hosts[0].SSHOptions = &sshconfig.ConnectionOptions{IdentitiesOnly: &only, IdentityFiles: []string{"configured-key"}}
	state.sessions[0].host = state.hosts[0]
	state.Click(hitHosts)
	openHost(state)
	if len(world.configs) != 2 {
		t.Fatal("changed identity restrictions reused the previous transport")
	}
}

func TestSSHSyncRefreshesAndRemovesConnectionOptions(t *testing.T) {
	state, path := newSyncState(t, "Host server\n    HostName server.example\n    ServerAliveInterval 60\n    ServerAliveCountMax 4\n    IdentitiesOnly yes\n")
	enableSync(t, state)
	host := mustHostLabel(t, state, "server")
	if host.SSHOptions == nil || *host.SSHOptions.ServerAliveInterval != 60 {
		t.Fatalf("synced options = %+v", host.SSHOptions)
	}
	contents := readSyncFile(t, path)
	writeSyncFile(t, path, strings.Replace(contents, "ServerAliveInterval 60", "ServerAliveInterval 0", 1))
	state.syncSSHConfig(true)
	host = mustHostLabel(t, state, "server")
	if host.SSHOptions == nil || *host.SSHOptions.ServerAliveInterval != 0 {
		t.Fatalf("changed options = %+v", host.SSHOptions)
	}
	writeSyncFile(t, path, "Host server\n    HostName server.example\n")
	state.syncSSHConfig(true)
	if host := mustHostLabel(t, state, "server"); host.SSHOptions != nil {
		t.Fatalf("removed options retained: %+v", host.SSHOptions)
	}
}
