package sftpclient

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func listAll(t *testing.T, worker *Worker, remote string) []Entry {
	t.Helper()
	result := requireQueuedResult(t, worker.List(remote), worker, OperationList)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if result.LinksPending {
		follow := waitResult(t, worker)
		if follow.Err != nil || !follow.FollowUp || follow.Path != result.Path {
			t.Fatalf("follow-up = %+v, first = %+v", follow, result)
		}
		result = follow
	}
	return result.Entries
}

func entryNamed(t *testing.T, entries []Entry, name string) Entry {
	t.Helper()
	for _, entry := range entries {
		if entry.Name == name {
			return entry
		}
	}
	t.Fatalf("no entry %q in %+v", name, entries)
	return Entry{}
}

func TestListingCarriesTheOwnerAndTheNamesTheAccountFilesGive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no numeric owners")
	}
	worker, root := newProtocolWorker(t)
	os.WriteFile(filepath.Join(root, "mine.txt"), []byte("x"), 0o644)
	entry := entryNamed(t, listAll(t, worker, root), "mine.txt")
	if !entry.HasOwner || entry.UID != uint32(os.Getuid()) || entry.GID != uint32(os.Getgid()) {
		t.Fatalf("owner = %+v, want %d:%d", entry, os.Getuid(), os.Getgid())
	}
	// The test server reads this machine's own /etc/passwd, so a name appears
	// wherever the account is listed there.
	if data, err := os.ReadFile("/etc/passwd"); err == nil && strings.Contains(string(data), ":"+itoa(os.Getuid())+":") {
		if entry.Owner == "" {
			t.Fatalf("no owner name though /etc/passwd lists uid %d", os.Getuid())
		}
	}
}

func itoa(value int) string { return strconv.Itoa(value) }

func TestAccountFilesParseToNames(t *testing.T) {
	names := parseIDFile(strings.NewReader("# comment\nroot:x:0:0:root:/root:/bin/bash\nweb:x:33:33::/var/www:/usr/sbin/nologin\nweb2:x:33:33::/x:/bin/sh\n+@netgroup::::::\nbad:x:notanumber:0\n::1:\n"))
	if names[0] != "root" || names[33] != "web" || len(names) != 2 {
		t.Fatalf("names = %v", names)
	}
}

func TestSymbolicLinksAreCreatedAndResolved(t *testing.T) {
	requireSymlinkSupport(t)
	worker, root := newProtocolWorker(t)
	os.Mkdir(filepath.Join(root, "real"), 0o755)
	os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644)
	if result := requireQueuedResult(t, worker.Symlink("to-dir", "real"), worker, OperationSymlink); result.Err != nil {
		t.Fatal(result.Err)
	}
	requireQueuedResult(t, worker.Symlink("to-file", "file.txt"), worker, OperationSymlink)
	requireQueuedResult(t, worker.Symlink("dangling", "nowhere"), worker, OperationSymlink)
	if target, err := os.Readlink(filepath.Join(root, "to-dir")); err != nil || !strings.HasSuffix(target, "real") {
		t.Fatalf("link target = %q, %v", target, err)
	}

	entries := listAll(t, worker, ".")
	dir := entryNamed(t, entries, "to-dir")
	if !dir.IsLink() || !dir.LinkDir || !dir.IsDir() || !strings.HasSuffix(dir.LinkTarget, "real") || dir.LinkBroken {
		t.Fatalf("link to a folder = %+v", dir)
	}
	file := entryNamed(t, entries, "to-file")
	if !file.IsLink() || file.LinkDir || file.IsDir() || !strings.HasSuffix(file.LinkTarget, "file.txt") {
		t.Fatalf("link to a file = %+v", file)
	}
	if dangling := entryNamed(t, entries, "dangling"); !dangling.LinkBroken || !strings.HasSuffix(dangling.LinkTarget, "nowhere") {
		t.Fatalf("dangling link = %+v", dangling)
	}
	if plain := entryNamed(t, entries, "file.txt"); plain.IsLink() || plain.LinkTarget != "" {
		t.Fatalf("a plain file looks like a link: %+v", plain)
	}

	if result := requireQueuedResult(t, worker.Symlink("", "x"), worker, OperationSymlink); result.Err == nil {
		t.Fatal("a link with no name was created")
	}
}

func TestTransfersKeepModificationTimesUnlessTold(t *testing.T) {
	worker, root := newProtocolWorker(t)
	stamp := time.Unix(1_500_000_000, 0)
	local := filepath.Join(t.TempDir(), "old.txt")
	os.WriteFile(local, []byte("content"), 0o640)
	os.Chtimes(local, stamp, stamp)

	id, queued := worker.QueueUpload(local, "up.txt", false, ExistingFail) // relative: the server starts in root
	if result := queueResult(t, id, queued, worker); result.Err != nil {
		t.Fatal(result.Err)
	}
	if info, _ := os.Stat(filepath.Join(root, "up.txt")); !info.ModTime().Equal(stamp) {
		t.Fatalf("uploaded file time = %v, want %v", info.ModTime(), stamp)
	}

	back := filepath.Join(t.TempDir(), "down.txt")
	id, queued = worker.QueueDownload("up.txt", back, false, ExistingFail)
	if result := queueResult(t, id, queued, worker); result.Err != nil {
		t.Fatal(result.Err)
	}
	if info, _ := os.Stat(back); !info.ModTime().Equal(stamp) {
		t.Fatalf("downloaded file time = %v, want %v", info.ModTime(), stamp)
	}

	worker.SetPreserveTimes(false)
	plain := filepath.Join(t.TempDir(), "plain.txt")
	id, queued = worker.QueueDownload("up.txt", plain, false, ExistingFail)
	queueResult(t, id, queued, worker)
	if info, _ := os.Stat(plain); info.ModTime().Equal(stamp) {
		t.Fatal("the time was kept though it was turned off")
	}
}

