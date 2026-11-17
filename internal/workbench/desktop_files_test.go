package workbench

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/desktop"
	"motty/internal/sshconn"
)

// fileDesktop is a desktop whose protocol takes files: it records what it was
// offered, and answers as its test says. While hold is set an offer waits for
// release, as the walk of a large folder would.
type fileDesktop struct {
	*followingDesktop
	mu       sync.Mutex
	texts    []string
	typed    []string
	keys     []desktop.KeyEvent
	offers   [][]string
	offer    desktop.FileOffer
	received []desktop.ReceivedFiles
	err      error
	hold     bool
	release  chan struct{}
	started  chan struct{}
}

func newFileDesktop() *fileDesktop {
	return &fileDesktop{followingDesktop: newFollowingDesktop(64, 48, false), started: make(chan struct{}, 16)}
}

func (s *fileDesktop) SetClipboardFiles(paths []string) (desktop.FileOffer, error) {
	s.mu.Lock()
	s.offers = append(s.offers, append([]string(nil), paths...))
	hold, release := s.hold, s.release
	offer, err := s.offer, s.err
	s.mu.Unlock()
	s.started <- struct{}{}
	if hold {
		<-release
	}
	return offer, err
}

func (s *fileDesktop) SetClipboard(text string) error {
	s.mu.Lock()
	s.texts = append(s.texts, text)
	s.mu.Unlock()
	return nil
}

func (s *fileDesktop) RemoteFiles() (desktop.ReceivedFiles, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.received) == 0 {
		return desktop.ReceivedFiles{}, false
	}
	files := s.received[0]
	s.received = s.received[1:]
	return files, true
}

func (s *fileDesktop) SendText(text string) error {
	s.mu.Lock()
	s.typed = append(s.typed, text)
	s.mu.Unlock()
	return nil
}

func (s *fileDesktop) SendKey(event desktop.KeyEvent) error {
	s.mu.Lock()
	s.keys = append(s.keys, event)
	s.mu.Unlock()
	return nil
}

// pasted reports whether Ctrl+V was pressed and let go of, in that order.
func (s *fileDesktop) pasted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := []desktop.KeyEvent{{Key: desktop.KeyControl, Down: true}, {Rune: 'v', Down: true}, {Rune: 'v'}, {Key: desktop.KeyControl}}
	return len(s.keys) >= len(want) && slices.Equal(s.keys[len(s.keys)-len(want):], want)
}

func (s *fileDesktop) typedTexts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.typed...)
}

func (s *fileDesktop) sentTexts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.texts...)
}

func (s *fileDesktop) offered() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]string(nil), s.offers...)
}

// fileDesktopState is a window with one desktop host open on remote, in front.
// Only the clipboard options of what it is given are used.
func fileDesktopState(t *testing.T, remote desktop.Session, clipboard bool, options Options) (*State, *Session) {
	t.Helper()
	_, base := newWorld(t)
	state := newTestState(t, base)
	state.options.Invalidate = func() {}
	if options.ClipboardFiles != nil {
		state.options.ClipboardFiles = options.ClipboardFiles
	}
	if options.ClipboardText != nil {
		state.options.ClipboardText = options.ClipboardText
	}
	if options.SetClipboardFiles != nil {
		state.options.SetClipboardFiles = options.SetClipboardFiles
	}
	state.connectionReconnect = reconnectOff
	state.dialDesktop = func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		return remote, nil
	}
	addStreamHost(state, Host{Label: "Win", Address: "win.example.com", User: "alice", Protocol: protocolRDP, Clipboard: clipboard})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "the picture", func() bool {
		return session.desk != nil && session.phase == sshconn.PhaseConnected
	})
	state.activeSection = sectionTerminal
	return state, session
}

func waitForOffer(t *testing.T, remote *fileDesktop) {
	t.Helper()
	select {
	case <-remote.started:
	case <-time.After(5 * time.Second):
		t.Fatal("no files were offered")
	}
}

