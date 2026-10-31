//go:build windows || darwin || linux

package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

func TestSFTPWindowHostMenuAuthenticatesWithoutTerminalAndRetainsDocumentConnection(t *testing.T) {
	state, original, remote, host, _ := dualSFTPFixture(t, 1.25)
	target := original.session.host
	target.ID += 1000
	target.CatalogID = "standalone-sftp"
	state.hosts = append(state.hosts, target)
	conn := &fakeConn{sftp: remote}
	conn.set(func(s *sshconn.Status) {
		s.Phase = sshconn.PhaseHostKeyPrompt
		s.HostKey = sshconn.HostKeyInfo{Address: target.Address, Type: "ssh-ed25519"}
	})
	var dialed sshconn.Config
	dials := 0
	raised := 0
	state.options.Raise = func() { raised++ }
	state.options.Dial = func(c sshconn.Config) Connection { dialed = c; dials++; return conn }
	sessions := len(state.sessions)
	state.activeSessionID, state.activeSection = 0, sectionHosts
	state.hostMenu = hostMenu{open: true, host: target.ID, at: ui.Point{X: 300, Y: 250}}
	var canvas ui.Canvas
	state.paintHostMenu(&canvas, ui.Rect{Width: 1400, Height: 900})
	connect, _ := hitByID(&canvas, hitHostMenuConnect)
	files, ok := hitByID(&canvas, hitHostMenuSFTP)
	edit, _ := hitByID(&canvas, hitHostMenuEdit)
	if !ok || files.Disabled || files.Bounds.Y < connect.Bounds.Y+connect.Bounds.Height || files.Bounds.Y >= edit.Bounds.Y {
		t.Fatal("SFTP must follow Connect in the host menu")
	}
	state.Click(hitHostMenuSFTP)
	state.Wake()
	if state.modal != modalHostKey || len(state.sessions) != sessions || len(host.windows) != 2 {
		t.Fatal("standalone authentication must show progress without a terminal")
	}
	if raised != 1 || len(state.fileNotices.items) != 0 {
		t.Fatal("authentication must raise the main prompt without a loading toast")
	}
	if kind, _ := state.hostLight(target); kind != lightBusy {
		t.Fatal("standalone handshake missing from host status")
	}
	if dialed.Host != target.Address || dialed.KnownHostsPath != state.options.KnownHostsPath || dials != 1 {
		t.Fatal("standalone SSH lost host configuration")
	}
	state.openHostSFTPWindow(target)
	if dials != 1 {
		t.Fatal("duplicate click started another handshake")
	}
	state.Click(hitHostKeyOnce)
	conn.set(func(s *sshconn.Status) { s.Phase = sshconn.PhasePasswordPrompt; s.Attempt = 1 })
	state.Wake()
	if state.modal != modalPassword {
		t.Fatal("standalone password prompt missing")
	}
	state.passwordFields[0].Insert("test-password")
	state.Click(hitPasswordSubmit)
	conn.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	var w *sftpWindow
	for _, candidate := range state.sftpWindows {
		if candidate.session.HostID == target.ID {
			w = candidate
		}
	}
	if w == nil || w.window == nil || !w.ownsConnection || w.session.term != nil || state.modal != modalNone {
		t.Fatal("standalone browser did not open cleanly")
	}
	view := host.windows[1].view
	dualWait(t, state, w, view, func() bool { return !w.view.busy && len(w.view.rows) == 2 })
	conn.mu.Lock()
	shells, passwords, trusted := len(conn.configs), len(conn.passwords), len(conn.trusted)
	conn.mu.Unlock()
	if shells != 0 || passwords != 1 || trusted != 1 || len(state.sessions) != sessions || state.activeSessionID != 0 || state.activeSection != sectionHosts {
		t.Fatal("SFTP opened or switched to a terminal")
	}
	if kind, _ := state.hostLight(target); kind != lightOn {
		t.Fatal("standalone connection missing from host status")
	}
	for i := range w.view.rows {
		if w.view.rows[i].entry.Name == "notes.txt" {
			w.openEntry(false, i)
			break
		}
	}
	dualWait(t, state, w, view, func() bool { return len(state.fileWindows.documents) == 1 && len(host.windows) == 3 })
	var document *fileDocument
	for _, d := range state.fileWindows.documents {
		document = d
	}
	w.window.Close()
	conn.mu.Lock()
	canceled := conn.canceled
	conn.mu.Unlock()
	if canceled != 0 || !w.closed {
		t.Fatal("browser close interrupted its open document")
	}
	app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextSelectAll}.Apply(&document.editor)
	document.editor.Insert("saved after browser close\n中文")
	document.saveDirect(false)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if !document.busy && remoteText(remote, "/home/test/notes.txt") == document.editor.String() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if document.busy || remoteText(remote, "/home/test/notes.txt") != document.editor.String() {
		t.Fatal("document could not save after browser closed")
	}
	document.window.Close()
	conn.mu.Lock()
	canceled = conn.canceled
	conn.mu.Unlock()
	if canceled != 1 {
		t.Fatal("standalone connection not released with its last document", canceled)
	}
}

