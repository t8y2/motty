package workbench

import (
	"net/http/httptest"
	"testing"
	"time"
)

func fastSync(state *State) {
	state.syncDelay, state.syncRetryBase = 15*time.Millisecond, 15*time.Millisecond
}

func idle(state *State) bool { return !state.sync.working && state.sync.timer == nil }

// setUpAutoSync configures the service on a window that waits only a moment,
// and lets the sync that setting it up starts finish.
func setUpAutoSync(t *testing.T, state *State, server *httptest.Server, dav *fakeDAV) {
	t.Helper()
	fastSync(state)
	configureSync(t, state, server)
	waitFor(t, state, func() bool { return dav.putCount() >= 1 && idle(state) })
}

func TestAChangeIsSyncedByItselfAfterAShortWait(t *testing.T) {
	server, dav := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	setUpAutoSync(t, first, server, dav)
	base := dav.putCount()

	first.terminalScrollback = 10000
	first.save()
	if first.sync.timer == nil {
		t.Fatal("a change armed no sync")
	}
	waitFor(t, first, func() bool { return dav.putCount() == base+1 && idle(first) })
	if first.sync.err != "" || first.sync.etag == "" || first.sync.last.IsZero() {
		t.Fatalf("after the sync: %+v", first.sync)
	}
	// With nothing more to say, nothing is pending: no timer, no request.
	time.Sleep(60 * time.Millisecond)
	first.Wake()
	if !idle(first) || dav.putCount() != base+1 {
		t.Fatalf("an idle application is still at work: %+v, %d uploads", first.sync, dav.putCount())
	}
}

func TestABurstOfChangesIsSentOnce(t *testing.T) {
	server, dav := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	setUpAutoSync(t, first, server, dav)
	base := dav.putCount()
	first.syncDelay = 80 * time.Millisecond
	for _, value := range []int{1000, 5000, 10000} {
		first.terminalScrollback = value
		first.save()
	}
	waitFor(t, first, func() bool { return dav.putCount() > base && idle(first) })
	time.Sleep(150 * time.Millisecond)
	first.Wake()
	if dav.putCount() != base+1 {
		t.Fatalf("%d uploads for one burst", dav.putCount()-base)
	}
}

func TestStartingTheApplicationPicksUpWhatTheOtherComputersDid(t *testing.T) {
	server, dav := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	setUpAutoSync(t, first, server, dav)
	first.terminalScrollback = 10000
	first.save()
	waitFor(t, first, func() bool { return dav.putCount() >= 2 && idle(first) })

	second, secondOptions, _ := backupTestState(t)
	configureSync(t, second, server)
	again := newTestState(t, secondOptions) // second computer, started again
	if !again.syncPending {
		t.Fatal("a configured service does not sync at start")
	}
	waitFor(t, again, func() bool { return again.terminalScrollback == 10000 })
	if again.modal != modalNone {
		t.Fatalf("the start-up sync opened a dialog (%d)", again.modal)
	}
}

func TestSwitchingAutomaticSyncOffStopsItAndOnStartsIt(t *testing.T) {
	server, dav := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	setUpAutoSync(t, first, server, dav)
	base := dav.putCount()
	first.Click(hitSyncAuto)
	if !first.sync.manual {
		t.Fatal("the switch did nothing")
	}
	first.terminalScrollback = 10000
	first.save()
	time.Sleep(80 * time.Millisecond)
	first.Wake()
	if first.sync.timer != nil || dav.putCount() != base {
		t.Fatalf("a manual setup synced by itself: %d uploads", dav.putCount()-base)
	}
	saved, _, _ := loadState(first.options.StatePath)
	if saved.Sync == nil || !saved.Sync.Manual {
		t.Fatalf("the choice was not kept: %+v", saved.Sync)
	}

	first.Click(hitSyncAuto)
	waitFor(t, first, func() bool { return dav.putCount() == base+1 && idle(first) })
}

