package sessionlog

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// schemaVersion is the schema this build writes. A file with a newer one is
// ErrTooNew. The version lives in PRAGMA user_version.
const schemaVersion = 2

const schemaV2 = `
ALTER TABLE session_logs ADD COLUMN catalog_id TEXT NOT NULL DEFAULT '';
ALTER TABLE session_logs ADD COLUMN port INTEGER NOT NULL DEFAULT 0;
ALTER TABLE session_logs ADD COLUMN shell_name TEXT NOT NULL DEFAULT '';
ALTER TABLE session_logs ADD COLUMN target_kind INTEGER NOT NULL DEFAULT 0;`

// schemaV1 creates the two tables. The events cascade with their record, and the
// indexes cover the keyset order and the filters the page uses.
const schemaV1 = `
CREATE TABLE session_logs (
	id              TEXT PRIMARY KEY NOT NULL,
	run_id          TEXT NOT NULL,
	host            TEXT NOT NULL,
	address         TEXT NOT NULL,
	account         TEXT NOT NULL,
	protocol        INTEGER NOT NULL,
	started_at      INTEGER NOT NULL,
	ended_at        INTEGER,
	status          INTEGER NOT NULL,
	favorite        INTEGER NOT NULL DEFAULT 0,
	reconnect_count INTEGER NOT NULL DEFAULT 0,
	reason          INTEGER NOT NULL DEFAULT 0,
	truncated       INTEGER NOT NULL DEFAULT 0
) WITHOUT ROWID;
CREATE INDEX session_logs_started_id_idx ON session_logs (started_at DESC, id DESC);
CREATE INDEX session_logs_status_idx ON session_logs (status);
CREATE INDEX session_logs_favorite_idx ON session_logs (favorite);
CREATE TABLE session_log_events (
	record_id TEXT NOT NULL REFERENCES session_logs (id) ON DELETE CASCADE,
	seq       INTEGER NOT NULL,
	kind      INTEGER NOT NULL,
	at        INTEGER NOT NULL,
	attempt   INTEGER NOT NULL DEFAULT 0,
	reason    INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (record_id, seq)
) WITHOUT ROWID;`

// Store is an open session-log database. It is safe for concurrent use if the
// Queue is not used; every method serializes on one mutex and the database uses
// one connection.
type Store struct {
	db   *sql.DB
	path string
	now  func() time.Time

	mu     sync.Mutex
	closed bool
}

// Open opens or creates the session log at path. The parent directory is
// created 0700 and a new database file 0600; an existing file keeps its mode.
// The database uses one connection, a busy timeout, the rollback journal (so no
// side files appear) and secure_delete. A missing path, a corrupt file, a read-only file or a file from a newer build returns
// an error; callers keep working without logging.
func Open(path string) (*Store, error) {
	return OpenWithClock(path, time.Now)
}

