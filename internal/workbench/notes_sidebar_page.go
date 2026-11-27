package workbench

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/workbench/icons"
)

// hitNoteSidebarSurface blocks presses on the panel's blank surface from
// reaching the terminal underneath it. It does nothing when clicked.
var hitNoteSidebarSurface = tree.ID(notesHits, "sidebar-surface")

func (s *State) noteSidebarCanExpandAll() bool {
	for _, folder := range s.notes.folders {
		if !s.noteSidebarState().content.view.expanded[folder.ID] {
			return true
		}
	}
	return false
}

// declareNoteSidebar declares the notes panel beside the terminals in the
// workbench page: its width splitter, its tree and the preview of the selected
// note.
func (s *State) declareNoteSidebar(c ui.C) {
	if !s.noteSidebarVisible() {
		return
	}
	v := s.noteSidebarState()
	n := &v.content
	s.noteTokens()
	s.noteTokensFor(&n.view)
	p := s.ensureNotePage(n, noteSurfaceSidebar)
	p.c = c
	frame := s.workspacePixelBounds(s.viewport)
	base := s.baseWorkspacePixelBounds(s.viewport)
	b := gfx.Rect{X: frame.X + frame.Width, Y: base.Y, Width: s.notesPanelWidth(s.viewport), Height: base.Height}
	v.bounds = b
	n.sourceBounds, n.previewBounds = gfx.Rect{}, gfx.Rect{}
	s.syncNoteSidebar()
	s.noteTreeRows(n, true)
	total := max(0, frame.Width+b.Width)
	if p.panel.Pos == 0 {
		p.panel.Pos = max(0, total-1-b.Width)
	}
	maxWidth := max(0, total*.7)
	minFirst := max(0, total-1-maxWidth)
	row := ui.Row(c).Absolute().Left(frame.X).Top(b.Y).Width(total).Height(b.Height)
	row.Children(func() {
		ui.Slot(c).Grow(1)
		// The old handles' FractionForKey moved them 5% of the room per arrow.
		p.panel.StepFraction = .05
		ui.Splitter(c, &p.panel).SplitterThickness(1).Mins(minFirst, min(280, maxWidth)).Label(s.noteText("Resize notes panel", "调整笔记侧栏宽度"))
		panel := ui.Column(c).Width(min(b.Width, maxWidth)).FillHeight().Background(s.pal.workspace).
			HitWith(hitNoteSidebarSurface, "", gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, SkipTab: true})
		panel.Children(func() {
			inset := gfx.Insets{Left: 1, Right: 1, Top: 1, Bottom: 1}
			ui.Column(c).Fill().MarginSides(inset.Top, inset.Right, inset.Bottom, inset.Left).Children(func() {
				if n.selected == "" {
					s.declareNoteSidebarTree(c, n, b.Width-2, b.Height-2)
					v.splitDragging = false
					return
				}
				usable := max(0, b.Height-2-1)
				minTop, minBottom := float32(120), float32(160)
				if usable < minTop+minBottom {
					minTop = usable * 120 / 280
					minBottom = usable - minTop
				}
				if p.side.Pos == 0 {
					p.side.Pos = usable * v.fraction
				}
				top := min(max(p.side.Pos, minTop), max(minTop, usable-minBottom))
				ui.Column(c).Height(top).FillWidth().Children(func() {
					s.declareNoteSidebarTree(c, n, b.Width-2, top)
				})
				p.side.StepFraction = .05
				ui.Splitter(c, &p.side).SplitterThickness(1).Mins(minTop, minBottom).Label(s.noteText("Resize note browser sections", "调整笔记上下区域"))
				ui.Column(c).Grow(1).FillWidth().Children(func() {
					s.declareNoteSidebarPreview(c, n, b.Width-2)
				})
			})
		})
	})
	row.DrawOver(p.afterLayout)
}

// declareNoteSidebarPreview declares the head of the note the panel shows and
// its Markdown preview.
func (s *State) declareNoteSidebarPreview(c ui.C, n *notesUI, width float32) {
	title := n.draft.Title
	if strings.TrimSpace(title) == "" {
		title = s.noteText("Untitled", "无标题")
	}
	body := ui.Column(c).FillWidth().FillHeight().MarginSides(6, 8, 8, 8)
	body.Children(func() {
		ui.Row(c).Height(28).Children(func() {
			ui.Text(c, title).FontSize(14).Color(s.pal.text).Grow(1).Ellipsis()
			if len(noteImageURLs(n.body.String())) > 0 {
				s.declareNoteToolbarButton(c, hitNoteLoadImages, s.noteText("Load images", "加载图片"), icons.Image, false)
			}
			s.declareNoteToolbarButton(c, hitNoteSideOpen, s.noteText("Open in window", "在窗口中打开"), icons.ExternalLink, false)
		})
		ui.Divider(c).Height(1).MarginSides(4, 0, 0, 0).Background(s.pal.line)
		if n.error != "" {
			ui.TextSelectionArea(c, &n.errorSelection).Children(func() {
				ui.Text(c, n.error).FontSize(11).Color(s.pal.danger).MarginSides(4, 0, 0, 0)
			})
		}
		ui.Column(c).Grow(1).FillWidth().MarginSides(6, 0, 0, 0).Children(func() {
			s.declareNotePreview(c, n, max(1, width-16), true)
		})
	})
}

