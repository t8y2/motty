package workbench

import "sync"

// asyncSleepClaim 将系统电源服务调用移出界面线程，合并各窗口的请求。
type asyncSleepClaim struct {
	mu      sync.Mutex
	windows int
	release func()
	prevent func(string) (func(), error)
	pending chan struct{}
}

func (claim *asyncSleepClaim) hold(held bool) {
	claim.mu.Lock()
	defer claim.mu.Unlock()
	if held {
		claim.windows++
	} else if claim.windows > 0 {
		claim.windows--
	}
	if claim.pending != nil || (claim.windows > 0) == (claim.release != nil) {
		return
	}
	claim.pending = make(chan struct{})
	go claim.reconcile()
}

// reconcile 在系统调用期间允许界面继续更新需求，迟到的申请会及时释放。
func (claim *asyncSleepClaim) reconcile() {
	claim.mu.Lock()
	defer func() {
		close(claim.pending)
		claim.pending = nil
		claim.mu.Unlock()
	}()
	for {
		if (claim.windows > 0) == (claim.release != nil) {
			return
		}
		if claim.release != nil {
			release := claim.release
			claim.release = nil
			claim.mu.Unlock()
			release()
			claim.mu.Lock()
			continue
		}
		claim.mu.Unlock()
		release, err := claim.prevent(transferSleepReason)
		claim.mu.Lock()
		if err != nil {
			return
		}
		claim.release = release
	}
}
