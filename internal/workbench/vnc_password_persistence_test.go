package workbench

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/text"
	"motty/internal/desktop"
	"motty/internal/sshconn"
	"motty/internal/vault"
)

func TestVNCSavedAccountPrefillIsUnambiguous(t *testing.T) {
	for _, test := range []struct {
		name                   string
		accounts               []string
		wantUser, wantPassword string
		automatic              bool
	}{
		{"one account", []string{"bob"}, "bob", "password/bob", true},
		{"empty account", []string{""}, "", "password/", false},
		{"two accounts", []string{"alice", "bob"}, "", "", false},
		{"empty and named accounts", []string{"", "bob"}, "", "", false},
		{"only legacy and another host", nil, "", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			sent := make(chan [2]string, 1)
			state, host := realVNCState(t, "", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
				username, password, err := request.AskCredentials()
				if err != nil {
					return nil, err
				}
				sent <- [2]string{username, password}
				return &encryptedVNC{newPictureOnly()}, nil
			})
			for _, account := range test.accounts {
				if err := state.secrets.vault.Set(hostSecretName(host.CatalogID, vncSystemPasswordPart(account)), "password/"+account); err != nil {
					t.Fatal(err)
				}
			}
			for name, value := range map[string]string{
				hostSecretName(host.CatalogID, hostPasswordPart):                 "legacy password",
				hostSecretName("another-host", vncSystemPasswordPart("mallory")): "another password",
			} {
				if err := state.secrets.vault.Set(name, value); err != nil {
					t.Fatal(err)
				}
			}
			openHost(state)
			if test.automatic {
				session, _ := state.sessionByIDPointer(state.activeSessionID)
				waitUntil(t, state, "saved VNC account login", func() bool { return session.desk != nil })
				if state.modal == modalPassword {
					t.Fatal("a unique saved system account required a credential prompt")
				}
				if got := <-sent; got != [2]string{test.wantUser, test.wantPassword} {
					t.Fatal("VNC login selected an unrelated saved account")
				}
				return
			}
			waitUntil(t, state, "saved VNC account", func() bool { return state.modal == modalPassword })
			if len(state.passwordFields) != 2 || state.passwordFields[0].String() != test.wantUser || state.passwordFields[1].String() != test.wantPassword {
				t.Fatal("VNC account prefill selected an unrelated or ambiguous secret")
			}
			select {
			case <-sent:
				t.Fatal("account credentials were sent without confirmation")
			default:
			}
		})
	}
}

func TestVNCPasswordSaveCheckboxSurvivesApplicationRestart(t *testing.T) {
	for _, username := range []string{"alice", ""} {
		t.Run("configured-user="+username, func(t *testing.T) {
			options := testOptions(t)
			options.Invalidate = func() {}
			answers := make(chan [2]string, 1)
			open := func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
				user, password, err := request.AskCredentials()
				if err != nil {
					return nil, err
				}
				answers <- [2]string{user, password}
				return &encryptedVNC{newPictureOnly()}, nil
			}
			state := newTestState(t, options)
			state.connectionReconnect, state.dialDesktop = reconnectOff, open
			host := addStreamHost(state, Host{Label: "Pi", Address: "pi.example.com", User: username, Protocol: protocolVNC})
			state.save()
			openHost(state)
			session, _ := state.sessionByIDPointer(state.activeSessionID)
			waitUntil(t, state, "initial password", func() bool { return state.modal == modalPassword })
			if !state.secrets.canSave {
				t.Fatal("VNC does not offer password saving")
			}
			state.Click(hitPasswordSave)
			if !state.secrets.save {
				t.Fatal("password save checkbox did not enable saving")
			}
			if username == "" {
				state.passwordFields[0] = *text.NewEditor("alice")
			}
			index := passwordQuestionIndex(state.passwordQuestions)
			state.passwordFields[index] = *text.NewEditor("saved account password")
			state.Click(hitPasswordSubmit)
			waitUntil(t, state, "initial desktop", func() bool { return session.desk != nil })
			if got := <-answers; got != [2]string{"alice", "saved account password"} {
				t.Fatalf("initial credentials=%q", got)
			}
			state.Close()
			vaultPath := filepath.Join(filepath.Dir(options.StatePath), "vault.json")
			// Reopen from disk rather than relying on the shared in-memory vault.
			vault.Forget(vaultPath)
			restored := newTestState(t, options)
			restored.connectionReconnect, restored.dialDesktop = reconnectOff, open
			saved, ok := restored.hostByCatalogID(host.CatalogID)
			if !ok {
				t.Fatal("VNC host did not survive restart")
			}
			restored.selectedHostID = saved.ID
			restored.Click(hitHosts)
			openHost(restored)
			reconnected, _ := restored.sessionByIDPointer(restored.activeSessionID)
			waitUntil(t, restored, "restored desktop", func() bool { return reconnected.phase == sshconn.PhaseConnected && reconnected.desk != nil })
			if restored.modal == modalPassword {
				t.Fatal("the saved system account required credential confirmation after restart")
			}
			if got := <-answers; got != [2]string{"alice", "saved account password"} {
				t.Fatalf("restored credentials=%q", got)
			}
		})
	}
}
