package workbench

import (
	"sync"
	"testing"

	"motty/internal/desktop"
)

// clippingDesktop records what it was given for the remote clipboard.
type clippingDesktop struct {
	*followingDesktop
	mu    sync.Mutex
	given []string
}

func (s *clippingDesktop) SetClipboard(text string) error {
	s.mu.Lock()
	s.given = append(s.given, text)
	s.mu.Unlock()
	return nil
}

func (s *clippingDesktop) offered() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.given...)
}

// clipboardStateWithTwoDesktops is a window with two sharing desktop sessions,
// the first of them in front.
func clipboardStateWithTwoDesktops(t *testing.T) (state *State, front, behind *clippingDesktop) {
	t.Helper()
	state = newTestState(t, Options{
		ClipboardText: func() (string, error) { return "pasted text", nil },
	})
	front = &clippingDesktop{followingDesktop: newFollowingDesktop(64, 48, false)}
	behind = &clippingDesktop{followingDesktop: newFollowingDesktop(64, 48, false)}
	firstSession := &Session{ID: 1, Address: "one:0"}
	secondSession := &Session{ID: 2, Address: "two:0"}
	firstSession.desk = newDesktopView(front, true)
	secondSession.desk = newDesktopView(behind, true)
	state.sessions = append(state.sessions, *firstSession, *secondSession)
	state.activeSessionID = 1
	state.activeSection = sectionTerminal
	return state, front, behind
}

// TestClipboardChangeReachesOnlyTheDesktopInFront marks the clipboard changed
// the way the platform watcher does and checks the next Wake offers the new
// content to the session the user is working in, without any pointer moving,
// and to no other: what is copied elsewhere is not handed to every host the
// window holds open.
func TestClipboardChangeReachesOnlyTheDesktopInFront(t *testing.T) {
	state, front, behind := clipboardStateWithTwoDesktops(t)

	// The watcher's word, from any goroutine.
	state.ClipboardChanged()
	state.Wake()
	if offered := front.offered(); len(offered) != 1 || offered[0] != "pasted text" {
		t.Fatalf("the session in front was offered %v", offered)
	}
	if offered := behind.offered(); len(offered) != 0 {
		t.Fatalf("a session behind was offered %v", offered)
	}

	// Without new news a Wake offers nothing new: the same text is not sent
	// again.
	state.Wake()
	if offered := front.offered(); len(offered) != 1 {
		t.Fatalf("unchanged clipboard was sent again: %v", offered)
	}
}

// TestClipboardChangeWaitsForADesktopInFront: a change made while the user is
// elsewhere, or the window is out of sight, stays pending and reaches the
// desktop once the user is at it.
func TestClipboardChangeWaitsForADesktopInFront(t *testing.T) {
	state, front, behind := clipboardStateWithTwoDesktops(t)
	shown := false
	state.windowVisible = func() bool { return shown }

	state.ClipboardChanged()
	state.Wake()
	if offered := front.offered(); len(offered) != 0 {
		t.Fatalf("a hidden window offered %v", offered)
	}

	shown = true
	state.activeSessionID = 2
	state.Wake()
	if offered := front.offered(); len(offered) != 0 {
		t.Fatalf("the session the user left was offered %v", offered)
	}
	if offered := behind.offered(); len(offered) != 1 || offered[0] != "pasted text" {
		t.Fatalf("the session the user came to was offered %v", offered)
	}
}

// TestClipboardChangeSkipsHostsWithoutSharing: a session whose host does not
// share the clipboard never sees one.
func TestClipboardChangeSkipsHostsWithoutSharing(t *testing.T) {
	state := newTestState(t, Options{
		ClipboardText: func() (string, error) { return "private", nil },
	})
	private := &clippingDesktop{followingDesktop: newFollowingDesktop(64, 48, false)}
	session := &Session{ID: 3, Address: "three:0"}
	session.desk = newDesktopView(private, false) // the host shares nothing
	state.sessions = append(state.sessions, *session)
	state.activeSessionID = session.ID
	state.activeSection = sectionTerminal

	state.ClipboardChanged()
	state.Wake()

	if offered := private.offered(); len(offered) != 0 {
		t.Fatalf("a host that shares nothing was offered %v", offered)
	}
}

// The fake above only implements what syncClipboard touches.
var _ desktop.Session = (*clippingDesktop)(nil)
