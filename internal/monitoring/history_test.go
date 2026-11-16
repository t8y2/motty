package monitoring

import (
	"math"
	"sync"
	"testing"
	"time"

	"motty/internal/remotestats"
)

func reading(cpu float64) remotestats.Sample {
	return remotestats.Sample{CPUPercent: cpu, MemoryUsedBytes: 4 << 30, MemoryTotalBytes: 8 << 30, ReceiveBytesPerSecond: 4096, SendBytesPerSecond: 1024, NetworkInterface: "eth0"}
}
func panel(id string, interval int) Panel {
	return Panel{ID: id, Name: id, Hosts: []string{"host"}, Interval: interval, Retention: 15}
}
func read(t *testing.T, e *Engine, id string) Snapshot {
	t.Helper()
	s, ok := e.Read(id, "host", 1000, 0)
	if !ok {
		t.Fatal("missing snapshot")
	}
	return s
}

func TestReconcileKeepsRevisionsUntilCollectionChanges(t *testing.T) {
	var e Engine
	p := panel("p", 5)
	e.Set(p, true)
	e.Publish("host", time.Now(), reading(10))
	s, ok := e.Read("p", "host", 100, 0)
	if !ok {
		t.Fatal("missing snapshot after the first sample")
	}
	// Editing another host reconciles every panel; the same collection must
	// keep the retained history and its revision.
	e.Set(p, true)
	if _, ok := e.Read("p", "host", 100, s.Version); ok {
		t.Fatal("an unchanged reconcile invalidated the retained history")
	}
	slower := p
	slower.Interval = 10
	e.Set(slower, true)
	if _, ok := e.Read("p", "host", 100, s.Version); !ok {
		t.Fatal("a changed cadence kept the old revision")
	}
}

func TestSharedSubscriptionsKeepTheirOwnCadenceAndStopIndependently(t *testing.T) {
	var e Engine
	a, b := panel("fast", 2), panel("slow", 5)
	e.Set(a, false)
	e.Set(b, false)
	if len(e.Wanted()) != 0 {
		t.Fatal("creating a panel requested collection")
	}
	e.Set(a, true)
	e.Set(b, true)
	if e.Wanted()["host"] != 2 {
		t.Fatal("did not choose fastest requested interval")
	}
	start := time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC)
	for n := 0; n <= 10; n += 2 {
		e.Publish("host", start.Add(time.Duration(n)*time.Second), reading(float64(n)))
	}
	if got := len(read(t, &e, "fast").Points); got != 6 {
		t.Fatalf("fast points=%d", got)
	}
	if got := len(read(t, &e, "slow").Points); got != 3 {
		t.Fatalf("slow points=%d", got)
	}
	e.Set(a, false)
	before := read(t, &e, "fast")
	e.Publish("host", start.Add(12*time.Second), reading(12))
	after := read(t, &e, "fast")
	if !after.Updated.Equal(before.Updated) || len(after.Points) != len(before.Points) {
		t.Fatal("stopped history changed")
	}
	if e.Wanted()["host"] != 5 {
		t.Fatal("stopping fast panel stopped the shared source")
	}
	e.Set(a, true)
	e.Publish("host", start.Add(14*time.Second), reading(14))
	points := read(t, &e, "fast").Points
	if !math.IsNaN(float64(points[len(points)-2].Values[0])) {
		t.Fatal("resume joined disconnected observations")
	}
	e.Remove("slow")
	e.Set(a, false)
	if len(e.Wanted()) != 0 {
		t.Fatal("subscriptions leaked")
	}
}
func TestRetentionChangesDoNotInventHistoryAndKeepMemoryBounded(t *testing.T) {
	var e Engine
	p := panel("p", 2)
	e.Set(p, true)
	start := time.Now()
	for n := 0; n <= 7200; n += 2 {
		e.Publish("host", start.Add(time.Duration(n)*time.Second), reading(24))
	}
	s := read(t, &e, "p")
	if len(s.Points) != 451 {
		t.Fatalf("15 minute window has %d points", len(s.Points))
	}
	p.Retention = 1440
	e.Set(p, true)
	if got := len(read(t, &e, "p").Points); got != 451 {
		t.Fatalf("retention increase invented history: %d", got)
	}
	p.Retention = 15
	e.Set(p, true)
	h := e.panels["p"].hosts["host"]
	if len(h.points) > h.limit {
		t.Fatal("buffer exceeds bounded capacity")
	}
}

