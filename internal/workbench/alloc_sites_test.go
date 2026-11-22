package workbench

import (
	"fmt"
	"runtime"
	"strings"
)

// allocationSites runs fn with every allocation sampled and returns where fn
// allocated: one stack per site, innermost frame first. An allocation test calls
// it once its count is wrong, so a failure on a machine nobody can reproduce on
// says what allocated instead of only how often.
func allocationSites(fn func()) string {
	previous := runtime.MemProfileRate
	runtime.MemProfileRate = 1
	defer func() { runtime.MemProfileRate = previous }()
	// A profile publishes an allocation only after the cycles that follow it.
	flush := func() {
		for range 3 {
			runtime.GC()
		}
	}
	flush()
	before := memProfileCounts()
	flush()
	fn()
	flush()
	var report strings.Builder
	for key, record := range memProfileRecords() {
		if grown := record.AllocObjects - before[key]; grown > 0 && !profilingItself(record) {
			fmt.Fprintf(&report, "%d allocation(s):\n", grown)
			frames := runtime.CallersFrames(record.Stack())
			for {
				frame, more := frames.Next()
				fmt.Fprintf(&report, "  %s %s:%d\n", frame.Function, frame.File, frame.Line)
				if !more {
					break
				}
			}
		}
	}
	return report.String()
}

func memProfileRecords() map[string]runtime.MemProfileRecord {
	n, _ := runtime.MemProfile(nil, true)
	records := make([]runtime.MemProfileRecord, n+64)
	n, _ = runtime.MemProfile(records, true)
	byStack := make(map[string]runtime.MemProfileRecord, n)
	for _, record := range records[:n] {
		byStack[fmt.Sprint(record.Stack())] = record
	}
	return byStack
}

func memProfileCounts() map[string]int64 {
	counts := map[string]int64{}
	for key, record := range memProfileRecords() {
		counts[key] = record.AllocObjects
	}
	return counts
}

// profilingItself reports a record of the helper's own bookkeeping.
func profilingItself(record runtime.MemProfileRecord) bool {
	frames := runtime.CallersFrames(record.Stack())
	for {
		frame, more := frames.Next()
		if strings.Contains(frame.Function, "memProfile") {
			return true
		}
		if !more {
			return false
		}
	}
}
