package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"slices"
	"strings"
)

type noteMode string

const (
	noteSource  noteMode = "source"
	notePreview noteMode = "preview"
	noteSplit   noteMode = "split"
)

type noteViewState struct {
	splitScroll                                          noteSplitScroll
	codeActions                                          noteCodeActions
	previewSelect                                        components.TextSelection
	previewSource                                        string
	treeBar, previewBar, popupBar                        noteScrollbar
	pointer                                              gfx.Point
	pointerValid                                         bool
	previewArea                                          components.ScrollArea
	previewDragging                                      bool
	previewGrab                                          float32
	tagsExpanded                                         bool
	hostPickerCollapsed                                  map[string]bool
	tagTokens                                            map[string]int
	tokenTags                                            map[int]string
	tagPopupArea                                         components.ScrollArea
	tagPopupScroll, tagPopupGrab                         float32
	tagPopupDragging                                     bool
	tagQuery                                             string
	returnFocus                                          gfx.ID
	treeFolderSelection                                  bool
	scrollDrag                                           bool
	scrollGrab                                           float32
	mode                                                 noteMode
	width                                                float32
	collapsed                                            bool
	expanded                                             map[string]bool
	searchOpen, drawer                                   bool
	treeBounds, workspace, documentBounds, overlayAnchor gfx.Rect
	// overlayFrom is the control the open menu or popup hangs under, found by
	// its identity each paint; without one the overlay hangs at overlayAnchor,
	// the point a context menu was opened at.
	overlayFrom              gfx.ID
	listArea                 components.ScrollArea
	treeScroll               float32
	noteTokens, folderTokens map[string]int
	tokenNotes, tokenFolders map[int]string
	inlineID, inlineParent   string
	inlineFolder, inlineNew  bool
	inline                   text.Editor
	focus                    gfx.ID
	menu                     string
	menuID                   string
	menuFolder               bool
	popup                    string
	picker                   text.Editor
	resizing                 bool
	drop                     noteDrop
}
type noteDrop struct {
	parent, before string
	valid, folder  bool
	bounds         gfx.Rect
	inside         bool
}

var (
	hitNoteTreeMore       = tree.ID(notesHits, "tree-more")
	hitNoteTreeToggle     = tree.ID(notesHits, "tree-toggle")
	hitNoteSearchToggle   = tree.ID(notesHits, "search-toggle")
	hitNoteExpandAll      = tree.ID(notesHits, "expand-all")
	hitNoteRename         = tree.ID(notesHits, "rename")
	hitNoteInline         = tree.ID(notesHits, "inline")
	hitNoteDismiss        = tree.ID(notesHits, "dismiss")
	hitNoteTagsOpen       = tree.ID(notesHits, "tags-open")
	hitNoteHostsOpen      = tree.ID(notesHits, "hosts-open")
	hitNotePicker         = tree.ID(notesHits, "picker")
	hitNoteTagSubmit      = tree.ID(notesHits, "tag-submit")
	hitNoteDivider        = tree.ID(notesHits, "divider")
	hitNoteDrawerDismiss  = tree.ID(notesHits, "drawer-dismiss")
	hitNoteModes          = tree.Sub(notesHits, tree.Name("mode-item"))
	hitNoteDisclosures    = tree.Sub(notesHits, tree.Name("disclosure"))
	hitNoteRowMore        = tree.Sub(notesHits, tree.Name("row-more"))
	hitNoteFolderMore     = tree.Sub(notesHits, tree.Name("folder-more"))
	hitNoteTagHints       = tree.Sub(notesHits, tree.Name("tag-hint"))
	hitNoteTagPickerThumb = tree.ID(notesHits, "tag-picker-thumb")
	hitNoteTagRows        = tree.Sub(notesHits, tree.Name("tag-row"))
	hitNoteTagRemove      = tree.Sub(notesHits, tree.Name("tag-remove"))
	hitNoteFormats        = tree.Sub(notesHits, tree.Name("format"))
)

