package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/ui/buttongroup"
	"motty/internal/workbench/icons"
)

// noteSurfaceKind says which page a notes view declares itself in: the notes
// library section, a note's own window, or the panel beside the terminals.
type noteSurfaceKind uint8

const (
	noteSurfaceLibrary noteSurfaceKind = iota
	noteSurfaceWindow
	noteSurfaceSidebar
)

// notePageUI is what the page declares for one notes view: its tree, its
// splitters and the fields bound to the editors the application owns. It is
// kept in the view, like the tree's rows, so a repaint declares the same
// controls with the same identities.
type notePageUI struct {
	kind noteSurfaceKind
	// c is the declaration context of this paint. ui.Tree calls the row
	// builders during the declaration, so the builders reach it here.
	c    ui.C
	tree ui.TreeState
	// library is the width of the library beside the note, and side and panel
	// the two splits of the panel beside the terminals. Their positions are in
	// pixels, as the application's own saved fields are.
	library, side, panel                                        ui.SplitterState
	title, query, inline, picker, find                          ui.Field
	titleBound, queryBound, inlineBound, pickerBound, findBound bool
	// modes shares the three mode actions' outline and outer corners, keeping
	// the application-owned identities and selection.
	modes buttongroup.Group
	// node, label and content build the tree's rows; drawSource and drawPreview
	// paint what the page only lays out. They are made once.
	node                    func(int) ui.TreeNode
	label                   func(int) string
	content                 func(int)
	drawSource, drawPreview func(*ui.DrawContext)
	// previewArea is the scroll area of the preview, as this paint declared it,
	// and previewContent its content height, as the last paint laid it out.
	previewArea    ui.Box
	previewContent float32
	// tagItems are the tag pills the header measured and drawTags paints them;
	// they belong to the paint that declared them.
	tagItems   []noteTagLayout
	tagCompact bool
	drawTags   func(*ui.DrawContext)
	// anchor is the control the open menu or popover hangs under, with the
	// identity that opened it; it is declared again every paint.
	anchor             ui.Box
	anchorID           gfx.ID
	inlineFocused      bool
	libraryWasDragging bool
	sideWasDragging    bool
	panelWasDragging   bool
	// afterLayout reads the splitters back after the page laid them out.
	afterLayout func(*ui.DrawContext)
}

// ensureNotePage prepares the page state of n for the given surface. The
// callbacks it makes capture n and the state, so they are made once.
func (state *State) ensureNotePage(n *notesUI, kind noteSurfaceKind) *notePageUI {
	p := &n.page
	if p.node != nil && p.kind == kind {
		return p
	}
	p.kind = kind
	p.tree.RowHeight = noteTreeRowHeight
	p.tree.Node = func(i int) ui.TreeNode { return state.noteTreeNode(n, i) }
	p.tree.Key = func(i int) int { return n.noteTreeKey(i) }
	p.tree.Label = func(i int) string { return n.view.treeScratch.rows[i].label }
	p.node = p.tree.Node
	p.label = p.tree.Label
	p.content = func(i int) { state.declareNoteTreeRow(n, i) }
	p.drawSource = func(d *ui.DrawContext) { state.drawNoteSource(d, n) }
	p.drawPreview = func(d *ui.DrawContext) { state.drawNotePreview(d, n) }
	if p.library.Pos == 0 {
		p.library.Pos = max(160, n.view.width)
	}
	switch kind {
	case noteSurfaceLibrary:
		p.afterLayout = func(d *ui.DrawContext) { state.noteLibraryAfterLayout(n) }
	case noteSurfaceSidebar:
		p.afterLayout = func(d *ui.DrawContext) { state.noteSidebarAfterLayout(n) }
	}
	return p
}

// noteLibraryAfterLayout keeps the saved library width in step with the
// splitter once the page has laid it out, and saves when the user lets go.
func (s *State) noteLibraryAfterLayout(n *notesUI) {
	p := &n.page
	if p.library.Pos == 0 {
		return
	}
	usable := max(1, s.catalogSectionBounds().Width-1)
	width := min(max(p.library.Pos, 160), min(520, max(160, usable-480)))
	if width != p.library.Pos && !p.library.Dragging() {
		p.library.Pos = width
	}
	if width != n.view.width {
		n.view.width = width
	}
	if p.library.Dragging() {
		p.libraryWasDragging = true
	} else if p.libraryWasDragging {
		p.libraryWasDragging = false
		s.save()
	}
}

// setPreviewScroll moves the preview to y on the next declaration; the page
// owns the wheel and writes its own position back into previewScroll.
func (n *notesUI) setPreviewScroll(y float32) {
	n.previewScroll = y
	n.previewSeek = true
}

