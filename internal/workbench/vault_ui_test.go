package workbench

import (
	"strings"
	"sync"
	"testing"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
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

const prodPasswordName = "password:deploy@prod.example.com:22"

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
		t.Fatal("the box was offered on a system with no secret store and no master password")
	}
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitPasswordSave); ok {
		t.Fatal("the box was painted")
	}
	// With a master password it can save without a store.
	if err := state.secrets.vault.SetMasterPassword("a master password"); err != nil {
		t.Fatal(err)
	}
	state.Click(hitPasswordCancel)
	state.Click(hitHosts)
	fake2 := connect(t, state, world)
	askForPassword(state, fake2, 1)
	if !state.secrets.canSave {
		t.Fatal("the box is not offered although the vault is unlocked")
	}
}

func TestALockedVaultAsksForTheMasterPasswordThenAnswers(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	signInSavingThePassword(t, state, world, "hunter2")
	if err := state.secrets.vault.SetMasterPassword("master password"); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.Lock()

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
	state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: hitVaultUnlockField})
	if state.modal != modalNone || len(second.passwords) != 1 || second.passwords[0] != "hunter2" {
		t.Fatalf("modal %d, sent %v: unlocking should have answered the question", state.modal, second.passwords)
	}
}

func TestDecliningToUnlockFallsBackToTheNormalDialogAndDoesNotAskAgain(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	signInSavingThePassword(t, state, world, "hunter2")
	state.secrets.vault.SetMasterPassword("master password")
	state.secrets.vault.Lock()

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

func TestSettingTheMasterPasswordFromSettingsValidatesAndLocksTheVault(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.Click(hitSettingsMasterSet)
	if state.modal != modalMasterPassword {
		t.Fatalf("modal = %d", state.modal)
	}
	typeInto(state, hitMasterPasswordNew, "short")
	typeInto(state, hitMasterPasswordConfirm, "short")
	state.Click(hitMasterPasswordSubmit)
	if state.modal != modalMasterPassword || !strings.Contains(state.secrets.dialogError, "8") {
		t.Fatalf("a short password: modal %d, error %q", state.modal, state.secrets.dialogError)
	}
	state.secrets.newPassword, state.secrets.confirmPassword = text.Editor{}, text.Editor{}
	typeInto(state, hitMasterPasswordNew, "long enough one")
	typeInto(state, hitMasterPasswordConfirm, "long enough two")
	state.Click(hitMasterPasswordSubmit)
	if state.secrets.dialogError != state.messages.MasterPasswordMismatch {
		t.Fatalf("mismatch: error %q", state.secrets.dialogError)
	}
	state.secrets.confirmPassword = text.Editor{}
	typeInto(state, hitMasterPasswordConfirm, "long enough one")
	state.Click(hitMasterPasswordSubmit)
	if state.modal != modalNone || !state.secrets.vault.HasMasterPassword() || state.status != state.messages.VaultMasterSet {
		t.Fatalf("modal %d, password %v, status %q", state.modal, state.secrets.vault.HasMasterPassword(), state.status)
	}

	// The group shows what can be done now, and Lock locks.
	for _, id := range []ui.ID{hitSettingsMasterLock, hitSettingsMasterChange, hitSettingsMasterRemove} {
		if !settingsShows(state, id) {
			t.Fatalf("settings lacks button %d with a master password set", id)
		}
	}
	state.Click(hitSettingsMasterLock)
	if state.secrets.vault.Unlocked() {
		t.Fatal("Lock now left the vault unlocked")
	}
	// Removing needs the password first.
	state.Click(hitSettingsMasterRemove)
	if state.modal != modalVaultUnlock {
		t.Fatalf("modal = %d, want the unlock dialog before removal", state.modal)
	}
	typeInto(state, hitVaultUnlockField, "long enough one")
	state.Click(hitVaultUnlockSubmit)
	if state.secrets.vault.HasMasterPassword() || state.status != state.messages.VaultMasterRemoved {
		t.Fatalf("password %v, status %q", state.secrets.vault.HasMasterPassword(), state.status)
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
	if !state.canAutofill(sshconn.Status{Phase: sshconn.PhasePasswordPrompt, Attempt: 1, Questions: password, Endpoint: sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}}) {
		t.Fatal("the vault does not answer the question a restore would meet")
	}
	for _, status := range []sshconn.Status{
		{Phase: sshconn.PhasePasswordPrompt, Attempt: 2, Questions: password, Endpoint: sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}},
		{Phase: sshconn.PhasePasswordPrompt, Attempt: 1, Questions: []sshconn.Question{{Text: "Code:", Secret: true}}, Endpoint: sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}},
		{Phase: sshconn.PhasePasswordPrompt, Attempt: 1, Questions: password, Endpoint: sshconn.Endpoint{Host: "other.example.com", Port: 22, User: "deploy"}},
		{Phase: sshconn.PhaseHostKeyPrompt, Attempt: 1},
	} {
		if state.canAutofill(status) {
			t.Fatalf("restore would count on the vault for %+v", status)
		}
	}
}

// settingsShows looks for a control on every category of the settings page, from
// the top down.
func settingsShows(state *State, id ui.ID) bool {
	for category := settingsCategory(0); category < settingsCategoryCount; category++ {
		state.selectSettingsCategory(category)
		for range 60 {
			canvas, _ := paintedTexts(state)
			if _, ok := hitByID(canvas, id); ok {
				return true
			}
			state.scrollSettings(ui.ScrollEvent{DeltaY: 120, Precise: true})
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
