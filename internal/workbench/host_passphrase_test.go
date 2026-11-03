package workbench

import (
	"testing"

	"motty/internal/sshconn"
)

func TestHostPassphraseOverrideIsBoundToOneHostAndKey(t *testing.T) {
	state := newTestState(t, testOptions(t))
	selected, other := state.hosts[0], state.hosts[1]
	key := "/keys/shared"
	global := passphraseSecretName(key)
	local := hostPassphraseSecretName(selected.CatalogID)
	if err := state.secrets.vault.Set(global, "shared-passphrase"); err != nil {
		t.Fatal(err)
	}
	if err := state.secrets.vault.Set(local, encodeHostPassphrase(key, "selected-passphrase")); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		owner, path, name string
	}{
		{selected.CatalogID, key, local},
		{other.CatalogID, key, global},
		{selected.CatalogID, "/keys/replaced", passphraseSecretName("/keys/replaced")},
	} {
		if got := savedPassphraseName(state.secrets.vault, test.owner, test.path); got != test.name {
			t.Errorf("%q %q: name=%q want=%q", test.owner, test.path, got, test.name)
		}
	}
	selectedSession := &Session{host: selected, identity: key, phase: sshconn.PhaseKeyPassphrasePrompt, attempt: 1}
	otherSession := &Session{host: other, identity: key, phase: sshconn.PhaseKeyPassphrasePrompt, attempt: 1}
	if got, _ := state.promptSecret(selectedSession); got != local {
		t.Fatalf("selected session read %q", got)
	}
	if got, _ := state.promptSecret(otherSession); got != global {
		t.Fatalf("other session read %q", got)
	}
}

func TestRefusedHostPassphraseDoesNotForgetTheSharedValue(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := state.hosts[0]
	key := "/keys/shared"
	local := hostPassphraseSecretName(host.CatalogID)
	global := passphraseSecretName(key)
	if err := state.secrets.vault.Set(local, encodeHostPassphrase(key, "wrong")); err != nil {
		t.Fatal(err)
	}
	if err := state.secrets.vault.Set(global, "correct"); err != nil {
		t.Fatal(err)
	}
	if !state.forgetRefusedSecret(&Session{filledSecret: local}, local) {
		t.Fatal("refused override was not forgotten")
	}
	if _, found := vaultValue(t, state, local); found {
		t.Fatal("refused override survived")
	}
	if value, found := vaultValue(t, state, global); !found || value != "correct" {
		t.Fatal("refusing an override changed the shared key password")
	}
}

func TestDeletingHostAlsoDeletesItsPassphraseOverride(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := state.hosts[0]
	name := hostPassphraseSecretName(host.CatalogID)
	if err := state.secrets.vault.Set(name, encodeHostPassphrase("/keys/shared", "secret")); err != nil {
		t.Fatal(err)
	}
	state.hostActionIDs = []uint32{host.ID}
	state.confirmDeleteHosts()
	if _, found := vaultValue(t, state, name); found {
		t.Fatal("deleted host left a passphrase override")
	}
}

func TestEditingHostPreservesItsOwnPassphraseOverride(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := state.hosts[0]
	key := writeWorkbenchIdentity(t, "id_selected", "secret")
	state.restoreIdentities([]string{key})
	host.Auth, host.Identity = authPrivateKey, state.identities[0].Fingerprint
	state.hosts[0] = host
	local := hostPassphraseSecretName(host.CatalogID)
	global := passphraseSecretName(key)
	if err := state.secrets.vault.Set(global, "shared-secret"); err != nil {
		t.Fatal(err)
	}
	if err := state.secrets.vault.Set(local, encodeHostPassphrase(key, "secret")); err != nil {
		t.Fatal(err)
	}
	state.openEditHostID(host.ID)
	if got := state.editor.passphrase.String(); got != "secret" {
		t.Fatalf("edited host did not prefill its own passphrase: %q", got)
	}
	state.Click(hitDialogSubmit)
	if value, found := vaultValue(t, state, global); !found || value != "shared-secret" {
		t.Fatal("editing selected host rewrote the shared key passphrase")
	}
}

