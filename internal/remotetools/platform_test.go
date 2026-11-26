package remotetools

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
)

func decodePowerShell(t *testing.T, command string) string {
	t.Helper()
	parts := strings.Fields(command)
	if len(parts) != 6 {
		t.Fatalf("unexpected invocation: %d arguments", len(parts))
	}
	b, err := base64.StdEncoding.DecodeString(parts[5])
	if err != nil || len(b)%2 != 0 {
		t.Fatal("invalid UTF-16LE command")
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	return string(utf16.Decode(u))
}

func TestWindowsProcessSnapshotUsesEncodedCommandAndStaysReadOnly(t *testing.T) {
	r := &captureRunner{output: []byte(`[{"pid":42,"parent":4,"user":"域\\操作员","state":3,"cpu":12.5,"memory":2,"rss":67108864,"virtual":134217728,"elapsed":90061,"command":"C:\\程序\\服务.exe --名字 中文"}]`)}
	snapshot, err := ReadSafeProcessSnapshot(context.Background(), ForSystem(r, "windows"), SortCPU)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Processes) != 1 || snapshot.Processes[0].Elapsed != "1-01:01:01" ||
		snapshot.Processes[0].ParentPID != 4 || snapshot.Processes[0].State != "Running" ||
		snapshot.Processes[0].ResidentBytes != 64<<20 || snapshot.Processes[0].VirtualBytes != 128<<20 ||
		snapshot.Processes[0].StartIdentity != "" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	// The bounded snapshot must fit cmd.exe's command-line budget.
	if len(encodePowerShell(windowsProcessScript)) >= 7800 {
		t.Fatal("script exceeds SSH cmd.exe budget")
	}
}

func TestWindowsProcessStateLabelsMatchCIMExecutionStates(t *testing.T) {
	for code, want := range map[int]string{
		0: "Unknown", 2: "Ready", 3: "Running", 4: "Blocked",
		5: "Suspended", 6: "Terminated", 7: "Stopped", 8: "Growing",
		9: "Not Responding",
	} {
		if got := windowsProcessState(code); got != want {
			t.Errorf("ExecutionState %d = %q, want %q", code, got, want)
		}
	}
}

func TestUnixSnapshotReadsProcessDetailsForTheListAndDialog(t *testing.T) {
	r := &captureRunner{output: []byte("42 7 alice S< 8.5 2.25 2048 8192 01:02:03 worker --job daily\n")}
	snapshot, err := ReadProcessSnapshot(context.Background(), ForSystem(r, "linux"), SortCPU)
	if err != nil || len(snapshot.Processes) != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	got := snapshot.Processes[0]
	if got.PID != 42 || got.ParentPID != 7 || got.User != "alice" || got.State != "S<" ||
		got.CPU != 8.5 || got.Memory != 2.25 || got.ResidentBytes != 2<<20 ||
		got.VirtualBytes != 8<<20 || got.Elapsed != "01:02:03" || got.Command != "worker --job daily" {
		t.Fatalf("process=%+v", got)
	}
	if !strings.Contains(r.command, "-o ppid=") || !strings.Contains(r.command, "-o rss=") ||
		!strings.Contains(r.command, "-o vsz=") {
		t.Fatalf("snapshot command lacks detail columns: %s", r.command)
	}
}

