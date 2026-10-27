package remotetools

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

type shellRunner struct{ commands []string }

func (runner *shellRunner) RunCommand(ctx context.Context, command string, limit int) ([]byte, error) {
	runner.commands = append(runner.commands, command)
	output, err := exec.CommandContext(ctx, "/bin/sh", "-c", command).Output()
	return output, err
}

type fixedRunner struct {
	output string
	err    error
	asked  string
}

func (runner *fixedRunner) RunCommand(_ context.Context, command string, _ int) ([]byte, error) {
	runner.asked = command
	return []byte(runner.output), runner.err
}

type scriptedResult struct {
	output string
	err    error
}

type scriptedRunner struct {
	commands []string
	results  []scriptedResult
}

func (runner *scriptedRunner) RunCommand(_ context.Context, command string, _ int) ([]byte, error) {
	runner.commands = append(runner.commands, command)
	if len(runner.commands) > len(runner.results) {
		return nil, fmt.Errorf("unexpected command %q", command)
	}
	result := runner.results[len(runner.commands)-1]
	return []byte(result.output), result.err
}

func TestParsingTheProcessListSortsAndSkipsItsOwnPipeline(t *testing.T) {
	output := "" +
		"    1 root      0.1  0.2    10-02:03:04 /sbin/init splash\n" +
		" 2211 www-data  42.5 1.3       01:02:03 nginx: worker process   (with   spaces)\n" +
		" 3000 alice      7.0 30.9          05:01 /usr/bin/python3 /srv/app.py --flag\n" +
		" 9001 alice      0.0  0.0          00:00 sh -c LC_ALL=C ps -eo pid=,user=,pcpu=,pmem=,etime=,args= 2>&1 | sort\n" +
		" 9003 alice      0.0  0.0          00:00 sort -k3,3nr\n" +
		" 9004 alice      0.0  0.0          00:00 head -n 68\n"
	byCPU, err := ParseProcesses([]byte(output), SortCPU)
	if err != nil || len(byCPU) != 3 {
		t.Fatalf("%d processes, %v", len(byCPU), err)
	}
	if byCPU[0].PID != 2211 || byCPU[0].CPU != 42.5 || byCPU[0].User != "www-data" || byCPU[0].Elapsed != "01:02:03" ||
		byCPU[0].Command != "nginx: worker process   (with   spaces)" {
		t.Fatalf("first by CPU: %+v", byCPU[0])
	}
	byMemory, _ := ParseProcesses([]byte(output), SortMemory)
	if byMemory[0].PID != 3000 {
		t.Fatalf("first by memory: %+v", byMemory[0])
	}
}

func TestAListingThatIsAnErrorMessageIsReportedAsUnsupported(t *testing.T) {
	_, err := ParseProcesses([]byte("ps: invalid option -- 'e'\nBusyBox v1.35\n"), SortCPU)
	var failure *Failure
	if !errors.Is(err, ErrUnsupported) || !errors.As(err, &failure) || !strings.Contains(failure.Message, "invalid option") {
		t.Fatalf("got %v", err)
	}
	if _, err := ParseProcesses(nil, SortCPU); !errors.Is(err, ErrMalformed) {
		t.Fatalf("empty output: %v", err)
	}
}

func TestTheProcessListRunsOnThisComputer(t *testing.T) {
	if _, err := exec.LookPath("ps"); err != nil {
		t.Skip("no ps")
	}
	runner := &shellRunner{}
	list, err := Processes(context.Background(), runner, SortMemory)
	if err != nil || len(list) == 0 {
		t.Fatalf("%d processes, %v", len(list), err)
	}
	if len(list) > maxProcesses {
		t.Fatalf("%d processes returned", len(list))
	}
	for _, process := range list {
		if ownPipeline(process.Command) {
			t.Fatalf("the listing's own process is in it: %+v", process)
		}
	}
}