// captureAnchor records a control as the anchor of the overlay it opened.
func (p *notePageUI) captureAnchor(c ui.C, n *notesUI, id gfx.ID, box ui.Box) {
	if id != (gfx.ID{}) && n.view.overlayFrom == id {
		p.anchor, p.anchorID = box, id
	}
}

// anchorBox is the control the open overlay hangs under.
func (p *notePageUI) anchorBox(n *notesUI) (ui.Box, bool) {
	if p.anchorID != (gfx.ID{}) && p.anchorID == n.view.overlayFrom && p.anchor.Valid() {
		return p.anchor, true
	}
	return ui.Box{}, false
}

// noteTokensFor fills a view's stable row tokens from the library.
func (s *State) noteTokensFor(v *noteViewState) {
	if v.noteTokens == nil {
		v.noteTokens = map[string]int{}
		v.tokenNotes = map[int]string{}
		v.folderTokens = map[string]int{}
		v.tokenFolders = map[int]string{}
	}
	for _, n := range s.notes.list {
		if _, ok := v.noteTokens[n.ID]; !ok {
			i := len(v.noteTokens)
			v.noteTokens[n.ID] = i
			v.tokenNotes[i] = n.ID
		}
	}
	for _, n := range s.notes.folders {
		if _, ok := v.folderTokens[n.ID]; !ok {
			i := len(v.folderTokens)
			v.folderTokens[n.ID] = i
			v.tokenFolders[i] = n.ID
		}
	}
}

func (n *notesUI) noteTreeKey(i int) int {
	rows := n.view.treeScratch.rows
	if i < 0 || i >= len(rows) {
		return 0
	}
	e := rows[i]
	if e.folder {
		return 1<<20 + n.view.folderTokens[e.id]
	}
	if e.id == "" {
		return -2
	}
	return n.view.noteTokens[e.id]
}

func (s *State) noteTreeNode(n *notesUI, i int) ui.TreeNode {
	rows := n.view.treeScratch.rows
	if i < 0 || i >= len(rows) {
		return ui.TreeNode{}
	}
	e := rows[i]
	query := strings.ToLower(n.query.String())
	expanded := e.folder && (n.view.expanded[e.id] || query != "")
	return ui.TreeNode{Depth: e.depth, Branch: e.folder, Expanded: expanded, Selected: selectedNoteNode(n, e)}
}

// selectedNoteNode says whether a row is the tree's selected one.
func selectedNoteNode(n *notesUI, e noteTreeEntry) bool {
	if e.id == "" {
		return false
	}
	if n.page.kind == noteSurfaceSidebar {
		return !e.folder && e.id == n.selected
	}
	if e.folder {
		return n.view.treeFolderSelection && n.folder == e.id
	}
	return n.selected == e.id && !n.view.treeFolderSelection
}

// declareNoteTreeRow declares what one row of a notes tree holds: its icon, its
// name and the action its hover shows (open in a window beside the terminals, or
// the actions menu in the library).
func (s *State) declareNoteTreeRow(n *notesUI, i int) {
	rows := n.view.treeScratch.rows
	if i < 0 || i >= len(rows) {
		return
	}
	p, e, c := &n.page, rows[i], n.page.c
	if c == nil {
		return
	}
	if e.id == "" {
		// The row a new folder or note is being named in.
		p.inline.Bind(&n.view.inline)
		field := ui.TextFieldID(c, hitNoteInline, &p.inline, s.noteText("Name", "名称"), s.noteText("Name", "名称")).
			FillWidth().Height(noteTreeRowHeight-4).MarginSides(0, 4, 2, 0)
		if field.Submitted() {
			c.After(func() { s.commitNoteInline() })
		}
		return
	}
	icon := icons.MitMarkdown
	color := s.pal.text
	if e.pinned {
		icon = icons.StarSolid
	}
	if e.folder {
		icon = icons.FolderSolid
		if n.view.expanded[e.id] || n.query.String() != "" {
			icon = icons.FolderSolidOpen
		}
	}
	rowID := p.tree.RowID(i)
	hovered := c.Canvas().Hovered() == rowID
	focused := c.Canvas().FocusVisible() == rowID
	ui.Row(c).Gap(0).FillWidth().Items(ui.Center).Children(func() {
		ui.Icon(c, icon).Size(16, 16).Color(color).Shrink(0).MarginSides(0, 0, 0, 4)
		label := ui.Text(c, e.label).FontSize(13).Color(color).Grow(1).Ellipsis()
		if !e.folder {
			label.MarginSides(0, 0, 0, 2)
		}
		switch p.kind {
		case noteSurfaceSidebar:
			if e.folder {
				return
			}
			openID := hitNoteSideRowOpen.At(n.view.noteTokens[e.id])
			if !hovered && !focused && c.Canvas().Hovered() != openID && c.Canvas().FocusVisible() != openID {
				return
			}
			s.noteIconButton(c, openID, icons.ExternalLink, s.noteText("Open in window", "在窗口中打开"), 28, false).
				MarginSides(1, -6, 1, 0)
		default:
			var moreID gfx.ID
			if e.folder {
				moreID = hitNoteFolderMore.At(n.view.folderTokens[e.id])
			} else {
				moreID = hitNoteRowMore.At(n.view.noteTokens[e.id])
			}
			menuOpen := (n.view.menu == "note" && !e.folder && n.view.menuID == e.id) || (n.view.menu == "folder" && e.folder && n.view.menuID == e.id)
			if !hovered && !focused && !selectedNoteNode(n, e) && !menuOpen && c.Canvas().Hovered() != moreID {
				return
			}
			p.captureAnchor(c, n, moreID, s.noteIconButton(c, moreID, icons.Ellipsis, s.noteText("More actions", "更多操作"), 28, false).
				MarginSides(1, -6, 1, 0))
		}
	})
}

