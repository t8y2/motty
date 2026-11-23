// Package autofill holds the rules that answer a prompt in a terminal by
// themselves, and the per-terminal state that keeps them safe: a rule fires on
// remote output only, never on what the user typed, never on the alternate
// screen, and never twice for one prompt.
//
// The package knows nothing about secrets or windows. The caller says which
// rules may apply to a terminal, gives the line under the cursor once output has
// paused, and decides what a fired rule sends.
package autofill

import (
	"errors"
	"regexp"
	"regexp/syntax"
	"strings"
	"time"
)

// Send is what a rule answers with.
type Send uint8

const (
	// SendPassword is the host's saved password.
	SendPassword Send = iota + 1
	// SendOTP is the host's current one-time code.
	SendOTP
	// SendText is a fixed text the caller keeps in its vault.
	SendText
)

func (send Send) String() string {
	switch send {
	case SendPassword:
		return "password"
	case SendOTP:
		return "otp"
	case SendText:
		return "text"
	}
	return ""
}

// SendFromString is the inverse of String; unknown names give 0.
func SendFromString(value string) Send {
	switch value {
	case "password":
		return SendPassword
	case "otp":
		return SendOTP
	case "text":
		return SendText
	}
	return 0
}

const (
	// MaxPattern is the longest pattern a rule may have.
	MaxPattern = 200
	// MaxRules is how many rules there can be.
	MaxRules = 32
	// Pause is how long the output must be quiet before the cursor line is
	// looked at: a prompt is the last thing a program prints before it waits.
	Pause = 150 * time.Millisecond
)

var (
	ErrEmpty        = errors.New("autofill: empty pattern")
	ErrTooLong      = errors.New("autofill: pattern too long")
	ErrMatchesEmpty = errors.New("autofill: pattern matches the empty line")
)

// Rule answers a prompt. ID is stable across edits and names the rule's
// fixed text in the vault.
type Rule struct {
	ID      string
	Name    string
	Pattern string
	Send    Send
	// Enter presses Enter after the answer.
	Enter bool
	// Enabled is false until the user turns the rule on.
	Enabled bool
	// Scope lists the host labels and group names the rule is limited to; empty
	// means every host.
	Scope []string

	re *regexp.Regexp
}

