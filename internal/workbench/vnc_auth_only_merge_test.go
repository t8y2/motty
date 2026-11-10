package workbench

import (
	"path/filepath"
	"testing"

	"motty/internal/vault"
)

func TestVNCAuthOnlyPermissionsKeepAccountsSeparateAcrossRestart(t *testing.T) {
	for _, allowFirst := range []bool{false, true} {
		name := "allowing copy has larger ID"
		if allowFirst {
			name = "allowing copy has smaller ID"
		}
		t.Run(name, func(t *testing.T) {
			options := testOptions(t)
			initial := newTestState(t, options)
			for name, value := range map[string]string{
				hostSecretName("host_a", vncSystemPasswordPart("pi")):    "first Pi password",
				hostSecretName("host_b", vncSystemPasswordPart("pi")):    "second Pi password",
				hostSecretName("host_a", vncSystemPasswordPart("alice")): "Alice password",
				hostSecretName("host_b", vncSystemPasswordPart("bob")):   "Bob password",
			} {
				if err := initial.secrets.vault.Set(name, value); err != nil {
					t.Fatal(err)
				}
			}
			initial.Close()
			if err := saveState(options.StatePath, savedState{Hosts: []savedHost{
				{ID: "host_b", Label: "Pi", Address: "pi.example.com", User: "pi", Port: 5900, savedTransport: savedTransport{Protocol: "vnc", VNCAuthOnly: !allowFirst}},
				{ID: "host_a", Label: "Pi", Address: "pi.example.com", User: "pi", Port: 5900, savedTransport: savedTransport{Protocol: "vnc", VNCAuthOnly: allowFirst}},
			}}); err != nil {
				t.Fatal(err)
			}
			for restart := 0; restart < 2; restart++ {
				state := newTestState(t, options)
				if len(state.hosts) != 2 {
					t.Fatalf("restart %d merged hosts with different encryption permissions: %+v", restart, state.hosts)
				}
				for _, want := range []struct {
					id, password, ownAccount, otherAccount string
					allow                                  bool
				}{
					{"host_a", "first Pi password", "alice", "bob", allowFirst},
					{"host_b", "second Pi password", "bob", "alice", !allowFirst},
				} {
					host, ok := state.hostByCatalogID(want.id)
					if !ok || host.VNCAuthOnly != want.allow || host.User != "pi" {
						t.Fatalf("restart %d changed host %q permission or identity: %+v", restart, want.id, host)
					}
					if got, found := vaultValue(t, state, hostSecretName(want.id, vncSystemPasswordPart("pi"))); !found || got != want.password {
						t.Fatalf("restart %d changed host %q password: %q found=%v", restart, want.id, got, found)
					}
					if _, found := vaultValue(t, state, hostSecretName(want.id, vncSystemPasswordPart(want.ownAccount))); !found {
						t.Fatalf("restart %d removed host %q own account", restart, want.id)
					}
					if _, found := vaultValue(t, state, hostSecretName(want.id, vncSystemPasswordPart(want.otherAccount))); found {
						t.Fatalf("restart %d moved another host's account to %q", restart, want.id)
					}
				}
				if err := state.saveWithResult(); err != nil {
					t.Fatal(err)
				}
				state.Close()
				vault.Forget(filepath.Join(filepath.Dir(options.StatePath), "vault.json"))
			}
		})
	}
}

func TestVNCAuthOnlyPermissionDistinguishesImportedAndEditedHosts(t *testing.T) {
	state := newTestState(t, testOptions(t))
	document := hostCatalogDocument{Hosts: []savedHost{
		{ID: "host_a", Label: "Pi", Address: "pi.example.com", User: "pi", Port: 5900, savedTransport: savedTransport{Protocol: "vnc"}},
		{ID: "host_b", Label: "Pi", Address: "pi.example.com", User: "pi", Port: 5900, savedTransport: savedTransport{Protocol: "vnc", VNCAuthOnly: true}},
	}}
	if added, duplicates := state.applyHostCatalog(document); added != 2 || duplicates != 0 {
		t.Fatalf("import with different permissions: added=%d duplicates=%d", added, duplicates)
	}
	if added, duplicates := state.applyHostCatalog(document); added != 0 || duplicates != 2 {
		t.Fatalf("repeat import: added=%d duplicates=%d", added, duplicates)
	}
	host, ok := state.hostByCatalogID("host_b")
	if !ok {
		t.Fatal("import lost the host allowing authentication-only encryption")
	}
	state.selectedHostID = host.ID
	clickHostAction(state, hitEditHost)
	state.Click(hitDialogSubmit)
	if state.modal != modalNone {
		t.Fatalf("editing a host with different permission was blocked: %q", state.dialogError)
	}
	host, ok = state.hostByCatalogID("host_b")
	if !ok || !host.VNCAuthOnly {
		t.Fatal("editing merged the host or removed its permission")
	}
	clickHostAction(state, hitEditHost)
	state.clickHostProtocol(hitDialogVNCAuthOnly)
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError == "" {
		t.Fatal("editing to match the existing host's permission accepted a duplicate")
	}
	host, ok = state.hostByCatalogID("host_b")
	if !ok || !host.VNCAuthOnly {
		t.Fatal("a rejected duplicate edit changed the saved permission")
	}
}
