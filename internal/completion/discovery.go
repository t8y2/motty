package completion

import (
	"context"
	"errors"
	"strings"

	"motty/internal/shellkind"
)

const discoveryLimit = 64

func ValidDiscoveryPrefix(prefix string) bool { return len(prefix) >= 2 && probeName(prefix) }

func DiscoverCommands(ctx context.Context, runner CommandRunner, kind shellkind.Kind, prefix string) ([]string, error) {
	if runner == nil || !ValidDiscoveryPrefix(prefix) {
		return nil, errors.New("completion: invalid command discovery prefix")
	}
	var command string
	switch kind {
	case shellkind.POSIX:
		command = `path=$PATH:
count=0
while [ -n "$path" ]; do
  dir=${path%%:*}
  path=${path#*:}
  [ -n "$dir" ] || dir=.
  for entry in "$dir"/"` + prefix + `"*; do
    [ -f "$entry" ] && [ -x "$entry" ] || continue
    name=${entry##*/}
    printf '%s\0' "$name"
    count=$((count+1))
    [ "$count" -lt 64 ] || break 2
  done
done`
	case shellkind.PowerShell:
		command = `Get-Command -Name '` + prefix + `*' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 64 -ExpandProperty Name`
	default:
		return nil, errors.New("completion: unsupported shell for command discovery")
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
		return nil, errors.New("completion: command discovery output exceeded limit")
	}
	separator := "\x00"
	if kind == shellkind.PowerShell {
		separator = "\n"
	}
	text := string(output)
	records := strings.Split(text, separator)
	if kind == shellkind.POSIX && !strings.HasSuffix(text, separator) {
		records = records[:len(records)-1]
	}
	seen := make(map[string]bool, discoveryLimit)
	names := make([]string, 0, discoveryLimit)
	for _, raw := range records {
		name := strings.TrimSuffix(raw, "\r")
		match := strings.HasPrefix(name, prefix)
		key := name
		if kind == shellkind.PowerShell {
			match = len(name) >= len(prefix) && strings.EqualFold(name[:min(len(name), len(prefix))], prefix)
			key = strings.ToLower(name)
		}
		if !match || len(name) == len(prefix) || !probeName(name) || !SafeInsert(name[len(prefix):]) || seen[key] {
			continue
		}
		seen[key] = true
		names = append(names, name)
		if len(names) == discoveryLimit {
			break
		}
	}
	return names, nil
}
