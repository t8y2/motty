package workbench

import (
	"os"
	"sync"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

func sftpPathFocusDriver(t *testing.T, state *State) *app.Driver {
	t.Helper()
	var fallback app.TextInput
	driver, err := app.NewDriver(app.Config{
		Title: "SFTP path focus", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
		OnScroll: state.Scroll, OnResize: state.Resize, OnWake: state.Wake,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if context.Terminal {
				return state.TerminalInput(context)
			}
			if input := state.SFTPInput(context); input != nil {
				return input
			}
			if editor := state.EditorFor(context.ID); editor != nil {
				fallback = app.TextInput{Editor: editor}
				return &fallback
			}
			return nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	return driver
}

func clickSFTPPathFocusHit(t *testing.T, driver *app.Driver, id gfx.ID) {
	t.Helper()
	hit := hitOf(t, driver, id)
	if err := driver.Click(hit.Bounds.X+hit.Bounds.Width-3, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
}

func typeSFTPPathDraft(t *testing.T, driver *app.Driver, state *State, draft string) {
	t.Helper()
	editor := state.EditorFor(contextID(hitSFTPPath))
	if editor == nil {
		t.Fatal("path input has no native editor")
	}
	if err := driver.SelectText(0, editor.RuneCount()); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type(draft); err != nil {
		t.Fatal(err)
	}
}

func assertSFTPPathBreadcrumbs(t *testing.T, driver *app.Driver, view *sftpView) {
	t.Helper()
	if view.active().pathEditing {
		t.Fatal("temporary path editing survived focus loss")
	}
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == contextID(hitSFTPPath) {
			t.Fatal("temporary path input is still painted after focus loss")
		}
	}
	hitOf(t, driver, contextID(hitSFTPPathEdit))
	if got := view.active().pathInput().String(); got != view.active().path {
		t.Fatalf("canceled draft=%q, want current folder %q", got, view.active().path)
	}
}

func TestSFTPPathTemporaryInputRevertsWhenClickingAFile(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(map[bool]string{false: "remote", true: "local"}[local], func(t *testing.T) {
			state, session, _, remote := openTestSFTP(t)
			view := session.files
			if local {
				openLocalPane(t, state, session, localFixture(t))
			}
			driver := sftpPathFocusDriver(t, state)
			clickSFTPPathFocusHit(t, driver, contextID(hitSFTPPathEdit))
			if driver.Snapshot().Focused != contextID(hitSFTPPath) {
				t.Fatal("initial breadcrumb click did not focus the newly mounted path input")
			}
			typeSFTPPathDraft(t, driver, state, "unsubmitted-folder")
			before, reads := view.active().path, remote.readCount(view.path)
			row := contextID(hitSFTPEntryList.At(1))
			clickSFTPPathFocusHit(t, driver, row)
			assertSFTPPathBreadcrumbs(t, driver, view)
			if driver.Snapshot().Focused != row || !view.active().rows[1].selected {
				t.Fatal("canceling path editing swallowed the file selection or stole its focus")
			}
			if view.active().path != before || remote.readCount(view.path) != reads {
				t.Fatal("focus loss submitted the draft or requested a remote listing")
			}
		})
	}
}

func TestSFTPPathTemporaryInputRevertsOnKeyboardFocusTraversal(t *testing.T) {
	for _, modifiers := range []gfx.Modifiers{0, gfx.ModifierShift} {
		t.Run(map[gfx.Modifiers]string{0: "tab", gfx.ModifierShift: "shift-tab"}[modifiers], func(t *testing.T) {
			state, session, _, _ := openTestSFTP(t)
			driver := sftpPathFocusDriver(t, state)
			clickSFTPPathFocusHit(t, driver, contextID(hitSFTPPathEdit))
			typeSFTPPathDraft(t, driver, state, "unsubmitted-folder")
			if err := driver.Key(gfx.KeyTab, modifiers); err != nil {
				t.Fatal(err)
			}
			assertSFTPPathBreadcrumbs(t, driver, session.files)
			if driver.Snapshot().Focused.IsZero() || driver.Snapshot().Focused == contextID(hitSFTPPath) {
				t.Fatal("Tab traversal lost the next control's focus")
			}
		})
	}
}

func TestSFTPPathTemporaryInputRevertsWhenClickingBlankPanelSpace(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	driver := sftpPathFocusDriver(t, state)
	clickSFTPPathFocusHit(t, driver, contextID(hitSFTPPathEdit))
	typeSFTPPathDraft(t, driver, state, "unsubmitted-folder")
	bounds := state.contextRect
	if err := driver.Click(bounds.X+bounds.Width/2, bounds.Y+bounds.Height*0.7); err != nil {
		t.Fatal(err)
	}
	assertSFTPPathBreadcrumbs(t, driver, session.files)
}

func TestSFTPPathPermanentInputKeepsItsDraftAfterFocusLoss(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.sftpPathInput = true
	driver := sftpPathFocusDriver(t, state)
	clickSFTPPathFocusHit(t, driver, contextID(hitSFTPPath))
	typeSFTPPathDraft(t, driver, state, "permanent draft")
	clickSFTPPathFocusHit(t, driver, contextID(hitSFTPEntryList.At(1)))
	hitOf(t, driver, contextID(hitSFTPPath))
	if !state.sftpPathInput || session.files.pathInput().String() != "permanent draft" {
		t.Fatal("focus loss changed the permanent-input setting or its draft")
	}
}

func TestSFTPPathShortcutFocusAndInternalEditingSurviveUntilTerminalClick(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	driver := sftpPathFocusDriver(t, state)
	clickSFTPPathFocusHit(t, driver, contextID(hitSFTPEntryList.At(1)))
	shortcut := state.shortcuts.chords.Chord(shortcutFilesPath)
	if err := driver.Chord(shortcut.Modifiers, string(shortcut.Char)); err != nil {
		t.Fatal(err)
	}
	if driver.Snapshot().Focused != contextID(hitSFTPPath) {
		t.Fatal("shortcut did not focus the newly mounted path input")
	}
	typeSFTPPathDraft(t, driver, state, "still editing")
	clickSFTPPathFocusHit(t, driver, contextID(hitSFTPPath))
	if !session.files.pathEditing || state.EditorFor(contextID(hitSFTPPath)).String() != "still editing" {
		t.Fatal("clicking inside the input canceled path editing")
	}
	editor := state.EditorFor(contextID(hitSFTPPath))
	app.TextEvent{Kind: app.TextSetMarked, Text: "\u76ee\u5f55"}.Apply(editor)
	if err := driver.Resize(1400, 901); err != nil {
		t.Fatal(err)
	}
	if _, composing := editor.Composition(); !composing || !session.files.pathEditing {
		t.Fatal("repainting an IME composition canceled path editing")
	}
	clickSFTPPathFocusHit(t, driver, state.activeTerminalFocusHit())
	assertSFTPPathBreadcrumbs(t, driver, session.files)
	if driver.Snapshot().Focused != state.activeTerminalFocusHit() {
		t.Fatal("returning to breadcrumbs stole focus from the terminal")
	}
}

type pendingPathErrorRemote struct {
	*workbenchSFTPRemote
	gate <-chan struct{}
}

func (remote pendingPathErrorRemote) ReadDir(path string) ([]os.FileInfo, error) {
	if path == "/home/test/missing" {
		<-remote.gate
		return nil, os.ErrNotExist
	}
	return remote.workbenchSFTPRemote.ReadDir(path)
}

func TestSFTPPathFailedNativeNavigationKeepsTheFocusedDraft(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	view.worker.Close()
	<-view.worker.Done()
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) {
		return pendingPathErrorRemote{remote, gate}, nil
	}, nil)
	driver := sftpPathFocusDriver(t, state)
	clickSFTPPathFocusHit(t, driver, contextID(hitSFTPPathEdit))
	typeSFTPPathDraft(t, driver, state, "missing")
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	if !view.busy || !view.pathEditing || driver.Snapshot().Focused != contextID(hitSFTPPath) {
		t.Fatal("pending navigation canceled the draft or dropped its focus before the server replied")
	}
	if err := driver.Type("blocked"); err == nil || view.pathInput().String() != "missing" {
		t.Fatal("pending navigation still accepts path edits")
	}
	release()
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
	// waitSFTP applies the fake worker's result directly, without a driver wake.
	if err := driver.Resize(1400, 901); err != nil {
		t.Fatal(err)
	}
	if !view.pathEditing || view.pathInput().String() != "missing" {
		t.Fatalf("failed navigation lost its editable draft: editing=%v draft=%q", view.pathEditing, view.pathInput().String())
	}
	if err := driver.Type("-retry"); err != nil || view.pathInput().String() != "missing-retry" {
		t.Fatalf("the failed path cannot be corrected without refocusing: focus=%v draft=%q err=%v", driver.Snapshot().Focused, view.pathInput().String(), err)
	}
}

