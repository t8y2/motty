package workbench

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/components"
	"motty/internal/desktop"
	"motty/internal/desktop/pins"
	"motty/internal/vault"
)

func TestVNCMergedHostKeepsEndpointTrustAndSavedAccountAcrossRestart(t *testing.T) {
	options := testOptions(t)
	options.Invalidate = func() {}
	initial := newTestState(t, options)
	if err := initial.secrets.vault.Set(hostSecretName("host_b", vncSystemPasswordPart("pi")), "saved Pi password"); err != nil {
		t.Fatal(err)
	}
	initial.Close()
	key := []byte("saved Pi server identity")
	endpoint := pins.Endpoint{Protocol: pins.VNC, Address: "pi.example.com:5900"}
	store := pins.New(filepath.Join(filepath.Dir(options.KnownHostsPath), rdpCertificatesFile))
	if err := store.Remember(endpoint, pins.Fingerprint(key), ""); err != nil {
		t.Fatal(err)
	}
	if err := saveState(options.StatePath, savedState{Hosts: []savedHost{
		{ID: "host_b", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc"}},
		{ID: "host_a", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc"}},
	}}); err != nil {
		t.Fatal(err)
	}
	for restart := 0; restart < 2; restart++ {
		state := newTestState(t, options)
		if len(state.hosts) != 1 {
			t.Fatalf("restart %d retained %d copies of a compatible VNC host", restart, len(state.hosts))
		}
		host := state.hosts[0]
		if host.CatalogID != "host_a" {
			t.Fatalf("restart %d changed the surviving host: %+v", restart, host)
		}
		answers := make(chan [2]string, 1)
		state.connectionReconnect = reconnectOff
		state.dialDesktop = func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
			if err := request.VerifyServerKey(endpoint.Address, key); err != nil {
				return nil, err
			}
			username, password, err := request.AskCredentials()
			if err != nil {
				return nil, err
			}
			answers <- [2]string{username, password}
			return &encryptedVNC{newPictureOnly()}, nil
		}
		state.selectedHostID = host.ID
		state.Click(hitHosts)
		openHost(state)
		session, _ := state.sessionByIDPointer(state.activeSessionID)
		waitUntil(t, state, "merged host login", func() bool {
			return session.desk != nil || state.modal == modalHostKey || state.modal == modalPassword
		})
		if state.modal == modalHostKey || state.modal == modalPassword {
			t.Fatalf("restart %d prompted for an already trusted endpoint or saved account", restart)
		}
		if got := <-answers; got != [2]string{"pi", "saved Pi password"} {
			t.Fatalf("restart %d used credentials %q", restart, got)
		}
		state.closeSession(session.ID)
		state.Close()
		vault.Forget(filepath.Join(filepath.Dir(options.StatePath), "vault.json"))
	}
}

func TestVNCMergedAccountChainsKeepExistingPasswordsAndOtherHostsSeparate(t *testing.T) {
	hosts := []savedHost{
		{ID: "host_a", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc"}},
		{ID: "host_b", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc"}},
		{ID: "host_c", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc"}},
		{ID: "host_neighbor", Label: "Another Pi entry", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc"}},
	}
	for _, order := range [][]int{{0, 1, 2}, {1, 2, 0}, {2, 1, 0}} {
		t.Run(fmt.Sprint(order), func(t *testing.T) {
			options := testOptions(t)
			initial := newTestState(t, options)
			for name, value := range map[string]string{
				hostSecretName("host_a", vncSystemPasswordPart("alice")):        "existing alice password",
				hostSecretName("host_b", vncSystemPasswordPart("alice")):        "older alice password",
				hostSecretName("host_c", vncSystemPasswordPart("alice")):        "another alice password",
				hostSecretName("host_b", vncSystemPasswordPart("bob")):          "first bob password",
				hostSecretName("host_c", vncSystemPasswordPart("bob")):          "later bob password",
				hostSecretName("host_c", vncSystemPasswordPart("charlie")):      "charlie password",
				hostSecretName("host_neighbor", vncSystemPasswordPart("other")): "neighbor password",
			} {
				if err := initial.secrets.vault.Set(name, value); err != nil {
					t.Fatal(err)
				}
			}
			initial.Close()
			var entries []savedHost
			for _, index := range order {
				entries = append(entries, hosts[index])
			}
			entries = append(entries, hosts[3])
			if err := saveState(options.StatePath, savedState{Hosts: entries}); err != nil {
				t.Fatal(err)
			}
			for restart := 0; restart < 2; restart++ {
				state := newTestState(t, options)
				merged, ok := state.hostByCatalogID("host_a")
				if !ok || len(state.hosts) != 2 || merged.CatalogID != "host_a" {
					t.Fatalf("restart %d changed the surviving host across the merge chain: %+v", restart, merged)
				}
				for account, want := range map[string]string{"alice": "existing alice password", "bob": "first bob password", "charlie": "charlie password"} {
					got, found := vaultValue(t, state, hostSecretName("host_a", vncSystemPasswordPart(account)))
					if !found || got != want {
						t.Fatalf("restart %d account %q has password %q, found=%v", restart, account, got, found)
					}
				}
				if account, unique, err := state.singleSavedVNCAccount("host_a"); err != nil || unique || account != "" {
					t.Fatalf("merged accounts were treated as unambiguous: account=%q unique=%v err=%v", account, unique, err)
				}
				if _, found := vaultValue(t, state, hostSecretName("host_a", vncSystemPasswordPart("other"))); found {
					t.Fatal("a differently named host's account moved to the merged host")
				}
				if got, found := vaultValue(t, state, hostSecretName("host_neighbor", vncSystemPasswordPart("other"))); !found || got != "neighbor password" {
					t.Fatal("merging another host changed the neighbor's account")
				}
				state.Close()
				vault.Forget(filepath.Join(filepath.Dir(options.StatePath), "vault.json"))
			}
		})
	}
}

func TestVNCMergeFailureShowsMissingVaultKeyAndKeepsHostsForRetry(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("mixed=%t", mixed), func(t *testing.T) {
			options := testOptions(t)
			keys := &memKeys{}
			options.Keystore = keys
			initial := newTestState(t, options)
			sourceName := hostSecretName("host_b", vncSystemPasswordPart("pi"))
			targetName := hostSecretName("host_a", vncSystemPasswordPart("pi"))
			if err := initial.secrets.vault.Set(sourceName, "saved Pi password"); err != nil {
				t.Fatal(err)
			}
			sshSourceName := hostSecretName("host_ssh_b", hostPasswordPart)
			if mixed {
				if err := initial.secrets.vault.Set(sshSourceName, "saved SSH password"); err != nil {
					t.Fatal(err)
				}
			}
			key, err := keys.Load(vault.KeyService, vault.KeyAccount)
			if err != nil {
				t.Fatal(err)
			}
			initial.Close()
			vaultPath := filepath.Join(filepath.Dir(options.StatePath), "vault.json")
			vault.Forget(vaultPath)
			if err := keys.Delete(vault.KeyService, vault.KeyAccount); err != nil {
				t.Fatal(err)
			}
			hosts := []savedHost{
				{ID: "host_b", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc"}},
				{ID: "host_a", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc"}},
			}
			if mixed {
				hosts = append(hosts,
					savedHost{ID: "host_ssh_b", Label: "SSH server", Address: "ssh.example.com", User: "alice"},
					savedHost{ID: "host_ssh_a", Label: "SSH server", Address: "ssh.example.com", User: "alice"},
				)
			}
			if err := saveState(options.StatePath, savedState{Hosts: hosts}); err != nil {
				t.Fatal(err)
			}
			failed := newTestState(t, options)
			wantHosts, wantAliases := len(hosts), 0
			if mixed {
				wantHosts, wantAliases = len(hosts)-1, 1
			}
			if len(failed.hosts) != wantHosts || failed.mergedHosts != mixed || len(failed.merged) != wantAliases {
				t.Fatal("failed VNC migration did not preserve VNC hosts while allowing unrelated SSH merges")
			}
			if _, found := failed.hostByCatalogID("host_b"); !found {
				t.Fatal("failed VNC migration discarded its source host")
			}
			if mixed && failed.merged["host_ssh_b"] != "host_ssh_a" {
				t.Fatal("failed VNC migration blocked an unrelated SSH merge")
			}
			if len(failed.fileNotices.items) != 1 {
				t.Fatalf("missing-key failure did not leave a visible notice: %+v", failed.fileNotices.items)
			}
			notice := failed.fileNotices.items[0]
			reason := failed.vaultErrorText(vault.ErrKeyLost)
			if notice.title != failed.messages.SettingsVault || notice.kind != components.ToastError || !strings.Contains(notice.detail, reason) {
				t.Fatalf("migration failure lost its original cause: %+v", notice)
			}
			_, texts := paintedTexts(failed)
			if !strings.Contains(strings.Join(texts, " "), reason) {
				t.Fatalf("the missing-key explanation was not painted in the visible notice: %v", texts)
			}
			failed.Close()
			vault.Forget(vaultPath)
			if err := keys.Store(vault.KeyService, vault.KeyAccount, key); err != nil {
				t.Fatal(err)
			}
			readable, err := vault.Open(vaultPath, keys)
			if err != nil {
				t.Fatal(err)
			}
			if got, found, err := readable.Get(sourceName); err != nil || !found || got != "saved Pi password" {
				t.Fatal("failed migration changed the source password")
			}
			if _, found, err := readable.Get(targetName); err != nil || found {
				t.Fatal("failed migration wrote a password under the new host identity")
			}
			sshTargetName := hostSecretName("host_ssh_a", hostPasswordPart)
			if mixed {
				if got, found, err := readable.Get(sshSourceName); err != nil || !found || got != "saved SSH password" {
					t.Fatal("failed mixed migration changed the SSH source password")
				}
				if _, found, err := readable.Get(sshTargetName); err != nil || found {
					t.Fatal("failed mixed migration wrote an SSH password under the new host identity")
				}
			}
			retried := newTestState(t, options)
			_, keptVNC := retried.hostByCatalogID("host_a")
			if len(retried.hosts) != len(hosts)/2 || !keptVNC {
				t.Fatal("restoring the key could not retry the saved host merge")
			}
			if got, found := vaultValue(t, retried, targetName); !found || got != "saved Pi password" {
				t.Fatal("retry did not migrate the saved system account")
			}
			if mixed {
				if _, found := vaultValue(t, retried, sshTargetName); found {
					t.Fatal("a completed SSH merge was retried after the vault key returned")
				}
			}
		})
	}
}

func TestNonVNCHostMergesKeepWorkingWhenVaultKeyIsMissing(t *testing.T) {
	for _, protocol := range []string{"ssh", "rdp"} {
		t.Run(protocol, func(t *testing.T) {
			options := testOptions(t)
			keys := &memKeys{}
			options.Keystore = keys
			initial := newTestState(t, options)
			sourceName := hostSecretName("host_b", hostPasswordPart)
			if err := initial.secrets.vault.Set(sourceName, "saved password"); err != nil {
				t.Fatal(err)
			}
			key, err := keys.Load(vault.KeyService, vault.KeyAccount)
			if err != nil {
				t.Fatal(err)
			}
			initial.Close()
			vaultPath := filepath.Join(filepath.Dir(options.StatePath), "vault.json")
			vault.Forget(vaultPath)
			if err := keys.Delete(vault.KeyService, vault.KeyAccount); err != nil {
				t.Fatal(err)
			}
			if err := saveState(options.StatePath, savedState{Hosts: []savedHost{
				{ID: "host_b", Label: "Server", Address: "server.example.com", User: "alice", savedTransport: savedTransport{Protocol: protocol}},
				{ID: "host_a", Label: "Server", Address: "server.example.com", User: "alice", savedTransport: savedTransport{Protocol: protocol}},
			}}); err != nil {
				t.Fatal(err)
			}
			for restart := 0; restart < 2; restart++ {
				state := newTestState(t, options)
				if len(state.hosts) != 1 || state.hosts[0].CatalogID != "host_a" {
					t.Fatalf("restart %d blocked %s duplicate merging on an unreadable vault", restart, protocol)
				}
				state.Close()
				vault.Forget(vaultPath)
			}
			if err := keys.Store(vault.KeyService, vault.KeyAccount, key); err != nil {
				t.Fatal(err)
			}
			readable, err := vault.Open(vaultPath, keys)
			if err != nil {
				t.Fatal(err)
			}
			if got, found, err := readable.Get(sourceName); err != nil || !found || got != "saved password" {
				t.Fatal("skipping an unreadable vault changed the source password")
			}
		})
	}
}
