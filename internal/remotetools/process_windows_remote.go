package remotetools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// windowsProcessScript reads processes through CIM so any Windows OpenSSH
// default shell works. The account pass needs an elevated session: without it
// every user reads as the placeholder, which keeps the list usable and
// read-only rather than failing.
const windowsProcessScript = `$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
[Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false)
[Threading.Thread]::CurrentThread.CurrentCulture=[Globalization.CultureInfo]::InvariantCulture
try {
  $os=Get-CimInstance Win32_OperatingSystem -OperationTimeoutSec 3
  if (!$os.TotalVisibleMemorySize) { throw 'Physical memory is unavailable' }
  $total=[double]$os.TotalVisibleMemorySize*1024
  $metadata=@{}
  Get-CimInstance Win32_Process -OperationTimeoutSec 3 | Select-Object -First 5001 | ForEach-Object { $metadata[[int]$_.ProcessId]=$_ }
  $owners=@{}
  try { Get-Process -IncludeUserName -ErrorAction Stop | Select-Object -First 5001 | ForEach-Object { $owners[[int]$_.Id]=[string]$_.UserName } } catch {}
  $rows=@(Get-CimInstance Win32_PerfFormattedData_PerfProc_Process -OperationTimeoutSec 3 | Where-Object { $_.IDProcess -gt 0 -and $_.Name -ne '_Total' -and $_.IDProcess -ne $PID } | Select-Object -First 5001 | ForEach-Object {
    $id=[int]$_.IDProcess; $p=$metadata[$id]
    $command=[string]$p.CommandLine
    if (!$command) { $command=[string]$p.Name }; if (!$command) { $command=[string]$_.Name }
    $truncated=$command.Length -gt 1024; if ($truncated) { $command=$command.Substring(0,1024) }
    $owner=$owners[$id]; if (!$owner) { $owner='—' }; if ($owner.Length -gt 128) { $owner=$owner.Substring(0,128) }
    $parent=0; $rss=0; $virtual=0; $execution=0
    if ($p) {
      $parent=[int]$p.ParentProcessId
      $rss=[uint64]$p.WorkingSetSize
      $virtual=[uint64]$p.VirtualSize
      $execution=[int]$p.ExecutionState
    }
    @{pid=$id;parent=$parent;user=$owner;state=$execution;cpu=$_.PercentProcessorTime;memory=([double]$_.WorkingSet/$total*100);rss=$rss;virtual=$virtual;elapsed=$_.ElapsedTime;command=$command;truncated=$truncated}
  })
  [Console]::WriteLine((ConvertTo-Json -InputObject $rows -Compress -Depth 3))
} catch { [Console]::WriteLine($_.Exception.Message); exit 1 }
`

func readWindowsProcesses(ctx context.Context, runner Runner, key SortKey) (ProcessSnapshot, error) {
	output, err := runner.RunCommand(ctx, encodePowerShell(windowsProcessScript), maxSnapshotOutput)
	if err != nil {
		if message := snapshotDiagnostic(output); message != "" {
			return ProcessSnapshot{}, &Failure{Message: message, Err: err}
		}
		return ProcessSnapshot{}, err
	}
	return parseWindowsProcesses(output, key)
}

func parseWindowsProcesses(output []byte, key SortKey) (ProcessSnapshot, error) {
	var rows []struct {
		PID       int      `json:"pid"`
		Parent    int      `json:"parent"`
		User      string   `json:"user"`
		State     int      `json:"state"`
		CPU       *float64 `json:"cpu"`
		Memory    float64  `json:"memory"`
		RSS       uint64   `json:"rss"`
		Virtual   uint64   `json:"virtual"`
		Elapsed   uint64   `json:"elapsed"`
		Command   string   `json:"command"`
		Truncated bool     `json:"truncated"`
	}
	if err := json.Unmarshal(output, &rows); err != nil || rows == nil || len(rows) > maxSnapshotProcesses+1 {
		return ProcessSnapshot{}, ErrMalformed
	}
	result := ProcessSnapshot{Truncated: len(rows) > maxSnapshotProcesses}
	seen := make(map[int]bool, len(rows))
	for _, r := range rows {
		// A row can be malformed without the whole machine being unreadable: a
		// process may exit between the CIM and the perf-counter query, and one
		// duplicate or zeroed row must not throw away every other process.
		if r.PID <= 0 || r.Parent < 0 || seen[r.PID] || r.CPU == nil || math.IsNaN(*r.CPU) || math.IsInf(*r.CPU, 0) || *r.CPU < 0 || math.IsNaN(r.Memory) || math.IsInf(r.Memory, 0) || r.Memory < 0 || r.Memory > 100 || r.Elapsed > uint64(1000*366*86400) {
			continue
		}
		seen[r.PID] = true
		command := strings.ToValidUTF8(r.Command, "�")
		if len(command) > maxSnapshotCommand {
			command = strings.ToValidUTF8(command[:maxSnapshotCommand], "") + "…"
			r.Truncated = true
		}
		result.CommandsTruncated = result.CommandsTruncated || r.Truncated
		seconds := r.Elapsed
		elapsed := fmt.Sprintf("%02d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
		if seconds >= 86400 {
			elapsed = fmt.Sprintf("%d-%02d:%02d:%02d", seconds/86400, seconds/3600%24, seconds/60%60, seconds%60)
		}
		result.Processes = append(result.Processes, Process{
			PID: r.PID, ParentPID: r.Parent, User: r.User, State: windowsProcessState(r.State),
			CPU: *r.CPU, Memory: r.Memory, ResidentBytes: r.RSS, VirtualBytes: r.Virtual,
			Elapsed: elapsed, Command: command,
		})
	}
	if len(result.Processes) == 0 && len(rows) > 0 {
		return ProcessSnapshot{}, ErrMalformed
	}
	sort.SliceStable(result.Processes, func(i, j int) bool {
		if key == SortMemory {
			return result.Processes[i].Memory > result.Processes[j].Memory
		}
		return result.Processes[i].CPU > result.Processes[j].CPU
	})
	if len(result.Processes) > maxSnapshotProcesses {
		result.Processes = result.Processes[:maxSnapshotProcesses]
	}
	return result, nil
}

func windowsProcessState(code int) string {
	switch code {
	case 2:
		return "Ready"
	case 3:
		return "Running"
	case 4:
		return "Blocked"
	case 5:
		return "Suspended"
	case 6:
		return "Terminated"
	case 7:
		return "Stopped"
	case 8:
		return "Growing"
	case 9:
		return "Not Responding"
	default:
		return "Unknown"
	}
}
