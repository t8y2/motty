package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/text"

	"motty/internal/vault"
)

func TestHostDialogKeyLostOffersForgetAllAndRecovers(t *testing.T) {
	_, options := newWorld(t)
	keys := &memKeys{}
	options.Keystore = keys
	state := newTestState(t, options)

	// Save an initial secret to give the vault records.
	if err := state.secrets.vault.Set("initial-secret", "initial-value"); err != nil {
		t.Fatal(err)
	}

	// Drop the key in memory and delete it from the keystore to simulate ErrKeyLost.
	state.secrets.vault.DropKeyForTests()
	if err := keys.Delete(vault.KeyService, vault.KeyAccount); err != nil {
		t.Fatal(err)
	}

	// Open the edit dialog for the first host.
	state.hostDialogMode = hostDialogEdit
	first := state.hosts[0]
	state.hostDialogID = first.ID
	state.editor.load(first)
	state.modal = modalNewHost
	state.dialogError = ""

	// Type a new password and try to save.
	typeInto(state, hitDialogPassword, "new-secret-password")
	state.Click(hitDialogSubmit)

	// Verify that submit failed with VaultKeyLost and offered the forget action.
	wantError := fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.messages.VaultKeyLost)
	if state.modal != modalNewHost {
		t.Fatalf("modal = %d, want modalNewHost", state.modal)
	}
	if state.dialogError != wantError {
		t.Fatalf("dialogError = %q, want %q", state.dialogError, wantError)
	}
	if !state.isHostDialogKeyLost() {
		t.Fatal("isHostDialogKeyLost() = false, want true")
	}

	// Verify that the Forget All button is painted in the dialog body.
	canvas, _ := paintedTexts(state)
	hit, ok := hitByID(canvas, hitDialogVaultForget)
	if !ok {
		t.Fatal("hitDialogVaultForget was not painted on error")
	}
	if hit.Label != state.messages.SettingsVaultForgetAll {
		t.Fatalf("hit.Label = %q, want %q", hit.Label, state.messages.SettingsVaultForgetAll)
	}

	// Click Forget All from the host dialog.
	state.Click(hitDialogVaultForget)
	if state.modal != modalVaultForget {
		t.Fatalf("modal = %d, want modalVaultForget", state.modal)
	}

	// Canceling must return cleanly to modalNewHost with the typed password intact.
	state.Click(hitVaultForgetCancel)
	if state.modal != modalNewHost {
		t.Fatalf("modal after cancel = %d, want modalNewHost", state.modal)
	}
	if state.editor.password.String() != "new-secret-password" {
		t.Fatalf("typed password was lost on cancel: %q", state.editor.password.String())
	}

	// Click Forget All again, and confirm this time.
	state.Click(hitDialogVaultForget)
	state.Click(hitVaultForgetConfirm)

	// The vault must be cleared, the new host saved, and the modal closed.
	if state.modal != modalNone {
		t.Fatalf("modal after confirm = %d, want modalNone", state.modal)
	}
	secretName, ok := hostPasswordSecretName(first)
	if !ok {
		t.Fatal("hostPasswordSecretName returned false")
	}
	savedValue, found, err := state.secrets.vault.Get(secretName)
	if err != nil {
		t.Fatalf("reading saved password from recovered vault: %v", err)
	}
	if !found || savedValue != "new-secret-password" {
		t.Fatalf("saved password = %q (found=%v), want %q", savedValue, found, "new-secret-password")
	}
}

