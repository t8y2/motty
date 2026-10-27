package remotestats

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Captured from nvidia-smi 535 on a two-GPU workstation (the second card is
// idle and does not report a fan).
const nvidiaTwoGPUs = `0, NVIDIA GeForce RTX 4090, 87, 20481, 24564, 71, 312.45, 64
1, NVIDIA GeForce RTX 3060, 0, 12, 12288, 38, 14.02, [N/A]
`

func nvidiaLines(output string) []byte {
	var lines []byte
	for _, row := range strings.Split(strings.TrimSpace(output), "\n") {
		lines = append(lines, gpuNvidiaPrefix+row+"\n"...)
	}
	return lines
}

func parseAll(t *testing.T, lines string) []GPU {
	t.Helper()
	var gpus []GPU
	for _, line := range strings.Split(strings.TrimSpace(lines), "\n") {
		gpus = parseGPULine(gpus, []byte(line))
	}
	return gpus
}

func TestNvidiaRowsOfSeveralGPUsWithPlaceholders(t *testing.T) {
	gpus := parseAll(t, string(nvidiaLines(nvidiaTwoGPUs)))
	want := []GPU{
		{Vendor: "NVIDIA", Index: 0, Name: "NVIDIA GeForce RTX 4090", UtilPercent: 87, MemoryUsedBytes: 20481 << 20, MemoryTotalBytes: 24564 << 20, TemperatureC: 71, PowerWatts: 312.45, FanPercent: 64},
		{Vendor: "NVIDIA", Index: 1, Name: "NVIDIA GeForce RTX 3060", UtilPercent: 0, MemoryUsedBytes: 12 << 20, MemoryTotalBytes: 12288 << 20, TemperatureC: 38, PowerWatts: 14.02, FanPercent: -1},
	}
	if !reflect.DeepEqual(gpus, want) {
		t.Fatalf("gpus = %+v", gpus)
	}
}

func TestNvidiaPlaceholdersAndOddNamesAreTolerated(t *testing.T) {
	// A Jetson reports almost nothing; a name may hold a comma.
	gpus := parseAll(t, string(nvidiaLines("0, Orin (nvgpu), [N/A], [N/A], [N/A], [Not Supported], [N/A], [N/A]\n2, Acme, Inc. Tesla, 5, 100, 200, 40, 90.5, 30")))
	if len(gpus) != 2 {
		t.Fatalf("gpus = %+v", gpus)
	}
	if g := gpus[0]; g.Name != "Orin (nvgpu)" || g.UtilPercent != -1 || g.MemoryTotalBytes != 0 || g.TemperatureC != -1 || g.PowerWatts != -1 {
		t.Fatalf("unsupported readings were shown as figures: %+v", g)
	}
	if g := gpus[1]; g.Index != 2 || g.Name != "Acme, Inc. Tesla" || g.FanPercent != 30 {
		t.Fatalf("name with a comma: %+v", g)
	}
}

func TestNvidiaFailureTextProducesNoGPU(t *testing.T) {
	for _, text := range []string{
		"NVIDIA-SMI has failed because it couldn't communicate with the NVIDIA driver. Make sure that the latest NVIDIA driver is installed and running.",
		"Failed to initialize NVML: Driver/library version mismatch",
		"",
		"0, , 1, 2, 3, 4, 5, 6",
		"x, GPU, 1, 2, 3, 4, 5, 6",
	} {
		if gpus := parseGPULine(nil, []byte(gpuNvidiaPrefix+text+"\n")); len(gpus) != 0 {
			t.Errorf("%q produced %+v", text, gpus)
		}
	}
}

// ROCm 5.x: a "system" entry beside the cards and a memory percentage field.
const rocmOld = `{"card0": {"GPU use (%)": "35", "GPU memory use (%)": "2", "Temperature (Sensor edge) (C)": "45.0", "Temperature (Sensor junction) (C)": "48.0", "VRAM Total Memory (B)": "17163091968", "VRAM Total Used Memory (B)": "292503552"}, "system": {"Driver version": "5.18.2"}}`

// ROCm 6.x on two MI250 dies, with product names and the other field names,
// printed over several lines after a warning.
const rocmNew = `WARNING: AMD GPU device(s) is/are in a low-power state.
{
"card1": {"Card Series": "AMD Instinct MI250X", "GPU use (%)": "100", "GPU Memory Allocated (VRAM%)": "50", "Temperature (Sensor edge) (C)": "N/A", "Temperature (Sensor junction) (C)": "61.0", "Temperature (Sensor memory) (C)": "58.0", "VRAM Total Memory (B)": 68702699520, "VRAM Total Used Memory (B)": "34351349760"},
"card0": {"Card Series": "AMD Instinct MI250X", "GPU use (%)": "0"}
}`

func TestROCmJSONInTwoShapes(t *testing.T) {
	old := parseROCm(rocmOld)
	wantOld := []GPU{{Vendor: "AMD", Index: 0, Name: "AMD card0", UtilPercent: 35, MemoryUsedBytes: 292503552, MemoryTotalBytes: 17163091968, TemperatureC: 45, PowerWatts: -1, FanPercent: -1}}
	if !reflect.DeepEqual(old, wantOld) {
		t.Fatalf("old = %+v", old)
	}
	// The monitor joins the lines of the output before sending it.
	current := parseROCm(strings.ReplaceAll(rocmNew, "\n", ""))
	if len(current) != 2 || current[0].Index != 0 || current[1].Index != 1 {
		t.Fatalf("current = %+v", current)
	}
	if g := current[0]; g.Name != "AMD Instinct MI250X" || g.UtilPercent != 0 || g.MemoryTotalBytes != 0 || g.TemperatureC != -1 {
		t.Fatalf("card with only a use figure: %+v", g)
	}
	if g := current[1]; g.UtilPercent != 100 || g.MemoryUsedBytes != 34351349760 || g.MemoryTotalBytes != 68702699520 || g.TemperatureC != 61 {
		t.Fatalf("hottest sensor expected when the edge one is N/A: %+v", g)
	}
	for _, junk := range []string{"", "{", "{}", `{"card0": 5}`, `{"cardX": {"GPU use (%)": "1"}}`, "[1,2]"} {
		if gpus := parseROCm(junk); len(gpus) != 0 {
			t.Errorf("%q produced %+v", junk, gpus)
		}
	}
}

func TestSysfsRowsFromTheAmdgpuFiles(t *testing.T) {
	gpus := parseAll(t, "gop-gpu-sysfs card0 35 292503552 17163091968 45000\ngop-gpu-sysfs card1 - - - -\ngop-gpu-sysfs card2 1 2\ngop-gpu-sysfs cardx 1 2 3 4")
	want := []GPU{
		{Vendor: "AMD", Index: 0, Name: "AMD card0", UtilPercent: 35, MemoryUsedBytes: 292503552, MemoryTotalBytes: 17163091968, TemperatureC: 45, PowerWatts: -1, FanPercent: -1},
		{Vendor: "AMD", Index: 1, Name: "AMD card1", UtilPercent: -1, TemperatureC: -1, PowerWatts: -1, FanPercent: -1},
	}
	if !reflect.DeepEqual(gpus, want) {
		t.Fatalf("gpus = %+v", gpus)
	}
}

func TestOnlyTheFirstGPUsAreKept(t *testing.T) {
	var gpus []GPU
	for index := range 2 * MaxGPUs {
		gpus = parseGPULine(gpus, []byte("gop-gpu-sysfs card"+itoa(index)+" 1 2 3 4"))
	}
	if len(gpus) != MaxGPUs {
		t.Fatalf("kept %d accelerators", len(gpus))
	}
}

func FuzzParseGPULine(f *testing.F) {
	f.Add(nvidiaLines(nvidiaTwoGPUs))
	f.Add([]byte(gpuRocmPrefix + rocmOld))
	f.Add([]byte("gop-gpu-sysfs card0 1 2 3 4"))
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, gpu := range parseGPULine(nil, data) {
			if gpu.UtilPercent > 100 || gpu.FanPercent > 100 {
				t.Fatalf("percentage out of range: %+v", gpu)
			}
		}
	})
}

