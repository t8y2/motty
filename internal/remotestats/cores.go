package remotestats

import (
	"strconv"
	"strings"
)

type coreCounters struct {
	id          string
	busy, total uint64
}

func parseCore(line []byte) (coreCounters, error) {
	p := strings.Fields(string(line))
	if len(p) != 4 || p[0] != "gop-core" {
		return coreCounters{}, ErrMalformed
	}
	if n, err := strconv.ParseUint(p[1], 10, 20); err != nil || n > 1<<19 {
		return coreCounters{}, ErrMalformed
	}
	b, e1 := strconv.ParseUint(p[2], 10, 64)
	t, e2 := strconv.ParseUint(p[3], 10, 64)
	if e1 != nil || e2 != nil || b > t {
		return coreCounters{}, ErrMalformed
	}
	return coreCounters{id: p[1], busy: b, total: t}, nil
}

func coreUsage(before, after []coreCounters) []Core {
	var result []Core
	seen := make(map[string]bool, len(after))
	for _, a := range after {
		if seen[a.id] {
			continue
		}
		seen[a.id] = true
		for _, b := range before {
			if a.id == b.id && a.total > b.total && a.busy >= b.busy {
				result = append(result, Core{ID: a.id, Percent: min(100, 100*float64(a.busy-b.busy)/float64(a.total-b.total))})
				break
			}
		}
	}
	return result
}
