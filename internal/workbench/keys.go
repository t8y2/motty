package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"golang.org/x/crypto/ssh"
	"motty/internal/i18n"
	"motty/internal/keyidentity"
	"motty/internal/sshconn"
	"motty/internal/vault"
)

// keyTableSource reads the keychain page's table cells when the table asks for
// them, one visible cell at a time.
type keyTableSource struct {
	records *[]identityRecord
}

func (source *keyTableSource) Len() int {
	if source == nil || source.records == nil {
		return 0
	}
	return len(*source.records)
}

func (source *keyTableSource) Cell(row, column int) string {
	if source == nil || source.records == nil {
		return ""
	}
	records := *source.records
	if row < 0 || row >= len(records) || column < 0 || column >= len(records[row].cells) {
		return ""
	}
	return records[row].cells[column]
}

// syncKeyTable makes the table show the key the application selected.
func (state *State) syncKeyTable() {
	row := -1
	for index := range state.identities {
		if state.identities[index].ID == state.selectedIdentityID {
			row = index
			break
		}
	}
	state.keysTable.Select(row)
}

// takeKeyTableSelection adopts the row a key moved the table to.
func (state *State) takeKeyTableSelection() {
	row := state.keysTable.Selected()
	if row >= 0 && row < len(state.identities) {
		state.selectIdentity(state.identities[row].ID)
	}
}

type identityRecord struct {
	ID          uint32
	Path        string
	Name        string
	Algorithm   string
	Fingerprint string
	// PublicKey is the key as a server's authorized_keys line; empty while the key
	// is locked and its public half is not readable.
	PublicKey string
	Encrypted bool
	// Certificate is true when the key signs in with an OpenSSH user
	// certificate: kept with a stored key in the vault, or in the -cert.pub file
	// beside an identity file.
	Certificate bool
	// Stored marks a key kept in the vault (see stored_keys.go): Path is then its
	// reference, not a file.
	Stored bool
	// SecurityKey marks a FIDO2/U2F key. It signs through an ssh-agent that
	// reaches the device; the passphrase is asked by ssh-add, not saved here.
	SecurityKey bool
	Error       string
	cells       [5]string
}

func inspectIdentity(id uint32, path string, messages keyMessages) identityRecord {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		absolute = filepath.Clean(path)
	}
	record := identityRecord{ID: id, Path: absolute, Name: filepath.Base(absolute)}
	info, inspectErr := keyidentity.Inspect(absolute)
	if inspectErr != nil {
		record.Error = inspectErr.Error()
		record.refreshCells(messages)
		return record
	}
	record.Path = info.Path
	record.Name = info.Name
	record.Algorithm = info.Algorithm
	record.Fingerprint = info.Fingerprint
	record.PublicKey = info.PublicKey
	record.Encrypted = info.Encrypted
	record.SecurityKey = info.SecurityKey
	record.Certificate = sshconn.CertificateBeside(absolute)
	record.refreshCells(messages)
	return record
}

func (record *identityRecord) refreshCells(messages keyMessages) {
	if record.Error != "" {
		record.cells = [5]string{record.Name, messages.Unavailable, "—", "—", record.Path}
		return
	}
	protection := messages.Unencrypted
	if record.SecurityKey {
		protection = messages.SecurityKey
	} else if record.Encrypted {
		protection = messages.Passphrase
	}
	algorithm := record.Algorithm
	if algorithm == "" {
		algorithm = messages.EncryptedUnknown
	}
	certificate := "—"
	if record.Certificate {
		certificate = messages.CertificatePresent
	}
	record.cells = [5]string{record.Name, algorithm, protection, certificate, record.Fingerprint}
}

type keyMessages struct {
	Unavailable        string
	Unencrypted        string
	Passphrase         string
	SecurityKey        string
	EncryptedUnknown   string
	CertificatePresent string
}

func (state *State) keyMessages() keyMessages { return keyMessagesIn(state.messages) }

func keyMessagesIn(messages i18n.Messages) keyMessages {
	return keyMessages{
		Unavailable:        messages.KeyUnavailable,
		Unencrypted:        messages.KeyUnencrypted,
		Passphrase:         messages.KeyPassphraseProtected,
		SecurityKey:        messages.KeySecurityKey,
		EncryptedUnknown:   messages.KeyEncryptedUnknown,
		CertificatePresent: messages.KeyCertificatePresent,
	}
}

