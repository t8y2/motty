package aiproviderimport

import (
	"strconv"
	"strings"
)

// parseCodexTOML reads the few Codex fields the import needs. cc-switch stores
// the Codex config as a TOML string; adding a TOML dependency for two
// top-level keys and one provider table would be disproportionate.
//
// The result is the top-level model plus the selected model_providers table's
// base_url and wire_api. Unknown keys are ignored.
func parseCodexTOML(config string) map[string]string {
	top := map[string]string{}
	tables := map[string]map[string]string{}
	current := ""
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(stripTOMLComment(line))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = normalizeTOMLTable(line[1 : len(line)-1])
			continue
		}
		key, value, ok := splitTOMLKeyValue(line)
		if !ok {
			continue
		}
		if current == "" {
			top[key] = value
			continue
		}
		if tables[current] == nil {
			tables[current] = map[string]string{}
		}
		tables[current][key] = value
	}
	selected := top["model_provider"]
	table := tables["model_providers."+selected]
	if table == nil && selected == "" && len(tables) == 1 {
		for name, only := range tables {
			if strings.HasPrefix(name, "model_providers.") {
				table = only
			}
		}
	}
	out := map[string]string{"model": top["model"]}
	if table != nil {
		out["base_url"] = table["base_url"]
		out["wire_api"] = table["wire_api"]
	}
	return out
}

func stripTOMLComment(line string) string {
	inDouble, inSingle, escaped := false, false, false
	for index, r := range line {
		switch {
		case escaped:
			escaped = false
		case inDouble && r == '\\':
			escaped = true
		case !inSingle && r == '"':
			inDouble = !inDouble
		case !inDouble && r == '\'':
			inSingle = !inSingle
		case !inDouble && !inSingle && r == '#':
			return line[:index]
		}
	}
	return line
}

func normalizeTOMLTable(name string) string {
	name = strings.TrimSpace(name)
	parts := strings.Split(name, ".")
	for index, part := range parts {
		parts[index] = strings.Trim(strings.TrimSpace(part), `"'`)
	}
	return strings.Join(parts, ".")
}

func splitTOMLKeyValue(line string) (key, value string, ok bool) {
	inDouble, inSingle, escaped := false, false, false
	for index, r := range line {
		switch {
		case escaped:
			escaped = false
		case inDouble && r == '\\':
			escaped = true
		case !inSingle && r == '"':
			inDouble = !inDouble
		case !inDouble && r == '\'':
			inSingle = !inSingle
		case !inDouble && !inSingle && r == '=':
			key = strings.Trim(strings.TrimSpace(line[:index]), `"'`)
			return key, parseTOMLValue(line[index+1:]), key != ""
		}
	}
	return "", "", false
}

func parseTOMLValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if raw[0] == '"' {
		if value, err := strconv.Unquote(raw); err == nil {
			return value
		}
		return ""
	}
	if raw[0] == '\'' {
		if end := strings.IndexByte(raw[1:], '\''); end >= 0 {
			return raw[1 : end+1]
		}
		return ""
	}
	if index := strings.IndexAny(raw, " \t"); index >= 0 {
		raw = raw[:index]
	}
	return strings.TrimSpace(raw)
}
