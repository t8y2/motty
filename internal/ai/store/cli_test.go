package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestCLIProvidersRoundTripAndRejectHTTPCredentials(t *testing.T) {
	s, _ := openTemp(t)
	for _, kind := range []string{"claude-code-cli", "codex-cli", "opencode-cli"} {
		p := Provider{ID: kind, Name: kind, Kind: kind, CLIPath: "/path with spaces/agent", Enabled: true}
		if err := s.SaveProvider(p); err != nil {
			t.Fatal(err)
		}
		if !ValidProviderKind(kind) || !IsCLIKind(kind) {
			t.Fatal("CLI kind rejected")
		}
		p.HasKey = true
		if err := s.SaveProvider(p); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted CLI credentials: %v", err)
		}
	}
	got, err := s.Providers()
	if err != nil || len(got) != 3 {
		t.Fatal(got, err)
	}
	for _, p := range got {
		if p.CLIPath != "/path with spaces/agent" || p.HasKey || p.APIKey != "" {
			t.Fatal(p)
		}
	}
	if ValidProviderKind("bogus") || IsCLIKind(KindOpenAI) {
		t.Fatal("invalid kind classification")
	}
}

func TestMigrationV2RetainsHTTPProviderAndDefaultsCLIPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(migrations[0] + migrations[1] + `INSERT INTO providers(id,name,kind,base_url,api_key,enabled,has_key) VALUES('p','P','openai','https://x','',1,1); PRAGMA user_version=2;`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ps, err := s.Providers()
	if err != nil || len(ps) != 1 || ps[0].CLIPath != "" || !ps[0].HasKey || ps[0].BaseURL != "https://x" {
		t.Fatal(ps, err)
	}
	ps[0].Name = "kept"
	if err := s.SaveProvider(ps[0]); err != nil {
		t.Fatal(err)
	}
}
