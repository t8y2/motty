// Package configstore keeps the application's configuration in one SQLite file
// as records that can be merged between devices.
//
// A record is a kind and an id naming one thing (a host, a snippet, one
// setting), a payload the application owns, and a Stamp saying when this
// version of it was written. A record that was deleted stays as a tombstone for
// a while, so the deletion can reach the other devices. Two devices that hold
// different versions of a record agree on the one with the later stamp, which
// makes merging order-independent: any devices that exchange all their records
// end up with the same ones.
//
// The store knows nothing about what the payloads mean. It also keeps a small
// table of values that belong to this device and are never merged.
//
// Like the other SQLite files of the application it is private to the user (0600
// inside a 0700 directory), uses the rollback journal instead of WAL so no side
// files appear, and overwrites deleted rows.
package configstore

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

var (
	// ErrCorrupt means the file is not a usable database of this store.
	ErrCorrupt = errors.New("config store: database is corrupt or not a config database")
	// ErrTooNew means the file was written by a newer build. The error returned
	// is a *TooNewError, which says which versions.
	ErrTooNew = errors.New("config store: database is newer than this build")
)

// TooNewError is what Open returns for a file written by a newer build.
type TooNewError struct {
	// Found is the version in the file and Supported the one this build reads,
	// as text.
	Found, Supported string
}

func (err *TooNewError) Error() string {
	return fmt.Sprintf("%s (file %s, this build reads %s)", ErrTooNew.Error(), err.Found, err.Supported)
}

func (err *TooNewError) Is(target error) bool { return target == ErrTooNew }

// MaxPayload bounds one record's payload, written or received.
const MaxPayload = 1 << 20

const schemaVersion = 1

var migrations = [schemaVersion]string{
	`CREATE TABLE records (
		kind    TEXT NOT NULL,
		id      TEXT NOT NULL,
		stamp   TEXT NOT NULL,
		deleted INTEGER NOT NULL DEFAULT 0,
		payload BLOB,
		PRIMARY KEY (kind, id)
	) WITHOUT ROWID;
	CREATE TABLE local (
		key   TEXT PRIMARY KEY,
		value BLOB NOT NULL
	) WITHOUT ROWID;
	CREATE TABLE meta (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	) WITHOUT ROWID;`,
}

const sqliteMagic = "SQLite format 3\x00"

// Key names one record.
type Key struct{ Kind, ID string }

// Entry is the live version of a record.
type Entry struct {
	Kind, ID string
	Payload  []byte
}

