//go:build windows || darwin || linux

package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/sftpclient"
)

func directoryTreeIndex(w *sftpWindow, local bool, location string) int {
	for index, row := range w.directoryTree[sideIndex(local)].visible {
		if sameDirectoryTreeTestLocation(local, row.node.path, location) {
			return index
		}
	}
	return -1
}

func directoryTreeTestNode(w *sftpWindow, local bool, location string) *sftpTreeNode {
	tree := &w.directoryTree[sideIndex(local)]
	if node := tree.nodes[directoryTreePathKey(local, location)]; node != nil {
		return node
	}
	if !local {
		return nil
	}
	target, err := os.Stat(location)
	if err != nil {
		return nil
	}
	for _, node := range tree.nodes {
		actual, err := os.Stat(node.path)
		if err == nil && os.SameFile(target, actual) {
			return node
		}
	}
	return nil
}

func sameDirectoryTreeTestLocation(local bool, left, right string) bool {
	return sameDirectoryTreeLocation(local, left, right)
}

func waitDirectoryTree(t *testing.T, state *State, w *sftpWindow, view *headless.Session, ready func() bool) {
	t.Helper()
	// Windows CI can start from a slow drive root and must disclose every
	// ancestor before reaching a temporary test directory.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if err := view.Driver().Pump(); err != nil {
			t.Fatal(err)
		}
		if ready() {
			view.Rest()
			return
		}
		time.Sleep(time.Millisecond)
	}
	for side := range 2 {
		tree := &w.directoryTree[side]
		t.Logf("tree %d enabled=%t root=%q roots=%d nodes=%d requests=%d current=%q", side, tree.enabled, tree.rootPath, len(tree.roots), len(tree.nodes), len(tree.requests), w.folder(side == 0).path)
		for _, root := range tree.roots {
			t.Logf("root path=%q loaded=%t loading=%t children=%d err=%q", root.path, root.loaded, root.loading, len(root.children), root.err)
		}
		current := w.folder(side == 0).path
		for key, node := range tree.nodes {
			if current != "" && strings.Contains(key, strings.ToLower(filepath.Base(current))) {
				t.Logf("node key=%q path=%q loaded=%t loading=%t children=%d", key, node.path, node.loaded, node.loading, len(node.children))
			}
		}
	}
	t.Fatal("directory tree did not settle")
}

