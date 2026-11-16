package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

type probeFixture struct {
	Version, Help, Features, Config, RunHelp, Agents, Plugins string
	Extra                                                     map[string]string
	VersionExit                                               int
	VersionDelay                                              time.Duration
	StderrHelp                                                bool
}

func TestProbeCLIHelper(t *testing.T) {
	raw := os.Getenv("MOTTY_TEST_PROBE")
	if raw == "" {
		return
	}
	var fixture probeFixture
	if json.Unmarshal([]byte(raw), &fixture) != nil {
		os.Exit(2)
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	joined := strings.Join(args, " ")
	if output, ok := fixture.Extra[joined]; ok {
		fmt.Println(output)
		os.Exit(0)
	}
	switch {
	case len(args) == 1 && args[0] == "--version":
		time.Sleep(fixture.VersionDelay)
		if fixture.VersionExit != 0 {
			os.Exit(fixture.VersionExit)
		}
		fmt.Println(fixture.Version)
	case strings.HasPrefix(joined, "features list"):
		fmt.Println(fixture.Features)
	case strings.Contains(joined, "debug config"):
		fmt.Println(fixture.Config)
	case strings.Contains(joined, "debug agents"):
		fmt.Println(fixture.Agents)
	case strings.Contains(joined, "plugins list"):
		fmt.Println(fixture.Plugins)
	case strings.HasSuffix(joined, "run --help"):
		fmt.Println(fixture.RunHelp)
	case strings.HasSuffix(joined, "--help"):
		if fixture.StderrHelp {
			fmt.Fprintln(os.Stderr, fixture.Help)
		} else {
			fmt.Println(fixture.Help)
		}
	default:
		os.Exit(3)
	}
	os.Exit(0)
}

func probeCommand(t *testing.T, fixture probeFixture) command {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	return command{program: exe, args: []string{"-test.run=^TestProbeCLIHelper$", "--"}, env: []string{"MOTTY_TEST_PROBE=" + string(raw)}, dir: t.TempDir()}
}

func probeFixtureError(t *testing.T, kind string, fixture probeFixture) error {
	t.Helper()
	c := probeCommand(t, fixture)
	return probe(t.Context(), kind, &c)
}

func TestProbeUsesCapabilitiesRatherThanVersionWhitelist(t *testing.T) {
	for _, tc := range []struct {
		kind, version, help string
	}{
		{"codex-cli", "codex-cli 0.162.0", "--ephemeral --json --sandbox"},
		{"codex-cli", "codex-cli 0.163.0", "--ephemeral --json --sandbox"},
		{"codex-cli", "codex-cli 0.165.0", "--ephemeral --json --sandbox"},
		{"codex-cli", "codex-cli 1.0.0", "--ephemeral --json --sandbox"},
		{"claude-code-cli", "2.2.0 (Claude Code)", "--settings --setting-sources --strict-mcp-config --tools --disable-slash-commands --include-partial-messages --no-session-persistence --print --output-format --mcp-config --permission-mode --allowedTools"},
		{"claude-code-cli", "3.0.0 (Claude Code)", "--settings --setting-sources --strict-mcp-config --tools --disable-slash-commands --include-partial-messages --no-session-persistence --print --output-format --mcp-config --permission-mode --allowedTools"},
		{"opencode-cli", "2.0.0", "--pure"},
	} {
		t.Run(tc.kind+"/"+tc.version, func(t *testing.T) {
			fixture := probeFixture{Version: tc.version, Help: tc.help, Features: "shell_tool stable false\njs_repl removed false\nhooks stable false", Config: `{"permission":"deny"}`}
			if err := probeFixtureError(t, tc.kind, fixture); err != nil {
				t.Fatalf("capable CLI rejected: %v", err)
			}
		})
	}
}

func TestProbeRejectsIneffectiveCodexControls(t *testing.T) {
	for _, features := range []string{
		"shell_tool stable true",
		"shell_tool stable false\njs_repl experimental true",
		"shell_tool stable false\nhooks stable true",
		"shell_tool stable false\nnot a feature table",
		"shell_tool stable false\nshell_tool stable true",
	} {
		t.Run(features, func(t *testing.T) {
			fixture := probeFixture{Version: "codex-cli 0.163.0", Help: "--ephemeral --json --sandbox", Features: features}
			if err := probeFixtureError(t, "codex-cli", fixture); err == nil {
				t.Fatal("ineffective isolation controls accepted")
			}
		})
	}
}

func TestProbeToleratesRemovedFeaturesAndHeaderRows(t *testing.T) {
	for _, features := range []string{
		"shell_tool removed false\njs_repl removed false",
		"Feature Stage Enabled\nshell_tool stable false",
		"js_repl removed false",
	} {
		fixture := probeFixture{Version: "codex-cli 0.165.0", Help: "--ephemeral --json --sandbox", Features: features}
		if err := probeFixtureError(t, "codex-cli", fixture); err != nil {
			t.Fatalf("%q rejected: %v", features, err)
		}
	}
}

// A flag named only in a description is prose: the session would fail on it.
func TestProbeIgnoresAFlagMentionedOnlyInHelpProse(t *testing.T) {
	help := "--settings --setting-sources --strict-mcp-config --tools --disable-slash-commands --include-partial-messages --no-session-persistence --print --output-format --permission-mode --allowedTools\n  --strict-mcp-config  reject the --mcp-config flag"
	fixture := probeFixture{Version: "2.2.0 (Claude Code)", Help: help, Features: "shell_tool stable false"}
	err := probeFixtureError(t, "claude-code-cli", fixture)
	if err == nil || !strings.Contains(err.Error(), "--mcp-config") {
		t.Fatalf("prose counted as a capability: %v", err)
	}
}

func TestProbeReportsVersionAndMissingCapabilityWithoutRawOutput(t *testing.T) {
	fixture := probeFixture{Version: "PRIVATE_TOKEN\ncodex-cli 0.165.0", Help: "--json --sandbox"}
	err := probeFixtureError(t, "codex-cli", fixture)
	if err == nil || !strings.Contains(err.Error(), "0.165.0") || !strings.Contains(err.Error(), "--ephemeral") || strings.Contains(err.Error(), "PRIVATE_TOKEN") {
		t.Fatalf("missing safe capability diagnostics: %v", err)
	}
}

func TestProbeStillRequiresOpenCodeAppliedPermission(t *testing.T) {
	fixture := probeFixture{Version: "2.0.0", Help: "--pure", Config: `{"permission":"allow"}`}
	if err := probeFixtureError(t, "opencode-cli", fixture); err == nil || !strings.Contains(err.Error(), "permission=deny") {
		t.Fatalf("permission was not checked: %v", err)
	}
}

const openCodeV2AgentRules = `[{"id":"motty_probe","permissions":[` +
	`{"action":"*","resource":"*","effect":"allow"},` +
	`{"action":"*","resource":"*","effect":"deny"},` +
	`{"action":"motty_probe_allow","resource":"*","effect":"allow"}]}]`

func openCodeV2Fixture(agents string) probeFixture {
	return probeFixture{
		Version: "opencode v2.3.0",
		Help:    "--standalone --server",
		RunHelp: "--format --agent --file --title --thinking --standalone",
		Agents:  agents,
	}
}

func TestProbeAcceptsOpenCodeV2ByPrivateServerCapability(t *testing.T) {
	c := probeCommand(t, openCodeV2Fixture(openCodeV2AgentRules))
	if err := probe(t.Context(), "opencode-cli", &c); err != nil {
		t.Fatalf("capable V2 build rejected: %v", err)
	}
	if c.flavor != openCodeV2 || c.version != "2.3.0" {
		t.Fatalf("V2 profile not selected: flavor=%q version=%q", c.flavor, c.version)
	}
}

func TestProbeKeepsOpenCodeV1ProfileWhenPureIsPresent(t *testing.T) {
	fixture := probeFixture{Version: "1.19.5", Help: "--pure --standalone", Config: `{"permission":"deny"}`}
	c := probeCommand(t, fixture)
	if err := probe(t.Context(), "opencode-cli", &c); err != nil {
		t.Fatal(err)
	}
	if c.flavor != openCodeV1 {
		t.Fatalf("V1 profile displaced by V2 signal: %q", c.flavor)
	}
}

func TestProbeReadsHelpFromStderr(t *testing.T) {
	fixture := probeFixture{Version: "1.19.5", Help: "--pure", Config: `{"permission":"deny"}`, StderrHelp: true}
	c := probeCommand(t, fixture)
	if err := probe(t.Context(), "opencode-cli", &c); err != nil {
		t.Fatalf("stderr help not read: %v", err)
	}
	if c.flavor != openCodeV1 {
		t.Fatalf("flavor = %q", c.flavor)
	}
}

func TestProbeRejectsOpenCodeV2WithoutRequiredRunCapabilities(t *testing.T) {
	fixture := openCodeV2Fixture(openCodeV2AgentRules)
	fixture.RunHelp = "--format --agent --file"
	err := probeFixtureError(t, "opencode-cli", fixture)
	if err == nil || !strings.Contains(err.Error(), "run --title") {
		t.Fatalf("incomplete V2 run surface accepted: %v", err)
	}
}

func TestProbeRejectsOpenCodeV2WithoutAppliedPermissions(t *testing.T) {
	for _, tc := range []struct {
		name, agents, want string
	}{
		{"missing agent", `[]`, "agent=motty_probe"},
		{"builtin allowed", `[{"id":"motty_probe","permissions":[` +
			`{"action":"*","resource":"*","effect":"deny"},` +
			`{"action":"read","resource":"*","effect":"allow"},` +
			`{"action":"motty_probe_allow","resource":"*","effect":"allow"}]}]`, "read=deny"},
		{"allow rule not applied", `[{"id":"motty_probe","permissions":[` +
			`{"action":"*","resource":"*","effect":"deny"}]}]`, "motty_probe_allow=allow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := probeFixtureError(t, "opencode-cli", openCodeV2Fixture(tc.agents))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unapplied V2 permission accepted: %v", err)
			}
		})
	}
}

