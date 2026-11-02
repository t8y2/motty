package completion

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// validTokenPart reports whether s can be a command or option name. A token is
// inserted into a shell command line, so it must be a plain bare word.
func validTokenPart(s string) bool {
	if s == "" || len(s) > 64 || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		switch {
		case r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)):
		case strings.ContainsRune("-_.:+@%", r):
		default:
			return false
		}
	}
	return true
}

// SafeValue reports whether a literal option value can be inserted as a bare
// word. Quotes, escapes, whitespace, control characters and shell metacharacters
// are rejected: the completion never needs to escape anything, and refusing
// those values keeps it that way.
//
// The value alphabet is deliberately narrow. A value is not just a word: a path
// or a token starting with "-" can change what a command does when the user
// accepts it, so values start with a letter or digit. The only leading "-" that
// is kept is a plain negative integer.
func SafeValue(s string) bool {
	if s == "" || len(s) > maxValueBytes || !utf8.ValidString(s) {
		return false
	}
	if strings.TrimSpace(s) != s {
		return false
	}
	first, _ := utf8.DecodeRuneInString(s)
	if !unicode.IsLetter(first) && !unicode.IsDigit(first) && !negativeNumberLiteral(s) {
		return false
	}
	for _, r := range s {
		switch {
		case r >= utf8.RuneSelf:
			// Non-ASCII text is allowed only when it is a letter or digit;
			// punctuation could be method-specific or look confusing.
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				return false
			}
		case unicode.IsLetter(r) || unicode.IsDigit(r):
		case strings.ContainsRune("-_.+,", r):
		default:
			return false
		}
	}
	return true
}

// negativeNumberLiteral accepts a plain negative integer such as -1, the only
// spelling allowed to start with a "-".
func negativeNumberLiteral(s string) bool {
	if len(s) < 2 || s[0] != '-' {
		return false
	}
	for index := 1; index < len(s); index++ {
		if s[index] < '0' || s[index] > '9' {
			return false
		}
	}
	return true
}

// SafeInsert reports whether s may be typed as a suffix of catalog text. It is
// the union of the command/option token alphabet and the literal value alphabet,
// so both kinds of catalog insertion are accepted, while whitespace, control
// characters, invisible format characters and shell punctuation are refused.
//
// The importer validates each token and value already; this check exists as a
// last defence for text that reached the workbench from a decoded cache.
func SafeInsert(s string) bool {
	if s == "" || len(s) > maxValueBytes || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		switch {
		case r >= utf8.RuneSelf:
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				return false
			}
		case unicode.IsLetter(r) || unicode.IsDigit(r):
		case strings.ContainsRune("-_.:+@%,", r):
		default:
			return false
		}
	}
	return true
}

// NormalizeText turns untrusted text into the one line that may be shown in the
// UI or stored in a description. Whitespace and line breaks collapse into single
// spaces, invisible characters disappear, and the result is bounded to limit
// runes (zero disables the bound).
//
// Dropped rather than spaced: bidirectional overrides, zero-width characters, a
// byte-order mark and the other Unicode format characters (Cf); the line and
// paragraph separators (Zl/Zp); C0/C1 control characters other than whitespace;
// and the invisible variation selectors. A description can otherwise display
// text other than the one that is stored, which is exactly the disguise a
// poisoned catalog would want.
func NormalizeText(s string, limit int) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Cf, r), r == '\u2028', r == '\u2029', isVariationSelector(r):
			continue
		case unicode.IsControl(r):
			if unicode.IsSpace(r) {
				space = b.Len() > 0
			}
			continue
		case unicode.IsSpace(r):
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	out := b.String()
	if limit > 0 && utf8.RuneCountInString(out) > limit {
		// Cutting a rune sequence can leave a trailing space, which a second
		// pass would remove; drop it here so normalization is idempotent.
		out = strings.TrimRight(string([]rune(out)[:limit]), " ")
	}
	return out
}

// isVariationSelector reports the two ranges of invisible variation selectors.
// They are combining marks (Mn), not format characters, so NormalizeText must
// name them explicitly.
func isVariationSelector(r rune) bool {
	return (r >= 0xFE00 && r <= 0xFE0F) || (r >= 0xE0100 && r <= 0xE01EF)
}
