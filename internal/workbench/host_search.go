package workbench

import "strings"

// matchesSearchWords requires every lowercased word somewhere in the
// lowercased searchable text, regardless of order.
func matchesSearchWords(search string, words []string) bool {
	for _, word := range words {
		if !strings.Contains(search, word) {
			return false
		}
	}
	return true
}

// matchesHost looks for every word in the same host, allowing different words
// to match different fields while preserving substring matching within fields.
func matchesHost(host Host, words []string) bool {
	values := [...]string{
		strings.ToLower(host.Label), strings.ToLower(host.Address), strings.ToLower(host.User),
		strings.ToLower(host.Group), strings.ToLower(host.Tags), strings.ToLower(host.Icon.String()),
	}
	for _, word := range words {
		found := false
		for _, value := range values {
			if strings.Contains(value, word) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
