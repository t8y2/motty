package cmdhistory

import "testing"

// A completion list may only open where the screen proves a shell prompt. When
// the shell's own marker is gone (a nested shell after sudo), a program's
// prompt looks like a prompt too, so the shapes below must be refused.
func TestAProgramsOwnPromptIsNotAShellPrompt(t *testing.T) {
	for _, prompt := range []string{
		">>> ", "> ", ">>", "...> ",
		"In [1]: ", "In [12]: ",
		"--More--", "(END)",
		":", "::",
		"mysql> ", "sqlite> ", "sqlite3> ", "MariaDB [db]> ", "SQL> ",
		"node> ", "irb(main):001:0> ", "[1] pry(main)> ", "postgres=# ", "postgres-# ",
	} {
		if !ProgramPrompt(prompt) {
			t.Errorf("%q was not recognised as a program's prompt", prompt)
		}
	}
	for _, prompt := range []string{
		"$ ", "# ", "user@host:~$ ", "root@host:/home/u# ",
		"[deploy@prod ~]$ ", "deploy@prod:~/srv$ ", "❯ ", "➜  ~ ",
		"PS C:\\Users\\me> ",
	} {
		if ProgramPrompt(prompt) {
			t.Errorf("shell prompt %q was taken for a program's", prompt)
		}
	}
}