// OpenWithClock uses the supplied wall clock for retention and timestamps.
// It lets the workbench use the same clock as its session lifecycle events.
func OpenWithClock(path string, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	if path == "" {
		return nil, errors.New("sessionlog: bad database path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("sessionlog: create directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("sessionlog: open database: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("sessionlog: open database: %w", err)
	}
	// A file that was already there keeps its mode through OpenFile; the journal
	// is private, so tighten it here rather than leaving a copied file readable.
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("sessionlog: protect database: %w", err)
	}
	db, err := sql.Open("sqlite", sessionLogDSNPath(path)+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(DELETE)&_pragma=secure_delete(1)")
	if err != nil {
		return nil, fmt.Errorf("sessionlog: open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	store := &Store{db: db, path: path, now: now}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// A path with URI delimiters needs escaping before SQLite query options are appended.
func sessionLogDSNPath(path string) string {
	if !strings.ContainsAny(path, "?#%") {
		return path
	}
	escaped := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(filepath.ToSlash(path))
	if !strings.HasPrefix(escaped, "/") {
		escaped = "/" + escaped
	}
	return "file://" + escaped
}

// migrate creates the schema of a new file, or validates the version of an
// existing one.
func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("sessionlog: read schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("%w (file %d, this build reads %d)", ErrTooNew, version, schemaVersion)
	}
	if version == schemaVersion {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("sessionlog: migrate: %w", err)
	}
	defer tx.Rollback()
	if version == 0 {
		if _, err := tx.Exec(schemaV1); err != nil {
			return fmt.Errorf("sessionlog: migrate: %w", err)
		}
	}
	if version < 2 {
		if _, err := tx.Exec(schemaV2); err != nil {
			return fmt.Errorf("sessionlog: migrate: %w", err)
		}
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("sessionlog: migrate: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sessionlog: migrate: %w", err)
	}
	return nil
}

// usable reports whether the database may still be touched. The caller holds mu.
func (s *Store) usable() error {
	if s.closed {
		return ErrClosed
	}
	return nil
}

// Close closes the database. Later calls are no-ops, and later reads or writes
// return ErrClosed.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("sessionlog: close: %w", err)
	}
	return nil
}

// Start inserts a connecting record. The lifecycle fields are owned by the
// store: Status becomes connecting, EndedAt empty and Reason none. Host is
// required; the other snapshots may be empty and are bounded. The insert also
// applies retention: finished un-favorited rows are kept to the most recent
// maxUnfavoritedRecords and pruned once older than unfavoritedRetention,
// measured from the new record's StartedAt; favorites are exempt.
func (s *Store) Start(record Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(); err != nil {
		return err
	}
	normalized, err := normalizeStart(record, s.now().UTC())
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("sessionlog: start: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO session_logs
		(id, run_id, host, address, account, protocol, started_at, ended_at, status, favorite, reconnect_count, reason, truncated, catalog_id, port, shell_name, target_kind)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, 0, ?, 0, ?, ?, ?, ?)`,
		normalized.ID, normalized.RunID, normalized.Host, normalized.Address, normalized.Account,
		int(normalized.Protocol), normalized.StartedAt.UnixMilli(), int(StatusConnecting),
		boolInt(normalized.Favorite), int(ReasonNone), normalized.CatalogID, normalized.Port, normalized.ShellName, normalized.TargetKind); err != nil {
		return fmt.Errorf("sessionlog: start: %w", err)
	}
	if err := insertEvent(tx, Event{RecordID: normalized.ID, Kind: EventStarted, At: normalized.StartedAt}, false); err != nil {
		return err
	}
	if err := prune(tx, normalized.StartedAt); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sessionlog: start: %w", err)
	}
	return nil
}

// prune applies retention in one transaction, oldest un-favorited rows first:
// rows past the age window and rows beyond the count cap are the same oldest
// rows, so one statement removes both.
func prune(tx *sql.Tx, reference time.Time) error {
	cutoff := reference.Add(-unfavoritedRetention).UnixMilli()
	_, err := tx.Exec(`DELETE FROM session_logs
		WHERE favorite = 0 AND status IN (?, ?, ?, ?)
		  AND (started_at < ?
		       OR id IN (SELECT id FROM session_logs WHERE favorite = 0 AND status IN (?, ?, ?, ?)
		                 ORDER BY started_at DESC, id DESC LIMIT -1 OFFSET ?))`,
		int(StatusEnded), int(StatusFailed), int(StatusInterrupted), int(StatusTrackingStopped),
		cutoff, int(StatusEnded), int(StatusFailed), int(StatusInterrupted), int(StatusTrackingStopped), maxUnfavoritedRecords)
	if err != nil {
		return fmt.Errorf("sessionlog: prune: %w", err)
	}
	return nil
}

// AppendEvent appends one event to a live record. Events for an unknown record
// are ErrNotFound. Events of a terminal record, which is a late race, are
// dropped. A record keeps at most maxEventsPerRecord events: later ones are
// dropped and the record is marked Truncated. An event that says the session
// connected or opened a shell makes it active, and a scheduled or started
// reconnect makes it reconnecting; reconnect events also raise ReconnectCount
// to at least their Attempt.
func (s *Store) AppendEvent(event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(); err != nil {
		return err
	}
	if err := validateKey("record id", event.RecordID); err != nil {
		return err
	}
	if !event.Kind.valid() {
		return fmt.Errorf("%w: event kind %d", ErrInvalidRecord, event.Kind)
	}
	if !event.Reason.valid() {
		return fmt.Errorf("%w: reason %d", ErrInvalidRecord, event.Reason)
	}
	attempt := event.Attempt
	if attempt < 0 {
		attempt = 0
	}
	at := event.At
	if at.IsZero() {
		at = s.now().UTC()
	} else {
		at = at.UTC()
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("sessionlog: append event: %w", err)
	}
	defer tx.Rollback()
	var status Status
	var protocol Protocol
	err = tx.QueryRow(`SELECT status, protocol FROM session_logs WHERE id = ?`, event.RecordID).Scan(&status, &protocol)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: event for %s", ErrNotFound, event.RecordID)
	}
	if err != nil {
		return fmt.Errorf("sessionlog: append event: %w", err)
	}
	if status.terminal() {
		// The session already finished; a late event must not rewrite history.
		return tx.Commit()
	}
	event.At, event.Attempt = at, attempt
	if err := insertEvent(tx, event, false); err != nil {
		return err
	}
	if reconnectEvent(event.Kind) && attempt > 0 {
		if _, err := tx.Exec(`UPDATE session_logs SET reconnect_count = MAX(reconnect_count, ?) WHERE id = ?`, attempt, event.RecordID); err != nil {
			return fmt.Errorf("sessionlog: append event: %w", err)
		}
	}
	if next, ok := statusAfterEvent(event.Kind, protocol); ok {
		if _, err := tx.Exec(`UPDATE session_logs SET status = ? WHERE id = ?`, int(next), event.RecordID); err != nil {
			return fmt.Errorf("sessionlog: append event: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sessionlog: append event: %w", err)
	}
	return nil
}

// Reserve the last slot for a final event, including when upgrading a full v1
// timeline. A truncated timeline must still describe the record's final result.
func insertEvent(tx *sql.Tx, event Event, final bool) error {
	var seq int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM session_log_events WHERE record_id = ?`, event.RecordID).Scan(&seq); err != nil {
		return err
	}
	limit := maxEventsPerRecord - 1
	if final {
		limit++
	}
	if seq > limit {
		if _, err := tx.Exec(`UPDATE session_logs SET truncated = 1 WHERE id = ?`, event.RecordID); err != nil {
			return err
		}
		if !final {
			return nil
		}
		seq = maxEventsPerRecord
		if _, err := tx.Exec(`DELETE FROM session_log_events WHERE record_id = ? AND seq >= ?`, event.RecordID, seq); err != nil {
			return err
		}
	}
	_, err := tx.Exec(`INSERT INTO session_log_events (record_id, seq, kind, at, attempt, reason) VALUES (?, ?, ?, ?, ?, ?)`, event.RecordID, seq, event.Kind, event.At.UnixMilli(), event.Attempt, event.Reason)
	return err
}

// nonTerminalUpdate finishes only a session that is still connecting, active or
// reconnecting, which makes a second Finish a no-op.
var nonTerminalUpdate = fmt.Sprintf(`UPDATE session_logs
	SET status = ?, ended_at = ?, reason = ?
	WHERE id = ? AND status NOT IN (%d, %d, %d, %d)`,
	StatusEnded, StatusFailed, StatusInterrupted, StatusTrackingStopped)

// Finish writes a terminal status, its end time and a known reason code. It is
// idempotent: once a record is terminal, later Finish calls do not overwrite its
// status, EndedAt or reason. status must be terminal (ended, failed, interrupted
// or tracking stopped); any other status is ErrInvalidRecord. An unknown record
// is ErrNotFound.
func (s *Store) Finish(id string, status Status, endedAt time.Time, reason Reason) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(); err != nil {
		return err
	}
	if err := validateKey("record id", id); err != nil {
		return err
	}
	if !status.terminal() {
		return fmt.Errorf("%w: status %d is not terminal", ErrInvalidRecord, status)
	}
	if !reason.valid() {
		return fmt.Errorf("%w: reason %d", ErrInvalidRecord, reason)
	}
	at := endedAt
	if at.IsZero() {
		at = s.now().UTC()
	} else {
		at = at.UTC()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := finishRecord(tx, id, status, at, reason, EventFinished); err != nil {
		return fmt.Errorf("sessionlog: finish: %w", err)
	}
	return tx.Commit()
}

func finishRecord(tx *sql.Tx, id string, status Status, at time.Time, reason Reason, kind EventKind) error {
	result, err := tx.Exec(nonTerminalUpdate, int(status), at.UnixMilli(), int(reason), id)
	if err != nil {
		return fmt.Errorf("sessionlog: finish: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sessionlog: finish: %w", err)
	}
	if affected > 0 {
		return insertEvent(tx, Event{RecordID: id, Kind: kind, At: at, Reason: reason}, true)
	}
	// Nothing changed: the record is either terminal already (nothing to do) or
	// missing (an error).
	var existing Status
	err = tx.QueryRow(`SELECT status FROM session_logs WHERE id = ?`, id).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return fmt.Errorf("sessionlog: finish: %w", err)
	}
	return nil
}

