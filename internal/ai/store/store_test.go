package store

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func openTemp(t testing.TB) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sub", "ai.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func TestFileModes(t *testing.T) {
	s, path := openTemp(t)
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	if _, err := s.CreateConversation("x", ""); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v err %v", fi.Mode(), err)
	}
	di, _ := os.Stat(filepath.Dir(path))
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", di.Mode())
	}
}

func TestProvidersModelsSettings(t *testing.T) {
	s, _ := openTemp(t)
	p := Provider{ID: "p1", Name: "A", Kind: KindOpenAI, BaseURL: "https://x", HasKey: true, Enabled: true}
	if err := s.SaveProvider(p); err != nil {
		t.Fatal(err)
	}
	p.Name = "A2"
	p.Enabled = false
	p.ModelsURL = "https://models.example.com/v1"
	if err := s.SaveProvider(p); err != nil {
		t.Fatal(err)
	}
	got, err := s.Providers()
	if err != nil || len(got) != 1 || got[0] != p {
		t.Fatalf("%v %v", got, err)
	}
	if err := s.SaveProvider(Provider{ID: "bad", Kind: "nope"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("kind: %v", err)
	}
	if err := s.SaveModels("p1", []Model{{ProviderID: "p1", Name: "m1", Enabled: true}, {ProviderID: "p1", Name: "m2", Enabled: false}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetModelEnabled("p1", "m2", true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetModelEnabled("p1", "zz", true); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	ms, _ := s.Models()
	if len(ms) != 2 || !ms[1].Enabled {
		t.Fatalf("%v", ms)
	}
	if err := s.SaveModels("p1", []Model{{ProviderID: "p1", Name: "m3", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if ms, _ = s.Models(); len(ms) != 1 || ms[0].Name != "m3" {
		t.Fatalf("replace: %v", ms)
	}
	if err := s.SaveModels("ghost", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ghost: %v", err)
	}
	if _, _, ok, _ := s.DefaultModel(); ok {
		t.Fatal("default should be unset")
	}
	if err := s.SetDefaultModel("p1", "m3"); err != nil {
		t.Fatal(err)
	}
	if pid, n, ok, _ := s.DefaultModel(); !ok || pid != "p1" || n != "m3" {
		t.Fatal(pid, n, ok)
	}
	if _, ok, _ := s.Setting("k"); ok {
		t.Fatal("k set")
	}
	s.SetSetting("k", "v1")
	s.SetSetting("k", "v2")
	if v, ok, _ := s.Setting("k"); !ok || v != "v2" {
		t.Fatal(v)
	}
	// cascade
	if err := s.DeleteProvider("p1"); err != nil {
		t.Fatal(err)
	}
	if ms, _ = s.Models(); len(ms) != 0 {
		t.Fatalf("models survived provider delete: %v", ms)
	}
}

func TestProviderAuthStyleRoundTrip(t *testing.T) {
	s, _ := openTemp(t)
	want := Provider{ID: "p1", Name: "Relay", Kind: KindAnthropic, BaseURL: "https://relay.example.com", AuthStyle: AuthStyleBearer, HasKey: true, Enabled: true}
	if err := s.SaveProvider(want); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProvider(Provider{ID: "p2", Name: "OpenAI", Kind: KindOpenAI, BaseURL: "https://api.example.com/v1", AuthStyle: AuthStyleBearer, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProvider(Provider{ID: "bad", Kind: KindAnthropic, AuthStyle: "basic"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown style: %v", err)
	}
	got, err := s.Providers()
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	byID := map[string]Provider{}
	for _, provider := range got {
		byID[provider.ID] = provider
	}
	if byID["p1"].AuthStyle != AuthStyleBearer {
		t.Fatalf("Anthropic style = %q, want %q", byID["p1"].AuthStyle, AuthStyleBearer)
	}
	// An OpenAI-compatible provider has no header choice: bearer is inherent.
	if byID["p2"].AuthStyle != "" {
		t.Fatalf("OpenAI style = %q, want empty", byID["p2"].AuthStyle)
	}
}

func TestProviderAPIFormatRoundTrip(t *testing.T) {
	s, _ := openTemp(t)
	for _, provider := range []Provider{
		{ID: "responses", Name: "A", Kind: KindOpenAI, BaseURL: "https://gw.example.com/v1", APIFormat: APIFormatResponses, Enabled: true},
		{ID: "chat", Name: "B", Kind: KindOpenAI, APIFormat: APIFormatChat, Enabled: true},
		{ID: "claude", Name: "C", Kind: KindAnthropic, APIFormat: APIFormatResponses, Enabled: true},
	} {
		if err := s.SaveProvider(provider); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveProvider(Provider{ID: "bad", Kind: KindOpenAI, APIFormat: "completions"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown format: %v", err)
	}
	if err := s.SaveProvider(Provider{ID: "cli", Kind: KindCodexCLI, APIFormat: APIFormatResponses}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("CLI provider with a format: %v", err)
	}
	got, err := s.Providers()
	if err != nil || len(got) != 3 {
		t.Fatalf("%v %v", got, err)
	}
	formats := map[string]string{}
	for _, provider := range got {
		formats[provider.ID] = provider.APIFormat
	}
	// Chat Completions is stored as the empty value and Anthropic has no format.
	if formats["responses"] != APIFormatResponses || formats["chat"] != "" || formats["claude"] != "" {
		t.Fatalf("formats = %v", formats)
	}
	// Switching back to Chat Completions overwrites the stored format.
	if err := s.SaveProvider(Provider{ID: "responses", Name: "A", Kind: KindOpenAI, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Providers(); got[0].ID != "responses" || got[0].APIFormat != "" {
		t.Fatalf("after switching back: %#v", got[0])
	}
}

// A database written before bearer authentication existed must migrate without
// losing providers, and every existing row keeps the historical x-api-key
// behavior.
func TestVersion2DatabaseMigratesWithTheDefaultAuthStyle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if _, err := raw.Exec(migrations[index]); err != nil {
			t.Fatalf("create v%d: %v", index+1, err)
		}
	}
	if _, err := raw.Exec(`PRAGMA user_version = 2`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO providers(id,name,kind,base_url,api_key,enabled,has_key) VALUES('old','Old','anthropic','https://old.example.com','',1,0)`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("version = %d, err %v, want %d", version, err, schemaVersion)
	}
	got, err := s.Providers()
	if err != nil || len(got) != 1 {
		t.Fatalf("providers = %v, err %v", got, err)
	}
	if got[0].ID != "old" || got[0].AuthStyle != "" || got[0].APIFormat != "" {
		t.Fatalf("migrated provider = %#v", got[0])
	}
}

func TestKeyRedaction(t *testing.T) {
	const key = "sk-very-secret"
	p := Provider{ID: "p", Kind: KindOpenAI, APIKey: key}
	for _, out := range []string{
		fmt.Sprint(p), fmt.Sprintf("%v", p), fmt.Sprintf("%+v", p), fmt.Sprintf("%#v", p), fmt.Sprintf("%s", p),
		fmt.Sprintf("%v", &p), fmt.Sprintf("%v", []Provider{p}),
	} {
		if strings.Contains(out, key) {
			t.Fatalf("key leaked: %s", out)
		}
	}
	s, _ := openTemp(t)
	// error paths with a key in the argument
	err := s.SaveProvider(Provider{ID: "", Kind: "bad", APIKey: key})
	if err == nil || strings.Contains(err.Error(), key) {
		t.Fatalf("error %v", err)
	}
	s.Close()
	err = s.SaveProvider(Provider{ID: "p", Kind: KindOpenAI, APIKey: key})
	if err == nil || strings.Contains(err.Error(), key) {
		t.Fatalf("closed error %v", err)
	}
}

func TestConversationsRoundTrip(t *testing.T) {
	s, _ := openTemp(t)
	c1, _ := s.CreateConversation("first", "host-a")
	time.Sleep(time.Millisecond)
	c2, _ := s.CreateConversation("second", "")
	list, _ := s.Conversations("", 0)
	if len(list) != 2 || list[0].ID != c2.ID {
		t.Fatalf("order %v", list)
	}
	tj := []byte(`[{"id":"1","name":"run","args":{"a":"\u0000"}}]`)
	time.Sleep(time.Millisecond)
	m, err := s.AppendMessage(c1.ID, StoredMessage{Role: "assistant", Text: "héllo", Reasoning: "think", ToolJSON: tj})
	if err != nil || m.ID == 0 {
		t.Fatal(m, err)
	}
	s.AppendMessage(c1.ID, StoredMessage{Role: "user", Text: "second msg"})
	msgs, _ := s.Messages(c1.ID)
	if len(msgs) != 2 || msgs[0].Text != "héllo" || msgs[0].Reasoning != "think" ||
		!bytes.Equal(msgs[0].ToolJSON, tj) || msgs[1].ToolJSON != nil || !msgs[0].Created.Equal(m.Created) {
		t.Fatalf("%+v", msgs)
	}
	list, _ = s.Conversations("", 0)
	if list[0].ID != c1.ID || !list[0].Updated.After(c1.Updated) || list[0].SessionLabel != "host-a" {
		t.Fatalf("bump failed: %+v", list)
	}
	if err := s.RenameConversation(c1.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameConversation(999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.AppendMessage(999, StoredMessage{Role: "user"}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if list, _ = s.Conversations("", 1); len(list) != 1 {
		t.Fatal("limit")
	}
	if err := s.DeleteConversation(c1.ID); err != nil {
		t.Fatal(err)
	}
	if msgs, _ = s.Messages(c1.ID); len(msgs) != 0 {
		t.Fatal("messages survived")
	}
	s.AppendMessage(c2.ID, StoredMessage{Role: "user", Text: "x"})
	if err := s.DeleteAllConversations(); err != nil {
		t.Fatal(err)
	}
	if list, _ = s.Conversations("", 0); len(list) != 0 {
		t.Fatal("not cleared")
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM messages`).Scan(&n)
	if n != 0 {
		t.Fatal("orphan messages")
	}
}

func TestBounds(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.CreateConversation(strings.Repeat("a", MaxTitleBytes+1), ""); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	c, _ := s.CreateConversation(strings.Repeat("a", MaxTitleBytes), "")
	_, err := s.AppendMessage(c.ID, StoredMessage{Role: "user", Text: strings.Repeat("a", MaxTextBytes+1)})
	var le *LimitError
	if !errors.As(err, &le) || !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	if _, err := s.AppendMessage(c.ID, StoredMessage{Role: "user", Text: strings.Repeat("a", MaxTextBytes)}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		s.CreateConversation("t", "")
	}
	if l, _ := s.Conversations("", 1_000_000); len(l) != 4 {
		t.Fatal(len(l))
	}
}

func TestSearchEscaping(t *testing.T) {
	s, _ := openTemp(t)
	mk := func(title, text string) int64 {
		c, _ := s.CreateConversation(title, "")
		if text != "" {
			s.AppendMessage(c.ID, StoredMessage{Role: "user", Text: text})
		}
		return c.ID
	}
	pct := mk("100% done", "")
	under := mk("plain", "snake_case word")
	back := mk("plain2", `path C:\temp`)
	other := mk("abc", "a1b and 100 plus")
	ci := mk("Hello", "")
	_ = other
	check := func(q string, want ...int64) {
		t.Helper()
		l, err := s.Conversations(q, 0)
		if err != nil || len(l) != len(want) {
			t.Fatalf("%q: %v %v", q, l, err)
		}
		got := map[int64]bool{}
		for _, c := range l {
			got[c.ID] = true
		}
		for _, w := range want {
			if !got[w] {
				t.Fatalf("%q missing %d", q, w)
			}
		}
	}
	check("100%", pct)
	check("e_c", under)
	check("a_b") // '_' must not be a wildcard (a1b present)
	check("%", pct)
	check(`\temp`, back)  // backslash literal
	check("hello", ci)    // case-insensitive
	check("SNAKE", under) // message text search
	check("zzz-no-match")
}

func TestReopenPersists(t *testing.T) {
	s, path := openTemp(t)
	s.SaveProvider(Provider{ID: "p", Kind: KindAnthropic, HasKey: true, Enabled: true})
	c, _ := s.CreateConversation("t", "l")
	s.AppendMessage(c.ID, StoredMessage{Role: "user", Text: "hi"})
	s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if p, _ := s2.Providers(); len(p) != 1 || !p[0].HasKey {
		t.Fatal(p)
	}
	if m, _ := s2.Messages(c.ID); len(m) != 1 || m[0].Text != "hi" {
		t.Fatal(m)
	}
	var v int
	s2.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != schemaVersion {
		t.Fatal(v)
	}
}

func TestMigrateFromVersionZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v0.db")
	// A valid, empty sqlite database at version 0.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 0; CREATE TABLE t(x); DROP TABLE t;`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.CreateConversation("ok", ""); err != nil {
		t.Fatal(err)
	}
	// Also a brand-new zero-byte file.
	p2 := filepath.Join(t.TempDir(), "empty.db")
	os.WriteFile(p2, nil, 0o600)
	s3, err := Open(p2)
	if err != nil {
		t.Fatal(err)
	}
	s3.Close()
}

func TestTooNewUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.db")
	db, _ := sql.Open("sqlite", path)
	if _, err := db.Exec(fmt.Sprintf(`CREATE TABLE t(x); PRAGMA user_version = %d`, schemaVersion+5)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	before, _ := os.ReadFile(path)
	if _, err := Open(path); !errors.Is(err, ErrTooNew) {
		t.Fatalf("got %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("file modified")
	}
	if _, err := os.Stat(path + "-journal"); err == nil {
		t.Fatal("journal left behind")
	}
}

func TestCorruptUntouched(t *testing.T) {
	dir := t.TempDir()
	cases := map[string][]byte{
		"garbage":   bytes.Repeat([]byte("not a database "), 100),
		"short":     []byte("hello"),
		"truncated": append([]byte(sqliteMagic), make([]byte, 30)...),
	}
	for name, data := range cases {
		path := filepath.Join(dir, name)
		os.WriteFile(path, data, 0o600)
		if _, err := Open(path); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("%s: %v", name, err)
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(data, after) {
			t.Fatalf("%s modified", name)
		}
	}
}

func TestConcurrent(t *testing.T) {
	s, _ := openTemp(t)
	c, _ := s.CreateConversation("c", "")
	var wg sync.WaitGroup
	errc := make(chan error, 64)
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				if _, err := s.AppendMessage(c.ID, StoredMessage{Role: "user", Text: fmt.Sprintf("w%d-%d", w, i)}); err != nil {
					errc <- err
					return
				}
			}
		}(w)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				if _, err := s.Conversations("w1", 10); err != nil {
					errc <- err
					return
				}
				if _, err := s.Messages(c.ID); err != nil {
					errc <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Fatal(err)
	}
	if m, _ := s.Messages(c.ID); len(m) != 100 {
		t.Fatal(len(m))
	}
}

func benchStore(b *testing.B) (*Store, int64) {
	s, _ := openTemp(b)
	var first int64
	for i := 0; i < 200; i++ {
		c, _ := s.CreateConversation(fmt.Sprintf("conversation %d", i), "")
		if i == 0 {
			first = c.ID
		}
		for j := 0; j < 10; j++ {
			s.AppendMessage(c.ID, StoredMessage{Role: "user", Text: fmt.Sprintf("message %d of %d: lorem ipsum dolor sit amet", j, i)})
		}
	}
	return s, first
}

func BenchmarkAppendMessage(b *testing.B) {
	s, id := benchStore(b)
	m := StoredMessage{Role: "assistant", Text: strings.Repeat("x", 512)}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.AppendMessage(id, m); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConversationsSearch(b *testing.B) {
	s, _ := benchStore(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Conversations("of 199", 50); err != nil {
			b.Fatal(err)
		}
	}
}

// A provider's key is never written to the file by SaveProvider, only the fact
// that one exists.
func TestSaveProviderNeverWritesKey(t *testing.T) {
	const key = "sk-plaintext-must-not-land-on-disk"
	s, path := openTemp(t)
	if err := s.SaveProvider(Provider{ID: "p", Kind: KindOpenAI, APIKey: key, HasKey: true}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Providers()
	if err != nil || len(got) != 1 || !got[0].HasKey || got[0].APIKey != "" {
		t.Fatalf("%v %v", got, err)
	}
	if legacy, err := s.LegacyKeys(); err != nil || len(legacy) != 0 {
		t.Fatalf("legacy %v %v", legacy, err)
	}
	s.Close()
	raw, err := os.ReadFile(path)
	if err != nil || bytes.Contains(raw, []byte(key)) {
		t.Fatalf("key reached the database file (read err %v)", err)
	}
}

// A schema v1 database keeps its providers, learns which of them had a key, and
// hands the plaintext keys out exactly once.
func TestMigrateV1ProvidersToV2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(migrations[0] + `;
		INSERT INTO providers VALUES('a','A','openai','https://x','sk-old',1);
		INSERT INTO providers VALUES('b','B','anthropic','','',0);
		INSERT INTO models VALUES('a','m',1);
		PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Providers()
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	if got[0].ID != "a" || !got[0].HasKey || !got[0].Enabled || got[1].ID != "b" || got[1].HasKey {
		t.Fatalf("providers after migration: %v", got)
	}
	if ms, _ := s.Models(); len(ms) != 1 {
		t.Fatalf("models lost: %v", ms)
	}
	legacy, err := s.LegacyKeys()
	if err != nil || len(legacy) != 1 || legacy["a"] != "sk-old" {
		t.Fatalf("legacy %v %v", legacy, err)
	}
	if err := s.ClearLegacyKey("a"); err != nil {
		t.Fatal(err)
	}
	if legacy, _ := s.LegacyKeys(); len(legacy) != 0 {
		t.Fatalf("legacy key survived clear: %v", legacy)
	}
	if got, _ := s.Providers(); !got[0].HasKey {
		t.Fatal("clearing the legacy key must not clear has_key")
	}
}

func TestAuditKeepsASummaryNewestFirstAndPrunesByAge(t *testing.T) {
	s, _ := openTemp(t)
	now := time.Now()
	old := AuditEntry{At: now.Add(-48 * time.Hour), HostID: "h1", HostLabel: "web", Tool: "terminal_exec", Summary: "uptime", Outcome: AuditOK}
	recent := AuditEntry{At: now, HostID: "h2", HostLabel: "db", Tool: "sftp_read", Summary: "/etc/passwd", Outcome: AuditRefused}
	for _, entry := range []AuditEntry{old, recent} {
		if err := s.RecordAudit(entry); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := s.Audit(10)
	if err != nil || len(entries) != 2 || entries[0].HostLabel != "db" || entries[1].Summary != "uptime" {
		t.Fatalf("entries = %+v, err %v", entries, err)
	}
	if got, err := s.PruneAudit(now.Add(-24 * time.Hour)); err != nil || got != 1 {
		t.Fatalf("pruned %d, %v", got, err)
	}
	if entries, _ := s.Audit(10); len(entries) != 1 || entries[0].Outcome != AuditRefused {
		t.Fatalf("after prune: %+v", entries)
	}
	if err := s.ClearAudit(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := s.Audit(10); len(entries) != 0 {
		t.Fatalf("after clear: %+v", entries)
	}
}

func TestAuditCutsALongSummaryOnACharacterBoundaryAndNeedsAToolAndOutcome(t *testing.T) {
	s, _ := openTemp(t)
	long := strings.Repeat("命", MaxAuditSummaryBytes) // 3 bytes each
	if err := s.RecordAudit(AuditEntry{Tool: "terminal_exec", Summary: long, Outcome: AuditOK}); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.Audit(1)
	if len(entries) != 1 || len(entries[0].Summary) > MaxAuditSummaryBytes || !utf8.ValidString(entries[0].Summary) {
		t.Fatalf("summary length %d valid %v", len(entries[0].Summary), utf8.ValidString(entries[0].Summary))
	}
	if err := s.RecordAudit(AuditEntry{Outcome: AuditOK}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no tool: %v", err)
	}
	if err := s.RecordAudit(AuditEntry{Tool: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no outcome: %v", err)
	}
}

func TestModelReasoningPersistenceAndRetention(t *testing.T) {
	s, _ := openTemp(t)
	p := Provider{ID: "p1", Name: "Provider", Kind: "openai", BaseURL: "https://api.openai.com", Enabled: true}
	if err := s.SaveProvider(p); err != nil {
		t.Fatal(err)
	}

	modelWithReasoning := Model{
		ProviderID: "p1",
		Name:       "o3-mini",
		Enabled:    true,
		Reasoning: &ReasoningConfig{
			Levels:  []string{"low", "medium", "high"},
			Default: "medium",
		},
	}
	modelPlain := Model{
		ProviderID: "p1",
		Name:       "gpt-4o",
		Enabled:    true,
	}

	if err := s.SaveModels("p1", []Model{modelWithReasoning, modelPlain}); err != nil {
		t.Fatal(err)
	}

	models, err := s.Models()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	var foundO3, found4o *Model
	for i := range models {
		if models[i].Name == "o3-mini" {
			foundO3 = &models[i]
		} else if models[i].Name == "gpt-4o" {
			found4o = &models[i]
		}
	}

	if foundO3 == nil || foundO3.Reasoning == nil {
		t.Fatalf("expected o3-mini to have reasoning config, got %+v", foundO3)
	}
	if !slices.Equal(foundO3.Reasoning.Levels, []string{"low", "medium", "high"}) || foundO3.Reasoning.Default != "medium" {
		t.Fatalf("unexpected o3 reasoning: %+v", foundO3.Reasoning)
	}
	if found4o == nil || found4o.Reasoning != nil {
		t.Fatalf("expected gpt-4o to have nil reasoning, got %+v", found4o)
	}

	// Test SetModelReasoning
	updatedCfg := &ReasoningConfig{
		Levels:  []string{"low", "high"},
		Default: "high",
	}
	if err := s.SetModelReasoning("p1", "o3-mini", updatedCfg, false); err != nil {
		t.Fatal(err)
	}
	models, _ = s.Models()
	for _, m := range models {
		if m.Name == "o3-mini" {
			if !slices.Equal(m.Reasoning.Levels, []string{"low", "high"}) || m.Reasoning.Default != "high" {
				t.Fatalf("updated reasoning mismatch: %+v", m.Reasoning)
			}
		}
	}

	// Test SetModelReasoning disabled
	if err := s.SetModelReasoning("p1", "o3-mini", nil, true); err != nil {
		t.Fatal(err)
	}
	models, _ = s.Models()
	for _, m := range models {
		if m.Name == "o3-mini" {
			if !m.ReasoningDisabled || m.Reasoning != nil {
				t.Fatalf("expected o3-mini to be ReasoningDisabled, got %+v", m)
			}
		}
	}

	// Re-enable with custom config
	_ = s.SetModelReasoning("p1", "o3-mini", updatedCfg, false)

	// Test retention: SaveModels without reasoning config should keep existing reasoning config
	if err := s.SaveModels("p1", []Model{{ProviderID: "p1", Name: "o3-mini", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	models, _ = s.Models()
	if len(models) != 1 || models[0].Reasoning == nil || models[0].Reasoning.Default != "high" {
		t.Fatalf("expected retained reasoning after SaveModels, got %+v", models)
	}
}
