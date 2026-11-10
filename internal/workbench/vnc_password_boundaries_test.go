package workbench

import (
	"fmt"
	"path/filepath"
	"testing"

	"motty/internal/sshconn"
	"motty/internal/vault"
)

func TestVNCPasswordSlotsKeepGatewayAndTargetOwnersDistinct(t *testing.T) {
	for _, user := range []string{vncAccount, vncAccountPrefix + "alice"} {
		for _, shared := range []struct{ gateway, target bool }{{}, {gateway: true}, {target: true}, {gateway: true, target: true}} {
			t.Run(fmt.Sprintf("%s/gateway-shared=%t/target-shared=%t", user, shared.gateway, shared.target), func(t *testing.T) {
				options := testOptions(t)
				options.Keystore = vault.FileKeystore{Path: filepath.Join(filepath.Dir(options.StatePath), "vault.key")}
				state := newTestState(t, options)
				gateway := Host{CatalogID: "test-gateway", Protocol: protocolSSH, Address: "same.example.test", Port: 22, User: user}
				target := Host{CatalogID: "test-vnc", Protocol: protocolVNC, Address: gateway.Address, Port: 5900, User: user, ViaSSH: true}
				state.credentials = []credential{{ID: "gateway-shared"}, {ID: "target-shared"}}
				if shared.gateway {
					gateway.Credential = "gateway-shared"
				}
				if shared.target {
					target.Credential = "target-shared"
				}
				state.hosts = []Host{gateway, target}
				gatewayName := hostSecretName(gateway.CatalogID, hostPasswordPart)
				if shared.gateway {
					gatewayName = credentialSecretName(gateway.Credential)
				}
				legacyName := hostSecretName(target.CatalogID, hostPasswordPart)
				systemName := hostSecretName(target.CatalogID, vncSystemPasswordPart("alice"))
				if shared.target {
					legacyName, systemName = credentialSecretName(target.Credential), credentialSecretName(target.Credential)
				}
				values := map[string]string{
					gatewayName: "gateway password", systemName: "system password",
					hostSecretName(gateway.CatalogID, vncSystemPasswordPart("alice")): "incorrect VNC slot on gateway",
				}
				if !shared.target {
					values[legacyName] = "legacy password"
				}
				for name, value := range values {
					if err := state.secrets.vault.Set(name, value); err != nil {
						t.Fatal(err)
					}
				}
				inner := &fakeConn{}
				conn := &desktopConn{inner: inner, credentialFields: 1, answers: make(chan []string, 1)}
				session := &Session{host: target, conn: conn, attempt: 1, phase: sshconn.PhasePasswordPrompt,
					endpoint: sshconn.Endpoint{Host: gateway.Address, Port: gateway.Port, User: user}, role: sshconn.EndpointProxyJump,
					questions: []sshconn.Question{{Secret: true}},
				}
				if got := state.passwordName(session); got != gatewayName {
					t.Fatalf("SSH gateway password uses the wrong secret slot: %q, want %q", got, gatewayName)
				}
				status := sshconn.Status{Phase: session.phase, Attempt: 1, Endpoint: session.endpoint, EndpointRole: session.role, Questions: session.questions}
				if !state.canAutofill(session, status) || state.autofill(session) != autofillSent || len(inner.passwords) != 1 || inner.passwords[0] != "gateway password" {
					t.Fatal("gateway prompt did not use its own or explicitly shared SSH password")
				}
				conn.asking = true
				session.role = sshconn.EndpointTarget
				session.endpoint.Port, session.endpoint.User = target.Port, vncAccount
				if got := state.passwordName(session); got != legacyName {
					t.Fatalf("legacy VNC prompt uses the wrong secret slot: %q, want %q", got, legacyName)
				}
				session.endpoint.User = vncAccountPrefix + "alice"
				if got := state.passwordName(session); got != systemName {
					t.Fatalf("VNC system account prompt uses the wrong secret slot: %q, want %q", got, systemName)
				}
				if state.autofill(session) != autofillSent {
					t.Fatal("VNC system account password was not available for autofill")
				}
				got := <-conn.answers
				if len(got) != 1 || got[0] != "system password" {
					t.Fatal("VNC system account autofill crossed the gateway or legacy password boundary")
				}
				if state.passwordShared(session) != shared.target {
					t.Fatal("target shared credential selection was lost")
				}
			})
		}
	}
}

func TestSSHPasswordWithVNCSentinelNameDoesNotUseVNCEmptyPasswordRule(t *testing.T) {
	for _, user := range []string{vncAccount, vncAccountPrefix + "alice"} {
		t.Run(user, func(t *testing.T) {
			options := testOptions(t)
			options.Keystore = vault.FileKeystore{Path: filepath.Join(filepath.Dir(options.StatePath), "vault.key")}
			state := newTestState(t, options)
			inner := &fakeConn{}
			conn := newDesktopConn(Host{Protocol: protocolVNC, ViaSSH: true}, nil, nil, 0, nil, nil)
			conn.inner = inner
			state.sessions = []Session{{ID: 91, host: Host{Protocol: protocolVNC, ViaSSH: true}, conn: conn,
				phase: sshconn.PhasePasswordPrompt, attempt: 1, role: sshconn.EndpointProxyJump,
				endpoint: sshconn.Endpoint{Host: "ssh.example.test", Port: 22, User: user}, questions: []sshconn.Question{{Secret: true}},
			}}
			state.modal, state.modalSession = modalPassword, 91
			state.preparePassword(&state.sessions[0])
			state.submitPassword()
			if state.passwordError != "" || len(inner.answers) != 1 || len(inner.answers[0]) != 1 || inner.answers[0][0] != "" {
				t.Fatal("SSH password prompt was blocked by the VNC empty-password requirement")
			}
			// The same sentinel belongs to VNC once the desktop's actual password
			// question is active; the empty-password protection must still apply.
			conn.asking, conn.credentialFields = true, 1
			state.sessions[0].role = sshconn.EndpointTarget
			state.sessions[0].endpoint.Port = 5900
			state.modal, state.modalSession = modalPassword, 91
			state.preparePassword(&state.sessions[0])
			state.submitPassword()
			if state.passwordError != state.messages.Desktop.ErrVNCPassword {
				t.Fatal("an actual VNC prompt accepted an empty password")
			}
			select {
			case <-conn.answers:
				t.Fatal("an empty password reached the VNC connection")
			default:
			}
		})
	}
}