func TestWorkerAttachesTheGPULinesBeforeACountersLineToThatSample(t *testing.T) {
	lines := []string{reading(1)}
	lines = append(lines, strings.SplitAfter(string(nvidiaLines(nvidiaTwoGPUs)), "\n")[:2]...)
	lines = append(lines, "gop-gpu-nvidia NVIDIA-SMI has failed\n", reading(2))
	next := make(chan struct{})
	worker := start(streamFunc(func(ctx context.Context, _ string, _ int, line func([]byte) bool) error {
		for _, text := range lines {
			if !line([]byte(text)) {
				return nil
			}
		}
		select {
		case <-next:
		case <-ctx.Done():
			return ctx.Err()
		}
		if !line([]byte(reading(3))) {
			return nil
		}
		<-ctx.Done()
		return ctx.Err()
	}), nil, time.Second, time.Second)
	defer worker.Close()
	first := nextResult(t, worker)
	if first.Err != nil || len(first.Sample.GPUs) != 2 {
		t.Fatalf("first = %+v", first)
	}
	close(next)
	if second := nextResult(t, worker); second.Err != nil || len(second.Sample.GPUs) != 0 {
		t.Fatalf("a reading without GPU lines carried %+v", second.Sample.GPUs)
	}
}

// probe runs the shipped GPU section of the monitor under the system shell,
// with the given directory first on the PATH.
func probe(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	begin, end := strings.Index(command, "# gpu-probe-begin"), strings.Index(command, "# gpu-probe-end")
	if begin < 0 || end < begin {
		t.Fatal("the monitor has no GPU section")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, "/bin/sh", "-c", command[begin:end]+"\ngpu_probe")
	process.Env = append(os.Environ(), "PATH="+dir+":/usr/bin:/bin")
	out, err := process.Output()
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	return string(out)
}

func fakeTool(t *testing.T, dir, name, output string) {
	t.Helper()
	script := "#!/bin/sh\ncat <<'EOF'\n" + output + "\nEOF\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestProbePrintsWhatTheInstalledToolsReport(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "nvidia-smi", strings.TrimSpace(nvidiaTwoGPUs))
	fakeTool(t, dir, "rocm-smi", rocmNew)
	lines := probe(t, dir)
	gpus := parseAll(t, lines)
	if len(gpus) != 4 || gpus[0].Vendor != "NVIDIA" || gpus[2].Name != "AMD Instinct MI250X" {
		t.Fatalf("probe output %q parsed to %+v", lines, gpus)
	}
}