func (state *State) restoreIdentities(paths []string) {
	clear(state.identities)
	state.identities = append(state.identities[:0], decodeIdentities(paths, state.keyMessages())...)
	state.nextIdentityID = uint32(len(state.identities)) + 1
	if len(state.identities) > 0 {
		state.selectedIdentityID = state.identities[0].ID
	}
	state.reloadStoredIdentities()
}

func (state *State) clickKeys(id gfx.ID) {
	if state.activeSection != sectionKeys || state.modal != modalNone {
		return
	}
	switch {
	case id == hitKeysAdd:
		state.keysMenu = true
	case id == hitKeysImport:
		state.importKeys()
	case id == hitKeysPaste:
		state.openPasteKeyForm()
	case id == hitKeysGenerate:
		state.openGenerateKeyForm()
	case id == hitKeysCopyPublic:
		state.copySelectedPublicKey()
	case id == hitKeysExport:
		state.exportSelectedKey()
	case id == hitKeysRemove:
		state.removeSelectedIdentity()
	case id == hitKeysPassphrase:
		state.openKeyPassphraseForm()
	case id == hitKeysCertificate:
		state.chooseCertificate()
	}
}

// selectIdentity makes a key the selected one. The table calls it from the paint
// that delivers the click on its row.
func (state *State) selectIdentity(id uint32) {
	if _, ok := state.identityByID(id); ok {
		state.selectedIdentityID = id
		state.status = state.messages.KeySelected + state.identityName(id)
	}
}

