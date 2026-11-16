package aiproviderimport

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func findBySourceAndName(t *testing.T, candidates []Candidate, source Source, name string) Candidate {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.Source == source && candidate.Name == name {
			return candidate
		}
	}
	t.Fatalf("no %s candidate named %q in %#v", source, name, candidates)
	return Candidate{}
}

func TestMagpieOffersTheGatewayAndEachUsableEndpoint(t *testing.T) {
	data := `{"providers":[
		{"id":"relay","name":"Relay","key":"fake-key","chat":"https://relay.example.com/v1","anthropic":"https://relay.example.com/anthropic","models":["m1","m1","m2"]},
		{"id":"bearer","name":"Bearer Relay","key":"fake-token","anthropic":"https://bearer.example.com","authHeader":"bearer","models":["b1"]},
		{"id":"api","name":"API Relay","key":"fake-api","anthropic":"https://api.example.com","authHeader":"x-api-key","models":["a1"]},
		{"id":"off","name":"Off","key":"fake","anthropic":"https://off.example.com","off":true,"models":["o1"]},
		{"id":"empty","name":"Empty","key":"fake"},
		{"id":"responses","name":"Responses","key":"fake","responses":"https://responses.example.com","gemini":"https://gemini.example.com"}
	]}`
	candidates, err := ParseMagpieBytes([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	gateway := findBySourceAndName(t, candidates, SourceMagpieGateway, "magpie")
	if gateway.Kind != KindAnthropic || gateway.BaseURL != "http://127.0.0.1:3425" || gateway.Key != "magpie" || gateway.Note != NoteMagpieGateway {
		t.Fatalf("gateway = %#v", gateway)
	}
	wantModels := []string{"api/a1", "bearer/b1", "relay/m1", "relay/m2"}
	if strings.Join(gateway.Models, ",") != strings.Join(wantModels, ",") {
		t.Fatalf("gateway models = %v, want %v", gateway.Models, wantModels)
	}

	relayAnthropic := findBySourceAndName(t, candidates, SourceMagpieUpstream, "Relay (Anthropic)")
	if relayAnthropic.Kind != KindAnthropic || relayAnthropic.BaseURL != "https://relay.example.com/anthropic" || relayAnthropic.AuthStyle != AuthStyleAPIKey {
		t.Fatalf("relay anthropic = %#v", relayAnthropic)
	}
	relayOpenAI := findBySourceAndName(t, candidates, SourceMagpieUpstream, "Relay (OpenAI)")
	if relayOpenAI.Kind != KindOpenAI || relayOpenAI.BaseURL != "https://relay.example.com/v1" || relayOpenAI.AuthStyle != "" {
		t.Fatalf("relay openai = %#v", relayOpenAI)
	}
	if got := findBySourceAndName(t, candidates, SourceMagpieUpstream, "Bearer Relay"); got.AuthStyle != AuthStyleBearer {
		t.Fatalf("bearer relay = %#v", got)
	}
	if got := findBySourceAndName(t, candidates, SourceMagpieUpstream, "API Relay"); got.AuthStyle != AuthStyleAPIKey {
		t.Fatalf("api relay = %#v", got)
	}
	for name, reason := range map[string]string{
		"Off": ReasonDisabled, "Empty": ReasonNoEndpoint, "Responses": ReasonUnsupportedProtocol,
	} {
		if got := findBySourceAndName(t, candidates, SourceMagpieUpstream, name); got.SkipReason != reason {
			t.Fatalf("%s reason = %q, want %q", name, got.SkipReason, reason)
		}
	}
}

func TestMagpieChatThatNeedsAnAPIKeyHeaderIsSkipped(t *testing.T) {
	data := `{"providers":[{"id":"x","name":"X","chat":"https://x.example.com/v1","key":"fake","authHeader":"x-api-key"}]}`
	candidates, err := ParseMagpieBytes([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	got := findBySourceAndName(t, candidates, SourceMagpieUpstream, "X")
	if got.SkipReason != ReasonUnsupportedAuth || got.Key != "" {
		t.Fatalf("candidate = %#v", got)
	}
}

func TestMagpieCapsTheGatewayModelList(t *testing.T) {
	models := make([]string, MaxModels+100)
	for index := range models {
		models[index] = fmt.Sprintf("m%04d", index)
	}
	quoted := make([]string, len(models))
	for index, model := range models {
		quoted[index] = fmt.Sprintf("%q", model)
	}
	provider := func(id string) string {
		return fmt.Sprintf(`{"id":%q,"name":%q,"anthropic":"https://%s.example.com","models":[%s]}`,
			id, id, id, strings.Join(quoted, ","))
	}
	data := fmt.Sprintf(`{"providers":[%s,%s]}`, provider("one"), provider("two"))
	candidates, err := ParseMagpieBytes([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	gateway := findBySourceAndName(t, candidates, SourceMagpieGateway, "magpie")
	if len(gateway.Models) != MaxModels {
		t.Fatalf("gateway models = %d, want %d", len(gateway.Models), MaxModels)
	}
}

func openCCSwitchFixture(t *testing.T, optional bool) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cc-switch.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	columns := `id TEXT PRIMARY KEY, app_type TEXT, name TEXT, settings_config TEXT`
	if optional {
		columns += `, meta TEXT, sort_index INTEGER, created_at INTEGER`
	}
	if _, err := db.Exec(`CREATE TABLE providers (` + columns + `)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

func addCCSwitchRow(t *testing.T, db *sql.DB, optional bool, id, app, name, settings, meta string, order int) {
	t.Helper()
	if optional {
		if _, err := db.Exec(`INSERT INTO providers(id,app_type,name,settings_config,meta,sort_index,created_at) VALUES(?,?,?,?,?,?,?)`,
			id, app, name, settings, meta, order, order); err != nil {
			t.Fatal(err)
		}
		return
	}
	if _, err := db.Exec(`INSERT INTO providers(id,app_type,name,settings_config) VALUES(?,?,?,?)`,
		id, app, name, settings); err != nil {
		t.Fatal(err)
	}
}

func TestCCSwitchSQLiteMapsClaudeCodexAndOpenCode(t *testing.T) {
	db, path := openCCSwitchFixture(t, true)
	addCCSwitchRow(t, db, true, "claude-token", "claude", "Claude Relay",
		`{"env":{"ANTHROPIC_BASE_URL":"https://relay.example.com/anthropic","ANTHROPIC_AUTH_TOKEN":"fake-token","ANTHROPIC_MODEL":"claude-x","ANTHROPIC_DEFAULT_HAIKU_MODEL":"claude-x","ANTHROPIC_DEFAULT_HAIKU_MODEL_NAME":"Claude X","CLAUDE_CODE_SUBAGENT_MODEL":"claude-y"}}`,
		`{"apiKeyField":"ANTHROPIC_AUTH_TOKEN"}`, 1)
	addCCSwitchRow(t, db, true, "claude-api", "claude", "Claude API",
		`{"env":{"ANTHROPIC_BASE_URL":"https://api.example.com","ANTHROPIC_API_KEY":"fake-api","ANTHROPIC_MODEL":"claude-api"}}`,
		`{"apiKeyField":"ANTHROPIC_API_KEY"}`, 2)
	addCCSwitchRow(t, db, true, "claude-chat", "claude", "Claude Chat",
		`{"env":{"ANTHROPIC_BASE_URL":"https://chat.example.com/v1","ANTHROPIC_AUTH_TOKEN":"fake-chat"}}`,
		`{"apiFormat":"openai_chat"}`, 3)
	addCCSwitchRow(t, db, true, "codex-chat", "codex", "Codex Chat",
		`{"auth":{"OPENAI_API_KEY":"fake-codex"},"config":"model_provider = \"relay\"\nmodel = \"gpt-chat\"\n[model_providers.relay]\nbase_url = \"https://codex.example.com/v1\"\nwire_api = \"chat\"\n"}`,
		`{}`, 4)
	addCCSwitchRow(t, db, true, "codex-responses", "codex", "Codex Responses",
		`{"auth":{"OPENAI_API_KEY":"fake-codex"},"config":"model_provider = \"relay\"\n[model_providers.relay]\nbase_url = \"https://responses.example.com/v1\"\nwire_api = \"responses\"\n"}`,
		`{}`, 5)
	addCCSwitchRow(t, db, true, "codex-oauth", "codex", "Codex OAuth",
		`{"auth":{"OPENAI_API_KEY":null,"auth_mode":"chatgpt","tokens":{"access_token":"fake"}},"config":"model_provider = \"relay\"\n[model_providers.relay]\nbase_url = \"https://oauth.example.com/v1\"\nwire_api = \"chat\"\n"}`,
		`{}`, 6)
	addCCSwitchRow(t, db, true, "opencode-anthropic", "opencode", "OpenCode Anthropic",
		`{"npm":"@ai-sdk/anthropic","options":{"baseURL":"https://opencode.example.com/anthropic","apiKey":"fake-open"},"models":{"m1":{"name":"M1"},"m2":{"name":"M2"}}}`,
		`{}`, 7)
	addCCSwitchRow(t, db, true, "opencode-openai", "opencode", "OpenCode OpenAI",
		`{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"https://openai.example.com/v1","apiKey":"fake-open"},"models":{"m1":{}}}`,
		`{}`, 8)
	addCCSwitchRow(t, db, true, "gemini", "gemini", "Gemini", `{}`, `{}`, 9)
	addCCSwitchRow(t, db, true, "ambiguous", "opencode", "Ambiguous",
		`{"npm":"@ai-sdk/unknown","options":{"baseURL":"https://unknown.example.com","apiKey":"fake"},"models":{}}`,
		`{}`, 10)
	if _, err := db.Exec(`CREATE TABLE proxy_config (listen TEXT); INSERT INTO proxy_config(listen) VALUES('127.0.0.1:15721')`); err != nil {
		t.Fatal(err)
	}
	addCCSwitchRow(t, db, true, "proxy", "claude", "Proxy",
		`{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:15721","ANTHROPIC_AUTH_TOKEN":"fake"}}`,
		`{}`, 11)
	addCCSwitchRow(t, db, true, "claude-both", "claude", "Claude Both",
		`{"env":{"ANTHROPIC_BASE_URL":"https://both.example.com","ANTHROPIC_API_KEY":"fake-api-both","ANTHROPIC_AUTH_TOKEN":"fake-token-both"}}`,
		`{}`, 12)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	candidates, err := ParseCCSwitchSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := findBySourceAndName(t, candidates, SourceCCSwitch, "Claude Relay"); got.Kind != KindAnthropic || got.AuthStyle != AuthStyleBearer || got.Key != "fake-token" {
		t.Fatalf("claude token = %#v", got)
	} else if strings.Join(got.Models, ",") != "claude-x,claude-y" {
		t.Fatalf("claude models = %v", got.Models)
	}
	if got := findBySourceAndName(t, candidates, SourceCCSwitch, "Claude API"); got.AuthStyle != AuthStyleAPIKey || got.Key != "fake-api" {
		t.Fatalf("claude api = %#v", got)
	}
	if got := findBySourceAndName(t, candidates, SourceCCSwitch, "Claude Chat"); got.Kind != KindOpenAI || got.AuthStyle != "" {
		t.Fatalf("claude chat = %#v", got)
	}
	if got := findBySourceAndName(t, candidates, SourceCCSwitch, "Claude Both"); got.AuthStyle != AuthStyleBearer || got.Key != "fake-token-both" {
		t.Fatalf("claude default credential = %#v", got)
	}
	if got := findBySourceAndName(t, candidates, SourceCCSwitch, "Codex Chat"); got.Kind != KindOpenAI || got.BaseURL != "https://codex.example.com/v1" || strings.Join(got.Models, ",") != "gpt-chat" {
		t.Fatalf("codex chat = %#v", got)
	}
	for name, reason := range map[string]string{
		"Codex Responses": ReasonResponses, "Codex OAuth": ReasonOAuth,
		"Gemini": ReasonGemini, "Ambiguous": ReasonAmbiguous, "Proxy": ReasonProxy,
	} {
		if got := findBySourceAndName(t, candidates, SourceCCSwitch, name); got.SkipReason != reason {
			t.Fatalf("%s reason = %q, want %q", name, got.SkipReason, reason)
		}
	}
	if got := findBySourceAndName(t, candidates, SourceCCSwitch, "OpenCode Anthropic"); got.Kind != KindAnthropic || got.BaseURL != "https://opencode.example.com/anthropic" || strings.Join(got.Models, ",") != "m1,m2" {
		t.Fatalf("opencode anthropic = %#v", got)
	}
	if got := findBySourceAndName(t, candidates, SourceCCSwitch, "OpenCode OpenAI"); got.Kind != KindOpenAI {
		t.Fatalf("opencode openai = %#v", got)
	}
}

func TestCCSwitchSQLiteToleratesAMissingOptionalColumnSet(t *testing.T) {
	db, path := openCCSwitchFixture(t, false)
	addCCSwitchRow(t, db, false, "p", "claude", "Old",
		`{"env":{"ANTHROPIC_BASE_URL":"https://old.example.com","ANTHROPIC_AUTH_TOKEN":"fake"}}`, "", 0)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	candidates, err := ParseCCSwitchSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Name != "Old" || candidates[0].AuthStyle != AuthStyleBearer {
		t.Fatalf("candidates = %#v", candidates)
	}
}

func TestCCSwitchLegacyKeepsGoodProvidersBesideAMalformedEntry(t *testing.T) {
	data := `{"claude":{"providers":{
		"good":{"id":"good","name":"Good","settingsConfig":{"env":{"ANTHROPIC_BASE_URL":"https://good.example.com","ANTHROPIC_AUTH_TOKEN":"fake"}}},
		"bad":{"id":"bad","name":123,"settingsConfig":"not an object"}
	}}}`
	candidates, err := ParseCCSwitchLegacy([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := findBySourceAndName(t, candidates, SourceCCSwitch, "Good"); got.SkipReason != "" || got.BaseURL != "https://good.example.com" {
		t.Fatalf("good entry = %#v", got)
	}
	if got := findBySourceAndName(t, candidates, SourceCCSwitch, "bad"); got.SkipReason != ReasonMalformed {
		t.Fatalf("bad entry = %#v", got)
	}
}

func TestCCSwitchLegacyV2AndV1(t *testing.T) {
	v2 := `{"version":2,
		"claude":{"providers":{"c1":{"id":"c1","name":"Legacy Claude","settingsConfig":{"env":{"ANTHROPIC_BASE_URL":"https://legacy.example.com","ANTHROPIC_AUTH_TOKEN":"fake"}},"meta":{"apiKeyField":"ANTHROPIC_AUTH_TOKEN"}}}} ,
		"codex":{"providers":{"x1":{"id":"x1","name":"Legacy Codex","settingsConfig":{"auth":{"OPENAI_API_KEY":"fake"},"config":"model_provider = \"r\"\nmodel = \"gpt\"\n[model_providers.r]\nbase_url = \"https://codex.example.com/v1\"\nwire_api = \"chat\""}}},"mcp":{}}}`
	candidates, err := ParseCCSwitchLegacy([]byte(v2))
	if err != nil {
		t.Fatal(err)
	}
	if got := findBySourceAndName(t, candidates, SourceCCSwitch, "Legacy Claude"); got.AuthStyle != AuthStyleBearer {
		t.Fatalf("legacy claude = %#v", got)
	}
	if got := findBySourceAndName(t, candidates, SourceCCSwitch, "Legacy Codex"); got.Kind != KindOpenAI || got.BaseURL != "https://codex.example.com/v1" {
		t.Fatalf("legacy codex = %#v", got)
	}

	v1 := `{"providers":{"c1":{"id":"c1","name":"V1","settingsConfig":{"env":{"ANTHROPIC_BASE_URL":"https://v1.example.com","ANTHROPIC_API_KEY":"fake"}}}},"current":"c1"}`
	candidates, err = ParseCCSwitchLegacy([]byte(v1))
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Name != "V1" || candidates[0].AuthStyle != AuthStyleAPIKey {
		t.Fatalf("v1 candidates = %#v", candidates)
	}
}

func TestCandidateFormattingHidesTheKey(t *testing.T) {
	candidate := Candidate{Name: "Relay", Key: "secret-value"}
	if strings.Contains(candidate.String(), "secret-value") || strings.Contains(candidate.GoString(), "secret-value") {
		t.Fatalf("a formatted candidate shows its key: %s / %s", candidate, candidate.GoString())
	}
	if !strings.Contains(candidate.GoString(), "redacted") {
		t.Fatalf("the redacted form says nothing about the key: %s", candidate.GoString())
	}
}

func TestBoundsAndMalformedInput(t *testing.T) {
	if _, err := ParseMagpie(strings.NewReader(strings.Repeat("x", MaxJSONBytes+1))); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized JSON: %v", err)
	}
	if _, err := ParseMagpieBytes([]byte(`{"providers":[`)); !errors.Is(err, ErrMalformed) {
		t.Fatalf("truncated JSON: %v", err)
	}
	many := make([]string, MaxProviders+1)
	for index := range many {
		many[index] = `{"id":"p","name":"P"}`
	}
	if _, err := ParseMagpieBytes([]byte(`{"providers":[` + strings.Join(many, ",") + `]}`)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("too many providers: %v", err)
	}
	longName := strings.Repeat("名", MaxNameRunes+5)
	candidates, err := ParseMagpieBytes([]byte(`{"providers":[{"id":"p","name":"` + longName + `","anthropic":"https://x.example.com"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	wantName := string([]rune(longName)[:MaxNameRunes])
	if got := findBySourceAndName(t, candidates, SourceMagpieUpstream, wantName); len([]rune(got.Name)) != MaxNameRunes {
		t.Fatalf("name = %q", got.Name)
	}
	models := make([]string, MaxModels+1)
	for index := range models {
		models[index] = `"m"`
	}
	candidates, err = ParseMagpieBytes([]byte(`{"providers":[{"id":"p","name":"P","anthropic":"https://x.example.com","models":[` + strings.Join(models, ",") + `]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := findBySourceAndName(t, candidates, SourceMagpieUpstream, "P"); len(got.Models) != 1 {
		t.Fatalf("duplicate models kept: %d", len(got.Models))
	}
}

func TestScanUsesTheStandardLocationsAndPrefersTheDatabase(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(t.TempDir(), "xdg")
	magpie := filepath.Join(xdg, "magpie", "providers.json")
	if err := os.MkdirAll(filepath.Dir(magpie), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(magpie, []byte(`{"providers":[{"id":"p","name":"Magpie","anthropic":"https://m.example.com"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(home, ".cc-switch", "config.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"claude":{"providers":{"c":{"id":"c","name":"Legacy","settingsConfig":{"env":{"ANTHROPIC_BASE_URL":"https://l.example.com"}}}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sources := scan(home, xdg, "darwin")
	if len(sources) != 2 || !sources[0].Available() || !sources[1].Available() {
		t.Fatalf("sources = %#v", sources)
	}
	if sources[0].Path != magpie || sources[1].Path != legacy {
		t.Fatalf("paths = %q %q", sources[0].Path, sources[1].Path)
	}

	// A Windows magpie location ignores XDG and lives under the home directory.
	windowsHome := t.TempDir()
	windowsMagpie := filepath.Join(windowsHome, ".config", "magpie", "providers.json")
	if err := os.MkdirAll(filepath.Dir(windowsMagpie), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(windowsMagpie, []byte(`{"providers":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := scan(windowsHome, xdg, "windows"); !got[0].Available() || got[0].Path != windowsMagpie {
		t.Fatalf("windows magpie = %#v", got[0])
	}
}

func TestScanIgnoresMissingHomeAndRelativeXDG(t *testing.T) {
	for _, source := range scan("", "relative/xdg", "darwin") {
		if source.Found || source.Path != "" {
			t.Fatalf("relative location resolved: %#v", source)
		}
	}
	for _, source := range scan("relative/home", "", "darwin") {
		if source.Found || source.Path != "" {
			t.Fatalf("relative home resolved: %#v", source)
		}
	}

	// An absolute XDG path still works when the home directory is unknown;
	// only cc-switch, which lives under home, becomes unavailable.
	xdg := t.TempDir()
	magpie := filepath.Join(xdg, "magpie", "providers.json")
	if err := os.MkdirAll(filepath.Dir(magpie), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(magpie, []byte(`{"providers":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sources := scan("", xdg, "darwin")
	if !sources[0].Available() || sources[0].Path != magpie || sources[1].Found {
		t.Fatalf("sources = %#v", sources)
	}
}

func TestParseFileIdentifiesTheChosenContent(t *testing.T) {
	dir := t.TempDir()
	magpie := filepath.Join(dir, "providers.txt")
	if err := os.WriteFile(magpie, []byte(`{"providers":[{"id":"p","name":"M","anthropic":"https://m.example.com"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if candidates, err := ParseFile(magpie); err != nil || len(candidates) != 2 {
		t.Fatalf("magpie file: %d %v", len(candidates), err)
	}

	legacy := filepath.Join(dir, "config.json")
	if err := os.WriteFile(legacy, []byte(`{"claude":{"providers":{"c":{"id":"c","name":"L","settingsConfig":{"env":{"ANTHROPIC_BASE_URL":"https://l.example.com"}}}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if candidates, err := ParseFile(legacy); err != nil || len(candidates) != 1 {
		t.Fatalf("legacy file: %d %v", len(candidates), err)
	}

	// A v1 file without a current id is still identified by its shape.
	v1 := filepath.Join(dir, "v1.json")
	if err := os.WriteFile(v1, []byte(`{"providers":{"c":{"id":"c","name":"V1 Copy","settingsConfig":{"env":{"ANTHROPIC_BASE_URL":"https://v1.example.com"}}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if candidates, err := ParseFile(v1); err != nil || len(candidates) != 1 || candidates[0].Name != "V1 Copy" {
		t.Fatalf("v1 file: %d %v", len(candidates), err)
	}

	db, dbPath := openCCSwitchFixture(t, true)
	addCCSwitchRow(t, db, true, "s", "claude", "SQLite Copy",
		`{"env":{"ANTHROPIC_BASE_URL":"https://s.example.com","ANTHROPIC_AUTH_TOKEN":"fake"}}`,
		`{"apiKeyField":"ANTHROPIC_AUTH_TOKEN"}`, 1)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(dir, "chosen-copy.db")
	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if candidates, err := ParseFile(copyPath); err != nil || len(candidates) != 1 || candidates[0].Name != "SQLite Copy" {
		t.Fatalf("sqlite copy: %d %v", len(candidates), err)
	}

	unknown := filepath.Join(dir, "unknown.json")
	if err := os.WriteFile(unknown, []byte(`{"hello":"world"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFile(unknown); !errors.Is(err, ErrUnknownSource) {
		t.Fatalf("unknown file: %v", err)
	}
}

func FuzzParseMagpie(f *testing.F) {
	f.Add([]byte(`{"providers":[]}`))
	f.Add([]byte(`{"providers":[{"id":"p","name":"P","anthropic":"https://x.example.com"}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseMagpieBytes(data)
	})
}

func FuzzParseCCSwitchSQLite(f *testing.F) {
	f.Add([]byte("not sqlite"))
	f.Add([]byte("SQLite format 3\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "fuzz.db")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		_, _ = ParseCCSwitchSQLite(path)
	})
}

func FuzzParseCCSwitchLegacy(f *testing.F) {
	f.Add([]byte(`{"claude":{"providers":{}}}`))
	f.Add([]byte(`{"providers":{},"current":""}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseCCSwitchLegacy(data)
	})
}

func FuzzParseCodexTOML(f *testing.F) {
	f.Add("model_provider = \"r\"\n[model_providers.r]\nbase_url = \"https://x\"\nwire_api = \"chat\"")
	f.Fuzz(func(t *testing.T, config string) {
		_ = parseCodexTOML(config)
	})
}
