package sessionlog

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// testBase is a whole-millisecond UTC wall time near now, so records stay
// inside the retention window whenever the tests run.
func testBase() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private", "session-logs.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

func TestListRetentionUsesStoreClock(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	store, err := OpenWithClock(filepath.Join(t.TempDir(), "session-logs.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	record := sampleRecord("clock-record", now.Add(-40*24*time.Hour))
	startRecord(t, store, record)
	if err := store.Finish(record.ID, StatusEnded, now.Add(-39*24*time.Hour), ReasonUserClosed); err != nil {
		t.Fatal(err)
	}
	if page := mustList(t, store, Query{}); len(page.Records) != 1 {
		t.Fatalf("record within the injected clock's retention window was removed: %+v", page.Records)
	}
	now = now.Add(51 * 24 * time.Hour)
	if page := mustList(t, store, Query{}); len(page.Records) != 0 {
		t.Fatalf("expired record remains after the clock advances: %+v", page.Records)
	}
}

func sampleRecord(id string, started time.Time) Record {
	return Record{
		ID:        id,
		RunID:     "run-1",
		Host:      "host-" + id,
		Address:   "10.0.0.9:22",
		Account:   "alice",
		Protocol:  ProtocolSSH,
		StartedAt: started,
	}
}

func startRecord(t *testing.T, store *Store, record Record) {
	t.Helper()
	if err := store.Start(record); err != nil {
		t.Fatalf("Start(%q): %v", record.ID, err)
	}
}

func newUniqueID(t *testing.T) string {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	return id
}

func mustList(t *testing.T, store *Store, query Query) Page {
	t.Helper()
	page, err := store.List(query)
	if err != nil {
		t.Fatalf("List(%+v): %v", query, err)
	}
	return page
}

func findRecord(t *testing.T, store *Store, id string) Record {
	t.Helper()
	page := mustList(t, store, Query{})
	for _, record := range page.Records {
		if record.ID == id {
			return record
		}
	}
	t.Fatalf("record %q is not in the first page", id)
	return Record{}
}

func countRows(t *testing.T, store *Store, where string, args ...any) int {
	t.Helper()
	statement := `SELECT COUNT(*) FROM session_logs`
	if where != "" {
		statement += ` WHERE ` + where
	}
	var count int
	if err := store.db.QueryRow(statement, args...).Scan(&count); err != nil {
		t.Fatalf("count session_logs: %v", err)
	}
	return count
}

func recordExists(t *testing.T, store *Store, id string) bool {
	t.Helper()
	return countRows(t, store, `id = ?`, id) > 0
}

func eventCount(t *testing.T, store *Store, id string) int {
	t.Helper()
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM session_log_events WHERE record_id = ?`, id).Scan(&count); err != nil {
		t.Fatalf("count session_log_events: %v", err)
	}
	return count
}

// insertRawRecords writes rows without going through Start, so a test can build
// a large or aged fixture and then let one Start apply retention.
func insertRawRecords(t *testing.T, store *Store, records []Record) {
	t.Helper()
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	for _, record := range records {
		_, err := tx.Exec(`INSERT INTO session_logs
			(id, run_id, host, address, account, protocol, started_at, status, favorite)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			record.ID, record.RunID, record.Host, record.Address, record.Account,
			int(record.Protocol), record.StartedAt.UnixMilli(), int(record.Status), asInt(record.Favorite))
		if err != nil {
			t.Fatalf("insert raw %q: %v", record.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit raw rows: %v", err)
	}
}

func asInt(flag bool) int {
	if flag {
		return 1
	}
	return 0
}

func statusPtr(status Status) *Status         { return &status }
func protocolPtr(protocol Protocol) *Protocol { return &protocol }
func boolPtr(flag bool) *bool                 { return &flag }

func columnNames(t *testing.T, store *Store, table string) []string {
	t.Helper()
	rows, err := store.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, primaryKey int
		var fallback any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &fallback, &primaryKey); err != nil {
			t.Fatalf("table_info(%s): %v", table, err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	return names
}

func TestOpenCreatesPrivateVersionedDatabase(t *testing.T) {
	store, path := openTestStore(t)

	if runtime.GOOS != "windows" {
		dirInfo, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatalf("stat parent: %v", err)
		}
		if got := dirInfo.Mode().Perm(); got != 0o700 {
			t.Fatalf("parent dir mode = %o, want 0700", got)
		}
		fileInfo, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat database: %v", err)
		}
		if got := fileInfo.Mode().Perm(); got != 0o600 {
			t.Fatalf("database mode = %o, want 0600", got)
		}
	}

	var version int
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 2 {
		t.Fatalf("user_version = %d, want 2", version)
	}

	for _, table := range []string{"session_logs", "session_log_events"} {
		var name string
		err := store.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil || name != table {
			t.Fatalf("table %q: name = %q, err = %v", table, name, err)
		}
	}
	for _, index := range []string{"session_logs_started_id_idx", "session_logs_status_idx", "session_logs_favorite_idx"} {
		var name string
		err := store.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&name)
		if err != nil || name != index {
			t.Fatalf("index %q: name = %q, err = %v", index, name, err)
		}
	}

	var foreignKeys int
	if err := store.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, err = %v; want 1", foreignKeys, err)
	}
	var busyTimeout int
	if err := store.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil || busyTimeout != 5000 {
		t.Fatalf("busy_timeout = %d, err = %v; want 5000", busyTimeout, err)
	}
	var secureDelete int
	if err := store.db.QueryRow(`PRAGMA secure_delete`).Scan(&secureDelete); err != nil || secureDelete != 1 {
		t.Fatalf("secure_delete = %d, err = %v; want 1", secureDelete, err)
	}
}

