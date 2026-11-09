package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestRevealButtonShowsASecretAndHidesItAgainWhenTheDialogIsReopened(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	typeInto(state, hitDialogPassword, "hunter2")

	secure := func() bool {
		canvas, _ := paintedTexts(state)
		for _, hit := range canvas.Hits() {
			if hit.ID == hitDialogPassword && hit.Role == gfx.RoleTextField {
				return hit.Secure
			}
		}
		t.Fatal("the password field was not painted")
		return false
	}
	if !secure() {
		t.Fatal("a password field starts out showing its text")
	}
	state.Click(revealButton(hitDialogPassword))
	if secure() {
		t.Fatal("the show button did not reveal the password")
	}
	state.Click(revealButton(hitDialogPassword))
	if !secure() {
		t.Fatal("the hide button did not mask the password")
	}

	state.Click(revealButton(hitDialogPassword))
	secure()
	state.Click(hitDialogCancel)
	paintedTexts(state)
	state.Click(hitAddHost)
	if !secure() {
		t.Fatal("a password stayed revealed after its dialog was closed")
	}
}

func TestHostFormSavesTheKeyPassphraseUnderTheKeyAndRefusesAWrongOne(t *testing.T) {
	locked := writeWorkbenchIdentity(t, "id_locked", "secret")
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{locked}, nil }
	state := newTestState(t, options)
	state.Click(hitKeychain)
	state.Click(hitKeysImport)
	state.Click(hitHosts)

	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, "Vault")
	typeInto(state, hitDialogAddress, "vault.example.com")
	typeInto(state, hitDialogUser, "ops")
	state.Click(hitDialogAuth)
	state.Click(hitDialogAuthItemList.At(int(authPrivateKey)))
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitDialogPassphrase); ok {
		t.Fatal("the passphrase was asked before a key was chosen")
	}
	state.Click(hitDialogIdentity)
	state.Click(hitDialogIdentityItemList.At(int(state.identities[0].ID)))
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitDialogPassphrase); !ok {
		t.Fatal("an encrypted key did not ask for its passphrase")
	}

	typeInto(state, hitDialogPassphrase, "not it")
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.KeyPassphraseWrong {
		t.Fatalf("a wrong passphrase was accepted: modal %d, error %q", state.modal, state.dialogError)
	}
	if _, found, _ := state.secrets.vault.Get(passphraseSecretName(locked)); found {
		t.Fatal("a wrong passphrase reached the vault")
	}

	state.editor.passphrase = text.Editor{}
	typeInto(state, hitDialogPassphrase, "secret")
	state.Click(hitDialogSubmit)
	if state.modal != modalNone {
		t.Fatalf("the host was not saved: %q", state.dialogError)
	}
	if value, found, err := state.secrets.vault.Get(passphraseSecretName(locked)); err != nil || !found || value != "secret" {
		t.Fatalf("saved passphrase = %q, %v, %v", value, found, err)
	}
}

func TestEditHostShowsItsSavedPasswordEvenAfterARestart(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if err := state.secrets.vault.Set(prodPasswordName, "source secret"); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.DropKeyForTests() // as after a restart: the key waits in the key store
	clickHostAction(state, hitEditHost)
	if got := state.editor.password.String(); got != "source secret" {
		t.Fatalf("the saved password was left out of the form: %q", got)
	}
	state.Click(hitDialogSubmit)
	if value, _, _ := state.secrets.vault.Get(prodPasswordName); value != "source secret" {
		t.Fatalf("saving the form unchanged changed the password to %q", value)
	}
}
