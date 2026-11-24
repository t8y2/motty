package workbench

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

type sftpMoveTargetKind uint8

const (
	sftpMoveTargetNone sftpMoveTargetKind = iota
	sftpMoveTargetRow
	sftpMoveTargetTree
	sftpMoveTargetCrumb
)

// sftpMoveTarget is a folder destination inside the same file browser: a
// subfolder row in the list, a directory tree node, or an ancestor breadcrumb.
type sftpMoveTarget struct {
	kind sftpMoveTargetKind
	dir  string
	row  int
	rect gfx.Rect
	clip gfx.Rect
}

// sftpCrumbBox remembers the laid-out box of an ancestor breadcrumb so
// sftpWindow's unified cross-window drag can drop onto it as well.
type sftpCrumbBox struct {
	path string
	box  ui.Box
}

// sftpPanelDrag is the docked file browser's in-page drag-and-drop move
// gesture, backed by Viva's ui.DragState so the workbench page owns press,
// threshold, target hit-testing, drop dispatch, and Escape cancellation.
type sftpPanelDrag struct {
	state       ui.DragState
	accept      func(source, target gfx.ID) bool
	drop        func(source, target gfx.ID)
	local       bool
	sourceDir   string
	preSelect   []int
	restoredFor gfx.ID
}

func joinSFTPPath(local bool, dir, name string) string {
	if local {
		return filepath.Join(dir, name)
	}
	return path.Join(dir, name)
}

func cleanSFTPPath(local bool, location string) string {
	if location == "" {
		return ""
	}
	if local {
		return filepath.Clean(location)
	}
	return path.Clean(location)
}

func sameSFTPPath(local bool, left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	if local {
		return sameDirectoryTreeLocation(true, left, right)
	}
	return path.Clean(left) == path.Clean(right)
}

func isSFTPSubpath(local bool, parent, candidate string) bool {
	if parent == "" || candidate == "" {
		return false
	}
	if local {
		parentClean := filepath.Clean(parent)
		candClean := filepath.Clean(candidate)
		if runtime.GOOS == "windows" {
			parentClean = strings.ToLower(parentClean)
			candClean = strings.ToLower(candClean)
		}
		if parentClean == candClean {
			return false
		}
		prefix := parentClean
		if !strings.HasSuffix(prefix, string(filepath.Separator)) {
			prefix += string(filepath.Separator)
		}
		if strings.HasPrefix(candClean, prefix) {
			return true
		}
		// On Windows, 8.3 short paths (e.g. RUNNER~1) can cause lexical prefix
		// checks to miss. Check whether candidate or any of its ancestors refers
		// to the same directory on disk.
		for cur := filepath.Clean(candidate); ; {
			next := filepath.Dir(cur)
			if next == cur || next == "." || next == "" {
				break
			}
			cur = next
			if sameDirectoryTreeLocation(true, parent, cur) {
				return true
			}
		}
		return false
	}
	parentClean := path.Clean(parent)
	candClean := path.Clean(candidate)
	if parentClean == candClean {
		return false
	}
	prefix := parentClean
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return strings.HasPrefix(candClean, prefix)
}

// validFileMoveTarget reports whether entries from sourceDir can be moved into
// targetDir without being a no-op or moving a directory into itself/a child.
func validFileMoveTarget(local bool, sourceDir string, entries []sftpclient.Entry, targetDir string) bool {
	if sourceDir == "" || targetDir == "" || len(entries) == 0 {
		return false
	}
	srcDir := cleanSFTPPath(local, sourceDir)
	dstDir := cleanSFTPPath(local, targetDir)
	if sameSFTPPath(local, srcDir, dstDir) {
		return false
	}
	for _, entry := range entries {
		if entry.Name == "" {
			return false
		}
		if entry.IsDir() && !entry.IsLink() {
			src := joinSFTPPath(local, srcDir, entry.Name)
			if sameSFTPPath(local, dstDir, src) || isSFTPSubpath(local, src, dstDir) {
				return false
			}
		}
	}
	return true
}