func (s *State) noteTokens() {
	v := &s.notes.view
	if v.noteTokens == nil {
		v.noteTokens = map[string]int{}
		v.tokenNotes = map[int]string{}
		v.folderTokens = map[string]int{}
		v.tokenFolders = map[int]string{}
	}
	for _, n := range s.notes.list {
		if _, ok := v.noteTokens[n.ID]; !ok {
			i := len(v.noteTokens)
			v.noteTokens[n.ID] = i
			v.tokenNotes[i] = n.ID
		}
	}
	for _, n := range s.notes.folders {
		if _, ok := v.folderTokens[n.ID]; !ok {
			i := len(v.folderTokens)
			v.folderTokens[n.ID] = i
			v.tokenFolders[i] = n.ID
		}
	}
	if v.expanded == nil {
		v.expanded = map[string]bool{}
	}
	if v.width == 0 {
		v.width = 300
	}
	if v.mode == "" {
		v.mode = noteSource
	}
}
func (s *State) noteFolder(id string) (savedNoteFolder, bool) {
	for _, f := range s.notes.folders {
		if f.ID == id {
			return f, true
		}
	}
	return savedNoteFolder{}, false
}
func (s *State) noteOpenAncestors(parent string) {
	seen := map[string]bool{}
	for parent != "" && !seen[parent] {
		seen[parent] = true
		s.notes.view.expanded[parent] = true
		f, ok := s.noteFolder(parent)
		if !ok {
			break
		}
		parent = f.Parent
	}
}
func (s *State) beginNoteInline(id string, folder, newItem bool) {
	if !s.flushNoteDraft() {
		return
	}
	v := &s.notes.view
	v.inline = text.Editor{}
	v.inlineID = id
	v.inlineFolder = folder
	v.inlineNew = newItem
	v.inlineParent = s.notes.folder
	v.searchOpen = false
	s.notes.query = text.Editor{}
	if !newItem {
		if folder {
			f, _ := s.noteFolder(id)
			v.inline.Insert(f.Name)
			v.inlineParent = f.Parent
		} else if i := s.noteIndex(id); i >= 0 {
			v.inline.Insert(s.notes.list[i].Title)
			v.inlineParent = s.notes.list[i].Folder
		}
	}
	v.inline.SetSelection(text.Range{End: v.inline.RuneCount()})
	s.noteOpenAncestors(v.inlineParent)
	v.focus = hitNoteInline
}
func (s *State) commitNoteInline() bool {
	v := &s.notes.view
	if v.inlineID == "" && !v.inlineNew {
		return true
	}
	if r, ok := v.inline.Composition(); ok && r.Start != r.End {
		return false
	}
	name := strings.TrimSpace(v.inline.String())
	if name == "" {
		s.notes.error = s.noteText("Name cannot be empty.", "名称不能为空。")
		return false
	}
	if v.inlineFolder {
		if v.inlineNew {
			f := savedNoteFolder{ID: noteID(), Name: name, Parent: v.inlineParent, Order: len(s.notes.folders)}
			s.notes.folders = append(s.notes.folders, f)
			s.notes.folder = f.ID
			v.treeFolderSelection = true
			s.noteOpenAncestors(f.ID)
		} else {
			for i := range s.notes.folders {
				if s.notes.folders[i].ID == v.inlineID {
					s.notes.folders[i].Name = name
				}
			}
		}
	} else if i := s.noteIndex(v.inlineID); i >= 0 {
		if !s.selectNote(v.inlineID) {
			return false
		}
		s.notes.title.SetSelection(text.Range{End: s.notes.title.RuneCount()})
		s.notes.title.Insert(name)
		s.flushNoteDraft()
	}
	v.inlineID = ""
	v.inlineNew = false
	s.notes.error = ""
	s.noteTokens()
	s.save()
	return true
}

