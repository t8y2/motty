package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/text"
	"motty/internal/sshconn"
)

func relayRouteState(t *testing.T) (*State, *world, Options) {
	t.Helper()
	world, options := newWorld(t)
	hosts := append([]savedHost(nil), sampleHosts...)
	hosts[1].savedTransport.LoginCommand = "sudo -i"
	if err := saveState(options.StatePath, savedState{Hosts: hosts}); err != nil {
		t.Fatal(err)
	}
	return newTestState(t, options), world, options
}

func endpointOf(state *State, host Host) sshconn.Endpoint {
	return sshconn.Endpoint{Host: host.Address, Port: host.Port, User: state.login(host)}
}

func addRelayRoute(t *testing.T, state *State, targetID, jumpID uint32) {
	t.Helper()
	state.Click(hitProxies)
	state.Click(hitProxyAdd)
	state.proxyEditor.targetID, state.proxyEditor.jumpID = targetID, jumpID
	state.Click(hitProxyDialogViaShell)
	if !state.proxyEditor.viaShell {
		t.Fatal("the dialog did not turn the shell relay on")
	}
	state.Click(hitProxyDialogSubmit)
	if state.modal != modalNone {
		t.Fatalf("route not added: %q", state.proxyDialogError)
	}
}

func TestARouteRelayedThroughTheJumpHostsShellTypesItsLoginCommandFirst(t *testing.T) {
	state, world, options := relayRouteState(t)
	addRelayRoute(t, state, state.hosts[0].ID, state.hosts[1].ID)
	route := state.proxies[0]
	if !route.ViaShell || route.Relay != defaultRelayCommand || !strings.Contains(route.cells[1], state.messages.ProxyViaShellTag) {
		t.Fatalf("route = %+v", route)
	}

	target, jump := state.hosts[0], state.hosts[1]
	state.Click(hitHosts)
	state.selectedHostID = target.ID
	openHost(state)
	config := world.configs[0]
	want := sshconn.Relay{Prelude: "sudo -i", Command: "nc %h %p"}
	if got := config.Relays[endpointOf(state, target)]; got != want {
		t.Fatalf("relay = %+v, want %+v (jump %s)", got, want, jump.Label)
	}
	if len(config.Relays) != 1 || len(config.ProxyJumps) != 1 {
		t.Fatalf("relays=%+v jumps=%+v", config.Relays, config.ProxyJumps)
	}

	restored := newTestState(t, options)
	if len(restored.proxies) != 1 || !restored.proxies[0].ViaShell || restored.proxies[0].Relay != defaultRelayCommand {
		t.Fatalf("restored routes = %+v", restored.proxies)
	}
}

func TestARouteWithoutTheShellRelayStillUsesAForwardedChannel(t *testing.T) {
	state, _, _ := relayRouteState(t)
	addProxyRoute(t, state, state.hosts[0].ID, state.hosts[1].ID)
	config, _, valid := state.hostCatalog.sshConfigFor(state.hosts[0], state.hosts[0], connectEnv{})
	if !valid || len(config.ProxyJumps) != 1 || len(config.Relays) != 0 {
		t.Fatalf("config jumps=%+v relays=%+v valid=%t", config.ProxyJumps, config.Relays, valid)
	}
}

func TestEachRelayedHopOfAChainTypesTheLoginCommandOfItsOwnJumpHost(t *testing.T) {
	state, _, _ := relayRouteState(t)
	state.hosts[2].LoginCommand = "su - net"
	first, second, third := state.hosts[0], state.hosts[1], state.hosts[2]
	addRelayRoute(t, state, first.ID, second.ID)
	addProxyRoute(t, state, second.ID, third.ID) // the second hop forwards a channel
	state.proxyEditor.viaShell = false
	state.Click(hitProxies)
	config, _, valid := state.hostCatalog.sshConfigFor(first, first, connectEnv{})
	if !valid || len(config.Relays) != 1 {
		t.Fatalf("relays = %+v, valid=%t", config.Relays, valid)
	}
	if got := config.Relays[endpointOf(state, first)]; got.Prelude != "sudo -i" {
		t.Fatalf("first hop relay = %+v", got)
	}

	// Turn the second hop into a relay as well: it types its own jump host's command.
	state.proxies[1].ViaShell = true
	config, _, _ = state.hostCatalog.sshConfigFor(first, first, connectEnv{})
	if got := config.Relays[endpointOf(state, second)]; got.Prelude != "su - net" || len(config.Relays) != 2 {
		t.Fatalf("relays = %+v", config.Relays)
	}
	if relay := config.Relays[endpointOf(state, second)]; sshconn.CheckRelay(relay) != nil {
		t.Fatalf("relay not usable: %v", sshconn.CheckRelay(relay))
	}
}

