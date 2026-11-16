package workbench

import (
	"testing"

	"motty/internal/sshconn"
)

func TestVNCSharedPasswordPrefillsSystemAccountQuestions(t *testing.T) {
	for _, test := range []struct {
		name, username string
		oldPassword    bool
	}{
		{name: "unknown username"},
		{name: "known username", username: "alice"},
		{name: "old host password", username: "alice", oldPassword: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			entry := addCredential(t, state, "Desktop", "shared desktop password")
			host := addStreamHost(state, Host{Label: "Pi", Address: "pi.example.com", Protocol: protocolVNC, Credential: entry.ID})
			if test.oldPassword {
				if err := state.secrets.vault.Set(hostSecretName(host.CatalogID, vncSystemPasswordPart(test.username)), "old host password"); err != nil {
					t.Fatal(err)
				}
			}
			session := &Session{
				host: host, endpoint: sshconn.Endpoint{Host: host.Address, Port: host.Port, User: vncAccountPrefix + test.username},
				role: sshconn.EndpointTarget, attempt: 1,
				questions: []sshconn.Question{{Text: "Username"}, {Text: "Password", Secret: true}},
			}
			state.preparePassword(session)
			if got := state.passwordFields[0].String(); got != test.username {
				t.Fatalf("username = %q, want %q", got, test.username)
			}
			if got := state.passwordFields[1].String(); got != "shared desktop password" {
				t.Fatalf("password = %q, want the selected shared password", got)
			}
			if state.secrets.canSave {
				t.Fatal("the login prompt offered to overwrite a shared password")
			}
		})
	}
}

func TestDesktopTunnelUsesGatewayPasswordInsteadOfDesktopSharedPassword(t *testing.T) {
	for _, desktop := range []struct {
		name     string
		protocol hostProtocol
	}{{"VNC", protocolVNC}, {"RDP", protocolRDP}} {
		for _, source := range []string{"gateway shared", "gateway own", "unsaved gateway", "gateway named vnc"} {
			t.Run(desktop.name+"/"+source, func(t *testing.T) {
				state := newTestState(t, testOptions(t))
				desktopPassword := addCredential(t, state, "Desktop", "desktop password")
				gatewayPassword := addCredential(t, state, "SSH", "gateway password")
				gateway := Host{Address: "pi.example.com", User: "ssh-user", Port: 2222, Protocol: protocolSSH}
				if source == "unsaved gateway" {
					gateway.Port = defaultPort
				}
				if source == "gateway named vnc" {
					gateway.User = "vnc"
				}
				gateway.finish()
				if source != "unsaved gateway" {
					gateway = addStreamHost(state, gateway)
				}
				if source == "gateway shared" || source == "gateway named vnc" {
					state.hosts[state.hostIndex(gateway.ID)].Credential = gatewayPassword.ID
				} else if err := state.secrets.vault.Set(hostSecretName(gateway.CatalogID, hostPasswordPart), "gateway password"); err != nil {
					t.Fatal(err)
				}
				addStreamHost(state, Host{Label: "Desktop", Address: gateway.Address, User: gateway.User, Protocol: desktop.protocol, ViaSSH: true, Credential: desktopPassword.ID})
				state.activeSection = sectionHosts
				openHost(state)
				session, _ := state.sessionByIDPointer(state.activeSessionID)
				conn := session.conn.(*desktopConn)
				inner := conn.inner.(*fakeConn)
				inner.set(func(status *sshconn.Status) {
					status.Phase, status.Attempt = sshconn.PhasePasswordPrompt, 1
					status.Endpoint = sshconn.Endpoint{Host: gateway.Address, User: gateway.User, Port: gateway.Port}
					status.EndpointRole = sshconn.EndpointTarget
					status.Questions = []sshconn.Question{{Secret: true}}
				})
				conn.innerChanged()
				state.Wake()
				if len(inner.passwords) != 1 || inner.passwords[0] != "gateway password" {
					t.Fatalf("SSH received %q, want the gateway password", inner.passwords)
				}
			})
		}
	}
}
