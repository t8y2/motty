package remotestats

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSelectedCommandsOmitDisabledProbes(t *testing.T) {
	for m := Metrics(0); m <= AllMetrics; m++ {
		linux := selectedLinux(command, m)
		darwin := selectedDarwin(darwinScript(5), 5, m)
		windows := decodeWindowsCommand(t, WindowsCommand(5, m))
		if m&GPUs == 0 {
			for _, script := range []string{linux, darwin, windows} {
				for _, forbidden := range []string{"nvidia-smi", "rocm-smi", "system_profiler", "Get-GPUAdapters", "Read-GPUs", "Add-Type"} {
					if strings.Contains(script, forbidden) {
						t.Fatalf("selection %d still probes GPU with %q", m, forbidden)
					}
				}
			}
		}
		if m&CPU == 0 && (strings.Contains(linux, "done </proc/stat") || strings.Contains(darwin, "top -l") || strings.Contains(windows, "Query 'Win32_PerfFormattedData_Counters_ProcessorInformation'")) {
			t.Fatalf("selection %d still samples CPU", m)
		}
		if m&Memory == 0 && (strings.Contains(linux, "done </proc/meminfo") || strings.Contains(darwin, "avail=$(vm_stat") || strings.Contains(windows, "Query 'Win32_OperatingSystem'")) {
			t.Fatalf("selection %d still samples memory", m)
		}
		if m&NetworkIO == 0 && (strings.Contains(linux, "done </proc/net/dev") || strings.Contains(darwin, "traffic=$(netstat") || strings.Contains(windows, "Query 'Win32_PerfFormattedData_Tcpip_NetworkInterface'")) {
			t.Fatalf("selection %d still samples network", m)
		}
		for _, darwin := range []bool{false, true} {
			script := detailsCommand
			if darwin {
				script = detailsCommandDarwin
			}
			script = selectedDetails(script, darwin, m)
			if m&Disks == 0 && strings.Contains(script, "df -Pk") {
				t.Fatalf("selection %d still polls filesystems", m)
			}
			if m&NetworkIO == 0 && (strings.Contains(script, "netstat -ibn") || strings.Contains(script, "}' /proc/net/dev")) {
				t.Fatalf("selection %d still polls network details", m)
			}
		}
		if m&Disks == 0 && strings.Contains(windows, "Query 'Win32_LogicalDisk'") {
			t.Fatalf("selection %d still polls Windows disks", m)
		}
	}
}

func TestSelectedShellTemplatesParseForEveryCombination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell validation runs on Linux/macOS")
	}
	for m := Metrics(0); m <= AllMetrics; m++ {
		for _, script := range []string{selectedLinux(command, m), selectedDarwin(darwinScript(5), 5, m), selectedDetails(detailsCommand, false, m), selectedDetails(detailsCommandDarwin, true, m)} {
			cmd := exec.Command("sh", "-n")
			cmd.Stdin = strings.NewReader(script)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("selection %d: %s: %v", m, out, err)
			}
		}
	}
}

func TestSelectedWindowsTemplatesParseForEveryCombination(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell syntax validation runs on Windows")
	}
	var scripts []string
	for m := Metrics(0); m <= AllMetrics; m++ {
		scripts = append(scripts, decodeWindowsCommand(t, WindowsCommand(5, m)))
	}
	data, err := json.Marshal(scripts)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `$scripts=ConvertFrom-Json ([Console]::In.ReadToEnd());foreach($s in $scripts){$tokens=$null;$errors=$null;$null=[System.Management.Automation.Language.Parser]::ParseInput($s,[ref]$tokens,[ref]$errors);if($errors.Count){$errors|Out-String|Write-Output;exit 1}}`)
	cmd.Stdin = strings.NewReader(string(data))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("invalid selected Windows script: %s: %v", out, err)
	}
}

func TestSelectedLinuxWorkerSkipsGPUProcessAndPreservesOtherReadings(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("executes the Linux sampler")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "gpu-called")
	tool := "#!/bin/sh\nprintf called >> \"$MOTTY_TEST_GPU_MARKER\"\nprintf '0, GPU-A, Test GPU, 50, 1024, 2048, 60, 100, 200, 30\\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "nvidia-smi"), []byte(tool), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MOTTY_TEST_GPU_MARKER", marker)
	for _, m := range []Metrics{Memory, GPUs} {
		w := StartWithInterval(Local{}, nil, 1, m)
		select {
		case r := <-w.Results():
			w.Close()
			if r.Err != nil || !r.Sample.CPUUnavailable || !r.Sample.NetworkUnavailable {
				t.Fatalf("selection %d failed: %+v", m, r)
			}
			if m == Memory {
				if r.Sample.MemoryTotalBytes == 0 {
					t.Fatal("disabling GPU stopped memory collection")
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatal("hidden GPU still executed its tool")
				}
			} else if len(r.Sample.GPUs) != 1 || r.Sample.MemoryTotalBytes != 0 {
				t.Fatal("GPU-only sample leaked transport placeholders or lost GPU data")
			}
		case <-time.After(10 * time.Second):
			w.Close()
			t.Fatal("selective sampler stopped producing readings")
		}
		select {
		case <-w.Done():
		case <-time.After(time.Second):
			t.Fatal("selective sampler did not cancel")
		}
	}
}

func TestSelectedWindowsWorkerKeepsOnlyRequestedMetrics(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("executes the Windows sampler")
	}
	w := StartWindows(Local{}, nil, 1, Memory)
	defer w.Close()
	select {
	case r := <-w.Results():
		if r.Err != nil || r.Sample.MemoryTotalBytes == 0 || !r.Sample.CPUUnavailable || !r.Sample.NetworkUnavailable || len(r.Sample.GPUs) != 0 || (r.Details != nil && len(r.Details.Disks) != 0) {
			t.Fatalf("selective Windows sampler failed or leaked disabled metrics: %+v", r)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("selective Windows sampler produced no reading")
	}
}
