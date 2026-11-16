package remotetools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const guardBoot = "12345678-1234-1234-1234-123456789abc"

func TestSafeSnapshotPairsIdentityAcrossCollection(t *testing.T) {
	runner := &scriptedRunner{results: []scriptedResult{
		{output: "boot " + guardBoot + "\n42 100\n43 200\n"},
		{output: "42 alice 1.0 2.0 00:01 old worker\n43 bob 0.1 0.2 00:02 replaced worker\n"},
		{output: "boot " + guardBoot + "\n42 100\n43 201\n"},
	}}
	snapshot, err := ReadSafeProcessSnapshot(context.Background(), runner, SortCPU)
	if err != nil || len(snapshot.Processes) != 2 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if snapshot.Processes[0].StartIdentity != "linux:"+guardBoot+":100" || snapshot.Processes[1].StartIdentity != "" {
		t.Fatalf("reused PID got an actionable identity: %+v", snapshot.Processes)
	}
}

func TestSafeSnapshotUnsupportedIdentityStillListsProcesses(t *testing.T) {
	runner := &scriptedRunner{results: []scriptedResult{
		{err: errors.New("no procfs")},
		{output: "42 alice 1.0 2.0 00:01 worker\n"},
	}}
	snapshot, err := ReadSafeProcessSnapshot(context.Background(), runner, SortCPU)
	if err != nil || len(snapshot.Processes) != 1 || snapshot.Processes[0].StartIdentity != "" || len(runner.commands) != 2 {
		t.Fatalf("read-only unsupported host: %+v, %v, commands=%d", snapshot, err, len(runner.commands))
	}
}

func TestGuardedProcessActionsRejectUnsafeTargetsBeforeRunning(t *testing.T) {
	for _, process := range []Process{{PID: 1, StartIdentity: "linux:" + guardBoot + ":100"}, {PID: 42}, {PID: 42, StartIdentity: "linux:" + guardBoot + ":100; touch /tmp/bad"}} {
		runner := &fixedRunner{}
		if err := SendSignalToProcess(context.Background(), runner, process, Kill); err == nil || runner.asked != "" {
			t.Fatalf("unsafe target ran a command: %+v err=%v command=%q", process, err, runner.asked)
		}
		if err := ReniceProcess(context.Background(), runner, process, 0); err == nil || runner.asked != "" {
			t.Fatalf("unsafe renice target ran: %+v err=%v", process, err)
		}
	}
}

func TestGuardedProcessActionsDoNotAcceptAnEmptySuccess(t *testing.T) {
	runner := &fixedRunner{}
	err := SendSignalToProcess(context.Background(), runner, Process{PID: 42, StartIdentity: "linux:" + guardBoot + ":100"}, Terminate)
	if !errors.Is(err, ErrProcessUnverifiable) || !strings.Contains(runner.asked, "kill -TERM 42") {
		t.Fatalf("missing acknowledgement was trusted: err=%v command=%q", err, runner.asked)
	}
}

func TestGuardedProcessActionAcknowledgementMustMatchItsNonce(t *testing.T) {
	runner := &fixedRunner{output: "motty-guard-00000000000000000000000000000000:ok\n"}
	err := ReniceProcess(context.Background(), runner, Process{PID: 42, StartIdentity: "linux:" + guardBoot + ":100"}, 0)
	if !errors.Is(err, ErrProcessUnverifiable) {
		t.Fatalf("unrelated acknowledgement trusted: %v", err)
	}
}

func TestSafeSnapshotDoesNotDowngradeCancellationToReadonly(t *testing.T) {
	runner := &scriptedRunner{results: []scriptedResult{{err: context.DeadlineExceeded}}}
	if _, err := ReadSafeProcessSnapshot(context.Background(), runner, SortCPU); !errors.Is(err, context.DeadlineExceeded) || len(runner.commands) != 1 {
		t.Fatalf("deadline allowed another command: err=%v commands=%d", err, len(runner.commands))
	}
}

