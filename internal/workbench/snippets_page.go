package workbench

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/workbench/icons"
)

const (
	snippetTreeDefaultWidth = float32(260)
	snippetTreeMinWidth     = float32(180)
	snippetTreeMaxWidth     = float32(420)
	snippetEditorMinWidth   = float32(360)
	snippetTreeRowHeight    = float32(30)
	snippetTreeIndent       = float32(16)
	// snippetInlineSaveDelay coalesces rapid typing in the Snippets page's
	// inline editor into one store write once typing settles.
	snippetInlineSaveDelay = 400 * time.Millisecond
)

// refreshSnippets updates derived display cells, group counts, tree rows,
// selection, and the inline editor after the snippet list or language changes.
func (state *State) refreshSnippets() {
	form := &state.snippets
	for index := range form.list {
		entry := &form.list[index]
		command := entry.Command
		shortcut := ""
		if entry.Shortcut.Valid() {
			shortcut = chordLabel(entry.Shortcut)
		}
		entry.cells = [4]string{entry.Name, entry.Group, shortcut, strings.ReplaceAll(command, "\n", " ↵ ")}
	}
	form.selected = max(0, min(form.selected, len(form.list)-1))
	form.refreshSnippetGroups()
	state.rebuildSnippetTreeRows()
	form.syncRowSelection()
	state.syncInlineSnippet(false)
}

// snippetsSectionUI is what the snippets page's declaration leaves for its
// paint and tests.
type snippetsSectionUI struct {
	tree     ui.Box
	treeRect gfx.Rect
	send     ui.Box
	empty    ui.Box
	draw     func(*ui.DrawContext)
}

// prepareSnippetTree initializes the Tree and Splitter callbacks once so
// steady-state paints allocate nothing.
func (state *State) prepareSnippetTree() {
	form := &state.snippets
	if form.treeNode != nil {
		return
	}
	if form.treeWidth <= 0 {
		form.treeWidth = snippetTreeDefaultWidth
	}
	if form.treeSplitter.Pos == 0 {
		form.treeSplitter.Pos = form.treeWidth
	}
	form.tree.RowHeight = snippetTreeRowHeight
	form.tree.Indent = snippetTreeIndent
	form.treeNode = state.snippetTreeNode
	form.treeLabel = state.snippetTreeLabel
	form.treeContent = state.declareSnippetTreeRow
	form.treeKey = state.snippetTreeKey
	form.treeCanDrop = state.snippetTreeCanDrop
	form.tree.Node = form.treeNode
	form.tree.Key = form.treeKey
	form.tree.Label = form.treeLabel
	form.tree.CanDrop = form.treeCanDrop
	form.treeAfterLayout = func(*ui.DrawContext) { state.snippetTreeAfterLayout() }
	form.recordTreeRect = func(*ui.DrawContext) {
		if view := &state.sectionBodies.snippets; view.tree.Valid() {
			view.treeRect = view.tree.Rect()
		}
	}
	form.drawInlineFields = func(d *ui.DrawContext) { state.paintSnippetInlineFields(d.Canvas) }
}

func (state *State) snippetTreeAfterLayout() {
	form := &state.snippets
	if form.treeSplitter.Pos == 0 {
		return
	}
	usable := max(1, state.catalogSectionBounds().Width-1)
	width := min(max(form.treeSplitter.Pos, snippetTreeMinWidth), min(snippetTreeMaxWidth, max(snippetTreeMinWidth, usable-snippetEditorMinWidth)))
	if width != form.treeSplitter.Pos && !form.treeSplitter.Dragging() {
		form.treeSplitter.Pos = width
	}
	if width != form.treeWidth {
		form.treeWidth = width
	}
}

func (state *State) snippetTreeFolderToken(groupKey string) int {
	form := &state.snippets
	if form.treeFolderTokens == nil {
		form.treeFolderTokens = make(map[string]int)
	}
	if token, ok := form.treeFolderTokens[groupKey]; ok {
		return token
	}
	token := len(form.treeFolderTokens)
	form.treeFolderTokens[groupKey] = token
	return token
}

func (state *State) snippetTreeKey(i int) int {
	rows := state.snippets.treeRows
	if i < 0 || i >= len(rows) {
		return 0
	}
	e := rows[i]
	if e.folder {
		return 1<<20 + state.snippetTreeFolderToken(e.groupKey)
	}
	return e.snippet
}

func (state *State) snippetTreeNode(i int) ui.TreeNode {
	rows := state.snippets.treeRows
	if i < 0 || i >= len(rows) {
		return ui.TreeNode{}
	}
	e := rows[i]
	if e.folder {
		expanded := state.snippets.treeSearching || !state.snippets.treeCollapsedGroups[e.groupKey]
		return ui.TreeNode{
			Depth:    e.depth,
			Branch:   true,
			Expanded: expanded,
		}
	}
	return ui.TreeNode{
		Depth:    e.depth,
		Selected: e.snippet == state.snippets.selected,
	}
}

func (state *State) snippetTreeLabel(i int) string {
	rows := state.snippets.treeRows
	if i < 0 || i >= len(rows) {
		return ""
	}
	return rows[i].label
}

// rebuildSnippetTreeRows flattens the group hierarchy and snippets into
// form.treeRows in pre-order (subfolders first, then snippets directly in that
// folder; ungrouped snippets at root depth 0).
func (state *State) rebuildSnippetTreeRows() {
	form := &state.snippets
	query := strings.ToLower(strings.TrimSpace(form.treeSearch.String()))
	form.treeSearching = query != ""
	matches := make([]bool, len(form.list))
	matchCount := 0
	matchingFolders := make(map[string]int)
	for i := range form.list {
		entry := &form.list[i]
		if !form.filter.matches(entry) {
			continue
		}
		if query != "" {
			if !strings.Contains(strings.ToLower(entry.Name), query) &&
				!strings.Contains(strings.ToLower(entry.Group), query) &&
				!strings.Contains(strings.ToLower(entry.Command), query) &&
				!strings.Contains(strings.ToLower(entry.cells[2]), query) {
				continue
			}
		}
		matches[i] = true
		matchCount++
		for path := entry.Group; path != ""; path = snippetGroupParent(path) {
			matchingFolders[strings.ToLower(path)]++
		}
	}

	form.treeRows = form.treeRows[:0]
	var walk func(parent string, depth int)
	walk = func(parent string, depth int) {
		for _, group := range form.groups {
			if !sameSnippetGroup(snippetGroupParent(group), parent) {
				continue
			}
			key := strings.ToLower(group)
			count := matchingFolders[key]
			if query != "" && count == 0 && !strings.Contains(key, query) {
				continue
			}
			// Pre-register stable folder token before paint.
			state.snippetTreeFolderToken(key)
			form.treeRows = append(form.treeRows, snippetTreeEntry{
				folder:    true,
				group:     group,
				groupKey:  key,
				label:     snippetGroupLeaf(group),
				countText: strconv.Itoa(count),
				depth:     depth,
				snippet:   -1,
			})
			if query != "" || !form.treeCollapsedGroups[key] {
				walk(group, depth+1)
			}
		}
		for i := range form.list {
			if !matches[i] {
				continue
			}
			if !sameSnippetGroup(form.list[i].Group, parent) {
				continue
			}
			form.treeRows = append(form.treeRows, snippetTreeEntry{
				folder:  false,
				group:   parent,
				label:   form.list[i].Name,
				depth:   depth,
				snippet: i,
			})
		}
	}
	walk("", 0)

	// Keep form.visible and countText synchronized with the active search/filter.
	if query != "" {
		form.visible = form.visible[:0]
		for i := range form.list {
			if matches[i] {
				form.visible = append(form.visible, i)
			}
		}
		if len(form.visible) > 0 && !slices.Contains(form.visible, form.selected) {
			form.selected = form.visible[0]
		}
	}
	form.countText = fmt.Sprintf(state.messages.Tools.SnippetCountFmt, len(form.list))
	if len(form.visible) != len(form.list) {
		form.countText = fmt.Sprintf(state.messages.Tools.SnippetCountFilteredFmt, len(form.visible), len(form.list))
	}
}

