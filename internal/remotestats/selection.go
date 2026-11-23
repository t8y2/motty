package remotestats

import (
	"strconv"
	"strings"
)

// Metrics limits the work performed by the shared samplers. Callers that omit
// a selection keep the sidebar's full collection behavior.
type Metrics uint8

const (
	CPU Metrics = 1 << iota
	Memory
	NetworkIO
	Disks
	GPUs
	AllMetrics = CPU | Memory | NetworkIO | Disks | GPUs
)

func selection(values []Metrics) Metrics {
	if len(values) == 0 {
		return AllMetrics
	}
	return values[0] & AllMetrics
}

// replaceSection edits only fixed, application-owned script templates. Bounds
// are pinned by command-generation and executable sampler tests.
func replaceSection(script, begin, end, replacement string) string {
	start := strings.Index(script, begin)
	if start < 0 {
		return script
	}
	stop := strings.Index(script[start+len(begin):], end)
	if stop < 0 {
		return script
	}
	return script[:start] + replacement + script[start+len(begin)+stop:]
}

func selectedLinux(script string, m Metrics) string {
	checks := []string{"[ ! -r /proc/uptime ]"}
	for _, file := range []struct {
		metric Metrics
		path   string
	}{{CPU, "/proc/stat"}, {Memory, "/proc/meminfo"}, {NetworkIO, "/proc/net/dev"}} {
		if m&file.metric != 0 {
			checks = append(checks, "[ ! -r "+file.path+" ]")
		}
	}
	script = strings.Replace(script, "if [ ! -r /proc/stat ] || [ ! -r /proc/meminfo ] || [ ! -r /proc/net/dev ] || [ ! -r /proc/uptime ]; then", "if "+strings.Join(checks, " || ")+"; then", 1)
	if m&GPUs == 0 {
		script = replaceSection(script, "# gpu-probe-begin\n", "# gpu-probe-end\n", "")
		script = strings.Replace(script, "\tgpu_probe\n", "", 1)
	}
	if m&CPU == 0 {
		script = replaceSection(script, "\tcore_count=0\n", "\trx=0\n", "\tbusy=0; total=$((total + 1))\n")
		// The protocol needs advancing counters to time a sample; these are
		// transport placeholders, cleared before a Sample reaches consumers.
		script = "total=0\n" + script
	}
	if m&NetworkIO == 0 {
		script = replaceSection(script, "\trx=0\n", "\tmt=\n", "\trx=0; tx=0; primary=\n")
	}
	if m&Memory == 0 {
		script = replaceSection(script, "\tmt=\n", "\tread -r up _", "\tmt=1; ma=1\n")
	}
	return script
}

func selectedDarwin(script string, seconds int, m Metrics) string {
	if m&GPUs == 0 {
		script = replaceSection(script, "gpu_probe() {\n", "top -l 2", "")
	}
	if m&Memory == 0 {
		script = replaceSection(script, "memsize=$(sysctl", "boot=$(sysctl", "memsize=1024\npagesize=4096\n")
		script = replaceSection(script, "\tavail=$(vm_stat", "\tprimary=", "\tavail=1\n")
	}
	if m&NetworkIO == 0 {
		script = replaceSection(script, "\tprimary=$(route", "\tnow=$(date", "\tprimary=\n\ttraffic='0 0'\n")
	}
	if m&CPU == 0 {
		start := strings.Index(script, "top -l 2")
		if start >= 0 {
			script = script[:start] + "emit 1 0\nsleep 0.25 2>/dev/null || sleep 1\nwhile :; do\n\temit 0 0\n\tsleep " + strconv.Itoa(seconds) + "\ndone"
		}
	}
	return script
}

func selectedWindows(script string, m Metrics) string {
	if m&GPUs == 0 {
		script = strings.Replace(script, windowsGPUProbe, "", 1)
		script = strings.Replace(script, "gpus=@(Read-GPUs $gpuAdapters)", "gpus=@()", 1)
		script = replaceSection(script, "    # Probe after the first reading", "    Start-Sleep", "")
	}
	if m&Memory == 0 {
		script = replaceSection(script, "    try { $os=(Query 'Win32_OperatingSystem')", "    $cpu=", "    $os=[pscustomobject]@{TotalVisibleMemorySize=1;FreePhysicalMemory=1;LastBootUpTime=[DateTime]::Now;Version=[Environment]::OSVersion.Version.ToString();SystemDrive=$env:SystemDrive}\n")
	}
	if m&CPU == 0 {
		script = replaceSection(script, "    $cpu=$null;", "    $net=", "    $cpu=$null; $coreRows=@(); $count=0\n")
		script = strings.Replace(script, "        if ($count -eq 0) { try { $count=[int](Query 'Win32_ComputerSystem')[0].NumberOfLogicalProcessors } catch { } }\n", "", 1)
	}
	if m&NetworkIO == 0 {
		script = replaceSection(script, "    $net=@();", "    if (!$meta", "    $net=@(); $netOK=$false\n")
	}
	if m&Disks == 0 {
		script = replaceSection(script, "        $disks=@()\n", "        if ($count", "        $disks=@()\n")
		// Without CPU the next statement after the disk block is $meta.
		if m&CPU == 0 {
			script = replaceSection(script, "        $disks=@()\n", "        $meta=", "        $disks=@()\n")
		}
	}
	return script
}

func selectedDetails(script string, darwin bool, m Metrics) string {
	// Dashboard cards do not display login sessions.
	script = strings.Replace(script, loginsCommand, "", 1)
	if darwin {
		if m&CPU == 0 {
			script = replaceSection(script, "cores=$(sysctl", "{ mount ", "printf 'info\\t1\\t0\\t0\\t0\\t0\\t-\\n'\n")
		}
		if m&Disks == 0 {
			script = replaceSection(script, "{ mount ", "netstat -ibn", "")
		}
		if m&NetworkIO == 0 {
			start := strings.Index(script, "netstat -ibn")
			if start >= 0 {
				script = script[:start]
			}
		}
	} else {
		if m&CPU == 0 {
			script = replaceSection(script, "[ -r /proc/stat ]", "df -Pk", "printf 'info\\t1\\t0\\t0\\t0\\t0\\t-\\n'\n")
		}
		if m&Disks == 0 {
			script = replaceSection(script, "df -Pk", "awk 'NR>2", "")
		}
		if m&NetworkIO == 0 {
			start := strings.Index(script, "awk 'NR>2")
			if start >= 0 {
				script = script[:start]
			}
		}
	}
	return script
}

// FilterSample clears disabled fields, including any protocol placeholders.
func (m Metrics) FilterSample(s Sample) Sample {
	if m&CPU == 0 {
		s.CPUUnavailable, s.CPUPercent, s.Cores = true, 0, nil
	}
	if m&Memory == 0 {
		s.MemoryUsedBytes, s.MemoryTotalBytes = 0, 0
	}
	if m&NetworkIO == 0 {
		s.NetworkUnavailable = true
		s.ReceiveBytesPerSecond, s.SendBytesPerSecond, s.NetworkInterface = 0, 0, ""
	}
	if m&GPUs == 0 {
		s.GPUs = nil
	}
	return s
}

// FilterDetails keeps only metadata requested by the consumer.
func (m Metrics) FilterDetails(d Details) Details {
	if m&CPU == 0 {
		d.CPUCount, d.Load, d.LoadUnavailable = 0, [3]float64{}, true
	}
	if m&Disks == 0 {
		d.Disks, d.PrimaryDisk = nil, ""
	}
	if m&NetworkIO == 0 {
		d.Networks = nil
	}
	return d
}