func TestSFTPDirectoryTreeIsOffByDefaultAndLoadsLocalChildrenOnExpand(t *testing.T) {
	state, w, _, host, directory := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	if state.sftpDirectoryTree || w.directoryTree[0].enabled || w.directoryTree[1].enabled {
		t.Fatal("directory trees should be off by default")
	}
	childDirectory := filepath.Join(directory, "tree", "leaf-dir")
	if err := os.Mkdir(childDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	w.click(sftpWindowTreeToggle[0])
	if !w.directoryTree[0].enabled || state.sftpDirectoryTree {
		t.Fatal("the window toggle did not stay local")
	}
	var treeNode *sftpTreeNode
	waitDirectoryTree(t, state, w, view, func() bool {
		treeNode = directoryTreeTestNode(w, true, filepath.Join(directory, "tree"))
		return treeNode != nil && !treeNode.loading
	})
	if treeNode.loaded {
		t.Fatal("a nested folder was read before its disclosure opened")
	}
	w.window.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	view.Rest()
	index := directoryTreeIndex(w, true, filepath.Join(directory, "tree"))
	if index < 0 {
		t.Fatal("nested folder is missing from the visible tree")
	}
	if result := w.key(gfx.KeyEvent{Target: sftpWindowTreeDisclosures[0].At(index), Key: gfx.KeyEnter}); !result.Handled {
		t.Fatal("keyboard activation of a tree disclosure was not handled")
	}
	waitDirectoryTree(t, state, w, view, func() bool {
		return treeNode.loaded && !treeNode.loading
	})
	if directoryTreeTestNode(w, true, childDirectory) == nil {
		t.Fatal("expanded local folder did not load its directory children")
	}
}

func TestSFTPDirectoryTreeKeyboardNavigatesAndLoadsLocalChild(t *testing.T) {
	state, window, _, host, directory := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	childDirectory := filepath.Join(directory, "tree", "nested-keyboard")
	if err := os.Mkdir(childDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	window.click(sftpWindowTreeToggle[0])
	var branch *sftpTreeNode
	waitDirectoryTree(t, state, window, view, func() bool {
		branch = directoryTreeTestNode(window, true, filepath.Join(directory, "tree"))
		return branch != nil && !branch.loading
	})
	window.window.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	view.Rest()
	branchIndex := directoryTreeIndex(window, true, branch.path)
	if branchIndex < 0 {
		t.Fatal("directory is not in the visible tree")
	}
	if result := window.key(gfx.KeyEvent{Target: sftpWindowTreeRows[0].At(branchIndex), Key: gfx.KeyRight}); !result.Handled || !branch.expanded {
		t.Fatal("Right did not expand the branch")
	}
	waitDirectoryTree(t, state, window, view, func() bool {
		return branch.loaded && !branch.loading && directoryTreeIndex(window, true, childDirectory) >= 0
	})
	branchIndex = directoryTreeIndex(window, true, branch.path)
	result := window.key(gfx.KeyEvent{Target: sftpWindowTreeRows[0].At(branchIndex), Key: gfx.KeyRight})
	childIndex := directoryTreeIndex(window, true, childDirectory)
	if !result.Handled || result.Focus != sftpWindowTreeRows[0].At(childIndex) ||
		!sameDirectoryTreeTestLocation(true, window.directoryTree[0].selectedPath, childDirectory) {
		t.Fatalf("Right did not select the child: focus=%+v childIndex=%d selected=%q child=%q", result, childIndex, window.directoryTree[0].selectedPath, childDirectory)
	}
	result = window.key(gfx.KeyEvent{Target: result.Focus, Key: gfx.KeyLeft})
	if !result.Handled || result.Focus != sftpWindowTreeRows[0].At(branchIndex) ||
		!sameDirectoryTreePath(true, window.directoryTree[0].selectedPath, branch.path) {
		t.Fatalf("Left did not select the parent: %+v", result)
	}
	result = window.key(gfx.KeyEvent{Target: result.Focus, Key: gfx.KeyLeft})
	if !result.Handled || branch.expanded {
		t.Fatalf("Left did not collapse the parent: %+v", result)
	}
}

func TestSFTPDirectoryTreeLoadsRemoteChildrenOnlyWhenExpanded(t *testing.T) {
	state, w, remote, host, _ := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	remote.mu.Lock()
	remote.entries["/"] = []os.FileInfo{workbenchFileInfo{name: "home", mode: os.ModeDir | 0o755}}
	remote.entries["/home"] = []os.FileInfo{workbenchFileInfo{name: "test", mode: os.ModeDir | 0o755}}
	remote.mu.Unlock()
	w.click(sftpWindowTreeToggle[1])
	var docs *sftpTreeNode
	waitDirectoryTree(t, state, w, view, func() bool {
		docs = w.directoryTree[1].nodes[directoryTreePathKey(false, "/home/test/docs")]
		return docs != nil
	})
	if docs.loaded || remote.readCount("/home/test/docs") != 0 {
		t.Fatal("the remote tree read a nested folder before expansion")
	}
	w.window.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	view.Rest()
	index := directoryTreeIndex(w, false, "/home/test/docs")
	if index < 0 {
		t.Fatal("remote folder is missing from the visible tree")
	}
	w.click(sftpWindowTreeDisclosures[1].At(index))
	waitDirectoryTree(t, state, w, view, func() bool {
		return docs.loaded && !docs.loading
	})
	if remote.readCount("/home/test/docs") == 0 {
		t.Fatal("expanding the remote folder did not request it")
	}
}

func TestSFTPDirectoryTreeSettingAndProfileSyncPersist(t *testing.T) {
	state, _, _, _ := openTestSFTP(t)
	state.sftpDirectoryTree = true
	state.sftpBookmarks = []sftpBookmark{{
		host: "profile-host", name: "Project", local: t.TempDir(), path: "/srv/project", sync: true,
	}}
	saved := state.snapshot()
	if !saved.SFTPDirectoryTree || len(saved.SFTPBookmarks) != 1 || !saved.SFTPBookmarks[0].Sync {
		t.Fatal("new file settings were not written")
	}
	restored := newTestState(t, state.options)
	restored.restoreSFTPSettings(saved)
	if !restored.sftpDirectoryTree || len(restored.sftpBookmarks) != 1 || !restored.sftpBookmarks[0].sync {
		t.Fatal("new file settings were not restored")
	}
}

func TestSFTPDirectoryTreeGlobalDefaultPreservesWindowOverrides(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1)
	w.toggleDirectoryTree(true)
	if !w.directoryTree[0].overridden || !w.directoryTree[0].enabled {
		t.Fatal("the local window toggle was not recorded as an override")
	}
	w.setDirectoryTreeDefault(false)
	if !w.directoryTree[0].enabled || w.directoryTree[1].enabled {
		t.Fatal("the global default overwrote a local override or failed to update the inherited side")
	}
	w.toggleDirectoryTree(true)
	w.setDirectoryTreeDefault(true)
	if w.directoryTree[0].enabled || !w.directoryTree[1].enabled {
		t.Fatal("a later global default overwrote the window's explicit disabled state")
	}
}

func TestSFTPDirectoryTreeRefreshPrunesRemovedNodes(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1)
	tree := &w.directoryTree[0]
	tree.enabled = true
	root := &sftpTreeNode{path: t.TempDir(), name: "root", loaded: true, expanded: true}
	removed := &sftpTreeNode{path: filepath.Join(root.path, "removed"), name: "removed", loaded: true, expanded: true}
	descendant := &sftpTreeNode{path: filepath.Join(removed.path, "child"), name: "child"}
	removed.children = []*sftpTreeNode{descendant}
	root.children = []*sftpTreeNode{removed}
	tree.nodes = map[string]*sftpTreeNode{
		directoryTreePathKey(true, root.path):       root,
		directoryTreePathKey(true, removed.path):    removed,
		directoryTreePathKey(true, descendant.path): descendant,
	}
	tree.requests = map[uint64]string{1: descendant.path}
	tree.selectedPath = descendant.path

	w.setDirectoryTreeChildren(tree, true, root, nil, false)
	if tree.nodes[directoryTreePathKey(true, removed.path)] != nil ||
		tree.nodes[directoryTreePathKey(true, descendant.path)] != nil ||
		len(tree.requests) != 0 {
		t.Fatal("refresh retained a directory subtree that no longer exists")
	}
	if !sameDirectoryTreePath(true, tree.selectedPath, root.path) {
		t.Fatalf("removed selection moved to %q instead of its surviving parent", tree.selectedPath)
	}
}

