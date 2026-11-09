package workbench

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

// hitToolbarLatency is the dockable latency readout. It is a terminal toolbar
// entry so its position and order travel with the other tools; the value it
// shows still comes from the connection, not from the layout.
var latencyHits = tree.Feature("latency")
var hitToolbarLatency = tree.ID(latencyHits, "toolbar-latency")

const latencyProbeInterval = 3 * time.Second

// latencyProbeTimeout is how long one probe waits. The request queues behind
// the data a transfer already has in flight, so it needs more than the default
// two seconds to get a reading while an upload fills the link.
const latencyProbeTimeout = 3500 * time.Millisecond

// latencyStaleAfter is how long the last reading stays on screen without a new
// one. Probes that time out give no sample; showing the last number forever
// would claim a round trip the connection no longer has.
const latencyStaleAfter = 4 * latencyProbeInterval

// wantLatencyProbe records a session Paint saw that may need a sample and
// keeps the refresh frame coming; the request itself is sent from Wake.
func (state *State) wantLatencyProbe(session *Session, canvas *gfx.Canvas) {
	if !state.terminalShowLatency || session == nil || session.isLocal() || session.phase != sshconn.PhaseConnected {
		return
	}
	if _, ok := session.conn.(pinger); !ok {
		return
	}
	state.latencyProbe = append(state.latencyProbe, session.ID)
	// A broken request cannot be canceled; stop waking for this connection
	// instead of asking every few seconds for a sample that cannot come back.
	if canvas == nil || atomic.LoadInt32(&session.rttBroken) == 1 {
		return
	}
	// Probe on the next frame when there is no sample yet, otherwise when the
	// current one is old enough to refresh.
	delay := latencyProbeInterval - time.Since(session.rttLastPing)
	if delay < 0 || session.rttLastPing.IsZero() {
		delay = 0
	}
	canvas.RequestFrameIn(delay)
}

// probeLatency starts the requests Paint asked for. Wake runs on the window
// thread before a repaint, so the network call is not made from Paint.
func (state *State) probeLatency() {
	for _, id := range state.latencyProbe {
		if session, ok := state.sessionByIDPointer(id); ok {
			state.maybeProbeLatency(session)
		}
	}
	state.latencyProbe = state.latencyProbe[:0]
}

// maybeProbeLatency starts one asynchronous ping when the latency display is
// enabled and the previous sample is old enough. The ping runs on its own
// goroutine and publishes through the session's atomics.
func (state *State) maybeProbeLatency(session *Session) {
	if !state.terminalShowLatency || session == nil || session.isLocal() || session.phase != sshconn.PhaseConnected {
		return
	}
	pinger, ok := session.conn.(pinger)
	if !ok {
		return
	}
	// A failed request cannot be canceled: its goroutine stays inside the SSH
	// client until the reply or the connection close. Stop probing this
	// connection instead of stacking another request every few seconds; the
	// next reconnect clears the flags.
	if atomic.LoadInt32(&session.rttBroken) == 1 {
		return
	}
	now := time.Now()
	if now.Sub(session.rttLastPing) < latencyProbeInterval {
		return
	}
	if !atomic.CompareAndSwapInt32(&session.rttMeasuring, 0, 1) {
		return
	}
	session.rttLastPing = now
	generation := atomic.LoadUint32(&session.rttGeneration)
	notify := state.notifyFor(session)
	go func() {
		defer func() {
			if atomic.LoadUint32(&session.rttGeneration) == generation {
				atomic.StoreInt32(&session.rttMeasuring, 0)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), latencyProbeTimeout)
		defer cancel()
		rtt, err := pinger.Ping(ctx)
		if atomic.LoadUint32(&session.rttGeneration) != generation {
			return
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, sshconn.ErrPingPending) {
			// No sample this round. The request cannot be canceled and Ping
			// sends no second one until it is answered, so the next interval
			// probes again without piling anything up. Drop a reading that has
			// gone stale rather than keep showing it.
			if atomic.LoadInt32(&session.rttAvailable) == 1 &&
				time.Since(time.Unix(0, atomic.LoadInt64(&session.rttSampledAt))) > latencyStaleAfter {
				atomic.StoreInt32(&session.rttAvailable, 0)
				notify()
			}
			return
		}
		if err != nil {
			atomic.StoreInt32(&session.rttBroken, 1)
			atomic.StoreInt32(&session.rttAvailable, 0)
			notify()
			return
		}
		ms := rtt.Milliseconds()
		if ms < 1 && rtt > 0 {
			ms = 1
		}
		atomic.StoreInt64(&session.rttMs, ms)
		atomic.StoreInt64(&session.rttSampledAt, time.Now().UnixNano())
		atomic.StoreInt32(&session.rttAvailable, 1)
		notify()
	}()
}

// resetLatency discards the samples of the previous connection. A probe still
// in flight sees the new generation and drops its result.
func (session *Session) resetLatency() {
	atomic.AddUint32(&session.rttGeneration, 1)
	atomic.StoreInt32(&session.rttMeasuring, 0)
	atomic.StoreInt32(&session.rttBroken, 0)
	atomic.StoreInt32(&session.rttAvailable, 0)
	atomic.StoreInt64(&session.rttMs, 0)
	atomic.StoreInt64(&session.rttSampledAt, 0)
	session.rttLastPing = time.Time{}
}

// latencyInfo returns the latency text and status color (<100ms green, <200ms yellow, >=200ms red).
func (state *State) latencyInfo(session *Session) (text string, color gfx.Color, ok bool) {
	if !state.latencyAvailable(session) {
		return "", gfx.Color{}, false
	}
	ms := atomic.LoadInt64(&session.rttMs)
	text = fmt.Sprintf("RTT %dms", ms)
	return text, state.latencyColor(ms), true
}

// latencyAvailable is the allocation-free presence check used while deciding
// whether the shared side rail needs to reserve space.
func (state *State) latencyAvailable(session *Session) bool {
	return state.terminalShowLatency && session != nil && !session.isLocal() &&
		session.phase == sshconn.PhaseConnected &&
		atomic.LoadInt32(&session.rttAvailable) == 1 &&
		atomic.LoadInt64(&session.rttMs) >= 0
}

func (state *State) latencyColor(ms int64) gfx.Color {
	switch {
	case ms < 100:
		return state.pal.success
	case ms < 200:
		return state.pal.warning
	default:
		return state.pal.danger
	}
}

// declareLatencyTag renders the latency badge in the bar.
func (state *State) declareLatencyTag(f *ui.Frame, canvas *gfx.Canvas, session *Session) {
	latencyText, latencyColor, ok := state.latencyInfo(session)
	if !ok {
		return
	}
	textWidth := canvas.TextWidthWeight(latencyText, metrics.FontSizeS, gfx.WeightMedium) + 2
	f.Row().PadSides(2, 6, 2, 6).MarginSides(0, 8, 0, 0).Items(ui.Center).
		Background(withAlpha(latencyColor, 0.14)).Radius(4).Children(func() {
		f.Text(latencyText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).
			Color(latencyColor).LineHeight(16).Width(textWidth)
	})
}
