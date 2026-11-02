package remotestats

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// UTF-8 interface names and metadata can exceed the Linux line budget on
// high-core-count hosts. Keep a separate bound for this single JSON record.
const maxWindowsLineBytes = 32 << 10

// PowerShell's encoded command works with both cmd.exe and PowerShell as an
// OpenSSH default shell. No paths, host names or credentials enter the script.
func WindowsCommand(seconds int) string {
	if !validInterval(seconds) {
		seconds = 2
	}
	script := strings.ReplaceAll(windowsScript, "__INTERVAL__", strconv.Itoa(seconds))
	return encodeWindowsScript(script)
}

func encodeWindowsScript(script string) string {
	lines := strings.Split(script, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	script = strings.Join(lines, "\n")
	u := utf16.Encode([]rune(script))
	data := make([]byte, len(u)*2)
	for i, v := range u {
		binary.LittleEndian.PutUint16(data[i*2:], v)
	}
	return "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(data)
}

func StartWindows(runner Runner, notify func(), seconds int) *Worker {
	if !validInterval(seconds) {
		seconds = 2
	}
	return startCommand(runner, notify, 20*time.Second, time.Duration(seconds)*time.Second+15*time.Second, WindowsCommand(seconds), kindWindows)
}

const windowsScript = `# motty-monitor-windows-v1
$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
[Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false)
[Threading.Thread]::CurrentThread.CurrentCulture=[Globalization.CultureInfo]::InvariantCulture
function Query($name) { @(Get-CimInstance -ClassName $name -OperationTimeoutSec 3 -ErrorAction Stop) }
$meta=$null
$lastMeta=[DateTime]::MinValue
while ($true) {
    try { $os=(Query 'Win32_OperatingSystem')[0]; if (!$os -or !$os.TotalVisibleMemorySize -or $null -eq $os.FreePhysicalMemory) { throw 'unavailable' } }
    catch { [Console]::WriteLine('gop-stats-unsupported Windows'); exit 0 }
    $cpu=$null; $coreRows=@(); $count=0
    try { $cpus=Query 'Win32_PerfFormattedData_Counters_ProcessorInformation' }
    catch { $cpus=@() }
    if (@($cpus | Where-Object { $_.Name -eq '_Total' -and $null -ne $_.PercentProcessorTime }).Count -eq 0) {
        try { $cpus=Query 'Win32_PerfFormattedData_PerfOS_Processor' } catch { $cpus=@() }
    }
    foreach ($c in $cpus) {
        if ($c.Name -eq '_Total') { if ($null -ne $c.PercentProcessorTime) { $cpu=[double]$c.PercentProcessorTime } }
        elseif ($c.Name -match '^[0-9]+(,[0-9]+)?$') {
            $count++
            if ($coreRows.Count -lt 256 -and $null -ne $c.PercentProcessorTime) { $coreRows+=@{id=[string]$c.Name;percent=[double]$c.PercentProcessorTime} }
        }
    }
    $net=@(); $netOK=$true
    try {
        foreach ($n in (Query 'Win32_PerfFormattedData_Tcpip_NetworkInterface' | Select-Object -First 16)) {
            if ($n.Name -match '(?i)loopback') { continue }
            if ($null -eq $n.BytesReceivedPersec -or $null -eq $n.BytesSentPersec) { $netOK=$false; continue }
            $name=[string]$n.Name; if ($name.Length -gt 64) { $name=$name.Substring(0,64) }
            $net+=@{name=$name;rx=[uint64]$n.BytesReceivedPersec;tx=[uint64]$n.BytesSentPersec}
        }
    } catch { $netOK=$false }
    if ($net.Count -eq 0) { $netOK=$false }
    if (!$meta -or (([DateTime]::UtcNow-$lastMeta).TotalSeconds -ge 5)) {
        $disks=@()
        try {
            foreach ($d in (Query 'Win32_LogicalDisk' | Where-Object { $_.DriveType -eq 3 -and $_.Size -gt 0 -and $null -ne $_.FreeSpace } | Select-Object -First 32)) {
                $disks+=@{path=([string]$d.DeviceID+'\');total=[uint64]$d.Size;free=[uint64]$d.FreeSpace}
            }
        } catch { }
        if ($count -eq 0) { try { $count=[int](Query 'Win32_ComputerSystem')[0].NumberOfLogicalProcessors } catch { } }
        $meta=@{count=$count;kernel=('Windows '+[string]$os.Version);disks=$disks;primaryDisk=([string]$os.SystemDrive+'\')}
        $lastMeta=[DateTime]::UtcNow
    }
    $payload=@{cpu=$cpu;total=([uint64]$os.TotalVisibleMemorySize*1024);free=([uint64]$os.FreePhysicalMemory*1024);uptime=[math]::Max(0,([DateTime]::Now-$os.LastBootUpTime).TotalSeconds);cores=@($coreRows);net=@($net);networkAvailable=$netOK;meta=$meta}
    $json=ConvertTo-Json -InputObject $payload -Depth 6 -Compress
    [Console]::WriteLine('gop-stats-win-v1 '+$json)
    Start-Sleep -Seconds __INTERVAL__
}`

func parseWindows(line []byte) (Result, error) {
	var r struct {
		CPU         *float64 `json:"cpu"`
		Total, Free uint64
		Uptime      float64
		Cores       []struct {
			ID      string
			Percent float64
		}
		Net []struct {
			Name   string
			RX, TX uint64
		}
		NetworkAvailable bool
		Meta             struct {
			PrimaryDisk string
			Count       int
			Kernel      string
			Disks       []struct {
				Path        string
				Total, Free uint64
			}
		}
	}
	if len(line) > maxWindowsLineBytes || json.Unmarshal([]byte(strings.TrimPrefix(string(line), "gop-stats-win-v1 ")), &r) != nil || r.Total == 0 || r.Free > r.Total || math.IsNaN(r.Uptime) || math.IsInf(r.Uptime, 0) || r.Uptime < 0 || len(r.Cores) > 256 || len(r.Net) > 16 || len(r.Meta.Disks) > 32 || r.Meta.Count < 0 || r.Meta.Count > 1<<20 {
		return Result{}, ErrMalformed
	}
	s := Sample{MemoryTotalBytes: r.Total, MemoryUsedBytes: r.Total - r.Free, UptimeSeconds: r.Uptime, CPUUnavailable: r.CPU == nil, NetworkUnavailable: !r.NetworkAvailable}
	if r.CPU != nil {
		if !validPercent(*r.CPU) {
			return Result{}, ErrMalformed
		}
		s.CPUPercent = *r.CPU
	}
	seen := make(map[string]bool, len(r.Cores))
	for _, c := range r.Cores {
		if !validCoreID(c.ID) || seen[c.ID] || !validPercent(c.Percent) {
			return Result{}, ErrMalformed
		}
		seen[c.ID] = true
		s.Cores = append(s.Cores, Core{ID: c.ID, Percent: c.Percent})
	}
	d := Details{LoadUnavailable: true, PrimaryDisk: detailText(r.Meta.PrimaryDisk, 32), CPUCount: r.Meta.Count, Uptime: r.Uptime, Kernel: detailText(r.Meta.Kernel, 120)}
	for _, n := range r.Net {
		if math.MaxUint64-s.ReceiveBytesPerSecond < n.RX || math.MaxUint64-s.SendBytesPerSecond < n.TX {
			return Result{}, ErrMalformed
		}
		s.ReceiveBytesPerSecond += n.RX
		s.SendBytesPerSecond += n.TX
		d.Networks = append(d.Networks, Network{Name: detailText(n.Name, 64), ReceivePerSecond: n.RX, SendPerSecond: n.TX, HasRate: true, TotalsUnavailable: true})
	}
	for _, disk := range r.Meta.Disks {
		if disk.Total == 0 || disk.Free > disk.Total {
			continue
		}
		d.Disks = append(d.Disks, Disk{Path: detailText(disk.Path, 2048), Used: disk.Total - disk.Free, Total: disk.Total, Available: disk.Free, Percent: 100 * float64(disk.Total-disk.Free) / float64(disk.Total)})
	}
	sort.Slice(s.Cores, func(i, j int) bool {
		a, b := coreOrder(s.Cores[i].ID)
		x, y := coreOrder(s.Cores[j].ID)
		if a != x {
			return a < x
		}
		return b < y
	})
	return Result{Sample: s, Details: &d}, nil
}

func coreOrder(id string) (int, int) {
	a, b, ok := strings.Cut(id, ",")
	if !ok {
		n, _ := strconv.Atoi(a)
		return 0, n
	}
	x, _ := strconv.Atoi(a)
	y, _ := strconv.Atoi(b)
	return x, y
}

func validCoreID(id string) bool {
	if id == "" || len(id) > 16 {
		return false
	}
	a, b, group := strings.Cut(id, ",")
	if _, err := strconv.ParseUint(a, 10, 20); err != nil {
		return false
	}
	if group {
		if _, err := strconv.ParseUint(b, 10, 20); err != nil {
			return false
		}
	}
	return true
}

func validPercent(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 100 }
