package aiproviderimport

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// ParseCCSwitchSQLite reads cc-switch's providers table with a read-only
// connection. The schema has gained columns over time, so the query selects
// only the columns that exist by name and ignores the rest.
func ParseCCSwitchSQLite(path string) ([]Candidate, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxSQLiteBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrTooLarge, info.Size())
	}
	if strings.ContainsAny(path, "?#") {
		return nil, fmt.Errorf("%w: unsupported database path", ErrMalformed)
	}
	db, err := sql.Open("sqlite", "file:"+strings.ReplaceAll(path, "\\", "/")+"?mode=ro&_pragma=busy_timeout(3000)")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	columns, err := sqliteColumns(db, "providers")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	for _, required := range []string{"id", "app_type", "name", "settings_config"} {
		if !columns[required] {
			return nil, ErrUnknownSource
		}
	}
	selected := []string{"id", "app_type", "name", "settings_config"}
	for _, optional := range []string{"meta", "sort_index", "created_at"} {
		if columns[optional] {
			selected = append(selected, optional)
		}
	}
	order := "name"
	if columns["sort_index"] && columns["created_at"] {
		order = "sort_index, created_at"
	}
	// Read the proxy table before the provider rows: the connection pool is
	// limited to one connection, so a second query must not run under an open
	// result set.
	proxies := proxyTakeoverAddresses(db)

	query := "SELECT " + strings.Join(selected, ",") + " FROM providers ORDER BY " + order + " LIMIT " + strconv.Itoa(MaxProviders+1)
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	defer rows.Close()

	var out []Candidate
	for rows.Next() {
		values := make([]sql.NullString, len(selected))
		pointers := make([]any, len(selected))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
		}
		field := map[string]sql.NullString{}
		for index, name := range selected {
			field[name] = values[index]
		}
		out = append(out, ccSwitchCandidate(
			field["app_type"].String,
			field["id"].String,
			field["name"].String,
			[]byte(field["settings_config"].String),
			[]byte(field["meta"].String),
			proxies,
		))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if len(out) > MaxProviders {
		return nil, fmt.Errorf("%w: more than %d providers", ErrTooLarge, MaxProviders)
	}
	return out, nil
}

// sqliteColumns returns the column names of a table, or an error when it does
// not exist.
func sqliteColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info("` + strings.ReplaceAll(table, `"`, `""`) + `")`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s does not exist", table)
	}
	return columns, nil
}

var (
	loopbackAddress = regexp.MustCompile(`(?i)(127\.0\.0\.1|localhost|\[::1\]):(\d{2,5})`)
	proxyPort       = regexp.MustCompile(`(?i)"?port"?\s*[:=]\s*(\d{2,5})`)
)

// proxyTakeoverAddresses reads the ports cc-switch's local proxy listens on.
// A provider whose endpoint points there depends on another running
// application, so the import skips it rather than creating a broken provider.
func proxyTakeoverAddresses(db *sql.DB) map[string]bool {
	columns, err := sqliteColumns(db, "proxy_config")
	if err != nil || len(columns) == 0 {
		return nil
	}
	rows, err := db.Query(`SELECT * FROM proxy_config LIMIT 10`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return nil
	}
	addresses := map[string]bool{}
	for rows.Next() {
		values := make([]sql.NullString, len(names))
		pointers := make([]any, len(names))
		for index := range values {
			pointers[index] = &values[index]
		}
		if rows.Scan(pointers...) != nil {
			continue
		}
		var parts []string
		for _, value := range values {
			if value.Valid {
				parts = append(parts, value.String)
			}
		}
		text := strings.Join(parts, " ")
		for _, match := range loopbackAddress.FindAllStringSubmatch(text, -1) {
			addresses[strings.ToLower(match[1])+":"+match[2]] = true
		}
		for _, match := range proxyPort.FindAllStringSubmatch(text, -1) {
			port := match[1]
			addresses["127.0.0.1:"+port] = true
			addresses["localhost:"+port] = true
			addresses["::1:"+port] = true
			addresses["[::1]:"+port] = true
		}
	}
	return addresses
}