func (state *State) importKeys() {
	paths, err := state.options.OpenFiles(app.OpenFileOptions{
		Title: state.messages.KeyImportTitle, AllowMultiple: true,
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		state.status = state.messages.KeyImportCanceled
		return
	}
	if err != nil {
		state.showActionFailure(state.messages.KeyImportTitle, err, state.messages.ActionFeedback.FilePickerFailed)
		return
	}
	state.importKeyFiles(paths)
}

// importKeyFiles keeps the private keys in files in the vault, as a pasted key is
// kept, so a key no longer depends on the file it came from: moving or deleting
// the file leaves it as it was, and it travels with the vault to the user's other
// computers. A file already listed as a key, because it was imported before the
// key was kept, is upgraded in place: same row, same host pins, same saved
// passphrase. A security key's file is only a handle the agent resolves by path,
// so it stays a file.
func (state *State) importKeyFiles(paths []string) {
	m := state.messages
	vaultFile := state.secrets.vault
	if vaultFile == nil {
		state.showActionFailure(m.KeyImportTitle, vault.ErrUnavailable, m.VaultUnavailable)
		return
	}
	if vaultFile.NeedsConversion() {
		state.openVaultUnlock(nil, func() { state.importKeyFiles(paths) })
		return
	}
	added := 0
	hadError := false
	certificateAttached := false
	for _, path := range paths {
		if path == "" {
			continue
		}
		absolute, err := filepath.Abs(filepath.Clean(path))
		if err != nil {
			absolute = filepath.Clean(path)
		}
		name := filepath.Base(absolute)
		data, err := keyidentity.ReadFile(absolute)
		var info keyidentity.Info
		if err == nil {
			info, err = keyidentity.InspectData(name, data)
		}
		if err != nil {
			hadError = true
			state.showActionFailure(m.KeyImportTitle, err, fmt.Sprintf(m.ActionFeedback.ImportPrivateKeyFailedFmt, elideMiddleRunes(name, 64)))
			continue
		}
		known := slices.IndexFunc(state.identities, func(identity identityRecord) bool {
			return !identity.Stored && identity.Path == absolute ||
				identity.Error == "" && info.Fingerprint != "" && identity.Fingerprint == info.Fingerprint
		})
		var certificate string
		if !info.SecurityKey {
			var certificateErr error
			certificate, certificateErr = companionCertificate(absolute, info.PublicKey)
			if certificateErr != nil {
				// The key is still worth importing; the certificate it came
				// with is reported so the user can attach a good one.
				hadError = true
				state.showActionFailure(m.KeyImportTitle, certificateErr, fmt.Sprintf(m.KeyCertificateIgnoredFmt, name))
			}
		}
		if known >= 0 && state.identities[known].Stored {
			// The key is already kept. A certificate beside the file it came from
			// fills a gap, but never replaces one the user attached here.
			if certificate != "" && !state.identities[known].Certificate {
				if err := state.storeCertificate(state.identities[known].Path, certificate); err != nil {
					state.showActionFailure(m.KeyImportTitle, err, fmt.Sprintf(m.VaultSaveFailedFmt, state.vaultErrorText(err)))
					continue
				}
				state.identities[known].Certificate = true
				state.identities[known].refreshCells(state.keyMessages())
				state.showActionInfo(m.KeyCertificate, fmt.Sprintf(m.KeyCertificateAttachedFmt, state.identities[known].Name))
				certificateAttached = true
			}
			continue
		}
		if known >= 0 && info.SecurityKey {
			continue // already listed
		}
		if info.SecurityKey {
			record := inspectIdentity(state.nextIdentityID, absolute, state.keyMessages())
			state.identities = append(state.identities, record)
			state.selectedIdentityID = record.ID
			state.nextIdentityID++
			added++
			continue
		}
		var replaces *identityRecord
		if known >= 0 {
			replaces = &state.identities[known]
		}
		record, err := state.sealStoredKey(storedKey{Name: name, Key: string(data), Certificate: certificate}, replaces)
		if err != nil {
			hadError = true
			state.showActionFailure(m.KeyImportTitle, err, fmt.Sprintf(m.VaultSaveFailedFmt, state.vaultErrorText(err)))
			continue
		}
		if known >= 0 {
			state.identities[known] = record
		} else {
			state.identities = append(state.identities, record)
		}
		state.selectedIdentityID = record.ID
		added++
	}
	if added > 0 {
		state.refreshIdentityCount()
		state.status = fmt.Sprintf(m.KeyImportedFmt, added)
		state.save()
	} else if !hadError && !certificateAttached {
		state.showActionInfo(m.KeyImportTitle, m.KeyNothingImported)
	}
}

// companionCertificate is the OpenSSH user certificate beside an imported key,
// as its authorized_keys line, when the key comes with one. public may be nil
// when the key's public half is not readable before its passphrase.
func companionCertificate(identity, publicKey string) (string, error) {
	var public ssh.PublicKey
	if publicKey != "" {
		public, _, _, _, _ = ssh.ParseAuthorizedKey([]byte(publicKey))
	}
	certificate, err := sshconn.ReadCertificate(identity+"-cert.pub", public)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return sshconn.CertificateLine(certificate), nil
}

// chooseCertificate attaches the OpenSSH user certificate of the selected key,
// or replaces the one it has. Certificates are short-lived, so this is how a key
// that stays in Motty keeps signing in after its old certificate expired.
func (state *State) chooseCertificate() {
	identity, ok := state.identityByID(state.selectedIdentityID)
	if !ok || identity.Error != "" || !identity.Stored {
		return
	}
	m := state.messages
	paths, err := state.options.OpenFiles(app.OpenFileOptions{Title: m.KeyCertificateTitle})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		return
	}
	if err != nil {
		state.showActionFailure(m.KeyCertificateTitle, err, m.ActionFeedback.FilePickerFailed)
		return
	}
	certificate, err := sshconn.ReadCertificate(paths[0], certificatePublicKey(identity))
	if err != nil {
		state.showActionFailure(m.KeyCertificateTitle, err, m.KeyCertificateInvalid)
		return
	}
	state.keepCertificate(identity, sshconn.CertificateLine(certificate))
}

// certificatePublicKey is the public half the certificate of a stored key must
// certify. It is nil when the key cannot name its public half before its
// passphrase; the match is then checked when the key is used.
func certificatePublicKey(identity identityRecord) ssh.PublicKey {
	if identity.PublicKey == "" {
		return nil
	}
	public, _, _, _, err := ssh.ParseAuthorizedKey([]byte(identity.PublicKey))
	if err != nil {
		return nil
	}
	return public
}

