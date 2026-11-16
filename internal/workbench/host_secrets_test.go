package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/text"
	"motty/internal/sshconn"
)

// savedHostNamed finds a host of the state by its label.
func savedHostNamed(t *testing.T, state *State, label string) Host {
	t.Helper()
	for _, host := range state.hosts {
		if host.Label == label {
			return host
		}
	}
	t.Fatalf("no host called %q", label)
	return Host{}
}

func vaultValue(t *testing.T, state *State, name string) (string, bool) {
	t.Helper()
	value, found, err := state.secrets.vault.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	return value, found
}

func TestEditingTheAddressLeavesThePasswordWithTheHost(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if err := state.secrets.vault.Set(prodPasswordName, "source secret"); err != nil {
		t.Fatal(err)
	}
	clickHostAction(state, hitEditHost)
	state.editor.address = text.Editor{}
	typeInto(state, hitDialogAddress, "moved.example.com")
	state.Click(hitDialogSubmit)
	if state.modal != modalNone || state.hosts[0].Address != "moved.example.com" {
		t.Fatalf("the edit was not saved: modal %d, %q", state.modal, state.dialogError)
	}
	if value, found := vaultValue(t, state, prodPasswordName); !found || value != "source secret" {
		t.Fatalf("the password did not stay with the host: %q, %v", value, found)
	}
	names, err := state.secrets.vault.Names()
	if err != nil || len(names) != 1 {
		t.Fatalf("the vault holds %v, %v: editing the address must not add or move anything", names, err)
	}
}

func TestEmptyingThePasswordFieldTakesTheSavedPasswordAway(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if err := state.secrets.vault.Set(prodPasswordName, "source secret"); err != nil {
		t.Fatal(err)
	}
	clickHostAction(state, hitEditHost)
	state.editor.password = text.Editor{}
	state.Click(hitDialogSubmit)
	if _, found := vaultValue(t, state, prodPasswordName); found {
		t.Fatal("a password the user emptied out of the form was kept")
	}
}

func TestACopyStartsFromTheSourcesPasswordAndKeepsItsOwn(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if err := state.secrets.vault.Set(prodPasswordName, "source secret"); err != nil {
		t.Fatal(err)
	}
	clickHostAction(state, hitCopyHost)
	if got := state.editor.password.String(); got != "source secret" {
		t.Fatalf("the copy's form shows %q, want the source's password", got)
	}
	state.editor.address = text.Editor{}
	typeInto(state, hitDialogAddress, "prod-copy.example.com")
	state.editor.password = text.Editor{}
	typeInto(state, hitDialogPassword, "copy secret")
	state.Click(hitDialogSubmit)
	if state.modal != modalNone || len(state.hosts) != 4 {
		t.Fatalf("the copy was not saved: modal %d, %q", state.modal, state.dialogError)
	}
	copied := state.hosts[len(state.hosts)-1]
	if copied.CatalogID == prodCatalogID {
		t.Fatal("the copy has the source's identity")
	}
	if value, _ := vaultValue(t, state, hostSecretName(copied.CatalogID, hostPasswordPart)); value != "copy secret" {
		t.Fatalf("the copy's password = %q", value)
	}
	if value, _ := vaultValue(t, state, prodPasswordName); value != "source secret" {
		t.Fatalf("the source's password became %q", value)
	}
}

func TestDeletingAHostDeletesItsSecrets(t *testing.T) {
	state := newTestState(t, testOptions(t))
	for _, name := range []string{prodPasswordName, prodOTPName} {
		if err := state.secrets.vault.Set(name, "x"); err != nil {
			t.Fatal(err)
		}
	}
	other := hostSecretName(state.hosts[1].CatalogID, hostPasswordPart)
	if err := state.secrets.vault.Set(other, "keep"); err != nil {
		t.Fatal(err)
	}
	state.hostActionIDs = []uint32{state.hosts[0].ID}
	state.confirmDeleteHosts()
	if len(state.hosts) != len(sampleHosts)-1 {
		t.Fatalf("the host was not deleted: %q", state.dialogError)
	}
	for _, name := range []string{prodPasswordName, prodOTPName} {
		if _, found := vaultValue(t, state, name); found {
			t.Fatalf("%s outlived its host", name)
		}
	}
	if value, _ := vaultValue(t, state, other); value != "keep" {
		t.Fatalf("another host's password became %q", value)
	}
}

