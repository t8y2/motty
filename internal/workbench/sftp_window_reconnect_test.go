//go:build windows || darwin || linux

package workbench

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

func sftpWindowRefreshHit(w *sftpWindow) (gfx.Hit, bool) {
	var canvas gfx.Canvas
	w.paint(&canvas, gfx.Size{Width: 1200, Height: 800})
	return hitByID(&canvas, sftpWindowRefresh[1])
}

func TestSFTPWindowBorrowedReconnectRestoresRemoteAndEnablesRefresh(t *testing.T) {
	for _, mode := range []string{"manual-tab", "auto-tab", "manual-window"} {
		t.Run(mode, func(t *testing.T) {
			var (
				state  *State
				source *Session
				w      *sftpWindow
				remote *workbenchSFTPRemote
				host   *sftpNativeHost
			)
			if mode == "manual-window" {
				state, w, remote, host, _ = dualSFTPFixture(t, 1)
				var ok bool
				source, ok = state.sessionByIDPointer(w.session.ID)
				if !ok {
					t.Fatal("missing source session")
				}
			} else {
				state, source, w, remote, _ = openSFTPTabFixture(t)
			}

			w.navigate(false, "/home/test/docs")
			if host != nil {
				dualWait(t, state, w, host.windows[0].view, func() bool { return !w.view.busy && w.view.path == "/home/test/docs" })
			} else {
				waitSFTPTab(t, state, w, func() bool { return !w.view.busy && w.view.path == "/home/test/docs" })
			}
			oldWorker := w.view.worker

			clock := time.Second
			if mode == "auto-tab" {
				state.options.Now = func() time.Duration { return clock }
				state.connectionReconnect = reconnectThree
				// Switch back to terminal tab so the file tab also recovers in the background.
				state.setActiveSection(sectionTerminal)
			}

			oldConn := source.conn.(*fakeConn)
			oldConn.set(func(s *sshconn.Status) {
				s.Phase = sshconn.PhaseClosed
				s.Err = errors.Join(sshconn.ErrDisconnected, errors.New("idle timeout"))
			})
			state.Wake()

			if w.connected() {
				t.Fatal("transfer window still reports connected after SSH closed")
			}
			if hit, ok := sftpWindowRefreshHit(w); !ok || !hit.Disabled {
				t.Fatalf("disconnected remote refresh hit = (%+v, %t), want disabled", hit, ok)
			}

			next := &fakeConn{sftp: remote}
			state.options.Dial = func(sshconn.Config) Connection { return next }
			if mode == "auto-tab" {
				if !source.reconnectPending {
					t.Fatal("unexpected disconnect did not schedule automatic SSH reconnect")
				}
				clock += time.Second
				state.Wake()
			} else {
				state.reconnect(source)
			}
			if source.conn != next {
				t.Fatal("source session did not dial the replacement connection")
			}
			// Before the handshake completes, the remote side remains disconnected.
			state.Wake()
			if w.connected() {
				t.Fatal("transfer window reported connected before SSH finished connecting")
			}

			next.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
			state.Wake()

			if host != nil {
				dualWait(t, state, w, host.windows[0].view, func() bool { return !w.view.busy && w.connected() && w.view.worker != oldWorker })
			} else {
				waitSFTPTab(t, state, w, func() bool { return !w.view.busy && w.connected() && w.view.worker != oldWorker })
			}
			if w.view.path != "/home/test/docs" || len(w.view.rows) != 1 || w.view.rows[0].entry.Name != "guide.md" {
				t.Fatalf("reconnected file window lost directory/rows: path=%q rows=%+v", w.view.path, w.view.rows)
			}
			if hit, ok := sftpWindowRefreshHit(w); !ok || hit.Disabled {
				t.Fatalf("reconnected remote refresh hit = (%+v, %t), want enabled", hit, ok)
			}

			// Verify refresh actually reads through the new connection.
			remote.mu.Lock()
			remote.entries["/home/test/docs"] = append(remote.entries["/home/test/docs"], workbenchFileInfo{
				name: "added.md", size: 4, mode: 0o644, when: time.Unix(1_700_000_500, 0),
			})
			remote.mu.Unlock()
			w.click(sftpWindowRefresh[1])
			if host != nil {
				dualWait(t, state, w, host.windows[0].view, func() bool { return !w.view.busy && len(w.view.rows) == 2 })
			} else {
				waitSFTPTab(t, state, w, func() bool { return !w.view.busy && len(w.view.rows) == 2 })
			}
		})
	}
}

