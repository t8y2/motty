package vault

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// FileKeystore keeps the data key in a file of its own, next to the vault it
// opens. It is the store for a system without a secret store, and for data that
// travels with its program (a portable copy), where a key left in one computer's
// store would not open the file on another.
//
// The file is readable by its user only, and it is as private as the directory it
// is in: whoever can copy the vault can copy the key. That is the price of having
// no system store, and the reason the system's store comes first wherever there is
// one (see FallbackKeystore).
type FileKeystore struct {
	Path string
}

// Store writes the key. The service and account name the system's entry and mean
// nothing to a file that holds one key.
func (store FileKeystore) Store(_, _ string, secret []byte) error {
	return writeFileAtomic(store.Path, []byte(hex.EncodeToString(secret)+"\n"))
}

// Load reads the key, or reports ErrKeystoreNotFound when there is no file.
func (store FileKeystore) Load(_, _ string) ([]byte, error) {
	data, err := os.ReadFile(store.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrKeystoreNotFound
	}
	if err != nil {
		return nil, err
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: the key file is not a key", ErrCorrupt)
	}
	return key, nil
}

// Delete removes the file; there being none is not an error.
func (store FileKeystore) Delete(_, _ string) error {
	if err := os.Remove(store.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// FallbackKeystore is the system's store with a key file behind it. The file is
// written only where the system has no store at all; a refusal by a store that
// exists is reported, never answered by putting the key somewhere weaker.
type FallbackKeystore struct {
	System Keystore
	File   FileKeystore
}

// Load asks the system's store, and the file when the system has no key or no store.
func (store FallbackKeystore) Load(service, account string) ([]byte, error) {
	key, err := store.System.Load(service, account)
	if err == nil || !(errors.Is(err, ErrKeystoreNotFound) || errors.Is(err, ErrKeystoreUnavailable)) {
		return key, err
	}
	return store.File.Load(service, account)
}

// Store keeps the key in the system's store, or in the file where there is none.
func (store FallbackKeystore) Store(service, account string, secret []byte) error {
	err := store.System.Store(service, account, secret)
	if errors.Is(err, ErrKeystoreUnavailable) {
		return store.File.Store(service, account, secret)
	}
	return err
}

// Delete forgets the key wherever it is.
func (store FallbackKeystore) Delete(service, account string) error {
	return errors.Join(store.System.Delete(service, account), store.File.Delete(service, account))
}
