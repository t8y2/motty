// Package highlight finds the words in a line of terminal text that deserve a
// color: errors, warnings, addresses, and whatever the user asked for. It knows
// nothing of cells or colors; it marks bytes of a line with a Slot, and the
// terminal turns a slot into a color of its scheme.
package highlight

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
)

// Slot names one of the colors a terminal scheme provides. The zero Slot marks
// nothing.
type Slot uint8

const (
	None Slot = iota
	Red
	Green
	Yellow
	Blue
	Magenta
	Cyan
	slotCount
)

// ANSI is the index in the terminal's sixteen named colors that a slot uses: the
// bright variants, which every scheme keeps legible on its background.
func (slot Slot) ANSI() int {
	if slot == None || slot >= slotCount {
		return -1
	}
	return 8 + int(slot)
}

// Name is the slot's word in a saved file.
func (slot Slot) Name() string {
	switch slot {
	case Red:
		return "red"
	case Green:
		return "green"
	case Yellow:
		return "yellow"
	case Blue:
		return "blue"
	case Magenta:
		return "magenta"
	case Cyan:
		return "cyan"
	}
	return ""
}

// SlotFromName reads a saved slot, or None for one it does not know.
func SlotFromName(name string) Slot {
	for slot := Red; slot < slotCount; slot++ {
		if slot.Name() == name {
			return slot
		}
	}
	return None
}

// Slots lists the colors a rule may use, in the order a chooser shows them.
func Slots() []Slot { return []Slot{Red, Yellow, Green, Cyan, Blue, Magenta} }

// MaxRules bounds how many rules a set takes, and MaxPatternLength how long one
// pattern is, so a damaged state file cannot make every line cost seconds.
const (
	MaxRules         = 64
	MaxPatternLength = 200
)

// Rule is what the user wrote: text to find, and the color to give it.
type Rule struct {
	// Pattern is literal text, or a regular expression when Regex is set.
	Pattern       string
	Regex         bool
	CaseSensitive bool
	Slot          Slot

	// fast, when set by Builtin, holds the literal matcher the package can run
	// instead of the expression. User rules never set it, so their behavior is
	// always the compiled expression.
	fast *fastMatcher
}

// fastMatcher is the whole-word form Builtin's first three rules take: every
// alternative is literal text between ASCII word boundaries. quad is the IPv4
// rule. It lets Mark skip the regular expression engine on plain ASCII lines.
type fastMatcher struct {
	words []string
	quad  bool
}

// ErrEmptyPattern and ErrNoColor say what is missing from a rule.
var (
	ErrEmptyPattern = errors.New("highlight: the rule has no text")
	ErrNoColor      = errors.New("highlight: the rule has no color")
	ErrTooLong      = errors.New("highlight: the pattern is too long")
)

func (rule Rule) expression() (*regexp.Regexp, error) {
	switch {
	case rule.Pattern == "":
		return nil, ErrEmptyPattern
	case len(rule.Pattern) > MaxPatternLength:
		return nil, ErrTooLong
	case rule.Slot == None || rule.Slot >= slotCount:
		return nil, ErrNoColor
	}
	pattern := rule.Pattern
	if !rule.Regex {
		pattern = regexp.QuoteMeta(pattern)
	}
	if !rule.CaseSensitive {
		pattern = "(?i)" + pattern
	}
	expression, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	if expression.MatchString("") {
		return nil, ErrEmptyPattern // it would match everywhere and color nothing useful
	}
	return expression, nil
}

// Validate reports why a rule cannot be used. Go's regular expressions run in
// time linear in the line, so a valid one is always safe.
func Validate(rule Rule) error {
	_, err := rule.expression()
	return err
}

type compiled struct {
	// expression is the fallback, and the only matcher of a user rule.
	expression *regexp.Regexp
	// words and quad are the fast matcher of a builtin rule.
	words [][]byte
	quad  bool
	slot  Slot
}

// Set is a compiled list of rules. A nil Set marks nothing.
type Set struct {
	rules            []compiled
	paddingSensitive bool
}

// Compile builds a set from rules in priority order: where two rules match the
// same text, the earlier one colors it. A rule that cannot be compiled is
// skipped, and at most MaxRules are used.
func Compile(rules []Rule) *Set {
	set := &Set{}
	for _, rule := range rules {
		if len(set.rules) == MaxRules {
			break
		}
		expression, err := rule.expression()
		if err != nil {
			continue
		}
		if rule.fast != nil && (len(rule.fast.words) > 0 || rule.fast.quad) {
			words := make([][]byte, len(rule.fast.words))
			for index, word := range rule.fast.words {
				words[index] = []byte(word)
			}
			set.rules = append(set.rules, compiled{expression: expression, words: words, quad: rule.fast.quad, slot: rule.Slot})
			continue
		}
		set.rules = append(set.rules, compiled{expression: expression, slot: rule.Slot})
		set.paddingSensitive = true
	}
	return set
}

// Empty reports whether the set can mark anything.
func (set *Set) Empty() bool { return set == nil || len(set.rules) == 0 }

// PaddingSensitive reports whether changing trailing spaces can change a
// match. Builtin whole-word and address rules are independent of padding;
// user rules may count spaces or anchor a match at the end of the row.
func (set *Set) PaddingSensitive() bool { return set != nil && set.paddingSensitive }

