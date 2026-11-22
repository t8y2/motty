package workbench

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

const (
	sftpDirectoryTreeRowHeight = float32(28)
	maxDirectoryTreeChildren   = 20000
	maxDirectoryTreeDepth      = 64
	directoryTreeMinWidth      = float32(140)
	directoryListMinWidth      = float32(150)
	directoryTreeGap           = float32(6)
	sftpDirectoryTreeIndent    = float32(14)
	directoryTreeLabelReserve  = float32(80)
	// Tree listings share the worker queue with ordinary navigation. Keep
	// their IDs in a separate high range so results cannot be consumed by the
	// wrong cache when both use the same sequence number.
	sftpDirectoryTreeRequestBit uint64 = 1 << 63
)

// sftpTreeNode contains only directory names. Loading is deferred until the
// user expands a node, and the window owns the cache for as long as it stays
// open.
type sftpTreeNode struct {
	// key names the node in the page Tree, stable while rows open above it.
	key       int
	path      string
	name      string
	children  []*sftpTreeNode
	expanded  bool
	loaded    bool
	loading   bool
	err       string
	truncated bool
	requestID uint64
}

type sftpTreeVisibleRow struct {
	node  *sftpTreeNode
	depth uint8
}

type sftpDirectoryTree struct {
	enabled      bool
	overridden   bool
	rootPath     string
	roots        []*sftpTreeNode
	nodes        map[string]*sftpTreeNode
	requests     map[uint64]string
	selectedPath string
	followPath   string // Navigation waiting for an ancestor's background read.
	visible      []sftpTreeVisibleRow
	// tree is the page Tree over visible: it owns the rows, their choice, the
	// scroll offset and the keys. c, label and content are the paint's context
	// and the functions it asked for, made once.
	tree           ui.TreeState
	c              ui.C
	label          func(int) string
	content        func(int)
	bounds         gfx.Rect // the pane width, for how deep a row may indent
	contentWidth   float32  // the room a row's name is indented against
	nextKey        int
	requestID      uint64
	generation     uint64
	revealSelected bool
}

func (w *sftpWindow) enableDirectoryTree(side int, location string) {
	if side < 0 || side > 1 {
		return
	}
	tree := &w.directoryTree[side]
	tree.enabled = true
	w.syncDirectoryTreeToPath(side == 0, location)
	w.invalidate()
}

func (w *sftpWindow) setDirectoryTreeDefault(enabled bool) {
	for side := range 2 {
		tree := &w.directoryTree[side]
		if tree.overridden {
			continue
		}
		location := w.folder(side == 0).path
		if location == "" {
			if side == 0 {
				location = w.initialLocal
			} else {
				location = w.initialRemote
			}
		}
		tree.enabled = enabled
		if enabled {
			w.enableDirectoryTree(side, location)
		} else {
			resetDirectoryTree(tree)
		}
	}
	w.invalidate()
}

func (w *sftpWindow) toggleDirectoryTree(local bool) {
	side := sideIndex(local)
	tree := &w.directoryTree[side]
	tree.overridden = true
	tree.enabled = !tree.enabled
	if tree.enabled {
		w.enableDirectoryTree(side, w.folder(local).path)
	} else {
		resetDirectoryTree(tree)
		w.invalidate()
	}
}

func resetDirectoryTree(tree *sftpDirectoryTree) {
	tree.generation++
	tree.rootPath = ""
	tree.roots = nil
	tree.nodes = nil
	tree.requests = nil
	tree.selectedPath = ""
	tree.followPath = ""
	tree.visible = nil
	tree.tree = ui.TreeState{}
	tree.label, tree.content = nil, nil
	tree.bounds = gfx.Rect{}
	tree.revealSelected = false
}

func (w *sftpWindow) clearDirectoryTreeRequests() {
	for side := range 2 {
		tree := &w.directoryTree[side]
		tree.generation++
		clear(tree.requests)
		for _, node := range tree.nodes {
			if node.loading {
				node.loading = false
			}
		}
	}
}

// Keep node identities and expansion while forgetting readings made before a
// mutation or an explicit refresh. Collapsed branches stay lazy.
func (w *sftpWindow) invalidateDirectoryTree(local bool) {
	tree := &w.directoryTree[sideIndex(local)]
	tree.generation++
	clear(tree.requests)
	for _, node := range tree.nodes {
		node.loaded, node.loading = false, false
		// The old reading is what failed, not the folder: a fresh read decides.
		node.err = ""
		node.requestID++
	}
}