func noOffer(t *testing.T, remote *fileDesktop) {
	t.Helper()
	select {
	case <-remote.started:
		t.Fatalf("files were offered: %v", remote.offered())
	case <-time.After(60 * time.Millisecond):
	}
}

func copiedFiles(t *testing.T, names ...string) []string {
	t.Helper()
	dir := t.TempDir()
	paths := make([]string, len(names))
	for index, name := range names {
		paths[index] = filepath.Join(dir, name)
		if err := os.WriteFile(paths[index], []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

// Copying files in a file manager also puts their names on the clipboard as
// text. The desktop in front is offered the files, not the names; the same files
// are not offered again by every click; and once something else is copied the
// text goes through again.
func TestCopiedFilesAreOfferedInsteadOfTheirNames(t *testing.T) {
	remote := newFileDesktop()
	remote.offer = desktop.FileOffer{Files: 2, Bytes: 2048}
	paths := copiedFiles(t, "a.txt", "b.txt")
	var clipboardFiles []string
	clipboardText := "a.txt\nb.txt"
	state, session := fileDesktopState(t, remote, true, Options{
		ClipboardFiles: func() ([]string, error) {
			if len(clipboardFiles) == 0 {
				return nil, app.ErrNoFiles
			}
			return clipboardFiles, nil
		},
		ClipboardText: func() (string, error) { return clipboardText, nil },
	})

	clipboardFiles = paths
	state.ClipboardChanged()
	state.Wake()
	waitForOffer(t, remote)
	want := state.desktopFilesOffered(remote.offer)
	waitUntil(t, state, "the offer to be reported", func() bool { return state.status == want })
	if got := remote.offered(); len(got) != 1 || strings.Join(got[0], "|") != strings.Join(paths, "|") {
		t.Fatalf("offered %v, want the copied files", got)
	}
	if texts := remote.sentTexts(); len(texts) != 0 {
		t.Fatalf("the names were sent as text: %v", texts)
	}

	// A click in the remote offers the clipboard again: the same files, not again.
	state.syncClipboard(session.desk)
	noOffer(t, remote)

	// A file edited since is another offer: the old list would be refused.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(paths[0], later, later); err != nil {
		t.Fatal(err)
	}
	state.syncClipboard(session.desk)
	waitForOffer(t, remote)
	waitUntil(t, state, "the second offer to finish", func() bool { return !session.desk.files.busy() })
	state.Wake()

	// Text copied afterwards is sent, and copying the files again offers them again.
	clipboardFiles, clipboardText = nil, "typed"
	state.syncClipboard(session.desk)
	if texts := remote.sentTexts(); len(texts) != 1 || texts[0] != "typed" {
		t.Fatalf("text after the files = %v", texts)
	}
	clipboardFiles = paths
	state.syncClipboard(session.desk)
	waitForOffer(t, remote)
}

func TestACopyThatFailsIsSilentButADropTellsWhy(t *testing.T) {
	remote := newFileDesktop()
	remote.err = desktop.ErrFilesUnsupported
	paths := copiedFiles(t, "a.txt")
	state, session := fileDesktopState(t, remote, true, Options{
		ClipboardFiles: func() ([]string, error) { return paths, nil },
	})
	state.status = "before"

	// The user only copied: no notice, and not asked again by the next click.
	state.syncClipboard(session.desk)
	waitForOffer(t, remote)
	waitUntil(t, state, "the refusal to be taken", func() bool { return len(remote.offered()) == 1 && !session.desk.files.busy() })
	state.Wake()
	if len(state.fileNotices.items) != 0 || state.status != "before" {
		t.Fatalf("a refused copy showed notices %v and status %q", state.fileNotices.items, state.status)
	}
	state.syncClipboard(session.desk)
	noOffer(t, remote)

	// A drop is the user's own act: it says what was wrong.
	x, y := dropPoint(t, state, session)
	state.FileDrop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: x, Y: y})
	result := state.FileDrop(gfx.FileDropEvent{Phase: gfx.FileDropDrop, X: x, Y: y, Paths: paths})
	if !result.Handled {
		t.Fatal("a drop on the picture was not taken")
	}
	waitForOffer(t, remote)
	waitUntil(t, state, "the notice", func() bool { return len(state.fileNotices.items) == 1 })
	if got := state.fileNotices.items[0]; got.detail != state.messages.Tools.DesktopFilesUnsupported || got.title != state.messages.Tools.DesktopFilesTitle {
		t.Fatalf("notice = %+v", got)
	}
}