// noteTreeRows rebuilds the flattened rows of the library or of the panel and
// keeps them in the view's scratch.
func (s *State) noteTreeRows(n *notesUI, side bool) {
	v := &n.view
	sc := &v.treeScratch
	sc.reset()
	matches, parents := sc.matches, sc.parents
	host := ""
	if side {
		if session, ok := s.sessionByIDPointer(s.activeSessionID); ok {
			host = session.host.CatalogID
		}
	}
	query := strings.ToLower(n.query.String())
	notes := sc.notes
	for _, stored := range s.notes.list {
		note := s.noteBrowserNote(stored)
		notes = append(notes, note)
		if side && !n.sideAll && !note.Pinned && (host == "" || !s.noteMatchesHost(note.Tags, host)) {
			continue
		}
		if !s.noteMatchesSearch(note, query) {
			continue
		}
		matches[note.ID] = true
		seen := sc.seen
		for parent := note.Folder; parent != "" && !seen[parent]; {
			seen[parent] = true
			parents[parent] = true
			f, ok := s.noteFolder(parent)
			if !ok {
				break
			}
			parent = f.Parent
		}
		clear(seen)
	}
	folders := append(sc.folders, s.notes.folders...)
	stableSortNoteFolders(folders)
	sc.folders = folders
	stableSortNotes(notes)
	sc.notes = notes
	seen := sc.walked
	rows := sc.rows
	var walk func(string, int)
	walk = func(parent string, depth int) {
		for _, f := range folders {
			if f.Parent != parent || seen[f.ID] {
				continue
			}
			if query != "" && !parents[f.ID] {
				continue
			}
			seen[f.ID] = true
			rows = append(rows, noteTreeEntry{id: f.ID, label: f.Name, folder: true, depth: depth})
			if v.expanded[f.ID] || query != "" {
				walk(f.ID, depth+1)
			}
		}
		if !side && v.inlineNew && v.inlineParent == parent {
			rows = append(rows, noteTreeEntry{folder: true, depth: depth})
		}
		for _, note := range notes {
			if !matches[note.ID] {
				continue
			}
			actual := note.Folder
			if _, ok := s.noteFolder(actual); !ok {
				actual = ""
			}
			if actual != parent {
				continue
			}
			label := note.Title
			if strings.TrimSpace(label) == "" {
				label = s.noteText("Untitled", "无标题")
			}
			rows = append(rows, noteTreeEntry{id: note.ID, label: label, pinned: note.Pinned, depth: depth})
		}
	}
	walk("", 0)
	sc.rows = rows
}

func stableSortNoteFolders(folders []savedNoteFolder) {
	for i := 1; i < len(folders); i++ {
		for j := i; j > 0 && folders[j].Order < folders[j-1].Order; j-- {
			folders[j], folders[j-1] = folders[j-1], folders[j]
		}
	}
}

func stableSortNotes(notes []savedNote) {
	for i := 1; i < len(notes); i++ {
		for j := i; j > 0; j-- {
			a, b := notes[j], notes[j-1]
			less := false
			if a.Pinned != b.Pinned {
				less = a.Pinned
			} else {
				less = a.Order < b.Order
			}
			if !less {
				break
			}
			notes[j], notes[j-1] = notes[j-1], notes[j]
		}
	}
}

// noteTreeChanges applies what the user did to the declared tree: a chosen row,
// a disclosure and a drop.
func (s *State) noteTreeChanges(n *notesUI, side bool) {
	p := &n.page
	rows := n.view.treeScratch.rows
	if i, ok := p.tree.Toggled(); ok && i >= 0 && i < len(rows) {
		e := rows[i]
		if e.folder {
			n.view.expanded[e.id] = !n.view.expanded[e.id]
			s.save()
		}
	}
	if i, ok := p.tree.Chosen(); ok && i >= 0 && i < len(rows) {
		s.chooseNoteTreeRow(n, rows[i], side)
	}
	if move, ok := p.tree.Dropped(); ok {
		s.dropNoteTreeRow(n, move)
	}
}

