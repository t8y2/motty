// Package cmdhistory remembers the commands a user types into terminals, and
// above all refuses to remember anything that might be a secret.
//
// A terminal client sees keystrokes and screen cells, never the shell's own
// state. The Recorder therefore records a line only when the screen proves the
// line was echoed back as it was typed: a password typed at a prompt that does
// not echo leaves nothing on the screen to prove, so it is never recorded. The
// screen check is backed by filters on the prompt's wording and on the command's
// text. When in doubt a command is dropped; a missed command costs nothing, a
// recorded secret does.
package cmdhistory

import (
	"regexp"
	"strings"
	"unicode"
)

// MaxCommand is the longest command that is recorded, in bytes.
const MaxCommand = 2000

// promptWords match the text before the cursor when a program asks for
// something that must not be kept. The wording of the prompt is the only thing
// a client can read about what the program will do with the reply.
var promptWords = regexp.MustCompile(`(?i)password|passphrase|passcode|passwd|\bpass\b|\bpin\b|verification code|security code|auth(?:entication)? code|\botp\b|\bmfa\b|\b2fa\b|token|secret|credential|api key|\[sudo\]|密码|口令|验证码|动态码|安全码|令牌|密钥|密碼|驗證碼|動態碼|安全碼|權杖|金鑰|密鑰`)

// PasswordPrompt reports whether text reads like a prompt for a secret.
func PasswordPrompt(text string) bool { return promptWords.MatchString(text) }

// secretText matches commands that carry a secret in their own text. Only
// shapes that are unambiguous are listed: a filter that guessed at every "-p"
// would drop half of all commands and still miss real secrets.
var secretText = regexp.MustCompile(`(?i)` + strings.Join([]string{
	`(?:password|passwd|token|secret|api[_-]?key|access[_-]?key|private[_-]?key|auth)\w*\s*=`, // KEY=value, ?token=...
	`--(?:password|passwd|pass|token|secret|api[_-]?key)(?:[= ]|$)`,                           // --password hunter2
	`\bauthorization\s*:`, // curl -H 'Authorization: ...'
	`\bbearer\s+\S`,
	`aws_secret`,
	`-----begin`,
	`://[^\s/:@]+:[^\s/@]+@`, // scheme://user:pass@host
	`\bsshpass\b`,
}, "|"))

// Secret reports whether a command's text looks like it carries a secret.
func Secret(command string) bool { return secretText.MatchString(command) }

// clean returns the command as it should be stored, or false when it must not
// be recorded at all.
func clean(line string) (string, bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' {
		return "", false // HISTCONTROL=ignorespace: the user asked for no trace
	}
	line = strings.TrimSpace(line)
	if line == "" || len(line) > MaxCommand {
		return "", false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '�' {
			return "", false
		}
	}
	if Secret(line) {
		return "", false
	}
	return line, true
}
