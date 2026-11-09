package workbench

import (
	"strings"
	"sync"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
	"motty/internal/vault"
)

// memKeys is the system's secret store in memory, which a test can take away.
type memKeys struct {
	mu          sync.Mutex
	items       map[[2]string][]byte
	unavailable bool
	loads       int
}

func (keys *memKeys) Store(service, account string, secret []byte) error {
	keys.mu.Lock()
	defer keys.mu.Unlock()
	if keys.unavailable {
		return vault.ErrKeystoreUnavailable
	}
	if keys.items == nil {
		keys.items = make(map[[2]string][]byte)
	}
	keys.items[[2]string{service, account}] = append([]byte(nil), secret...)
	return nil
}

func (keys *memKeys) Load(service, account string) ([]byte, error) {
	keys.mu.Lock()
	defer keys.mu.Unlock()
	keys.loads++
	if keys.unavailable {
		return nil, vault.ErrKeystoreUnavailable
	}
	secret, ok := keys.items[[2]string{service, account}]
	if !ok {
		return nil, vault.ErrKeystoreNotFound
	}
	return append([]byte(nil), secret...), nil
}

func (keys *memKeys) loadCount() int {
	keys.mu.Lock()
	defer keys.mu.Unlock()
	return keys.loads
}

func (keys *memKeys) Delete(service, account string) error {
	keys.mu.Lock()
	defer keys.mu.Unlock()
	delete(keys.items, [2]string{service, account})
	return nil
}

// The saved login of the host the tests connect to, and where its secrets are
// kept.
var (
	prodCatalogID    = catalogIDFor(Host{Protocol: protocolSSH, Address: "prod.example.com", User: "deploy", Port: 22})
	prodPasswordName = hostSecretName(prodCatalogID, hostPasswordPart)
)

func askForPassword(state *State, fake *fakeConn, attempt int) {
	fake.set(func(s *sshconn.Status) {
		s.Phase, s.Attempt = sshconn.PhasePasswordPrompt, attempt
		s.Questions = []sshconn.Question{{Secret: true}}
		s.Endpoint = sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}
	})
	state.Wake()
}

// signInSavingThePassword connects Production and answers its password prompt with
// the box ticked, until the server accepts it.
func signInSavingThePassword(t *testing.T, state *State, world *world, password string) *fakeConn {
	t.Helper()
	fake := connect(t, state, world)
	askForPassword(state, fake, 1)
	if state.modal != modalPassword || !state.secrets.canSave {
		t.Fatalf("modal %d, can save %v", state.modal, state.secrets.canSave)
	}
	state.Click(hitPasswordSave)
	typeInto(state, hitPasswordField, password)
	state.Click(hitPasswordSubmit)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	return fake
}

func TestAPasswordTheUserAsksToSaveIsKeptOnceTheServerAcceptsItAndAnswersTheNextPrompt(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	first := signInSavingThePassword(t, state, world, "hunter2")
	if len(first.passwords) != 1 || first.passwords[0] != "hunter2" {
		t.Fatalf("passwords sent = %v", first.passwords)
	}
	if got, found, err := state.secrets.vault.Get(prodPasswordName); err != nil || !found || got != "hunter2" {
		t.Fatalf("the vault holds %q, %v, %v", got, found, err)
	}
	if !strings.Contains(state.status, "deploy@prod.example.com") {
		t.Fatalf("status = %q", state.status)
	}

	// Another connection to the same account is answered without a dialog.
	state.Click(hitHosts)
	second := connect(t, state, world)
	askForPassword(state, second, 1)
	if state.modal != modalNone {
		t.Fatalf("a saved password still opened the dialog (modal %d)", state.modal)
	}
	if len(second.passwords) != 1 || second.passwords[0] != "hunter2" {
		t.Fatalf("the saved password was not sent: %v", second.passwords)
	}
}

