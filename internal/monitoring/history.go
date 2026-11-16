// Package monitoring owns bounded, in-memory dashboard history. Collection can
// publish while the native window is hidden; painting never drives sampling.
package monitoring

import (
	"math"
	"slices"
	"sync"
	"time"

	"motty/internal/remotestats"
)

type Panel struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Note      string   `json:"note,omitempty"`
	Hosts     []string `json:"hosts"`
	Simple    bool     `json:"simple,omitempty"`
	Interval  int      `json:"interval"`
	Retention int      `json:"retention_minutes"`
}

func ValidInterval(n int) bool  { return n == 2 || n == 5 || n == 10 || n == 30 || n == 60 }
func ValidRetention(n int) bool { return n == 15 || n == 60 || n == 360 || n == 1440 }
func (p *Panel) Normalize() {
	if !ValidInterval(p.Interval) {
		p.Interval = 5
	}
	if !ValidRetention(p.Retention) {
		p.Retention = 60
	}
	hosts := make([]string, 0, len(p.Hosts))
	seen := make(map[string]bool, len(p.Hosts))
	for _, id := range p.Hosts {
		if id != "" && !seen[id] {
			seen[id] = true
			hosts = append(hosts, id)
		}
	}
	p.Hosts = hosts
}

type Status uint8

const (
	Waiting Status = iota
	Connecting
	Live
	Failed
	Unsupported
	Missing
)

// Point carries actual observation time, with NaN for absent metrics. Values
// are CPU %, memory %, received bytes/s, sent bytes/s and primary disk %.
type Point struct {
	At     time.Time
	Values [5]float32
}
type Snapshot struct {
	Version       uint64
	Status        Status
	Sample        remotestats.Sample
	HasSample     bool
	Details       remotestats.Details
	DetailsFailed bool
	Updated       time.Time
	Points        []Point
}
type history struct {
	Snapshot
	points             []Point
	head, count, limit int
	gap                bool
	next               time.Time
}
type runtime struct {
	config  Panel
	running bool
	hosts   map[string]*history
}
type Engine struct {
	mu      sync.Mutex
	panels  map[string]*runtime
	sources map[string]uint64
}

// BeginSource invalidates replies from an earlier, cancelled collector.
func (e *Engine) BeginSource(id string) uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sources == nil {
		e.sources = make(map[string]uint64)
	}
	e.sources[id]++
	return e.sources[id]
}

func (e *Engine) Set(p Panel, running bool) {
	p.Normalize()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.panels == nil {
		e.panels = make(map[string]*runtime)
	}
	r := e.panels[p.ID]
	if r == nil {
		r = &runtime{hosts: make(map[string]*history)}
		e.panels[p.ID] = r
	}
	if r.running == running && sameCollection(r.config, p) {
		// Editing another host reconciles every panel through Set; when this
		// panel still collects exactly the same hosts on the same schedule its
		// history must keep its revision, or every chart would be rebuilt.
		r.config = p
		return
	}
	for id := range r.hosts {
		if !slices.Contains(p.Hosts, id) {
			delete(r.hosts, id)
		}
	}
	for _, id := range p.Hosts {
		h := r.hosts[id]
		if h == nil {
			h = &history{}
			r.hosts[id] = h
		}
		// A slower new cadence must not evict faster samples still inside the
		// retention window. Bound by the fastest supported cadence (2s), with
		// room for an explicit gap before each observation.
		limit := p.Retention*60 + 8
		if h.limit != limit {
			old := h.ordered()
			h.points = nil
			h.head = 0
			h.count = 0
			h.limit = limit
			for _, v := range old[max(0, len(old)-limit):] {
				h.push(v)
			}
		}
		if running && !r.running {
			h.gap = h.HasSample
			h.next = time.Time{}
			h.Status = Connecting
		}
		if p.Interval != r.config.Interval {
			h.next = time.Time{}
		}
		if h.count > 0 {
			h.prune(h.at(h.count - 1).At.Add(-time.Duration(p.Retention) * time.Minute))
		}
		h.Version++
	}
	r.config = p
	r.running = running
}

