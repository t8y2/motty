package monitoring

import (
	"fmt"
	"motty/internal/remotestats"
	"slices"
	"time"
)

// Display selects visible and collected metrics. Missing settings preserve the old defaults;
// listing all five metrics explicitly also represents an intentionally empty card.
type Display struct {
	Hidden []string `json:"hidden,omitempty"`
}

// Clone detaches a panel definition for persistence and local-state refreshes.
func (p Panel) Clone() Panel {
	p.Hosts = slices.Clone(p.Hosts)
	if p.Display != nil {
		display := make(map[string]Display, len(p.Display))
		for host, settings := range p.Display {
			display[host] = Display{Hidden: slices.Clone(settings.Hidden)}
		}
		p.Display = display
	}
	return p
}

func (p *Panel) Shows(host, metric string) bool {
	return !slices.Contains(p.Display[host].Hidden, metric)
}

func (p *Panel) Metrics(host string) remotestats.Metrics {
	var metrics remotestats.Metrics
	for i, name := range [5]string{"cpu", "memory", "network", "disks", "gpu"} {
		if p.Shows(host, name) {
			metrics |= 1 << i
		}
	}
	return metrics
}

// GPUKey follows hardware identity rather than a potentially reordered slice.
func GPUKey(g remotestats.GPU) string {
	if g.UUID != "" {
		return g.UUID
	}
	return fmt.Sprintf("%s/%d/%s", g.Vendor, g.Index, g.Name)
}

type GPUHistory struct {
	Device  remotestats.GPU
	Present bool
	Points  []Point // utilization %, VRAM GiB; other values are absent
}
type gpuHistory struct {
	device  remotestats.GPU
	present bool
	seen    time.Time
	series  history
}

func (h *history) resize(limit int) {
	if h.limit == limit {
		return
	}
	old := h.ordered()
	h.points = nil
	h.head = 0
	h.count = 0
	h.limit = limit
	for _, p := range old[max(0, len(old)-limit):] {
		h.push(p)
	}
}
func (h *history) publishGPUs(at time.Time, devices []remotestats.GPU, gap bool, interval time.Duration, cutoff time.Time) {
	if len(devices) == 0 && len(h.gpus) == 0 {
		return
	}
	if h.gpus == nil {
		h.gpus = make(map[string]*gpuHistory)
	}
	for _, g := range h.gpus {
		g.present = false
	}
	current := make(map[string]bool, len(devices))
	for _, device := range devices[:min(len(devices), remotestats.MaxGPUs)] {
		current[GPUKey(device)] = true
	}
	for _, device := range devices[:min(len(devices), remotestats.MaxGPUs)] {
		key := GPUKey(device)
		g := h.gpus[key]
		if g == nil {
			if len(h.gpus) >= remotestats.MaxGPUs {
				oldest := ""
				for k, v := range h.gpus {
					if !current[k] && (oldest == "" || v.seen.Before(h.gpus[oldest].seen)) {
						oldest = k
					}
				}
				if oldest == "" {
					continue
				}
				delete(h.gpus, oldest)
			}
			g = &gpuHistory{}
			h.gpus[key] = g
		}
		device.Processes = nil // process snapshots belong to the sidebar, not retained chart history
		g.device, g.present, g.seen = device, true, at
	}
	for key, g := range h.gpus {
		if !g.present && g.seen.Before(cutoff) {
			delete(h.gpus, key)
			continue
		}
		g.series.resize(h.limit)
		if g.series.count > 0 {
			last := g.series.at(g.series.count - 1).At
			if gap || at.Sub(last) > 3*interval {
				g.series.push(Point{At: last.Add(at.Sub(last) / 2), Values: [5]float32{nan(), nan(), nan(), nan(), nan()}})
			}
		}
		values := [5]float32{nan(), nan(), nan(), nan(), nan()}
		if g.present {
			if g.device.UtilPercent >= 0 {
				values[0] = float32(g.device.UtilPercent)
			}
			if g.device.MemoryKnown || g.device.MemoryUsedBytes > 0 || g.device.MemoryTotalBytes > 0 {
				values[1] = float32(float64(g.device.MemoryUsedBytes) / (1 << 30))
			}
		}
		g.series.push(Point{At: at, Values: values})
		g.series.prune(cutoff)
	}
}
func (h *history) readGPUs(buckets int) []GPUHistory {
	if len(h.gpus) == 0 {
		return nil
	}
	out := make([]GPUHistory, 0, len(h.gpus))
	for _, g := range h.gpus {
		out = append(out, GPUHistory{Device: g.device, Present: g.present, Points: reduce(g.series.count, g.series.at, buckets)})
	}
	slices.SortFunc(out, func(a, b GPUHistory) int {
		if a.Device.Index < b.Device.Index {
			return -1
		}
		if a.Device.Index > b.Device.Index {
			return 1
		}
		if GPUKey(a.Device) < GPUKey(b.Device) {
			return -1
		}
		if GPUKey(a.Device) > GPUKey(b.Device) {
			return 1
		}
		return 0
	})
	return out
}