func (w *sftpWindow) loadExpandedDirectoryTree(local bool) {
	side := sideIndex(local)
	tree := &w.directoryTree[side]
	if !tree.enabled || w.closed || len(tree.requests) != 0 {
		return
	}
	// Refresh one branch at a time. Its result advances the next branch, leaving
	// the worker's bounded queue available for navigation and other user actions.
	for _, node := range tree.nodes {
		if node.loading {
			return
		}
	}
	var load func(*sftpTreeNode) bool
	load = func(node *sftpTreeNode) bool {
		if !node.expanded {
			return false
		}
		if !node.loaded {
			reveal := tree.revealSelected
			w.loadDirectoryTreeNode(side, node)
			tree.revealSelected = reveal
			return true
		}
		for _, child := range node.children {
			if load(child) {
				return true
			}
		}
		return false
	}
	for _, root := range tree.roots {
		if load(root) {
			return
		}
	}
}

func cancelDirectoryTreeRead(tree *sftpDirectoryTree, local bool, node *sftpTreeNode) {
	if directory, ok := tree.requests[node.requestID]; ok && sameDirectoryTreePath(local, directory, node.path) {
		delete(tree.requests, node.requestID)
	}
	node.requestID++
	node.loading = false
}

func directoryTreeRoot(local bool, location string) string {
	if !local {
		return "/"
	}
	location = filepath.Clean(location)
	volume := filepath.VolumeName(location)
	if volume != "" {
		if strings.HasPrefix(volume, `\\`) {
			return volume + string(filepath.Separator)
		}
		return volume + string(filepath.Separator)
	}
	return string(filepath.Separator)
}

func directoryTreeRootLabel(root string) string {
	root = filepath.Clean(root)
	if root == string(filepath.Separator) {
		return root
	}
	return strings.TrimSuffix(root, string(filepath.Separator))
}

func directoryTreePaneWidths(contentWidth float32) (float32, bool) {
	if contentWidth < directoryTreeMinWidth+directoryListMinWidth+directoryTreeGap {
		return 0, false
	}
	treeWidth := min(float32(240), max(directoryTreeMinWidth, contentWidth*.28))
	treeWidth = min(treeWidth, contentWidth-directoryListMinWidth-directoryTreeGap)
	return treeWidth, true
}

func directoryTreeToggleDisabled(enabled bool, contentWidth float32) bool {
	_, fits := directoryTreePaneWidths(contentWidth)
	return !enabled && !fits
}

func directoryTreeDisplayDepth(depth uint8, width float32) uint8 {
	maxDepth := int(max((width-directoryTreeLabelReserve)/sftpDirectoryTreeIndent, 0))
	return uint8(min(int(depth), maxDepth))
}

func directoryTreePathKey(local bool, location string) string {
	if local {
		location = filepath.Clean(location)
		if runtime.GOOS == "windows" {
			return strings.ToLower(location)
		}
		return location
	}
	return path.Clean(location)
}

func sameDirectoryTreePath(local bool, left, right string) bool {
	return directoryTreePathKey(local, left) == directoryTreePathKey(local, right)
}