// moveSFTPEntries queues a server-side move of entries from sourceDir into
// targetDir using the SFTP worker's Rename / MoveSelection operations.
func (state *State) moveSFTPEntries(view *sftpView, sourceDir string, entries []sftpclient.Entry, targetDir string) bool {
	if view == nil || view.worker == nil || view.busy {
		return false
	}
	if !validFileMoveTarget(false, sourceDir, entries, targetDir) {
		return false
	}
	srcDir := path.Clean(sourceDir)
	dstDir := path.Clean(targetDir)
	view.moveAfterRename = false
	var (
		queued    bool
		operation sftpclient.Operation
		status    string
	)
	if len(entries) > 1 {
		sources := make([]string, 0, len(entries))
		for _, entry := range entries {
			sources = append(sources, path.Join(srcDir, entry.Name))
		}
		queued = view.worker.MoveSelection(sources, dstDir)
		operation = sftpclient.OperationMoveSelection
		status = fmt.Sprintf(state.messages.SFTPMovingCountFmt, len(sources))
	} else {
		source := path.Join(srcDir, entries[0].Name)
		destination := path.Join(dstDir, entries[0].Name)
		queued = view.worker.Rename(source, destination)
		operation = sftpclient.OperationRename
		status = fmt.Sprintf(state.messages.SFTPMovingFmt, entries[0].Name)
		view.moveAfterRename = queued
	}
	if !queued {
		view.errorText = state.messages.SFTPUnavailable
		return false
	}
	view.moveTargetDir = dstDir
	view.busy = true
	view.operation = operation
	view.status = status
	view.errorText = ""
	view.invalidateDetached()
	return true
}

// localMoveResult records the outcome of moving local entries on a goroutine.
type localMoveResult struct {
	sourceDir string
	targetDir string
	total     int
	moved     []string
	err       error
}

// moveLocalEntries moves local entries from sourceDir into targetDir off the
// window thread, matching submitLocalDelete's async lifecycle.
func (state *State) moveLocalEntries(view *sftpView, sourceDir string, entries []sftpclient.Entry, targetDir string) bool {
	if view == nil || view.localBusy {
		return false
	}
	if !validFileMoveTarget(true, sourceDir, entries, targetDir) {
		return false
	}
	cloned := slices.Clone(entries)
	srcDir := filepath.Clean(sourceDir)
	dstDir := filepath.Clean(targetDir)
	view.errorText = ""
	view.localBusy = true
	if len(cloned) > 1 {
		view.status = fmt.Sprintf(state.messages.SFTPMovingCountFmt, len(cloned))
	} else {
		view.status = fmt.Sprintf(state.messages.SFTPMovingFmt, cloned[0].Name)
	}
	view.invalidateDetached()
	go func() {
		result := runLocalMove(srcDir, dstDir, cloned)
		state.aiPost(func(*State) { state.finishLocalMove(view, result) })
	}()
	return true
}

func runLocalMove(sourceDir, targetDir string, entries []sftpclient.Entry) localMoveResult {
	result := localMoveResult{
		sourceDir: sourceDir,
		targetDir: targetDir,
		total:     len(entries),
	}
	var failures []error
	for _, entry := range entries {
		src := filepath.Join(sourceDir, entry.Name)
		dst := filepath.Join(targetDir, entry.Name)
		if _, err := os.Lstat(dst); err == nil {
			failures = append(failures, fmt.Errorf("%s: %w", dst, os.ErrExist))
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Errorf("%s: %w", dst, err))
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", src, err))
			continue
		}
		result.moved = append(result.moved, dst)
	}
	result.err = errors.Join(failures...)
	return result
}

