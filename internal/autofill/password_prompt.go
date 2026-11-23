package autofill

import "regexp"

// Account password prompts recognised by the built-in assist. Keep these
// anchored: a command or a message merely mentioning a password is not a prompt.
// Key passphrases and one-time codes have their own authentication paths.
const (
	SudoPromptPattern = `(?i)^\[sudo(?::[^\]]*)?\][^\n]*(?:password|密码|密碼)[^\n]*[:：]\s*$`
	SuPromptPattern   = `(?i)^(?:password|密码|密碼|口令)\s*[:：]\s*$`
	SSHPromptPattern  = `(?i)^([^\s@]+)@[^\s']+'s password\s*[:：]\s*$`
)

var (
	sshPromptUser  = regexp.MustCompile(SSHPromptPattern)
	sudoPromptUser = regexp.MustCompile(`(?i)^\[sudo(?::[^\]]*)?\]\s*(?:password for ([^\s:：]+)|([^\s:：]+)\s*的密[码碼])\s*[:：]\s*$`)
)

// PasswordPromptUser is the account named by a recognised prompt. A bare su
// prompt and sudo-rs's generic prompt do not identify the account; in particular,
// su need not be switching to root, and a nested SSH need not use the outer user.
func PasswordPromptUser(line string) string {
	if match := sshPromptUser.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	if match := sudoPromptUser.FindStringSubmatch(line); match != nil {
		if match[1] != "" {
			return match[1]
		}
		return match[2]
	}
	return ""
}
