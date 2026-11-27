package workbench

import (
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

var (
	hitNoteSourceScroll = tree.ID(notesHits, "source-scroll")
	hitNoteHostsScroll  = tree.ID(notesHits, "hosts-scroll")
	hitNoteListScroll   = tree.ID(notesHits, "list-scroll")
	hitNoteLinks        = tree.Sub(notesHits, tree.Name("link"))
)

func (s *State) notesPanelWidth(size gfx.Size) float32 {
	return s.notesPanelWidthFor(s.deck.ActiveWorkspace(), size)
}
func (s *State) notesPanelWidthFor(id layoutcore.WorkspaceID, size gfx.Size) float32 {
	base := s.baseWorkspacePixelBounds(size)
	if context, ok := s.deck.Context(id); ok && context.Open && size.Width >= contextDrawerAt {
		base.Width = max(0, base.Width-s.contextPanelWidth(base, context)-1)
	}
	base.Width -= s.aiReserve(id, size) + s.independentMonitorReserve(id, size)
	width := float32(400)
	if v := s.noteSidebars[id]; v != nil {
		width = v.width
	}
	maximum := max(0, base.Width*.7)
	return min(max(width, min(280, maximum)), maximum)
}
func (s *State) notesVisible() bool {
	if s.noteTarget != nil && s.noteTarget.windowView {
		return true
	}
	return s.modal == modalNone && (s.activeSection == sectionNotes || s.activeSection == sectionTerminal && s.notesPanelOpen())
}
func (s *State) notesPointer(e gfx.PointerEvent) (result gfx.EventResult) {
	if s.noteSidebarVisible() {
		return s.noteSidebarPointer(e)
	}
	s.noteContent().view.pointer = gfx.Point{X: e.X, Y: e.Y}
	s.noteContent().view.pointerValid = e.Kind != gfx.PointerExit
	if pointInRect(e.X, e.Y, s.noteContent().previewBounds) || e.Kind == gfx.PointerExit {
		result.Repaint = true
		// A press held on a control (a splitter being dragged across the
		// preview) keeps receiving its moves; only hover is swallowed.
		if s.noteLibraryView() && (e.Kind == gfx.PointerMove && !e.Captured || e.Kind == gfx.PointerExit) {
			result.Handled = true
		}
	}
	n, v := s.noteContent(), &s.noteContent().view
	if e.Kind == gfx.PointerDown && (e.Target == hitNoteTitle || e.Target == hitNoteBody) && !s.activateNoteView(n) {
		return gfx.EventResult{Handled: true}
	}
	if !s.notesVisible() {
		v.previewSelect.EndDrag()
		return gfx.EventResult{}
	}
	if n.confirm != "" || v.menu != "" || v.popup != "" {
		return gfx.EventResult{}
	}
	// A secondary press on a tree row opens its actions, as the hand-painted
	// tree did, anchored under the row's action button.
	if e.Kind == gfx.PointerDown && e.Button == gfx.PointerButtonSecondary {
		rows := n.view.treeScratch.rows
		first, last := n.page.tree.Visible()
		for i := max(first, 0); i <= last && i < len(rows); i++ {
			if n.page.tree.RowID(i) != e.Target || rows[i].id == "" {
				continue
			}
			row := rows[i]
			v.menuFolder = row.folder
			v.menuID = row.id
			if row.folder {
				v.menu = "folder"
				v.overlayFrom = hitNoteFolderMore.At(n.view.folderTokens[row.id])
				n.folder = row.id
				v.treeFolderSelection = true
				s.save()
			} else {
				v.menu = "note"
				v.overlayFrom = hitNoteRowMore.At(n.view.noteTokens[row.id])
				s.selectNote(row.id)
			}
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	if r := s.notePreviewSelectionPointer(e); r.Handled {
		return r
	}
	if e.Kind == gfx.PointerDown && (v.inlineID != "" || v.inlineNew) && e.Target != hitNoteInline {
		if !s.commitNoteInline() {
			return gfx.EventResult{Handled: true, Focus: hitNoteInline, Repaint: true}
		}
	}
	if e.Kind == gfx.PointerDown && e.Button == gfx.PointerButtonPrimary && e.Target == hitNoteListScroll {
		n.folder = ""
		v.treeFolderSelection = true
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if r := n.code.Pointer(e); r.Handled {
		s.noteScrollDriver(false)
		return r
	}
	if e.Kind == gfx.PointerDown {
		switch e.Target {
		case hitNoteTitle:
			n.body.Unmark()
			n.tags.Unmark()
		case hitNoteBody:
			n.title.Unmark()
			n.tags.Unmark()
		}
	}
	return result
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
func (s *State) notesScroll(e gfx.ScrollEvent) gfx.EventResult {
	if s.noteSidebarVisible() {
		return s.noteSidebarScroll(e)
	}
	if !s.notesVisible() {
		return gfx.EventResult{}
	}
	n, v := s.noteContent(), &s.noteContent().view
	if n.confirm != "" {
		return gfx.EventResult{Handled: true}
	}
	if v.menu != "" {
		if r, ok := s.menuWheel(e); ok {
			return r
		}
		return gfx.EventResult{Handled: true}
	}
	if pointInRect(e.X, e.Y, n.sourceBounds) {
		dx, dy := e.DeltaX, e.DeltaY
		if e.Pages != 0 {
			dx, dy = e.StepXIn(1, n.sourceBounds.Width), e.StepYIn(1, n.sourceBounds.Height)
		}
		n.code.ScrollBy(dx, dy)
		s.noteScrollDriver(false)
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if pointInRect(e.X, e.Y, n.previewBounds) {
		s.noteScrollDriver(true)
		step := e.DeltaY
		if e.Pages != 0 {
			step = e.StepYIn(1, n.previewBounds.Height)
		}
		n.setPreviewScroll(min(max(0, n.previewScroll+step), max(0, n.previewHeight-n.previewBounds.Height)))
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}
func (s *State) notesKey(e gfx.KeyEvent) gfx.EventResult {
	if result := s.noteEditingKey(e); result.Handled {
		return result
	}
	if s.noteSidebarVisible() {
		return s.noteSidebarKey(e)
	}
	if !s.notesVisible() {
		return gfx.EventResult{}
	}
	if result := s.noteContent().page.library.Key(e); result.Handled {
		s.noteLibraryAfterLayout(s.noteContent())
		s.save()
		return result
	}
	v := &s.noteContent().view
	if e.Key == gfx.KeyEscape {
		if v.menu != "" || v.popup != "" {
			v.menu = ""
			v.popup = ""
			return gfx.EventResult{Handled: true, Repaint: true, Focus: v.returnFocus}
		}
		if v.inlineID != "" || v.inlineNew {
			v.inlineID = ""
			v.inlineNew = false
			if n := s.noteContent(); n.error == s.noteText("Name cannot be empty.", "名称不能为空。") {
				n.error = ""
			}
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		if v.drawer {
			v.drawer = false
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	if v.menu != "" {
		if r, ok := s.menuKey(e); ok {
			return r
		}
		if e.Key == gfx.KeyEnter || e.Key == gfx.KeySpace || e.Key == gfx.KeyTab {
			return gfx.EventResult{}
		}
		return gfx.EventResult{Handled: true}
	}
	if v.popup != "" {
		if e.Target == hitNotePicker && e.Key == gfx.KeyEnter && v.popup == "tags" {
			if r, ok := v.picker.Composition(); !ok || r.Start == r.End {
				s.noteViewClick(hitNoteTagSubmit)
			}
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		if e.Key == gfx.KeyTab || e.Key == gfx.KeyEnter || e.Key == gfx.KeySpace {
			return gfx.EventResult{}
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if e.Target == hitNotePreviewText && e.Key == gfx.KeyEscape {
		v.clearPreviewSelection()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if e.Target == hitNoteInline {
		if e.Key == gfx.KeyTab {
			ok := s.commitNoteInline()
			result := gfx.EventResult{Handled: true, Repaint: true, ContinueTraversal: ok}
			if !ok {
				result.Focus = hitNoteInline
			}
			return result
		}
		if e.Key == gfx.KeyEnter && !e.Repeat {
			s.commitNoteInline()
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return gfx.EventResult{}
	}
	if !e.Release && !e.Repeat && (e.Key == gfx.KeyEnter || e.Key == gfx.KeySpace) {
		if _, ok := e.Target.In(hitNoteCodeCopies); ok {
			s.clickNoteCodeCopy(e.Target, s.noteContent())
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	return gfx.EventResult{}
}