func (state *State) finishLocalMove(view *sftpView, result localMoveResult) {
	session, ok := state.sftpSession(view)
	if !ok || session.files != view || !view.localBusy {
		return
	}
	view.localBusy = false
	messages := state.messages
	if (view.local || view.detached != nil) &&
		(sameSFTPPath(true, view.lfolder.path, result.sourceDir) || sameSFTPPath(true, view.lfolder.path, result.targetDir)) {
		state.listLocal(view, view.lfolder.path)
	}
	if view.detached != nil {
		view.detached.refreshDirectoryTreeNode(true, result.sourceDir)
		view.detached.refreshDirectoryTreeNode(true, result.targetDir)
	}
	if result.err != nil {
		detail := fmt.Sprintf(messages.SFTPErrorFmt, result.err)
		view.status = detail
		view.errorText = detail
		state.fileOpenNotice(view.detached, messages.SFTPMoveTitle, detail, components.ToastError)
	} else if result.total > 1 {
		view.status = fmt.Sprintf(messages.SFTPMovedCountFmt, len(result.moved), result.targetDir)
	} else if len(result.moved) == 1 {
		view.status = fmt.Sprintf(messages.SFTPMovedFmt, filepath.Join(result.sourceDir, filepath.Base(result.moved[0])), result.moved[0])
	}
	view.invalidateDetached()
}

func (view *sftpView) cancelPanelDrag() {
	if view == nil {
		return
	}
	view.panelDrag.state.Cancel()
	view.panelDrag.preSelect = nil
	view.panelDrag.restoredFor = gfx.ID{}
	view.flushDeferredPanelListings()
}

func (view *sftpView) flushDeferredPanelListings() {
	pending := view.deferredPanelListings
	view.deferredPanelListings = [2]func(){}
	for _, apply := range pending {
		if apply != nil {
			apply()
		}
	}
}

func (state *State) sftpPanelCanDrag(view *sftpView, folder *sftpFolder) bool {
	if view == nil || folder == nil || folder.searching() || folder.path == "" {
		return false
	}
	if view.dialog != sftpDialogNone || view.editPath != "" || view.bookmarksOpen || folder.headerMenuOpen {
		return false
	}
	if view.local {
		return !view.localBusy
	}
	return !view.busy && view.worker != nil
}

// ensureSFTPPanelDrag makes the DragState callbacks for view once so warm
// paints do not allocate closures.
func (state *State) ensureSFTPPanelDrag(view *sftpView) *sftpPanelDrag {
	drag := &view.panelDrag
	if drag.accept == nil {
		drag.accept = func(source, target gfx.ID) bool {
			return state.sftpPanelDragAccept(view, source, target)
		}
		drag.drop = func(source, target gfx.ID) {
			state.sftpPanelDragDrop(view, source, target)
		}
		drag.state.OnMove = func(event gfx.PointerEvent) {
			state.sftpPanelDragMoved(view, event)
		}
	}
	return drag
}

// sftpPanelDragMoved lets a drag of a row leave the application: once the
// pointer is outside every Motty window, the desktop takes the gesture and the
// files are offered to it. Until then the panel keeps its own move targets.
func (state *State) sftpPanelDragMoved(view *sftpView, event gfx.PointerEvent) {
	if event.Kind != gfx.PointerMove || !pointOutsideViewport(event) || !view.panelDrag.state.Dragging() {
		return
	}
	if state.ownWindowAt(gfx.Point{X: event.ScreenX, Y: event.ScreenY}, nil) {
		return
	}
	folder := view.active()
	if !state.sftpPanelCanDrag(view, folder) {
		return
	}
	entries := state.sftpPanelDraggedEntries(view, view.panelDrag.state.Source())
	if len(entries) == 0 {
		return
	}
	directory, local := folder.path, view.local
	view.cancelPanelDrag()
	state.dragFilesOut(nil, view, entries, directory, local)
}

