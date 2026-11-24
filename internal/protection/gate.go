// Package protection coordinates one profile's master password, device unlock
// enrollment, encrypted SQLite records, and saved-secret vault.
package protection

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/argon2"
	"motty/internal/configstore"
	"motty/internal/vault"
)

const MinPasswordLength = 8

var (
	ErrWrongPassword    = errors.New("protection: incorrect master password")
	ErrWeakPassword     = errors.New("protection: master password must have at least eight characters")
	ErrLocked           = errors.New("protection: profile is locked")
	ErrCorrupt          = errors.New("protection: protection metadata is missing or damaged")
	ErrMigrationPending = errors.New("protection: migration needs recovery; unlock with the master password")
)

type Options struct {
	Directory    string
	ConfigPath   string
	VaultPath    string
	ConfigFormat int
	Keystore     vault.Keystore
	DeviceStore  vault.Keystore
}

type kdf struct {
	Name      string `json:"name"`
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memory_kib"`
	Threads   uint8  `json:"threads"`
	Salt      []byte `json:"salt"`
}
type envelope struct {
	Version int    `json:"version"`
	Profile string `json:"profile"`
	KDF     kdf    `json:"kdf"`
	Wrapped []byte `json:"wrapped_key"`
	Check   []byte `json:"key_check"`
}
type device struct {
	Version int    `json:"version"`
	Profile string `json:"profile"`
	Blob    []byte `json:"blob"`
}

var kdfCost = kdf{Name: "argon2id", Time: 3, MemoryKiB: 64 * 1024, Threads: 4}

type Gate struct {
	mu           sync.Mutex
	options      Options
	envelope     *envelope
	key          []byte
	device       []byte
	pending      *migration
	orphanStages []string
}

// Open is read-only. A protected database without its envelope never falls back
// to ordinary credentials or to a fresh empty profile.
func Open(options Options) (*Gate, error) {
	if options.Directory == "" {
		options.Directory = filepath.Dir(options.ConfigPath)
	}
	if options.Directory == "" || options.Directory == "." && options.ConfigPath == "" {
		return nil, errors.New("protection: profile directory is required")
	}
	var err error
	options.Directory, err = filepath.Abs(options.Directory)
	if err != nil {
		return nil, err
	}
	if options.ConfigPath == "" {
		options.ConfigPath = filepath.Join(options.Directory, "config.db")
	}
	if options.VaultPath == "" {
		options.VaultPath = filepath.Join(options.Directory, "vault.json")
	}
	options.ConfigPath, err = filepath.Abs(options.ConfigPath)
	if err != nil {
		return nil, err
	}
	options.VaultPath, err = filepath.Abs(options.VaultPath)
	if err != nil {
		return nil, err
	}
	if filepath.Dir(options.ConfigPath) != options.Directory || filepath.Dir(options.VaultPath) != options.Directory {
		return nil, errors.New("protection: profile files must share one directory")
	}
	if options.Keystore == nil {
		options.Keystore = vault.FileKeystore{Path: filepath.Join(options.Directory, "vault.key")}
	}
	g := &Gate{options: options}
	if data, readErr := os.ReadFile(g.path("protection-migration.json")); readErr == nil {
		var pending migration
		if len(data) > 1<<20 || json.Unmarshal(data, &pending) != nil || !validEnvelope(&pending.Envelope) {
			return nil, ErrCorrupt
		}
		g.pending = &pending
		g.envelope = &pending.Envelope
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}
	if g.pending == nil {
		data, readErr := os.ReadFile(g.path("protection.json"))
		if readErr == nil {
			var current envelope
			if len(data) > 64<<10 || json.Unmarshal(data, &current) != nil || !validEnvelope(&current) {
				return nil, ErrCorrupt
			}
			g.envelope = &current
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return nil, readErr
		}
	}
	if g.envelope == nil && protectedMarker(options.ConfigPath, options.VaultPath) {
		return nil, ErrCorrupt
	}
	if g.envelope != nil && g.pending == nil {
		if err := g.validateProtectedFiles(); err != nil {
			return nil, err
		}
	}
	if err := g.loadDeviceStore(); err != nil {
		return nil, err
	}
	if err := g.discoverOrphanStages(); err != nil {
		return nil, err
	}
	return g, nil
}