func dropPoint(t *testing.T, state *State, session *Session) (x, y float32) {
	t.Helper()
	var canvas gfx.Canvas
	state.Resize(gfx.Size{Width: 1200, Height: 800})
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 800})
	_, bounds, ok := state.paneAt(600, 400)
	if !ok {
		t.Fatal("no pane under the middle of the window")
	}
	return bounds.X + bounds.Width/2, bounds.Y + bounds.Height/2
}

func TestFilesDroppedOnTheDesktopAreOfferedToTheRemoteClipboard(t *testing.T) {
	remote := newFileDesktop()
	remote.offer = desktop.FileOffer{Files: 1, Folders: 1, Bytes: 1 << 20, Skipped: 1, FirstSkipped: "/x/link"}
	state, session := fileDesktopState(t, remote, true, Options{})
	paths := copiedFiles(t, "report.pdf")
	x, y := dropPoint(t, state, session)

	over := state.FileDrop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: x, Y: y})
	if !over.Handled || state.terminalDrop.session != session.ID || !state.terminalDrop.desktop {
		t.Fatalf("hovering files over the picture: %+v, drop = %+v", over, state.terminalDrop)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 800})
	if !canvasHasText(&canvas, state.messages.Tools.DropDesktopFiles) {
		t.Fatalf("the hover did not say what a drop does: %v", canvasTexts(&canvas))
	}
	if left := state.FileDrop(gfx.FileDropEvent{Phase: gfx.FileDropLeave}); !left.Repaint || state.terminalDrop.session != 0 {
		t.Fatalf("leaving left the hover: %+v", state.terminalDrop)
	}

	state.FileDrop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: x, Y: y})
	if result := state.FileDrop(gfx.FileDropEvent{Phase: gfx.FileDropDrop, X: x, Y: y, Paths: paths}); !result.Handled {
		t.Fatalf("the drop was not taken: %+v", result)
	}
	waitForOffer(t, remote)
	if got := remote.offered(); len(got) != 1 || len(got[0]) != 1 || got[0][0] != paths[0] {
		t.Fatalf("offered %v, want %v", got, paths)
	}
	waitUntil(t, state, "the notice", func() bool { return len(state.fileNotices.items) == 1 })
	detail := state.fileNotices.items[0].detail
	if !strings.Contains(detail, "2") || !strings.Contains(detail, "/x/link") || state.terminalDrop.session != 0 {
		t.Fatalf("notice = %q, hover = %+v", detail, state.terminalDrop)
	}
}

func TestADropOnADesktopThatCannotTakeFilesSaysSo(t *testing.T) {
	state, session := fileDesktopState(t, newFollowingDesktop(64, 48, false), true, Options{}) // a picture and no file transfer, as VNC
	x, y := dropPoint(t, state, session)
	state.FileDrop(gfx.FileDropEvent{Phase: gfx.FileDropDrop, X: x, Y: y, Paths: copiedFiles(t, "a")})
	if len(state.fileNotices.items) != 1 || state.fileNotices.items[0].detail != state.messages.Tools.DesktopFilesUnsupported {
		t.Fatalf("notices = %+v", state.fileNotices.items)
	}
}

func TestADropOnAHostWithoutClipboardSharingSaysSo(t *testing.T) {
	remote := newFileDesktop()
	state, session := fileDesktopState(t, remote, false, Options{})
	x, y := dropPoint(t, state, session)
	state.FileDrop(gfx.FileDropEvent{Phase: gfx.FileDropDrop, X: x, Y: y, Paths: copiedFiles(t, "a")})
	noOffer(t, remote)
	if len(state.fileNotices.items) != 1 || state.fileNotices.items[0].detail != state.messages.Tools.DesktopFilesClipboardOff {
		t.Fatalf("notices = %+v", state.fileNotices.items)
	}
}

