package workbench

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"motty/internal/keyidentity"
	"motty/internal/sshconn"
)

func TestTabbyVaultChooserUnlocksAndSavesMissingPasswords(t *testing.T) {
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) {
		return []string{filepath.Join("..", "sessionimport", "testdata", "tabby-vault.yaml")}, nil
	}
	state := newTestState(t, options)
	unlock := func() {
		state.Click(hitImportSessions)
		if _, texts := paintedTexts(state); !contains(texts, "Tabby") || contains(texts, "Tabby Vault") {
			t.Fatal("the chooser should offer one Tabby entry")
		}
		state.Click(hitImportTabby)
		if state.modal != modalForm || !state.form.fields[0].secure || state.form.back != nil {
			t.Fatal("the Vault chooser did not open a masked password form")
		}
		state.form.set(0, "wrong")
		state.Click(hitFormSubmit)
		if state.modal != modalForm || state.form.dialogError == "" || state.form.value(0) != "" {
			t.Fatal("the unlock form should discard a rejected master password")
		}
		state.form.set(0, "fixture-vault-password")
		state.Click(hitFormSubmit)
		if state.modal != modalSessionImportPreview || state.form.value(0) != "" {
			t.Fatal("unlock should show the preview and discard the master password")
		}
	}
	unlock()
	if len(state.hosts) != 0 || state.sessionImport.eligible != 2 {
		t.Fatal("unlock should preview two credentials without writing hosts")
	}
	state.Click(hitImportPreviewCancel)
	if len(state.hosts) != 0 || len(state.sessionImport.passwords) != 0 {
		t.Fatal("cancel should discard the preview")
	}
	unlock()
	state.Click(hitImportSavePasswords)
	if len(state.hosts) != 4 || state.modal != modalHostImportResult {
		t.Fatal("confirmed Vault import did not save the hosts")
	}
	name, _ := hostPasswordSecretName(state.hosts[0])
	if value, found := vaultValue(t, state, name); !found || value != "fixture-first" {
		t.Fatal("confirmed Vault import did not save the password")
	}
	if err := state.secrets.vault.Set(name, "existing-password"); err != nil {
		t.Fatal(err)
	}
	state.Click(hitHostImportDone)
	unlock()
	if state.sessionImport.eligible != 0 {
		t.Fatal("repeat import offered to replace existing passwords")
	}
	state.Click(hitImportSavePasswords)
	if value, found := vaultValue(t, state, name); !found || value != "existing-password" || len(state.hosts) != 4 {
		t.Fatal("repeat import changed the existing password or added duplicate hosts")
	}
}

func TestTabbyCanSkipVaultWhenProfilesAreReadable(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "sessionimport", "testdata", "tabby-vault.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	profiles := "profiles:\n  - type: ssh\n    options: {host: primary.example.test, user: ops}\n"
	data := []byte(strings.Replace(string(fixture), "encrypted: true", profiles, 1))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportTabby)
	if state.modal != modalForm || state.form.backLabel != state.messages.Tools.TabbyVaultSkip {
		t.Fatal("readable profiles should have an option to skip unlocking")
	}
	state.Click(hitFormBack)
	if state.modal != modalHostImportResult || len(state.hosts) != 1 {
		t.Fatal("skipping the Vault should import readable profiles")
	}
	name, _ := hostPasswordSecretName(state.hosts[0])
	if _, found := vaultValue(t, state, name); found {
		t.Fatal("skipping the Vault should leave its password unimported")
	}
}

func TestTabbyPreviewRejectsCatalogReload(t *testing.T) {
	options := freshOptions(t)
	options.Group = NewGroup()
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) {
		return []string{filepath.Join("..", "sessionimport", "testdata", "tabby-vault.yaml")}, nil
	}
	first := newTestState(t, options)
	second := newTestState(t, options)
	first.Click(hitImportSessions)
	first.Click(hitImportTabby)
	first.form.set(0, "fixture-vault-password")
	first.Click(hitFormSubmit)
	if first.modal != modalSessionImportPreview || first.sessionImport.eligible != 2 {
		t.Fatal("did not reach the Vault preview")
	}
	name, _ := hostPasswordSecretName(first.sessionImport.plan.hosts[0])
	second.hosts = append(second.hosts, Host{ID: second.nextHostID, Label: "New", Address: "new.example.test"})
	second.nextHostID++
	second.hosts[0].finish()
	if err := second.saveWithResultMode(false); err != nil {
		t.Fatal(err)
	}
	if len(first.hosts) != 1 || first.hosts[0].Label != "New" {
		t.Fatal("the new host did not reach the first window")
	}
	first.Click(hitImportSavePasswords)
	if first.dialogError != first.messages.Tools.ImportPreviewStale || len(first.hosts) != 1 {
		t.Fatal("a preview from before the reload should be rejected")
	}
	if _, found := vaultValue(t, first, name); found {
		t.Fatal("the stale import saved a credential")
	}
}

