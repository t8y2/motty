package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
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
	if first.proxies[0].label != "Production → Staging → Router" || first.proxies[0].cells[2] != "prod.example.com → staging.example.com → 192.168.1.1" {
		t.Fatalf("expanded route = %+v", first.proxies[0])
	}

	second := newTestState(t, options)
	if second.activeSection != sectionProxies || len(second.proxies) != 2 {
		t.Fatalf("restored workspace=%d routes=%+v", second.activeSection, second.proxies)
	}
	restored := second.proxies[0]
	target, _ := second.hostByID(restored.TargetID)
	inner, _ := second.hostByID(restored.JumpID)
	outer := second.hosts[2]
	if target.Label != "Production" || inner.Label != "Staging" || restored.label != "Production → Staging → Router" {
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
	if len(state.proxies) != 2 || state.proxies[1].label != "Router → Staging" {
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
	if len(state.proxies) != 1 || state.proxies[0].label != "Production → Staging" {
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

func TestJumpRoutePageAndHostSelectorsAreInteractive(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitProxies)
	canvas, texts := paintedTexts(state)
	if !contains(texts, state.messages.ProxyTitle) || !contains(texts, state.messages.ProxyEmpty) || !hasHit(canvas, hitProxyAdd) {
		t.Fatalf("empty proxy page texts=%v hits=%+v", texts, canvas.Hits())
	}

	state.Click(hitProxyAdd)
	canvas, texts = paintedTexts(state)
	if !contains(texts, state.messages.ProxyDialogTitle) || !hasHit(canvas, hitProxyDialogTarget) || !hasHit(canvas, hitProxyDialogJump) {
		t.Fatalf("proxy dialog texts=%v hits=%+v", texts, canvas.Hits())
	}
	state.Click(hitProxyDialogJump)
	canvas, _ = paintedTexts(state)
	if !hasHit(canvas, hitProxyJumpItemList.At(int(state.hosts[0].ID))) || !hasHit(canvas, hitProxyJumpItemList.At(int(state.hosts[2].ID))) {
		t.Fatalf("jump options = %+v", canvas.Hits())
	}
	state.Click(hitProxyJumpItemList.At(int(state.hosts[2].ID)))
	state.Click(hitProxyDialogSubmit)
	canvas, texts = paintedTexts(state)
	if !contains(texts, "Production") || !contains(texts, "Router") || !hasHit(canvas, hitProxyRowList.At(int(state.proxies[0].ID))) {
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
	state.Click(hitProxyDialogTarget)
	state.Click(hitProxyTargetItemList.At(int(state.hosts[0].ID)))
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
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
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
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
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
