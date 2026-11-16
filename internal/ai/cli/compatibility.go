package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type adapter struct {
	program  string
	flavor   string
	required []string
	version  []string
	help     []string
	checks   []capabilityCheck
	// alternate is the same program's other capability profile. It is tried
	// only when none of required's flags are present, so a program that keeps
	// its old profile stays on the primary one.
	alternate *adapter
}

type capabilityCheck struct {
	label    string
	args     []string
	env      []string
	isolate  bool
	validate func([]byte) checkFailure
}

type checkFailure struct{ reason, detail string }

// CompatibilityDiagnostic contains only probe metadata, never subprocess output
// or credential-bearing arguments. It survives the agent's string event channel.
type CompatibilityDiagnostic struct {
	Program string `json:"program"`
	Version string `json:"version,omitempty"`
	Reason  string `json:"reason"`
	Detail  string `json:"detail,omitempty"`
	Command string `json:"command"`
}

func (d CompatibilityDiagnostic) Error() string {
	raw, _ := json.Marshal(d)
	return "cli: compatibility: " + string(raw)
}

func ParseCompatibilityDiagnostic(message string) (CompatibilityDiagnostic, bool) {
	raw, ok := strings.CutPrefix(message, "cli: compatibility: ")
	var diagnostic CompatibilityDiagnostic
	if !ok || json.Unmarshal([]byte(raw), &diagnostic) != nil {
		return diagnostic, false
	}
	return diagnostic, diagnostic.Program != "" && diagnostic.Reason != "" && diagnostic.Command != ""
}

func compatibilityFailure(program string, c command, label, reason, detail string) error {
	return CompatibilityDiagnostic{Program: program, Version: c.version, Reason: reason, Detail: detail, Command: label}
}

var cliVersionPattern = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+\b`)

// helpFlags collects the flags a --help page lists. Only the leading column is
// read: a name a description happens to mention is prose, not a capability.
func helpFlags(raw []byte) map[string]bool {
	flags := make(map[string]bool)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "-") {
			continue
		}
		if cut := strings.Index(line, "  "); cut >= 0 {
			line = line[:cut]
		}
		for _, field := range strings.Fields(line) {
			name, _, _ := strings.Cut(strings.Trim(field, ","), "=")
			if strings.HasPrefix(name, "-") {
				flags[name] = true
			}
		}
	}
	return flags
}

func probe(ctx context.Context, kind string, c *command) error {
	a, ok := adapters[kind]
	if !ok {
		return errors.New("cli: unsupported_provider")
	}
	return probeAdapter(ctx, a, c)
}

func probeAdapter(ctx context.Context, a adapter, c *command) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	run := func(runCtx context.Context, args, env []string, limit int) ([]byte, error) {
		check := *c
		check.args = append(append([]string{}, c.args...), args...)
		check.env = append(append([]string{}, c.env...), env...)
		return capture(runCtx, check, limit)
	}
	versionArgs := a.version
	if len(versionArgs) == 0 {
		versionArgs = []string{"--version"}
	}
	c.version = ""
	versionCtx, stopVersion := context.WithTimeout(ctx, 2*time.Second)
	rawVersion, err := run(versionCtx, versionArgs, nil, 64<<10)
	stopVersion()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Version is diagnostic metadata, not an admission check. Unrecognized
	// banners and unavailable version commands do not bypass capability checks.
	if err == nil {
		c.version = string(cliVersionPattern.Find(rawVersion))
	}
	helpArgs := a.help
	if len(helpArgs) == 0 {
		helpArgs = []string{"--help"}
	}
	raw, err := run(ctx, helpArgs, nil, 2<<20)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return compatibilityFailure(a.program, *c, strings.Join(helpArgs, " "), "unavailable", "")
	}
	flags := helpFlags(raw)
	if missing := missingFlag(flags, a.required); missing != "" {
		if a.alternate == nil {
			return compatibilityFailure(a.program, *c, strings.Join(helpArgs, " "), "missing_capability", missing)
		}
		if altMissing := missingFlag(flags, a.alternate.required); altMissing != "" {
			return compatibilityFailure(a.program, *c, strings.Join(helpArgs, " "), "missing_capability", missing)
		}
		a = *a.alternate
	}
	c.flavor = a.flavor
	for _, check := range a.checks {
		env := check.env
		if check.isolate {
			home, err := os.MkdirTemp("", "motty-cli-probe-")
			if err != nil {
				return errors.New("cli: runtime_directory_failed")
			}
			defer os.RemoveAll(home)
			env = append(env, "HOME="+home, "USERPROFILE="+home,
				"OPENCODE_CONFIG_DIR="+filepath.Join(home, ".config", "opencode"),
				"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
				"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
				"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
				"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"))
		}
		raw, err := run(ctx, check.args, env, 4<<20)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return compatibilityFailure(a.program, *c, check.label, "unavailable", "")
		}
		if failure := check.validate(raw); failure.reason != "" {
			return compatibilityFailure(a.program, *c, check.label, failure.reason, failure.detail)
		}
	}
	return nil
}

// missingFlag reports the first declared capability the help page does not list.
func missingFlag(flags map[string]bool, required []string) string {
	for _, flag := range required {
		if !flags[flag] {
			return flag
		}
	}
	return ""
}
