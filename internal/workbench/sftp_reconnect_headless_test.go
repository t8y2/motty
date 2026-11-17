//go:build darwin || linux || windows

package workbench

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"motty/internal/sshconn"
)

func TestSFTPReconnectHeadlessRestoresBrowsedPath(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	preview, err := headless.Open(state.WindowConfig(app.Config{
		Title: "SFTP reconnect", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
		OnResize: state.Resize, OnWake: state.Wake,
	}), nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	if err := preview.DoubleClickLabel("docs"); err != nil {
		t.Fatal(err)
	}
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs" })
	next := &fakeConn{sftp: remote}
	state.options.Dial = func(sshconn.Config) Connection { return next }
	state.askSessionLink(session, false)
	if err := preview.Rest(); err != nil {
		t.Fatal(err)
	}
	dualClickID(t, preview, hitSessionLinkConfirm)
	next.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
	if session.files.path != "/home/test/docs" {
		t.Fatalf("native reconnect reset the file panel to %q", session.files.path)
	}
	next.shell(t).say("\r\nReconnected to the test fixture.\r\n$ ")
	state.Wake()
	if err := preview.Rest(); err != nil {
		t.Fatal(err)
	}
	if output := os.Getenv("MOTTY_SFTP_RECONNECT_EVIDENCE"); output != "" {
		if err := preview.Screenshot(filepath.Join(output, "847-reconnected.png")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSFTPReconnectAutomaticRetryRestoresDirectory(t *testing.T) {
	state, session, connection, remote := openTestSFTP(t)
	state.navigateSFTPDirectory(session.files, "/home/test/docs")
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
	clock := time.Second
	state.options.Now = func() time.Duration { return clock }
	state.connectionReconnect = reconnectThree
	next := &fakeConn{sftp: remote}
	state.options.Dial = func(sshconn.Config) Connection { return next }
	disconnectEstablished(connection, state)
	if !session.reconnectPending {
		t.Fatal("unexpected disconnect did not schedule an automatic retry")
	}
	clock += time.Second
	state.Wake()
	if session.conn != next {
		t.Fatal("automatic retry did not dial a new connection")
	}
	next.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
	if session.files.path != "/home/test/docs" {
		t.Fatalf("automatic reconnect reset path to %q", session.files.path)
	}
}
