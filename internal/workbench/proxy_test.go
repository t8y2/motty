package workbench

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/sshconn"
)

func addTestProxyRoute(t testing.TB, state *State) proxyRecord {
	t.Helper()
	state.Click(hitProxies)
	state.Click(hitProxyAdd)
	if state.modal != modalNewProxy {
		t.Fatalf("proxy dialog = %d", state.modal)
	}
	state.Click(hitProxyDialogSubmit)
	if state.modal != modalNone || len(state.proxies) != 1 {
		t.Fatalf("proxy submit: modal=%d routes=%+v error=%q", state.modal, state.proxies, state.proxyDialogError)
	}
	return state.proxies[0]
}

func addProxyRoute(t testing.TB, state *State, targetID, jumpID uint32) proxyRecord {
	t.Helper()
	state.Click(hitProxies)
	state.Click(hitProxyAdd)
	if state.modal != modalNewProxy {
		t.Fatalf("proxy dialog = %d", state.modal)
	}
	state.proxyEditor.targetID = targetID
	state.proxyEditor.jumpID = jumpID
	state.Click(hitProxyDialogSubmit)
	if state.modal != modalNone || state.proxyDialogError != "" {
		t.Fatalf("proxy submit: modal=%d routes=%+v error=%q", state.modal, state.proxies, state.proxyDialogError)
	}
	return state.proxies[len(state.proxies)-1]
}

func TestJumpChainPersistsAndReachesTheConnectionOutermostFirst(t *testing.T) {
	world, options := newWorld(t)
	first := newTestState(t, options)
	route := addTestProxyRoute(t, first)
	if route.TargetID != first.hosts[0].ID || route.JumpID != first.hosts[1].ID {
		t.Fatalf("default route = %+v", route)
	}
	addProxyRoute(t, first, first.hosts[1].ID, first.hosts[2].ID)
	if first.proxies[0].label != "Router → Staging → Production" || first.proxies[0].cells[3] != "192.168.1.1 → staging.example.com → prod.example.com" {
		t.Fatalf("expanded route = %+v", first.proxies[0])
	}

	second := newTestState(t, options)
	if second.activeSection != sectionProxies || len(second.proxies) != 2 {
		t.Fatalf("restored workspace=%d routes=%+v", second.activeSection, second.proxies)
	}
	restored := second.proxies[0]
	if restored.Stopped {
		t.Fatal("a route saved before start/stop support must default to running")
	}
	target, _ := second.hostByID(restored.TargetID)
	inner, _ := second.hostByID(restored.JumpID)
	outer := second.hosts[2]
	if target.Label != "Production" || inner.Label != "Staging" || restored.label != "Router → Staging → Production" {
		t.Fatalf("restored route=%+v target=%+v inner=%+v", restored, target, inner)
	}

	second.Click(hitHosts)
	second.selectedHostID = target.ID
	openHost(second)
	if len(world.configs) != 1 {
		t.Fatalf("dials = %d", len(world.configs))
	}
	config := world.configs[0]
	wantJumps := []sshconn.Endpoint{
		{Host: outer.Address, Port: outer.Port, User: outer.User},
		{Host: inner.Address, Port: inner.Port, User: inner.User},
	}
	if len(config.ProxyJumps) != len(wantJumps) || config.ProxyJumps[0] != wantJumps[0] || config.ProxyJumps[1] != wantJumps[1] {
		t.Fatalf("proxy jumps = %+v, want %+v", config.ProxyJumps, wantJumps)
	}
}

func TestJumpRouteStopsPersistsAndRestarts(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	route := addTestProxyRoute(t, state)
	if route.Stopped || route.cells[2] != state.messages.ProxyRunning {
		t.Fatalf("new route = %+v", route)
	}
	target, _ := state.hostByID(route.TargetID)
	config, _, valid := state.hostCatalog.sshConfigFor(target, target, connectEnv{})
	if !valid || len(config.ProxyJumps) != 1 {
		t.Fatalf("running route config = %+v, valid=%t", config.ProxyJumps, valid)
	}
	canvas, texts := paintedTexts(state)
	if !hasHit(canvas, hitProxyRowSwitchList.At(int(route.ID))) || hasHit(canvas, hitProxyStartStop) || !contains(texts, state.messages.ProxyRunning) {
		t.Fatalf("running route page texts=%v hits=%+v", texts, canvas.Hits())
	}

	state.Click(hitProxyRowSwitchList.At(int(route.ID)))
	stopped := state.proxies[0]
	if !stopped.Stopped || stopped.cells[2] != state.messages.ProxyStopped || stopped.label != route.label {
		t.Fatalf("stopped route = %+v", stopped)
	}
	config, _, valid = state.hostCatalog.sshConfigFor(target, target, connectEnv{})
	if !valid || len(config.ProxyJumps) != 0 {
		t.Fatalf("stopped route config = %+v, valid=%t", config.ProxyJumps, valid)
	}

	state = newTestState(t, options)
	if len(state.proxies) != 1 || !state.proxies[0].Stopped || state.proxies[0].cells[2] != state.messages.ProxyStopped {
		t.Fatalf("restored stopped route = %+v", state.proxies)
	}
	target, _ = state.hostByID(state.proxies[0].TargetID)
	config, _, valid = state.hostCatalog.sshConfigFor(target, target, connectEnv{})
	if !valid || len(config.ProxyJumps) != 0 {
		t.Fatalf("restored stopped route config = %+v, valid=%t", config.ProxyJumps, valid)
	}

	state.Click(hitProxyRowSwitchList.At(int(state.proxies[0].ID)))
	if state.proxies[0].Stopped || state.proxies[0].cells[2] != state.messages.ProxyRunning {
		t.Fatalf("restarted route = %+v", state.proxies[0])
	}
	config, _, valid = state.hostCatalog.sshConfigFor(target, target, connectEnv{})
	if !valid || len(config.ProxyJumps) != 1 {
		t.Fatalf("restarted route config = %+v, valid=%t", config.ProxyJumps, valid)
	}
	state = newTestState(t, options)
	if len(state.proxies) != 1 || state.proxies[0].Stopped {
		t.Fatalf("restored running route = %+v", state.proxies)
	}
}

