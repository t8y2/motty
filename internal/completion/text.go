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
// The alphabet is the one every release since the first has used, so a cache
// written by an older version still decodes and values such as
// "application/json", "host:port" or "key=value" stay usable.
func SafeValue(s string) bool {
	if s == "" || len(s) > maxValueBytes || !utf8.ValidString(s) {
		return false
	}
	if strings.TrimSpace(s) != s {
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
		case strings.ContainsRune("-_.:@%+,=/", r):
		default:
			return false
		}
	}
	return true
}

// SafeInsert reports whether s may be typed as a suffix of catalog text, which
// is a fragment of a command or option token or of a literal value. Both use
// the SafeValue alphabet (a token's alphabet is a subset of it), so there is a
// single definition of what a catalog may type.
//
// The importer validates each token and value already; the workbench calls this
// as a last defence for text that reached it from a decoded cache.
func SafeInsert(s string) bool { return SafeValue(s) }

// NormalizeText turns untrusted text into the one line that may be shown in the
// UI or stored in a description. Whitespace and line breaks collapse into single
// spaces, invisible characters disappear, and the result is bounded to limit
// runes. A positive limit also bounds the input scan to 16 bytes per output
// rune, so an excessive invisible prefix cannot make UI rendering scan megabytes
// without producing text. Zero disables both bounds.
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
	if isNormalized(s, limit) {
		return s
	}
	var b strings.Builder
	capacity := len(s)
	if limit > 0 {
		capacity = min(capacity, limit)
	}
	b.Grow(capacity)
	scanBytes := len(s)
	if limit > 0 && limit <= scanBytes/16 {
		scanBytes = limit * 16
	}
	space := false
	runes := 0
	for index, r := range s {
		if index >= scanBytes {
			break
		}
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
			if limit > 0 && runes+1 >= limit {
				break
			}
			b.WriteByte(' ')
			runes++
			space = false
		}
		b.WriteRune(r)
		runes++
		if limit > 0 && runes >= limit {
			break
		}
	}
	return b.String()
}

// isNormalized reports whether NormalizeText(s, limit) would return s
// unchanged, without allocating. Decoding a catalog and auditing it both ask
// this about every description, and nearly all of them are already clean, so
// the common case must not build a second copy of each. The scan stops once it
// has seen more than limit runes.
func isNormalized(s string, limit int) bool {
	if s == "" {
		return true
	}
	runes := 0
	previousSpace := true // a leading space is dropped, so it is not normalized
	for index := 0; index < len(s); runes++ {
		if limit > 0 && runes >= limit {
			return false
		}
		c := s[index]
		if c < utf8.RuneSelf {
			index++
			switch {
			case c == ' ':
				if previousSpace {
					return false
				}
				previousSpace = true
			case c < ' ' || c == 0x7f:
				return false
			default:
				previousSpace = false
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(s[index:])
		if r == utf8.RuneError && size == 1 {
			return false
		}
		if unicode.Is(unicode.Cf, r) || unicode.IsControl(r) || unicode.IsSpace(r) || isVariationSelector(r) {
			return false
		}
		index += size
		previousSpace = false
	}
	return !previousSpace
}

// isVariationSelector reports the two ranges of invisible variation selectors.
// They are combining marks (Mn), not format characters, so NormalizeText must
// name them explicitly.
func isVariationSelector(r rune) bool {
	return (r >= 0xFE00 && r <= 0xFE0F) || (r >= 0xE0100 && r <= 0xE01EF)
}