func sameDirectoryTreeLocation(local bool, left, right string) bool {
	if sameDirectoryTreePath(local, left, right) {
		return true
	}
	if !local {
		return false
	}
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

// Windows can expose the same directory through both its long name and an
// 8.3 alias (for example RUNNER~1). The tree cache uses the long name returned
// by its directory listing, so retry a missing local child by file identity.
// Restricting this to real directories avoids confusing a symlink with a
// separate entry that happens to target the same location.
func directoryTreeLocalAliasChild(node *sftpTreeNode, location string) *sftpTreeNode {
	if node == nil || !node.loaded {
		return nil
	}
	target, err := os.Lstat(location)
	if err != nil || !target.IsDir() || target.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	for _, child := range node.children {
		info, err := os.Lstat(child.path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if os.SameFile(target, info) {
			return child
		}
	}
	return nil
}

// directoryTreeChild resolves one path component under node. The direct key is
// the normal case; a local miss retries by file identity so an 8.3 alias and
// the long name its listing recorded reach the same node.
func directoryTreeChild(tree *sftpDirectoryTree, local bool, node *sftpTreeNode, location string) *sftpTreeNode {
	child := tree.nodes[directoryTreePathKey(local, location)]
	if child == nil && local {
		child = directoryTreeLocalAliasChild(node, location)
	}
	return child
}

// directoryTreeResolveNode finds the cached node for location, walking from the
// root the way syncDirectoryTreeToPath does. The direct key answers whenever the
// cache still knows the path, including a node a refresh has not reread yet.
// A local miss retries the selection's canonical path and then the walk, whose
// file-identity fallback needs loaded parents to prove two names are one folder.
func directoryTreeResolveNode(tree *sftpDirectoryTree, local bool, location string) *sftpTreeNode {
	if tree == nil || location == "" {
		return nil
	}
	if node := tree.nodes[directoryTreePathKey(local, location)]; node != nil {
		return node
	}
	if len(tree.roots) == 0 {
		return nil
	}
	if !local {
		return nil
	}
	if tree.selectedPath != "" && sameDirectoryTreeLocation(true, tree.selectedPath, location) {
		if node := tree.nodes[directoryTreePathKey(true, tree.selectedPath)]; node != nil {
			return node
		}
	}
	root := tree.roots[0]
	parts, inside := directoryTreeRelativeParts(local, root.path, location)
	if !inside {
		return nil
	}
	node := root
	for _, part := range parts {
		nextPath := filepath.Join(node.path, part)
		node = directoryTreeChild(tree, true, node, nextPath)
		if node == nil {
			return nil
		}
	}
	return node
}

func cleanDirectoryTreePath(local bool, location string) string {
	if local {
		return filepath.Clean(location)
	}
	return path.Clean(location)
}

func directoryTreeRelativeParts(local bool, root, location string) ([]string, bool) {
	root = cleanDirectoryTreePath(local, root)
	location = cleanDirectoryTreePath(local, location)
	var relative string
	if local {
		var err error
		relative, err = filepath.Rel(root, location)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, false
		}
		if relative == "." {
			return nil, true
		}
		return strings.Split(relative, string(filepath.Separator)), true
	}
	switch {
	case location == root:
		return nil, true
	case root == "/" && path.IsAbs(location):
		relative = strings.TrimPrefix(location, "/")
	case strings.HasPrefix(location, root+"/"):
		relative = strings.TrimPrefix(location, root+"/")
	default:
		return nil, false
	}
	if relative == "" {
		return nil, true
	}
	return strings.Split(relative, "/"), true
}

func (w *sftpWindow) syncDirectoryTreeToPath(local bool, location string) {
	side := sideIndex(local)
	tree := &w.directoryTree[side]
	if !tree.enabled || location == "" {
		return
	}
	tree.followPath = ""
	location = cleanDirectoryTreePath(local, location)
	rootPath := directoryTreeRoot(local, location)
	if tree.rootPath == "" || !sameDirectoryTreePath(local, tree.rootPath, rootPath) {
		tree.generation++
		tree.rootPath = rootPath
		root := &sftpTreeNode{path: rootPath, name: directoryTreeRootLabel(rootPath)}
		tree.nextKey++
		root.key = tree.nextKey
		tree.roots = []*sftpTreeNode{root}
		tree.nodes = map[string]*sftpTreeNode{directoryTreePathKey(local, rootPath): root}
		tree.requests = make(map[uint64]string)
	}
	tree.selectedPath = location
	tree.revealSelected = true
	if len(tree.roots) == 0 {
		return
	}
	root := tree.roots[0]
	root.expanded = true
	parts, inside := directoryTreeRelativeParts(local, root.path, location)
	if !inside {
		return
	}
	node := root
	for _, part := range parts {
		if !node.loaded {
			tree.followPath = location
			w.loadDirectoryTreeNode(side, node)
			return
		}
		node.expanded = true
		nextPath := part
		if local {
			nextPath = filepath.Join(node.path, part)
		} else {
			nextPath = path.Join(node.path, part)
		}
		next := directoryTreeChild(tree, local, node, nextPath)
		if next == nil {
			return
		}
		node = next
	}
	node.expanded = true
	if local && !sameDirectoryTreePath(true, node.path, location) {
		target, targetErr := os.Lstat(location)
		actual, actualErr := os.Lstat(node.path)
		if targetErr == nil && actualErr == nil && os.SameFile(target, actual) {
			// Keep selection and reveal logic keyed to the node's canonical
			// long path rather than the caller's short-path alias.
			location = node.path
			tree.selectedPath = node.path
		}
	}
	if sameDirectoryTreePath(local, node.path, location) && !node.loaded {
		folder := w.folder(local)
		if sameDirectoryTreeLocation(local, folder.path, location) {
			entries := make([]sftpclient.Entry, len(folder.allRows))
			for index := range folder.allRows {
				entries[index] = folder.allRows[index].entry
			}
			cancelDirectoryTreeRead(tree, local, node)
			w.setDirectoryTreeChildren(tree, local, node, entries, false)
		}
	}
}

// Only unfinished navigation follows background reads. Refreshing an already
// displayed folder must keep the user's collapsed branches and tree selection.
func (w *sftpWindow) syncDirectoryTreeAfterBackgroundLoad(local bool) {
	current := w.folder(local).path
	tree := &w.directoryTree[sideIndex(local)]
	if tree.followPath == "" {
		return
	}
	if current == "" || !sameDirectoryTreeLocation(local, tree.selectedPath, current) || !sameDirectoryTreeLocation(local, tree.followPath, current) {
		tree.followPath = ""
		return
	}
	w.syncDirectoryTreeToPath(local, current)
}

func (w *sftpWindow) loadDirectoryTreeNode(side int, node *sftpTreeNode) {
	if node == nil || node.loaded || node.loading {
		return
	}
	tree := &w.directoryTree[side]
	node.loading, node.err = true, ""
	tree.revealSelected = true
	if side == 0 {
		generation := tree.generation
		node.requestID++
		requestID := node.requestID
		directory := node.path
		go func() {
			children, truncated, err := readLocalDirectoryTreeChildren(directory)
			w.owner.aiPost(func(*State) {
				w.applyLocalDirectoryTreeResult(tree, node, generation, requestID, directory, children, truncated, err)
			})
		}()
		return
	}
	if w.view.worker == nil {
		node.loading = false
		node.err = w.owner.messages.SFTPUnavailable
		return
	}
	tree.requestID++
	if tree.requests == nil {
		tree.requests = make(map[uint64]string)
	}
	id := sftpDirectoryTreeRequestBit | tree.requestID
	if !w.view.worker.PrefetchWithID(node.path, id) {
		node.loading = false
		node.err = w.owner.messages.SFTPUnavailable
		return
	}
	node.requestID = id
	tree.requests[id] = node.path
}

func (w *sftpWindow) applyLocalDirectoryTreeResult(tree *sftpDirectoryTree, node *sftpTreeNode, generation, requestID uint64, directory string, children []sftpclient.Entry, truncated bool, err error) {
	if w.closed || tree.generation != generation || node.requestID != requestID {
		return
	}
	// The map lookup is not enough: a refresh can remove this path and create a
	// new node with the same name before an old local read returns. The pointer
	// check keeps that stale result away from the replacement node.
	if tree.nodes[directoryTreePathKey(true, directory)] != node {
		return
	}
	node.loading = false
	if err != nil {
		node.err = err.Error()
		node.loaded = true
	} else {
		w.setDirectoryTreeChildren(tree, true, node, children, truncated)
	}
	w.syncDirectoryTreeAfterBackgroundLoad(true)
	w.loadExpandedDirectoryTree(true)
	w.invalidate()
}

func readLocalDirectoryTreeChildren(directory string) ([]sftpclient.Entry, bool, error) {
	file, err := os.Open(directory)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	items, err := file.ReadDir(maxDirectoryTreeChildren + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	truncated := len(items) > maxDirectoryTreeChildren
	if truncated {
		items = items[:maxDirectoryTreeChildren]
	}
	entries := make([]sftpclient.Entry, 0, len(items))
	for _, item := range items {
		isDirectory := item.IsDir()
		if item.Type()&os.ModeSymlink != 0 {
			if info, statErr := os.Stat(filepath.Join(directory, item.Name())); statErr == nil {
				isDirectory = info.IsDir()
			}
		}
		if !isDirectory {
			continue
		}
		entries = append(entries, sftpclient.Entry{Name: item.Name(), Mode: item.Type() | os.ModeDir})
	}
	slices.SortFunc(entries, func(left, right sftpclient.Entry) int {
		return strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name))
	})
	return entries, truncated, nil
}