func TestStoppedOuterJumpShortensTheConnectionButKeepsTheConfiguredRoute(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addProxyRoute(t, state, state.hosts[0].ID, state.hosts[1].ID)
	addProxyRoute(t, state, state.hosts[1].ID, state.hosts[2].ID)
	state.Click(hitProxyRowSwitchList.At(int(state.proxies[1].ID)))

	chain, valid := state.proxyChainFor(state.hosts[0])
	if !valid || len(chain) != 1 || chain[0].ID != state.hosts[1].ID {
		t.Fatalf("running chain = %+v, valid=%t", chain, valid)
	}
	if state.proxies[0].label != "Router → Staging → Production" || state.proxies[1].cells[2] != state.messages.ProxyStopped {
		t.Fatalf("configured routes = %+v", state.proxies)
	}

	state.Click(hitProxyRowSwitchList.At(int(state.proxies[1].ID)))
	chain, valid = state.proxyChainFor(state.hosts[0])
	if !valid || len(chain) != 2 {
		t.Fatalf("restarted chain = %+v, valid=%t", chain, valid)
	}
}

func TestStoppedJumpRouteStillRejectsCycles(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addProxyRoute(t, state, state.hosts[0].ID, state.hosts[1].ID)
	state.Click(hitProxyRowSwitchList.At(int(state.proxies[0].ID)))
	state.Click(hitProxyAdd)
	state.proxyEditor.targetID = state.hosts[1].ID
	state.proxyEditor.jumpID = state.hosts[0].ID
	state.Click(hitProxyDialogSubmit)
	if state.proxyDialogError != state.messages.ErrProxyCycle || len(state.proxies) != 1 {
		t.Fatalf("cycle error=%q routes=%+v", state.proxyDialogError, state.proxies)
	}
}

func TestJumpRouteRejectsTwoAndThreeHostCycles(t *testing.T) {
	for _, test := range []struct {
		name  string
		edges [][2]int
	}{
		{name: "two hosts", edges: [][2]int{{0, 1}, {1, 0}}},
		{name: "three hosts", edges: [][2]int{{0, 1}, {1, 2}, {2, 0}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			for index, edge := range test.edges {
				state.Click(hitProxies)
				state.Click(hitProxyAdd)
				state.proxyEditor.targetID = state.hosts[edge[0]].ID
				state.proxyEditor.jumpID = state.hosts[edge[1]].ID
				state.Click(hitProxyDialogSubmit)
				if index == len(test.edges)-1 {
					if state.proxyDialogError != state.messages.ErrProxyCycle || len(state.proxies) != len(test.edges)-1 {
						t.Fatalf("cycle error=%q routes=%+v", state.proxyDialogError, state.proxies)
					}
					continue
				}
				if state.modal != modalNone {
					t.Fatalf("valid edge %d failed: %q", index, state.proxyDialogError)
				}
			}
		})
	}
}

func TestIndependentJumpRoutesCanShareAJumpHost(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addProxyRoute(t, state, state.hosts[0].ID, state.hosts[1].ID)
	addProxyRoute(t, state, state.hosts[2].ID, state.hosts[1].ID)
	if len(state.proxies) != 2 || state.proxies[1].label != "Staging → Router" {
		t.Fatalf("independent routes = %+v", state.proxies)
	}
}