func TestSFTPWindowLoadingCanCancelAuthenticationAndRespectsReducedMotion(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state, original, _, host, _ := dualSFTPFixture(t, scale)
			target := original.session.host
			target.ID += 1000
			conn := &fakeConn{}
			conn.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnecting })
			state.options.Dial = func(sshconn.Config) Connection { return conn }
			state.openHostSFTPWindow(target)
			var w *sftpWindow
			for _, candidate := range state.sftpWindows {
				if candidate.session.HostID == target.ID {
					w = candidate
				}
			}
			if w == nil || w.window == nil || w.view.worker != nil || len(host.windows) != 2 || len(state.fileNotices.items) != 0 {
				t.Fatal("handshake needs its own loading window, not a toast or browser")
			}
			for _, reduced := range []bool{false, true} {
				var canvas ui.Canvas
				canvas.SetReduceMotion(reduced)
				canvas.BeginFrame()
				w.paint(&canvas, ui.Size{Width: 640, Height: 400})
				canvas.EndFrame()
				if _, animated := canvas.NextFrame(); animated == reduced {
					t.Fatal("connection animation did not respect reduced motion")
				}
				if _, ok := hitByID(&canvas, hitSFTPWindowConnectCancel); !ok {
					t.Fatal("loading window cannot cancel its handshake")
				}
			}
			conn.set(func(s *sshconn.Status) { s.Phase = sshconn.PhasePasswordPrompt; s.Attempt = 1 })
			state.Wake()
			if state.modal != modalPassword || state.modalSession != w.session.ID {
				t.Fatal("loading window lost its authentication prompt")
			}
			w.click(hitSFTPWindowConnectCancel)
			conn.mu.Lock()
			canceled := conn.canceled
			conn.mu.Unlock()
			if !w.closed || canceled != 1 || state.modal != modalNone || state.sftpWindows[w.session.ID] != nil {
				t.Fatal("closing loading left authentication or its connection alive")
			}
		})
	}
}

func TestSFTPWindowFailedHandshakeRetriesInsideSameWindow(t *testing.T) {
	state, original, remote, host, _ := dualSFTPFixture(t, 1.25)
	target := original.session.host
	target.ID += 1000
	first, second := &fakeConn{}, &fakeConn{sftp: remote}
	first.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnecting })
	second.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	dials := 0
	state.options.Dial = func(sshconn.Config) Connection {
		dials++
		if dials == 1 {
			return first
		}
		return second
	}
	state.openHostSFTPWindow(target)
	var w *sftpWindow
	for _, candidate := range state.sftpWindows {
		if candidate.session.HostID == target.ID {
			w = candidate
		}
	}
	window := w.window
	first.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseFailed; s.Err = errors.New("connection refused") })
	state.Wake()
	var canvas ui.Canvas
	w.paint(&canvas, ui.Size{Width: 640, Height: 400})
	if _, ok := hitByID(&canvas, hitSFTPWindowConnectRetry); !ok || w.closed || len(state.fileNotices.items) != 0 {
		t.Fatal("failed handshake needs inline failure and retry")
	}
	w.click(hitSFTPWindowConnectRetry)
	dualWait(t, state, w, host.windows[1].view, func() bool { return w.view.worker != nil && !w.view.busy && len(w.view.rows) == 2 })
	if w.window != window || dials != 2 || len(host.windows) != 2 || w.session.term != nil {
		t.Fatal("retry replaced the native window or created a terminal")
	}
	first.mu.Lock()
	canceled := first.canceled
	first.mu.Unlock()
	if canceled != 1 {
		t.Fatal("failed handshake was not released")
	}
}

