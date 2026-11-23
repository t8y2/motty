package monitoring

import (
	"encoding/json"
	"math"
	"motty/internal/remotestats"
	"testing"
	"time"
)

func TestGPUHistoryFollowsHardwareAcrossReorderingMissingReadingsAndPauses(t *testing.T) {
	e := &Engine{}
	p := Panel{ID: "p", Hosts: []string{"host"}, Interval: 5, Retention: 15}
	e.Set(p, true)
	at := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	a := remotestats.GPU{UUID: "gpu-a", Name: "card-a", Index: 0, UtilPercent: 74, MemoryUsedBytes: 18 << 30, MemoryTotalBytes: 24 << 30}
	b := remotestats.GPU{UUID: "gpu-b", Name: "card-b", Index: 1, UtilPercent: 31, MemoryUsedBytes: 8 << 30, MemoryTotalBytes: 24 << 30}
	publish := func(n int, devices ...remotestats.GPU) {
		e.Publish("host", at.Add(time.Duration(n)*5*time.Second), remotestats.Sample{GPUs: devices})
	}
	publish(0, a, b)
	publish(1, b, a)
	snap, _ := e.Read("p", "host", 100, 0)
	if len(snap.GPUs) != 2 || len(snap.GPUs[0].Points) != 2 || snap.GPUs[0].Points[0].Values[0] != 74 || snap.GPUs[1].Points[1].Values[1] != 8 {
		t.Fatalf("mixed GPU history: %+v", snap.GPUs)
	}
	publish(2)
	snap, _ = e.Read("p", "host", 100, 0)
	if snap.GPUs[0].Present || !math.IsNaN(float64(snap.GPUs[0].Points[2].Values[0])) || snap.GPUs[0].Device.Name != "card-a" {
		t.Fatal("missing reading became zero or lost identity")
	}
	publish(3, a, b)
	e.Set(p, false)
	publish(4, a, b)
	e.Set(p, true)
	publish(6, a, b)
	snap, _ = e.Read("p", "host", 100, 0)
	points := snap.GPUs[0].Points
	if len(points) != 6 || !math.IsNaN(float64(points[4].Values[0])) || points[5].Values[0] != 74 {
		t.Fatalf("pause lost gap/history: %+v", points)
	}
	// Read returns independent chart slices.
	points[0].Values[0] = 999
	again, _ := e.Read("p", "host", 100, 0)
	if again.GPUs[0].Points[0].Values[0] != 74 {
		t.Fatal("reader changed retained history")
	}
}

func TestGPUHistoryRetentionAndCardLimit(t *testing.T) {
	e := &Engine{}
	p := Panel{ID: "p", Hosts: []string{"host"}, Interval: 2, Retention: 15}
	e.Set(p, true)
	at := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	e.Publish("host", at, remotestats.Sample{})
	if e.panels["p"].hosts["host"].gpus != nil {
		t.Fatal("GPU-free host allocated GPU histories")
	}
	for n := 0; n < 1100; n++ {
		e.Publish("host", at.Add(time.Duration(n+1)*2*time.Second), remotestats.Sample{GPUs: []remotestats.GPU{{UUID: "a", UtilPercent: 50, MemoryTotalBytes: 8 << 30}}})
	}
	snap, _ := e.Read("p", "host", 10000, 0)
	if len(snap.GPUs) != 1 || len(snap.GPUs[0].Points) > 451 || snap.GPUs[0].Points[0].At.Before(snap.Updated.Add(-15*time.Minute)) {
		t.Fatal("GPU retention is unbounded")
	}
	for n := 0; n < 40; n++ {
		e.Publish("host", at.Add(time.Duration(1101+n)*2*time.Second), remotestats.Sample{GPUs: []remotestats.GPU{{UUID: time.Duration(n).String(), UtilPercent: 50}}})
	}
	if len(e.panels["p"].hosts["host"].gpus) > remotestats.MaxGPUs {
		t.Fatal("device churn grew retained identities without a bound")
	}
}

