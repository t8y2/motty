//go:build windows || darwin || linux

package workbench

import (
	"os"
	"strings"
	"testing"
	"time"

	"motty/internal/sftpclient"
)

func setupLinuxRootFixture(t *testing.T, remote *workbenchSFTPRemote, when time.Time) {
	t.Helper()
	remote.mu.Lock()
	defer remote.mu.Unlock()
	remote.entries["/"] = []os.FileInfo{
		workbenchFileInfo{name: "bin", mode: os.ModeSymlink | 0o777, when: when},
		workbenchFileInfo{name: "boot", mode: os.ModeDir | 0o755, when: when},
		workbenchFileInfo{name: "etc", mode: os.ModeDir | 0o755, when: when},
		workbenchFileInfo{name: "home", mode: os.ModeDir | 0o755, when: when},
		workbenchFileInfo{name: "lib", mode: os.ModeSymlink | 0o777, when: when},
		workbenchFileInfo{name: "run", mode: os.ModeDir | 0o755, when: when},
		workbenchFileInfo{name: "sbin", mode: os.ModeSymlink | 0o777, when: when},
		workbenchFileInfo{name: "usr", mode: os.ModeDir | 0o755, when: when},
	}
	remote.entries["/etc"] = []os.FileInfo{
		workbenchFileInfo{name: "hosts", size: 42, mode: 0o644, when: when},
	}
	remote.entries["/usr"] = []os.FileInfo{
		workbenchFileInfo{name: "bin", mode: os.ModeDir | 0o755, when: when},
		workbenchFileInfo{name: "lib", mode: os.ModeDir | 0o755, when: when},
		workbenchFileInfo{name: "sbin", mode: os.ModeDir | 0o755, when: when},
	}
	remote.entries["/usr/bin"] = []os.FileInfo{
		workbenchFileInfo{name: "local", mode: os.ModeDir | 0o755, when: when},
	}
	remote.entries["/usr/lib"] = nil
	remote.entries["/usr/sbin"] = nil
	remote.entries["/boot"] = nil
	remote.entries["/run"] = nil
	if remote.links == nil {
		remote.links = make(map[string]string)
	}
	remote.links["/bin"] = "usr/bin"
	remote.links["/lib"] = "usr/lib"
	remote.links["/sbin"] = "usr/sbin"
}

func treeChildNames(node *sftpTreeNode) string {
	if node == nil {
		return ""
	}
	names := make([]string, len(node.children))
	for index, child := range node.children {
		names[index] = child.name
	}
	return strings.Join(names, ",")
}

func TestSFTPReturnToRootKeepsDirectoryTreeStableDuringPrefetchRefresh(t *testing.T) {
	state, w, remote, host, _ := dualSFTPFixture(t, 1)
	when := time.Unix(1_700_000_000, 0)
	setupLinuxRootFixture(t, remote, when)
	w.enableDirectoryTree(1, w.view.path)
	waitDirectoryTree(t, state, w, host.windows[0].view, func() bool {
		root := directoryTreeTestNode(w, false, "/")
		return root != nil && root.loaded && directoryTreeTestNode(w, false, "/bin") != nil && len(w.directoryTree[1].requests) == 0
	})

	// Open "/" so it is shown and cached with resolved folder links.
	w.navigate(false, "/")
	waitDirectoryTree(t, state, w, host.windows[0].view, func() bool {
		return !w.view.busy && w.view.path == "/" && len(w.view.rows) == 8 &&
			w.view.rows[0].entry.Name == "bin" && w.view.rows[0].entry.LinkDir
	})
	wantTree := "bin,boot,etc,home,lib,run,sbin,usr"
	root := directoryTreeTestNode(w, false, "/")
	if got := treeChildNames(root); got != wantTree {
		t.Fatalf("initial root tree children = %q, want %q", got, wantTree)
	}
	binNode := directoryTreeTestNode(w, false, "/bin")

	// Enter a folder from root, then slow link resolution before returning to root.
	w.navigate(false, "/etc")
	waitDirectoryTree(t, state, w, host.windows[0].view, func() bool {
		return !w.view.busy && w.view.path == "/etc" && len(w.view.prefetching) == 0
	})
	remote.mu.Lock()
	remote.linkDelay = 25 * time.Millisecond
	remote.mu.Unlock()

	beforeReads := remote.readCount("/")
	w.navigate(false, "/")
	if w.view.path != "/" {
		t.Fatalf("return to cached root left path at %q", w.view.path)
	}

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if err := host.windows[0].view.Driver().Pump(); err != nil {
			t.Fatal(err)
		}
		if got := treeChildNames(root); got != wantTree {
			t.Fatalf("returning to root transiently changed directory tree children to %q, want %q", got, wantTree)
		}
		if directoryTreeTestNode(w, false, "/bin") != binNode {
			t.Fatal("returning to root replaced the /bin directory tree node")
		}
		if remote.readCount("/") > beforeReads && len(w.view.prefetching) == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if remote.readCount("/") <= beforeReads || len(w.view.prefetching) != 0 {
		t.Fatalf("background refresh did not complete: reads=%d->%d prefetching=%v", beforeReads, remote.readCount("/"), w.view.prefetching)
	}
}