func TestSFTPWindowBorrowedReconnectDropsHistoryFromAnotherEndpoint(t *testing.T) {
	state, source, w, remote, _ := openSFTPTabFixture(t)
	w.navigate(false, "/home/test/docs")
	waitSFTPTab(t, state, w, func() bool { return !w.view.busy && w.view.path == "/home/test/docs" })

	previous := source.host
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
		t.Fatal("missing host record")
	}
	state.replaceHost(index, previous, updated)

	next := &fakeConn{sftp: remote}
	state.options.Dial = func(sshconn.Config) Connection { return next }
	state.reconnect(source)
	next.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	waitSFTPTab(t, state, w, func() bool { return !w.view.busy && w.connected() })
	if w.view.path != "/home/test" {
		t.Fatalf("changed endpoint restored %q from previous server", w.view.path)
	}
}

func TestSFTPWindowBorrowedSudoDisconnectRecoversOnReconnect(t *testing.T) {
	state, source, w, remote, _ := openSFTPTabFixture(t)
	w.navigate(false, "/home/test/docs")
	waitSFTPTab(t, state, w, func() bool { return !w.view.busy && w.view.path == "/home/test/docs" })

	privileged := &sudoSFTPTestConn{fakeConn: source.conn.(*fakeConn), remote: remote}
	w.session.conn = privileged
	w.ui.menuOpen = true
	w.click(hitSFTPMenuSudo)
	state.sudoSFTP.password.Insert("secret")
	state.Click(hitSudoSFTPEnable)
	waitSFTPTab(t, state, w, func() bool { return w.view.sudo && !w.view.busy })

	source.conn.(*fakeConn).set(func(s *sshconn.Status) {
		s.Phase = sshconn.PhaseClosed
		s.Err = sshconn.ErrDisconnected
	})
	state.Wake()
	if w.view.worker != nil || w.view.sudo {
		t.Fatal("sudo worker survived disconnect")
	}

	next := &fakeConn{sftp: remote}
	state.options.Dial = func(sshconn.Config) Connection { return next }
	state.reconnect(source)
	next.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	waitSFTPTab(t, state, w, func() bool { return !w.view.busy && w.connected() && w.view.worker != nil })
	if w.view.path != "/home/test/docs" || w.view.sudo {
		t.Fatalf("sudo reconnect state: path=%q sudo=%t", w.view.path, w.view.sudo)
	}
}

func TestSFTPWindowBorrowedReconnectResumesInterruptedUpload(t *testing.T) {
	state, source, w, remote, localDir := openSFTPTabFixture(t)
	state.hideFilesPanel(source)
	payload := bytes.Repeat([]byte("motty window resume "), 1<<15)
	local := filepath.Join(localDir, "payload.bin")
	if err := os.WriteFile(local, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	w.view.worker.Close()
	gated := newReconnectGateRemote(remote, 32<<10)
	w.view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return gated, nil }, state.notifyForView(&w.session, w.view))
	state.configureSFTPWorker(w.view)

	w.queueEntries(true, []sftpclient.Entry{{Name: "payload.bin"}}, localDir, w.view.path)
	select {
	case <-gated.reached:
	case <-time.After(waitBudget()):
		t.Fatal("upload did not reach the gate")
	}

	source.conn.(*fakeConn).set(func(s *sshconn.Status) {
		s.Phase = sshconn.PhaseClosed
		s.Err = sshconn.ErrDisconnected
	})
	state.Wake()

	oldWorker := w.view.worker
	next := &fakeConn{sftp: remote}
	state.options.Dial = func(sshconn.Config) Connection { return next }
	state.reconnect(source)
	next.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	close(gated.release)
	select {
	case <-oldWorker.Done():
	case <-time.After(waitBudget()):
		t.Fatal("interrupted worker did not stop")
	}

	waitSFTPTab(t, state, w, func() bool {
		return !w.view.busy && len(w.view.transfers) == 1 && w.view.transfers[0].State == sftpclient.TransferCompleted
	})
	remote.mu.Lock()
	uploaded := append([]byte(nil), remote.files["/home/test/payload.bin"]...)
	remote.mu.Unlock()
	if !bytes.Equal(uploaded, payload) {
		t.Fatalf("resumed upload size = %d, want %d", len(uploaded), len(payload))
	}
}

