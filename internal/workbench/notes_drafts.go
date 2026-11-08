package workbench

import (
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
)

// Content and undo history belong to a note. Native windows own only views.
type noteDraft struct {
	title, body, tags text.Editor
	draft             savedNote
	conflictSuffix    string
	observed          string
	lastChange        time.Duration
	problem           string
	owner             *notesUI
}

type noteEditorSelection struct {
	body, title             text.Range
	bodySource, titleSource string
	ready                   bool
}

func (d *noteDraft) composing() bool {
	for _, e := range []*text.Editor{&d.title, &d.body, &d.tags} {
		if _, ok := e.Composition(); ok {
			return true
		}
	}
	return false
}
func (d *noteDraft) changed() bool {
	return d.title.String() != d.draft.Title || d.body.String() != d.draft.Body || !slices.Equal(noteWords(d.tags.String()), d.draft.Tags) || d.conflictSuffix != ""
}
func (s *State) noteDrafts() map[string]*noteDraft {
	if s.notes.drafts == nil {
		s.notes.drafts = map[string]*noteDraft{}
	}
	if s.notes.noteDraft != nil && s.notes.selected != "" && s.notes.draft.ID == s.notes.selected {
		s.notes.drafts[s.notes.selected] = s.notes.noteDraft
	}
	return s.notes.drafts
}
func (s *State) noteBuffer(id string) *noteDraft {
	drafts := s.noteDrafts()
	if s.noteIndex(id) < 0 {
		return &noteDraft{}
	}
	if d := drafts[id]; d != nil {
		return d
	}
	d := &noteDraft{draft: s.notes.list[s.noteIndex(id)]}
	d.title.Insert(d.draft.Title)
	d.body.Insert(d.draft.Body)
	d.tags.Insert(strings.Join(d.draft.Tags, ", "))
	drafts[id] = d
	return d
}

// Scope only synchronous UI dispatch. Callbacks and async jobs bind their own
// view explicitly; the library and save pipeline always use s.notes.
func (s *State) withNoteView(n *notesUI, run func()) {
	previous := s.noteTarget
	s.noteTarget = n
	defer func() { s.noteTarget = previous }()
	run()
}
func (s *State) noteContent() *notesUI {
	if s.noteTarget != nil {
		return s.noteTarget
	}
	return &s.notes
}
func (s *State) noteLibraryView() bool {
	return s.noteTarget != nil && s.noteTarget.windowView || s.activeSection == sectionNotes
}

// Move inactive selections with edits, retaining their direction and clamping
// positions in replaced text. This also handles undo and external refreshes.
func noteAdjustedRange(r text.Range, before, after string) text.Range {
	if before == after {
		return r
	}
	a, b := []rune(before), []rune(after)
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA--
		endB--
	}
	adjust := func(p int) int {
		if p > start {
			if p >= endA {
				p += endB - endA
			} else {
				p = endB
			}
		}
		return min(max(0, p), len(b))
	}
	return text.Range{Start: adjust(r.Start), End: adjust(r.End)}
}
func noteDirectedSelection(e *text.Editor) text.Range {
	selection := e.Selection()
	if e.Caret() == selection.Start {
		selection.Start, selection.End = selection.End, selection.Start
	}
	return selection
}
func (n *notesUI) captureNoteSelection() {
	if n.noteDraft == nil || n.owner != n {
		return
	}
	n.selection = noteEditorSelection{body: noteDirectedSelection(&n.body), title: noteDirectedSelection(&n.title), bodySource: n.body.String(), titleSource: n.title.String(), ready: true}
}
func (n *notesUI) noteSelections() (text.Range, text.Range) {
	if n.owner == n || !n.selection.ready {
		return noteDirectedSelection(&n.body), noteDirectedSelection(&n.title)
	}
	return noteAdjustedRange(n.selection.body, n.selection.bodySource, n.body.String()), noteAdjustedRange(n.selection.title, n.selection.titleSource, n.title.String())
}
func (s *State) activateNoteView(n *notesUI) bool {
	if n.noteDraft == nil || s.noteIndex(n.selected) < 0 {
		return false
	}
	if n.owner == n {
		return true
	}
	if n.composing() {
		return false
	}
	if n.owner != nil {
		n.owner.captureNoteSelection()
	}
	body, title := n.noteSelections()
	n.body.BreakUndoGroup()
	n.title.BreakUndoGroup()
	n.body.SetSelection(body)
	n.title.SetSelection(title)
	n.owner = n
	n.captureNoteSelection()
	return true
}
func (s *State) invalidateNoteViews() {
	s.syncNoteWindows()
	if s.options.Invalidate != nil {
		s.options.Invalidate()
	}
	if w := s.fileWindows.window; w != nil {
		w.Invalidate()
	}
}

