package llm

import (
	"fmt"
	"sync"
	"time"
)

// stallWatch fails a stream that stops delivering data. A provider can hold a
// connection open and send nothing at all, which no other timeout covers: the
// client has no overall timeout (answers are long) and the response-header
// timeout only guards the wait before the headers. Without this the caller
// waits forever, with no error to show.
type stallWatch struct {
	limit time.Duration
	close func()

	mu    sync.Mutex
	last  time.Time
	hit   bool
	stopc chan struct{}
}

// newStallWatch starts watching; closeBody is how the watcher ends the read
// that is blocked on a silent connection.
func newStallWatch(limit time.Duration, closeBody func()) *stallWatch {
	w := &stallWatch{limit: limit, close: closeBody, last: time.Now(), stopc: make(chan struct{})}
	go w.run()
	return w
}

// touch records that the stream delivered data.
func (w *stallWatch) touch() {
	w.mu.Lock()
	w.last = time.Now()
	w.mu.Unlock()
}

// stop releases the watcher. The pump calls it once, when the stream ends.
func (w *stallWatch) stop() { close(w.stopc) }

// fired reports whether the watcher ended the stream.
func (w *stallWatch) fired() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.hit
}

func (w *stallWatch) err() error {
	return fmt.Errorf("llm: the provider sent nothing for %s; the stream stalled", w.limit)
}

// run waits out the quiet period, then re-checks: a touch that landed while the
// timer ran moved the deadline and the wait starts over.
func (w *stallWatch) run() {
	for {
		w.mu.Lock()
		left := w.limit - time.Since(w.last)
		w.mu.Unlock()
		if left > 0 {
			timer := time.NewTimer(left)
			select {
			case <-timer.C:
			case <-w.stopc:
				timer.Stop()
				return
			}
		}
		w.mu.Lock()
		stalled := time.Since(w.last) >= w.limit
		if stalled {
			w.hit = true
		}
		w.mu.Unlock()
		if stalled {
			w.close()
			return
		}
	}
}