func TestLegacyProcessFallbackKeepsCPUAndMemoryWithoutInventingElapsedTime(t *testing.T) {
	runner := &scriptedRunner{results: []scriptedResult{
		{output: "ps: invalid option -- e\n"},
		{output: "USER PID %CPU %MEM VSZ RSS TTY STAT START TIME COMMAND\n" +
			"root 2 1.5 2.3 1832 256 ? S 08:00 0:01 /sbin/init\n" +
			"www 9 22.7 5.4 9182 128 ? S 08:15 0:02 nginx: worker\n"},
	}}
	got, err := Processes(context.Background(), runner, SortCPU)
	if err != nil || len(got) != 2 || len(runner.commands) != 2 || got[0].PID != 9 ||
		got[0].User != "www" || got[0].CPU != 22.7 || got[0].Memory != 5.4 || got[0].Command != "nginx: worker" || got[0].Elapsed != "" {
		t.Fatalf("processes=%+v commands=%q err=%v", got, runner.commands, err)
	}
	runner = &scriptedRunner{results: []scriptedResult{{output: "ps: invalid option -- e\n"}, {output: "ps: invalid option -- a\n"}}}
	if got, err := Processes(context.Background(), runner, SortCPU); len(got) != 0 || !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported ps returned rows=%+v err=%v", got, err)
	}
	runner = &scriptedRunner{results: []scriptedResult{
		{output: "ps: invalid option -- e\n"},
		{output: "root 2 1.5 2.3 1832 256 ? S 08:00 0:01 /sbin/init\nps: aborted\n", err: errors.New("exit status 1")},
	}}
	if got, err := Processes(context.Background(), runner, SortCPU); len(got) != 0 || err == nil {
		t.Fatalf("partial failed ps output became successful rows=%+v err=%v", got, err)
	}
}

func TestLegacyProcessDoesNotRetryPermissionErrors(t *testing.T) {
	runner := &scriptedRunner{results: []scriptedResult{{output: "permission denied\n", err: errors.New("exit status 1")}}}
	if _, err := Processes(context.Background(), runner, SortCPU); err == nil || len(runner.commands) != 1 {
		t.Fatalf("permission error=%v commands=%q", err, runner.commands)
	}
	runner = &scriptedRunner{results: []scriptedResult{{output: " 2211 www 42.5 1.3 01:02:03 nginx: worker\n", err: errors.New("exit status 1")}}}
	if got, err := Processes(context.Background(), runner, SortCPU); len(got) != 0 || err == nil {
		t.Fatalf("failed modern ps returned partial success=%+v err=%v", got, err)
	}
}

func TestSignalsRefuseWhatCouldReachMoreThanOneProcess(t *testing.T) {
	runner := &fixedRunner{}
	for _, pid := range []int{-1, 0, 1} {
		if err := SendSignal(context.Background(), runner, pid, Kill); err == nil {
			t.Fatalf("pid %d was accepted", pid)
		}
	}
	if runner.asked != "" {
		t.Fatalf("a command was sent: %q", runner.asked)
	}
	if err := SendSignal(context.Background(), runner, 4242, Terminate); err != nil || runner.asked != "kill -TERM 4242 2>&1" {
		t.Fatalf("%q, %v", runner.asked, err)
	}
	runner.err, runner.output = errors.New("exit status 1"), "sh: line 1: kill: (4242) - Operation not permitted\nmore\n"
	err := SendSignal(context.Background(), runner, 4242, Kill)
	var failure *Failure
	if !errors.As(err, &failure) || !strings.Contains(failure.Message, "Operation not permitted") || strings.Contains(failure.Message, "more") {
		t.Fatalf("got %v", err)
	}
}