func TestAFailedSyncIsRetriedLaterAndASettledOneStops(t *testing.T) {
	server, dav := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	setUpAutoSync(t, first, server, dav)
	base := dav.putCount()
	dav.mu.Lock()
	dav.fail = 2
	dav.mu.Unlock()
	first.terminalScrollback = 10000
	first.save()
	waitFor(t, first, func() bool { return first.sync.err != "" })
	if first.sync.timer == nil {
		t.Fatal("a failure that could pass was not retried")
	}
	waitFor(t, first, func() bool { return dav.putCount() == base+1 && idle(first) && first.sync.err == "" })
	if first.sync.retries != 0 {
		t.Fatalf("retries not reset: %d", first.sync.retries)
	}

	// A wrong password does not get better by waiting.
	if err := first.secrets.vault.Set(backupPasswordSecret, "another password"); err != nil {
		t.Fatal(err)
	}
	first.terminalScrollback = 1000
	first.save()
	waitFor(t, first, func() bool { return first.sync.err != "" })
	time.Sleep(60 * time.Millisecond)
	first.Wake()
	if first.sync.timer != nil || first.sync.working {
		t.Fatalf("a wrong password was retried: %+v", first.sync)
	}
}

func TestALockedVaultMakesAQuietSyncWaitInsteadOfAskingForThePassword(t *testing.T) {
	server, dav := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	setUpAutoSync(t, first, server, dav)
	base := dav.putCount()
	if err := first.secrets.vault.LegacyForTests("a long master password"); err != nil {
		t.Fatal(err)
	}
	first.secrets.declined = true
	first.secrets.vault.DropKeyForTests()

	first.terminalScrollback = 10000
	first.save()
	waitFor(t, first, func() bool { return first.sync.waitingForVault })
	if first.modal != modalNone || dav.putCount() != base || first.sync.err == "" {
		t.Fatalf("modal %d, %d uploads, err %q", first.modal, dav.putCount()-base, first.sync.err)
	}
	if err := first.secrets.vault.ConvertLegacy("a long master password"); err != nil {
		t.Fatal(err)
	}
	first.scheduleSync() // what unlocking does
	waitFor(t, first, func() bool { return dav.putCount() == base+1 && idle(first) })
}

// Windows of one process share one sync: a change in any of them is sent by the
// first, and what comes in reaches all of them.
func TestWindowsShareOneSync(t *testing.T) {
	server, dav := newFakeDAV(t)
	group := NewGroup()
	first, options, _ := backupTestState(t)
	configureSync(t, first, server)
	options.Group = group
	leader := newTestState(t, options)
	follower := newTestState(t, options)
	fastSync(leader)
	fastSync(follower)
	first.Close()
	base := dav.putCount()
	if leader.syncLeader() != leader || follower.syncLeader() != leader {
		t.Fatal("the first window is not the one that syncs")
	}

	follower.terminalScrollback = 10000
	follower.save()
	if follower.sync.timer != nil || leader.sync.timer == nil {
		t.Fatalf("timers: follower %v, leader %v", follower.sync.timer != nil, leader.sync.timer != nil)
	}
	waitFor(t, leader, func() bool { return dav.putCount() == base+1 && idle(leader) })
	if !idle(follower) {
		t.Fatal("the other window synced too")
	}

	// Another computer's change reaches both windows.
	other, _, _ := backupTestState(t)
	configureSync(t, other, server)
	other.hosts = append(other.hosts, Host{ID: other.nextHostID, Label: "From elsewhere", Address: "elsewhere.example.com", User: "me", Port: 22})
	other.nextHostID++
	other.hosts[len(other.hosts)-1].finish()
	other.save()
	other.syncNow()
	waitFor(t, other, func() bool { return other.sync.etag != "" && !other.sync.working })
	leader.syncNow()
	hasHost := func(state *State) bool {
		for _, host := range state.hosts {
			if host.Label == "From elsewhere" {
				return true
			}
		}
		return false
	}
	waitFor(t, leader, func() bool { return !leader.sync.working && hasHost(leader) })
	if !hasHost(follower) {
		t.Fatal("the other window did not hear of what came in")
	}
}
