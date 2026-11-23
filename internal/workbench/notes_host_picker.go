package workbench

import (
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/workbench/icons"
)

var hitNoteGroupSelect = tree.Sub(notesHits, tree.Name("host-picker-group-select"))

var hitNoteHostGroups = tree.Sub(notesHits, tree.Name("host-picker-group"))

func (s *State) notePopupSize(c ui.C, n *notesUI) (float32, float32) {
	viewport := c.Bounds()
	return notePopupExtent(n.view.documentBounds.Width, viewport.Width, viewport.Height, n.view.popup == "hosts")
}

// notePopupExtent is the size of a notes picker over a document docWidth wide in
// a window windowWidth by windowHeight.
func notePopupExtent(docWidth, windowWidth, windowHeight float32, hosts bool) (float32, float32) {
	width := docWidth / 2
	if hosts {
		width = docWidth
	}
	return min(max(width, 340), max(1, windowWidth-24)), windowHeight * .6
}

type noteHostPickerItem struct {
	group      bool
	key, label string
	bounds     gfx.Rect
	expanded   bool
}

// Build from the full catalog, independently of the hosts page's visible rows.
func (s *State) layoutNoteHostPicker(width float32, query string) ([]noteHostPickerItem, float32) {
	groups := map[string]hostGroup{}
	for _, g := range s.hostGroups {
		groups[g.Name] = g
	}
	for path := range groups {
		for strings.Contains(path, "/") {
			path = hostGroupParent(path)
			if _, ok := groups[path]; !ok {
				groups[path] = hostGroup{Name: path}
			}
		}
	}
	matched := []Host{}
	query = strings.ToLower(strings.TrimSpace(query))
	for _, h := range s.hosts {
		path := strings.TrimSpace(h.Group)
		if _, ok := groups[path]; !ok {
			groups[path] = hostGroup{Name: path}
		}
		for strings.Contains(path, "/") {
			path = hostGroupParent(path)
			if _, ok := groups[path]; !ok {
				groups[path] = hostGroup{Name: path}
			}
		}
		if query != "" && !strings.Contains(strings.ToLower(h.Label+" "+h.Address+" "+h.User+" "+strings.TrimSpace(h.Group)), query) {
			continue
		}
		matched = append(matched, h)

	}
	// Drop state for removed groups so a later group at that path starts open.
	for path := range s.noteContent().view.hostPickerCollapsed {
		if _, exists := groups[path]; !exists {
			delete(s.noteContent().view.hostPickerCollapsed, path)
		}
	}
	visible := map[string]bool{}
	for path, g := range groups {
		if path != "" && g.Explicit && (query == "" || strings.Contains(strings.ToLower(path), query)) {
			visible[path] = true
			for parent := path; strings.Contains(parent, "/"); {
				parent = hostGroupParent(parent)
				visible[parent] = true
			}
		}
	}
	paths := []string{}
	for path := range groups {
		if visible[path] {
			paths = append(paths, path)
			continue
		}
		for _, h := range matched {
			hp := strings.TrimSpace(h.Group)
			if hp == path || path != "" && strings.HasPrefix(hp, path+"/") {
				paths = append(paths, path)
				break
			}
		}
	}
	collator := s.hostNameCollator()
	slices.SortFunc(paths, func(a, b string) int {
		if a == b {
			return 0
		}
		if orderedHostGroupLess(collator, a, b, groups) {
			return -1
		}
		return 1
	})
	var items []noteHostPickerItem
	y := float32(0)
	for _, path := range paths {
		hidden := false
		parent := path
		for strings.Contains(parent, "/") {
			parent = hostGroupParent(parent)
			if query == "" && s.noteContent().view.hostPickerCollapsed[parent] {
				hidden = true
				break
			}
		}
		if hidden {
			continue
		}
		depth := min(strings.Count(path, "/"), 12)
		inset := min(float32(depth)*12, max(0, width-180))
		expanded := query != "" || !s.noteContent().view.hostPickerCollapsed[path]
		label := s.hostGroupLabel(groups[path])
		if path != "" {
			label = hostGroupLeaf(path)
		}
		if len(items) > 0 {
			y += 12
		}
		items = append(items, noteHostPickerItem{group: true, key: path, label: label, bounds: gfx.Rect{X: inset, Y: y, Width: width - inset, Height: 28}, expanded: expanded})
		y += 32
		if !expanded {
			continue
		}
		direct := []Host{}
		for _, h := range matched {
			if strings.TrimSpace(h.Group) == path {
				direct = append(direct, h)
			}
		}
		columns := max(1, int((width-inset+8)/188))
		cardWidth := (width - inset - float32(columns-1)*8) / float32(columns)
		for i, h := range direct {
			items = append(items, noteHostPickerItem{key: h.CatalogID, label: noteHostLabel(noteHostFor(h)), bounds: gfx.Rect{X: inset + float32(i%columns)*(cardWidth+8), Y: y + float32(i/columns)*48, Width: cardWidth, Height: 40}})
		}
		if len(direct) > 0 {
			y += float32((len(direct)+columns-1)/columns) * 48
		}
	}
	return items, max(28, y)
}

