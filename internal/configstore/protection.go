package configstore

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	ErrLocked         = errors.New("config store: profile is locked")
	ErrAuthentication = errors.New("config store: protected data authentication failed")
)

const protectionMeta = "protection-v1"

var activeStores struct {
	sync.Mutex
	paths map[string]map[*Store]struct{}
}

func trackStore(path string, s *Store) {
	activeStores.Lock()
	defer activeStores.Unlock()
	path = canonicalPath(path)
	if activeStores.paths == nil {
		activeStores.paths = map[string]map[*Store]struct{}{}
	}
	if activeStores.paths[path] == nil {
		activeStores.paths[path] = map[*Store]struct{}{}
	}
	activeStores.paths[path][s] = struct{}{}
}

func untrackStore(s *Store) {
	activeStores.Lock()
	defer activeStores.Unlock()
	for path, stores := range activeStores.paths {
		if _, exists := stores[s]; exists {
			delete(stores, s)
			if len(stores) == 0 {
				delete(activeStores.paths, path)
			}
			return
		}
	}
}

func (s *Store) readLocal(ctx context.Context, tx queryer, key string) ([]byte, error) {
	id, err := s.codec.identifier("local", key)
	if err != nil {
		return nil, err
	}
	var value []byte
	if err := tx.QueryRowContext(ctx, `SELECT value FROM local WHERE key=?`, id).Scan(&value); err != nil {
		return nil, err
	}
	_, value, err = s.codec.decodeLocal(id, value)
	return value, err
}

func (s *Store) writeLocal(ctx context.Context, tx *sql.Tx, key string, value []byte) error {
	if s.codec != nil {
		if err := writeLockPreferences(ctx, tx, key, value); err != nil {
			return err
		}
	}
	id, err := s.codec.identifier("local", key)
	if err != nil {
		return err
	}
	if len(value) == 0 {
		_, err = tx.ExecContext(ctx, `DELETE FROM local WHERE key=?`, id)
		return err
	}
	id, value, err = s.codec.encodeLocal(key, value)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO local(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, id, value)
	return err
}

// EnsureClosed refuses migration while a shared application handle is open.
func EnsureClosed(path string) error {
	activeStores.Lock()
	defer activeStores.Unlock()
	if len(activeStores.paths[canonicalPath(path)]) > 0 {
		return ErrBusy
	}
	return nil
}

// A codec is shared by every handle of one profile. Locking it invalidates
// existing handles as well as subsequent Open and ReadOnly calls.
type codec struct {
	mu  sync.RWMutex
	key []byte
}

var profileKeys struct {
	sync.Mutex
	keys map[string]*codec
}

func canonicalPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return filepath.Clean(absolute)
}

func codecFor(path string) *codec {
	profileKeys.Lock()
	defer profileKeys.Unlock()
	return profileKeys.keys[canonicalPath(path)]
}

// SetProtectionKey verifies a profile key before making it available to readers.
// A key is process-local; it is never persisted by this package.
func SetProtectionKey(path string, key []byte) error {
	if len(key) != 32 {
		return ErrAuthentication
	}
	if _, err := os.Stat(path); err == nil {
		db, err := sql.Open("sqlite", dsnPath(path)+"?_pragma=query_only(1)")
		if err != nil {
			return err
		}
		defer db.Close()
		var check string
		err = db.QueryRow(`SELECT value FROM meta WHERE key=?`, protectionMeta).Scan(&check)
		if err == nil {
			candidate := &codec{key: append([]byte(nil), key...)}
			defer candidate.lock()
			data, decodeErr := hex.DecodeString(check)
			if decodeErr != nil {
				return ErrAuthentication
			}
			plain, decodeErr := candidate.open("key-check", data)
			if decodeErr != nil || string(plain) != "motty protected configuration v1" {
				return ErrAuthentication
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	profileKeys.Lock()
	defer profileKeys.Unlock()
	if profileKeys.keys == nil {
		profileKeys.keys = make(map[string]*codec)
	}
	name := canonicalPath(path)
	c := profileKeys.keys[name]
	if c == nil {
		c = &codec{}
		profileKeys.keys[name] = c
	}
	c.mu.Lock()
	clear(c.key)
	c.key = append([]byte(nil), key...)
	c.mu.Unlock()
	return nil
}

func (c *codec) lock() {
	if c == nil {
		return
	}
	c.mu.Lock()
	clear(c.key)
	c.key = nil
	c.mu.Unlock()
}

// LockProtection removes all in-process copies of this store's key.
func LockProtection(path string) { codecFor(path).lock() }

func (c *codec) ready() error {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.key) != 32 {
		return ErrLocked
	}
	return nil
}

func (c *codec) identifier(purpose, name string) (string, error) {
	if c == nil {
		return name, nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.key) != 32 {
		return "", ErrLocked
	}
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte("motty config id/v1/" + purpose + "\x00" + name))
	return "p1_" + hex.EncodeToString(mac.Sum(nil)), nil
}

func (c *codec) seal(aad string, plain []byte) ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.key) != 32 {
		return nil, ErrLocked
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, []byte("motty config/v1/"+aad)), nil
}

func (c *codec) open(aad string, sealed []byte) ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.key) != 32 {
		return nil, ErrLocked
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize()+gcm.Overhead() {
		return nil, ErrAuthentication
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte("motty config/v1/"+aad))
	if err != nil {
		return nil, ErrAuthentication
	}
	return plain, nil
}

