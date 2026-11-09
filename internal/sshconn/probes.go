package sshconn

import (
	"sync"
	"time"
)

const (
	// auxiliaryProbeSettle bounds how long a connection whose transport has
	// ended waits for its optional probes to report. A refused probe and the
	// disconnect it provokes are woken by the same transport error, so a report
	// can arrive a moment after the connection's own end.
	auxiliaryProbeSettle = 250 * time.Millisecond
	// auxiliaryProbeWindow is how soon after a failed probe the transport must
	// end for the failure to explain it. A probe that failed long before an
	// ordinary network loss says nothing about that loss.
	auxiliaryProbeWindow = 3 * time.Second
)

// auxiliaryProbes tracks the optional command sessions opened beside the user's
// shell. Some gateways permit only an interactive login and answer any of them
// by ending the whole connection, so this is how such a server is recognized.
// One transport has one set, however many tabs share it: the server's answer is
// about the connection, not the tab.
type auxiliaryProbes struct {
	mu       sync.Mutex
	inFlight int
	failedAt time.Time
	changed  chan struct{}
}

func newAuxiliaryProbes() *auxiliaryProbes {
	return &auxiliaryProbes{changed: make(chan struct{}, 1)}
}

// begin records a probe that is about to be sent.
func (probes *auxiliaryProbes) begin() {
	probes.mu.Lock()
	probes.inFlight++
	probes.mu.Unlock()
}

// end records how a probe ended; a nil error finished without one.
func (probes *auxiliaryProbes) end(err error) {
	probes.mu.Lock()
	probes.inFlight--
	if err != nil {
		probes.failedAt = time.Now()
	}
	probes.mu.Unlock()
	select {
	case probes.changed <- struct{}{}:
	default:
	}
}

// settle waits, bounded, for probes that a dead transport is still waking, and
// reports whether one failed recently enough to have ended it.
func (probes *auxiliaryProbes) settle(limit time.Duration) bool {
	timer := time.NewTimer(limit)
	defer timer.Stop()
	for {
		probes.mu.Lock()
		failed, idle := probes.failedRecentlyLocked(), probes.inFlight == 0
		probes.mu.Unlock()
		if failed {
			return true
		}
		if idle {
			return false
		}
		select {
		case <-probes.changed:
		case <-timer.C:
			probes.mu.Lock()
			failed = probes.failedRecentlyLocked()
			probes.mu.Unlock()
			return failed
		}
	}
}

// failedRecentlyLocked reports whether a probe failed within the window that
// lets it explain a disconnect. The caller holds probes.mu.
func (probes *auxiliaryProbes) failedRecentlyLocked() bool {
	return !probes.failedAt.IsZero() && time.Since(probes.failedAt) <= auxiliaryProbeWindow
}

// refused reports whether a probe failed within that window. A tab whose
// transport ended by its own probes is told so, even though only the owner
// waited for the reports.
func (probes *auxiliaryProbes) refused() bool {
	probes.mu.Lock()
	defer probes.mu.Unlock()
	return probes.failedRecentlyLocked()
}

// probeState is the probe set of the transport this Conn uses: its own, or the
// owner's when the Conn shares one.
func (conn *Conn) probeState() *auxiliaryProbes {
	if conn.owner != nil {
		return conn.owner.probes
	}
	return conn.probes
}
