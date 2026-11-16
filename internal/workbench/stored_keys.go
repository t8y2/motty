package workbench

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"motty/internal/keyidentity"
	"motty/internal/sshconn"
)

// A stored key is a private key that is not a file: one pasted into Motty or made
// by it. The vault keeps it, sealed like a password, under a random reference
// that begins sshconn.StoredKeyPrefix. The reference is what the identity lists
// and the passphrase secrets name it by where a file's path would stand, so a
// stored key connects, asks for its passphrase and is chosen for a host exactly
// as a file does. Because the vault travels with the user's other computers, so
// do the keys in it, and the list of keys is read from the vault, not from the
// saved state.

// storedKey is what the vault holds under a reference: the name the user gave
// the key and the text of the key file.
type storedKey struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// storedKeySource is the part of the vault that lists and reads stored keys.
type storedKeySource interface {
	passwordReader
	Names() ([]string, error)
}

func newStoredKeyRef() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return sshconn.StoredKeyPrefix + hex.EncodeToString(random[:]), nil
}

func marshalStoredKey(name string, data []byte) string {
	encoded, _ := json.Marshal(storedKey{Name: name, Key: string(data)}) // two strings cannot fail to encode
	return string(encoded)
}

func unmarshalStoredKey(value string) (storedKey, error) {
	var key storedKey
	if err := json.Unmarshal([]byte(value), &key); err != nil || key.Key == "" {
		return storedKey{}, errors.New("the saved key is damaged")
	}
	return key, nil
}

// storedKeyReader is how a connection reads the private key a reference names.
func storedKeyReader(source passwordReader) func(string) ([]byte, error) {
	return func(ref string) ([]byte, error) {
		value, found, err := source.Get(ref)
		switch {
		case err != nil:
			return nil, err
		case !found:
			return nil, errors.New("it is no longer saved in Motty")
		}
		key, err := unmarshalStoredKey(value)
		if err != nil {
			return nil, err
		}
		return []byte(key.Key), nil
	}
}

// inspectStoredIdentity describes one stored key like inspectIdentity does a file.
func inspectStoredIdentity(id uint32, ref string, key storedKey, messages keyMessages) identityRecord {
	record := identityRecord{ID: id, Path: ref, Name: key.Name, Stored: true}
	info, err := keyidentity.InspectData(ref, []byte(key.Key))
	if err != nil {
		record.Error = err.Error()
	} else {
		record.Algorithm, record.Fingerprint, record.PublicKey, record.Encrypted = info.Algorithm, info.Fingerprint, info.PublicKey, info.Encrypted
	}
	record.refreshCells(messages)
	return record
}

// storedIdentities lists the stored keys in source, by name, numbering them from
// first. A vault that cannot be opened has none to show.
func storedIdentities(source storedKeySource, first uint32, messages keyMessages) []identityRecord {
	names, err := source.Names()
	if err != nil {
		return nil
	}
	var identities []identityRecord
	for _, ref := range names {
		if !sshconn.IsStoredKey(ref) {
			continue
		}
		value, found, err := source.Get(ref)
		if err != nil || !found {
			continue
		}
		key, err := unmarshalStoredKey(value)
		if err != nil {
			continue
		}
		identities = append(identities, inspectStoredIdentity(0, ref, key, messages))
	}
	slices.SortStableFunc(identities, func(a, b identityRecord) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	for index := range identities {
		identities[index].ID = first + uint32(index)
	}
	return identities
}

// reloadStoredIdentities makes the stored keys in the list those the vault holds:
// at start, and after the vault received keys from another computer. What was
// selected stays selected when it is still there.
func (state *State) reloadStoredIdentities() {
	selected := ""
	if identity, ok := state.identityByID(state.selectedIdentityID); ok {
		selected = identity.Path
	}
	state.identities = slices.DeleteFunc(state.identities, func(identity identityRecord) bool { return identity.Stored })
	for _, identity := range state.identities {
		state.nextIdentityID = max(state.nextIdentityID, identity.ID+1)
	}
	if state.secrets.vault != nil {
		stored := storedIdentities(state.secrets.vault, state.nextIdentityID, state.keyMessages())
		state.identities = append(state.identities, stored...)
		state.nextIdentityID += uint32(len(stored))
	}
	state.selectedIdentityID = 0
	for _, identity := range state.identities {
		if identity.Path == selected {
			state.selectedIdentityID = identity.ID
		}
	}
	if state.selectedIdentityID == 0 && len(state.identities) > 0 {
		state.selectedIdentityID = state.identities[0].ID
	}
	state.refreshIdentityCount()
}

// addStoredKey keeps a private key in the vault and lists it. It returns the text
// of what is wrong, "" when the key was added, and also "" when the vault first
// has to be unlocked: the key is added when it is, and then says so in the
// status line.
func (state *State) addStoredKey(name string, data []byte, onAdded func(identityRecord)) string {
	m := state.messages
	vaultFile := state.secrets.vault
	if vaultFile == nil {
		return m.VaultUnavailable
	}
	info, err := keyidentity.InspectData("", data)
	if err != nil {
		return fmt.Sprintf(m.KeyInvalidTextFmt, err)
	}
	if info.Fingerprint != "" {
		for _, identity := range state.identities {
			if identity.Error == "" && identity.Fingerprint == info.Fingerprint {
				return fmt.Sprintf(m.KeyDuplicateFmt, identity.Name)
			}
		}
	}
	if name = strings.TrimSpace(name); name == "" {
		name = defaultKeyName(info)
	}
	if vaultFile.NeedsConversion() {
		state.openVaultUnlock(nil, func() {
			if message := state.addStoredKey(name, data, onAdded); message != "" {
				state.status = message
			}
		})
		return ""
	}
	ref, err := newStoredKeyRef()
	if err != nil {
		return fmt.Sprintf(m.VaultSaveFailedFmt, err)
	}
	if err := vaultFile.Set(ref, marshalStoredKey(name, data)); err != nil {
		return fmt.Sprintf(m.VaultSaveFailedFmt, state.vaultErrorText(err))
	}
	record := inspectStoredIdentity(state.nextIdentityID, ref, storedKey{Name: name, Key: string(data)}, state.keyMessages())
	state.nextIdentityID++
	state.identities = append(state.identities, record)
	state.selectedIdentityID = record.ID
	state.refreshIdentityCount()
	onAdded(record)
	return ""
}

// defaultKeyName names a key whose adder gave it no name: its type and the start
// of its fingerprint, which tells two keys apart.
func defaultKeyName(info keyidentity.Info) string {
	kind := strings.TrimPrefix(info.Algorithm, "ssh-")
	if kind == "" {
		kind = "key"
	}
	fingerprint := strings.TrimPrefix(info.Fingerprint, "SHA256:")
	if fingerprint == "" {
		return kind
	}
	return kind + "-" + fingerprint[:min(len(fingerprint), 6)]
}

// storedKeyCount is how many keys the vault holds that exist nowhere else.
func (state *State) storedKeyCount() int {
	count := 0
	for _, identity := range state.identities {
		if identity.Stored {
			count++
		}
	}
	return count
}
