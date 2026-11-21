//go:build windows || darwin || linux

package workbench

import (
	"os"
	"path/filepath"
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