// Record is a record as it travels between devices: live, or a tombstone.
type Record struct {
	Kind    string          `json:"kind"`
	ID      string          `json:"id"`
	Stamp   Stamp           `json:"stamp"`
	Deleted bool            `json:"deleted,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Change is what the application wants stored: records that now exist, records
// that no longer do, and values of this device. Only differences are written.
type Change struct {
	// Writer identifies the program making the edit, for diagnosis only.
	Writer string
	Put    []Entry
	Delete []Key
	Patch  []PatchEntry
	// Expected guards replacements and deletions against concurrent edits.
	// A nil payload requires that the record is absent or deleted.
	Expected map[Key][]byte
	// Local sets values of this device; an empty value removes the key.
	Local      map[string][]byte
	LocalPatch map[string]JSONChange
	// ExpectedLocal guards an explicit replacement, such as recovery of a
	// malformed workspace, without disabling protection for other local keys.
	ExpectedLocal map[string][]byte
	// MergeLocal merges a patched local value inside the transaction.
	// Callbacks must not call the Store. An error aborts the commit.
	MergeLocal map[string]func(current, next []byte) ([]byte, error)
}

// Store is the open database. It is safe for concurrent use.
type Store struct {
	db     *sql.DB
	format int
	now    func() time.Time

	// mu guards the clock, which every write advances.
	mu    sync.Mutex
	clock clock

	// shared is the registry key of a store from OpenShared, with the number of
	// holders; both are guarded by registry.mu.
	shared  string
	holders int
}

// registry holds the stores opened with OpenShared.
var registry struct {
	mu     sync.Mutex
	stores map[string]*Store
}

// OpenShared is Open, but every caller that asks for the same file gets the same
// store. Windows of one process must share it: the clock that orders their edits
// lives in the store, and two clocks over one file would issue the same stamp
// twice. Each caller closes it; the file is closed with the last.
func OpenShared(path string, format int) (*Store, error) {
	key, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("config store: %w", err)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if store, ok := registry.stores[key]; ok {
		if store.format != format {
			return nil, errors.New("config store: opened with another format")
		}
		store.holders++
		return store, nil
	}
	store, err := Open(path, format)
	if err != nil {
		return nil, err
	}
	store.shared, store.holders = key, 1
	if registry.stores == nil {
		registry.stores = map[string]*Store{}
	}
	registry.stores[key] = store
	return store, nil
}

// Open opens or creates the store at path. Format is the reader/writer
// compatibility epoch; a store requiring a higher epoch is ErrTooNew.
func Open(path string, format int) (*Store, error) {
	if path == "" {
		return nil, errors.New("config store: bad database path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("config store: create directory: %w", err)
	}
	if err := checkHeader(path); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("config store: open file: %w", err)
	}
	file.Close()

	db, err := sql.Open("sqlite", dsnPath(path)+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(DELETE)&_pragma=secure_delete(1)")
	if err != nil {
		return nil, fmt.Errorf("config store: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &Store{db: db, format: format, now: time.Now}
	if err := store.init(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// dsnPath is the path as the driver wants it before the options: a plain path
// unless it holds a character the options would be mistaken for, which then goes
// in a file: URI with those characters escaped.
func dsnPath(path string) string {
	if !strings.ContainsAny(path, "?#%") {
		return path
	}
	escaped := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(filepath.ToSlash(path))
	if !strings.HasPrefix(escaped, "/") {
		escaped = "/" + escaped // C:/dir becomes /C:/dir
	}
	return "file://" + escaped
}

// checkHeader inspects the file without changing it, so a file that is not a
// database, or is a newer one, is told apart before anything is written.
func checkHeader(path string) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("config store: open file: %w", err)
	}
	defer file.Close()
	header := make([]byte, 100)
	n, err := io.ReadFull(file, header)
	if n == 0 {
		return nil // an empty file is a fresh database
	}
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("config store: read header: %w", err)
	}
	if n < len(header) || !bytes.HasPrefix(header, []byte(sqliteMagic)) {
		return ErrCorrupt
	}
	if version := binary.BigEndian.Uint32(header[60:64]); version > schemaVersion {
		return &TooNewError{Found: fmt.Sprintf("schema %d", version), Supported: fmt.Sprintf("schema %d", schemaVersion)}
	}
	return nil
}

func (s *Store) init() error {
	ctx := context.Background()
	var result string
	if err := s.db.QueryRowContext(ctx, `PRAGMA quick_check(1)`).Scan(&result); err != nil || result != "ok" {
		return ErrCorrupt
	}
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return ErrCorrupt
	}
	if version > schemaVersion {
		return &TooNewError{Found: fmt.Sprintf("schema %d", version), Supported: fmt.Sprintf("schema %d", schemaVersion)}
	}
	for v := version; v < schemaVersion; v++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("config store: migrate: %w", err)
		}
		if _, err := tx.ExecContext(ctx, migrations[v]); err != nil {
			tx.Rollback()
			return fmt.Errorf("config store: migrate to v%d: %w", v+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", v+1)); err != nil {
			tx.Rollback()
			return fmt.Errorf("config store: migrate to v%d: %w", v+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("config store: migrate to v%d: %w", v+1, err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("config store: %w", err)
	}
	defer tx.Rollback()
	device, err := metaValue(ctx, tx, "device")
	if err != nil {
		return err
	}
	if device == "" {
		raw := make([]byte, deviceDigits/2)
		if _, err := rand.Read(raw); err != nil {
			return fmt.Errorf("config store: %w", err)
		}
		device = hex.EncodeToString(raw)
		if err := setMeta(ctx, tx, "device", device); err != nil {
			return err
		}
	}
	s.clock.device = device
	stored, err := metaValue(ctx, tx, "format")
	if err != nil {
		return err
	}
	if stored != "" {
		var found int
		if _, err := fmt.Sscanf(stored, "%d", &found); err != nil {
			return ErrCorrupt
		}
		if found > s.format {
			return &TooNewError{Found: fmt.Sprintf("format %d", found), Supported: fmt.Sprintf("format %d", s.format)}
		}
	}
	if stored != fmt.Sprint(s.format) {
		if err := setMeta(ctx, tx, "format", fmt.Sprint(s.format)); err != nil {
			return err
		}
	}
	last, err := metaValue(ctx, tx, "clock")
	if err != nil {
		return err
	}
	s.clock.load(last)
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("config store: %w", err)
	}
	return nil
}

func metaValue(ctx context.Context, tx *sql.Tx, key string) (string, error) {
	var value string
	err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("config store: read %s: %w", key, err)
	}
	return value, nil
}

func setMeta(ctx context.Context, tx *sql.Tx, key, value string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
		return fmt.Errorf("config store: write %s: %w", key, err)
	}
	return nil
}

func (s *Store) checkWritable(ctx context.Context, tx *sql.Tx) error {
	var schema int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&schema); err != nil {
		return err
	}
	if schema > schemaVersion {
		return &TooNewError{Found: fmt.Sprintf("schema %d", schema), Supported: fmt.Sprintf("schema %d", schemaVersion)}
	}
	text, err := metaValue(ctx, tx, "format")
	if err != nil {
		return err
	}
	format, err := strconv.Atoi(text)
	if err != nil || format < 0 {
		return ErrCorrupt
	}
	if format > s.format {
		return &TooNewError{Found: fmt.Sprintf("format %d", format), Supported: fmt.Sprintf("format %d", s.format)}
	}
	return nil
}

func (s *Store) prepareWrite(ctx context.Context, tx *sql.Tx) error {
	if err := s.checkWritable(ctx, tx); err != nil {
		return err
	}
	text, err := metaValue(ctx, tx, "clock")
	if err != nil {
		return err
	}
	other := s.clock
	other.load(text)
	if other.wall > s.clock.wall || other.wall == s.clock.wall && other.counter > s.clock.counter {
		s.clock = other
	}
	return nil
}

// Snapshot returns a transactionally consistent SQLite image, including opaque
// records and device values. Copying an open database file directly is unsafe.
func (s *Store) Snapshot() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.CreateTemp("", "motty-config-snapshot-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())
	if err := file.Close(); err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(`VACUUM main INTO ?`, file.Name()); err != nil {
		return nil, fmt.Errorf("config store: snapshot: %w", err)
	}
	return os.ReadFile(file.Name())
}

// Close releases the database. A store from OpenShared is released when its last
// holder closes it.
func (s *Store) Close() error {
	if s.shared != "" {
		registry.mu.Lock()
		defer registry.mu.Unlock()
		s.holders--
		if s.holders > 0 {
			return nil
		}
		delete(registry.stores, s.shared)
	}
	return s.db.Close()
}

// Device is the id this store puts into the stamps it issues.
func (s *Store) Device() string { return s.clock.device }

// Format is the reader/writer compatibility epoch the store was opened with.
func (s *Store) Format() int { return s.format }

// Load returns every live record and this device's own values.
func (s *Store) Load() (entries []Entry, local map[string][]byte, err error) {
	return load(context.Background(), s.db)
}

// load reads the live records and this device's values from db.
func load(ctx context.Context, db *sql.DB) (entries []Entry, local map[string][]byte, err error) {
	rows, err := db.QueryContext(ctx, `SELECT kind, id, payload FROM records WHERE deleted=0 ORDER BY kind, id`)
	if err != nil {
		return nil, nil, fmt.Errorf("config store: load: %w", err)
	}
	for rows.Next() {
		var entry Entry
		if err := rows.Scan(&entry.Kind, &entry.ID, &entry.Payload); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("config store: load: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, nil, fmt.Errorf("config store: load: %w", err)
	}
	rows, err = db.QueryContext(ctx, `SELECT key, value FROM local`)
	if err != nil {
		return nil, nil, fmt.Errorf("config store: load: %w", err)
	}
	local = map[string][]byte{}
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("config store: load: %w", err)
		}
		local[key] = value
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, nil, fmt.Errorf("config store: load: %w", err)
	}
	return entries, local, nil
}

// ReadOnly reads the live records and this device's values of the store at path
// without changing anything: it does not create the file, migrate it, register a
// device or advance the clock, and it opens the database read-only. It is for a
// second process that only needs to look at what the application saved. A file
// that is missing is os.ErrNotExist, one that is not a store ErrCorrupt, and one
// written by a newer build (a newer schema, or payloads newer than format) a
// *TooNewError; an older schema is read as it is, and reports an error if it
// lacks a table this build needs.
func ReadOnly(path string, format int) (entries []Entry, local map[string][]byte, err error) {
	if path == "" {
		return nil, nil, errors.New("config store: bad database path")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, nil, fmt.Errorf("config store: %w", err)
	}
	if err := checkHeader(path); err != nil {
		return nil, nil, err
	}
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed // C:/dir becomes /C:/dir
	}
	uri := url.URL{Scheme: "file", Path: slashed, RawQuery: "mode=ro&_pragma=busy_timeout(5000)&_pragma=query_only(1)"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, nil, fmt.Errorf("config store: open: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	var stored string
	err = db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='format'`).Scan(&stored)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, nil, ErrCorrupt
	default:
		var found int
		if _, err := fmt.Sscanf(stored, "%d", &found); err != nil {
			return nil, nil, ErrCorrupt
		}
		if found > format {
			return nil, nil, &TooNewError{Found: fmt.Sprintf("format %d", found), Supported: fmt.Sprintf("format %d", format)}
		}
	}
	return load(ctx, db)
}

