package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/keyidentity"
)

// editKeyByHand does what the user does to edit the selected key: Edit Key, the
// name, the text, Save.
func editKeyByHand(t *testing.T, state *State, name, text string) {
	t.Helper()
	state.Click(hitKeysEdit)
	if state.modal != modalForm {
		t.Fatalf("Edit Key did not open a form: modal %d", state.modal)
	}
	state.form.set(0, name)
	state.form.set(1, text)
	state.Click(hitFormSubmit)
}

func keyPageSize() gfx.Size { return gfx.Size{Width: 1400, Height: 900} }

// textField is the text field named label on the canvas, zero when there is none.
func textField(canvas *gfx.Canvas, label string) gfx.Hit {
	for _, hit := range canvas.Hits() {
		if hit.Label == label && hit.Role == gfx.RoleTextField {
			return hit
		}
	}
	return gfx.Hit{}
}

// keyPageHit is the control of an identity on the canvas, zero when there is none.
func keyPageHit(canvas *gfx.Canvas, id gfx.ID) gfx.Hit {
	for _, found := range canvas.Hits() {
		if found.ID == id {
			return found
		}
	}
	return gfx.Hit{}
}

func storedRecord(t *testing.T, state *State, ref string) storedKey {
	t.Helper()
	record, err := readStoredKeyRecord(state.secrets.vault, ref)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func identityNamed(state *State, name string) identityRecord {
	for _, identity := range state.identities {
		if identity.Name == name {
			return identity
		}
	}
	return identityRecord{}
}

func hostNamed(state *State, label string) Host {
	for _, host := range state.hosts {
		if host.Label == label {
			return host
		}
	}
	return Host{}
}

func fingerprintOf(t *testing.T, text string) string {
	t.Helper()
	info, err := keyidentity.InspectData("", []byte(text))
	if err != nil || info.Fingerprint == "" {
		t.Fatalf("the text is no readable key: %+v, %v", info, err)
	}
	return info.Fingerprint
}

func lockedPKCS8(t *testing.T) string {
	t.Helper()
	// Made by OpenSSL with the passphrase "correct horse"; its public half stays
	// locked until then, as older encrypted PEM forms do.
	data, err := os.ReadFile(filepath.Join("..", "keyidentity", "testdata", "pkcs8_ed25519_aes256_sha256.pem"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The edit form shows a stored key as it is kept. Its text is a password's: not
// on the canvas and not in the hit until the button shows it, and hidden again
// when the form is opened next.
func TestEditKeyFormShowsTheKeyConcealedUntilShown(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	m := state.messages
	addKeyByHand(t, state, string(identityPEM(t, "")), "work")
	key := state.identities[0]
	state.Click(hitKeysEdit)
	if state.modal != modalForm || state.form.value(0) != "work" || state.form.fields[1].reveal.show != m.KeyShow {
		t.Fatalf("modal %d, name %q, reveal %+v", state.modal, state.form.value(0), state.form.fields[1].reveal)
	}
	stored := storedRecord(t, state, key.Path)
	if state.form.value(1) != stored.Key {
		t.Fatalf("the text is not the key as it is kept: %q", state.form.value(1))
	}
	canvas, texts := paintAt(state, keyPageSize())
	if got := textField(canvas, m.KeyTextLabel); !got.Secure || got.Value != "" {
		t.Fatalf("the key's field is not a password's: %+v", got)
	}
	if got := keyPageHit(canvas, revealButton(hitFormFieldList.At(1))); got.ID.IsZero() || got.Label != m.KeyShow {
		t.Fatalf("the key's field has no button to show its text: %+v", got)
	}
	for _, text := range texts {
		if strings.Contains(text, "PRIVATE KEY") {
			t.Fatalf("the concealed key reached the canvas: %q", text)
		}
	}

	state.Click(revealButton(hitFormFieldList.At(1)))
	canvas, texts = paintAt(state, keyPageSize())
	if got := textField(canvas, m.KeyTextLabel); got.Secure || got.Value != stored.Key {
		t.Fatalf("the shown key's field = %+v", got)
	}
	if !strings.Contains(strings.Join(texts, "\n"), "-----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Fatalf("the shown key is not on the canvas: %q", texts)
	}

	// Leaving the form by any route hides the text for the next time it opens.
	state.Click(hitFormCancel)
	state.Click(hitKeysEdit)
	if canvas, _ = paintAt(state, keyPageSize()); textField(canvas, m.KeyTextLabel).Secure != true {
		t.Fatal("the key is shown when its form opens again")
	}
}

// A saved edit keeps the key's place in every list that knows it: the same row,
// reference and selection, its certificate, and a passphrase saved for it.
func TestEditingAKeysNameKeepsItsPlace(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	addKeyByHand(t, state, string(identityPEM(t, "locked")), "work")
	addKeyByHand(t, state, string(identityPEM(t, "")), "laptop")
	work := identityNamed(state, "work")
	if err := state.secrets.vault.Set(work.Path, marshalStoredKey(storedKey{Name: "work", Key: string(identityPEM(t, "locked")), Certificate: "ssh-ed25519-cert-v01@openssh.com AAAA"})); err != nil {
		t.Fatal(err)
	}
	if err := state.secrets.vault.Set(passphraseSecretName(work.Path), "locked"); err != nil {
		t.Fatal(err)
	}
	// The reload numbers the keys again; the entry is found by where it lives.
	state.reloadStoredIdentities()
	state.selectedIdentityID = identityNamed(state, "work").ID
	stored := storedRecord(t, state, work.Path)
	editKeyByHand(t, state, "  work server  ", stored.Key)

	if state.modal != modalNone {
		t.Fatalf("the form stayed open: %q", state.form.dialogError)
	}
	if len(state.identities) != 2 || identityNamed(state, "laptop").ID == 0 {
		t.Fatalf("identities = %+v", state.identities)
	}
	if kept := identityNamed(state, "work server"); kept.Path != work.Path || kept.Fingerprint != work.Fingerprint || !kept.Certificate || !kept.Encrypted ||
		kept.ID == 0 || state.selectedIdentityID != kept.ID || state.form.fields[1].editor.String() != "" {
		t.Fatalf("the edited key = %+v (was %+v), selected %d, text left %q", kept, work, state.selectedIdentityID, state.form.fields[1].editor.String())
	}
	record := storedRecord(t, state, work.Path)
	if _, found, _ := state.secrets.vault.Get(passphraseSecretName(work.Path)); record.Name != "work server" || record.Certificate != stored.Certificate || record.Key != stored.Key || !found {
		t.Fatalf("after the edit the vault holds %+v, passphrase %v", record, found)
	}
	restarted := newTestState(t, options)
	if got := identityNamed(restarted, "work server"); got.ID == 0 || !got.Certificate {
		t.Fatalf("after a restart the key is %+v", got)
	}
}

// A different key in an entry takes its hosts along: each host that signed in
// with the old key signs in with the new one, through the same row, while hosts
// on other keys and other ways to sign in stay as they were. The old key's
// certificate and passphrase are the old key's and go with it.
func TestReplacingAKeyTakesItsHostsAlong(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	m := state.messages
	plain := identityPEM(t, "")
	addKeyByHand(t, state, string(plain), "prod")
	old := identityNamed(state, "prod")
	rotated := identityPEM(t, "rotated")
	addKeyByHand(t, state, string(rotated), "spare")
	addKeyByHand(t, state, string(identityPEM(t, "x")), "dev")
	dev := identityNamed(state, "dev")
	state.hosts = append(state.hosts,
		Host{ID: 101, Label: "web", Address: "web.example.com", Port: 22, Auth: authPrivateKey, Identity: old.Fingerprint},
		Host{ID: 102, Label: "db", Address: "db.example.com", Port: 22, Auth: authPrivateKey, Identity: old.Fingerprint},
		Host{ID: 103, Label: "ci", Address: "ci.example.com", Port: 22, Auth: authPrivateKey, Identity: dev.Fingerprint},
		Host{ID: 104, Label: "pw", Address: "pw.example.com", Port: 22, Auth: authPassword},
	)
	state.hostDataChanged()
	if err := state.secrets.vault.Set(old.Path, marshalStoredKey(storedKey{Name: "prod", Key: string(plain), Certificate: "ssh-ed25519-cert-v01@openssh.com AAAA"})); err != nil {
		t.Fatal(err)
	}
	if err := state.secrets.vault.Set(passphraseSecretName(old.Path), "unused"); err != nil {
		t.Fatal(err)
	}
	// The reload numbers the keys again; the entry is found by where it lives.
	state.reloadStoredIdentities()
	selected := identityNamed(state, "prod").ID

	// The form says what a different key does, to an entry hosts use, before
	// there is anything to save.
	state.selectedIdentityID = selected
	state.Click(hitKeysEdit)
	if state.modal != modalForm || !strings.Contains(state.form.description, "2 hosts") {
		t.Fatalf("modal %d, description %q", state.modal, state.form.description)
	}
	// A key that is listed elsewhere is not let in twice.
	state.form.set(1, string(rotated))
	state.Click(hitFormSubmit)
	if state.modal != modalForm || state.form.dialogError != fmt.Sprintf(m.KeyDuplicateFmt, "spare") {
		t.Fatalf("a listed key was taken in: modal %d, error %q", state.modal, state.form.dialogError)
	}
	state.Click(hitFormCancel)
	// The spare leaves; then the rotated key has the old key's home.
	if err := state.secrets.vault.Delete(identityNamed(state, "spare").Path); err != nil {
		t.Fatal(err)
	}
	state.reloadStoredIdentities()
	state.selectedIdentityID = identityNamed(state, "prod").ID
	editKeyByHand(t, state, "prod", string(rotated))

	if state.modal != modalNone {
		t.Fatalf("the form stayed open: %q", state.form.dialogError)
	}
	fingerprint := fingerprintOf(t, string(rotated))
	replaced := identityNamed(state, "prod")
	if replaced.Path != old.Path || replaced.Fingerprint != fingerprint || !replaced.Encrypted || replaced.Certificate {
		t.Fatalf("the entry now holds %+v", replaced)
	}
	for label, want := range map[string]string{"web": fingerprint, "db": fingerprint, "ci": dev.Fingerprint, "pw": ""} {
		if got := hostNamed(state, label).Identity; got != want {
			t.Fatalf("%s now signs in with %q, want %q", label, got, want)
		}
	}
	if authentication := state.hostCatalog.authenticationFor(hostNamed(state, "web")); len(authentication.IdentityFiles) != 1 || authentication.IdentityFiles[0] != old.Path {
		t.Fatalf("the host no longer reaches its key through its row: %+v", authentication)
	}
	record := storedRecord(t, state, old.Path)
	if _, found, _ := state.secrets.vault.Get(passphraseSecretName(old.Path)); record.Certificate != "" || found {
		t.Fatalf("the old key's certificate %q or passphrase %v stayed", record.Certificate, found)
	}
	if state.status != fmt.Sprintf(m.KeyEditReplacedFmt, "prod", 2) {
		t.Fatalf("status = %q", state.status)
	}

	restarted := newTestState(t, options)
	if got := hostNamed(restarted, "db").Identity; got != fingerprint || len(restarted.identities) != 2 {
		t.Fatalf("after a restart db signs in with %q, identities %+v", got, restarted.identities)
	}
	if got := fingerprintOf(t, storedRecord(t, restarted, old.Path).Key); got != fingerprint {
		t.Fatalf("after a restart the entry holds the key %s, want %s", got, fingerprint)
	}
}

// A text whose public half stays behind its passphrase cannot be known for the
// key hosts sign in with: it is kept for an entry no host uses, and refused for
// one they do, with the key as it was.
func TestALockedTextCannotStandInForAKeyThatHostsUse(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addKeyByHand(t, state, string(identityPEM(t, "")), "alone")
	editKeyByHand(t, state, "alone", lockedPKCS8(t))
	if kept := identityNamed(state, "alone"); state.modal != modalNone || kept.Fingerprint != "" || !kept.Encrypted {
		t.Fatalf("modal %d, the entry holds %+v", state.modal, kept)
	}
	if got := storedRecord(t, state, identityNamed(state, "alone").Path).Key; got != lockedPKCS8(t) {
		t.Fatalf("the locked text was not kept as it is: %q", got)
	}

	addKeyByHand(t, state, string(identityPEM(t, "")), "shared")
	shared := identityNamed(state, "shared")
	state.hosts = append(state.hosts, Host{ID: 101, Label: "web", Address: "web.example.com", Port: 22, Auth: authPrivateKey, Identity: shared.Fingerprint})
	state.hostDataChanged()
	before := storedRecord(t, state, shared.Path)
	editKeyByHand(t, state, "shared", lockedPKCS8(t))
	if state.modal != modalForm || state.form.dialogError != state.messages.KeyEditUnverified {
		t.Fatalf("a locked text took the place of a key hosts use: modal %d, error %q", state.modal, state.form.dialogError)
	}
	if got := storedRecord(t, state, shared.Path); got.Key != before.Key || hostNamed(state, "web").Identity != shared.Fingerprint {
		t.Fatalf("the refused edit changed the key: %+v", got)
	}
}

// A passphrase saved for a key follows the key that is kept: it stays while it
// still opens the key and is forgotten once the edit leaves it useless.
func TestASavedPassphraseFollowsTheEditedKey(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	text := string(identityPEM(t, "same"))
	addKeyByHand(t, state, text, "work")
	key := identityNamed(state, "work")
	if err := state.secrets.vault.Set(passphraseSecretName(key.Path), "same"); err != nil {
		t.Fatal(err)
	}
	saved := func() bool {
		_, found, _ := state.secrets.vault.Get(passphraseSecretName(key.Path))
		return found
	}
	editKeyByHand(t, state, "work server", text)
	if !saved() {
		t.Fatal("a renamed key lost its passphrase")
	}
	// The same key, pasted again with another line ending.
	editKeyByHand(t, state, "work server", strings.ReplaceAll(text, "\n", "\r\n"))
	if !saved() {
		t.Fatal("the same key, pasted again, lost its passphrase")
	}
	// A key with another passphrase.
	editKeyByHand(t, state, "work server", string(identityPEM(t, "other")))
	if saved() {
		t.Fatal("a passphrase that no longer opens the key is still saved")
	}
	// A key that needs none.
	if err := state.secrets.vault.Set(passphraseSecretName(key.Path), "other"); err != nil {
		t.Fatal(err)
	}
	editKeyByHand(t, state, "work server", string(identityPEM(t, "")))
	if saved() {
		t.Fatal("an unencrypted key kept a passphrase")
	}
}

// Copy Private Key gives the text exactly as the key is kept, protected or not,
// as Export Key writes it.
func TestCopyPrivateKeyPutsTheKeptTextOnTheClipboard(t *testing.T) {
	options := freshOptions(t)
	copied := ""
	options.SetClipboardText = func(text string) error { copied = text; return nil }
	state := newTestState(t, options)
	addKeyByHand(t, state, string(identityPEM(t, "")), "work")
	state.Click(hitKeysCopyPrivate)
	if copied != storedRecord(t, state, state.identities[0].Path).Key || state.status != fmt.Sprintf(state.messages.KeyPrivateCopiedFmt, "work") {
		t.Fatalf("copied %q, status %q", copied, state.status)
	}
	addKeyByHand(t, state, string(identityPEM(t, "locked")), "laptop")
	state.Click(hitKeysCopyPrivate)
	if copied != storedRecord(t, state, identityNamed(state, "laptop").Path).Key {
		t.Fatalf("copied %q", copied)
	}
	if signer, err := keyidentity.Parse([]byte(copied), []byte("locked")); err != nil || signer.PublicKey() == nil {
		t.Fatalf("the copied key does not open with its passphrase: %v", err)
	}
	if _, err := keyidentity.Parse([]byte(copied), nil); err == nil {
		t.Fatal("the protected key was copied unprotected")
	}
}

// Edit Key and Copy Private Key wait for a key Motty keeps: both are off with
// nothing selected and on with a stored key.
func TestEditAndCopyPrivateKeyWaitForAKeptKey(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.Click(hitKeychain)
	canvas, _ := paintAt(state, keyPageSize())
	for _, id := range []gfx.ID{hitKeysEdit, hitKeysCopyPrivate} {
		if got := keyPageHit(canvas, id); got.ID.IsZero() || !got.Disabled {
			t.Fatalf("with no key %v is %+v", id, got)
		}
	}
	addKeyByHand(t, state, string(identityPEM(t, "")), "work")
	canvas, _ = paintAt(state, keyPageSize())
	for _, id := range []gfx.ID{hitKeysEdit, hitKeysCopyPrivate} {
		if got := keyPageHit(canvas, id); got.ID.IsZero() || got.Disabled {
			t.Fatalf("with a stored key %v is %+v", id, got)
		}
	}
}