// Validate checks a pattern and returns it compiled. RE2 matches in time linear
// in the line, so a pattern cannot stall the window. A pattern that matches the
// empty string would match every line, which is never a prompt.
func Validate(pattern string) (*regexp.Regexp, error) {
	switch {
	case strings.TrimSpace(pattern) == "":
		return nil, ErrEmpty
	case len(pattern) > MaxPattern:
		return nil, ErrTooLong
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	if re.MatchString("") {
		return nil, ErrMatchesEmpty
	}
	return re, nil
}

// IsSyntax reports a pattern the regexp parser refused, and its message.
func IsSyntax(err error) (string, bool) {
	var syntaxError *syntax.Error
	if errors.As(err, &syntaxError) {
		return string(syntaxError.Code), true
	}
	return "", false
}

// Compile makes the rule usable. A rule that does not compile never matches.
func (rule *Rule) Compile() error {
	re, err := Validate(rule.Pattern)
	rule.re = re
	return err
}

// Matches reports whether the line is the prompt this rule answers.
func (rule *Rule) Matches(line string) bool {
	return rule.re != nil && line != "" && rule.re.MatchString(line)
}

// AppliesTo reports whether the rule is on for a host.
func (rule *Rule) AppliesTo(label, group string) bool {
	if len(rule.Scope) == 0 {
		return true
	}
	for _, entry := range rule.Scope {
		if entry != "" && (strings.EqualFold(entry, label) || strings.EqualFold(entry, group)) {
			return true
		}
	}
	return false
}

// Watcher is the state of one terminal.
type Watcher struct {
	dirty  bool
	quiet  time.Duration // when the last output arrived
	typed  []byte        // what the user typed on the current line
	taint  bool          // the user typed more than can be told from an echo
	enters uint32        // times the user pressed Enter, ever
	fired  []firing
	// asked lists the rules the user confirmed for this session.
	allowed []string
}

// firing is the last answer a rule gave: the line it answered, and what the
// terminal has done since.
type firing struct {
	rule   string
	line   string
	left   bool   // a line that is not the prompt has been seen since
	enters uint32 // the user's Enters when it fired
}

const maxTyped = 256

// Output notes that the remote wrote something at time now.
func (w *Watcher) Output(now time.Duration) {
	w.dirty, w.quiet = true, now
}

// Pending reports whether output is waiting to be looked at, and for how much
// longer it must be quiet before that.
func (w *Watcher) Pending(now time.Duration) (wait time.Duration, pending bool) {
	if !w.dirty {
		return 0, false
	}
	return max(w.quiet+Pause-now, 0), true
}

// Skip drops pending output without looking at it.
func (w *Watcher) Skip() { w.dirty = false }

// HasInput reports unsubmitted user input, including input the remote may have
// chosen not to echo, such as a manually typed password.
func (w *Watcher) HasInput() bool { return len(w.typed) > 0 || w.taint }

// Retry makes the line be looked at again at the next pause, as when a dialog
// held the answer back.
func (w *Watcher) Retry(now time.Duration) { w.dirty, w.quiet = true, now-Pause }

// Typed notes what the user sent to the shell. The echo of it comes back as
// output, and must not be taken for a prompt.
func (w *Watcher) Typed(p []byte) {
	for _, b := range p {
		switch {
		case b == '\r' || b == '\n':
			w.typed, w.taint = w.typed[:0], false
			w.enters++
		case b == 0x03 || b == 0x04 || b == 0x15:
			w.typed, w.taint = w.typed[:0], false
		case b == 0x7f || b == 0x08:
			if n := len(w.typed); n > 0 {
				w.typed = w.typed[:n-1]
			}
		case b == 0x1b || b < 0x20:
			w.taint = true
		case len(w.typed) < maxTyped:
			w.typed = append(w.typed, b)
		default:
			w.taint = true
		}
	}
}

func (w *Watcher) UnknownInput() { w.taint = true }

// echo reports whether the line may be what the user typed.
func (w *Watcher) echo(line string) bool {
	return w.taint || len(w.typed) > 0 && strings.Contains(line, string(w.typed))
}

// Scan looks at the line under the cursor after a pause and returns the rule to
// answer it with, if there is one. usable says whether a rule applies to this
// terminal. The caller calls Fired once it has committed to the answer; until
// then nothing is spent.
func (w *Watcher) Scan(line string, rules []*Rule, usable func(*Rule) bool) *Rule {
	w.dirty = false
	if w.echo(line) {
		return nil
	}
	var found *Rule
	for _, rule := range rules {
		if !rule.Enabled || !usable(rule) {
			continue
		}
		if !rule.Matches(line) {
			w.leave(rule.ID)
			continue
		}
		if found == nil && w.mayFire(rule.ID, line) {
			found = rule
		}
	}
	return found
}

func (w *Watcher) leave(id string) {
	for index := range w.fired {
		if w.fired[index].rule == id {
			w.fired[index].left = true
		}
	}
}

// mayFire is false for a rule that already answered this line: the same text
// comes back when an answer was wrong, and answering it again would repeat the
// mistake until the account locks. The text is a new prompt only after the
// terminal showed something else and the user pressed Enter in between.
func (w *Watcher) mayFire(id, line string) bool {
	for _, last := range w.fired {
		if last.rule == id {
			return last.line != line || last.left && w.enters != last.enters
		}
	}
	return true
}

// Fired records that the rule answers the line.
func (w *Watcher) Fired(id, line string) {
	entry := firing{rule: id, line: line, enters: w.enters}
	for index := range w.fired {
		if w.fired[index].rule == id {
			w.fired[index] = entry
			return
		}
	}
	w.fired = append(w.fired, entry)
}

// Allowed reports whether the user has confirmed the rule in this session.
func (w *Watcher) Allowed(id string) bool {
	for _, allowed := range w.allowed {
		if allowed == id {
			return true
		}
	}
	return false
}

// Allow remembers the user's confirmation for the rest of the session.
func (w *Watcher) Allow(id string) {
	if !w.Allowed(id) {
		w.allowed = append(w.allowed, id)
	}
}
