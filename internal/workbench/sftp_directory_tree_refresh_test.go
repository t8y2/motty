//go:build windows || darwin || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/vivaui/viva/app"
	"motty/internal/sftpclient"
)

func enableRefreshTestTrees(t *testing.T, state *State, w *sftpWindow, remote *workbenchSFTPRemote, host *sftpNativeHost) {
	t.Helper()
	remote.mu.Lock()
	remote.entries["/"] = []os.FileInfo{workbenchFileInfo{name: "home", mode: os.ModeDir | 0o755}}
	remote.entries["/home"] = []os.FileInfo{workbenchFileInfo{name: "test", mode: os.ModeDir | 0o755}}
	remote.mu.Unlock()
	w.enableDirectoryTree(0, w.view.lfolder.path)
	w.enableDirectoryTree(1, w.view.path)
	waitDirectoryTree(t, state, w, host.windows[0].view, func() bool {
		local := directoryTreeTestNode(w, true, w.view.lfolder.path)
		current := directoryTreeTestNode(w, false, w.view.path)
		return local != nil && local.loaded && current != nil && current.loaded && len(w.directoryTree[1].requests) == 0
	})
}

func TestSFTPDirectoryTreeReexpandRereadsEmptyLocalAndRemoteFolders(t *testing.T) {
	state, w, remote, host, local := dualSFTPFixture(t, 1)
	enableRefreshTestTrees(t, state, w, remote, host)
	for _, localSide := range []bool{true, false} {
		t.Run(map[bool]string{true: "local", false: "remote"}[localSide], func(t *testing.T) {
			location := "/home/test/docs"
			if localSide {
				location = filepath.Join(local, "tree")
			}
			side := sideIndex(localSide)
			node := directoryTreeTestNode(w, localSide, location)
			w.flattenDirectoryTree(side)
			sftpTreeActivate(t, w, w.directoryTree[side].tree.DisclosureID(directoryTreeIndex(w, localSide, location)))
			waitDirectoryTree(t, state, w, host.windows[0].view, func() bool { return node.loaded && !node.loading })
			if !w.directoryTree[side].tree.Node(directoryTreeIndex(w, localSide, location)).Branch {
				t.Fatal("a loaded empty folder cannot be reopened to discover external children")
			}
			sftpTreeActivate(t, w, w.directoryTree[side].tree.DisclosureID(directoryTreeIndex(w, localSide, location)))
			if localSide {
				if err := os.Mkdir(filepath.Join(location, "external"), 0o700); err != nil {
					t.Fatal(err)
				}
			} else if err := remote.Mkdir(location + "/external"); err != nil {
				t.Fatal(err)
			}
			sftpTreeActivate(t, w, w.directoryTree[side].tree.DisclosureID(directoryTreeIndex(w, localSide, location)))
			waitDirectoryTree(t, state, w, host.windows[0].view, func() bool { return node.loaded && !node.loading })
			if len(node.children) != 1 || node.children[0].name != "external" {
				t.Fatalf("reopened folder retained stale children: %+v", node.children)
			}
		})
	}
}

func TestSFTPDirectoryTreeRemoteMutationInvalidatesCachedNodesAndOldReads(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1)
	tree := &w.directoryTree[1]
	tree.enabled = true
	node := &sftpTreeNode{path: "/cached", loaded: true, loading: true, expanded: true}
	id := sftpDirectoryTreeRequestBit | 37
	node.requestID = id
	tree.nodes = map[string]*sftpTreeNode{node.path: node}
	tree.requests = map[uint64]string{id: node.path}
	tree.selectedPath = node.path
	w.view.forgetListings()
	if node.loaded || node.loading || len(tree.requests) != 0 {
		t.Fatal("remote mutation retained the directory tree cache or pending read")
	}
	if !node.expanded || tree.selectedPath != node.path || tree.nodes[node.path] != node {
		t.Fatal("cache invalidation discarded expansion, selection or node identity")
	}
	if w.acceptDirectoryTreeResult(sftpclient.Result{Operation: sftpclient.OperationList, Path: node.path, ListID: id}) {
		t.Fatal("a tree read started before the mutation was accepted")
	}
}

