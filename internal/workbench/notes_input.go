package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

var (
	hitNoteSourceScroll = tree.ID(notesHits, "source-scroll")
	hitNoteHostsScroll  = tree.ID(notesHits, "hosts-scroll")
	hitNoteListScroll   = tree.ID(notesHits, "list-scroll")
	hitNoteLinks        = tree.Sub(notesHits, tree.Name("link"))
)

func (s *State) notesPanelWidth(size ui.Size) float32 {
	base := s.baseWorkspacePixelBounds(size)
	if context, ok := s.deck.Context(s.deck.ActiveWorkspace()); ok && context.Open && size.Width >= contextDrawerAt {
		base.Width = max(0, base.Width-s.contextPanelWidth(base, context)-1)
	}
	base.Width -= s.aiReserve(s.deck.ActiveWorkspace(), size) + s.independentMonitorReserve(s.deck.ActiveWorkspace(), size)
	width := float32(400)
	if s.notes.sidebar != nil {
		width = s.notes.sidebar.width
	}
	maximum := max(0, base.Width*.7)
	return min(max(width, min(280, maximum)), maximum)
}
func (s *State) notesVisible() bool {
	if s.noteTarget != nil && s.noteTarget.windowView {
		return true
	}
	return s.modal == modalNone && (s.activeSection == sectionNotes || s.activeSection == sectionTerminal && s.notes.panel)
}
func (s *State) noteResizable(canvas *ui.Canvas) components.Resizable {
	v := &s.notes.view
	b := v.workspace
	return components.BeginResizable(canvas, s.Theme(), hitNoteDivider, b, v.width/max(1, b.Width-1)).Limits(160/max(1, b.Width-1), 520/max(1, b.Width-1)).LimitsPx(160, 480)
}
func (s *State) notesPointer(e ui.PointerEvent) (result ui.EventResult) {
	if s.noteSidebarVisible() {
		return s.noteSidebarPointer(e)
	}
	before := s.noteScrollRegions()
	s.noteContent().view.pointer = ui.Point{X: e.X, Y: e.Y}
	s.noteContent().view.pointerValid = e.Kind != ui.PointerExit
	defer func() {
		if before != s.noteScrollRegions() || pointInRect(e.X, e.Y, s.noteContent().previewBounds) || e.Kind == ui.PointerExit {
			result.Repaint = true
			if s.noteLibraryView() && (e.Kind == ui.PointerMove || e.Kind == ui.PointerExit) {
				result.Handled = true
			}
		}
	}()
	n, v := s.noteContent(), &s.noteContent().view
	if e.Kind == ui.PointerDown && (e.Target == hitNoteTitle || e.Target == hitNoteBody) && !s.activateNoteView(n) {
		return ui.EventResult{Handled: true}
	}
	if !s.notesVisible() {
		n.dragID = ""
		v.resizing = false
		v.previewDragging = false
		v.previewSelect.EndDrag()
		return ui.EventResult{}
	}
	if v.popup == "tags" || v.popup == "hosts" {
		if e.Kind == ui.PointerDown && e.Target == hitNoteTagPickerThumb {
			geo := v.tagPopupArea.Geometry()
			v.tagPopupDragging = true
			v.tagPopupGrab = geo.VerticalThumb.Height / 2
			if e.Y >= geo.VerticalThumb.Y && e.Y < geo.VerticalThumb.Y+geo.VerticalThumb.Height {
				v.tagPopupGrab = e.Y - geo.VerticalThumb.Y
			}
		}
		if v.tagPopupDragging {
			if e.Kind == ui.PointerMove || e.Kind == ui.PointerDown || e.Kind == ui.PointerUp {
				v.tagPopupScroll = v.tagPopupArea.Geometry().VerticalOffsetForThumb(e.Y - v.tagPopupGrab)
				if e.Kind == ui.PointerUp {
					v.tagPopupDragging = false
				}
				return ui.EventResult{Handled: true, Repaint: true}
			}
			v.tagPopupDragging = false
		}
	}
	if n.confirm != "" || v.menu != "" || v.popup != "" {
		return ui.EventResult{}
	}
	if s.noteLibraryView() {
		if r := s.notePreviewScrollbarPointer(e); r.Handled {
			return r
		}
	}
	if r := s.notePreviewSelectionPointer(e); r.Handled {
		return r
	}
	if e.Kind == ui.PointerDown && e.Target == hitNoteDivider {
		v.resizing = true
	}
	if v.resizing {
		if e.Kind == ui.PointerMove || e.Kind == ui.PointerDown || e.Kind == ui.PointerUp {
			v.width = s.noteResizable(nil).FractionAt(e.X) * max(1, v.workspace.Width-1)
			if e.Kind == ui.PointerUp {
				v.resizing = false
				s.save()
			}
			return ui.EventResult{Handled: true, Repaint: true, Focus: hitNoteDivider}
		}
		if e.Kind == ui.PointerExit {
			v.resizing = false
		}
		return ui.EventResult{Handled: true}
	}
	if e.Kind == ui.PointerDown && e.Target == hitNoteListThumb {
		geo := v.listArea.Geometry()
		v.scrollDrag = true
		v.scrollGrab = geo.VerticalThumb.Height / 2
		if e.Y >= geo.VerticalThumb.Y && e.Y < geo.VerticalThumb.Y+geo.VerticalThumb.Height {
			v.scrollGrab = e.Y - geo.VerticalThumb.Y
		}
	}
	if v.scrollDrag {
		if e.Kind == ui.PointerMove || e.Kind == ui.PointerDown || e.Kind == ui.PointerUp {
			v.treeScroll = v.listArea.Geometry().VerticalOffsetForThumb(e.Y - v.scrollGrab)
			if e.Kind == ui.PointerUp {
				v.scrollDrag = false
			}
			return ui.EventResult{Handled: true, Repaint: true}
		}
		v.scrollDrag = false
	}
	if e.Kind == ui.PointerDown && (v.inlineID != "" || v.inlineNew) && e.Target != hitNoteInline {
		if !s.commitNoteInline() {
			return ui.EventResult{Handled: true, Focus: hitNoteInline, Repaint: true}
		}
	}
	if e.Kind == ui.PointerDown && e.Button == ui.PointerButtonPrimary && e.Target == hitNoteListScroll && !n.dragging {
		n.folder = ""
		v.treeFolderSelection = true
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if r := n.code.Pointer(e); r.Handled {
		s.noteScrollDriver(false)
		return r
	}
	switch e.Kind {
	case ui.PointerDown:
		switch e.Target {
		case hitNoteTitle:
			n.body.Unmark()
			n.tags.Unmark()
		case hitNoteBody:
			n.title.Unmark()
			n.tags.Unmark()
		}
		n.dragID = ""
		n.dragging = false
		if e.Button != ui.PointerButtonPrimary || n.query.String() != "" {
			return ui.EventResult{}
		}
		if token, ok := e.Target.In(hitNoteRows); ok {
			n.dragID = v.tokenNotes[token]
			n.dragFolder = false
		}
		if token, ok := e.Target.In(hitNoteFolderRows); ok {
			n.dragID = v.tokenFolders[token]
			n.dragFolder = true
		}
		n.dragStart = ui.Point{X: e.X, Y: e.Y}
	case ui.PointerMove:
		if n.dragID != "" {
			dx, dy := e.X-n.dragStart.X, e.Y-n.dragStart.Y
			if dx*dx+dy*dy > 25 {
				n.dragging = true
			}
			if n.dragging {
				s.noteDropAt(e.X, e.Y)
				if e.Y < n.listBounds.Y+48 {
					v.treeScroll = max(0, v.treeScroll-12)
				} else if e.Y > n.listBounds.Y+n.listBounds.Height-24 {
					v.treeScroll += 12
				}
				return ui.EventResult{Handled: true, Repaint: true}
			}
		}
	case ui.PointerUp:
		if n.dragID == "" || !n.dragging {
			n.dragID = ""
			return ui.EventResult{}
		}
		s.noteDropAt(e.X, e.Y)
		id, folder, d := n.dragID, n.dragFolder, v.drop
		n.dragID = ""
		n.dragging = false
		if d.valid {
			s.moveNote(id, d.parent, d.before, folder)
			s.noteOpenAncestors(d.parent)
		}
		return ui.EventResult{Handled: true, Repaint: true}
	case ui.PointerExit:
		n.dragID = ""
		n.dragging = false
	}
	return ui.EventResult{}
}
func (s *State) noteDropAt(x, y float32) {
	n, v := &s.notes, &s.notes.view
	d := noteDrop{folder: n.dragFolder}
	for _, row := range n.rows {
		if row.id == "" || !pointInRect(x, y, row.bounds) {
			continue
		}
		d.bounds = row.bounds
		d.valid = row.id != n.dragID
		if row.folder && (!n.dragFolder || y > row.bounds.Y+7 && y < row.bounds.Y+row.bounds.Height-7) {
			d.parent = row.id
			d.inside = true
		} else {
			d.before = row.id
			if row.folder {
				f, _ := s.noteFolder(row.id)
				d.parent = f.Parent
			} else if at := s.noteIndex(row.id); at >= 0 {
				d.parent = n.list[at].Folder
			}
			if y > row.bounds.Y+row.bounds.Height/2 {
				d.before = s.noteNextSibling(row.id, row.folder)
				d.bounds.Y += row.bounds.Height
			}
		}
		if n.dragFolder && (d.parent == n.dragID || s.noteFolderWithin(d.parent, n.dragID)) {
			d.valid = false
		}
		v.drop = d
		return
	}
	if pointInRect(x, y, n.listBounds) {
		d.valid = true
		d.bounds = ui.Rect{X: n.listBounds.X + 6, Y: y, Width: n.listBounds.Width - 12, Height: 2}
	}
	v.drop = d
}
func (s *State) noteNextSibling(id string, folder bool) string {
	best := ""
	order := int(^uint(0) >> 1)
	if folder {
		f, ok := s.noteFolder(id)
		if !ok {
			return ""
		}
		for _, v := range s.notes.folders {
			if v.Parent == f.Parent && v.Order > f.Order && v.Order < order {
				order = v.Order
				best = v.ID
			}
		}
	} else if at := s.noteIndex(id); at >= 0 {
		n := s.notes.list[at]
		for _, v := range s.notes.list {
			if v.Folder == n.Folder && v.Order > n.Order && v.Order < order {
				order = v.Order
				best = v.ID
			}
		}
	}
	return best
}
func (s *State) notesScroll(e ui.ScrollEvent) ui.EventResult {
	if s.noteSidebarVisible() {
		return s.noteSidebarScroll(e)
	}
	if !s.notesVisible() {
		return ui.EventResult{}
	}
	n, v := s.noteContent(), &s.noteContent().view
	if n.confirm != "" {
		return ui.EventResult{Handled: true}
	}
	if v.menu != "" {
		if r, ok := s.menuWheel(e); ok {
			return r
		}
		return ui.EventResult{Handled: true}
	}
	if v.popup == "tags" || v.popup == "hosts" {
		v.popupBar.activity = true
		v.tagPopupScroll = min(max(0, v.tagPopupScroll+e.DeltaY), v.tagPopupArea.Geometry().MaxOffsetY)
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if pointInRect(e.X, e.Y, n.listBounds) {
		v.treeBar.activity = true
		maximum := v.listArea.Geometry().MaxOffsetY
		v.treeScroll = min(max(0, v.treeScroll+e.DeltaY), maximum)
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if pointInRect(e.X, e.Y, n.sourceBounds) {
		n.code.ScrollBy(e.DeltaX, e.DeltaY)
		s.noteScrollDriver(false)
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if pointInRect(e.X, e.Y, n.previewBounds) {
		v.previewBar.activity = true
		s.noteScrollDriver(true)
		n.previewScroll = min(max(0, n.previewScroll+e.DeltaY), max(0, n.previewHeight-n.previewBounds.Height))
		return ui.EventResult{Handled: true, Repaint: true}
	}
	return ui.EventResult{}
}
func (s *State) notesKey(e ui.KeyEvent) ui.EventResult {
	if result := s.noteEditingKey(e); result.Handled {
		return result
	}
	if s.noteSidebarVisible() {
		return s.noteSidebarKey(e)
	}
	if !s.notesVisible() {
		return ui.EventResult{}
	}
	v := &s.noteContent().view
	if e.Key == ui.KeyEscape {
		if v.menu != "" || v.popup != "" {
			v.menu = ""
			v.popup = ""
			v.resetPopupScrollbar()
			return ui.EventResult{Handled: true, Repaint: true, Focus: v.returnFocus}
		}
		if v.inlineID != "" || v.inlineNew {
			v.inlineID = ""
			v.inlineNew = false
			return ui.EventResult{Handled: true, Repaint: true}
		}
		if v.drawer {
			v.drawer = false
			return ui.EventResult{Handled: true, Repaint: true}
		}
	}
	if v.menu != "" {
		if r, ok := s.menuKey(e); ok {
			return r
		}
		if e.Key == ui.KeyEnter || e.Key == ui.KeySpace || e.Key == ui.KeyTab {
			return ui.EventResult{}
		}
		return ui.EventResult{Handled: true}
	}
	if v.popup != "" {
		if e.Target == hitNotePicker && e.Key == ui.KeyEnter && v.popup == "tags" {
			if r, ok := v.picker.Composition(); !ok || r.Start == r.End {
				s.noteViewClick(hitNoteTagSubmit)
			}
			return ui.EventResult{Handled: true, Repaint: true}
		}
		if e.Key == ui.KeyTab || e.Key == ui.KeyEnter || e.Key == ui.KeySpace {
			return ui.EventResult{}
		}
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if e.Target == hitNotePreviewText && e.Key == ui.KeyEscape {
		v.clearPreviewSelection()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if e.Target == hitNoteInline {
		if e.Key == ui.KeyTab {
			ok := s.commitNoteInline()
			result := ui.EventResult{Handled: true, Repaint: true, ContinueTraversal: ok}
			if !ok {
				result.Focus = hitNoteInline
			}
			return result
		}
		if e.Key == ui.KeyEnter && !e.Repeat {
			s.commitNoteInline()
			return ui.EventResult{Handled: true, Repaint: true}
		}
		return ui.EventResult{}
	}
	if e.Target == hitNoteDivider {
		if ratio, ok := s.noteResizable(nil).FractionForKey(e.Key); ok {
			v.width = ratio * max(1, v.workspace.Width-1)
			s.save()
			return ui.EventResult{Handled: true, Repaint: true}
		}
	}
	if !e.Release && !e.Repeat && (e.Key == ui.KeyEnter || e.Key == ui.KeySpace) {
		if _, ok := e.Target.In(hitNoteCodeCopies); ok {
			s.clickNoteCodeCopy(e.Target, s.noteContent())
			return ui.EventResult{Handled: true, Repaint: true}
		}
	}
	if e.Key == ui.KeyEnter {
		if _, ok := e.Target.In(hitNoteRows); ok {
			s.Click(e.Target)
			return ui.EventResult{Handled: true, Repaint: true}
		}
	}
	return ui.EventResult{}
}