func protectedMarker(configPath, vaultPath string) bool {
	if file, err := os.Open(configPath); err == nil {
		header := make([]byte, 100)
		n, _ := file.Read(header)
		file.Close()
		if n == 100 && string(header[:16]) == "SQLite format 3\x00" && binary.BigEndian.Uint32(header[60:64]) == 2 {
			return true
		}
	}
	if file, err := os.Open(vaultPath); err == nil {
		defer file.Close()
		decoder := json.NewDecoder(io.LimitReader(file, 4096))
		// Vault's canonical file header starts with version. Reading that one
		// field avoids loading a multi-megabyte credential file on every start.
		if first, err := decoder.Token(); err == nil && first == json.Delim('{') {
			if field, err := decoder.Token(); err == nil && field == "version" {
				var version int
				if decoder.Decode(&version) == nil && version == 2 {
					return true
				}
			}
		}
	}
	return false
}

func (g *Gate) path(name string) string { return filepath.Join(g.options.Directory, name) }
func (g *Gate) Enabled() bool           { g.mu.Lock(); defer g.mu.Unlock(); return g.envelope != nil }
func (g *Gate) Locked() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.envelope != nil && len(g.key) == 0
}
func (g *Gate) ProfileID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.envelope == nil {
		return ""
	}
	return g.envelope.Profile
}
func (g *Gate) PendingMigration() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.pending != nil || len(g.orphanStages) > 0
}
func (g *Gate) DeviceBlob() []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]byte(nil), g.device...)
}

func (g *Gate) SetDeviceBlob(blob []byte) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.envelope == nil || len(g.key) != 32 {
		return ErrLocked
	}
	if len(blob) > 1<<20 {
		return errors.New("protection: device enrollment is too large")
	}
	return g.persistDevice(blob)
}

func (g *Gate) Lock() {
	g.mu.Lock()
	defer g.mu.Unlock()
	configstore.LockProtection(g.options.ConfigPath)
	vault.LockProtection(g.options.VaultPath)
	clear(g.key)
	g.key = nil
}

func (g *Gate) Unlock(password string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.envelope == nil {
		return nil
	}
	key, err := unwrap(*g.envelope, password)
	if err != nil {
		return err
	}
	defer clear(key)
	return g.unlock(key)
}

func (g *Gate) UnlockWithKey(key []byte) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.envelope == nil {
		return ErrLocked
	}
	return g.unlock(key)
}

func (g *Gate) unlock(key []byte) error {
	if !checkKey(g.envelope, key) {
		return ErrWrongPassword
	}
	if err := g.removeOrphanStages(); err != nil {
		return err
	}
	if g.pending != nil {
		if err := g.finishMigration(key); err != nil {
			return err
		}
		if g.envelope == nil {
			return nil
		}
	}
	if err := g.validateProtectedFiles(); err != nil {
		return err
	}
	if err := configstore.SetProtectionKey(g.options.ConfigPath, key); err != nil {
		return err
	}
	vault.SetProtectionKey(g.options.VaultPath, key)
	clear(g.key)
	g.key = append([]byte(nil), key...)
	return nil
}

func (g *Gate) validateProtectedFiles() error {
	file, err := os.Open(g.options.ConfigPath)
	if err != nil {
		return errors.Join(ErrCorrupt, err)
	}
	header := make([]byte, 100)
	_, err = io.ReadFull(file, header)
	file.Close()
	if err != nil || string(header[:16]) != "SQLite format 3\x00" || binary.BigEndian.Uint32(header[60:64]) != 2 {
		return ErrCorrupt
	}
	file, err = os.Open(g.options.VaultPath)
	if err != nil {
		return errors.Join(ErrCorrupt, err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 8<<20))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return ErrCorrupt
	}
	var version int
	var profile string
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return ErrCorrupt
		}
		switch token {
		case "version":
			err = decoder.Decode(&version)
		case "protection":
			err = decoder.Decode(&profile)
		default:
			var discard json.RawMessage
			err = decoder.Decode(&discard)
		}
		if err != nil {
			return ErrCorrupt
		}
		if version == 2 && profile != "" {
			if profile != g.envelope.Profile {
				return ErrCorrupt
			}
			return nil
		}
	}
	return ErrCorrupt
}