func TestAJumpHostPromptBelongsToTheSavedHostThatIsTheJumpHost(t *testing.T) {
	state := newTestState(t, testOptions(t))
	target := state.hosts[1]
	jump := state.hosts[0]
	endpoint := sshconn.Endpoint{Host: jump.Address, Port: jump.Port, User: state.login(jump)}
	if got := state.secretOwner(target, endpoint, sshconn.EndpointProxyJump); got != jump.CatalogID {
		t.Fatalf("a saved jump host's prompt belongs to %q, want %q", got, jump.CatalogID)
	}
	if got := state.secretOwner(target, endpoint, sshconn.EndpointTarget); got != target.CatalogID {
		t.Fatalf("the target's prompt belongs to %q, want %q", got, target.CatalogID)
	}
	unsaved := sshconn.Endpoint{Host: "bastion.example.com", Port: 22, User: "ops"}
	want := catalogIDFor(Host{Protocol: protocolSSH, Address: "bastion.example.com", User: "ops", Port: 22})
	if got := state.secretOwner(target, unsaved, sshconn.EndpointProxyJump); got != want {
		t.Fatalf("an unsaved jump host's prompt belongs to %q, want %q", got, want)
	}
}

func TestAPasswordTypedForAQuickConnectionIsKeptForTheHostItWouldBecome(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	parsed, err := parseConnection("me@quick.example.com:2200")
	if err != nil {
		t.Fatal(err)
	}
	before := len(world.conns)
	state.connectParsed(parsed)
	if len(world.conns) != before+1 {
		t.Fatal("the quick connection was not dialed")
	}
	fake := world.conns[len(world.conns)-1]
	fake.set(func(s *sshconn.Status) {
		s.Phase, s.Attempt = sshconn.PhasePasswordPrompt, 1
		s.Questions = []sshconn.Question{{Secret: true}}
		s.Endpoint = sshconn.Endpoint{Host: "quick.example.com", Port: 2200, User: "me"}
	})
	state.Wake()
	if state.modal != modalPassword || !state.secrets.canSave {
		t.Fatalf("modal %d, can save %v", state.modal, state.secrets.canSave)
	}
	state.Click(hitPasswordSave)
	typeInto(state, hitPasswordField, "quick secret")
	state.Click(hitPasswordSubmit)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()

	later := Host{Label: "quick.example.com", Address: "quick.example.com", User: "me", Port: 2200, Protocol: protocolSSH}
	later.finish()
	if value, found := vaultValue(t, state, hostSecretName(later.CatalogID, hostPasswordPart)); !found || value != "quick secret" {
		t.Fatalf("the quick connection's password = %q, %v: saving the connection as a host would not find it", value, found)
	}
}