func TestSkippingExistingFilesMergesFoldersWithoutOverwriting(t *testing.T) {
	worker, root := newProtocolWorker(t)
	local := filepath.Join(t.TempDir(), "project")
	os.MkdirAll(filepath.Join(local, "sub"), 0o755)
	os.WriteFile(filepath.Join(local, "keep.txt"), []byte("NEW keep"), 0o644)
	os.WriteFile(filepath.Join(local, "sub", "added.txt"), []byte("added"), 0o644)
	remote := filepath.Join(root, "project") // where the files are on this computer
	os.MkdirAll(remote, 0o755)
	os.WriteFile(filepath.Join(remote, "keep.txt"), []byte("OLD keep"), 0o644)

	id, queued := worker.QueueUpload(local, "project", true, ExistingFail)
	if result := queueResult(t, id, queued, worker); result.Err == nil {
		t.Fatal("an existing folder was not reported as a conflict")
	}
	id, queued = worker.QueueUpload(local, "project", true, ExistingSkip)
	result := queueResult(t, id, queued, worker)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if got, _ := os.ReadFile(filepath.Join(remote, "keep.txt")); string(got) != "OLD keep" {
		t.Fatalf("an existing file was overwritten: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(remote, "sub", "added.txt")); string(got) != "added" {
		t.Fatalf("a missing file was not copied: %q", got)
	}
	if snapshot := snapshotOf(worker, id); snapshot.BytesDone != snapshot.BytesTotal {
		t.Fatalf("progress %d of %d: skipped files count as done", snapshot.BytesDone, snapshot.BytesTotal)
	}

	// The same for a download.
	back := filepath.Join(t.TempDir(), "project")
	os.MkdirAll(back, 0o755)
	os.WriteFile(filepath.Join(back, "keep.txt"), []byte("LOCAL keep"), 0o644)
	id, queued = worker.QueueDownload("project", back, true, ExistingSkip)
	if result := queueResult(t, id, queued, worker); result.Err != nil {
		t.Fatal(result.Err)
	}
	if got, _ := os.ReadFile(filepath.Join(back, "keep.txt")); string(got) != "LOCAL keep" {
		t.Fatalf("a local file was overwritten: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(back, "sub", "added.txt")); string(got) != "added" {
		t.Fatalf("a missing local file was not fetched: %q", got)
	}
}

func TestAListingArrivesBeforeItsLinksResolve(t *testing.T) {
	requireSymlinkSupport(t)
	worker, root := newProtocolWorker(t)
	os.Mkdir(filepath.Join(root, "real"), 0o755)
	os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644)
	if result := requireQueuedResult(t, worker.Symlink("to-dir", "real"), worker, OperationSymlink); result.Err != nil {
		t.Fatal(result.Err)
	}

	if !worker.List(".") {
		t.Fatal("list was not queued")
	}
	first := waitResult(t, worker)
	if first.Err != nil || !first.LinksPending || first.FollowUp {
		t.Fatalf("first result = %+v", first)
	}
	if link := entryNamed(t, first.Entries, "to-dir"); link.LinkTarget != "" || link.LinkDir {
		t.Fatalf("the first result already resolved the link: %+v", link)
	}
	follow := waitResult(t, worker)
	if follow.Err != nil || !follow.FollowUp || follow.Path != first.Path || follow.LinksPending {
		t.Fatalf("follow-up = %+v", follow)
	}
	if link := entryNamed(t, follow.Entries, "to-dir"); !strings.HasSuffix(link.LinkTarget, "real") || !link.LinkDir {
		t.Fatalf("the follow-up did not resolve the link: %+v", link)
	}
}

func requireSymlinkSupport(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	if err := os.Symlink(root, filepath.Join(root, "probe")); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skipf("Windows account lacks symbolic-link privilege: %v", err)
		}
		t.Fatal(err)
	}
}

func TestAListingWithoutLinksHasNoFollowUp(t *testing.T) {
	worker, root := newProtocolWorker(t)
	os.WriteFile(filepath.Join(root, "plain.txt"), []byte("x"), 0o644)
	result := requireQueuedResult(t, worker.List("."), worker, OperationList)
	if result.Err != nil || result.LinksPending {
		t.Fatalf("result = %+v", result)
	}
	select {
	case extra := <-worker.Results():
		t.Fatalf("unexpected follow-up: %+v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}