func directoryEntries(entries []sftpclient.Entry) []sftpclient.Entry {
	directories := make([]sftpclient.Entry, 0, min(len(entries), maxDirectoryTreeChildren))
	for _, entry := range entries {
		if entry.IsDir() && !entry.LinkBroken {
			directories = append(directories, entry)
			if len(directories) == maxDirectoryTreeChildren {
				break
			}
		}
	}
	slices.SortFunc(directories, func(left, right sftpclient.Entry) int {
		return strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name))
	})
	return directories
}

func (w *sftpWindow) setDirectoryTreeChildren(tree *sftpDirectoryTree, local bool, node *sftpTreeNode, entries []sftpclient.Entry, truncated bool) {
	existing := make(map[string]*sftpTreeNode, len(node.children))
	for _, child := range node.children {
		existing[directoryTreePathKey(local, child.path)] = child
	}
	directories := directoryEntries(entries)
	children := make([]*sftpTreeNode, 0, len(directories))
	retained := make(map[string]struct{}, len(directories))
	for _, entry := range directories {
		location := path.Join(node.path, entry.Name)
		if local {
			location = filepath.Join(node.path, entry.Name)
		}
		key := directoryTreePathKey(local, location)
		child := existing[key]
		if child == nil {
			child = &sftpTreeNode{path: location, name: entry.Name}
		}
		tree.nodes[key] = child
		children = append(children, child)
		retained[key] = struct{}{}
	}
	for _, child := range node.children {
		key := directoryTreePathKey(local, child.path)
		if _, ok := retained[key]; ok {
			continue
		}
		if _, selected := directoryTreeRelativeParts(local, child.path, tree.selectedPath); selected {
			tree.selectedPath = node.path
			tree.revealSelected = true
		}
		removeDirectoryTreeSubtree(tree, local, child)
	}
	node.children = children
	node.loaded = true
	node.loading = false
	node.err = ""
	node.truncated = truncated
}