func (s *State) chooseNoteTreeRow(n *notesUI, e noteTreeEntry, side bool) {
	if e.id == "" {
		return
	}
	if e.folder {
		if side {
			n.view.expanded[e.id] = !n.view.expanded[e.id]
			s.save()
			return
		}
		n.folder = e.id
		n.view.treeFolderSelection = true
		s.save()
		return
	}
	if s.noteIndex(e.id) < 0 || e.id == n.selected {
		return
	}
	if side {
		if n.imageCancel != nil {
			n.imageCancel()
		}
		n.imageToken++
		n.images = nil
		n.imageSource = ""
		n.error = ""
		n.view.codeActions.source = ""
		n.view.codeActions.targets = nil
		n.selected = e.id
		n.setPreviewScroll(0)
		n.view.clearPreviewSelection()
		s.syncNoteSidebar()
		return
	}
	if !s.selectNote(e.id) {
		return
	}
	n.view.treeFolderSelection = false
	if at := s.noteIndex(n.selected); at >= 0 {
		n.folder = n.list[at].Folder
	}
	n.view.drawer = false
	n.view.focus = hitNoteBody
}

// noteTreeFolderParent is the folder a row drops beside: its own for a folder,
// the note's folder for a note.
func (s *State) noteTreeFolderParent(e noteTreeEntry) string {
	if e.folder {
		if f, ok := s.noteFolder(e.id); ok {
			return f.Parent
		}
		return ""
	}
	if i := s.noteIndex(e.id); i >= 0 {
		if _, ok := s.noteFolder(s.notes.list[i].Folder); ok {
			return s.notes.list[i].Folder
		}
	}
	return ""
}

func (s *State) dropNoteTreeRow(n *notesUI, move ui.TreeMove) {
	rows := n.view.treeScratch.rows
	if move.From < 0 || move.From >= len(rows) || move.To < 0 || move.To >= len(rows) {
		return
	}
	from, to := rows[move.From], rows[move.To]
	if from.id == "" || to.id == "" || from.id == to.id {
		return
	}
	parent, before := "", ""
	switch move.Where {
	case ui.TreeDropInside:
		if !to.folder {
			return
		}
		parent = to.id
	case ui.TreeDropBefore:
		parent, before = s.noteTreeFolderParent(to), to.id
	case ui.TreeDropAfter:
		parent = s.noteTreeFolderParent(to)
		before = s.noteNextSibling(to.id, to.folder)
	}
	if from.folder && (parent == from.id || s.noteFolderWithin(parent, from.id)) {
		return
	}
	if s.moveNote(from.id, parent, before, from.folder) {
		s.noteOpenAncestors(parent)
	}
}

// noteTreeCanDrop says whether the tree may drop the row from at where of row
// to, keeping folders out of themselves and their own contents.
func (s *State) noteTreeCanDrop(n *notesUI, from, to int, where ui.TreeDrop) bool {
	rows := n.view.treeScratch.rows
	if from < 0 || from >= len(rows) || to < 0 || to >= len(rows) {
		return false
	}
	f, t := rows[from], rows[to]
	if f.id == "" || t.id == "" || f.id == t.id {
		return false
	}
	parent := ""
	switch where {
	case ui.TreeDropInside:
		if !t.folder {
			return false
		}
		parent = t.id
	case ui.TreeDropBefore, ui.TreeDropAfter:
		parent = s.noteTreeFolderParent(t)
	}
	if f.folder && (parent == f.id || s.noteFolderWithin(parent, f.id)) {
		return false
	}
	return true
}

// declareNotesWorkspace declares the notes section in the workbench page.
func (s *State) declareNotesWorkspace(c ui.C) {
	n, v := &s.notes, &s.notes.view
	s.noteTokens()
	s.noteTokensFor(v)
	state := s.ensureNotePage(n, noteSurfaceLibrary)
	state.c = c
	state.tree.CanDrop = func(from, to int, where ui.TreeDrop) bool { return s.noteTreeCanDrop(n, from, to, where) }
	n.listBounds, n.sourceBounds, n.previewBounds = gfx.Rect{}, gfx.Rect{}, gfx.Rect{}
	bounds := s.catalogSectionBounds()
	v.workspace = gfx.Rect{Width: bounds.Width, Height: bounds.Height}
	s.noteTreeRows(n, false)
	collapsed := v.collapsed || bounds.Width < v.width+480
	if collapsed {
		ui.Column(c).Fill().Children(func() {
			s.declareNoteEditor(c, n, bounds.Width, false)
		})
		if v.drawer {
			s.declareNoteDrawer(c, n)
		}
		s.declareNoteOverlays(c, n)
		return
	}
	ui.Row(c).Fill().DrawOver(state.afterLayout).Children(func() {
		ui.Column(c).Width(v.width).FillHeight().Children(func() {
			s.declareNoteLibrary(c, n, v.width, false)
		})
		// The old handle's FractionForKey moved it 5% of the room per arrow.
		state.library.StepFraction = .05
		ui.Splitter(c, &state.library).SplitterThickness(1).Mins(160, 480).Label(s.noteText("Resize note library", "调整目录宽度"))
		ui.Column(c).Grow(1).FillHeight().Children(func() {
			s.declareNoteEditor(c, n, bounds.Width-v.width-1, false)
		})
	})
	s.declareNoteOverlays(c, n)
}

