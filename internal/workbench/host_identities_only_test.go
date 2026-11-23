package workbench

import (
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconfig"
)

// The automatic policy offers everything, unless the host asks for its
// configured identities only: then the agent may supply only keys matching the
// key library, the host's files or the defaults, and no unrelated agent key is
// offered. An imported ssh_config that says so keeps its stricter meaning.
func TestAutomaticAuthenticationRestrictsAgentKeysWhenAsked(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.identities = []identityRecord{{Path: "/keys/library", Name: "library", Fingerprint: "SHA256:library"}}
	host := Host{Protocol: protocolSSH, Address: "auto.example.com", User: "me", Port: 22, Auth: authAutomatic}
	authentication := state.authenticationFor(host)
	if authentication.IdentitiesOnly || !authentication.Agent || !slices.Equal(authentication.AdditionalIdentityFiles, []string{"/keys/library"}) {
		t.Fatalf("automatic: %+v", authentication)
	}
	host.IdentitiesOnly = true
	authentication = state.authenticationFor(host)
	if !authentication.IdentitiesOnly || !authentication.Agent || !slices.Equal(authentication.AdditionalIdentityFiles, []string{"/keys/library"}) {
		t.Fatalf("configured identities only: %+v", authentication)
	}
	only := true
	host.IdentitiesOnly = false
	host.SSHOptions = &sshconfig.ConnectionOptions{IdentitiesOnly: &only, IdentityFiles: []string{"/keys/host"}}
	authentication = state.authenticationFor(host)
	if !authentication.IdentitiesOnly || !slices.Equal(authentication.IdentityFiles, []string{"/keys/host"}) ||
		len(authentication.AdditionalIdentityFiles) != 0 {
		t.Fatalf("imported identities only: %+v", authentication)
	}
	host.IdentitiesOnly = true
	authentication = state.authenticationFor(host)
	if !authentication.IdentitiesOnly || !slices.Equal(authentication.IdentityFiles, []string{"/keys/host"}) ||
		!slices.Equal(authentication.AdditionalIdentityFiles, []string{"/keys/library"}) {
		t.Fatalf("imported files and the app switch: %+v", authentication)
	}
}

// The switch is part of the host, saved with it, and only shown where it
// applies: automatic authentication.
func TestTheHostDialogSavesTheAgentIdentityRestriction(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	state.editor.label.Insert("Restricted")
	state.editor.address.Insert("restricted.example.com")
	state.editor.advancedOpen = true
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1600})
	if _, ok := hitByID(&canvas, hitDialogIdentitiesOnly); !ok {
		t.Fatal("the agent identity switch was not painted for automatic authentication")
	}
	state.Click(hitDialogIdentitiesOnly)
	if !state.editor.identitiesOnly {
		t.Fatal("the switch did not turn on")
	}
	state.Click(hitDialogSubmit)
	if state.modal != modalNone {
		t.Fatalf("the host was refused: %q", state.dialogError)
	}
	var saved Host
	for _, host := range state.hosts {
		if host.Label == "Restricted" {
			saved = host
		}
	}
	if !saved.IdentitiesOnly || !state.authenticationFor(saved).IdentitiesOnly {
		t.Fatalf("saved host = %+v", saved)
	}

	again := New(state.options)
	defer again.Close()
	var reloaded Host
	for _, host := range again.hosts {
		if host.Label == "Restricted" {
			reloaded = host
		}
	}
	if !reloaded.IdentitiesOnly {
		t.Fatalf("reloaded host = %+v", reloaded)
	}

	// The switch is not part of the other policies.
	state.Click(hitAddHost)
	state.editor.advancedOpen = true
	chooseSelectOption(t, state, state.messages.FieldAuthentication, state.messages.AuthPassword)
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1600})
	if _, ok := hitByID(&canvas, hitDialogIdentitiesOnly); ok {
		t.Fatal("the agent identity switch was painted for password authentication")
	}
}