type procFixtureRunner struct {
	root string
	log  string
}

func (runner *procFixtureRunner) RunCommand(ctx context.Context, command string, _ int) ([]byte, error) {
	command = strings.ReplaceAll(command, "/proc/", runner.root+"/")
	prelude := "kill() { printf '%s\\n' \"kill $*\" >> \"$ACTION_LOG\"; }; renice() { printf '%s\\n' \"renice $*\" >> \"$ACTION_LOG\"; }; "
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", prelude+command)
	cmd.Env = append(os.Environ(), "ACTION_LOG="+runner.log)
	return cmd.Output()
}

func procFixture(t *testing.T, ticks, comm string) *procFixtureRunner {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"sys/kernel/random", "42"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "sys/kernel/random/boot_id"), []byte(guardBoot+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Fields 3..21 precede the start tick at field 22.
	stat := "42 (" + comm + ") S" + strings.Repeat(" 0", 18) + " " + ticks + " 0 0\n"
	if err := os.WriteFile(filepath.Join(root, "42/stat"), []byte(stat), 0600); err != nil {
		t.Fatal(err)
	}
	return &procFixtureRunner{root: root, log: filepath.Join(root, "actions")}
}

func TestGuardedShellChecksBootAndTicksBeforeEveryMutation(t *testing.T) {
	for _, test := range []struct {
		name, ticks, identity, comm string
		want                        error
	}{
		{"same", "100", "linux:" + guardBoot + ":100", "worker ) with (spaces)", nil},
		{"reused", "101", "linux:" + guardBoot + ":100", "worker", ErrProcessChanged},
		{"rebooted", "100", "linux:aaaaaaaa-1234-1234-1234-123456789abc:100", "worker", ErrProcessChanged},
		{"malformed", "not-a-number", "linux:" + guardBoot + ":100", "worker", ErrProcessUnverifiable},
		{"newline-comm", "100", "linux:" + guardBoot + ":100", "worker\ninjected", ErrProcessUnverifiable},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, nice := range []bool{false, true} {
				runner := procFixture(t, test.ticks, test.comm)
				process := Process{PID: 42, StartIdentity: test.identity, Command: "$(touch /tmp/do-not-run)"}
				var err error
				if nice {
					err = ReniceProcess(context.Background(), runner, process, -5)
				} else {
					err = SendSignalToProcess(context.Background(), runner, process, Kill)
				}
				if !errors.Is(err, test.want) {
					t.Fatalf("renice=%v: got %v want %v", nice, err, test.want)
				}
				actions, readErr := os.ReadFile(runner.log)
				if test.want != nil {
					if readErr == nil || len(actions) != 0 {
						t.Fatalf("guard refusal still acted: %q", actions)
					}
				} else {
					want := "kill -KILL 42\n"
					if nice {
						want = "renice -5 -p 42\n"
					}
					if string(actions) != want {
						t.Fatalf("action arguments=%q want=%q err=%v", actions, want, readErr)
					}
				}
			}
		})
	}
}

func TestIdentityProbeDoesNotInterpretProcessCommandAsShell(t *testing.T) {
	runner := procFixture(t, "100", "worker ) with * $HOME ; echo bad")
	output, err := runner.RunCommand(context.Background(), processIdentityCommand, 1<<20)
	if err != nil || string(output) != "boot "+guardBoot+"\n42 100\n" {
		t.Fatalf("probe=%q err=%v", output, err)
	}
}

