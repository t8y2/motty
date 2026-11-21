package workbench

import (
	"bytes"
	"sync"
	"testing"

	"github.com/vivaui/viva/app"
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

// imagingDesktop records the text and the pictures it was given for the remote
// clipboard.
type imagingDesktop struct {
	*followingDesktop
	mu     sync.Mutex
	given  []string
	images [][]byte
}

func (s *imagingDesktop) SetClipboard(text string) error {
	s.mu.Lock()
	s.given = append(s.given, text)
	s.mu.Unlock()
	return nil
}

func (s *imagingDesktop) SetClipboardImage(png []byte) error {
	s.mu.Lock()
	s.images = append(s.images, append([]byte(nil), png...))
	s.mu.Unlock()
	return nil
}

func (s *imagingDesktop) offered() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.given...)
}

func (s *imagingDesktop) offeredImages() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.images...)
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

// A clipboard of a screenshot and no words is offered to the desktop in front as
// a picture, and only once: a click does not send it again, a new picture does,
// and text copied since takes its place until a picture is copied again.
func TestClipboardPictureReachesTheDesktopInFront(t *testing.T) {
	picture := redSquarePNG(t)
	other := append([]byte(nil), picture...)
	other[len(other)-1] ^= 0xFF // another copy, not another valid PNG: only the digest is compared
	var text string
	current := picture
	state, remote := pictureDesktopState(t,
		func() (string, error) { return text, nil },
		func() ([]byte, error) { return current, nil })
	front := &state.sessions[0]

	state.ClipboardChanged()
	state.Wake()
	if images := remote.offeredImages(); len(images) != 1 || !bytes.Equal(images[0], picture) {
		t.Fatalf("the desktop in front was offered %d pictures", len(images))
	}
	if offered := remote.offered(); len(offered) != 0 {
		t.Fatalf("a picture was sent as text too: %v", offered)
	}

	// The same picture is not offered again by a click.
	state.syncClipboard(front.desk)
	if images := remote.offeredImages(); len(images) != 1 {
		t.Fatalf("an unchanged picture was offered again: %d", len(images))
	}

	// A different picture is news.
	current = other
	state.syncClipboard(front.desk)
	images := remote.offeredImages()
	if len(images) != 2 || !bytes.Equal(images[1], other) {
		t.Fatalf("a new picture was not offered: %d pictures", len(images))
	}

	// Text copied since replaces the picture on the remote clipboard, so copying
	// the same picture again is offered again rather than taken as unchanged.
	text = "words"
	state.syncClipboard(front.desk)
	if offered := remote.offered(); len(offered) != 1 || offered[0] != text {
		t.Fatalf("text after the picture = %v", offered)
	}
	text = ""
	state.syncClipboard(front.desk)
	if images := remote.offeredImages(); len(images) != 3 || !bytes.Equal(images[2], other) {
		t.Fatalf("the picture copied again was not offered: %d pictures", len(images))
	}
}

// A host whose protocol carries only clipboard text (VNC) never reads the
// clipboard's picture: there is nothing to offer it to.
func TestClipboardPictureIsNotReadByAHostThatTakesOnlyText(t *testing.T) {
	reads := 0
	state := newTestState(t, Options{
		ClipboardText:     func() (string, error) { return "", nil },
		ClipboardImagePNG: func() ([]byte, error) { reads++; return nil, app.ErrNoImage },
	})
	remote := &clippingDesktop{followingDesktop: newFollowingDesktop(64, 48, false)}
	session := &Session{ID: 4, Address: "four:0"}
	session.desk = newDesktopView(remote, true)
	state.sessions = append(state.sessions, *session)
	state.activeSessionID = session.ID
	state.activeSection = sectionTerminal

	state.ClipboardChanged()
	state.Wake()

	if reads != 0 {
		t.Fatalf("the clipboard's picture was read %d times for a text-only host", reads)
	}
	if offered := remote.offered(); len(offered) != 0 {
		t.Fatalf("a text-only host was offered %v", offered)
	}
}

// pictureDesktopState is a window with one sharing desktop in front whose system
// clipboard answers from the functions the test can change.
func pictureDesktopState(t *testing.T, text func() (string, error), picture func() ([]byte, error)) (*State, *imagingDesktop) {
	t.Helper()
	state := newTestState(t, Options{
		ClipboardText:     text,
		ClipboardImagePNG: picture,
	})
	remote := &imagingDesktop{followingDesktop: newFollowingDesktop(64, 48, false)}
	session := &Session{ID: 5, Address: "five:0"}
	session.desk = newDesktopView(remote, true)
	state.sessions = append(state.sessions, *session)
	state.activeSessionID = session.ID
	state.activeSection = sectionTerminal
	return state, remote
}

// The fakes above only implement what syncClipboard touches.
var (
	_ desktop.Session        = (*clippingDesktop)(nil)
	_ desktop.ImageClipboard = (*imagingDesktop)(nil)
)
