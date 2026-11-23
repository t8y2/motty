package sessionimport

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var xterminalFields = regexp.MustCompile(`(?:^|[\t ])(host|port|user|authType|password|sshProxy|title)=`)

// ParseXterminalText reads one exported SSH connection per line. sshProxy is a
// client-local identifier and is deliberately ignored. Values are not unescaped.
func ParseXterminalText(input string) (Result, error) {
	var result Result
	if len(input) > MaxFileSize {
		return result, fmt.Errorf("xterminal export is too large")
	}
	for index, line := range strings.Split(strings.TrimPrefix(input, "\ufeff"), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		source := fmt.Sprintf("xterminal line %d", index+1)
		matches := xterminalFields.FindAllStringSubmatchIndex(line, -1)
		fields := map[string]string{}
		valid := len(matches) > 0 && strings.TrimSpace(line[:matches[0][0]]) == ""
		for i, match := range matches {
			end := len(line)
			if i+1 < len(matches) {
				end = matches[i+1][0]
			}
			key := line[match[2]:match[3]]
			if _, exists := fields[key]; exists {
				valid = false
			}
			fields[key] = line[match[1]:end]
		}
		port := 22
		if value, exists := fields["port"]; exists {
			var err error
			port, err = strconv.Atoi(strings.TrimSpace(value))
			valid = valid && err == nil && port > 0 && port <= 65535
		}
		if !valid || strings.TrimSpace(fields["host"]) == "" {
			result.warn("%s: invalid connection fields", source)
			continue
		}
		auth := strings.TrimSpace(fields["authType"])
		if auth != "" && auth != "password" {
			result.warn("%s: unsupported authentication type; session not imported", source)
			result.Skipped++
			continue
		}
		if len(result.Entries) >= MaxEntries {
			result.warn("%s: session limit exceeded", source)
			break
		}
		before := len(result.Entries)
		result.add(Entry{Protocol: SSH, Host: fields["host"], Port: port, User: fields["user"], Name: fields["title"], Password: fields["password"], Source: source})
		if len(result.Entries) > before && fields["password"] != "" {
			result.PlainSecrets++
		}
	}
	if len(result.Entries) == 0 {
		return result, fmt.Errorf("xterminal: no valid SSH connections found")
	}
	return result, nil
}