func TestGuardedShellRefusesExitedProcessAndHonorsCancellation(t *testing.T) {
	runner := procFixture(t, "100", "worker")
	if err := os.Remove(filepath.Join(runner.root, "42/stat")); err != nil {
		t.Fatal(err)
	}
	process := Process{PID: 42, StartIdentity: "linux:" + guardBoot + ":100"}
	if err := SendSignalToProcess(context.Background(), runner, process, Kill); !errors.Is(err, ErrProcessUnverifiable) {
		t.Fatalf("exited process err=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	untouched := &fixedRunner{}
	if err := SendSignalToProcess(ctx, untouched, process, Kill); !errors.Is(err, context.Canceled) || untouched.asked != "" {
		t.Fatalf("canceled action dispatched: %s", fmt.Sprint(err, untouched.asked))
	}
}

func TestMalformedStatPrefixCannotMutate(t *testing.T) {
	runner := procFixture(t, "100", "worker")
	malformed := "42 (worker) not-a-state" + strings.Repeat(" not-a-number", 18) + " 100\n"
	if err := os.WriteFile(filepath.Join(runner.root, "42/stat"), []byte(malformed), 0600); err != nil {
		t.Fatal(err)
	}
	process := Process{PID: 42, StartIdentity: "linux:" + guardBoot + ":100"}
	err := SendSignalToProcess(context.Background(), runner, process, Kill)
	actions, _ := os.ReadFile(runner.log)
	if !errors.Is(err, ErrProcessUnverifiable) || len(actions) != 0 {
		t.Fatalf("malformed stat accepted: err=%v actions=%q", err, actions)
	}
}

func TestGuardedStatPrefixValidatesStateAndNumericFieldTypes(t *testing.T) {
	for _, test := range []struct {
		name  string
		field int
		value string
		valid bool
	}{
		{"signed-fields", 0, "", true},
		{"traced-state", 3, "t", true},
		{"historical-state", 3, "W", true},
		{"idle-state", 3, "I", true},
		{"unknown-state", 3, "?", false},
		{"multi-letter-state", 3, "running", false},
		{"malformed-ppid", 4, "1x", false},
		{"signed-tty", 7, "-1234", true},
		{"unsigned-flags", 9, "-1", false},
		{"malformed-fault-count", 12, "1.5", false},
		{"unsigned-cpu-time", 14, "-1", false},
		{"malformed-child-time", 16, "2x", false},
		{"sign-only-priority", 18, "-", false},
		{"malformed-niceness", 19, "1;echo", false},
		{"unsigned-start", 22, "-100", false},
		{"overlong-field", 10, strings.Repeat("9", 21), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, nice := range []bool{false, true} {
				runner := procFixture(t, "100", "worker")
				// Fields 3..24: signed tpgid, child times, priority and nice are legal.
				fields := strings.Fields("S 1 2 3 0 -1 4194304 0 0 0 0 3 4 -5 -6 -7 -8 1 0 100 0 0")
				if test.field != 0 {
					fields[test.field-3] = test.value
				}
				stat := "42 (worker ) with (parentheses)) " + strings.Join(fields, " ") + "\n"
				if err := os.WriteFile(filepath.Join(runner.root, "42/stat"), []byte(stat), 0600); err != nil {
					t.Fatal(err)
				}
				process := Process{PID: 42, StartIdentity: "linux:" + guardBoot + ":100"}
				var err error
				if nice {
					err = ReniceProcess(context.Background(), runner, process, -5)
				} else {
					err = SendSignalToProcess(context.Background(), runner, process, Kill)
				}
				actions, _ := os.ReadFile(runner.log)
				if test.valid {
					if err != nil || len(actions) == 0 {
						t.Fatalf("legal signed/state fields rejected: nice=%v err=%v actions=%q", nice, err, actions)
					}
				} else if !errors.Is(err, ErrProcessUnverifiable) || len(actions) != 0 {
					t.Fatalf("malformed field %d accepted: nice=%v err=%v actions=%q", test.field, nice, err, actions)
				}
				output, probeErr := runner.RunCommand(context.Background(), processIdentityCommand, 1<<20)
				if probeErr != nil || strings.Contains(string(output), "\n42 100\n") != test.valid {
					t.Fatalf("snapshot probe disagrees with guarded action: output=%q err=%v", output, probeErr)
				}
			}
		})
	}
}
