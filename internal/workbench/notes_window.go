package workbench

import (
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// Note tabs share navigation with files and content with the note library.
type noteWindow struct {
	id            string
	missing       bool
	content       notesUI
	source, title string
	input         app.TextInput
}

func (s *State) openNoteWindow(id string) {
	if s.options.OpenContentWindow == nil || s.noteIndex(id) < 0 {
		return
	}
	m := &s.fileWindows
	key := "note:" + id
	if d := m.documents[key]; d != nil {
		m.activate(key)
		return
	}
	if len(m.documents) >= 8 {
		s.fileOpenNotice(nil, s.messages.FileWindow.CannotOpen, s.messages.FileWindow.TooMany, components.ToastWarning)
		return
	}
	if m.documents == nil {
		m.documents = make(map[string]*fileDocument)
	}
	d := &fileDocument{owner: s, cacheKey: key, ready: true, local: true, note: &noteWindow{id: id}}
	d.syncNoteWindow()
	m.documents[key], m.pending = d, key
	if err := m.present(d); err != nil {
		m.forgetDocument(d)
		s.fileOpenNotice(nil, s.messages.FileWindow.CannotOpen, err.Error(), components.ToastError)
	}
}

func (d *fileDocument) syncNoteWindow() bool {
	v, s := d.note, d.owner
	n := &v.content
	missing := s.noteIndex(v.id) < 0
	if n.noteDraft == nil {
		n.noteDraft = s.noteBuffer(v.id)
		n.selected = v.id
		n.windowView = true
		n.view.mode = s.noteWindowMode()
	}
	if v.missing && !missing {
		n.noteDraft = s.noteBuffer(v.id)
		n.selected = v.id
		n.selection = noteEditorSelection{}
	}
	if !missing && !n.changed() && !n.composing() {
		incoming := s.notes.list[s.noteIndex(v.id)]
		if string(mustNoteJSON(incoming)) != string(mustNoteJSON(n.draft)) {
			applyNoteIncoming(n.noteDraft, incoming)
		}
	}
	source, title := n.body.String(), n.title.String()
	changed := v.missing != missing || v.source != source || v.title != title
	if missing && !v.missing || v.source != source {
		if n.imageCancel != nil {
			n.imageCancel()
			n.imageCancel = nil
		}
		n.imageToken++
		n.images, n.imageSource = nil, ""
		n.view.clearPreviewSelection()
		n.error = ""
	}
	if missing {
		if !v.missing {
			n.noteDraft = &noteDraft{}
			n.selected = ""
			delete(s.notes.drafts, v.id)
		}
	} else {
		d.name = title
		if strings.TrimSpace(title) == "" {
			d.name = s.noteText("Untitled", "无标题")
		}
	}
	v.missing, v.source, v.title = missing, source, title
	return changed
}

func (s *State) syncNoteWindows() {
	changed := false
	for _, d := range s.fileWindows.documents {
		if d.note != nil && !d.closed && d.syncNoteWindow() {
			changed = true
		}
	}
	if changed && s.fileWindows.window != nil {
		s.fileWindows.window.Invalidate()
	}
}

func (d *fileDocument) paintNoteWindow(c *gfx.Canvas, size gfx.Size) {
	d.syncNoteWindow()
	s, n := d.owner, &d.note.content
	n.viewport = gfx.Rect{Width: size.Width, Height: size.Height}
	d.ui.size, d.ui.scale = size, c.PixelScale()
	c.SetTree(&tree)
	if id, ok := c.ClickTarget(); ok {
		c.AfterPaint(func() { d.click(id) })
	}
	c.Fill(n.viewport, s.Theme().Background)
	d.windowPage().Draw(c, n.viewport, d.declareNoteWindow)
	s.fileWindows.paintTabsScrollbar(c)
	if _, formatting := c.Hovered().In(hitNoteFormats); !formatting {
		d.ui.tooltip.Paint(c, s.Theme(), n.viewport, components.TooltipOptions{Layer: tooltipLayer, SideOffset: tooltipGap}, components.HoverTooltipDelay)
	}
}

// declareNoteWindow declares the note window's share of the page: the tabs it
// shares with files and the room the note takes.
func (d *fileDocument) declareNoteWindow(c ui.C) {
	s := d.owner
	theme := s.Theme()
	ui.Column(c).Fill().Children(func() {
		declareWindowTitlebar(c, s.pal, d.name, d.source)
		ui.Column(c).Grow(1).Shrink(1).Pad(12).Gap(8).Children(func() {
			s.fileWindows.declareTabsSection(c, c.Bounds().Width)
			ui.Divider(c).Background(theme.Border)
			d.ui.contentBox = ui.Slot(c).Grow(1).Children(func() {
				d.declareNoteWindowContent(c, s, &d.note.content)
			})
		})
	})
}

// declareNoteWindowContent declares the note itself in its window's share of
// the page: the formatting bar, the document and the footer that names it.
func (d *fileDocument) declareNoteWindowContent(c ui.C, s *State, n *notesUI) {
	s.withNoteView(n, func() {
		if d.note.missing {
			ui.Text(c, s.noteText("Note deleted", "笔记已删除")).FontSize(14).Color(s.pal.muted)
			return
		}
		page := s.ensureNotePage(n, noteSurfaceWindow)
		page.c = c
		n.sourceBounds, n.previewBounds = gfx.Rect{}, gfx.Rect{}
		width := c.Bounds().Width - 24
		ui.Column(c).Fill().Children(func() {
			s.declareNoteWindowBody(c, n, width)
		})
		s.declareNoteOverlays(c, n)
		s.declareNoteExit(c)
		if !n.view.focus.IsZero() {
			c.Canvas().RequestFocus(n.view.focus)
			n.view.focus = gfx.ID{}
		}
		s.autosaveNotes(c.Canvas())
	})
}

// declareNoteWindowBody declares the note in its window: the formatting bar at
// the top, the document, and the footer that carries the title, the actions and
// the mode switch.
func (s *State) declareNoteWindowBody(c ui.C, n *notesUI, width float32) {
	v := &n.view
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
		s.declareNoteFormatting(c, n, width, true)
	}
	if message := n.error; message != "" || n.problem != "" {
		if n.problem != "" {
			message = n.problem
		}
		ui.Text(c, message).FontSize(12).Color(s.pal.danger)
	}
	s.declareNoteLinkNotice(c, n, width, 0, 0)
	body := ui.Row(c).Grow(1).FillWidth().MarginSides(8, 0, 8, 0)
	body.Children(func() {
		switch mode {
		case noteSource:
			ui.Slot(c).Grow(1).FillHeight().Draw(n.page.drawSource)
		case notePreview:
			s.declareNotePreview(c, n, width, true)
		default:
			pane := (width - 16) / 2
			ui.Column(c).Grow(1).Basis(0).FillHeight().Children(func() {
				ui.Slot(c).Grow(1).FillWidth().Draw(n.page.drawSource)
			})
			ui.Slot(c).Width(16)
			ui.Column(c).Grow(1).Basis(0).FillHeight().Children(func() {
				s.declareNotePreview(c, n, pane, true)
			})
		}
	})
	ui.Divider(c).Height(1).FillWidth().Background(s.Theme().Border)
	footer := ui.Column(c).FillWidth().MarginSides(11, 0, 0, 0)
	footer.Children(func() { s.declareNoteEditorHeader(c, n, width, true) })
}

