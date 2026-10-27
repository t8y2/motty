package workbench

import (
	"errors"
	"fmt"
	"github.com/vivaui/viva/ui"
	"path/filepath"
	"slices"

	"github.com/vivaui/viva/app"
	"motty/internal/keyidentity"
)

type identityRecord struct {
	ID          uint32
	Path        string
	Name        string
	Algorithm   string
	Fingerprint string
	Encrypted   bool
	Error       string
	cells       [4]string
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

func (state *State) keyMessages() keyMessages {
	return keyMessages{
		Unavailable:      state.messages.KeyUnavailable,
		Unencrypted:      state.messages.KeyUnencrypted,
		Passphrase:       state.messages.KeyPassphraseProtected,
		EncryptedUnknown: state.messages.KeyEncryptedUnknown,
	}
}

func (state *State) identityPaths() []string {
	paths := make([]string, 0, len(state.identities))
	for _, identity := range state.identities {
		if identity.Error == "" {
			paths = append(paths, identity.Path)
		}
	}
	return paths
}

func (state *State) restoreIdentities(paths []string) {
	clear(state.identities)
	state.identities = state.identities[:0]
	state.nextIdentityID = 1
	for _, path := range paths {
		if path == "" || state.hasIdentityPath(path) {
			continue
		}
		state.identities = append(state.identities, inspectIdentity(state.nextIdentityID, path, state.keyMessages()))
		state.nextIdentityID++
	}
	if len(state.identities) > 0 {
		state.selectedIdentityID = state.identities[0].ID
	}
	state.refreshIdentityCount()
}

func (state *State) hasIdentityPath(path string) bool {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		absolute = filepath.Clean(path)
	}
	return slices.ContainsFunc(state.identities, func(identity identityRecord) bool { return identity.Path == absolute })
}

func (state *State) clickKeys(id ui.ID) {
	if state.activeSection != sectionKeys || state.modal != modalNone {
		return
	}
	switch {
	case id == hitKeysImport:
		state.importKeys()
	case id == hitKeysRemove:
		state.removeSelectedIdentity()
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
	if errors.Is(err, app.ErrCanceled) {
		state.status = state.messages.KeyImportCanceled
		return
	}
	if err != nil {
		state.status = fmt.Sprintf(state.messages.KeyImportFailedFmt, err)
		return
	}
	added := 0
	for _, path := range paths {
		if path == "" || state.hasIdentityPath(path) {
			continue
		}
		record := inspectIdentity(state.nextIdentityID, path, state.keyMessages())
		if record.Error != "" {
			state.status = fmt.Sprintf(state.messages.KeyInvalidFmt, filepath.Base(path), record.Error)
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
	} else if err == nil && state.status != state.messages.KeyImportCanceled {
		state.status = state.messages.KeyNothingImported
	}
}

func (state *State) removeSelectedIdentity() {
	for index, identity := range state.identities {
		if identity.ID != state.selectedIdentityID {
			continue
		}
		for _, host := range state.hosts {
			if host.Auth == authPrivateKey && host.Identity == identity.Fingerprint {
				state.status = fmt.Sprintf(state.messages.KeyInUseFmt, host.Label)
				return
			}
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

func (state *State) identityName(id uint32) string {
	identity, _ := state.identityByID(id)
	return identity.Name
}
