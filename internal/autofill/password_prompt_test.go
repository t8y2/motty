package autofill

import "testing"

func TestAccountPasswordPrompts(t *testing.T) {
	for pattern, cases := range map[string][]string{
		SudoPromptPattern: {
			"[sudo] password for alice: ", "[sudo] alice 的密码：",
			"[sudo: authenticate] Password: ", "[sudo: 验证] 密碼：",
		},
		SuPromptPattern: {"Password: ", "密码：", "密碼: ", "口令："},
		SSHPromptPattern: {
			"deploy@server-b's password: ", "root@192.0.2.1's password:",
			"alice@2001:db8::1's password: ", "Alice@server.example's Password:",
		},
	} {
		rule := Rule{Pattern: pattern}
		if err := rule.Compile(); err != nil {
			t.Fatal(err)
		}
		for _, line := range cases {
			if !rule.Matches(line) {
				t.Errorf("%q did not recognise %q", pattern, line)
			}
		}
		for _, line := range []string{
			"", "$ ssh deploy@server-b", "$ echo Password:", "Password: already typed",
			"Enter passphrase for key '/home/dev/.ssh/id_ed25519': ",
			"Verification code: ", "One-time password: ", "New password: ",
			"Retype new password: ", "deploy@server-b's password has expired",
			"echo deploy@server-b's password:", "[sudoku] password for alice:",
		} {
			if rule.Matches(line) {
				t.Errorf("%q mistook %q for an account password prompt", pattern, line)
			}
		}
	}
}

func TestPasswordPromptUser(t *testing.T) {
	for line, want := range map[string]string{
		"deploy@server-b's password: ":           "deploy",
		"Alice@2001:db8::1's password:":          "Alice",
		"[sudo] password for alice: ":            "alice",
		"[sudo: authenticate] password for dev:": "dev",
		"[sudo] alice 的密码：":                      "alice",
		"[sudo] alice 的密碼：":                      "alice",
		"[sudo: authenticate] Password: ":        "",
		"Password: ":                             "",
		"Verification code: ":                    "",
	} {
		if got := PasswordPromptUser(line); got != want {
			t.Errorf("PasswordPromptUser(%q) = %q, want %q", line, got, want)
		}
	}
}

func TestPasswordAssistDetectsUnsubmittedInputWithoutEcho(t *testing.T) {
	var watcher Watcher
	watcher.Typed([]byte("ssh deploy@server-b\r"))
	if watcher.HasInput() {
		t.Fatal("a submitted command remained pending")
	}
	watcher.Typed([]byte("manual-secret"))
	if !watcher.HasInput() {
		t.Fatal("unsubmitted password was not tracked")
	}
	watcher.Typed([]byte("\x15"))
	if watcher.HasInput() {
		t.Fatal("cleared input remained pending")
	}
}