func (d *fileDocument) clickNoteWindow(id gfx.ID) {
	d.syncNoteWindow()
	s, n := d.owner, &d.note.content
	if d.closed || d.note.missing || s.fileWindows.activeDocument() != d {
		return
	}
	s.withNoteView(n, func() {
		if s.clickNoteCodeCopy(id, n) {
			return
		}
		if s.noteViewClick(id) {
			return
		}
		switch id {
		case hitNotePin:
			if !n.composing() && s.flushNoteBuffer(n.selected, n.noteDraft) {
				if i := s.noteIndex(n.selected); i >= 0 {
					n.draft.Pinned = !n.draft.Pinned
					s.notes.list[i] = n.draft
					s.save()
				}
			}
		case hitNoteLoadImages:
			s.loadNoteImagesTarget(n, func(owner *State) *notesUI {
				if d.closed || owner.fileWindows.documents[d.cacheKey] != d {
					return nil
				}
				d.syncNoteWindow()
				if d.window != nil {
					d.window.Invalidate()
				}
				return &d.note.content
			}, s.options.Invalidate)
		default:
			if token, ok := id.In(hitNoteHostRows); ok {
				if catalogID, exists := n.view.tokenTags[token]; exists {
					s.toggleNoteHost(catalogID)
				}
			} else if i, ok := id.In(hitNoteLinks); ok && s.options.OpenURL != nil {
				if url := n.rendered.Link(i); url != "" {
					if err := s.options.OpenURL(url); err != nil {
						n.error = err.Error()
					}
				}
			}
		}
	})
	n.captureNoteSelection()
	s.invalidateNoteViews()
}

