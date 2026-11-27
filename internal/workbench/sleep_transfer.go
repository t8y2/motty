package workbench

import "sync"

// asyncSleepClaim is one reason the machine has to stay awake, held by the
// windows that need it. It keeps the call into the system's power service off
// the goroutine that paints and merges the requests of every window: a service
// that does not answer must not stall a frame or a tab switch.
type asyncSleepClaim struct {
	mu      sync.Mutex
	reason  string
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

// reconcile asks for or releases the system request on its own goroutine. Calls
// that arrive while the system is answering are applied when it returns, so a
// claim that was let go meanwhile is released at once instead of being kept.
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
		release, err := claim.prevent(claim.reason)
		claim.mu.Lock()
		if err != nil {
			return
		}
		claim.release = release
	}
}