// sameCollection reports whether p collects exactly like previous: the same
// hosts at the same cadence and retention. Name, note and display mode never
// reach the history, so changing them alone does not invalidate it.
func sameCollection(previous, p Panel) bool {
	return previous.Interval == p.Interval && previous.Retention == p.Retention && slices.Equal(previous.Hosts, p.Hosts)
}
func (e *Engine) Remove(id string) { e.mu.Lock(); defer e.mu.Unlock(); delete(e.panels, id) }
func (e *Engine) Wanted() map[string]int {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make(map[string]int)
	for _, r := range e.panels {
		if r.running {
			for _, id := range r.config.Hosts {
				if n, ok := result[id]; !ok || r.config.Interval < n {
					result[id] = r.config.Interval
				}
			}
		}
	}
	return result
}
func (e *Engine) each(id string, f func(*runtime, *history)) {
	for _, r := range e.panels {
		if r.running {
			if h := r.hosts[id]; h != nil {
				f(r, h)
			}
		}
	}
}
func (e *Engine) SetStatus(id string, status Status) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.each(id, func(_ *runtime, h *history) {
		if h.Status != status {
			h.Status = status
			h.Version++
			if status != Live {
				h.gap = h.HasSample
				h.next = time.Time{}
			}
		}
	})
}
func (e *Engine) Details(id string, d remotestats.Details, failed bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.details(id, d, failed)
}
func (e *Engine) DetailsFrom(id string, generation uint64, d remotestats.Details, failed bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sources[id] != generation {
		return
	}
	e.details(id, d, failed)
}
func (e *Engine) details(id string, d remotestats.Details, failed bool) {
	e.each(id, func(_ *runtime, h *history) {
		if !failed {
			h.Details = d
		}
		h.DetailsFailed = failed
		h.Version++
	})
}
func (e *Engine) Publish(id string, at time.Time, s remotestats.Sample) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.publish(id, at, s)
}
func (e *Engine) PublishFrom(id string, generation uint64, at time.Time, s remotestats.Sample) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sources[id] != generation {
		return
	}
	e.publish(id, at, s)
}
func (e *Engine) publish(id string, at time.Time, s remotestats.Sample) {
	e.each(id, func(r *runtime, h *history) {
		previousInterface := h.Sample.NetworkInterface
		if h.count > 0 && !at.After(h.at(h.count-1).At) {
			return
		}
		if !h.next.IsZero() && at.Before(h.next) {
			return
		}
		h.Sample = s
		h.HasSample = true
		h.Status = Live
		h.Updated = at
		h.Version++
		if h.count > 0 && (h.gap || at.Sub(h.at(h.count-1).At) > time.Duration(r.config.Interval*3)*time.Second) {
			last := h.at(h.count - 1).At
			gapAt := last.Add(at.Sub(last) / 2)
			h.push(Point{At: gapAt, Values: [5]float32{nan(), nan(), nan(), nan(), nan()}})
		}
		h.gap = false
		v := [5]float32{nan(), nan(), nan(), nan(), nan()}
		if !s.CPUUnavailable {
			v[0] = float32(s.CPUPercent)
		}
		if s.MemoryTotalBytes > 0 {
			v[1] = 100 * float32(s.MemoryUsedBytes) / float32(s.MemoryTotalBytes)
		}
		if !s.NetworkUnavailable {
			v[2] = float32(s.ReceiveBytesPerSecond)
			v[3] = float32(s.SendBytesPerSecond)
		}
		if !h.DetailsFailed {
			for _, disk := range h.Details.Disks {
				if !disk.Virtual && (h.Details.PrimaryDisk == "" || disk.Path == h.Details.PrimaryDisk) {
					v[4] = float32(disk.Percent)
					break
				}
			}
		}
		// A changed interface is a new counter domain, not a continuous trend.
		if h.count > 0 && previousInterface != s.NetworkInterface {
			v[2] = nan()
			v[3] = nan()
		}
		h.push(Point{At: at, Values: v})
		if h.next.IsZero() {
			h.next = at
		}
		step := time.Duration(r.config.Interval) * time.Second
		for !h.next.After(at) {
			h.next = h.next.Add(step)
		}
		h.prune(at.Add(-time.Duration(r.config.Retention) * time.Minute))
	})
}

