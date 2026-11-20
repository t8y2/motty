//go:build darwin || windows || linux

package workbench

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

// TestWriteReadmeShots writes the gallery pictures in the READMEs: the
// dashboard's detailed overview and the transfer center. Both scenes are built
// from the same in-process fixtures the workbench tests use, so the pictures
// show data from this repository rather than a staged session.
//
// The command is opt-in because it writes files:
//
//	MOTTY_README_SHOTS=/tmp/readme go test -tags vivahead ./internal/workbench \
//	    -run '^TestWriteReadmeShots$' -count=1
//
// The frames are 1600x1000 points at 2x, saved at 1800x1125 (the size the other
// README pictures use) with:
//
//	sips -z 1125 1800 /tmp/readme/readme-dashboard.png --out assets/readme-dashboard.png
//	sips -z 1125 1800 /tmp/readme/readme-transfers.png --out assets/readme-transfers.png
//
// The wider-than-1560 frame keeps the transfer center's surface clear of the
// sidebar logo.
func TestWriteReadmeShots(t *testing.T) {
	dir := os.Getenv("MOTTY_README_SHOTS")
	if dir == "" {
		t.Skip("set MOTTY_README_SHOTS to write the README gallery shots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Run("dashboard", func(t *testing.T) {
		s := dashboardNativeFixture(t)
		s.setLanguage(i18n.English)
		s.dashboard.panels[0].Name = "Production"
		view, err := headless.Open(s.WindowConfig(app.Config{Title: "Motty", Width: 1600, Height: 1000, Tree: Tree(), Paint: s.Paint, OnPointer: s.Pointer, OnKey: s.Key, OnShortcut: s.Shortcut, OnScroll: s.Scroll, OnResize: s.Resize}), nil, headless.Options{Scale: 2})
		if err != nil {
			t.Fatal(err)
		}
		defer view.Close()
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(dir, "readme-dashboard.png")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("transfers", func(t *testing.T) {
		s := readmeTransferState(t)
		view, err := headless.Open(s.WindowConfig(app.Config{Title: "Motty", Width: 1600, Height: 1000, Tree: Tree(), Paint: s.Paint, OnPointer: s.Pointer, OnKey: s.Key, OnShortcut: s.Shortcut, OnScroll: s.Scroll, OnResize: s.Resize}), nil, headless.Options{Scale: 2})
		if err != nil {
			t.Fatal(err)
		}
		defer view.Close()
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(dir, "readme-transfers.png")); err != nil {
			t.Fatal(err)
		}
	})
}

// readmeTransferState is the transfer center scene: an upload and a download
// in flight, a paused archive, queued downloads, finished records and one
// retryable failure, over a host catalog of demo machines. Every row goes
// through describeTransfer, so the picture uses the same formatting the running
// app does.
func readmeTransferState(t *testing.T) *State {
	t.Helper()
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.setLanguage(i18n.English)
	state.setAppearance(appearanceDark)
	state.hosts = []Host{
		{ID: 1, CatalogID: "web-01", Label: "web-01", Address: "10.0.1.11", User: "deploy", Port: 22, Group: "Production", Tags: "nginx, web", System: sshconn.SystemUbuntu, Icon: hostIconOf(sshconn.SystemUbuntu), Color: "#3fb950"},
		{ID: 2, CatalogID: "web-02", Label: "web-02", Address: "10.0.1.12", User: "deploy", Port: 22, Group: "Production", Tags: "nginx, web", System: sshconn.SystemUbuntu, Icon: hostIconOf(sshconn.SystemUbuntu), Color: "#3fb950"},
		{ID: 3, CatalogID: "db-primary", Label: "db-primary", Address: "10.0.2.21", User: "postgres", Port: 22, Group: "Production", Tags: "postgres, database", System: sshconn.SystemDebian, Icon: hostIconOf(sshconn.SystemDebian), Color: "#d29922"},
		{ID: 4, CatalogID: "cache-01", Label: "cache-01", Address: "10.0.2.31", User: "redis", Port: 22, Group: "Production", Tags: "redis, cache", System: sshconn.SystemDebian, Icon: hostIconOf(sshconn.SystemDebian), Color: "#d29922"},
		{ID: 5, CatalogID: "build-runner", Label: "build-runner", Address: "10.0.3.41", User: "ci", Port: 22, Group: "CI", Tags: "ci", System: sshconn.SystemArch, Icon: hostIconOf(sshconn.SystemArch), Color: "#a371f7"},
		{ID: 6, CatalogID: "edge-proxy", Label: "edge-proxy", Address: "10.0.0.5", User: "ops", Port: 22, Group: "Staging", Tags: "proxy, nginx", System: sshconn.SystemUbuntu, Icon: hostIconOf(sshconn.SystemUbuntu), Color: "#388bfd"},
	}
	state.refreshHostGroups()
	state.refreshHostSelection()

	when := time.Date(2026, 11, 20, 9, 42, 0, 0, time.Local)
	add := func(id int, server, address string, snapshot sftpclient.TransferSnapshot) {
		entry := transferEntry{id: id, server: server, address: address, snapshot: snapshot}
		entry.snapshot.ID = uint64(id)
		if transferActive(entry.snapshot) || entry.snapshot.Retryable {
			entry.owner = &sftpView{}
		}
		state.describeTransfer(&entry)
		state.transfers.entries = append(state.transfers.entries, entry)
	}
	add(1, "web-01", "deploy@10.0.1.11:22", sftpclient.TransferSnapshot{
		Operation: sftpclient.OperationUpload, State: sftpclient.TransferRunning,
		Source: "/Users/deploy/Downloads/release-2026.11.20.tar.gz", Destination: "/srv/www/releases",
		BytesDone: 384 << 20, BytesTotal: 1 << 30, BytesPerSecond: 12 << 20,
		FilesDone: 1, FilesTotal: 1, QueuedAt: when, StartedAt: when,
	})
	add(2, "build-runner", "ci@10.0.3.41:22", sftpclient.TransferSnapshot{
		Operation: sftpclient.OperationUpload, State: sftpclient.TransferPaused,
		Source: "/Users/deploy/Downloads/build-cache.tar.zst", Destination: "/srv/ci/cache",
		BytesDone: 256 << 20, BytesTotal: 640 << 20,
		FilesDone: 1, FilesTotal: 1, QueuedAt: when.Add(4 * time.Minute), StartedAt: when.Add(4 * time.Minute),
	})
	add(3, "db-primary", "postgres@10.0.2.21:22", sftpclient.TransferSnapshot{
		Operation: sftpclient.OperationDownload, State: sftpclient.TransferQueued,
		Source: "/var/backups/db-primary-2026-11-20.sql.gz", Destination: "/Users/deploy/Downloads",
		BytesTotal: 86 << 20, FilesDone: 0, FilesTotal: 1, QueuedAt: when.Add(5 * time.Minute),
	})
	add(4, "web-02", "deploy@10.0.1.12:22", sftpclient.TransferSnapshot{
		Operation: sftpclient.OperationDownload, State: sftpclient.TransferRunning,
		Source: "/srv/www/shared/assets-2026.11.zip", Destination: "/Users/deploy/Downloads",
		BytesDone: 86 << 20, BytesTotal: 240 << 20, BytesPerSecond: 9 << 20,
		FilesDone: 1, FilesTotal: 1, QueuedAt: when.Add(2 * time.Minute), StartedAt: when.Add(2 * time.Minute),
	})
	add(5, "edge-proxy", "ops@10.0.0.5:22", sftpclient.TransferSnapshot{
		Operation: sftpclient.OperationDownload, State: sftpclient.TransferQueued,
		Source: "/var/log/nginx/access-2026-11-19.log.gz", Destination: "/Users/deploy/Downloads",
		BytesTotal: 512 << 20, FilesDone: 0, FilesTotal: 1, QueuedAt: when.Add(6 * time.Minute),
	})
	add(6, "web-01", "deploy@10.0.1.11:22", sftpclient.TransferSnapshot{
		Operation: sftpclient.OperationUpload, State: sftpclient.TransferCompleted,
		Source: "/Users/deploy/Downloads/nginx.conf", Destination: "/etc/nginx/nginx.conf",
		BytesDone: 4_608, BytesTotal: 4_608, FilesDone: 1, FilesTotal: 1,
		QueuedAt: when, StartedAt: when, FinishedAt: when.Add(2 * time.Second),
	})
	add(7, "db-primary", "postgres@10.0.2.21:22", sftpclient.TransferSnapshot{
		Operation: sftpclient.OperationDownload, State: sftpclient.TransferCompleted,
		Source: "/srv/app/deploy.sh", Destination: "/Users/deploy/Downloads",
		BytesDone: 2_304, BytesTotal: 2_304, FilesDone: 1, FilesTotal: 1,
		QueuedAt: when, StartedAt: when, FinishedAt: when.Add(3 * time.Second),
	})
	add(8, "web-02", "deploy@10.0.1.12:22", sftpclient.TransferSnapshot{
		Operation: sftpclient.OperationDownload, State: sftpclient.TransferCompleted,
		Source: "/srv/www/backups/site-2026-11-18.tar.gz", Destination: "/Users/deploy/Downloads",
		BytesDone: 1_932_735_283, BytesTotal: 1_932_735_283, FilesDone: 1, FilesTotal: 1,
		QueuedAt: when.Add(-12 * time.Minute), StartedAt: when.Add(-12 * time.Minute), FinishedAt: when.Add(-9 * time.Minute),
	})
	add(9, "edge-proxy", "ops@10.0.0.5:22", sftpclient.TransferSnapshot{
		Operation: sftpclient.OperationUpload, State: sftpclient.TransferCompleted,
		Source: "/Users/deploy/Downloads/redis.conf", Destination: "/etc/redis/redis.conf",
		BytesDone: 12_288, BytesTotal: 12_288, FilesDone: 1, FilesTotal: 1,
		QueuedAt: when.Add(-20 * time.Minute), StartedAt: when.Add(-20 * time.Minute), FinishedAt: when.Add(-20*time.Minute + 2*time.Second),
	})
	add(10, "cache-01", "redis@10.0.2.31:22", sftpclient.TransferSnapshot{
		Operation: sftpclient.OperationDownload, State: sftpclient.TransferFailed, Retryable: true,
		Source: "/var/log/redis-2026-11-19.tar.zst", Destination: "/Users/deploy/Downloads",
		BytesDone: 128 << 20, BytesTotal: 640 << 20, Error: "Connection reset by peer",
		FilesDone: 1, FilesTotal: 1, QueuedAt: when, StartedAt: when, FinishedAt: when.Add(47 * time.Second),
	})
	state.transfers.nextID = 10
	state.transfers.open = true
	state.describeTransferSummary()
	return state
}