// declareNoteSidebarTree declares the panel's browser: the scope row and the
// tree of the notes it shows.
func (s *State) declareNoteSidebarTree(c ui.C, n *notesUI, width, height float32) {
	p := &n.page
	rows := n.view.treeScratch.rows
	expandLabel := s.noteText("Collapse all", "收起全部")
	expandIcon := icons.ListX
	if s.noteSidebarCanExpandAll() || len(s.notes.folders) == 0 {
		expandLabel = s.noteText("Expand all", "展开全部")
		expandIcon = icons.ListTree
	}
	disabled := len(s.notes.folders) == 0 || n.query.String() != ""
	linked := s.noteText("Linked only", "只看关联")
	ui.Column(c).FillWidth().Height(height).Children(func() {
		ui.Row(c).Height(30).MarginSides(8, 8, 0, 8).Gap(6).Items(ui.Center).Children(func() {
			// The expand control was a ghost button, not a quiet icon: its
			// hover and focus are the button recipe's, as before.
			s.noteIconButton(c, hitNoteSideExpand, expandIcon, expandLabel, 28, false).
				Disabled(disabled)
			p.query.Bind(&n.query)
			field := ui.TextField(c, &p.query, s.noteText("Search title, body and tags", "搜索标题、正文和标签"), s.noteText("Search title, body and tags", "搜索标题、正文和标签"), ui.WithID(hitNoteSearch)).
				Height(30).Grow(1)
			if field.Changed() {
				s.noteSearchChanged(n)
			}
			ui.Switch(c, s.pageFlag(!n.sideAll), linked).Hit(hitNoteSideLinked, linked).Shrink(0)
			s.noteIconButton(c, hitNoteSideClose, icons.X, s.noteText("Close notes sidebar", "关闭笔记侧栏"), 24, true)
		})
		ui.Divider(c).Height(1).MarginSides(7, 8, 0, 8).Background(s.pal.line)
		area := ui.Column(c).Grow(1).FillWidth().MarginSides(6, 6, 6, 6).
			HitWith(hitNoteListScroll, s.noteText("Scroll notes", "滚动笔记"), gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true})
		area.Children(func() {
			ui.Tree(c, &p.tree, len(rows), p.label, p.content).Grow(1).FillWidth().Scrollbars().
				Label(s.noteText("Scroll notes", "滚动笔记"))
			s.noteTreeChanges(n, true)
			if len(rows) == 0 {
				message := s.noteText("No notes yet.", "暂无笔记。")
				if n.query.String() != "" {
					message = s.noteText("No matching notes.", "没有匹配的笔记。")
				}
				ui.Text(c, message).FontSize(12).Color(s.pal.muted).MarginSides(8, 0, 0, 0)
			}
		})
	})
}

// noteSidebarWheel scrolls the panel's preview over the panes, where the page's
// own scroll areas are laid out outside its flow.
func (s *State) noteSidebarWheel(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	n := &s.noteSidebarState().content
	if n.previewBounds.Width <= 0 || !pointInRect(event.X, event.Y, n.previewBounds) {
		return gfx.EventResult{}, false
	}
	maximum := max(0, n.previewHeight-n.previewBounds.Height)
	step := event.DeltaY
	if event.Pages != 0 {
		step = event.StepYIn(1, n.previewBounds.Height)
	}
	next := min(max(0, n.previewScroll+step), maximum)
	if next != n.previewScroll {
		n.setPreviewScroll(next)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{Handled: true}, true
}

// noteSidebarAfterLayout keeps the saved split and width in step with the
// panel's splitters once the page has laid them out.
func (s *State) noteSidebarAfterLayout(n *notesUI) {
	p := &n.page
	base := s.baseWorkspacePixelBounds(s.viewport)
	frame := s.workspacePixelBounds(s.viewport)
	total := max(0, frame.Width+s.notesPanelWidth(s.viewport))
	if p.panel.Pos != 0 {
		maximum := max(0, total*.7)
		width := min(max(total-1-p.panel.Pos, min(280, maximum)), maximum)
		if width != s.notesPanelWidth(s.viewport) {
			if panel := s.noteSidebars[s.deck.ActiveWorkspace()]; panel != nil {
				panel.width = width
				s.fitWorkspaceTerminals(s.deck.ActiveWorkspace())
			}
		}
		if p.panel.Dragging() {
			p.panelWasDragging = true
		} else if p.panelWasDragging {
			p.panelWasDragging = false
			s.save()
		}
	}
	if p.side.Pos != 0 {
		side := s.noteSidebarState()
		usable := max(1, base.Height-3)
		fraction := min(max(p.side.Pos/usable, 0), 1)
		if fraction != side.fraction {
			side.fraction = fraction
		}
		if p.side.Dragging() {
			p.sideWasDragging = true
		} else if p.sideWasDragging {
			p.sideWasDragging = false
			s.save()
		}
	}
}