// Apply persisted metadata without replacing an editor or its undo history.
func applyNoteIncoming(d *noteDraft, note savedNote) {
	replaceNoteEditorValue(&d.title, note.Title)
	replaceNoteEditorValue(&d.body, note.Body)
	replaceNoteEditorValue(&d.tags, strings.Join(note.Tags, ", "))
	d.draft = note
}

func (s *State) pruneNoteDrafts() {
	for id, d := range s.notes.drafts {
		if s.noteIndex(id) < 0 {
			delete(s.notes.drafts, id)
			d.owner = nil
		}
	}
	s.noteLinksRestored()
}

func (s *State) notePopoverSide() components.PopoverSide {
	if s.noteContent().windowView {
		return components.PopoverTop
	}
	return components.PopoverBottom
}

// Native text editing resolves the view for each event, never retaining an
// editor pointer across a note switch or an IME owner change.
func (s *State) noteViewInput(n *notesUI, context app.TextInputContext, input *app.TextInput) *app.TextInput {
	if s.noteExit != nil {
		return nil
	}
	var editor *text.Editor
	s.withNoteView(n, func() { editor = s.noteViewEditor(n, context.ID, false) })
	if editor == nil {
		return nil
	}
	state := app.TextStateOf(editor)
	if context.ID == hitNoteBody || context.ID == hitNoteTitle {
		body, title := n.noteSelections()
		selection := body
		if context.ID == hitNoteTitle {
			selection = title
		}
		state.Selection = app.TextRange{Start: min(selection.Start, selection.End), End: max(selection.Start, selection.End)}
		state.SelectionReversed = selection.Start > selection.End
	}
	*input = app.TextInput{State: state, NavigationKeys: context.NavigationKeys}
	id, buffer := n.selected, n.noteDraft
	input.OnEvent = func(event app.TextEvent) (app.TextState, error) {
		if s.noteExit != nil || n.selected != id || n.noteDraft != buffer || s.noteIndex(id) < 0 {
			return app.TextState{}, nil
		}
		var current *text.Editor
		s.withNoteView(n, func() { current = s.noteEditor(context.ID) })
		if current == nil {
			return app.TextStateOf(editor), nil
		}
		event.Apply(current)
		if context.ID == hitNoteFind && s.activateNoteView(n) && !n.composing() {
			n.editing.find.Update(n.body.String())
			if n.editing.find.Seek(&n.body) {
				n.code.Reveal(n.body.ByteOffset(n.body.Caret()))
			}
		}
		n.captureNoteSelection()
		s.invalidateNoteViews()
		return app.TextStateOf(current), nil
	}
	return input
}

// NotesInput resolves the main window's shared note draft without claiming an
// inactive editor until a native input event actually arrives.
func (s *State) NotesInput(context app.TextInputContext) *app.TextInput {
	if context.ID != hitNoteBody && context.ID != hitNoteTitle && context.ID != hitNoteTags && context.ID != hitNoteFind {
		return nil
	}
	if s.noteExit != nil || !s.notesVisible() || s.noteSidebarVisible() {
		return nil
	}
	return s.noteViewInput(&s.notes, context, &s.notes.input)
}

