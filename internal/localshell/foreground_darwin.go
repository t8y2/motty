//go:build darwin

package localshell

import (
	"bytes"
	"encoding/binary"

	"golang.org/x/sys/unix"
)

func processArgs(pid int) ([]string, bool) {
	if args, ok := darwinArgv(pid); ok {
		return args, true
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return nil, false
	}
	name := info.Proc.P_comm[:]
	if end := bytes.IndexByte(name, 0); end >= 0 {
		name = name[:end]
	}
	if len(name) == 0 {
		return nil, false
	}
	return []string{string(name)}, true
}

// darwinArgv reads KERN_PROCARGS2 of another process.
func darwinArgv(pid int) ([]string, bool) {
	buf, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil, false
	}
	return parseProcArgs2(buf)
}

// parseProcArgs2 reads a KERN_PROCARGS2 buffer: a 32-bit argc, the executable
// path, NUL padding, then argc NUL-terminated argument strings, then the
// environment. Every string up to argc is counted, empty ones included, so an
// empty argument cannot push the reader into the environment.
func parseProcArgs2(buf []byte) ([]string, bool) {
	if len(buf) < 4 {
		return nil, false
	}
	argc := int(binary.LittleEndian.Uint32(buf[:4]))
	if argc <= 0 || argc > 4096 {
		return nil, false
	}
	rest := buf[4:]
	end := bytes.IndexByte(rest, 0)
	if end < 0 {
		return nil, false
	}
	rest = rest[end+1:]
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}
	args := make([]string, 0, argc)
	for i := 0; i < argc && len(rest) > 0; i++ {
		var arg []byte
		if end = bytes.IndexByte(rest, 0); end < 0 {
			arg, rest = rest, nil
		} else {
			arg, rest = rest[:end], rest[end+1:]
		}
		if len(arg) > 0 {
			args = append(args, string(arg))
		}
	}
	return args, len(args) > 0
}
