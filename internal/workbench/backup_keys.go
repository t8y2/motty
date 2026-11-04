package workbench

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"motty/internal/backup"
	"motty/internal/keyidentity"
	"motty/internal/vault"
)

// The private key files themselves travel in a backup only when the user asks
// for them (state.backupKeys): unlike a password, a key file is a credential of
// its own, and a backup leaves this computer. They are written back beside the
// state file, never to the path they came from, so a restore never overwrites
// another computer's keys.

// unionBackupKeys is what to upload: this computer's keys, and any the server
// holds that it does not. A key both have is this computer's copy.
func unionBackupKeys(local, remote []backup.Key) []backup.Key {
	out := make([]backup.Key, 0, len(local)+len(remote))
	seen := make(map[string]bool, len(local))
	for _, key := range local {
		seen[key.Fingerprint] = true
		out = append(out, key)
	}
	for _, key := range remote {
		if !seen[key.Fingerprint] {
			out = append(out, key)
		}
	}
	return out
}

// sameBackupKeys reports whether two sets carry the same keys, so that a run
// that changed nothing else does not upload.
func sameBackupKeys(a, b []backup.Key) bool {
	if len(a) != len(b) {
		return false
	}
	held := make(map[string][]byte, len(a))
	for _, key := range a {
		held[key.Fingerprint] = key.Data
	}
	for _, key := range b {
		data, ok := held[key.Fingerprint]
		if !ok || !bytes.Equal(data, key.Data) {
			return false
		}
	}
	return true
}

// keyDirectory is where a restored key is written: the folder imported keys have
// always gone to.
func (state *State) keyDirectory() string {
	return filepath.Join(filepath.Dir(state.options.StatePath), "keys")
}

// backupKeyFiles reads the key files a backup carries. A key that cannot be
// read, or is no longer the key it is listed as, is left out and counted: the
// backup then says how many it left behind instead of carrying a file that
// could not log in anyway.
func (state *State) backupKeyFiles() (keys []backup.Key, skipped int) {
	seen := make(map[string]bool, len(state.identities))
	for _, identity := range state.identities {
		if identity.Error != "" || identity.Fingerprint == "" || seen[identity.Fingerprint] {
			continue
		}
		if len(keys) >= backup.MaxKeys {
			skipped++
			continue
		}
		data, err := os.ReadFile(identity.Path)
		if err != nil || len(data) == 0 || len(data) > backup.MaxKeySize {
			skipped++
			continue
		}
		seen[identity.Fingerprint] = true
		keys = append(keys, backup.Key{
			Fingerprint: identity.Fingerprint,
			Name:        filepath.Base(identity.Path),
			Path:        identity.Path,
			Data:        data,
		})
	}
	return keys, skipped
}

// applyBackupKeys writes the keys a backup carries into the key folder and
// registers them, so the hosts that name a key by fingerprint find it again. A
// key already there under the same fingerprint is kept as it is; one whose file
// is not the key it claims to be is not registered. The passphrase saved for a
// key follows it to its new path, which is where the keys page looks for it.
//
// It returns how many files were written and how many keys could not be placed.
func (state *State) applyBackupKeys(keys []backup.Key, opened *vault.Vault) (written, failed int) {
	if len(keys) == 0 || state.options.StatePath == "" {
		return 0, 0
	}
	directory := state.keyDirectory()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return 0, len(keys)
	}
	paths := make([]string, 0, len(state.identities)+len(keys))
	known := make(map[string]bool, len(state.identities)+len(keys))
	for _, identity := range state.identities {
		paths = append(paths, identity.Path)
		known[identity.Path] = true
	}
	added := false
	for _, key := range keys {
		target := filepath.Join(directory, key.Name)
		if !keyFileMatches(target, key.Fingerprint) {
			// A name another key already took keeps its file: the restored one
			// gets a name of its own rather than replacing a working key.
			if _, err := os.Stat(target); err == nil {
				target = filepath.Join(directory, keyFileName(key.Fingerprint, key.Name))
			}
			if err := writePrivateAtomic(target, key.Data); err != nil {
				failed++
				continue
			}
			written++
		}
		if !keyFileMatches(target, key.Fingerprint) {
			failed++
			continue
		}
		if !known[target] {
			known[target] = true
			paths = append(paths, target)
			added = true
		}
		state.moveKeyPassphrase(opened, key.Path, target)
	}
	if added {
		state.restoreIdentities(paths)
		state.save()
	}
	return written, failed
}

// keyFileMatches reports whether path holds the key with this fingerprint.
func keyFileMatches(path, fingerprint string) bool {
	info, err := keyidentity.Inspect(path)
	return err == nil && info.Fingerprint == fingerprint
}

// keyFileName is a file name for a key whose own name is taken: the fingerprint
// says which key it is, and only its readable characters are used.
func keyFileName(fingerprint, name string) string {
	cleaned := strings.TrimPrefix(fingerprint, "SHA256:")
	var prefix strings.Builder
	for _, char := range cleaned {
		if prefix.Len() >= 12 {
			break
		}
		if char >= '0' && char <= '9' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' {
			prefix.WriteRune(char)
		}
	}
	if prefix.Len() == 0 {
		return name
	}
	return prefix.String() + "-" + name
}

// moveKeyPassphrase keeps the passphrase saved for a key under its old path
// working under the new one. The old record is left as it was: another computer
// may still have the key where it used to be.
func (state *State) moveKeyPassphrase(opened *vault.Vault, from, to string) {
	if opened == nil || from == "" || from == to {
		return
	}
	value, found, err := opened.Get(passphraseSecretName(from))
	if err != nil || !found {
		return
	}
	if _, taken, err := opened.Get(passphraseSecretName(to)); err != nil || taken {
		return
	}
	_ = opened.Set(passphraseSecretName(to), value)
}