func TestParsingTheContainerListPutsRunningOnesFirst(t *testing.T) {
	output := "" +
		"a1b2c3d4e5f6|old-job|alpine:3.19|exited|Exited (0) 3 days ago\n" +
		"0f1e2d3c4b5a|web|nginx:latest|running|Up 2 hours (healthy)\n"
	list, err := ParseContainers([]byte(output))
	if err != nil || len(list) != 2 || list[0].Name != "web" || list[0].Status != "Up 2 hours (healthy)" || !list[0].Running() || list[1].Running() {
		t.Fatalf("%+v, %v", list, err)
	}
	if _, err := ParseContainers([]byte("Cannot connect to the Docker daemon\n")); !errors.Is(err, ErrMalformed) {
		t.Fatalf("a message was read as a container: %v", err)
	}
}

func TestContainersExplainAMissingOrRefusedDocker(t *testing.T) {
	missing := &fixedRunner{output: "sh: 1: docker: not found\n", err: errors.New("exit status 127")}
	if _, err := Containers(context.Background(), missing); !errors.Is(err, ErrNoDocker) {
		t.Fatalf("missing docker: %v", err)
	}
	refused := &fixedRunner{output: "permission denied while trying to connect to the Docker daemon socket\nsecond line\n", err: errors.New("exit status 1")}
	_, err := Containers(context.Background(), refused)
	var failure *Failure
	if !errors.As(err, &failure) || !strings.HasPrefix(failure.Message, "permission denied") || strings.Contains(failure.Message, "second") {
		t.Fatalf("refused: %v", err)
	}
}

type versionedDockerRunner struct {
	commands  []string
	modern    string
	modernErr error
	legacy    string
	legacyErr error
}

func (runner *versionedDockerRunner) RunCommand(_ context.Context, command string, _ int) ([]byte, error) {
	runner.commands = append(runner.commands, command)
	if strings.Contains(command, "{{.State}}") {
		return []byte(runner.modern), runner.modernErr
	}
	return []byte(runner.legacy), runner.legacyErr
}

func TestContainersLegacyStateTemplateFallsBackWithoutLosingRows(t *testing.T) {
	runner := &versionedDockerRunner{
		modern:    "Template parsing error: template: :1:32: executing \"\" at <.State>: can't evaluate field State in type *formatter.containerContext\n",
		modernErr: errors.New("exit status 1"),
		legacy: "a1b2c3d4e5f6|job|alpine:3.19|Exited (0) 3 days ago\n" +
			"0f1e2d3c4b5a|web|registry.example.com/nginx:1.27|Up 2 hours (healthy)\n",
	}
	got, err := Containers(context.Background(), runner)
	if err != nil || len(got) != 2 || len(runner.commands) != 2 ||
		got[0].Name != "web" || got[0].Image != "registry.example.com/nginx:1.27" || got[0].State != "running" ||
		got[1].ID != "a1b2c3d4e5f6" || got[1].State != "exited" || strings.Contains(runner.commands[1], "{{.State}}") {
		t.Fatalf("rows=%+v commands=%q err=%v", got, runner.commands, err)
	}
}

func TestContainersLegacyStatesAndMalformedFallback(t *testing.T) {
	fixture := "000000000001|up|image:1|Up 5 minutes (healthy)\n" +
		"000000000002|paused|image:2|Up 5 minutes (Paused)\n" +
		"000000000003|created|image:3|Created\n" +
		"000000000004|restart|image:4|Restarting (1) 10 seconds ago\n" +
		"000000000005|dead|image:5|Dead\n" +
		"000000000006|other|image:6|Something New\n"
	runner := &versionedDockerRunner{modern: "template error at <.State>: can't evaluate field State in type *formatter.containerContext", modernErr: errors.New("exit status 1"), legacy: fixture}
	got, err := Containers(context.Background(), runner)
	if err != nil || len(got) != 6 {
		t.Fatalf("rows=%+v err=%v", got, err)
	}
	states := map[string]string{"up": "running", "paused": "paused", "created": "created", "restart": "restarting", "dead": "dead", "other": "unknown"}
	for _, row := range got {
		if row.State != states[row.Name] {
			t.Errorf("%s: state=%s want=%s", row.Name, row.State, states[row.Name])
		}
	}
	runner.legacy = "this is not a container row\n"
	if _, err := Containers(context.Background(), runner); !errors.Is(err, ErrMalformed) {
		t.Fatalf("malformed fallback became empty list: %v", err)
	}
}

