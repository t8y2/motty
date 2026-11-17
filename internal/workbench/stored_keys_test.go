package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"golang.org/x/crypto/ssh"
	"motty/internal/keyidentity"
	"motty/internal/sshconn"
)

// addKeyByHand does what the user does to paste a key: Add Key, Paste, the text,
// a name, Add.
func addKeyByHand(t *testing.T, state *State, text, name string) {
	t.Helper()
	state.Click(hitKeychain)
	state.Click(hitKeysAdd)
	if !state.keysMenu {
		t.Fatal("Add Key did not open its menu")
	}
	state.Click(hitKeysPaste)
	if state.keysMenu || state.modal != modalForm || !state.form.fields[0].multiline {
		t.Fatalf("Paste did not open a form with a multiline key field: modal %d", state.modal)
	}
	state.form.set(0, text)
	state.form.set(1, name)
	state.Click(hitFormSubmit)
}

func TestPastedKeyIsKeptInTheVaultAndSurvivesARestart(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	pem := string(identityPEM(t, ""))
	// What a terminal or a chat leaves behind: CRLF line ends and indentation.
	pasted := "  " + strings.ReplaceAll(strings.TrimSpace(pem), "\n", "\r\n  ") + "\r\n"
	addKeyByHand(t, state, pasted, "work server")

	if state.modal != modalNone {
		t.Fatalf("the form stayed open: %q", state.form.dialogError)
	}
	if len(state.identities) != 1 {
		t.Fatalf("identities = %+v", state.identities)
	}
	key := state.identities[0]
	if !key.Stored || key.Name != "work server" || !sshconn.IsStoredKey(key.Path) || key.Fingerprint == "" || !strings.HasPrefix(key.PublicKey, "ssh-ed25519 ") {
		t.Fatalf("stored key = %+v", key)
	}
	if state.selectedIdentityID != key.ID || state.form.fields[0].editor.String() != "" {
		t.Fatalf("selected %d, key text left in the form: %q", state.selectedIdentityID, state.form.fields[0].editor.String())
	}

	// Not a file, and not in the saved state: the vault is where it lives.
	if saved := state.snapshot(); len(saved.IdentityFiles) != 0 {
		t.Fatalf("the saved state names the stored key: %v", saved.IdentityFiles)
	}
	restarted := newTestState(t, options)
	if len(restarted.identities) != 1 || restarted.identities[0].Path != key.Path || restarted.identities[0].Name != "work server" ||
		restarted.identities[0].Fingerprint != key.Fingerprint {
		t.Fatalf("restored identities = %+v", restarted.identities)
	}
}

func TestPastedTextThatIsNotAPrivateKeyIsRefusedAndKept(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	public := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIA6Qp9o5Zr9Gj6bC0Oe3Hh3q7hB1n0QmJw8m7cV3w9sM me@host"
	addKeyByHand(t, state, public, "")
	if state.modal != modalForm || state.form.dialogError == "" {
		t.Fatalf("a public key was accepted as a private one: modal %d, error %q", state.modal, state.form.dialogError)
	}
	if got := state.form.value(0); got != public {
		t.Fatalf("the form lost what was pasted: %q", got)
	}
	if len(state.identities) != 0 {
		t.Fatalf("identities = %+v", state.identities)
	}

	state.form.set(0, "-----BEGIN OPENSSH PRIVATE KEY-----\nnot base64 at all\n-----END OPENSSH PRIVATE KEY-----")
	state.Click(hitFormSubmit)
	if state.modal != modalForm || state.form.dialogError == "" || len(state.identities) != 0 {
		t.Fatalf("a damaged key was accepted: modal %d, error %q, identities %+v", state.modal, state.form.dialogError, state.identities)
	}
}

func TestTheSameKeyIsNotAddedTwice(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	text := string(identityPEM(t, ""))
	addKeyByHand(t, state, text, "first")
	addKeyByHand(t, state, text, "second")
	if state.modal != modalForm || !strings.Contains(state.form.dialogError, "first") || len(state.identities) != 1 {
		t.Fatalf("modal %d, error %q, identities %d", state.modal, state.form.dialogError, len(state.identities))
	}
}

func TestAKeyWithoutANameIsNamedAfterItself(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addKeyByHand(t, state, string(identityPEM(t, "")), "")
	if len(state.identities) != 1 || !strings.HasPrefix(state.identities[0].Name, "ed25519-") {
		t.Fatalf("identities = %+v", state.identities)
	}
}