func removeDirectoryTreeSubtree(tree *sftpDirectoryTree, local bool, node *sftpTreeNode) {
	if node == nil {
		return
	}
	for _, child := range node.children {
		removeDirectoryTreeSubtree(tree, local, child)
	}
	delete(tree.nodes, directoryTreePathKey(local, node.path))
	for id, location := range tree.requests {
		if sameDirectoryTreePath(local, location, node.path) {
			delete(tree.requests, id)
		}
	}
}

func (w *sftpWindow) observeDirectoryTreeListing(local bool, directory string, entries []sftpclient.Entry) {
	if w == nil {
		return
	}
	tree := &w.directoryTree[sideIndex(local)]
	if !tree.enabled {
		return
	}
	// The panel can name a local folder by an 8.3 alias while the node holds
	// the long name its listing recorded; resolve it as navigation does.
	node := directoryTreeResolveNode(tree, local, directory)
	if node == nil {
		return
	}
	cancelDirectoryTreeRead(tree, local, node)
	w.setDirectoryTreeChildren(tree, local, node, entries, len(entries) > maxDirectoryTreeChildren)
	w.loadExpandedDirectoryTree(local)
	w.invalidate()
}

// acceptDirectoryTreeResult consumes only the background listings started by
// a tree disclosure. Other prefetches continue through the existing listing
// cache unchanged.
func (w *sftpWindow) acceptDirectoryTreeResult(result sftpclient.Result) bool {
	if w == nil || result.Operation != sftpclient.OperationList || result.ListID == 0 {
		return false
	}
	tree := &w.directoryTree[1]
	directory, ok := tree.requests[result.ListID]
	if !ok || !sameDirectoryTreePath(false, directory, result.Path) {
		return false
	}
	node := tree.nodes[directoryTreePathKey(false, result.Path)]
	if node == nil {
		delete(tree.requests, result.ListID)
		return true
	}
	if result.Err != nil {
		node.loading = false
		node.loaded = true
		node.err = result.Err.Error()
		delete(tree.requests, result.ListID)
	} else {
		w.setDirectoryTreeChildren(tree, false, node, result.Entries, len(result.Entries) > maxDirectoryTreeChildren)
		if !result.LinksPending || result.FollowUp {
			delete(tree.requests, result.ListID)
		}
		w.syncDirectoryTreeAfterBackgroundLoad(false)
	}
	w.loadExpandedDirectoryTree(false)
	w.invalidate()
	return true
}