func TestContainersVersionFallbackIsNotUsedForModernOrDaemonFailures(t *testing.T) {
	modern := &versionedDockerRunner{modern: "0f1e2d3c4b5a|web|nginx:1|running|Up 1 hour\n"}
	if got, err := Containers(context.Background(), modern); err != nil || len(got) != 1 || len(modern.commands) != 1 {
		t.Fatalf("modern rows=%+v commands=%q err=%v", got, modern.commands, err)
	}
	modern.modern = ""
	if got, err := Containers(context.Background(), modern); err != nil || len(got) != 0 || len(modern.commands) != 2 {
		t.Fatalf("empty rows=%+v commands=%q err=%v", got, modern.commands, err)
	}
	daemon := &versionedDockerRunner{modern: "Cannot connect to the Docker daemon", modernErr: errors.New("exit status 1")}
	if _, err := Containers(context.Background(), daemon); err == nil || len(daemon.commands) != 1 {
		t.Fatalf("daemon error=%v commands=%q", err, daemon.commands)
	}
	failed := &versionedDockerRunner{modern: "error at <.State>: can't evaluate field State", modernErr: errors.New("exit status 1"), legacy: "daemon unavailable", legacyErr: errors.New("exit status 1")}
	if _, err := Containers(context.Background(), failed); err == nil || len(failed.commands) != 2 {
		t.Fatalf("failed fallback error=%v commands=%q", err, failed.commands)
	}
}

type shellProbeRunner struct {
	commands     []string
	missing      map[string]bool
	refused      bool
	runtimeError string
}

func (runner *shellProbeRunner) RunCommand(_ context.Context, command string, _ int) ([]byte, error) {
	runner.commands = append(runner.commands, command)
	if strings.HasPrefix(command, "docker inspect --format") {
		return []byte("0f1e2d3c4b5a" + strings.Repeat("0", 52) + "\n"), nil
	}
	if runner.refused {
		return []byte("permission denied while connecting to the Docker daemon"), errors.New("exit status 1")
	}
	if runner.runtimeError != "" {
		return []byte(runner.runtimeError), errors.New("exit status 1")
	}
	for shell := range runner.missing {
		if strings.Contains(command, " "+shell+" -c ") && runner.missing[shell] {
			return []byte(fmt.Sprintf("OCI runtime exec failed: exec: %q: executable file not found", shell)), errors.New("exit status 126")
		}
	}
	return nil, nil
}

func TestContainerShellUsesAPresentShWithoutLoginFlags(t *testing.T) {
	runner := &shellProbeRunner{}
	command, err := ShellCommand(context.Background(), runner, "0f1e2d3c4b5a")
	full := "0f1e2d3c4b5a" + strings.Repeat("0", 52)
	if err != nil || command != "docker exec -it "+full+" sh" || len(runner.commands) != 2 ||
		runner.commands[0] != "docker inspect --format '{{.Id}}' 0f1e2d3c4b5a 2>&1" ||
		runner.commands[1] != "docker exec "+full+" sh -c : 2>&1" {
		t.Fatalf("command=%q probe=%q err=%v", command, runner.commands, err)
	}
}

func TestContainerShellFallsBackOnlyBeforeTheInteractiveSessionStarts(t *testing.T) {
	runner := &shellProbeRunner{missing: map[string]bool{"sh": true}}
	command, err := ShellCommand(context.Background(), runner, "0f1e2d3c4b5a")
	full := "0f1e2d3c4b5a" + strings.Repeat("0", 52)
	if err != nil || command != "docker exec -it "+full+" bash" || len(runner.commands) != 3 {
		t.Fatalf("command=%q probes=%q err=%v", command, runner.commands, err)
	}
	runner.missing = map[string]bool{"sh": true, "bash": true, "ash": true}
	if command, err := ShellCommand(context.Background(), runner, "0f1e2d3c4b5a"); command != "" || !errors.Is(err, ErrNoShell) {
		t.Fatalf("shell-less container returned %q, %v", command, err)
	}
}

