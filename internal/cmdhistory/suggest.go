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
