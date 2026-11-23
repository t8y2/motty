package workbench

import (
	"sync/atomic"
	"testing"
	"time"
)

// 验证多个窗口共享一个请求，并在最后一个窗口停止传输时释放。
func TestRunningTransfersHoldOneSleepRequest(t *testing.T) {
	var requests, releases atomic.Int32
	claim := asyncSleepClaim{prevent: func(reason string) (func(), error) {
		if reason != transferSleepReason {
			t.Errorf("sleep reason = %q", reason)
		}
		requests.Add(1)
		return func() { releases.Add(1) }, nil
	}}
	t.Cleanup(func() {
		for {
			claim.mu.Lock()
			count := claim.windows
			claim.mu.Unlock()
			if count == 0 {
				break
			}
			claim.hold(false)
		}
		waitSleepClaim(t, &claim)
	})
	claim.hold(true)
	claim.hold(true)
	waitSleepClaim(t, &claim)
	if requests.Load() != 1 || releases.Load() != 0 {
		t.Fatal("two windows did not share one sleep request")
	}
	claim.hold(false)
	waitSleepClaim(t, &claim)
	if releases.Load() != 0 {
		t.Fatal("sleep request released while another window is transferring")
	}
	claim.hold(false)
	waitSleepClaim(t, &claim)
	if releases.Load() != 1 {
		t.Fatal("last window did not release its sleep request")
	}
}

func waitSleepClaim(t testing.TB, claim *asyncSleepClaim) {
	t.Helper()
	claim.mu.Lock()
	pending := claim.pending
	claim.mu.Unlock()
	if pending != nil {
		select {
		case <-pending:
		case <-time.After(time.Second):
			t.Fatal("sleep request did not settle")
		}
	}
}