// Records returns every record, tombstones included: what a device sends to the
// others.
func (s *Store) Records() ([]Record, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT kind, id, stamp, deleted, payload FROM records ORDER BY kind, id`)
	if err != nil {
		return nil, fmt.Errorf("config store: records: %w", err)
	}
	var records []Record
	for rows.Next() {
		var record Record
		var deleted int
		var payload []byte
		if err := rows.Scan(&record.Kind, &record.ID, &record.Stamp, &deleted, &payload); err != nil {
			rows.Close()
			return nil, fmt.Errorf("config store: records: %w", err)
		}
		record.Deleted = deleted != 0
		if !record.Deleted {
			record.Payload = payload
		}
		records = append(records, record)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, fmt.Errorf("config store: records: %w", err)
	}
	return records, nil
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func existing(ctx context.Context, tx queryer, kind, id string) (stamp Stamp, deleted bool, payload []byte, found bool, err error) {
	var flag int
	err = tx.QueryRowContext(ctx, `SELECT stamp, deleted, payload FROM records WHERE kind=? AND id=?`, kind, id).Scan(&stamp, &flag, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil, false, nil
	}
	if err != nil {
		return "", false, nil, false, fmt.Errorf("config store: read record: %w", err)
	}
	return stamp, flag != 0, payload, true, nil
}

func writeRecord(ctx context.Context, tx *sql.Tx, record Record) error {
	var payload []byte
	deleted := 0
	if record.Deleted {
		deleted = 1
	} else {
		payload = record.Payload
		if payload == nil {
			payload = []byte{}
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO records(kind,id,stamp,deleted,payload) VALUES(?,?,?,?,?)
		ON CONFLICT(kind,id) DO UPDATE SET stamp=excluded.stamp, deleted=excluded.deleted, payload=excluded.payload`,
		record.Kind, record.ID, string(record.Stamp), deleted, payload)
	if err != nil {
		return fmt.Errorf("config store: write record: %w", err)
	}
	return nil
}

