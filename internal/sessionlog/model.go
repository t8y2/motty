// Package sessionlog keeps a local, bounded journal of connection lifecycles:
// when a session started, which host and account it was opened for, how it
// ended, and which lifecycle events happened in between.
//
// Privacy is the point of the package. The database is a separate SQLite file
// (0600 inside a 0700 directory) that is never part of a backup or a sync. Only
// typed, bounded display snapshots and known enum values are stored: there is no
// column for terminal bytes, credentials, authentication answers or arbitrary
// error text, and the write methods reject strings that hold control characters.
// Callers must pass the host/account they are willing to show on screen, never a
// raw server response.
//
// The store is local-only. It is safe for concurrent use: every method takes a
// mutex and the database uses a single connection. A broken or read-only file
// makes Open and the write methods return an error; callers must not treat that
// as fatal for the connection itself.
package sessionlog

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Status is the lifecycle state of one recorded session.
type Status uint8

const (
	// StatusConnecting means the record exists but the connection is not usable
	// yet.
	StatusConnecting Status = iota + 1
	// StatusActive means the transport connected and (for shells) opened.
	StatusActive
	// StatusReconnecting means an automatic retry is in progress; it is still
	// the same logical session.
	StatusReconnecting
	// StatusEnded is a terminal status: the user or the server closed it.
	StatusEnded
	// StatusFailed is a terminal status: the session never became usable, or it
	// broke without recovery.
	StatusFailed
	// StatusInterrupted is a terminal status: a previous process run left it
	// active and it was recovered on startup.
	StatusInterrupted
	// StatusTrackingStopped is a terminal status: metadata capture was disabled
	// for a session that was still open. It does not claim the session closed.
	StatusTrackingStopped
)

func (status Status) valid() bool {
	return status >= StatusConnecting && status <= StatusTrackingStopped
}

// terminal reports whether a status is final. A terminal record is never
// changed again: Finish is idempotent and late events are dropped.
func (status Status) terminal() bool {
	switch status {
	case StatusEnded, StatusFailed, StatusInterrupted, StatusTrackingStopped:
		return true
	default:
		return false
	}
}

// Protocol is the kind of connection a record describes.
type Protocol uint8

const (
	ProtocolSSH Protocol = iota + 1
	ProtocolLocal
	ProtocolTelnet
	ProtocolSerial
	ProtocolVNC
	ProtocolRDP
)

func (protocol Protocol) valid() bool {
	return protocol >= ProtocolSSH && protocol <= ProtocolRDP
}

// Reason is a known, non-sensitive result code. It never carries an error
// string from the connection stack.
type Reason uint8

const (
	ReasonNone Reason = iota
	ReasonUserClosed
	ReasonConnectFailed
	ReasonAuthFailed
	ReasonHostKeyRejected
	ReasonCanceled
	ReasonTimeout
	ReasonNetworkLost
	ReasonShellFailed
	ReasonTrackingOff
	ReasonProcessRestart
	ReasonUnknown
)

func (reason Reason) valid() bool {
	return reason >= ReasonNone && reason <= ReasonUnknown
}

// EventKind is one step in a session's timeline.
type EventKind uint8

const (
	EventConnected EventKind = iota + 1
	EventShellOpened
	EventShellFailed
	EventDisconnected
	EventFailed
	EventReconnectScheduled
	EventReconnectStarted
	EventReconnectExhausted
	EventReconnectCanceled
	EventTrackingStopped
	EventStarted
	EventHostKeyPrompt
	EventAuthenticating
	EventFinished
	EventRecovered
)

func (kind EventKind) valid() bool {
	return kind >= EventConnected && kind <= EventRecovered
}

// TargetKind distinguishes reusable catalog entries from borrowed transports.
type TargetKind uint8

const (
	TargetUnknown TargetKind = iota
	TargetSavedHost
	TargetLocal
	TargetShared
)

// Record is one logical session. ID is a random 128-bit identifier that stays
// with the session when its tab moves between panes or windows; RunID names the
// process run that owns it.
type Record struct {
	ID             string
	RunID          string
	Host           string
	Address        string
	Account        string
	Protocol       Protocol
	StartedAt      time.Time
	EndedAt        time.Time
	Status         Status
	Favorite       bool
	ReconnectCount int
	Reason         Reason
	Truncated      bool
	CatalogID      string
	Port           uint16
	ShellName      string
	TargetKind     TargetKind
}