func TestSFTPWindowOwnedReconnectAutoAndManualRefresh(t *testing.T) {
	for _, auto := range []bool{false, true} {
		name := "manual-refresh"
		if auto {
			name = "auto-reconnect"
		}
		t.Run(name, func(t *testing.T) {
			state, original, remote, host, _ := dualSFTPFixture(t, 1)
			target := original.session.host
			target.ID += 1000
			target.CatalogID = "standalone-reconnect"
			state.hosts = append(state.hosts, target)

			first := &fakeConn{sftp: remote}
			first.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
			second := &fakeConn{sftp: remote}
			dials := 0
			state.options.Dial = func(sshconn.Config) Connection {
				dials++
				if dials == 1 {
					return first
				}
				return second
			}

			clock := time.Second
			state.options.Now = func() time.Duration { return clock }
			if auto {
				state.connectionReconnect = reconnectThree
			} else {
				state.connectionReconnect = reconnectOff
			}

			state.openHostSFTPWindow(target)
			var w *sftpWindow
			for _, candidate := range state.sftpWindows {
				if candidate.session.HostID == target.ID {
					w = candidate
				}
			}
			if w == nil || !w.ownsConnection {
				t.Fatal("standalone transfer window did not open")
			}
			view := host.windows[1].view
			dualWait(t, state, w, view, func() bool { return !w.view.busy && w.view.path == "/home/test" })
			w.navigate(false, "/home/test/docs")
			dualWait(t, state, w, view, func() bool { return !w.view.busy && w.view.path == "/home/test/docs" })

			first.set(func(s *sshconn.Status) {
				s.Phase = sshconn.PhaseClosed
				s.Err = sshconn.ErrDisconnected
			})
			state.Wake()
			if w.connected() {
				t.Fatal("standalone window stayed connected after SSH closed")
			}

			if auto {
				if !w.session.reconnectPending {
					t.Fatal("standalone disconnect did not schedule automatic reconnect")
				}
				clock += time.Second
				state.Wake()
			} else {
				if hit, ok := sftpWindowRefreshHit(w); !ok || hit.Disabled {
					t.Fatalf("standalone disconnected refresh hit = (%+v, %t), want enabled for reconnect", hit, ok)
				}
				w.click(sftpWindowRefresh[1])
			}
			if dials != 2 || w.session.conn != second {
				t.Fatalf("standalone window did not dial replacement connection: dials=%d", dials)
			}

			second.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
			state.Wake()
			dualWait(t, state, w, view, func() bool { return !w.view.busy && w.connected() && w.view.path == "/home/test/docs" })
			if len(w.view.rows) != 1 || w.view.rows[0].entry.Name != "guide.md" {
				t.Fatalf("standalone reconnect lost directory rows: %+v", w.view.rows)
			}
		})
	}
}

func TestSFTPWindowOwnedCloseCancelsPendingReconnect(t *testing.T) {
	state, original, remote, _, _ := dualSFTPFixture(t, 1)
	target := original.session.host
	target.ID += 1000
	conn := &fakeConn{sftp: remote}
	conn.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.options.Dial = func(sshconn.Config) Connection { return conn }
	state.connectionReconnect = reconnectThree

	state.openHostSFTPWindow(target)
	var w *sftpWindow
	for _, candidate := range state.sftpWindows {
		if candidate.session.HostID == target.ID {
			w = candidate
		}
	}
	state.Wake()
	conn.set(func(s *sshconn.Status) {
		s.Phase = sshconn.PhaseClosed
		s.Err = sshconn.ErrDisconnected
	})
	state.Wake()
	if !w.session.reconnectPending || state.reconnectWaiters == 0 {
		t.Fatal("expected pending reconnect before closing")
	}
	w.window.Close()
	if w.session.reconnectPending || state.reconnectWaiters != 0 {
		t.Fatalf("closing standalone window leaked reconnect waiter: pending=%t waiters=%d", w.session.reconnectPending, state.reconnectWaiters)
	}
}