func TestRestoreJumpRoutesSkipsMissingAndCycleClosingEdges(t *testing.T) {
	_, options := newWorld(t)
	if err := saveState(options.StatePath, savedState{
		Hosts: sampleHosts,
		Proxies: []savedProxy{
			{TargetAddress: sampleHosts[0].Address, TargetUser: sampleHosts[0].User, TargetPort: sampleHosts[0].Port, JumpAddress: sampleHosts[1].Address, JumpUser: sampleHosts[1].User, JumpPort: sampleHosts[1].Port},
			{TargetAddress: sampleHosts[1].Address, TargetUser: sampleHosts[1].User, TargetPort: sampleHosts[1].Port, JumpAddress: sampleHosts[0].Address, JumpUser: sampleHosts[0].User, JumpPort: sampleHosts[0].Port},
			{TargetAddress: sampleHosts[2].Address, TargetUser: sampleHosts[2].User, TargetPort: sampleHosts[2].Port, JumpAddress: "missing.example.com", JumpUser: "nobody", JumpPort: 22},
		},
	}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if len(state.proxies) != 1 || state.proxies[0].label != "Staging → Production" {
		t.Fatalf("restored routes = %+v", state.proxies)
	}
}

func TestJumpRouteRejectsSelfAndDuplicateTargets(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitProxies)
	state.Click(hitProxyAdd)
	state.proxyEditor.jumpID = state.proxyEditor.targetID
	state.Click(hitProxyDialogSubmit)
	if state.proxyDialogError != state.messages.ErrProxySameHost || len(state.proxies) != 0 {
		t.Fatalf("self route error=%q routes=%+v", state.proxyDialogError, state.proxies)
	}
	state.proxyEditor.jumpID = state.hosts[1].ID
	state.Click(hitProxyDialogSubmit)
	if len(state.proxies) != 1 {
		t.Fatalf("valid route = %+v", state.proxies)
	}

	state.Click(hitProxyAdd)
	if state.modal != modalNewProxy {
		t.Fatal("a second route dialog did not open")
	}
	state.proxyEditor.targetID = state.proxies[0].TargetID
	state.proxyEditor.jumpID = state.hosts[2].ID
	state.Click(hitProxyDialogSubmit)
	if state.proxyDialogError != state.messages.ErrProxyTargetTaken || len(state.proxies) != 1 {
		t.Fatalf("duplicate target error=%q routes=%+v", state.proxyDialogError, state.proxies)
	}
}

func TestProxyEditModifiesAndPersists(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	target := state.hosts[0]
	jump1 := state.hosts[1]
	jump2 := state.hosts[2]
	addProxyRoute(t, state, target.ID, jump1.ID)

	state.selectedProxyID = state.proxies[0].ID
	state.Click(hitProxyEdit)
	if state.modal != modalNewProxy || !state.proxyEditor.editing || state.proxyEditor.routeID != state.proxies[0].ID {
		t.Fatalf("proxy edit modal=%d editing=%v routeID=%d", state.modal, state.proxyEditor.editing, state.proxyEditor.routeID)
	}
	if state.proxyEditor.targetID != target.ID || state.proxyEditor.jumpID != jump1.ID {
		t.Fatalf("proxy editor prefill: target=%d jump=%d", state.proxyEditor.targetID, state.proxyEditor.jumpID)
	}

	// Keeping own target should be valid
	if err := state.proxyEditor.validate(state); err != "" {
		t.Fatalf("validating unchanged proxy route failed: %q", err)
	}

	// Change jump host to jump2, enable viaShell
	state.proxyEditor.jumpID = jump2.ID
	state.Click(hitProxyDialogViaShell)
	if !state.proxyEditor.viaShell {
		t.Fatal("viaShell should be enabled")
	}
	state.proxyEditor.relay = text.Editor{}
	state.proxyEditor.relay.Insert("ssh %r@%h")

	state.Click(hitProxyDialogSubmit)
	if state.modal != modalNone || state.proxyDialogError != "" {
		t.Fatalf("submit edit proxy: modal=%d error=%q", state.modal, state.proxyDialogError)
	}
	if len(state.proxies) != 1 {
		t.Fatalf("proxies count = %d, want 1", len(state.proxies))
	}
	edited := state.proxies[0]
	if edited.TargetID != target.ID || edited.JumpID != jump2.ID || !edited.ViaShell || edited.Relay != "ssh %r@%h" {
		t.Fatalf("edited route = %+v", edited)
	}
	if !strings.HasPrefix(state.status, state.messages.ProxyUpdated) {
		t.Fatalf("status = %q, want prefix %q", state.status, state.messages.ProxyUpdated)
	}

	// Verify persistence
	restarted := newTestState(t, options)
	if len(restarted.proxies) != 1 {
		t.Fatalf("restored proxies count = %d, want 1", len(restarted.proxies))
	}
	restored := restarted.proxies[0]
	if restored.TargetID != target.ID || restored.JumpID != jump2.ID || !restored.ViaShell || restored.Relay != "ssh %r@%h" {
		t.Fatalf("restored route = %+v", restored)
	}
}