// declareNoteDrawer declares the library as a drawer over the editor on a
// narrow card.
func (s *State) declareNoteDrawer(c ui.C, n *notesUI) {
	v := &n.view
	extent := min(v.width, s.catalogSectionBounds().Width-32)
	open := v.drawer
	ui.Drawer(c, &open, s.noteText("Note library", "笔记目录"), ui.DrawerSpec{
		Side: ui.DrawerLeft, Size: extent,
	}, func() {
		s.declareNoteLibrary(c, n, extent, true)
	}, nil)
	if !open {
		v.drawer = false
	}
}

// declareNoteLibrary declares the library pane: its tool row, its search, and
// its tree.
func (s *State) declareNoteLibrary(c ui.C, n *notesUI, width float32, drawer bool) {
	p, v := &n.page, &n.view
	p.c = c
	rows := v.treeScratch.rows
	actions := [...]struct {
		id    gfx.ID
		label string
		icon  gfx.Icon
	}{
		{hitNoteSearchToggle, s.noteText("Search notes", "搜索笔记"), icons.Search},
		{hitNoteAdd, s.noteText("New note", "新建笔记"), icons.SquarePen},
		{hitNoteFolderAdd, s.noteText("New folder", "新建文件夹"), icons.FolderPlus},
		{hitNoteImport, s.noteText("Import Markdown", "导入 Markdown"), icons.Upload},
		{hitNoteLinkFile, s.noteText("Link Markdown file", "链接 Markdown 文件"), icons.Link},
		{hitNoteLinkFolder, s.noteText("Link Markdown folder", "链接 Markdown 文件夹"), icons.FolderOpen},
		{hitNoteExportAll, s.noteText("Export ZIP", "导出 ZIP"), icons.Download},
		{hitNoteExpandAll, s.noteExpandLabel(), noteExpandIcon(s.noteCanExpandAll())},
	}
	ui.Row(c).Height(28).MarginSides(8, 8, 0, 8).Gap(2).Items(ui.Center).Children(func() {
		more := max(0, int((width-16)/30)-1)
		for i, a := range actions {
			if i == more && i < len(actions)-1 {
				p.captureAnchor(c, n, hitNoteTreeMore, s.declareNoteToolbarButton(c, hitNoteTreeMore, s.noteText("Library actions", "目录操作"), icons.Ellipsis, false))
				break
			}
			disabled := a.id == hitNoteExpandAll && (len(s.notes.folders) == 0 || n.query.String() != "")
			s.declareNoteToolbarButton(c, a.id, a.label, a.icon, disabled)
		}
	})
	if v.searchOpen {
		p.query.Bind(&n.query)
		field := ui.TextFieldID(c, hitNoteSearch, &p.query, s.noteText("Search title, body and tags", "搜索标题、正文和标签"), s.noteText("Search title, body and tags", "搜索标题、正文和标签")).
			Height(30).FillWidth().MarginSides(8, 8, 0, 8)
		if field.Changed() {
			s.noteSearchChanged(n)
		}
	}
	ui.Divider(c).Height(1).MarginSides(7, 8, 0, 8).Background(s.pal.line)
	area := ui.Column(c).Grow(1).FillWidth().MarginSides(6, 6, 6, 6).
		HitWith(hitNoteListScroll, s.noteText("Scroll notes", "滚动笔记"), gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true})
	area.Children(func() {
		ui.Tree(c, &p.tree, len(rows), p.label, p.content).Grow(1).FillWidth().Scrollbars().
			Label(s.noteText("Scroll notes", "滚动笔记"))
		s.noteTreeChanges(n, drawer)
		if len(rows) == 0 {
			message := s.noteText("No notes yet. Create one above.", "暂无笔记，可点击上方新建。")
			if n.query.String() != "" {
				message = s.noteText("No matching notes.", "没有匹配的笔记。")
			}
			ui.Text(c, message).FontSize(12).Color(s.pal.muted).MarginSides(8, 0, 0, 0)
		}
	})
}