func TestAPasswordNobodyAskedToSaveIsNotSaved(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	askForPassword(state, fake, 1)
	typeInto(state, hitPasswordField, "hunter2") // the box stays clear
	state.Click(hitPasswordSubmit)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	if state.secrets.vault.Len() != 0 {
		t.Fatal("a password was saved that nobody asked to save")
	}
}

func TestAWrongPasswordIsNeverSaved(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	askForPassword(state, fake, 1)
	state.Click(hitPasswordSave)
	typeInto(state, hitPasswordField, "wrong")
	state.Click(hitPasswordSubmit)
	askForPassword(state, fake, 2) // refused
	if state.secrets.vault.Len() != 0 {
		t.Fatal("a refused password was saved")
	}
	if state.secrets.save {
		t.Fatal("the box stayed ticked for the next attempt")
	}
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseFailed; s.Err = sshconn.ErrAuthFailed })
	state.Wake()
	if state.secrets.vault.Len() != 0 {
		t.Fatal("a password was saved from a failed connection")
	}
}

func TestASavedPasswordTheServerRefusesIsForgottenAndTheUserIsAsked(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	signInSavingThePassword(t, state, world, "old password")

	state.Click(hitHosts)
	second := connect(t, state, world)
	askForPassword(state, second, 1)
	if len(second.passwords) != 1 {
		t.Fatalf("the saved password was not tried: %v", second.passwords)
	}
	askForPassword(state, second, 2) // the account's password has changed
	if state.modal != modalPassword || state.passwordError != state.messages.PasswordSavedRefused {
		t.Fatalf("modal %d, error %q", state.modal, state.passwordError)
	}
	if _, found, _ := state.secrets.vault.Get(prodPasswordName); found {
		t.Fatal("a refused saved password was kept")
	}
	if len(second.passwords) != 1 {
		t.Fatalf("the refused password was tried again: %v", second.passwords)
	}
}

func TestASecondFactorRoundKeepsTheSavedPasswordAndReportsNoError(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	signInSavingThePassword(t, state, world, "hunter2")

	state.Click(hitHosts)
	second := connect(t, state, world)
	askForPassword(state, second, 1)
	if len(second.passwords) != 1 || second.passwords[0] != "hunter2" {
		t.Fatalf("the saved password was not tried: %v", second.passwords)
	}
	// The password was accepted; the server moved on to its second factor.
	second.set(func(s *sshconn.Status) {
		s.Phase, s.Attempt = sshconn.PhasePasswordPrompt, 2
		s.Questions = []sshconn.Question{{Text: "Please enter the MFA verification code: ", Secret: true}}
		s.Endpoint = sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}
	})
	state.Wake()
	if state.modal != modalPassword || state.passwordError != "" {
		t.Fatalf("modal %d, error %q; the second factor was mistaken for a refused password", state.modal, state.passwordError)
	}
	if got, found, _ := state.secrets.vault.Get(prodPasswordName); !found || got != "hunter2" {
		t.Fatalf("the saved password did not survive the second-factor round: %q, %v", got, found)
	}
}

func TestAKeyPassphraseCanBeSavedAndAnswersTheNextQuestion(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	ask := func(fake *fakeConn) {
		fake.set(func(s *sshconn.Status) {
			s.Phase = sshconn.PhaseKeyPassphrasePrompt
			s.Attempt = 1
			s.IdentityPath = "/home/me/.ssh/id_work"
		})
		state.Wake()
	}
	ask(fake)
	if state.modal != modalKeyPassphrase || !state.secrets.canSave {
		t.Fatalf("modal %d, can save %v", state.modal, state.secrets.canSave)
	}
	state.Click(hitKeyPassphraseSave)
	typeInto(state, hitKeyPassphraseField, "open sesame")
	state.Click(hitKeyPassphraseSubmit)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	if got, found, _ := state.secrets.vault.Get("passphrase:/home/me/.ssh/id_work"); !found || got != "open sesame" {
		t.Fatalf("the vault holds %q, %v", got, found)
	}

	state.Click(hitHosts)
	second := connect(t, state, world)
	ask(second)
	if state.modal != modalNone || len(second.keyPassphrases) != 1 || second.keyPassphrases[0] != "open sesame" {
		t.Fatalf("modal %d, passphrases sent %v", state.modal, second.keyPassphrases)
	}
}

