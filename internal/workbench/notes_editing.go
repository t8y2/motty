package workbench

import (
	"fmt"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/text/find"
	"github.com/vivaui/viva/ui"
	"motty/internal/workbench/icons"
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
	// main and bold are the weights the face is drawn at: the same source wraps
	// differently under another weight, so a layout made at one is not kept for
	// the other.
	main, bold int
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

// declareNoteFindBar declares the find bar over the note's source: its query
// field and the three actions. The bar is a control of the page, so its keys
// and text reach the application through the page's routing.
func (s *State) declareNoteFindBar(c ui.C, n *notesUI) {
	p := &n.page
	f := &n.editing.find
	f.Update(n.body.String())
	current, total := f.Position(&n.body)
	status := ""
	if !f.Empty() {
		status = fmt.Sprintf("%d / %d", current, total)
	}
	height := components.FindBarHeight(s.Theme(), false)
	ui.Row(c).Height(height).FillWidth().MarginSides(0, 16, 6, 16).Gap(6).Items(ui.Center).Children(func() {
		p.find.Bind(&f.Query)
		field := ui.TextFieldID(c, hitNoteFind, &p.find, s.noteText("Find in note", "在当前笔记中查找"), s.noteText("Find in note", "在当前笔记中查找")).
			Height(height - 6).Grow(1)
		if field.Submitted() {
			s.stepNoteFind(n, true)
		}
		ui.Button(c, s.noteText("Previous match", "上一个匹配")).Icon(icons.ChevronUp).IconOnly().Ghost().Size(28, 28).
			Hit(hitNoteFindPrevious, s.noteText("Previous match", "上一个匹配")).Disabled(total == 0)
		ui.Button(c, s.noteText("Next match", "下一个匹配")).Icon(icons.ChevronDown).IconOnly().Ghost().Size(28, 28).
			Hit(hitNoteFindNext, s.noteText("Next match", "下一个匹配")).Disabled(total == 0)
		ui.Button(c, s.noteText("Close find", "关闭查找")).Icon(lucideIcons[iconClose]).IconOnly().Ghost().Size(28, 28).
			Hit(hitNoteFindClose, s.noteText("Close find", "关闭查找"))
		if status != "" {
			ui.Text(c, status).FontSize(11).Color(s.pal.muted)
		}
	})
	if n.editing.findFocus {
		c.Canvas().RequestFocus(hitNoteFind)
		n.editing.findFocus = false
	}
}
