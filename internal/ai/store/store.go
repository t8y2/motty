// Package store persists the AI assistant's configuration and conversation
// history in a single sqlite file, using the pure-Go modernc.org/sqlite driver
// (no CGO).
//
// Security notes: provider API keys are not stored here any more. They live in
// the encrypted saved-passwords vault; the store only remembers whether a
// provider has one (Provider.HasKey). The legacy api_key column of schema v1
// is kept so old databases can be migrated, but nothing writes it, and
// LegacyKeys/ClearLegacyKey let the caller move and wipe what is left. The
// file and its parent directory are created with owner-only permissions
// (0600 / 0700). Keys are never included in error strings or in the fmt
// output of Provider.
//
// Journal mode: the store uses journal_mode=DELETE rather than WAL. The
// workload is tiny and single-process, DELETE leaves no -wal/-shm side files
// whose permissions would need separate care, and the rollback journal sqlite
// creates inherits the database file's mode.
package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Sentinel errors.
var (
	// ErrTooNew means the file was written by a newer schema version. The file
	// is left untouched.
	ErrTooNew = errors.New("ai store: database schema is newer than this build")
	// ErrCorrupt means the file is not a usable sqlite database. The file is
	// never modified or deleted.
	ErrCorrupt = errors.New("ai store: database is corrupt or not a sqlite database")
	// ErrNotFound means a referenced row does not exist.
	ErrNotFound = errors.New("ai store: not found")
	// ErrInvalid means an argument failed validation.
	ErrInvalid = errors.New("ai store: invalid argument")
	// ErrTooLarge is matched (errors.Is) by *LimitError.
	ErrTooLarge = errors.New("ai store: value too large")
	// ErrClosed means the store was closed.
	ErrClosed = errors.New("ai store: closed")
)

// Limits.
const (
	MaxTextBytes  = 16 << 20 // per message text, reasoning and tool JSON
	MaxTitleBytes = 256
	// DefaultListLimit applies when Conversations gets limit <= 0.
	DefaultListLimit = 200
	// MaxListLimit caps the number of conversations returned.
	MaxListLimit = 1000
)

// LimitError reports a value that exceeds a size bound.
type LimitError struct {
	Field string
	Size  int
	Max   int
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("ai store: %s is %d bytes, limit is %d", e.Field, e.Size, e.Max)
}

// Is makes errors.Is(err, ErrTooLarge) true.
func (e *LimitError) Is(target error) bool { return target == ErrTooLarge }

const schemaVersion = 8

var migrations = [...]string{
	// version 1
	`CREATE TABLE providers (
		id       TEXT PRIMARY KEY,
		name     TEXT NOT NULL,
		kind     TEXT NOT NULL,
		base_url TEXT NOT NULL,
		api_key  TEXT NOT NULL,
		enabled  INTEGER NOT NULL
	);
	CREATE TABLE models (
		provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
		name        TEXT NOT NULL,
		enabled     INTEGER NOT NULL,
		PRIMARY KEY (provider_id, name)
	);
	CREATE TABLE settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	CREATE TABLE conversations (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		title         TEXT NOT NULL,
		session_label TEXT NOT NULL,
		created       INTEGER NOT NULL,
		updated       INTEGER NOT NULL
	);
	CREATE INDEX conversations_updated ON conversations(updated DESC, id DESC);
	CREATE TABLE messages (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		conversation_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
		role            TEXT NOT NULL,
		text            TEXT NOT NULL,
		reasoning       TEXT NOT NULL,
		tool_json       BLOB,
		created         INTEGER NOT NULL
	);
	CREATE INDEX messages_conversation ON messages(conversation_id, id);`,
	// version 2: API keys move to the vault; has_key records that one exists.
	// The legacy api_key column stays until its value has been moved.
	`ALTER TABLE providers ADD COLUMN has_key INTEGER NOT NULL DEFAULT 0;
	UPDATE providers SET has_key = (api_key <> '');`,
	// Executable paths belong to this machine, not provider sync records.
	`ALTER TABLE providers ADD COLUMN cli_path TEXT NOT NULL DEFAULT '';`,
	// version 4: an Anthropic provider may send its key as a bearer token
	// instead of x-api-key. The empty value keeps the pre-existing behavior.
	`ALTER TABLE providers ADD COLUMN auth_style TEXT NOT NULL DEFAULT '';`,
	// version 5: what an agent did through the stdio MCP server, which runs no
	// window to show it in. A summary of each call, never its output.
	`CREATE TABLE agent_audit (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		at         INTEGER NOT NULL,
		host_id    TEXT NOT NULL,
		host_label TEXT NOT NULL,
		tool       TEXT NOT NULL,
		summary    TEXT NOT NULL,
		outcome    TEXT NOT NULL
	);
	CREATE INDEX agent_audit_at ON agent_audit(at DESC, id DESC);`,
	// version 6: an OpenAI-compatible provider may speak the Responses API
	// instead of Chat Completions. The empty value keeps Chat Completions.
	`ALTER TABLE providers ADD COLUMN api_format TEXT NOT NULL DEFAULT '';`,
	// version 7: an optional models_url for providers where model listing is at
	// a different path than completion requests.
	`ALTER TABLE providers ADD COLUMN models_url TEXT NOT NULL DEFAULT '';`,
	// version 8: reasoning effort configuration per model.
	`ALTER TABLE models ADD COLUMN reasoning_enabled INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE models ADD COLUMN reasoning_levels TEXT NOT NULL DEFAULT '';
	ALTER TABLE models ADD COLUMN reasoning_default TEXT NOT NULL DEFAULT '';`,
}