// EnrollmentKey requires the master password even when the profile is already
// open. The caller must clear its returned copy after native enrollment.
func (g *Gate) EnrollmentKey(currentPassword string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.envelope == nil || len(g.key) != 32 {
		return nil, ErrLocked
	}
	return unwrap(*g.envelope, currentPassword)
}

func (g *Gate) ChangePassword(current, next string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.envelope == nil {
		return ErrLocked
	}
	key, err := unwrap(*g.envelope, current)
	if err != nil {
		return err
	}
	defer clear(key)
	updated, err := makeEnvelope(g.envelope.Profile, next, key)
	if err != nil {
		return err
	}
	data, err := json.Marshal(updated)
	if err != nil {
		return err
	}
	if err := writeAtomic(g.path("protection.json"), data); err != nil {
		return err
	}
	g.envelope = &updated
	return nil
}

func validEnvelope(e *envelope) bool {
	return e != nil && e.Version == 1 && len(e.Profile) == 32 && e.KDF.Name == "argon2id" && e.KDF.Time >= 1 && e.KDF.Time <= 16 && e.KDF.MemoryKiB >= 8 && e.KDF.MemoryKiB <= 256*1024 && e.KDF.Threads >= 1 && e.KDF.Threads <= 16 && len(e.KDF.Salt) == 16 && len(e.Wrapped) == 60 && len(e.Check) >= 28
}
func derive(password string, params kdf) []byte {
	return argon2.IDKey([]byte(password), params.Salt, params.Time, params.MemoryKiB, params.Threads, 32)
}
func seal(key []byte, purpose string, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, data, []byte("motty protection/v1/"+purpose)), nil
}
func open(key []byte, purpose string, data []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrWrongPassword
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrCorrupt
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte("motty protection/v1/"+purpose))
	if err != nil {
		return nil, ErrWrongPassword
	}
	return plain, nil
}
func makeEnvelope(profile, password string, key []byte) (envelope, error) {
	if len([]rune(password)) < MinPasswordLength {
		return envelope{}, ErrWeakPassword
	}
	params := kdfCost
	params.Salt = make([]byte, 16)
	if _, err := rand.Read(params.Salt); err != nil {
		return envelope{}, err
	}
	kek := derive(password, params)
	defer clear(kek)
	wrapped, err := seal(kek, "root/"+profile, key)
	if err != nil {
		return envelope{}, err
	}
	check, err := seal(key, "check/"+profile, []byte("motty protected profile"))
	if err != nil {
		return envelope{}, err
	}
	return envelope{Version: 1, Profile: profile, KDF: params, Wrapped: wrapped, Check: check}, nil
}
func unwrap(e envelope, password string) ([]byte, error) {
	if !validEnvelope(&e) {
		return nil, ErrCorrupt
	}
	kek := derive(password, e.KDF)
	defer clear(kek)
	key, err := open(kek, "root/"+e.Profile, e.Wrapped)
	if err != nil {
		return nil, ErrWrongPassword
	}
	if !checkKey(&e, key) {
		clear(key)
		return nil, ErrWrongPassword
	}
	return key, nil
}
func checkKey(e *envelope, key []byte) bool {
	if e == nil {
		return false
	}
	plain, err := open(key, "check/"+e.Profile, e.Check)
	defer clear(plain)
	return err == nil && string(plain) == "motty protected profile"
}
func identifier() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
func authenticate(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("motty profile migration/v1\x00"))
	mac.Write(data)
	return mac.Sum(nil)
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".motty-protection-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := os.Chmod(name, 0600); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	syncDirectory(filepath.Dir(path))
	return nil
}
func syncDirectory(path string) {
	if file, err := os.Open(path); err == nil {
		_ = file.Sync()
		_ = file.Close()
	}
}

func (g *Gate) Enable(password string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.envelope != nil {
		return errors.New("protection: profile protection is already enabled")
	}
	return g.migrate(password, true)
}
func (g *Gate) Disable(password string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.envelope == nil {
		return nil
	}
	return g.migrate(password, false)
}
