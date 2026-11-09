package workbench

import (
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text/find"
	"motty/internal/workbench/icons"
)

func (s *State) paintNotesPanel(c *gfx.Canvas, size gfx.Size) {
	if !s.notes.panel {
		return
	}
	frame := s.workspacePixelBounds(size)
	b := gfx.Rect{X: frame.X + frame.Width, Y: frame.Y, Width: s.notesPanelWidth(size), Height: frame.Height}
	s.paintNoteSidebar(c, b)
}
func (s *State) noteButton(c *gfx.Canvas, id gfx.ID, b gfx.Rect, label string, icon gfx.Icon) {
	if b.Width <= 0 {
		return
	}
	button := components.BeginButton(c, s.Theme().WithControlHeight(28), id, b).Height(b.Height)
	if len(icon.Commands) > 0 {
		button = button.WithIcon(icon).IconSmall()
	}
	if id == hitNoteExpandAll && len(s.notes.folders) == 0 {
		button = button.Disabled()
	}
	button.Ghost(label)
}
func (s *State) noteInputAt(c *gfx.Canvas, id gfx.ID, b gfx.Rect, label, value string) {
	components.PaintInput(c, s.Theme(), b, components.InputSnapshot{ID: id, Label: label, Value: value, Placeholder: label, Focused: c.Focused() == id, PlatformEditing: c.Focused() == id})
}
func (s *State) paintNotesWorkspace(c *gfx.Canvas, b gfx.Rect) {
	s.noteTokens()
	n, v := &s.notes, &s.notes.view
	// The enclosing card or panel owns the background, including its corners.
	v.workspace = b
	n.rows = n.rows[:0]
	n.listBounds = gfx.Rect{}
	n.sourceBounds = gfx.Rect{}
	n.previewBounds = gfx.Rect{}
	collapsed := v.collapsed || b.Width < v.width+480
	if !collapsed {
		split := s.noteResizable(c).Paint(s.noteText("Resize note library", "调整目录宽度"))
		v.treeBounds = split.First
		s.paintNoteTree(c, split.First)
		s.paintNoteEditor(c, split.Second)
	} else {
		s.paintNoteEditor(c, b)
	}
	if v.drawer {
		sheet := components.BeginSheet(c, s.Theme(), b, components.SheetOptions{Layer: popupLayer, Scope: hitNoteDrawerDismiss, DismissID: hitNoteDrawerDismiss, CloseID: hitNoteDrawerDismiss, Side: components.SheetLeft, ContentExtent: min(v.width, b.Width-32)})
		layout := sheet.Begin(s.noteText("Note library", "笔记目录"), "")
		s.paintNoteTree(c, layout.Body)
		sheet.End()
	}
	s.paintNoteOverlays(c, b)
	if !v.focus.IsZero() {
		c.RequestFocus(v.focus)
		v.focus = gfx.ID{}
	}
}
func (s *State) paintNoteTree(c *gfx.Canvas, b gfx.Rect) {
	n, v := &s.notes, &s.notes.view
	theme := s.Theme()
	n.listBounds = b
	x, y := b.X+8, b.Y+8
	expandIcon := icons.ListX
	if s.noteCanExpandAll() {
		expandIcon = icons.ListTree
	}
	actions := []struct {
		id    gfx.ID
		label string
		icon  gfx.Icon
	}{
		{hitNoteSearchToggle, s.noteText("Search notes", "搜索笔记"), icons.Search}, {hitNoteAdd, s.noteText("New note", "新建笔记"), icons.SquarePen}, {hitNoteFolderAdd, s.noteText("New folder", "新建文件夹"), icons.FolderPlus}, {hitNoteImport, s.noteText("Import Markdown", "导入 Markdown"), icons.Upload}, {hitNoteLinkFile, s.noteText("Link Markdown file", "链接 Markdown 文件"), icons.Link}, {hitNoteLinkFolder, s.noteText("Link Markdown folder", "链接 Markdown 文件夹"), icons.FolderOpen}, {hitNoteExportAll, s.noteText("Export ZIP", "导出 ZIP"), icons.Download}, {hitNoteExpandAll, s.noteExpandLabel(), expandIcon},
	}
	for i, a := range actions {
		if i == max(0, int((b.Width-16)/30)-1) && i < len(actions)-1 {
			s.noteButton(c, hitNoteTreeMore, gfx.Rect{X: x, Y: y, Width: 28, Height: 28}, s.noteText("Library actions", "目录操作"), icons.Ellipsis)
			break
		}
		s.noteButton(c, a.id, gfx.Rect{X: x, Y: y, Width: 28, Height: 28}, a.label, a.icon)
		x += 30
	}
	y += 36
	if v.searchOpen {
		placeholder := s.noteText("Search title, body and tags", "搜索标题、正文和标签")
		s.noteInputAt(c, hitNoteSearch, gfx.Rect{X: b.X + 8, Y: y, Width: b.Width - 16, Height: 30}, placeholder, n.query.String())
		y += 38
	}
	components.PaintSeparator(c, theme, gfx.Rect{X: b.X + 8, Y: y - 1, Width: max(0, b.Width-16), Height: 1}, components.SeparatorHorizontal)
	y += 6
	body := gfx.Rect{X: b.X + 6, Y: y, Width: b.Width - 12, Height: max(0, b.Y+b.Height-y-6)}
	type entry struct {
		id, label      string
		folder, pinned bool
		depth          int
	}
	var rows []entry
	query := strings.ToLower(n.query.String())
	matches := map[string]bool{}
	parents := map[string]bool{}
	for _, note := range n.list {
		if !s.noteMatchesSearch(note, query) {
			continue
		}
		matches[note.ID] = true
		id := note.Folder
		seen := map[string]bool{}
		for id != "" && !seen[id] {
			seen[id] = true
			parents[id] = true
			f, ok := s.noteFolder(id)
			if !ok {
				break
			}
			id = f.Parent
		}
	}
	folders := slices.Clone(n.folders)
	slices.SortStableFunc(folders, func(a, b savedNoteFolder) int { return a.Order - b.Order })
	notes := slices.Clone(n.list)
	slices.SortStableFunc(notes, func(a, b savedNote) int {
		if a.Pinned != b.Pinned {
			if a.Pinned {
				return -1
			}
			return 1
		}
		return a.Order - b.Order
	})
	seen := map[string]bool{}
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
			rows = append(rows, entry{id: f.ID, label: f.Name, folder: true, depth: depth})
			if v.expanded[f.ID] || query != "" {
				walk(f.ID, depth+1)
			}
		}
		if v.inlineNew && v.inlineParent == parent {
			rows = append(rows, entry{folder: true, depth: depth})
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
			rows = append(rows, entry{id: note.ID, label: label, pinned: note.Pinned, depth: depth})
		}
	}
	walk("", 0)
	v.listArea = s.noteHoverScrollArea(c, body, gfx.Size{Width: body.Width, Height: float32(len(rows)) * 30}, v.treeScroll, hitNoteListScroll, hitNoteListThumb, s.noteText("Scroll notes", "滚动笔记"), &v.treeBar, v.scrollDrag, s.noteScrollRegions()&1 != 0)
	geo := v.listArea.BeginContent()
	v.treeScroll = geo.OffsetY
	treeView := components.BeginTree(c, theme, geo.Content).RowHeight(30)
	for _, e := range rows {
		token := v.noteTokens[e.id]
		id, more := hitNoteRows.At(token), hitNoteRowMore.At(token)
		disclosure := gfx.ID{}
		selected := n.selected == e.id && !e.folder && !v.treeFolderSelection
		if e.folder {
			token = v.folderTokens[e.id]
			id = hitNoteFolderRows.At(token)
			more = hitNoteFolderMore.At(token)
			disclosure = hitNoteDisclosures.At(token)
			selected = v.treeFolderSelection && n.folder == e.id
		}
		inline := e.id == v.inlineID && v.inlineID != "" || e.id == "" && v.inlineNew
		if inline {
			id = gfx.ID{}
			disclosure = gfx.ID{}
		}
		row := treeView.Item(components.TreeItem{ID: id, DisclosureID: disclosure, Label: e.label, Depth: uint8(min(e.depth, 31)), Branch: e.folder, Expanded: v.expanded[e.id] || query != "", Selected: selected})
		n.rows = append(n.rows, noteRow{e.id, e.folder, row.Bounds})
		if inline {
			s.noteInputAt(c, hitNoteInline, row.Content.Inset(gfx.Insets{Right: 4}), s.noteText("Name", "名称"), v.inline.String())
			continue
		}
		content := row.Content
		content.X += 4
		content.Width = max(0, content.Width-36)
		icon := icons.MitMarkdown
		if e.pinned {
			icon = icons.StarSolid
		}
		if e.folder {
			icon = icons.FolderSolid
			if v.expanded[e.id] || query != "" {
				icon = icons.FolderSolidOpen
			}
		}
		c.DrawIcon(icon, gfx.Rect{X: content.X, Y: content.Y + 7, Width: 16, Height: 16}, gfx.Color{Alpha: 1})
		content.X += 22
		content.Width = max(0, content.Width-22)
		c.Clip(content)
		c.Label(e.label, content, gfx.TextStyle{Size: 13, Color: s.pal.text})
		c.Unclip()
		if c.Hovered() == id || c.Hovered() == more || selected {
			s.noteButton(c, more, gfx.Rect{X: row.Bounds.X + row.Bounds.Width - 30, Y: row.Bounds.Y + 1, Width: 28, Height: 28}, s.noteText("More actions", "更多操作"), icons.Ellipsis)
		}
		if event, ok := c.PointerOn(id); ok && event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonSecondary {
			v.menuID = e.id
			v.menuFolder = e.folder
			v.menu = "note"
			if e.folder {
				v.menu = "folder"
			}
			v.overlayAnchor = gfx.Rect{X: event.X, Y: event.Y, Width: 1, Height: 1}
			v.overlayFrom = gfx.ID{}
			c.Consume()
		}
	}
	if len(rows) == 0 {
		message := s.noteText("No notes yet. Create one above.", "暂无笔记，可点击上方新建。")
		if query != "" {
			message = s.noteText("No matching notes.", "没有匹配的笔记。")

		}
		c.Label(message, gfx.Rect{X: geo.Content.X + 6, Y: geo.Content.Y + 12, Width: body.Width - 12, Height: 40}, gfx.TextStyle{Size: 12, Color: s.pal.muted})
	}
	if n.dragging {
		d := v.drop
		if d.valid {
			color := theme.Primary
			if d.inside {
				c.RoundedOutline(d.bounds, 4, 2, color)
			} else {
				c.Fill(gfx.Rect{X: d.bounds.X, Y: d.bounds.Y, Width: d.bounds.Width, Height: 2}, color)
			}
		} else {
			c.Label(s.noteText("Cannot move here", "无法移到此处"), gfx.Rect{X: n.dragStart.X, Y: n.dragStart.Y, Width: 150, Height: 24}, gfx.TextStyle{Size: 12, Color: s.pal.danger})
		}
	}
	v.listArea.EndContent()
}
func (s *State) paintNoteEditor(c *gfx.Canvas, b gfx.Rect) {
	n, v := &s.notes, &s.notes.view
	v.documentBounds = b
	n.sourceBounds, n.previewBounds = gfx.Rect{}, gfx.Rect{}
	x, y := b.X+18, b.Y+8
	right := b.X + b.Width - 16
	s.noteButton(c, hitNoteTreeToggle, gfx.Rect{X: x, Y: y, Width: 28, Height: 28}, s.noteText("Toggle library", "显示 / 隐藏目录"), icons.PanelLeft)
	x += 34
	if s.noteIndex(n.selected) < 0 {
		v.splitScroll.active = false
		c.Label(s.noteText("Select a note or create one to begin.", "选择笔记，或新建一篇笔记。"), gfx.Rect{X: x, Y: y + 80, Width: max(0, right-x), Height: 40}, gfx.TextStyle{Size: 16, Color: s.pal.muted})
		return
	}
	y = s.paintNoteLibraryHeader(c, b, x, y, right)
	mode := v.mode
	if mode == noteSplit && b.Width < 640 {
		mode = noteSource
	}
	if mode != noteSplit {
		v.splitScroll.active = false
	}
	if mode != notePreview {
		y = s.paintNoteFormatting(c, b, y, right)
	}
	message := n.error
	if n.problem != "" {
		message = n.problem
	}
	if message != "" {
		c.Label(message, gfx.Rect{X: b.X + 20, Y: y, Width: b.Width - 40, Height: 24}, gfx.TextStyle{Size: 12, Color: s.pal.danger})
		y += 26
	}
	y += s.paintNoteLinkNotice(c, b.X+20, y, b.Width-40)
	content := gfx.Rect{X: b.X + 16, Y: y, Width: max(0, b.Width-32), Height: max(0, b.Y+b.Height-y-16)}
	if mode == noteSource {
		n.sourceBounds = content
		s.paintNoteSource(c, content)
	} else if mode == notePreview {
		n.previewBounds = content
		s.paintNotePreview(c, content)
	} else {
		source := content
		source.Width = (content.Width - 16) / 2
		preview := source
		preview.X += source.Width + 16
		n.sourceBounds = source
		n.previewBounds = preview
		s.paintNoteSource(c, source)
		s.paintNotePreview(c, preview)
	}
}
func (s *State) paintNoteFormatting(c *gfx.Canvas, b gfx.Rect, y, right float32) float32 {
	x := b.X + 18
	for i, label := range noteFormatLabels {
		if x+30 > right {
			break
		}
		icon := noteFormatIcon(i)
		if len(icon.Commands) > 0 {
			label = s.noteFormatName(i)
		}
		s.noteButton(c, hitNoteFormats.At(i), gfx.Rect{X: x, Y: y, Width: 28, Height: 26}, label, icon)
		x += 32
	}
	status := s.noteUpdatedLabel()
	if linked, ok := s.noteLinkStatus(s.noteContent().selected, func(text string) float32 { return c.TextWidth(text, 11) }, right-x-12); ok {
		status = linked
	}
	width := c.TextWidth(status, 11)
	if right-width >= x-4+12 {
		c.Label(status, gfx.Rect{X: right - width, Y: y + 3, Width: width, Height: 20}, gfx.TextStyle{Size: 11, Color: s.pal.muted})
	}
	return y + 34
}
func (s *State) paintNoteSource(c *gfx.Canvas, b gfx.Rect) {
	n := s.noteContent()
	b = s.paintNoteFind(c, n, b)
	n.sourceBounds = b
	value := n.body.String()
	sel, _ := n.noteSelections()
	snap := components.CodeEditorSnapshot{ID: hitNoteBody, Label: s.noteText("Markdown source", "Markdown 源码"), Value: value, Focused: c.Focused() == hitNoteBody, Wrap: true, LineNumbers: true, SelectionStart: aiByteOffset(value, sel.Start), SelectionEnd: aiByteOffset(value, sel.End), Caret: aiByteOffset(value, sel.End), ShowCaret: sel.Start == sel.End, Language: "markdown", Highlight: s.highlightNoteMarkdown, ScrollbarID: hitNoteSourceScroll}
	snap.CurrentLine, snap.MatchBrackets = true, true
	snap.FontSize, snap.MonoFace, snap.TabWidth = noteFontSize, s.terminalFontFace, 4
	if n.editing.findOpen {
		snap.Marks = n.editing.find.Matches()
		if index := find.Index(snap.Marks, snap.SelectionStart, snap.SelectionEnd); index >= 0 {
			snap.CurrentMark = snap.Marks[index]
		}
	}
	if r, ok := n.body.Composition(); ok {
		snap.CompositionStart = aiByteOffset(value, r.Start)
		snap.CompositionEnd = aiByteOffset(value, r.End)
	}
	n.code.Paint(c, s.Theme(), b, snap)
}
func (s *State) paintNotePreview(c *gfx.Canvas, b gfx.Rect) {
	s.paintNotePreviewContent(c, b, s.noteContent())
}
func (s *State) paintNotePreviewContent(c *gfx.Canvas, b gfx.Rect, n *notesUI) {
	if source := n.body.String(); source != n.view.previewSource {
		n.view.previewSource = source
		n.view.clearPreviewSelection()
	}
	source, width := n.body.String(), max(1, b.Width-12)
	cache := &n.previewLayout
	if !cache.ready || cache.source != source || cache.width != width || cache.face != s.terminalFontFace {
		n.doc.Parse(source)
		height := n.rendered.Layout(c, components.MarkdownStyle{Size: noteFontSize, MonoFace: s.terminalFontFace, CodeInset: 28, Highlight: highlightCode}, &n.doc, width)
		*cache = notePreviewLayout{ready: true, source: source, width: width, face: s.terminalFontFace, height: height}
	}
	height := cache.height
	images := float32(0)
	if n.imageSource == n.body.String() {
		for _, im := range n.images {
			images += min(float32(320), b.Width*float32(im.bitmap.Height)/float32(im.bitmap.Width)) + 12
		}
	}
	n.previewHeight = height + 12 + images
	if n == s.noteContent() && s.noteLibraryView() && n.view.mode == noteSplit && n.view.documentBounds.Width >= 640 {
		s.syncNoteSplit(c)
	}
	v := &n.view
	hovered := s.noteScrollRegions()&2 != 0
	if n != &s.notes {
		hovered = v.pointerValid && pointInRect(v.pointer.X, v.pointer.Y, b)
	}
	area := s.noteHoverScrollArea(c, b, gfx.Size{Width: b.Width, Height: n.previewHeight}, n.previewScroll, hitNotePreviewScroll, hitNotePreviewThumb, s.noteText("Scroll preview", "滚动预览"), &v.previewBar, v.previewDragging, hovered)
	v.previewArea = area
	geo := area.BeginContent()
	n.previewScroll = geo.OffsetY
	c.HitWithOptions(hitNotePreviewText, gfx.Rect{X: b.X, Y: b.Y, Width: b.Width, Height: min(b.Height, max(0, geo.Content.Y+height+12-b.Y))}, s.noteText("Preview text", "预览文本"), gfx.HitOptions{Role: gfx.RoleGroup, SkipTab: true, ReadOnly: true, Cursor: gfx.PointerCursorText})
	look := components.MarkdownLookFor(s.Theme())
	look.Code = s.codeColors()
	n.rendered.Paint(c, components.MarkdownPaint{Selection: n.view.previewSelect.In(0), Look: look, X: geo.Content.X + 6, Y: geo.Content.Y + 6, Clip: b, Links: hitNoteLinks})
	s.paintNoteCodeCopies(c, n, gfx.Point{X: geo.Content.X + 6, Y: geo.Content.Y + 6}, b)
	s.paintNoteImagesContent(c, gfx.Rect{X: geo.Content.X, Y: geo.Content.Y, Width: b.Width - 12, Height: b.Height}, geo.Content.Y+height+12, n)
	area.EndContent()
}
func (s *State) noteModeLabel(mode noteMode) string {
	switch mode {
	case notePreview:
		return s.noteText("Preview", "预览")
	case noteSplit:
		return s.noteText("Split", "分屏")
	default:
		return s.noteText("Source", "源码")
	}
}

var hitNotePreviewScroll = tree.ID(notesHits, "preview-scroll")
var hitNoteListThumb = tree.ID(notesHits, "list-thumb")

func (s *State) noteUpdatedLabel() string {
	status := s.noteContent().draft.Updated
	if date, err := time.Parse(time.RFC3339Nano, status); err == nil {
		status = date.Local().Format("2006-01-02 15:04")
	}
	return s.noteText("Updated ", "更新于 ") + status
}