// A route keeps the number it was saved with, so a window that reloads the
// catalog does not re-point a dialog that was opened on it at another route.
func TestAJumpRouteKeepsItsSavedNumberAcrossAReload(t *testing.T) {
	hosts := decodeHostsForTest(t)
	target, jump := hosts[0], hosts[1]
	saved := func(id uint32, target Host, jump Host) savedProxy {
		return savedProxy{
			ID: id, TargetAddress: target.Address, TargetUser: target.User, TargetPort: target.Port,
			JumpAddress: jump.Address, JumpUser: jump.User, JumpPort: jump.Port,
		}
	}
	routes := decodeProxies([]savedProxy{saved(4, target, jump), saved(2, hosts[2], hosts[1])}, hosts)
	if len(routes) != 2 {
		t.Fatalf("routes = %+v", routes)
	}
	if got := [2]uint32{routes[0].ID, routes[1].ID}; got != [2]uint32{4, 2} {
		t.Fatalf("restored numbers = %v, want the saved 4 and 2", got)
	}

	// A record older than the number, and a second record claiming one already
	// taken, both take the lowest free number instead of colliding.
	routes = decodeProxies([]savedProxy{saved(4, target, jump), saved(0, hosts[2], hosts[1])}, hosts)
	if got := [2]uint32{routes[0].ID, routes[1].ID}; got != [2]uint32{4, 1} {
		t.Fatalf("numbers after a legacy record = %v, want 4 and the free 1", got)
	}
	routes = decodeProxies([]savedProxy{saved(4, target, jump), saved(4, hosts[2], hosts[1])}, hosts)
	if routes[0].ID == routes[1].ID {
		t.Fatalf("two routes answer to the same number: %+v", routes)
	}
}

// Saving an edit whose route is gone reports it instead of quietly adding a
// second copy under a new number.
func TestEditingAJumpRouteThatIsGoneReportsItInsteadOfAddingACopy(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addProxyRoute(t, state, state.hosts[0].ID, state.hosts[1].ID)

	state.selectedProxyID = state.proxies[0].ID
	state.Click(hitProxyEdit)
	if state.modal != modalNewProxy || !state.proxyEditor.editing {
		t.Fatalf("edit dialog = %d editing=%v", state.modal, state.proxyEditor.editing)
	}
	// The route goes away under the open dialog, as it does when another window
	// removes it.
	state.proxies = nil
	state.Click(hitProxyDialogSubmit)
	if state.proxyDialogError != state.messages.ErrProxyEditMissing {
		t.Fatalf("error = %q, want %q", state.proxyDialogError, state.messages.ErrProxyEditMissing)
	}
	if len(state.proxies) != 0 {
		t.Fatalf("routes = %+v, want no copy appended", state.proxies)
	}
	if state.modal != modalNewProxy {
		t.Fatalf("the dialog closed on a failed save: modal = %d", state.modal)
	}
}

func TestJumpRoutePageAndHostSelectorsAreInteractive(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitProxies)
	canvas, texts := paintedTexts(state)
	if !contains(texts, state.messages.ProxyTitle) || !contains(texts, state.messages.ProxyEmpty) || !hasHit(canvas, hitProxyAdd) {
		t.Fatalf("empty proxy page texts=%v hits=%+v", texts, canvas.Hits())
	}

	state.Click(hitProxyAdd)
	canvas, texts = paintedTexts(state)
	if !contains(texts, state.messages.ProxyDialogTitle) || !hasLabel(canvas, state.messages.ProxyFieldTarget, gfx.RoleCombobox) || !hasLabel(canvas, state.messages.ProxyFieldJump, gfx.RoleCombobox) {
		t.Fatalf("proxy dialog texts=%v hits=%+v", texts, canvas.Hits())
	}
	clickPageLabel(t, state, gfx.RoleCombobox, state.messages.ProxyFieldJump)
	canvas, _ = paintedTexts(state)
	for _, host := range state.hosts {
		if !hasLabel(canvas, host.Label, gfx.RoleOption) {
			t.Fatalf("jump options = %+v, missing %q", canvas.Hits(), host.Label)
		}
	}
	clickPageLabel(t, state, gfx.RoleOption, "Router")
	state.Click(hitProxyDialogSubmit)
	canvas, texts = paintedTexts(state)
	if !contains(texts, "Production") || !contains(texts, "Router") || !contains(texts, state.messages.ProxyRunning) ||
		!hasHit(canvas, hitProxyRowList.At(0)) || !hasHit(canvas, hitProxyRowSwitchList.At(int(state.proxies[0].ID))) {
		t.Fatalf("route page texts=%v hits=%+v", texts, canvas.Hits())
	}
}

func TestJumpRouteSelectionsSwapInsteadOfDeadlocking(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.selectedHostID = state.hosts[1].ID
	state.Click(hitProxies)
	state.Click(hitProxyAdd)
	if state.proxyEditor.targetID != state.hosts[1].ID || state.proxyEditor.jumpID != state.hosts[0].ID {
		t.Fatalf("defaults = %+v", state.proxyEditor)
	}
	clickPageLabel(t, state, gfx.RoleCombobox, state.messages.ProxyFieldTarget)
	clickPageLabel(t, state, gfx.RoleOption, "Production")
	if state.proxyEditor.targetID != state.hosts[0].ID || state.proxyEditor.jumpID != state.hosts[1].ID {
		t.Fatalf("swapped selection = %+v", state.proxyEditor)
	}
	state.Click(hitProxyDialogSubmit)
	if len(state.proxies) != 1 || state.proxies[0].TargetID != state.hosts[0].ID || state.proxies[0].JumpID != state.hosts[1].ID {
		t.Fatalf("route after swap = %+v", state.proxies)
	}
}

