package store

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Outcomes of an audited call.
const (
	AuditOK      = "ok"      // the call ran and returned a result
	AuditFailed  = "failed"  // it ran and failed, or could not run
	AuditRefused = "refused" // the gate or the policy refused it
)

// MaxAuditSummaryBytes bounds the summary of one audited call.
const MaxAuditSummaryBytes = 512

// AuditEntry is one call an agent made through the stdio MCP server.
type AuditEntry struct {
	ID        int64
	At        time.Time
	HostID    string
	HostLabel string
	Tool      string
	// Summary is the command, or the path, the call was about; never its output.
	Summary string
	Outcome string
}

// RecordAudit appends one entry. The summary is cut at MaxAuditSummaryBytes on a
// character boundary.
func (s *Store) RecordAudit(entry AuditEntry) error {
	if entry.Tool == "" || entry.Outcome == "" {
		return fmt.Errorf("%w: audit entry needs a tool and an outcome", ErrInvalid)
	}
	summary := entry.Summary
	if len(summary) > MaxAuditSummaryBytes {
		cut := MaxAuditSummaryBytes
		for cut > 0 && !utf8.RuneStart(summary[cut]) {
			cut--
		}
		summary = summary[:cut]
	}
	summary = strings.ToValidUTF8(summary, "�")
	at := entry.At
	if at.IsZero() {
		at = time.Now()
	}
	_, err := s.db.Exec(`INSERT INTO agent_audit(at, host_id, host_label, tool, summary, outcome) VALUES(?,?,?,?,?,?)`,
		at.UnixNano(), entry.HostID, entry.HostLabel, entry.Tool, summary, entry.Outcome)
	return wrapDB(err)
}

// Audit returns the most recent entries, newest first. A limit of zero or less
// means DefaultListLimit, and it is capped at MaxListLimit.
func (s *Store) Audit(limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = DefaultListLimit
	}
	limit = min(limit, MaxListLimit)
	rows, err := s.db.Query(`SELECT id, at, host_id, host_label, tool, summary, outcome FROM agent_audit ORDER BY at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()
	var entries []AuditEntry
	for rows.Next() {
		var entry AuditEntry
		var at int64
		if err := rows.Scan(&entry.ID, &at, &entry.HostID, &entry.HostLabel, &entry.Tool, &entry.Summary, &entry.Outcome); err != nil {
			return nil, wrapDB(err)
		}
		entry.At = toTime(at)
		entries = append(entries, entry)
	}
	return entries, wrapDB(rows.Err())
}

// PruneAudit deletes the entries older than before and says how many went.
func (s *Store) PruneAudit(before time.Time) (int, error) {
	result, err := s.db.Exec(`DELETE FROM agent_audit WHERE at < ?`, before.UnixNano())
	if err != nil {
		return 0, wrapDB(err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// ClearAudit deletes every entry.
func (s *Store) ClearAudit() error {
	_, err := s.db.Exec(`DELETE FROM agent_audit`)
	return wrapDB(err)
}
