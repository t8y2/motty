package vault

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"weak"
)

var protectedProfiles struct {
	sync.Mutex
	keys   map[string][]byte
	opened map[string]map[weak.Pointer[Vault]]struct{}
}

func protectionPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return filepath.Clean(absolute)
}

func rememberVault(v *Vault) {
	protectedProfiles.Lock()
	defer protectedProfiles.Unlock()
	name := protectionPath(v.path)
	if protectedProfiles.opened == nil {
		protectedProfiles.opened = map[string]map[weak.Pointer[Vault]]struct{}{}
	}
	if protectedProfiles.opened[name] == nil {
		protectedProfiles.opened[name] = map[weak.Pointer[Vault]]struct{}{}
	}
	for pointer := range protectedProfiles.opened[name] {
		if pointer.Value() == nil {
			delete(protectedProfiles.opened[name], pointer)
		}
	}
	protectedProfiles.opened[name][weak.Make(v)] = struct{}{}
}

func protectedKey(path string) []byte {
	protectedProfiles.Lock()
	defer protectedProfiles.Unlock()
	return append([]byte(nil), protectedProfiles.keys[protectionPath(path)]...)
}

// SetProtectionKey installs a key already authenticated by the profile gate.
// Deriving a purpose-specific key keeps vault records separate from the config.
func SetProtectionKey(path string, root []byte, profile string) error {
	if len(root) != 32 {
		return ErrCorrupt
	}
	mac := hmac.New(sha256.New, root)
	mac.Write([]byte("motty protected vault key/v2"))
	key := mac.Sum(nil)
	// Authenticate the contents before publishing a key to any existing window.
	// A readable JSON header alone cannot detect swapped or truncated records.
	v, err := Open(path, nil)
	if err != nil {
		clear(key)
		return err
	}
	if v.file.Version != 2 || v.file.Protection != profile {
		clear(key)
		return ErrCorrupt
	}
	for _, entry := range v.file.Records {
		if _, err := open(key, entry); err != nil {
			clear(key)
			return err
		}
	}
	protectedProfiles.Lock()
	defer protectedProfiles.Unlock()
	if protectedProfiles.keys == nil {
		protectedProfiles.keys = map[string][]byte{}
	}
	name := protectionPath(path)
	clear(protectedProfiles.keys[name])
	protectedProfiles.keys[name] = key
	return nil
}

// LockProtection erases cached keys in every vault opened for this profile.
func LockProtection(path string) {
	name := protectionPath(path)
	protectedProfiles.Lock()
	clear(protectedProfiles.keys[name])
	delete(protectedProfiles.keys, name)
	var opened []*Vault
	for pointer := range protectedProfiles.opened[name] {
		if v := pointer.Value(); v != nil {
			opened = append(opened, v)
		} else {
			delete(protectedProfiles.opened[name], pointer)
		}
	}
	protectedProfiles.Unlock()
	for _, v := range opened {
		v.mu.Lock()
		clear(v.key)
		v.key = nil
		v.mu.Unlock()
	}
}

// RetireProtection makes handles of the previous on-disk generation unusable.
// They must not reload an old shared OS key and expose an in-memory snapshot.
func RetireProtection(path string) {
	name := protectionPath(path)
	protectedProfiles.Lock()
	var opened []*Vault
	for pointer := range protectedProfiles.opened[name] {
		if v := pointer.Value(); v != nil {
			opened = append(opened, v)
		} else {
			delete(protectedProfiles.opened[name], pointer)
		}
	}
	protectedProfiles.Unlock()
	for _, v := range opened {
		v.mu.Lock()
		clear(v.key)
		v.key = nil
		v.superseded = true
		v.mu.Unlock()
	}
}

// RecoveryKey returns an ordinary key only when a historical vault actually
// authenticates with it. A corrupt or unrelated backup remains recoverable as
// raw bytes without exporting a shared system key into its archive.
func RecoveryKey(data []byte, keys Keystore) ([]byte, error) {
	var file fileData
	if json.Unmarshal(data, &file) != nil || file.Version != 1 || file.KDF != nil || len(file.Records) == 0 {
		return nil, nil
	}
	key, err := loadOrdinaryKey(file, keys)
	if errors.Is(err, ErrCorrupt) || errors.Is(err, ErrKeystoreNotFound) || errors.Is(err, ErrKeystoreUnavailable) {
		return nil, nil
	}
	return key, err
}

// BuildProtectedCopy rotates the data key while preserving names, stamps and
// tombstones. Existing ciphertext cannot be opened using a copied old vault.key.
func BuildProtectedCopy(source, destination string, keys Keystore, root []byte, profileID string) error {
	if len(root) != 32 || profileID == "" {
		return ErrCorrupt
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return errors.New("vault: migration destination exists")
		}
		return err
	}
	opened, err := Open(source, keys)
	if err != nil {
		return err
	}
	opened.mu.Lock()
	defer opened.mu.Unlock()
	if err := opened.prepare(false); err != nil {
		return err
	}
	plains, err := opened.openAll()
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, root)
	mac.Write([]byte("motty protected vault key/v2"))
	key := mac.Sum(nil)
	defer clear(key)
	file := fileData{Version: 2, Protection: profileID, Records: make([]record, 0, len(plains))}
	for _, plain := range plains {
		sealed, err := seal(key, plain)
		if err != nil {
			return err
		}
		file.Records = append(file.Records, sealed)
	}
	return writeVerifiedCopy(destination, file, key, plains)
}

// Read back the staged file with its destination key before publishing a
// migration journal. A successful write must preserve every secret and stamp.
func writeVerifiedCopy(path string, file fileData, key []byte, expected []plainRecord) error {
	if err := writeFile(path, file); err != nil {
		return err
	}
	saved, err := Open(path, nil)
	if err != nil {
		return err
	}
	if saved.file.Version != file.Version || saved.file.Protection != file.Protection || len(saved.file.Records) != len(expected) {
		return ErrCorrupt
	}
	for i, record := range saved.file.Records {
		plain, err := open(key, record)
		if err != nil {
			return err
		}
		if plain != expected[i] {
			return ErrCorrupt
		}
	}
	return nil
}

// BuildUnprotectedCopy creates an ordinary vault under the selected ordinary key.
// The caller stores returnedKey only as part of the coordinated disable commit.
func BuildUnprotectedCopy(source, destination string, keys Keystore, profile string, targetKeys ...[]byte) (returnedKey []byte, err error) {
	opened, err := Open(source, keys)
	if err != nil {
		return nil, err
	}
	opened.mu.Lock()
	defer opened.mu.Unlock()
	if opened.file.Version != 2 || opened.file.Protection != profile {
		return nil, ErrCorrupt
	}
	if err := opened.prepare(false); err != nil {
		return nil, err
	}
	plains, err := opened.openAll()
	if err != nil {
		return nil, err
	}
	key := make([]byte, keySize)
	if len(targetKeys) > 0 {
		if len(targetKeys[0]) != keySize {
			return nil, ErrCorrupt
		}
		copy(key, targetKeys[0])
	} else {
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
	}
	file := fileData{Version: 1, Records: make([]record, 0, len(plains))}
	for _, plain := range plains {
		sealed, err := seal(key, plain)
		if err != nil {
			clear(key)
			return nil, err
		}
		file.Records = append(file.Records, sealed)
	}
	if err := writeVerifiedCopy(destination, file, key, plains); err != nil {
		clear(key)
		return nil, err
	}
	return key, nil
}