func TestWindowsSnapshotRejectsMalformedAndLabelsLimits(t *testing.T) {
	for _, s := range []string{`null`, `{}`, `[{"pid":0,"cpu":1}]`, `[{"pid":2,"cpu":null}]`, `[{"pid":2,"cpu":-1}]`, `[{"pid":2,"cpu":0,"memory":101}]`, `[{"pid":0,"cpu":0},{"pid":-1,"cpu":0}]`} {
		if _, err := parseWindowsProcesses([]byte(s), SortCPU); !errors.Is(err, ErrMalformed) {
			t.Fatalf("accepted %s: %v", s, err)
		}
	}
	// A row that went bad between the CIM query and the perf counters must not
	// discard the machine's readable processes.
	rows, err := parseWindowsProcesses([]byte(`[{"pid":2,"cpu":1,"memory":1,"command":"a","truncated":true},{"pid":3,"cpu":null},{"pid":4,"cpu":0,"memory":2,"command":"b"},{"pid":4,"cpu":0,"memory":2,"command":"dup"}]`), SortMemory)
	if err != nil || !rows.CommandsTruncated || len(rows.Processes) != 2 || rows.Processes[0].PID != 4 || rows.Processes[1].PID != 2 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

type captureRunner struct {
	command string
	output  []byte
	err     error
}

func (r *captureRunner) RunCommand(_ context.Context, c string, _ int) ([]byte, error) {
	r.command = c
	return r.output, r.err
}

func TestDockerCommandSurvivesWindowsDefaultShellAndMissingCLI(t *testing.T) {
	r := &captureRunner{output: []byte("motty-docker-missing\n"), err: errors.New("exit 127")}
	_, err := Containers(context.Background(), ForSystem(r, "windows"))
	if !errors.Is(err, ErrNoContainerRuntime) {
		t.Fatal(err)
	}
	script := decodePowerShell(t, r.command)
	if !strings.Contains(script, "'{{.ID}}|{{.Names}}|") || !strings.Contains(script, "exit $LASTEXITCODE") || !strings.Contains(script, "Docker\\Docker\\resources") {
		t.Fatal("Docker arguments or exit status lost")
	}
}

func TestDarwinSnapshotSkipsProcfsAndDockerFindsDesktop(t *testing.T) {
	r := &captureRunner{output: []byte("42 alice 2.0 1.0 01:20 /Applications/应用.app/Contents/MacOS/server\n")}
	rows, err := ReadSafeProcessSnapshot(context.Background(), ForSystem(r, "darwin"), SortCPU)
	if err != nil || len(rows.Processes) != 1 || rows.Processes[0].StartIdentity != "" {
		t.Fatalf("%+v %v", rows, err)
	}
	if strings.Contains(r.command, "/proc") || !strings.Contains(r.command, "/bin/ps -A") {
		t.Fatal(r.command)
	}
	r.output = nil
	_, _ = Containers(context.Background(), ForSystem(r, "darwin"))
	if !strings.Contains(r.command, "/Applications/Docker.app/Contents/Resources/bin/docker") {
		t.Fatal("missing Desktop fallback")
	}
}

func TestDarwinMissingDockerMarksTheCommandOutput(t *testing.T) {
	r := &captureRunner{output: []byte("motty-docker-missing\n"), err: errors.New("exit 127")}
	if _, err := Containers(context.Background(), ForSystem(r, "darwin")); !errors.Is(err, ErrNoContainerRuntime) {
		t.Fatalf("missing macOS Docker = %v", err)
	}
	// A command runner returns standard output only, so the marker must not be
	// redirected to stderr: a missing Docker would then surface as a bare exit
	// status instead of the explicit "not installed" state.
	if strings.Contains(r.command, ">&2") || !strings.Contains(r.command, "printf 'motty-docker-missing") {
		t.Fatalf("missing-CLI marker is not on stdout: %s", r.command)
	}
}

// fakeEngineDir holds a script named after one engine that prints its own name
// and arguments, so the chain a server would run is executed, not just read.
func fakeEngineDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		script := "#!/bin/sh\necho \"" + name + " $*\"\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type localShellRunner struct{ path string }

func (r localShellRunner) RunCommand(ctx context.Context, command string, _ int) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Env = []string{"PATH=" + r.path}
	return cmd.Output()
}

func skipWithoutUnixEngines(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX engine chain runs on the remote Unix shell")
	}
	for _, path := range []string{"/usr/local/bin/docker", "/usr/local/bin/podman"} {
		if _, err := os.Stat(path); err == nil {
			t.Skip(path + " exists on this machine and would answer instead of the fixture")
		}
	}
}

