package sshconn

import (
	"errors"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// proxyCommandProcess runs a command line the way OpenSSH for Windows does:
// the program is its first word, quoted or not, and the program reads the rest
// of the line itself, so a path such as "C:\Program Files\..." keeps its
// backslashes. No console window opens for it.
func proxyCommandProcess(line string) (*exec.Cmd, error) {
	program := firstCommandWord(line)
	if program == "" {
		return nil, errors.New("the proxy command names no program")
	}
	process := exec.Command(program)
	process.SysProcAttr = &syscall.SysProcAttr{CmdLine: line, HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return process, nil
}

// firstCommandWord is the program of a command line, as Windows reads it: up to
// the closing quote when it starts with one, else up to the first blank.
func firstCommandWord(line string) string {
	line = strings.TrimLeft(line, " \t")
	if rest, quoted := strings.CutPrefix(line, `"`); quoted {
		program, _, _ := strings.Cut(rest, `"`)
		return program
	}
	if end := strings.IndexAny(line, " \t"); end >= 0 {
		return line[:end]
	}
	return line
}

// startProxyCommand starts the program inside a job object that is closed with
// the connection, so the programs it started end with it; a job that cannot be
// made leaves only the program itself to kill.
func startProxyCommand(process *exec.Cmd) (proxyCommandTree, error) {
	if err := process.Start(); err != nil {
		return proxyCommandTree{}, err
	}
	alone := proxyCommandTree{kill: func() { _ = process.Process.Kill() }, release: func() {}}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return alone, nil
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)))
	if err != nil {
		windows.CloseHandle(job)
		return alone, nil
	}
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(process.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return alone, nil
	}
	err = windows.AssignProcessToJobObject(job, handle)
	windows.CloseHandle(handle)
	if err != nil {
		windows.CloseHandle(job)
		return alone, nil
	}
	// A closed handle's number may be handed out again, so neither call touches
	// the job once it is closed.
	var (
		lock   sync.Mutex
		closed bool
	)
	return proxyCommandTree{
		kill: func() {
			lock.Lock()
			defer lock.Unlock()
			if !closed {
				_ = windows.TerminateJobObject(job, 1)
			}
		},
		// Closing the job also ends what the program left running.
		release: func() {
			lock.Lock()
			defer lock.Unlock()
			if !closed {
				closed = true
				windows.CloseHandle(job)
			}
		},
	}, nil
}