// keepCertificate writes the certificate into the stored key in the vault and
// shows it in the list.
func (state *State) keepCertificate(identity identityRecord, certificate string) {
	m := state.messages
	vaultFile := state.secrets.vault
	if vaultFile == nil {
		state.showActionInfo(m.KeyCertificateTitle, m.VaultUnavailable)
		return
	}
	if vaultFile.NeedsConversion() {
		state.openVaultUnlock(nil, func() { state.keepCertificate(identity, certificate) })
		return
	}
	if err := state.storeCertificate(identity.Path, certificate); err != nil {
		state.showActionFailure(m.KeyCertificateTitle, err, fmt.Sprintf(m.VaultSaveFailedFmt, state.vaultErrorText(err)))
		return
	}
	for index := range state.identities {
		if state.identities[index].ID == identity.ID {
			state.identities[index].Certificate = certificate != ""
			state.identities[index].refreshCells(state.keyMessages())
			break
		}
	}
	state.showActionInfo(m.KeyCertificate, fmt.Sprintf(m.KeyCertificateAttachedFmt, identity.Name))
}

// openKeyPassphraseForm asks for the passphrase of the selected encrypted key to
// keep in the vault, so connecting does not ask for it again. An empty field
// removes what is saved. The passphrase is kept by the key's path, the name the
// connection looks it up under.
func (state *State) openKeyPassphraseForm() {
	identity, ok := state.identityByID(state.selectedIdentityID)
	if !ok || !identity.Encrypted || identity.SecurityKey {
		return
	}
	m := state.messages
	state.openForm(formDialog{
		title: m.KeySavePassphraseTitle, description: fmt.Sprintf(m.KeySavePassphraseFmt, identity.Name), submit: m.KeySavePassphraseSubmit,
		run: func(state *State, form *formDialog) string { return state.keepKeyPassphrase(identity, form.value(0)) },
	}, formField{label: m.KeyPassphraseLabel, secure: true})
}

// keyPassphraseOpens reports whether passphrase opens the private key the
// identity names. A key kept in the vault is read from there, exactly as a
// connection reads it; a file is read from disk. Callers own the passphrase
// bytes and can clear them as soon as this returns.
func (state *State) keyPassphraseOpens(identity identityRecord, passphrase []byte) error {
	if !identity.Stored {
		_, err := keyidentity.Load(identity.Path, passphrase)
		return err
	}
	if state.secrets.vault == nil {
		return vault.ErrUnavailable
	}
	data, err := readStoredKey(state.secrets.vault, identity.Path)
	if err != nil {
		return err
	}
	defer clear(data)
	_, err = keyidentity.Parse(data, passphrase)
	return err
}

// keepKeyPassphrase saves, or with an empty passphrase forgets, the passphrase
// of identity. A passphrase that does not open the key is refused: it would
// only fail at the next connection, after the saved one is tried and forgotten.
func (state *State) keepKeyPassphrase(identity identityRecord, passphrase string) string {
	m := state.messages
	vaultFile := state.secrets.vault
	if vaultFile == nil {
		return m.VaultUnavailable
	}
	if passphrase != "" {
		secret := []byte(passphrase)
		err := state.keyPassphraseOpens(identity, secret)
		clear(secret)
		if err != nil {
			return m.KeyPassphraseWrong
		}
	}
	if vaultFile.NeedsConversion() {
		state.openVaultUnlock(nil, func() {
			if message := state.keepKeyPassphrase(identity, passphrase); message != "" {
				state.status = message
			}
		})
		return ""
	}
	name := passphraseSecretName(identity.Path)
	var err error
	if passphrase == "" {
		err = vaultFile.Delete(name)
	} else {
		err = vaultFile.Set(name, passphrase)
	}
	if err != nil {
		return fmt.Sprintf(m.VaultSaveFailedFmt, state.vaultErrorText(err))
	}
	if passphrase == "" {
		state.status = fmt.Sprintf(m.KeyPassphraseRemovedFmt, identity.Name)
	} else {
		state.status = fmt.Sprintf(m.KeyPassphraseSavedFmt, identity.Name)
	}
	return ""
}

func (state *State) removeSelectedIdentity() {
	for index, identity := range state.identities {
		if identity.ID != state.selectedIdentityID {
			continue
		}
		for _, host := range state.hosts {
			if host.Auth == authPrivateKey && identity.Fingerprint != "" && host.Identity == identity.Fingerprint {
				state.showActionWarning(state.messages.KeyTitle, fmt.Sprintf(state.messages.KeyInUseFmt, host.Label))
				return
			}
		}
		if identity.Stored {
			state.confirmDeleteStoredKey(identity)
			return
		}
		state.identities = slices.Delete(state.identities, index, index+1)
		state.selectedIdentityID = 0
		if len(state.identities) > 0 {
			state.selectedIdentityID = state.identities[min(index, len(state.identities)-1)].ID
		}
		state.status = state.messages.KeyRemoved + identity.Name
		state.refreshIdentityCount()
		state.save()
		return
	}
}