func TestSlowingSamplingKeepsExistingHistoryWithinRetention(t *testing.T) {
	var e Engine
	p := panel("p", 2)
	e.Set(p, true)
	start := time.Now().Add(-10 * time.Minute)
	for n := 0; n <= 600; n += 2 {
		e.Publish("host", start.Add(time.Duration(n)*time.Second), reading(24))
	}
	before := read(t, &e, "p")
	p.Interval = 60
	e.Set(p, true)
	after := read(t, &e, "p")
	if len(after.Points) != len(before.Points) || !after.Points[0].At.Equal(before.Points[0].At) {
		t.Fatalf("slowing sampling discarded retained history: %d -> %d", len(before.Points), len(after.Points))
	}
}

func TestPanelNormalizationKeepsUniqueStableHostOrder(t *testing.T) {
	p := Panel{Hosts: []string{"a", "b", "a", "", "c", "b"}}
	p.Normalize()
	if len(p.Hosts) != 3 || p.Hosts[0] != "a" || p.Hosts[1] != "b" || p.Hosts[2] != "c" {
		t.Fatalf("invalid normalized order: %v", p.Hosts)
	}
}
func TestMissingMetricsAndChangedInterfacesRemainGaps(t *testing.T) {
	var e Engine
	e.Set(panel("p", 2), true)
	at := time.Now()
	e.Publish("host", at, reading(1))
	s := reading(2)
	s.NetworkInterface = "eth1"
	s.CPUUnavailable = true
	e.Publish("host", at.Add(2*time.Second), s)
	p := read(t, &e, "p").Points
	if !math.IsNaN(float64(p[1].Values[0])) || !math.IsNaN(float64(p[1].Values[2])) {
		t.Fatal("unknown metric or new interface was drawn as valid")
	}
}
func TestDownsamplingPreservesSpikesAndBreaks(t *testing.T) {
	start := time.Now()
	p := make([]Point, 20000)
	for i := range p {
		p[i] = Point{At: start.Add(time.Duration(i) * time.Second), Values: [5]float32{5, 10, 20, 30}}
	}
	p[1043].Values[0] = 99
	p[1100].Values = [5]float32{nan(), nan(), nan(), nan(), nan()}
	out := reduce(len(p), func(i int) Point { return p[i] }, 100)
	if len(out) > 1200 {
		t.Fatalf("did not reduce points: %d", len(out))
	}
	spike, gap := false, false
	for _, v := range out {
		spike = spike || v.Values[0] == 99
		gap = gap || math.IsNaN(float64(v.Values[0]))
	}
	if !spike || !gap {
		t.Fatal("lost spike or gap")
	}
}
func TestPublishAndReadAreSafeWithoutPainting(t *testing.T) {
	var e Engine
	e.Set(panel("p", 2), true)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			e.Publish("host", time.Unix(int64(i*2), 0), reading(float64(i%100)))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			e.Read("p", "host", 30, 0)
			e.Details("host", remotestats.Details{}, false)
		}
	}()
	wg.Wait()
	if !read(t, &e, "p").HasSample {
		t.Fatal("collection depended on painting")
	}
}

func TestCancelledCollectorsCannotPublishIntoRestartedPanels(t *testing.T) {
	var e Engine
	e.Set(panel("p", 2), true)
	old := e.BeginSource("host")
	current := e.BeginSource("host")
	at := time.Now()
	e.PublishFrom("host", old, at, reading(99))
	e.DetailsFrom("host", old, remotestats.Details{CPUCount: 99}, false)
	s := read(t, &e, "p")
	if s.HasSample || s.Details.CPUCount != 0 {
		t.Fatal("accepted a reply from a cancelled collector")
	}
	e.PublishFrom("host", current, at, reading(24))
	if !read(t, &e, "p").HasSample {
		t.Fatal("rejected current collector")
	}
}