func TestPromptsIdentifyJumpAndTargetSeparately(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	addTestProxyRoute(t, state)
	state.Click(hitHosts)
	openHost(state)
	fake := world.conns[0]
	jump := state.hosts[1]
	target := state.hosts[0]

	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseHostKeyPrompt
		status.Endpoint = sshconn.Endpoint{Host: jump.Address, Port: jump.Port, User: jump.User}
		status.EndpointRole = sshconn.EndpointProxyJump
		status.HostKey = sshconn.HostKeyInfo{Address: jump.target, Type: "ssh-ed25519", Fingerprint: "jump-key"}
	})
	state.Wake()
	if state.modal != modalHostKey || !strings.Contains(state.hostKeyDescription, jump.target) {
		t.Fatalf("jump host-key prompt: modal=%d description=%q", state.modal, state.hostKeyDescription)
	}
	state.Click(hitHostKeyOnce)
	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhasePasswordPrompt
		status.Attempt = 1
		status.Endpoint = sshconn.Endpoint{Host: jump.Address, Port: jump.Port, User: jump.User}
		status.EndpointRole = sshconn.EndpointProxyJump
	})
	state.Wake()
	if state.modal != modalPassword || !strings.Contains(state.passwordDescription, jump.Address) {
		t.Fatalf("jump password prompt: modal=%d description=%q", state.modal, state.passwordDescription)
	}
	state.Click(hitPasswordCancel)

	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseHostKeyPrompt
		status.Attempt = 0
		status.Endpoint = sshconn.Endpoint{Host: target.Address, Port: target.Port, User: target.User}
		status.EndpointRole = sshconn.EndpointTarget
		status.HostKey = sshconn.HostKeyInfo{Address: target.target, Type: "ssh-ed25519", Fingerprint: "target-key"}
	})
	state.Wake()
	if state.modal != modalHostKey || !strings.Contains(state.hostKeyDescription, target.target) {
		t.Fatalf("target host-key prompt was hidden by jump prompt: modal=%d description=%q", state.modal, state.hostKeyDescription)
	}
}

func TestJumpConnectionProgressDescribesTheCurrentEndpoint(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   sshconn.Status
		stage    int
		step     stepState
		detail   int
		contains string
	}{
		{
			name: "dial jump", status: sshconn.Status{Phase: sshconn.PhaseConnecting, EndpointRole: sshconn.EndpointProxyJump},
			stage: stageConnect, step: stepActive, detail: stageConnect, contains: "Waiting for jump host",
		},
		{
			name: "verify jump", status: sshconn.Status{Phase: sshconn.PhaseHostKeyPrompt, EndpointRole: sshconn.EndpointProxyJump},
			stage: stageConnect, step: stepWaiting, detail: stageConnect, contains: "Check the jump host key",
		},
		{
			name: "unlock jump key", status: sshconn.Status{Phase: sshconn.PhaseKeyPassphrasePrompt, EndpointRole: sshconn.EndpointProxyJump},
			stage: stageConnect, step: stepWaiting, detail: stageConnect, contains: "Signing in to jump host",
		},
		{
			name: "unlock target key", status: sshconn.Status{Phase: sshconn.PhaseKeyPassphrasePrompt, EndpointRole: sshconn.EndpointTarget, IdentityPath: "/tmp/id_target"},
			stage: stageLogin, step: stepWaiting, detail: stageLogin, contains: "Waiting to unlock id_target",
		},
		{
			name: "verify target", status: sshconn.Status{Phase: sshconn.PhaseHostKeyPrompt, EndpointRole: sshconn.EndpointTarget},
			stage: stageHostKey, step: stepWaiting, detail: stageConnect, contains: "Reached the target through",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			world, options := newWorld(t)
			state := newTestState(t, options)
			addTestProxyRoute(t, state)
			state.Click(hitHosts)
			openHost(state)
			jump := state.hosts[1]
			target := state.hosts[0]
			endpoint := sshconn.Endpoint{Host: target.Address, Port: target.Port, User: target.User}
			if test.status.EndpointRole == sshconn.EndpointProxyJump {
				endpoint = sshconn.Endpoint{Host: jump.Address, Port: jump.Port, User: jump.User}
			}
			test.status.Endpoint = endpoint
			world.conns[0].set(func(status *sshconn.Status) { *status = test.status })
			state.Wake()
			session := &state.sessions[0]
			if session.stage != test.stage || session.stepOf(test.stage) != test.step || !strings.Contains(session.details[test.detail], test.contains) {
				t.Fatalf("stage=%d step=%d detail=%q", session.stage, session.stepOf(test.stage), session.details[test.detail])
			}
		})
	}
}

