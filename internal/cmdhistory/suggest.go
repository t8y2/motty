package cmdhistory

import "strings"

// Suggest is the rest of the command that the line typed so far most likely
// continues into: the most recently used entry that starts with it and is
// longer. entries are newest first, as Store.List returns them. A line that
// reads like a secret, or ends in a newline, gets nothing.
func Suggest(entries []Entry, line string) (rest string, ok bool) {
	matches := SuggestAll(entries, line, 1)
	if len(matches) == 0 {
		return "", false
	}
	return matches[0], true
}

// SuggestAll returns up to maxMatches command suffixes that match the line.
// Matches are sorted by recency (entries are newest first). A line that
// reads like a secret, or ends in a newline, gets nothing.
func SuggestAll(entries []Entry, line string, maxMatches int) []string {
	if line == "" || strings.ContainsAny(line, "\r\n") || Secret(line) || maxMatches <= 0 {
		return nil
	}
	var matches []string
	for _, entry := range entries {
		if len(entry.Command) > len(line) && strings.HasPrefix(entry.Command, line) {
			matches = append(matches, entry.Command[len(line):])
			if len(matches) >= maxMatches {
				break
			}
		}
	}
	return matches
}

// SuggestAllFull returns up to maxMatches full command entries that match the line.
// Matches are sorted by recency (entries are newest first). A line that
// reads like a secret, or ends in a newline, gets nothing.
func SuggestAllFull(entries []Entry, line string, maxMatches int) []Entry {
	if line == "" || strings.ContainsAny(line, "\r\n") || Secret(line) || maxMatches <= 0 {
		return nil
	}
	var matches []Entry
	for _, entry := range entries {
		if len(entry.Command) > len(line) && strings.HasPrefix(entry.Command, line) {
			matches = append(matches, entry)
			if len(matches) >= maxMatches {
				break
			}
		}
	}
	return matches
}

// CommonPrefix returns the longest common prefix of the given strings.
func CommonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	prefix := strs[0]
	for _, s := range strs[1:] {
		prefix = commonPrefixLen(prefix, s)
		if prefix == "" {
			break
		}
	}
	return prefix
}

func commonPrefixLen(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[:i]
		}
	}
	return a[:n]
}
