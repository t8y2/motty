package remotetools

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

var ErrProcessEnvironmentUnsupported = errors.New("remotetools: process environment is unsupported on this host")

const (
	maxProcessEnvironmentBytes     = 256 << 10
	maxProcessEnvironmentVariables = 512
	maxProcessEnvironmentOutput    = 368 << 10
)

// ProcessEnvironmentVariable is one name-value pair from a process environment.
// Values remain opaque strings and are never interpreted as shell syntax.
type ProcessEnvironmentVariable struct {
	Name  string
	Value string
}

// ReadProcessEnvironment reads the selected Linux process's environment on
// demand. Environment blocks can be large and sensitive, so they are not part
// of the process snapshot request. Windows and macOS expose no portable SSH
// interface with the same fidelity.
func ReadProcessEnvironment(ctx context.Context, runner Runner, process Process) ([]ProcessEnvironmentVariable, error) {
	if system := runnerSystem(runner); system == "windows" || system == "darwin" {
		return nil, ErrProcessEnvironmentUnsupported
	}
	if process.PID <= 0 || process.StartIdentity == "" {
		return nil, ErrProcessUnverifiable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pid := strconv.Itoa(process.PID)
	command := `# motty-process-environment
LC_ALL=C; export LC_ALL
pid=` + pid + `
boot=
IFS= read -r boot < /proc/sys/kernel/random/boot_id 2>/dev/null || exit 1
[ -n "$boot" ] || exit 1
read_ticks() {
` + readProcessTicks + `}
path=/proc/$pid/stat
read_ticks || exit 1
before=$ticks
printf 'motty-env-before %s %s\n' "$boot" "$before"
[ -r "/proc/$pid/environ" ] || exit 1
head -c ` + strconv.Itoa(maxProcessEnvironmentBytes) + ` < "/proc/$pid/environ" | base64
read_ticks || exit 1
printf 'motty-env-after %s %s\n' "$boot" "$ticks"
`
	output, err := runner.RunCommand(ctx, command, maxProcessEnvironmentOutput)
	if err != nil {
		if text := strings.TrimSpace(string(output)); text != "" {
			line, _, _ := strings.Cut(text, "\n")
			return nil, &Failure{Message: line, Err: err}
		}
		return nil, err
	}
	return parseProcessEnvironmentOutput(output, process.StartIdentity)
}

func parseProcessEnvironmentOutput(output []byte, expectedIdentity string) ([]ProcessEnvironmentVariable, error) {
	lines := strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n")
	before, after := -1, -1
	for index, line := range lines {
		switch {
		case strings.HasPrefix(line, "motty-env-before "):
			if before >= 0 {
				return nil, ErrMalformed
			}
			before = index
		case strings.HasPrefix(line, "motty-env-after "):
			if after >= 0 {
				return nil, ErrMalformed
			}
			after = index
		}
	}
	if before < 0 || after <= before {
		return nil, ErrMalformed
	}
	identity := func(line string) (string, bool) {
		fields := strings.Fields(line)
		if len(fields) != 3 || !validBootIdentity(fields[1]) || !decimalIdentity(fields[2]) {
			return "", false
		}
		return "linux:" + fields[1] + ":" + fields[2], true
	}
	beforeIdentity, beforeOK := identity(lines[before])
	afterIdentity, afterOK := identity(lines[after])
	if !beforeOK || !afterOK {
		return nil, ErrProcessUnverifiable
	}
	if beforeIdentity != expectedIdentity || afterIdentity != expectedIdentity {
		return nil, ErrProcessChanged
	}
	return ParseProcessEnvironment([]byte(strings.Join(lines[before+1:after], "")))
}

// ParseProcessEnvironment decodes the base64 output returned by
// ReadProcessEnvironment. Malformed entries are omitted instead of making the
// entire table unusable.
func ParseProcessEnvironment(output []byte) ([]ProcessEnvironmentVariable, error) {
	encoded := strings.TrimSpace(string(output))
	if encoded == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrMalformed
	}
	if len(raw) > maxProcessEnvironmentBytes {
		raw = raw[:maxProcessEnvironmentBytes]
	}
	result := make([]ProcessEnvironmentVariable, 0, min(maxProcessEnvironmentVariables, 32))
	for _, item := range strings.Split(string(raw), "\x00") {
		if item == "" {
			continue
		}
		name, value, ok := strings.Cut(item, "=")
		if !ok || name == "" {
			continue
		}
		result = append(result, ProcessEnvironmentVariable{
			Name:  strings.ToValidUTF8(name, "�"),
			Value: strings.ToValidUTF8(value, "�"),
		})
		if len(result) == maxProcessEnvironmentVariables {
			break
		}
	}
	return result, nil
}
