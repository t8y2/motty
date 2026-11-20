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
	"motty/internal/vault"
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
// the key, the text of the key file, and the user certificate that goes with it,
// when it has one. The certificate is the authorized_keys line of the -cert.pub
// file OpenSSH pairs with a key; keeping it beside the key is what lets the key
// still sign in with its certificate after the files it came from are gone.
type storedKey struct {
	Name        string `json:"name"`
	Key         string `json:"key"`
	Certificate string `json:"certificate,omitempty"`
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

func marshalStoredKey(key storedKey) string {
	encoded, _ := json.Marshal(key) // strings cannot fail to encode
	return string(encoded)
}

func unmarshalStoredKey(value string) (storedKey, error) {
	var key storedKey
	if err := json.Unmarshal([]byte(value), &key); err != nil || key.Key == "" {
		return storedKey{}, errors.New("the saved key is damaged")
	}
	return key, nil
}

// readStoredKey reads the private key a reference names from the vault.
func readStoredKey(source passwordReader, ref string) ([]byte, error) {
	key, err := readStoredKeyRecord(source, ref)
	if err != nil {
		return nil, err
	}
	return []byte(key.Key), nil
}

// readStoredKeyRecord reads what the vault holds under a stored key reference.
func readStoredKeyRecord(source passwordReader, ref string) (storedKey, error) {
	value, found, err := source.Get(ref)
	switch {
	case err != nil:
		return storedKey{}, err
	case !found:
		return storedKey{}, errors.New("it is no longer saved in Motty")
	}
	return unmarshalStoredKey(value)
}

// storedKeyReader is how a connection reads the private key a reference names.
func storedKeyReader(source passwordReader) func(string) ([]byte, error) {
	return func(ref string) ([]byte, error) {
		return readStoredKey(source, ref)
	}
}

// storedCertificateReader is how a connection reads the certificate a stored key
// carries, if any.
func storedCertificateReader(source passwordReader) func(string) ([]byte, error) {
	return func(ref string) ([]byte, error) {
		key, err := readStoredKeyRecord(source, ref)
		if err != nil {
			return nil, err
		}
		if key.Certificate == "" {
			return nil, nil
		}
		return []byte(key.Certificate), nil
	}
}

// storeCertificate replaces the certificate kept with the stored key ref. The
// caller has already checked that it belongs to the key.
func (state *State) storeCertificate(ref, certificate string) error {
	vaultFile := state.secrets.vault
	if vaultFile == nil {
		return vault.ErrUnavailable
	}
	key, err := readStoredKeyRecord(vaultFile, ref)
	if err != nil {
		return err
	}
	key.Certificate = certificate
	return vaultFile.Set(ref, marshalStoredKey(key))
}

// inspectStoredIdentity describes one stored key like inspectIdentity does a file.
func inspectStoredIdentity(id uint32, ref string, key storedKey, messages keyMessages) identityRecord {
	record := identityRecord{ID: id, Path: ref, Name: key.Name, Stored: true, Certificate: key.Certificate != ""}
	info, err := keyidentity.InspectData(ref, []byte(key.Key))
	if err != nil {
		record.Error = err.Error()
	} else {
		record.Algorithm, record.Fingerprint, record.PublicKey, record.Encrypted = info.Algorithm, info.Fingerprint, info.PublicKey, info.Encrypted
		record.SecurityKey = info.SecurityKey
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
	record, err := state.sealStoredKey(storedKey{Name: name, Key: string(data)}, nil)
	if err != nil {
		return fmt.Sprintf(m.VaultSaveFailedFmt, state.vaultErrorText(err))
	}
	state.identities = append(state.identities, record)
	state.selectedIdentityID = record.ID
	state.refreshIdentityCount()
	onAdded(record)
	return ""
}

// sealStoredKey keeps a private key and its certificate, when it has one, in the
// vault under a new reference and returns its record, numbered but not yet
// listed. A key that was listed as a file passes it as replaces: the record
// takes its ID, so what was selected stays selected, and the passphrase saved
// for the file's path moves to the new reference in the same write, so the key
// does not ask for it again.
func (state *State) sealStoredKey(key storedKey, replaces *identityRecord) (identityRecord, error) {
	vaultFile := state.secrets.vault
	if vaultFile == nil {
		return identityRecord{}, vault.ErrUnavailable
	}
	ref, err := newStoredKeyRef()
	if err != nil {
		return identityRecord{}, err
	}
	changes := []vault.Change{{Name: ref, Value: marshalStoredKey(key)}}
	id := state.nextIdentityID
	if replaces != nil {
		id = replaces.ID
		if passphrase, found, err := vaultFile.Get(passphraseSecretName(replaces.Path)); err != nil {
			return identityRecord{}, err
		} else if found {
			changes = append(changes,
				vault.Change{Name: passphraseSecretName(ref), Value: passphrase},
				vault.Change{Name: passphraseSecretName(replaces.Path), Delete: true})
		}
	}
	if err := vaultFile.Apply(changes); err != nil {
		return identityRecord{}, err
	}
	if replaces == nil {
		state.nextIdentityID++
	}
	return inspectStoredIdentity(id, ref, key, state.keyMessages()), nil
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