func TestSFTPDirectoryTreeRefreshPreservesTheRevealIntent(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1)
	tree := &w.directoryTree[1]
	tree.enabled = true
	node := &sftpTreeNode{path: "/cached", expanded: true}
	tree.roots = []*sftpTreeNode{node}
	tree.nodes = map[string]*sftpTreeNode{node.path: node}
	tree.revealSelected = false

	w.loadExpandedDirectoryTree(false)
	if tree.revealSelected {
		t.Fatal("a background tree refresh changed the user's reveal intent")
	}
	w.clearDirectoryTreeRequests()
}

func TestReplacingAnSFTPWorkerDropsOldDirectoryTreeRequests(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1)
	tree := &w.directoryTree[1]
	tree.enabled = true
	node := &sftpTreeNode{path: "/cached", loading: true, requestID: sftpDirectoryTreeRequestBit | 41}
	tree.nodes = map[string]*sftpTreeNode{node.path: node}
	tree.requests = map[uint64]string{node.requestID: node.path}
	generation := tree.generation

	w.clearDirectoryTreeRequests()
	if tree.generation == generation || len(tree.requests) != 0 || node.loading {
		t.Fatalf("old worker request was not retired: generation=%d->%d requests=%v loading=%v", generation, tree.generation, tree.requests, node.loading)
	}
}

func TestRestartingTheSFTPBrowserRetiresRequestsFromItsOldWorker(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1)
	oldWorker := w.view.worker
	oldWorker.Close()
	w.view.worker = nil
	tree := &w.directoryTree[1]
	tree.enabled = true
	id := sftpDirectoryTreeRequestBit | 42
	node := &sftpTreeNode{path: "/old-worker", loading: true, requestID: id}
	tree.nodes = map[string]*sftpTreeNode{node.path: node}
	tree.roots = []*sftpTreeNode{node}
	tree.requests = map[uint64]string{id: node.path}
	generation := tree.generation

	w.startBrowser()
	if tree.generation == generation || tree.requests[id] != "" || node.loading {
		t.Fatalf("browser restart retained an old worker request: generation=%d->%d requests=%v loading=%v", generation, tree.generation, tree.requests, node.loading)
	}
}

func TestSFTPDirectoryTreeNewerListingSupersedesRemoteDisclosure(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1)
	tree := &w.directoryTree[1]
	tree.enabled = true
	id := sftpDirectoryTreeRequestBit | 38
	node := &sftpTreeNode{path: "/cached", loading: true, requestID: id}
	tree.nodes = map[string]*sftpTreeNode{node.path: node}
	tree.requests = map[uint64]string{id: node.path}
	w.observeDirectoryTreeListing(false, node.path, []sftpclient.Entry{{Name: "new", Mode: os.ModeDir}})
	w.acceptDirectoryTreeResult(sftpclient.Result{Operation: sftpclient.OperationList, Path: node.path, ListID: id})
	if len(node.children) != 1 || node.children[0].name != "new" {
		t.Fatal("an older disclosure read overwrote the newer main listing")
	}
}

func TestSFTPDirectoryTreeListingKeepsAnotherNodesRead(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1)
	tree := &w.directoryTree[1]
	tree.enabled = true
	id := sftpDirectoryTreeRequestBit | 39
	// A retired node's revision can equal another node's newer worker ID.
	node := &sftpTreeNode{path: "/cached", requestID: id}
	tree.nodes = map[string]*sftpTreeNode{node.path: node}
	tree.requests = map[uint64]string{id: "/other"}
	w.observeDirectoryTreeListing(false, node.path, nil)
	if tree.requests[id] != "/other" {
		t.Fatal("observing a node canceled another node's background read")
	}
}

