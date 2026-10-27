package workbench

import (
	"slices"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

func TestHostDialogSelectsAPrivateKey(t *testing.T) {
	state := newTestState(t, testOptions(t))
	identityPath := writeWorkbenchIdentity(t, "id_dialog", "")
	state.restoreIdentities([]string{identityPath})
	identityFingerprint := state.identities[0].Fingerprint
	state.Click(hitAddHost)

	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitDialogAuth); !ok {
		t.Fatal("SSH form has no authentication choice")
	}
	if _, ok := hitByID(&canvas, hitDialogPassword); !ok {
		t.Fatal("automatic authentication does not offer an optional password")
	}

	state.Click(hitDialogAuth)
	state.Click(hitDialogAuthItemList.At(int(authPrivateKey)))
	canvas.Reset()
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitDialogIdentity); !ok {
		t.Fatal("private-key authentication has no key choice")
	}
	if _, ok := hitByID(&canvas, hitDialogPassword); ok {
		t.Fatal("private-key authentication still paints a password field")
	}

	state.Click(hitDialogIdentity)
	state.Click(hitDialogIdentityItemList.At(int(state.identities[0].ID)))
	typeInto(state, hitDialogLabel, "Keyed")
	typeInto(state, hitDialogAddress, "keyed.example.com")
	typeInto(state, hitDialogUser, "deploy")
	state.Click(hitDialogSubmit)
	added := state.hosts[len(state.hosts)-1]
	if added.Auth != authPrivateKey || added.Identity != identityFingerprint {
		t.Fatalf("added host authentication = %d %q", added.Auth, added.Identity)
	}
}

func TestHostAuthenticationSurvivesCatalogRoundTrip(t *testing.T) {
	want := Host{Protocol: protocolSSH, Auth: authPrivateKey, Identity: "SHA256:release"}
	var got Host
	if !got.applyTransport(want.transport()) {
		t.Fatal("saved authentication was rejected")
	}
	if got.Auth != want.Auth || got.Identity != want.Identity {
		t.Fatalf("restored authentication = %d %q", got.Auth, got.Identity)
	}

	var legacy Host
	if !legacy.applyTransport(savedTransport{}) || legacy.Auth != authAutomatic || legacy.Identity != "" {
		t.Fatalf("legacy host authentication = %d %q", legacy.Auth, legacy.Identity)
	}
	if (Host{Protocol: protocolSSH}).transport().Authentication != "" {
		t.Fatal("automatic authentication should stay absent from saved catalogs")
	}
}

func TestSSHConfigKeepsTargetAndJumpAuthenticationSeparate(t *testing.T) {
	state := newTestState(t, testOptions(t))
	identityPath := writeWorkbenchIdentity(t, "id_target", "")
	state.restoreIdentities([]string{identityPath})
	target := Host{ID: 41, Label: "Target", Address: "target.example.com", User: "target", Port: 22, Auth: authPrivateKey, Identity: state.identities[0].Fingerprint}
	jump := Host{ID: 42, Label: "Jump", Address: "jump.example.com", User: "jump", Port: 22, Auth: authPassword}
	target.finish()
	jump.finish()
	state.hosts = []Host{target, jump}
	state.proxies = []proxyRecord{{ID: 1, TargetID: target.ID, JumpID: jump.ID}}
	session := &Session{host: target}

	config, valid := state.sshConfigFor(session, target)
	if !valid || len(config.ProxyJumps) != 1 {
		t.Fatalf("config route valid=%v jumps=%v", valid, config.ProxyJumps)
	}
	targetEndpoint := sshconn.Endpoint{Host: target.Address, Port: target.Port, User: target.User}
	targetAuth := config.Authentication[targetEndpoint]
	if targetAuth.Methods != sshconn.AuthPublicKey || !slices.Equal(targetAuth.IdentityFiles, []string{identityPath}) || targetAuth.Agent {
		t.Fatalf("target authentication = %+v", targetAuth)
	}
	jumpEndpoint := sshconn.Endpoint{Host: jump.Address, Port: jump.Port, User: jump.User}
	jumpAuth := config.Authentication[jumpEndpoint]
	if jumpAuth.Methods != sshconn.AuthKeyboardInteractive|sshconn.AuthPassword || jumpAuth.IdentityFiles == nil || jumpAuth.Agent {
		t.Fatalf("jump authentication = %+v", jumpAuth)
	}
}

func TestUnavailableSelectedKeyDoesNotFallBackToAnotherKey(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.identities = []identityRecord{{Path: "/keys/other", Fingerprint: "SHA256:other"}}
	authentication := state.authenticationFor(Host{Auth: authPrivateKey, Identity: "SHA256:missing"})
	if authentication.Methods != sshconn.AuthPublicKey || authentication.IdentityFiles == nil || len(authentication.IdentityFiles) != 0 || authentication.Agent {
		t.Fatalf("authentication = %+v", authentication)
	}
}

func TestAgentAuthenticationHidesAndDoesNotSaveAPassword(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := Host{Protocol: protocolSSH, Address: "agent.example.com", User: "me", Port: 22, Auth: authAgent}
	if _, ok := state.hostPasswordSecretName(host); ok {
		t.Fatal("agent-only host has a password vault entry")
	}
	state.Click(hitAddHost)
	typeInto(state, hitDialogPassword, "discard me")
	state.Click(hitDialogAuth)
	state.Click(hitDialogAuthItemList.At(int(authAgent)))
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitDialogPassword); ok {
		t.Fatal("agent-only authentication paints a password field")
	}
	if state.hostEndpointRows() != 2 {
		t.Fatalf("agent endpoint rows = %d", state.hostEndpointRows())
	}
}

func TestAnIdentitySelectedByAHostCannotBeRemoved(t *testing.T) {
	state := newTestState(t, testOptions(t))
	identity := identityRecord{ID: 9, Path: "/keys/id_prod", Name: "id_prod", Fingerprint: "SHA256:prod"}
	state.identities = []identityRecord{identity}
	state.selectedIdentityID = identity.ID
	state.hosts = []Host{{Label: "Production", Auth: authPrivateKey, Identity: identity.Fingerprint}}
	state.removeSelectedIdentity()
	if len(state.identities) != 1 || state.status == "" {
		t.Fatalf("identity removed=%v status=%q", len(state.identities) == 0, state.status)
	}
}

func TestLoadingHostAuthenticationResetsTransientMenus(t *testing.T) {
	editor := hostEditor{authOpen: true, identityOpen: true}
	editor.load(Host{Auth: authPrivateKey, Identity: "SHA256:key"})
	if editor.auth != authPrivateKey || editor.identity != "SHA256:key" || editor.authOpen || editor.identityOpen {
		t.Fatalf("loaded editor = %+v", editor)
	}
}

func BenchmarkHostAuthenticationPolicy(b *testing.B) {
	state := &State{identities: []identityRecord{
		{Path: "/keys/one", Fingerprint: "SHA256:one"},
		{Path: "/keys/two", Fingerprint: "SHA256:two"},
	}}
	for _, benchmark := range []struct {
		name string
		host Host
	}{
		{name: "automatic", host: Host{Auth: authAutomatic}},
		{name: "password", host: Host{Auth: authPassword}},
		{name: "specific-key", host: Host{Auth: authPrivateKey, Identity: "SHA256:two"}},
		{name: "agent", host: Host{Auth: authAgent}},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = state.authenticationFor(benchmark.host)
			}
		})
	}
}