func TestDisplayDefaultsAndExplicitEmptySurviveJSON(t *testing.T) {
	var p Panel
	if err := json.Unmarshal([]byte(`{"id":"p","hosts":["a","b"]}`), &p); err != nil {
		t.Fatal(err)
	}
	p.Normalize()
	if !p.Shows("a", "gpu") || !p.Shows("b", "cpu") {
		t.Fatal("old backup changed display defaults")
	}
	p.Display = map[string]Display{"a": {Hidden: []string{"cpu", "memory", "network", "disks", "gpu"}}}
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var restored Panel
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	restored.Normalize()
	if restored.Shows("a", "cpu") || restored.Shows("a", "gpu") || !restored.Shows("b", "gpu") {
		t.Fatal("explicit empty display or host isolation was lost")
	}
}

func TestDisplayCollectionDemandUnionsPanelsAndStopsEmptyHosts(t *testing.T) {
	e := &Engine{}
	a := Panel{ID: "a", Hosts: []string{"host"}, Interval: 10, Retention: 15, Display: map[string]Display{"host": {Hidden: []string{"gpu"}}}}
	b := Panel{ID: "b", Hosts: []string{"host"}, Interval: 2, Retention: 15}
	e.Set(a, true)
	e.Set(b, true)
	if e.Metrics("host") != remotestats.AllMetrics || e.Wanted()["host"] != 2 {
		t.Fatal("shared host did not combine both panels' needs")
	}
	b.Display = map[string]Display{"host": {Hidden: []string{"cpu", "memory", "network", "disks", "gpu"}}}
	e.Set(b, true)
	if e.Metrics("host")&remotestats.GPUs != 0 || e.Wanted()["host"] != 10 {
		t.Fatal("empty panel still demanded GPU probes or faster polling")
	}
	// Engine configurations own their maps: UI edits cannot change a running
	// collector's demand until Set reconciles it under the engine lock.
	a.Display["host"] = Display{Hidden: []string{"cpu", "memory", "network", "disks", "gpu"}}
	if e.Metrics("host") == 0 {
		t.Fatal("UI map edit mutated the engine's configuration")
	}
	e.Set(a, true)
	if len(e.Wanted()) != 0 || e.Metrics("host") != 0 {
		t.Fatal("fully hidden host still owns a collection stream")
	}
}

func TestDisplayEmptyPanelIgnoresSharedSourceUntilEnabled(t *testing.T) {
	e := &Engine{}
	a := Panel{ID: "a", Hosts: []string{"host"}, Interval: 5, Retention: 15}
	b := a.Clone()
	b.ID = "b"
	e.Set(a, true)
	e.Set(b, true)
	at := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	sample := remotestats.Sample{MemoryTotalBytes: 1024, MemoryUsedBytes: 512}
	e.Publish("host", at, sample)
	a.Display = map[string]Display{"host": {Hidden: []string{"cpu", "memory", "network", "disks", "gpu"}}}
	e.Set(a, true)
	stopped, _ := e.Read("a", "host", 100, 0)
	e.SetStatus("host", Failed)
	e.Details("host", remotestats.Details{}, true)
	e.Publish("host", at.Add(5*time.Second), sample)
	if _, changed := e.Read("a", "host", 100, stopped.Version); changed {
		t.Fatal("empty panel still consumed updates from another panel's source")
	}
	active, _ := e.Read("b", "host", 100, 0)
	if !active.Updated.Equal(at.Add(5*time.Second)) || len(active.Points) < 2 {
		t.Fatal("empty panel stopped another panel's shared source")
	}
	delete(a.Display, "host")
	e.Set(a, true)
	e.Publish("host", at.Add(10*time.Second), sample)
	resumed, _ := e.Read("a", "host", 100, stopped.Version)
	if len(resumed.Points) != 3 || !math.IsNaN(float64(resumed.Points[1].Values[1])) || resumed.Points[2].Values[1] != 50 {
		t.Fatal("re-enabling an empty panel lost its history or pause gap")
	}
}