func TestContainerShellRefusesBadIDsAndDaemonErrors(t *testing.T) {
	runner := &shellProbeRunner{}
	for _, id := range []string{"web", "a;exit", "-f", "ABCDEF123456"} {
		if command, err := ShellCommand(context.Background(), runner, id); command != "" || err == nil {
			t.Fatalf("invalid ID %q: command=%q err=%v", id, command, err)
		}
	}
	if len(runner.commands) != 0 {
		t.Fatalf("invalid IDs reached the host: %q", runner.commands)
	}
	runner.refused = true
	if command, err := ShellCommand(context.Background(), runner, "0f1e2d3c4b5a"); command != "" || err == nil || errors.Is(err, ErrNoShell) || len(runner.commands) != 2 {
		t.Fatalf("daemon error became no-shell: command=%q err=%v probes=%q", command, err, runner.commands)
	}
}

func TestContainerShellDoesNotMisreportADeletedWorkingDirectoryAsNoShell(t *testing.T) {
	runner := &shellProbeRunner{runtimeError: "OCI runtime exec failed: chdir to cwd /app: no such file or directory"}
	command, err := ShellCommand(context.Background(), runner, "0f1e2d3c4b5a")
	if command != "" || err == nil || errors.Is(err, ErrNoShell) || len(runner.commands) != 2 || !strings.Contains(err.Error(), "chdir to cwd") {
		t.Fatalf("runtime failure hidden as shell absence: command=%q err=%v probes=%q", command, err, runner.commands)
	}
}

func TestContainerActionsOnlyTakeValidNames(t *testing.T) {
	runner := &fixedRunner{}
	for _, id := range []string{"", "web; rm -rf /", "$(reboot)", "a b", "-rf", strings.Repeat("a", 200), "web\nx"} {
		if err := Do(context.Background(), runner, Stop, id); err == nil {
			t.Fatalf("%q was accepted", id)
		}
		if _, ok := LogsCommand(id); ok {
			t.Fatalf("logs for %q", id)
		}
	}
	if runner.asked != "" {
		t.Fatalf("a command was sent: %q", runner.asked)
	}
	for action, want := range map[Action]string{Start: "docker start web-1 2>&1", Stop: "docker stop web-1 2>&1", Restart: "docker restart web-1 2>&1", Remove: "docker rm web-1 2>&1"} {
		if err := Do(context.Background(), runner, action, "web-1"); err != nil || runner.asked != want {
			t.Fatalf("%q, %v", runner.asked, err)
		}
	}
	if command, ok := LogsCommand("web-1"); !ok || command != "docker logs -f --tail 200 web-1" {
		t.Fatalf("logs: %q", command)
	}
}

func TestDockerObjectListingsBecomeRowsKeyedByWhatRemovesThem(t *testing.T) {
	images, err := ParseResources(Images, []byte("d4c45f66efc5|debian:bookworm-slim|74.9MB|2 weeks ago\ncf001c2f8af7|docker.m.example.com/library/python:<none>|1GB|3 months ago\n"), 4)
	if err != nil || len(images) != 2 {
		t.Fatalf("%d images, %v", len(images), err)
	}
	if images[0].Key != "debian:bookworm-slim" || images[0].Cells[0] != "debian:bookworm-slim" || images[0].Cells[1] != "d4c45f66efc5" {
		t.Errorf("a tagged image: %+v", images[0])
	}
	if images[1].Key != "cf001c2f8af7" {
		t.Errorf("an untagged image must be removed by its ID, got %q", images[1].Key)
	}
	volumes, err := ParseResources(Volumes, []byte("pgdata|local|local\n"), 3)
	if err != nil || volumes[0].Key != "pgdata" {
		t.Errorf("volume: %+v, %v", volumes, err)
	}
	networks, err := ParseResources(Networks, []byte("4cb549b7e5e0|app_default|bridge|local\n"), 4)
	if err != nil || networks[0].Key != "4cb549b7e5e0" || networks[0].Cells[0] != "app_default" {
		t.Errorf("network: %+v, %v", networks, err)
	}
	if _, err := ParseResources(Volumes, []byte("only|two\n"), 3); !errors.Is(err, ErrMalformed) {
		t.Errorf("a short line: %v", err)
	}
	if _, err := ParseResources(Volumes, []byte("a b; rm -rf /|local|local\n"), 3); !errors.Is(err, ErrMalformed) {
		t.Errorf("a name that could become shell syntax was accepted: %v", err)
	}
}