// overlayAnchorRect is where the open menu or popup hangs: under the control
// that opened it, as this paint declared it, or at the point it was opened at.
func (v *noteViewState) overlayAnchorRect(c *gfx.Canvas) gfx.Rect {
	if bounds, ok := c.HitBounds(v.overlayFrom); ok {
		return bounds
	}
	return v.overlayAnchor
}

func (s *State) noteViewClick(id gfx.ID) bool {
	if s.clickNoteFindActions(id) {
		return true
	}
	n, v := s.noteContent(), &s.noteContent().view
	if id == hitNoteDismiss {
		v.menu = ""
		v.popup = ""
		v.resetPopupScrollbar()
		v.focus = v.returnFocus
		return true
	}
	if id == hitNoteDrawerDismiss {
		v.drawer = false
		return true
	}
	if v.menu == "note" || v.menu == "folder" {
		switch id {
		case hitNoteRename, hitNotePin, hitNoteDelete, hitNoteExport, hitNoteFolderRemove, hitNoteAdd, hitNoteFolderAdd, hitNoteImport, hitNoteLinkFile, hitNoteLinkUnlink, hitNoteLinkFolder, hitNoteFolderUnlink:
			if v.menuFolder {
				n.folder = v.menuID

			} else if !s.selectNote(v.menuID) {
				return true
			}
		}
	}
	v.menu = ""
	if s.clickNoteLink(id) {
		return true
	}
	switch id {
	case hitNoteTreeMore:
		v.menu = "tree"
		v.overlayFrom = id
	case hitNoteTreeToggle:
		if v.workspace.Width < v.width+480 {
			v.drawer = !v.drawer
		} else {
			v.collapsed = !v.collapsed
		}
		s.save()
	case hitNoteSearchToggle:
		v.searchOpen = !v.searchOpen
		if v.searchOpen {
			v.focus = hitNoteSearch
		} else {
			n.query = text.Editor{}
		}
	case hitNoteExpandAll:
		expand := s.noteCanExpandAll()
		for _, f := range n.folders {
			v.expanded[f.ID] = expand
		}
		s.save()
	case hitNoteTagsExpand:
		v.tagsExpanded = !v.tagsExpanded
	case hitNoteTagsOpen:
		v.overlayFrom = id
		v.tagPopupScroll = 0
		v.resetPopupScrollbar()
		v.tagQuery = ""
		v.returnFocus = hitNoteTagsOpen
		v.popup = "tags"
		v.picker = text.Editor{}
		v.focus = hitNotePicker
	case hitNoteHostsOpen:
		v.overlayFrom = id
		v.tagPopupScroll = 0
		v.resetPopupScrollbar()
		v.tagQuery = ""
		v.returnFocus = hitNoteHostsOpen
		v.popup = "hosts"
		v.picker = text.Editor{}
		v.focus = hitNotePicker
	case hitNoteTagSubmit:
		if _, composing := v.picker.Composition(); composing {
			return true
		}
		tags := noteWords(n.tags.String())
		for _, tag := range noteWords(v.picker.String()) {
			if !slices.Contains(tags, tag) {
				tags = append(tags, tag)
			}
		}
		s.setNoteTags(tags)
		v.picker = text.Editor{}
		v.focus = hitNotePicker
	case hitNoteRename:
		s.beginNoteInline(v.menuID, v.menuFolder, false)
	default:
		if token, ok := id.In(hitNoteGroupSelect); ok {
			if path, exists := v.tokenTags[token]; exists {
				s.toggleNoteGroup(path)
			}
			return true
		}
		if token, ok := id.In(hitNoteHostGroups); ok {
			if path, exists := v.tokenTags[token]; exists && strings.TrimSpace(v.picker.String()) == "" {
				if v.hostPickerCollapsed == nil {
					v.hostPickerCollapsed = map[string]bool{}
				}
				v.hostPickerCollapsed[path] = !v.hostPickerCollapsed[path]
				v.tagPopupScroll = 0
			}
			return true
		}
		if i, ok := id.In(hitNoteModes); ok {
			v.mode = []noteMode{noteSource, notePreview, noteSplit}[min(i, 2)]
			v.focus = hitNoteBody
			if v.mode == notePreview {
				v.focus = id
			}
			if !n.windowView {
				s.save()
			}
			return true
		}
		if token, ok := id.In(hitNoteTagRows); ok {
			if tag, exists := v.tokenTags[token]; exists {
				s.toggleNoteTag(tag)
			}
			return true
		}
		if token, ok := id.In(hitNoteTagRemove); ok {
			if tag, exists := v.tokenTags[token]; exists {
				if path, special := parseNoteHostGroup(tag); special {
					s.removeNoteGroup(path)
					return true
				}
				if host, special := parseNoteHost(tag); special {
					s.replaceNoteTagEditor(removeNoteHost(noteWords(n.tags.String()), host.CatalogID))
					s.save()
					return true
				}
				tags := noteWords(n.tags.String())
				if at := slices.Index(tags, tag); at >= 0 {
					s.setNoteTags(slices.Delete(tags, at, at+1))
				}
			}
			return true
		}
		if i, ok := id.In(hitNoteFormats); ok {
			s.formatNote(i)
			return true
		}
		if token, ok := id.In(hitNoteDisclosures); ok {
			folder := v.tokenFolders[token]
			v.expanded[folder] = !v.expanded[folder]
			s.save()
			return true
		}
		if token, ok := id.In(hitNoteRowMore); ok {
			v.menu = "note"
			v.menuID = v.tokenNotes[token]
			v.menuFolder = false
			v.overlayFrom = id
			return true
		}
		if token, ok := id.In(hitNoteFolderMore); ok {
			v.menu = "folder"
			v.menuID = v.tokenFolders[token]
			v.menuFolder = true
			v.overlayFrom = id
			return true
		}
		if token, ok := id.In(hitNoteRows); ok {
			if s.selectNote(v.tokenNotes[token]) {
				v.treeFolderSelection = false
				if at := s.noteIndex(s.noteContent().selected); at >= 0 {
					n.folder = n.list[at].Folder
				}
				v.drawer = false
				v.focus = hitNoteBody
			}
			return true
		}
		if token, ok := id.In(hitNoteFolderRows); ok {
			n.folder = v.tokenFolders[token]
			v.treeFolderSelection = true
			s.save()
			return true
		}
		return false
	}
	return true
}