func TestAnOneTimeCodeQuestionIsNeverAnsweredFromTheVault(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	signInSavingThePassword(t, state, world, "hunter2")
	state.Click(hitHosts)
	second := connect(t, state, world)
	second.set(func(s *sshconn.Status) {
		s.Phase = sshconn.PhasePasswordPrompt
		s.Attempt = 1
		s.Questions = []sshconn.Question{{Text: "Verification code:", Secret: true}}
	})
	state.Wake()
	if state.modal != modalPassword || len(second.passwords) != 0 {
		t.Fatalf("modal %d, sent %v: a code question must reach the user", state.modal, second.passwords)
	}
}

func TestTheBoxToSaveIsOfferedOnlyWhereSavingCanWork(t *testing.T) {
	world, options := newWorld(t)
	options.Keystore = &memKeys{unavailable: true}
	state := newTestState(t, options)
	fake := connect(t, state, world)
	askForPassword(state, fake, 1)
	if state.secrets.canSave {
		t.Fatal("the box was offered on a system with no secret store and no key file")
	}
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitPasswordSave); ok {
		t.Fatal("the box was painted")
	}
}

func TestAKeyFileLetsASystemWithoutASecretStoreSavePasswords(t *testing.T) {
	world, options := newWorld(t)
	options.Keystore = vault.FallbackKeystore{System: &memKeys{unavailable: true}, File: vault.FileKeystore{Path: t.TempDir() + "/vault.key"}}
	state := newTestState(t, options)
	fake := connect(t, state, world)
	askForPassword(state, fake, 1)
	if !state.secrets.canSave {
		t.Fatal("the box was not offered although a key file can keep the key")
	}
}

func TestAVaultFromAnEarlierVersionAsksForItsMasterPasswordThenAnswers(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	signInSavingThePassword(t, state, world, "hunter2")
	if err := state.secrets.vault.LegacyForTests("master password"); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.DropKeyForTests()

	state.Click(hitHosts)
	second := connect(t, state, world)
	askForPassword(state, second, 1)
	if state.modal != modalVaultUnlock {
		t.Fatalf("modal = %d, want the unlock dialog", state.modal)
	}
	typeInto(state, hitVaultUnlockField, "not it")
	state.Click(hitVaultUnlockSubmit)
	if state.modal != modalVaultUnlock || state.secrets.dialogError != state.messages.VaultWrongPassword {
		t.Fatalf("modal %d, error %q", state.modal, state.secrets.dialogError)
	}
	typeInto(state, hitVaultUnlockField, "master password")
	state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitVaultUnlockField})
	if state.modal != modalNone || len(second.passwords) != 1 || second.passwords[0] != "hunter2" {
		t.Fatalf("modal %d, sent %v: unlocking should have answered the question", state.modal, second.passwords)
	}
}

func TestDecliningToUnlockFallsBackToTheNormalDialogAndDoesNotAskAgain(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	signInSavingThePassword(t, state, world, "hunter2")
	state.secrets.vault.LegacyForTests("master password")
	state.secrets.vault.DropKeyForTests()

	state.Click(hitHosts)
	second := connect(t, state, world)
	askForPassword(state, second, 1)
	state.Click(hitVaultUnlockCancel)
	if state.modal != modalPassword {
		t.Fatalf("modal = %d, want the password dialog", state.modal)
	}
	state.Click(hitPasswordCancel)
	state.Click(hitHosts)
	third := connect(t, state, world)
	askForPassword(state, third, 1)
	if state.modal != modalPassword {
		t.Fatalf("modal = %d: the window asked to unlock again after it was declined", state.modal)
	}
}

