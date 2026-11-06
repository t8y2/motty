package workbench

import (
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
)

// windowsOnOneStore makes two windows of one process over one store, the way the
// application does: both read and write the same configuration file, so a save
// in one is what the other has to pick up.
type windowsOnOneStore struct {
	first, second *State
	firstDialer   *world
	secondDialer  *world
	firstWakes    atomic.Int32
	secondWakes   atomic.Int32
}

func newWindowsOnOneStore(t *testing.T) *windowsOnOneStore {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := saveState(path, savedState{Hosts: sampleHosts}); err != nil {
		t.Fatal(err)
	}
	group := NewGroup()
	pair := &windowsOnOneStore{}
	makeOne := func(id int, wakes *atomic.Int32) (*State, *world) {
		dialer, options := newWorld(t)
		options.StatePath = path
		options.KnownHostsPath = filepath.Join(dir, "known_hosts")
		options.Group = group
		// Two windows of one process, as the application makes them: each keeps
		// its own tabs under its own number.
		options.WindowID = id
		options.Invalidate = func() { wakes.Add(1) }
		return newTestState(t, options), dialer
	}
	pair.first, pair.firstDialer = makeOne(1, &pair.firstWakes)
	pair.second, pair.secondDialer = makeOne(2, &pair.secondWakes)
	return pair
}

// A setting changed in one window is what the other windows show as well, both a
// setting every computer shares and one of this device's own.
func TestASettingChangedInOneWindowReachesTheOthers(t *testing.T) {
	pair := newWindowsOnOneStore(t)
	if pair.second.notifyOff {
		t.Fatal("the second window does not start with notifications on")
	}
	if !pair.first.setNotifications(false) {
		t.Fatal("turning notifications off changed nothing")
	}
	pair.first.save()
	if !pair.second.notifyOff {
		t.Fatal("the second window still shows notifications on")
	}

	if !pair.first.chooseAppearanceMode(appearancePinnedLight) {
		t.Fatal("choosing the light look changed nothing")
	}
	pair.first.save()
	if pair.second.appearanceMode != appearancePinnedLight {
		t.Fatalf("the second window is still in look %v", pair.second.appearanceMode)
	}
}

// Reading the change back takes the settings and the shared records, not what
// the window has open of its own.
func TestAWindowKeepsItsOwnTabsWhenAnotherWindowChangesSettings(t *testing.T) {
	pair := newWindowsOnOneStore(t)
	liveTab(t, pair.second, pair.secondDialer, 0)
	opened := tabOrder(pair.second)
	if len(opened) == 0 {
		t.Fatal("the second window has no tab open")
	}

	if !pair.first.setNotifications(false) {
		t.Fatal("turning notifications off changed nothing")
	}
	pair.first.save()

	if !pair.second.notifyOff {
		t.Fatal("the second window did not read the setting back")
	}
	if got := tabOrder(pair.second); !slices.Equal(got, opened) {
		t.Fatalf("the second window's tabs are %v, were %v", got, opened)
	}
	if len(pair.second.sessions) == 0 {
		t.Fatal("the second window lost its session")
	}
}

// Opening a tab is this window's own work: the save that records it has nothing
// in it for the other windows to read. What reading the store back leaves behind
// is a replaced base, so holding the same one is how this window says it did not.
func TestATabOpenedInOneWindowLeavesTheOthersAlone(t *testing.T) {
	pair := newWindowsOnOneStore(t)
	pair.first.save()
	base := pair.second.configBase
	before := pair.first.snapshot()

	liveTab(t, pair.first, pair.firstDialer, 0)
	pair.first.save()

	if changes := changedSavedFields(before, pair.first.snapshot()); len(changes) == 0 {
		t.Fatal("opening a tab left nothing to save")
	}
	if pair.second.configBase != base {
		t.Fatal("opening a tab made the other window read the configuration back")
	}
}
