package workbench

import (
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/workbench/icons"
)

var hitNoteGroupSelect = tree.Sub(notesHits, tree.Name("host-picker-group-select"))

var hitNoteHostGroups = tree.Sub(notesHits, tree.Name("host-picker-group"))

func (s *State) notePopupSize(viewport gfx.Rect) (float32, float32) {
	width := s.noteContent().view.documentBounds.Width / 2
	if s.noteContent().view.popup == "hosts" {
		width = s.noteContent().view.documentBounds.Width
	}
	return min(max(width, 340), max(1, viewport.Width-24)), viewport.Height * .6
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
	slices.SortFunc(paths, func(a, b string) int {
		if a == b {
			return 0
		}
		if orderedHostGroupLess(a, b, groups) {
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

func (s *State) paintNoteHostPopup(c *gfx.Canvas, viewport gfx.Rect) {
	v := &s.noteContent().view
	theme := s.Theme()
	pad := theme.Metrics.PadSurface
	query := strings.ToLower(strings.TrimSpace(v.picker.String()))
	if query != v.tagQuery {
		v.tagQuery = query
		v.tagPopupScroll = 0
	}
	width, maxHeight := s.notePopupSize(viewport)
	items, height := s.layoutNoteHostPicker(max(1, width-2*pad-12), query)
	pop := components.BeginPopover(c, theme, viewport, v.overlayAnchorRect(c), components.PopoverOptions{Layer: popupLayer + 2, Scope: hitNoteDismiss, DismissID: hitNoteDismiss, ConsumeOutsidePress: true, Align: components.PopoverAlignStart, Side: s.notePopoverSide(), ContentSize: gfx.Size{Width: width, Height: min(maxHeight, 2*pad+38+height)}, InitialFocusID: hitNotePicker})
	layout := pop.Begin("", "")
	c.TrapFocus(hitNoteDismiss)
	body := layout.Content.Inset(gfx.All(pad))
	field := body
	field.Height = 30
	s.noteInputAt(c, hitNotePicker, field, s.noteText("Search hosts", "搜索主机"), v.picker.String())
	list := body
	list.Y += 38
	list.Height = max(0, list.Height-38)
	v.tagPopupArea = s.noteHoverScrollArea(c, list, gfx.Size{Width: list.Width, Height: height}, v.tagPopupScroll, hitNotePickerScroll, hitNoteTagPickerThumb, s.noteText("Scroll results", "滚动结果"), &v.popupBar, v.tagPopupDragging, s.noteScrollHover(list))
	geo := v.tagPopupArea.BeginContent()
	v.tagPopupScroll = geo.OffsetY
	tooltip := ""
	var tooltipBounds gfx.Rect
	tags := noteWords(s.noteContent().tags.String())
	for _, item := range items {
		rect := item.bounds
		rect.X += geo.Content.X
		rect.Y += geo.Content.Y
		if item.group {
			if item.bounds.Y > 0 {
				components.PaintSeparator(c, theme, gfx.Rect{X: rect.X, Y: rect.Y - 6, Width: rect.Width, Height: 1}, components.SeparatorHorizontal)
			}
			c.Rounded(rect, 6, s.pal.surfaceSoft)
			icon := icons.ChevronRight
			if item.expanded {
				icon = icons.ChevronDown
			}
			id := hitNoteHostGroups.At(s.noteTagToken(item.key))
			c.DrawIcon(icon, gfx.Rect{X: rect.X + 8, Y: rect.Y + 6, Width: 16, Height: 16}, s.pal.muted)
			label := c.FitLine(item.label, 16, gfx.WeightSemibold, max(0, rect.Width-68))
			labelWidth := c.TextWidthWeight(label, 16, gfx.WeightSemibold)
			c.Label(label, gfx.Rect{X: rect.X + 32, Y: rect.Y, Width: labelWidth, Height: 28}, gfx.TextStyle{Size: 16, Weight: gfx.WeightSemibold, Color: s.pal.text})
			c.HitWithOptions(id, rect, item.label, gfx.HitOptions{Role: gfx.RoleButton, Expanded: item.expanded, Expandable: true})
			if item.key != "" {
				selectID := hitNoteGroupSelect.At(s.noteTagToken(item.key))
				components.BeginCheckbox(c, theme, selectID, gfx.Rect{X: rect.X + 32 + labelWidth + 10, Y: rect.Y + 6, Width: 16, Height: 16}, noteHasGroup(tags, item.key)).Label(item.key).Paint()
			}
			continue
		}
		id := hitNoteHostRows.At(s.noteTagToken(item.key))
		selected := noteHasHost(tags, item.key)
		cardTheme := theme
		if selected {
			cardTheme.Card = theme.Secondary
		} else if c.Hovered() == id {
			cardTheme.Card = s.pal.surfaceSoft
		}
		components.PaintCard(c, cardTheme, rect, components.CardOptions{Size: components.CardSizeSmall})
		if c.FocusVisible() == id {
			c.RoundedOutline(rect, theme.Metrics.RadiusSurface+4, 2, theme.Primary)
		}
		s.paintNoteHostIcon(c, item.key, gfx.Rect{X: rect.X + 10, Y: rect.Y + 10, Width: 20, Height: 20}, cardTheme.Card)
		label := c.FitLine(item.label, theme.Metrics.FontSizeM, gfx.WeightMedium, max(0, rect.Width-76))
		c.Label(label, gfx.Rect{X: rect.X + 38, Y: rect.Y, Width: max(0, rect.Width-76), Height: 40}, gfx.TextStyle{Size: theme.Metrics.FontSizeM, Weight: gfx.WeightMedium, Color: s.pal.text})
		components.BeginCheckbox(c, theme, id, gfx.Rect{X: rect.X + rect.Width - 26, Y: rect.Y + 12, Width: 16, Height: 16}, selected).Label(item.label).HitBounds(rect).Paint()
		if label != item.label && c.Hovered() == id {
			tooltip = item.label
			tooltipBounds = rect
		}
	}
	if len(items) == 0 {
		c.Label(s.noteText("No hosts", "暂无主机"), gfx.Rect{X: geo.Content.X, Y: geo.Content.Y, Width: list.Width, Height: 28}, gfx.TextStyle{Size: 12, Color: s.pal.muted})
	}
	v.tagPopupArea.EndContent()
	if tooltip != "" {
		components.BeginTooltip(c, theme, viewport, tooltipBounds, components.TooltipOptions{Layer: tooltipLayer}).Paint(tooltip)
	}
	pop.End()
}