func (state *State) snippetTreeCanExpandAll() bool {
	form := &state.snippets
	for _, g := range form.groups {
		if form.treeCollapsedGroups[strings.ToLower(g)] {
			return true
		}
	}
	return false
}

func (state *State) snippetTreeExpandAncestors(group string) {
	if group == "" || len(state.snippets.treeCollapsedGroups) == 0 {
		return
	}
	for path := group; path != ""; path = snippetGroupParent(path) {
		delete(state.snippets.treeCollapsedGroups, strings.ToLower(path))
	}
}

func (state *State) snippetTreeCanDrop(from, to int, where ui.TreeDrop) bool {
	form := &state.snippets
	if state.activeSection != sectionSnippets || state.modal != modalNone || strings.TrimSpace(form.treeSearch.String()) != "" {
		return false
	}
	rows := form.treeRows
	if from < 0 || from >= len(rows) || to < 0 || to >= len(rows) || from == to {
		return false
	}
	src, dst := rows[from], rows[to]
	if !src.folder {
		switch where {
		case ui.TreeDropInside:
			return dst.folder
		case ui.TreeDropBefore, ui.TreeDropAfter:
			return true
		}
		return false
	}
	// Dragging a folder.
	if !dst.folder {
		return false
	}
	var targetParent string
	switch where {
	case ui.TreeDropInside:
		targetParent = dst.group
	case ui.TreeDropBefore, ui.TreeDropAfter:
		targetParent = snippetGroupParent(dst.group)
	default:
		return false
	}
	return state.snippetFolderMoveTarget(src.group, targetParent) != ""
}

// snippetFolderMoveTarget computes the destination path for moving folder src
// under targetParent ("" for root), returning "" when the move is invalid, a
// no-op, or conflicts with an existing sibling group.
func (state *State) snippetFolderMoveTarget(src, targetParent string) string {
	if src == "" {
		return ""
	}
	if targetParent != "" && snippetGroupWithin(targetParent, src) {
		return ""
	}
	leaf := snippetGroupLeaf(src)
	dest := leaf
	if targetParent != "" {
		dest = targetParent + "/" + leaf
	}
	dest = normalizeSnippetGroupPath(dest)
	if !validSnippetGroup(dest) || sameSnippetGroup(dest, src) {
		return ""
	}
	for _, group := range state.snippets.groups {
		if !snippetGroupWithin(group, src) {
			continue
		}
		remapped := remapSnippetGroupPath(group, src, dest)
		if !validSnippetGroup(remapped) {
			return ""
		}
		for _, existing := range state.snippets.groups {
			if snippetGroupWithin(existing, src) {
				continue
			}
			if sameSnippetGroup(existing, remapped) {
				return ""
			}
		}
	}
	return dest
}

func (state *State) dropSnippetTreeRow(move ui.TreeMove) {
	form := &state.snippets
	rows := form.treeRows
	if move.From < 0 || move.From >= len(rows) || move.To < 0 || move.To >= len(rows) || move.From == move.To {
		return
	}
	src, dst := rows[move.From], rows[move.To]
	if src.folder {
		if !dst.folder {
			return
		}
		targetParent := dst.group
		if move.Where != ui.TreeDropInside {
			targetParent = snippetGroupParent(dst.group)
		}
		dest := state.snippetFolderMoveTarget(src.group, targetParent)
		if dest == "" {
			return
		}
		if state.renameSnippetGroup(src.group, dest) {
			state.snippetTreeExpandAncestors(dest)
			state.rebuildSnippetTreeRows()
		}
		return
	}
	// Moving a snippet.
	srcIdx := src.snippet
	if srcIdx < 0 || srcIdx >= len(form.list) {
		return
	}
	if dst.folder {
		destGroup := dst.group
		if move.Where != ui.TreeDropInside {
			destGroup = snippetGroupParent(dst.group)
		}
		if sameSnippetGroup(form.list[srcIdx].Group, destGroup) {
			return
		}
		form.list[srcIdx].Group = destGroup
		form.selected = srcIdx
		state.snippetTreeExpandAncestors(destGroup)
		form.resetSnippetPreview()
		state.refreshSnippets()
		state.save()
		return
	}
	// Dropping a snippet before/after another snippet: adopt its group and reorder.
	dstIdx := dst.snippet
	if dstIdx < 0 || dstIdx >= len(form.list) || srcIdx == dstIdx {
		return
	}
	entry := form.list[srcIdx]
	entry.Group = form.list[dstIdx].Group
	to := dstIdx
	if move.Where == ui.TreeDropAfter {
		to = dstIdx + 1
	}
	if srcIdx < to {
		to--
		copy(form.list[srcIdx:to], form.list[srcIdx+1:to+1])
	} else {
		copy(form.list[to+1:srcIdx+1], form.list[to:srcIdx])
	}
	form.list[to] = entry
	form.selected = to
	state.snippetTreeExpandAncestors(entry.Group)
	form.resetSnippetPreview()
	state.refreshSnippets()
	state.save()
}