func TestProbeAcceptsQoderWithScopedPluginListing(t *testing.T) {
	fixture := probeFixture{
		Version: "1.1.89",
		Help: "--print --output-format --permission-mode --allowed-tools --mcp-config --strict-mcp-config " +
			"--setting-sources --settings --no-session-persistence --tools --attachment --model",
		Plugins: "[]",
	}
	c := probeCommand(t, fixture)
	if err := probe(t.Context(), "qoder-cli", &c); err != nil {
		t.Fatalf("capable Qoder build rejected: %v", err)
	}
	if c.version != "1.1.89" {
		t.Fatalf("version = %q", c.version)
	}
}

func TestProbeRejectsQoderPluginListWithoutIDs(t *testing.T) {
	fixture := probeFixture{
		Version: "1.1.89",
		Help: "--print --output-format --permission-mode --allowed-tools --mcp-config --strict-mcp-config " +
			"--setting-sources --settings --no-session-persistence --tools --attachment --model",
		Plugins: "not json",
	}
	if err := probeFixtureError(t, "qoder-cli", fixture); err == nil || !strings.Contains(err.Error(), "invalid_output") {
		t.Fatalf("invalid plugin list accepted: %v", err)
	}
}

func TestProbeRejectsQoderWithoutCLIIsolationFlags(t *testing.T) {
	fixture := probeFixture{Version: "1.1.89", Help: "--print --output-format", Plugins: "[]"}
	if err := probeFixtureError(t, "qoder-cli", fixture); err == nil || !strings.Contains(err.Error(), "missing_capability") {
		t.Fatalf("incomplete Qoder isolation surface accepted: %v", err)
	}
}