func TestSFTPPathTemporaryInputRevertsWhenSwitchingFileSides(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := openLocalPane(t, state, session, localFixture(t))
	state.Click(hitSFTPPaneToggle)
	driver := sftpPathFocusDriver(t, state)
	clickSFTPPathFocusHit(t, driver, contextID(hitSFTPPathEdit))
	typeSFTPPathDraft(t, driver, state, "remote draft")
	clickSFTPPathFocusHit(t, driver, contextID(hitSFTPPaneToggle))
	if !view.local || view.sftpFolder.pathEditing || view.lfolder.pathEditing {
		t.Fatal("switching file sides retained or transferred temporary path editing")
	}
	hitOf(t, driver, contextID(hitSFTPPathEdit))
	clickSFTPPathFocusHit(t, driver, contextID(hitSFTPPaneToggle))
	assertSFTPPathBreadcrumbs(t, driver, view)
}

type pendingPathRemote struct {
	*workbenchSFTPRemote
	gate <-chan struct{}
}

func (remote pendingPathRemote) ReadDir(directory string) ([]os.FileInfo, error) {
	if directory == "/home/test/docs" {
		<-remote.gate
	}
	return remote.workbenchSFTPRemote.ReadDir(directory)
}

// Escape cancels the navigation it started, not only the text: a late listing
// must not move the panel to a directory the user backed out of.
func TestSFTPPathEscapeAbandonsAPendingNavigation(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	view.worker.Close()
	<-view.worker.Done()
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) {
		return pendingPathRemote{remote, gate}, nil
	}, nil)
	driver := sftpPathFocusDriver(t, state)
	clickSFTPPathFocusHit(t, driver, contextID(hitSFTPPathEdit))
	typeSFTPPathDraft(t, driver, state, "docs")
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	if !view.busy || view.pathRequest != "/home/test/docs" {
		t.Fatalf("navigation was not pending: busy=%v request=%q", view.busy, view.pathRequest)
	}
	if result, handled := state.sftpPathKey(gfx.KeyEvent{Target: hitSFTPPath, Key: gfx.KeyEscape}); !handled || !result.Handled {
		t.Fatalf("Escape in the path input was not handled: %+v", result)
	}
	if view.pathRequest != "" || view.abandonedPath != "/home/test/docs" {
		t.Fatalf("Escape left the request pending: request=%q abandoned=%q", view.pathRequest, view.abandonedPath)
	}
	release()
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
	if view.path != "/home/test" {
		t.Fatalf("abandoned navigation moved the panel to %q", view.path)
	}
}