// Mark colors the bytes of line that a rule matches: marks[i] is the slot of
// line[i]. It reports whether it marked anything. Bytes another rule already
// marked keep their slot. marks must be as long as line and zeroed.
func (set *Set) Mark(line []byte, marks []Slot) bool {
	if set.Empty() || len(line) == 0 {
		return false
	}
	// The fast matcher is exact only on ASCII text: the expressions fold case
	// with (?i), whose Unicode folding a byte-wise lowercase would not copy. A
	// line with any other byte takes the expressions, so the result is the same
	// either way.
	var lower [fastLineLimit]byte
	fast := len(line) <= len(lower)
	if fast {
		for index, value := range line {
			if value >= 0x80 {
				fast = false
				break
			}
			lower[index] = lowerASCII(value)
		}
	}
	any := false
	for _, rule := range set.rules {
		switch {
		case fast && rule.quad:
			any = markQuad(line, marks, rule.slot) || any
		case fast && rule.words != nil:
			any = markWords(lower[:len(line)], marks, rule.words, rule.slot) || any
		default:
			any = markExpression(rule.expression, line, marks, rule.slot) || any
		}
	}
	return any
}

// fastLineLimit bounds the line a fast matcher copies to lower case on its
// stack. A longer line falls back to the expressions; terminal rows are far
// shorter, so the fallback stays cold.
const fastLineLimit = 512

func lowerASCII(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value + 'a' - 'A'
	}
	return value
}

// isWord is the ASCII word character \b counts: letters, digits and underscore.
func isWord(value byte) bool {
	return value == '_' || value >= '0' && value <= '9' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

// markExpression is the regular expression path, which serves every user rule
// and the builtin rules on text the fast matcher does not take.
func markExpression(expression *regexp.Regexp, line []byte, marks []Slot, slot Slot) bool {
	any := false
	for _, span := range expression.FindAllIndex(line, -1) {
		for index := span[0]; index < span[1]; index++ {
			if marks[index] == None {
				marks[index] = slot
				any = true
			}
		}
	}
	return any
}

// markWords marks every whole-word occurrence of words in the already lower
// cased line. It reproduces \b at both ends of each alternative, including the
// non-word final character of "active (running)".
func markWords(line []byte, marks []Slot, words [][]byte, slot Slot) bool {
	any := false
	for _, word := range words {
		if len(word) == 0 {
			continue
		}
		for from := 0; from+len(word) <= len(line); {
			at := bytes.Index(line[from:], word)
			if at < 0 {
				break
			}
			start, end := from+at, from+at+len(word)
			from = start + 1
			if start > 0 && isWord(line[start-1]) == isWord(line[start]) {
				continue
			}
			if isWord(line[end-1]) == (end < len(line) && isWord(line[end])) {
				continue
			}
			for index := start; index < end; index++ {
				if marks[index] == None {
					marks[index] = slot
					any = true
				}
			}
		}
	}
	return any
}

// markQuad marks IPv4-shaped dotted quads the way \b\d{1,3}(\.\d{1,3}){3}\b
// does: four groups of one to three ASCII digits with word boundaries outside.
func markQuad(line []byte, marks []Slot, slot Slot) bool {
	any := false
	for at := 0; at < len(line); at++ {
		if line[at] < '0' || line[at] > '9' {
			continue
		}
		if at > 0 && isWord(line[at-1]) {
			continue
		}
		end := at
		ok := true
		for group := 0; group < 4; group++ {
			digits := 0
			for end < len(line) && line[end] >= '0' && line[end] <= '9' {
				digits++
				end++
			}
			if digits == 0 || digits > 3 {
				ok = false
				break
			}
			if group < 3 {
				if end >= len(line) || line[end] != '.' {
					ok = false
					break
				}
				end++
			}
		}
		if !ok || isWord(line[end-1]) == (end < len(line) && isWord(line[end])) {
			continue
		}
		for index := at; index < end; index++ {
			if marks[index] == None {
				marks[index] = slot
				any = true
			}
		}
		at = end - 1
	}
	return any
}

// Builtin are the rules a terminal starts with: failures in red, warnings in
// yellow, successes in green and addresses in cyan. They are what an operator
// scanning a log is looking for.
func Builtin() []Rule {
	return []Rule{
		wordRule(Red, "error", "errors", "err", "fail", "fails", "failed", "failure", "fatal", "panic", "denied", "refused", "critical", "exception", "traceback", "segfault", "segmentation fault"),
		wordRule(Yellow, "warn", "warning", "warnings", "deprecated", "timeout", "timed out", "retrying"),
		wordRule(Green, "ok", "success", "successful", "successfully", "passed", "done", "connected", "active (running)"),
		{Slot: Cyan, Regex: true, Pattern: `\b\d{1,3}(\.\d{1,3}){3}\b`, fast: &fastMatcher{quad: true}},
	}
}

// wordRule builds a whole-word literal rule. Pattern is the same expression it
// always had, so Validate and the non-ASCII fallback keep their exact meaning;
// fast is the matcher Mark uses on plain ASCII lines.
func wordRule(slot Slot, words ...string) Rule {
	quoted := make([]string, len(words))
	for index, word := range words {
		quoted[index] = regexp.QuoteMeta(word)
	}
	return Rule{
		Slot: slot, Regex: true,
		Pattern: `\b(` + strings.Join(quoted, "|") + `)\b`,
		fast:    &fastMatcher{words: words},
	}
}