func (state *State) clickSnippetsPage(id gfx.ID) bool {
	if state.activeSection != sectionSnippets || state.modal != modalNone {
		return false
	}
	form := &state.snippets
	if state.clickSnippetInlineGroup(id) {
		return true
	}
	if id.Scope() == hitSnippetFolderMore {
		token := item(id)
		for _, row := range form.treeRows {
			if row.folder && state.snippetTreeFolderToken(row.groupKey) == token {
				at := form.folderMenu.at
				if at == (gfx.Point{}) {
					at = gfx.Point{X: 120, Y: 120}
				}
				form.folderMenu = snippetFolderMenu{
					open:  true,
					group: row.group,
					at:    at,
				}
				return true
			}
		}
		return true
	}
	switch id {
	case hitSnippetAdd:
		state.startInlineSnippetDraft(form.selectedFolder)
	case hitSnippetGroups:
		state.openSnippetGroupsManager()
	case hitSnippetSend:
		if form.inline.draft {
			return true
		}
		state.commitInlineSnippetPendingGroup()
		state.sendSnippetFromPage(form.selected, form.sendAt)
	case hitSnippetRemove:
		if form.inline.draft {
			form.inline.draft = false
			form.inline.savedDraft = nil
			state.syncInlineSnippet(true)
			return true
		}
		state.openSnippetDelete(form.selected)
	case hitSnippetTreeToggle:
		bounds := state.catalogSectionBounds()
		width := form.treeWidth
		if width <= 0 {
			width = snippetTreeDefaultWidth
		}
		if bounds.Width < width+snippetEditorMinWidth {
			form.treeDrawer = !form.treeDrawer
		} else {
			form.treeCollapsed = !form.treeCollapsed
		}
	case hitSnippetTreeSearchToggle:
		state.flushInlineSnippetSave()
		form.treeSearchOpen = !form.treeSearchOpen
		if form.treeSearchOpen {
			form.treeFocus = hitSnippetTreeSearch
		} else if form.treeSearch.String() != "" {
			form.treeSearch = text.Editor{}
			state.refreshSnippets()
		}
	case hitSnippetTreeExpandAll:
		if len(form.groups) == 0 || strings.TrimSpace(form.treeSearch.String()) != "" {
			return true
		}
		if state.snippetTreeCanExpandAll() {
			clear(form.treeCollapsedGroups)
		} else {
			if form.treeCollapsedGroups == nil {
				form.treeCollapsedGroups = make(map[string]bool)
			}
			for _, g := range form.groups {
				form.treeCollapsedGroups[strings.ToLower(g)] = true
			}
		}
		state.rebuildSnippetTreeRows()
		form.syncRowSelection()
	case hitSnippetInlineRun:
		if form.inline.draft {
			form.inline.groupPicker.Open = false
			form.inline.loadedSnap.Run = !form.inline.loadedSnap.Run
		} else if form.selected >= 0 && form.selected < len(form.list) {
			form.inline.groupPicker.Open = false
			form.list[form.selected].Run = !form.list[form.selected].Run
			form.inline.loadedSnap.Run = form.list[form.selected].Run
			state.save()
		}
	case hitSnippetInlineLines:
		if form.inline.draft {
			form.inline.groupPicker.Open = false
			form.inline.loadedSnap.Lines = !form.inline.loadedSnap.Lines
		} else if form.selected >= 0 && form.selected < len(form.list) {
			form.inline.groupPicker.Open = false
			form.list[form.selected].Lines = !form.list[form.selected].Lines
			form.inline.loadedSnap.Lines = form.list[form.selected].Lines
			state.save()
		}
	case hitSnippetInlineShortcut:
		state.flushInlineSnippetSave()
		form.inline.capturing = !form.inline.capturing
		form.inline.error = ""
		state.refreshInlineShortcutLabel()
	case hitSnippetInlineShortcutOff:
		if form.inline.draft {
			form.inline.loadedSnap.Shortcut = keymap.Chord{}
			form.inline.capturing = false
			form.inline.error = ""
			state.refreshInlineShortcutLabel()
		} else if form.selected >= 0 && form.selected < len(form.list) {
			form.list[form.selected].Shortcut = keymap.Chord{}
			form.inline.capturing = false
			form.inline.error = ""
			form.inline.loadedSnap.Shortcut = keymap.Chord{}
			state.refreshSnippets()
			state.save()
		}
	default:
		return false
	}
	return true
}

// declareSnippetsWorkspace declares the Snippets page: an empty state when no
// commands exist, or a split directory-tree browser + inline editor workspace.
func (state *State) declareSnippetsWorkspace(c ui.C) {
	pal, m := state.pal, state.messages.Tools
	form := &state.snippets
	bounds := state.catalogSectionBounds()
	view := &state.sectionBodies.snippets
	draw := view.draw
	*view = snippetsSectionUI{}
	if draw == nil {
		draw = func(d *ui.DrawContext) { state.paintSnippetsWorkspace(d.Canvas) }
	}
	view.draw = draw

	if len(form.list) == 0 && !form.inline.draft {
		body := ui.Column(c).Fill().PadSides(12, 18, 0, 18)
		body.Children(func() {
			ui.Row(c).MinHeight(58).Gap(10).MarginSides(0, 0, 12, 0).Items(ui.Start).Children(func() {
				ui.Column(c).Grow(1).Gap(3).MarginSides(4, 0, 0, 0).Children(func() {
					ui.Text(c, m.SnippetsTitle).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis()
					ui.Paragraph(c, m.SnippetsPageDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted)
				})
				addWidth := toolbarButtonWidth(c.Canvas(), m.SnippetAdd, true)
				state.declareButtonSized(c, hitSnippetAdd, m.SnippetAdd, iconPlus, true, false, true, toolbarFont, false).Size(addWidth, toolbarHeight)
			})
			view.empty = state.reserveEmptyPage(c, bounds, m.SnippetEmpty, m.SnippetEmptySub, m.SnippetAdd, hitSnippetAdd, iconSnippets, true)
		})
		body.Clip().DrawOver(view.draw)
		return
	}

	state.prepareSnippetTree()
	if !form.inline.draft && (!form.inline.loadedValid || form.inline.loadedIndex != form.selected) {
		state.syncInlineSnippet(false)
	}
	width := form.treeWidth
	if width <= 0 {
		width = snippetTreeDefaultWidth
	}
	narrow := bounds.Width < width+snippetEditorMinWidth
	collapsed := form.treeCollapsed || narrow
	if collapsed {
		root := ui.Column(c).Fill().DrawOver(view.draw)
		root.Children(func() {
			state.declareSnippetInlineEditor(c, bounds.Width, true)
		})
		if form.treeDrawer {
			state.declareSnippetsDrawer(c, width, bounds.Width)
		}
		return
	}
	root := ui.Row(c).Fill().DrawOver(form.treeAfterLayout)
	root.Children(func() {
		ui.Column(c).Width(width).FillHeight().Children(func() {
			state.declareSnippetsTree(c, width)
		})
		form.treeSplitter.StepFraction = .05
		ui.Splitter(c, &form.treeSplitter).SplitterThickness(1).Mins(snippetTreeMinWidth, snippetEditorMinWidth).Label(m.SnippetTreeResize)
		ui.Column(c).Grow(1).FillHeight().DrawOver(view.draw).Children(func() {
			state.declareSnippetInlineEditor(c, bounds.Width-width-1, false)
		})
	})
}

func (state *State) declareSnippetsDrawer(c ui.C, width, boundsWidth float32) {
	form, m := &state.snippets, state.messages.Tools
	extent := min(width, max(snippetTreeMinWidth, boundsWidth-32))
	open := form.treeDrawer
	ui.Drawer(c, &open, m.SnippetsTitle, ui.DrawerSpec{
		Side: ui.DrawerLeft, Size: extent,
	}, func() {
		state.declareSnippetsTree(c, extent)
	}, nil)
	if !open {
		form.treeDrawer = false
	}
}

