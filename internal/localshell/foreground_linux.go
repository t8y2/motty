//go:build linux

package localshell

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

func processArgs(pid int) ([]string, bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err == nil {
		parts := bytes.Split(data, []byte{0})
		args := make([]string, 0, len(parts))
		for _, part := range parts {
			if len(part) > 0 {
				args = append(args, string(part))
			}
		}
		if len(args) > 0 {
			return args, true
		}
	}
	comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return nil, false
	}
	name := strings.TrimSpace(string(comm))
	if name == "" {
		return nil, false
	}
	return []string{name}, true
}