func TestGeneratedKeyIsStoredAndItsPublicKeyIsCopied(t *testing.T) {
	options := freshOptions(t)
	var copied string
	options.SetClipboardText = func(text string) error { copied = text; return nil }
	state := newTestState(t, options)
	state.Click(hitKeychain)
	state.Click(hitKeysAdd)
	state.Click(hitKeysGenerate)
	if state.modal != modalForm {
		t.Fatal("Generate did not open its form")
	}
	state.Click(hitFormSubmit)
	if state.modal != modalForm || state.form.dialogError != state.messages.KeyNameRequired {
		t.Fatalf("a key was made without a name: modal %d, error %q", state.modal, state.form.dialogError)
	}
	state.form.set(0, "work laptop")
	state.Click(hitFormSubmit)
	if state.modal != modalNone || len(state.identities) != 1 {
		t.Fatalf("modal %d, error %q, identities %d", state.modal, state.form.dialogError, len(state.identities))
	}
	key := state.identities[0]
	public, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(copied))
	if err != nil || comment != "work laptop" || ssh.FingerprintSHA256(public) != key.Fingerprint {
		t.Fatalf("copied %q: %v, comment %q, key %+v", copied, err, comment, key)
	}
	if key.Encrypted || !key.Stored {
		t.Fatalf("generated key = %+v", key)
	}

	// The copy button gives the same line again, from any later time.
	copied = ""
	state.Click(hitKeysCopyPublic)
	if !strings.HasPrefix(copied, key.PublicKey) {
		t.Fatalf("Copy Public Key copied %q, want %q", copied, key.PublicKey)
	}
}