func TestSFTPReturnToRootKeepsFileListOrderStableWhenMtimeChanges(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	when := time.Unix(1_700_000_000, 0)
	setupLinuxRootFixture(t, remote, when)
	view := session.files

	// Visit "/" and wait until all symlinks are resolved and cached.
	state.navigateSFTPDirectory(view, "/")
	waitSFTP(t, state, session, func(v *sftpView) bool {
		return !v.busy && v.path == "/" && len(v.rows) == 8 && v.rows[0].entry.Name == "bin" && v.rows[0].entry.LinkDir
	})
	wantOrder := "bin,boot,etc,home,lib,run,sbin,usr"
	if got := strings.Join(rowNames(view.rows), ","); got != wantOrder {
		t.Fatalf("initial root row order = %q, want %q", got, wantOrder)
	}

	// Enter "/etc", then bump /run's ModTime (like /run or /tmp on live Linux)
	// and slow down symlink resolution before returning to "/".
	state.navigateSFTPDirectory(view, "/etc")
	waitSFTP(t, state, session, func(v *sftpView) bool {
		return !v.busy && v.path == "/etc" && len(v.prefetching) == 0
	})
	updatedWhen := when.Add(5 * time.Second)
	remote.mu.Lock()
	remote.linkDelay = 25 * time.Millisecond
	for index := range remote.entries["/"] {
		info := remote.entries["/"][index].(workbenchFileInfo)
		if info.name == "run" {
			info.when = updatedWhen
			remote.entries["/"][index] = info
		}
	}
	remote.mu.Unlock()

	beforeReads := remote.readCount("/")
	state.navigateSFTPDirectory(view, "/")
	if view.path != "/" {
		t.Fatalf("return to cached root left path at %q", view.path)
	}

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if got := strings.Join(rowNames(view.rows), ","); got != wantOrder {
			t.Fatalf("returning to root transiently reordered rows to %q, want %q", got, wantOrder)
		}
		runIdx := sftpRowNameIndex(view.rows, "run")
		if remote.readCount("/") > beforeReads && len(view.prefetching) == 0 && runIdx >= 0 && view.rows[runIdx].entry.ModTime.Equal(updatedWhen) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("background refresh did not apply updated /run ModTime without reordering: reads=%d->%d prefetching=%v", beforeReads, remote.readCount("/"), view.prefetching)
}

