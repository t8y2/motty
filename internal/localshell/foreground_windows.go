//go:build windows

package localshell

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsForeground is the argv of the program in front under the shell with
// the given pid and start time. The start time guards against the pid having
// been handed to another process once the shell is gone.
func windowsForeground(shell uint32, shellCreated uint64) ([]string, bool) {
	if now, ended := processStarted(shell); ended || now == 0 || now != shellCreated {
		return nil, false
	}
	procs, ok := processSnapshot()
	if !ok {
		return nil, false
	}
	pid := pickForeground(procs, shell, shellCreated, processStarted)
	if args, ok := processCommandLine(pid); ok {
		return args, true
	}
	for _, proc := range procs {
		if proc.pid == pid {
			return []string{proc.exe}, true
		}
	}
	return nil, false
}

// processSnapshot lists the running processes with their parents.
func processSnapshot() ([]procInfo, bool) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, false
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if windows.Process32First(snapshot, &entry) != nil {
		return nil, false
	}
	var procs []procInfo
	for {
		procs = append(procs, procInfo{pid: entry.ProcessID, ppid: entry.ParentProcessID, exe: windows.UTF16ToString(entry.ExeFile[:])})
		if windows.Process32Next(snapshot, &entry) != nil {
			return procs, true
		}
	}
}

// stillActive is the exit code GetExitCodeProcess reports for a running process.
const stillActive = 259

// processStarted is when a process started, as a FILETIME, or 0 when it cannot
// be read, and whether it has ended already.
func processStarted(pid uint32) (start uint64, ended bool) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(handle)
	var code uint32
	ended = windows.GetExitCodeProcess(handle, &code) == nil && code != stillActive
	return processStart(handle), ended
}

// processStart reads when a process started, or 0 when that cannot be read.
func processStart(handle windows.Handle) uint64 {
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(handle, &created, &exited, &kernel, &user) != nil {
		return 0
	}
	return uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
}

// processCommandLine reads another process's command line and splits it the way
// Windows programs do. It fails for a process this one may not look into, such
// as an elevated one; the caller then has only the program's name.
func processCommandLine(pid uint32) ([]string, bool) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return nil, false
	}
	defer windows.CloseHandle(handle)
	size := uint32(1024)
	for range 3 {
		// A UNICODE_STRING header and the text it points at are returned in
		// one buffer. 8-byte units keep the header aligned.
		buf := make([]uint64, (size+7)/8)
		var need uint32
		status := windows.NtQueryInformationProcess(handle, windows.ProcessCommandLineInformation, unsafe.Pointer(&buf[0]), size, &need)
		if status != nil {
			switch status {
			case windows.STATUS_INFO_LENGTH_MISMATCH, windows.STATUS_BUFFER_OVERFLOW, windows.STATUS_BUFFER_TOO_SMALL:
				if need > size {
					size = need
					continue
				}
			}
			return nil, false
		}
		text := (*windows.NTUnicodeString)(unsafe.Pointer(&buf[0]))
		if text.Buffer == nil || text.Length == 0 {
			return nil, false
		}
		line := windows.UTF16ToString(unsafe.Slice(text.Buffer, text.Length/2))
		runtime.KeepAlive(buf)
		args, err := windows.DecomposeCommandLine(line)
		if err != nil || len(args) == 0 {
			return nil, false
		}
		return args, true
	}
	return nil, false
}