func TestOpenCodePermissionRulesUseLastMatch(t *testing.T) {
	rules := []openCodePermissionRule{
		{Action: "*", Resource: "*", Effect: "allow"},
		{Action: "*", Resource: "*", Effect: "deny"},
		{Action: "motty_*", Resource: "*", Effect: "allow"},
		{Action: "motty_secret", Resource: "*", Effect: "deny"},
	}
	for _, tc := range []struct{ action, want string }{
		{"read", "deny"},
		{"shell", "deny"},
		{"motty_search", "allow"},
		{"motty_secret", "deny"},
	} {
		if got := openCodePermissionEffect(rules, tc.action, "*"); got != tc.want {
			t.Errorf("%s: effect=%q want %q", tc.action, got, tc.want)
		}
	}
	for _, tc := range []struct {
		pattern, value string
		want           bool
	}{
		{"*", "read", true},
		{"*", "a/b", true},
		{"motty_*", "motty_x", true},
		{"motty_?", "motty_x", true},
		{"motty_?", "motty_xy", false},
		{"read", "write", false},
	} {
		if got := openCodeWildcard(tc.pattern, tc.value); got != tc.want {
			t.Errorf("wildcard(%q, %q)=%t want %t", tc.pattern, tc.value, got, tc.want)
		}
	}
}