func TestTabbyPasswordHostImportsWithoutCredentialPreview(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("profiles:\n  - type: ssh\n    options:\n      host: example.test\n      auth: password\n")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportTabby)
	if state.modal != modalHostImportResult || len(state.hosts) != 1 || state.hosts[0].Auth != authPassword {
		t.Fatal("a password host without embedded credentials should import immediately")
	}
	name, _ := hostPasswordSecretName(state.hosts[0])
	if _, found := vaultValue(t, state, name); found {
		t.Fatal("a host-only import saved a password")
	}
}

func TestTabbyMigratesLocalKeyAndKeepsMottyPassphrase(t *testing.T) {
	const passphrase = "fixture-key-passphrase"
	key := writeWorkbenchIdentity(t, "tabby identity", passphrase)
	keyBytes, err := os.ReadFile(key)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "config.yaml")
	yaml := `profiles:
  - type: ssh
    name: Key host
    options:
      host: key.example.test
      auth: publicKey
      privateKeys: ['file://` + key + `']
  - type: ssh
    name: Password host
    options:
      host: password.example.test
      user: deploy
      auth: password
`
	if err := os.WriteFile(file, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	state := newTestState(t, options)
	choose := func() {
		state.Click(hitImportSessions)
		state.Click(hitImportTabby)
		if state.modal != modalSessionImportPreview {
			t.Fatal("expected Tabby preview")
		}
	}
	choose()
	state.Click(hitImportConnectionsOnly)
	if len(state.hosts) != 2 || len(state.identities) != 0 {
		t.Fatal("connections-only import unexpectedly copied the private key")
	}
	state.Click(hitHostImportDone)
	choose()
	previousLocal := state.configBase.local
	state.lastSaved, state.hasSaved = state.snapshot(), true
	state.configBase.local = []byte("{invalid local state")
	state.Click(hitImportSavePasswords)
	state.configBase.local = previousLocal
	if state.modal != modalSessionImportPreview || len(state.identities) != 0 ||
		!slices.Equal(state.hosts[0].SSHOptions.IdentityFiles, []string{key}) {
		t.Fatal("failed config write did not restore the previous host settings")
	}
	if secrets, err := state.secrets.vault.Export(); err != nil || len(secrets) != 0 {
		t.Fatal("failed config write left new credentials in the vault")
	}
	state.Click(hitImportSavePasswords)
	if state.modal != modalHostImportResult || len(state.hosts) != 2 || len(state.identities) != 1 {
		t.Fatal("expected passwords and keys to migrate into the existing catalog")
	}
	ref := state.identities[0].Path
	name, _ := hostPasswordSecretName(state.hosts[1])
	if _, found := vaultValue(t, state, name); found {
		t.Fatal("the key import saved a password absent from the config")
	}
	if _, found := vaultValue(t, state, passphraseSecretName(ref)); found {
		t.Fatal("the key import saved a passphrase Motty did not have")
	}
	if err := state.secrets.vault.Set(passphraseSecretName(key), passphrase); err != nil {
		t.Fatal(err)
	}
	if !sshconn.IsStoredKey(ref) || !slices.Equal(state.hosts[0].SSHOptions.IdentityFiles, []string{ref}) {
		t.Fatal("the existing host does not reference the migrated key")
	}
	state.Click(hitHostImportDone)
	choose()
	state.Click(hitImportSavePasswords)
	if len(state.identities) != 1 {
		t.Fatal("a repeated import duplicated a key")
	}

	restarted := newTestState(t, options)
	keyData, err := readStoredKey(restarted.secrets.vault, ref)
	if err != nil || !bytes.Equal(keyData, keyBytes) {
		t.Fatal("the saved key was not retained")
	}
	value, found := vaultValue(t, restarted, passphraseSecretName(ref))
	if _, err := keyidentity.Parse(keyData, []byte(value)); !found || err != nil {
		t.Fatal("the stored passphrase did not unlock the migrated key")
	}
	if !slices.Equal(restarted.hosts[0].SSHOptions.IdentityFiles, []string{ref}) {
		t.Fatal("the stored key reference was not retained")
	}
}
