// Package linkwatch reports when the machine's network path changes.
//
// The signal is the set of addresses the interfaces carry: switching a Wi-Fi
// network, unplugging a cable and a VPN coming up each replace it, and a
// connection that was bound to the old one is dead even though neither end has
// closed it. The system says when it changes (a route socket on macOS and the
// BSDs, a netlink socket on Linux, an IP Helper notification on Windows), so a
// watcher reads the interfaces only after it was told something moved. Where
// no notification can be had, it falls back to reading them on a timer.
package linkwatch

import (
	"net"
	"slices"
	"sync"
	"time"
)

// DefaultInterval is how often the interfaces are read where the system cannot
// say when they change. Two seconds is short enough that a Wi-Fi switch is
// noticed before a user reaches for the keyboard.
const DefaultInterval = 2 * time.Second

// settle is how long a watcher waits after the system's first word of a change
// before it reads the interfaces. A switch arrives as a burst (an address goes,
// another comes), and reading after the burst reports where it ended, not each
// step on the way.
const settle = 250 * time.Millisecond

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
func Watch(report func(Change)) func() {
	done := make(chan struct{})
	signals, err := subscribe(done)
	if err != nil {
		close(done)
		return watch(Read, DefaultInterval, report)
	}
	return follow(Read, signals, settle, done, report)
}

// watch is Watch on a timer, with the reading injected, so a test can move the
// network without touching the machine's.
func watch(read func() Snapshot, interval time.Duration, report func(Change)) func() {
	if interval <= 0 {
		interval = DefaultInterval
	}
	done := make(chan struct{})
	return follow(read, tick(interval, done), 0, done, report)
}

// tick signals every interval until done is closed.
func tick(interval time.Duration, done <-chan struct{}) <-chan struct{} {
	signals := make(chan struct{}, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				select {
				case signals <- struct{}{}:
				default:
				}
			}
		}
	}()
	return signals
}

// follow reads the network after each signal and reports what changed. With
// settle, signals that arrive within it of the first are taken as one. A
// signal source that closes (its socket failed) is replaced by the timer, so a
// watcher never goes deaf.
func follow(read func() Snapshot, signals <-chan struct{}, settle time.Duration, done chan struct{}, report func(Change)) func() {
	var once sync.Once
	// A stop function called twice, or after the watcher already ended, must
	// not close the channel again.
	stop := func() { once.Do(func() { close(done) }) }
	go func() {
		previous := read()
		for {
			select {
			case <-done:
				return
			case _, open := <-signals:
				if !open {
					signals = tick(DefaultInterval, done)
					continue
				}
			}
			if settle > 0 {
				wait := time.NewTimer(settle)
			gather:
				for {
					select {
					case <-done:
						wait.Stop()
						return
					case <-wait.C:
						break gather
					case <-signals:
					}
				}
			}
			current := read()
			if slices.Equal(previous.Addrs, current.Addrs) {
				continue
			}
			previous = current
			report(Change{At: time.Now(), Up: current.Up, Addrs: current.Addrs})
		}
	}()
	return stop
}