// Event is one step of a record's timeline.
type Event struct {
	RecordID string
	Kind     EventKind
	At       time.Time
	Attempt  int
	Reason   Reason
}

// Cursor is the last returned composite key, bound to its sort order.
type Cursor struct {
	StartedAt      time.Time
	ID             string
	Sort           SortOrder
	DurationMillis int64
	Ongoing        bool
}

type SortOrder uint8

const (
	SortStartedDesc SortOrder = iota
	SortStartedAsc
	SortDurationDesc
	SortDurationAsc
)

// Query selects records for List.
//
// Search is a substring match over the stored Host, Address and Account; %, _
// and \ are matched literally. Status, Protocol and Favorite filter when set.
// Since is inclusive and Until is exclusive, both on StartedAt. Limit clamps to
// 1..50 (0 means the default of 50), and Cursor continues a previous page.
type Query struct {
	Sort     SortOrder
	Outcome  OutcomeFilter
	Search   string
	Status   *Status
	Protocol *Protocol
	Favorite *bool
	Since    time.Time
	Until    time.Time
	Limit    int
	Cursor   Cursor
}

type OutcomeFilter uint8

const (
	OutcomeAll OutcomeFilter = iota
	OutcomeNormal
	OutcomeAbnormal
	OutcomeTrackingStopped
)

// Page contains one sorted page and consistent counts before cursor/limit.
// More says another page exists; Next continues the same sort and filters.
type Page struct {
	Matched int64
	Total   int64
	Records []Record
	Next    Cursor
	More    bool
}

// OperationKind is the write a Queue should perform.
type OperationKind uint8

const (
	OpStart OperationKind = iota + 1
	OpAppendEvent
	OpFinish
	OpSetFavorite
	OpDelete
	OpClear
	// OpWait orders writes after a previous window's journal queue when a tab moves.
	OpWait
	OpDeleteSelection
)

// Operation is one queued write. Only the fields of its Kind are read.
type Operation struct {
	Kind             OperationKind
	Record           Record
	Event            Event
	ID               string
	Status           Status
	EndedAt          time.Time
	Reason           Reason
	Favorite         bool
	IncludeFavorites bool
	Wait             <-chan struct{}
	IDs              []string
}

type ClearSelection struct {
	IDs       []string
	Favorites int
}

var (
	// ErrInvalidRecord means a caller supplied a record, event or enum value the
	// store refuses to persist (for example control characters in a snapshot, a
	// missing host, or an unknown protocol).
	ErrInvalidRecord = errors.New("sessionlog: invalid record")
	// ErrNotFound means the record does not exist (any more).
	ErrNotFound = errors.New("sessionlog: record not found")
	// ErrClosed means the store or queue was already closed.
	ErrClosed = errors.New("sessionlog: store is closed")
	// ErrQueueClosed means Submit was called after Queue.Close; the returned
	// acknowledgment carries this error.
	ErrQueueClosed = errors.New("sessionlog: queue is closed")
	// ErrQueueFull refuses a write rather than blocking the window event thread.
	ErrQueueFull = errors.New("sessionlog: queue is full")
	// ErrTooNew means the file was written by a newer build.
	ErrTooNew = errors.New("sessionlog: database is newer than this build")
)

const (
	// maxSnapshotBytes bounds the stored Host, Address and Account snapshots.
	maxSnapshotBytes = 255
	// maxKeyBytes bounds the random record and run identifiers.
	maxKeyBytes = 64
	// maxEventsPerRecord bounds a record's timeline; extra events are dropped
	// and Record.Truncated records that fact.
	maxEventsPerRecord = 128
	// maxUnfavoritedRecords is how many un-favorited rows retention keeps.
	maxUnfavoritedRecords = 500
	// defaultQueryLimit and maxQueryLimit bound one page.
	defaultQueryLimit = 50
	maxQueryLimit     = 50
	// unfavoritedRetention is how old an un-favorited row may get. Retention is
	// applied on every Start, relative to the new record's StartedAt.
	unfavoritedRetention = 90 * 24 * time.Hour
)