func TestAStoppedRelayRouteGoesDirect(t *testing.T) {
	state, _, _ := relayRouteState(t)
	addRelayRoute(t, state, state.hosts[0].ID, state.hosts[1].ID)
	state.Click(hitProxyStartStop)
	config, _, _ := state.hostCatalog.sshConfigFor(state.hosts[0], state.hosts[0], connectEnv{})
	if len(config.ProxyJumps) != 0 || len(config.Relays) != 0 {
		t.Fatalf("stopped route config jumps=%+v relays=%+v", config.ProxyJumps, config.Relays)
	}
}

func TestARelayThatIsNotOneLineIsRefusedByTheDialog(t *testing.T) {
	state, _, _ := relayRouteState(t)
	state.Click(hitProxies)
	state.Click(hitProxyAdd)
	state.Click(hitProxyDialogViaShell)
	for _, command := range []string{"", "   ", "nc %h\n%p", "nc %h %q"} {
		state.proxyEditor.relay = text.Editor{}
		state.proxyEditor.relay.Insert(command)
		state.Click(hitProxyDialogSubmit)
		if state.modal != modalNewProxy || state.proxyDialogError != state.messages.ErrProxyRelay || len(state.proxies) != 0 {
			t.Fatalf("command %q: modal=%d error=%q routes=%d", command, state.modal, state.proxyDialogError, len(state.proxies))
		}
	}
	// With the relay off the command is not looked at.
	state.Click(hitProxyDialogViaShell)
	state.Click(hitProxyDialogSubmit)
	if state.modal != modalNone || len(state.proxies) != 1 || state.proxies[0].ViaShell {
		t.Fatalf("modal=%d routes=%+v error=%q", state.modal, state.proxies, state.proxyDialogError)
	}
}

func TestASavedRelayCommandThatCannotHaveComeFromTheDialogRunsTheDefault(t *testing.T) {
	hosts := decodeHostsForTest(t)
	saved := []savedProxy{{
		TargetAddress: hosts[0].Address, TargetUser: hosts[0].User, TargetPort: hosts[0].Port,
		JumpAddress: hosts[1].Address, JumpUser: hosts[1].User, JumpPort: hosts[1].Port,
		ViaShell: true, RelayCommand: "nc %h\n%p",
	}}
	routes := decodeProxies(saved, hosts)
	if len(routes) != 1 || !routes[0].ViaShell || routes[0].Relay != "" || routes[0].relayCommand() != defaultRelayCommand {
		t.Fatalf("routes = %+v", routes)
	}
}

func decodeHostsForTest(t *testing.T) []Host {
	t.Helper()
	hosts, _ := decodeHosts(sampleHosts, nil, 1)
	if len(hosts) < 2 {
		t.Fatalf("hosts = %d", len(hosts))
	}
	return hosts
}

func TestSyncingASSHConfigKeepsTheShellRelayOfARoute(t *testing.T) {
	state, _, _ := relayRouteState(t)
	addRelayRoute(t, state, state.hosts[0].ID, state.hosts[1].ID)
	state.proxies[0].Relay = "ncat %h %p"
	if !state.setProxyChain(state.hosts[0].ID, []uint32{state.hosts[1].ID}) {
		t.Fatal("the chain was refused")
	}
	route := state.proxies[len(state.proxies)-1]
	if !route.ViaShell || route.Relay != "ncat %h %p" {
		t.Fatalf("route = %+v", route)
	}
}

func TestTheRelayDialogShowsWhatIsTypedOnTheJumpHostFirst(t *testing.T) {
	state, _, _ := relayRouteState(t)
	state.Click(hitProxies)
	state.Click(hitProxyAdd)
	state.proxyEditor.targetID, state.proxyEditor.jumpID = state.hosts[0].ID, state.hosts[1].ID
	state.Click(hitProxyDialogViaShell)
	_, texts := paintedTexts(state)
	if !contains(texts, state.messages.ProxyFieldViaShell) || !contains(texts, state.messages.ProxyFieldRelay) {
		t.Fatalf("texts = %v", texts)
	}
	if note := state.proxyRelayNote(); !strings.Contains(note, "sudo -i") {
		t.Fatalf("note = %q", note)
	}
	state.proxyEditor.jumpID = state.hosts[2].ID
	if state.proxyRelayNote() != state.messages.ProxyRelayNoPrelude {
		t.Fatalf("note without a login command = %q", state.proxyRelayNote())
	}
}