func (state *State) declareSnippetsTree(c ui.C, width float32) {
	form, m := &state.snippets, state.messages.Tools
	view := &state.sectionBodies.snippets
	form.treeC = c
	canExpandAll := state.snippetTreeCanExpandAll()
	expandLabel, expandIcon := m.SnippetTreeCollapseAll, icons.ListX
	if canExpandAll {
		expandLabel, expandIcon = m.SnippetTreeExpandAll, icons.ListTree
	}
	searching := form.treeSearching
	ui.Row(c).Height(28).MarginSides(8, 8, 0, 8).Gap(2).Items(ui.Center).Children(func() {
		ui.Text(c, m.SnippetsTitle).FontSize(13).Weight(gfx.WeightSemibold).Color(state.pal.text).Grow(1).Ellipsis().MarginSides(0, 4, 0, 4)
		state.declareNoteToolbarButton(c, hitSnippetTreeSearchToggle, m.SnippetTreeSearch, icons.Search, false)
		state.declareNoteToolbarButton(c, hitSnippetTreeExpandAll, expandLabel, expandIcon, len(form.groups) == 0 || searching)
		state.declareNoteToolbarButton(c, hitSnippetGroups, m.SnippetGroupsManage, icons.FolderOpen, len(form.groups) == 0)
	})
	addBtn := ui.Button(c, m.SnippetAdd).Icon(lucideIcons[iconPlus]).
		FillWidth().Height(34).MarginSides(8, 8, 0, 8).Hit(hitSnippetAdd, m.SnippetAdd)
	if len(form.list) >= maxSnippets {
		addBtn.Disabled(true)
	}
	if form.treeSearchOpen {
		if !form.treeSearchBound {
			form.treeSearchField.Bind(&form.treeSearch)
			form.treeSearchBound = true
		}
		field := ui.TextField(c, &form.treeSearchField, m.SnippetPickerPlaceholder, m.SnippetPickerPlaceholder, ui.WithID(hitSnippetTreeSearch)).
			Height(30).FillWidth().MarginSides(8, 8, 0, 8)
		if form.treeFocus == hitSnippetTreeSearch {
			c.Canvas().RequestFocus(hitSnippetTreeSearch)
			form.treeFocus = gfx.ID{}
		}
		if field.Changed() {
			state.rebuildSnippetTreeRows()
			form.tree.ScrollTo(0, ui.ListStart)
			form.syncRowSelection()
		}
	}
	ui.Divider(c).Height(1).MarginSides(8, 8, 0, 8).Background(state.pal.line)
	area := ui.Column(c).Grow(1).FillWidth().MarginSides(6, 6, 4, 6).
		HitWith(hitSnippetTreeScroll, m.SnippetListScroll, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true}).
		DrawOver(form.recordTreeRect)
	area.Children(func() {
		if len(form.treeRows) == 0 {
			ui.Text(c, m.SnippetPickerEmpty).FontSize(12).Color(state.pal.muted).FillWidth().Ellipsis().MarginSides(8, 4, 0, 6)
		}
		view.tree = ui.Tree(c, &form.tree, len(form.treeRows), form.treeLabel, form.treeContent).
			Grow(1).FillWidth().Scrollbars().Label(m.SnippetListScroll)
		state.snippetTreeChanges()
		state.openSnippetBlankMenuOn(c.Canvas(), hitSnippetTreeScroll)
	})
	ui.Divider(c).Height(1).Background(state.pal.line)
	ui.Row(c).Height(32).PadSides(0, 10, 0, 12).Items(ui.Center).Children(func() {
		ui.Text(c, form.countText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.faint).Grow(1).Ellipsis()
	})
}

func (state *State) snippetTreeChanges() {
	form := &state.snippets
	rows := form.treeRows
	if i, ok := form.tree.Toggled(); ok && i >= 0 && i < len(rows) {
		e := rows[i]
		if e.folder {
			state.toggleSnippetTreeFolder(e.groupKey)
		}
	}
	if i, ok := form.tree.Chosen(); ok && i >= 0 && i < len(rows) {
		e := rows[i]
		if e.folder {
			state.flushInlineSnippetSave()
			form.selectedFolder = e.group
			state.toggleSnippetTreeFolder(e.groupKey)
		} else if e.snippet >= 0 && e.snippet < len(form.list) {
			if form.inline.draft {
				state.stashInlineSnippetDraft()
			} else if e.snippet != form.selected {
				state.flushInlineSnippetSave()
			}
			form.inline.draft = false
			form.selected = e.snippet
			form.selectedFolder = form.list[e.snippet].Group
			form.treeDrawer = false
			state.syncInlineSnippet(true)
		}
	}
	if move, ok := form.tree.Dropped(); ok {
		state.dropSnippetTreeRow(move)
	}
}

func (state *State) toggleSnippetTreeFolder(groupKey string) {
	form := &state.snippets
	if form.treeCollapsedGroups == nil {
		form.treeCollapsedGroups = make(map[string]bool)
	}
	if form.treeCollapsedGroups[groupKey] {
		delete(form.treeCollapsedGroups, groupKey)
	} else {
		form.treeCollapsedGroups[groupKey] = true
	}
	state.rebuildSnippetTreeRows()
}

func (state *State) declareSnippetTreeRow(i int) {
	form := &state.snippets
	rows := form.treeRows
	c := form.treeC
	if c == nil || i < 0 || i >= len(rows) {
		return
	}
	e := rows[i]
	rowID := form.tree.RowID(i)
	canvas := c.Canvas()
	hovered := canvas.Hovered() == rowID
	focused := canvas.FocusVisible() == rowID

	if e.folder {
		icon := icons.FolderSolid
		if form.treeSearching || !form.treeCollapsedGroups[e.groupKey] {
			icon = icons.FolderSolidOpen
		}
		token := state.snippetTreeFolderToken(e.groupKey)
		moreID := hitSnippetFolderMore.At(token)
		menuOpen := form.folderMenu.open && sameSnippetGroup(form.folderMenu.group, e.group)
		ui.Row(c).Gap(4).FillWidth().Items(ui.Center).PadSides(0, 4, 0, 2).Children(func() {
			ui.Icon(c, icon).Size(15, 15).Color(state.pal.muted).Shrink(0)
			ui.Text(c, e.label).FontSize(13).Weight(gfx.WeightMedium).Color(state.pal.text).Grow(1).Ellipsis()
			if hovered || focused || menuOpen || canvas.Hovered() == moreID {
				btn := state.noteIconButton(c, moreID, icons.Ellipsis, state.messages.Tools.SnippetFolderMore, 24, true)
				if event, ok := canvas.PointerOn(moreID); ok && event.Kind == gfx.PointerDown {
					form.folderMenu.at = gfx.Point{X: event.X, Y: event.Y}
				} else if rect := btn.Rect(); rect.Width > 0 && canvas.Hovered() == moreID {
					form.folderMenu.at = gfx.Point{X: rect.X, Y: rect.Y + rect.Height}
				}
			} else {
				ui.Text(c, e.countText).FontSize(11).Color(state.pal.faint).Shrink(0).PadSides(0, 4, 0, 4)
			}
		})
		state.openSnippetFolderMenuOn(canvas, rowID, e.group)
		return
	}

	entry := &form.list[e.snippet]
	ui.Row(c).Gap(6).FillWidth().Items(ui.Center).PadSides(0, 6, 0, 2).Children(func() {
		ui.Icon(c, lucideIcons[iconSnippets]).Size(14, 14).Color(state.pal.muted).Shrink(0)
		ui.Text(c, e.label).FontSize(13).Color(state.pal.text).Grow(1).Ellipsis()
		if entry.Shortcut.Valid() {
			ui.Text(c, entry.cells[2]).FontSize(11).Color(state.pal.faint).Shrink(0)
		}
	})
	state.sendSnippetOn(canvas, rowID, e.snippet)
	state.openSnippetMenuOn(canvas, rowID, e.snippet)
}

