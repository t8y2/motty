package linkwatch

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeNetwork is a read function a test moves by hand.
type fakeNetwork struct {
	mu        sync.Mutex
	snapshots []Snapshot
	calls     int
}

func (fake *fakeNetwork) read() Snapshot {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	index := min(fake.calls, len(fake.snapshots)-1)
	fake.calls++
	return fake.snapshots[index]
}

func (fake *fakeNetwork) set(snapshot Snapshot) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.snapshots = append(fake.snapshots, snapshot)
}

const watchTick = 5 * time.Millisecond

func TestADifferentPathIsReported(t *testing.T) {
	network := &fakeNetwork{snapshots: []Snapshot{
		{Addrs: []string{"192.168.1.5/24"}, Up: true},
		{Addrs: []string{"10.0.0.7/8"}, Up: true},
	}}
	changes := make(chan Change, 4)
	stop := watch(network.read, watchTick, func(change Change) { changes <- change })
	defer stop()

	select {
	case change := <-changes:
		if !change.Up {
			t.Errorf("Up = false, want true")
		}
		if len(change.Addrs) != 1 || change.Addrs[0] != "10.0.0.7/8" {
			t.Errorf("Addrs = %v, want the new address", change.Addrs)
		}
	case <-time.After(time.Second):
		t.Fatal("no change reported")
	}
}

func TestTheFirstReadingIsTheBaseline(t *testing.T) {
	network := &fakeNetwork{snapshots: []Snapshot{{Addrs: []string{"192.168.1.5/24"}, Up: true}}}
	changes := make(chan Change, 4)
	stop := watch(network.read, watchTick, func(change Change) { changes <- change })
	defer stop()

	select {
	case change := <-changes:
		t.Fatalf("reported %v for the reading it started from", change)
	case <-time.After(20 * watchTick):
	}
}

func TestAnUnchangedPathIsNotAChange(t *testing.T) {
	network := &fakeNetwork{snapshots: []Snapshot{{Addrs: []string{"192.168.1.5/24"}, Up: true}}}
	changes := make(chan Change, 4)
	stop := watch(network.read, watchTick, func(change Change) { changes <- change })
	defer stop()

	select {
	case change := <-changes:
		t.Fatalf("reported %v for an unchanged path", change)
	case <-time.After(20 * watchTick):
	}
}

func TestALostNetworkIsReportedAsDown(t *testing.T) {
	network := &fakeNetwork{snapshots: []Snapshot{
		{Addrs: []string{"192.168.1.5/24"}, Up: true},
		{},
	}}
	changes := make(chan Change, 4)
	stop := watch(network.read, watchTick, func(change Change) { changes <- change })
	defer stop()

	select {
	case change := <-changes:
		if change.Up {
			t.Errorf("Up = true, want false when every address is gone")
		}
	case <-time.After(time.Second):
		t.Fatal("no change reported")
	}
}

func TestStoppingEndsTheWatcher(t *testing.T) {
	network := &fakeNetwork{snapshots: []Snapshot{{Addrs: []string{"192.168.1.5/24"}, Up: true}}}
	var reports atomic.Int32
	stop := watch(network.read, watchTick, func(Change) { reports.Add(1) })
	stop()
	stop() // a second call must not panic

	before := reports.Load()
	network.set(Snapshot{Addrs: []string{"10.0.0.7/8"}, Up: true})
	time.Sleep(20 * watchTick)
	if got := reports.Load(); got != before {
		t.Errorf("reports = %d after stop, want %d", got, before)
	}
}

func TestRoutable(t *testing.T) {
	cases := []struct {
		address string
		want    bool
	}{
		{"192.168.1.5/24", true},
		{"10.0.0.7/8", true},
		{"2001:db8::1/64", true},
		{"127.0.0.1/8", false},
		{"fe80::1/64", false},
		{"::1/128", false},
	}
	for _, test := range cases {
		_, ipNet, err := net.ParseCIDR(test.address)
		if err != nil {
			t.Fatalf("ParseCIDR(%q): %v", test.address, err)
		}
		if got := routable(ipNet); got != test.want {
			t.Errorf("routable(%s) = %v, want %v", test.address, got, test.want)
		}
	}
}

func TestReadReportsTheMachineAsItIs(t *testing.T) {
	// The machine's own path: nothing to assert beyond a self-consistent
	// snapshot, which is what a caller reads before any change arrives.
	snapshot := Read()
	if snapshot.Up && len(snapshot.Addrs) == 0 {
		t.Error("Up without an address to carry a connection")
	}
}