func TestOpenCodeV2ForeignServersReadsBothConfigShapes(t *testing.T) {
	raw := []byte(`[
	  {"type":"document","info":{"mcp":{"servers":{"native":{"type":"remote","url":"http://x/"}},
	    "timeout":{"execution":1000}}}},
	  {"type":"directory"},
	  {"type":"document","info":{"mcp":{"legacy":{"type":"local","command":["x"]}}}}
	]`)
	names, ok := openCodeV2ForeignServers(raw)
	if !ok || strings.Join(names, ",") != "legacy,native" {
		t.Fatalf("foreign servers = %v, %t", names, ok)
	}
	if _, ok := openCodeV2ForeignServers([]byte(`{"not":"an array"}`)); ok {
		t.Fatal("non-array config listing accepted")
	}
}

func TestProbeDoesNotMistakeSimilarFlagForRequiredCapability(t *testing.T) {
	fixture := probeFixture{Version: "codex-cli 0.163.0", Help: "--ephemeral-extra --json --sandbox", Features: "shell_tool stable false"}
	if err := probeFixtureError(t, "codex-cli", fixture); err == nil || !strings.Contains(err.Error(), "--ephemeral") {
		t.Fatalf("missing exact flag accepted: %v", err)
	}
}

func TestProbeDoesNotEchoUnrecognizedVersionOutput(t *testing.T) {
	fixture := probeFixture{Version: "PRIVATE_TOKEN invalid output", Help: "--json --sandbox"}
	err := probeFixtureError(t, "codex-cli", fixture)
	if err == nil || !strings.Contains(err.Error(), "--ephemeral") || strings.Contains(err.Error(), "PRIVATE_TOKEN") {
		t.Fatalf("capability check has no safe diagnostic: %v", err)
	}
}

func TestVersionMetadataDoesNotGateCapableCLI(t *testing.T) {
	for _, fixture := range []probeFixture{
		{Version: "development build", Help: "--ephemeral --json --sandbox", Features: "shell_tool stable false"},
		{VersionExit: 7, Help: "--ephemeral --json --sandbox", Features: "shell_tool stable false"},
	} {
		if err := probeFixtureError(t, "codex-cli", fixture); err != nil {
			t.Fatalf("version metadata blocked capability detection: %v", err)
		}
	}
}

func TestSharedProbeRunsNewAdapterDeclarationsWithoutRuntimeBranches(t *testing.T) {
	for _, policy := range []string{"deny", "allow"} {
		t.Run(policy, func(t *testing.T) {
			fixture := probeFixture{Extra: map[string]string{"about": "custom-agent 23.4.5", "usage": "--isolated", "policy": policy}}
			c := probeCommand(t, fixture)
			a := adapter{
				program: "custom-agent", version: []string{"about"}, help: []string{"usage"}, required: []string{"--isolated"},
				checks: []capabilityCheck{{label: "policy", args: []string{"policy"}, validate: func(raw []byte) checkFailure {
					if strings.TrimSpace(string(raw)) != "deny" {
						return checkFailure{reason: "not_applied", detail: "native-execution=deny"}
					}
					return checkFailure{}
				}}},
			}
			err := probeAdapter(t.Context(), a, &c)
			if c.version != "23.4.5" || (err == nil) != (policy == "deny") {
				t.Fatalf("adapter declaration ignored: version=%q error=%v", c.version, err)
			}
			if err != nil {
				diagnostic, ok := ParseCompatibilityDiagnostic(err.Error())
				if !ok || diagnostic.Program != "custom-agent" || diagnostic.Command != "policy" || diagnostic.Detail != "native-execution=deny" {
					t.Fatalf("generic diagnostics lost adapter metadata: %v", err)
				}
			}
		})
	}
}

func TestSlowOptionalVersionDoesNotConsumeCapabilityBudget(t *testing.T) {
	fixture := probeFixture{VersionDelay: time.Minute, Help: "--ephemeral --json --sandbox", Features: "shell_tool stable false"}
	c := probeCommand(t, fixture)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := probe(ctx, "codex-cli", &c); err != nil {
		t.Fatalf("optional version lookup blocked capable CLI: %v", err)
	}
}

func TestProbePreservesParentCancellationAndDeadline(t *testing.T) {
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	expired, stop := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer stop()
	for _, ctx := range []context.Context{canceled, expired} {
		c := probeCommand(t, probeFixture{})
		if err := probe(ctx, "codex-cli", &c); !errors.Is(err, ctx.Err()) {
			t.Fatalf("lost parent context error %v: %v", ctx.Err(), err)
		}
	}
}