func (state *State) openSnippetFolderMenuOn(canvas *gfx.Canvas, id gfx.ID, group string) {
	event, ok := canvas.PointerOn(id)
	if !ok || event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonSecondary || state.modal != modalNone || state.activeSection != sectionSnippets {
		return
	}
	state.snippets.folderMenu = snippetFolderMenu{
		open:  true,
		group: group,
		at:    gfx.Point{X: event.X, Y: event.Y},
	}
	canvas.Consume()
}

func (state *State) declareSnippetFolderMenu(c ui.C) {
	menu := &state.snippets.folderMenu
	if !menu.open {
		return
	}
	if state.modal != modalNone || state.activeSection != sectionSnippets || menu.group == "" {
		menu.open = false
		return
	}
	m := state.messages.Tools
	icon := func(kind uint8) gfx.Icon { return lucideIcons[kind] }
	var rows menuRows
	rows.add(components.ContextMenuItem{ID: hitSnippetFolderMenuAdd, Label: m.SnippetFolderAdd, Icon: icon(iconPlus)})
	rows.add(components.ContextMenuItem{ID: hitSnippetFolderMenuNewChild, Label: state.messages.HostGroupActions.NewChild, Icon: icon(iconFolderOpen)})
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitSnippetFolderMenuRename, Label: m.SnippetGroupRename, Icon: icon(iconPencil)})
	rows.add(components.ContextMenuItem{ID: hitSnippetFolderMenuDelete, Label: m.SnippetGroupDelete, Icon: icon(iconTrash), Variant: components.ContextMenuItemDestructive})
	state.declareRowsMenu(c, &menu.open, menu.at, hitSnippetFolderMenuDismiss, &rows, state.chooseSnippetFolderMenu)
}

func (state *State) chooseSnippetFolderMenu(id gfx.ID) {
	group := state.snippets.folderMenu.group
	if group == "" {
		return
	}
	switch id {
	case hitSnippetFolderMenuAdd:
		state.startInlineSnippetDraft(group)
	case hitSnippetFolderMenuNewChild:
		state.openSnippetGroupCreate(group + "/")
	case hitSnippetFolderMenuRename:
		state.openSnippetGroupRenameDirect(group)
	case hitSnippetFolderMenuDelete:
		state.openSnippetGroupDeleteDirect(group)
	}
}

func (state *State) openSnippetBlankMenu(at gfx.Point) {
	state.snippetMenu.open = false
	state.snippets.folderMenu.open = false
	state.snippets.blankMenu = snippetBlankMenu{open: true, at: at}
}

func (state *State) snippetBlankPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if state.modal != modalNone || state.activeSection != sectionSnippets ||
		event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonSecondary {
		return gfx.EventResult{}, false
	}
	if !event.Target.IsZero() && event.Target != hitSnippetTreeScroll {
		return gfx.EventResult{}, false
	}
	area := state.sectionBodies.snippets.treeRect
	if area.Width <= 0 || area.Height <= 0 || !pointInRect(event.X, event.Y, area) {
		return gfx.EventResult{}, false
	}
	state.openSnippetBlankMenu(gfx.Point{X: event.X, Y: event.Y})
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) openSnippetBlankMenuOn(canvas *gfx.Canvas, id gfx.ID) {
	if id.IsZero() {
		return
	}
	event, ok := canvas.PointerOn(id)
	if !ok || event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonSecondary || state.modal != modalNone || state.activeSection != sectionSnippets {
		return
	}
	state.openSnippetBlankMenu(gfx.Point{X: event.X, Y: event.Y})
	canvas.Consume()
}

func (state *State) declareSnippetBlankMenu(c ui.C) {
	menu := &state.snippets.blankMenu
	if !menu.open {
		return
	}
	if state.modal != modalNone || state.activeSection != sectionSnippets {
		menu.open = false
		return
	}
	m := state.messages.Tools
	icon := func(kind uint8) gfx.Icon { return lucideIcons[kind] }
	var rows menuRows
	rows.add(components.ContextMenuItem{ID: hitSnippetBlankNewGroup, Label: state.messages.NewGroup, Icon: icon(iconFolderOpen)})
	rows.add(components.ContextMenuItem{ID: hitSnippetBlankNewSnippet, Label: m.SnippetAdd, Icon: icon(iconPlus)})
	state.declareRowsMenu(c, &menu.open, menu.at, hitSnippetBlankMenuDismiss, &rows, state.chooseSnippetBlankMenu)
}

func (state *State) chooseSnippetBlankMenu(id gfx.ID) {
	switch id {
	case hitSnippetBlankNewGroup:
		state.openSnippetGroupCreate("")
	case hitSnippetBlankNewSnippet:
		state.startInlineSnippetDraft("")
	}
}