func TestMultiHopProgressShowsCurrentJumpAndCompleteRoute(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	addProxyRoute(t, state, state.hosts[0].ID, state.hosts[1].ID)
	addProxyRoute(t, state, state.hosts[1].ID, state.hosts[2].ID)
	state.Click(hitHosts)
	state.selectedHostID = state.hosts[0].ID
	openHost(state)

	inner := state.hosts[1]
	outer := state.hosts[2]
	world.conns[0].set(func(status *sshconn.Status) {
		*status = sshconn.Status{
			Phase:        sshconn.PhaseConnecting,
			Endpoint:     sshconn.Endpoint{Host: inner.Address, Port: inner.Port, User: inner.User},
			EndpointRole: sshconn.EndpointProxyJump,
		}
	})
	state.Wake()
	session := &state.sessions[0]
	if !strings.Contains(session.details[stageConnect], inner.Address) || !strings.Contains(session.details[stageConnect], outer.Address) {
		t.Fatalf("jump detail = %q", session.details[stageConnect])
	}
	if !strings.Contains(session.jumpPath, outer.Address+":22 → "+inner.User+"@"+inner.Address+":22") {
		t.Fatalf("jump path = %q", session.jumpPath)
	}
}

func TestSkippedIdentitiesAreReportedPerEndpointAndIncrementally(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	addTestProxyRoute(t, state)
	state.Click(hitHosts)
	openHost(state)
	fake := world.conns[0]
	jump := state.hosts[1]
	target := state.hosts[0]

	updates := []sshconn.Status{
		{Phase: sshconn.PhaseAuthenticating, Endpoint: sshconn.Endpoint{Host: jump.Address, Port: jump.Port, User: jump.User}, EndpointRole: sshconn.EndpointProxyJump, Skipped: []string{"jump-a"}},
		{Phase: sshconn.PhaseAuthenticating, Endpoint: sshconn.Endpoint{Host: jump.Address, Port: jump.Port, User: jump.User}, EndpointRole: sshconn.EndpointProxyJump, Skipped: []string{"jump-a", "jump-b"}},
		{Phase: sshconn.PhaseAuthenticating, Endpoint: sshconn.Endpoint{Host: target.Address, Port: target.Port, User: target.User}, EndpointRole: sshconn.EndpointTarget, Skipped: []string{"target-a"}},
	}
	for _, update := range updates {
		fake.set(func(status *sshconn.Status) {
			version := status.Version
			*status = update
			status.Version = version
		})
		state.Wake()
	}
	screen := state.sessions[0].term.screen.Text()
	for _, path := range []string{"jump-a", "jump-b", "target-a"} {
		if count := strings.Count(screen, path); count != 1 {
			t.Fatalf("%q appears %d times in %q", path, count, screen)
		}
	}
}

func TestProxyWorkspacePaintDoesNotAllocateOnceWarm(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addTestProxyRoute(t, state)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	if allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}); allocations != 0 {
		t.Fatalf("proxy workspace paint allocations = %v", allocations)
	}
}

