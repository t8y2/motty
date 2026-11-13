package remotestats

import (
	"bytes"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"
)

// MaxGPUs bounds how many accelerators one reading carries; a server with more
// is shown its first MaxGPUs.
const MaxGPUs = 16

// MaxGPUProcesses bounds the process rows one reading carries per accelerator;
// a busy card with more is shown its first MaxGPUProcesses.
const MaxGPUProcesses = 64

// GPU is one accelerator in a Sample. A figure the driver did not report is
// negative (or, for MemoryTotalBytes, zero) so that "not reported" is never
// drawn as "0".
type GPU struct {
	// Vendor is "NVIDIA" or "AMD".
	Vendor string
	// Index is the device number the vendor's tool reports (nvidia-smi index,
	// the N of rocm-smi's cardN).
	Index int
	Name  string
	UUID  string

	UtilPercent      float64
	MemoryUsedBytes  uint64
	MemoryTotalBytes uint64
	TemperatureC     float64
	PowerWatts       float64
	PowerLimitWatts  float64
	FanPercent       float64
	Processes        []GPUProcess
}

// GPUProcess is one compute process an NVIDIA driver reports against a GPU.
type GPUProcess struct {
	UUID            string
	PID             uint64
	Name            string
	MemoryUsedBytes uint64
	// MemoryKnown is false when the driver could not report the process's
	// memory; the row is still listed, without a figure.
	MemoryKnown bool
}

// The probe lines are printed by the monitor before each counters line, which
// closes the reading; they are untrusted text from the server and a line that
// does not fit its format is skipped, never an error for the monitor.
const (
	gpuNvidiaPrefix        = "gop-gpu-nvidia "
	gpuNvidiaProcessPrefix = "gop-gpu-nvidia-proc "
	gpuRocmPrefix          = "gop-gpu-rocm "
	gpuSysfsPrefix         = "gop-gpu-sysfs "
)

func isGPULine(line []byte) bool { return bytes.HasPrefix(line, []byte("gop-gpu-")) }

// parseGPULine appends the accelerators one probe line describes.
func parseGPULine(gpus []GPU, line []byte) []GPU {
	text := strings.TrimSpace(string(line))
	room := MaxGPUs - len(gpus)
	if room <= 0 {
		return gpus
	}
	var found []GPU
	switch {
	case strings.HasPrefix(text, gpuNvidiaPrefix):
		if gpu, ok := parseNvidia(text[len(gpuNvidiaPrefix):]); ok {
			found = append(found, gpu)
		}
	case strings.HasPrefix(text, gpuNvidiaProcessPrefix):
		// The row itself carries no GPU identity, so it is parsed by the
		// worker only after the matching GPU rows are known.
	case strings.HasPrefix(text, gpuRocmPrefix):
		found = parseROCm(text[len(gpuRocmPrefix):])
	case strings.HasPrefix(text, gpuSysfsPrefix):
		if gpu, ok := parseSysfs(text[len(gpuSysfsPrefix):]); ok {
			found = append(found, gpu)
		}
	}
	return append(gpus, found[:min(len(found), room)]...)
}

