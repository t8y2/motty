// Package highlight finds the words in a line of terminal text that deserve a
// color: errors, warnings, addresses, and whatever the user asked for. It knows
// nothing of cells or palettes; it marks bytes of a line with a Color, which the
// terminal resolves through its scheme when the color is a named one, or draws
// as the explicit value the rule holds.
package highlight

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Color is the color a rule gives its text: one of the colors a terminal scheme
// names, or an explicit RGB value the user chose. The zero Color marks nothing.
type Color uint32

const (
	None Color = iota
	Red
	Green
	Yellow
	Blue
	Magenta
	Cyan
	namedColorCount
)

// customColor marks a Color as explicit RGB channels rather than a named one. A
// named color is below namedColorCount, so the bit tells them apart.
const customColor Color = 1 << 24

// RGB is the color of eight-bit red, green and blue channels.
func RGB(red, green, blue uint8) Color {
	return customColor | Color(red)<<16 | Color(green)<<8 | Color(blue)
}

// IsCustom reports whether the color is explicit RGB rather than one of the
// terminal scheme's named colors.
func (color Color) IsCustom() bool { return color&customColor != 0 }

// Channels is the eight-bit red, green and blue of an explicit color.
func (color Color) Channels() (red, green, blue uint8) {
	return uint8(color >> 16), uint8(color >> 8), uint8(color)
}

// valid reports whether the value names a color a rule can use: one of the
// named ones or an explicit RGB value. The zero value and bits beyond the RGB
// channels are refused.
func (color Color) valid() bool {
	if color == None {
		return false
	}
	if color.IsCustom() {
		return color&0xFF000000 == customColor
	}
	return color < namedColorCount
}

// ANSI is the index in the terminal's sixteen named colors that a named color
// uses: the bright variants, which every scheme keeps legible on its background.
// An explicit color, and None, have no index.
func (color Color) ANSI() int {
	if color == None || color >= namedColorCount {
		return -1
	}
	return 8 + int(color)
}

// Name is the color's word in a saved file: a named color's word, or its
// #rrggbb form when it is explicit.
func (color Color) Name() string {
	switch color {
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
	if color.IsCustom() {
		red, green, blue := color.Channels()
		return fmt.Sprintf("#%02x%02x%02x", red, green, blue)
	}
	return ""
}

// ColorFromName reads a saved color, or None for one it does not know. A named
// color is its word in any case; an explicit one is #rgb or #rrggbb.
func ColorFromName(name string) Color {
	name = strings.ToLower(strings.TrimSpace(name))
	for color := Red; color < namedColorCount; color++ {
		if color.Name() == name {
			return color
		}
	}
	return colorFromHex(name)
}

// colorFromHex reads #rgb or #rrggbb, with the channels an eight-bit color has.
func colorFromHex(value string) Color {
	if len(value) == 0 || value[0] != '#' {
		return None
	}
	digits, short := value[1:], false
	switch len(digits) {
	case 3:
		short = true
	case 6:
	default:
		return None
	}
	var channels [3]uint8
	for index := range channels {
		var text string
		if short {
			text = strings.Repeat(digits[index:index+1], 2)
		} else {
			text = digits[index*2 : index*2+2]
		}
		channel, err := strconv.ParseUint(text, 16, 8)
		if err != nil {
			return None
		}
		channels[index] = uint8(channel)
	}
	return RGB(channels[0], channels[1], channels[2])
}

// Colors lists the named colors a rule may use, in the order a chooser shows
// them.
func Colors() []Color { return []Color{Red, Yellow, Green, Cyan, Blue, Magenta} }

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
	Color         Color

	// fast, when set by Builtin, holds the literal matcher the package can run
	// instead of the expression. User rules never set it, so their behavior is
	// always the compiled expression.
	fast *fastMatcher
}

// fastMatcher matches builtin rules without regular expressions on ASCII lines.
// words are literal alternatives between word boundaries; quad matches IPv4.
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
	case !rule.Color.valid():
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
	color Color
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
			set.rules = append(set.rules, compiled{expression: expression, words: words, quad: rule.fast.quad, color: rule.Color})
			continue
		}
		set.rules = append(set.rules, compiled{expression: expression, color: rule.Color})
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

// Mark colors the bytes of line that a rule matches: marks[i] is the color of
// line[i]. It reports whether it marked anything. Bytes another rule already
// marked keep their color. marks must be as long as line and zeroed.
func (set *Set) Mark(line []byte, marks []Color) bool {
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
			any = markQuad(line, marks, rule.color) || any
		case fast && rule.words != nil:
			any = markWords(lower[:len(line)], marks, rule.words, rule.color) || any
		default:
			any = markExpression(rule.expression, line, marks, rule.color) || any
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
func markExpression(expression *regexp.Regexp, line []byte, marks []Color, color Color) bool {
	any := false
	for _, span := range expression.FindAllIndex(line, -1) {
		for index := span[0]; index < span[1]; index++ {
			if marks[index] == None {
				marks[index] = color
				any = true
			}
		}
	}
	return any
}

// markWords marks every whole-word occurrence of words in the already lower
// cased line. It reproduces \b at both ends of each alternative, including the
// non-word final character of "active (running)".
func markWords(line []byte, marks []Color, words [][]byte, color Color) bool {
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
					marks[index] = color
					any = true
				}
			}
		}
	}
	return any
}

// markQuad marks IPv4-shaped dotted quads the way \b\d{1,3}(\.\d{1,3}){3}\b
// does: four groups of one to three ASCII digits with word boundaries outside.
func markQuad(line []byte, marks []Color, color Color) bool {
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
				marks[index] = color
				any = true
			}
		}
		at = end - 1
	}
	return any
}

// Builtin returns the default rules for log levels, common status words and
// IPv4 addresses. Colors come from the terminal's scheme.
func Builtin() []Rule {
	return []Rule{
		wordRule(Red, "error", "errors", "err", "fail", "fails", "failed", "failure", "fatal", "panic", "denied", "refused", "critical", "exception", "traceback", "segfault", "segmentation fault"),
		wordRule(Yellow, "warn", "warning", "warnings", "deprecated", "timeout", "timed out", "retrying"),
		wordRule(Green, "info", "ok", "success", "successful", "successfully", "passed", "done", "connected", "active (running)"),
		wordRule(Blue, "debug"),
		wordRule(Magenta, "trace"),
		{Color: Cyan, Regex: true, Pattern: `\b\d{1,3}(\.\d{1,3}){3}\b`, fast: &fastMatcher{quad: true}},
	}
}

// wordRule builds a whole-word literal rule. Pattern is the same expression it
// always had, so Validate and the non-ASCII fallback keep their exact meaning;
// fast is the matcher Mark uses on plain ASCII lines.
func wordRule(color Color, words ...string) Rule {
	quoted := make([]string, len(words))
	for index, word := range words {
		quoted[index] = regexp.QuoteMeta(word)
	}
	return Rule{
		Color: color, Regex: true,
		Pattern: `\b(` + strings.Join(quoted, "|") + `)\b`,
		fast:    &fastMatcher{words: words},
	}
}
