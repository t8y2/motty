package workbench

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"motty/internal/ai/agent"
	"motty/internal/ai/gate"
	"motty/internal/ai/tools"
	"motty/internal/remotestats"
	"motty/internal/sshconn"
)

// agentResourceTimeout bounds one host's reading once it is connected; dialing
// has its own limit. The monitor's first reading alone may take ten seconds on
// a host with a heavy login script.
const agentResourceTimeout = 30 * time.Second

// agentWorkerStop is how long a finished reading waits for its sampling command
// to end before the connection goes back to the pool.
const agentWorkerStop = 5 * time.Second

var (
	_ gate.HostResourceReader = (*agentBackend)(nil)
	_ gate.ActivityReader     = (*agentBackend)(nil)
)

// Resources reads CPU, memory and disk use once from each named host with the
// monitor's own sampling commands, in parallel. A host that cannot be reached
// or read reports its own error; the others are unaffected.
func (b *agentBackend) Resources(ctx context.Context, args tools.HostResourcesArgs) ([]gate.HostResources, error) {
	handles := args.Sessions
	if len(handles) == 0 {
		host, err := b.resolve("")
		if err != nil {
			return nil, err
		}
		handles = []string{host.CatalogID}
	}
	readings := make([]gate.HostResources, len(handles))
	var wait sync.WaitGroup
	seen := make(map[string]bool, len(handles))
	for index, handle := range handles {
		host, err := b.resolve(handle)
		if err != nil {
			readings[index] = gate.HostResources{Host: handle, Name: handle, Error: err.Error()}
			continue
		}
		if seen[host.CatalogID] { // the same host named by its id and by its label
			readings[index] = gate.HostResources{Host: host.CatalogID, Name: host.Label, Error: "listed more than once"}
			continue
		}
		seen[host.CatalogID] = true
		wait.Add(1)
		go func() {
			defer wait.Done()
			readings[index] = b.readResources(ctx, host)
		}()
	}
	wait.Wait()
	return readings, nil
}

func (b *agentBackend) readResources(ctx context.Context, host Host) gate.HostResources {
	reading := gate.HostResources{Host: host.CatalogID, Name: host.Label}
	entry, err := b.acquire(ctx, host.CatalogID)
	if err != nil {
		reading.Error = err.Error()
		return reading
	}
	defer entry.release()
	system := entry.conn.Status().System
	if system == sshconn.SystemUnknown {
		system = host.System
	}
	limited, cancel := context.WithTimeout(ctx, agentResourceTimeout)
	defer cancel()
	snapshot := takeMonitorSnapshot(limited, entry.conn, system)
	return snapshot.report(reading, time.Now())
}

// monitorSnapshot is the first reading of the monitor's samplers, taken once.
type monitorSnapshot struct {
	sample                remotestats.Sample
	haveSample            bool
	details               *remotestats.Details
	sampleErr, detailsErr error
}

// takeMonitorSnapshot runs the monitor's CPU/memory sampler and its disk
// details until each has answered once, then stops both. It is the same
// commands and parsers as the monitor panel, so the figures agree; nothing keeps
// running afterwards.
func takeMonitorSnapshot(ctx context.Context, runner monitorRunner, system sshconn.RemoteSystem) monitorSnapshot {
	var snapshot monitorSnapshot
	if !monitorSupported(system) {
		snapshot.sampleErr = remotestats.ErrUnsupported
		return snapshot
	}
	// A Windows stream carries its disks with the sample; elsewhere the slow
	// details command reads them on its own.
	metrics := remotestats.CPU | remotestats.Memory
	if system == sshconn.SystemWindows {
		metrics |= remotestats.Disks
	}
	sampler := startMonitorWorker(runner, nil, system, 1, metrics)
	details := startMonitorDetails(runner, nil, system, 5, remotestats.Disks)
	defer func() {
		sampler.Close()
		details.Close()
		timer := time.NewTimer(agentWorkerStop)
		defer timer.Stop()
		for _, done := range []<-chan struct{}{sampler.Done(), detailsDone(details)} {
			select {
			case <-done:
			case <-timer.C:
				return
			}
		}
	}()

	sampleResults := sampler.Results()
	var detailResults <-chan remotestats.DetailResult
	if details != nil {
		detailResults = details.Results()
	}
	for sampleResults != nil || detailResults != nil {
		select {
		case result, ok := <-sampleResults:
			sampleResults = nil
			switch {
			case !ok:
				snapshot.sampleErr = remotestats.ErrEnded
			case result.Err != nil:
				snapshot.sampleErr = result.Err
			default:
				snapshot.sample, snapshot.haveSample = result.Sample, true
				if result.Details != nil { // a Windows stream carries its disks
					snapshot.details = result.Details
				}
			}
		case result, ok := <-detailResults:
			detailResults = nil
			switch {
			case !ok:
				snapshot.detailsErr = remotestats.ErrEnded
			case result.Err != nil:
				snapshot.detailsErr = result.Err
			default:
				snapshot.details = &result.Details
			}
		case <-ctx.Done():
			if sampleResults != nil {
				snapshot.sampleErr = ctx.Err()
			}
			if detailResults != nil {
				snapshot.detailsErr = ctx.Err()
			}
			sampleResults, detailResults = nil, nil
		}
	}
	return snapshot
}

