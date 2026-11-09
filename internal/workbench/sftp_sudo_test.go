package workbench

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

func TestSFTPModeControlInFileTab(t *testing.T) {
	state, source, window, remote, _ := openSFTPTabFixture(t)
	state.setLanguage(i18n.Chinese)
	privileged := &sudoSFTPTestConn{fakeConn: source.conn.(*fakeConn), remote: remote}
	window.session.conn = privileged
	var invalidator app.Invalidator
	preview, err := headless.Open(app.Config{
		Title: "File tab", Width: 960, Height: 650, Tree: Tree(), Invalidator: &invalidator,
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
		OnResize: state.Resize, TextInput: state.SFTPTabInput,
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	modeID := tree.MountID(sftpWindowMount(window), hitSFTPWindowSudoMode)
	checkMode := func(active bool) gfx.Rect {
		t.Helper()
		invalidator.Invalidate()
		if err := preview.Driver().Pump(); err != nil {
			t.Fatal(err)
		}
		for _, hit := range preview.Snapshot().Hits {
			if hit.ID == modeID && !hit.Block {
				if hit.Label != state.messages.SFTPModeToggle || hit.Checked != active || hit.Bounds.Width <= 0 || hit.Bounds.Width > 100 || hit.Bounds.Y >= window.ui.list[1].Y {
					t.Fatalf("mode toggle: %+v; want checked=%t beside remote title", hit, active)
				}
				return hit.Bounds
			}
		}
		t.Fatal("remote file pane has no mode control")
		return gfx.Rect{}
	}
	click := func(bounds gfx.Rect) {
		t.Helper()
		if err := preview.Click(bounds.X+bounds.Width/2, bounds.Y+bounds.Height/2); err != nil {
			t.Fatal(err)
		}
	}
	mode := checkMode(false)
	if directory := os.Getenv("MOTTY_SUDO_SCREENSHOT_DIR"); directory != "" {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := preview.Screenshot(filepath.Join(directory, "sftp-normal-mode.png")); err != nil {
			t.Fatal(err)
		}
	}
	click(mode)
	if state.modal != modalSudoSFTP || window.view.sudo {
		t.Fatal("mode control elevated without confirmation or did not open prompt")
	}
	if directory := os.Getenv("MOTTY_SUDO_SCREENSHOT_DIR"); directory != "" {
		invalidator.Invalidate()
		if err := preview.Wait(300 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err := preview.Screenshot(filepath.Join(directory, "sftp-mode-confirm.png")); err != nil {
			t.Fatal(err)
		}
	}
	state.Click(hitSudoSFTPCancel)
	if _, opens := privileged.snapshot(); opens != 0 {
		t.Fatal("canceling mode control opened privileged SFTP")
	}
	click(checkMode(false))
	state.sudoSFTP.password.Insert("secret")
	state.Click(hitSudoSFTPEnable)
	waitSFTPTab(t, state, window, func() bool { return window.view.sudo && !window.view.busy })
	checkMode(true)
	if directory := os.Getenv("MOTTY_SUDO_SCREENSHOT_DIR"); directory != "" {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := preview.Wait(300 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err := preview.Screenshot(filepath.Join(directory, "sftp-sudo-mode.png")); err != nil {
			t.Fatal(err)
		}
	}
	click(checkMode(true))
	waitSFTPTab(t, state, window, func() bool { return !window.view.sudo && !window.view.busy })
	checkMode(false)
	if _, opens := privileged.snapshot(); opens != 1 {
		t.Fatalf("unexpected privileged server opens: %d", opens)
	}
	window.session.host.Transfer = transferSCP
	invalidator.Invalidate()
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	if _, ok := hitByIDFromSnapshot(preview.Snapshot(), modeID); ok {
		t.Fatal("SCP-only server showed sudo mode control")
	}
	window.session.host.Transfer = 0
	window.session.host.System = sshconn.SystemWindows
	invalidator.Invalidate()
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	if _, ok := hitByIDFromSnapshot(preview.Snapshot(), modeID); ok {
		t.Fatal("Windows server showed sudo mode control")
	}
}

func TestSFTPModeControlInDetachedWindow(t *testing.T) {
	state, window, remote, host, _ := dualSFTPFixture(t, 1)
	privileged := &sudoSFTPTestConn{fakeConn: window.session.conn.(*fakeConn), remote: remote}
	window.session.conn = privileged
	preview := host.windows[0].view
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	clickMode := func() {
		t.Helper()
		bounds := dualHit(t, preview, hitSFTPWindowSudoMode)
		if err := preview.Click(bounds.X+bounds.Width/2, bounds.Y+bounds.Height/2); err != nil {
			t.Fatal(err)
		}
	}
	clickMode()
	if state.modal != modalSudoSFTP || state.sudoSFTP.window != window {
		t.Fatal("detached mode control did not open confirmation in parent window")
	}
	state.sudoSFTP.password.Insert("secret")
	state.Click(hitSudoSFTPEnable)
	dualWait(t, state, window, preview, func() bool { return window.view.sudo && !window.view.busy })
	clickMode()
	dualWait(t, state, window, preview, func() bool { return !window.view.sudo && !window.view.busy })
}

type sudoSFTPTestConn struct {
	*fakeConn
	mu       sync.Mutex
	remote   *workbenchSFTPRemote
	password string
	opens    int
}

func TestDocumentKeepsUnsavedEditsWhenSwitchingToPrivilegedSFTP(t *testing.T) {
	host := &sftpNativeHost{scale: 1}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() { host.Quit(); app.UseApplicationRunner(nil) })
	if err := app.RunApplication(func(application *app.Application) error {
		state, session, fake, remote := openTestSFTP(t)
		privileged := &sudoSFTPTestConn{fakeConn: fake, remote: remote}
		session.conn = privileged
		state.options.OpenContentWindow = application.OpenContentWindow
		state.openFileWindow(session.files, "/home/test/notes.txt", false)
		var document *fileDocument
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			state.Wake()
			for _, candidate := range state.fileWindows.documents {
				document = candidate
			}
			if document != nil && document.ready && !document.busy {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if document == nil || !document.ready {
			t.Fatal("document did not open")
		}
		app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextSelectAll}.Apply(&document.editor)
		document.editor.Insert("privileged edit\n")
		state.sftpMenu = sftpMenu{open: true, session: session.ID}
		state.Click(hitSFTPMenuSudo)
		if state.modal != modalSudoSFTP {
			t.Fatal("document blocked privilege confirmation while idle")
		}
		state.sudoSFTP.password.Insert("secret")
		state.Click(hitSudoSFTPEnable)
		waitSFTP(t, state, session, func(view *sftpView) bool { return view.sudo && !view.busy })
		if !document.dirty() || document.editor.String() != "privileged edit\n" {
			t.Fatal("switching privilege lost unsaved edits")
		}
		document.saveDirect(false)
		for time.Now().Before(deadline) {
			state.Wake()
			if !document.busy {
				break
			}
			time.Sleep(time.Millisecond)
		}
		password, opens := privileged.snapshot()
		remote.mu.Lock()
		content := string(remote.files["/home/test/notes.txt"])
		remote.mu.Unlock()
		if document.busy || document.problem != "" || document.dirty() || content != "privileged edit\n" || password != "secret" || opens < 2 {
			t.Fatalf("save after sudo: busy=%t problem=%q dirty=%t content=%q password=%q opens=%d", document.busy, document.problem, document.dirty(), content, password, opens)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func (conn *sudoSFTPTestConn) OpenSudoSFTP(password string) (sftpclient.Remote, error) {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	conn.password = password
	conn.opens++
	return conn.remote, nil
}

func (conn *sudoSFTPTestConn) snapshot() (string, int) {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.password, conn.opens
}

func TestSFTPPrivilegeRequiresAnExplicitSessionChoice(t *testing.T) {
	state, session, fake, remote := openTestSFTP(t)
	privileged := &sudoSFTPTestConn{fakeConn: fake, remote: remote}
	session.conn = privileged
	view := session.files
	_, opens := privileged.snapshot()
	if view.sudo || opens != 0 {
		t.Fatal("SFTP started privileged without consent")
	}
	state.sftpMenu = sftpMenu{open: true, session: session.ID}
	state.Click(hitSFTPMenuSudo)
	_, opens = privileged.snapshot()
	if state.modal != modalSudoSFTP || opens != 0 || view.sudo {
		t.Fatal("opening the prompt elevated files before confirmation")
	}
	state.Click(hitSudoSFTPCancel)
	_, opens = privileged.snapshot()
	if state.modal != modalNone || opens != 0 || view.sudo {
		t.Fatal("canceling the prompt elevated files")
	}
	state.sftpMenu = sftpMenu{open: true, session: session.ID}
	state.Click(hitSFTPMenuSudo)
	state.sudoSFTP.password.Insert("secret")
	state.Click(hitSudoSFTPEnable)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		_, opens = privileged.snapshot()
		return view.sudo && !view.busy && opens > 0
	})
	password, _ := privileged.snapshot()
	if state.modal != modalNone || password != "secret" || len(view.rows) != 2 {
		t.Fatal("privileged SFTP did not list the same files after authorization")
	}
	state.sftpMenu = sftpMenu{open: true, session: session.ID}
	state.Click(hitSFTPMenuSudo)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.sudo && !view.busy })
	_, opens = privileged.snapshot()
	if opens != 1 {
		t.Fatal("disabling sudo kept the secret or reopened the elevated connection")
	}
}

func TestDetachedSFTPPrivilegeRequiresAuthorizationAndForgetsItOnClose(t *testing.T) {
	state, source, window, remote, _ := openSFTPTabFixture(t)
	privileged := &sudoSFTPTestConn{fakeConn: source.conn.(*fakeConn), remote: remote}
	window.session.conn = privileged
	window.ui.menuOpen = true
	window.click(hitSFTPMenuSudo)
	if state.modal != modalSudoSFTP || state.sudoSFTP.window != window || window.view.sudo {
		t.Fatal("detached menu did not request authorization")
	}
	state.Click(hitSudoSFTPCancel)
	if _, opens := privileged.snapshot(); opens != 0 {
		t.Fatal("canceling detached authorization opened privileged SFTP")
	}
	window.ui.menuOpen = true
	window.click(hitSFTPMenuSudo)
	state.sudoSFTP.password.Insert("secret")
	state.Click(hitSudoSFTPEnable)
	waitSFTPTab(t, state, window, func() bool { return window.view.sudo && !window.view.busy })
	if password, opens := privileged.snapshot(); password != "secret" || opens == 0 {
		t.Fatalf("privileged detached SFTP: password=%q opens=%d", password, opens)
	}
	window.ui.menuOpen = true
	window.click(hitSFTPMenuSudo)
	waitSFTPTab(t, state, window, func() bool { return !window.view.sudo && !window.view.busy })
	window.ui.menuOpen = true
	window.click(hitSFTPMenuSudo)
	state.sudoSFTP.password.Insert("secret")
	window.onClosed()
	if state.modal != modalNone || state.sudoSFTP.window != nil || window.view.openFiles != nil {
		t.Fatal("closing detached window retained the authorization prompt or secret")
	}
}

func TestExplicitSCPHostDoesNotOfferSFTPPrivilege(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	session.host.Transfer = transferSCP
	rows := state.sftpMenuRows(session, session.files)
	for _, entry := range rows.entries[:rows.count] {
		if entry.item.ID == hitSFTPMenuSudo {
			t.Fatal("an SCP-only host offered an SFTP sudo server")
		}
	}
	state.toggleSudoSFTP(session, session.files)
	if state.modal == modalSudoSFTP || session.files.sudo {
		t.Fatal("SCP-only host accepted an SFTP privilege request")
	}
}