func TestAVaultStillKeptByAnOldMasterPasswordIsConvertedFromSettings(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	signInSavingThePassword(t, state, world, "hunter2")
	if err := state.secrets.vault.LegacyForTests("long enough one"); err != nil {
		t.Fatal(err)
	}
	state.Click(hitSettings)
	if !settingsShows(state, hitSettingsVaultConvert) {
		t.Fatal("Settings does not offer to enter the old master password")
	}
	state.Click(hitSettingsVaultConvert)
	if state.modal != modalVaultUnlock {
		t.Fatalf("modal = %d, want the dialog for the old master password", state.modal)
	}
	typeInto(state, hitVaultUnlockField, "long enough one")
	state.Click(hitVaultUnlockSubmit)
	if state.modal != modalNone || state.secrets.vault.NeedsConversion() {
		t.Fatalf("modal %d, still needs conversion %v", state.modal, state.secrets.vault.NeedsConversion())
	}
	if value, found, err := state.secrets.vault.Get(prodPasswordName); err != nil || !found || value != "hunter2" {
		t.Fatalf("after converting: %q, %v, %v", value, found, err)
	}
	if settingsShows(state, hitSettingsVaultConvert) {
		t.Fatal("Settings still offers the old master password after it was converted")
	}
}

func TestAnOldMasterPasswordIsAskedForOnceAtStartAndOnlyOnce(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	signInSavingThePassword(t, state, world, "hunter2")
	if err := state.secrets.vault.LegacyForTests("long enough one"); err != nil {
		t.Fatal(err)
	}
	state.Wake()
	if state.modal != modalVaultUnlock {
		t.Fatalf("modal = %d: a vault from an earlier version was not opened by asking for its password", state.modal)
	}
	state.Click(hitVaultUnlockCancel)
	state.Wake()
	if state.modal != modalNone {
		t.Fatalf("modal = %d: the window asked again after it was declined", state.modal)
	}
}

func TestForgetAllAsksFirstThenEmptiesTheVault(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	signInSavingThePassword(t, state, world, "hunter2")
	state.Click(hitSettings)
	state.Click(hitSettingsVaultForget)
	if state.modal != modalVaultForget || state.secrets.vault.Len() != 1 {
		t.Fatalf("modal %d, saved %d", state.modal, state.secrets.vault.Len())
	}
	state.Click(hitVaultForgetCancel)
	if state.secrets.vault.Len() != 1 {
		t.Fatal("cancelling forgot the passwords")
	}
	state.Click(hitSettingsVaultForget)
	state.Click(hitVaultForgetConfirm)
	if state.secrets.vault.Len() != 0 || state.status != state.messages.VaultCleared {
		t.Fatalf("saved %d, status %q", state.secrets.vault.Len(), state.status)
	}
}

func TestARestoredTabConnectsItselfWhenTheVaultHasItsPassword(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	signInSavingThePassword(t, state, world, "hunter2")
	password := []sshconn.Question{{Secret: true}}
	session := &state.sessions[0]
	if !state.canAutofill(session, sshconn.Status{Phase: sshconn.PhasePasswordPrompt, Attempt: 1, Questions: password, Endpoint: sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}}) {
		t.Fatal("the vault does not answer the question a restore would meet")
	}
	for _, status := range []sshconn.Status{
		{Phase: sshconn.PhasePasswordPrompt, Attempt: 2, Questions: password, Endpoint: sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}},
		{Phase: sshconn.PhasePasswordPrompt, Attempt: 1, Questions: []sshconn.Question{{Text: "Code:", Secret: true}}, Endpoint: sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}},
		{Phase: sshconn.PhasePasswordPrompt, Attempt: 1, Questions: password, EndpointRole: sshconn.EndpointProxyJump, Endpoint: sshconn.Endpoint{Host: "other.example.com", Port: 22, User: "deploy"}},
		{Phase: sshconn.PhaseHostKeyPrompt, Attempt: 1},
	} {
		if state.canAutofill(session, status) {
			t.Fatalf("restore would count on the vault for %+v", status)
		}
	}
}