func TestSFTPWindowActivationRefreshesExternalChangesWithoutPolling(t *testing.T) {
	state, w, remote, host, local := dualSFTPFixture(t, 1)
	enableRefreshTestTrees(t, state, w, remote, host)
	currentLocal := directoryTreeTestNode(w, true, local)
	currentRemote := directoryTreeTestNode(w, false, w.view.path)
	currentLocal.expanded, currentRemote.expanded = false, false
	localSelection := filepath.Join(currentLocal.path, "tree")
	w.directoryTree[0].selectedPath = localSelection
	w.directoryTree[1].selectedPath = "/home/test/docs"
	if err := os.Mkdir(filepath.Join(local, "external"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := remote.Mkdir("/home/test/external"); err != nil {
		t.Fatal(err)
	}
	before := remote.readCount(w.view.path)
	for range 3 {
		state.Wake()
	}
	if remote.readCount(w.view.path) != before {
		t.Fatal("idle wakes polled the remote folder")
	}
	if err := host.windows[0].view.Driver().ActivateWindow(); err != nil {
		t.Fatal(err)
	}
	waitDirectoryTree(t, state, w, host.windows[0].view, func() bool {
		return directoryTreeTestNode(w, true, filepath.Join(local, "external")) != nil && directoryTreeTestNode(w, false, "/home/test/external") != nil
	})
	if remote.readCount(w.view.path) <= before {
		t.Fatal("native window activation did not reread the remote folder")
	}
	if currentLocal.expanded || currentRemote.expanded || w.directoryTree[0].selectedPath != localSelection || w.directoryTree[1].selectedPath != "/home/test/docs" {
		t.Fatal("activation replaced the user's collapsed branches or tree selection")
	}
}

func TestSFTPWindowActivationKeepsLocalSelectionAndOffset(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1)
	folder := &w.view.lfolder
	if len(folder.rows) < 2 {
		t.Fatalf("the fixture needs two local rows, has %d", len(folder.rows))
	}
	folder.first = 1
	folder.selectRow(0, 0)
	topName, selectedName := folder.rows[1].entry.Name, folder.rows[0].entry.Name
	wantOffset := float32(folder.first) * sftpRowHeight
	w.ui.offset[0] = wantOffset
	w.activate()
	if len(folder.rows) < 2 {
		t.Fatalf("activation replaced the local rows: %d", len(folder.rows))
	}
	if got := sftpRowNameIndex(folder.rows, topName); folder.first != got {
		t.Fatalf("activation moved the local offset from row %q to %d", topName, folder.first)
	}
	if got := sftpRowNameIndex(folder.rows, selectedName); folder.selected != got || got < 0 || !folder.rows[got].selected {
		t.Fatalf("activation lost the local selection of %q: selected=%d", selectedName, folder.selected)
	}
	if w.ui.offset[0] != wantOffset {
		t.Fatalf("activation moved the local scroll offset to %v, want %v", w.ui.offset[0], wantOffset)
	}
}

func TestSFTPDirectoryTreeRefreshResolvesLocalAlias(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1)
	parent := t.TempDir()
	canonical := filepath.Join(parent, "canonical")
	if err := os.Mkdir(canonical, 0o700); err != nil {
		t.Fatal(err)
	}
	aliasRoot := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(parent, aliasRoot); err != nil {
		t.Skipf("the platform cannot create the directory alias this test needs: %v", err)
	}
	// A Windows panel can name a folder by an 8.3 alias while the tree holds
	// the long name its listing recorded. Refresh the node through the alias
	// without asking the selected path for help.
	tree := &w.directoryTree[0]
	tree.enabled = true
	root := &sftpTreeNode{path: aliasRoot, name: "root", loaded: true, expanded: true}
	node := &sftpTreeNode{path: canonical, name: "canonical", loaded: true, expanded: true}
	root.children = []*sftpTreeNode{node}
	tree.roots = []*sftpTreeNode{root}
	tree.nodes = map[string]*sftpTreeNode{
		directoryTreePathKey(true, aliasRoot): root,
		directoryTreePathKey(true, canonical): node,
	}
	w.observeDirectoryTreeListing(true, filepath.Join(aliasRoot, "canonical"), []sftpclient.Entry{{Name: "new", Mode: os.ModeDir}})
	child := tree.nodes[directoryTreePathKey(true, filepath.Join(canonical, "new"))]
	if child == nil || child.name != "new" {
		t.Fatalf("refresh through an alias did not record the child under its canonical path: %+v", node.children)
	}
}

