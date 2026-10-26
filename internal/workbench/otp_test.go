package workbench

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/text"
	"motty/internal/otp"
	"motty/internal/sshconn"
)

// The seed of RFC 6238's test key, "12345678901234567890", and the code it gives
// at Unix time 59.
const (
	testOTPSeed = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	testOTPCode = "287082"
)

const prodOTPName = "otp:deploy@prod.example.com:22"

func otpState(t *testing.T) (*State, *world) {
	t.Helper()
	world, options := newWorld(t)
	options.WallNow = func() time.Time { return time.Unix(59, 0) }
	return newTestState(t, options), world
}

func editProduction(t *testing.T, state *State) {
	t.Helper()
	for _, host := range state.hosts {
		if host.Label == "Production" {
			state.selectedHostID = host.ID
		}
	}
	state.hostSelections = nil
	state.openEditHost()
	if state.modal != modalNewHost {
		t.Fatal("the edit dialog did not open")
	}
}

func askForCode(state *State, fake *fakeConn, attempt int, question string) {
	fake.set(func(s *sshconn.Status) {
		s.Phase, s.Attempt = sshconn.PhasePasswordPrompt, attempt
		s.Questions = []sshconn.Question{{Text: question, Secret: true}}
		s.Endpoint = sshconn.Endpoint{Host: "prod.example.com", Port: 22, User: "deploy"}
	})
	state.Wake()
}

func TestASecretGivenInTheHostDialogIsKeptInTheVaultAndNeverShownAgain(t *testing.T) {
	state, _ := otpState(t)
	editProduction(t, state)
	if state.editor.otpSaved {
		t.Fatal("a secret that was never given is reported saved")
	}
	typeInto(state, hitDialogOTP, "not a secret!")
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.Tools.ErrOTPInvalid {
		t.Fatalf("an invalid secret: modal %d, error %q", state.modal, state.dialogError)
	}
	state.editor.otp = text.Editor{}
	typeInto(state, hitDialogOTP, "otpauth://hotp/Prod:deploy?secret="+testOTPSeed+"&counter=3")
	state.Click(hitDialogSubmit)
	if state.modal != modalNone {
		t.Fatalf("a valid secret was refused: %q", state.dialogError)
	}
	stored, found, err := state.secrets.vault.Get(prodOTPName)
	if err != nil || !found || !strings.Contains(stored, "counter=3") {
		t.Fatalf("vault holds %q, %v, %v", stored, found, err)
	}
	if !strings.Contains(state.status, "deploy@prod.example.com") {
		t.Fatalf("status %q", state.status)
	}
	editProduction(t, state)
	if !state.editor.otpSaved || state.editor.value(fieldOTP) != "" {
		t.Fatalf("the dialog should say it is saved and show nothing: saved %v, text %q", state.editor.otpSaved, state.editor.value(fieldOTP))
	}
	state.Click(hitDialogOTPRemove)
	state.Click(hitDialogSubmit)
	if _, found, _ := state.secrets.vault.Get(prodOTPName); found {
		t.Fatal("the secret was not removed")
	}
}

func TestChangingTheAccountMovesTheSecretWithIt(t *testing.T) {
	state, _ := otpState(t)
	editProduction(t, state)
	typeInto(state, hitDialogOTP, testOTPSeed)
	state.Click(hitDialogSubmit)
	state.secrets.vault.Lock()
	editProduction(t, state)
	if state.editor.otpChecked {
		t.Fatal("opening the editor checked the locked vault")
	}
	state.editor.user = text.Editor{}
	state.editor.user.Insert("ops")
	state.Click(hitDialogSubmit)
	if _, found, _ := state.secrets.vault.Get(prodOTPName); found {
		t.Fatal("the old account still has the secret")
	}
	if _, found, _ := state.secrets.vault.Get("otp:ops@prod.example.com:22"); !found {
		t.Fatal("the secret did not follow the account")
	}
}

