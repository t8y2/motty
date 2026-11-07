package cmdhistory

import (
	"strings"
	"unicode/utf8"
)

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
	full := SuggestAllFull(entries, line, maxMatches)
	if len(full) == 0 {
		return nil
	}
	matches := make([]string, len(full))
	for index, entry := range full {
		matches[index] = entry.Command[len(line):]
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

// CommonPrefix returns the longest common prefix of the given strings, cut on a
// rune boundary: a lead shared only as far as the middle of a character is not
// something the user could type.
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
	limit := min(len(a), len(b))
	for i := 0; i < limit; i++ {
		if a[i] != b[i] {
			return trimToRuneStart(a[:i])
		}
	}
	return trimToRuneStart(a[:limit])
}

// trimToRuneStart drops bytes from the end until what is left stops on a rune
// boundary. Only the tail can be torn, because the lead came from a valid
// string, so a few steps at most are needed.
func trimToRuneStart(s string) string {
	for len(s) > 0 {
		if r, size := utf8.DecodeLastRuneInString(s); r != utf8.RuneError || size > 1 {
			return s
		}
		s = s[:len(s)-1]
	}
	return s
}
