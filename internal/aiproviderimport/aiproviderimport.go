// Package aiproviderimport reads provider configurations from magpie and
// cc-switch without modifying either application's data.
//
// The package is deliberately pure: it opens only the file it is given, never
// touches the network, the vault or the window, and returns source-neutral
// candidates for the workbench to preview and write. api keys travel in
// Candidate.Key for the caller to move into the vault; nothing here logs or
// formats them.
package aiproviderimport

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Source says where a candidate came from.
type Source string

const (
	SourceMagpieUpstream Source = "magpie-upstream"
	SourceMagpieGateway  Source = "magpie-gateway"
	SourceCCSwitch       Source = "cc-switch"
)

// Tool names the application whose data a source is.
type Tool string

const (
	ToolMagpie   Tool = "magpie"
	ToolCCSwitch Tool = "cc-switch"
)

// Provider kinds and authentication styles mirror the assistant's stored
// values. They are repeated here so the parser stays independent of the AI
// database package.
const (
	KindOpenAI    = "openai"
	KindAnthropic = "anthropic"

	AuthStyleAPIKey = "api_key"
	AuthStyleBearer = "bearer"
)

// Skip reasons are stable codes; the workbench translates them.
const (
	ReasonDisabled            = "disabled"
	ReasonNoEndpoint          = "no-endpoint"
	ReasonUnsupportedProtocol = "unsupported-protocol"
	ReasonResponses           = "responses"
	ReasonOAuth               = "oauth"
	ReasonGemini              = "gemini-native"
	ReasonProxy               = "proxy-takeover"
	ReasonUnsupportedApp      = "unsupported-app"
	ReasonAmbiguous           = "ambiguous"
	ReasonNoChat              = "no-chat-completions"
	ReasonUnsupportedAuth     = "unsupported-auth"
	ReasonMalformed           = "malformed"
)

// Notes are stable codes for non-blocking information.
const (
	NoteMagpieGateway = "magpie-must-run"
)

// Bounds on untrusted input. They are generous for real provider files and
// small enough that a hostile file cannot make the import allocate without
// limit.
const (
	MaxJSONBytes   = 8 << 20 // one JSON source
	MaxSQLiteBytes = 256 << 20

	MaxProviders = 1000
	MaxModels    = 2000
	MaxNameRunes = 64
	MaxModelRune = 200
	MaxURLRunes  = 2048
)

var (
	// ErrMalformed is returned for content that is not the expected source.
	ErrMalformed = errors.New("aiproviderimport: malformed source")
	// ErrTooLarge is returned when a file or list exceeds a bound.
	ErrTooLarge = errors.New("aiproviderimport: source exceeds a limit")
	// ErrUnknownSource is returned when a chosen file is neither magpie nor
	// cc-switch data.
	ErrUnknownSource = errors.New("aiproviderimport: unknown source")
)

// Candidate is one provider the import can offer.
type Candidate struct {
	Source     Source
	Name       string
	Kind       string
	BaseURL    string
	AuthStyle  string
	Key        string
	Models     []string
	Note       string // a Note* code, or empty
	SkipReason string // a Reason* code, or empty when importable
}

// Importable reports whether the candidate may be written.
func (c Candidate) Importable() bool { return c.SkipReason == "" }

// HasKey says whether the source supplied a credential.
func (c Candidate) HasKey() bool { return c.Key != "" }

// String and GoString keep a candidate's credential out of a log line or a
// failed assertion, the same way store.Provider does.
func (c Candidate) String() string   { return c.redacted() }
func (c Candidate) GoString() string { return c.redacted() }

func (c Candidate) redacted() string {
	key := ""
	if c.Key != "" {
		key = "<redacted>"
	}
	return fmt.Sprintf("Candidate{Source:%q Name:%q Kind:%q BaseURL:%q AuthStyle:%q Key:%q Models:%v Note:%q SkipReason:%q}",
		c.Source, c.Name, c.Kind, c.BaseURL, c.AuthStyle, key, c.Models, c.Note, c.SkipReason)
}

// SourceInfo is one application's discovered data.
type SourceInfo struct {
	Tool       Tool
	Path       string
	Found      bool
	Candidates []Candidate
	Err        error
}

// Available reports whether the source was found and read.
func (s SourceInfo) Available() bool { return s.Found && s.Err == nil }

// Importable counts the candidates that can be written.
func (s SourceInfo) Importable() int {
	n := 0
	for _, candidate := range s.Candidates {
		if candidate.Importable() {
			n++
		}
	}
	return n
}

// Scan looks for magpie and cc-switch at their standard locations. It is
// called only after the user asks to import; it performs local reads and no
// network requests.
func Scan() []SourceInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return scan(home, os.Getenv("XDG_CONFIG_HOME"), runtime.GOOS)
}

