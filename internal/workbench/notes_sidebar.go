package workbench

import (
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/workbench/icons"
)

var (
	hitNoteSideWidth   = tree.ID(notesHits, "side-width")
	hitNoteSideSplit   = tree.ID(notesHits, "side-split")
	hitNoteSideOpen    = tree.ID(notesHits, "side-open")
	hitNoteSideLinked  = tree.ID(notesHits, "side-linked")
	hitNoteSideExpand  = tree.ID(notesHits, "side-expand-all")
	hitNoteSideRowOpen = tree.Sub(notesHits, tree.Name("side-row-open"))
)

// The browser owns its presentation state; it never selects an editing draft.
type noteSidebar struct {
	content                      notesUI
	width, fraction              float32
	lastQuery                    string
	bounds, resizeBounds         gfx.Rect
	widthDragging, splitDragging bool
}

func (s *State) noteSidebarVisible() bool {
	return s.noteTarget == nil && s.modal == modalNone && s.activeSection == sectionTerminal && s.notes.panel
}
func (s *State) noteSidebarState() *noteSidebar {
	if s.notes.sidebar == nil {
		s.notes.sidebar = &noteSidebar{width: 400, fraction: .5, content: notesUI{noteDraft: &noteDraft{}}}
		s.notes.sidebar.content.view.expanded = map[string]bool{}
	}
	return s.notes.sidebar
}
func (s *State) notePreviewState() *notesUI {
	if s.noteTarget != nil {
		return s.noteTarget
	}
	if s.noteSidebarVisible() {
		return &s.noteSidebarState().content
	}
	return &s.notes
}
func (v *noteSidebar) stopCapture() {
	v.widthDragging, v.splitDragging = false, false
	v.content.view.scrollDrag, v.content.view.previewDragging = false, false
	v.content.view.previewSelect.EndDrag()
	v.content.view.pointerValid = false
}
func (s *State) noteBrowserNote(note savedNote) savedNote {
	if buffer := s.notes.drafts[note.ID]; buffer != nil {
		note.Title, note.Body, note.Tags = buffer.title.String(), buffer.body.String(), noteWords(buffer.tags.String())
	} else if note.ID == s.notes.selected && s.notes.noteDraft != nil {
		note.Title, note.Body, note.Tags = s.notes.title.String(), s.notes.body.String(), noteWords(s.notes.tags.String())
	}
	return note
}

