package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/backup"
	"motty/internal/keyidentity"
)

func TestABackupCarriesPrivateKeysOnlyWhenAsked(t *testing.T) {
	state, _, _ := backupTestState(t)
	path := writeWorkbenchIdentity(t, "id_ed25519", "")
	state.restoreIdentities([]string{path})

	document, _, err := state.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Keys) != 0 {
		t.Fatalf("a backup carried %d keys without being asked", len(document.Keys))
	}

	state.Click(hitBackupKeys)
	if !state.backupKeys {
		t.Fatal("the row did not turn carrying keys on")
	}
	document, _, err = state.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Keys) != 1 {
		t.Fatalf("keys in the backup: %d", len(document.Keys))
	}
	key := document.Keys[0]
	if key.Fingerprint != state.identities[0].Fingerprint || key.Name != "id_ed25519" || len(key.Data) == 0 {
		t.Fatalf("the key in the backup: %+v", key)
	}
}

func TestTheExportDialogSaysWhetherKeysGoInAndTheAnswerSticks(t *testing.T) {
	state, _, file := backupTestState(t)
	path := writeWorkbenchIdentity(t, "id_ed25519", "")
	state.restoreIdentities([]string{path})

	state.Click(hitBackupExport)
	if state.modal != modalForm {
		t.Fatal("the export form did not open")
	}
	if state.form.checkLabel != state.messages.Tools.BackupKeys {
		t.Fatalf("the form asks %q", state.form.checkLabel)
	}
	if state.form.checked {
		t.Fatal("the box starts ticked")
	}
	state.Click(hitFormCheckbox)
	if !state.form.checked {
		t.Fatal("the box did not tick")
	}
	fillForm(state, testBackupPassword, testBackupPassword)
	state.Click(hitFormSubmit)
	if state.modal != modalNone {
		t.Fatalf("export refused: %q", state.form.dialogError)
	}
	if !state.backupKeys {
		t.Fatal("ticking the box for one export did not keep the choice")
	}
	sealed, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	document, err := backup.Open(sealed, testBackupPassword)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Keys) != 1 {
		t.Fatalf("keys in the exported file: %d", len(document.Keys))
	}
}

func TestRestoringABackupWritesItsKeysWhereTheHostsFindThem(t *testing.T) {
	source, _, file := backupTestState(t)
	path := writeWorkbenchIdentity(t, "id_ed25519", "secret")
	source.restoreIdentities([]string{path})
	fingerprint := source.identities[0].Fingerprint
	if err := source.secrets.vault.Set(passphraseSecretName(path), "secret"); err != nil {
		t.Fatal(err)
	}
	source.backupKeys = true
	source.save()
	exportBackup(t, source)

	_, targetOptions := newWorld(t)
	targetOptions.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{file}, nil }
	target := newTestState(t, targetOptions)
	target.activeSection, target.settingsCategory = sectionSettings, settingsBackup

	target.Click(hitBackupRestore)
	if target.modal != modalForm {
		t.Fatal("the restore form did not open")
	}
	fillForm(target, testBackupPassword)
	target.Click(hitFormSubmit)
	if target.modal != modalNone {
		t.Fatalf("restore refused: %q", target.form.dialogError)
	}

	restored := filepath.Join(filepath.Dir(targetOptions.StatePath), "keys", "id_ed25519")
	data, err := os.ReadFile(restored)
	if err != nil {
		t.Fatalf("the key was not written: %v", err)
	}
	if info, err := keyidentity.Inspect(restored); err != nil || info.Fingerprint != fingerprint {
		t.Fatalf("the written key: %+v %v", info, err)
	}
	if len(data) == 0 {
		t.Fatal("the written key is empty")
	}
	if info, err := os.Stat(restored); err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("the written key's mode: %v %v", info.Mode(), err)
	}
	if _, ok := target.identityForFingerprint(fingerprint); !ok {
		t.Fatal("the restored key is not in the keys page, so a host naming it would not find it")
	}
	value, found, err := target.secrets.vault.Get(passphraseSecretName(restored))
	if err != nil || !found || value != "secret" {
		t.Fatalf("the passphrase under its new path: %q %v %v", value, found, err)
	}
}

func TestASyncedComputerWithoutTheKeyToggleOnlyPassesTheServerKeysOn(t *testing.T) {
	state, _, _ := backupTestState(t)
	path := writeWorkbenchIdentity(t, "id_ed25519", "")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := keyidentity.Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	remote := backup.Key{Fingerprint: info.Fingerprint, Name: "id_ed25519", Data: data}

	outgoing, err := state.mergeRemote(backup.Document{Keys: []backup.Key{remote}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if outgoing == nil || len(outgoing.Keys) != 1 || outgoing.Keys[0].Fingerprint != info.Fingerprint {
		t.Fatalf("what goes back: %+v", outgoing)
	}
	if len(state.identities) != 0 {
		t.Fatalf("a computer that did not ask for keys registered %d", len(state.identities))
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(state.options.StatePath), "keys", "id_ed25519")); err == nil {
		t.Fatal("a computer that did not ask for keys wrote one")
	}
}

func TestASyncedComputerWithTheKeyToggleWritesTheServerKeys(t *testing.T) {
	state, _, _ := backupTestState(t)
	state.backupKeys = true
	path := writeWorkbenchIdentity(t, "id_ed25519", "")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := keyidentity.Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	remote := backup.Key{Fingerprint: info.Fingerprint, Name: "id_ed25519", Data: data}

	outgoing, err := state.mergeRemote(backup.Document{Keys: []backup.Key{remote}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if outgoing == nil || len(outgoing.Keys) != 1 {
		t.Fatalf("what goes back: %+v", outgoing)
	}
	written := filepath.Join(filepath.Dir(state.options.StatePath), "keys", "id_ed25519")
	if _, err := os.Stat(written); err != nil {
		t.Fatalf("the key from the server was not written: %v", err)
	}
	if _, ok := state.identityForFingerprint(info.Fingerprint); !ok {
		t.Fatal("the key from the server is not in the keys page")
	}
}

func TestTheBackupPageAndTheExportDialogBothOfferTheKeyChoice(t *testing.T) {
	state, _, _ := backupTestState(t)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitBackupKeys); !ok {
		t.Fatal("the backup page has no switch for private keys")
	}
	state.Click(hitBackupExport)
	if state.modal != modalForm {
		t.Fatal("the export form did not open")
	}
	canvas.Reset()
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitFormCheckbox); !ok {
		t.Fatal("the export form has no box for private keys")
	}
}
