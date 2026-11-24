package configstore

import (
	"bytes"
	"cmp"
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
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
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

// CheckpointForMigration folds committed WAL frames into the main file and
// lets SQLite recover a hot rollback journal before taking a physical snapshot.
// The caller holds the exclusive profile lease until the migration finishes.
func CheckpointForMigration(path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	db, err := sql.Open("sqlite", dsnPath(path)+"?mode=rw&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode=DELETE").Scan(&mode); err != nil {
		return storeError("checkpoint", err)
	}
	if mode != "delete" {
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
	ctx := context.Background()
	db, err := openReadOnly(ctx, path, math.MaxInt)
	if err != nil {
		return err
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != 2 {
		return ErrAuthentication
	}
	var check string
	if err := db.QueryRow("SELECT value FROM meta WHERE key=?", protectionMeta).Scan(&check); err != nil {
		return errors.Join(ErrAuthentication, err)
	}
	data, err := hex.DecodeString(check)
	if err != nil {
		return ErrAuthentication
	}
	candidate := &codec{key: append([]byte(nil), key...)}
	defer candidate.lock()
	plain, err := candidate.open("key-check", data)
	if err != nil || string(plain) != "motty protected configuration v1" {
		return ErrAuthentication
	}
	if _, _, err := load(ctx, db, candidate); err != nil {
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
	if version == 1 {
		var found int
		if err := db.QueryRow("SELECT COUNT(*) FROM meta WHERE key=?", protectionMeta).Scan(&found); err != nil {
			return nil, err
		}
		if found != 0 {
			return nil, ErrAuthentication
		}
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
// With a key, the destination contains only protected records and local values.
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
	to, err := Open(destination, format)
	if err != nil {
		return err
	}
	defer to.Close()
	from, err := Open(source, format)
	if err != nil {
		return err
	}
	defer from.Close()
	return copyRecords(from, to, key)
}

// BuildUnprotectedImage prepares the future ordinary database entirely in memory.
// Protection can persist the image under its migration key until commit.
func BuildUnprotectedImage(source string, format int) ([]byte, error) {
	if err := EnsureClosed(source); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	to := &Store{db: db, format: format, now: time.Now}
	if err := to.init(); err != nil {
		return nil, err
	}
	// A protected source must already exist. Opening it read-only also
	// prevents SQLite from creating an empty replacement if it disappears.
	src, err := openReadOnly(context.Background(), source, format)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	c, err := openCodec(src, source)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, ErrAuthentication
	}
	if err := copyRecords(&Store{db: src, codec: c}, to, nil); err != nil {
		return nil, err
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var image []byte
	err = conn.Raw(func(raw any) error {
		serializer, ok := raw.(interface{ Serialize() ([]byte, error) })
		if !ok {
			return errors.New("config store: SQLite serialization unavailable")
		}
		var err error
		image, err = serializer.Serialize()
		return err
	})
	return image, err
}

func copyRecords(from, to *Store, key []byte) error {
	ctx := context.Background()
	snapshot, err := from.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer snapshot.Rollback()
	records, err := readRecords(ctx, snapshot, from.codec, "ORDER BY kind, id")
	if err != nil {
		return err
	}
	locals, err := readLocals(ctx, snapshot, from.codec)
	if err != nil {
		return err
	}
	if key != nil {
		if len(key) != 32 {
			return ErrAuthentication
		}
		to.codec = &codec{key: append([]byte(nil), key...)}
		defer to.codec.lock()
	}
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
		value, err := metaValue(ctx, snapshot, name)
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
	copied, err := readRecords(ctx, to.db, to.codec, "ORDER BY kind, id")
	if err != nil {
		return err
	}
	copiedLocal, err := readLocals(ctx, to.db, to.codec)
	if err != nil {
		return err
	}
	// Protected identifiers have different physical ordering. Compare the
	// decoded row identities and contents, including tombstones and clocks.
	order := func(a, b Record) int {
		if k := cmp.Compare(a.Kind, b.Kind); k != 0 {
			return k
		}
		return cmp.Compare(a.ID, b.ID)
	}
	slices.SortFunc(records, order)
	slices.SortFunc(copied, order)
	match := slices.EqualFunc(records, copied, func(a, b Record) bool {
		return a.Kind == b.Kind && a.ID == b.ID && a.Stamp == b.Stamp && a.Deleted == b.Deleted && bytes.Equal(a.Payload, b.Payload)
	})
	if !match || !maps.EqualFunc(locals, copiedLocal, bytes.Equal) {
		return errors.New("config store: migration verification failed")
	}
	return nil
}