func (s *State) syncNoteSidebar() {
	n := &s.noteSidebarState().content
	if n.selected == "" {
		return
	}
	i := s.noteIndex(n.selected)
	if i < 0 {
		n.selected = ""
		n.view.clearPreviewSelection()
		if n.imageCancel != nil {
			n.imageCancel()
		}
		n.imageToken++
		n.images = nil
		n.imageSource = ""
		return
	}
	note := s.noteBrowserNote(s.notes.list[i])
	if n.body.String() != note.Body {
		n.body = text.Editor{}
		n.body.Insert(note.Body)
		n.view.clearPreviewSelection()
		if n.imageCancel != nil {
			n.imageCancel()
		}
		n.imageToken++
		n.images = nil
		n.imageSource = ""
	}
	n.draft = note
}
func (s *State) noteSideWidthResizable(c *gfx.Canvas) components.Resizable {
	v := s.noteSidebarState()
	b := v.resizeBounds
	usable := max(1, b.Width-1)
	maximum := max(0, b.Width*.7)
	minimum := min(float32(280), maximum)
	return components.BeginResizable(c, s.Theme(), hitNoteSideWidth, b, 1-v.bounds.Width/usable).Limits(1-maximum/usable, 1-minimum/usable).HandleSize(paneDividerHitWidth)
}
func (s *State) noteSideSplitResizable(c *gfx.Canvas) components.Resizable {
	v := s.noteSidebarState()
	bounds := v.bounds.Inset(gfx.Insets{Left: 1, Right: 1, Top: 1, Bottom: 1})
	usable := max(1, bounds.Height-1)
	first, second := float32(120), float32(160)
	if usable < first+second {
		first = usable * 120 / 280
		second = usable - first
	}
	return components.BeginResizable(c, s.Theme(), hitNoteSideSplit, bounds, v.fraction).Vertical().Limits(first/usable, 1-second/usable).HandleSize(paneDividerHitWidth)
}
func (s *State) paintNoteSidebar(c *gfx.Canvas, b gfx.Rect) {
	s.noteTokens()
	v := s.noteSidebarState()
	n := &v.content
	s.syncNoteSidebar()
	v.bounds = b
	v.resizeBounds = gfx.Rect{X: s.workspacePixelBounds(s.viewport).X, Y: b.Y, Width: s.workspacePixelBounds(s.viewport).Width + b.Width, Height: b.Height}
	c.Fill(b, s.pal.workspace)
	if n.selected == "" {
		s.paintNoteSidebarTree(c, b.Inset(gfx.Insets{Left: 1, Right: 1, Top: 1, Bottom: 1}))
		n.previewBounds = gfx.Rect{}
		v.splitDragging = false
	} else {
		split := s.noteSideSplitResizable(c).Paint(s.noteText("Resize note browser sections", "调整笔记上下区域"))
		s.paintNoteSidebarTree(c, split.First)
		body := split.Second.Inset(gfx.Insets{Left: 8, Right: 8, Top: 6, Bottom: 8})
		title := n.draft.Title
		if strings.TrimSpace(title) == "" {
			title = s.noteText("Untitled", "无标题")
		}
		w := body.Width - 34
		components.BeginButton(c, s.Theme(), hitNoteSideOpen, gfx.Rect{X: body.X + body.Width - 28, Y: body.Y, Width: 28, Height: 28}).Height(28).WithIcon(icons.ExternalLink).Icon().Ghost(s.noteText("Open in window", "在窗口中打开"))
		if len(noteImageURLs(n.body.String())) > 0 {
			w -= 34
			components.BeginButton(c, s.Theme(), hitNoteLoadImages, gfx.Rect{X: body.X + body.Width - 62, Y: body.Y, Width: 28, Height: 28}).Height(28).WithIcon(icons.Image).Icon().Ghost(s.noteText("Load images", "加载图片"))
		}
		c.Clip(gfx.Rect{X: body.X, Y: body.Y, Width: max(0, w), Height: 28})
		c.Label(title, gfx.Rect{X: body.X, Y: body.Y, Width: max(0, w), Height: 28}, gfx.TextStyle{Size: 14, Color: s.pal.text})
		c.Unclip()
		components.PaintSeparator(c, s.Theme(), gfx.Rect{X: body.X, Y: body.Y + 32, Width: body.Width, Height: 1}, components.SeparatorHorizontal)
		body.Y += 39
		body.Height = max(0, body.Height-39)
		n.previewBounds = body
		s.paintNotePreviewContent(c, body, n)
		if n.error != "" {
			c.Label(n.error, gfx.Rect{X: body.X, Y: body.Y, Width: body.Width, Height: 24}, gfx.TextStyle{Size: 11, Color: s.pal.danger})
		}
	}
	s.noteSideWidthResizable(c).Paint(s.noteText("Resize notes panel", "调整笔记侧栏宽度"))
}

func (s *State) noteSidebarCanExpandAll() bool {
	for _, folder := range s.notes.folders {
		if !s.noteSidebarState().content.view.expanded[folder.ID] {
			return true
		}
	}
	return false
}