type protectedRecord struct {
	ID      string `json:"id"`
	Payload []byte `json:"payload"`
}

func recordAAD(kind, id string, stamp Stamp, deleted bool) string {
	return fmt.Sprintf("record/%s/%s/%s/%t", kind, id, stamp, deleted)
}

func (c *codec) encodeRecord(record Record) (Record, error) {
	if c == nil {
		return record, nil
	}
	id, err := c.identifier("record/"+record.Kind, record.ID)
	if err != nil {
		return Record{}, err
	}
	plain, err := json.Marshal(protectedRecord{record.ID, record.Payload})
	if err != nil {
		return Record{}, err
	}
	sealed, err := c.seal(recordAAD(record.Kind, id, record.Stamp, record.Deleted), plain)
	clear(plain)
	if err != nil {
		return Record{}, err
	}
	record.ID, record.Payload = id, sealed
	return record, nil
}

func (c *codec) decodeRecord(record Record) (Record, error) {
	if c == nil {
		return record, nil
	}
	plain, err := c.open(recordAAD(record.Kind, record.ID, record.Stamp, record.Deleted), record.Payload)
	if err != nil {
		return Record{}, err
	}
	defer clear(plain)
	var decoded protectedRecord
	if json.Unmarshal(plain, &decoded) != nil {
		return Record{}, ErrAuthentication
	}
	id, err := c.identifier("record/"+record.Kind, decoded.ID)
	if err != nil {
		return Record{}, err
	}
	if id != record.ID {
		return Record{}, ErrAuthentication
	}
	record.ID, record.Payload = decoded.ID, decoded.Payload
	return record, nil
}

type protectedLocal struct {
	Key   string `json:"key"`
	Value []byte `json:"value"`
}

func (c *codec) encodeLocal(key string, value []byte) (string, []byte, error) {
	if c == nil {
		return key, value, nil
	}
	id, err := c.identifier("local", key)
	if err != nil {
		return "", nil, err
	}
	plain, err := json.Marshal(protectedLocal{key, value})
	if err != nil {
		return "", nil, err
	}
	sealed, err := c.seal("local/"+id, plain)
	clear(plain)
	return id, sealed, err
}

func (c *codec) decodeLocal(id string, value []byte) (string, []byte, error) {
	if c == nil {
		return id, value, nil
	}
	plain, err := c.open("local/"+id, value)
	if err != nil {
		return "", nil, err
	}
	defer clear(plain)
	var decoded protectedLocal
	if json.Unmarshal(plain, &decoded) != nil {
		return "", nil, ErrAuthentication
	}
	want, err := c.identifier("local", decoded.Key)
	if err != nil {
		return "", nil, err
	}
	if want != id {
		return "", nil, ErrAuthentication
	}
	return decoded.Key, decoded.Value, nil
}

func openCodec(db *sql.DB, path string) (*codec, error) {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return nil, err
	}
	if version < 2 {
		return nil, nil
	}
	var check string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key=?`, protectionMeta).Scan(&check); err != nil {
		return nil, ErrAuthentication
	}
	c := codecFor(path)
	if c == nil {
		return nil, ErrLocked
	}
	if err := c.ready(); err != nil {
		return nil, err
	}
	sealed, err := hex.DecodeString(check)
	if err != nil {
		return nil, ErrAuthentication
	}
	plain, err := c.open("key-check", sealed)
	if err != nil {
		return nil, err
	}
	if string(plain) != "motty protected configuration v1" {
		return nil, ErrAuthentication
	}
	return c, nil
}

// BuildCopy creates a new SQLite image, encrypting records, their identifiers,
// and all device-local values. Nil key creates an ordinary unprotected image.
// The source is untouched and the destination contains no plaintext profile data.
func BuildCopy(source, destination string, format int, key []byte) (err error) {
	if err := EnsureClosed(source); err != nil {
		return err
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return errors.New("config store: migration destination already exists")
		}
		return err
	}
	from, err := Open(source, format)
	if err != nil {
		return err
	}
	defer from.Close()
	records, err := from.Records()
	if err != nil {
		return err
	}
	_, locals, err := from.Load()
	if err != nil {
		return err
	}
	to, err := Open(destination, format)
	if err != nil {
		return err
	}
	defer to.Close()
	if key != nil {
		if len(key) != 32 {
			return ErrAuthentication
		}
		to.codec = &codec{key: append([]byte(nil), key...)}
		defer to.codec.lock()
	}
	ctx := context.Background()
	tx, err := to.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, record := range records {
		if err := writeRecord(ctx, tx, record, to.codec); err != nil {
			return err
		}
	}
	for name, value := range locals {
		if err := to.writeLocal(ctx, tx, name, value); err != nil {
			return err
		}
	}
	for _, name := range []string{"device", "clock", "writer"} {
		value, err := metaValue(ctx, from.db, name)
		if err != nil {
			return err
		}
		if value != "" {
			if err := setMeta(ctx, tx, name, value); err != nil {
				return err
			}
		}
	}
	if to.codec != nil {
		check, err := to.codec.seal("key-check", []byte("motty protected configuration v1"))
		if err != nil {
			return err
		}
		if err := setMeta(ctx, tx, protectionMeta, hex.EncodeToString(check)); err != nil {
			return err
		}
		if _, err := tx.Exec(`PRAGMA user_version=2`); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if _, _, err := load(ctx, to.db, to.codec); err != nil {
		return err
	}
	return nil
}
