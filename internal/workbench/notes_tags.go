package workbench

import (
	"slices"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/workbench/icons"
)

func (s *State) paintNoteHostIcon(c *gfx.Canvas, catalogID string, bounds gfx.Rect, background gfx.Color) {
	for _, host := range s.hosts {
		if host.CatalogID == catalogID {
			s.paintHostCatalogIcon(c, host, bounds, background)
			return
		}
	}
	c.DrawIcon(icons.Server, bounds, s.pal.text)
}

func (s *State) noteTagCandidates() []string {
	tags := []string{}
	for _, note := range s.notes.list {
		values := s.noteTagsForID(note.ID)
		for _, tag := range values {
			if noteSpecialTag(tag) {
				continue
			}
			if !slices.Contains(tags, tag) {
				tags = append(tags, tag)
			}
		}
	}
	slices.Sort(tags)
	return tags
}

func (s *State) noteTagToken(tag string) int {
	v := &s.noteContent().view
	if v.tagTokens == nil {
		v.tagTokens = map[string]int{}
		v.tokenTags = map[int]string{}
	}
	if token, ok := v.tagTokens[tag]; ok {
		return token
	}
	token := len(v.tagTokens)
	v.tagTokens[tag], v.tokenTags[token] = token, tag
	return token
}

func (s *State) setNoteTags(tags []string) {
	ordinary := []string{}
	for _, tag := range tags {
		if !noteSpecialTag(tag) {
			ordinary = append(ordinary, tag)
		}
	}
	for _, tag := range noteWords(s.noteContent().tags.String()) {
		if noteSpecialTag(tag) {
			ordinary = append(ordinary, tag)
		}
	}
	s.replaceNoteTagEditor(ordinary)
	s.save()
}

func (s *State) toggleNoteTag(tag string) {
	tags := noteWords(s.noteContent().tags.String())
	if at := slices.Index(tags, tag); at >= 0 {
		tags = slices.Delete(tags, at, at+1)
	} else {
		tags = append(tags, tag)
	}
	s.setNoteTags(tags)
}

// noteTagLayout keeps measuring, painting and hit testing on the same geometry.
type noteTagLayout struct {
	tag, label string
	bounds     gfx.Rect
	truncated  bool
}

func layoutNoteTags(c *gfx.Canvas, tags []string, width, fontSize, padding float32) ([]noteTagLayout, float32) {
	var items []noteTagLayout
	x, y := float32(0), float32(0)
	width = max(1, width)
	for _, tag := range tags {
		textWidth := c.TextWidthWeight(tag, fontSize, gfx.WeightMedium)
		w := min(width, textWidth+padding)
		if x > 0 && x+w > width {
			x = 0
			y += 34
		}
		label := tag
		if textWidth > max(0, width-padding) {
			label = c.FitLine(tag, fontSize, gfx.WeightMedium, max(0, width-padding))
		}
		items = append(items, noteTagLayout{tag: tag, label: label, bounds: gfx.Rect{X: x, Y: y, Width: w, Height: 28}, truncated: label != tag})
		x += w + 6
	}
	height := float32(28)
	if len(items) > 0 {
		height = y + 28
	}
	return items, height
}
func (s *State) noteTagTooltip(c *gfx.Canvas, item noteTagLayout, id gfx.ID, b gfx.Rect) {
	if item.truncated && c.Hovered() == id {
		components.BeginTooltip(c, s.Theme(), s.windowRect(), b, components.TooltipOptions{Layer: tooltipLayer}).Paint(item.tag)
	}
}

var hitNoteTagsExpand = tree.ID(notesHits, "expand-tags")