func validName(text string) bool {
	return text != "" && len(text) <= 256 && !strings.ContainsRune(text, 0)
}

// Commit stores a change and returns how many records it wrote. Records that
// already hold the payload given, and tombstones for records that are already
// gone, are left alone, so storing the same state twice writes nothing and a
// record's stamp moves only when its content does.
func (s *Store) Commit(change Change) (written int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range change.Put {
		if !validName(entry.Kind) || !validName(entry.ID) {
			return 0, errors.New("config store: a record needs a kind and an id")
		}
		if len(entry.Payload) > MaxPayload {
			return 0, fmt.Errorf("config store: %s %s is too large", entry.Kind, entry.ID)
		}
		if !json.Valid(entry.Payload) {
			return 0, fmt.Errorf("config store: %s %s has no JSON payload", entry.Kind, entry.ID)
		}
	}
	ctx := context.Background()
	saved := s.clock
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("config store: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
			s.clock = saved
			written = 0
		}
	}()
	if err := s.prepareWrite(ctx, tx); err != nil {
		return 0, err
	}
	for key, expected := range change.Expected {
		_, deleted, payload, found, err := existing(ctx, tx, key.Kind, key.ID)
		if err != nil {
			return 0, err
		}
		if !found || deleted {
			payload = nil
		}
		if !sameJSON(payload, expected) {
			return 0, fmt.Errorf("%s %s: %w", key.Kind, key.ID, ErrConflict)
		}
	}
	now := s.now()
	for key, expected := range change.ExpectedLocal {
		var current []byte
		err := tx.QueryRowContext(ctx, `SELECT value FROM local WHERE key=?`, key).Scan(&current)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		if !bytes.Equal(current, expected) {
			return 0, fmt.Errorf("%s: %w", key, ErrConflict)
		}
	}
	for _, patch := range change.Patch {
		if !validName(patch.Kind) || !validName(patch.ID) || !json.Valid(patch.Before) || !json.Valid(patch.After) {
			return 0, errors.New("config store: invalid record patch")
		}
		_, deleted, current, found, err := existing(ctx, tx, patch.Kind, patch.ID)
		if err != nil {
			return 0, err
		}
		if !found || deleted {
			return 0, fmt.Errorf("%s %s: %w", patch.Kind, patch.ID, ErrConflict)
		}
		payload, err := patchJSON(current, patch.Before, patch.After)
		if err != nil {
			return 0, fmt.Errorf("%s %s: %w", patch.Kind, patch.ID, err)
		}
		if len(payload) > MaxPayload {
			return 0, fmt.Errorf("config store: %s %s is too large", patch.Kind, patch.ID)
		}
		if sameJSON(current, payload) {
			continue
		}
		if err := writeRecord(ctx, tx, Record{Kind: patch.Kind, ID: patch.ID, Stamp: s.clock.next(now), Payload: payload}); err != nil {
			return 0, err
		}
		written++
	}
	for _, entry := range change.Put {
		_, deleted, payload, found, err := existing(ctx, tx, entry.Kind, entry.ID)
		if err != nil {
			return 0, err
		}
		if found && !deleted && bytes.Equal(payload, entry.Payload) {
			continue
		}
		record := Record{Kind: entry.Kind, ID: entry.ID, Stamp: s.clock.next(now), Payload: entry.Payload}
		if err := writeRecord(ctx, tx, record); err != nil {
			return 0, err
		}
		written++
	}
	for _, key := range change.Delete {
		_, deleted, _, found, err := existing(ctx, tx, key.Kind, key.ID)
		if err != nil {
			return 0, err
		}
		if !found || deleted {
			continue
		}
		if err := writeRecord(ctx, tx, Record{Kind: key.Kind, ID: key.ID, Stamp: s.clock.next(now), Deleted: true}); err != nil {
			return 0, err
		}
		written++
	}
	for key, value := range change.Local {
		if !validName(key) {
			return 0, errors.New("config store: a local value needs a name")
		}
		if len(value) == 0 {
			_, err = tx.ExecContext(ctx, `DELETE FROM local WHERE key=?`, key)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO local(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
		}
		if err != nil {
			return 0, fmt.Errorf("config store: write %s: %w", key, err)
		}
	}
	for key, patch := range change.LocalPatch {
		if !validName(key) || !json.Valid(patch.Before) || !json.Valid(patch.After) {
			return 0, errors.New("config store: invalid local patch")
		}
		var current []byte
		err := tx.QueryRowContext(ctx, `SELECT value FROM local WHERE key=?`, key).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			current = []byte("{}")
		} else if err != nil {
			return 0, fmt.Errorf("config store: read %s: %w", key, err)
		}
		value, err := patchJSON(current, patch.Before, patch.After)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", key, err)
		}
		if merge := change.MergeLocal[key]; merge != nil {
			value, err = merge(current, value)
			if err != nil {
				return 0, fmt.Errorf("config store: merge %s: %w", key, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO local(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			return 0, fmt.Errorf("config store: write %s: %w", key, err)
		}
	}
	if written > 0 {
		if err := setMeta(ctx, tx, "clock", s.clock.String()); err != nil {
			return 0, err
		}
	}
	if change.Writer != "" {
		if err := setMeta(ctx, tx, "writer", change.Writer); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("config store: %w", err)
	}
	return written, nil
}

// MergeResult says what a merge did.
type MergeResult struct {
	// Applied are the received records that replaced or added to what was here,
	// as stored.
	Applied []Record
	// Refused counts received records that were not valid, or carried a stamp
	// further ahead than MaxFuture, and were ignored.
	Refused int
}

// Merge takes records received from another device. For each, the version with
// the later stamp is kept; a received record with the same stamp as the stored
// one is the same version.
func (s *Store) Merge(received []Record) (result MergeResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	saved := s.clock
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MergeResult{}, fmt.Errorf("config store: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
			s.clock = saved
			result = MergeResult{}
		}
	}()
	if err := s.prepareWrite(ctx, tx); err != nil {
		return MergeResult{}, err
	}
	limit := s.now().Add(MaxFuture)
	for _, record := range received {
		if !validName(record.Kind) || !validName(record.ID) || !record.Stamp.valid() ||
			record.Stamp.Time().After(limit) || len(record.Payload) > MaxPayload || !record.Deleted && !json.Valid(record.Payload) {
			result.Refused++
			continue
		}
		if record.Deleted {
			record.Payload = nil
		}
		s.clock.observe(record.Stamp)
		stamp, _, _, found, err := existing(ctx, tx, record.Kind, record.ID)
		if err != nil {
			return MergeResult{}, err
		}
		if found && stamp >= record.Stamp {
			continue
		}
		if err := writeRecord(ctx, tx, record); err != nil {
			return MergeResult{}, err
		}
		result.Applied = append(result.Applied, record)
	}
	if err := setMeta(ctx, tx, "clock", s.clock.String()); err != nil {
		return MergeResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return MergeResult{}, fmt.Errorf("config store: %w", err)
	}
	return result, nil
}

