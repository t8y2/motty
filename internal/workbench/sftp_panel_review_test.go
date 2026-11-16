//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"motty/internal/sftpclient"
	layoutcore "motty/internal/workspace"
)

func TestSFTPPanelStartupDefaultsOverrideEachWorkspaceWithoutChangingWidths(t *testing.T) {
	for _, open := range []bool{false, true} {
		for _, side := range []layoutcore.ContextSide{layoutcore.ContextLeft, layoutcore.ContextRight} {
			t.Run(fmt.Sprintf("open=%v/side=%v", open, side), func(t *testing.T) {
				world, options := newWorld(t)
				state := newTestState(t, options)
				state.sftpPanelAutoOpen, state.sftpPanelSide = open, side
				widths := []float32{303, 417}
				for i, width := range widths {
					state.newHostSession(state.hosts[i])
					id := state.deck.ActiveWorkspace()
					context, _ := state.deck.Context(id)
					context.Open, context.Side, context.Width = !open, layoutcore.ContextRight-side, width
					if err := state.deck.SetContext(id, context); err != nil {
						t.Fatal(err)
					}
				}
				state.save()
				dials := len(world.conns)
				restored := newTestState(t, options)
				if len(world.conns) != dials {
					t.Fatal("restoring panel defaults dialed a disconnected session")
				}
				for i, workspace := range restored.deck.Workspaces(nil) {
					context, _ := restored.deck.Context(workspace.ID)
					if context.Kind != layoutcore.ContextSFTP || context.Open != open || context.Side != side || context.Width != widths[i] {
						t.Fatalf("workspace %v restored the temporary layout instead of defaults: %+v", workspace.ID, context)
					}
					restored.activateWorkspace(workspace.ID)
					if after, _ := restored.deck.Context(workspace.ID); after != context {
						t.Fatal("selecting the restored workspace changed its defaults")
					}
				}
			})
		}
	}
}

// Block both directions after a real worker has opened the transfer streams.
// Closing the worker still releases the gate, including on test failure.
type panelDuplexRemote struct {
	*centerGatedRemote
	written chan struct{}
}

type panelDuplexFile struct {
	sftpclient.TransferFile
	remote *panelDuplexRemote
}

func (remote *panelDuplexRemote) OpenTransferFile(name string, flags int) (sftpclient.TransferFile, error) {
	file, err := remote.centerGatedRemote.OpenTransferFile(name, flags)
	if err != nil {
		return nil, err
	}
	return &panelDuplexFile{file, remote}, nil
}

func (file *panelDuplexFile) Write(data []byte) (int, error) {
	select {
	case file.remote.written <- struct{}{}:
	default:
	}
	<-file.remote.gate
	return file.TransferFile.Write(data)
}

func TestSFTPPanelNativeSwitchKeepsConcurrentUploadAndDownloadAtDisplayScales(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state, session, _, remote := openTestSFTP(t)
			files, conn := session.files, session.conn
			files.worker.Close()
			gated := &panelDuplexRemote{
				centerGatedRemote: &centerGatedRemote{workbenchSFTPRemote: remote, gate: make(chan struct{}), opened: make(chan struct{}, 1)},
				written:           make(chan struct{}, 1),
			}
			worker := sftpclient.NewWorker(func() (sftpclient.Remote, error) { return gated, nil }, nil)
			files.worker = worker
			defer worker.Close()
			state.configureSFTPWorker(files)
			dir := t.TempDir()
			upload, download := filepath.Join(dir, "review-upload.yml"), filepath.Join(dir, "review-download.txt")
			const uploaded = "name: 中文\nport: 8080\n"
			if err := os.WriteFile(upload, []byte(uploaded), 0600); err != nil {
				t.Fatal(err)
			}
			if _, ok := worker.QueueDownload("/home/test/notes.txt", download, false, sftpclient.ExistingFail); !ok {
				t.Fatal("download was not queued")
			}
			if _, ok := worker.QueueUpload(upload, "/home/test/review-upload.yml", false, sftpclient.ExistingFail); !ok {
				t.Fatal("upload was not queued")
			}
			for _, started := range []chan struct{}{gated.opened, gated.written} {
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("both transfer directions must be active before moving")
				}
			}
			preview, err := headless.Open(app.Config{Title: "Duplex transfer", Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnResize: state.Resize}, nil, headless.Options{Scale: scale})
			if err != nil {
				t.Fatal(err)
			}
			defer preview.Close()
			for range 4 {
				hit, ok := findSnapshotHit(preview.Snapshot(), contextID(hitSFTPPanelSide))
				if !ok {
					t.Fatal("move control is unavailable during transfer")
				}
				if err := preview.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				if session.conn != conn || session.files != files || files.worker != worker || files.path != "/home/test" {
					t.Fatal("moving changed the connection, worker or current directory")
				}
				if tasks := worker.Transfers(nil); len(tasks) != 2 || tasks[0].State != sftpclient.TransferRunning || tasks[1].State != sftpclient.TransferRunning {
					t.Fatal("moving interrupted a transfer", tasks)
				}
			}
			gated.once.Do(func() { close(gated.gate) })
			waitTransfersDone(t, state, session, 2)
			if data, err := os.ReadFile(download); err != nil || string(data) != "hello world" {
				t.Fatal("download content or destination changed", err)
			}
			remote.mu.Lock()
			actual := string(remote.files["/home/test/review-upload.yml"])
			remote.mu.Unlock()
			if actual != uploaded {
				t.Fatal("upload content or destination changed")
			}
		})
	}
}
