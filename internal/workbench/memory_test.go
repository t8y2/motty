package workbench

import (
	"runtime"
	"strings"
	"testing"
	"time"
	"weak"

	"motty/internal/sftpclient"
)

// collected reports whether ref became unreachable. Collection needs more than
// one cycle when a value was still on a stack in an earlier cycle.
func collected[T any](ref weak.Pointer[T]) bool {
	for range 5 {
		runtime.GC()
		if ref.Value() == nil {
			return true
		}
	}
	return false
}

// Closing a tab releases its terminal, scrollback and file panel. The session
// used to stay referenced by the slice's backing array, so closing a heavy tab
// did not give its memory back.
func TestClosingATabReleasesItsTerminal(t *testing.T) {
	state := newTestState(t, testOptions(t))
	screen := newTerminalWithScrollback(80, 24, 5000)
	screen.screen.Write([]byte(strings.Repeat("a line of output\r\n", 5000)))
	state.sessions = append(state.sessions, Session{ID: 99, Title: "closed", term: screen})
	state.activeSessionID = 99
	state.nextSessionID = 100

	ref := weak.Make(screen)
	state.closeSession(99)
	screen = nil
	if !collected(ref) {
		t.Fatal("a closed tab is still reachable through the workbench")
	}
	runtime.KeepAlive(state)
}

// A window that closed leaves the process-wide lists that name open windows:
// the group tabs move between, the status list and the assistant hub.
func TestAClosedWindowLeavesTheProcessLists(t *testing.T) {
	group := NewGroup()
	tray := NewTray(func() {})
	hub := NewAIHub("")

	state := &State{}
	group.join(state)
	tray.Add(state, nil)
	hub.Add(state, func() {})

	ref := weak.Make(state)
	group.leave(state)
	tray.Remove(state)
	hub.Remove(state)
	state = nil
	if !collected(ref) {
		t.Fatal("a closed window is still reachable from the process lists")
	}
	runtime.KeepAlive(group)
	runtime.KeepAlive(tray)
	runtime.KeepAlive(hub)
}

// Forgetting a remote folder must give its entries back, not just reset the
// accounting: the cache is bounded by bytes, and a forgotten listing that is
// still referenced is memory the budget believes it no longer holds.
func TestForgettingListingsReleasesTheirEntries(t *testing.T) {
	view := &sftpView{}
	view.remember("/one", []sftpclient.Entry{{Name: "alpha"}, {Name: "beta"}})
	if len(view.listings) != 1 {
		t.Fatalf("remembered %d listings, want 1", len(view.listings))
	}
	view.forgetListings()
	if view.listingBytes != 0 {
		t.Fatalf("listing bytes = %d after forgetting", view.listingBytes)
	}
	for index, listing := range view.listings[:cap(view.listings)] {
		if listing.entries != nil || listing.path != "" {
			t.Fatalf("forgotten listing %d is still referenced", index)
		}
	}
}

// The cache is bounded by bytes: an evicted folder must not stay reachable from
// the slice the cache keeps, or the real memory can be twice the budget.
func TestAnEvictedListingIsReleased(t *testing.T) {
	first := []sftpclient.Entry{{Name: "alpha"}, {Name: "beta"}}
	size := sftpListingSize(first)
	view := &sftpView{listingLimitBytes: size}
	// A grown cache keeps spare capacity, which is where an evicted listing used
	// to stay referenced after the slice started one element later.
	view.listings = make([]sftpListing, 0, 8)
	view.remember("/one", first)
	ref := weak.Make(&first[0])
	first = nil
	view.remember("/two", []sftpclient.Entry{{Name: "gamma"}})
	view.remember("/three", []sftpclient.Entry{{Name: "delta"}})
	if !collected(ref) {
		t.Fatal("an evicted listing is still reachable through the cache")
	}
	runtime.KeepAlive(view)
}

// Closing several tabs in a row hands memory back once, after the last of them,
// and nothing is scheduled while no tab is closing.
func TestReleasedMemoryIsTrimmedOncePerBurst(t *testing.T) {
	trimmed := make(chan struct{}, 8)
	trimmer := &memoryTrimmer{delay: 20 * time.Millisecond, trim: func() { trimmed <- struct{}{} }}
	for range 5 {
		trimmer.request()
	}
	select {
	case <-trimmed:
	case <-time.After(5 * time.Second):
		t.Fatal("released memory was never trimmed")
	}
	time.Sleep(100 * time.Millisecond)
	if len(trimmed) != 0 {
		t.Fatal("one burst of releases trimmed more than once")
	}
	trimmer.request()
	select {
	case <-trimmed:
	case <-time.After(5 * time.Second):
		t.Fatal("a later release was not trimmed")
	}
}

// Closing a tab asks for the trim: without it the closed tab's memory waits for
// the next collection and then stays in the process.
func TestClosingATabTrimsMemory(t *testing.T) {
	trimmed := make(chan struct{}, 8)
	previous := memoryTrim
	memoryTrim = &memoryTrimmer{delay: 10 * time.Millisecond, trim: func() { trimmed <- struct{}{} }}
	t.Cleanup(func() { memoryTrim = previous })

	state := newTestState(t, testOptions(t))
	state.sessions = append(state.sessions, Session{ID: 99, Title: "closed", term: newTerminalWithScrollback(80, 24, 100)})
	state.activeSessionID = 99
	state.nextSessionID = 100
	state.closeSession(99)
	select {
	case <-trimmed:
	case <-time.After(5 * time.Second):
		t.Fatal("closing a tab did not trim memory")
	}
}
