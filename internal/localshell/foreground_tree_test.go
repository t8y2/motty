package localshell

import "testing"

func TestPickForegroundFollowsTheNewestChild(t *testing.T) {
	const shell = 100
	cases := []struct {
		name  string
		procs []procInfo
		times map[uint32]uint64
		want  uint32
	}{
		{"prompt", nil, nil, shell},
		{"console helper is skipped", []procInfo{{101, shell, "conhost.exe"}, {102, shell, "OpenConsole.exe"}}, nil, shell},
		{"pwsh runs a node shim", []procInfo{{101, shell, "conhost.exe"}, {110, shell, "node.exe"}}, map[uint32]uint64{110: 50}, 110},
		{"newest of two children", []procInfo{{110, shell, "ping.exe"}, {111, shell, "node.exe"}}, map[uint32]uint64{110: 40, 111: 60}, 111},
		{"nested shells are looked through", []procInfo{{110, shell, "pwsh.exe"}, {120, 110, "claude.exe"}}, map[uint32]uint64{110: 40, 120: 60}, 120},
		{"an idle nested shell is the answer", []procInfo{{110, shell, "pwsh.exe"}}, map[uint32]uint64{110: 40}, 110},
		{"tools an agent starts do not replace it", []procInfo{{110, shell, "claude.exe"}, {120, 110, "git.exe"}}, map[uint32]uint64{110: 40, 120: 60}, 110},
		{"a reused parent id is rejected", []procInfo{{110, shell, "node.exe"}, {111, shell, "stale.exe"}}, map[uint32]uint64{110: 60, 111: 5}, 110},
		{"unknown start time still counts", []procInfo{{110, shell, "node.exe"}}, nil, 110},
		{"a process is not its own child", []procInfo{{shell, shell, "cmd.exe"}}, nil, shell},
		{"a loop is cut off", []procInfo{{110, shell, "cmd.exe"}, {shell, 110, "cmd.exe"}}, map[uint32]uint64{110: 60, shell: 70}, shell},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := pickForeground(test.procs, shell, 10, func(pid uint32) (uint64, bool) { return test.times[pid], false })
			if got != test.want {
				t.Fatalf("foreground = %d, want %d", got, test.want)
			}
		})
	}
}

func TestPickForegroundSkipsAChildThatHasEnded(t *testing.T) {
	procs := []procInfo{{110, 100, "node.exe"}, {111, 100, "claude.exe"}}
	times := map[uint32]uint64{110: 40, 111: 60}
	got := pickForeground(procs, 100, 10, func(pid uint32) (uint64, bool) { return times[pid], pid == 111 })
	if got != 110 {
		t.Fatalf("foreground = %d, want 110", got)
	}
}