func isProxyTakeover(endpoint string, addresses map[string]bool) bool {
	if len(addresses) == 0 {
		return false
	}
	host, port := endpointKey(endpoint)
	if host == "" || port == "" {
		return false
	}
	return addresses[host+":"+port]
}

func ccSwitchCandidate(app, id, name string, settings, meta []byte, proxies map[string]bool) Candidate {
	display := CleanName(name)
	if display == "" {
		display = CleanName(id)
	}
	if display == "" {
		display = "cc-switch"
	}
	switch lowerTrim(app) {
	case "claude":
		return claudeCandidate(display, settings, meta, proxies)
	case "codex":
		return codexCandidate(display, settings, meta, proxies)
	case "opencode":
		return opencodeCandidate(display, settings, meta, proxies)
	case "gemini":
		return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonGemini}
	default:
		return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonUnsupportedApp}
	}
}

func claudeCandidate(display string, settings, meta []byte, proxies map[string]bool) Candidate {
	object, err := jsonObject(settings)
	if err != nil || object == nil {
		return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonMalformed}
	}
	env := objectField(object, "env")
	metaObject, _ := jsonObject(meta)
	apiFormat := lowerTrim(stringField(metaObject, "apiFormat"))
	switch apiFormat {
	case "openai_responses":
		return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonResponses}
	case "gemini_native":
		return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonGemini}
	}
	kind := KindAnthropic
	if apiFormat == "openai_chat" {
		kind = KindOpenAI
	}
	base := firstNonEmpty(stringField(env, "ANTHROPIC_BASE_URL"), stringField(object, "base_url"), stringField(object, "baseURL"))
	if !validEndpoint(base) {
		return Candidate{Source: SourceCCSwitch, Name: display, Kind: kind, SkipReason: ReasonNoEndpoint}
	}
	if isProxyTakeover(base, proxies) {
		return Candidate{Source: SourceCCSwitch, Name: display, Kind: kind, BaseURL: base, SkipReason: ReasonProxy}
	}

	var key, style string
	if kind == KindAnthropic {
		key, style = claudeCredential(env, stringField(metaObject, "apiKeyField"))
	} else {
		key = firstNonEmpty(stringField(env, "ANTHROPIC_API_KEY"), stringField(env, "ANTHROPIC_AUTH_TOKEN"), stringField(env, "OPENAI_API_KEY"))
	}
	return Candidate{
		Source: SourceCCSwitch, Name: display, Kind: kind, BaseURL: base,
		AuthStyle: style, Key: key, Models: claudeModels(env),
	}
}

// claudeCredential follows the source's recorded field choice. The default is
// ANTHROPIC_AUTH_TOKEN (bearer); an explicit ANTHROPIC_API_KEY choice maps to
// the x-api-key style.
func claudeCredential(env map[string]json.RawMessage, field string) (key, style string) {
	switch strings.ToUpper(strings.TrimSpace(field)) {
	case "ANTHROPIC_API_KEY":
		if key = stringField(env, "ANTHROPIC_API_KEY"); key != "" {
			return key, AuthStyleAPIKey
		}
		return stringField(env, "ANTHROPIC_AUTH_TOKEN"), AuthStyleBearer
	case "ANTHROPIC_AUTH_TOKEN":
		if key = stringField(env, "ANTHROPIC_AUTH_TOKEN"); key != "" {
			return key, AuthStyleBearer
		}
		return stringField(env, "ANTHROPIC_API_KEY"), AuthStyleAPIKey
	}
	if key = stringField(env, "ANTHROPIC_AUTH_TOKEN"); key != "" {
		return key, AuthStyleBearer
	}
	if key = stringField(env, "ANTHROPIC_API_KEY"); key != "" {
		return key, AuthStyleAPIKey
	}
	return "", ""
}

// claudeModels lists the model identifiers, never the display names cc-switch
// stores beside them in *_NAME fields.
func claudeModels(env map[string]json.RawMessage) []string {
	var names []string
	for _, key := range []string{
		"ANTHROPIC_MODEL",
		"ANTHROPIC_DEFAULT_FABLE_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"CLAUDE_CODE_SUBAGENT_MODEL",
	} {
		if name := stringField(env, key); name != "" {
			names = append(names, name)
		}
	}
	return cleanModels(names)
}