func layoutNoteDetailTags(c *gfx.Canvas, labels []string, width, fontSize, padding, badgeHeight float32, expanded bool) ([]noteTagLayout, float32, bool) {
	items, height := layoutNoteTags(c, labels, width, fontSize, padding)
	overflow := height > 28
	if overflow {
		reserved := float32(34)
		if !expanded {
			reserved += c.TextWidthWeight(strconv.Itoa(len(labels)), fontSize, gfx.WeightMedium) + 6
		}
		items, _ = layoutNoteTags(c, labels, max(1, width-reserved), fontSize, padding)
	}
	visible := items[:0]
	height = 28
	for _, item := range items {
		row := item.bounds.Y / 34
		if overflow && !expanded && row > 0 {
			break
		}
		item.bounds.Y = row * (badgeHeight + 6)
		item.bounds.Height = badgeHeight + 4
		height = max(height, item.bounds.Y+badgeHeight+8)
		visible = append(visible, item)
	}
	return visible, height, overflow
}
func (s *State) paintNoteTags(c *gfx.Canvas, b gfx.Rect) float32 {
	return s.paintNoteTagArea(c, b, false)
}
func (s *State) paintNoteLibraryTags(c *gfx.Canvas, b gfx.Rect) float32 {
	return s.paintNoteTagArea(c, b, true)
}
func (s *State) paintNoteTagArea(c *gfx.Canvas, b gfx.Rect, compact bool) float32 {
	theme := s.Theme()
	padding, badgeHeight := components.BadgeExtent(c, theme, " ×")
	tags := noteWords(s.noteContent().tags.String())
	ordered, labels := []string{}, []string{}
	for _, tag := range tags {
		if !noteSpecialTag(tag) {
			ordered = append(ordered, tag)
			labels = append(labels, tag)
		}
	}
	for _, tag := range tags {
		if path, ok := parseNoteHostGroup(tag); ok {
			label := path
			if !s.noteGroupExists(path) {
				label += s.noteText(" (group missing)", "（分组不存在）")
			}
			ordered = append(ordered, tag)
			labels = append(labels, "     "+label)
		}
	}
	for _, tag := range tags {
		h, host := parseNoteHost(tag)
		if !host {
			continue
		}
		label := noteHostLabel(h)
		if _, exists := s.noteHostByID(h.CatalogID); !exists {
			label += s.noteText(" (host missing)", "（主机不存在）")
		}
		ordered = append(ordered, tag)
		labels = append(labels, "     "+label)
	}
	items, height := layoutNoteTags(c, labels, b.Width, theme.Metrics.FontSizeS, padding)
	if compact {
		var overflow bool
		items, height, overflow = layoutNoteDetailTags(c, labels, b.Width, theme.Metrics.FontSizeS, padding, badgeHeight, s.noteContent().view.tagsExpanded)
		if overflow {
			icon, label := icons.ChevronDown, s.noteText("Expand tags", "展开标签")
			if s.noteContent().view.tagsExpanded {
				icon, label = icons.ChevronUp, s.noteText("Collapse tags", "收起标签")
			} else {
				count := strconv.Itoa(len(labels) - len(items))
				countWidth := c.TextWidthWeight(count, theme.Metrics.FontSizeS, gfx.WeightMedium)
				c.Label(count, gfx.Rect{X: b.X + max(0, b.Width-34-countWidth), Y: b.Y, Width: countWidth, Height: 28}, gfx.TextStyle{Size: theme.Metrics.FontSizeS, Weight: gfx.WeightMedium, Color: s.pal.muted})
			}
			s.noteButton(c, hitNoteTagsExpand, gfx.Rect{X: b.X + max(0, b.Width-28), Y: b.Y, Width: 28, Height: 28}, label, icon)
		}
	}
	for i := range items {
		items[i].tag = ordered[i]
	}

	for _, item := range items {
		rect := item.bounds
		rect.X += b.X
		rect.Y += b.Y
		offset := float32(4)
		badgeTheme := theme
		if compact {
			badgeTheme.SecondaryForeground.Alpha = 0
		}
		badge := components.PaintBadge(c, badgeTheme, gfx.Rect{X: rect.X, Y: rect.Y + offset}, item.label+" ×", components.BadgeSecondary)
		if compact {
			c.Label(item.label, gfx.Rect{X: badge.X + 9, Y: badge.Y, Width: max(0, badge.Width-27), Height: badge.Height}, gfx.TextStyle{Size: theme.Metrics.FontSizeS, Weight: gfx.WeightMedium, Color: theme.SecondaryForeground})
		}
		rect.Width = badge.Width
		host, isHost := parseNoteHost(item.tag)
		full := item.tag
		if isHost {
			s.paintNoteHostIcon(c, host.CatalogID, gfx.Rect{X: rect.X + 7, Y: rect.Y + offset + (badge.Height-14)/2, Width: 14, Height: 14}, badgeTheme.Secondary)
			full = noteHostLabel(host) + "\n" + host.Address + "\n" + host.User
			if _, exists := s.noteHostByID(host.CatalogID); !exists {
				full += s.noteText("\nHost missing", "\n主机不存在")
			}
		}
		path, isGroup := parseNoteHostGroup(item.tag)
		if isGroup {
			c.DrawIcon(icons.FolderSolidOpen, gfx.Rect{X: rect.X + 7, Y: rect.Y + offset + (badge.Height-14)/2, Width: 14, Height: 14}, gfx.Color{Alpha: 1})
			full = path
			if !s.noteGroupExists(path) {
				full += s.noteText(" (group missing)", "（分组不存在）")
			}
		}
		id := hitNoteTagRemove.At(s.noteTagToken(item.tag))
		remove := gfx.Rect{X: rect.X + rect.Width - 22, Y: rect.Y, Width: 22, Height: rect.Height}
		if compact {
			color := s.pal.muted
			if c.Hovered() == id {
				color = theme.SecondaryForeground
			}
			c.DrawIcon(icons.X, gfx.Rect{X: rect.X + rect.Width - 18, Y: badge.Y + (badge.Height-12)/2, Width: 12, Height: 12}, color)
		}
		c.HitWithOptions(id, remove, s.noteText("Remove tag ", "移除标签 ")+full, gfx.HitOptions{Role: gfx.RoleButton})
		// The tooltip covers the whole truncated pill, including its label.
		labelID := hitNoteTagHints.At(s.noteTagToken(item.tag))
		c.HitWithOptions(labelID, gfx.Rect{X: rect.X, Y: rect.Y, Width: max(0, rect.Width-22), Height: rect.Height}, full, gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true})
		if !isHost && !isGroup && s.noteContent().view.popup == "" {
			s.noteTagTooltip(c, item, labelID, rect)
			s.noteTagTooltip(c, item, id, rect)
		}
	}
	return height
}