func noteExpandIcon(canExpand bool) gfx.Icon {
	if canExpand {
		return icons.ListTree
	}
	return icons.ListX
}

// noteSearchChanged restarts the tree at the top when the search text changes,
// and opens the field when the query is typed.
func (s *State) noteSearchChanged(n *notesUI) {
	if n.page.tree.HasFocus() || s.notes.query.String() != "" {
		n.page.tree.ScrollTo(0, ui.ListStart)
	}
}

func (s *State) declareNoteToolbarButton(c ui.C, id gfx.ID, label string, icon gfx.Icon, disabled bool) ui.Box {
	button := ui.Button(c, label).Icon(icon).IconOnly().Ghost().Size(28, 28).Shrink(0).Hit(id, label)
	if disabled {
		button = button.Disabled(true)
	}
	return button
}

// noteIconButton declares a square icon button in a notes surface: a ghost one,
// or quiet (a muted glyph the pointer brightens) beside the terminals.
func (s *State) noteIconButton(c ui.C, id gfx.ID, icon gfx.Icon, hint string, size float32, quiet bool) ui.Box {
	if !quiet {
		return ui.Button(c, hint).Icon(icon).IconOnly().Ghost().Size(size, size).Shrink(0).Hit(id, hint)
	}
	pal := s.pal
	button := ui.ButtonBase(c, hint).Radius(radius.control).Size(size, size).Shrink(0).
		Hover(ui.Look{}.Background(withAlpha(pal.surfaceHot, 0.7)).Color(pal.text)).
		Pressed(ui.Look{}.Background(pal.surfaceHot).Color(pal.text)).
		Focus(ui.Look{}.Ring(1.5, withAlpha(pal.accent, 0.5))).Hit(id, hint)
	button.Children(func() {
		ui.Icon(c, icon).Color(pal.muted).StateFrom(button).
			Hover(ui.Look{}.Color(pal.text)).Pressed(ui.Look{}.Color(pal.text))
	})
	return button
}

// declareNoteEditor declares the note's own side of the card.
func (s *State) declareNoteEditor(c ui.C, n *notesUI, width float32, window bool) {
	v := &n.view
	n.sourceBounds, n.previewBounds = gfx.Rect{}, gfx.Rect{}
	n.view.documentBounds = gfx.Rect{Width: width}
	if window {
		s.declareNoteWindowBody(c, n, width)
		return
	}
	if s.noteIndex(n.selected) < 0 {
		ui.Row(c).Height(28).MarginSides(8, 16, 0, 18).Children(func() {
			s.declareNoteToolbarButton(c, hitNoteTreeToggle, s.noteText("Toggle library", "显示 / 隐藏目录"), icons.PanelLeft, false)
		})
		ui.Text(c, s.noteText("Select a note or create one to begin.", "选择笔记，或新建一篇笔记。")).
			FontSize(16).Color(s.pal.muted).MarginSides(88, 16, 0, 18)
		return
	}
	s.declareNoteEditorHeader(c, n, width, false)
	s.declareNoteEditorBody(c, n, width, false)
	_ = v
}