// Read returns a new immutable snapshot only when its revision changes. The
// caller invalidates its revision on viewport-width changes as well.
func (e *Engine) Read(panel, id string, buckets int, version uint64) (Snapshot, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.panels[panel]
	if r == nil {
		return Snapshot{}, false
	}
	h := r.hosts[id]
	if h == nil || h.Version == version {
		return Snapshot{}, false
	}
	s := h.Snapshot
	// Reduce straight out of the ring: a retained 24 h window holds tens of
	// thousands of points, and copying them all on every sample would cost more
	// than the chart it feeds.
	s.Points = reduce(h.count, h.at, max(1, buckets))
	return s, true
}
func nan() float32                { return float32(math.NaN()) }
func (h *history) at(i int) Point { return h.points[(h.head+i)%len(h.points)] }
func (h *history) ordered() []Point {
	p := make([]Point, h.count)
	for i := range p {
		p[i] = h.at(i)
	}
	return p
}
func (h *history) push(p Point) {
	if h.count < len(h.points) {
		h.points[(h.head+h.count)%len(h.points)] = p
		h.count++
		return
	}
	if len(h.points) < h.limit {
		if h.head != 0 {
			h.points = h.ordered()
			h.head = 0
		}
		h.points = append(h.points, p)
		h.count++
		return
	}
	h.points[h.head] = p
	h.head = (h.head + 1) % len(h.points)
}
func (h *history) prune(cutoff time.Time) {
	for h.count > 0 && h.at(0).At.Before(cutoff) {
		h.head = (h.head + 1) % len(h.points)
		h.count--
	}
}

// Each time bucket retains endpoints and extrema of every series, including
// missing-value transitions. Spikes and genuine gaps survive long time ranges.
// It reads through at so the ring buffer is reduced in place, without first
// being copied into order.
func reduce(n int, at func(int) Point, buckets int) []Point {
	if n <= buckets*10 {
		out := make([]Point, n)
		for i := range out {
			out[i] = at(i)
		}
		return out
	}
	out := make([]Point, 0, buckets*10)
	start := 0
	from, to := at(0).At, at(n-1).At
	span := to.Sub(from)
	if span <= 0 {
		out = make([]Point, n)
		for i := range out {
			out[i] = at(i)
		}
		return out
	}
	for bucket := 1; bucket <= buckets && start < n; bucket++ {
		end := start
		edge := from.Add(time.Duration(float64(span) * float64(bucket) / float64(buckets)))
		for end < n && !at(end).At.After(edge) {
			end++
		}
		if end == start {
			continue
		}
		indices := []int{start, end - 1}
		for metric := 0; metric < 5; metric++ {
			lo, hi := -1, -1
			wasNaN := false
			for i := start; i < end; i++ {
				v := at(i).Values[metric]
				missing := math.IsNaN(float64(v))
				if missing {
					if !wasNaN {
						indices = append(indices, i)
					}
					wasNaN = true
					continue
				}
				if wasNaN {
					indices = append(indices, i)
					wasNaN = false
				}
				if lo < 0 || v < at(lo).Values[metric] {
					lo = i
				}
				if hi < 0 || v > at(hi).Values[metric] {
					hi = i
				}
			}
			if lo >= 0 {
				indices = append(indices, lo, hi)
			}
		}
		slices.Sort(indices)
		indices = slices.Compact(indices)
		for _, i := range indices {
			out = append(out, at(i))
		}
		start = end
	}
	return out
}
