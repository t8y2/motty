package gate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"motty/internal/ai/tools"
)

// DiskUsage is one mounted filesystem of a host.
type DiskUsage struct {
	Mount          string  `json:"mount"`
	UsedBytes      uint64  `json:"used_bytes"`
	TotalBytes     uint64  `json:"total_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	Percent        float64 `json:"percent"`
}

// MemoryUsage is a host's memory in use against its total.
type MemoryUsage struct {
	UsedBytes  uint64  `json:"used_bytes"`
	TotalBytes uint64  `json:"total_bytes"`
	Percent    float64 `json:"percent"`
}

// HostResources is one host's reading. A host that could not be read carries
// Error and nothing else, so one unreachable host never hides the others.
type HostResources struct {
	Host        string       `json:"host"`
	Name        string       `json:"name"`
	CollectedAt string       `json:"collected_at,omitempty"`
	CPUPercent  *float64     `json:"cpu_percent,omitempty"`
	Memory      *MemoryUsage `json:"memory,omitempty"`
	Disks       []DiskUsage  `json:"disks,omitempty"`
	// Warnings name what a reachable host could not report.
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// ActivityEntry is one recorded agent call.
type ActivityEntry struct {
	Time    string `json:"time"`
	Host    string `json:"host"`
	Tool    string `json:"tool"`
	Summary string `json:"summary"`
	Outcome string `json:"outcome"`
}

// HostResourceReader is a backend that can sample the resource use of the hosts
// it serves.
type HostResourceReader interface {
	Resources(context.Context, tools.HostResourcesArgs) ([]HostResources, error)
}

// ActivityReader is a backend that keeps a log of what agents did on its hosts.
type ActivityReader interface {
	Activity(context.Context, tools.HostActivityArgs) ([]ActivityEntry, error)
}

func (g Gate) runHostStatusTool(ctx context.Context, name string, raw []byte) (Result, bool) {
	switch name {
	case tools.NameHostResources:
		reader, ok := g.Backend.(HostResourceReader)
		if !ok {
			return failure(errors.New("host resources are not available on this backend")), true
		}
		args, err := tools.DecodeHostResources(raw)
		if err != nil {
			return failure(err), true
		}
		hosts, err := reader.Resources(ctx, args)
		if err != nil {
			return failure(err), true
		}
		reply := structured(map[string]any{"hosts": hosts}, formatResources(hosts))
		// Reaching no host at all is a failed call; reaching some is an answer.
		reply.IsError = len(hosts) > 0
		for _, host := range hosts {
			if host.Error == "" {
				reply.IsError = false
			}
		}
		return reply, true
	case tools.NameHostActivity:
		reader, ok := g.Backend.(ActivityReader)
		if !ok {
			return failure(errors.New("the activity log is not available on this backend")), true
		}
		args, err := tools.DecodeHostActivity(raw)
		if err != nil {
			return failure(err), true
		}
		entries, err := reader.Activity(ctx, args)
		if err != nil {
			return failure(err), true
		}
		return structured(map[string]any{"entries": entries}, formatActivity(entries)), true
	}
	return Result{}, false
}

func formatResources(hosts []HostResources) string {
	if len(hosts) == 0 {
		return "no hosts are open to agents"
	}
	var text strings.Builder
	for index, host := range hosts {
		if index > 0 {
			text.WriteString("\n\n")
		}
		fmt.Fprintf(&text, "== %s (%s) ==\n", host.Host, host.Name)
		if host.Error != "" {
			text.WriteString("[error: " + host.Error + "]")
			continue
		}
		fmt.Fprintf(&text, "collected_at: %s\n", host.CollectedAt)
		if host.CPUPercent != nil {
			fmt.Fprintf(&text, "cpu: %.1f%%\n", *host.CPUPercent)
		}
		if host.Memory != nil {
			fmt.Fprintf(&text, "memory: %.1f%% (%d of %d bytes)\n", host.Memory.Percent, host.Memory.UsedBytes, host.Memory.TotalBytes)
		}
		for _, disk := range host.Disks {
			fmt.Fprintf(&text, "disk %s: %.0f%% (%d of %d bytes, %d available)\n", disk.Mount, disk.Percent, disk.UsedBytes, disk.TotalBytes, disk.AvailableBytes)
		}
		for _, warning := range host.Warnings {
			text.WriteString("[warning: " + warning + "]\n")
		}
	}
	return strings.TrimRight(text.String(), "\n")
}

func formatActivity(entries []ActivityEntry) string {
	if len(entries) == 0 {
		return "no recorded activity"
	}
	var text strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&text, "%s\t%s\t%s\t%s\t%s\n", entry.Time, entry.Host, entry.Tool, entry.Outcome, entry.Summary)
	}
	return strings.TrimRight(text.String(), "\n")
}