func TestSFTPTabReturnRefreshesExternalChanges(t *testing.T) {
	state, source, w, remote, local := openSFTPTabFixture(t)
	if err := os.Mkdir(filepath.Join(local, "external"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := remote.Mkdir("/home/test/external"); err != nil {
		t.Fatal(err)
	}
	state.activeSection = sectionWorkspaces
	if !state.activateSFTPTab(source.ID) {
		t.Fatal("file tab could not be reactivated")
	}
	waitSFTPTab(t, state, w, func() bool {
		return len(w.view.lfolder.rows) == 2 && len(w.view.rows) == 3
	})
	before := remote.readCount(w.view.path)
	state.activateSFTPTab(source.ID)
	if remote.readCount(w.view.path) != before || w.view.busy {
		t.Fatal("activating the already active tab restarted its listing")
	}
}

func TestSFTPDirectoryTreeNewerListingSupersedesLocalDisclosure(t *testing.T) {
	_, w, _, _, directory := dualSFTPFixture(t, 1)
	tree := &w.directoryTree[0]
	tree.enabled = true
	node := &sftpTreeNode{path: directory, loading: true, requestID: 7}
	tree.nodes = map[string]*sftpTreeNode{directoryTreePathKey(true, directory): node}
	w.observeDirectoryTreeListing(true, directory, []sftpclient.Entry{{Name: "new", Mode: os.ModeDir}})
	w.applyLocalDirectoryTreeResult(tree, node, tree.generation, 7, directory, nil, false, nil)
	if len(node.children) != 1 || node.children[0].name != "new" {
		t.Fatal("an older local disclosure read overwrote the newer main listing")
	}
	// A second read of the same node also supersedes the first, even before
	// either result has reached the window thread.
	node.requestID, node.loading = 9, true
	w.applyLocalDirectoryTreeResult(tree, node, tree.generation, 8, directory, nil, false, nil)
	if !node.loading || len(node.children) != 1 {
		t.Fatal("an older read finished a newer read of the same node")
	}
	w.closed = true
	defer func() { w.closed = false }()
	w.applyLocalDirectoryTreeResult(tree, node, tree.generation, 9, directory, nil, false, nil)
	if !node.loading || len(node.children) != 1 {
		t.Fatal("a closed window accepted a late local read")
	}
}

func TestSFTPWindowActivationLeavesInteractionsInCharge(t *testing.T) {
	for _, mode := range []string{"closed", "dialog", "path", "local-path", "editor", "drag", "foreign-drag", "bookmark", "properties", "navigation", "local-navigation", "local-operation", "disconnected"} {
		t.Run(mode, func(t *testing.T) {
			_, w, remote, host, local := dualSFTPFixture(t, 1)
			defer func() { w.closed = false }()
			if err := os.Mkdir(filepath.Join(local, "external"), 0o700); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "closed":
				w.closed = true
			case "dialog":
				w.view.dialog = sftpDialogCreateDirectory
			case "path":
				w.view.pathEditing = true
			case "local-path":
				w.view.lfolder.pathEditing = true
			case "editor":
				w.view.editPath = "/home/test/readme.txt"
			case "drag":
				w.drag.armed = true
			case "foreign-drag":
				w.foreign.active = true
			case "bookmark":
				w.bookmark.open = true
			case "properties":
				w.fileProperties.open = true
			case "navigation":
				w.view.busy = true
			case "local-navigation":
				w.pendingLocal = local
			case "local-operation":
				w.view.localBusy = true
			case "disconnected":
				w.session.conn = nil
			}
			beforeReads, beforeSeq := remote.readCount(w.view.path), w.view.localSeq
			host.windows[0].config.OnActivate()
			if remote.readCount(w.view.path) != beforeReads || w.view.localSeq != beforeSeq {
				t.Fatal("activation refreshed rows during an interaction or navigation")
			}
		})
	}
}

func TestSFTPTabWindowActivationPreservesExistingCallbackAndModal(t *testing.T) {
	state, _, w, _, _ := openSFTPTabFixture(t)
	called := 0
	config := state.WindowConfig(app.Config{OnActivate: func() { called++ }})
	before := w.view.navigationID
	state.modal = modalNewHost
	config.OnActivate()
	if called != 1 || w.view.navigationID != before {
		t.Fatal("main-window activation replaced the callback or refreshed behind a modal")
	}
	state.modal = modalNone
	config.OnActivate()
	if called != 2 || w.view.navigationID == before {
		t.Fatal("main-window activation did not chain the callback and refresh the visible file tab")
	}
}

func TestSFTPDirectoryTreeAppCreatesRefreshBothSides(t *testing.T) {
	state, w, remote, host, local := dualSFTPFixture(t, 1)
	enableRefreshTestTrees(t, state, w, remote, host)
	for _, localSide := range []bool{true, false} {
		w.activeLocal, w.view.local = localSide, localSide
		state.openSFTPDialog(w.view, sftpDialogCreateDirectory)
		w.view.dialogEditor.Insert("in-app")
		state.submitSFTPDialog(w.view)
		location := "/home/test/in-app"
		if localSide {
			location = filepath.Join(local, "in-app")
		}
		waitDirectoryTree(t, state, w, host.windows[0].view, func() bool {
			return !w.navigationBusy() && directoryTreeTestNode(w, localSide, location) != nil
		})
	}
}

func TestSFTPDirectoryTreeRefreshLeavesRoomForNavigation(t *testing.T) {
	state, w, remote, host, _ := dualSFTPFixture(t, 1)
	enableRefreshTestTrees(t, state, w, remote, host)
	directory := w.view.path
	entries := []sftpclient.Entry{{Name: "docs", Mode: os.ModeDir}}
	for index := range 40 { // More expanded branches than the worker's 32 slots.
		name := fmt.Sprintf("branch-%02d", index)
		if err := remote.Mkdir(directory + "/" + name); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, sftpclient.Entry{Name: name, Mode: os.ModeDir})
	}
	w.observeDirectoryTreeListing(false, directory, entries)
	for index := range 40 {
		location := fmt.Sprintf("%s/branch-%02d", directory, index)
		node := directoryTreeTestNode(w, false, location)
		node.expanded, node.loaded = true, true
		if err := remote.Mkdir(location + "/external"); err != nil {
			t.Fatal(err)
		}
	}
	started, release := make(chan struct{}), make(chan struct{})
	var startOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	remote.mu.Lock()
	remote.unreadable = map[string]bool{directory + "/branch-00": true}
	remote.onRead = func(location string) {
		if strings.HasPrefix(location, directory+"/branch-") {
			startOnce.Do(func() { close(started); <-release })
		}
	}
	remote.mu.Unlock()
	w.refresh(false)
	waitDirectoryTree(t, state, w, host.windows[0].view, func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	})
	tree := &w.directoryTree[1]
	if len(tree.requests) > 1 {
		t.Errorf("automatic refresh queued %d tree reads behind a blocked server", len(tree.requests))
	}
	for index := range 40 {
		node := directoryTreeTestNode(w, false, fmt.Sprintf("%s/branch-%02d", directory, index))
		if node.err != "" {
			t.Errorf("queue pressure reported a directory error: %s: %s", node.path, node.err)
		}
	}
	w.navigate(false, directory+"/docs")
	if !w.view.busy {
		t.Error("automatic tree refresh filled the queue and prevented user navigation")
	}
	unblock()
	waitDirectoryTree(t, state, w, host.windows[0].view, func() bool {
		if w.view.busy || w.view.path != directory+"/docs" || len(tree.requests) != 0 {
			return false
		}
		for index := range 40 {
			node := directoryTreeTestNode(w, false, fmt.Sprintf("%s/branch-%02d", directory, index))
			if !node.loaded || node.loading {
				return false
			}
			if index == 0 {
				if node.err == "" {
					return false
				}
			} else if len(node.children) != 1 || node.children[0].name != "external" {
				return false
			}
		}
		return true
	})
}