func TestLegacyResourceOptionalFieldsFallBackWithoutChangingKeys(t *testing.T) {
	tests := []struct {
		kind    Kind
		missing string
		legacy  string
		key     string
		name    string
	}{
		{Images, ".CreatedSince", "d4c45f66efc5|debian:bookworm-slim|74.9MB\n", "debian:bookworm-slim", "debian:bookworm-slim"},
		{Volumes, ".Scope", "pgdata|local\r\n", "pgdata", "pgdata"},
		{Networks, ".Scope", "4cb549b7e5e0|app_default|bridge\n", "4cb549b7e5e0", "app_default"},
	}
	for _, test := range tests {
		t.Run(test.missing+test.name, func(t *testing.T) {
			runner := &scriptedRunner{results: []scriptedResult{
				{output: "Template parsing error: executing at <" + test.missing + ">: can't evaluate field " + strings.TrimPrefix(test.missing, ".") + " in type *formatter.context\n", err: errors.New("exit status 1")},
				{output: test.legacy},
			}}
			rows, err := Resources(context.Background(), runner, test.kind)
			if err != nil || len(rows) != 1 || len(runner.commands) != 2 || rows[0].Key != test.key || rows[0].Cells[0] != test.name ||
				len(rows[0].Cells) < 3 || rows[0].Cells[len(rows[0].Cells)-1] != "" || strings.Contains(runner.commands[1], test.missing) {
				t.Fatalf("rows=%+v commands=%q err=%v", rows, runner.commands, err)
			}
			if test.kind == Volumes && rows[0].Cells[1] != "local" {
				t.Fatalf("Windows CRLF leaked into driver field: %+v", rows[0])
			}
		})
	}
}

func TestLegacyResourceRetriesOnlyTemplateFieldFailures(t *testing.T) {
	runner := &scriptedRunner{results: []scriptedResult{{output: "d4c45f66efc5|debian:bookworm-slim|74.9MB|2 weeks ago\n"}}}
	if rows, err := Resources(context.Background(), runner, Images); err != nil || len(rows) != 1 || len(runner.commands) != 1 {
		t.Fatalf("modern resources=%+v commands=%q err=%v", rows, runner.commands, err)
	}
	runner = &scriptedRunner{results: []scriptedResult{{output: "permission denied while connecting to the daemon", err: errors.New("exit status 1")}}}
	if _, err := Resources(context.Background(), runner, Images); err == nil || len(runner.commands) != 1 {
		t.Fatalf("daemon error=%v commands=%q", err, runner.commands)
	}
	runner = &scriptedRunner{results: []scriptedResult{
		{output: "at <.CreatedSince>: can't evaluate field CreatedSince", err: errors.New("exit status 1")},
		{output: "legacy format also failed", err: errors.New("exit status 1")},
	}}
	if _, err := Resources(context.Background(), runner, Images); err == nil || len(runner.commands) != 2 {
		t.Fatalf("failed fallback=%v commands=%q", err, runner.commands)
	}
	runner = &scriptedRunner{results: []scriptedResult{{output: "Error response from daemon: No such file or directory", err: errors.New("exit status 1")}}}
	if _, err := Resources(context.Background(), runner, Images); errors.Is(err, ErrNoDocker) {
		t.Fatalf("daemon error misdiagnosed as a missing Docker executable: %v", err)
	}
	runner = &scriptedRunner{results: []scriptedResult{{output: "sh: 1: docker: not found", err: errors.New("exit status 127")}}}
	if _, err := Resources(context.Background(), runner, Images); !errors.Is(err, ErrNoDocker) {
		t.Fatalf("missing Docker executable was not identified: %v", err)
	}
}