// Recover marks the records of a previous process run as interrupted: rows whose
// RunID is not runID and whose status is connecting, active or reconnecting get
// interrupted, EndedAt = at and reason process restart. Rows of the current run,
// and rows that are already terminal, are untouched.
func (s *Store) Recover(runID string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(); err != nil {
		return err
	}
	if runID != "" {
		if err := validateKey("run id", runID); err != nil {
			return err
		}
	}
	ended := at
	if ended.IsZero() {
		ended = s.now().UTC()
	} else {
		ended = ended.UTC()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id FROM session_logs WHERE run_id <> ? AND status IN (?, ?, ?)`, runID, StatusConnecting, StatusActive, StatusReconnecting)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := finishRecord(tx, id, StatusInterrupted, ended.UTC(), ReasonProcessRestart, EventRecovered); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// List returns one page of records, newest first. Ordering is strictly
// descending (StartedAt, ID); save Next and pass it as Query.Cursor to continue,
// and expect More to say whether another page exists. Limit clamps to 1..50.
func (s *Store) List(query Query) (Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(); err != nil {
		return Page{}, err
	}
	if err := validateCursor(query); err != nil {
		return Page{}, err
	}
	if err := s.pruneExpired(s.now().UTC()); err != nil {
		return Page{}, err
	}
	if query.Status != nil && !query.Status.valid() {
		return Page{}, fmt.Errorf("%w: status filter %d", ErrInvalidRecord, *query.Status)
	}
	if query.Protocol != nil && !query.Protocol.valid() {
		return Page{}, fmt.Errorf("%w: protocol filter %d", ErrInvalidRecord, *query.Protocol)
	}
	limit := clampLimit(query.Limit)
	where := make([]string, 0, 8)
	args := make([]any, 0, 12)
	switch query.Outcome {
	case OutcomeAll:
	case OutcomeNormal:
		where = append(where, `status = ?`)
		args = append(args, StatusEnded)
	case OutcomeAbnormal:
		where = append(where, `status IN (?, ?)`)
		args = append(args, StatusFailed, StatusInterrupted)
	case OutcomeTrackingStopped:
		where = append(where, `status = ?`)
		args = append(args, StatusTrackingStopped)
	default:
		return Page{}, ErrInvalidRecord
	}
	if query.Search != "" {
		pattern := "%" + escapeLike(query.Search) + "%"
		where = append(where, `(host LIKE ? ESCAPE '\' OR address LIKE ? ESCAPE '\' OR account LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern, pattern)
	}
	if query.Status != nil {
		where = append(where, `status = ?`)
		args = append(args, int(*query.Status))
	}
	if query.Protocol != nil {
		where = append(where, `protocol = ?`)
		args = append(args, int(*query.Protocol))
	}
	if query.Favorite != nil {
		where = append(where, `favorite = ?`)
		args = append(args, boolInt(*query.Favorite))
	}
	if !query.Since.IsZero() {
		where = append(where, `started_at >= ?`)
		args = append(args, query.Since.UnixMilli())
	}
	if !query.Until.IsZero() {
		where = append(where, `started_at < ?`)
		args = append(args, query.Until.UnixMilli())
	}
	filter := ""
	if len(where) > 0 {
		filter = ` WHERE ` + strings.Join(where, ` AND `)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Page{}, err
	}
	defer tx.Rollback()
	var page Page
	if err := tx.QueryRow(`SELECT COUNT(*) FROM session_logs`).Scan(&page.Total); err != nil {
		return Page{}, err
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM session_logs`+filter, args...).Scan(&page.Matched); err != nil {
		return Page{}, err
	}
	if clause, cursorArgs := cursorPredicate(query); clause != "" {
		where = append(where, clause)
		args = append(args, cursorArgs...)
	}
	statement := `SELECT id, run_id, host, address, account, protocol, started_at, ended_at,
		status, favorite, reconnect_count, reason, truncated, catalog_id, port, shell_name, target_kind FROM session_logs`
	if len(where) > 0 {
		statement += ` WHERE ` + strings.Join(where, ` AND `)
	}
	statement += ` ORDER BY ` + queryOrder(query.Sort) + ` LIMIT ?`
	args = append(args, limit+1)
	rows, err := tx.Query(statement, args...)
	if err != nil {
		return Page{}, fmt.Errorf("sessionlog: list: %w", err)
	}
	defer rows.Close()
	records := make([]Record, 0, limit+1)
	for rows.Next() {
		var record Record
		var protocol, status, reason, favorite, truncated int
		var started int64
		var ended sql.NullInt64
		if err := rows.Scan(&record.ID, &record.RunID, &record.Host, &record.Address, &record.Account,
			&protocol, &started, &ended, &status, &favorite, &record.ReconnectCount, &reason, &truncated, &record.CatalogID, &record.Port, &record.ShellName, &record.TargetKind); err != nil {
			return Page{}, fmt.Errorf("sessionlog: list: %w", err)
		}
		record.Protocol = Protocol(protocol)
		record.Status = Status(status)
		record.Reason = Reason(reason)
		record.Favorite = favorite != 0
		record.Truncated = truncated != 0
		record.StartedAt = time.UnixMilli(started).UTC()
		if ended.Valid {
			record.EndedAt = time.UnixMilli(ended.Int64).UTC()
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("sessionlog: list: %w", err)
	}
	if err := rows.Close(); err != nil {
		return Page{}, err
	}
	if err := tx.Commit(); err != nil {
		return Page{}, err
	}
	page.Records = records
	if len(records) > limit {
		page.More = true
		page.Records = records[:limit]
		last := page.Records[len(page.Records)-1]
		page.Next = cursorFor(last, query.Sort)
	}
	return page, nil
}

func (s *Store) pruneExpired(at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("sessionlog: prune: %w", err)
	}
	defer tx.Rollback()
	if err := prune(tx, at); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sessionlog: prune: %w", err)
	}
	return nil
}

// Events returns a record's events, oldest first, at most maxEventsPerRecord.
func (s *Store) Events(id string) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(); err != nil {
		return nil, err
	}
	if err := validateKey("record id", id); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT kind, at, attempt, reason FROM session_log_events
		WHERE record_id = ? ORDER BY seq ASC LIMIT ?`, id, maxEventsPerRecord)
	if err != nil {
		return nil, fmt.Errorf("sessionlog: events: %w", err)
	}
	defer rows.Close()
	events := make([]Event, 0, 8)
	for rows.Next() {
		var kind, at, attempt, reason int
		if err := rows.Scan(&kind, &at, &attempt, &reason); err != nil {
			return nil, fmt.Errorf("sessionlog: events: %w", err)
		}
		events = append(events, Event{
			RecordID: id,
			Kind:     EventKind(kind),
			At:       time.UnixMilli(int64(at)).UTC(),
			Attempt:  attempt,
			Reason:   Reason(reason),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sessionlog: events: %w", err)
	}
	return events, nil
}

// SetFavorite marks or unmarks a record. A record that is already missing is
// ErrNotFound. Un-favoriting does not prune immediately; the next Start applies
// retention and removes it if it is out of the window.
func (s *Store) SetFavorite(id string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(); err != nil {
		return err
	}
	if err := validateKey("record id", id); err != nil {
		return err
	}
	result, err := s.db.Exec(`UPDATE session_logs SET favorite = ? WHERE id = ?`, boolInt(on), id)
	if err != nil {
		return fmt.Errorf("sessionlog: set favorite: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sessionlog: set favorite: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return nil
}

// Delete removes one record and, through the foreign key, its events. Deleting
// a record that is already gone is not an error.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(); err != nil {
		return err
	}
	if err := validateKey("record id", id); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM session_logs WHERE id = ?`, id); err != nil {
		return fmt.Errorf("sessionlog: delete: %w", err)
	}
	return nil
}

// Clear removes every record, or only the un-favorited ones. Events cascade. It
// touches only this database; command history, recordings and diagnostic logs are
// separate files and stay untouched. The file is vacuumed afterwards so the
// content is not merely unlinked.
func (s *Store) Clear(includeFavorites bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(); err != nil {
		return err
	}
	statement := `DELETE FROM session_logs`
	if !includeFavorites {
		statement += ` WHERE favorite = 0`
	}
	if _, err := s.db.Exec(statement); err != nil {
		return fmt.Errorf("sessionlog: clear: %w", err)
	}
	if _, err := s.db.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("sessionlog: clear: %w", err)
	}
	return nil
}

// Stats is the journal's footprint, so the page can show what it occupies
// without reading the whole database.
type Stats struct {
	Records   int
	Favorites int
	Events    int
	Bytes     int64
}

// Stats summarizes the journal: records, how many are favorites, how many
// timeline events they hold, and the size of the file on disk. Reading it is a
// few counts and one stat, never a scan of the rows.
func (s *Store) Stats() (Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(); err != nil {
		return Stats{}, err
	}
	var stats Stats
	if err := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(favorite), 0) FROM session_logs`).
		Scan(&stats.Records, &stats.Favorites); err != nil {
		return Stats{}, fmt.Errorf("sessionlog: stats: %w", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM session_log_events`).Scan(&stats.Events); err != nil {
		return Stats{}, fmt.Errorf("sessionlog: stats: %w", err)
	}
	if info, err := os.Stat(s.path); err == nil {
		stats.Bytes = info.Size()
	}
	return stats, nil
}

func boolInt(flag bool) int {
	if flag {
		return 1
	}
	return 0
}

// PreviewClear captures identities, not a moving filter or time boundary.
func (s *Store) PreviewClear(includeFavorites bool) (ClearSelection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(); err != nil {
		return ClearSelection{}, err
	}
	statement := `SELECT id, favorite FROM session_logs`
	if !includeFavorites {
		statement += ` WHERE favorite = 0`
	}
	rows, err := s.db.Query(statement)
	if err != nil {
		return ClearSelection{}, err
	}
	defer rows.Close()
	var selection ClearSelection
	for rows.Next() {
		var id string
		var favorite int
		if err := rows.Scan(&id, &favorite); err != nil {
			return ClearSelection{}, err
		}
		selection.IDs = append(selection.IDs, id)
		selection.Favorites += favorite
	}
	return selection, rows.Err()
}

func (s *Store) DeleteSelection(ids []string, includeFavorites bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := validateKey("record id", id); err != nil {
			return err
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for start := 0; start < len(ids); start += 200 {
		batch := ids[start:min(start+200, len(ids))]
		statement := `DELETE FROM session_logs WHERE id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",") + `)`
		if !includeFavorites {
			statement += ` AND favorite = 0`
		}
		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		if _, err := tx.Exec(statement, args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}
