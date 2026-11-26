package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
	"motty/internal/sshconn"
)

func TestTheKeychainOpensADeploymentDialogForAStoredKey(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionKeys
	host := Host{ID: 9, CatalogID: "deploy-host", Label: "Deploy target", Address: "127.0.0.1", Port: 2222, User: "me", Auth: authPassword, System: sshconn.SystemLinux}
	host.finish()
	state.hosts = []Host{host}
	if message := state.addStoredKey("Deploy key", []byte(identityPEM(t, "")), func(identityRecord) {}); message != "" {
		t.Fatalf("the key was not stored: %s", message)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitKeysDeploy); !ok {
		t.Fatal("the keychain has no Deploy button for a stored key")
	}

	state.Click(hitKeysDeploy)
	if state.modal != modalHostKeyDeploy {
		t.Fatalf("modal = %v, want the deployment dialog", state.modal)
	}
	if state.hostKeyDeployView.keyRef != state.identities[0].Path {
		t.Fatalf("key = %q", state.hostKeyDeployView.keyRef)
	}
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitHostKeyDeployHosts.At(int(host.ID))); !ok {
		t.Fatal("the host is not in the deployment dialog")
	}
	state.Click(hitHostKeyDeployHosts.At(int(host.ID)))
	if !state.hostKeyDeployView.selected[host.ID] {
		t.Fatal("the host was not selected")
	}
	state.Click(hitHostKeyDeployHosts.At(int(host.ID)))
	if state.hostKeyDeployView.selected[host.ID] {
		t.Fatal("the host stayed selected")
	}
	state.Click(hitHostKeyDeploySwitch)
	if !state.hostKeyDeployView.switchAuth {
		t.Fatal("the switch-to-key choice did not toggle")
	}
	state.Click(hitHostKeyDeployClose)
	if state.modal != modalNone {
		t.Fatal("the dialog did not close")
	}
}

func TestDeployingAnUnavailableKeyReportsEveryChosenHost(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := Host{ID: 3, CatalogID: "fixture", Label: "Fixture", Address: "127.0.0.1", Port: 22, User: "me", Auth: authPassword}
	host.finish()
	state.hosts = []Host{host}
	state.identities = []identityRecord{{ID: 1, Path: "sshkey:missing", Stored: true, PublicKey: "", Fingerprint: "SHA256:missing"}}

	state.startHostKeyDeployment([]uint32{host.ID}, "sshkey:missing", false)
	if state.hostKeyDeploy.running || len(state.hostKeyDeploy.results) != 1 {
		t.Fatalf("deployment = %+v", state.hostKeyDeploy)
	}
	if result := state.hostKeyDeploy.results[0]; result.Imported || result.Error != hostKeyDeployErrorKeyUnavailable {
		t.Fatalf("result = %+v", result)
	}
}

func TestDeploymentErrorsUseLocalizedCopyInsteadOfTheServerDetail(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.setLanguage(i18n.Chinese)
	for _, code := range []hostKeyDeployErrorCode{
		hostKeyDeployErrorKeyUnavailable, hostKeyDeployErrorHostKey, hostKeyDeployErrorNoPassword,
		hostKeyDeployErrorAuth, hostKeyDeployErrorTimeout, hostKeyDeployErrorNotConfirmed,
	} {
		got := state.hostKeyDeployError(hostKeyDeployResult{Error: code, Detail: "English server detail"})
		if got == "" || got == "English server detail" {
			t.Fatalf("%s used the server detail: %q", code, got)
		}
	}
}

func TestAFileBackedOrCertifiedKeyIsNotOfferedForDeployment(t *testing.T) {
	for _, key := range []identityRecord{
		{Path: "/home/me/.ssh/id_ed25519", PublicKey: "ssh-ed25519 AAAA", Fingerprint: "SHA256:one"},
		{Stored: true, Path: "sshkey:cert", PublicKey: "ssh-ed25519 AAAA", Fingerprint: "SHA256:two", Certificate: true},
		{Stored: true, Path: "sshkey:security", PublicKey: "sk-ssh-ed25519 AAAA", Fingerprint: "SHA256:three", SecurityKey: true},
		{Path: "sshkey:plain"},
	} {
		if usableDeployIdentity(key) {
			t.Fatalf("key %+v is offered for deployment", key)
		}
	}
}
