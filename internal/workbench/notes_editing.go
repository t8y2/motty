package workbench

import (
	"fmt"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/text/find"
)

var (
	hitNoteFind         = tree.ID(notesHits, "find-in-note")
	hitNoteFindNext     = tree.ID(notesHits, "find-next")
	hitNoteFindPrevious = tree.ID(notesHits, "find-previous")
	hitNoteFindClose    = tree.ID(notesHits, "find-close")
)

const noteFontSize float32 = 14

type noteEditing struct {
	find      find.Session
	findOpen  bool
	findFocus bool
}

type notePreviewLayout struct {
	ready         bool
	source        string
	width, height float32
	face          gfx.MonoFace
}

func (s *State) configureNoteEditor(e *text.Editor) {
	e.SetCode(text.CodeOptions{TabWidth: 4, AutoIndent: true, AutoClose: true})
}

func (s *State) stepNoteFind(n *notesUI, forward bool) {
	if !s.activateNoteView(n) || n.composing() {
		return
	}
	n.editing.find.Update(n.body.String())
	if n.editing.find.Step(&n.body, forward) {
		n.code.Reveal(n.body.ByteOffset(n.body.Caret()))
		n.captureNoteSelection()
	}
}

func (s *State) openNoteFind(n *notesUI) {
	if n.selected == "" || !s.activateNoteView(n) || n.composing() {
		return
	}
	if n.view.mode == notePreview {
		n.view.mode = noteSource
	}
	r := n.body.Selection()
	if r.Start != r.End && n.editing.find.Query.RuneCount() == 0 {
		value := []rune(n.body.String())
		n.editing.find.Query = *text.NewEditor(string(value[min(r.Start, r.End):max(r.Start, r.End)]))
	}
	n.editing.find.Query.SetSelection(text.Range{End: n.editing.find.Query.RuneCount()})
	n.editing.findOpen, n.editing.findFocus = true, true
	n.editing.find.Update(n.body.String())
}

func (s *State) clickNoteFindActions(id gfx.ID) bool {
	n := s.noteContent()
	switch id {
	case hitNoteFindNext, hitNoteFindPrevious:
		s.stepNoteFind(n, id == hitNoteFindNext)
	case hitNoteFindClose:
		n.editing.findOpen, n.editing.findFocus = false, false
		n.view.focus = hitNoteBody
	default:
		return false
	}
	return true
}

// Run before native text shortcuts, and from the ordinary key handler for Tab
// and visual-row navigation. Commands act on the shared draft's active view.
func (s *State) noteEditingKey(e gfx.KeyEvent) gfx.EventResult {
	n := s.noteContent()
	if !s.notesVisible() || s.noteSidebarVisible() || n.selected == "" || n.view.menu != "" || n.view.popup != "" || n.confirm != "" || e.Release {
		return gfx.EventResult{}
	}
	inFind := e.Target == hitNoteFind && n.editing.findOpen
	command, hasCommand := components.FindBarCommandForKey(e, inFind)
	toggle := hasCommand && command == components.FindBarOpen && tree.Within(e.Target.Scope(), notesHits)
	findControl := n.editing.findOpen && (inFind || e.Target == hitNoteFindNext || e.Target == hitNoteFindPrevious || e.Target == hitNoteFindClose)
	if e.Target != hitNoteBody && e.Target != hitNotePreviewText && !findControl && !toggle {
		return gfx.EventResult{}
	}
	handled := gfx.EventResult{Handled: true, Repaint: true}
	if e.Key == gfx.KeyEscape && n.editing.findOpen {
		n.editing.findOpen, n.editing.findFocus = false, false
		handled.Focus = hitNoteBody
		return handled
	}
	if hasCommand {
		switch command {
		case components.FindBarOpen:
			if e.Repeat {
				return handled
			}
			if n.editing.findOpen {
				n.editing.findOpen, n.editing.findFocus = false, false
				handled.Focus = hitNoteBody
			} else {
				s.openNoteFind(n)
			}
		case components.FindBarNext, components.FindBarPrevious:
			if !n.editing.findOpen {
				return gfx.EventResult{}
			}
			s.stepNoteFind(n, command == components.FindBarNext)
		case components.FindBarCloseCommand:
			if !n.editing.findOpen {
				return gfx.EventResult{}
			}
			n.editing.findOpen, n.editing.findFocus = false, false
			handled.Focus = hitNoteBody
		default:
			return gfx.EventResult{}
		}
		return handled
	}
	if e.Target != hitNoteBody || !s.activateNoteView(n) || n.composing() {
		return gfx.EventResult{}
	}
	s.configureNoteEditor(&n.body)
	if command, ok := components.CodeEditorCommandForKey(e); ok {
		if !command.Apply(&n.body) {
			return gfx.EventResult{}
		}
		n.code.Reveal(n.body.ByteOffset(n.body.Caret()))
		n.captureNoteSelection()
		s.invalidateNoteViews()
		return handled
	}
	if movement, extend := components.TextareaMovementForKey(e.Key, e.Modifiers); movement != components.TextareaMoveNone {
		value := n.body.String()
		offset := n.code.Move(value, n.body.ByteOffset(n.body.Caret()), movement)
		n.body.MoveTo(utf8.RuneCountInString(value[:offset]), extend)
		n.captureNoteSelection()
		return handled
	}
	return gfx.EventResult{}
}

func (s *State) paintNoteFind(c *gfx.Canvas, n *notesUI, b gfx.Rect) gfx.Rect {
	if !n.editing.findOpen {
		return b
	}
	f := &n.editing.find
	f.Update(n.body.String())
	current, total := f.Position(&n.body)
	status := ""
	if !f.Empty() {
		status = fmt.Sprintf("%d / %d", current, total)
	}
	height := components.FindBarHeight(s.Theme(), false)
	components.PaintFindBar(c, s.Theme(), gfx.Rect{X: b.X, Y: b.Y, Width: b.Width, Height: height}, components.FindBarSnapshot{
		Find:       editorField(c, hitNoteFind, s.noteText("Find in note", "在当前笔记中查找"), s.noteText("Find in note", "在当前笔记中查找"), &f.Query, false),
		PreviousID: hitNoteFindPrevious, NextID: hitNoteFindNext, CloseID: hitNoteFindClose,
		Status: status, NoResults: !f.Empty() && total == 0, Disabled: total == 0,
		Labels: components.FindBarLabels{Previous: s.noteText("Previous match", "上一个匹配"), Next: s.noteText("Next match", "下一个匹配"), Close: s.noteText("Close find", "关闭查找")},
	})
	if n.editing.findFocus {
		c.RequestFocus(hitNoteFind)
		n.editing.findFocus = false
	}
	b.Y += height + 6
	b.Height = max(0, b.Height-height-6)
	return b
}
