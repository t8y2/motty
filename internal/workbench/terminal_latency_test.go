package workbench

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
)

type mockPingerConn struct {
	fakeConn
	rtt   time.Duration
	err   error
	calls int32
}

func (m *mockPingerConn) Ping(ctx context.Context) (time.Duration, error) {
	atomic.AddInt32(&m.calls, 1)
	return m.rtt, m.err
}

func TestLatencyToggleAndPersistence(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)

	// 默认状态下显示延迟应为关闭
	if state.terminalShowLatency {
		t.Fatal("expected terminalShowLatency to be false by default")
	}

	// 点击设置中的切换开关
	state.Click(hitSettingsLatencyToggle)
	if !state.terminalShowLatency {
		t.Fatal("expected terminalShowLatency to be true after click")
	}

	// 模拟重启恢复状态
	restarted := newTestState(t, options)
	if !restarted.terminalShowLatency {
		t.Fatal("expected terminalShowLatency to survive restart")
	}

	// 再次点击关闭
	restarted.Click(hitSettingsLatencyToggle)
	if restarted.terminalShowLatency {
		t.Fatal("expected terminalShowLatency to be false after second click")
	}

	// 再次重启确认关闭状态已保存
	restartedAgain := newTestState(t, options)
	if restartedAgain.terminalShowLatency {
		t.Fatal("expected terminalShowLatency false to survive restart")
	}
}

func TestLatencyColorThresholds(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.terminalShowLatency = true

	session := &Session{
		phase: sshconn.PhaseConnected,
	}

	// 1. 小于 100ms：绿色 (state.pal.success)
	atomic.StoreInt64(&session.rttMs, 50)
	atomic.StoreInt32(&session.rttAvailable, 1)
	text, color, ok := state.latencyInfo(session)
	if !ok || text != "RTT 50ms" || color != state.pal.success {
		t.Fatalf("expected RTT 50ms green (%v), got text=%q, color=%v, ok=%v", state.pal.success, text, color, ok)
	}

	atomic.StoreInt64(&session.rttMs, 99)
	text, color, ok = state.latencyInfo(session)
	if !ok || text != "RTT 99ms" || color != state.pal.success {
		t.Fatalf("expected RTT 99ms green, got text=%q, color=%v, ok=%v", text, color, ok)
	}

	// 2. 小于 200ms（>= 100ms）：黄色 (state.pal.warning)
	atomic.StoreInt64(&session.rttMs, 100)
	text, color, ok = state.latencyInfo(session)
	if !ok || text != "RTT 100ms" || color != state.pal.warning {
		t.Fatalf("expected RTT 100ms yellow (%v), got text=%q, color=%v, ok=%v", state.pal.warning, text, color, ok)
	}

	atomic.StoreInt64(&session.rttMs, 199)
	text, color, ok = state.latencyInfo(session)
	if !ok || text != "RTT 199ms" || color != state.pal.warning {
		t.Fatalf("expected RTT 199ms yellow, got text=%q, color=%v, ok=%v", text, color, ok)
	}

	// 3. 大于等于 200ms：红色 (state.pal.danger)
	atomic.StoreInt64(&session.rttMs, 200)
	text, color, ok = state.latencyInfo(session)
	if !ok || text != "RTT 200ms" || color != state.pal.danger {
		t.Fatalf("expected RTT 200ms red (%v), got text=%q, color=%v, ok=%v", state.pal.danger, text, color, ok)
	}

	atomic.StoreInt64(&session.rttMs, 350)
	text, color, ok = state.latencyInfo(session)
	if !ok || text != "RTT 350ms" || color != state.pal.danger {
		t.Fatalf("expected RTT 350ms red, got text=%q, color=%v, ok=%v", text, color, ok)
	}
}

func TestLatencyHiddenWhenDisabledOrLocal(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.terminalShowLatency = false

	session := &Session{
		phase: sshconn.PhaseConnected,
	}
	atomic.StoreInt64(&session.rttMs, 40)
	atomic.StoreInt32(&session.rttAvailable, 1)

	// 开关关闭时应该不显示
	if _, _, ok := state.latencyInfo(session); ok {
		t.Fatal("expected latency to be hidden when terminalShowLatency is false")
	}

	// 开启开关
	state.terminalShowLatency = true
	if _, _, ok := state.latencyInfo(session); !ok {
		t.Fatal("expected latency to be shown when enabled")
	}

	// 本地 shell 应该不显示
	session.HostID = localHostID
	if _, _, ok := state.latencyInfo(session); ok {
		t.Fatal("expected latency to be hidden for local sessions")
	}

	// 未就绪时不显示
	session.HostID = 100
	atomic.StoreInt32(&session.rttAvailable, 0)
	if _, _, ok := state.latencyInfo(session); ok {
		t.Fatal("expected latency to be hidden when not available")
	}
}

