// Package backup seals the application's state into one encrypted file that can
// be carried to another computer or kept on a server. The file is encrypted with
// a password the user chooses (Argon2id into AES-256-GCM), independent of the
// saved-passwords vault whose key belongs to one machine, so whoever holds the
// file and not the password learns nothing but its size.
package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/argon2"

	"motty/internal/configstore"
	"motty/internal/vault"
)

// Document is what a backup holds. Records and secrets carry stamps for
// cross-device merging; file backups may also hold device-local preferences.
type Document struct {
	// Created is when the backup was made.
	Created time.Time `json:"created"`
	// Device is the computer that made it.
	Device string `json:"device,omitempty"`
	// Format is the minimum reader/writer compatibility epoch of the records.
	Format int `json:"format,omitempty"`
	// Writer is informational; a newer application version alone is compatible.
	Writer string `json:"writer,omitempty"`
	// Records are the hosts, settings and the rest of the shared configuration,
	// deleted ones included as tombstones.
	Records []configstore.Record `json:"records"`
	// KnownHosts is the text of the trusted-keys file.
	KnownHosts string `json:"known_hosts,omitempty"`
	// Secrets are the saved passwords and keys' passphrases, deleted ones
	// included as tombstones.
	Secrets             []vault.Secret       `json:"secrets,omitempty"`
	ShortcutPreferences *ShortcutPreferences `json:"shortcut_preferences,omitempty"`
}

type ShortcutPreferences struct {
	DefaultsGeneration int        `json:"defaults_generation"`
	Shortcuts          []Shortcut `json:"shortcuts"`
}

type Shortcut struct {
	Action    string `json:"action"`
	Key       string `json:"key"`
	Modifiers uint8  `json:"modifiers"`
}

const (
	format  = "motty-backup"
	version = 2
	// MaxSize bounds a backup file, sealed or opened.
	MaxSize = 64 << 20
	// MinPasswordLength is the shortest password a backup is made with.
	MinPasswordLength = 8

	saltSize = 16
	keySize  = 32
)

// Errors callers act on; test with errors.Is.
var (
	// ErrNotBackup means the data is not a backup of this program.
	ErrNotBackup = errors.New("backup: not a Motty backup")
	// ErrTooNew means the backup was made by a newer version.
	ErrTooNew = errors.New("backup: made by a newer version of Motty")
	// ErrWrongPassword means the password does not open the backup, or the backup
	// was altered; the two cannot be told apart.
	ErrWrongPassword = errors.New("backup: wrong password, or the file was changed")
	// ErrWeakPassword means the password is too short.
	ErrWeakPassword = errors.New("backup: the password is too short")
	// ErrTooLarge means the backup is bigger than MaxSize.
	ErrTooLarge = errors.New("backup: too large")
)

// envelope is the file: everything but the sealed bytes is readable, and the
// readable part is bound to them as additional data, so changing the costs or
// the salt fails to open instead of opening something else.
type envelope struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	KDF     kdf    `json:"kdf"`
	Nonce   []byte `json:"nonce"`
	Data    []byte `json:"data"`
}

type kdf struct {
	Name      string `json:"name"`
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memory_kib"`
	Threads   uint8  `json:"threads"`
	Salt      []byte `json:"salt"`
}

// UseCheapKDFForTests makes every new backup cost a few milliseconds to derive
// instead of the production tens of them, for test binaries. Files that already
// exist carry their own cost, and nothing in the program calls it.
func UseCheapKDFForTests() {
	defaultKDF = kdf{Name: "argon2id", Time: 2, MemoryKiB: 64, Threads: 1}
}

var defaultKDF = kdf{Name: "argon2id", Time: 3, MemoryKiB: 64 * 1024, Threads: 4}

func (params kdf) key(password string) []byte {
	return argon2.IDKey([]byte(password), params.Salt, params.Time, params.MemoryKiB, params.Threads, keySize)
}

// additional is what the sealed bytes are bound to.
func (file envelope) additional() []byte {
	text := fmt.Sprintf("%s/%d/%s/%d/%d/%d/", file.Format, file.Version, file.KDF.Name, file.KDF.Time, file.KDF.MemoryKiB, file.KDF.Threads)
	return append([]byte(text), file.KDF.Salt...)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal encrypts the document under the password.
func Seal(document Document, password string) ([]byte, error) {
	if len(password) < MinPasswordLength {
		return nil, ErrWeakPassword
	}
	plain, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	if len(plain) > MaxSize {
		return nil, ErrTooLarge
	}
	file := envelope{Format: format, Version: version, KDF: defaultKDF}
	file.KDF.Salt = make([]byte, saltSize)
	if _, err := rand.Read(file.KDF.Salt); err != nil {
		return nil, err
	}
	gcm, err := newGCM(file.KDF.key(password))
	if err != nil {
		return nil, err
	}
	file.Nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(file.Nonce); err != nil {
		return nil, err
	}
	file.Data = gcm.Seal(nil, file.Nonce, plain, file.additional())
	return json.Marshal(file)
}

// Open decrypts a backup. A file that is not a backup, or is one of another
// version, is told apart from a wrong password.
func Open(data []byte, password string) (Document, error) {
	if len(data) > MaxSize+1<<16 {
		return Document{}, ErrTooLarge
	}
	var file envelope
	if err := json.Unmarshal(data, &file); err != nil || file.Format != format {
		return Document{}, ErrNotBackup
	}
	if file.Version > version {
		return Document{}, ErrTooNew
	}
	params := file.KDF
	// Costs read from a file are bounded, so a crafted file cannot ask for hours
	// or gigabytes of work.
	if params.Name != "argon2id" || params.Time == 0 || params.Time > 16 || params.MemoryKiB < 8 ||
		params.MemoryKiB > 1<<20 || params.Threads == 0 || params.Threads > 16 || len(params.Salt) < 8 {
		return Document{}, ErrNotBackup
	}
	gcm, err := newGCM(params.key(password))
	if err != nil || len(file.Nonce) != gcm.NonceSize() {
		return Document{}, ErrNotBackup
	}
	plain, err := gcm.Open(nil, file.Nonce, file.Data, file.additional())
	if err != nil {
		return Document{}, ErrWrongPassword
	}
	var document Document
	if err := json.Unmarshal(plain, &document); err != nil {
		return Document{}, ErrNotBackup
	}
	return document, nil
}
