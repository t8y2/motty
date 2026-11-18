package localshell

import "testing"

// TestIdleTellsAPromptFromAProgramInFront checks that a local shell reports idle
// only when the program in front is a shell itself, and that an unreadable
// foreground is reported as unknown rather than guessed. Closing a tab only
// skips the confirmation when idle is known and true.
func TestIdleTellsAPromptFromAProgramInFront(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		ok    bool
		idle  bool
		known bool
	}{
		{"posix prompt", []string{"/bin/bash"}, true, true, true},
		{"powershell prompt", []string{`C:\Program Files\PowerShell\7\pwsh.exe`}, true, true, true},
		{"cmd prompt", []string{`C:\Windows\System32\cmd.exe`}, true, true, true},
		{"a command in front", []string{"vim", "notes.txt"}, true, false, true},
		{"an agent in front", []string{"claude"}, true, false, true},
		{"platform cannot tell", nil, false, false, false},
		{"empty argv", []string{}, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shell := &Shell{done: make(chan struct{})}
			shell.b.foreground = func() ([]string, bool) { return tc.args, tc.ok }
			idle, known := shell.Idle()
			if idle != tc.idle || known != tc.known {
				t.Fatalf("Idle() = %v, %v; want %v, %v", idle, known, tc.idle, tc.known)
			}
		})
	}
}

// TestIdleIsUnknownOnceTheShellEnded makes sure a finished shell is never called
// idle: its process id may already belong to another program.
func TestIdleIsUnknownOnceTheShellEnded(t *testing.T) {
	shell := &Shell{done: make(chan struct{})}
	shell.b.foreground = func() ([]string, bool) { return []string{"/bin/bash"}, true }
	close(shell.done)
	if idle, known := shell.Idle(); idle || known {
		t.Fatalf("Idle() on an ended shell = %v, %v; want false, false", idle, known)
	}
}