// While a list is being made another request waits, and only the latest of
// those waiting is made after it.
func TestOffersMadeWhileOneRunsKeepOnlyTheLatest(t *testing.T) {
	remote := newFileDesktop()
	remote.hold, remote.release = true, make(chan struct{})
	state, session := fileDesktopState(t, remote, true, Options{})
	view := session.desk
	first, second, third := copiedFiles(t, "1"), copiedFiles(t, "2"), copiedFiles(t, "3")

	state.offerDesktopFiles(view, fileOfferRequest{paths: first, key: "1"})
	waitForOffer(t, remote)
	state.offerDesktopFiles(view, fileOfferRequest{paths: second, key: "2"})
	state.offerDesktopFiles(view, fileOfferRequest{paths: third, key: "3"})
	noOffer(t, remote)
	close(remote.release)
	waitForOffer(t, remote)
	waitUntil(t, state, "both offers to finish", func() bool { return !view.files.busy() })
	got := remote.offered()
	if len(got) != 2 || got[0][0] != first[0] || got[1][0] != third[0] {
		t.Fatalf("offered %v, want the first and then the latest of the rest", got)
	}
}

// A channel that is still starting is not a verdict: the same clipboard is offered
// again by the next sync.
func TestAnOfferTheChannelWasNotReadyForIsMadeAgain(t *testing.T) {
	remote := newFileDesktop()
	remote.err = desktop.ErrClipboardNotReady
	paths := copiedFiles(t, "a")
	state, session := fileDesktopState(t, remote, true, Options{
		ClipboardFiles: func() ([]string, error) { return paths, nil },
	})
	state.syncClipboard(session.desk)
	waitForOffer(t, remote)
	waitUntil(t, state, "the result", func() bool { return !session.desk.files.busy() })
	state.Wake()
	remote.mu.Lock()
	remote.err = nil
	remote.mu.Unlock()
	state.syncClipboard(session.desk)
	waitForOffer(t, remote)
}

func TestWhyFilesCouldNotBeOffered(t *testing.T) {
	state := newTestState(t, testOptions(t))
	m := state.messages.Tools
	cases := []struct {
		err    error
		offer  desktop.FileOffer
		wants  string
		wantIn string
	}{
		{desktop.ErrFilesUnsupported, desktop.FileOffer{}, m.DesktopFilesUnsupported, ""},
		{desktop.ErrClipboardOff, desktop.FileOffer{}, m.DesktopFilesClipboardOff, ""},
		{desktop.ErrClipboardNotReady, desktop.FileOffer{}, m.DesktopFilesNotReady, ""},
		{desktop.ErrNothingToOffer, desktop.FileOffer{}, m.DesktopFilesNothing, ""},
		{desktop.ErrTooManyFiles, desktop.FileOffer{}, "", "20000"},
		{desktop.ErrPathTooLong, desktop.FileOffer{FirstSkipped: "/a/very/long/path"}, "", "/a/very/long/path"},
		{errors.New("boom"), desktop.FileOffer{}, "", "boom"},
	}
	for _, c := range cases {
		got := state.desktopFilesError(c.err, c.offer)
		if (c.wants != "" && got != c.wants) || !strings.Contains(got, c.wantIn) || got == "" {
			t.Errorf("%v: %q, want %q containing %q", c.err, got, c.wants, c.wantIn)
		}
	}
}

func pasteEvent(kind app.TextEventKind, text string) app.TextEvent {
	return app.TextEvent{Kind: kind, Text: text}
}