// detailsDone is the done channel of a possibly absent details worker.
func detailsDone(worker *remotestats.DetailsWorker) <-chan struct{} {
	if worker == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return worker.Done()
}

// report fills the reading from what the snapshot got. With nothing at all the
// host is an error; with part, the missing part is named as a warning.
func (snapshot monitorSnapshot) report(reading gate.HostResources, at time.Time) gate.HostResources {
	if !snapshot.haveSample && snapshot.details == nil {
		err := snapshot.sampleErr
		if err == nil {
			err = snapshot.detailsErr
		}
		reading.Error = describeResourceError(err)
		return reading
	}
	reading.CollectedAt = at.UTC().Format(time.RFC3339)
	if snapshot.haveSample {
		sample := snapshot.sample
		if sample.CPUUnavailable {
			reading.Warnings = append(reading.Warnings, "CPU use is not available on this host")
		} else {
			cpu := sample.CPUPercent
			reading.CPUPercent = &cpu
		}
		if sample.MemoryTotalBytes > 0 {
			reading.Memory = &gate.MemoryUsage{
				UsedBytes: sample.MemoryUsedBytes, TotalBytes: sample.MemoryTotalBytes,
				Percent: float64(sample.MemoryUsedBytes) / float64(sample.MemoryTotalBytes) * 100,
			}
		}
	} else {
		reading.Warnings = append(reading.Warnings, "CPU and memory: "+describeResourceError(snapshot.sampleErr))
	}
	if snapshot.details != nil {
		for _, disk := range snapshot.details.Disks {
			if disk.Virtual {
				continue
			}
			reading.Disks = append(reading.Disks, gate.DiskUsage{
				Mount: disk.Path, UsedBytes: disk.Used, TotalBytes: disk.Total, AvailableBytes: disk.Available, Percent: disk.Percent,
			})
		}
	} else {
		reading.Warnings = append(reading.Warnings, "disks: "+describeResourceError(snapshot.detailsErr))
	}
	return reading
}

func describeResourceError(err error) string {
	switch {
	case err == nil:
		return "no reading"
	case errors.Is(err, context.DeadlineExceeded):
		return "no reading arrived in time"
	}
	return strings.ToValidUTF8(err.Error(), "�")
}

// Activity lists the latest recorded agent calls on the hosts open to agents
// now, newest first. A host closed since stays out of the answer, and so do the
// names of other hosts a multi-host call reached. Summaries pass through the
// same secret masking as text sent to a model: the log keeps commands as typed.
func (b *agentBackend) Activity(ctx context.Context, args tools.HostActivityArgs) ([]gate.ActivityEntry, error) {
	if b.auditLog == nil {
		return nil, errors.New("the activity log is not open")
	}
	hosts := b.currentHosts()
	if args.Session != "" {
		host, err := b.resolve(args.Session)
		if err != nil {
			return nil, err
		}
		hosts = []Host{host}
	}
	labels := make(map[string]string, len(hosts))
	ids := make([]string, 0, len(hosts))
	for _, host := range hosts {
		labels[host.CatalogID] = host.Label
		ids = append(ids, host.CatalogID)
	}
	entries, err := b.auditLog.AuditForHosts(ids, args.Limit)
	if err != nil {
		return nil, err
	}
	listed := make([]gate.ActivityEntry, 0, len(entries))
	for _, entry := range entries {
		var names []string
		for _, id := range strings.Split(entry.HostID, ",") {
			if label, ok := labels[id]; ok {
				names = append(names, label)
			}
		}
		listed = append(listed, gate.ActivityEntry{
			Time: entry.At.UTC().Format(time.RFC3339), Host: strings.Join(names, ", "),
			Tool: entry.Tool, Summary: agent.Redact(entry.Summary), Outcome: entry.Outcome,
		})
	}
	return listed, nil
}
