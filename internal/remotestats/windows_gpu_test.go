package remotestats

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func decodeWindowsCommand(t *testing.T, command string) string {
	t.Helper()
	pipe, ok := strings.CutPrefix(command, windowsCommandPrefix)
	if !ok {
		t.Fatal("missing compressed script pipe")
	}
	encoded, _, ok := strings.Cut(pipe, windowsPowerShellPipe)
	if !ok {
		t.Fatal("missing PowerShell reader")
	}
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	script, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(script)
}

func TestWindowsGPUPreservesMissingAndZeroMetrics(t *testing.T) {
	line := strings.Replace(windowsFixture, `"meta":`, `"gpus":[{"index":0,"name":"Virtual display"},{"index":1,"name":"Intel Arc","util":0,"memory":0},{"index":2,"name":"AMD Radeon","util":87,"memory":3221225472}],"meta":`, 1)
	result, err := parseWindows([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	gpus := result.Sample.GPUs
	if len(gpus) != 3 || gpus[0].UtilPercent != -1 || gpus[0].MemoryKnown || gpus[1].UtilPercent != 0 || !gpus[1].MemoryKnown || gpus[1].MemoryUsedBytes != 0 || gpus[2].UtilPercent != 87 || gpus[2].MemoryUsedBytes != 3<<30 {
		t.Fatalf("GPU metrics=%+v", gpus)
	}
	for _, gpu := range gpus {
		if gpu.PowerWatts != -1 || gpu.TemperatureC != -1 {
			t.Fatalf("invented sensor data: %+v", gpu)
		}
	}
}

// Execute the same PowerShell function with synthetic CIM and DXGI results.
// Shared names, a busy software renderer and a third physical engine must not
// transfer readings to another adapter or change the busiest-engine figure.
func TestWindowsGPUCountersFollowLUIDsAcrossAdapters(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows PowerShell")
	}
	script := windowsGPUProbe + `
function Query($name) {
    if ($name -like '*GPUAdapterMemory') {
        return @(
            @{Name='luid_0x00000000_0x000000A1_phys_0';DedicatedUsage=10;SharedUsage=20},
            @{Name='luid_0x00000000_0x000000B2_phys_0';DedicatedUsage=0;SharedUsage=0},
            @{Name='luid_0x00000000_0x000000FF_phys_0';DedicatedUsage=999;SharedUsage=999}
        )
    }
    return @(
        @{Name='pid_1_luid_0x00000000_0x000000A1_phys_0_eng_0_engtype_3D';UtilizationPercentage=40},
        @{Name='pid_2_luid_0x00000000_0x000000A1_phys_0_eng_0_engtype_3D';UtilizationPercentage=20},
        @{Name='pid_1_luid_0x00000000_0x000000A1_phys_0_eng_1_engtype_Copy';UtilizationPercentage=50},
        @{Name='pid_1_luid_0x00000000_0x000000B2_phys_0_eng_0_engtype_3D';UtilizationPercentage=0},
        @{Name='pid_1_luid_0x00000000_0x000000FF_phys_0_eng_0_engtype_3D';UtilizationPercentage=99}
    )
}
$adapters=@{
    devices=@(
        @{Name='Intel Arc';Luid='luid_0x00000000_0x000000B2';DeviceKey='PCI\VEN_8086'},
        @{Name='Intel Arc';Luid='luid_0x00000000_0x000000A1';DeviceKey='PCI\VEN_8086'},
        @{Name='Intel Arc';Luid='luid_0x00000000_0x000000C3';DeviceKey='PCI\VEN_8086'}
    )
    controllers=@(
        @{Name='Intel Arc';PNPDeviceID='PCI\VEN_8086\1'},
        @{Name='Intel Arc';PNPDeviceID='PCI\VEN_8086\2'},
        @{Name='GameViewer Virtual Display Adapter';PNPDeviceID='ROOT\DISPLAY\0001'}
    )
}
ConvertTo-Json -InputObject @(Read-GPUs $adapters) -Compress
`
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		t.Fatalf("GPU counter fixture: %v: %.1000s", err, output)
	}
	var rows []windowsGPUReading
	if err := json.Unmarshal(bytes.TrimSpace(output), &rows); err != nil {
		t.Fatalf("GPU rows: %v: %.1000s", err, output)
	}
	gpus := windowsGPUs(rows)
	if len(gpus) != 3 || gpus[0].UtilPercent != 0 || !gpus[0].MemoryKnown || gpus[1].UtilPercent != 60 || gpus[1].MemoryUsedBytes != 30 || gpus[2].UtilPercent != -1 || gpus[2].MemoryKnown {
		t.Fatalf("GPU counters crossed adapters: %+v", gpus)
	}
}

func TestWindowsCompressedCommandWorksThroughBothSSHShells(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows shells")
	}
	command := encodeWindowsScript(`[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false);[Console]::WriteLine('GPU · 显存')`)
	for _, shell := range []string{"cmd.exe", "powershell.exe", "local"} {
		t.Run(shell, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			process := localCommand(ctx, command)
			if shell == "cmd.exe" {
				process = windowsBatchCommand(t, ctx, command)
			} else if shell == "powershell.exe" {
				process = exec.CommandContext(ctx, shell, "-NoProfile", "-NonInteractive", "-Command", command)
			}
			output, err := process.CombinedOutput()
			if err != nil || !bytes.Equal(bytes.TrimSpace(output), []byte("GPU · 显存")) {
				t.Fatalf("compressed command: %v: %.1000s", err, output)
			}
		})
	}
}

func windowsBatchCommand(t *testing.T, ctx context.Context, command string) *exec.Cmd {
	t.Helper()
	// cmd.exe consumes raw shell quotes rather than the C runtime's escaped
	// argv representation. A batch file preserves the exact SSH command line.
	file := filepath.Join(t.TempDir(), "sampler.cmd")
	if err := os.WriteFile(file, []byte("@echo off\r\n"+command+"\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", file)
}

func TestWindowsLocalSamplerOwnsItsPowerShellProcess(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows PowerShell")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	process := localCommand(ctx, encodeWindowsScript(`[Console]::WriteLine($PID)`))
	output, err := process.Output()
	if err != nil || strings.TrimSpace(string(output)) != strconv.Itoa(process.Process.Pid) {
		t.Fatalf("the owned process is not the PowerShell sampler: %v: %.100s", err, output)
	}
}