func (state *State) refreshIdentityCount() {
	state.identityCountText = fmt.Sprintf(state.messages.KeyCountFmt, len(state.identities))
}

func (state *State) refreshIdentityCells() {
	messages := state.keyMessages()
	for index := range state.identities {
		state.identities[index].refreshCells(messages)
	}
}

func (state *State) identityByID(id uint32) (identityRecord, bool) {
	for _, identity := range state.identities {
		if identity.ID == id {
			return identity, true
		}
	}
	return identityRecord{}, false
}

// identityLabel names the key a connection asks the passphrase of: the name of a
// stored key, the file name of a file.
func (state *State) identityLabel(ref string) string {
	for _, identity := range state.identities {
		if identity.Path == ref && identity.Stored {
			return identity.Name
		}
	}
	return filepath.Base(ref)
}

func (state *State) identityName(id uint32) string {
	identity, _ := state.identityByID(id)
	return identity.Name
}

// openPasteKeyForm asks for the text of a private key to keep in the vault, so
// a key never has to be a file first.
func (state *State) openPasteKeyForm() {
	m := state.messages
	state.openForm(formDialog{
		title: m.KeyPasteTitle, description: m.KeyPasteDescription, submit: m.KeyPasteSubmit,
		run: func(state *State, form *formDialog) string {
			message := state.pasteKey(form.value(1), form.value(0))
			if message == "" {
				form.set(0, "") // the key does not stay in the form's memory
			}
			return message
		},
	},
		formField{label: m.KeyTextLabel, placeholder: m.KeyTextPlaceholder, multiline: true},
		formField{label: m.KeyNameLabel, placeholder: m.KeyNamePlaceholder},
	)
}

// pasteKey keeps pasted key text under name (a default when it is empty). It
// returns what is wrong with it, "" when it was kept.
func (state *State) pasteKey(name, text string) string {
	data, err := keyidentity.Normalize(text)
	if err != nil {
		return fmt.Sprintf(state.messages.KeyInvalidTextFmt, err)
	}
	return state.addStoredKey(name, data, func(record identityRecord) {
		state.status = fmt.Sprintf(state.messages.KeyAddedFmt, record.Name)
		state.save()
	})
}

// openGenerateKeyForm asks for the name, and optionally the passphrase, of a new
// key. The name is also the comment the public key carries, which is what
// identifies it in a server's authorized_keys.
func (state *State) openGenerateKeyForm() {
	m := state.messages
	state.openForm(formDialog{
		title: m.KeyGenerateTitle, description: m.KeyGenerateDescription, submit: m.KeyGenerateSubmit,
		run: func(state *State, form *formDialog) string {
			return state.generateKey(form.value(0), form.value(1))
		},
	},
		formField{label: m.KeyNameLabel, placeholder: m.KeyGenerateNamePlaceholder},
		formField{label: m.KeyGeneratePassphraseLabel, secure: true},
	)
}

// generateKey makes an Ed25519 key, keeps it in the vault and puts its public
// key on the clipboard: the next thing anyone does with a new key is to add that
// line to a server.
func (state *State) generateKey(name, passphrase string) string {
	m := state.messages
	name = strings.TrimSpace(name)
	if name == "" {
		return m.KeyNameRequired
	}
	secret := []byte(passphrase)
	generated, err := keyidentity.Generate(name, secret)
	clear(secret)
	if err != nil {
		return fmt.Sprintf(m.KeyInvalidTextFmt, err)
	}
	return state.addStoredKey(name, generated.Private, func(record identityRecord) {
		if err := state.options.SetClipboardText(generated.Public); err != nil {
			state.status = fmt.Sprintf(m.KeyGeneratedNotCopiedFmt, record.Name)
			return
		}
		state.status = fmt.Sprintf(m.KeyGeneratedFmt, record.Name)
	})
}