// declareSnippetInlineEditor declares the right-pane editor for the selected
// snippet so users can view and edit every field directly alongside the tree.
func (state *State) declareSnippetInlineEditor(c ui.C, width float32, narrow bool) {
	form, m, pal := &state.snippets, state.messages.Tools, state.pal
	view := &state.sectionBodies.snippets
	inline := &form.inline
	var entry *snippet
	if inline.draft {
		entry = &inline.loadedSnap
	} else {
		if form.selected < 0 || form.selected >= len(form.list) {
			return
		}
		entry = &form.list[form.selected]
	}
	focused := c.Canvas().Focused()
	prevFocus := inline.lastFocus
	inline.lastFocus = focused
	if inline.dirty && (prevFocus == hitSnippetInlineName || prevFocus == hitSnippetInlineCommand) && focused != prevFocus {
		state.aiPost(func(state *State) {
			if !state.closed && state.snippets.inline.dirty {
				state.flushInlineSnippetSave()
			}
		})
	}
	condensed := width < 520
	sendWidth, sendIcon := toolbarButtonLayout(c.Canvas(), m.SnippetSend, iconPlay, iconPlay, condensed)
	removeLabel := m.SnippetDelete
	if inline.draft {
		removeLabel = state.messages.Cancel
	}
	removeWidth, removeIcon := toolbarButtonLayout(c.Canvas(), removeLabel, 0, iconTrash, condensed)

	// Editor header bar.
	ui.Row(c).Height(44).FillWidth().PadSides(6, 16, 6, 12).Gap(8).Items(ui.Center).Children(func() {
		state.declareNoteToolbarButton(c, hitSnippetTreeToggle, m.SnippetTreeToggle, icons.PanelLeft, false)
		ui.Text(c, inline.breadcrumb).FontSize(13).Weight(gfx.WeightMedium).Color(pal.muted).Grow(1).Ellipsis()
		view.send = state.declareButtonSized(c, hitSnippetSend, m.SnippetSend, sendIcon, true, false, !inline.draft, toolbarFont, condensed).Size(sendWidth, toolbarHeight)
		view.send.Shrink(0)
		state.declareButtonSized(c, hitSnippetRemove, removeLabel, removeIcon, false, false, true, toolbarFont, condensed).Size(removeWidth, toolbarHeight).Shrink(0)
	})
	ui.Divider(c).Height(1).Background(pal.line)

	// Scrollable editor form.
	scroll := ui.Scroll(c).Grow(1).FillWidth().Scrollbars().Label(m.SnippetsTitle)
	scroll.DrawOver(form.drawInlineFields)
	scroll.Children(func() {
		ui.Column(c).FillWidth().PadSides(16, 20, 20, 20).Gap(10).Children(func() {
			if !inline.nameBound {
				inline.nameField.Bind(&inline.name)
				inline.nameBound = true
			}
			inline.nameField.Invalid = inline.error == m.SnippetErrorName
			ui.FormField(c, &inline.nameForm, m.SnippetName).FillWidth().Height(dialogRowHeight(c.Theme())).Children(func() {
				field := ui.TextField(c, &inline.nameField, m.SnippetName, m.SnippetNamePlaceholder, ui.WithID(hitSnippetInlineName))
				if field.Changed() {
					state.applyInlineSnippetEdits()
				}
			})

			inline.groupSlot = ui.Slot(c).FillWidth().Height(62).Shrink(0)

			cmdHeight := max(float32(180), min(float32(340), state.catalogSectionBounds().Height-420))
			if !inline.commandArea.bound {
				inline.commandArea.input.Bind(&inline.command)
				inline.commandArea.bound = true
			}
			theme := c.Theme()
			titleHeight := theme.Metrics.FontSizeM + 4
			ui.Column(c).FillWidth().Height(cmdHeight).Shrink(0).Gap(theme.Metrics.GapItem / 2).Children(func() {
				// The title, then the help that says how a variable is written in
				// the command; the rest of the row stays empty.
				ui.Row(c).FillWidth().Height(titleHeight).Gap(8).Items(ui.Center).Children(func() {
					ui.Text(c, m.SnippetCommand).FontSize(theme.Metrics.FontSizeM).Weight(gfx.WeightMedium).Color(theme.Foreground).LineHeight(titleHeight).Ellipsis().LabelFor(hitSnippetInlineCommand)
					ui.Icon(c, lucideIcons[iconHelp]).Size(16, 16).Muted().Shrink(0).
						HitWith(hitSnippetInlineCommandHelp, m.SnippetVarNote, gfx.HitOptions{SkipFocus: true}).Tooltip(m.SnippetVarNote)
					ui.Spacer(c).Grow(1)
				})
				area := ui.TextArea(c, &inline.commandArea.input, m.SnippetCommand, m.SnippetCommandPlaceholder, ui.WithID(hitSnippetInlineCommand)).FillWidth().Grow(1)
				if area.Changed() {
					state.applyInlineSnippetEdits()
				}
			})

			state.declareDialogCheck(c, hitSnippetInlineRun, m.SnippetRun, entry.Run, 4, 0).Shrink(0)
			state.declareDialogCheck(c, hitSnippetInlineLines, m.SnippetLines, entry.Lines, dialogCheckGapAbove, 0).Shrink(0)

			ui.Row(c).Height(32).Shrink(0).FillWidth().Items(ui.Center).MarginSides(4, 0, 0, 2).Children(func() {
				ui.Text(c, m.SnippetShortcut).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(20).Grow(1)
				ui.Row(c).Size(240, 32).Shrink(0).Justify(ui.End).Gap(8).Children(func() {
					ui.Button(c, inline.shortcutLbl).Outline().Height(28).Hit(hitSnippetInlineShortcut, inline.shortcutLbl)
					ui.Button(c, m.SnippetShortcutClear).Ghost().Height(28).
						Disabled(!entry.Shortcut.Valid() && !inline.capturing).
						Hit(hitSnippetInlineShortcutOff, m.SnippetShortcutClear)
				})
			})

			if inline.error != "" {
				ui.TextSelectionArea(c, &inline.errorSelection).Children(func() {
					ui.Text(c, inline.error).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.danger).LineHeight(18).MarginSides(4, 0, 0, 0)
				})
			}
		})
	})
}

func (state *State) paintSnippetInlineFields(canvas *gfx.Canvas) {
	state.paintSnippetInlineGroupField(canvas, state.snippets.inline.groupSlot.Rect())
}

// paintSnippetsWorkspace records the Send button anchor and paints the empty
// state icon when the list has no snippets.
func (state *State) paintSnippetsWorkspace(canvas *gfx.Canvas) {
	form := &state.snippets
	view := &state.sectionBodies.snippets
	if view.send.Valid() {
		form.sendAt = gfx.Point{X: view.send.Rect().X, Y: view.send.Rect().Y + toolbarHeight + 4}
	}
	if len(form.list) == 0 && !form.inline.draft {
		state.drawReservedEmptyPage(canvas, view.empty, iconSnippets)
	}
}

// setActiveSection switches the active workbench section and flushes any
// pending inline snippet edits when leaving the Snippets page.
func (state *State) setActiveSection(section uint8) {
	if state.activeSection == sectionSnippets && section != sectionSnippets {
		state.leaveSnippetsSection()
	}
	state.activeSection = section
}

// leaveSnippetsSection flushes coalesced inline snippet saves and clears
// transient focus/capture state when leaving the Snippets page, while keeping
// any in-progress draft or unvalidated edits intact.
func (state *State) leaveSnippetsSection() {
	inline := &state.snippets.inline
	inline.lastFocus = gfx.ID{}
	inline.capturing = false
	inline.groupPicker.Open = false
	state.snippets.folderMenu.open = false
	state.snippets.blankMenu.open = false
	state.sectionBodies.snippets.treeRect = gfx.Rect{}
	if inline.dirty {
		state.flushInlineSnippetSave()
	}
}

// stashInlineSnippetDraft preserves an unfinished new-snippet draft when the
// user switches away to view another snippet in the tree.
func (state *State) stashInlineSnippetDraft() {
	inline := &state.snippets.inline
	if !inline.draft {
		return
	}
	if inline.name.String() != "" || inline.command.String() != "" {
		inline.savedDraft = &snippetNewDraft{
			name:    inline.name.String(),
			command: inline.command.String(),
			snap:    inline.loadedSnap,
			error:   inline.error,
		}
	}
}

// startInlineSnippetDraft opens a blank snippet in the inline editor so adding
// a command uses the same single editing surface as editing an existing one.
func (state *State) startInlineSnippetDraft(defaultGroup string) {
	state.flushInlineSnippetSave()
	form, m := &state.snippets, state.messages.Tools
	if len(form.list) >= maxSnippets {
		form.inline.error = m.SnippetErrorTooMany
		return
	}
	state.setActiveSection(sectionSnippets)
	inline := &form.inline
	inline.draft = true
	inline.loadedValid = true
	inline.loadedIndex = -1
	inline.name = text.Editor{}
	inline.command = text.Editor{}
	inline.commandArea = dialogArea{}
	inline.capturing = false
	if saved := inline.savedDraft; saved != nil {
		inline.loadedSnap = saved.snap
		inline.name.Insert(saved.name)
		inline.command.Insert(saved.command)
		inline.error = saved.error
		inline.savedDraft = nil
	} else {
		group := cleanSnippetGroup(defaultGroup)
		inline.loadedSnap = snippet{Group: group, Run: true}
		inline.error = ""
	}
	form.treeDrawer = false
	state.initInlineGroupPicker(inline.loadedSnap.Group)
	state.refreshInlineMeta(inline.loadedSnap)
	state.pendingFocus = hitSnippetInlineName
}

