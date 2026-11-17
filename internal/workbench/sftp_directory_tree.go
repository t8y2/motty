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

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

const (
	sftpDirectoryTreeRowHeight   = float32(28)
	maxDirectoryTreeChildren     = 20000
	maxDirectoryTreeDepth        = 64
	directoryTreeMinWidth        = float32(140)
	directoryListMinWidth        = float32(150)
	directoryTreeGap             = float32(6)
	sftpDirectoryTreeIndent      = float32(14)
	directoryTreeDisclosureWidth = float32(24)
	directoryTreeLabelReserve    = float32(80)
	// Tree listings share the worker queue with ordinary navigation. Keep
	// their IDs in a separate high range so results cannot be consumed by the
	// wrong cache when both use the same sequence number.
	sftpDirectoryTreeRequestBit uint64 = 1 << 63
)

// sftpTreeNode contains only directory names. Loading is deferred until the
// user expands a node, and the window owns the cache for as long as it stays
// open.
type sftpTreeNode struct {
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
	enabled        bool
	overridden     bool
	rootPath       string
	roots          []*sftpTreeNode
	nodes          map[string]*sftpTreeNode
	requests       map[uint64]string
	selectedPath   string
	visible        []sftpTreeVisibleRow
	navigation     []components.TreeNavigationItem
	bounds         gfx.Rect
	offset         float32
	bar            components.ScrollAreaGeometry
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
	tree.visible = nil
	tree.navigation = nil
	tree.bounds = gfx.Rect{}
	tree.offset = 0
	tree.bar = components.ScrollAreaGeometry{}
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

// The Viva tree component only reserves the disclosure column for branches.
// A loaded folder with no children is still a folder in this view, so keep its
// icon aligned with sibling folders instead of letting it jump left.
func directoryTreeContentBounds(content gfx.Rect, branch bool) gfx.Rect {
	if branch {
		return content
	}
	content.X += directoryTreeDisclosureWidth
	content.Width = max(0, content.Width-directoryTreeDisclosureWidth)
	return content
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
	location = cleanDirectoryTreePath(local, location)
	rootPath := directoryTreeRoot(local, location)
	if tree.rootPath == "" || !sameDirectoryTreePath(local, tree.rootPath, rootPath) {
		tree.generation++
		tree.rootPath = rootPath
		root := &sftpTreeNode{path: rootPath, name: directoryTreeRootLabel(rootPath)}
		tree.roots = []*sftpTreeNode{root}
		tree.nodes = map[string]*sftpTreeNode{directoryTreePathKey(local, rootPath): root}
		tree.requests = make(map[uint64]string)
		tree.offset = 0
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
		next := tree.nodes[directoryTreePathKey(local, nextPath)]
		if next == nil && local {
			next = directoryTreeLocalAliasChild(node, nextPath)
		}
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
			w.setDirectoryTreeChildren(tree, local, node, entries, false)
		}
	}
}

