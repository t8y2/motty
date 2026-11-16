package workbench

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"motty/internal/sshconn"
)

func TestSFTPReconnectRestoresLastAcceptedDirectory(t *testing.T) {
	for _, mode := range []string{"live", "closed", "failed", "disconnected", "hidden"} {
		t.Run(mode, func(t *testing.T) {
			state, session, connection, remote := openTestSFTP(t)
			state.navigateSFTPDirectory(session.files, "/home/test/docs")
			waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs" })
			oldWorker := session.files.worker
			if mode == "hidden" {
				state.hideFilesPanel(session)
			}
			if mode == "disconnected" {
				state.disconnect(session)
			} else if mode == "closed" || mode == "failed" {
				connection.set(func(status *sshconn.Status) {
					status.Phase = sshconn.PhaseClosed
					if mode == "failed" {
						status.Phase = sshconn.PhaseFailed
					}
					status.Err = errors.New("network lost")
				})
				state.Wake()
			}
			next := &fakeConn{sftp: remote}
			state.options.Dial = func(sshconn.Config) Connection { return next }
			state.reconnect(session)
			if session.files != nil {
				t.Fatal("reconnect retained the old SFTP worker")
			}
			<-oldWorker.Done()
			next.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
			state.Wake()
			if mode == "hidden" {
				state.showFilesPanel(session)
			}
			waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
			if session.files.path != "/home/test/docs" || len(session.files.history.entries) != 2 {
				t.Fatalf("reconnect lost directory/history: path=%q history=%+v", session.files.path, session.files.history)
			}
			if session.files.worker == oldWorker || len(session.files.rows) != 1 || session.files.rows[0].entry.Name != "guide.md" {
				t.Fatal("reconnect did not fetch the restored directory with a fresh worker")
			}
		})
	}
}

// Editing a host's address while its tab is open makes the tab connect to a
// different server. Folders browsed on the old one are not the new one's to
// restore.
func TestSFTPReconnectDropsHistoryFromAnotherEndpoint(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	state.navigateSFTPDirectory(session.files, "/home/test/docs")
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs" })

	previous := session.host
	updated := previous
	updated.Address = "other.example"
	index := -1
	for candidate := range state.hosts {
		if state.hosts[candidate].ID == updated.ID {
			index = candidate
			break
		}
	}
	if index < 0 {
		t.Fatal("the session's host is not in the host list")
	}
	state.replaceHost(index, previous, updated)

	next := &fakeConn{sftp: remote}
	state.options.Dial = func(sshconn.Config) Connection { return next }
	state.reconnect(session)
	next.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
	if session.files.path != "/home/test" {
		t.Fatalf("a changed endpoint restored %q from the previous server", session.files.path)
	}
}

// A server that reports a missing folder with the SFTP v4-v6 status code gets
// the same notice as one that reports it with FX_NO_SUCH_FILE.
func TestSFTPPathNotFoundNoticeCoversCodeTen(t *testing.T) {
	state, _, _, _ := openTestSFTP(t)
	state.showSFTPPathErrorAt(nil, "/gone", &sftp.StatusError{Code: 10})
	detail := fmt.Sprintf(state.messages.Tools.FilesPathNotFoundFmt, "/gone")
	if len(state.fileNotices.items) != 1 || state.fileNotices.items[0].detail != detail {
		t.Fatalf("notice=%v want detail %q", state.fileNotices.items, detail)
	}
}

func TestSFTPReconnectFailedRetriesKeepSavedDirectory(t *testing.T) {
	state, session, connection, remote := openTestSFTP(t)
	state.navigateSFTPDirectory(session.files, "/home/test/docs")
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
	state.connectionReconnect = reconnectForever
	state.disconnect(session)
	state.closeSFTP(session)
	for range 2 {
		next := &fakeConn{sftp: remote}
		state.options.Dial = func(sshconn.Config) Connection { return next }
		state.reconnect(session)
		next.set(func(status *sshconn.Status) {
			status.Phase = sshconn.PhaseFailed
			status.Err = errors.New("dial failed")
		})
		state.Wake()
		connection = next
	}
	connection.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected; status.Err = nil })
	state.Wake()
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
	if session.files.path != "/home/test/docs" {
		t.Fatalf("failed retries erased saved path: %q", session.files.path)
	}
}

func TestSFTPReconnectInterruptedRestoreKeepsLastAcceptedPath(t *testing.T) {
	for _, hide := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "hide"}[hide], func(t *testing.T) {
			state, session, _, remote := openTestSFTP(t)
			state.navigateSFTPDirectory(session.files, "/home/test/docs")
			waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
			state.closeSFTP(session)
			remote.mu.Lock()
			remote.readDelay = 50 * time.Millisecond
			remote.mu.Unlock()
			state.openSFTP(session)
			if session.files.path != "" {
				t.Fatal("fixture already accepted the restored directory")
			}
			if hide {
				state.hideFilesPanel(session)
			} else {
				state.closeSFTP(session)
			}
			if session.fileHistory[1].path != "/home/test/docs" {
				t.Fatalf("interrupted restore erased the last accepted path: %q", session.fileHistory[1].path)
			}
		})
	}
}

type reconnectPathRemote struct {
	*workbenchSFTPRemote
	err      error
	attempts *atomic.Int32
}

func (remote reconnectPathRemote) ReadDir(directory string) ([]os.FileInfo, error) {
	if directory == "/home/test/docs" {
		remote.attempts.Add(1)
		return nil, remote.err
	}
	return remote.workbenchSFTPRemote.ReadDir(directory)
}

func TestSFTPReconnectUnavailableDirectoryFallsBackOnce(t *testing.T) {
	for _, failure := range []error{os.ErrNotExist, os.ErrPermission, &sftp.StatusError{Code: 2}, &sftp.StatusError{Code: 3}, &sftp.StatusError{Code: 10}} {
		t.Run(failure.Error(), func(t *testing.T) {
			state, session, _, remote := openTestSFTP(t)
			state.navigateSFTPDirectory(session.files, "/home/test/docs")
			waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
			attempts := new(atomic.Int32)
			next := &fakeConn{sftp: reconnectPathRemote{remote, failure, attempts}}
			state.options.Dial = func(sshconn.Config) Connection { return next }
			state.reconnect(session)
			next.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
			state.Wake()
			waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
			if session.files.path != "/home/test" || len(session.files.rows) != 2 {
				t.Fatalf("unavailable restore directory did not recover: path=%q error=%q", session.files.path, session.files.errorText)
			}
			if attempts.Load() != 1 {
				t.Fatal("restored stale cached rows instead of checking the directory")
			}
		})
	}
}