func TestSFTPReturnToRootWithReverseSyncDoesNotDoubleRefresh(t *testing.T) {
	state, session, fake, remote := openTestSFTP(t)
	when := time.Unix(1_700_000_000, 0)
	setupLinuxRootFixture(t, remote, when)
	state.Click(hitSettingsTerminalFollowToggle)
	shell := fake.shell(t)
	shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	view := session.files

	// Visit "/" and wait for resolved links.
	state.navigateSFTPDirectory(view, "/")
	waitSFTP(t, state, session, func(v *sftpView) bool {
		return !v.busy && v.path == "/" && len(v.rows) == 8 && v.rows[0].entry.Name == "bin" && v.rows[0].entry.LinkDir
	})
	shell.say(folderSyncMarker(session, "C") + "\x1b]133;D;0;motty=" + session.term.folderToken + "\a" +
		folderSyncMarker(session, "A") + "\x1b]7;file://box/\a" + folderSyncMarker(session, "B"))
	state.Wake()

	// Enter "/etc" and let shell complete the reverse-synced cd.
	state.navigateSFTPDirectory(view, "/etc")
	waitSFTP(t, state, session, func(v *sftpView) bool {
		return !v.busy && v.path == "/etc"
	})
	shell.say(folderSyncMarker(session, "C") + "\x1b]133;D;0;motty=" + session.term.folderToken + "\a" +
		folderSyncMarker(session, "A") + "\x1b]7;file://box/etc\a" + folderSyncMarker(session, "B"))
	state.Wake()

	// Slow link resolution and return to "/".
	remote.mu.Lock()
	remote.linkDelay = 25 * time.Millisecond
	remote.mu.Unlock()
	beforeReads := remote.readCount("/")
	wantOrder := "bin,boot,etc,home,lib,run,sbin,usr"

	state.navigateSFTPDirectory(view, "/")
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if view.path == "/" {
			if got := strings.Join(rowNames(view.rows), ","); got != wantOrder {
				t.Fatalf("returning to root with reverse sync transiently showed %q, want %q", got, wantOrder)
			}
		}
		if !view.busy && view.path == "/" && strings.HasSuffix(folderSyncInput(shell), "builtin cd -- '/'\r") {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// Shell executes the reverse-synced `builtin cd -- '/'`. That internal cd
	// must not trigger a second SFTP refresh of "/".
	readsAfterNav := remote.readCount("/")
	shell.say(folderSyncMarker(session, "C") + "\x1b]133;D;0;motty=" + session.term.folderToken + "\a" +
		folderSyncMarker(session, "A") + "\x1b]7;file://box/\a" + folderSyncMarker(session, "B"))
	for range 20 {
		state.Wake()
		if got := strings.Join(rowNames(view.rows), ","); got != wantOrder {
			t.Fatalf("post-cd tick transiently showed %q, want %q", got, wantOrder)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got := remote.readCount("/"); got != beforeReads+1 || got != readsAfterNav {
		t.Fatalf("returning to root with reverse sync read / %d times (before=%d afterNav=%d), want single read %d", got, beforeReads, readsAfterNav, beforeReads+1)
	}
}

func TestSFTPDirectoryTreeRefreshKeepsExpandedSymlinkBranch(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1)
	tree := &w.directoryTree[1]
	tree.enabled = true
	tree.rootPath = "/"
	root := &sftpTreeNode{path: "/", name: "/", loaded: true, expanded: true}
	bin := &sftpTreeNode{path: "/bin", name: "bin", loaded: true, expanded: true}
	local := &sftpTreeNode{path: "/bin/local", name: "local"}
	bin.children = []*sftpTreeNode{local}
	etc := &sftpTreeNode{path: "/etc", name: "etc"}
	root.children = []*sftpTreeNode{bin, etc}
	tree.roots = []*sftpTreeNode{root}
	tree.nodes = map[string]*sftpTreeNode{
		"/":          root,
		"/bin":       bin,
		"/bin/local": local,
		"/etc":       etc,
	}
	id := sftpDirectoryTreeRequestBit | 99
	tree.requests = map[uint64]string{id: "/"}
	root.loading = true
	root.requestID = id

	// Initial ReadDir result with LinksPending=true (bin is not yet known to be a dir).
	if !w.acceptDirectoryTreeResult(sftpclient.Result{
		Operation:    sftpclient.OperationList,
		Path:         "/",
		ListID:       id,
		Background:   true,
		LinksPending: true,
		Entries: []sftpclient.Entry{
			{Name: "etc", Mode: os.ModeDir | 0o755},
			{Name: "bin", Mode: os.ModeSymlink | 0o777},
		},
	}) {
		t.Fatal("pending tree result was not accepted")
	}
	if tree.nodes["/bin"] != bin || !bin.expanded || tree.nodes["/bin/local"] != local {
		t.Fatal("pending unresolved tree listing pruned an existing symlink folder branch")
	}

	// FollowUp arrives with resolved symlink target.
	if !w.acceptDirectoryTreeResult(sftpclient.Result{
		Operation:  sftpclient.OperationList,
		Path:       "/",
		ListID:     id,
		Background: true,
		FollowUp:   true,
		Entries: []sftpclient.Entry{
			{Name: "bin", Mode: os.ModeSymlink | 0o777, LinkTarget: "usr/bin", LinkDir: true},
			{Name: "etc", Mode: os.ModeDir | 0o755},
		},
	}) {
		t.Fatal("follow-up tree result was not accepted")
	}
	if tree.nodes["/bin"] != bin || !bin.expanded || tree.nodes["/bin/local"] != local || len(tree.requests) != 0 {
		t.Fatal("resolved follow-up did not preserve expanded symlink branch or clear request")
	}
}