func TestRemovingAnObjectRunsOneFixedCommandAndRefusesUnsafeNames(t *testing.T) {
	runner := &fixedRunner{}
	for kind, want := range map[Kind]string{Images: "docker rmi web:1 2>&1", Volumes: "docker volume rm web:1 2>&1", Networks: "docker network rm web:1 2>&1"} {
		if err := RemoveResource(context.Background(), runner, kind, "web:1"); err != nil || runner.asked != want {
			t.Errorf("kind %d ran %q (%v), want %q", kind, runner.asked, err, want)
		}
	}
	runner.asked = ""
	if err := RemoveResource(context.Background(), runner, Images, "x; reboot"); err == nil || runner.asked != "" {
		t.Errorf("an unsafe name reached the server: %q, %v", runner.asked, err)
	}
}

func TestNamedVolumesComeBeforeTheAnonymousOnesAndSurviveTheLimit(t *testing.T) {
	var listing strings.Builder
	for index := 0; index < maxItems+50; index++ {
		listing.WriteString(strings.Repeat("a", 60) + fmt.Sprintf("%04d", index) + "|local|local\n")
	}
	listing.WriteString("pgdata|local|local\n")
	volumes, err := ParseResources(Volumes, []byte(listing.String()), 3)
	if err != nil || len(volumes) != maxItems {
		t.Fatalf("%d volumes, %v", len(volumes), err)
	}
	if volumes[0].Key != "pgdata" {
		t.Errorf("the named volume is at the end, past the limit: first is %q", volumes[0].Key)
	}
}

func TestParseComposeReadsTheJSONListing(t *testing.T) {
	projects, err := ParseCompose([]byte(`[{"Name":"web","Status":"running(2)","ConfigFiles":"/a/compose.yml,/a/o.yml"},{"Name":"api","Status":"exited(1)","ConfigFiles":"/b/compose.yml"}]`))
	if err != nil || len(projects) != 2 || projects[0].Name != "api" || projects[1].Status != "running(2)" {
		t.Fatalf("%+v, %v", projects, err)
	}
	if projects, err := ParseCompose([]byte("[]\n")); err != nil || len(projects) != 0 {
		t.Errorf("empty: %+v, %v", projects, err)
	}
	if _, err := ParseCompose([]byte("not json")); !errors.Is(err, ErrMalformed) {
		t.Errorf("garbage: %v", err)
	}
}

func TestComposeNeedsThePluginAndActionsRefuseUnsafeNames(t *testing.T) {
	runner := &fixedRunner{output: "docker: 'compose' is not a docker command.\nSee 'docker --help'", err: errors.New("exit status 1")}
	if _, err := Compose(context.Background(), runner); !errors.Is(err, ErrNoCompose) {
		t.Errorf("no plugin: %v", err)
	}
	runner = &fixedRunner{}
	if err := DoCompose(context.Background(), runner, ComposeDown, "my-app_1"); err != nil || runner.asked != "docker compose -p my-app_1 down 2>&1" {
		t.Errorf("ran %q, %v", runner.asked, err)
	}
	runner.asked = ""
	for _, name := range []string{"", "Web", "a b", "x;reboot", "-f", "$(id)"} {
		if err := DoCompose(context.Background(), runner, ComposeStart, name); err == nil || runner.asked != "" {
			t.Errorf("%q reached the server: %q", name, runner.asked)
		}
	}
}