func TestContainerCommandFallsBackToPodmanOnAUnixServer(t *testing.T) {
	skipWithoutUnixEngines(t)
	runner := ForSystem(localShellRunner{path: fakeEngineDir(t, "podman")}, "linux")
	output, err := runner.RunCommand(context.Background(), "docker ps -a", 1024)
	if err != nil || strings.TrimSpace(string(output)) != "podman ps -a" {
		t.Fatalf("output=%q err=%v", output, err)
	}
	if got, err := DetectRuntime(context.Background(), runner); err != nil || got != RuntimePodman {
		t.Fatalf("runtime=%v err=%v", got, err)
	}
}

// A host reports its distribution ("fedora", "ubuntu"), not only "linux", and
// Podman is the engine on many of them. Every Unix system must reach it.
func TestContainerCommandFallsBackToPodmanOnEveryUnixDistribution(t *testing.T) {
	skipWithoutUnixEngines(t)
	for _, system := range []string{"linux", "ubuntu", "debian", "fedora", "rhel", "alpine", "freebsd", ""} {
		t.Run(system, func(t *testing.T) {
			runner := ForSystem(localShellRunner{path: fakeEngineDir(t, "podman")}, system)
			output, err := runner.RunCommand(context.Background(), "docker ps -a", 1024)
			if err != nil || strings.TrimSpace(string(output)) != "podman ps -a" {
				t.Fatalf("output=%q err=%v", output, err)
			}
			if got, err := DetectRuntime(context.Background(), runner); err != nil || got != RuntimePodman {
				t.Fatalf("runtime=%v err=%v", got, err)
			}
		})
	}
}

func TestContainerCommandPrefersDockerWhenBothExist(t *testing.T) {
	skipWithoutUnixEngines(t)
	runner := ForSystem(localShellRunner{path: fakeEngineDir(t, "docker", "podman")}, "linux")
	output, err := runner.RunCommand(context.Background(), "docker ps -a", 1024)
	if err != nil || strings.TrimSpace(string(output)) != "docker ps -a" {
		t.Fatalf("output=%q err=%v", output, err)
	}
	if got, err := DetectRuntime(context.Background(), runner); err != nil || got != RuntimeDocker {
		t.Fatalf("runtime=%v err=%v", got, err)
	}
}

func TestNeitherEngineIsReportedAsMissingRuntime(t *testing.T) {
	skipWithoutUnixEngines(t)
	runner := ForSystem(localShellRunner{path: t.TempDir()}, "linux")
	if _, err := Containers(context.Background(), runner); !errors.Is(err, ErrNoContainerRuntime) {
		t.Fatalf("Containers = %v", err)
	}
	if _, err := DetectRuntime(context.Background(), runner); !errors.Is(err, ErrNoContainerRuntime) {
		t.Fatalf("DetectRuntime = %v", err)
	}
}

func TestWindowsProbeAndCommandShareTheEngineSearch(t *testing.T) {
	r := &captureRunner{output: []byte("podman\n")}
	if got, err := DetectRuntime(context.Background(), ForSystem(r, "windows")); err != nil || got != RuntimePodman {
		t.Fatalf("runtime=%v err=%v", got, err)
	}
	script := decodePowerShell(t, r.command)
	if !strings.Contains(script, "podman") || !strings.Contains(script, "WriteLine($engine)") {
		t.Fatal("the probe does not search for Podman")
	}
}

func TestTerminalCommandsUseTheDetectedEngine(t *testing.T) {
	if command, ok := LogsCommand(RuntimePodman, "web-1"); !ok || command != "podman logs -f --tail 200 web-1" {
		t.Fatalf("logs: %q", command)
	}
	if command, ok := PullCommand(RuntimePodman, "nginx:1"); !ok || command != "podman pull nginx:1" {
		t.Fatalf("pull: %q", command)
	}
	full := "0f1e2d3c4b5a" + strings.Repeat("0", 52)
	command, err := ShellCommand(context.Background(), &shellProbeRunner{}, RuntimePodman, full)
	if err != nil || command != "podman exec -it "+full+" sh" {
		t.Fatalf("shell: %q %v", command, err)
	}
}
