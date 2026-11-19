package workbench

import (
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	layoutcore "motty/internal/workspace"
)

var (
	hitNoteSideOpen    = tree.ID(notesHits, "side-open")
	hitNoteSideClose   = tree.ID(notesHits, "side-close")
	hitNoteSideLinked  = tree.ID(notesHits, "side-linked")
	hitNoteSideExpand  = tree.ID(notesHits, "side-expand-all")
	hitNoteSideRowOpen = tree.Sub(notesHits, tree.Name("side-row-open"))
)

// The browser owns its presentation state; it never selects an editing draft.
type noteSidebar struct {
	open                         bool
	inputVersion                 uint64
	observedQuery                string
	queryChanged                 time.Duration
	queryDirty                   bool
	content                      notesUI
	width, fraction              float32
	lastQuery                    string
	bounds                       gfx.Rect
	widthDragging, splitDragging bool
}

func (s *State) noteSidebarVisible() bool {
	return s.noteTarget == nil && s.modal == modalNone && s.activeSection == sectionTerminal && s.notesPanelOpen()
}
func (s *State) noteSidebarState() *noteSidebar {
	return s.noteSidebarFor(s.deck.ActiveWorkspace())
}

func (s *State) noteSidebarFor(id layoutcore.WorkspaceID) *noteSidebar {
	if s.noteSidebars == nil {
		s.noteSidebars = make(map[layoutcore.WorkspaceID]*noteSidebar)
	}
	if s.noteSidebars[id] == nil {
		s.noteSidebars[id] = &noteSidebar{width: 400, fraction: .5, content: notesUI{noteDraft: &noteDraft{}}}
		s.noteSidebars[id].content.view.expanded = map[string]bool{}
	}
	return s.noteSidebars[id]
}

func (s *State) notesPanelOpen() bool { return s.notesPanelOpenFor(s.deck.ActiveWorkspace()) }
func (s *State) notesPanelOpenFor(id layoutcore.WorkspaceID) bool {
	v := s.noteSidebars[id]
	return v != nil && v.open
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
	v.inputVersion++
	v.widthDragging, v.splitDragging = false, false
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
	s.syncNoteSidebarContent(&s.noteSidebarState().content)
}
func (s *State) syncNoteSidebarContent(n *notesUI) {
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
func (s *State) clickNoteSidebar(id gfx.ID) bool {
	if id == hitNoteSideClose {
		if s.notesPanelOpen() {
			s.clickNotes(hitNotesPanel)
		}
		return true
	}
	defer func() {
		if id == hitNoteSideExpand || id == hitNoteSideLinked {
			s.save()
			return
		}
		if _, ok := id.In(hitNoteDisclosures); ok {
			s.save()
			return
		}
		if _, ok := id.In(hitNoteFolderRows); ok {
			s.save()
			return
		}
		if _, ok := id.In(hitNoteRows); ok {
			s.save()
		}
	}()
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
	if e.Kind == gfx.PointerDown && (e.Button == gfx.PointerButtonPrimary || e.Button == gfx.PointerButtonNone) && e.ClickCount == 2 {
		rows := n.view.treeScratch.rows
		first, last := n.page.tree.Visible()
		for i := max(first, 0); i <= last && i < len(rows); i++ {
			if n.page.tree.RowID(i) == e.Target && !rows[i].folder && rows[i].id != "" {
				s.openNoteWindow(rows[i].id)
				return gfx.EventResult{Handled: true, Repaint: true}
			}
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
	// The tree and the preview are the page's scroll areas: WindowConfig gives
	// the wheel to them before it reaches here.
	return gfx.EventResult{}
}
func (s *State) noteSidebarKey(e gfx.KeyEvent) gfx.EventResult {
	v := s.noteSidebarState()
	n := &v.content
	p := &n.page
	if result := p.panel.Key(e); result.Handled {
		s.noteSidebarAfterLayout(n)
		s.save()
		return result
	}
	if result := p.side.Key(e); result.Handled {
		s.noteSidebarAfterLayout(n)
		s.save()
		return result
	}
	if e.Key == gfx.KeyEscape && e.Target == hitNotePreviewText {
		n.view.clearPreviewSelection()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}