const sqliteMagic = "SQLite format 3\x00"

// Store is a handle to the sqlite file. It is safe for concurrent use.
type Store struct {
	db *sql.DB

	stAppend *sql.Stmt
	stBump   *sql.Stmt
	stMsgs   *sql.Stmt
}

// Open opens or creates the store at path, migrating it forward.
func Open(path string) (*Store, error) {
	if path == "" || strings.ContainsAny(path, "?#") {
		return nil, fmt.Errorf("%w: bad database path", ErrInvalid)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("ai store: create directory: %w", err)
	}
	if err := checkHeader(path); err != nil {
		return nil, err
	}
	// Ensure the file exists with 0600 before sqlite creates it.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("ai store: open file: %w", err)
	}
	f.Close()

	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(DELETE)&_pragma=secure_delete(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("ai store: open: %w", err)
	}
	// One connection serialises access; sqlite gives no parallel writers
	// anyway and this keeps pragmas per-connection state stable.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	s := &Store{db: db}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// checkHeader inspects the raw file header without modifying the file.
func checkHeader(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("ai store: open file: %w", err)
	}
	defer f.Close()
	hdr := make([]byte, 100)
	n, err := io.ReadFull(f, hdr)
	if n == 0 {
		return nil // empty file: a fresh database
	}
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("ai store: read header: %w", err)
	}
	if n < len(hdr) || !bytes.HasPrefix(hdr, []byte(sqliteMagic)) {
		return ErrCorrupt
	}
	if v := binary.BigEndian.Uint32(hdr[60:64]); v > schemaVersion {
		return ErrTooNew
	}
	return nil
}

func (s *Store) init() error {
	ctx := context.Background()
	var res string
	if err := s.db.QueryRowContext(ctx, `PRAGMA quick_check(1)`).Scan(&res); err != nil || res != "ok" {
		return ErrCorrupt
	}
	var ver int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&ver); err != nil {
		return ErrCorrupt
	}
	if ver > schemaVersion {
		return ErrTooNew
	}
	for v := ver; v < schemaVersion; v++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("ai store: migrate: %w", err)
		}
		if _, err := tx.ExecContext(ctx, migrations[v]); err != nil {
			tx.Rollback()
			return fmt.Errorf("ai store: migrate to v%d: %w", v+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", v+1)); err != nil {
			tx.Rollback()
			return fmt.Errorf("ai store: migrate to v%d: %w", v+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("ai store: migrate to v%d: %w", v+1, err)
		}
	}
	var err error
	if s.stAppend, err = s.db.PrepareContext(ctx,
		`INSERT INTO messages(conversation_id, role, text, reasoning, tool_json, created) VALUES(?,?,?,?,?,?)`); err != nil {
		return fmt.Errorf("ai store: prepare: %w", err)
	}
	if s.stBump, err = s.db.PrepareContext(ctx, `UPDATE conversations SET updated=? WHERE id=?`); err != nil {
		return fmt.Errorf("ai store: prepare: %w", err)
	}
	if s.stMsgs, err = s.db.PrepareContext(ctx,
		`SELECT id, role, text, reasoning, tool_json, created FROM messages WHERE conversation_id=? ORDER BY id`); err != nil {
		return fmt.Errorf("ai store: prepare: %w", err)
	}
	return nil
}