func scan(home, xdg, goos string) []SourceInfo {
	if !filepath.IsAbs(home) {
		home = ""
	}
	magpie := SourceInfo{Tool: ToolMagpie, Path: magpiePath(home, xdg, goos)}
	if magpie.Path != "" {
		if info, err := os.Stat(magpie.Path); err == nil && !info.IsDir() {
			magpie.Found = true
			magpie.Candidates, magpie.Err = ParseMagpieFile(magpie.Path)
		}
	}

	cc := SourceInfo{Tool: ToolCCSwitch}
	if home == "" {
		return []SourceInfo{magpie, cc}
	}
	dbPath := filepath.Join(home, ".cc-switch", "cc-switch.db")
	legacyPath := filepath.Join(home, ".cc-switch", "config.json")
	if info, err := os.Stat(dbPath); err == nil && !info.IsDir() {
		cc.Found, cc.Path = true, dbPath
		cc.Candidates, cc.Err = ParseCCSwitchSQLite(dbPath)
		if cc.Err != nil {
			// A database from a version we cannot read may still sit beside a
			// legacy file. Use it rather than reporting the source unavailable.
			if info, legacyErr := os.Stat(legacyPath); legacyErr == nil && !info.IsDir() {
				if candidates, err := ParseCCSwitchLegacyFile(legacyPath); err == nil {
					cc.Path, cc.Candidates, cc.Err = legacyPath, candidates, nil
				}
			}
		}
	} else if info, err := os.Stat(legacyPath); err == nil && !info.IsDir() {
		cc.Found, cc.Path = true, legacyPath
		cc.Candidates, cc.Err = ParseCCSwitchLegacyFile(legacyPath)
	}
	return []SourceInfo{magpie, cc}
}

// magpiePath is magpie's own app-directory rule. Unix honours an absolute
// XDG_CONFIG_HOME; Windows uses the user home, matching the locations explored
// for this change. A relative XDG path and a missing home are ignored rather
// than resolved against the process's working directory.
func magpiePath(home, xdg, goos string) string {
	if goos != "windows" && filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "magpie", "providers.json")
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "magpie", "providers.json")
}

// ParseFile identifies a chosen file by content and parses it.
func ParseFile(path string) ([]Candidate, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%w: %s is a directory", ErrMalformed, filepath.Base(path))
	}
	if info.Size() > MaxSQLiteBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrTooLarge, info.Size())
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 16)
	n, err := io.ReadFull(f, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		f.Close()
		return nil, err
	}
	f.Close()
	if n == 16 && string(header) == "SQLite format 3\x00" {
		return ParseCCSwitchSQLite(path)
	}
	data, err := readFileBounded(path, MaxJSONBytes)
	if err != nil {
		return nil, err
	}
	switch identifyJSON(data) {
	case ToolMagpie:
		return ParseMagpieBytes(data)
	case ToolCCSwitch:
		return ParseCCSwitchLegacy(data)
	}
	return nil, ErrUnknownSource
}

func readFileBounded(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readBounded(f, max)
}

func readBounded(r io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%w: more than %d bytes", ErrTooLarge, max)
	}
	return data, nil
}

func identifyJSON(data []byte) Tool {
	root, err := jsonObject(data)
	if err != nil || root == nil {
		return ""
	}
	if raw, ok := root["providers"]; ok {
		// magpie holds an array; cc-switch v1 held an object, with or without
		// a current id. The shape alone is enough to identify the source.
		if firstJSONByte(raw) == '[' {
			return ToolMagpie
		}
		return ToolCCSwitch
	}
	if _, ok := root["apps"]; ok {
		return ToolCCSwitch
	}
	for _, app := range []string{"claude", "codex", "gemini", "opencode"} {
		if raw, ok := root[app]; ok {
			if firstJSONByte(raw) == '{' {
				return ToolCCSwitch
			}
		}
	}
	return ""
}

func firstJSONByte(raw []byte) byte {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return b
		}
	}
	return 0
}

// CleanName strips control characters and bounds a display name.
func CleanName(name string) string {
	return boundText(name, MaxNameRunes)
}

// CleanModel strips control characters and bounds one model identifier.
func CleanModel(name string) string {
	return boundText(name, MaxModelRune)
}

func boundText(text string, maxRunes int) string {
	text = strings.ToValidUTF8(text, "")
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > maxRunes {
		runes := []rune(text)
		text = string(runes[:maxRunes])
	}
	return text
}

func cleanModels(names []string) []string {
	out := make([]string, 0, min(len(names), MaxModels))
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		name = CleanModel(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
		if len(out) >= MaxModels {
			break
		}
	}
	return out
}

func validEndpoint(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || utf8.RuneCountInString(raw) > MaxURLRunes {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return false
	}
	return true
}

func endpointKey(raw string) (host, port string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", ""
	}
	port = u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return strings.ToLower(u.Hostname()), port
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func lowerTrim(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
