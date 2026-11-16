package remotestats

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

const windowsFixture = `gop-stats-win-v1 {"cpu":12.5,"total":8589934592,"free":4294967296,"uptime":120,"cores":[{"id":"0,0","percent":10},{"id":"0,1","percent":15}],"networkAvailable":true,"net":[{"name":"以太网","rx":1024,"tx":2048}],"meta":{"count":2,"kernel":"Windows 10.0.26100","primaryDisk":"C:\\","disks":[{"path":"C:\\","total":107374182400,"free":53687091200}]}}`

func TestLargeWindowsHostsHaveABoundedUTF8RecordBudget(t *testing.T) {
	cores, nets, disks := []string{}, []string{}, []string{}
	for i := 0; i < 256; i++ {
		cores = append(cores, fmt.Sprintf(`{"id":"1048575,%d","percent":99.123456789}`, i))
	}
	for i := 0; i < 16; i++ {
		nets = append(nets, fmt.Sprintf(`{"name":%q,"rx":123456789012345678,"tx":123456789012345678}`, strings.Repeat("网", 64)))
	}
	for i := 0; i < 32; i++ {
		disks = append(disks, fmt.Sprintf(`{"path":%q,"total":123456789012345678,"free":12345678901234567}`, fmt.Sprintf("%c:\\", 'A'+i)))
	}
	line := fmt.Sprintf(`gop-stats-win-v1 {"cpu":10,"total":8589934592,"free":4294967296,"uptime":100,"cores":[%s],"networkAvailable":true,"net":[%s],"meta":{"count":256,"kernel":"Windows","disks":[%s]}}`, strings.Join(cores, ","), strings.Join(nets, ","), strings.Join(disks, ","))
	if len(line) <= maxLineBytes || len(line) > maxWindowsLineBytes {
		t.Fatalf("fixture does not exercise the bounded extension: %d", len(line))
	}
	runner := streamFunc(func(ctx context.Context, script string, limit int, consume func([]byte) bool) error {
		if limit != maxWindowsLineBytes {
			t.Fatalf("Windows stream budget=%d", limit)
		}
		return feed(line)(ctx, script, limit, consume)
	})
	w := StartWindows(runner, nil, 2)
	defer w.Close()
	result := nextResult(t, w)
	if result.Err != nil || len(result.Sample.Cores) != 256 || len(result.Details.Networks) != 16 || len(result.Details.Disks) != 32 {
		t.Fatalf("large host reading: %v", result.Err)
	}
	if _, err := parseWindows([]byte(strings.Repeat(" ", maxWindowsLineBytes+1))); err == nil {
		t.Fatal("accepted an unbounded record")
	}
}

func TestWindowsReadingsPreserveNativeVolumesAndProcessorGroups(t *testing.T) {
	r, err := parseWindows([]byte(windowsFixture))
	if err != nil {
		t.Fatal(err)
	}
	if r.Sample.CPUPercent != 12.5 || r.Sample.MemoryUsedBytes != 4<<30 || r.Sample.ReceiveBytesPerSecond != 1024 || len(r.Sample.Cores) != 2 || r.Sample.Cores[0].ID != "0,0" || r.Details.Disks[0].Path != `C:\` || !r.Details.LoadUnavailable || r.Details.Networks[0].Name != "以太网" {
		t.Fatalf("reading=%+v details=%+v", r.Sample, r.Details)
	}
	missing := strings.Replace(windowsFixture, `"cpu":12.5`, `"cpu":null`, 1)
	missing = strings.Replace(missing, `"networkAvailable":true`, `"networkAvailable":false`, 1)
	r, err = parseWindows([]byte(missing))
	if err != nil || !r.Sample.CPUUnavailable || !r.Sample.NetworkUnavailable {
		t.Fatalf("missing metrics: %+v %v", r, err)
	}
}

func TestWindowsReadingsRejectInvalidCounters(t *testing.T) {
	for _, text := range []string{strings.Replace(windowsFixture, `"cpu":12.5`, `"cpu":101`, 1), strings.Replace(windowsFixture, `"free":4294967296`, `"free":999999999999`, 1), strings.Replace(windowsFixture, `"id":"0,1"`, `"id":"0,0"`, 1), strings.Replace(windowsFixture, `"uptime":120`, `"uptime":-1`, 1)} {
		if _, err := parseWindows([]byte(text)); err == nil {
			t.Fatal("accepted invalid Windows metrics")
		}
	}
}

func TestCoreUsageHandlesCPUHotplugAndCounterReset(t *testing.T) {
	before := []coreCounters{{"0", 20, 100}, {"2", 50, 100}}
	after := []coreCounters{{"0", 40, 200}, {"1", 20, 30}, {"2", 1, 2}}
	got := coreUsage(before, after)
	if len(got) != 1 || got[0].ID != "0" || got[0].Percent != 20 {
		t.Fatalf("cores=%+v", got)
	}
	if _, err := parseCore([]byte("gop-core 0 201 200")); err == nil {
		t.Fatal("accepted busy > total")
	}
}

func TestWindowsCommandRunsWithWindowsPowerShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows CIM")
	}
	script := strings.Replace(windowsScript, "while ($true)", "for ($once=0; $once -lt 1; $once++)", 1)
	script = strings.Replace(script, "Start-Sleep -Seconds __INTERVAL__", "", 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := encodeWindowsScript(script)
	output, err := exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", command).Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			t.Fatalf("Windows PowerShell: %v: %.500s", err, e.Stderr)
		}
		t.Fatal(err)
	}
	if strings.Contains(string(output), "gop-stats-unsupported") {
		t.Skip("this environment does not expose Windows CIM OS data")
	}
	r, err := parseWindows([]byte(strings.TrimSpace(string(output))))
	if err != nil {
		t.Fatalf("Windows metrics could not be parsed: %v (output %d bytes)", err, len(output))
	}
	if r.Sample.MemoryTotalBytes == 0 || r.Details == nil {
		t.Fatal("missing Windows system information")
	}
	t.Logf("collected Windows metrics: %d cores, %d volumes, %d interfaces", len(r.Sample.Cores), len(r.Details.Disks), len(r.Details.Networks))
}

func TestWindowsSamplerAcceptsABoundedStartupBanner(t *testing.T) {
	if len(WindowsCommand(2)) > 7800 {
		t.Fatal("encoded command is too long for a Windows cmd.exe default shell")
	}
	w := StartWindows(feed("Windows SSH service", windowsFixture), nil, 2)
	defer w.Close()
	r := nextResult(t, w)
	if r.Err != nil || r.Details == nil || r.Sample.CPUPercent != 12.5 {
		t.Fatalf("startup banner broke sampling: %+v", r)
	}
}