// Close releases the database. Subsequent calls fail.
func (s *Store) Close() error {
	for _, st := range []*sql.Stmt{s.stAppend, s.stBump, s.stMsgs} {
		if st != nil {
			st.Close()
		}
	}
	return s.db.Close()
}

func (s *Store) tx(fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return wrapDB(err)
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return wrapDB(tx.Commit())
}

func wrapDB(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if strings.Contains(err.Error(), "database is closed") {
		return ErrClosed
	}
	return fmt.Errorf("ai store: %w", err)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func toTime(ns int64) time.Time { return time.Unix(0, ns) }

// ---- providers ----

// Provider kinds.
const (
	KindOpenAI      = "openai"
	KindAnthropic   = "anthropic"
	KindClaudeCLI   = "claude-code-cli"
	KindCodexCLI    = "codex-cli"
	KindOpenCodeCLI = "opencode-cli"
)

// Authentication styles of an Anthropic provider. The empty value means the
// kind's historical default, which is an API key for Anthropic and a bearer
// token for OpenAI-compatible endpoints.
const (
	AuthStyleAPIKey = "api_key"
	AuthStyleBearer = "bearer"
)

// ValidAuthStyle reports whether style is one a provider may store.
func ValidAuthStyle(style string) bool {
	return style == "" || style == AuthStyleAPIKey || style == AuthStyleBearer
}

// API formats of an OpenAI-compatible provider. The empty value means Chat
// Completions, which nearly every compatible endpoint implements.
const (
	APIFormatChat      = "chat"
	APIFormatResponses = "responses"
)

// ValidAPIFormat reports whether format is one a provider may store.
func ValidAPIFormat(format string) bool {
	return format == "" || format == APIFormatChat || format == APIFormatResponses
}

func IsCLIKind(kind string) bool {
	return kind == KindClaudeCLI || kind == KindCodexCLI || kind == KindOpenCodeCLI
}

func ValidProviderKind(kind string) bool {
	return kind == KindOpenAI || kind == KindAnthropic || IsCLIKind(kind)
}

// Provider is a configured model endpoint. The API key is not persisted by
// the store: HasKey says a key exists in the vault, and APIKey is an in-memory
// field the caller fills from the vault (Providers always leaves it empty).
type Provider struct {
	ID        string
	Name      string
	Kind      string
	BaseURL   string
	ModelsURL string
	CLIPath   string // Machine-local; never included in provider sync records.
	APIKey    string
	HasKey    bool
	Enabled   bool
	// AuthStyle selects the Anthropic request header. Empty has the historical
	// meaning: x-api-key for Anthropic, bearer for OpenAI-compatible endpoints.
	AuthStyle string
	// APIFormat selects the OpenAI-compatible request protocol. Empty has the
	// historical meaning: Chat Completions. Anthropic providers have none.
	APIFormat string
}

func (p Provider) redacted() string {
	key := ""
	if p.APIKey != "" {
		key = "<redacted>"
	}
	return fmt.Sprintf("Provider{ID:%q Name:%q Kind:%q BaseURL:%q ModelsURL:%q AuthStyle:%q APIFormat:%q APIKey:%q HasKey:%t Enabled:%t}",
		p.ID, p.Name, p.Kind, p.BaseURL, p.ModelsURL, p.AuthStyle, p.APIFormat, key, p.HasKey, p.Enabled)
}

// String implements fmt.Stringer and hides the API key.
func (p Provider) String() string { return p.redacted() }

// GoString implements fmt.GoStringer and hides the API key.
func (p Provider) GoString() string { return p.redacted() }

// SaveProvider inserts or updates a provider by ID. It never writes the API
// key; a legacy plaintext key already in the row is left for ClearLegacyKey.
func (s *Store) SaveProvider(p Provider) error {
	if p.ID == "" || !ValidProviderKind(p.Kind) {
		return fmt.Errorf("%w: provider needs an id and a supported kind", ErrInvalid)
	}
	if IsCLIKind(p.Kind) && (p.BaseURL != "" || p.ModelsURL != "" || p.APIKey != "" || p.HasKey || p.AuthStyle != "" || p.APIFormat != "") {
		return fmt.Errorf("%w: CLI providers use their own authentication", ErrInvalid)
	}
	if !ValidAuthStyle(p.AuthStyle) {
		return fmt.Errorf("%w: unsupported authentication style", ErrInvalid)
	}
	if !ValidAPIFormat(p.APIFormat) {
		return fmt.Errorf("%w: unsupported API format", ErrInvalid)
	}
	// Only Anthropic has a choice of header. OpenAI-compatible requests always
	// use bearer authentication, so keeping a style there would be misleading.
	// The API format is the mirror image: only OpenAI-compatible providers
	// have one. The default is stored as the empty value, never as "chat", so
	// there is exactly one spelling of it.
	authStyle, apiFormat := p.AuthStyle, p.APIFormat
	if p.Kind != KindAnthropic {
		authStyle = ""
	}
	if p.Kind != KindOpenAI || apiFormat == APIFormatChat {
		apiFormat = ""
	}
	_, err := s.db.Exec(`INSERT INTO providers(id,name,kind,base_url,models_url,api_key,has_key,enabled,cli_path,auth_style,api_format) VALUES(?,?,?,?,?,'',?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, kind=excluded.kind,
		base_url=excluded.base_url, models_url=excluded.models_url, has_key=excluded.has_key, enabled=excluded.enabled,
		cli_path=excluded.cli_path, auth_style=excluded.auth_style, api_format=excluded.api_format`,
		p.ID, p.Name, p.Kind, p.BaseURL, p.ModelsURL, b2i(p.HasKey), b2i(p.Enabled), p.CLIPath, authStyle, apiFormat)
	return wrapDB(err)
}

// Providers lists all providers ordered by name then ID.
func (s *Store) Providers() ([]Provider, error) {
	rows, err := s.db.Query(`SELECT id,name,kind,base_url,models_url,has_key,enabled,cli_path,auth_style,api_format FROM providers ORDER BY name, id`)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()
	var out []Provider
	for rows.Next() {
		var p Provider
		var hk, en int
		if err := rows.Scan(&p.ID, &p.Name, &p.Kind, &p.BaseURL, &p.ModelsURL, &hk, &en, &p.CLIPath, &p.AuthStyle, &p.APIFormat); err != nil {
			return nil, wrapDB(err)
		}
		p.HasKey = hk != 0
		p.Enabled = en != 0
		out = append(out, p)
	}
	return out, wrapDB(rows.Err())
}

// LegacyKeys returns the plaintext API keys that schema v1 left in the
// database, by provider ID. Rows without one are omitted.
func (s *Store) LegacyKeys() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT id, api_key FROM providers WHERE api_key <> ''`)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			return nil, wrapDB(err)
		}
		out[id] = key
	}
	return out, wrapDB(rows.Err())
}

// ClearLegacyKey wipes the legacy plaintext key of one provider once it has
// been moved to the vault.
func (s *Store) ClearLegacyKey(id string) error {
	_, err := s.db.Exec(`UPDATE providers SET api_key='' WHERE id=?`, id)
	return wrapDB(err)
}

// DeleteProvider removes a provider and its models.
func (s *Store) DeleteProvider(id string) error {
	_, err := s.db.Exec(`DELETE FROM providers WHERE id=?`, id)
	return wrapDB(err)
}

// ---- models ----

// ReasoningConfig defines the reasoning effort levels supported by a model.
type ReasoningConfig struct {
	Levels  []string `json:"levels"`
	Default string   `json:"default"`
}

func (rc *ReasoningConfig) validate() error {
	if rc == nil {
		return nil
	}
	if len(rc.Levels) == 0 {
		return fmt.Errorf("%w: reasoning levels cannot be empty when enabled", ErrInvalid)
	}
	for _, l := range rc.Levels {
		if l == "" {
			return fmt.Errorf("%w: reasoning level cannot be empty", ErrInvalid)
		}
	}
	if rc.Default != "" && !slices.Contains(rc.Levels, rc.Default) {
		return fmt.Errorf("%w: default reasoning level %q not in levels", ErrInvalid, rc.Default)
	}
	return nil
}

// EffectiveLevel resolves the reasoning level given an optional user override.
func (rc *ReasoningConfig) EffectiveLevel(override string) (string, bool) {
	if rc == nil || len(rc.Levels) == 0 {
		return "", false
	}
	if override != "" && slices.Contains(rc.Levels, override) {
		return override, true
	}
	if rc.Default != "" {
		return rc.Default, true
	}
	return rc.Levels[0], true
}

func decodeReasoningConfig(levels, def string) (*ReasoningConfig, error) {
	if levels == "" {
		return nil, nil
	}
	parts := strings.Split(levels, ",")
	var cleaned []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	if len(cleaned) == 0 {
		return nil, nil
	}
	cfg := &ReasoningConfig{
		Levels:  cleaned,
		Default: strings.TrimSpace(def),
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Model is a model offered by a provider.
type Model struct {
	ProviderID        string
	Name              string
	Enabled           bool
	Reasoning         *ReasoningConfig
	ReasoningDisabled bool
}

// SaveModels replaces the model set of a provider.
func (s *Store) SaveModels(providerID string, models []Model) error {
	return s.tx(func(tx *sql.Tx) error {
		var one int
		if err := tx.QueryRow(`SELECT 1 FROM providers WHERE id=?`, providerID).Scan(&one); err != nil {
			return wrapDB(err)
		}
		// The reasoning declaration belongs to the user or cached configuration,
		// keep it for every model still in the new set if not explicitly passed.
		type oldReasoning struct {
			cfg      *ReasoningConfig
			disabled bool
		}
		old := map[string]oldReasoning{}
		rows, rerr := tx.Query(`SELECT name, reasoning_enabled, reasoning_levels, reasoning_default
			FROM models WHERE provider_id=? AND reasoning_enabled<>0`, providerID)
		if rerr != nil {
			return wrapDB(rerr)
		}
		for rows.Next() {
			var name, levels, def string
			var rEn int
			if err := rows.Scan(&name, &rEn, &levels, &def); err != nil {
				rows.Close()
				return wrapDB(err)
			}
			if rEn == -1 {
				old[name] = oldReasoning{disabled: true}
			} else if rEn == 1 {
				cfg, err := decodeReasoningConfig(levels, def)
				if err != nil {
					rows.Close()
					return err
				}
				old[name] = oldReasoning{cfg: cfg}
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return wrapDB(err)
		}

		if _, err := tx.Exec(`DELETE FROM models WHERE provider_id=?`, providerID); err != nil {
			return wrapDB(err)
		}
		for _, m := range models {
			if m.Name == "" {
				return fmt.Errorf("%w: empty model name", ErrInvalid)
			}
			if r, ok := old[m.Name]; ok && m.Reasoning == nil && !m.ReasoningDisabled {
				m.Reasoning = r.cfg
				m.ReasoningDisabled = r.disabled
			}
			reasoningEnabled, levels, def := 0, "", ""
			if m.ReasoningDisabled {
				reasoningEnabled = -1
			} else if m.Reasoning != nil {
				if err := m.Reasoning.validate(); err != nil {
					return err
				}
				reasoningEnabled, levels, def = 1, strings.Join(m.Reasoning.Levels, ","), m.Reasoning.Default
			}
			if _, err := tx.Exec(`INSERT OR REPLACE INTO models(provider_id,name,enabled,reasoning_enabled,reasoning_levels,reasoning_default) VALUES(?,?,?,?,?,?)`,
				providerID, m.Name, b2i(m.Enabled), reasoningEnabled, levels, def); err != nil {
				return wrapDB(err)
			}
		}
		return nil
	})
}

// Models lists all models ordered by provider then name.
func (s *Store) Models() ([]Model, error) {
	rows, err := s.db.Query(`SELECT provider_id,name,enabled,reasoning_enabled,reasoning_levels,reasoning_default FROM models ORDER BY provider_id, name`)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()
	var out []Model
	for rows.Next() {
		var m Model
		var en, rEn int
		var levels, def string
		if err := rows.Scan(&m.ProviderID, &m.Name, &en, &rEn, &levels, &def); err != nil {
			return nil, wrapDB(err)
		}
		m.Enabled = en != 0
		if rEn == 1 {
			m.Reasoning, _ = decodeReasoningConfig(levels, def)
		} else if rEn == -1 {
			m.ReasoningDisabled = true
		}
		out = append(out, m)
	}
	return out, wrapDB(rows.Err())
}

// SetModelEnabled toggles one model; ErrNotFound if it does not exist.
func (s *Store) SetModelEnabled(providerID, name string, enabled bool) error {
	res, err := s.db.Exec(`UPDATE models SET enabled=? WHERE provider_id=? AND name=?`, b2i(enabled), providerID, name)
	if err != nil {
		return wrapDB(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetModelReasoning updates reasoning configuration for one model; ErrNotFound if it does not exist.
// disabled=true forces the model not to use reasoning.
// disabled=false and cfg!=nil sets custom reasoning levels.
// disabled=false and cfg==nil resets the model to auto detection.
func (s *Store) SetModelReasoning(providerID, name string, cfg *ReasoningConfig, disabled bool) error {
	if !disabled && cfg != nil {
		if err := cfg.validate(); err != nil {
			return err
		}
	}
	enabled, levels, def := 0, "", ""
	if disabled {
		enabled = -1
	} else if cfg != nil {
		enabled = 1
		levels = strings.Join(cfg.Levels, ",")
		def = cfg.Default
	}
	res, err := s.db.Exec(`UPDATE models SET reasoning_enabled=?, reasoning_levels=?, reasoning_default=? WHERE provider_id=? AND name=?`,
		enabled, levels, def, providerID, name)
	if err != nil {
		return wrapDB(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const (
	keyDefaultProvider = "default_model.provider"
	keyDefaultModel    = "default_model.name"
)

// DefaultModel returns the default model selection, ok=false if unset.
func (s *Store) DefaultModel() (providerID, name string, ok bool, err error) {
	providerID, ok1, err := s.Setting(keyDefaultProvider)
	if err != nil {
		return "", "", false, err
	}
	name, ok2, err := s.Setting(keyDefaultModel)
	if err != nil {
		return "", "", false, err
	}
	if !ok1 || !ok2 {
		return "", "", false, nil
	}
	return providerID, name, true, nil
}

// SetDefaultModel records the default model atomically.
func (s *Store) SetDefaultModel(providerID, name string) error {
	return s.tx(func(tx *sql.Tx) error {
		for _, kv := range [][2]string{{keyDefaultProvider, providerID}, {keyDefaultModel, name}} {
			if _, err := tx.Exec(setSettingSQL, kv[0], kv[1]); err != nil {
				return wrapDB(err)
			}
		}
		return nil
	})
}

// ---- settings ----

const setSettingSQL = `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`

// Setting returns a setting value.
func (s *Store) Setting(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, wrapDB(err)
	}
	return v, true, nil
}

// SetSetting stores a setting value.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(setSettingSQL, key, value)
	return wrapDB(err)
}

// ---- conversations ----

// Conversation is a chat thread.
type Conversation struct {
	ID           int64
	Title        string
	SessionLabel string
	Created      time.Time
	Updated      time.Time
}

// StoredMessage is one persisted message. ToolJSON is opaque and stored
// verbatim.
type StoredMessage struct {
	ID        int64
	Role      string
	Text      string
	Reasoning string
	ToolJSON  []byte
	Created   time.Time
}

func checkTitle(t string) error {
	if len(t) > MaxTitleBytes {
		return &LimitError{Field: "title", Size: len(t), Max: MaxTitleBytes}
	}
	return nil
}

// CreateConversation creates an empty conversation.
func (s *Store) CreateConversation(title, sessionLabel string) (Conversation, error) {
	if err := checkTitle(title); err != nil {
		return Conversation{}, err
	}
	if err := checkTitle(sessionLabel); err != nil {
		return Conversation{}, err
	}
	now := time.Now()
	res, err := s.db.Exec(`INSERT INTO conversations(title,session_label,created,updated) VALUES(?,?,?,?)`,
		title, sessionLabel, now.UnixNano(), now.UnixNano())
	if err != nil {
		return Conversation{}, wrapDB(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Conversation{}, wrapDB(err)
	}
	return Conversation{ID: id, Title: title, SessionLabel: sessionLabel, Created: toTime(now.UnixNano()), Updated: toTime(now.UnixNano())}, nil
}

// RenameConversation changes a title without touching Updated.
func (s *Store) RenameConversation(id int64, title string) error {
	if err := checkTitle(title); err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE conversations SET title=? WHERE id=?`, title, id)
	if err != nil {
		return wrapDB(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteConversation removes a conversation and its messages.
func (s *Store) DeleteConversation(id int64) error {
	_, err := s.db.Exec(`DELETE FROM conversations WHERE id=?`, id)
	return wrapDB(err)
}

// DeleteAllConversations clears the whole history.
func (s *Store) DeleteAllConversations() error {
	_, err := s.db.Exec(`DELETE FROM conversations`)
	return wrapDB(err)
}

// escapeLike escapes %, _ and \ for use with ESCAPE '\'.
func escapeLike(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(q)
}

// Conversations lists conversations, most recently updated first. A non-empty
// query keeps those whose title or any message text contains it as a
// substring. Case folding is ASCII-only (sqlite LIKE). limit <= 0 means
// DefaultListLimit; it is capped at MaxListLimit.
func (s *Store) Conversations(query string, limit int) ([]Conversation, error) {
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if limit > MaxListLimit {
		limit = MaxListLimit
	}
	var rows *sql.Rows
	var err error
	if query == "" {
		rows, err = s.db.Query(`SELECT id,title,session_label,created,updated FROM conversations
			ORDER BY updated DESC, id DESC LIMIT ?`, limit)
	} else {
		pat := "%" + escapeLike(query) + "%"
		rows, err = s.db.Query(`SELECT id,title,session_label,created,updated FROM conversations c
			WHERE c.title LIKE ?1 ESCAPE '\' OR EXISTS (
				SELECT 1 FROM messages m WHERE m.conversation_id=c.id AND m.text LIKE ?1 ESCAPE '\')
			ORDER BY updated DESC, id DESC LIMIT ?2`, pat, limit)
	}
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()
	var out []Conversation
	for rows.Next() {
		var c Conversation
		var cr, up int64
		if err := rows.Scan(&c.ID, &c.Title, &c.SessionLabel, &cr, &up); err != nil {
			return nil, wrapDB(err)
		}
		c.Created, c.Updated = toTime(cr), toTime(up)
		out = append(out, c)
	}
	return out, wrapDB(rows.Err())
}

// Messages returns a conversation's messages in insertion order.
func (s *Store) Messages(conversationID int64) ([]StoredMessage, error) {
	rows, err := s.stMsgs.Query(conversationID)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()
	var out []StoredMessage
	for rows.Next() {
		var m StoredMessage
		var cr int64
		if err := rows.Scan(&m.ID, &m.Role, &m.Text, &m.Reasoning, &m.ToolJSON, &cr); err != nil {
			return nil, wrapDB(err)
		}
		m.Created = toTime(cr)
		out = append(out, m)
	}
	return out, wrapDB(rows.Err())
}

// AppendMessage adds a message and bumps the conversation's Updated time. The
// stored message (with ID and Created filled in) is returned.
func (s *Store) AppendMessage(conversationID int64, m StoredMessage) (StoredMessage, error) {
	for _, f := range []struct {
		name string
		n    int
	}{{"text", len(m.Text)}, {"reasoning", len(m.Reasoning)}, {"tool json", len(m.ToolJSON)}} {
		if f.n > MaxTextBytes {
			return StoredMessage{}, &LimitError{Field: f.name, Size: f.n, Max: MaxTextBytes}
		}
	}
	now := time.Now().UnixNano()
	err := s.tx(func(tx *sql.Tx) error {
		res, err := tx.Stmt(s.stBump).Exec(now, conversationID)
		if err != nil {
			return wrapDB(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		var tj any
		if m.ToolJSON != nil {
			tj = m.ToolJSON
		}
		res, err = tx.Stmt(s.stAppend).Exec(conversationID, m.Role, m.Text, m.Reasoning, tj, now)
		if err != nil {
			return wrapDB(err)
		}
		m.ID, err = res.LastInsertId()
		return wrapDB(err)
	})
	if err != nil {
		return StoredMessage{}, err
	}
	m.Created = toTime(now)
	return m, nil
}
