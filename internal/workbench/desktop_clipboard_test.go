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

// TestClipboardChangeReachesEveryDesktopSession marks the clipboard changed the
// way the platform watcher does and checks the next Wake offers the new
// content to each sharing session, without any pointer moving between.
func TestClipboardChangeReachesEveryDesktopSession(t *testing.T) {
	state := newTestState(t, Options{
		ClipboardText: func() (string, error) { return "pasted text", nil },
	})
	first := &clippingDesktop{followingDesktop: newFollowingDesktop(64, 48, false)}
	second := &clippingDesktop{followingDesktop: newFollowingDesktop(64, 48, false)}
	firstSession := &Session{ID: 1, Address: "one:0"}
	secondSession := &Session{ID: 2, Address: "two:0"}
	firstSession.desk = newDesktopView(first, true)
	secondSession.desk = newDesktopView(second, true)
	state.sessions = append(state.sessions, *firstSession, *secondSession)

	// The watcher's word, from any goroutine.
	state.ClipboardChanged()

	// The next Wake drains it and offers the clipboard to every session.
	for state.clipboardDirty.Load() {
		state.Wake()
	}
	if offered := first.offered(); len(offered) != 1 || offered[0] != "pasted text" {
		t.Fatalf("first session offered %v", offered)
	}
	if offered := second.offered(); len(offered) != 1 || offered[0] != "pasted text" {
		t.Fatalf("second session offered %v", offered)
	}

	// Without new news a Wake offers nothing new: the same text is not sent
	// again.
	state.Wake()
	if offered := first.offered(); len(offered) != 1 {
		t.Fatalf("unchanged clipboard was sent again: %v", offered)
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

	state.ClipboardChanged()
	state.Wake()

	if offered := private.offered(); len(offered) != 0 {
		t.Fatalf("a host that shares nothing was offered %v", offered)
	}
}

// The fake above only implements what syncClipboard touches.
var _ desktop.Session = (*clippingDesktop)(nil)
