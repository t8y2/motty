package workbench

import (
	"runtime/debug"
	"sync"
	"time"
)

// memoryTrimDelay is how long a release waits for more to come: closing several
// tabs in a row is trimmed once, after the last of them has let go.
const memoryTrimDelay = 2 * time.Second

// memoryTrimmer hands memory back to the operating system once something big
// has been released. Go neither collects nor returns memory on its own while
// the program is idle: a closed tab's scrollback stays on the heap until the
// next collection, and the freed pages stay in the process after it, so the
// resident size never drops. One timer is armed per burst of releases; nothing
// runs while no release is pending.
type memoryTrimmer struct {
	delay time.Duration
	trim  func()

	mu      sync.Mutex
	pending bool
}

// memoryTrim is shared by every window: they share one heap.
var memoryTrim = &memoryTrimmer{delay: memoryTrimDelay, trim: debug.FreeOSMemory}

// request asks for a trim after the delay. A request while one is already
// waiting is covered by it.
func (m *memoryTrimmer) request() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending {
		return
	}
	m.pending = true
	time.AfterFunc(m.delay, m.fire)
}

func (m *memoryTrimmer) fire() {
	// Cleared first: a release that happens during the trim is not covered by it.
	m.mu.Lock()
	m.pending = false
	m.mu.Unlock()
	m.trim()
}