func TestMaybeProbeLatency(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.terminalShowLatency = true

	pingerConn := &mockPingerConn{rtt: 42 * time.Millisecond}
	session := &Session{
		ID:    1,
		conn:  pingerConn,
		phase: sshconn.PhaseConnected,
	}

	state.maybeProbeLatency(session)

	// 等待异步 goroutine 完成
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&session.rttAvailable) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if atomic.LoadInt32(&session.rttAvailable) != 1 {
		t.Fatal("expected rttAvailable to be set to 1")
	}
	if got := atomic.LoadInt64(&session.rttMs); got != 42 {
		t.Fatalf("expected rttMs to be 42, got %d", got)
	}
}

func TestLatencyProbeRateLimitsAndStopsAfterAFailure(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.terminalShowLatency = true
	conn := &mockPingerConn{rtt: 12 * time.Millisecond}
	session := &Session{ID: 1, conn: conn, phase: sshconn.PhaseConnected}

	state.maybeProbeLatency(session)
	waitForLatency(t, session, 1)
	// The next paint is inside the interval: no second request.
	state.maybeProbeLatency(session)
	if got := atomic.LoadInt32(&conn.calls); got != 1 {
		t.Fatalf("probes = %d, want 1", got)
	}

	failing := &mockPingerConn{err: errors.New("no reply")}
	fallingSession := &Session{ID: 2, conn: failing, phase: sshconn.PhaseConnected}
	state.maybeProbeLatency(fallingSession)
	waitForLatencyBroken(t, fallingSession)
	// A broken connection is not probed again until it reconnects.
	session = fallingSession
	session.resetLatency()
	state.maybeProbeLatency(session)
	waitForLatencyBroken(t, session)
	if got := atomic.LoadInt32(&failing.calls); got != 2 {
		t.Fatalf("probes after reconnect = %d, want 2", got)
	}
}

func waitForLatency(t *testing.T, session *Session, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&session.rttAvailable) == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("rttAvailable = %d, want %d", atomic.LoadInt32(&session.rttAvailable), want)
}

func waitForLatencyBroken(t *testing.T, session *Session) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&session.rttBroken) == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("latency probe did not stop after a failure")
}

func TestLatencyToggleThroughSettings(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 2400})
	var texts []string
	for _, text := range canvas.Texts() {
		texts = append(texts, text.Value)
	}
	if !contains(texts, state.messages.Tools.LatencyRow) || !hasHit(&canvas, hitSettingsLatencyToggle) {
		t.Fatalf("setting is not shown: %v", texts)
	}
	state.Click(hitSettingsLatencyToggle)
	if !state.terminalShowLatency {
		t.Fatal("the settings toggle did not turn the display on")
	}
}

// A probe that gets no reply in time is not a broken connection, but it gives no
// sample either: the reading is kept while it is recent, dropped once it is not,
// and probing resumes by itself when replies come back.
func TestLatencyProbeTimeoutKeepsRecentReadingThenDropsIt(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.terminalShowLatency = true

	conn := &mockPingerConn{rtt: 35 * time.Millisecond}
	session := &Session{ID: 1, conn: conn, phase: sshconn.PhaseConnected}
	state.maybeProbeLatency(session)
	waitForLatency(t, session, 1)

	probeAgain := func() {
		t.Helper()
		session.rttLastPing = time.Now().Add(-2 * latencyProbeInterval)
		state.maybeProbeLatency(session)
		deadline := time.Now().Add(2 * time.Second)
		for atomic.LoadInt32(&session.rttMeasuring) != 0 {
			if time.Now().After(deadline) {
				t.Fatal("probe never finished")
			}
			time.Sleep(time.Millisecond)
		}
	}

	for _, timeout := range []error{context.DeadlineExceeded, sshconn.ErrPingPending} {
		conn.err = timeout
		probeAgain()
		if atomic.LoadInt32(&session.rttBroken) != 0 {
			t.Fatalf("%v marked probing as broken", timeout)
		}
		if atomic.LoadInt32(&session.rttAvailable) != 1 {
			t.Fatalf("%v dropped a reading that is still recent", timeout)
		}
	}

	atomic.StoreInt64(&session.rttSampledAt, time.Now().Add(-2*latencyStaleAfter).UnixNano())
	probeAgain()
	if atomic.LoadInt32(&session.rttAvailable) != 0 {
		t.Fatal("a reading that stopped being refreshed is still shown")
	}

	conn.err, conn.rtt = nil, 48*time.Millisecond
	probeAgain()
	if got := atomic.LoadInt64(&session.rttMs); got != 48 || atomic.LoadInt32(&session.rttAvailable) != 1 {
		t.Fatalf("after recovery rttMs = %d available = %d, want 48 and 1", got, atomic.LoadInt32(&session.rttAvailable))
	}
}
