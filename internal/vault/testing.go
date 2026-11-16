package vault

import (
	"crypto/rand"
	"errors"
)

// Nothing in the program calls the functions of this file; they let the tests of
// other packages put a vault in the states earlier versions could leave one in.

// DropKeyForTests forgets the data key held in memory, as a restart does: the
// next use reads it from the key store again.
func (vault *Vault) DropKeyForTests() {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	clear(vault.key)
	vault.key = nil
}

// LegacyForTests rewrites the vault the way a version with a master password
// kept it: the data key wrapped by a key derived from password, and no copy of
// it in the key store. It is ConvertLegacy that turns it back.
func (vault *Vault) LegacyForTests(password string) error {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	if err := vault.prepare(false); err != nil {
		return err
	}
	if vault.key == nil {
		vault.key = make([]byte, keySize)
		if _, err := rand.Read(vault.key); err != nil {
			return err
		}
	}
	params := kdfParams{Name: "argon2id", Time: 1, MemoryKiB: 64, Threads: 1, Salt: make([]byte, 16)}
	if _, err := rand.Read(params.Salt); err != nil {
		return err
	}
	kek := deriveKey(password, &params)
	defer clear(kek)
	gcm, err := newGCM(kek)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	file := fileData{Version: fileVersion, KDF: &params, WrappedKey: gcm.Seal(nonce, nonce, vault.key, []byte(wrapAssociated)), Records: vault.file.Records}
	if file.Records == nil {
		file.Records = []record{}
	}
	if err := writeFile(vault.path, file); err != nil {
		return err
	}
	vault.file = file
	if vault.keys != nil {
		err = errors.Join(err, vault.keys.Delete(KeyService, KeyAccount))
	}
	clear(vault.key)
	vault.key = nil
	return err
}