func TestHostDialogNewHostKeyLostOffersForgetAllAndRecovers(t *testing.T) {
	_, options := newWorld(t)
	keys := &memKeys{}
	options.Keystore = keys
	state := newTestState(t, options)

	if err := state.secrets.vault.Set("initial-secret", "initial-value"); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.DropKeyForTests()
	if err := keys.Delete(vault.KeyService, vault.KeyAccount); err != nil {
		t.Fatal(err)
	}

	state.openHostDialog()
	typeInto(state, hitDialogLabel, "New Server")
	typeInto(state, hitDialogAddress, "new.example.com")
	typeInto(state, hitDialogUser, "root")
	typeInto(state, hitDialogPassword, "root-password")
	state.Click(hitDialogSubmit)

	wantError := fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.messages.VaultKeyLost)
	if state.modal != modalNewHost || state.dialogError != wantError || !state.isHostDialogKeyLost() {
		t.Fatalf("submit failed check: modal=%d, error=%q, keyLost=%v", state.modal, state.dialogError, state.isHostDialogKeyLost())
	}

	// Verify painted button.
	canvas, _ := paintedTexts(state)
	if hit, ok := hitByID(canvas, hitDialogVaultForget); !ok || hit.Label != state.messages.SettingsVaultForgetAll {
		t.Fatalf("new host forget button not painted: ok=%v, hit=%+v", ok, hit)
	}

	// Confirm forget all directly.
	state.Click(hitDialogVaultForget)
	state.Click(hitVaultForgetConfirm)

	if state.modal != modalNone {
		t.Fatalf("modal after confirm = %d, want modalNone", state.modal)
	}

	// The new host should now be in state.hosts.
	var savedHost *Host
	for i := range state.hosts {
		if state.hosts[i].Label == "New Server" {
			savedHost = &state.hosts[i]
			break
		}
	}
	if savedHost == nil {
		t.Fatal("new host was not saved")
	}
	secretName, ok := hostPasswordSecretName(*savedHost)
	if !ok {
		t.Fatal("hostPasswordSecretName returned false")
	}
	savedValue, found, err := state.secrets.vault.Get(secretName)
	if err != nil {
		t.Fatalf("reading saved password from recovered vault: %v", err)
	}
	if !found || savedValue != "root-password" {
		t.Fatalf("saved password = %q (found=%v), want %q", savedValue, found, "root-password")
	}
}

// The recovery button answers the key-lost error only: once another error
// replaces it, offering to empty the vault would be a destructive button under
// a message that has nothing to do with it.
func TestHostDialogKeyLostButtonGoesWithItsError(t *testing.T) {
	_, options := newWorld(t)
	keys := &memKeys{}
	options.Keystore = keys
	state := newTestState(t, options)
	if err := state.secrets.vault.Set("initial-secret", "initial-value"); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.DropKeyForTests()
	if err := keys.Delete(vault.KeyService, vault.KeyAccount); err != nil {
		t.Fatal(err)
	}

	state.hostDialogMode = hostDialogEdit
	first := state.hosts[0]
	state.hostDialogID = first.ID
	state.editor.load(first)
	state.modal = modalNewHost
	typeInto(state, hitDialogPassword, "new-secret-password")
	state.Click(hitDialogSubmit)
	if !state.isHostDialogKeyLost() {
		t.Fatal("the key-lost error did not offer the recovery button")
	}

	// Emptying a required field turns the error into a validation one.
	state.editor.address = text.Editor{}
	state.Click(hitDialogSubmit)
	if state.dialogError == "" {
		t.Fatal("an empty address was accepted")
	}
	if state.isHostDialogKeyLost() {
		t.Fatalf("the recovery button stayed under an unrelated error: %q", state.dialogError)
	}
}

// When the vault cannot be cleared the form the user filled in stays, with the
// reason on it.
func TestHostDialogKeyLostForgetFailureKeepsTheForm(t *testing.T) {
	_, options := newWorld(t)
	keys := &memKeys{}
	options.Keystore = keys
	state := newTestState(t, options)
	if err := state.secrets.vault.Set("initial-secret", "initial-value"); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.DropKeyForTests()
	if err := keys.Delete(vault.KeyService, vault.KeyAccount); err != nil {
		t.Fatal(err)
	}

	state.openHostDialog()
	typeInto(state, hitDialogLabel, "New Server")
	typeInto(state, hitDialogAddress, "new.example.com")
	typeInto(state, hitDialogUser, "root")
	typeInto(state, hitDialogPassword, "root-password")
	state.Click(hitDialogSubmit)
	if !state.isHostDialogKeyLost() {
		t.Fatal("the key-lost error did not offer the recovery button")
	}

	// A directory where the vault file is makes every write of it fail.
	path := filepath.Join(filepath.Dir(options.StatePath), "vault.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	state.Click(hitDialogVaultForget)
	state.Click(hitVaultForgetConfirm)
	if state.modal != modalNewHost {
		t.Fatalf("modal after a failed forget = %d, want the host dialog", state.modal)
	}
	if state.dialogError == "" {
		t.Fatal("the failure was not shown on the form")
	}
	if state.editor.password.String() != "root-password" {
		t.Fatalf("the typed password was lost: %q", state.editor.password.String())
	}
}