// NewID returns a random 128-bit identifier as 32 lowercase hex characters.
func NewID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("sessionlog: generate id: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// clampLimit applies the page bound to a Query.Limit.
func clampLimit(limit int) int {
	switch {
	case limit == 0:
		return defaultQueryLimit
	case limit < 1:
		return 1
	case limit > maxQueryLimit:
		return maxQueryLimit
	default:
		return limit
	}
}

// escapeLike makes a search term a literal LIKE pattern fragment, so %, _ and
// the escape character itself only match themselves.
func escapeLike(term string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(term)
}

// validateKey checks a stable identifier: present, bounded, valid UTF-8 and
// free of control characters.
func validateKey(field, value string) error {
	if value == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalidRecord, field)
	}
	if len(value) > maxKeyBytes {
		return fmt.Errorf("%w: %s is too long", ErrInvalidRecord, field)
	}
	return validateDisplayText(field, value)
}

// boundedDisplay validates a display snapshot and truncates it, on a rune
// boundary, to maxSnapshotBytes. Control characters, which is how terminal
// bytes and multi-line server text look, are refused instead of stored.
func boundedDisplay(field, value string) (string, error) {
	if err := validateDisplayText(field, value); err != nil {
		return "", err
	}
	if len(value) <= maxSnapshotBytes {
		return value, nil
	}
	cut := maxSnapshotBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut], nil
}

func validateDisplayText(field, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%w: %s is not valid UTF-8", ErrInvalidRecord, field)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: %s holds control characters", ErrInvalidRecord, field)
		}
	}
	return nil
}

// normalizeStart validates a Start record and returns the row to insert: the
// lifecycle fields are owned by the store.
func normalizeStart(record Record, now time.Time) (Record, error) {
	if err := validateKey("record id", record.ID); err != nil {
		return Record{}, err
	}
	if err := validateKey("run id", record.RunID); err != nil {
		return Record{}, err
	}
	host, err := boundedDisplay("host", record.Host)
	if err != nil {
		return Record{}, err
	}
	if host == "" {
		return Record{}, fmt.Errorf("%w: host is required", ErrInvalidRecord)
	}
	address, err := boundedDisplay("address", record.Address)
	if err != nil {
		return Record{}, err
	}
	account, err := boundedDisplay("account", record.Account)
	if err != nil {
		return Record{}, err
	}
	if err := validateDisplayText("catalog id", record.CatalogID); err != nil {
		return Record{}, err
	}
	if len(record.CatalogID) > maxSnapshotBytes || record.TargetKind > TargetShared {
		return Record{}, ErrInvalidRecord
	}
	shell, err := boundedDisplay("shell name", record.ShellName)
	if err != nil {
		return Record{}, err
	}
	if !record.Protocol.valid() {
		return Record{}, fmt.Errorf("%w: protocol %d", ErrInvalidRecord, record.Protocol)
	}
	started := record.StartedAt
	if started.IsZero() {
		started = now
	} else {
		started = started.UTC()
	}
	return Record{
		ID:         record.ID,
		RunID:      record.RunID,
		Host:       host,
		Address:    address,
		Account:    account,
		Protocol:   record.Protocol,
		StartedAt:  started,
		Status:     StatusConnecting,
		Favorite:   record.Favorite,
		CatalogID:  record.CatalogID,
		Port:       record.Port,
		ShellName:  shell,
		TargetKind: record.TargetKind,
	}, nil
}

// statusAfterEvent is the status an event moves a session to, if it moves it.
// A ready desktop transport or opened shell makes the session active; an automatic retry
// moves it back to reconnecting. A failed or disconnected transport does not
// decide the outcome on its own: Engine calls Finish with a terminal status.
func statusAfterEvent(kind EventKind, protocol Protocol) (Status, bool) {
	switch kind {
	case EventConnected:
		if protocol == ProtocolVNC || protocol == ProtocolRDP {
			return StatusActive, true
		}
		return 0, false
	case EventShellOpened:
		return StatusActive, true
	case EventReconnectScheduled, EventReconnectStarted:
		return StatusReconnecting, true
	default:
		return 0, false
	}
}

// reconnectEvent reports whether an event counts retries toward ReconnectCount.
func reconnectEvent(kind EventKind) bool {
	switch kind {
	case EventReconnectScheduled, EventReconnectStarted, EventReconnectExhausted, EventReconnectCanceled:
		return true
	default:
		return false
	}
}
