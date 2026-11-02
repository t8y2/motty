package sftpclient

import (
	"context"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

// serveSFTP runs a real SFTP server over a pipe, so a walk is read through the
// protocol's own listing, errors and file types rather than a stand-in for them.
func serveSFTP(t *testing.T) *sftp.Client {
	t.Helper()
	clientConnection, serverConnection := net.Pipe()
	server, err := sftp.NewServer(io.ReadWriteCloser(serverConnection))
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(); _ = server.Close() }()
	client, err := sftp.NewClientPipe(clientConnection, clientConnection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func TestWalkOverARealSFTPServer(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"docs/Guide.md", "docs/api/guide-v2.md", "notes.txt", "locked/secret.txt"} {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	unreadable := runtime.GOOS != "windows" && os.Geteuid() != 0
	if unreadable {
		if err := os.Chmod(filepath.Join(root, "locked"), 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "locked"), 0o755) })
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(root, filepath.Join(root, "docs", "back")); err != nil {
			t.Fatal(err)
		}
	}

	client := serveSFTP(t)
	got, summary := walkAll(t, remote{Client: client}, filepath.ToSlash(root), WalkOptions{MaxEntries: 100, MaxDepth: 5})
	want := []string{"docs", "locked", "notes.txt", "docs/Guide.md", "docs/api", "docs/back", "docs/api/guide-v2.md"}
	if runtime.GOOS == "windows" {
		want = slices.DeleteFunc(want, func(name string) bool { return name == "docs/back" })
	}
	if unreadable {
		if summary.Failed != 1 || summary.RootErr != nil {
			t.Fatalf("an unreadable folder was not counted on its own: %+v", summary)
		}
	} else {
		want = append(want, "locked/secret.txt")
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) || summary.Truncated || summary.Canceled {
		t.Fatalf("got %q, want %q, summary %+v", got, want, summary)
	}
	// A folder that is not there fails the whole walk, and says why.
	_, summary = walkAll(t, remote{Client: client}, path.Join(filepath.ToSlash(root), "missing"), WalkOptions{MaxEntries: 100, MaxDepth: 5})
	if summary.RootErr == nil {
		t.Fatalf("a missing root was not reported: %+v", summary)
	}
}

// Closing the channel is how a cancelled search gets free of a read under way.
func TestWalkEndsWhenItsChannelIsClosedUnderIt(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c", "d"} {
		if err := os.MkdirAll(filepath.Join(root, name, "deeper"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	client := serveSFTP(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan WalkSummary, 1)
	go func() {
		done <- Walk(ctx, remote{Client: client}, filepath.ToSlash(root), WalkOptions{Join: joinSlash, MaxEntries: 100, MaxDepth: 5, Parallel: 2}, func(found []Found) {
			cancel()
			client.Close()
		})
	}()
	select {
	case summary := <-done:
		if !summary.Canceled {
			t.Fatalf("summary %+v", summary)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the walk did not end after its channel was closed")
	}
}
