package workbench

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
	"motty/internal/keyidentity"
	"motty/internal/vault"
)

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
	// Stored marks a key kept in the vault (see stored_keys.go): Path is then its
	// reference, not a file.
	Stored bool
	Error  string
	cells  [4]string
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
	record.refreshCells(messages)
	return record
}

func (record *identityRecord) refreshCells(messages keyMessages) {
	if record.Error != "" {
		record.cells = [4]string{record.Name, messages.Unavailable, "—", record.Path}
		return
	}
	protection := messages.Unencrypted
	if record.Encrypted {
		protection = messages.Passphrase
	}
	algorithm := record.Algorithm
	if algorithm == "" {
		algorithm = messages.EncryptedUnknown
	}
	record.cells = [4]string{record.Name, algorithm, protection, record.Fingerprint}
}

type keyMessages struct {
	Unavailable      string
	Unencrypted      string
	Passphrase       string
	EncryptedUnknown string
}

func (state *State) keyMessages() keyMessages { return keyMessagesIn(state.messages) }

func keyMessagesIn(messages i18n.Messages) keyMessages {
	return keyMessages{
		Unavailable:      messages.KeyUnavailable,
		Unencrypted:      messages.KeyUnencrypted,
		Passphrase:       messages.KeyPassphraseProtected,
		EncryptedUnknown: messages.KeyEncryptedUnknown,
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

func (state *State) hasIdentityPath(path string) bool {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		absolute = filepath.Clean(path)
	}
	return slices.ContainsFunc(state.identities, func(identity identityRecord) bool { return identity.Path == absolute })
}

func (state *State) clickKeys(id gfx.ID) {
	if state.activeSection != sectionKeys || state.modal != modalNone {
		return
	}
	if state.keysMenu {
		// A choice closes the menu and then runs as if it had been clicked
		// directly; any other press only dismisses it.
		state.keysMenu = false
		switch id {
		case hitKeysImport, hitKeysPaste, hitKeysGenerate:
		default:
			return
		}
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
	case id == hitKeysRemove:
		state.removeSelectedIdentity()
	case id == hitKeysPassphrase:
		state.openKeyPassphraseForm()
	}
}

// keysKey closes the Add Key menu on Escape.
func (state *State) keysKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.keysMenu && event.Key == gfx.KeyEscape {
		state.keysMenu = false
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
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
	added := 0
	hadError := false
	for _, path := range paths {
		if path == "" || state.hasIdentityPath(path) {
			continue
		}
		record := inspectIdentity(state.nextIdentityID, path, state.keyMessages())
		if record.Error != "" {
			hadError = true
			state.showActionFailure(state.messages.KeyImportTitle, errors.New(record.Error), fmt.Sprintf(state.messages.ActionFeedback.ImportPrivateKeyFailedFmt, elideMiddleRunes(filepath.Base(path), 64)))
			continue
		}
		state.identities = append(state.identities, record)
		state.selectedIdentityID = record.ID
		state.nextIdentityID++
		added++
	}
	if added > 0 {
		state.refreshIdentityCount()
		state.status = fmt.Sprintf(state.messages.KeyImportedFmt, added)
		state.save()
	} else if !hadError {
		state.showActionInfo(state.messages.KeyImportTitle, state.messages.KeyNothingImported)
	}
}

// openKeyPassphraseForm asks for the passphrase of the selected encrypted key to
// keep in the vault, so connecting does not ask for it again. An empty field
// removes what is saved. The passphrase is kept by the key's path, the name the
// connection looks it up under.
func (state *State) openKeyPassphraseForm() {
	identity, ok := state.identityByID(state.selectedIdentityID)
	if !ok || !identity.Encrypted {
		return
	}
	m := state.messages
	state.openForm(formDialog{
		title: m.KeySavePassphraseTitle, description: fmt.Sprintf(m.KeySavePassphraseFmt, identity.Name), submit: m.KeySavePassphraseSubmit,
		run: func(state *State, form *formDialog) string { return state.keepKeyPassphrase(identity, form.value(0)) },
	}, formField{label: m.KeyPassphraseLabel, secure: true})
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
		_, err := keyidentity.Load(identity.Path, secret)
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