func (w *sftpWindow) flattenDirectoryTree(side int) []sftpTreeVisibleRow {
	tree := &w.directoryTree[side]
	tree.visible = tree.visible[:0]
	var appendNode func(*sftpTreeNode, uint8)
	appendNode = func(node *sftpTreeNode, depth uint8) {
		if node == nil || depth > maxDirectoryTreeDepth {
			return
		}
		if node.key == 0 {
			tree.nextKey++
			node.key = tree.nextKey
		}
		tree.visible = append(tree.visible, sftpTreeVisibleRow{node: node, depth: depth})
		if !node.expanded {
			return
		}
		for _, child := range node.children {
			appendNode(child, depth+1)
		}
	}
	for _, root := range tree.roots {
		appendNode(root, 0)
	}
	return tree.visible
}

// declareDirectoryTreePane declares one side's directory tree in the pane: the
// frame the window always drew around it, and inside it the page Tree. The page
// owns the rows, their scroll offset, their choice and their keys; the window
// reads what it delivered after the declaration and opens, closes or navigates.
func (w *sftpWindow) declareDirectoryTreePane(c ui.C, side int, width float32) ui.Box {
	tree := &w.directoryTree[side]
	if !tree.enabled {
		tree.visible = tree.visible[:0]
		return ui.Box{}
	}
	theme := w.owner.Theme()
	tree.bounds = gfx.Rect{Width: width}
	rows := w.flattenDirectoryTree(side)
	w.initDirectoryTreeState(side)
	// Indent by each row's display depth against the room the tree actually
	// has, as the painted tree did; the row keeps its real depth for keys and
	// navigation.
	tree.tree.Indent = sftpDirectoryTreeIndent
	tree.contentWidth = max(width-14, 0)
	if tree.revealSelected {
		for index, row := range rows {
			if sameDirectoryTreePath(side == 0, row.node.path, tree.selectedPath) {
				tree.tree.ScrollIntoView(index)
				break
			}
		}
		tree.revealSelected = false
	}
	frame := ui.Column(c).Width(width).Grow(1).Pad(2).Background(theme.Background).Radius(6).
		Border(1, theme.Border.Mix(theme.MutedForeground, .35))
	frame.Children(func() {
		if len(rows) == 0 {
			ui.Column(c).Grow(1).Items(ui.Center).Justify(ui.Center).Children(func() {
				ui.Text(c, w.owner.messages.SFTPWindow.DirectoryTree).
					FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground)
			})
			return
		}
		tree.c = c
		ui.Tree(c, &tree.tree, len(rows), tree.label, tree.content).Grow(1).AlwaysScrollbars().ScrollbarSize(10).
			ScrollbarIDs(sftpWindowTreeBar[side], gfx.ID{}).ScrollbarLabels(w.owner.messages.SFTPWindow.DirectoryTree, "")
		w.directoryTreeChanges(side, rows)
		tree.c = nil
	})
	return frame
}

// initDirectoryTreeState makes the functions the Tree asks about each row once,
// so a warm paint allocates nothing.
func (w *sftpWindow) initDirectoryTreeState(side int) {
	tree := &w.directoryTree[side]
	if tree.content != nil {
		return
	}
	tree.tree.RowHeight = sftpDirectoryTreeRowHeight
	tree.tree.Indent = sftpDirectoryTreeIndent
	tree.tree.DisplayDepth = func(depth int) int {
		return int(directoryTreeDisplayDepth(uint8(min(depth, 255)), tree.contentWidth))
	}
	tree.tree.Key = func(index int) int {
		visible := w.directoryTree[side].visible
		if index < 0 || index >= len(visible) {
			return 0
		}
		return visible[index].node.key
	}
	tree.tree.Node = func(index int) ui.TreeNode {
		tree := &w.directoryTree[side]
		if index < 0 || index >= len(tree.visible) {
			return ui.TreeNode{}
		}
		row := tree.visible[index]
		node := row.node
		// Depth is the folder's own level: the tree reads it for keyboard
		// movement between a parent and its children. DisplayDepth compresses
		// only the indent a deep path draws with.
		return ui.TreeNode{
			Depth: int(row.depth), Branch: true,
			Expanded: node.expanded, Selected: sameDirectoryTreePath(side == 0, node.path, tree.selectedPath),
		}
	}
	tree.label = func(index int) string {
		tree := &w.directoryTree[side]
		if index < 0 || index >= len(tree.visible) {
			return ""
		}
		node := tree.visible[index].node
		switch {
		case node.err != "":
			return fmt.Sprintf(w.owner.messages.SFTPWindow.DirectoryTreeLoadFailedFmt, node.name, node.err)
		case node.truncated:
			return node.name + " · " + w.owner.messages.SFTPWindow.DirectoryTreeTruncated
		}
		return node.name
	}
	tree.content = func(index int) {
		tree := &w.directoryTree[side]
		if index < 0 || index >= len(tree.visible) || tree.c == nil {
			return
		}
		w.declareDirectoryTreeRow(tree.c, side, index)
	}
}