// Newer returns the records here that are not in received at the same stamp or
// later: what the other device is missing.
func (s *Store) Newer(received []Record) ([]Record, error) {
	have := make(map[Key]Stamp, len(received))
	for _, record := range received {
		key := Key{record.Kind, record.ID}
		if have[key] < record.Stamp {
			have[key] = record.Stamp
		}
	}
	all, err := s.Records()
	if err != nil {
		return nil, err
	}
	var missing []Record
	for _, record := range all {
		if have[Key{record.Kind, record.ID}] < record.Stamp {
			missing = append(missing, record)
		}
	}
	return missing, nil
}

// Prune forgets tombstones older than keep, once every device has had time to
// hear of the deletion. A device that stays away longer than that may bring a
// deleted record back.
func (s *Store) Prune(keep time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := s.checkWritable(ctx, tx); err != nil {
		return 0, err
	}
	cutoff := CutoffStamp(s.now().Add(-keep))
	result, err := tx.ExecContext(ctx, `DELETE FROM records WHERE deleted=1 AND stamp < ?`, string(cutoff))
	if err != nil {
		return 0, fmt.Errorf("config store: prune: %w", err)
	}
	count, _ := result.RowsAffected()
	return int(count), tx.Commit()
}

// NextStamp issues a stamp later than every one this store has issued or seen,
// for a version that is kept somewhere else (the saved passwords) and compared
// with the records' stamps by the same rule.
func (s *Store) NextStamp() (Stamp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	saved := s.clock
	if err := s.prepareWrite(ctx, tx); err != nil {
		return "", err
	}
	stamp := s.clock.next(s.now())
	if err := setMeta(ctx, tx, "clock", s.clock.String()); err != nil {
		s.clock = saved
		return "", err
	}
	if err := tx.Commit(); err != nil {
		s.clock = saved
		return "", err
	}
	return stamp, nil
}

// Acceptable reports whether a stamp received from another device is one to
// take: well formed, and not so far ahead of this clock that it would outrank
// every later edit made here.
func (s *Store) Acceptable(stamp Stamp) bool {
	return stamp.valid() && !stamp.Time().After(s.now().Add(MaxFuture))
}

// Observe moves the clock past stamps received from another device, so an edit
// made here afterwards is later than what was received. Stamps that are not
// Acceptable are ignored.
func (s *Store) Observe(stamps ...Stamp) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	saved := s.clock
	for _, stamp := range stamps {
		if s.Acceptable(stamp) {
			s.clock.observe(stamp)
		}
	}
	if s.clock == saved {
		return nil
	}
	if err := s.saveClock(); err != nil {
		s.clock = saved
		return err
	}
	return nil
}

func (s *Store) saveClock() error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("config store: %w", err)
	}
	defer tx.Rollback()
	if err := s.prepareWrite(context.Background(), tx); err != nil {
		return err
	}
	if err := setMeta(context.Background(), tx, "clock", s.clock.String()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("config store: %w", err)
	}
	return nil
}
