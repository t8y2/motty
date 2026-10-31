package highlight

import (
	"strings"
)

// ParseRules reads a batch of rules in the import format: one rule per line,
//
//	pattern | color
//	pattern | color | regex
//	pattern | color | case
//	pattern | color | regex case
//
// where color is a slot's name (red, yellow, green, cyan, blue, magenta) and
// the flags say the pattern is a regular expression and that it matches case
// sensitively. The pattern is everything before the color, so it may hold a
// pipe of its own — an alternation in a regular expression does; a final
// segment of flags is told from a color by its words. Blank lines and lines
// opening with # are ignored; a line with no color, an unknown color or flag,
// or a pattern that Validate refuses is skipped, and skipped counts them. The
// order is the file's: the first rule wins a tie of two that match the same
// text. A leading byte-order mark, which Windows editors write, is dropped
// rather than joined to the first pattern.
func ParseRules(text string) (rules []Rule, skipped int) {
	text = strings.TrimPrefix(text, "\ufeff")
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		flags := ""
		if len(parts) >= 3 && onlyRuleFlags(parts[len(parts)-1]) {
			flags, parts = parts[len(parts)-1], parts[:len(parts)-1]
		}
		pattern := strings.TrimSpace(strings.Join(parts[:len(parts)-1], "|"))
		words := strings.Fields(parts[len(parts)-1])
		if len(words) != 1 {
			skipped++
			continue
		}
		slot := SlotFromName(strings.ToLower(words[0]))
		if slot == None {
			skipped++
			continue
		}
		rule := Rule{Pattern: pattern, Slot: slot}
		bad := false
		for _, flag := range strings.Fields(flags) {
			switch strings.ToLower(flag) {
			case "regex":
				rule.Regex = true
			case "case":
				rule.CaseSensitive = true
			default:
				bad = true
			}
		}
		if bad || rule.Pattern == "" || Validate(rule) != nil {
			skipped++
			continue
		}
		rules = append(rules, rule)
	}
	return rules, skipped
}

// onlyRuleFlags is whether a segment holds nothing but regex and case words.
func onlyRuleFlags(segment string) bool {
	words := strings.Fields(segment)
	if len(words) == 0 {
		return false
	}
	for _, word := range words {
		switch strings.ToLower(word) {
		case "regex", "case":
		default:
			return false
		}
	}
	return true
}