func TestStartEventFinishRoundTrip(t *testing.T) {
	store, _ := openTestStore(t)
	base := testBase()
	id := newUniqueID(t)
	record := Record{
		ID: id, RunID: "run-a", Host: "db1.example", Address: "10.0.0.5:22",
		Account: "alice", Protocol: ProtocolSSH, StartedAt: base,
	}
	startRecord(t, store, record)

	page := mustList(t, store, Query{})
	if len(page.Records) != 1 {
		t.Fatalf("records after Start = %d, want 1", len(page.Records))
	}
	got := page.Records[0]
	if got.ID != id || got.RunID != "run-a" || got.Host != "db1.example" || got.Address != "10.0.0.5:22" || got.Account != "alice" {
		t.Fatalf("stored snapshots = %+v", got)
	}
	if got.Protocol != ProtocolSSH || got.Status != StatusConnecting || got.Favorite || got.Truncated {
		t.Fatalf("stored state = %+v", got)
	}
	if !got.StartedAt.Equal(base) || !got.EndedAt.IsZero() || got.Reason != ReasonNone || got.ReconnectCount != 0 {
		t.Fatalf("stored times/state = %+v", got)
	}

	for i, kind := range []EventKind{EventConnected, EventShellOpened, EventDisconnected} {
		event := Event{RecordID: id, Kind: kind, At: base.Add(time.Duration(i+1) * time.Second)}
		if err := store.AppendEvent(event); err != nil {
			t.Fatalf("AppendEvent(%v): %v", kind, err)
		}
	}
	if got := findRecord(t, store, id); got.Status != StatusActive {
		t.Fatalf("status after connected/shell events = %v, want active", got.Status)
	}

	ended := base.Add(time.Minute)
	if err := store.Finish(id, StatusEnded, ended, ReasonUserClosed); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	got = findRecord(t, store, id)
	if got.Status != StatusEnded || !got.EndedAt.Equal(ended) || got.Reason != ReasonUserClosed {
		t.Fatalf("finished record = %+v", got)
	}

	events, err := store.Events(id)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 5 || events[0].Kind != EventStarted || events[4].Kind != EventFinished {
		t.Fatalf("events do not include start and finish: %+v", events)
	}
	events = events[1:4]
	for i, kind := range []EventKind{EventConnected, EventShellOpened, EventDisconnected} {
		if events[i].RecordID != id || events[i].Kind != kind {
			t.Fatalf("event %d = %+v", i, events[i])
		}
		if want := base.Add(time.Duration(i+1) * time.Second); !events[i].At.Equal(want) {
			t.Fatalf("event %d at = %v, want %v", i, events[i].At, want)
		}
	}
}

func TestSameSecondDistinctIDs(t *testing.T) {
	store, _ := openTestStore(t)
	base := testBase()
	const count = 20
	ids := make([]string, 0, count)
	for i := 0; i < count; i++ {
		id := newUniqueID(t)
		if len(id) != 32 || strings.ToLower(id) != id {
			t.Fatalf("NewID() = %q, want 32 lowercase hex characters", id)
		}
		for _, r := range id {
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
				t.Fatalf("NewID() = %q, want lowercase hex", id)
			}
		}
		ids = append(ids, id)
		startRecord(t, store, sampleRecord(id, base))
	}
	seen := make(map[string]bool, count)
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate record ID %q", id)
		}
		seen[id] = true
	}
	page := mustList(t, store, Query{Limit: 50})
	if len(page.Records) != count {
		t.Fatalf("records = %d, want %d", len(page.Records), count)
	}
	// The start times tie, so the descending ID breaks the tie.
	for i := 1; i < len(page.Records); i++ {
		if page.Records[i-1].ID <= page.Records[i].ID {
			t.Fatalf("records are not strictly descending by ID at %d: %q then %q",
				i, page.Records[i-1].ID, page.Records[i].ID)
		}
	}
}

func TestKeysetPaginationIsStable(t *testing.T) {
	store, _ := openTestStore(t)
	base := testBase()
	type row struct {
		id      string
		started time.Time
	}
	rows := []row{
		{"r1", base},
		{"r2", base.Add(time.Second)},
		{"r3", base.Add(time.Second)},
		{"r4", base.Add(2 * time.Second)},
		{"r5", base.Add(2 * time.Second)},
		{"r6", base.Add(3 * time.Second)},
		{"r7", base.Add(3 * time.Second)},
	}
	for _, item := range rows {
		startRecord(t, store, sampleRecord(item.id, item.started))
	}
	expected := slices.Clone(rows)
	sort.Slice(expected, func(i, j int) bool {
		if !expected[i].started.Equal(expected[j].started) {
			return expected[i].started.After(expected[j].started)
		}
		return expected[i].id > expected[j].id
	})

	var got []string
	cursor := Cursor{}
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("pagination did not finish")
		}
		page := mustList(t, store, Query{Limit: 2, Cursor: cursor})
		for _, record := range page.Records {
			got = append(got, record.ID)
		}
		if !page.More {
			if page.Next.ID != "" || !page.Next.StartedAt.IsZero() {
				t.Fatalf("last page has a next cursor: %+v", page.Next)
			}
			break
		}
		if page.Next.ID == "" || page.Next.StartedAt.IsZero() {
			t.Fatalf("More with an empty Next cursor")
		}
		cursor = page.Next
		if pages == 0 {
			// A row inserted after the first page must not shift the cursor.
			extra := sampleRecord("r-new", base.Add(10*time.Second))
			startRecord(t, store, extra)
		}
	}
	if len(got) != len(expected) {
		t.Fatalf("paged IDs = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i].id {
			t.Fatalf("paged IDs = %v, want %v", got, expected)
		}
	}
}