func (s *State) restoreNotes(list []savedNote, folders []savedNoteFolder) {
	drafts := s.noteDrafts()
	mainID := s.notes.selected
	s.restoreMainNotes(list, folders)
	if s.notes.selected != mainID {
		if s.notes.selected != "" && s.notes.draft.ID == s.notes.selected {
			s.remapNoteWindows(mainID, s.notes.selected)
		}
		delete(drafts, mainID)
		if s.notes.selected != "" {
			drafts[s.notes.selected] = s.notes.noteDraft
		}
	}
	for id, d := range drafts {
		if d == s.notes.noteDraft {
			continue
		}
		var baseline *savedNote
		for _, saved := range s.lastSaved.Notes {
			if saved.ID == id {
				value := s.normalizeNoteHosts(saved)
				baseline = &value
				break
			}
		}
		dirty := d.changed() || s.hasSaved && (baseline == nil || string(mustNoteJSON(*baseline)) != string(mustNoteJSON(d.draft)))
		i := s.noteIndex(id)
		if dirty && (i < 0 || s.notes.list[i].Body != d.draft.Body || noteRevision(s.notes.list[i]) != noteRevision(d.draft)) {
			if baseline == nil && i < 0 {
				s.notes.list = append(s.notes.list, d.draft)
				continue
			}
			old := d.draft
			old.Revision = noteID()
			old.Updated = s.options.WallNow().UTC().Format(time.RFC3339Nano)
			conflict := s.noteConflict(old)
			d.conflictSuffix = strings.TrimPrefix(conflict.Title, old.Title)
			d.draft = conflict
			if s.noteIndex(conflict.ID) < 0 {
				s.notes.list = append(s.notes.list, conflict)
			}
			delete(drafts, id)
			drafts[conflict.ID] = d
			s.remapNoteWindows(id, conflict.ID)
		} else if i >= 0 && !dirty {
			applyNoteIncoming(d, s.notes.list[i])
		} else if i >= 0 {
			incoming := s.notes.list[i]
			base := d.draft
			if baseline != nil {
				base = *baseline
			}
			if d.title.String() == base.Title {
				replaceNoteEditorValue(&d.title, incoming.Title)
			}
			if slices.Equal(noteWords(d.tags.String()), base.Tags) {
				replaceNoteEditorValue(&d.tags, strings.Join(incoming.Tags, ", "))
			}
			if d.draft.Pinned != base.Pinned {
				incoming.Pinned = d.draft.Pinned
			}
			if d.draft.Folder != base.Folder {
				incoming.Folder = d.draft.Folder
			}
			if d.draft.Order != base.Order {
				incoming.Order = d.draft.Order
			}
			d.draft = incoming
			s.notes.list[i] = incoming
		} else {
			delete(drafts, id)
		}
	}
}
func replaceNoteEditorValue(e *text.Editor, value string) {
	if e.String() == value {
		return
	}
	before, selection := e.String(), noteDirectedSelection(e)
	e.BreakUndoGroup()
	e.SetSelection(text.Range{End: e.RuneCount()})
	e.Insert(value)
	e.BreakUndoGroup()
	e.SetSelection(noteAdjustedRange(selection, before, value))
}
func (s *State) remapNoteWindows(oldID, newID string) {
	m := &s.fileWindows
	oldKey, newKey := "note:"+oldID, "note:"+newID
	d := m.documents[oldKey]
	if d == nil {
		return
	}
	delete(m.documents, oldKey)
	m.documents[newKey] = d
	d.cacheKey = newKey
	d.note.id = newID
	d.note.content.selected = newID
	for i, key := range m.order {
		if key == oldKey {
			m.order[i] = newKey
		}
	}
	if m.active == oldKey {
		m.active = newKey
	}
	if m.pending == oldKey {
		m.pending = newKey
	}
}

func (s *State) noteTagsForID(id string) []string {
	if d := s.noteDrafts()[id]; d != nil {
		return noteWords(d.tags.String())
	}
	if i := s.noteIndex(id); i >= 0 {
		return s.notes.list[i].Tags
	}
	return nil
}
func (s *State) updateNoteBufferTags(id string, tags []string) {
	if i := s.noteIndex(id); i >= 0 {
		s.notes.list[i].Tags = slices.Clone(tags)
	}
	if d := s.noteDrafts()[id]; d != nil {
		replaceNoteEditorValue(&d.tags, strings.Join(tags, ", "))
		d.draft.Tags = slices.Clone(tags)
	}
}