func (s *State) paintNoteSidebarTree(c *gfx.Canvas, b gfx.Rect) {
	v := s.noteSidebarState()
	n := &v.content
	theme := s.Theme()
	top := b.Inset(gfx.Insets{Left: 8, Right: 8, Top: 8})
	label := s.noteText("Linked only", "只看关联")
	labelWidth := c.TextWidth(label, 12)
	switchW := labelWidth + 6 + aiSwitchWidth
	labelX := top.X + top.Width - switchW
	expandIcon, expandLabel := icons.ListX, s.noteText("Collapse all", "收起全部")
	if s.noteSidebarCanExpandAll() || len(s.notes.folders) == 0 {
		expandIcon, expandLabel = icons.ListTree, s.noteText("Expand all", "展开全部")
	}
	expandButton := components.BeginButton(c, theme, hitNoteSideExpand, gfx.Rect{X: top.X, Y: top.Y + 1, Width: 28, Height: 28}).Height(28).WithIcon(expandIcon).Icon()
	if len(s.notes.folders) == 0 || n.query.String() != "" {
		expandButton = expandButton.Disabled()
	}
	expandButton.Ghost(expandLabel)
	s.noteInputAt(c, hitNoteSearch, gfx.Rect{X: top.X + 34, Y: top.Y, Width: max(1, top.Width-switchW-42), Height: 30}, s.noteText("Search title, body and tags", "搜索标题、正文和标签"), n.query.String())
	c.Label(label, gfx.Rect{X: labelX, Y: top.Y, Width: labelWidth, Height: 30}, gfx.TextStyle{Size: 12, Color: s.pal.text})
	s.aiSwitch(c, hitNoteSideLinked, gfx.Rect{X: top.X + top.Width - aiSwitchWidth, Y: top.Y + (30-aiSwitchHeight)/2, Width: aiSwitchWidth, Height: aiSwitchHeight}, !n.sideAll, false, label)
	// One native switch hit covers its caption too, without adding a tab stop.
	hits := c.Hits()
	if len(hits) > 0 && hits[len(hits)-1].ID == hitNoteSideLinked {
		hits[len(hits)-1].Bounds = gfx.Rect{X: labelX, Y: top.Y, Width: switchW, Height: 30}
	}

	components.PaintSeparator(c, theme, gfx.Rect{X: top.X, Y: top.Y + 36, Width: top.Width, Height: 1}, components.SeparatorHorizontal)
	body := gfx.Rect{X: b.X + 6, Y: top.Y + 43, Width: max(0, b.Width-12), Height: max(0, b.Y+b.Height-top.Y-49)}
	n.listBounds = body
	type entry struct {
		note     savedNote
		folder   savedNoteFolder
		isFolder bool
		depth    int
	}
	var rows []entry
	matches, parents := map[string]bool{}, map[string]bool{}
	query := strings.ToLower(n.query.String())
	if query != v.lastQuery {
		n.view.treeScroll = 0
		v.lastQuery = query
	}
	host := ""
	if session, ok := s.sessionByIDPointer(s.activeSessionID); ok {
		host = session.host.CatalogID
	}
	for _, stored := range s.notes.list {
		note := s.noteBrowserNote(stored)
		if !n.sideAll && !note.Pinned && (host == "" || !s.noteMatchesHost(note.Tags, host)) {
			continue
		}
		if !s.noteMatchesSearch(note, query) {
			continue
		}
		matches[note.ID] = true
		seen := map[string]bool{}
		for parent := note.Folder; parent != "" && !seen[parent]; {
			seen[parent] = true
			parents[parent] = true
			f, ok := s.noteFolder(parent)
			if !ok {
				break
			}
			parent = f.Parent
		}
	}
	folders := slices.Clone(s.notes.folders)
	slices.SortStableFunc(folders, func(a, b savedNoteFolder) int { return a.Order - b.Order })
	notes := slices.Clone(s.notes.list)
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
			if f.Parent != parent || seen[f.ID] || ((query != "" || !n.sideAll) && !parents[f.ID]) {
				continue
			}
			seen[f.ID] = true
			rows = append(rows, entry{folder: f, isFolder: true, depth: depth})
			if n.view.expanded[f.ID] || query != "" {
				walk(f.ID, depth+1)
			}
		}
		for _, note := range notes {
			p := note.Folder
			if _, ok := s.noteFolder(p); !ok {
				p = ""
			}
			if matches[note.ID] && p == parent {
				rows = append(rows, entry{note: s.noteBrowserNote(note), depth: depth})
			}
		}
	}
	walk("", 0)
	n.view.listArea = s.noteHoverScrollArea(c, body, gfx.Size{Width: body.Width, Height: float32(len(rows)) * 30}, n.view.treeScroll, hitNoteListScroll, hitNoteListThumb, s.noteText("Scroll notes", "滚动笔记"), &n.view.treeBar, n.view.scrollDrag, n.view.pointerValid && pointInRect(n.view.pointer.X, n.view.pointer.Y, body))
	geo := n.view.listArea.BeginContent()
	n.view.treeScroll = geo.OffsetY
	tv := components.BeginTree(c, theme, geo.Content).RowHeight(30)
	for _, e := range rows {
		id := e.note.ID
		label := e.note.Title
		icon := icons.MitMarkdown
		rowID := hitNoteRows.At(s.notes.view.noteTokens[id])
		disclosure := gfx.ID{}
		if e.note.Pinned {
			icon = icons.StarSolid
		}
		if e.isFolder {
			id = e.folder.ID
			label = e.folder.Name
			rowID = hitNoteFolderRows.At(s.notes.view.folderTokens[id])
			disclosure = hitNoteDisclosures.At(s.notes.view.folderTokens[id])
			icon = icons.FolderSolid
			if n.view.expanded[id] || query != "" {
				icon = icons.FolderSolidOpen
			}
		}
		if strings.TrimSpace(label) == "" {
			label = s.noteText("Untitled", "无标题")
		}
		row := tv.Item(components.TreeItem{ID: rowID, DisclosureID: disclosure, Label: label, Depth: uint8(min(e.depth, 31)), Branch: e.isFolder, Expanded: n.view.expanded[id] || query != "", Selected: !e.isFolder && id == n.selected})
		content := row.Content
		content.X += 4
		content.Width = max(0, content.Width-4)
		c.DrawIcon(icon, gfx.Rect{X: content.X, Y: content.Y + 7, Width: 16, Height: 16}, gfx.Color{Alpha: 1})
		content.X += 22
		content.Width = max(0, content.Width-22)
		openID := hitNoteSideRowOpen.At(s.notes.view.noteTokens[id])
		showOpen := !e.isFolder && (c.Hovered() == rowID || c.Hovered() == openID || c.FocusVisible() == rowID || c.FocusVisible() == openID)
		if showOpen {
			content.Width = max(0, content.Width-32)
		}
		c.Clip(content)
		c.Label(label, content, gfx.TextStyle{Size: 13, Color: s.pal.text})
		c.Unclip()
		if showOpen {
			components.BeginButton(c, theme, openID, gfx.Rect{X: row.Bounds.X + row.Bounds.Width - 30, Y: row.Bounds.Y + 1, Width: 28, Height: 28}).Height(28).WithIcon(icons.ExternalLink).Icon().Ghost(s.noteText("Open in window", "在窗口中打开"))
		}
	}
	if len(rows) == 0 {
		c.Label(s.noteText("No matching notes.", "没有匹配的笔记。"), gfx.Rect{X: geo.Content.X + 6, Y: geo.Content.Y + 8, Width: max(0, body.Width-12), Height: 40}, gfx.TextStyle{Size: 12, Color: s.pal.muted})
	}
	n.view.listArea.EndContent()
}
func (s *State) clickNoteSidebar(id gfx.ID) bool {
	n := &s.noteSidebarState().content
	if id == hitNoteSideExpand {
		if n.query.String() == "" {
			expand := s.noteSidebarCanExpandAll()
			for _, folder := range s.notes.folders {
				n.view.expanded[folder.ID] = expand
			}
		}
		return true
	}
	if token, ok := id.In(hitNoteSideRowOpen); ok {
		s.openNoteWindow(s.notes.view.tokenNotes[token])
		return true
	}
	if s.clickNoteCodeCopy(id, n) {
		return true
	}
	if id == hitNoteSideOpen {
		s.openNoteWindow(n.selected)
		return true
	}
	if id == hitNoteSideLinked {
		n.sideAll = !n.sideAll
		n.view.treeScroll = 0
		return true
	}
	if id == hitNoteLoadImages {
		s.syncNoteSidebar()
		if n.selected != "" {
			s.loadNoteImagesContent(n, true)
		}
		return true
	}
	token, ok := id.In(hitNoteDisclosures)
	if !ok {
		token, ok = id.In(hitNoteFolderRows)
	}
	if ok {
		if path := s.notes.view.tokenFolders[token]; path != "" {
			n.view.expanded[path] = !n.view.expanded[path]
		}
		return true
	}
	if token, ok := id.In(hitNoteRows); ok {
		key := s.notes.view.tokenNotes[token]
		if s.noteIndex(key) >= 0 && key != n.selected {
			if n.imageCancel != nil {
				n.imageCancel()
			}
			n.imageToken++
			n.images = nil
			n.imageSource = ""
			n.error = ""
			n.view.codeActions.source = ""
			n.view.codeActions.targets = nil
			n.selected = key
			n.previewScroll = 0
			n.view.previewBar = noteScrollbar{}
			n.view.clearPreviewSelection()
			s.syncNoteSidebar()
		}
		return true
	}
	if i, ok := id.In(hitNoteLinks); ok {
		if url := n.rendered.Link(i); url != "" && s.options.OpenURL != nil {
			if err := s.options.OpenURL(url); err != nil {
				n.error = err.Error()
			}
		}
		return true
	}
	// Stale editing hits must never fall through to the main editor.
	return tree.Within(id.Scope(), notesHits)
}
func (s *State) noteSidebarPointer(e gfx.PointerEvent) gfx.EventResult {
	v := s.noteSidebarState()
	n := &v.content
	n.view.pointer = gfx.Point{X: e.X, Y: e.Y}
	n.view.pointerValid = e.Kind != gfx.PointerExit
	if e.Kind == gfx.PointerDown && (e.Button == gfx.PointerButtonPrimary || e.Button == gfx.PointerButtonNone) {
		if e.ClickCount == 2 {
			if token, ok := e.Target.In(hitNoteRows); ok {
				s.openNoteWindow(s.notes.view.tokenNotes[token])
				return gfx.EventResult{Handled: true, Repaint: true}
			}
		}
		v.widthDragging = e.Target == hitNoteSideWidth
		v.splitDragging = e.Target == hitNoteSideSplit
	}
	if v.widthDragging || v.splitDragging {
		if e.Kind == gfx.PointerMove || e.Kind == gfx.PointerDown || e.Kind == gfx.PointerUp {
			if v.widthDragging {
				v.width = (1 - s.noteSideWidthResizable(nil).FractionAt(e.X)) * max(1, v.resizeBounds.Width-1)
				s.fitTerminals()
			}
			if v.splitDragging {
				v.fraction = s.noteSideSplitResizable(nil).FractionAt(e.Y)
			}
		}
		if e.Kind == gfx.PointerUp || e.Kind == gfx.PointerExit {
			v.stopCapture()
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	for _, bar := range []struct {
		id           gfx.ID
		area         components.ScrollArea
		dragging     *bool
		grab, offset *float32
	}{{hitNoteListThumb, n.view.listArea, &n.view.scrollDrag, &n.view.scrollGrab, &n.view.treeScroll}, {hitNotePreviewThumb, n.view.previewArea, &n.view.previewDragging, &n.view.previewGrab, &n.previewScroll}} {
		geo := bar.area.Geometry()
		if e.Kind == gfx.PointerDown && e.Target == bar.id && (e.Button == gfx.PointerButtonPrimary || e.Button == gfx.PointerButtonNone) && geo.MaxOffsetY > 0 {
			*bar.dragging = true
			*bar.grab = geo.VerticalThumb.Height / 2
			if e.Y >= geo.VerticalThumb.Y && e.Y < geo.VerticalThumb.Y+geo.VerticalThumb.Height {
				*bar.grab = e.Y - geo.VerticalThumb.Y
			}
		}
		if *bar.dragging {
			if e.Kind == gfx.PointerDown || e.Kind == gfx.PointerMove || e.Kind == gfx.PointerUp {
				*bar.offset = geo.VerticalOffsetForThumb(e.Y - *bar.grab)
			}
			if e.Kind == gfx.PointerUp || e.Kind == gfx.PointerExit {
				*bar.dragging = false
			}
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	if r := s.notePreviewSelectionPointer(e); r.Handled {
		return r
	}
	if pointInRect(e.X, e.Y, v.bounds) || e.Kind == gfx.PointerExit {
		// Native controls activate on an unconsumed pointer-up.
		return gfx.EventResult{Handled: e.Kind == gfx.PointerMove || e.Kind == gfx.PointerExit || e.Target.IsZero(), Repaint: true}
	}
	return gfx.EventResult{}
}
func (s *State) noteSidebarScroll(e gfx.ScrollEvent) gfx.EventResult {
	n := &s.noteSidebarState().content
	if pointInRect(e.X, e.Y, n.listBounds) {
		n.view.treeScroll = min(max(0, n.view.treeScroll+e.DeltaY), n.view.listArea.Geometry().MaxOffsetY)
		n.view.treeBar.activity = true
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if pointInRect(e.X, e.Y, n.previewBounds) {
		n.previewScroll = min(max(0, n.previewScroll+e.DeltaY), n.view.previewArea.Geometry().MaxOffsetY)
		n.view.previewBar.activity = true
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}
func (s *State) noteSidebarKey(e gfx.KeyEvent) gfx.EventResult {
	v := s.noteSidebarState()
	n := &v.content
	if e.Target == hitNoteSideWidth {
		if ratio, ok := s.noteSideWidthResizable(nil).FractionForKey(e.Key); ok {
			v.width = (1 - ratio) * max(1, v.resizeBounds.Width-1)
			s.fitTerminals()
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	if e.Target == hitNoteSideSplit {
		if ratio, ok := s.noteSideSplitResizable(nil).FractionForKey(e.Key); ok {
			v.fraction = ratio
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	if e.Key == gfx.KeyEscape && e.Target == hitNotePreviewText {
		n.view.clearPreviewSelection()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if !e.Release && !e.Repeat && (e.Key == gfx.KeyEnter || e.Key == gfx.KeySpace) {
		_, row := e.Target.In(hitNoteRows)
		_, folder := e.Target.In(hitNoteFolderRows)
		_, disclosure := e.Target.In(hitNoteDisclosures)
		_, copyCode := e.Target.In(hitNoteCodeCopies)
		_, rowOpen := e.Target.In(hitNoteSideRowOpen)
		if copyCode || row || rowOpen || folder || disclosure || e.Target == hitNoteSideExpand || e.Target == hitNoteSideLinked || e.Target == hitNoteSideOpen || e.Target == hitNoteLoadImages {
			s.clickNoteSidebar(e.Target)
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	if tree.Within(e.Target.Scope(), notesHits) && e.Target != hitNoteSearch && e.Key != gfx.KeyTab {
		return gfx.EventResult{Handled: true}
	}
	return gfx.EventResult{}
}