func (state *State) sftpPanelDraggedEntries(view *sftpView, source gfx.ID) []sftpclient.Entry {
	folder := view.active()
	srcIdx, ok := folder.listRow(source)
	if !ok || srcIdx < 0 || srcIdx >= len(folder.rows) {
		return nil
	}
	drag := &view.panelDrag
	if len(drag.preSelect) > 1 && drag.local == view.local && drag.sourceDir == folder.path && slices.Contains(drag.preSelect, srcIdx) {
		if drag.restoredFor != source {
			for _, idx := range drag.preSelect {
				if idx >= 0 && idx < len(folder.rows) {
					folder.rows[idx].selected = true
					folder.rows[idx].cells[0] = sftpSelectionCell(true)
				}
			}
			folder.listNeedsSeed = true
			view.setItemCountStatus(state.messages.SFTPItemsFmt, selectedSFTPCount(view))
			drag.restoredFor = source
		}
	}
	if folder.rows[srcIdx].selected {
		entries := make([]sftpclient.Entry, 0, len(folder.rows))
		for _, row := range folder.rows {
			if row.selected {
				entries = append(entries, row.entry)
			}
		}
		if len(entries) > 0 {
			return entries
		}
	}
	return []sftpclient.Entry{folder.rows[srcIdx].entry}
}

func (state *State) sftpPanelDragTargetDir(view *sftpView, target gfx.ID) (string, bool) {
	folder := view.active()
	if index, ok := folder.listRow(target); ok {
		if index >= 0 && index < len(folder.rows) {
			entry := folder.rows[index].entry
			if entry.IsDir() && !entry.LinkBroken {
				return joinSFTPPath(view.local, folder.path, entry.Name), true
			}
		}
		return "", false
	}
	if target.Scope() == hitSFTPBreadcrumbList {
		index := item(target)
		if index >= 0 && index < len(folder.crumbs)-1 {
			return folder.crumbs[index].path, true
		}
	}
	return "", false
}

func (state *State) sftpPanelDragAccept(view *sftpView, source, target gfx.ID) bool {
	folder := view.active()
	if !state.sftpPanelCanDrag(view, folder) {
		return false
	}
	entries := state.sftpPanelDraggedEntries(view, source)
	targetDir, ok := state.sftpPanelDragTargetDir(view, target)
	if !ok {
		return false
	}
	return validFileMoveTarget(view.local, folder.path, entries, targetDir)
}

func (state *State) sftpPanelDragDrop(view *sftpView, source, target gfx.ID) {
	defer func() {
		view.panelDrag.preSelect = nil
		view.panelDrag.restoredFor = gfx.ID{}
		view.flushDeferredPanelListings()
	}()
	folder := view.active()
	if !state.sftpPanelCanDrag(view, folder) {
		return
	}
	entries := state.sftpPanelDraggedEntries(view, source)
	targetDir, ok := state.sftpPanelDragTargetDir(view, target)
	if !ok {
		return
	}
	if view.local {
		state.moveLocalEntries(view, folder.path, entries, targetDir)
	} else {
		state.moveSFTPEntries(view, folder.path, entries, targetDir)
	}
}

func sftpBreadcrumbMoveTarget(folder *sftpFolder, local bool, sourceDir string, entries []sftpclient.Entry, x, y float32) sftpMoveTarget {
	if folder == nil || folder.pathEditing {
		return sftpMoveTarget{}
	}
	for _, crumb := range folder.crumbBoxes {
		rect := crumb.box.Rect()
		if rect.Width <= 0 || rect.Height <= 0 {
			continue
		}
		hit := gfx.Rect{X: rect.X - 3, Y: rect.Y - 3, Width: rect.Width + 6, Height: rect.Height + 6}
		if pointInRect(x, y, hit) && validFileMoveTarget(local, sourceDir, entries, crumb.path) {
			return sftpMoveTarget{
				kind: sftpMoveTargetCrumb,
				dir:  crumb.path,
				row:  -1,
				rect: hit,
			}
		}
	}
	return sftpMoveTarget{}
}

