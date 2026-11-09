package workbench

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/text"
	"motty/internal/configstore"
	"motty/internal/i18n"
)

type savedNoteView struct {
	Mode      noteMode `json:"mode,omitempty"`
	Width     float32  `json:"width,omitempty"`
	Collapsed bool     `json:"collapsed,omitempty"`
	Expanded  []string `json:"expanded,omitempty"`
	Panel     bool     `json:"panel,omitempty"`
	Selected  string   `json:"selected,omitempty"`
	Folder    string   `json:"folder,omitempty"`
}

type savedNote struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Body    string   `json:"body"`
	Folder  string   `json:"folder,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Pinned  bool     `json:"pinned,omitempty"`
	Order   int      `json:"order,omitempty"`
	Created string   `json:"created"`
	Updated string   `json:"updated"`
	// Ancestors name earlier body revisions, allowing sync to distinguish an
	// ordinary later edit from two edits made independently on different devices.
	Revision  string   `json:"revision"`
	Ancestors []string `json:"ancestors,omitempty"`
	// Link names the Markdown file the note's text lives in. Such a note is kept
	// in this device's own state, not in the shared records, because the path
	// means nothing elsewhere; Body is then the last text seen in the file.
	Link *noteLink `json:"link,omitempty"`
}

// noteLink is where a linked note's text is stored.
type noteLink struct {
	Path string `json:"path"`
	// Tree is the linked folder this note's file belongs to, or empty for a file
	// linked on its own. The folder's own link follows its files.
	Tree string `json:"tree,omitempty"`
}
type savedNoteFolder struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Parent string `json:"parent,omitempty"`
	Order  int    `json:"order,omitempty"`
	// Link ties the folder to a directory on disk. Like a linked note it is kept
	// in this device's own state, never in shared records.
	Link *noteFolderLink `json:"link,omitempty"`
}

// noteFolderLink is the directory a folder mirrors.
type noteFolderLink struct {
	Path string `json:"path"`
	// Root marks the directory the user linked; the folders below it are found by
	// reading it.
	Root bool `json:"root,omitempty"`
	// Hidden are files, as paths below the root, that the user removed from Motty
	// and that must not come back when the directory is read again.
	Hidden []string `json:"hidden,omitempty"`
}
type notesUI struct {
	editing       noteEditing
	previewLayout notePreviewLayout
	*noteDraft
	input      app.TextInput
	drafts     map[string]*noteDraft
	windowView bool
	viewport   gfx.Rect
	selection  noteEditorSelection
	sidebar    *noteSidebar

	groupJobs []*noteGroupJob

	hostRefreshNeeded                       bool
	hostScanRunning, hostScanAgain          bool
	hostUpdates                             []noteHostUpdate
	view                                    noteViewState
	list                                    []savedNote
	folders                                 []savedNoteFolder
	selected, folder                        string
	query                                   text.Editor
	panel                                   bool
	sideAll                                 bool
	error                                   string
	code                                    components.CodeEditor
	doc                                     markdown.Document
	rendered                                components.MarkdownView
	previewScroll                           float32
	confirm                                 string
	dragID                                  string
	dragFolder                              bool
	dragStart                               gfx.Point
	dragging                                bool
	rows                                    []noteRow
	listBounds, sourceBounds, previewBounds gfx.Rect
	previewHeight                           float32
	images                                  []noteImage
	imageSource                             string
	imageToken                              uint64
	followConflict                          string
	imageCancel                             context.CancelFunc
}
type noteRow struct {
	id     string
	folder bool
	bounds gfx.Rect
}

var notesHits = tree.Feature("notes")
var (
	hitNotes            = tree.ID(shellHits, "notes")
	hitNotesPanel       = tree.ID(notesHits, "panel")
	hitNoteAdd          = tree.ID(notesHits, "add")
	hitNoteTitle        = tree.ID(notesHits, "title")
	hitNoteBody         = tree.ID(notesHits, "body")
	hitNoteTags         = tree.ID(notesHits, "tags")
	hitNoteSearch       = tree.ID(notesHits, "search")
	hitNoteFolderAdd    = tree.ID(notesHits, "folder-add")
	hitNoteFolderRemove = tree.ID(notesHits, "folder-remove")
	hitNoteDelete       = tree.ID(notesHits, "delete")
	hitNotePin          = tree.ID(notesHits, "pin")
	hitNoteImport       = tree.ID(notesHits, "import")
	hitNoteExport       = tree.ID(notesHits, "export")
	hitNoteExportAll    = tree.ID(notesHits, "export-all")
	hitNoteConfirm      = tree.ID(notesHits, "confirm")
	hitNoteDeleteAll    = tree.ID(notesHits, "delete-all")
	hitNoteCancel       = tree.ID(notesHits, "cancel")
	hitNoteRows         = tree.Sub(notesHits, tree.Name("row"))
	hitNoteFolderRows   = tree.Sub(notesHits, tree.Name("folder"))
	hitNoteHostRows     = tree.Sub(notesHits, tree.Name("host"))
)

func (s *State) noteText(en, zh string) string {
	if s.language == i18n.Chinese {
		return zh
	}
	return en
}

// noteSaveFailure is the one wording for a note change that did not reach the
// store. The paths that set the notice and the path that clears it compare
// against this, so they cannot drift apart and leave a stale warning behind.
func (s *State) noteSaveFailure() string {
	return s.noteText("Unable to save notes; changes retained in this window.", "无法保存笔记，更改已保留在此窗口中。")
}
func noteID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func noteHash(body string) string { h := sha256.Sum256([]byte(body)); return hex.EncodeToString(h[:]) }
func (s *State) noteIndex(id string) int {
	return slices.IndexFunc(s.notes.list, func(n savedNote) bool { return n.ID == id })
}
func (s *State) noteEditor(id gfx.ID) *text.Editor {
	return s.noteViewEditor(s.noteContent(), id, true)
}
func (s *State) noteViewEditor(n *notesUI, id gfx.ID, activate bool) *text.Editor {
	if s.noteSidebarVisible() {
		if id == hitNoteSearch {
			return &s.noteSidebarState().content.query
		}
		return nil
	}
	if !s.notesVisible() || n.confirm != "" {
		return nil
	}
	if id == hitNoteInline && (n.view.inlineID != "" || n.view.inlineNew) {
		return &n.view.inline
	}
	if n.view.popup != "" {
		if id == hitNotePicker {
			return &n.view.picker
		}
		return nil
	}
	if n.view.menu != "" {
		return nil
	}
	if !n.windowView && (s.modal != modalNone || (s.activeSection != sectionNotes && !(n.panel && s.activeSection == sectionTerminal))) {
		return nil
	}
	switch id {
	case hitNoteSearch:
		return &n.query
	case hitNoteFind:
		if n.editing.findOpen {
			return &n.editing.find.Query
		}
	}
	if n.selected == "" {
		return nil
	}
	switch id {
	case hitNoteTitle:
		if activate && !s.activateNoteView(n) || !activate && n.owner != n && n.composing() {
			return nil
		}
		return &n.title
	case hitNoteBody:
		if n.windowView && n.view.mode == notePreview {
			return nil
		}
		if activate && !s.activateNoteView(n) || !activate && n.owner != n && n.composing() {
			return nil
		}
		s.configureNoteEditor(&n.body)
		return &n.body
	case hitNoteTags:
		if activate && !s.activateNoteView(n) || !activate && n.owner != n && n.composing() {
			return nil
		}
		return &n.tags
	}
	return nil
}
func (s *State) noteComposing() bool {
	if s.notes.noteDraft == nil {
		s.notes.noteDraft = &noteDraft{}
	}
	for _, d := range s.noteDrafts() {
		if d.composing() {
			return true
		}
	}
	return false
}

func noteWords(v string) []string {
	var out []string
	for _, w := range strings.Split(v, ",") {
		w = strings.TrimSpace(w)
		if w != "" && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}
func (s *State) flushNoteDraft() bool {
	s.runNoteGroupJobs()
	ok := true
	for id, draft := range s.noteDrafts() {
		if id != "" && !s.flushNoteBuffer(id, draft) {
			if draft == s.notes.noteDraft {
				s.notes.error = draft.problem
			}
			ok = false
		}
	}
	return ok
}
func (s *State) flushNoteBuffer(id string, buffer *noteDraft) bool {
	n := buffer
	i := s.noteIndex(id)
	if i < 0 {
		return true
	}
	if buffer.composing() {
		return false
	}
	next := s.notes.list[i]
	if n.conflictSuffix != "" {
		sel := n.title.Selection()
		n.title.MoveTo(n.title.RuneCount(), false)
		n.title.Insert(n.conflictSuffix)
		n.title.SetSelection(sel)
		n.conflictSuffix = ""
	}
	next.Title = n.title.String()
	next.Body = n.body.String()
	next.Tags = noteWords(n.tags.String())
	if next.Title == n.draft.Title && next.Body == n.draft.Body && slices.Equal(next.Tags, n.draft.Tags) {
		return true
	}
	if next.Body != n.draft.Body {
		h := noteRevision(n.draft)
		next.Revision = noteID()
		if !slices.Contains(next.Ancestors, h) {
			next.Ancestors = append(slices.Clone(next.Ancestors), h)
		}
	}
	next.Updated = s.options.WallNow().UTC().Format(time.RFC3339Nano)
	raw := mustNoteJSON(recordPayload{Data: mustNoteJSON(next)})
	if len(raw) > configstore.MaxPayload {
		buffer.problem = s.noteText("Note exceeds the 1 MiB record limit; draft retained.", "笔记超过 1 MiB 记录上限，草稿已保留。")
		return false
	}
	if next.Link != nil && next.Body != n.draft.Body {
		s.noteLinkWrite(next)
	}
	s.notes.list[i] = next
	n.draft = next
	buffer.problem = ""
	return true
}
func mustNoteJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func (s *State) selectNote(id string) bool {
	if !s.flushNoteDraft() {
		return false
	}
	s.save()
	if s.hasUnsavedNotes() {
		s.notes.error = s.noteSaveFailure()
		return false
	}
	if s.notes.selected != id {
		s.selectNoteWithoutSave(id)
	}
	return true
}
func (s *State) restoreMainNotes(list []savedNote, folders []savedNoteFolder) {
	n := &s.notes
	n.hostRefreshNeeded = true
	dirty := n.selected != "" && (n.title.String() != n.draft.Title || n.body.String() != n.draft.Body || !slices.Equal(noteWords(n.tags.String()), n.draft.Tags))
	var baseline *savedNote
	if s.hasSaved {
		for i := range s.lastSaved.Notes {
			if s.lastSaved.Notes[i].ID == n.selected {
				normalized := s.normalizeNoteHosts(s.lastSaved.Notes[i])
				baseline = &normalized
				break
			}
		}
		if n.selected != "" && (baseline == nil || string(mustNoteJSON(*baseline)) != string(mustNoteJSON(n.draft))) {
			dirty = true
		}
	}
	old := n.draft
	n.list = make([]savedNote, len(list))
	for i, note := range list {
		n.list[i] = s.normalizeNoteHosts(note)
	}
	n.folders = append([]savedNoteFolder(nil), folders...)
	if dirty {
		at := s.noteIndex(n.selected)
		if at < 0 && baseline == nil {
			n.list = append(n.list, old)
			return
		}
		if at < 0 || n.list[at].Body != old.Body || noteRevision(n.list[at]) != noteRevision(old) {
			old.Revision = noteID()
			old.Updated = s.options.WallNow().UTC().Format(time.RFC3339Nano)
			originalTitle := old.Title
			old = s.noteConflict(old)
			n.conflictSuffix = strings.TrimPrefix(old.Title, originalTitle)
			n.selected = old.ID
			n.draft = old
			if s.noteIndex(old.ID) < 0 {
				n.list = append(n.list, old)
			}
		} else {
			// Rebase unchanged metadata onto the refresh while retaining pending fields.
			incoming := n.list[at]
			base := old
			if baseline != nil {
				base = *baseline
			}
			if n.title.String() == base.Title && n.title.String() != incoming.Title {
				replaceNoteEditorValue(&n.title, incoming.Title)
			}
			if slices.Equal(noteWords(n.tags.String()), base.Tags) && !slices.Equal(base.Tags, incoming.Tags) {
				replaceNoteEditorValue(&n.tags, strings.Join(incoming.Tags, ", "))
			}
			if old.Pinned != base.Pinned {
				incoming.Pinned = old.Pinned
			}
			if old.Folder != base.Folder {
				incoming.Folder = old.Folder
			}
			if old.Order != base.Order {
				incoming.Order = old.Order
			}
			n.list[at] = incoming
			n.draft = incoming
		}
		return
	}
	id := n.selected
	if n.followConflict != "" {
		id = n.followConflict
		n.followConflict = ""
	}
	if s.noteIndex(id) < 0 {
		id = ""
	}
	if i := s.noteIndex(id); i >= 0 {
		n.noteDraft = s.noteBuffer(id)
		applyNoteIncoming(n.noteDraft, s.notes.list[i])
	}
	if id != n.selected {
		s.selectNoteWithoutSave(id)
	}
}
func (s *State) selectNoteWithoutSave(id string) {
	if s.notes.noteDraft != nil {
		s.notes.captureNoteSelection()
		if s.notes.selected != id && s.notes.owner == &s.notes {
			s.notes.owner = nil
		}
	}
	if s.notes.selected != id {
		s.notes.view.tagsExpanded = false
	}
	s.notes.view.tagPopupScroll = 0
	s.notes.view.resetPopupScrollbar()
	s.notes.view.previewBar = noteScrollbar{}
	s.notes.view.treeBar = noteScrollbar{}
	s.notes.view.clearPreviewSelection()
	s.notes.view.previewDragging = false
	s.notes.view.scrollDrag = false
	s.notes.view.tagTokens = nil
	s.notes.view.tokenTags = nil
	s.notes.view.popup = ""
	s.notes.view.picker = text.Editor{}
	s.notes.previewScroll = 0
	s.notes.code = components.CodeEditor{}
	s.notes.view.splitScroll = noteSplitScroll{}
	s.notes.view.codeActions.source = ""
	s.notes.view.codeActions.targets = nil
	s.notes.view.codeActions.copied = gfx.ID{}
	n := &s.notes
	s.cancelNoteImages()
	n.selected = id
	n.noteDraft = s.noteBuffer(id)
	n.selection = noteEditorSelection{}

}
func (s *State) autosaveNotes(c *gfx.Canvas) {
	if len(s.notes.groupJobs) > 0 && !s.noteComposing() {
		c.AfterPaint(func() { s.save() })
	}

	if s.notes.hostRefreshNeeded && (s.activeSection == sectionNotes || s.activeSection == sectionTerminal && s.notes.panel) {
		c.AfterPaint(func() { s.refreshNoteHosts() })
	}
	if !s.noteComposing() && (len(s.notes.hostUpdates) > 0 || s.notes.hostScanAgain && !s.notes.hostScanRunning) {
		c.AfterPaint(func() { s.applyNoteHostUpdates() })
	}
	for id, n := range s.noteDrafts() {
		if id == "" || s.noteIndex(id) < 0 {
			continue
		}
		observed := n.title.String() + "\x00" + n.body.String() + "\x00" + n.tags.String()
		if observed != n.observed {
			n.observed, n.lastChange = observed, c.Now()
			c.RequestFrameIn(300 * time.Millisecond)
		} else if n.changed() && !n.composing() {
			if c.Now()-n.lastChange >= 300*time.Millisecond {
				c.AfterPaint(func() { s.save(); s.invalidateNoteViews() })
			} else {
				c.RequestFrameIn(300 * time.Millisecond)
			}
		}
	}

}
func (s *State) clickNotes(id gfx.ID) bool {
	s.noteTokens()
	n := &s.notes
	if id == hitNotes {
		if !s.flushNoteDraft() {
			return true
		}
		s.noteLinksRecheck()
		s.activeSection = sectionNotes
		s.refreshNoteHosts()
		s.save()
		return true
	}
	if id == hitNotesPanel {
		n.panel = !n.panel
		if n.panel {
			s.noteSidebarState()
			s.refreshNoteHosts()
		} else if n.sidebar != nil {
			n.sidebar.stopCapture()
		}
		s.fitTerminals()
		return true
	}
	if s.noteSidebarVisible() {
		return s.clickNoteSidebar(id)
	}

	if s.modal != modalNone || (s.activeSection != sectionNotes && !(n.panel && s.activeSection == sectionTerminal)) {
		return false
	}
	if n.confirm != "" {
		switch id {
		case hitNoteCancel:
			n.confirm = ""
		case hitNoteConfirm:
			s.deleteNoteSelection(false)
		case hitNoteDeleteAll:
			s.deleteNoteSelection(true)
		}
		return id.Scope() == notesHits
	}
	if s.clickNoteCodeCopy(id, n) {
		return true
	}
	if s.noteViewClick(id) {
		return true
	}
	switch id {
	case hitNoteAdd:
		if !s.flushNoteDraft() {
			return true
		}
		now := s.options.WallNow().UTC().Format(time.RFC3339Nano)
		entry := savedNote{Revision: noteID(), ID: noteID(), Folder: n.folder, Order: len(n.list), Created: now, Updated: now}
		s.newLinkedNote(&entry)
		n.list = append(n.list, entry)
		s.selectNote(entry.ID)
		n.view.treeFolderSelection = false
		n.view.drawer = false
		n.view.focus = hitNoteTitle
		s.noteOpenAncestors(entry.Folder)
		s.save()
	case hitNoteDelete:
		if n.selected != "" {
			n.confirm = "note"
		}
	case hitNotePin:
		if s.flushNoteDraft() {
			if i := s.noteIndex(n.selected); i >= 0 {
				n.list[i].Pinned = !n.list[i].Pinned
				n.draft = n.list[i]
				s.save()
			}
		}
	case hitNoteFolderAdd:
		s.beginNoteInline("", true, true)
	case hitNoteFolderRemove:
		if n.folder != "" {
			n.confirm = "folder"
		}
	case hitNoteLoadImages:
		s.loadNoteImages()
	case hitNoteImport:
		s.importNotes()
	case hitNoteLinkFile:
		s.linkNotes()
	case hitNoteLinkFolder:
		s.linkFolder()
	case hitNoteFolderUnlink:
		s.noteFolderUnlink(n.folder)
	case hitNoteExport:
		s.exportNotes(false)
	case hitNoteExportAll:
		s.exportNotes(true)
	default:
		if i, ok := id.In(hitNoteLinks); ok {
			if url := n.rendered.Link(i); url != "" {
				if err := s.options.OpenURL(url); err != nil {
					n.error = err.Error()
				}
			}
			return true
		}
		if token, ok := id.In(hitNoteHostRows); ok {
			if catalogID, exists := n.view.tokenTags[token]; exists {
				s.toggleNoteHost(catalogID)
			}
			return true
		}
		return false
	}
	return true
}
func (s *State) deleteNoteSelection(all bool) {
	n := &s.notes
	if !s.flushNoteDraft() {
		return
	}
	if n.confirm == "note" {
		id := n.selected
		s.noteHideMembers(map[string]bool{id: true})
		n.list = slices.DeleteFunc(n.list, func(v savedNote) bool { return v.ID == id })
		s.selectNoteWithoutSave("")
	} else {
		folder := n.folder
		parent := ""
		for _, f := range n.folders {
			if f.ID == folder {
				parent = f.Parent
			}
		}
		if all {
			descendants := map[string]bool{}
			for _, f := range n.folders {
				if s.noteFolderWithin(f.ID, folder) {
					descendants[f.ID] = true
				}
			}
			doomed := map[string]bool{}
			for _, v := range n.list {
				if s.noteFolderWithin(v.Folder, folder) {
					doomed[v.ID] = true
				}
			}
			s.noteHideMembers(doomed)
			n.list = slices.DeleteFunc(n.list, func(v savedNote) bool { return s.noteFolderWithin(v.Folder, folder) })
			n.folders = slices.DeleteFunc(n.folders, func(f savedNoteFolder) bool { return descendants[f.ID] })
			if s.noteIndex(n.selected) < 0 {
				s.selectNoteWithoutSave("")
			}
		} else {
			for i := range n.list {
				if n.list[i].Folder == folder {
					n.list[i].Folder = parent
				}
			}
			for i := range n.folders {
				if n.folders[i].Parent == folder {
					n.folders[i].Parent = parent
				}
			}
			n.folders = slices.DeleteFunc(n.folders, func(f savedNoteFolder) bool { return f.ID == folder })
		}
		n.folder = parent
	}
	if i := s.noteIndex(n.selected); i >= 0 {
		n.draft = n.list[i]
	}
	n.confirm = ""
	s.pruneNoteDrafts()
	s.save()
}
func (s *State) noteFolderWithin(id, ancestor string) bool {
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		if id == ancestor {
			return true
		}
		seen[id] = true
		parent := ""
		for _, f := range s.notes.folders {
			if f.ID == id {
				parent = f.Parent
				break
			}
		}
		id = parent
	}
	return false
}
func (s *State) moveNote(id, parent, before string, folder bool) bool {
	if s.notes.query.String() != "" || !s.flushNoteDraft() {
		return false
	}
	n := &s.notes
	if folder {
		if id == parent || s.noteFolderWithin(parent, id) {
			return false
		}
		for i := range n.folders {
			if n.folders[i].ID == id {
				n.folders[i].Parent = parent
				n.folders[i].Order = 0
				for _, sibling := range n.folders {
					if sibling.ID != id && sibling.Parent == parent {
						n.folders[i].Order = max(n.folders[i].Order, sibling.Order+1)
					}
				}
				if before != "" {
					for _, f := range n.folders {
						if f.ID == before {
							n.folders[i].Order = f.Order
						}
					}
				}
				for j := range n.folders {
					if n.folders[j].ID != id && n.folders[j].Parent == parent && n.folders[j].Order >= n.folders[i].Order {
						n.folders[j].Order++
					}
				}
				break
			}
		}
	} else {
		at := s.noteIndex(id)
		if at < 0 {
			return false
		}
		n.list[at].Folder = parent
		order := 0
		for _, sibling := range n.list {
			if sibling.ID != id && sibling.Folder == parent {
				order = max(order, sibling.Order+1)
			}
		}
		if i := s.noteIndex(before); i >= 0 {
			order = n.list[i].Order
		}
		for i := range n.list {
			if i != at && n.list[i].Folder == parent && n.list[i].Order >= order {
				n.list[i].Order++
			}
		}
		n.list[at].Order = order
		if n.selected == id {
			n.draft = n.list[at]
		}
	}
	s.save()
	return true
}

func (s *State) unlinkNoteHost(id string) {
	for i := range s.notes.list {
		n := &s.notes.list[i]
		s.updateNoteBufferTags(n.ID, removeNoteHost(s.noteTagsForID(n.ID), id))
	}
}