// focusInlineSnippet switches to the Snippets page and focuses the inline
// editor on the snippet at index (used when editing a chip from the command bar).
func (state *State) focusInlineSnippet(index int) {
	if state.snippets.inline.draft {
		state.stashInlineSnippetDraft()
	} else {
		state.flushInlineSnippetSave()
	}
	form := &state.snippets
	if index < 0 || index >= len(form.list) {
		return
	}
	state.setActiveSection(sectionSnippets)
	form.inline.draft = false
	form.selected = index
	form.selectedFolder = form.list[index].Group
	form.treeDrawer = false
	state.snippetTreeExpandAncestors(form.list[index].Group)
	state.rebuildSnippetTreeRows()
	form.syncRowSelection()
	state.syncInlineSnippet(true)
	state.pendingFocus = hitSnippetInlineName
}

func (state *State) snippetTreeRowForTarget(id gfx.ID) int {
	if id.IsZero() {
		return -1
	}
	for i := range state.snippets.treeRows {
		if state.snippets.tree.RowID(i) == id {
			return i
		}
	}
	return -1
}

func (state *State) snippetTreeRowIDForSnippet(index int) gfx.ID {
	for i, row := range state.snippets.treeRows {
		if !row.folder && row.snippet == index {
			return state.snippets.tree.RowID(i)
		}
	}
	return gfx.ID{}
}

// snippetTreeKeyEvent handles Alt+Up/Alt+Down on a focused snippet row in the
// directory tree, preserving keyboard reordering alongside drag-and-drop.
func (state *State) snippetTreeKeyEvent(event gfx.KeyEvent) (gfx.EventResult, bool) {
	form := &state.snippets
	if state.modal != modalNone || state.activeSection != sectionSnippets || form.inline.capturing || event.Release {
		return gfx.EventResult{}, false
	}
	if event.Modifiers != gfx.ModifierAlt || (event.Key != gfx.KeyUp && event.Key != gfx.KeyDown) {
		return gfx.EventResult{}, false
	}
	rowIdx := state.snippetTreeRowForTarget(event.Target)
	if rowIdx < 0 || rowIdx >= len(form.treeRows) {
		return gfx.EventResult{}, false
	}
	row := form.treeRows[rowIdx]
	if row.folder || strings.TrimSpace(form.treeSearch.String()) != "" {
		return gfx.EventResult{Handled: true}, true
	}
	dir := -1
	if event.Key == gfx.KeyDown {
		dir = 1
	}
	if !state.moveSnippet(row.snippet, dir) {
		return gfx.EventResult{Handled: true}, true
	}
	return gfx.EventResult{Handled: true, Repaint: true, Focus: state.snippetTreeRowIDForSnippet(form.selected)}, true
}

func snippetsSameContent(a, b snippet) bool {
	return a.Name == b.Name && a.Command == b.Command && a.Group == b.Group &&
		a.Run == b.Run && a.Lines == b.Lines && a.Shortcut == b.Shortcut
}

// syncInlineSnippet loads the selected snippet into the inline editor when the
// selection or underlying snippet changed externally.
func (state *State) syncInlineSnippet(force bool) {
	form := &state.snippets
	inline := &form.inline
	if inline.draft {
		state.refreshInlineGroupOptions(inline.loadedSnap.Group)
		state.refreshInlineMeta(inline.loadedSnap)
		return
	}
	if len(form.list) == 0 || form.selected < 0 || form.selected >= len(form.list) {
		inline.loadedValid = false
		inline.capturing = false
		inline.error = ""
		inline.breadcrumb = ""
		inline.shortcutLbl = ""
		return
	}
	entry := form.list[form.selected]
	if !force && inline.loadedValid && inline.loadedIndex == form.selected && snippetsSameContent(inline.loadedSnap, entry) {
		state.refreshInlineGroupOptions(entry.Group)
		state.refreshInlineMeta(entry)
		return
	}
	inline.loadedValid = true
	inline.loadedIndex = form.selected
	inline.loadedSnap = entry
	inline.name = text.Editor{}
	inline.command = text.Editor{}
	inline.commandArea = dialogArea{}
	inline.capturing = false
	if draft := entry.inlineDraft; draft != nil {
		inline.name.Insert(draft.name)
		inline.command.Insert(draft.command)
		inline.error = draft.error
	} else {
		inline.name.Insert(entry.Name)
		inline.command.Insert(entry.Command)
		inline.error = ""
	}
	state.initInlineGroupPicker(entry.Group)
	state.refreshInlineMeta(entry)
}

func (state *State) refreshInlineMeta(entry snippet) {
	inline := &state.snippets.inline
	if entry.Group != "" {
		inline.breadcrumb = entry.Group
	} else {
		inline.breadcrumb = state.messages.Tools.SnippetFilterNone
	}
	state.refreshInlineShortcutLabel()
}

func (state *State) refreshInlineShortcutLabel() {
	form := &state.snippets
	inline := &form.inline
	m := state.messages.Tools
	if inline.capturing {
		inline.shortcutLbl = m.SnippetShortcutRecording
		return
	}
	if inline.draft {
		if inline.loadedSnap.Shortcut.Valid() {
			inline.shortcutLbl = chordLabel(inline.loadedSnap.Shortcut)
			return
		}
		inline.shortcutLbl = m.SnippetShortcutNone
		return
	}
	if form.selected >= 0 && form.selected < len(form.list) && form.list[form.selected].Shortcut.Valid() {
		inline.shortcutLbl = chordLabel(form.list[form.selected].Shortcut)
		return
	}
	inline.shortcutLbl = m.SnippetShortcutNone
}

func (state *State) initInlineGroupPicker(value string) {
	control := &state.snippets.inline.groupPicker
	control.AllowCreate = true
	control.MatchCaseInsensitive = true
	control.Separator = "/"
	control.SetValidateCreate(validSnippetGroup)
	state.refreshInlineGroupOptions(value)
	control.SetValue(value)
	control.Open = false
}

func (state *State) refreshInlineGroupOptions(value string) {
	control := &state.snippets.inline.groupPicker
	options := make([]components.SingleSelectOption, len(state.snippets.groups))
	for i, group := range state.snippets.groups {
		options[i] = components.SingleSelectOption{Value: group}
	}
	control.SetOptions(options)
	if !control.Open && strings.TrimSpace(control.Query.String()) == "" && control.Value() != value {
		control.SetValue(value)
	}
}

func (inline *snippetInlineEditor) composing() bool {
	fields := [...]*text.Editor{&inline.name, &inline.command, &inline.groupPicker.Query}
	for _, editor := range fields {
		if marked, composing := editor.Composition(); composing && marked.Start != marked.End {
			return true
		}
	}
	return false
}

