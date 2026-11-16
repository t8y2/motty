package autofill

import (
	"strings"
	"testing"
	"time"
)

func newRule(t *testing.T, pattern string) *Rule {
	t.Helper()
	rule := &Rule{ID: "r1", Name: "sudo", Pattern: pattern, Send: SendPassword, Enabled: true}
	if err := rule.Compile(); err != nil {
		t.Fatal(err)
	}
	return rule
}

func all(*Rule) bool { return true }

func TestValidateRejectsWhatCouldNotBeAPrompt(t *testing.T) {
	for _, pattern := range []string{"", "   ", ".*", "a*", "x?", "(a|)", "^$"} {
		if _, err := Validate(pattern); err == nil {
			t.Errorf("%q was accepted", pattern)
		}
	}
	if _, err := Validate(strings.Repeat("a", MaxPattern+1)); err != ErrTooLong {
		t.Errorf("long pattern: %v", err)
	}
	if _, err := Validate("(unclosed"); err == nil {
		t.Error("bad syntax was accepted")
	} else if _, ok := IsSyntax(err); !ok {
		t.Errorf("syntax error not recognised: %v", err)
	}
	if _, err := Validate(`\[sudo\] password for \w+:`); err != nil {
		t.Errorf("a prompt pattern: %v", err)
	}
}

func TestRuleAnswersAPromptOnceAndNotItsReproduction(t *testing.T) {
	rule := newRule(t, `password.*:$`)
	rules := []*Rule{rule}
	var w Watcher
	line := "[sudo] password for me:"
	if got := w.Scan(line, rules, all); got != rule {
		t.Fatal("the prompt was not answered")
	}
	w.Fired(rule.ID, line)
	// A wrong answer brings the same prompt back, even after other output was
	// seen, as long as the user did not press Enter.
	w.Scan("Sorry, try again.", rules, all)
	if got := w.Scan(line, rules, all); got != nil {
		t.Fatal("the same prompt was answered twice")
	}
	// The user pressed Enter at the shell, and the shell was seen between: a new
	// occurrence of the prompt.
	w.Scan("me@host:~$", rules, all)
	w.Typed([]byte("sudo ls\r"))
	if got := w.Scan(line, rules, all); got != rule {
		t.Fatal("a later occurrence was not answered")
	}
}

func TestDifferentLineIsANewPrompt(t *testing.T) {
	rule := newRule(t, `^Password`)
	var w Watcher
	w.Fired(rule.ID, "Password:")
	if w.Scan("Password (attempt 2):", []*Rule{rule}, all) != rule {
		t.Fatal("a distinct line was refused")
	}
}

func TestEchoOfTypedTextIsNotAPrompt(t *testing.T) {
	rule := newRule(t, `password:$`)
	var w Watcher
	w.Typed([]byte("echo password:"))
	if w.Scan("$ echo password:", []*Rule{rule}, all) != nil {
		t.Fatal("typed text was answered")
	}
	w.Typed([]byte("\r"))
	if w.Scan("password:", []*Rule{rule}, all) != rule {
		t.Fatal("output after Enter was not answered")
	}
	w.Typed([]byte("\x1b[A"))
	if w.Scan("password:", []*Rule{rule}, all) != nil {
		t.Fatal("history recall may echo anything")
	}
}

func TestPauseAndRules(t *testing.T) {
	var w Watcher
	if _, pending := w.Pending(0); pending {
		t.Fatal("nothing arrived yet")
	}
	w.Output(time.Second)
	if wait, pending := w.Pending(time.Second + 50*time.Millisecond); !pending || wait != Pause-50*time.Millisecond {
		t.Fatalf("wait %v pending %v", wait, pending)
	}
	if wait, _ := w.Pending(time.Second + time.Minute); wait != 0 {
		t.Fatal("should be due")
	}
	off := newRule(t, `x:`)
	off.Enabled = false
	if w.Scan("x:", []*Rule{off}, all) != nil {
		t.Fatal("a disabled rule fired")
	}
	on := newRule(t, `x:`)
	if w.Scan("x:", []*Rule{on}, func(*Rule) bool { return false }) != nil {
		t.Fatal("a rule out of scope fired")
	}
	if _, pending := w.Pending(time.Hour); pending {
		t.Fatal("scan should consume the pending output")
	}
}

func TestScope(t *testing.T) {
	rule := &Rule{Scope: []string{"prod", "Build"}}
	if !rule.AppliesTo("web1", "Prod") || !rule.AppliesTo("build", "") || rule.AppliesTo("web1", "dev") {
		t.Fatal("scope")
	}
	if !(&Rule{}).AppliesTo("any", "") {
		t.Fatal("empty scope is every host")
	}
}

// The saved state names the answer kinds; an unknown name must not come back as
// a different kind.
func TestSendNamesRoundTrip(t *testing.T) {
	for _, send := range []Send{SendPassword, SendOTP, SendText} {
		if got := SendFromString(send.String()); got != send {
			t.Fatalf("%v round-tripped to %v", send, got)
		}
	}
	if SendFromString("") != 0 || SendFromString("nonsense") != 0 {
		t.Fatal("an unknown answer name became a kind")
	}
}
