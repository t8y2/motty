package workbench

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/components"
	"motty/internal/desktop"
	"motty/internal/desktop/rdp"
	"motty/internal/vault"
)

func TestVNCMergedHostKeepsServerIdentityAndSavedAccountAcrossRestart(t *testing.T) {
	options := testOptions(t)
	options.Invalidate = func() {}
	initial := newTestState(t, options)
	if err := initial.secrets.vault.Set(hostSecretName("host_b", vncSystemPasswordPart("pi")), "saved Pi password"); err != nil {
		t.Fatal(err)
	}
	initial.Close()
	key := []byte("saved Pi server identity")
	pin := vncHostKey{Address: "pi.example.com:5900", Kind: vncServerKeyType, Fingerprint: rdp.Fingerprint(key)}
	if err := saveState(options.StatePath, savedState{Hosts: []savedHost{
		{ID: "host_b", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc", VNCKeys: []vncHostKey{pin}}},
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
		if host.CatalogID != "host_a" || !slices.Equal(host.VNCKeys, []vncHostKey{pin}) {
			t.Fatalf("restart %d lost the merged server identity: %+v", restart, host)
		}
		answers := make(chan [2]string, 1)
		state.connectionReconnect = reconnectOff
		state.dialDesktop = func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
			if err := request.VerifyServerKey(pin.Address, key); err != nil {
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
			t.Fatalf("restart %d required confirmation for an already trusted server or saved account", restart)
		}
		if got := <-answers; got != [2]string{"pi", "saved Pi password"} {
			t.Fatalf("restart %d used credentials %q", restart, got)
		}
		state.closeSession(session.ID)
		state.Close()
		vault.Forget(filepath.Join(filepath.Dir(options.StatePath), "vault.json"))
	}
}

func TestVNCMergingHostsNeverDiscardsConflictingServerIdentities(t *testing.T) {
	first := vncHostKey{Address: "pi.example.com:5900", Kind: vncServerKeyType, Fingerprint: rdp.Fingerprint([]byte("first identity"))}
	second := first
	second.Fingerprint = rdp.Fingerprint([]byte("second identity"))
	hosts := []savedHost{
		{ID: "host_a", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc"}},
		{ID: "host_b", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc", VNCKeys: []vncHostKey{first}}},
		{ID: "host_c", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc", VNCKeys: []vncHostKey{second}}},
	}
	for _, order := range [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		t.Run(fmt.Sprint(order), func(t *testing.T) {
			options := testOptions(t)
			var entries []savedHost
			for _, index := range order {
				entries = append(entries, hosts[index])
			}
			if err := saveState(options.StatePath, savedState{Hosts: entries}); err != nil {
				t.Fatal(err)
			}
			for restart := 0; restart < 2; restart++ {
				state := newTestState(t, options)
				if len(state.hosts) != len(hosts) {
					t.Fatalf("restart %d merged a group with conflicting server identities", restart)
				}
				var fingerprints []string
				for _, host := range state.hosts {
					if len(host.VNCKeys) > 1 {
						t.Fatal("conflicting pins were combined into an ambiguous host identity")
					}
					if fingerprint := host.vncFingerprint(first.Address, first.Kind); fingerprint != "" {
						fingerprints = append(fingerprints, fingerprint)
					}
				}
				slices.Sort(fingerprints)
				want := []string{first.Fingerprint, second.Fingerprint}
				slices.Sort(want)
				if !slices.Equal(fingerprints, want) {
					t.Fatalf("restart %d discarded conflicting identities: %v", restart, fingerprints)
				}
				state.Close()
			}
		})
	}
}

func TestVNCMergedAccountChainsKeepExistingPasswordsAndOtherHostsSeparate(t *testing.T) {
	pin := vncHostKey{Address: "pi.example.com:5900", Kind: vncServerKeyType, Fingerprint: rdp.Fingerprint([]byte("Pi identity"))}
	otherPin := pin
	otherPin.Address = "previous-pi.example.com:5900"
	hosts := []savedHost{
		{ID: "host_a", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc"}},
		{ID: "host_b", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc", VNCKeys: []vncHostKey{pin}}},
		{ID: "host_c", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc", VNCKeys: []vncHostKey{pin, otherPin}}},
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
				if !ok || len(state.hosts) != 2 || len(merged.VNCKeys) != 2 || !slices.Contains(merged.VNCKeys, pin) || !slices.Contains(merged.VNCKeys, otherPin) {
					t.Fatalf("restart %d lost compatible identities across the merge chain: %+v", restart, merged)
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
	options := testOptions(t)
	keys := &memKeys{}
	options.Keystore = keys
	initial := newTestState(t, options)
	sourceName := hostSecretName("host_b", vncSystemPasswordPart("pi"))
	targetName := hostSecretName("host_a", vncSystemPasswordPart("pi"))
	if err := initial.secrets.vault.Set(sourceName, "saved Pi password"); err != nil {
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
	pin := vncHostKey{Address: "pi.example.com:5900", Kind: vncServerKeyType, Fingerprint: rdp.Fingerprint([]byte("Pi identity"))}
	if err := saveState(options.StatePath, savedState{Hosts: []savedHost{
		{ID: "host_b", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc", VNCKeys: []vncHostKey{pin}}},
		{ID: "host_a", Label: "Pi", Address: "pi.example.com", Port: 5900, savedTransport: savedTransport{Protocol: "vnc"}},
	}}); err != nil {
		t.Fatal(err)
	}
	failed := newTestState(t, options)
	if len(failed.hosts) != 2 || failed.mergedHosts || len(failed.merged) != 0 {
		t.Fatal("failed password migration discarded hosts or kept aliases to a merge that did not finish")
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
	retried := newTestState(t, options)
	if len(retried.hosts) != 1 || retried.hosts[0].CatalogID != "host_a" || !slices.Equal(retried.hosts[0].VNCKeys, []vncHostKey{pin}) {
		t.Fatal("restoring the key could not retry the saved host merge")
	}
	if got, found := vaultValue(t, retried, targetName); !found || got != "saved Pi password" {
		t.Fatal("retry did not migrate the saved system account")
	}
}
