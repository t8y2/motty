package monitoring

import (
	"testing"
	"time"

	"motty/internal/remotestats"
)

// BenchmarkRetainedHistoryRead measures the snapshot a dashboard row asks for
// when the next sample lands, at the largest window a panel can keep: 24 hours
// at the fastest supported cadence.
func BenchmarkRetainedHistoryRead(b *testing.B) {
	const interval, retention = 2, 1440 // seconds, minutes
	var engine Engine
	engine.Set(Panel{ID: "p", Hosts: []string{"host"}, Interval: interval, Retention: retention}, true)
	start := time.Now()
	sample := remotestats.Sample{CPUPercent: 20, MemoryUsedBytes: 4 << 30, MemoryTotalBytes: 16 << 30}
	for i := 0; i < retention*60/interval; i++ {
		engine.Publish("host", start.Add(time.Duration(i*interval)*time.Second), sample)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := engine.Read("p", "host", 200, uint64(i)+1<<62); !ok {
			b.Fatal("the snapshot was not returned")
		}
	}
}