// declareNoteEditorHeader declares the title, the actions and the mode switch
// over the note.
func (s *State) declareNoteEditorHeader(c ui.C, n *notesUI, width float32, window bool) {
	p, v := &n.page, &n.view
	hasImages := len(noteImageURLs(n.body.String())) > 0
	actionsWidth := float32(198)
	if hasImages {
		actionsWidth += 34
	}
	available := width - actionsWidth - 80
	if window {
		available = width - actionsWidth - 12
	}
	stacked := available < 120
	left, top, right := float32(18), float32(8), float32(16)
	if window {
		left, top, right = 0, 0, 0
	}
	title := func() {
		p.title.Bind(&n.title)
		field := ui.TextFieldID(c, hitNoteTitle, &p.title, s.noteText("Title", "标题"), s.noteText("Untitled", "无标题")).
			Height(28).FontSize(16).Grow(1)
		if stacked {
			field.MarginSides(0, 0, 0, 6)
		} else {
			field.MarginSides(0, 12, 0, 6)
		}
	}
	actions := func() {
		ui.Row(c).Shrink(0).Gap(6).Items(ui.Center).Children(func() {
			p.captureAnchor(c, n, hitNoteTagsOpen, s.declareNoteToolbarButton(c, hitNoteTagsOpen, s.noteText("Tags", "标签"), icons.Tags, false))
			p.captureAnchor(c, n, hitNoteHostsOpen, s.declareNoteToolbarButton(c, hitNoteHostsOpen, s.noteText("Linked hosts", "关联主机"), icons.Server, false))
			pinIcon, pinLabel := icons.Star, s.noteText("Pin note", "置顶笔记")
			if n.draft.Pinned {
				pinIcon, pinLabel = icons.StarSolid, s.noteText("Unpin note", "取消置顶")
			}
			s.declareNoteToolbarButton(c, hitNotePin, pinLabel, pinIcon, false)
			if hasImages {
				s.declareNoteToolbarButton(c, hitNoteLoadImages, s.noteText("Load images", "加载图片"), icons.Image, false)
			}
			modes := &p.modes
			modes.Reset()
			choices := [...]struct {
				mode noteMode
				icon gfx.Icon
			}{{noteSource, icons.CodeXml}, {noteSplit, icons.Columns2}, {notePreview, icons.Eye}}
			tokens := [...]int{0, 2, 1} // source, split, preview
			for i, choice := range choices {
				modes.Item(hitNoteModes.At(tokens[i]), s.noteModeLabel(choice.mode)).Icon(choice.icon).IconOnly().Outline()
				if v.mode == choice.mode {
					modes.Selected(i)
				}
			}
			modes.View(c, s.noteText("View mode", "查看模式")).Size(96, 28).Shrink(0)
		})
	}
	ui.Column(c).FillWidth().Children(func() {
		ui.Row(c).Height(28).MarginSides(top, right, 0, left).Items(ui.Center).Children(func() {
			if !window {
				s.declareNoteToolbarButton(c, hitNoteTreeToggle, s.noteText("Toggle library", "显示 / 隐藏目录"), icons.PanelLeft, false).
					MarginSides(0, 6, 0, 0)
			}
			if stacked {
				title()
				return
			}
			title()
			actions()
		})
		if stacked {
			ui.Row(c).Height(28).MarginSides(8, right, 0, left).Children(func() {
				ui.Spacer(c)
				actions()
			})
		}
	})
}

// declareNoteEditorBody declares the tags line, the formatting bar, the notes
// on the note, and the source or preview under them.
func (s *State) declareNoteEditorBody(c ui.C, n *notesUI, width float32, window bool) {
	v := &n.view
	if !window {
		ui.Divider(c).Height(1).MarginSides(7, 16, 0, 16).Background(s.pal.line)
		if len(noteWords(n.tags.String())) > 0 {
			s.declareNoteTagArea(c, n, width-40, true)
			ui.Divider(c).Height(1).MarginSides(4, 16, 0, 16).Background(s.pal.line)
		}
	}
	mode := v.mode
	if mode == noteSplit && width < 640 {
		mode = noteSource
	}
	if mode != noteSplit {
		v.splitScroll.active = false
	}
	if n.editing.findOpen && mode != notePreview {
		s.declareNoteFindBar(c, n)
	}
	if mode != notePreview {
		s.declareNoteFormatting(c, n, width, window)
	}
	if message := n.error; message != "" || n.problem != "" {
		if n.problem != "" {
			message = n.problem
		}
		ui.Text(c, message).FontSize(12).Color(s.pal.danger).MarginSides(0, 16, 0, 16)
		if detail := n.saveDetail; detail != "" && message == s.noteSaveFailure() {
			ui.Text(c, s.noteText("Cause: ", "原因：")+detail).FontSize(11).Color(s.pal.muted).MarginSides(0, 16, 0, 16)
		}
	}
	left, right := float32(20), float32(20)
	if window {
		left, right = 0, 0
	}
	s.declareNoteLinkNotice(c, n, width-left-right, left, right)
	body := ui.Row(c).Grow(1).FillWidth().MarginSides(8, 16, 16, 16)
	if window {
		body = ui.Row(c).Grow(1).FillWidth().MarginSides(8, 0, 0, 0)
	}
	body.Children(func() {
		switch mode {
		case noteSource:
			ui.Slot(c).Grow(1).FillHeight().Draw(n.page.drawSource)
		case notePreview:
			contentWidth := width - 32
			if window {
				contentWidth = width
			}
			s.declareNotePreview(c, n, contentWidth, true)
		default:
			contentWidth := width - 32
			if window {
				contentWidth = width
			}
			pane := (contentWidth - 16) / 2
			ui.Column(c).Grow(1).Basis(0).FillHeight().Children(func() {
				ui.Slot(c).Grow(1).FillWidth().Draw(n.page.drawSource)
			})
			ui.Slot(c).Width(16)
			ui.Column(c).Grow(1).Basis(0).FillHeight().Children(func() {
				s.declareNotePreview(c, n, pane, true)
			})
		}
	})
}