func codexCandidate(display string, settings, meta []byte, proxies map[string]bool) Candidate {
	object, err := jsonObject(settings)
	if err != nil || object == nil {
		return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonMalformed}
	}
	auth := objectField(object, "auth")
	apiKey := stringField(auth, "OPENAI_API_KEY")
	if apiKey == "" {
		if tokens, ok := auth["tokens"]; ok && hasObject(tokens) {
			return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonOAuth}
		}
		if strings.EqualFold(stringField(auth, "auth_mode"), "chatgpt") {
			return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonOAuth}
		}
	}
	config := stringField(object, "config")
	fields := parseCodexTOML(config)
	metaObject, _ := jsonObject(meta)
	apiFormat := lowerTrim(stringField(metaObject, "apiFormat"))
	wire := lowerTrim(fields["wire_api"])
	switch wire {
	case "responses":
		return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonResponses}
	case "chat", "chat_completions", "completions":
		// importable
	case "":
		switch apiFormat {
		case "openai_chat":
			// importable
		case "openai_responses":
			return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonResponses}
		default:
			return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonNoChat}
		}
	default:
		return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonUnsupportedProtocol}
	}
	base := strings.TrimSpace(fields["base_url"])
	if !validEndpoint(base) {
		return Candidate{Source: SourceCCSwitch, Name: display, Kind: KindOpenAI, SkipReason: ReasonNoEndpoint}
	}
	if isProxyTakeover(base, proxies) {
		return Candidate{Source: SourceCCSwitch, Name: display, Kind: KindOpenAI, BaseURL: base, SkipReason: ReasonProxy}
	}
	return Candidate{
		Source: SourceCCSwitch, Name: display, Kind: KindOpenAI, BaseURL: base,
		Key: apiKey, Models: cleanModels([]string{fields["model"]}),
	}
}

func opencodeCandidate(display string, settings, meta []byte, proxies map[string]bool) Candidate {
	object, err := jsonObject(settings)
	if err != nil || object == nil {
		return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonMalformed}
	}
	options := objectField(object, "options")
	base := firstNonEmpty(
		stringField(options, "baseURL"), stringField(options, "baseUrl"), stringField(options, "base_url"),
		stringField(object, "baseURL"), stringField(object, "baseUrl"),
	)
	key := firstNonEmpty(stringField(options, "apiKey"), stringField(options, "api_key"), stringField(object, "apiKey"))
	npm := lowerTrim(stringField(object, "npm"))
	metaObject, _ := jsonObject(meta)
	apiFormat := lowerTrim(stringField(metaObject, "apiFormat"))
	if name := CleanName(stringField(object, "name")); name != "" && display == "cc-switch" {
		display = name
	}
	models := cleanModels(sortedKeys(objectField(object, "models")))

	kind := ""
	switch apiFormat {
	case "anthropic":
		kind = KindAnthropic
	case "openai_chat":
		kind = KindOpenAI
	case "openai_responses":
		return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonResponses}
	case "gemini_native":
		return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonGemini}
	}
	if kind == "" {
		switch {
		case strings.Contains(npm, "anthropic"):
			kind = KindAnthropic
		case strings.Contains(npm, "openai"):
			kind = KindOpenAI
		case strings.Contains(npm, "google") || strings.Contains(npm, "gemini"):
			return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonGemini}
		case strings.Contains(lowerTrim(base), "anthropic"):
			kind = KindAnthropic
		case strings.Contains(lowerTrim(base), "openai"):
			kind = KindOpenAI
		default:
			return Candidate{Source: SourceCCSwitch, Name: display, SkipReason: ReasonAmbiguous}
		}
	}
	if !validEndpoint(base) {
		return Candidate{Source: SourceCCSwitch, Name: display, Kind: kind, SkipReason: ReasonNoEndpoint}
	}
	if isProxyTakeover(base, proxies) {
		return Candidate{Source: SourceCCSwitch, Name: display, Kind: kind, BaseURL: base, SkipReason: ReasonProxy}
	}
	style := ""
	if kind == KindAnthropic {
		style = AuthStyleAPIKey
	}
	return Candidate{
		Source: SourceCCSwitch, Name: display, Kind: kind, BaseURL: base,
		AuthStyle: style, Key: key, Models: models,
	}
}
