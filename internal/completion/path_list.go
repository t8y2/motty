package completion

import (
	"context"
	"errors"
	"strings"
)

func ListPathCandidates(ctx context.Context, runner CommandRunner, query PathQuery) ([]Candidate, error) {
	valid, ok := ParsePathQuery(query.Line, query.CWD)
	if runner == nil || !ok || valid != query {
		return nil, errors.New("completion: invalid path query")
	}
	command := `dir=` + pathQuote(query.Directory) + `
prefix=` + pathQuote(query.Prefix) + `
count=0
for entry in "$dir"/"$prefix"*; do
  [ -e "$entry" ] && [ ! -L "$entry" ] || continue
  name=${entry##*/}
  if [ -d "$entry" ]; then
    printf 'd%s\0' "$name"
  elif [ -f "$entry" ]; then
    printf 'f%s\0' "$name"
  else
    continue
  fi
  count=$((count+1))
  [ "$count" -lt 64 ] || break
done`
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
		return nil, errors.New("completion: path query output exceeded limit")
	}
	text := string(output)
	records := strings.Split(text, "\x00")
	if !strings.HasSuffix(text, "\x00") {
		records = records[:len(records)-1]
	}
	candidates := make([]Candidate, 0, min(len(records), discoveryLimit))
	seen := make(map[string]bool, discoveryLimit)
	for _, record := range records {
		if len(record) < 2 || record[0] != 'd' && record[0] != 'f' || query.DirectoriesOnly && record[0] != 'd' {
			continue
		}
		name := record[1:]
		if strings.ContainsRune(name, '/') || !strings.HasPrefix(name, query.Prefix) || !SafeValue(name) || seen[name] {
			continue
		}
		seen[name] = true
		suffix := strings.TrimPrefix(name, query.Prefix)
		directory := record[0] == 'd'
		if directory {
			suffix += "/"
		}
		if !SafeInsert(suffix) {
			continue
		}
		candidates = append(candidates, Candidate{
			Text: query.Word + suffix, Insert: suffix, Kind: KindPath, FromCatalog: true, FromHost: true, IsDirectory: directory,
		})
		if len(candidates) == discoveryLimit {
			break
		}
	}
	return candidates, nil
}

func pathQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