// declareNoteHostPopup declares the host picker over its trigger: the search,
// the group rows and the host cards, as page controls of the popover.
func (s *State) declareNoteHostPopup(c ui.C, n *notesUI) {
	v := &n.view
	p := &n.page
	theme := s.Theme()
	pad := theme.Metrics.PadSurface
	query := strings.ToLower(strings.TrimSpace(v.picker.String()))
	queryChanged := query != v.tagQuery
	if queryChanged {
		v.tagQuery = query
	}
	width, maxHeight := s.notePopupSize(c, n)
	items, height := s.layoutNoteHostPicker(max(1, width-2*pad-12), query)
	contentHeight := min(maxHeight, 2*pad+38+height)
	anchor, anchored := p.anchorBox(n)
	if !anchored {
		v.popup = ""
		return
	}
	open := true
	ui.PopoverWith(c, anchor, &open, ui.PopoverSpec{
		Scope: hitNoteDismiss, DismissID: hitNoteDismiss, ConsumeOutsidePress: true,
		Side: s.notePopoverSide(), Align: ui.Start, Width: width, InitialFocusID: hitNotePicker,
	}, func() {
		ui.Column(c).Children(func() {
			p.picker.Bind(&v.picker)
			ui.TextFieldID(c, hitNotePicker, &p.picker, s.noteText("Search hosts", "搜索主机"), s.noteText("Search hosts", "搜索主机")).
				Height(30).Grow(1)
		})
		listHeight := max(0, contentHeight-2*pad-38)
		var area ui.Box
		if queryChanged {
			area = ui.ScrollAt(c, 0)
		} else {
			area = ui.Scroll(c)
		}
		area.Height(listHeight).FillWidth().Scrollbars()
		area.Children(func() {
			stack := ui.Stack(c).FillWidth().Height(max(height, 28))
			stack.Children(func() {
				tags := noteWords(n.tags.String())
				for _, item := range items {
					rect := item.bounds
					if item.group {
						if item.bounds.Y > 0 {
							ui.Divider(c).Absolute().Left(rect.X).Top(rect.Y - 6).Width(rect.Width).Height(1).Background(s.pal.line)
						}
						groupID := hitNoteHostGroups.At(s.noteTagToken(item.key))
						group := ui.Row(c).Absolute().Left(rect.X).Top(rect.Y).Width(rect.Width).Height(28).
							Radius(6).Background(s.pal.surfaceSoft).
							HitWith(groupID, item.label, gfx.HitOptions{Role: gfx.RoleButton, Expanded: item.expanded, Expandable: true})
						group.Children(func() {
							icon := icons.ChevronRight
							if item.expanded {
								icon = icons.ChevronDown
							}
							ui.Icon(c, icon).Size(16, 16).Color(s.pal.muted).MarginSides(0, 0, 0, 8).Shrink(0)
							ui.Text(c, item.label).FontSize(16).Weight(gfx.WeightSemibold).Color(s.pal.text).Ellipsis().MarginSides(0, 6, 0, 8).Grow(1)
							if item.key != "" {
								ui.Checkbox(c, s.pageFlag(noteHasGroup(tags, item.key)), "").
									Hit(hitNoteGroupSelect.At(s.noteTagToken(item.key)), item.key).Size(16, 16).Shrink(0)
							}
						})
						continue
					}
					id := hitNoteHostRows.At(s.noteTagToken(item.key))
					selected := noteHasHost(tags, item.key)
					cardColor := theme.Card
					if selected {
						cardColor = theme.Secondary
					} else if c.Canvas().Hovered() == id {
						cardColor = s.pal.surfaceSoft
					}
					card := ui.Row(c).Absolute().Left(rect.X).Top(rect.Y).Width(rect.Width).Height(40).
						Radius(theme.Metrics.RadiusSurface + 4).Background(cardColor)
					card.Children(func() {
						ui.Slot(c).Size(20, 20).Shrink(0).Draw(func(d *ui.DrawContext) {
							s.paintNoteHostIcon(d.Canvas, item.key, d.Rect, cardColor)
						})
						label := ui.Text(c, item.label).FontSize(theme.Metrics.FontSizeM).Weight(gfx.WeightMedium).Color(s.pal.text).
							Ellipsis().Grow(1).MarginSides(0, 8, 0, 8)
						if item.label != item.fullLabel() {
							label.Tooltip(item.label)
						}
						ui.Checkbox(c, s.pageFlag(selected), "").Hit(id, item.label).Size(16, 16).Shrink(0).MarginSides(0, 10, 0, 0)
					})
				}
			})
		})
		if len(items) == 0 {
			ui.Text(c, s.noteText("No hosts", "暂无主机")).FontSize(12).Color(s.pal.muted)
		}
	})
	if !open {
		v.popup = ""
	}
}

// fullLabel is the label as the host picker measured it, trimmed to its card.
func (i noteHostPickerItem) fullLabel() string { return i.label }
