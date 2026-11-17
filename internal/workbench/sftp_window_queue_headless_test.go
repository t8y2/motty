//go:build darwin || linux || windows

package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
)

func TestSFTPWindowQueuePreview(t *testing.T) {
	for _, mode := range []string{"window", "tab"} {
		t.Run(mode, func(t *testing.T) {
			state, _, w, _, _ := openSFTPTabFixture(t)
			state.setLanguage(i18n.Chinese)
			state.chooseAppearanceMode(appearancePinnedDark)
			config := w.config()
			if mode == "tab" {
				state.chooseAppearanceMode(appearancePinnedLight)
				config = state.WindowConfig(app.Config{
					Width: 1100, Height: 760, Tree: Tree(), Paint: state.Paint,
					OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
				})
			} else {
				state.activeSFTPTab = 0
				// Keep demo snapshots fixed instead of polling the fixture's idle worker.
				config.OnWake = nil
			}
			seedSFTPQueuePreview(state, w)
			view, err := headless.Open(config, nil, headless.Options{Scale: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			first := dualHit(t, view, sftpWindowQueueRows.At(3))
			second := dualHit(t, view, sftpWindowQueueRows.At(2))
			if first.Y >= second.Y || first.Y < w.ui.queueBounds.Y {
				t.Fatalf("running and queued rows out of order: %+v, %+v", first, second)
			}
			dualHit(t, view, tree.MountID(sftpWindowQueueActions, hitTransfersPause.At(3)))
			dualHit(t, view, tree.MountID(sftpWindowQueueActions, hitTransfersReplace.At(4)))
			save := func(suffix string) {
				t.Helper()
				if dir := os.Getenv("MOTTY_SFTP_QUEUE_SCREENSHOTS"); dir != "" {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := view.Screenshot(filepath.Join(dir, "sftp-queue-"+mode+suffix+".png")); err != nil {
						t.Fatal(err)
					}
				}
			}
			save("")
			if err := view.Move(first.X+20, first.Y+first.Height/2); err != nil {
				t.Fatal(err)
			}
			if err := view.Wait(transferDetailDelay + 220*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if _, ok := findSnapshotHit(view.Snapshot(), hitSFTPQueueDetails); !ok {
				d := &w.ui.queueDetails
				t.Fatalf("no details: hovered=%v row=%d showing=%d", view.Snapshot().Hovered, d.hover.id, d.showing)
			}
			card := dualHit(t, view, hitSFTPQueueDetails)
			if card.Y >= first.Y || intersectRect(w.ui.bounds, card) != card {
				t.Fatalf("details should flip above the queue inside the viewport: %+v", card)
			}
			var labels strings.Builder
			for _, label := range view.Snapshot().Texts {
				if label.Layer == hoverCardLayer {
					labels.WriteString(label.Value)
					labels.WriteByte('\n')
				}
			}
			for _, want := range []string{"release-bundle.zip", "/srv/releases/release-bundle.zip", "开始时间", "3.0 MB/秒"} {
				if !strings.Contains(labels.String(), want) {
					t.Fatalf("details missing %q: %s", want, labels.String())
				}
			}
			save("-details")
			if err := view.Move(4, 4); err != nil {
				t.Fatal(err)
			}
			if err := view.Wait(transferDetailLinger + 100*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if _, ok := findSnapshotHit(view.Snapshot(), hitSFTPQueueDetails); ok {
				t.Fatal("details remained after the pointer left")
			}
			failed := dualHit(t, view, sftpWindowQueueRows.At(4))
			if err := view.Click(failed.X+20, failed.Y+failed.Height/2); err != nil {
				t.Fatal(err)
			}
			if err := view.Wait(220 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if w.ui.queueDetails.pinned != 4 {
				t.Fatal("click did not keep the failed task's details open")
			}
			for _, label := range view.Snapshot().Texts {
				if label.Layer == hoverCardLayer && label.Value == "连接超时" {
					save("-failed")
					if mode == "tab" {
						dualClickID(t, view, hitTransferCenter)
						if !state.transfers.open {
							t.Fatal("global transfer center did not open above the file tab")
						}
						actions := 0
						for _, hit := range view.Snapshot().Hits {
							if hit.ID == hitTransfersPause.At(3) {
								actions++
							}
						}
						if actions != 1 {
							t.Fatalf("expected one global pause control, found %d with the file tab behind it", actions)
						}
					}
					return
				}
			}
			t.Fatal("failed task details did not include its error")
		})
	}
}

func seedSFTPQueuePreview(state *State, w *sftpWindow) {
	at := time.Now().Truncate(time.Second).Add(-85 * time.Second)
	for i, item := range []struct {
		name   string
		status sftpclient.TransferState
		op     sftpclient.Operation
	}{
		{"deployment.yml", sftpclient.TransferCompleted, sftpclient.OperationDownload},
		{"media-assets.tar.gz", sftpclient.TransferQueued, sftpclient.OperationUpload},
		{"release-bundle.zip", sftpclient.TransferRunning, sftpclient.OperationUpload},
		{"backup-snapshot.sql", sftpclient.TransferFailed, sftpclient.OperationDownload},
		{"release-notes.md", sftpclient.TransferCompleted, sftpclient.OperationDownload},
		{"previous-release.yml", sftpclient.TransferCompleted, sftpclient.OperationDownload},
	} {
		id := i + 1
		local, remote := filepath.Join("preview", "releases", item.name), "/srv/releases/"+item.name
		if item.op == sftpclient.OperationDownload {
			local, remote = remote, local
		}
		job := sftpclient.TransferSnapshot{
			ID: uint64(id), State: item.status, Operation: item.op, Source: local, Destination: remote,
			QueuedAt: at.Add(time.Duration(i) * time.Second), QueueOrder: uint64(id),
		}
		if job.State != sftpclient.TransferQueued {
			job.StartedAt = job.QueuedAt.Add(time.Second)
		}
		switch job.State {
		case sftpclient.TransferRunning:
			job.BytesDone, job.BytesTotal, job.BytesPerSecond = 48<<20, 96<<20, 3<<20
		case sftpclient.TransferQueued:
			job.BytesTotal = 272 << 20
		case sftpclient.TransferFailed:
			job.BytesTotal, job.Retryable, job.Error = 24<<20, true, "连接超时"
			job.FinishedAt = at.Add(65 * time.Second)
		case sftpclient.TransferCompleted:
			job.BytesDone, job.BytesTotal = 8<<10, 8<<10
			job.FinishedAt = at.Add(time.Duration(45-i*4) * time.Second)
		}
		entry := transferEntry{id: id, owner: w.view, snapshot: job, server: "发布服务器", address: "ops@192.0.2.41:22"}
		state.describeTransfer(&entry)
		state.transfers.entries = append(state.transfers.entries, entry)
	}
	state.describeTransferSummary()
}
