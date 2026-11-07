package workbench

import (
	"os"
	"strings"
	"testing"

	"github.com/vivaui/viva/text"
	"motty/internal/sshconn"
)

// addCredential makes a shared password through Settings, the way the user does.
func addCredential(t *testing.T, state *State, name, password string) credential {
	t.Helper()
	state.activeSection = sectionSettings
	state.settingsCategory = settingsSecurity
	state.modal = modalNone
	state.Click(hitSettingsCredentialAdd)
	typeInto(state, hitCredentialName, name)
	typeInto(state, hitCredentialPassword, password)
	state.Click(hitCredentialSubmit)
	if state.modal != modalNone {
		t.Fatalf("the dialog stayed open: %q", state.credentialForm.dialogError)
	}
	entry, ok := state.credentials[len(state.credentials)-1], true
	if !ok || entry.Name != name {
		t.Fatalf("credentials = %+v", state.credentials)
	}
	return entry
}

// useCredential edits a host so that it takes its password from a saved one.
func useCredential(t *testing.T, state *State, host Host, entry credential) {
	t.Helper()
	state.activeSection = sectionHosts
	state.Click(hitHostList.At(int(host.ID)))
	state.Click(hitEditHost)
	index := -1
	for position, have := range state.credentials {
		if have.ID == entry.ID {
			index = position
		}
	}
	state.Click(hitDialogCredential)
	state.Click(hitDialogCredentialItemList.At(index))
	state.Click(hitDialogSubmit)
	if state.modal != modalNone {
		t.Fatalf("the host was not saved: %q", state.dialogError)
	}
}

func hostLabeled(t *testing.T, state *State, label string) Host {
	t.Helper()
	return savedHostNamed(t, state, label)
}

func askStagingForPassword(state *State, fake *fakeConn, attempt int) {
	fake.set(func(s *sshconn.Status) {
		s.Phase, s.Attempt = sshconn.PhasePasswordPrompt, attempt
		s.Questions = []sshconn.Question{{Secret: true}}
		s.Endpoint = sshconn.Endpoint{Host: "staging.example.com", Port: 22, User: "deploy"}
	})
	state.Wake()
}

func connectHost(t *testing.T, state *State, world *world, label string) *fakeConn {
	t.Helper()
	state.activeSection = sectionHosts
	state.selectedHostID = hostLabeled(t, state, label).ID
	return connect(t, state, world)
}

func TestASavedPasswordIsKeptInTheVaultNotTheStateFile(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	entry := addCredential(t, state, "Internal", "s3cret-shared")
	if value, found := vaultValue(t, state, credentialSecretName(entry.ID)); !found || value != "s3cret-shared" {
		t.Fatalf("the vault holds %q, %v", value, found)
	}
	data, err := os.ReadFile(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "s3cret-shared") {
		t.Fatal("the password reached the state file")
	}
	restarted := newTestState(t, options)
	if len(restarted.credentials) != 1 || restarted.credentials[0] != entry {
		t.Fatalf("after a restart: %+v", restarted.credentials)
	}
}

func TestHostsShareASavedPasswordAndChangingItReachesBoth(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	entry := addCredential(t, state, "Internal", "first-password")
	useCredential(t, state, hostLabeled(t, state, "Production"), entry)
	useCredential(t, state, hostLabeled(t, state, "Staging"), entry)
	if got := hostLabeled(t, state, "Staging").Credential; got != entry.ID {
		t.Fatalf("Staging uses %q", got)
	}
	if _, found := vaultValue(t, state, hostSecretName(hostLabeled(t, state, "Production").CatalogID, hostPasswordPart)); found {
		t.Fatal("choosing a shared password wrote a password of the host's own")
	}

	prod := connectHost(t, state, world, "Production")
	askForPassword(state, prod, 1)
	if state.modal != modalNone || len(prod.passwords) != 1 || prod.passwords[0] != "first-password" {
		t.Fatalf("Production: modal %d, sent %v", state.modal, prod.passwords)
	}
	staging := connectHost(t, state, world, "Staging")
	askStagingForPassword(state, staging, 1)
	if len(staging.passwords) != 1 || staging.passwords[0] != "first-password" {
		t.Fatalf("Staging sent %v", staging.passwords)
	}

	// One change, both follow.
	state.activeSection = sectionSettings
	state.settingsCategory = settingsSecurity
	state.Click(hitCredentialEditList.At(0))
	if got := state.credentialForm.password.String(); got != "first-password" {
		t.Fatalf("the edit dialog shows %q, want what is saved", got)
	}
	state.credentialForm.password = text.Editor{}
	typeInto(state, hitCredentialPassword, "second-password")
	state.Click(hitCredentialSubmit)
	if state.modal != modalNone {
		t.Fatalf("the edit was refused: %q", state.credentialForm.dialogError)
	}
	again := connectHost(t, state, world, "Production")
	askForPassword(state, again, 1)
	if len(again.passwords) != 1 || again.passwords[0] != "second-password" {
		t.Fatalf("after the change Production sent %v", again.passwords)
	}
}