func TestGeneratedKeyWithAPassphraseIsEncrypted(t *testing.T) {
	options := freshOptions(t)
	options.SetClipboardText = func(string) error { return nil } // never the machine's clipboard
	state := newTestState(t, options)
	state.Click(hitKeychain)
	state.Click(hitKeysAdd)
	state.Click(hitKeysGenerate)
	state.form.set(0, "locked")
	state.form.set(1, "correct horse")
	state.Click(hitFormSubmit)
	if len(state.identities) != 1 || !state.identities[0].Encrypted || state.identities[0].PublicKey == "" {
		t.Fatalf("identities = %+v", state.identities)
	}
	value, _, err := state.secrets.vault.Get(state.identities[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := unmarshalStoredKey(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keyidentity.Parse([]byte(stored.Key), []byte("correct horse")); err != nil {
		t.Fatalf("the stored key does not open with its passphrase: %v", err)
	}
}

func TestStoredKeyReachesTheConnectionByName(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	text := identityPEM(t, "")
	addKeyByHand(t, state, string(text), "work")
	ref := state.identities[0].Path
	state.Click(hitHosts)
	connect(t, state, world)

	if len(world.configs) != 1 || len(world.configs[0].AdditionalIdentityFiles) != 1 || world.configs[0].AdditionalIdentityFiles[0] != ref {
		t.Fatalf("dial config = %+v", world.configs)
	}
	read := world.configs[0].StoredKey
	if read == nil {
		t.Fatal("the connection cannot read stored keys")
	}
	data, err := read(ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keyidentity.Parse(data, nil); err != nil {
		t.Fatalf("the connection was given something that is not the key: %v", err)
	}
	if _, err := read(sshconn.StoredKeyPrefix + "gone"); err == nil {
		t.Fatal("a key that is not saved was read")
	}
}

func TestStoredKeyCanBePinnedToAHost(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addKeyByHand(t, state, string(identityPEM(t, "")), "pinned")
	key := state.identities[0]
	host := Host{Auth: authPrivateKey, Identity: key.Fingerprint}
	authentication := state.hostCatalog.authenticationFor(host)
	if len(authentication.IdentityFiles) != 1 || authentication.IdentityFiles[0] != key.Path {
		t.Fatalf("authentication = %+v", authentication)
	}
}

func TestDeletingAStoredKeyAsksFirstAndForgetsItsPassphrase(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	addKeyByHand(t, state, string(identityPEM(t, "")), "doomed")
	key := state.identities[0]
	if err := state.secrets.vault.Set(passphraseSecretName(key.Path), "pw"); err != nil {
		t.Fatal(err)
	}

	state.Click(hitKeysRemove)
	if state.modal != modalForm || !state.form.danger || len(state.identities) != 1 {
		t.Fatalf("Remove did not ask: modal %d, identities %d", state.modal, len(state.identities))
	}
	state.Click(hitFormCancel)
	if len(state.identities) != 1 {
		t.Fatal("canceling deleted the key")
	}

	state.Click(hitKeysRemove)
	state.Click(hitFormSubmit)
	if state.modal != modalNone || len(state.identities) != 0 {
		t.Fatalf("modal %d, identities %+v", state.modal, state.identities)
	}
	for _, name := range []string{key.Path, passphraseSecretName(key.Path)} {
		if _, found, _ := state.secrets.vault.Get(name); found {
			t.Fatalf("%s is still in the vault", name)
		}
	}
	if restarted := newTestState(t, options); len(restarted.identities) != 0 {
		t.Fatalf("the deleted key came back: %+v", restarted.identities)
	}
}

func TestAKeyDeletedOnAnotherComputerLeavesTheList(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addKeyByHand(t, state, string(identityPEM(t, "")), "synced")
	addKeyByHand(t, state, string(identityPEM(t, "x")), "kept")
	if len(state.identities) != 2 {
		t.Fatalf("identities = %+v", state.identities)
	}
	gone := state.identities[0].Path
	for _, identity := range state.identities {
		if identity.Name == "synced" {
			gone = identity.Path
		}
	}
	if err := state.secrets.vault.Delete(gone); err != nil {
		t.Fatal(err)
	}
	state.reloadStoredIdentities()
	if len(state.identities) != 1 || state.identities[0].Name != "kept" || state.selectedIdentityID != state.identities[0].ID {
		t.Fatalf("identities = %+v, selected %d", state.identities, state.selectedIdentityID)
	}
}

func TestStoredKeysAreNamedByTheirNameWhereAPassphraseIsAsked(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addKeyByHand(t, state, string(identityPEM(t, "")), "work server")
	if got := state.identityLabel(state.identities[0].Path); got != "work server" {
		t.Fatalf("label = %q", got)
	}
	if got := state.identityLabel("/home/me/.ssh/id_rsa"); got != "id_rsa" {
		t.Fatalf("label = %q", got)
	}
}

func TestForgettingAllSavedPasswordsWarnsAboutStoredKeys(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addKeyByHand(t, state, string(identityPEM(t, "")), "only copy")
	state.openVaultForget()
	_, texts := paintAt(state, gfx.Size{Width: 1000, Height: 800})
	found := false
	for _, text := range texts {
		found = found || strings.Contains(text, "1 SSH keys")
	}
	if !found {
		t.Fatalf("the dialog does not mention the stored key: %q", texts)
	}
	state.Click(hitVaultForgetConfirm)
	if len(state.identities) != 0 {
		t.Fatalf("forgetting everything left the keys listed: %+v", state.identities)
	}
}

func TestVaultlessStateRefusesToStoreAKey(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.secrets.vault = nil
	addKeyByHand(t, state, string(identityPEM(t, "")), "nowhere")
	if state.modal != modalForm || state.form.dialogError != state.messages.VaultUnavailable {
		t.Fatalf("modal %d, error %q", state.modal, state.form.dialogError)
	}
}

func TestEnterInTheKeyFieldIsANewLineAndArrowsMoveByRow(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.Click(hitKeychain)
	state.Click(hitKeysAdd)
	state.Click(hitKeysPaste)
	field := &state.form.fields[0]
	field.editor.Insert("first\nsecond\nthird")
	canvas, _ := paintAt(state, gfx.Size{Width: 1000, Height: 800})
	if _, ok := hitByID(canvas, hitFormFieldList.At(0)); !ok {
		t.Fatal("the key field was not painted")
	}
	target := hitFormFieldList.At(0)
	end := field.editor.Caret()
	state.Key(gfx.KeyEvent{Key: gfx.KeyUp, Target: target})
	if up := field.editor.Caret(); up >= end || up == 0 {
		t.Fatalf("Up moved the caret from %d to %d", end, up)
	}
	// Enter is the native editor's new line; the form must not take it as Submit.
	state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: target})
	if state.modal != modalForm {
		t.Fatal("Enter in the key field submitted the form")
	}
}