func (s *State) paintNoteTagPopup(c *gfx.Canvas, viewport gfx.Rect) {
	v := &s.noteContent().view
	theme := s.Theme()
	m := theme.Metrics
	query := strings.ToLower(strings.TrimSpace(v.picker.String()))
	if v.tagQuery != query {
		v.tagQuery = query
		v.tagPopupScroll = 0
	}
	candidates := s.noteTagCandidates()
	selected := noteWords(s.noteContent().tags.String())
	filtered := []string{}
	for _, tag := range candidates {
		if strings.Contains(strings.ToLower(tag), query) {
			filtered = append(filtered, tag)
		}
	}
	hasNew := false
	for _, tag := range noteWords(v.picker.String()) {
		if noteSpecialTag(tag) {
			continue
		}
		if !slices.Contains(candidates, tag) {
			hasNew = true
		}
	}
	width, maxHeight := s.notePopupSize(viewport)
	innerWidth := max(1, width-2*m.PadSurface-12)
	items, height := layoutNoteTags(c, filtered, innerWidth, m.FontSizeM, 32)
	contentHeight := min(2*m.PadSurface+38+height, maxHeight)
	pop := components.BeginPopover(c, theme, viewport, v.overlayAnchorRect(c), components.PopoverOptions{Layer: popupLayer + 2, Scope: hitNoteDismiss, DismissID: hitNoteDismiss, ConsumeOutsidePress: true, Align: components.PopoverAlignStart, Side: s.notePopoverSide(), ContentSize: gfx.Size{Width: width, Height: contentHeight}, InitialFocusID: hitNotePicker})
	layout := pop.Begin("", "")
	c.TrapFocus(hitNoteDismiss)
	body := layout.Content.Inset(gfx.All(m.PadSurface))
	field := body
	field.Height = 30
	field.Width = max(0, field.Width-66)
	s.noteInputAt(c, hitNotePicker, field, s.noteText("Search or add", "搜索或添加"), v.picker.String())
	add := components.BeginButton(c, theme, hitNoteTagSubmit, gfx.Rect{X: field.X + field.Width + 6, Y: field.Y, Width: 60, Height: 30}).Height(30)
	if !hasNew {
		add = add.Disabled()
	}
	add.Outline(s.noteText("Add", "新增"))
	list := body
	list.Y += 38
	list.Height = max(0, list.Height-38)
	v.tagPopupArea = s.noteHoverScrollArea(c, list, gfx.Size{Width: list.Width, Height: height}, v.tagPopupScroll, hitNotePickerScroll, hitNoteTagPickerThumb, s.noteText("Scroll results", "滚动结果"), &v.popupBar, v.tagPopupDragging, s.noteScrollHover(list))
	geo := v.tagPopupArea.BeginContent()
	v.tagPopupScroll = geo.OffsetY
	var tooltipItem noteTagLayout
	var tooltipID gfx.ID
	var tooltipBounds gfx.Rect
	for _, item := range items {
		rect := item.bounds
		rect.X += geo.Content.X
		rect.Y += geo.Content.Y
		label := item.label
		active := slices.Contains(selected, item.tag)
		if active {
			label = "✓ " + label
		}
		id := hitNoteTagRows.At(s.noteTagToken(item.tag))
		s.paintNoteTagChoice(c, id, rect, label, item.tag, active)
		if item.truncated && c.Hovered() == id {
			tooltipItem, tooltipID, tooltipBounds = item, id, rect
		}
	}
	if len(items) == 0 {
		c.Label(s.noteText("No tags", "暂无标签"), gfx.Rect{X: geo.Content.X, Y: geo.Content.Y, Width: innerWidth, Height: 28}, gfx.TextStyle{Size: m.FontSizeS, Color: s.pal.muted})
	}
	v.tagPopupArea.EndContent()
	if !tooltipID.IsZero() {
		s.noteTagTooltip(c, tooltipItem, tooltipID, tooltipBounds)
	}
	pop.End()
}
func (s *State) paintNoteTagChoice(c *gfx.Canvas, id gfx.ID, b gfx.Rect, label, full string, selected bool) {
	button := components.BeginButton(c, s.Theme(), gfx.ID{}, b).Height(28).WithState(components.ButtonState{Hovered: c.Hovered() == id, Pressed: c.Pressed() == id, Focused: c.FocusVisible() == id})
	if selected {
		button.Secondary(label)
	} else {
		button.Outline(label)
	}
	c.HitWithOptions(id, b, full, gfx.HitOptions{Role: gfx.RoleButton, Selected: selected})
}