// settingsShows looks for a control on every category of the settings page, from
// the top down.
func settingsShows(state *State, id gfx.ID) bool {
	for category := settingsCategory(0); category < settingsCategoryCount; category++ {
		state.selectSettingsCategory(category)
		for range 60 {
			canvas, _ := paintedTexts(state)
			if _, ok := hitByID(canvas, id); ok {
				return true
			}
			state.scrollSettings(gfx.ScrollEvent{DeltaY: 120, Precise: true})
		}
	}
	return false
}

func TestPasswordHintMovesBesideTheSaveBox(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	endpoint := sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}
	fake.set(func(s *sshconn.Status) {
		s.Phase, s.Attempt, s.Endpoint = sshconn.PhasePasswordPrompt, 2, endpoint
		s.Questions = []sshconn.Question{{Secret: true}}
	})
	state.Wake()
	canvas, texts := paintedTexts(state)
	if !contains(texts, state.messages.PasswordRetry) {
		t.Fatalf("error line missing from %v", texts)
	}
	if contains(texts, state.messages.PasswordHint) {
		t.Fatalf("the instruction still overlaps the error line: %v", texts)
	}
	hit, ok := hitByID(canvas, hitPasswordHint)
	if !ok || hit.Hint != state.messages.PasswordHint {
		t.Fatalf("help hit = %+v, %v", hit, ok)
	}
	input, _ := hitByID(canvas, hitPasswordField)
	saveRow, _ := hitByID(canvas, hitPasswordSave)
	if gap := saveRow.Bounds.Y - (input.Bounds.Y + input.Bounds.Height); gap > 40 {
		t.Fatalf("password field to save row gap = %v, want a compact form gap", gap)
	}
}

// Most OpenSSH servers take the password through keyboard-interactive, whose
// prompt has wording ("Password: "); the saved password answers it as it
// answers the plain password method.
func TestASavedPasswordAnswersAKeyboardInteractivePasswordPrompt(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	ask := func(fake *fakeConn, question string) {
		fake.set(func(s *sshconn.Status) {
			s.Phase, s.Attempt = sshconn.PhasePasswordPrompt, 1
			s.Questions = []sshconn.Question{{Text: question, Secret: true}}
			s.Endpoint = sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}
		})
		state.Wake()
	}
	ask(fake, "Password: ")
	if state.modal != modalPassword || !state.secrets.canSave {
		t.Fatalf("modal %d, can save %v", state.modal, state.secrets.canSave)
	}
	state.Click(hitPasswordSave)
	typeInto(state, hitPasswordField, "hunter2")
	state.Click(hitPasswordSubmit)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()

	state.Click(hitHosts)
	second := connect(t, state, world)
	ask(second, "Password: ")
	if state.modal != modalNone {
		t.Fatalf("a saved password still opened the dialog (modal %d)", state.modal)
	}
	if len(second.passwords) != 1 || second.passwords[0] != "hunter2" {
		t.Fatalf("the saved password was not sent: %v", second.passwords)
	}

	// A hidden question that is not asking for the password is the user's to
	// answer; guessing would be refused and the refusal would forget the password.
	state.Click(hitHosts)
	third := connect(t, state, world)
	ask(third, "Enter your YubiKey PIN: ")
	if state.modal != modalPassword || len(third.passwords) != 0 {
		t.Fatalf("modal %d, passwords sent %v; a PIN was answered with the password", state.modal, third.passwords)
	}
	if _, found, _ := state.secrets.vault.Get(prodPasswordName); !found {
		t.Fatal("the saved password was lost")
	}
}