func TestSearchEscapesLikeWildcards(t *testing.T) {
	store, _ := openTestStore(t)
	base := testBase()
	hosts := []string{"db%prod", "dbXprod", "db_prod", "dbZprod", `db\prod`}
	for i, host := range hosts {
		record := sampleRecord(fmt.Sprintf("h%d", i), base.Add(time.Duration(i)*time.Millisecond))
		record.Host = host
		startRecord(t, store, record)
	}
	extra := sampleRecord("ops", base.Add(10*time.Millisecond))
	extra.Host = "other.example"
	extra.Address = "10.1.2.3:22"
	extra.Account = "ops_user"
	startRecord(t, store, extra)

	cases := []struct {
		search string
		want   []string
	}{
		{"db%prod", []string{"db%prod"}},
		{"db_prod", []string{"db_prod"}},
		{`db\prod`, []string{`db\prod`}},
		{"dbXprod", []string{"dbXprod"}},
		{"ops_user", []string{"other.example"}},
		{"1.2.3", []string{"other.example"}},
	}
	for _, tc := range cases {
		page := mustList(t, store, Query{Search: tc.search, Limit: 50})
		got := make([]string, 0, len(page.Records))
		for _, record := range page.Records {
			got = append(got, record.Host)
		}
		slices.Sort(got)
		want := slices.Clone(tc.want)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("search %q matched %v, want %v", tc.search, got, want)
		}
	}

	// A bare wildcard is a literal character, not "everything".
	page := mustList(t, store, Query{Search: "%", Limit: 50})
	if len(page.Records) != 1 || page.Records[0].Host != "db%prod" {
		t.Fatalf("search %% matched %+v, want only db%%prod", page.Records)
	}
}

func TestListFilters(t *testing.T) {
	store, _ := openTestStore(t)
	base := testBase()

	endedID := "filter-ended"
	startRecord(t, store, sampleRecord(endedID, base))
	if err := store.Finish(endedID, StatusEnded, base.Add(time.Minute), ReasonUserClosed); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if err := store.SetFavorite(endedID, true); err != nil {
		t.Fatalf("SetFavorite: %v", err)
	}

	connectingID := "filter-connecting"
	connecting := sampleRecord(connectingID, base.Add(time.Hour))
	connecting.Protocol = ProtocolLocal
	startRecord(t, store, connecting)

	failedID := "filter-failed"
	failed := sampleRecord(failedID, base.Add(2*time.Hour))
	failed.Protocol = ProtocolTelnet
	startRecord(t, store, failed)
	if err := store.Finish(failedID, StatusFailed, base.Add(2*time.Hour+time.Minute), ReasonNetworkLost); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	ids := func(page Page) []string {
		var found []string
		for _, record := range page.Records {
			found = append(found, record.ID)
		}
		return found
	}

	page := mustList(t, store, Query{Status: statusPtr(StatusConnecting)})
	if got := ids(page); !slices.Equal(got, []string{connectingID}) {
		t.Fatalf("status filter = %v", got)
	}
	page = mustList(t, store, Query{Protocol: protocolPtr(ProtocolSSH)})
	if got := ids(page); !slices.Equal(got, []string{endedID}) {
		t.Fatalf("protocol filter = %v", got)
	}
	page = mustList(t, store, Query{Favorite: boolPtr(true)})
	if got := ids(page); !slices.Equal(got, []string{endedID}) {
		t.Fatalf("favorite true filter = %v", got)
	}
	page = mustList(t, store, Query{Favorite: boolPtr(false)})
	if got := ids(page); len(got) != 2 {
		t.Fatalf("favorite false filter = %v", got)
	}
	// Since is inclusive and Until is exclusive.
	page = mustList(t, store, Query{Since: base.Add(time.Hour)})
	if got := ids(page); len(got) != 2 {
		t.Fatalf("since filter = %v", got)
	}
	page = mustList(t, store, Query{Until: base.Add(2 * time.Hour)})
	if got := ids(page); len(got) != 2 {
		t.Fatalf("until filter = %v", got)
	}
	page = mustList(t, store, Query{Since: base.Add(time.Hour), Until: base.Add(2 * time.Hour)})
	if got := ids(page); !slices.Equal(got, []string{connectingID}) {
		t.Fatalf("since/until filter = %v", got)
	}
	page = mustList(t, store, Query{Search: "filter-", Status: statusPtr(StatusFailed)})
	if got := ids(page); !slices.Equal(got, []string{failedID}) {
		t.Fatalf("search + status filter = %v", got)
	}

	var invalidStatus = Status(200)
	if _, err := store.List(Query{Status: &invalidStatus}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("invalid status filter error = %v, want ErrInvalidRecord", err)
	}
	var invalidProtocol = Protocol(0)
	if _, err := store.List(Query{Protocol: &invalidProtocol}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("invalid protocol filter error = %v, want ErrInvalidRecord", err)
	}
}

func TestQueryLimitClamps(t *testing.T) {
	for input, want := range map[int]int{0: 50, -3: 1, 1: 1, 50: 50, 51: 50, 1000: 50} {
		if got := clampLimit(input); got != want {
			t.Fatalf("clampLimit(%d) = %d, want %d", input, got, want)
		}
	}

	store, _ := openTestStore(t)
	base := testBase()
	raw := make([]Record, 0, 55)
	for i := 0; i < 55; i++ {
		raw = append(raw, sampleRecord(fmt.Sprintf("limit-%02d", i), base.Add(time.Duration(i)*time.Millisecond)))
	}
	insertRawRecords(t, store, raw)

	page := mustList(t, store, Query{})
	if len(page.Records) != 50 || !page.More {
		t.Fatalf("default page = %d records, more = %v; want 50 and more", len(page.Records), page.More)
	}
	page = mustList(t, store, Query{Limit: 1000})
	if len(page.Records) != 50 || !page.More {
		t.Fatalf("clamped page = %d records, more = %v; want 50 and more", len(page.Records), page.More)
	}
	page = mustList(t, store, Query{Limit: -3})
	if len(page.Records) != 1 || !page.More {
		t.Fatalf("negative limit page = %d records, more = %v; want 1 and more", len(page.Records), page.More)
	}
}