// A paste shortcut with files on the clipboard is the remote's own paste: the
// names a file manager also leaves there as text are not typed, and the chord is
// pressed once the files are on the remote clipboard.
func TestPastingCopiedFilesPressesPasteOnTheRemoteAfterTheOffer(t *testing.T) {
	for _, kind := range []app.TextEventKind{app.TextPaste, app.TextPasteNoText} {
		remote := newFileDesktop()
		remote.hold, remote.release = true, make(chan struct{})
		paths := copiedFiles(t, "a.txt")
		state, session := fileDesktopState(t, remote, true, Options{
			ClipboardFiles: func() ([]string, error) { return paths, nil },
			ClipboardText:  func() (string, error) { return "a.txt", nil },
		})
		state.desktopText(session.ID, pasteEvent(kind, "a.txt"))
		waitForOffer(t, remote)
		if remote.pasted() {
			t.Fatal("the remote was told to paste before the files were on its clipboard")
		}
		close(remote.release)
		waitUntil(t, state, "the paste", remote.pasted)
		if typed := remote.typedTexts(); len(typed) != 0 {
			t.Fatalf("the names were typed: %v", typed)
		}
		// Pasting again, the files already offered: pressed at once, and not offered again.
		before := len(remote.offered())
		state.desktopText(session.ID, pasteEvent(kind, "a.txt"))
		if len(remote.offered()) != before || len(remote.keys) < 8 {
			t.Fatalf("a second paste offered again (%d offers) or did not press (%d keys)", len(remote.offered()), len(remote.keys))
		}
	}
}

func TestPastingTextStillTypesIt(t *testing.T) {
	remote := newFileDesktop()
	state, session := fileDesktopState(t, remote, true, Options{
		ClipboardText: func() (string, error) { return "some text", nil },
	})
	state.desktopText(session.ID, pasteEvent(app.TextPaste, "some text"))
	if typed := remote.typedTexts(); len(typed) != 1 || typed[0] != "some text" {
		t.Fatalf("typed %v", typed)
	}
	state.desktopText(session.ID, pasteEvent(app.TextPasteNoText, ""))
	if remote.pasted() || len(remote.offered()) != 0 {
		t.Fatal("an empty clipboard pasted files")
	}
}

// Files dropped on the picture stay the remote clipboard until the local one
// changes: a click does not replace them with a clipboard copied earlier, and a
// paste shortcut pastes them.
func TestDroppedFilesStayTheRemoteClipboardUntilSomethingNewIsCopied(t *testing.T) {
	remote := newFileDesktop()
	remote.offer = desktop.FileOffer{Files: 1}
	staleFiles := copiedFiles(t, "old.txt")
	clipboardText := "an old copy"
	state, session := fileDesktopState(t, remote, true, Options{
		ClipboardFiles: func() ([]string, error) { return staleFiles, nil },
		ClipboardText:  func() (string, error) { return clipboardText, nil },
	})
	dropped := copiedFiles(t, "dropped.txt")
	x, y := dropPoint(t, state, session)
	state.FileDrop(gfx.FileDropEvent{Phase: gfx.FileDropDrop, X: x, Y: y, Paths: dropped})
	waitForOffer(t, remote)
	waitUntil(t, state, "the drop to be reported", func() bool { return session.desk.files.dropped })

	// Clicking in the remote offers nothing: what was on the clipboard before the
	// drop is not news.
	state.syncClipboard(session.desk)
	noOffer(t, remote)
	if texts := remote.sentTexts(); len(texts) != 0 {
		t.Fatalf("a click sent the old clipboard text %v over the dropped files", texts)
	}
	// A paste shortcut pastes the dropped files.
	state.desktopText(session.ID, pasteEvent(app.TextPaste, clipboardText))
	if !remote.pasted() || len(remote.typedTexts()) != 0 {
		t.Fatalf("the dropped files were not pasted (typed %v)", remote.typedTexts())
	}

	// Copying something new is what the user wants next: the text is typed, and
	// the dropped files are no longer what a paste means.
	staleFiles = nil
	clipboardText = "a new copy"
	before := len(remote.keys)
	state.desktopText(session.ID, pasteEvent(app.TextPaste, clipboardText))
	if typed := remote.typedTexts(); len(typed) != 1 || typed[0] != clipboardText || len(remote.keys) != before {
		t.Fatalf("a new copy was not pasted as text: typed %v", typed)
	}
	if session.desk.files.dropped {
		t.Fatal("the drop is still the remote clipboard after something new was copied")
	}
}