// A background disclosure read must not replace a selection the user made
// while it was running. Synchronize only while the tree still points at the
// folder currently shown in the file list.
func (w *sftpWindow) syncDirectoryTreeAfterBackgroundLoad(local bool) {
	current := w.folder(local).path
	tree := &w.directoryTree[sideIndex(local)]
	if current == "" || !sameDirectoryTreeLocation(local, tree.selectedPath, current) {
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
		directory := node.path
		go func() {
			children, truncated, err := readLocalDirectoryTreeChildren(directory)
			w.owner.aiPost(func(*State) {
				w.applyLocalDirectoryTreeResult(tree, node, generation, directory, children, truncated, err)
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

func (w *sftpWindow) applyLocalDirectoryTreeResult(tree *sftpDirectoryTree, node *sftpTreeNode, generation uint64, directory string, children []sftpclient.Entry, truncated bool, err error) {
	if w.closed || tree.generation != generation {
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
	selectedDirectory := directory
	if sameDirectoryTreeLocation(local, tree.selectedPath, directory) {
		selectedDirectory = tree.selectedPath
	}
	node := tree.nodes[directoryTreePathKey(local, selectedDirectory)]
	if node == nil {
		return
	}
	w.setDirectoryTreeChildren(tree, local, node, entries, len(entries) > maxDirectoryTreeChildren)
	if local && sameDirectoryTreeLocation(true, tree.selectedPath, directory) {
		w.syncDirectoryTreeToPath(true, directory)
	}
	if !local && sameDirectoryTreeLocation(false, tree.selectedPath, directory) {
		w.syncDirectoryTreeToPath(false, directory)
	}
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

func (w *sftpWindow) paintDirectoryTree(canvas *gfx.Canvas, bounds gfx.Rect, local bool) {
	side := sideIndex(local)
	tree := &w.directoryTree[side]
	tree.bounds = bounds
	if !tree.enabled || bounds.Width <= 0 || bounds.Height <= 0 {
		tree.visible = tree.visible[:0]
		return
	}
	theme, m := w.owner.Theme(), w.owner.messages.SFTPWindow
	canvas.RoundedBorder(bounds, 6, 1, theme.Border.Mix(theme.MutedForeground, .35), theme.Background)
	inner := bounds.Inset(gfx.All(2))
	rows := w.flattenDirectoryTree(side)
	contentHeight := max(float32(len(rows))*sftpDirectoryTreeRowHeight, inner.Height)
	if tree.revealSelected {
		for index, row := range rows {
			if sameDirectoryTreePath(local, row.node.path, tree.selectedPath) {
				top := float32(index) * sftpDirectoryTreeRowHeight
				bottom := top + sftpDirectoryTreeRowHeight
				if top < tree.offset {
					tree.offset = top
				} else if bottom > tree.offset+inner.Height {
					tree.offset = bottom - inner.Height
				}
				break
			}
		}
		tree.offset = max(0, min(tree.offset, max(0, contentHeight-inner.Height)))
		tree.revealSelected = false
	}
	area := components.BeginScrollArea(canvas, theme, inner, gfx.Size{Width: inner.Width, Height: contentHeight}).
		Offset(0, tree.offset).
		Viewport(sftpWindowTreeViewport[side], m.DirectoryTree).
		VerticalScrollbar(sftpWindowTreeBar[side], m.DirectoryTree, "")
	geometry := area.BeginContent()
	tree.bar = geometry
	tree.offset = geometry.OffsetY
	if len(rows) == 0 {
		canvas.LabelCenter(m.DirectoryTree, inner, gfx.TextStyle{Size: theme.Metrics.FontSizeS, Color: theme.MutedForeground})
		area.EndContent()
		return
	}
	first := max(0, int(tree.offset/sftpDirectoryTreeRowHeight))
	last := min(len(rows), first+int(inner.Height/sftpDirectoryTreeRowHeight)+2)
	treeBounds := gfx.Rect{
		X:      inner.X,
		Y:      inner.Y + float32(first)*sftpDirectoryTreeRowHeight - tree.offset,
		Width:  max(inner.Width-10, 0),
		Height: float32(last-first) * sftpDirectoryTreeRowHeight,
	}
	canvas.Clip(inner)
	component := components.BeginTree(canvas, theme, treeBounds).RowHeight(sftpDirectoryTreeRowHeight).Indent(sftpDirectoryTreeIndent)
	for index := first; index < last; index++ {
		row := rows[index]
		node := row.node
		branch := !node.loaded || node.loading || node.err != "" || len(node.children) > 0
		accessibleLabel := node.name
		if node.err != "" {
			accessibleLabel = fmt.Sprintf(m.DirectoryTreeLoadFailedFmt, node.name, node.err)
		} else if node.truncated {
			accessibleLabel = node.name + " · " + m.DirectoryTreeTruncated
		}
		painted := component.Item(components.TreeItem{
			ID:           sftpWindowTreeRows[side].At(index),
			DisclosureID: sftpWindowTreeDisclosures[side].At(index),
			Label:        accessibleLabel,
			Depth:        directoryTreeDisplayDepth(row.depth, treeBounds.Width),
			Branch:       branch,
			Expanded:     node.expanded,
			Selected:     sameDirectoryTreePath(local, node.path, tree.selectedPath),
		})
		content := directoryTreeContentBounds(painted.Content, branch)
		icon := uint8(iconFolderOpen)
		iconBounds := gfx.Rect{X: content.X + 2, Y: content.Y + 6, Width: 16, Height: 16}
		canvas.DrawIcon(lucideIcons[icon], iconBounds, theme.MutedForeground)
		indicator, indicatorColor := "", theme.MutedForeground
		switch {
		case node.loading:
			indicator = "…"
		case node.err != "":
			indicator, indicatorColor = "!", w.owner.pal.danger
		case node.truncated:
			indicator, indicatorColor = "…", w.owner.pal.warning
		}
		reserved := float32(26)
		if indicator != "" {
			reserved = 40
		}
		labelWidth := max(0, content.Width-reserved)
		label := canvas.FitLine(node.name, theme.Metrics.FontSizeS, gfx.WeightRegular, labelWidth)
		color := theme.Foreground
		if node.err != "" {
			color = w.owner.pal.danger
		}
		canvas.Label(label, gfx.Rect{X: iconBounds.X + 20, Y: content.Y, Width: labelWidth, Height: content.Height}, gfx.TextStyle{
			Size: theme.Metrics.FontSizeS, Weight: gfx.WeightRegular, Color: color,
		})
		if indicator != "" {
			canvas.Label(indicator, gfx.Rect{X: content.X + content.Width - 16, Y: content.Y, Width: 12, Height: content.Height}, gfx.TextStyle{
				Size: theme.Metrics.FontSizeS, Weight: gfx.WeightMedium, Color: indicatorColor,
			})
		}
	}
	canvas.Unclip()
	area.EndContent()
}

func (w *sftpWindow) directoryTreeTarget(id gfx.ID) (local bool, index int, disclosure bool, ok bool) {
	for side := range 2 {
		if index, ok := id.In(sftpWindowTreeDisclosures[side]); ok {
			return side == 0, index, true, true
		}
		if index, ok := id.In(sftpWindowTreeRows[side]); ok {
			return side == 0, index, false, true
		}
	}
	return false, 0, false, false
}

func (w *sftpWindow) clickDirectoryTree(id gfx.ID) bool {
	local, index, disclosure, ok := w.directoryTreeTarget(id)
	if !ok {
		return false
	}
	side := sideIndex(local)
	tree := &w.directoryTree[side]
	if index < 0 || index >= len(tree.visible) {
		return true
	}
	node := tree.visible[index].node
	tree.selectedPath = node.path
	tree.revealSelected = true
	w.activeLocal, w.view.local = local, local
	if disclosure {
		node.expanded = !node.expanded
		if node.expanded {
			if node.err != "" {
				node.loaded, node.err = false, ""
			}
			if !node.loaded {
				w.loadDirectoryTreeNode(side, node)
			}
		}
		w.invalidate()
		return true
	}
	if !sameDirectoryTreePath(local, w.folder(local).path, node.path) {
		w.navigate(local, node.path)
	}
	w.invalidate()
	return true
}

func (w *sftpWindow) directoryTreeKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	local, index, _, ok := w.directoryTreeTarget(event.Target)
	if !ok {
		return gfx.EventResult{}, false
	}
	side := sideIndex(local)
	tree := &w.directoryTree[side]
	if len(tree.visible) == 0 {
		return gfx.EventResult{Handled: true}, true
	}
	index = max(0, min(index, len(tree.visible)-1))
	node := tree.visible[index].node
	result := gfx.EventResult{Handled: true, Repaint: true}
	switch event.Key {
	case gfx.KeyEnter, gfx.KeySpace:
		if !event.Repeat {
			w.clickDirectoryTree(event.Target)
		}
	case gfx.KeyRight, gfx.KeyLeft, gfx.KeyUp, gfx.KeyDown, gfx.KeyHome, gfx.KeyEnd, gfx.KeyPageUp, gfx.KeyPageDown:
		tree.navigation = tree.navigation[:0]
		for _, row := range tree.visible {
			branch := !row.node.loaded || row.node.loading || row.node.err != "" || len(row.node.children) > 0
			tree.navigation = append(tree.navigation, components.TreeNavigationItem{
				Depth: row.depth, Branch: branch, Expanded: row.node.expanded,
			})
		}
		next, action, _ := components.NavigateTree(tree.navigation, index, event.Key, int(tree.bounds.Height/sftpDirectoryTreeRowHeight))
		switch action {
		case components.TreeNavigationExpand:
			node.expanded = true
			if node.err != "" {
				node.loaded, node.err = false, ""
			}
			if !node.loaded {
				w.loadDirectoryTreeNode(side, node)
			}
		case components.TreeNavigationCollapse:
			node.expanded = false
		case components.TreeNavigationMove:
			tree.selectedPath = tree.visible[next].node.path
			tree.revealSelected = true
			result.Focus = sftpWindowTreeRows[side].At(next)
			result.FocusVisible = true
		}
	default:
		return gfx.EventResult{}, false
	}
	w.activeLocal, w.view.local = local, local
	w.invalidate()
	return result, true
}

func (w *sftpWindow) directoryTreeScroll(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	for side, tree := range w.directoryTree {
		if !tree.enabled || !pointInRect(event.X, event.Y, tree.bounds) {
			continue
		}
		before := tree.offset
		w.directoryTree[side].offset = max(0, min(tree.bar.MaxOffsetY, tree.offset+event.DeltaY))
		return gfx.EventResult{Handled: true, Repaint: before != w.directoryTree[side].offset}, true
	}
	return gfx.EventResult{}, false
}

func (w *sftpWindow) directoryTreePointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if w.ui.treeBarDrag > 0 {
		side := w.ui.treeBarDrag - 1
		tree := &w.directoryTree[side]
		if event.Kind == gfx.PointerMove {
			if !event.Captured {
				return gfx.EventResult{Handled: true}, true
			}
			before := tree.offset
			tree.offset = tree.bar.VerticalOffsetForThumb(event.Y - w.ui.treeBarGrab)
			return gfx.EventResult{Handled: true, Repaint: before != tree.offset}, true
		}
		if event.Kind == gfx.PointerUp || event.Kind == gfx.PointerExit && !event.Captured {
			w.ui.treeBarDrag = 0
		}
		return gfx.EventResult{Handled: true}, true
	}
	for side, id := range sftpWindowTreeBar {
		if event.Kind != gfx.PointerDown || event.Target != id {
			continue
		}
		tree := &w.directoryTree[side]
		w.ui.treeBarGrab = tree.bar.VerticalThumb.Height / 2
		if event.Y >= tree.bar.VerticalThumb.Y && event.Y < tree.bar.VerticalThumb.Y+tree.bar.VerticalThumb.Height {
			w.ui.treeBarGrab = event.Y - tree.bar.VerticalThumb.Y
		}
		w.ui.treeBarDrag = side + 1
		before := tree.offset
		tree.offset = tree.bar.VerticalOffsetForThumb(event.Y - w.ui.treeBarGrab)
		return gfx.EventResult{Handled: true, Repaint: before != tree.offset}, true
	}
	return gfx.EventResult{}, false
}