func (d *fileDocument) noteWindowPointer(e gfx.PointerEvent) (result gfx.EventResult) {
	d.syncNoteWindow()
	if d.closed || d.note.missing {
		return
	}
	n := &d.note.content
	if e.Kind == gfx.PointerDown && (e.Target == hitNoteBody || e.Target == hitNoteTitle) && !d.owner.activateNoteView(n) {
		return gfx.EventResult{Handled: true}
	}
	d.owner.withNoteView(n, func() { result = d.owner.notesPointer(e) })
	n.captureNoteSelection()
	return
}
func (d *fileDocument) noteWindowScroll(e gfx.ScrollEvent) (result gfx.EventResult) {
	if d.closed || d.note.missing {
		return
	}
	d.owner.withNoteView(&d.note.content, func() { result = d.owner.notesScroll(e) })
	return
}
func (d *fileDocument) noteWindowKey(e gfx.KeyEvent) (result gfx.EventResult) {
	if d.closed || d.note.missing {
		return
	}
	d.owner.withNoteView(&d.note.content, func() { result = d.owner.notesKey(e) })
	return
}
func (d *fileDocument) noteWindowShortcut(e gfx.KeyEvent) gfx.EventResult {
	if d.closed || d.note.missing {
		return gfx.EventResult{Handled: true}
	}
	n := &d.note.content
	var editing gfx.EventResult
	d.owner.withNoteView(n, func() { editing = d.owner.noteEditingKey(e) })
	if editing.Handled {
		return editing
	}
	if e.Target == hitNotePreviewText {
		return d.owner.notePreviewShortcutContent(e, n)
	}
	if n.view.popup != "" && e.Target != hitNotePicker {
		return gfx.EventResult{Handled: true}
	}
	if e.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta) != 0 && !e.Release {
		switch strings.ToLower(e.Text) {
		case "w":
			d.owner.fileWindows.closeActiveTab()
			return gfx.EventResult{Handled: true, Repaint: true}
		case "s":
			d.owner.save()
			d.owner.invalidateNoteViews()
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	return gfx.EventResult{}
}
func (d *fileDocument) noteWindowTextInput(context app.TextInputContext) *app.TextInput {
	d.syncNoteWindow()
	if d.closed || d.note.missing {
		return nil
	}
	input := d.owner.noteViewInput(&d.note.content, context, &d.note.input)
	if input != nil {
		apply := input.OnEvent
		input.OnEvent = func(e app.TextEvent) (app.TextState, error) {
			if d.closed || d.note.missing || d.owner.fileWindows.activeDocument() != d {
				return app.TextState{}, nil
			}
			return apply(e)
		}
	}
	return input
}