func TestSFTPDirectoryTreeNavigationFollowsInvalidatedCollapsedAncestors(t *testing.T) {
	for _, localSide := range []bool{true, false} {
		t.Run(map[bool]string{true: "local", false: "remote"}[localSide], func(t *testing.T) {
			state, w, remote, host, local := dualSFTPFixture(t, 1)
			enableRefreshTestTrees(t, state, w, remote, host)
			original, parent := w.view.path, w.view.path+"/docs"
			if localSide {
				original, parent = local, filepath.Join(local, "tree")
			}
			target := parent + "/target"
			if localSide {
				target = filepath.Join(parent, "target")
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if err := remote.Mkdir(target); err != nil {
				t.Fatal(err)
			}
			side := sideIndex(localSide)
			settled := func() bool {
				for _, node := range w.directoryTree[side].nodes {
					if node.loading {
						return false
					}
				}
				return !w.navigationBusy() && len(w.directoryTree[side].requests) == 0
			}
			w.navigate(localSide, target)
			waitDirectoryTree(t, state, w, host.windows[0].view, settled)
			w.navigate(localSide, original)
			waitDirectoryTree(t, state, w, host.windows[0].view, settled)
			ancestor := directoryTreeTestNode(w, localSide, parent)
			ancestor.expanded = false
			w.invalidateDirectoryTree(localSide)
			w.navigate(localSide, target)
			waitDirectoryTree(t, state, w, host.windows[0].view, settled)
			w.flattenDirectoryTree(side)
			if !sameDirectoryTreeLocation(localSide, w.folder(localSide).path, target) || !ancestor.expanded || directoryTreeIndex(w, localSide, target) < 0 {
				t.Fatalf("navigation left its target behind a collapsed ancestor: expanded=%v selected=%q target=%q", ancestor.expanded, w.directoryTree[side].selectedPath, target)
			}
		})
	}
}

func TestSFTPDirectoryTreeActivationKeepsCollapsedAncestorsOfCurrentSelection(t *testing.T) {
	state, w, remote, host, local := dualSFTPFixture(t, 1)
	enableRefreshTestTrees(t, state, w, remote, host)
	localParent := directoryTreeTestNode(w, true, filepath.Dir(local))
	remoteParent := directoryTreeTestNode(w, false, "/home")
	localParent.expanded, remoteParent.expanded = false, false
	w.activate()
	waitDirectoryTree(t, state, w, host.windows[0].view, func() bool {
		for side := range 2 {
			for _, node := range w.directoryTree[side].nodes {
				if node.loading {
					return false
				}
			}
		}
		return directoryTreeTestNode(w, true, local).loaded && directoryTreeTestNode(w, false, w.view.path).loaded
	})
	if localParent.expanded || remoteParent.expanded {
		t.Fatal("activation unfolded an ancestor of the current tree selection")
	}
	if !sameDirectoryTreeLocation(true, w.directoryTree[0].selectedPath, local) || w.directoryTree[1].selectedPath != w.view.path {
		t.Fatal("activation replaced the current tree selection")
	}
}

func TestSFTPDirectoryTreeRefreshesAfterTerminalCommandKeepingExpansion(t *testing.T) {
	state, w, remote, host, _ := dualSFTPFixture(t, 1)
	enableRefreshTestTrees(t, state, w, remote, host)
	session, ok := state.sessionByIDPointer(w.session.ID)
	if !ok || session.term == nil {
		t.Fatal("missing terminal session for SFTP window")
	}

	// Expand /home/test/docs in the remote directory tree before running terminal commands.
	docsNode := directoryTreeTestNode(w, false, "/home/test/docs")
	w.flattenDirectoryTree(1)
	sftpTreeActivate(t, w, w.directoryTree[1].tree.DisclosureID(directoryTreeIndex(w, false, "/home/test/docs")))
	waitDirectoryTree(t, state, w, host.windows[0].view, func() bool {
		return docsNode.expanded && docsNode.loaded && !docsNode.loading
	})

	// Simulate `mkdir newdir && mkdir docs/nested && touch newfile.txt` in the terminal
	// without switching to or activating the SFTP window.
	if err := remote.Mkdir("/home/test/newdir"); err != nil {
		t.Fatal(err)
	}
	if err := remote.Mkdir("/home/test/docs/nested"); err != nil {
		t.Fatal(err)
	}
	remote.mu.Lock()
	remote.entries["/home/test"] = append(remote.entries["/home/test"], workbenchFileInfo{name: "newfile.txt", mode: 0o644})
	remote.mu.Unlock()

	session.term.sftpRefreshPending = true
	state.followTerminalDirectory(session)

	waitDirectoryTree(t, state, w, host.windows[0].view, func() bool {
		return sftpHasRow(w.view, "newdir") &&
			sftpHasRow(w.view, "newfile.txt") &&
			directoryTreeTestNode(w, false, "/home/test/newdir") != nil &&
			directoryTreeTestNode(w, false, "/home/test/docs/nested") != nil
	})
	if !directoryTreeTestNode(w, false, "/home/test").expanded || !docsNode.expanded {
		t.Fatal("terminal-triggered refresh collapsed previously expanded directory tree levels")
	}
}