// The paste shortcut of this computer, with a desktop holding the keyboard, pastes
// files by pressing the remote's paste: even when the clipboard has no text at
// all, which the text system would swallow, and a held key does not press twice.
func TestThePasteShortcutPastesFilesOnTheRemote(t *testing.T) {
	remote := newFileDesktop()
	paths := copiedFiles(t, "a.txt")
	clipboardText := ""
	state, session := fileDesktopState(t, remote, true, Options{
		ClipboardFiles: func() ([]string, error) { return paths, nil },
		ClipboardText:  func() (string, error) { return clipboardText, nil },
	})
	paste := gfx.KeyEvent{Target: state.activeTerminalFocusHit(), Modifiers: keymap.Primary, Text: "v"}
	if result := state.Shortcut(paste); !result.Handled {
		t.Fatalf("the paste shortcut was left to the text system: %+v", result)
	}
	waitForOffer(t, remote)
	waitUntil(t, state, "the paste", remote.pasted)
	pressed := len(remote.keys)
	held := paste
	held.Repeat = true
	if result := state.Shortcut(held); result.Handled || len(remote.keys) != pressed {
		t.Fatalf("a held paste shortcut pasted again: %+v, %d keys", result, len(remote.keys))
	}
	// The repeat that is not the shortcut's falls through to the key path; the
	// press it repeats was never sent, so it must not reach the remote either.
	if result := state.Key(held); !result.Handled || len(remote.keys) != pressed {
		t.Fatalf("a repeat of a paste that never reached the remote was sent: %+v, %d keys", result, len(remote.keys))
	}

	// With nothing but text on the clipboard it is the text system's paste.
	paths, clipboardText = nil, "plain"
	session.desk.files.sentKey = ""
	if result := state.Shortcut(paste); result.Handled || len(remote.keys) != pressed {
		t.Fatalf("a paste of text was taken: %+v", result)
	}
}

func TestFilesCopiedOnTheRemoteArePutOnTheLocalClipboard(t *testing.T) {
	remote := newFileDesktop()
	root := t.TempDir()
	paths := []string{filepath.Join(root, "report.pdf"), filepath.Join(root, "folder")}
	var put [][]string
	remote.received = append(remote.received, desktop.ReceivedFiles{
		Paths: paths, Files: 1, Folders: 1, Bytes: 2048,
	})
	state, session := fileDesktopState(t, remote, true, Options{
		SetClipboardFiles: func(got []string) error {
			put = append(put, append([]string(nil), got...))
			return nil
		},
		ClipboardFiles: func() ([]string, error) { return paths, nil },
		ClipboardText:  func() (string, error) { return "report.pdf\nfolder", nil },
	})
	state.pumpDesktop(session)
	if len(put) != 1 || !slices.Equal(put[0], paths) {
		t.Fatalf("put %q, want %q", put, paths)
	}
	if !strings.Contains(state.status, "Received 2 items") {
		t.Fatalf("status = %q", state.status)
	}
	// The files stand for the local clipboard now; the next sync must not offer
	// them back to the remote as a new copy.
	state.syncClipboard(session.desk)
	if offers := remote.offered(); len(offers) != 0 {
		t.Fatalf("the received files were offered back: %q", offers)
	}
}

func TestAFailedRemoteFileCopyIsShown(t *testing.T) {
	remote := newFileDesktop()
	remote.received = append(remote.received, desktop.ReceivedFiles{Err: errors.New("read failed")})
	state, session := fileDesktopState(t, remote, true, Options{})
	state.pumpDesktop(session)
	if !strings.Contains(state.status, "read failed") {
		t.Fatalf("status = %q", state.status)
	}
}
