// Package linkwatch reports when the machine's network path changes.
//
// The signal is the set of addresses the interfaces carry: switching a Wi-Fi
// network, unplugging a cable and a VPN coming up each replace it, and a
// connection that was bound to the old one is dead even though neither end has
// closed it. It is polled rather than subscribed because the desktop APIs that
// push the same information need cgo, and this repository builds without it.
package linkwatch

import (
	"net"
	"slices"
	"sync"
	"time"
)

// DefaultInterval is how often the interfaces are read. Two seconds is short
// enough that a Wi-Fi switch is noticed before a user reaches for the keyboard,
// and long enough that reading them costs nothing measurable.
const DefaultInterval = 2 * time.Second

// Change is one difference in the network path.
type Change struct {
	// At is when the change was seen.
	At time.Time
	// Up reports that at least one interface still carries a routable address.
	// It is false when the machine has no network left at all.
	Up bool
	// Addrs is the set after the change, sorted, for a reader that logs it.
	Addrs []string
}

// Snapshot is what the interfaces look like at one moment.
type Snapshot struct {
	// Addrs is every address of every up, non-loopback interface, sorted.
	Addrs []string
	// Up reports that one of them is routable.
	Up bool
}

// Read reports the network path as it is now.
func Read() Snapshot {
	interfaces, err := net.Interfaces()
	if err != nil {
		return Snapshot{}
	}
	snapshot := Snapshot{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			snapshot.Addrs = append(snapshot.Addrs, address.String())
			if routable(address) {
				snapshot.Up = true
			}
		}
	}
	slices.Sort(snapshot.Addrs)
	return snapshot
}

// routable reports whether an address can carry a connection to somewhere else.
// A link-local address cannot: it is how an interface talks to its own segment,
// and it is present on the tunnel interfaces that come and go on their own.
func routable(address net.Addr) bool {
	ipNet, ok := address.(*net.IPNet)
	if !ok {
		return false
	}
	ip := ipNet.IP
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	return ip.IsGlobalUnicast()
}

// Watch calls report after every change of the network path, until the returned
// function is called. It reports nothing for the first reading: that one is the
// baseline, not a change. report runs on Watch's own goroutine and must not
// block.
func Watch(interval time.Duration, report func(Change)) func() {
	return watch(Read, interval, report)
}

// watch is Watch with the reading injected, so a test can move the network
// without touching the machine's.
func watch(read func() Snapshot, interval time.Duration, report func(Change)) func() {
	if interval <= 0 {
		interval = DefaultInterval
	}
	done := make(chan struct{})
	var once sync.Once
	// A stop function called twice, or after the watcher already ended, must
	// not close the channel again.
	stop := func() { once.Do(func() { close(done) }) }
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		previous := read()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				current := read()
				if slices.Equal(previous.Addrs, current.Addrs) {
					continue
				}
				previous = current
				report(Change{At: time.Now(), Up: current.Up, Addrs: current.Addrs})
			}
		}
	}()
	return stop
}