func (s *State) savedNotesView() savedNoteView {
	v := &s.notes.view
	expanded := []string{}
	for id, open := range v.expanded {
		if open {
			expanded = append(expanded, id)
		}
	}
	slices.Sort(expanded)
	return savedNoteView{Panel: s.notes.panel, Selected: s.notes.selected, Folder: s.notes.folder, Mode: v.mode, Width: v.width, Collapsed: v.collapsed, Expanded: expanded}
}
func (s *State) restoreNotesView(saved savedNoteView) {
	v := &s.notes.view
	v.mode = saved.Mode
	if v.mode != noteSource && v.mode != notePreview && v.mode != noteSplit {
		v.mode = noteSource
	}
	v.width = saved.Width
	if v.width == 0 {
		v.width = 300
	}
	v.width = min(max(v.width, 160), 520)
	v.collapsed = saved.Collapsed
	v.expanded = map[string]bool{}
	for _, id := range saved.Expanded {
		v.expanded[id] = true
	}
}

func (s *State) noteCanExpandAll() bool {
	for _, folder := range s.notes.folders {
		if !s.notes.view.expanded[folder.ID] {
			return true
		}
	}
	return false
}
func (s *State) noteExpandLabel() string {
	if s.noteCanExpandAll() || len(s.notes.folders) == 0 {
		return s.noteText("Expand all", "展开全部")
	}
	return s.noteText("Collapse all", "收起全部")
}