func TestComposeCompatibilityReportsUnsupportedFormatRatherThanEmptyProjects(t *testing.T) {
	runner := &scriptedRunner{results: []scriptedResult{{output: "unknown flag: --format\n", err: errors.New("exit status 1")}}}
	if projects, err := Compose(context.Background(), runner); len(projects) != 0 || !errors.Is(err, ErrComposeListUnsupported) || len(runner.commands) != 1 {
		t.Fatalf("projects=%+v err=%v commands=%q", projects, err, runner.commands)
	}
	runner = &scriptedRunner{results: []scriptedResult{{output: "docker: 'compose' is not a docker command", err: errors.New("exit status 1")}}}
	if _, err := Compose(context.Background(), runner); !errors.Is(err, ErrNoCompose) {
		t.Fatalf("missing plugin reported as an unsupported format: %v", err)
	}
	runner = &scriptedRunner{results: []scriptedResult{{output: "Name Status Config Files\nweb running /srv/web/compose.yml\n"}}}
	if projects, err := Compose(context.Background(), runner); len(projects) != 0 || !errors.Is(err, ErrMalformed) {
		t.Fatalf("non-JSON projects=%+v err=%v", projects, err)
	}
	if projects, err := ParseCompose([]byte("null")); len(projects) != 0 || !errors.Is(err, ErrMalformed) {
		t.Fatalf("null was interpreted as an empty project list: projects=%+v err=%v", projects, err)
	}
	runner = &scriptedRunner{results: []scriptedResult{{output: "unknown command \"ls\" for \"docker compose\"", err: errors.New("exit status 1")}}}
	if _, err := Compose(context.Background(), runner); !errors.Is(err, ErrComposeListUnsupported) {
		t.Fatalf("old compose plugin without ls was misdiagnosed: %v", err)
	}
}

func TestReniceIsOneFixedCommandWithBoundedNumbers(t *testing.T) {
	runner := &fixedRunner{}
	if err := Renice(context.Background(), runner, 4321, -5); err != nil || runner.asked != "renice -5 -p 4321 2>&1" {
		t.Errorf("ran %q, %v", runner.asked, err)
	}
	runner.asked = ""
	for _, bad := range [][2]int{{1, 0}, {0, 0}, {-3, 0}, {10, 20}, {10, -21}} {
		if err := Renice(context.Background(), runner, bad[0], bad[1]); err == nil || runner.asked != "" {
			t.Errorf("%v reached the server: %q", bad, runner.asked)
		}
	}
}

func TestCreatingAVolumeOrNetworkRunsOneFixedCommandAndRefusesUnsafeNames(t *testing.T) {
	runner := &fixedRunner{}
	for kind, want := range map[Kind]string{Volumes: "docker volume create data_1 2>&1", Networks: "docker network create data_1 2>&1"} {
		if err := CreateResource(context.Background(), runner, kind, "data_1"); err != nil || runner.asked != want {
			t.Errorf("kind %d ran %q (%v), want %q", kind, runner.asked, err, want)
		}
	}
	runner.asked = ""
	for _, name := range []string{"x; reboot", "a b", "-v", "web:1", ""} {
		if err := CreateResource(context.Background(), runner, Volumes, name); err == nil || runner.asked != "" {
			t.Errorf("%q reached the server: %q, %v", name, runner.asked, err)
		}
	}
	if err := CreateResource(context.Background(), runner, Images, "nginx"); err == nil || runner.asked != "" {
		t.Errorf("an image was created: %q, %v", runner.asked, err)
	}
}

func TestPullCommandIsOnlyBuiltForAnImageReference(t *testing.T) {
	if command, ok := PullCommand("ghcr.io/org/app:1.2@sha256:abc"); !ok || command != "docker pull ghcr.io/org/app:1.2@sha256:abc" {
		t.Errorf("got %q, %v", command, ok)
	}
	for _, reference := range []string{"nginx; reboot", "a b", "--all-tags", "", "$(id)"} {
		if command, ok := PullCommand(reference); ok {
			t.Errorf("%q became %q", reference, command)
		}
	}
}