// legacySecrets writes the secrets an earlier version kept under logins.
func legacySecrets(t *testing.T, state *State, secrets map[string]string) {
	t.Helper()
	for name, value := range secrets {
		if err := state.secrets.vault.Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSecretsSavedUnderALoginMoveToEveryHostThatLogsInThere(t *testing.T) {
	state := newTestState(t, testOptions(t))
	twin := Host{ID: 90, CatalogID: "host_twin", Label: "Twin", Address: "prod.example.com", User: "deploy", Port: 22, Protocol: protocolSSH}
	desk := Host{ID: 91, CatalogID: "host_desk", Label: "Desk", Address: "desk.example.com", Port: 5900, Protocol: protocolVNC}
	windows := Host{ID: 92, CatalogID: "host_windows", Label: "Windows", Address: "rdp.example.com", User: "CORP\\alice", Port: 3389, Protocol: protocolRDP}
	state.hosts = append(state.hosts, twin, desk, windows)
	legacySecrets(t, state, map[string]string{
		"password:deploy@prod.example.com:22":           "ssh secret",
		"otp:deploy@prod.example.com:22":                "otpauth://totp/x?secret=ABC",
		"password:vnc@desk.example.com:5900":            "vnc secret",
		"password:rdp:CORP\\alice@rdp.example.com:3389": "rdp secret",
		"aikey:untouched":                               "sk-1",
	})

	state.migrateSecretsToHosts(true)

	want := map[string]string{
		hostSecretName(prodCatalogID, hostPasswordPart):  "ssh secret",
		hostSecretName("host_twin", hostPasswordPart):    "ssh secret",
		hostSecretName(prodCatalogID, hostOTPPart):       "otpauth://totp/x?secret=ABC",
		hostSecretName("host_twin", hostOTPPart):         "otpauth://totp/x?secret=ABC",
		hostSecretName("host_desk", hostPasswordPart):    "vnc secret",
		hostSecretName("host_windows", hostPasswordPart): "rdp secret",
		"aikey:untouched": "sk-1",
	}
	for name, value := range want {
		if got, found := vaultValue(t, state, name); !found || got != value {
			t.Errorf("%s = %q, %v; want %q", name, got, found, value)
		}
	}
	names, _ := state.secrets.vault.Names()
	if len(names) != len(want) {
		t.Errorf("the vault holds %v: the endpoint-named records should be gone", names)
	}
}

func TestSecretsSavedForAConnectionNeverSavedAsAHostGoToThatConnection(t *testing.T) {
	state := newTestState(t, testOptions(t))
	legacySecrets(t, state, map[string]string{"password:me@quick.example.com:2200": "quick secret"})
	state.migrateSecretsToHosts(true)
	adhoc := Host{Label: "quick.example.com", Address: "quick.example.com", User: "me", Port: 2200, Protocol: protocolSSH}
	adhoc.finish()
	if value, found := vaultValue(t, state, hostSecretName(adhoc.CatalogID, hostPasswordPart)); !found || value != "quick secret" {
		t.Fatalf("the orphan became %q, %v", value, found)
	}
	if _, found := vaultValue(t, state, "password:me@quick.example.com:2200"); found {
		t.Fatal("the old record was left behind")
	}
}

func TestTheMoveWaitsForAnOldMasterPasswordAndRunsOnceItIsGiven(t *testing.T) {
	state := newTestState(t, testOptions(t))
	legacySecrets(t, state, map[string]string{"password:deploy@prod.example.com:22": "ssh secret"})
	if err := state.secrets.vault.LegacyForTests("long enough one"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(filepath.Dir(state.options.StatePath), secretsLayoutFile)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	state.migrateSecretsToHosts(false)
	if !state.secrets.vault.NeedsConversion() {
		t.Fatal("the move opened a vault that still needs its master password")
	}

	state.Wake()
	typeInto(state, hitVaultUnlockField, "long enough one")
	state.Click(hitVaultUnlockSubmit)
	if value, found := vaultValue(t, state, prodPasswordName); !found || value != "ssh secret" {
		t.Fatalf("after converting the password is %q, %v", value, found)
	}
	if _, found := vaultValue(t, state, "password:deploy@prod.example.com:22"); found {
		t.Fatal("the old record was left behind")
	}
}

func TestOnceMovedOnlyARestoreMovesAgain(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.migrateSecretsToHosts(false) // an empty vault: marks the move as made
	legacySecrets(t, state, map[string]string{"password:deploy@prod.example.com:22": "late"})
	state.migrateSecretsToHosts(false)
	if _, found := vaultValue(t, state, prodPasswordName); found {
		t.Fatal("a start moved secrets after the move was made")
	}
	state.migrateSecretsToHosts(true)
	if value, found := vaultValue(t, state, prodPasswordName); !found || value != "late" {
		t.Fatalf("a restore did not move a backup's old records: %q, %v", value, found)
	}
}
