package tools

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestOrderAndLookup(t *testing.T) {
	want := []string{NameListSessions, NameTerminalRead, NameTerminalExec, NameTerminalExecMany, NameSFTPList, NameSFTPRead, NameSFTPWrite, NameSFTPRemove}
	var got []string
	for _, s := range All() {
		got = append(got, s.Name)
		if s.Description == "" || len(s.Description) > 400 {
			t.Errorf("%s: description length %d", s.Name, len(s.Description))
		}
		l, ok := Lookup(s.Name)
		if !ok || l.Name != s.Name {
			t.Errorf("lookup %s", s.Name)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("order %v", got)
	}
	if _, ok := Lookup("nope"); ok {
		t.Fatal("found nope")
	}
	All()[0].Name = "x" // copy
	if All()[0].Name != NameListSessions {
		t.Fatal("All must return a copy")
	}
}

func jsonNames(t *testing.T, v any) []string {
	t.Helper()
	rt := reflect.TypeOf(v)
	var names []string
	for i := 0; i < rt.NumField(); i++ {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if name == "" {
			t.Fatalf("%s.%s has no json tag", rt.Name(), rt.Field(i).Name)
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func TestSchemasMatchArgs(t *testing.T) {
	args := map[string]any{
		NameListSessions: ListSessionsArgs{}, NameTerminalRead: TerminalReadArgs{}, NameTerminalExec: TerminalExecArgs{}, NameTerminalExecMany: TerminalExecManyArgs{},
		NameSFTPList: SFTPListArgs{}, NameSFTPRead: SFTPReadArgs{}, NameSFTPWrite: SFTPWriteArgs{}, NameSFTPRemove: SFTPRemoveArgs{},
	}
	required := map[string][]string{
		NameTerminalExec: {"command"}, NameTerminalExecMany: {"command", "sessions"}, NameSFTPList: {"path"}, NameSFTPRead: {"path"},
		NameSFTPWrite: {"content", "path"}, NameSFTPRemove: {"path"},
	}
	for _, s := range All() {
		var sc struct {
			Type                 string                     `json:"type"`
			Properties           map[string]json.RawMessage `json:"properties"`
			Required             []string                   `json:"required"`
			AdditionalProperties *bool                      `json:"additionalProperties"`
		}
		if !json.Valid(s.Schema) {
			t.Fatalf("%s: invalid schema", s.Name)
		}
		if err := json.Unmarshal(s.Schema, &sc); err != nil {
			t.Fatal(err)
		}
		if sc.Type != "object" || sc.AdditionalProperties == nil || *sc.AdditionalProperties {
			t.Errorf("%s: schema must be a closed object", s.Name)
		}
		var props []string
		for p := range sc.Properties {
			props = append(props, p)
		}
		slices.Sort(props)
		if want := jsonNames(t, args[s.Name]); !slices.Equal(props, want) {
			t.Errorf("%s: schema properties %v, struct %v", s.Name, props, want)
		}
		slices.Sort(sc.Required)
		if !slices.Equal(sc.Required, required[s.Name]) {
			t.Errorf("%s: required %v", s.Name, sc.Required)
		}
		for _, r := range sc.Required {
			if !slices.Contains(props, r) {
				t.Errorf("%s: required %s not a property", s.Name, r)
			}
		}
	}
}

func TestDecoders(t *testing.T) {
	big := func(n int) string { return strings.Repeat("a", n) }
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	type tc struct {
		name, in string
		fn       func([]byte) (any, error)
		want     any
		bad      bool
	}
	ls := func(b []byte) (any, error) { return DecodeListSessions(b) }
	tr := func(b []byte) (any, error) { return DecodeTerminalRead(b) }
	te := func(b []byte) (any, error) { return DecodeTerminalExec(b) }
	sl := func(b []byte) (any, error) { return DecodeSFTPList(b) }
	sr := func(b []byte) (any, error) { return DecodeSFTPRead(b) }
	sw := func(b []byte) (any, error) { return DecodeSFTPWrite(b) }
	rm := func(b []byte) (any, error) { return DecodeSFTPRemove(b) }
	tests := []tc{
		{"ls empty", ``, ls, ListSessionsArgs{}, false},
		{"ls null", `null`, ls, ListSessionsArgs{}, false},
		{"ls object", `{}`, ls, ListSessionsArgs{}, false},
		{"ls unknown", `{"a":1}`, ls, nil, true},
		{"ls array", `[]`, ls, nil, true},

		{"tr defaults", `{}`, tr, TerminalReadArgs{Lines: 60, Scope: "screen"}, false},
		{"tr full", `{"session":"s1","lines":500,"scope":"scrollback"}`, tr, TerminalReadArgs{"s1", 500, "scrollback"}, false},
		{"tr lines 0", `{"lines":0}`, tr, nil, true},
		{"tr lines 501", `{"lines":501}`, tr, nil, true},
		{"tr lines float", `{"lines":1.5}`, tr, nil, true},
		{"tr lines string", `{"lines":"5"}`, tr, nil, true},
		{"tr scope", `{"scope":"all"}`, tr, nil, true},
		{"tr unknown field", `{"lines":5,"x":1}`, tr, nil, true},
		{"tr session type", `{"session":5}`, tr, nil, true},
		{"tr session long", `{"session":` + q(big(257)) + `}`, tr, nil, true},
		{"tr session ctl", `{"session":"a\nb"}`, tr, nil, true},
		{"tr trailing", `{} {}`, tr, nil, true},
		{"tr malformed", `{"lines":`, tr, nil, true},

		{"te ok", `{"command":"ls -l"}`, te, TerminalExecArgs{Command: "ls -l", TimeoutSeconds: 60}, false},
		{"te multi", `{"command":"a\nb","timeout_seconds":600,"session":"x"}`, te, TerminalExecArgs{"x", "a\nb", 600}, false},
		{"te missing", `{}`, te, nil, true},
		{"te empty", `{"command":""}`, te, nil, true},
		{"te blank", `{"command":"  \n"}`, te, nil, true},
		{"te null", `{"command":null}`, te, nil, true},
		{"te nul", `{"command":"a\u0000b"}`, te, nil, true},
		{"te max", `{"command":` + q(big(8192)) + `}`, te, TerminalExecArgs{Command: big(8192), TimeoutSeconds: 60}, false},
		{"te too long", `{"command":` + q(big(8193)) + `}`, te, nil, true},
		{"te timeout 0", `{"command":"a","timeout_seconds":0}`, te, nil, true},
		{"te timeout 601", `{"command":"a","timeout_seconds":601}`, te, nil, true},
		{"te timeout neg", `{"command":"a","timeout_seconds":-1}`, te, nil, true},
		{"te wrong type", `{"command":["a"]}`, te, nil, true},

		{"sl ok", `{"path":"/etc"}`, sl, SFTPListArgs{Path: "/etc"}, false},
		{"sl missing", `{}`, sl, nil, true},
		{"sl empty", `{"path":""}`, sl, nil, true},
		{"sl long", `{"path":` + q(big(4097)) + `}`, sl, nil, true},
		{"sl nul", `{"path":"/a\u0000"}`, sl, nil, true},

		{"sr default", `{"path":"/f"}`, sr, SFTPReadArgs{Path: "/f", MaxBytes: 262144}, false},
		{"sr max", `{"path":"/f","max_bytes":262144}`, sr, SFTPReadArgs{Path: "/f", MaxBytes: 262144}, false},
		{"sr over", `{"path":"/f","max_bytes":262145}`, sr, nil, true},
		{"sr zero", `{"path":"/f","max_bytes":0}`, sr, nil, true},
		{"sr huge", `{"path":"/f","max_bytes":99999999999999999999}`, sr, nil, true},

		{"sw ok", `{"path":"/f","content":"hi"}`, sw, SFTPWriteArgs{Path: "/f", Content: "hi"}, false},
		{"sw empty content", `{"path":"/f","content":"","overwrite":true}`, sw, SFTPWriteArgs{Path: "/f", Overwrite: true}, false},
		{"sw no content", `{"path":"/f"}`, sw, nil, true},
		{"sw overwrite type", `{"path":"/f","content":"x","overwrite":"yes"}`, sw, nil, true},
		{"sw max", `{"path":"/f","content":` + q(big(262144)) + `}`, sw, SFTPWriteArgs{Path: "/f", Content: big(262144)}, false},
		{"sw over", `{"path":"/f","content":` + q(big(262145)) + `}`, sw, nil, true},

		{"rm ok", `{"path":"/f","session":"s"}`, rm, SFTPRemoveArgs{"s", "/f"}, false},
		{"rm missing", `{"session":"s"}`, rm, nil, true},
		{"rm unknown", `{"path":"/f","recursive":true}`, rm, nil, true},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.fn([]byte(c.in))
			if c.bad {
				if err == nil {
					t.Fatalf("accepted: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %+v want %+v", got, c.want)
			}
		})
	}
}

func TestDecodeTerminalExecManyNeedsTwoToEightDistinctSessionsAndACommand(t *testing.T) {
	good, err := DecodeTerminalExecMany([]byte(`{"sessions":["w1:1","w1:2"],"command":"uptime"}`))
	if err != nil || len(good.Sessions) != 2 || good.Command != "uptime" || good.TimeoutSeconds != DefaultTimeoutSeconds {
		t.Fatalf("%+v, %v", good, err)
	}
	many := make([]string, MaxBatchSessions+1)
	for index := range many {
		many[index] = "w1:" + strings.Repeat("x", index+1)
	}
	tooMany, _ := json.Marshal(map[string]any{"sessions": many, "command": "uptime"})
	for name, raw := range map[string]string{
		"one session":       `{"sessions":["w1:1"],"command":"uptime"}`,
		"no sessions":       `{"command":"uptime"}`,
		"duplicate":         `{"sessions":["w1:1","w1:1"],"command":"uptime"}`,
		"an empty id":       `{"sessions":["w1:1",""],"command":"uptime"}`,
		"no command":        `{"sessions":["w1:1","w1:2"]}`,
		"a blank command":   `{"sessions":["w1:1","w1:2"],"command":"  "}`,
		"too many":          string(tooMany),
		"a bad timeout":     `{"sessions":["w1:1","w1:2"],"command":"x","timeout_seconds":601}`,
		"an unknown field":  `{"sessions":["w1:1","w1:2"],"command":"x","all":true}`,
		"a control char id": "{\"sessions\":[\"w1:1\",\"w1\\u0007\"],\"command\":\"x\"}",
	} {
		if _, err := DecodeTerminalExecMany([]byte(raw)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestEscalationToolIsAskOnlyAndDecodes(t *testing.T) {
	spec := RequestAgentModeSpec()
	if spec.Name != NameRequestAgentMode || spec.Kind != Read || !json.Valid(spec.Schema) {
		t.Fatalf("spec = %+v", spec)
	}
	if _, ok := Lookup(NameRequestAgentMode); ok {
		t.Fatal("the escalation tool leaked into the normal registry")
	}
	for _, entry := range All() {
		if entry.Name == NameRequestAgentMode {
			t.Fatal("the escalation tool leaked into All")
		}
	}
	args, err := DecodeRequestAgentMode([]byte(`{"reason":"  needs a shell  ","command":" ls "}`))
	if err != nil || args.Reason != "needs a shell" || args.Command != "ls" {
		t.Fatalf("decoded = %+v, %v", args, err)
	}
	if _, err := DecodeRequestAgentMode([]byte(`{"reason":"` + strings.Repeat("x", MaxEscalationReason+1) + `"}`)); err == nil {
		t.Fatal("an oversized reason was accepted")
	}
	if _, err := DecodeRequestAgentMode([]byte(`{"other":1}`)); err == nil {
		t.Fatal("an unknown field was accepted")
	}
}

func TestHeadlessCatalogDropsTheScreenAndStopsPromisingATerminal(t *testing.T) {
	names := func(commands bool) []string {
		var names []string
		for _, spec := range Headless(commands, false) {
			names = append(names, spec.Name)
			if len(spec.Description) == 0 || len(spec.Description) > 400 {
				t.Errorf("%s: description length %d", spec.Name, len(spec.Description))
			}
			if !json.Valid(spec.Schema) {
				t.Errorf("%s: schema is not JSON", spec.Name)
			}
			full, _ := Lookup(spec.Name)
			if spec.Kind != full.Kind {
				t.Errorf("%s: kind %v, want %v", spec.Name, spec.Kind, full.Kind)
			}
			for _, text := range []string{spec.Description, string(spec.Schema)} {
				for _, stale := range []string{"visible terminal", "user sees", "active session", "that is ready", "must approve"} {
					if strings.Contains(text, stale) {
						t.Errorf("%s still says %q: %s", spec.Name, stale, text)
					}
				}
			}
		}
		return names
	}
	if got, want := names(false), []string{NameListSessions, NameSFTPList, NameSFTPRead}; !slices.Equal(got, want) {
		t.Errorf("read-only headless tools %v, want %v", got, want)
	}
	if got, want := names(true), []string{NameListSessions, NameTerminalExec, NameTerminalExecMany, NameSFTPList, NameSFTPRead}; !slices.Equal(got, want) {
		t.Errorf("headless tools with commands %v, want %v", got, want)
	}
	if _, ok := Lookup(NameTerminalRead); !ok {
		t.Fatal("the full catalog must keep terminal_read")
	}
}

func TestHeadlessFullAccessAddsFileChangesWithoutInventingATerminal(t *testing.T) {
	full := Headless(true, true)
	if len(full) != len(All())-1 {
		t.Fatal("full access did not expose every tool that works without a terminal")
	}
	var writes int
	for _, spec := range full {
		if spec.Name == NameTerminalRead {
			t.Fatal("full access invented a visible terminal")
		}
		if spec.Kind == Write {
			writes++
			if strings.Contains(spec.Description, "must approve") || !json.Valid(spec.Schema) {
				t.Fatalf("invalid full-access file tool: %+v", spec)
			}
		}
	}
	if writes != 2 {
		t.Fatal("write and remove were not both offered")
	}
}