// declareDirectoryTreeRow declares what one visible row shows after its chevron:
// a folder, its name, and what a background read is doing.
func (w *sftpWindow) declareDirectoryTreeRow(c ui.C, side, index int) {
	tree := &w.directoryTree[side]
	row := tree.visible[index]
	node := row.node
	theme := w.owner.Theme()
	ui.Icon(c, lucideIcons[iconFolderOpen]).Size(16, 16).Color(theme.MutedForeground).Shrink(0)
	color := theme.Foreground
	if node.err != "" {
		color = w.owner.pal.danger
	}
	ui.Text(c, node.name).FontSize(theme.Metrics.FontSizeS).Color(color).Ellipsis().Grow(1)
	indicator, indicatorColor := "", theme.MutedForeground
	switch {
	case node.loading:
		indicator = "…"
	case node.err != "":
		indicator, indicatorColor = "!", w.owner.pal.danger
	case node.truncated:
		indicator, indicatorColor = "…", w.owner.pal.warning
	}
	if indicator != "" {
		ui.Text(c, indicator).FontSize(theme.Metrics.FontSizeS).Weight(gfx.WeightMedium).Color(indicatorColor).Shrink(0)
	}
}

// directoryTreeChanges applies what the page Tree delivered: a chevron opens or
// closes a branch, choosing a row navigates there, and the focused row is the
// selected one.
func (w *sftpWindow) directoryTreeChanges(side int, rows []sftpTreeVisibleRow) {
	tree := &w.directoryTree[side]
	if index, ok := tree.tree.Toggled(); ok && index >= 0 && index < len(rows) {
		w.toggleDirectoryTreeRow(side, index)
	}
	if index, ok := tree.tree.Chosen(); ok && index >= 0 && index < len(rows) {
		w.chooseDirectoryTreeRow(side, index)
	}
	if focused := tree.c.Canvas().Focused(); !focused.IsZero() {
		for index := range rows {
			if tree.tree.RowID(index) == focused {
				tree.selectedPath = rows[index].node.path
				break
			}
		}
	}
}

// toggleDirectoryTreeRow opens or closes the branch at a visible row, reading
// its children again when it opens: SFTP has no change notifications.
func (w *sftpWindow) toggleDirectoryTreeRow(side, index int) {
	tree := &w.directoryTree[side]
	if index < 0 || index >= len(tree.visible) {
		return
	}
	node := tree.visible[index].node
	tree.selectedPath = node.path
	tree.revealSelected = true
	w.activeLocal, w.view.local = side == 0, side == 0
	node.expanded = !node.expanded
	if node.expanded {
		cancelDirectoryTreeRead(tree, side == 0, node)
		node.loaded = false
		w.loadDirectoryTreeNode(side, node)
	}
	w.invalidate()
}

// chooseDirectoryTreeRow navigates to the folder at a visible row.
func (w *sftpWindow) chooseDirectoryTreeRow(side, index int) {
	tree := &w.directoryTree[side]
	if index < 0 || index >= len(tree.visible) {
		return
	}
	node := tree.visible[index].node
	tree.selectedPath = node.path
	tree.revealSelected = true
	w.activeLocal, w.view.local = side == 0, side == 0
	if !sameDirectoryTreePath(side == 0, w.folder(side == 0).path, node.path) {
		w.navigate(side == 0, node.path)
	}
	w.invalidate()
}
