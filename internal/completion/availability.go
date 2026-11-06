package completion

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"motty/internal/shellkind"
)

const (
	probeOutputLimit = 4096
	probeNameLimit   = 64
	probeTimeout     = 2 * time.Second
)

type CommandRunner interface {
	RunCommand(context.Context, string, int) ([]byte, error)
}

func ProbeAvailable(ctx context.Context, runner CommandRunner, kind shellkind.Kind, names []string) (map[string]bool, error) {
	if runner == nil || len(names) > probeNameLimit {
		return nil, errors.New("completion: invalid availability request")
	}
	requested := make(map[string]bool, len(names))
	unique := make([]string, 0, len(names))
	for _, name := range names {
		if !probeName(name) {
			return nil, fmt.Errorf("completion: invalid command name %q", name)
		}
		if !requested[name] {
			requested[name] = true
			unique = append(unique, name)
		}
	}
	if len(unique) == 0 {
		return map[string]bool{}, nil
	}
	var command string
	switch kind {
	case shellkind.POSIX:
		command = `for name in ` + strings.Join(unique, " ") + `; do if command -v "$name" >/dev/null 2>&1; then printf '%s\n' "$name"; fi; done`
	case shellkind.PowerShell:
		quoted := make([]string, len(unique))
		for index, name := range unique {
			quoted[index] = "'" + name + "'"
		}
		command = `$names = @(` + strings.Join(quoted, ",") + `); foreach ($name in $names) { if (Get-Command -Name $name -ErrorAction SilentlyContinue) { [Console]::Out.WriteLine($name) } }`
	default:
		return nil, errors.New("completion: unsupported shell for command detection")
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	output, err := runner.RunCommand(probeCtx, command, probeOutputLimit)
	if err != nil {
		return nil, err
	}
	if err := probeCtx.Err(); err != nil {
		return nil, err
	}
	if len(output) > probeOutputLimit {
		return nil, errors.New("completion: command detection output exceeded limit")
	}
	available := make(map[string]bool, len(unique))
	for _, line := range strings.Split(string(output), "\n") {
		name := strings.TrimSuffix(line, "\r")
		if requested[name] {
			available[name] = true
		}
	}
	return available, nil
}

func probeName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for index := 0; index < len(name); index++ {
		value := name[index]
		if value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || index > 0 && (value >= '0' && value <= '9' || strings.ContainsRune("-_.+", rune(value))) {
			continue
		}
		return false
	}
	return true
}