func TestTheUseOfASavedPasswordSurvivesARestart(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	entry := addCredential(t, state, "Internal", "pw")
	useCredential(t, state, hostLabeled(t, state, "Router"), entry)
	restarted := newTestState(t, options)
	if got := hostLabeled(t, restarted, "Router").Credential; got != entry.ID {
		t.Fatalf("Router uses %q after a restart, want %q", got, entry.ID)
	}
}

func TestASharedPasswordTheServerRefusesIsKeptForTheOtherHosts(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	entry := addCredential(t, state, "Internal", "pw")
	useCredential(t, state, hostLabeled(t, state, "Production"), entry)

	fake := connectHost(t, state, world, "Production")
	askForPassword(state, fake, 1)
	askForPassword(state, fake, 2) // this host's password is different
	if state.modal != modalPassword {
		t.Fatalf("modal %d, want the password dialog", state.modal)
	}
	if value, found := vaultValue(t, state, credentialSecretName(entry.ID)); !found || value != "pw" {
		t.Fatalf("one host's refusal changed the shared password: %q, %v", value, found)
	}
	if state.secrets.canSave {
		t.Fatal("the prompt offered to overwrite a shared password with what one host was given")
	}
}

func TestDeletingASavedPasswordReleasesItsHostsAndRemovesTheSecret(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	entry := addCredential(t, state, "Internal", "pw")
	useCredential(t, state, hostLabeled(t, state, "Production"), entry)

	state.Click(hitSettings)
	state.activeSection = sectionSettings
	state.settingsCategory = settingsSecurity
	state.Click(hitCredentialDeleteList.At(0))
	if state.modal != modalDeleteRule || state.ruleDelete.uses != 1 {
		t.Fatalf("modal %d, uses %d", state.modal, state.ruleDelete.uses)
	}
	state.Click(hitRuleDeleteConfirm)
	if len(state.credentials) != 0 {
		t.Fatalf("credentials = %+v", state.credentials)
	}
	if _, found := vaultValue(t, state, credentialSecretName(entry.ID)); found {
		t.Fatal("the secret of a deleted shared password stayed in the vault")
	}
	if got := hostLabeled(t, state, "Production").Credential; got != "" {
		t.Fatalf("Production still names the deleted password: %q", got)
	}
}

func TestAHostWhoseSavedPasswordIsGoneFallsBackToItsOwn(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	prod := hostLabeled(t, state, "Production")
	state.hosts[state.hostIndex(prod.ID)].Credential = "cred_missing"
	if err := state.secrets.vault.Set(prodPasswordName, "own"); err != nil {
		t.Fatal(err)
	}
	fake := connectHost(t, state, world, "Production")
	askForPassword(state, fake, 1)
	if len(fake.passwords) != 1 || fake.passwords[0] != "own" {
		t.Fatalf("sent %v, want the host's own password", fake.passwords)
	}
}

func TestASavedPasswordNeedsANameAndAPassword(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	addCredential(t, state, "Internal", "pw")
	state.Click(hitSettingsCredentialAdd)
	state.Click(hitCredentialSubmit)
	if state.modal != modalCredential || state.credentialForm.dialogError != state.messages.Credentials.ErrorName {
		t.Fatalf("an empty name: modal %d, %q", state.modal, state.credentialForm.dialogError)
	}
	typeInto(state, hitCredentialName, "internal") // the same name in another case
	typeInto(state, hitCredentialPassword, "x")
	state.Click(hitCredentialSubmit)
	if state.credentialForm.dialogError != state.messages.Credentials.ErrorNameTaken {
		t.Fatalf("a repeated name: %q", state.credentialForm.dialogError)
	}
	state.credentialForm.name = text.Editor{}
	typeInto(state, hitCredentialName, "Other")
	state.credentialForm.password = text.Editor{}
	state.Click(hitCredentialSubmit)
	if state.credentialForm.dialogError != state.messages.Credentials.ErrorPassword {
		t.Fatalf("an empty password: %q", state.credentialForm.dialogError)
	}
}

func TestTheHostFormOffersNoChoiceWhileNothingIsSaved(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	clickHostAction(state, hitEditHost)
	if state.hostOffersCredential() {
		t.Fatal("the form offers a shared password that does not exist")
	}
	state.modal = modalNone
	addCredential(t, state, "Internal", "pw")
	state.activeSection = sectionHosts
	clickHostAction(state, hitEditHost)
	if !state.hostOffersCredential() {
		t.Fatal("the form does not offer the shared password")
	}
	state.editor.auth = authPrivateKey
	if state.hostOffersCredential() {
		t.Fatal("a host that signs in with a key was offered a password")
	}
}
