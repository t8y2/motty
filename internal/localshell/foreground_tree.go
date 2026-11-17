package localshell

import "strings"

// This file chooses the foreground program of a Windows shell from a table of
// processes. A ConPTY has no foreground process group to ask, so the answer is
// built from the process tree: the shell's newest child, looked through when it
// is itself a shell. It is plain data in and out, so it runs and is tested on
// every platform.

// procInfo is one row of a process snapshot.
type procInfo struct {
	pid, ppid uint32
	// exe is the executable's file name, as the snapshot gives it.
	exe string
}

// maxForegroundDepth bounds how many nested shells are followed.
const maxForegroundDepth = 16

// ignoredChildren are helper processes a console attaches to its shell.
var ignoredChildren = map[string]bool{
	"conhost.exe":     true,
	"openconsole.exe": true,
}

// interactiveShells are programs a user types commands into: when one is the
// newest child it is looked through, not reported, so a PowerShell inside cmd
// that runs an agent shows the agent.
var interactiveShells = map[string]bool{
	"cmd.exe": true, "powershell.exe": true, "pwsh.exe": true, "bash.exe": true,
	"sh.exe": true, "zsh.exe": true, "fish.exe": true, "nu.exe": true,
}

// pickForeground returns the pid of the program in front under root: the
// shell's newest child, then that child's newest child while it is an
// interactive shell, and the last one reached otherwise. Only direct children
// count, so tools an agent starts (git, rg, bash) never replace the agent.
//
// started gives a process's start time (a FILETIME), or 0 when it is unknown,
// and whether the process has already ended: the snapshot still lists an ended
// process while some other program holds a handle to it. A child that started
// before its parent is an orphan whose parent id was reused by the parent's
// pid. Ended processes and orphans are skipped.
func pickForeground(procs []procInfo, root uint32, rootCreated uint64, started func(pid uint32) (start uint64, ended bool)) uint32 {
	current, currentCreated := root, rootCreated
	for range maxForegroundDepth {
		var best procInfo
		var bestCreated uint64
		found := false
		for _, proc := range procs {
			if proc.ppid != current || proc.pid == current || ignoredChildren[strings.ToLower(proc.exe)] {
				continue
			}
			start, ended := started(proc.pid)
			if ended {
				continue
			}
			if start != 0 && currentCreated != 0 && start < currentCreated {
				continue
			}
			if !found || start > bestCreated {
				best, bestCreated, found = proc, start, true
			}
		}
		if !found {
			return current
		}
		current, currentCreated = best.pid, bestCreated
		if !interactiveShells[strings.ToLower(best.exe)] {
			return current
		}
	}
	return current
}