func BenchmarkPaintProxyWorkspace(b *testing.B) {
	state := newTestState(b, testOptions(b))
	addTestProxyRoute(b, state)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	b.ReportAllocs()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

func BenchmarkProxyChainFor(b *testing.B) {
	state := newTestState(b, testOptions(b))
	addProxyRoute(b, state, state.hosts[0].ID, state.hosts[1].ID)
	addProxyRoute(b, state, state.hosts[1].ID, state.hosts[2].ID)
	target := state.hosts[0]
	b.ReportAllocs()
	for b.Loop() {
		chain, ok := state.proxyChainFor(target)
		if !ok || len(chain) != 2 {
			b.Fatalf("proxy chain = %+v, ok=%t", chain, ok)
		}
	}
}

// Syncing the SSH config rebuilds a target's route, which must not start a route
// the user stopped: the file does not know about stopping.
func TestSyncingAStoppedRouteKeepsItStopped(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addProxyRoute(t, state, state.hosts[0].ID, state.hosts[1].ID)
	state.Click(hitProxyRowSwitchList.At(int(state.proxies[0].ID)))

	if !state.setProxyChain(state.hosts[0].ID, []uint32{state.hosts[1].ID}) {
		t.Fatal("the same chain was refused")
	}
	if len(state.proxies) != 1 || !state.proxies[0].Stopped {
		t.Fatalf("routes after syncing the same chain = %+v, want it still stopped", state.proxies)
	}
	if !state.setProxyChain(state.hosts[0].ID, []uint32{state.hosts[2].ID}) {
		t.Fatal("another jump host was refused")
	}
	if len(state.proxies) != 1 || state.proxies[0].Stopped || state.proxies[0].JumpID != state.hosts[2].ID {
		t.Fatalf("routes after syncing another jump = %+v, want a new, running route", state.proxies)
	}
}

func TestDesktopJumpRoutePersistsAcrossRestart(t *testing.T) {
	options := testOptions(t)
	first := newTestState(t, options)
	jump := first.hosts[0]
	rdpHost := Host{
		ID: first.nextHostID, CatalogID: "host_win_rdp", Label: "Windows Desktop",
		Address: "10.0.0.50", User: "Administrator", Port: 3389,
		Protocol: protocolRDP, System: sshconn.SystemWindows,
	}
	first.nextHostID++
	vncHost := Host{
		ID: first.nextHostID, CatalogID: "host_vnc", Label: "Lab VNC",
		Address: "10.0.0.51", Port: 5900, Protocol: protocolVNC,
	}
	first.nextHostID++
	first.hosts = append(first.hosts, rdpHost, vncHost)
	first.save()

	// Choosing a remote desktop target through the route dialog's combobox must commit it.
	first.Click(hitProxies)
	first.Click(hitProxyAdd)
	if first.modal != modalNewProxy {
		t.Fatalf("proxy dialog = %d", first.modal)
	}
	clickPageLabel(t, first, gfx.RoleCombobox, first.messages.ProxyFieldTarget)
	clickPageLabel(t, first, gfx.RoleOption, rdpHost.Label)
	if first.proxyEditor.targetID != rdpHost.ID {
		t.Fatalf("target choice after selecting RDP host = %d, want %d", first.proxyEditor.targetID, rdpHost.ID)
	}
	first.applyProxyJumpChoice(jump.ID)
	first.Click(hitProxyDialogSubmit)
	if first.modal != modalNone || first.proxyDialogError != "" || len(first.proxies) != 1 {
		t.Fatalf("rdp route submit: modal=%d error=%q routes=%+v", first.modal, first.proxyDialogError, first.proxies)
	}

	addProxyRoute(t, first, vncHost.ID, jump.ID)
	if len(first.proxies) != 2 {
		t.Fatalf("routes before restart = %+v, want 2", first.proxies)
	}

	// Reopening must keep both desktop routes visible in the jump routes list and host form.
	second := newTestState(t, options)
	if len(second.proxies) != 2 {
		t.Fatalf("restored routes = %+v, want 2 desktop routes", second.proxies)
	}
	second.Click(hitProxies)
	if got := second.visibleProxyCount(); got != 2 {
		t.Fatalf("visible routes after restart = %d, want 2", got)
	}
	restoredRDP, ok := second.hostByCatalogID(rdpHost.CatalogID)
	if !ok {
		t.Fatal("restored RDP host missing")
	}
	restoredJump, ok := second.hostByCatalogID(jump.CatalogID)
	if !ok {
		t.Fatal("restored jump host missing")
	}
	route, ok := second.proxyRouteForTarget(restoredRDP.ID)
	if !ok {
		t.Fatalf("restored RDP route missing from %+v", second.proxies)
	}
	if route.label != jump.Label+" → "+rdpHost.Label || !strings.Contains(route.cells[3], "rdp://") {
		t.Fatalf("restored RDP route = %+v, want label %q and rdp:// target", route, jump.Label+" → "+rdpHost.Label)
	}

	second.openEditHostID(restoredRDP.ID)
	wantSummary := fmt.Sprintf(second.messages.Tools.HostJumpDescriptionFmt, restoredJump.Label)
	if got := second.hostJumpDescription(); got != wantSummary {
		t.Fatalf("host form jump summary = %q, want %q", got, wantSummary)
	}
	second.Click(hitHostJumpConfigure)
	if second.modal != modalNewProxy || !second.proxyEditor.editing || second.proxyEditor.targetID != restoredRDP.ID {
		t.Fatalf("editing restored RDP route: modal=%d editing=%v target=%d", second.modal, second.proxyEditor.editing, second.proxyEditor.targetID)
	}
	second.Click(hitProxyDialogSubmit)
	if second.modal != modalNewHost || second.proxyDialogError != "" || len(second.proxies) != 2 {
		t.Fatalf("saving restored RDP route: modal=%d error=%q routes=%d", second.modal, second.proxyDialogError, len(second.proxies))
	}

	// A desktop host must still be refused as the jump host when decoding.
	bad := decodeProxies([]savedProxy{{
		ID: 9, TargetID: jump.CatalogID, JumpID: rdpHost.CatalogID,
	}}, second.hosts)
	if len(bad) != 0 {
		t.Fatalf("desktop jump host must not decode: %+v", bad)
	}

	// A destination-only record without catalog IDs still resolves a desktop target
	// (including when its port is omitted and falls back to the protocol default).
	for _, port := range []uint16{rdpHost.Port, 0} {
		legacy := decodeProxies([]savedProxy{{
			TargetAddress: rdpHost.Address, TargetUser: rdpHost.User, TargetPort: port,
			JumpAddress: jump.Address, JumpUser: jump.User, JumpPort: jump.Port,
		}}, second.hosts)
		if len(legacy) != 1 || legacy[0].TargetID != restoredRDP.ID || legacy[0].JumpID != restoredJump.ID {
			t.Fatalf("legacy desktop route (port=%d) = %+v, want target %d jump %d", port, legacy, restoredRDP.ID, restoredJump.ID)
		}
	}
}

func TestProxyChoiceSwapKeepsBothEndsValid(t *testing.T) {
	state := newTestState(t, testOptions(t))
	sshA, sshB, sshC := state.hosts[0], state.hosts[1], state.hosts[2]
	rdpHost := Host{
		ID: state.nextHostID, CatalogID: "host_win_swap", Label: "Windows Swap",
		Address: "10.0.0.60", User: "Administrator", Port: 3389,
		Protocol: protocolRDP, System: sshconn.SystemWindows,
	}
	state.nextHostID++
	state.hosts = append(state.hosts, rdpHost)

	// Switching target from a desktop host to the current jump clears the jump
	// instead of placing the desktop into the jump slot, and the jump picker refuses desktops.
	state.openProxyDialogForHost(rdpHost.ID, false)
	state.applyProxyJumpChoice(sshA.ID)
	state.applyProxyTargetChoice(sshA.ID)
	if state.proxyEditor.targetID != sshA.ID {
		t.Fatalf("targetID = %d, want %d", state.proxyEditor.targetID, sshA.ID)
	}
	if state.proxyEditor.jumpID != 0 {
		t.Fatalf("jumpID after swapping from RDP target = %d, want 0 (cleared)", state.proxyEditor.jumpID)
	}
	state.applyProxyJumpChoice(rdpHost.ID)
	if state.proxyEditor.jumpID != 0 {
		t.Fatalf("jump picker accepted desktop host: jumpID = %d", state.proxyEditor.jumpID)
	}
	state.closeProxyDialog()

	// Switching jump to the current target clears the target when the old jump
	// already has a route, instead of moving an already-routed host into the target slot.
	addProxyRoute(t, state, sshA.ID, sshB.ID)
	state.openProxyDialogForHost(sshC.ID, false)
	state.applyProxyJumpChoice(sshA.ID)
	state.applyProxyJumpChoice(sshC.ID)
	if state.proxyEditor.jumpID != sshC.ID {
		t.Fatalf("jumpID = %d, want %d", state.proxyEditor.jumpID, sshC.ID)
	}
	if state.proxyEditor.targetID != 0 {
		t.Fatalf("targetID after swapping from already-routed jump = %d, want 0 (cleared)", state.proxyEditor.targetID)
	}
}

// A route can be set up from nothing: the dialog creates both hosts in place
// and each lands in the end it was created for.
func TestProxyDialogCreatesItsHostsInPlace(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.hosts = nil
	state.Click(hitProxies)
	state.Click(hitProxyAdd)
	if state.modal != modalNewProxy {
		t.Fatalf("empty routes page did not open the dialog: modal=%d", state.modal)
	}
	create := func(button gfx.ID, label, address string) {
		t.Helper()
		state.Click(button)
		if state.modal != modalNewHost {
			t.Fatalf("new host form not open: modal=%d", state.modal)
		}
		state.editor.label.Insert(label)
		state.editor.address.Insert(address)
		state.Click(hitDialogSubmit)
		if state.modal != modalNewProxy || state.dialogError != "" {
			t.Fatalf("did not return to the route dialog: modal=%d error=%q", state.modal, state.dialogError)
		}
	}
	create(hitProxyDialogNewJump, "Bastion", "bastion.example.com")
	create(hitProxyDialogNewTarget, "Inner", "inner.example.com")
	editor := &state.proxyEditor
	jump, _ := state.hostByID(editor.jumpID)
	target, _ := state.hostByID(editor.targetID)
	if jump.Label != "Bastion" || target.Label != "Inner" || editor.jumpChoice != editor.jumpID || editor.targetChoice != editor.targetID {
		t.Fatalf("adopted jump=%q target=%q", jump.Label, target.Label)
	}
	state.Click(hitProxyDialogSubmit)
	if state.modal != modalNone || len(state.proxies) != 1 {
		t.Fatalf("route not saved: modal=%d routes=%+v error=%q", state.modal, state.proxies, state.proxyDialogError)
	}
}

// Cancelling the host form leaves the route dialog and what it held as it was.
func TestProxyDialogHostFormCancelReturnsToDialog(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.Click(hitProxies)
	state.Click(hitProxyAdd)
	target, jump := state.proxyEditor.targetID, state.proxyEditor.jumpID
	hosts := len(state.hosts)
	state.Click(hitProxyDialogNewJump)
	state.Click(hitDialogCancel)
	if state.modal != modalNewProxy || len(state.hosts) != hosts || state.proxyEditor.targetID != target || state.proxyEditor.jumpID != jump {
		t.Fatalf("modal=%d hosts=%d editor=%+v", state.modal, len(state.hosts), state.proxyEditor)
	}
}
