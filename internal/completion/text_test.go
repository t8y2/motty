package completion

import (
	"runtime"
	"strings"
	"testing"
)

func TestNormalizeTextFlattensOneLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"plain description", "plain description"},
		{"  leading and trailing  ", "leading and trailing"},
		{"two\nlines", "two lines"},
		{"tab\tseparated", "tab separated"},
		{"non-breaking\u00a0space", "non-breaking space"},
		{"wide\u3000space", "wide space"},
	}
	for _, c := range cases {
		if got := NormalizeText(c.in, maxDescRunes); got != c.want {
			t.Errorf("NormalizeText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Invisible characters are the spoofing vector this function exists to remove:
// they can reverse or reorder what a row displays without changing the text
// stored in the catalog. None of them may survive.
func TestNormalizeTextDropsInvisibleCharacters(t *testing.T) {
	payloads := map[string]string{
		"right-to-left override":  "\u202e",
		"right-to-left mark":      "\u200f",
		"left-to-right override":  "\u202d",
		"left-to-right isolate":   "\u2066",
		"pop directional isolate": "\u2069",
		"zero-width space":        "\u200b",
		"zero-width joiner":       "\u200d",
		"soft hyphen":             "\u00ad",
		"byte-order mark":         "\ufeff",
		"arabic letter mark":      "\u061c",
		"tag character":           "\U000E0041",
		"line separator":          "\u2028",
		"paragraph separator":     "\u2029",
		"variation selector-16":   "\ufe0f",
		"variation selector-256":  "\U000e0100",
		"null":                    "\x00",
		"escape":                  "\x1b",
		"c1 control":              "\u009b",
	}
	for name, payload := range payloads {
		got := NormalizeText("safe"+payload+"text", maxDescRunes)
		if got != "safetext" {
			t.Errorf("%s: NormalizeText = %q, want %q", name, got, "safetext")
		}
	}
	for name, payload := range map[string]string{
		"nbsp": "\u00a0", "ideographic space": "\u3000", "tab": "\t",
		// NEL is a control and a whitespace character: it separates words
		// instead of being cut out.
		"windows-1252 nel": "\u0085",
	} {
		if got := NormalizeText("safe"+payload+"text", maxDescRunes); got != "safe text" {
			t.Errorf("%s: NormalizeText = %q, want %q", name, got, "safe text")
		}
	}
}

func TestNormalizeTextKeepsVisibleUnicode(t *testing.T) {
	legit := "µ’‐→“”‘↔📚 café 中文"
	if got := NormalizeText(legit, maxDescRunes); got != legit {
		t.Fatalf("NormalizeText(%q) = %q", legit, got)
	}
}

func TestNormalizeTextBoundsTheDescription(t *testing.T) {
	long := strings.Repeat("é", maxDescRunes+20)
	if got := NormalizeText(long, maxDescRunes); len([]rune(got)) != maxDescRunes {
		t.Fatalf("normalized length = %d runes, want %d", len([]rune(got)), maxDescRunes)
	}
}

// allocatedPerCall reports the average heap bytes one call of f allocates.
func allocatedPerCall(f func()) uint64 {
	const runs = 20
	f()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		f()
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / runs
}

// The result is bounded by the limit, so what NormalizeText allocates must not
// follow the size of the input: a megabyte of text costs about what a kilobyte
// does.
func TestNormalizeTextAllocationDoesNotGrowWithInput(t *testing.T) {
	small := strings.Repeat("bad text ", 3_000)
	large := strings.Repeat("bad text ", 300_000)
	if got := NormalizeText(large, 240); len(got) != 240 {
		t.Fatalf("normalized length = %d, want 240", len(got))
	}
	smallBytes := allocatedPerCall(func() { NormalizeText(small, 240) })
	largeBytes := allocatedPerCall(func() { NormalizeText(large, 240) })
	t.Logf("allocated per call: %d bytes for %d input bytes, %d bytes for %d", smallBytes, len(small), largeBytes, len(large))
	if largeBytes > 2*smallBytes+1024 {
		t.Fatalf("allocation grew with the input: %d bytes for %d input bytes, %d bytes for %d", largeBytes, len(large), smallBytes, len(small))
	}
}

func TestNormalizeTextTruncatesAtRuneBoundaries(t *testing.T) {
	for _, tc := range []struct {
		input string
		limit int
		want  string
	}{
		{"a b", 2, "a"},
		{"a b", 3, "a b"},
		{"é 中文", 3, "é 中"},
		{"a\u202eb", 2, "ab"},
		{"a\t\tb", 0, "a b"},
	} {
		if got := NormalizeText(tc.input, tc.limit); got != tc.want {
			t.Errorf("NormalizeText(%q, %d) = %q, want %q", tc.input, tc.limit, got, tc.want)
		}
	}
}

func TestNormalizeTextBoundsInvisibleInput(t *testing.T) {
	for _, prefix := range []string{"\u202e", " ", "\x00"} {
		input := strings.Repeat(prefix, 1_000_000) + "late text"
		if got := NormalizeText(input, 240); got != "" {
			t.Errorf("scanned past an excessive invisible prefix %q: %q", prefix, got)
		}
	}
}

func TestSafeValueRejectsDangerousWords(t *testing.T) {
	for _, value := range []string{
		"", " leading", "trailing ", "two words", "a\tb", "a\nb",
		"a;b", "a|b", "a&b", "$(id)", "`id`", "a>b", "a<b", "a*b", "a?b", "a!b",
		`a"b`, "a'b", `a\b`, "a\u202eb", "a\u00a0b", "🛠", "a\x00b",
		strings.Repeat("a", maxValueBytes+1),
	} {
		if SafeValue(value) {
			t.Errorf("SafeValue(%q) = true", value)
		}
	}
}

// Values that earlier releases accepted must stay accepted: an installed cache
// holding one of them has to keep decoding after an upgrade.
func TestSafeValueKeepsPlainLiterals(t *testing.T) {
	for _, value := range []string{
		"json", "smart-squash", "v1.8.2", "中文", "µs", "-1", "a_b", "a.b", "a+b", "a,b",
		"2025", "node18", "application/json", "host:port", "key=value", "100%", "@scope",
	} {
		if !SafeValue(value) {
			t.Errorf("SafeValue(%q) = false", value)
		}
	}
}

func TestSafeInsertAcceptsTokensAndValues(t *testing.T) {
	for _, insert := range []string{"stage", "mmit", "--flag", "-v", "a:b", "@scope", "a@b", "100%", "v1.8.2", "中文", "-1", "a/b", "a=b"} {
		if !SafeInsert(insert) {
			t.Errorf("SafeInsert(%q) = false", insert)
		}
	}
	for _, insert := range []string{"", " ", "a b", "safe\u202etext", "a\nb", "a;b", "a|b", "$(id)", "a?b", "a*b", "a!b", "a&b", `a\b`, `a"b`, "a'b", "a\u00a0b"} {
		if SafeInsert(insert) {
			t.Errorf("SafeInsert(%q) = true", insert)
		}
	}
}

// isNormalized is the allocation-free shortcut for "NormalizeText would change
// nothing". If it ever claimed a text was clean when normalization would alter
// it, an invisible or oversized description would pass the audit.
func TestIsNormalizedAgreesWithNormalizeText(t *testing.T) {
	pieces := []string{
		"a", "Z", "0", " ", "  ", "\t", "\n", "\x00", "\x7f", "\u0085", "\u00a0",
		"é", "中", "📚", "\u202e", "\u200b", "\u2028", "\ufe0f", "\U000e0100",
		"\u00ad", "\xff", "\xc3", "x y", "...",
	}
	var build func(prefix string, depth int)
	build = func(prefix string, depth int) {
		for _, limit := range []int{0, 1, 2, 3, 5} {
			if got, want := isNormalized(prefix, limit), NormalizeText(prefix, limit) == prefix; got != want {
				t.Fatalf("isNormalized(%q, %d) = %v, but NormalizeText returns %q", prefix, limit, got, NormalizeText(prefix, limit))
			}
		}
		if depth == 0 {
			return
		}
		for _, piece := range pieces {
			build(prefix+piece, depth-1)
		}
	}
	build("", 3)
}

func FuzzIsNormalizedAgreesWithNormalizeText(f *testing.F) {
	for _, seed := range []string{"", "plain text", " lead", "trail ", "a  b", "a\u202eb", "\xff", "é 中文"} {
		f.Add(seed, 4)
	}
	f.Fuzz(func(t *testing.T, s string, limit int) {
		if limit < 0 || limit > 300 {
			return
		}
		if got, want := isNormalized(s, limit), NormalizeText(s, limit) == s; got != want {
			t.Fatalf("isNormalized(%q, %d) = %v, NormalizeText = %q", s, limit, got, NormalizeText(s, limit))
		}
	})
}
