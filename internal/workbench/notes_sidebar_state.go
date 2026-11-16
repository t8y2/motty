package workbench

import (
	"math"
	"slices"
	"time"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

// A non-nil object, including an empty one, marks the per-workspace format.
// It belongs to the window record, never to the shared note library.
type savedNoteSidebars struct {
	Workspaces map[layoutcore.WorkspaceID]savedNoteSidebar `json:"workspaces"`
}
type savedNoteSidebar struct {
	Open     bool     `json:"open,omitempty"`
	Width    float32  `json:"width"`
	Fraction float32  `json:"fraction"`
	Expanded []string `json:"expanded,omitempty"`
	Query    string   `json:"query,omitempty"`
	All      bool     `json:"all,omitempty"`
	Selected string   `json:"selected,omitempty"`
}

func (s *State) savedNoteSidebars() *savedNoteSidebars {
	saved := &savedNoteSidebars{Workspaces: make(map[layoutcore.WorkspaceID]savedNoteSidebar)}
	for _, workspace := range s.savedWorkspaceDeck().Workspaces {
		v := s.noteSidebars[workspace.ID]
		if v == nil {
			continue
		}
		n := &v.content
		s.syncNoteSidebarContent(n)
		entry := savedNoteSidebar{Open: v.open, Width: v.width, Fraction: v.fraction, Query: n.query.String(), All: n.sideAll, Selected: n.selected}
		for id, open := range n.view.expanded {
			if open {
				entry.Expanded = append(entry.Expanded, id)
			}
		}
		slices.Sort(entry.Expanded)
		saved.Workspaces[workspace.ID] = entry
	}
	return saved
}
func (s *State) restoreNoteSidebars(saved *savedNoteSidebars, legacyOpen bool) {
	for id := range s.noteSidebars {
		s.closeNoteSidebar(id)
	}
	if saved == nil {
		if id := s.deck.ActiveWorkspace(); id != 0 && legacyOpen {
			s.noteSidebarFor(id).open = true
		}
		return
	}
	for _, workspace := range s.deck.Workspaces(nil) {
		entry, ok := saved.Workspaces[workspace.ID]
		if !ok {
			continue
		}
		v := s.noteSidebarFor(workspace.ID)
		v.open = entry.Open
		if entry.Width > 0 && !math.IsInf(float64(entry.Width), 0) {
			v.width = entry.Width
		}
		if entry.Fraction > 0 && entry.Fraction < 1 {
			v.fraction = entry.Fraction
		}
		n := &v.content
		n.query.Insert(entry.Query)
		v.observedQuery, v.lastQuery = entry.Query, entry.Query
		n.sideAll = entry.All
		if s.noteIndex(entry.Selected) >= 0 {
			n.selected = entry.Selected
			s.syncNoteSidebarContent(n)
		}
		for _, id := range entry.Expanded {
			if _, ok := s.noteFolder(id); ok {
				n.view.expanded[id] = true
			}
		}
	}
}
func (s *State) closeNoteSidebar(id layoutcore.WorkspaceID) {
	if v := s.noteSidebars[id]; v != nil {
		v.stopCapture()
		if v.content.imageCancel != nil {
			v.content.imageCancel()
		}
		delete(s.noteSidebars, id)
	}
}

// Search uses the same paint-driven debounce as note drafts. Saving snapshots
// every sidebar also covers a query changed immediately before switching tabs.
func (s *State) autosaveNoteSidebars(c *gfx.Canvas) {
	for _, v := range s.noteSidebars {
		query := v.content.query.String()
		if query != v.observedQuery {
			v.observedQuery = query
			v.queryChanged = c.Now()
			v.queryDirty = true
		}
		if !v.queryDirty {
			continue
		}
		wait := 300*time.Millisecond - (c.Now() - v.queryChanged)
		if wait > 0 {
			c.RequestFrameIn(wait)
			continue
		}
		if _, composing := v.content.query.Composition(); composing {
			c.RequestFrameIn(300 * time.Millisecond)
			continue
		}
		v.queryDirty = false
		c.AfterPaint(func() { s.save() })
	}
}