func (state *State) paintSFTPPanelDrag(canvas *gfx.Canvas, s *sftpPanelScratch, view *sftpView) {
	drag := &view.panelDrag
	if !drag.state.Dragging() {
		view.flushDeferredPanelListings()
		return
	}
	folder := view.active()
	if target := drag.state.Target(); !target.IsZero() {
		if index, ok := folder.listRow(target); ok && index >= 0 && index < len(folder.rows) {
			body := s.listSlot.Rect()
			rowWidth := body.Width
			if s.rowWidth > 0 && s.rowWidth < rowWidth {
				rowWidth = s.rowWidth
			}
			rect := gfx.Rect{
				X:      body.X,
				Y:      body.Y + float32(index)*sftpRowHeight - folder.scrollOffset,
				Width:  rowWidth,
				Height: sftpRowHeight,
			}
			canvas.Clip(body)
			state.paintSFTPDropOverlayWithIcon(canvas, rect, state.messages.SFTPWindow.DropMoveHint, iconFolderOpen)
			canvas.Unclip()
		} else if target.Scope() == hitSFTPBreadcrumbList {
			if idx := item(target); idx >= 0 && idx < len(folder.crumbs) {
				targetPath := folder.crumbs[idx].path
				for _, crumb := range folder.crumbBoxes {
					if crumb.path == targetPath {
						if rect := crumb.box.Rect(); rect.Width > 0 && rect.Height > 0 {
							hit := gfx.Rect{X: rect.X - 3, Y: rect.Y - 3, Width: rect.Width + 6, Height: rect.Height + 6}
							paintSFTPBreadcrumbDropHighlight(canvas, hit, state.pal)
						}
						break
					}
				}
			}
		}
	}
	entries := state.sftpPanelDraggedEntries(view, drag.state.Source())
	paintSFTPDragBadge(canvas, drag.state.At(), entries, state.pal)
}

func paintSFTPBreadcrumbDropHighlight(canvas *gfx.Canvas, rect gfx.Rect, pal *palette) {
	if rect.Width <= 0 || rect.Height <= 0 {
		return
	}
	canvas.PushLayer(popupLayer)
	defer canvas.PopLayer()
	canvas.Rounded(rect, cornerPx(4), withAlpha(pal.accent, 0.14))
	canvas.RoundedOutline(rect, 4, 1.5, pal.accent)
}

func paintSFTPDragBadge(canvas *gfx.Canvas, at gfx.Point, entries []sftpclient.Entry, pal *palette) {
	if len(entries) == 0 {
		return
	}
	label := entries[0].Name
	if len(entries) > 1 {
		label = fmt.Sprintf("%s (+%d)", entries[0].Name, len(entries)-1)
	}
	style := gfx.TextStyle{Size: 12, Weight: gfx.WeightMedium, Color: pal.text}
	const maxWidth = float32(200)
	textWidth := min(canvas.TextWidthWeight(label, style.Size, style.Weight), maxWidth-32)
	label = elide(canvas, label, style.Size, style.Weight, textWidth)
	pill := gfx.Rect{
		X:      at.X + 12,
		Y:      at.Y + 12,
		Width:  textWidth + 32,
		Height: 26,
	}
	canvas.PushLayer(tooltipLayer)
	defer canvas.PopLayer()
	canvas.RoundedBorder(pill, cornerPx(6), 1, pal.accent, withAlpha(pal.surface, 0.96))
	glyph := uint8(iconFile)
	if entries[0].IsDir() {
		glyph = iconFolderOpen
	}
	paintIcon(canvas, glyph, gfx.Rect{X: pill.X + 8, Y: pill.Y + 6, Width: 14, Height: 14}, pal.accent)
	canvas.Label(label, gfx.Rect{X: pill.X + 26, Y: pill.Y, Width: textWidth, Height: pill.Height}, style)
}
