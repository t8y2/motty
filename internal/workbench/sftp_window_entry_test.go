//go:build windows || darwin || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
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
	if state.modal != modalHostKey || len(state.sessions) != sessions || len(host.windows) != 1 {
		t.Fatal("standalone authentication created a terminal/window too early")
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
	document.save(false)
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
