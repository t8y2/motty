package workbench

import (
	"bytes"
	"slices"
	"sync"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
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

// vncProtocolDesktop wraps fileDesktop while reporting the VNC protocol.
type vncProtocolDesktop struct {
	*fileDesktop
}

func (s *vncProtocolDesktop) Info() desktop.Info { return desktop.Info{Protocol: "VNC"} }

// TestRDPPasteWithClipboardSharingUsesRemoteClipboardAndReleasesMeta reproduces
// the macOS Cmd+V multiline paste bug: holding Command left desktop.KeyMeta
// down on the remote while desktopText typed characters via SendText, turning
// every Enter into Win+Enter and flattening multiline text into one line.
// With clipboard sharing enabled on RDP, TextPaste syncs the clipboard and
// presses remote Ctrl+V while cleanly releasing held Meta (masked by Control so
// the Windows Start menu / GNOME overview does not open).
func TestRDPPasteWithClipboardSharingUsesRemoteClipboardAndReleasesMeta(t *testing.T) {
	multiline := "line1\nline2\nline3"
	remote := newFileDesktop()
	state, session := fileDesktopState(t, remote, true, Options{
		ClipboardText: func() (string, error) { return multiline, nil },
	})
	hit := state.activeTerminalFocusHit()

	// User holds Command (Meta) before pressing V.
	state.Key(gfx.KeyEvent{Key: gfx.KeyMetaLeft, Modifiers: gfx.ModifierMeta, Target: hit})
	// AppKit performKeyEquivalent delivers TextPaste while Meta is held.
	state.desktopText(session.ID, pasteEvent(app.TextPaste, multiline))

	if typed := remote.typedTexts(); len(typed) != 0 {
		t.Fatalf("multiline paste typed %v instead of pasting via remote clipboard", typed)
	}
	if offered := remote.sentTexts(); len(offered) != 1 || offered[0] != multiline {
		t.Fatalf("remote clipboard offered %v, want [%q]", offered, multiline)
	}
	wantKeys := []desktop.KeyEvent{
		{Key: desktop.KeyMeta, Down: true},
		{Key: desktop.KeyControl, Down: true},
		{Key: desktop.KeyMeta, Down: false},
		{Rune: 'v', Down: true},
		{Rune: 'v', Down: false},
		{Key: desktop.KeyControl, Down: false},
	}
	if !slices.Equal(remote.keys, wantKeys) {
		t.Fatalf("remote keys = %+v, want %+v", remote.keys, wantKeys)
	}

	// Releasing the physical Command key sends no duplicate Meta up.
	state.Key(gfx.KeyEvent{Key: gfx.KeyMetaLeft, Release: true, Target: hit})
	if len(remote.keys) != len(wantKeys) {
		t.Fatalf("physical Meta release sent extra keys: %+v", remote.keys[len(wantKeys):])
	}

	// TextPasteNoText with a clipboard image offers the picture and presses Ctrl+V.
	picture := redSquarePNG(t)
	state.options.ClipboardText = func() (string, error) { return "", nil }
	state.options.ClipboardImagePNG = func() ([]byte, error) { return picture, nil }
	beforeKeys := len(remote.keys)
	state.desktopText(session.ID, pasteEvent(app.TextPasteNoText, ""))
	if images := remote.offeredImages(); len(images) != 1 || !bytes.Equal(images[0], picture) {
		t.Fatalf("offered images = %d, want 1", len(images))
	}
	if !remote.pasted() || len(remote.keys) != beforeKeys+4 {
		t.Fatalf("image paste keys = %+v, want Ctrl+V", remote.keys[beforeKeys:])
	}

	// TextPasteNoText with an empty clipboard does not press Ctrl+V.
	state.options.ClipboardImagePNG = func() ([]byte, error) { return nil, app.ErrNoImage }
	beforeKeys = len(remote.keys)
	state.desktopText(session.ID, pasteEvent(app.TextPasteNoText, ""))
	if len(remote.keys) != beforeKeys {
		t.Fatalf("empty clipboard paste pressed keys: %+v", remote.keys[beforeKeys:])
	}
}

// TestRDPPasteWithoutClipboardSharingReleasesMetaBeforeTyping guards the
// typing fallback when clipboard sharing is disabled: held Meta must still be
// released before SendText runs so Enter keys are not sent as Win+Enter.
func TestRDPPasteWithoutClipboardSharingReleasesMetaBeforeTyping(t *testing.T) {
	multiline := "alpha\nbeta"
	remote := newFileDesktop()
	state, session := fileDesktopState(t, remote, false, Options{
		ClipboardText: func() (string, error) { return multiline, nil },
	})
	hit := state.activeTerminalFocusHit()

	state.Key(gfx.KeyEvent{Key: gfx.KeyMetaLeft, Modifiers: gfx.ModifierMeta, Target: hit})
	state.desktopText(session.ID, pasteEvent(app.TextPaste, multiline))

	if typed := remote.typedTexts(); len(typed) != 1 || typed[0] != multiline {
		t.Fatalf("typed = %v, want [%q]", typed, multiline)
	}
	wantKeys := []desktop.KeyEvent{
		{Key: desktop.KeyMeta, Down: true},
		{Key: desktop.KeyControl, Down: true},
		{Key: desktop.KeyMeta, Down: false},
		{Key: desktop.KeyControl, Down: false},
	}
	if !slices.Equal(remote.keys, wantKeys) {
		t.Fatalf("remote keys = %+v, want %+v", remote.keys, wantKeys)
	}
	if len(session.desk.held) != 0 {
		t.Fatalf("held keys remained after paste: %+v", session.desk.held)
	}
	state.Key(gfx.KeyEvent{Key: gfx.KeyMetaLeft, Release: true, Target: hit})
	if len(remote.keys) != len(wantKeys) {
		t.Fatalf("physical Meta release sent extra keys: %+v", remote.keys[len(wantKeys):])
	}
}

// TestMacOSCommandCInRDPCopiesViaRemoteControlC reproduces the macOS Cmd+C
// bug: previously desktopShortcut left Cmd+C unhandled so the macOS Edit->Copy
// menu fell through to terminal copySelection (empty) and the remote desktop
// never received Ctrl+C. On macOS, Cmd+C on a focused RDP session sends remote
// Ctrl+C (cleanly releasing held Meta) and handles the shortcut so the menu
// bar does not swallow it; on Windows/Linux Win+C stays untouched, and VNC
// keeps Meta+C for macOS VNC servers.
func TestMacOSCommandCInRDPCopiesViaRemoteControlC(t *testing.T) {
	savedPrimary := primaryModifier
	primaryModifier = gfx.ModifierMeta
	t.Cleanup(func() { primaryModifier = savedPrimary })

	remote := newFileDesktop()
	state, session := fileDesktopState(t, remote, true, Options{})
	hit := state.activeTerminalFocusHit()

	// User holds Command, then presses C.
	state.Key(gfx.KeyEvent{Key: gfx.KeyMetaLeft, Modifiers: gfx.ModifierMeta, Target: hit})
	cmdC := gfx.KeyEvent{Modifiers: gfx.ModifierMeta, Text: "c", Target: hit}
	if result := state.Shortcut(cmdC); !result.Handled {
		t.Fatalf("macOS Cmd+C on RDP was not handled by desktopShortcut: %+v", result)
	}
	wantKeys := []desktop.KeyEvent{
		{Key: desktop.KeyMeta, Down: true},
		{Key: desktop.KeyControl, Down: true},
		{Key: desktop.KeyMeta, Down: false},
		{Rune: 'c', Down: true},
		{Rune: 'c', Down: false},
		{Key: desktop.KeyControl, Down: false},
	}
	if !slices.Equal(remote.keys, wantKeys) {
		t.Fatalf("remote keys = %+v, want %+v", remote.keys, wantKeys)
	}

	// Key repeat is swallowed without sending a second Ctrl+C.
	repeatCmdC := cmdC
	repeatCmdC.Repeat = true
	if result := state.Shortcut(repeatCmdC); !result.Handled || len(remote.keys) != len(wantKeys) {
		t.Fatalf("repeat Cmd+C = %+v, keys = %d", result, len(remote.keys))
	}

	// Physical Command release sends nothing extra.
	state.Key(gfx.KeyEvent{Key: gfx.KeyMetaLeft, Release: true, Target: hit})
	if len(remote.keys) != len(wantKeys) {
		t.Fatalf("physical Meta release sent extra keys: %+v", remote.keys[len(wantKeys):])
	}

	// When keyboard capture is released, Cmd+C is no longer sent to the remote.
	state.toggleCapture(session.desk)
	before := len(remote.keys)
	state.Shortcut(cmdC)
	if len(remote.keys) != before {
		t.Fatalf("released desktop still sent Ctrl+C: %+v", remote.keys[before:])
	}

	// On Windows/Linux (Control primary), Win+C is left alone for the remote.
	state.toggleCapture(session.desk)
	primaryModifier = gfx.ModifierControl
	if result := state.Shortcut(cmdC); result.Handled || len(remote.keys) != before {
		t.Fatalf("Win+C on non-macOS was intercepted: %+v, keys = %+v", result, remote.keys[before:])
	}

	// On VNC, Cmd+C stays Meta+C rather than being rewritten to Ctrl+C.
	primaryModifier = gfx.ModifierMeta
	vncRemote := &vncProtocolDesktop{fileDesktop: newFileDesktop()}
	vncState, _ := fileDesktopState(t, vncRemote, true, Options{})
	vncHit := vncState.activeTerminalFocusHit()
	if result := vncState.Shortcut(gfx.KeyEvent{Modifiers: gfx.ModifierMeta, Text: "c", Target: vncHit}); result.Handled || len(vncRemote.keys) != 0 {
		t.Fatalf("Cmd+C on VNC was intercepted: %+v, keys = %+v", result, vncRemote.keys)
	}
}

// TestMenuBarCopyAndPasteWorkOnFocusedRDPDesktop checks choosing Edit -> Copy
// and Edit -> Paste from the macOS menu bar while an RDP session holds the
// keyboard.
func TestMenuBarCopyAndPasteWorkOnFocusedRDPDesktop(t *testing.T) {
	clipboard := "first\nsecond"
	remote := newFileDesktop()
	state, _ := fileDesktopState(t, remote, true, Options{
		ClipboardText: func() (string, error) { return clipboard, nil },
	})

	// Edit -> Copy sends Ctrl+C to the remote desktop.
	state.MenuBar()[1].Items[0].OnSelect()
	wantCopy := []desktop.KeyEvent{
		{Key: desktop.KeyControl, Down: true},
		{Rune: 'c', Down: true},
		{Rune: 'c', Down: false},
		{Key: desktop.KeyControl, Down: false},
	}
	if !slices.Equal(remote.keys, wantCopy) {
		t.Fatalf("menu Copy keys = %+v, want %+v", remote.keys, wantCopy)
	}

	// Edit -> Paste syncs clipboard text and sends Ctrl+V to the remote desktop.
	state.MenuBar()[1].Items[1].OnSelect()
	if offered := remote.sentTexts(); len(offered) != 1 || offered[0] != clipboard {
		t.Fatalf("menu Paste offered %v, want [%q]", offered, clipboard)
	}
	if !remote.pasted() {
		t.Fatalf("menu Paste did not press Ctrl+V: %+v", remote.keys)
	}
}
