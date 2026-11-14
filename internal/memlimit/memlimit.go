// Package memlimit keeps the process's heap under a soft limit that follows the
// remote desktops it is showing. A desktop picture costs memory in proportion to
// its pixels (the client's framebuffer, the graphics pipeline's surfaces, the
// progressive decoder's tile state, and Viva's uploaded picture), so the limit
// is derived from the same pixels the picture is made of rather than from a
// fixed number. With no desktop shown, no limit is set.
//
// The limit is a soft one: Go's collector runs more often as the heap approaches
// it, and the process keeps working below it. It bounds the worst case that
// otherwise follows GOGC's doubling of a large live heap.
package memlimit

import (
	"math"
	"os"
	"runtime/debug"
	"strconv"
	"sync"
)

// Size is a remote desktop picture in pixels.
type Size struct{ Width, Height int }

// The estimate and its headroom. Each term is a bound the client already
// enforces, not an observation of one machine:
//
//   - 4 bytes per pixel for the client's framebuffer;
//   - 4 for one graphics-pipeline surface (a session may have more, and the
//     surface budget allows a few; one is what a desktop normally holds);
//   - 16 for the progressive decoder's coefficients and cached tile pixels
//     (12 + 4 per pixel, see internal/rdpcore/protocol/rfx);
//   - 4 for the tile buffer pool and transient copies;
//   - 4 for Viva's picture of the desktop (../viva/gfx/surface.go).
//
// 32 bytes per pixel in total, plus the fixed app baseline and the bitmap
// cache ceiling, times a 1.25 headroom for collectors and allocator slack.
const (
	appBaseBytes  = 56 << 20
	cacheBytes    = 100 << 20
	bytesPerPixel = 32
	headroom      = 1.25
	floorBytes    = 512 << 20
	ceilingBytes  = 4 << 30
)

// ForDesktops is the soft heap limit for the pictures shown, or zero when none
// are. Pictures with a non-positive side are ignored.
func ForDesktops(sizes []Size) int64 {
	var pixels int64
	for _, size := range sizes {
		if size.Width <= 0 || size.Height <= 0 {
			continue
		}
		pixels += int64(size.Width) * int64(size.Height)
	}
	if pixels == 0 {
		return 0
	}
	estimate := int64(appBaseBytes+cacheBytes) + int64(bytesPerPixel)*pixels
	limit := int64(headroom * float64(estimate))
	if limit < floorBytes {
		limit = floorBytes
	}
	if limit > ceilingBytes {
		limit = ceilingBytes
	}
	return limit
}

var (
	applyMu sync.Mutex
	applied int64 = -1 // the limit last given to the runtime, -1 before any

	overrideOnce sync.Once
	override     int64
)

// overrideLimit is a fixed limit in bytes from MOTTY_MEMORY_LIMIT_MIB, for
// measuring a build or a machine without changing the policy. Zero when unset.
func overrideLimit() int64 {
	overrideOnce.Do(func() {
		value := os.Getenv("MOTTY_MEMORY_LIMIT_MIB")
		if value == "" {
			return
		}
		mib, err := strconv.ParseInt(value, 10, 64)
		if err == nil && mib > 0 {
			override = mib << 20
		}
	})
	return override
}

// Current is the soft limit in bytes last applied, or zero when none is set.
func Current() int64 {
	applyMu.Lock()
	defer applyMu.Unlock()
	if applied <= 0 || applied == math.MaxInt64 {
		return 0
	}
	return applied
}

// Apply gives the runtime the soft limit for the pictures shown (see
// ForDesktops), or removes the limit when there are none. A fixed
// MOTTY_MEMORY_LIMIT_MIB overrides the argument. Applying the same limit twice
// does nothing.
func Apply(limit int64) {
	if fixed := overrideLimit(); fixed > 0 {
		limit = fixed
	}
	if limit <= 0 {
		limit = math.MaxInt64 // no limit; GOGC alone decides
	}
	applyMu.Lock()
	defer applyMu.Unlock()
	if applied == limit {
		return
	}
	applied = limit
	debug.SetMemoryLimit(limit)
}