func TestRetentionPreservesFavorites(t *testing.T) {
	store, _ := openTestStore(t)
	base := testBase()

	raw := make([]Record, 0, maxUnfavoritedRecords+2)
	ids := make([]string, 0, maxUnfavoritedRecords)
	for i := 0; i < maxUnfavoritedRecords+5; i++ {
		id := fmt.Sprintf("ret-%04d", i)
		ids = append(ids, id)
		record := sampleRecord(id, base.Add(time.Duration(i)*time.Millisecond))
		record.Status = StatusEnded
		raw = append(raw, record)
	}
	oldFavorite1 := sampleRecord("old-favorite-1", base.Add(-100*24*time.Hour))
	oldFavorite1.Favorite = true
	oldFavorite1.Status = StatusEnded
	oldFavorite2 := sampleRecord("old-favorite-2", base.Add(-200*24*time.Hour))
	oldFavorite2.Favorite = true
	oldFavorite2.Status = StatusEnded
	raw = append(raw, oldFavorite1, oldFavorite2)
	insertRawRecords(t, store, raw)

	// One insert applies retention: at most 500 un-favorited rows, oldest first.
	startRecord(t, store, sampleRecord("ret-trigger", base.Add(time.Hour)))
	if got := countRows(t, store, `favorite = 0 AND status = ?`, int(StatusEnded)); got != maxUnfavoritedRecords {
		t.Fatalf("finished un-favorited rows = %d, want %d", got, maxUnfavoritedRecords)
	}
	if got := countRows(t, store, `favorite = 1`); got != 2 {
		t.Fatalf("favorite rows = %d, want 2", got)
	}
	for _, id := range ids[:5] {
		if recordExists(t, store, id) {
			t.Fatalf("oldest un-favorited row %q survived retention", id)
		}
	}
	for _, id := range []string{ids[5], ids[len(ids)-1], "old-favorite-1", "old-favorite-2", "ret-trigger"} {
		if !recordExists(t, store, id) {
			t.Fatalf("retention dropped %q", id)
		}
	}

	// A row older than 90 days is pruned by the next insert.
	staleID := ids[10]
	if _, err := store.db.Exec(`UPDATE session_logs SET started_at = ? WHERE id = ?`,
		base.Add(-100*24*time.Hour).UnixMilli(), staleID); err != nil {
		t.Fatalf("age fixture: %v", err)
	}
	startRecord(t, store, sampleRecord("ret-trigger-2", base.Add(2*time.Hour)))
	if recordExists(t, store, staleID) {
		t.Fatalf("row older than 90 days survived retention")
	}
	if got := countRows(t, store, `favorite = 0 AND status = ?`, int(StatusEnded)); got != maxUnfavoritedRecords-1 {
		t.Fatalf("finished rows after age prune = %d, want %d", got, maxUnfavoritedRecords-1)
	}

	// Un-favoriting an expired record must not wait for another connection.
	if err := store.SetFavorite("old-favorite-1", false); err != nil {
		t.Fatalf("SetFavorite: %v", err)
	}
	mustList(t, store, Query{})
	if recordExists(t, store, "old-favorite-1") {
		t.Fatal("expired un-favorited row survived a read without another connection")
	}
}

func TestRetentionKeepsAnActiveSessionUntilItCanFinish(t *testing.T) {
	store, _ := openTestStore(t)
	base := testBase()
	active := sampleRecord("long-running-session", base.Add(-91*24*time.Hour))
	startRecord(t, store, active)
	for i := 0; i < maxUnfavoritedRecords+1; i++ {
		id := fmt.Sprintf("finished-%03d", i)
		started := base.Add(time.Duration(i) * time.Millisecond)
		startRecord(t, store, sampleRecord(id, started))
		if err := store.Finish(id, StatusEnded, started.Add(time.Second), ReasonUserClosed); err != nil {
			t.Fatal(err)
		}
	}
	if !recordExists(t, store, active.ID) {
		t.Fatal("retention deleted a live session before it could finish")
	}
	if err := store.Finish(active.ID, StatusEnded, base, ReasonUserClosed); err != nil {
		t.Fatalf("live session could not finish: %v", err)
	}
}

func TestRetentionExpiresEndedSessionsWithoutAnotherConnection(t *testing.T) {
	store, _ := openTestStore(t)
	base := testBase()
	id := "expired-ended-session"
	started := base.Add(-91 * 24 * time.Hour)
	startRecord(t, store, sampleRecord(id, started))
	if err := store.Finish(id, StatusEnded, started.Add(time.Minute), ReasonUserClosed); err != nil {
		t.Fatal(err)
	}
	if page := mustList(t, store, Query{}); len(page.Records) != 0 {
		t.Fatalf("expired session remains without a new connection: %+v", page.Records)
	}
}

