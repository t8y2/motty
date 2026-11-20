package highlight

import (
	"testing"
)

func TestParseRules(t *testing.T) {
	rules, skipped := ParseRules(`# colors of the build log
ERROR | red
WARN | yellow | case
ERROR \d+: | magenta | regex
plain | blue | regex case

no color
wrong | purple
unclosed | red | bold
"" | green
`)
	want := []Rule{
		{Pattern: "ERROR", Color: Red},
		{Pattern: "WARN", CaseSensitive: true, Color: Yellow},
		{Pattern: `ERROR \d+:`, Regex: true, Color: Magenta},
		{Pattern: "plain", Regex: true, CaseSensitive: true, Color: Blue},
		{Pattern: `""`, Color: Green},
	}
	if len(rules) != len(want) || skipped != 3 {
		t.Fatalf("parsed %d rules, skipped %d; want %d and 3", len(rules), skipped, len(want))
	}
	for i, rule := range rules {
		if rule.Pattern != want[i].Pattern || rule.Regex != want[i].Regex || rule.CaseSensitive != want[i].CaseSensitive || rule.Color != want[i].Color {
			t.Errorf("rule %d = %+v, want %+v", i, rule, want[i])
		}
	}
}

// The color sits after the last pipe, so a regular expression's alternation
// stays whole.
func TestParseRulesTakesTheColorFromTheLastPipe(t *testing.T) {
	rules, skipped := ParseRules("a|b | red")
	if skipped != 0 || len(rules) != 1 || rules[0].Pattern != "a|b" || rules[0].Color != Red {
		t.Fatalf("parsed %+v, skipped %d; want a|b in red and none skipped", rules, skipped)
	}
}

func TestParseRulesTakesNothingFromAnEmptyFile(t *testing.T) {
	rules, skipped := ParseRules("# only a comment\n\n   \n")
	if len(rules) != 0 || skipped != 0 {
		t.Fatalf("parsed %d rules, skipped %d; want none", len(rules), skipped)
	}
}

// The color may be written in any case; the flags too.
func TestParseRulesReadsColorAndFlagsInAnyCase(t *testing.T) {
	rules, skipped := ParseRules("err | RED | REGEX Case")
	if skipped != 0 || len(rules) != 1 || rules[0].Pattern != "err" || rules[0].Color != Red || !rules[0].Regex || !rules[0].CaseSensitive {
		t.Fatalf("parsed %+v, skipped %d", rules, skipped)
	}
}

// A Windows editor writes a byte-order mark before the first line; it belongs
// to the file, not to the first pattern.
func TestParseRulesDropsTheByteOrderMark(t *testing.T) {
	rules, skipped := ParseRules("\ufeffZZTOP | red\n")
	if skipped != 0 || len(rules) != 1 || rules[0].Pattern != "ZZTOP" || rules[0].Color != Red {
		t.Fatalf("parsed %+v, skipped %d; want ZZTOP in red", rules, skipped)
	}
}

// The import format takes an explicit color where it takes a name, so a
// configuration from a terminal that writes #rrggbb keeps its colors.
func TestParseRulesReadsExplicitColors(t *testing.T) {
	rules, skipped := ParseRules("ERROR | #FF5555\nWARN | #abc | case\nbad | #12345\n")
	want := []struct {
		pattern string
		color   Color
		matched bool
	}{
		{"ERROR", RGB(0xff, 0x55, 0x55), false},
		{"WARN", RGB(0xaa, 0xbb, 0xcc), true},
	}
	if skipped != 1 || len(rules) != len(want) {
		t.Fatalf("parsed %d rules, skipped %d; want %d and 1", len(rules), skipped, len(want))
	}
	for index, rule := range rules {
		if rule.Pattern != want[index].pattern || rule.Color != want[index].color || rule.CaseSensitive != want[index].matched {
			t.Errorf("rule %d = %+v, want %+v", index, rule, want[index])
		}
	}
}