func TestEditingAHostDoesNotOpenTheSystemSecretStoreForOTP(t *testing.T) {
	_, options := newWorld(t)
	options.WallNow = func() time.Time { return time.Unix(59, 0) }
	keys := options.Keystore.(*memKeys)
	state := newTestState(t, options)
	if err := state.secrets.vault.Set(prodOTPName, testOTPSeed); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.Lock()
	loads := keys.loadCount()

	editProduction(t, state)
	if keys.loadCount() != loads || state.editor.otpChecked || state.editor.otpSaved {
		t.Fatalf("opening edit loaded the secret store: loads=%d checked=%v saved=%v", keys.loadCount()-loads, state.editor.otpChecked, state.editor.otpSaved)
	}
	state.Click(hitDialogSubmit)
	if keys.loadCount() != loads {
		t.Fatalf("saving an unchanged endpoint loaded the secret store %d time(s)", keys.loadCount()-loads)
	}

	editProduction(t, state)
	state.Click(hitDialogOTPRemove)
	if keys.loadCount() != loads+1 || !state.editor.otpChecked || !state.editor.otpSaved {
		t.Fatalf("explicit check: loads=%d checked=%v saved=%v", keys.loadCount()-loads, state.editor.otpChecked, state.editor.otpSaved)
	}
}

func TestAOneTimeCodeQuestionIsAnsweredOnceFromTheSecret(t *testing.T) {
	state, world := otpState(t)
	editProduction(t, state)
	typeInto(state, hitDialogOTP, testOTPSeed)
	state.Click(hitDialogSubmit)

	fake := connect(t, state, world)
	askForCode(state, fake, 2, "Verification code: ")
	if state.modal != modalNone || len(fake.passwords) != 1 || fake.passwords[0] != testOTPCode {
		t.Fatalf("modal %d, sent %v", state.modal, fake.passwords)
	}
	// Refused and asked again: the person types it, nothing is resent.
	askForCode(state, fake, 3, "Verification code: ")
	if state.modal != modalPassword || len(fake.passwords) != 1 {
		t.Fatalf("a refused code was sent again: modal %d, sent %v", state.modal, fake.passwords)
	}
}

func TestAPasswordQuestionIsNotTakenForACodeQuestion(t *testing.T) {
	state, world := otpState(t)
	editProduction(t, state)
	typeInto(state, hitDialogOTP, testOTPSeed)
	state.Click(hitDialogSubmit)
	fake := connect(t, state, world)
	askForCode(state, fake, 1, "Password: ")
	if state.modal != modalPassword || len(fake.passwords) != 0 {
		t.Fatalf("a password question was answered with a code: modal %d, sent %v", state.modal, fake.passwords)
	}
}

func TestAHOTPCodeAdvancesItsCounterInTheVault(t *testing.T) {
	state, world := otpState(t)
	if err := state.secrets.vault.Prepare(); err != nil {
		t.Fatal(err)
	}
	secret, _ := otp.Parse("otpauth://hotp/x?secret=" + testOTPSeed + "&counter=1")
	if err := state.secrets.vault.Set(prodOTPName, secret.String()); err != nil {
		t.Fatal(err)
	}
	fake := connect(t, state, world)
	askForCode(state, fake, 2, "Enter your one-time token:")
	if len(fake.passwords) != 1 || fake.passwords[0] != "287082" {
		t.Fatalf("counter 1 should give 287082, sent %v", fake.passwords)
	}
	stored, _, _ := state.secrets.vault.Get(prodOTPName)
	if !strings.Contains(stored, "counter=2") {
		t.Fatalf("the counter did not move: %q", stored)
	}
}

func TestWithoutASecretACodeQuestionIsTheUsers(t *testing.T) {
	state, world := otpState(t)
	fake := connect(t, state, world)
	askForCode(state, fake, 2, "Verification code: ")
	if state.modal != modalPassword || len(fake.passwords) != 0 {
		t.Fatalf("modal %d, sent %v", state.modal, fake.passwords)
	}
}

func TestSavingASecretNeedsAnUnlockedVault(t *testing.T) {
	state, _ := otpState(t)
	if err := state.secrets.vault.SetMasterPassword("master-pass"); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.Lock()
	editProduction(t, state)
	typeInto(state, hitDialogOTP, testOTPSeed)
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.Tools.ErrOTPLocked {
		t.Fatalf("modal %d, error %q", state.modal, state.dialogError)
	}
}