func TestSFTPDirectoryTreeRejectsLateLocalResultForRecreatedPath(t *testing.T) {
	_, w, _, _, directory := dualSFTPFixture(t, 1)
	tree := &w.directoryTree[0]
	tree.enabled = true
	tree.generation = 9
	root := &sftpTreeNode{path: directory, name: "root", loaded: true, expanded: true}
	oldNode := &sftpTreeNode{path: filepath.Join(directory, "same-name"), name: "same-name", loading: true}
	root.children = []*sftpTreeNode{oldNode}
	tree.nodes = map[string]*sftpTreeNode{
		directoryTreePathKey(true, root.path):    root,
		directoryTreePathKey(true, oldNode.path): oldNode,
	}
	tree.selectedPath = root.path

	w.setDirectoryTreeChildren(tree, true, root, nil, false)
	w.setDirectoryTreeChildren(tree, true, root, []sftpclient.Entry{
		{Name: oldNode.name, Mode: os.ModeDir | 0o755},
	}, false)
	replacement := root.children[0]
	if replacement == oldNode {
		t.Fatal("same-name refresh did not create a replacement node")
	}

	w.applyLocalDirectoryTreeResult(tree, oldNode, tree.generation, oldNode.path, []sftpclient.Entry{
		{Name: "stale-child", Mode: os.ModeDir | 0o755},
	}, false, nil)
	if replacement.loaded || len(replacement.children) != 0 {
		t.Fatal("a late result for the removed node populated its same-name replacement")
	}
}

func TestSFTPDirectoryTreeDisableClearsCache(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1)
	tree := &w.directoryTree[0]
	tree.enabled = true
	tree.rootPath = "/cached"
	tree.roots = []*sftpTreeNode{{path: "/cached"}}
	tree.nodes = map[string]*sftpTreeNode{"cached": {path: "/cached"}}
	tree.requests = map[uint64]string{1: "/cached"}
	tree.selectedPath = "/cached"
	tree.visible = []sftpTreeVisibleRow{{node: tree.roots[0]}}
	w.toggleDirectoryTree(true)
	if tree.enabled || tree.rootPath != "" || tree.roots != nil || tree.nodes != nil ||
		tree.requests != nil || tree.selectedPath != "" || tree.visible != nil {
		t.Fatal("disabling the tree retained stale cache or selection state")
	}
}

func TestSFTPLocationProfileRestoreClearsSyncWithoutLocalRoot(t *testing.T) {
	state, _, _, _ := openTestSFTP(t)
	state.restoreSFTPSettings(savedState{SFTPBookmarks: []savedBookmark{{
		Host: "profile-host", Name: "Cross-platform", Local: "relative/path", Path: "/srv/project", Sync: true,
	}}})
	if len(state.sftpBookmarks) != 1 {
		t.Fatalf("restored profiles = %d, want 1", len(state.sftpBookmarks))
	}
	if state.sftpBookmarks[0].local != "" || state.sftpBookmarks[0].sync {
		t.Fatalf("invalid local root must disable synchronization: %+v", state.sftpBookmarks[0])
	}
}