func TestHostPassphraseOverrideAutofillsOnlyItsOwnConnection(t *testing.T) {
	state := newTestState(t, testOptions(t))
	key := "/keys/shared"
	first, second := state.hosts[0], state.hosts[1]
	if err := state.secrets.vault.Set(passphraseSecretName(key), "global"); err != nil {
		t.Fatal(err)
	}
	if err := state.secrets.vault.Set(hostPassphraseSecretName(first.CatalogID), encodeHostPassphrase(key, "local")); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		host Host
		want string
	}{{first, "local"}, {second, "global"}} {
		conn := &fakeConn{}
		session := &Session{host: test.host, identity: key, phase: sshconn.PhaseKeyPassphrasePrompt, attempt: 1, conn: conn}
		if got := state.autofill(session); got != autofillSent || len(conn.keyPassphrases) != 1 || conn.keyPassphrases[0] != test.want {
			t.Fatalf("%s: autofill=%d passphrases=%v", test.host.Label, got, conn.keyPassphrases)
		}
	}
}

func TestHostConnectionTestUsesOwnPassphraseOverride(t *testing.T) {
	state := newTestState(t, testOptions(t))
	key := writeWorkbenchIdentity(t, "id_test", "secret")
	state.restoreIdentities([]string{key})
	host := state.hosts[0]
	host.Auth, host.Identity = authPrivateKey, state.identities[0].Fingerprint
	state.hosts[0] = host
	if err := state.secrets.vault.Set(hostPassphraseSecretName(host.CatalogID), encodeHostPassphrase(key, "secret")); err != nil {
		t.Fatal(err)
	}
	state.openEditHostID(host.ID)
	state.hostTest = hostTest{conn: &fakeConn{}}
	status := sshconn.Status{Phase: sshconn.PhaseKeyPassphrasePrompt, Attempt: 1, IdentityPath: key}
	state.answerHostTestPassphrase(status)
	if conn := state.hostTest.conn.(*fakeConn); len(conn.keyPassphrases) != 1 || conn.keyPassphrases[0] != "secret" {
		t.Fatalf("host test did not use the override: %v", conn.keyPassphrases)
	}
}

func TestDefaultLoginJumpReadsItsOwnPassphraseOverride(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hostCatalog.systemUser = "local-user"
	jump := state.hosts[0]
	jump.User = ""
	state.hosts[0] = jump
	key := "/keys/shared"
	if err := state.secrets.vault.Set(hostPassphraseSecretName(jump.CatalogID), encodeHostPassphrase(key, "jump-only")); err != nil {
		t.Fatal(err)
	}
	if err := state.secrets.vault.Set(passphraseSecretName(key), "shared"); err != nil {
		t.Fatal(err)
	}
	endpoint := sshconn.Endpoint{Host: jump.Address, Port: jump.Port, User: "local-user"}
	owner := state.secretOwner(state.hosts[1], endpoint, sshconn.EndpointProxyJump)
	if owner != jump.CatalogID {
		t.Fatalf("jump passphrase belongs to %q, want %q", owner, jump.CatalogID)
	}
	if _, value, found, err := savedPassphrase(state.secrets.vault, owner, key); err != nil || !found || value != "jump-only" {
		t.Fatalf("jump passphrase lookup: value=%q found=%v err=%v", value, found, err)
	}
}

func TestConfiguredDefaultLoginJumpWinsOverExplicitDuplicate(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hostCatalog.systemUser = "local-user"
	jump, explicit, target := state.hosts[0], state.hosts[1], state.hosts[2]
	jump.User = ""
	explicit.Address, explicit.Port, explicit.User = jump.Address, jump.Port, "local-user"
	state.hosts[0], state.hosts[1] = jump, explicit
	state.proxies = []proxyRecord{{TargetID: target.ID, JumpID: jump.ID}}
	endpoint := sshconn.Endpoint{Host: jump.Address, Port: jump.Port, User: "local-user"}
	owner := state.secretOwner(target, endpoint, sshconn.EndpointProxyJump)
	if owner != jump.CatalogID {
		t.Fatalf("configured jump credentials belong to %q, want %q", owner, jump.CatalogID)
	}
}