// parseNvidia reads one row of
// nvidia-smi --query-gpu=index,gpu_uuid,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,power.limit,fan.speed --format=csv,noheader,nounits.
// The figures are counted from the right so that a name holding a comma still
// parses. Rows from the older query without UUID and power limit are accepted
// too; error text ("NVIDIA-SMI has failed ...") has no index and is rejected.
func parseNvidia(row string) (GPU, bool) {
	fields := strings.Split(row, ",")
	if len(fields) < 8 {
		return GPU{}, false
	}
	index, err := strconv.Atoi(strings.TrimSpace(fields[0]))
	if err != nil || index < 0 {
		return GPU{}, false
	}
	if len(fields) == 8 || len(fields) == 9 {
		tail := fields[len(fields)-6:]
		name := strings.TrimSpace(strings.Join(fields[1:len(fields)-6], ","))
		if name == "" {
			return GPU{}, false
		}
		const mib = 1 << 20
		return GPU{
			Vendor: "NVIDIA", Index: index, Name: name,
			UtilPercent:      percent(figure(tail[0])),
			MemoryUsedBytes:  bytesOf(figure(tail[1]), mib),
			MemoryTotalBytes: bytesOf(figure(tail[2]), mib),
			TemperatureC:     nonNegative(figure(tail[3])),
			PowerWatts:       nonNegative(figure(tail[4])),
			PowerLimitWatts:  -1,
			FanPercent:       percent(figure(tail[5])),
		}, true
	}
	if len(fields) < 10 {
		return GPU{}, false
	}
	tail := fields[len(fields)-7:]
	name := strings.TrimSpace(strings.Join(fields[2:len(fields)-7], ","))
	if name == "" {
		return GPU{}, false
	}
	const mib = 1 << 20
	return GPU{
		Vendor: "NVIDIA", Index: index, Name: name, UUID: strings.TrimSpace(fields[1]),
		UtilPercent:      percent(figure(tail[0])),
		MemoryUsedBytes:  bytesOf(figure(tail[1]), mib),
		MemoryTotalBytes: bytesOf(figure(tail[2]), mib),
		TemperatureC:     nonNegative(figure(tail[3])),
		PowerWatts:       nonNegative(figure(tail[4])),
		PowerLimitWatts:  nonNegative(figure(tail[5])),
		FanPercent:       percent(figure(tail[6])),
	}, true
}

// parseNvidiaProcess reads one row of
// nvidia-smi --query-compute-apps=gpu_uuid,pid,process_name,used_memory --format=csv,noheader,nounits.
// It is skipped when the driver cannot report a value or the row does not match.
func parseNvidiaProcess(row string) (GPUProcess, bool) {
	fields := strings.Split(row, ",")
	if len(fields) < 4 {
		return GPUProcess{}, false
	}
	uuid := strings.TrimSpace(fields[0])
	if uuid == "" || strings.EqualFold(uuid, "[N/A]") {
		return GPUProcess{}, false
	}
	pid, err := strconv.ParseUint(strings.TrimSpace(fields[1]), 10, 64)
	if err != nil || pid == 0 {
		return GPUProcess{}, false
	}
	name := nvidiaProcessName(strings.TrimSpace(strings.Join(fields[2:len(fields)-1], ",")))
	if name == "" {
		return GPUProcess{}, false
	}
	used := figure(fields[len(fields)-1])
	return GPUProcess{
		UUID: uuid, PID: pid, Name: name,
		MemoryUsedBytes: bytesOf(used, 1<<20), MemoryKnown: used >= 0,
	}, true
}

func nvidiaProcessName(value string) string {
	if index := strings.LastIndexAny(value, "/\\"); index >= 0 {
		value = value[index+1:]
	}
	return detailText(value, 128)
}

// attachNvidiaProcesses moves each process row onto the GPU whose UUID matches.
// Unmatched rows are dropped so untrusted output cannot invent a GPU.
func attachNvidiaProcesses(gpus []GPU, processes []GPUProcess) []GPU {
	if len(processes) == 0 {
		return gpus
	}
	matched := make(map[string][]GPUProcess, len(gpus))
	for index := range gpus {
		gpu := &gpus[index]
		if gpu.UUID == "" {
			continue
		}
		for _, process := range processes {
			if strings.EqualFold(process.UUID, gpu.UUID) {
				matched[gpu.UUID] = append(matched[gpu.UUID], process)
			}
		}
	}
	for index := range gpus {
		processes := matched[gpus[index].UUID]
		gpus[index].Processes = processes[:min(len(processes), MaxGPUProcesses)]
	}
	return gpus
}

// figure parses a number, or reports -1 for the placeholders drivers print for
// a reading they cannot give ("[N/A]", "[Not Supported]", "N/A", empty).
func figure(text string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return -1
	}
	return value
}

func nonNegative(value float64) float64 {
	if value < 0 {
		return -1
	}
	return value
}