func TestSFTPWindowInvalidRouteStaysFailedAndRetryable(t *testing.T) {
	state, original, _, host, _ := dualSFTPFixture(t, 1.25)
	target := original.session.host
	target.ID += 1000
	state.hosts = append(state.hosts, target)
	state.proxies = append(state.proxies, proxyRecord{TargetID: target.ID, JumpID: target.ID + 1})
	sessions := len(state.sessions)
	dials := 0
	state.options.Dial = func(sshconn.Config) Connection {
		dials++
		return &fakeConn{}
	}

	state.openHostSFTPWindow(target)
	var w *sftpWindow
	for _, candidate := range state.sftpWindows {
		if candidate.session.HostID == target.ID {
			w = candidate
		}
	}
	if w == nil || w.window == nil {
		t.Fatal("invalid route did not open its failure window")
	}
	state.Wake()
	if w.session.phase != sshconn.PhaseFailed || w.phase != sshconn.PhaseFailed || w.session.conn != nil {
		t.Fatalf("invalid route state = session %v, window %v, conn %v", w.session.phase, w.phase, w.session.conn)
	}
	var canvas ui.Canvas
	w.paint(&canvas, ui.Size{Width: 640, Height: 400})
	if _, ok := hitByID(&canvas, hitSFTPWindowConnectRetry); !ok {
		t.Fatal("invalid route failure cannot be retried")
	}
	w.click(hitSFTPWindowConnectRetry)
	state.Wake()
	if w.session.phase != sshconn.PhaseFailed || w.closed || len(host.windows) != 2 || dials != 0 || len(state.sessions) != sessions || w.session.term != nil {
		t.Fatalf("retry changed invalid route window: phase=%v closed=%t windows=%d dials=%d sessions=%d terminal=%v", w.session.phase, w.closed, len(host.windows), dials, len(state.sessions), w.session.term)
	}
}

func TestSFTPWindowQuitWhileAuthenticationPendingReleasesConnection(t *testing.T) {
	state, original, _, _, _ := dualSFTPFixture(t, 1)
	target := original.session.host
	target.ID += 1000
	conn := &fakeConn{}
	conn.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnecting })
	state.options.Dial = func(sshconn.Config) Connection { return conn }
	state.openHostSFTPWindow(target)
	state.Close()
	conn.mu.Lock()
	canceled := conn.canceled
	shells := len(conn.configs)
	conn.mu.Unlock()
	if canceled != 1 || shells != 0 {
		t.Fatal("pending standalone connection leaked or opened a shell", canceled, shells)
	}
}

func TestSFTPWindowClosingBorrowedBrowserPreservesTerminalAuthentication(t *testing.T) {
	state, w, _, _, _ := dualSFTPFixture(t, 1.25)
	state.modal, state.modalSession = modalPassword, w.session.ID
	state.passwordFields = []text.Editor{*text.NewEditor("unfinished password")}
	w.window.Close()
	if !w.closed || state.modal != modalPassword || state.modalSession != w.session.ID ||
		len(state.passwordFields) != 1 || state.passwordFields[0].String() != "unfinished password" {
		t.Fatal("closing a borrowed file browser dismissed the terminal authentication")
	}
}

func TestSFTPWindowAsyncRemoteNavigationBlocksOldDirectoryTransfers(t *testing.T) {
	state, w, remote, host, dir := dualSFTPFixture(t, 1)
	remote.mu.Lock()
	for i := range sftpPrepInlineEntries + 1 {
		remote.entries["/many"] = append(remote.entries["/many"], workbenchFileInfo{name: fmt.Sprintf("file-%04d.txt", i), mode: 0644})
	}
	remote.mu.Unlock()
	w.navigate(false, "/many")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && w.view.preparingPath == "" {
		w.wake()
		time.Sleep(time.Millisecond)
	}
	if w.view.preparingPath != "/many" || !w.view.busy || w.view.path == "/many" {
		t.Fatal("large listing stopped being busy before rows were ready")
	}
	w.queueEntries(true, []sftpclient.Entry{{Name: "中文上传.yml"}}, dir, "/home/test")
	if len(w.view.worker.Transfers(nil)) != 0 {
		t.Fatal("pending remote navigation uploaded into old directory")
	}
	dualWait(t, state, w, host.windows[0].view, func() bool { return !w.view.busy && w.view.path == "/many" })
}

func TestSFTPWindowSecondaryPressCancelsDragBeforeDeferredRowsChange(t *testing.T) {
	state, w, _, _, dir := dualSFTPFixture(t, 1)
	w.drag = sftpWindowDrag{armed: true, active: true, fromLocal: true, sourceDir: dir}
	state.applyLocalListing(w.view, localListing{dir: dir}, w.view.localSeq)
	if len(w.view.lfolder.rows) == 0 {
		t.Fatal("listing was not deferred")
	}
	result := w.pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonSecondary, Target: sftpWindowRows[0].At(1), Captured: true})
	if !result.Handled || w.drag.active || len(w.view.lfolder.rows) != 0 || len(w.view.worker.Transfers(nil)) != 0 {
		t.Fatal("secondary press did not safely cancel drag")
	}
}