// copySelectedPublicKey puts the public key of the selected key on the clipboard.
func (state *State) copySelectedPublicKey() {
	identity, ok := state.identityByID(state.selectedIdentityID)
	if !ok || identity.Error != "" {
		return
	}
	if identity.PublicKey == "" {
		state.showActionInfo(state.messages.KeyTitle, state.messages.KeyPublicUnavailable)
		return
	}
	if err := state.options.SetClipboardText(identity.PublicKey); err != nil {
		state.status = fmt.Sprintf(state.messages.StatusClipboardFailedFmt, err)
		return
	}
	state.status = fmt.Sprintf(state.messages.KeyPublicCopiedFmt, identity.Name)
}

// exportSelectedKey saves the selected private key to a file the user chooses,
// exactly as it is kept: a passphrase-protected key stays protected. It is the
// way back out for a key that exists only in Motty, and the plain way to move a
// key to another tool or computer.
func (state *State) exportSelectedKey() {
	identity, ok := state.identityByID(state.selectedIdentityID)
	if !ok || identity.Error != "" {
		return
	}
	m := state.messages
	if vaultFile := state.secrets.vault; identity.Stored && vaultFile != nil && vaultFile.NeedsConversion() {
		state.openVaultUnlock(nil, state.exportSelectedKey)
		return
	}
	data, err := state.identityKeyData(identity)
	if err != nil {
		state.showFileReadFailure(m.KeyExportTitle, err, m.KeyExportReadFailed)
		return
	}
	defer clear(data)
	path, err := state.options.SaveFile(app.SaveFileOptions{Title: m.KeyExportTitle, SuggestedName: keyExportName(identity.Name)})
	if errors.Is(err, app.ErrCanceled) || err == nil && path == "" {
		return
	}
	if err != nil {
		state.showActionFailure(m.KeyExportTitle, err, m.ActionFeedback.FilePickerFailed)
		return
	}
	if err := writePrivateAtomic(path, data); err != nil {
		state.showFileSaveFailure(m.KeyExportTitle, err)
		return
	}
	state.status = fmt.Sprintf(m.KeyExportedFmt, identity.Name)
}

// identityKeyData reads the private key a record names: from the vault for a
// stored key, from its file otherwise. The caller owns the bytes.
func (state *State) identityKeyData(identity identityRecord) ([]byte, error) {
	if !identity.Stored {
		return keyidentity.ReadFile(identity.Path)
	}
	if state.secrets.vault == nil {
		return nil, vault.ErrUnavailable
	}
	return readStoredKey(state.secrets.vault, identity.Path)
}

// keyExportName turns a key's name into a file name the platforms accept: the
// characters a path or a file system reserves become underscores.
func keyExportName(name string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < ' ' || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name))
	if name = strings.Trim(name, "."); name == "" {
		return "id_motty"
	}
	return name
}

// confirmDeleteStoredKey asks before deleting a key that exists nowhere else.
func (state *State) confirmDeleteStoredKey(identity identityRecord) {
	m := state.messages
	state.openForm(formDialog{
		title: m.KeyDeleteTitle, description: fmt.Sprintf(m.KeyDeleteDescriptionFmt, identity.Name), submit: m.KeyDeleteConfirm, danger: true,
		run: func(state *State, _ *formDialog) string { return state.deleteStoredKey(identity) },
	})
}

// deleteStoredKey removes a stored key and the passphrase saved for it.
func (state *State) deleteStoredKey(identity identityRecord) string {
	m := state.messages
	vaultFile := state.secrets.vault
	if vaultFile == nil {
		return m.VaultUnavailable
	}
	if vaultFile.NeedsConversion() {
		state.openVaultUnlock(nil, func() {
			if message := state.deleteStoredKey(identity); message != "" {
				state.status = message
			}
		})
		return ""
	}
	err := vaultFile.Apply([]vault.Change{
		{Name: identity.Path, Delete: true},
		{Name: passphraseSecretName(identity.Path), Delete: true},
	})
	if err != nil {
		return fmt.Sprintf(m.VaultSaveFailedFmt, state.vaultErrorText(err))
	}
	index := slices.IndexFunc(state.identities, func(other identityRecord) bool { return other.Path == identity.Path })
	if index >= 0 {
		state.identities = slices.Delete(state.identities, index, index+1)
		state.selectedIdentityID = 0
		if len(state.identities) > 0 {
			state.selectedIdentityID = state.identities[min(index, len(state.identities)-1)].ID
		}
	}
	state.refreshIdentityCount()
	state.status = fmt.Sprintf(m.KeyDeletedFmt, identity.Name)
	return ""
}