// declareNoteFormatting declares the Markdown formatting buttons and the
// updated status beside them.
func (s *State) declareNoteFormatting(c ui.C, n *notesUI, width float32, window bool) {
	formatTop, formatRight, formatLeft := float32(5), float32(16), float32(18)
	if window {
		formatTop, formatRight, formatLeft = 0, 0, 0
	}
	status := s.noteUpdatedLabel()
	if linked, ok := s.noteLinkStatus(n.selected, func(text string) float32 { return c.Canvas().TextWidth(text, 11) }, width-40); ok {
		status = linked
	}
	tools := float32(0)
	for i := range noteFormatLabels {
		if len(noteFormatIcon(i).Commands) > 0 {
			tools += 26
		} else {
			tools += 28
		}
		if i > 0 {
			tools += 4
		}
	}
	statusWidth := c.Canvas().TextWidth(status, 11)
	showStatus := width-formatLeft-formatRight-tools >= statusWidth+24
	ui.Row(c).Height(26).MarginSides(formatTop, formatRight, 0, formatLeft).Gap(4).Items(ui.Center).Children(func() {
		for i, label := range noteFormatLabels {
			icon := noteFormatIcon(i)
			name := s.noteFormatName(i)
			var button ui.Box
			if len(icon.Commands) > 0 {
				button = s.noteIconButton(c, hitNoteFormats.At(i), icon, name, 26, false)
			} else {
				button = ui.Button(c, label).Ghost().Size(28, 26).Hit(hitNoteFormats.At(i), label)
			}
			button.Tooltip(name)
		}
		if showStatus {
			ui.Spacer(c)
			ui.Text(c, status).FontSize(11).Color(s.pal.muted).MarginSides(0, 0, 0, 4)
		}
	})
}

// declareNotePreview declares the scrolling preview of the note's Markdown.
func (s *State) declareNotePreview(c ui.C, n *notesUI, width float32, fill bool) {
	s.notePreviewLayoutHeight(c, n, width)
	seek := n.previewSeek
	if !seek && n.previewScroll > 0 && n.page.previewContent == 0 {
		// The area's state is dropped while its panel is closed; reopening
		// starts where the application last left it.
		seek = true
	}
	var area ui.Box
	if seek {
		area = ui.ScrollAt(c, n.previewScroll)
		n.previewSeek = false
	} else {
		area = ui.Scroll(c)
		n.previewSeek = false
	}
	if fill {
		area.Grow(1).FillWidth()
	}
	area.Scrollbars().Label(s.noteText("Scroll preview", "滚动预览"))
	n.page.previewArea = area
	area.Children(func() {
		ui.Slot(c).Height(max(n.previewHeight, 0)).FillWidth().Draw(n.page.drawPreview)
	})
}

// notePreviewLayoutHeight lays the note's Markdown out for width and records
// the preview's content height, as the preview paints it.
func (s *State) notePreviewLayoutHeight(c ui.C, n *notesUI, width float32) {
	s.notePreviewLayout(c.Canvas(), n, width)
}

// notePreviewLayout lays the note out at width and records the preview's
// content height.
func (s *State) notePreviewLayout(canvas *gfx.Canvas, n *notesUI, width float32) {
	source := n.body.String()
	width = max(1, width-12)
	cache := &n.previewLayout
	if !cache.ready || cache.source != source || cache.width != width || cache.face != s.terminalFontFace || cache.main != s.fonts.main || cache.bold != s.fonts.bold {
		n.doc.Parse(source)
		height := n.rendered.Layout(canvas, noteMarkdownStyle(s), &n.doc, width)
		*cache = notePreviewLayout{ready: true, source: source, width: width, face: s.terminalFontFace, main: s.fonts.main, bold: s.fonts.bold, height: height}
	}
	images := float32(0)
	if n.imageSource == source {
		for _, im := range n.images {
			images += min(float32(320), width*float32(im.bitmap.Height)/float32(im.bitmap.Width)) + 12
		}
	}
	n.previewHeight = cache.height + 12 + images
}

func (s *State) drawNoteSource(d *ui.DrawContext, n *notesUI) {
	n.sourceBounds = d.Rect
	s.paintNoteSource(d.Canvas, n, d.Rect)
}

func (s *State) drawNotePreview(d *ui.DrawContext, n *notesUI) {
	viewport := n.page.previewArea.Rect()
	n.previewBounds = viewport
	if st := n.page.previewArea.ScrollState(); st != nil {
		n.previewScroll = st.Offset
		n.page.previewContent = st.Content
	}
	s.paintNotePreviewAt(d.Canvas, d.Rect, viewport, n)
	s.syncNoteScroll(d.Canvas, n)
}

// noteMarkdownStyle is the style the preview lays out with.
func noteMarkdownStyle(s *State) components.MarkdownStyle {
	return components.MarkdownStyle{Size: noteFontSize, MonoFace: s.terminalFontFace, CodeInset: 28, Highlight: highlightCode}
}
