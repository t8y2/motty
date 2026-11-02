package sessionlog

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestStartIncludesInitialEvent(t *testing.T) {
	s, _ := openTestStore(t)
	r := sampleRecord("initial", testBase())
	startRecord(t, s, r)
	events, err := s.Events(r.ID)
	if err != nil || len(events) != 1 || events[0].Kind != EventKind(11) || !events[0].At.Equal(r.StartedAt) {
		t.Fatalf("initial event: %+v, %v", events, err)
	}
}

func TestDeleteSelectionPreservesNewRowsAndNewFavorites(t *testing.T) {
	s, _ := openTestStore(t)
	for _, id := range []string{"plain", "protected"} {
		startRecord(t, s, sampleRecord(id, testBase()))
	}
	selection, err := s.PreviewClear(false)
	if err != nil || len(selection.IDs) != 2 || selection.Favorites != 0 {
		t.Fatalf("preview = %+v, %v", selection, err)
	}
	startRecord(t, s, sampleRecord("new", testBase()))
	if err := s.SetFavorite("protected", true); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSelection(selection.IDs, false); err != nil {
		t.Fatal(err)
	}
	if recordExists(t, s, "plain") || !recordExists(t, s, "new") || !recordExists(t, s, "protected") {
		t.Fatal("cleanup deleted a new or newly favorited record")
	}
}