func TestSFTPDirectoryTreeRequestIDsDoNotCollideWithNavigation(t *testing.T) {
	tree := sftpDirectoryTree{requestID: 1}
	view := sftpView{navigationID: 1}
	if got := sftpDirectoryTreeRequestBit | tree.requestID; got == view.navigationID {
		t.Fatalf("directory tree request ID %d collides with navigation ID %d", got, view.navigationID)
	}
}

func TestSFTPDirectoryTreeWidthKeepsFileListUsable(t *testing.T) {
	if _, show := directoryTreePaneWidths(directoryTreeMinWidth + directoryListMinWidth + directoryTreeGap - 1); show {
		t.Fatal("tree shown without room for the minimum tree and file-list widths")
	}
	if !directoryTreeToggleDisabled(false, directoryTreeMinWidth+directoryListMinWidth+directoryTreeGap-1) {
		t.Fatal("tree can be enabled when the pane cannot display it")
	}
	if directoryTreeToggleDisabled(true, directoryTreeMinWidth+directoryListMinWidth+directoryTreeGap-1) {
		t.Fatal("an enabled tree cannot be turned off in a narrow pane")
	}
	width := directoryTreeMinWidth + directoryListMinWidth + directoryTreeGap
	treeWidth, show := directoryTreePaneWidths(width)
	if !show || treeWidth < directoryTreeMinWidth || width-treeWidth-directoryTreeGap < directoryListMinWidth {
		t.Fatalf("tree=%v show=%v leaves an unusable file list at pane width %v", treeWidth, show, width)
	}
	if got := directoryTreeDisplayDepth(maxDirectoryTreeDepth, treeWidth); got > 8 {
		t.Fatalf("deep tree indentation leaves no room for labels: depth %d at width %v", got, treeWidth)
	}
}

func TestSFTPDirectoryTreeLeafKeepsDisclosureColumn(t *testing.T) {
	content := gfx.Rect{X: 20, Y: 10, Width: 160, Height: 28}
	branch := directoryTreeContentBounds(content, true)
	leaf := directoryTreeContentBounds(content, false)
	if branch != content {
		t.Fatalf("branch content moved unexpectedly: %+v", branch)
	}
	if leaf.X != branch.X+directoryTreeDisclosureWidth {
		t.Fatalf("leaf folder starts at x=%v, want disclosure-aligned x=%v", leaf.X, branch.X+directoryTreeDisclosureWidth)
	}
	if leaf.Width != branch.Width-directoryTreeDisclosureWidth {
		t.Fatalf("leaf width=%v, want %v", leaf.Width, branch.Width-directoryTreeDisclosureWidth)
	}
}

func TestSFTPDirectoryTreeBackgroundLoadKeepsUserSelection(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1)
	tree := &w.directoryTree[1]
	tree.enabled = true
	tree.rootPath = "/"
	root := &sftpTreeNode{path: "/", name: "/", loaded: true, expanded: true}
	docs := &sftpTreeNode{path: "/home/test/docs", name: "docs", loading: true}
	tree.roots = []*sftpTreeNode{root}
	tree.nodes = map[string]*sftpTreeNode{
		directoryTreePathKey(false, root.path): root,
		directoryTreePathKey(false, docs.path): docs,
	}
	id := sftpDirectoryTreeRequestBit | 41
	tree.requests = map[uint64]string{id: docs.path}
	tree.selectedPath = docs.path

	if !w.acceptDirectoryTreeResult(sftpclient.Result{
		Operation:  sftpclient.OperationList,
		Path:       docs.path,
		ListID:     id,
		Background: true,
		Entries: []sftpclient.Entry{
			{Name: "api", Mode: os.ModeDir | 0o755},
		},
	}) {
		t.Fatal("background tree result was not accepted")
	}
	if !sameDirectoryTreePath(false, tree.selectedPath, docs.path) {
		t.Fatalf("background load replaced the user's selection with %q", tree.selectedPath)
	}
}

func TestSFTPRemoteOnlyLocationProfileDisablesBrowseSync(t *testing.T) {
	state, w, _, host, _ := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	w.browse.enabled = true
	w.openPairedBookmark(sftpBookmark{path: "/home/test/docs"})
	if w.browse.enabled || w.browse.pending != nil {
		t.Fatal("remote-only profile retained synchronization state")
	}
	dualWait(t, state, w, view, func() bool {
		return !w.navigationBusy() && w.view.path == "/home/test/docs"
	})
}

