package remotetools

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

var (
	ErrProcessCommandUnsupported = errors.New("remotetools: complete process command is unsupported on this host")
	ErrProcessCommandTooLarge    = errors.New("remotetools: process command exceeds the display limit")
)

const (
	maxProcessCommandBytes  = 256 << 10
	maxProcessCommandOutput = 368 << 10
)

// ReadProcessCommand lazily reads a Linux process's complete argv after a
// detail dialog has selected it. The process identity is checked before and
// after the bounded read, so a reused PID cannot attach another command to the
// row the user opened.
func ReadProcessCommand(ctx context.Context, runner Runner, process Process) (string, error) {
	if system := runnerSystem(runner); system == "windows" || system == "darwin" {
		return "", ErrProcessCommandUnsupported
	}
	if process.PID <= 0 || process.StartIdentity == "" {
		return "", ErrProcessCommandUnsupported
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	pid := strconv.Itoa(process.PID)
	command := `# motty-process-command
LC_ALL=C; export LC_ALL
pid=` + pid + `
boot=
IFS= read -r boot < /proc/sys/kernel/random/boot_id 2>/dev/null || exit 1
[ -n "$boot" ] || exit 1
command -v base64 >/dev/null 2>&1 || {
  printf 'motty-command-error encoder\n'
  exit 1
}
read_ticks() {
` + readProcessTicks + `}
path=/proc/$pid/stat
read_ticks || {
  printf 'motty-command-error unavailable\n'
  exit 1
}
before=$ticks
printf 'motty-command-before %s %s\n' "$boot" "$before"
[ -r "/proc/$pid/cmdline" ] || {
  printf 'motty-command-error unreadable\n'
  exit 1
}
head -c ` + strconv.Itoa(maxProcessCommandBytes+1) + ` < "/proc/$pid/cmdline" | base64
read_ticks || exit 1
printf 'motty-command-after %s %s\n' "$boot" "$ticks"
`
	output, err := runner.RunCommand(ctx, command, maxProcessCommandOutput)
	if err != nil {
		if len(output) >= maxProcessCommandOutput {
			return "", ErrProcessCommandTooLarge
		}
		return "", classifyProcessCommandFailure(output, process.StartIdentity, err)
	}
	return parseProcessCommandOutput(output, process.StartIdentity)
}

func classifyProcessCommandFailure(output []byte, expectedIdentity string, commandErr error) error {
	text := strings.TrimSpace(string(output))
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	before := ""
	after := false
	diagnostic := ""
	for _, line := range lines {
		switch {
		case line == "motty-command-error unreadable":
			diagnostic = line
		case line == "motty-command-error encoder" || line == "motty-command-error unavailable":
			diagnostic = line
		case strings.HasPrefix(line, "motty-command-before "):
			before = line
		case strings.HasPrefix(line, "motty-command-after "):
			after = true
		}
	}
	switch diagnostic {
	case "motty-command-error unreadable":
		return &Failure{Message: "process command is unreadable", Err: errors.Join(ErrProcessCommandUnsupported, commandErr)}
	case "motty-command-error encoder":
		return &Failure{Message: "process command encoder is unavailable", Err: errors.Join(ErrProcessCommandUnsupported, commandErr)}
	case "motty-command-error unavailable":
		return &Failure{Message: "process command is unavailable", Err: errors.Join(ErrProcessUnverifiable, commandErr)}
	}
	if before != "" && !after {
		identity, ok := processProbeIdentity(before)
		if !ok {
			return &Failure{Message: "process identity is unreadable", Err: errors.Join(ErrProcessUnverifiable, commandErr)}
		}
		if identity != expectedIdentity {
			return &Failure{Message: "process changed while its command was read", Err: errors.Join(ErrProcessChanged, commandErr)}
		}
		return &Failure{Message: "process exited while its command was read", Err: errors.Join(ErrProcessChanged, commandErr)}
	}
	return commandErr
}

func parseProcessCommandOutput(output []byte, expectedIdentity string) (string, error) {
	lines := strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n")
	before, after := -1, -1
	for index, line := range lines {
		switch {
		case strings.HasPrefix(line, "motty-command-before "):
			if before >= 0 {
				return "", ErrMalformed
			}
			before = index
		case strings.HasPrefix(line, "motty-command-after "):
			if after >= 0 {
				return "", ErrMalformed
			}
			after = index
		}
	}
	if before < 0 || after <= before {
		return "", ErrMalformed
	}
	beforeIdentity, beforeOK := processProbeIdentity(lines[before])
	afterIdentity, afterOK := processProbeIdentity(lines[after])
	if !beforeOK || !afterOK {
		return "", ErrProcessUnverifiable
	}
	if beforeIdentity != expectedIdentity || afterIdentity != expectedIdentity {
		return "", ErrProcessChanged
	}
	raw, err := base64.StdEncoding.DecodeString(strings.Join(lines[before+1:after], ""))
	if err != nil {
		return "", ErrMalformed
	}
	if len(raw) > maxProcessCommandBytes {
		return "", ErrProcessCommandTooLarge
	}
	value := strings.TrimRight(string(raw), "\x00")
	value = strings.ReplaceAll(value, "\x00", " ")
	value = strings.ToValidUTF8(value, "�")
	if strings.TrimSpace(value) == "" {
		// Kernel threads and zombies have a readable but empty cmdline: the
		// caller falls back to the ps summary instead of a blank field.
		return "", ErrProcessCommandUnsupported
	}
	return value, nil
}