func TestTransportConnectedIsNotYetAnOpenShell(t *testing.T) {
	store, _ := openTestStore(t)
	base := testBase()
	sshID := "pending-shell"
	startRecord(t, store, sampleRecord(sshID, base))
	if err := store.AppendEvent(Event{RecordID: sshID, Kind: EventConnected, At: base.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	if got := findRecord(t, store, sshID); got.Status != StatusConnecting {
		t.Fatalf("SSH transport alone became usable: %v", got.Status)
	}
	if err := store.AppendEvent(Event{RecordID: sshID, Kind: EventShellOpened, At: base.Add(2 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	if got := findRecord(t, store, sshID); got.Status != StatusActive {
		t.Fatalf("opened shell not active: %v", got.Status)
	}
	desktop := sampleRecord("desktop-ready", base)
	desktop.Protocol = ProtocolVNC
	startRecord(t, store, desktop)
	if err := store.AppendEvent(Event{RecordID: desktop.ID, Kind: EventConnected, At: base.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	if got := findRecord(t, store, desktop.ID); got.Status != StatusActive {
		t.Fatalf("desktop transport did not become active: %v", got.Status)
	}
}

func TestStoreOpensAHomePathContainingSQLiteURICharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home?#%", "session-logs.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open path with URI characters: %v", err)
	}
	id := newUniqueID(t)
	startRecord(t, store, sampleRecord(id, testBase()))
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen path with URI characters: %v", err)
	}
	defer reopened.Close()
	if got := findRecord(t, reopened, id); got.ID != id {
		t.Fatalf("reopened wrong database: %+v", got)
	}
}

func TestRecoverOnlyPriorRun(t *testing.T) {
	store, _ := openTestStore(t)
	base := testBase()
	at := base.Add(2 * time.Minute)

	currentActive := "current-active"
	currentActiveRecord := sampleRecord(currentActive, base)
	currentActiveRecord.RunID = "run-current"
	startRecord(t, store, currentActiveRecord)
	if err := store.AppendEvent(Event{RecordID: currentActive, Kind: EventConnected, At: base.Add(time.Second)}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := store.AppendEvent(Event{RecordID: currentActive, Kind: EventShellOpened, At: base.Add(2 * time.Second)}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	currentEnded := "current-ended"
	currentEndedRecord := sampleRecord(currentEnded, base.Add(time.Millisecond))
	currentEndedRecord.RunID = "run-current"
	startRecord(t, store, currentEndedRecord)
	if err := store.Finish(currentEnded, StatusEnded, base.Add(time.Minute), ReasonUserClosed); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	prior := func(id string, runID string) Record {
		record := sampleRecord(id, base.Add(2*time.Millisecond))
		record.RunID = runID
		return record
	}
	priorConnecting := prior("prior-connecting", "run-old")
	startRecord(t, store, priorConnecting)
	priorActive := prior("prior-active", "run-old")
	startRecord(t, store, priorActive)
	if err := store.AppendEvent(Event{RecordID: priorActive.ID, Kind: EventConnected, At: base.Add(time.Second)}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := store.AppendEvent(Event{RecordID: priorActive.ID, Kind: EventShellOpened, At: base.Add(2 * time.Second)}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	priorReconnecting := prior("prior-reconnecting", "run-old")
	startRecord(t, store, priorReconnecting)
	if err := store.AppendEvent(Event{RecordID: priorReconnecting.ID, Kind: EventReconnectScheduled, At: base.Add(time.Second), Attempt: 1}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	priorEnded := prior("prior-ended", "run-old")
	startRecord(t, store, priorEnded)
	if err := store.Finish(priorEnded.ID, StatusEnded, base.Add(time.Minute), ReasonUserClosed); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	if err := store.Recover("run-current", at); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	for _, id := range []string{priorConnecting.ID, priorActive.ID, priorReconnecting.ID} {
		got := findRecord(t, store, id)
		if got.Status != StatusInterrupted {
			t.Fatalf("%q status = %v, want interrupted", id, got.Status)
		}
		if !got.EndedAt.Equal(at) || got.Reason != ReasonProcessRestart {
			t.Fatalf("%q recovered = %+v, want ended at %v with process restart", id, got, at)
		}
	}
	for _, id := range []string{currentActive, currentEnded, priorEnded.ID} {
		got := findRecord(t, store, id)
		if got.Status == StatusInterrupted {
			t.Fatalf("%q was interrupted by Recover", id)
		}
	}
	if got := findRecord(t, store, currentActive); got.Status != StatusActive || !got.EndedAt.IsZero() {
		t.Fatalf("current run active row = %+v, want untouched active", got)
	}
	if got := findRecord(t, store, priorEnded.ID); got.Status != StatusEnded {
		t.Fatalf("prior ended row = %+v, want ended", got)
	}
}

func TestFinishIsIdempotent(t *testing.T) {
	store, _ := openTestStore(t)
	base := testBase()
	id := newUniqueID(t)
	startRecord(t, store, sampleRecord(id, base))

	firstEnd := base.Add(time.Minute)
	if err := store.Finish(id, StatusEnded, firstEnd, ReasonUserClosed); err != nil {
		t.Fatalf("first Finish: %v", err)
	}
	if err := store.Finish(id, StatusFailed, base.Add(2*time.Minute), ReasonNetworkLost); err != nil {
		t.Fatalf("second Finish: %v", err)
	}
	got := findRecord(t, store, id)
	if got.Status != StatusEnded || !got.EndedAt.Equal(firstEnd) || got.Reason != ReasonUserClosed {
		t.Fatalf("record after second Finish = %+v, want the first terminal result", got)
	}

	if err := store.Finish(id, StatusActive, base.Add(3*time.Minute), ReasonNone); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("Finish with a non-terminal status error = %v, want ErrInvalidRecord", err)
	}
	if err := store.Finish("missing-record", StatusEnded, base.Add(time.Minute), ReasonNone); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Finish of a missing record error = %v, want ErrNotFound", err)
	}

	// A late event cannot revive a terminal record.
	if err := store.AppendEvent(Event{RecordID: id, Kind: EventConnected, At: base.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("late AppendEvent: %v", err)
	}
	if got := findRecord(t, store, id); got.Status != StatusEnded {
		t.Fatalf("late event changed status to %v", got.Status)
	}
	events, err := store.Events(id)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 2 || events[0].Kind != EventStarted || events[1].Kind != EventFinished {
		t.Fatalf("late event was stored: %+v", events)
	}
}

func TestAppendEventUpdatesStatusAndReconnectCount(t *testing.T) {
	store, _ := openTestStore(t)
	base := testBase()
	id := newUniqueID(t)
	startRecord(t, store, sampleRecord(id, base))

	appendEvent := func(kind EventKind, attempt int) {
		t.Helper()
		if err := store.AppendEvent(Event{RecordID: id, Kind: kind, At: base.Add(time.Second), Attempt: attempt}); err != nil {
			t.Fatalf("AppendEvent(%v, %d): %v", kind, attempt, err)
		}
	}
	appendEvent(EventConnected, 0)
	if got := findRecord(t, store, id); got.Status != StatusConnecting {
		t.Fatalf("status after transport = %v, want connecting", got.Status)
	}
	appendEvent(EventShellOpened, 0)
	if got := findRecord(t, store, id); got.Status != StatusActive {
		t.Fatalf("status after shell opened = %v, want active", got.Status)
	}
	appendEvent(EventReconnectScheduled, 3)
	got := findRecord(t, store, id)
	if got.Status != StatusReconnecting || got.ReconnectCount != 3 {
		t.Fatalf("after reconnect scheduled = %+v, want reconnecting with 3 attempts", got)
	}
	appendEvent(EventReconnectStarted, 1)
	if got := findRecord(t, store, id); got.ReconnectCount != 3 {
		t.Fatalf("reconnect count = %d after a lower attempt, want 3", got.ReconnectCount)
	}
	appendEvent(EventConnected, 0)
	if got := findRecord(t, store, id); got.Status != StatusReconnecting {
		t.Fatalf("transport reconnected before shell = %v, want reconnecting", got.Status)
	}
	appendEvent(EventShellOpened, 0)
	if got := findRecord(t, store, id); got.Status != StatusActive {
		t.Fatalf("status after shell reopened = %v, want active", got.Status)
	}
	appendEvent(EventDisconnected, 9)
	if got := findRecord(t, store, id); got.ReconnectCount != 3 {
		t.Fatalf("non-reconnect event changed reconnect count to %d", got.ReconnectCount)
	}
}

func TestEventTruncationAt128(t *testing.T) {
	store, _ := openTestStore(t)
	base := testBase()
	id := newUniqueID(t)
	startRecord(t, store, sampleRecord(id, base))

	for i := 0; i < maxEventsPerRecord+2; i++ {
		event := Event{RecordID: id, Kind: EventDisconnected, At: base.Add(time.Duration(i+1) * time.Second)}
		if err := store.AppendEvent(event); err != nil {
			t.Fatalf("AppendEvent(%d): %v", i, err)
		}
	}
	events, err := store.Events(id)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != maxEventsPerRecord-1 {
		t.Fatalf("events = %d, want 127 with the final slot reserved", len(events))
	}
	for i := 1; i < len(events); i++ {
		if !events[i].At.After(events[i-1].At) {
			t.Fatalf("events not ascending at %d: %v then %v", i, events[i-1].At, events[i].At)
		}
	}
	got := findRecord(t, store, id)
	if !got.Truncated {
		t.Fatalf("record Truncated = false after %d events", len(events))
	}
}

func TestDeleteCascadesEvents(t *testing.T) {
	store, _ := openTestStore(t)
	base := testBase()
	id := newUniqueID(t)
	startRecord(t, store, sampleRecord(id, base))
	for i, kind := range []EventKind{EventConnected, EventDisconnected} {
		if err := store.AppendEvent(Event{RecordID: id, Kind: kind, At: base.Add(time.Duration(i+1) * time.Second)}); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}
	if err := store.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if recordExists(t, store, id) {
		t.Fatalf("record %q survived Delete", id)
	}
	if got := eventCount(t, store, id); got != 0 {
		t.Fatalf("events after Delete = %d, want 0 (cascade)", got)
	}
	events, err := store.Events(id)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("Events after Delete = %+v", events)
	}
	if err := store.Delete(id); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
}

func TestClearRespectsFavorites(t *testing.T) {
	store, path := openTestStore(t)
	base := testBase()
	favoriteID := newUniqueID(t)
	favoriteRecord := sampleRecord(favoriteID, base)
	favoriteRecord.Favorite = true
	startRecord(t, store, favoriteRecord)
	plainID := newUniqueID(t)
	startRecord(t, store, sampleRecord(plainID, base.Add(time.Millisecond)))
	for _, id := range []string{favoriteID, plainID} {
		if err := store.AppendEvent(Event{RecordID: id, Kind: EventConnected, At: base.Add(time.Second)}); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}

	if err := store.Clear(false); err != nil {
		t.Fatalf("Clear(false): %v", err)
	}
	if !recordExists(t, store, favoriteID) {
		t.Fatalf("Clear(false) removed a favorite")
	}
	if recordExists(t, store, plainID) {
		t.Fatalf("Clear(false) kept an un-favorited record")
	}
	if got := eventCount(t, store, plainID); got != 0 {
		t.Fatalf("Clear(false) kept %d events of a removed record", got)
	}
	if got := eventCount(t, store, favoriteID); got != 2 {
		t.Fatalf("Clear(false) touched the favorite's events: %d left", got)
	}

	if err := store.Clear(true); err != nil {
		t.Fatalf("Clear(true): %v", err)
	}
	if got := countRows(t, store, ""); got != 0 {
		t.Fatalf("records after Clear(true) = %d, want 0", got)
	}
	if got := eventCount(t, store, favoriteID); got != 0 {
		t.Fatalf("events after Clear(true) = %d, want 0", got)
	}
	// VACUUM must not lose the schema version or leave an unusable file.
	var version int
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 2 {
		t.Fatalf("user_version after Clear(true) = %d, err = %v; want 2", version, err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen after Clear(true): %v", err)
	}
	defer reopened.Close()
	if page := mustList(t, reopened, Query{}); len(page.Records) != 0 {
		t.Fatalf("records after reopen = %d, want 0", len(page.Records))
	}
}

func TestSetFavoriteReportsMissingRecord(t *testing.T) {
	store, _ := openTestStore(t)
	if err := store.SetFavorite("missing-record", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetFavorite of a missing record error = %v, want ErrNotFound", err)
	}
}

func TestDisplaySnapshotsAreBoundedRuneSafely(t *testing.T) {
	store, _ := openTestStore(t)
	base := testBase()
	id := newUniqueID(t)
	record := Record{
		ID:        id,
		RunID:     "run-1",
		Host:      strings.Repeat("é", 200),
		Address:   strings.Repeat("地", 100),
		Account:   strings.Repeat("a", 300),
		Protocol:  ProtocolSSH,
		StartedAt: base,
	}
	startRecord(t, store, record)
	got := findRecord(t, store, id)
	if len(got.Host) > maxSnapshotBytes || !utf8.ValidString(got.Host) {
		t.Fatalf("host = %q (%d bytes), want valid UTF-8 within %d bytes", got.Host, len(got.Host), maxSnapshotBytes)
	}
	if want := strings.Repeat("é", 127); got.Host != want {
		t.Fatalf("host = %q, want %q", got.Host, want)
	}
	if len(got.Address) > maxSnapshotBytes || !utf8.ValidString(got.Address) || got.Address != strings.Repeat("地", 85) {
		t.Fatalf("address = %q", got.Address)
	}
	if len(got.Account) > maxSnapshotBytes || got.Account != strings.Repeat("a", maxSnapshotBytes) {
		t.Fatalf("account length = %d, want %d", len(got.Account), maxSnapshotBytes)
	}
}

func TestNoTerminalOrAuthPayloadInStoredRows(t *testing.T) {
	store, path := openTestStore(t)
	base := testBase()

	rejected := []struct {
		name   string
		record Record
	}{
		{"escape", Record{ID: newUniqueID(t), RunID: "run-1", Host: "db.example\x1b[2J", Protocol: ProtocolSSH, StartedAt: base}},
		{"newline", Record{ID: newUniqueID(t), RunID: "run-1", Host: "db.example\n10.0.0.1", Protocol: ProtocolSSH, StartedAt: base}},
		{"carriage return", Record{ID: newUniqueID(t), RunID: "run-1", Host: "db.example\r", Protocol: ProtocolSSH, StartedAt: base}},
		{"tab", Record{ID: newUniqueID(t), RunID: "run-1", Host: "db.example", Account: "ali\tce", Protocol: ProtocolSSH, StartedAt: base}},
		{"nul", Record{ID: newUniqueID(t), RunID: "run-1", Host: "db.example", Address: "10.0.0.1\x00", Protocol: ProtocolSSH, StartedAt: base}},
		{"delete", Record{ID: newUniqueID(t), RunID: "run-1", Host: "db.example\x7f", Protocol: ProtocolSSH, StartedAt: base}},
		{"invalid utf-8", Record{ID: newUniqueID(t), RunID: "run-1", Host: string([]byte{0xff, 0xfe}), Protocol: ProtocolSSH, StartedAt: base}},
		{"auth answer", Record{ID: newUniqueID(t), RunID: "run-1", Host: "db.example", Account: "hunter2\x1b[?25h", Protocol: ProtocolSSH, StartedAt: base}},
	}
	for _, tc := range rejected {
		if err := store.Start(tc.record); !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("%s: Start error = %v, want ErrInvalidRecord", tc.name, err)
		}
	}
	if got := countRows(t, store, ""); got != 0 {
		t.Fatalf("rejected records wrote %d rows", got)
	}

	wantLogColumns := []string{
		"id", "run_id", "host", "address", "account", "protocol", "started_at",
		"ended_at", "status", "favorite", "reconnect_count", "reason", "truncated",
		"catalog_id", "port", "shell_name", "target_kind",
	}
	if got := columnNames(t, store, "session_logs"); !slices.Equal(got, wantLogColumns) {
		t.Fatalf("session_logs columns = %v, want %v (no arbitrary error/payload field)", got, wantLogColumns)
	}
	wantEventColumns := []string{"record_id", "seq", "kind", "at", "attempt", "reason"}
	if got := columnNames(t, store, "session_log_events"); !slices.Equal(got, wantEventColumns) {
		t.Fatalf("session_log_events columns = %v, want %v", got, wantEventColumns)
	}

	id := newUniqueID(t)
	startRecord(t, store, Record{
		ID: id, RunID: "run-1", Host: "db.example", Address: "10.0.0.1:22",
		Account: "alice", Protocol: ProtocolSSH, StartedAt: base,
	})
	if err := store.AppendEvent(Event{RecordID: id, Kind: EventConnected, At: base.Add(time.Second)}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := store.Finish(id, StatusEnded, base.Add(time.Minute), ReasonUserClosed); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	rows, err := store.db.Query(`SELECT id, run_id, host, address, account FROM session_logs`)
	if err != nil {
		t.Fatalf("scan stored rows: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, runID, host, address, account string
		if err := rows.Scan(&id, &runID, &host, &address, &account); err != nil {
			t.Fatalf("scan stored row: %v", err)
		}
		for _, text := range []string{id, runID, host, address, account} {
			if strings.ContainsAny(text, "\x00\x1b\n\r\t\x7f") {
				t.Fatalf("stored text holds control bytes: %q", text)
			}
			if strings.Contains(text, "hunter2") {
				t.Fatalf("stored text holds a rejected auth answer: %q", text)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("scan stored rows: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read database file: %v", err)
	}
	for _, marker := range []string{"hunter2", "\x1b[2J", "\x1b[?25h"} {
		if bytes.Contains(data, []byte(marker)) {
			t.Fatalf("database file holds rejected payload %q", marker)
		}
	}
}

func TestReadOnlyStoreReportsError(t *testing.T) {
	store, path := openTestStore(t)
	startRecord(t, store, sampleRecord(newUniqueID(t), testBase()))
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatalf("make database read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if reopened, err := Open(path); err == nil {
		_ = reopened.Close()
		t.Fatal("Open of a read-only database succeeded, want an error")
	}
}

func TestOpenBrokenDatabaseReportsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken", "session-logs.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("this is not a database ", 40)), 0o600); err != nil {
		t.Fatalf("write broken database: %v", err)
	}
	if store, err := Open(path); err == nil {
		_ = store.Close()
		t.Fatal("Open of a broken database succeeded, want an error")
	}
}

func TestClosedStoreReportsError(t *testing.T) {
	store, _ := openTestStore(t)
	id := newUniqueID(t)
	startRecord(t, store, sampleRecord(id, testBase()))
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := store.List(Query{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("List after Close error = %v, want ErrClosed", err)
	}
	if _, err := store.Events(id); !errors.Is(err, ErrClosed) {
		t.Fatalf("Events after Close error = %v, want ErrClosed", err)
	}
	if err := store.Start(sampleRecord(newUniqueID(t), testBase())); !errors.Is(err, ErrClosed) {
		t.Fatalf("Start after Close error = %v, want ErrClosed", err)
	}
	if err := store.AppendEvent(Event{RecordID: id, Kind: EventConnected}); !errors.Is(err, ErrClosed) {
		t.Fatalf("AppendEvent after Close error = %v, want ErrClosed", err)
	}
	if err := store.Finish(id, StatusEnded, time.Now(), ReasonNone); !errors.Is(err, ErrClosed) {
		t.Fatalf("Finish after Close error = %v, want ErrClosed", err)
	}
	if err := store.SetFavorite(id, true); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetFavorite after Close error = %v, want ErrClosed", err)
	}
	if err := store.Delete(id); !errors.Is(err, ErrClosed) {
		t.Fatalf("Delete after Close error = %v, want ErrClosed", err)
	}
	if err := store.Clear(true); !errors.Is(err, ErrClosed) {
		t.Fatalf("Clear after Close error = %v, want ErrClosed", err)
	}
	if err := store.Recover("run-1", time.Now()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Recover after Close error = %v, want ErrClosed", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestStoreIsSafeForConcurrentUse(t *testing.T) {
	store, _ := openTestStore(t)
	base := testBase()
	const goroutines = 8
	const perGoroutine = 5
	errs := make(chan error, goroutines*perGoroutine)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				id := fmt.Sprintf("concurrent-%02d-%d", g, i)
				started := base.Add(time.Duration(g*perGoroutine+i) * time.Millisecond)
				errs <- store.Start(sampleRecord(id, started))
				if _, err := store.List(Query{Limit: 10}); err != nil {
					errs <- err
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent store use: %v", err)
		}
	}
	if got := countRows(t, store, ""); got != goroutines*perGoroutine {
		t.Fatalf("records = %d, want %d", got, goroutines*perGoroutine)
	}
}

// The enums are written to the database as their numeric values, so the value
// of every constant is part of the on-disk contract.
func TestEnumValuesAreStable(t *testing.T) {
	statuses := []Status{
		StatusConnecting, StatusActive, StatusReconnecting, StatusEnded,
		StatusFailed, StatusInterrupted, StatusTrackingStopped,
	}
	for i, status := range statuses {
		if want := Status(i + 1); status != want {
			t.Fatalf("status %d = %d, want %d", i, status, want)
		}
		if !status.valid() {
			t.Fatalf("status %d is not valid", status)
		}
	}
	protocols := []Protocol{
		ProtocolSSH, ProtocolLocal, ProtocolTelnet, ProtocolSerial, ProtocolVNC, ProtocolRDP,
	}
	for i, protocol := range protocols {
		if want := Protocol(i + 1); protocol != want {
			t.Fatalf("protocol %d = %d, want %d", i, protocol, want)
		}
		if !protocol.valid() {
			t.Fatalf("protocol %d is not valid", protocol)
		}
	}
	reasons := []Reason{
		ReasonNone, ReasonUserClosed, ReasonConnectFailed, ReasonAuthFailed,
		ReasonHostKeyRejected, ReasonCanceled, ReasonTimeout, ReasonNetworkLost,
		ReasonShellFailed, ReasonTrackingOff, ReasonProcessRestart, ReasonUnknown,
	}
	for i, reason := range reasons {
		if want := Reason(i); reason != want {
			t.Fatalf("reason %d = %d, want %d", i, reason, want)
		}
		if !reason.valid() {
			t.Fatalf("reason %d is not valid", reason)
		}
	}
	kinds := []EventKind{
		EventConnected, EventShellOpened, EventShellFailed, EventDisconnected,
		EventFailed, EventReconnectScheduled, EventReconnectStarted,
		EventReconnectExhausted, EventReconnectCanceled, EventTrackingStopped,
	}
	for i, kind := range kinds {
		if want := EventKind(i + 1); kind != want {
			t.Fatalf("event kind %d = %d, want %d", i, kind, want)
		}
		if !kind.valid() {
			t.Fatalf("event kind %d is not valid", kind)
		}
	}
	operations := []OperationKind{OpStart, OpAppendEvent, OpFinish, OpSetFavorite, OpDelete, OpClear}
	for i, operation := range operations {
		if want := OperationKind(i + 1); operation != want {
			t.Fatalf("operation kind %d = %d, want %d", i, operation, want)
		}
	}
}

func TestStatsReportsTheJournalFootprint(t *testing.T) {
	store, _ := openTestStore(t)
	empty, err := store.Stats()
	if err != nil {
		t.Fatalf("Stats on a new journal: %v", err)
	}
	if empty.Records != 0 || empty.Favorites != 0 || empty.Events != 0 || empty.Bytes <= 0 {
		t.Fatalf("empty stats = %+v", empty)
	}
	base := testBase()
	favorite := sampleRecord("stats-favorite", base)
	favorite.Favorite = true
	startRecord(t, store, favorite)
	startRecord(t, store, sampleRecord("stats-plain", base.Add(time.Minute)))
	for _, id := range []string{"stats-favorite", "stats-plain"} {
		if err := store.AppendEvent(Event{RecordID: id, Kind: EventConnected, At: base}); err != nil {
			t.Fatalf("event for %s: %v", id, err)
		}
	}
	stats, err := store.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Records != 2 || stats.Favorites != 1 || stats.Events != 4 || stats.Bytes < empty.Bytes {
		t.Fatalf("stats = %+v (empty %+v)", stats, empty)
	}
}

func TestOpenTightensAnExistingFilesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-logs.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open existing: %v", err)
	}
	defer reopened.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("existing file mode = %v, want 0600", info.Mode().Perm())
	}
}