func TestSFTPDirectoryTreeRightKeyAtDepthLimitDoesNotPanic(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1)
	tree := &w.directoryTree[1]
	tree.enabled = true
	tree.visible = make([]sftpTreeVisibleRow, maxDirectoryTreeDepth+1)
	for index := range tree.visible {
		node := &sftpTreeNode{name: "node", loaded: true, expanded: index < maxDirectoryTreeDepth}
		if index == maxDirectoryTreeDepth {
			node.children = []*sftpTreeNode{{name: "hidden by depth limit"}}
		}
		tree.visible[index] = sftpTreeVisibleRow{node: node, depth: uint8(index)}
	}
	result, handled := w.directoryTreeKey(gfx.KeyEvent{
		Target: sftpWindowTreeRows[1].At(maxDirectoryTreeDepth),
		Key:    gfx.KeyRight,
	})
	if !handled || !result.Handled {
		t.Fatal("right arrow at the depth limit was not handled")
	}
}

func TestSFTPLocationProfileQuickMenuAndSync(t *testing.T) {
	state, w, _, host, directory := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	target := filepath.Join(directory, "tree")
	profile := sftpBookmark{
		host: w.session.host.CatalogID, name: "Synchronized project",
		local: target, path: "/home/test/docs", sync: true,
	}
	state.sftpBookmarks = []sftpBookmark{profile}
	w.window.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	view.Rest()
	w.click(hitSFTPProfileQuick)
	w.window.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	view.Rest()
	if !w.ui.profileQuickOpen {
		t.Fatal("profile quick menu did not open")
	}
	if w.ui.profileQuickScroll.BarID != hitSFTPProfileQuickBar {
		t.Fatal("profile quick menu did not expose a draggable scrollbar")
	}
	dualHit(t, view, hitSFTPProfileQuickItems.At(0))
	w.click(hitSFTPProfileQuickItems.At(0))
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
	if !w.browse.enabled || w.view.lfolder.path != target || w.view.path != "/home/test/docs" {
		t.Fatal("quick profile did not apply its saved synchronized roots")
	}
}

func TestSFTPLocationProfileEditorPersistsSync(t *testing.T) {
	state, w, _, host, directory := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	w.session.host.CatalogID = "sync-editor"
	profile := sftpBookmark{host: "sync-editor", name: "Project", local: directory, path: "/home/test"}
	state.sftpBookmarks = []sftpBookmark{profile}
	w.editBookmark(&profile)
	w.window.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	view.Rest()
	dualHit(t, view, hitSFTPBookmarkSync)
	w.click(hitSFTPBookmarkSync)
	w.click(hitSFTPBookmarkSave)
	if len(state.sftpBookmarks) != 1 || !state.sftpBookmarks[0].sync {
		t.Fatal("profile synchronization was not saved")
	}
	if !state.snapshot().SFTPBookmarks[0].Sync {
		t.Fatal("profile synchronization was not included in the saved state")
	}
}

func TestSFTPLocationProfileHoverShowsQuickActions(t *testing.T) {
	state, w, _, host, directory := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	state.sftpBookmarks = []sftpBookmark{{
		host: w.session.host.CatalogID, name: "Hover profile",
		local: directory, path: "/home/test", sync: true,
	}}
	w.window.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	view.Rest()
	button := dualHit(t, view, hitSFTPLocationProfiles)
	if err := view.Move(button.X+button.Width/2, button.Y+button.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for !w.ui.profileHoverOpen && time.Now().Before(deadline) {
		state.Wake()
		if err := view.Driver().Pump(); err != nil {
			t.Fatal(err)
		}
	}
	view.Rest()
	if !w.ui.profileHoverOpen {
		t.Fatal("hovering the profiles button did not open the quick card")
	}
	dualHit(t, view, hitSFTPProfileHoverItems.At(0))
	result, handled := w.profileQuickKey(gfx.KeyEvent{Target: hitSFTPProfileHoverItems.At(0), Key: gfx.KeyEscape})
	if !handled || !result.Handled || w.ui.profileHoverOpen || result.Focus != hitSFTPLocationProfiles {
		t.Fatal("Escape did not close the focused profile preview and restore trigger focus")
	}
	w.ui.profileHoverOpen = true
	w.ui.profileHoverTrigger = hitSFTPProfileQuick
	result, handled = w.profileQuickKey(gfx.KeyEvent{Target: hitSFTPProfileHoverItems.At(0), Key: gfx.KeyEscape})
	if !handled || !result.Handled || result.Focus != hitSFTPProfileQuick {
		t.Fatal("Escape did not return focus to the profile quick trigger")
	}
}
