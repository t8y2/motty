package linkwatch

import (
	"sync"

	"golang.org/x/sys/windows"
)

// One callback serves every watcher of the process: a callback made with
// NewCallback is never released, so it is made once. Each notification wakes
// every listener; a listener with nothing new reads the same addresses and
// reports nothing.
var notifications struct {
	once      sync.Once
	callback  uintptr
	mu        sync.Mutex
	listeners map[chan struct{}]struct{}
}

func notified(_, _, _ uintptr) uintptr {
	notifications.mu.Lock()
	for listener := range notifications.listeners {
		select {
		case listener <- struct{}{}:
		default:
		}
	}
	notifications.mu.Unlock()
	return 0
}

// subscribe asks IP Helper for changes to interfaces and to their unicast
// addresses, and signals after each, until done is closed.
func subscribe(done <-chan struct{}) (<-chan struct{}, error) {
	notifications.once.Do(func() {
		notifications.callback = windows.NewCallback(notified)
		notifications.listeners = map[chan struct{}]struct{}{}
	})
	signals := make(chan struct{}, 1)
	notifications.mu.Lock()
	notifications.listeners[signals] = struct{}{}
	notifications.mu.Unlock()
	unregister := func() {
		notifications.mu.Lock()
		delete(notifications.listeners, signals)
		notifications.mu.Unlock()
	}
	var addresses, interfaces windows.Handle
	if err := windows.NotifyUnicastIpAddressChange(windows.AF_UNSPEC, notifications.callback, nil, false, &addresses); err != nil {
		unregister()
		return nil, err
	}
	if err := windows.NotifyIpInterfaceChange(windows.AF_UNSPEC, notifications.callback, nil, false, &interfaces); err != nil {
		windows.CancelMibChangeNotify2(addresses)
		unregister()
		return nil, err
	}
	go func() {
		<-done
		// A callback still running finds the listener gone once it is
		// unregistered, under the same lock, and sends nothing.
		windows.CancelMibChangeNotify2(addresses)
		windows.CancelMibChangeNotify2(interfaces)
		unregister()
	}()
	return signals, nil
}