func (inline *snippetInlineEditor) abandonOther(target gfx.ID) {
	switch target {
	case hitSnippetInlineName, hitSnippetInlineCommand, hitSnippetInlineGroup:
	default:
		return
	}
	if target != hitSnippetInlineName {
		inline.name.Unmark()
	}
	if target != hitSnippetInlineCommand {
		inline.command.Unmark()
	}
	if target != hitSnippetInlineGroup {
		inline.groupPicker.Query.Unmark()
	}
}

// applyInlineSnippetEdits validates and applies text edits made in the Snippets
// page's right-pane editor in memory, coalescing the store write until typing
// settles, the field blurs, or the selection changes.
func (state *State) applyInlineSnippetEdits() {
	form, m := &state.snippets, state.messages.Tools
	inline := &form.inline
	if inline.composing() {
		return
	}
	if !inline.draft && (form.selected < 0 || form.selected >= len(form.list)) {
		return
	}
	var current snippet
	order := form.selected
	if inline.draft {
		current = inline.loadedSnap
		order = len(form.list)
	} else {
		current = form.list[form.selected]
	}
	candidate := current
	candidate.Name = cleanSnippetText(inline.name.String(), maxSnippetName)
	candidate.Command = sanitizeSnippetCommand(inline.command.String())
	var validationErr string
	switch {
	case candidate.Name == "":
		validationErr = m.SnippetErrorName
	case strings.TrimSpace(candidate.Command) == "":
		validationErr = m.SnippetErrorCommand
	case len(candidate.Command) > maxSnippetCommand || !snippetRecordFits(candidate, order):
		validationErr = m.SnippetErrorTooLong
	}
	if validationErr != "" {
		inline.error = validationErr
		if !inline.draft {
			form.list[form.selected].inlineDraft = &snippetInlineDraft{
				name:    inline.name.String(),
				command: inline.command.String(),
				error:   validationErr,
			}
		}
		return
	}
	inline.error = ""
	if inline.draft {
		if len(form.list) >= maxSnippets {
			inline.error = m.SnippetErrorTooMany
			return
		}
		candidate.inlineDraft = nil
		form.list = append(form.list, candidate)
		form.selected = len(form.list) - 1
		form.selectedFolder = candidate.Group
		inline.draft = false
		inline.savedDraft = nil
		inline.loadedIndex = form.selected
		inline.loadedSnap = candidate
		form.showSnippet(form.selected)
		state.snippetTreeExpandAncestors(candidate.Group)
		state.refreshSnippets()
		inline.dirty = true
		state.scheduleInlineSnippetSave()
		return
	}
	form.list[form.selected].inlineDraft = nil
	if snippetsSameContent(current, candidate) {
		return
	}
	candidate.inlineDraft = nil
	form.list[form.selected] = candidate
	inline.loadedSnap = candidate
	state.refreshSnippets()
	inline.dirty = true
	state.scheduleInlineSnippetSave()
}

func (state *State) inlineSnippetSaveDelay() time.Duration {
	if delay := state.snippets.inline.saveDelay; delay > 0 {
		return delay
	}
	return snippetInlineSaveDelay
}

// scheduleInlineSnippetSave writes inline editor text edits once rapid typing
// settles. Each edit re-arms the timer so a typing burst produces one store
// write. Edits are already in memory, so flush/Close saves carry them at once.
func (state *State) scheduleInlineSnippetSave() {
	inline := &state.snippets.inline
	if inline.saveTimer != nil {
		inline.saveTimer.Stop()
	}
	inline.saveSeq++
	seq := inline.saveSeq
	inline.saveTimer = time.AfterFunc(state.inlineSnippetSaveDelay(), func() {
		state.aiPost(func(state *State) {
			if state.snippets.inline.saveSeq != seq {
				return
			}
			state.snippets.inline.saveTimer = nil
			if !state.closed && state.snippets.inline.dirty {
				state.flushInlineSnippetSave()
			}
		})
	})
}

// stopInlineSnippetSave cancels any pending coalesced inline save and clears
// the dirty flag.
func (state *State) stopInlineSnippetSave() {
	inline := &state.snippets.inline
	if inline.saveTimer != nil {
		inline.saveTimer.Stop()
		inline.saveTimer = nil
	}
	inline.saveSeq++
	inline.dirty = false
}

// flushInlineSnippetSave persists pending inline editor edits immediately when
// the user leaves the field, switches selection, or triggers an action.
func (state *State) flushInlineSnippetSave() {
	inline := &state.snippets.inline
	if !inline.dirty {
		state.stopInlineSnippetSave()
		return
	}
	state.stopInlineSnippetSave()
	state.save()
}

func (state *State) commitInlineSnippetPendingGroup() {
	inline := &state.snippets.inline
	if _, composing := inline.groupPicker.Query.Composition(); composing {
		return
	}
	if strings.TrimSpace(inline.groupPicker.Query.String()) == "" {
		return
	}
	if inline.groupPicker.CommitQuery() {
		state.applyInlineSnippetGroup()
	}
}

func (state *State) applyInlineSnippetGroup() {
	form := &state.snippets
	inline := &form.inline
	nextGroup := cleanSnippetGroup(inline.groupPicker.Value())
	if inline.draft {
		inline.loadedSnap.Group = nextGroup
		form.selectedFolder = nextGroup
		state.refreshInlineMeta(inline.loadedSnap)
		return
	}
	if form.selected < 0 || form.selected >= len(form.list) {
		return
	}
	if sameSnippetGroup(form.list[form.selected].Group, nextGroup) && form.list[form.selected].Group == nextGroup {
		return
	}
	form.list[form.selected].Group = nextGroup
	inline.loadedSnap.Group = nextGroup
	form.selectedFolder = nextGroup
	state.snippetTreeExpandAncestors(nextGroup)
	state.refreshSnippets()
	state.save()
}

func (state *State) captureInlineSnippetShortcut(event gfx.KeyEvent, chord keymap.Chord, ok bool) gfx.EventResult {
	form, m := &state.snippets, state.messages
	inline := &form.inline
	result := gfx.EventResult{Handled: true, Repaint: true}
	if event.Repeat {
		return gfx.EventResult{Handled: true}
	}
	if !inline.draft && (form.selected < 0 || form.selected >= len(form.list)) {
		inline.capturing = false
		return result
	}
	if !ok || !validForSnippet(chord) {
		inline.error = m.Tools.SnippetShortcutInvalid
		return result
	}
	skip := form.selected
	if inline.draft {
		skip = -1
	}
	if owner := state.chordOwner(chord, skip); owner != "" {
		inline.error = fmt.Sprintf(m.ShortcutsConflictFmt, owner)
		return result
	}
	if inline.draft {
		inline.loadedSnap.Shortcut = chord
		inline.capturing = false
		inline.error = ""
		state.refreshInlineShortcutLabel()
		return result
	}
	form.list[form.selected].Shortcut = chord
	inline.loadedSnap.Shortcut = chord
	inline.capturing = false
	inline.error = ""
	state.refreshSnippets()
	state.save()
	return result
}