func percent(value float64) float64 {
	if value < 0 {
		return -1
	}
	return min(value, 100)
}

func bytesOf(value float64, unit uint64) uint64 {
	if value < 0 || value > float64(math.MaxInt64/unit) {
		return 0
	}
	return uint64(value) * unit
}

// parseROCm reads the output of
// rocm-smi --showuse --showmemuse --showtemp --showmeminfo vram --showproductname --json,
// joined onto one line. Warnings may precede the JSON object and the keys vary
// with the ROCm release, so only the "cardN" objects are read and only the
// figures present are taken.
func parseROCm(text string) []GPU {
	start, end := strings.IndexByte(text, '{'), strings.LastIndexByte(text, '}')
	if start < 0 || end < start {
		return nil
	}
	var cards map[string]json.RawMessage
	if json.Unmarshal([]byte(text[start:end+1]), &cards) != nil {
		return nil
	}
	var gpus []GPU
	for key, raw := range cards {
		digits, ok := strings.CutPrefix(key, "card")
		index, err := strconv.Atoi(digits)
		if !ok || err != nil || index < 0 {
			continue
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			continue
		}
		gpu := GPU{
			Vendor: "AMD", Index: index, Name: "AMD " + key,
			UtilPercent: -1, TemperatureC: -1, PowerWatts: -1, PowerLimitWatts: -1, FanPercent: -1,
		}
		temperature, edge := -1.0, false
		for label, value := range fields {
			lower := strings.ToLower(label)
			number := figure(rocmText(value))
			switch {
			case strings.HasPrefix(lower, "gpu use"):
				gpu.UtilPercent = percent(number)
			case strings.HasPrefix(lower, "vram total used memory"):
				gpu.MemoryUsedBytes = bytesOf(number, 1)
			case strings.HasPrefix(lower, "vram total memory"):
				gpu.MemoryTotalBytes = bytesOf(number, 1)
			case strings.HasPrefix(lower, "temperature"):
				// The edge sensor is the one a fan curve follows; otherwise the
				// hottest of the others is shown.
				if isEdge := strings.Contains(lower, "edge"); isEdge || !edge {
					if number >= 0 && (isEdge || number > temperature) {
						temperature, edge = number, isEdge
					}
				}
			case strings.Contains(lower, "power (w)"):
				if number >= 0 && gpu.PowerWatts < 0 {
					gpu.PowerWatts = number
				}
			case lower == "card series" || lower == "card model":
				if name := strings.TrimSpace(rocmText(value)); name != "" {
					gpu.Name = name
				}
			}
		}
		gpu.TemperatureC = temperature
		gpus = append(gpus, gpu)
	}
	slices.SortFunc(gpus, func(a, b GPU) int { return a.Index - b.Index })
	return gpus
}

// rocmText is a JSON value as text: rocm-smi prints numbers as strings, other
// versions as numbers.
func rocmText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return string(raw)
}

// parseSysfs reads the "cardN busy used total tempMilli" row of the amdgpu
// sysfs fallback, "-" standing for a file that could not be read.
func parseSysfs(row string) (GPU, bool) {
	fields := strings.Fields(row)
	if len(fields) != 5 {
		return GPU{}, false
	}
	digits, ok := strings.CutPrefix(fields[0], "card")
	index, err := strconv.Atoi(digits)
	if !ok || err != nil || index < 0 {
		return GPU{}, false
	}
	temperature := figure(fields[4])
	if temperature >= 0 {
		temperature /= 1000
	}
	return GPU{
		Vendor: "AMD", Index: index, Name: "AMD " + fields[0],
		UtilPercent:      percent(figure(fields[1])),
		MemoryUsedBytes:  bytesOf(figure(fields[2]), 1),
		MemoryTotalBytes: bytesOf(figure(fields[3]), 1),
		TemperatureC:     temperature, PowerWatts: -1, PowerLimitWatts: -1, FanPercent: -1,
	}, true
}