func TestSFTPWindowMetadataRefreshKeepsSortedCursorAndRangeAnchor(t *testing.T) {
	state, w, _, _, _ := dualSFTPFixture(t, 1)
	entries := []sftpclient.Entry{{Name: "a.txt", Size: 1}, {Name: "b.txt", Size: 2}, {Name: "c.txt", Size: 3}}
	w.ui.sort[1] = sftpWindowSort{column: 1, descending: true}
	state.showSFTPListing(w.view, w.view.path, entries)
	w.view.sftpFolder.selectRow(0, 0)
	w.view.anchor = 2
	entries[2].Size = 4
	state.updateSFTPRows(w.view, entries)
	if w.view.rows[w.view.selected].entry.Name != "c.txt" || w.view.rows[w.view.anchor].entry.Name != "a.txt" || !w.view.rows[w.view.selected].selected {
		t.Fatal("metadata refresh moved cursor/anchor to a different file")
	}
}

func TestSFTPWindowSortedTransfersUseVisibleFolderAtEveryScale(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state, w, remote, host, dir := dualSFTPFixture(t, scale)
			view := host.windows[0].view
			remote.mu.Lock()
			remote.entries["/home/test"] = append(remote.entries["/home/test"], workbenchFileInfo{name: "z-target", size: 300, mode: os.ModeDir | 0755, when: time.Unix(1_700_000_300, 0)}, workbenchFileInfo{name: "a-other", size: 200, mode: os.ModeDir | 0755, when: time.Unix(1_700_000_000, 0)})
			remote.entries["/home/test/z-target"] = nil
			remote.mu.Unlock()
			for column := range 3 {
				for _, descending := range []bool{false, true} {
					w.refresh(false)
					dualWait(t, state, w, view, func() bool { return !w.view.busy && len(w.view.rows) == 4 })
					name := fmt.Sprintf("排序-%d-%t.yml", column, descending)
					data := []byte("name: 中文\n")
					if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
						t.Fatal(err)
					}
					w.navigate(true, dir)
					for side := range 2 {
						for w.ui.sort[side].column != column || w.ui.sort[side].descending != descending {
							view.Driver().Pump()
							h := dualHit(t, view, sftpWindowSortHits[side].At(column))
							view.Click(h.X+h.Width/2, h.Y+h.Height/2)
						}
					}
					view.Driver().Pump()
					before := len(w.view.transfers)
					dualDrag(t, view, dualHit(t, view, dualRow(t, w, true, name)), dualHit(t, view, dualRow(t, w, false, "z-target")))
					dualWait(t, state, w, view, func() bool {
						return len(w.view.transfers) == before+1 && !sftpTransfersActive(w.view.transfers) && !w.view.busy && !w.view.refreshAfterTransfers
					})
					if remoteText(remote, "/home/test/z-target/"+name) != string(data) {
						t.Fatal("sorted upload landed in another directory")
					}
					w.navigate(false, "/home/test/z-target")
					dualWait(t, state, w, view, func() bool { return !w.view.busy && w.view.path == "/home/test/z-target" })
					dualDrag(t, view, dualHit(t, view, dualRow(t, w, false, name)), dualHit(t, view, dualRow(t, w, true, "destination")))
					dualWait(t, state, w, view, func() bool { return len(w.view.transfers) == before+2 && !sftpTransfersActive(w.view.transfers) })
					got, err := os.ReadFile(filepath.Join(dir, "destination", name))
					if err != nil || string(got) != string(data) {
						t.Fatal("sorted download landed in another directory", err)
					}
					w.navigate(false, "/home/test")
					dualWait(t, state, w, view, func() bool { return !w.view.busy && w.view.path == "/home/test" })
				}
			}
		})
	}
}

func TestSFTPWindowPathUsesUpstreamValidationAndOwnNotices(t *testing.T) {
	state, w, remote, host, _ := dualSFTPFixture(t, 1)
	w.view.worker.Close()
	w.view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return pathErrorRemote{remote, os.ErrNotExist}, nil }, w.window.Invalidate)
	w.view.remember("/home/test/missing", []sftpclient.Entry{{Name: "stale.txt"}})
	editor := w.view.sftpFolder.pathInput()
	*editor = *text.NewEditor("missing")
	editor.SetMarkedText("目录", text.Range{Start: 0, End: 2})
	w.key(ui.KeyEvent{Target: sftpWindowPath[1], Key: ui.KeyEnter})
	if w.view.busy || w.view.pathRequest != "" {
		t.Fatal("IME confirmation submitted a directory")
	}
	for attempt := range 2 {
		_ = attempt
		*editor = *text.NewEditor("missing")
		w.key(ui.KeyEvent{Target: sftpWindowPath[1], Key: ui.KeyEnter})
		if !w.view.busy {
			t.Fatal("manual navigation trusted a stale cached directory")
		}
		dualWait(t, state, w, host.windows[0].view, func() bool { return !w.view.busy })
	}
	if w.view.path != "/home/test" || editor.String() != "missing" || len(w.notices.items) != 1 || len(state.fileNotices.items) != 0 || !strings.Contains(w.notices.items[0].detail, "/home/test/missing") {
		t.Fatal("native path failure lost its draft, origin or target path")
	}
}