func TestDeleteSelectionIsAtomicAcrossBatches(t *testing.T) {
	s, _ := openTestStore(t)
	ids := make([]string, 205)
	for i := range ids {
		ids[i] = fmt.Sprintf("entry-%03d", i)
		startRecord(t, s, sampleRecord(ids[i], testBase()))
	}
	if _, err := s.db.Exec(`CREATE TRIGGER refuse_delete BEFORE DELETE ON session_logs WHEN OLD.id='entry-204' BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSelection(ids, true); err == nil {
		t.Fatal("cleanup ignored the failing second batch")
	}
	if countRows(t, s, "") != 205 {
		t.Fatal("cleanup committed the first batch despite a later failure")
	}
}

func TestQueueCopiesDeleteSelectionIDs(t *testing.T) {
	s, _ := openTestStore(t)
	for _, id := range []string{"delete", "keep"} {
		startRecord(t, s, sampleRecord(id, testBase()))
	}
	q := NewQueue(s)
	defer q.Close()
	gate := make(chan struct{})
	q.Submit(Operation{Kind: OpWait, Wait: gate})
	ids := []string{"delete"}
	ack := q.Submit(Operation{Kind: OpDeleteSelection, IDs: ids, IncludeFavorites: true})
	ids[0] = "keep"
	close(gate)
	if err := awaitAck(t, ack); err != nil {
		t.Fatal(err)
	}
	if recordExists(t, s, "delete") || !recordExists(t, s, "keep") {
		t.Fatal("queued cleanup followed a mutated selection")
	}
}

func TestFinishIncludesExactlyOneAtomicFinalEvent(t *testing.T) {
	s, _ := openTestStore(t)
	r := sampleRecord("final", testBase())
	startRecord(t, s, r)
	// A failed event insert must roll back the record update as well.
	if _, err := s.db.Exec(`CREATE TRIGGER refuse_final BEFORE INSERT ON session_log_events WHEN NEW.kind = 14 BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(r.ID, StatusEnded, r.StartedAt.Add(time.Minute), ReasonUserClosed); err == nil {
		t.Fatal("finish succeeded although its final event could not be written")
	}
	if got := mustList(t, s, Query{}).Records[0]; got.Status != StatusConnecting || !got.EndedAt.IsZero() {
		t.Fatalf("failed finish changed record: %+v", got)
	}
	if _, err := s.db.Exec(`DROP TRIGGER refuse_final`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Finish(r.ID, StatusEnded, r.StartedAt.Add(time.Minute), ReasonUserClosed); err != nil {
			t.Fatal(err)
		}
	}
	events, _ := s.Events(r.ID)
	if len(events) != 2 || events[1].Kind != EventKind(14) || events[1].Reason != ReasonUserClosed {
		t.Fatalf("final event is missing or duplicated: %+v", events)
	}
}

func TestMigrateV1ToV2PreservesRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schemaV1 + `PRAGMA user_version = 1; INSERT INTO session_logs(id,run_id,host,address,account,protocol,started_at,status,favorite) VALUES ('old','old-run','legacy','host','root',1,1000,4,1);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 2 {
		t.Fatalf("schema version=%d, %v", version, err)
	}
	var host, catalog string
	var favorite, port, target int
	if err := s.db.QueryRow(`SELECT host,favorite,catalog_id,port,target_kind FROM session_logs WHERE id='old'`).Scan(&host, &favorite, &catalog, &port, &target); err != nil || host != "legacy" || favorite != 1 || catalog != "" || port != 0 || target != 0 {
		t.Fatalf("legacy snapshot changed: %q %d %q %d %d, %v", host, favorite, catalog, port, target, err)
	}
}

func TestMigrationFailureRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// A partial, externally modified schema must not leave half a migration.
	if _, err := db.Exec(schemaV1 + `ALTER TABLE session_logs ADD COLUMN port INTEGER; PRAGMA user_version=1;`); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("invalid schema unexpectedly upgraded")
	}
	var version int
	db.QueryRow(`PRAGMA user_version`).Scan(&version)
	if version != 1 {
		t.Fatalf("failed migration changed version: %d", version)
	}
	if _, err := db.Exec(`SELECT catalog_id FROM session_logs`); err == nil {
		t.Fatal("failed migration left a newly added column")
	}
}

func TestFinalEventAtCapacity(t *testing.T) {
	s, _ := openTestStore(t)
	r := sampleRecord("capacity", testBase())
	startRecord(t, s, r)
	for range 150 {
		if err := s.AppendEvent(Event{RecordID: r.ID, Kind: EventConnected, At: r.StartedAt}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Finish(r.ID, StatusEnded, r.StartedAt.Add(time.Minute), ReasonUserClosed); err != nil {
		t.Fatal(err)
	}
	events, _ := s.Events(r.ID)
	if len(events) != 128 || events[127].Kind != EventKind(14) || !mustList(t, s, Query{}).Records[0].Truncated {
		t.Fatalf("bounded final event missing: count=%d last=%+v", len(events), events[len(events)-1])
	}
}

func TestRecoverAddsDetectionEventOnlyForPriorRun(t *testing.T) {
	s, _ := openTestStore(t)
	r := sampleRecord("recover", testBase())
	startRecord(t, s, r)
	if err := s.Recover("new-run", r.StartedAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.Recover("new-run", r.StartedAt.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	events, _ := s.Events(r.ID)
	if len(events) != 2 || events[1].Kind != EventKind(15) || events[1].Reason != ReasonProcessRestart || !events[1].At.Equal(r.StartedAt.Add(time.Hour)) {
		t.Fatalf("recovery did not retain first detection: %+v", events)
	}
}

func TestOutcomeFilterMatchesTerminalClasses(t *testing.T) {
	s, _ := openTestStore(t)
	for i, status := range []Status{StatusEnded, StatusFailed, StatusInterrupted, StatusTrackingStopped, StatusConnecting} {
		r := sampleRecord(string(rune('a'+i)), testBase())
		startRecord(t, s, r)
		if status != StatusConnecting {
			if err := s.Finish(r.ID, status, r.StartedAt.Add(time.Minute), ReasonUnknown); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		filter OutcomeFilter
		count  int
	}{{OutcomeAll, 5}, {OutcomeNormal, 1}, {OutcomeAbnormal, 2}, {OutcomeTrackingStopped, 1}} {
		if got := len(mustList(t, s, Query{Outcome: tc.filter}).Records); got != tc.count {
			t.Fatalf("filter %d returned %d records, want %d", tc.filter, got, tc.count)
		}
	}
	if _, err := s.List(Query{Outcome: OutcomeFilter(255)}); err == nil {
		t.Fatal("unknown outcome filter accepted")
	}
}

func TestNewMetadataIsBoundedAndSecretFree(t *testing.T) {
	s, _ := openTestStore(t)
	r := sampleRecord("metadata", testBase())
	r.CatalogID, r.Port, r.ShellName, r.TargetKind = "catalog-stable", 2222, "zsh", TargetSavedHost
	startRecord(t, s, r)
	got := mustList(t, s, Query{}).Records[0]
	if got.CatalogID != "catalog-stable" || got.Port != 2222 || got.ShellName != "zsh" || got.TargetKind != TargetSavedHost {
		t.Fatalf("metadata did not round-trip: %+v", got)
	}
	for i, invalid := range []Record{
		{ShellName: "shell\npassword=secret"}, {CatalogID: "catalog\x1bsecret"}, {TargetKind: TargetKind(255)},
	} {
		candidate := sampleRecord(fmt.Sprintf("invalid-%d", i), testBase())
		candidate.ShellName, candidate.CatalogID, candidate.TargetKind = invalid.ShellName, invalid.CatalogID, invalid.TargetKind
		if err := s.Start(candidate); !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("invalid metadata accepted: %v", err)
		}
	}
	if countRows(t, s, "") != 1 {
		t.Fatal("invalid metadata wrote records")
	}
}
