package workbench

import (
	"slices"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
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

// declareNoteTagArea declares the note's tags over a region the page gives
// them: the pills are painted by hand, while their remove and hint targets are
// page controls, so the tooltip and the click reach the page's routing.
func (s *State) declareNoteTagArea(c ui.C, n *notesUI, width float32, compact bool) float32 {
	p := &n.page
	theme := s.Theme()
	padding, badgeHeight := components.BadgeExtent(c.Canvas(), theme, " ×")
	tags := noteWords(n.tags.String())
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
	items, height := layoutNoteTags(c.Canvas(), labels, width, theme.Metrics.FontSizeS, padding)
	overflow := false
	if compact {
		items, height, overflow = layoutNoteDetailTags(c.Canvas(), labels, width, theme.Metrics.FontSizeS, padding, badgeHeight, n.view.tagsExpanded)
	}
	for i := range items {
		items[i].tag = ordered[i]
	}
	p.tagItems = items
	p.tagCompact = compact
	stack := ui.Stack(c).FillWidth().Height(height).MarginSides(5, 20, 0, 20)
	stack.Children(func() {
		for _, item := range items {
			rect := item.bounds
			host, isHost := parseNoteHost(item.tag)
			full := item.tag
			if isHost {
				full = noteHostLabel(host) + "\n" + host.Address + "\n" + host.User
				if _, exists := s.noteHostByID(host.CatalogID); !exists {
					full += s.noteText("\nHost missing", "\n主机不存在")
				}
			}
			path, isGroup := parseNoteHostGroup(item.tag)
			if isGroup {
				full = path
				if !s.noteGroupExists(path) {
					full += s.noteText(" (group missing)", "（分组不存在）")
				}
			}
			removeID := hitNoteTagRemove.At(s.noteTagToken(item.tag))
			ui.Slot(c).Absolute().Left(rect.X+rect.Width-22).Top(rect.Y).Size(22, rect.Height).
				HitWith(removeID, s.noteText("Remove tag ", "移除标签 ")+full, gfx.HitOptions{Role: gfx.RoleButton})
			labelID := hitNoteTagHints.At(s.noteTagToken(item.tag))
			hint := ui.Slot(c).Absolute().Left(rect.X).Top(rect.Y).Size(max(rect.Width-22, 0), rect.Height).
				HitWith(labelID, full, gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true})
			if !isHost && !isGroup && item.truncated {
				hint.Tooltip(item.tag)
			}
		}
		if compact && overflow {
			icon, label := icons.ChevronDown, s.noteText("Expand tags", "展开标签")
			if n.view.tagsExpanded {
				icon, label = icons.ChevronUp, s.noteText("Collapse tags", "收起标签")
			} else {
				count := strconv.Itoa(len(labels) - len(items))
				ui.Text(c, count).FontSize(theme.Metrics.FontSizeS).Color(s.pal.muted).
					Absolute().Right(34).Top(0).Width(34)
			}
			s.declareNoteToolbarButton(c, hitNoteTagsExpand, label, icon, false).Absolute().Right(0).Top(0)
		}
	})
	stack.DrawOver(s.noteTagDraw(n))
	return height
}

// noteTagDraw paints the pills the declaration measured.
func (s *State) noteTagDraw(n *notesUI) func(*ui.DrawContext) {
	p := &n.page
	if p.drawTags == nil {
		p.drawTags = func(d *ui.DrawContext) { s.paintNoteTagItems(d.Canvas, d.Rect, n) }
	}
	return p.drawTags
}

func (s *State) paintNoteTagItems(c *gfx.Canvas, origin gfx.Rect, n *notesUI) {
	p := &n.page
	theme := s.Theme()
	_, badgeHeight := components.BadgeExtent(c, theme, " ×")
	_ = badgeHeight
	for _, item := range p.tagItems {
		rect := item.bounds
		rect.X += origin.X
		rect.Y += origin.Y
		offset := float32(4)
		badgeTheme := theme
		if p.tagCompact {
			badgeTheme.SecondaryForeground.Alpha = 0
		}
		badge := components.PaintBadge(c, badgeTheme, gfx.Rect{X: rect.X, Y: rect.Y + offset}, item.label+" ×", components.BadgeSecondary)
		if p.tagCompact {
			c.Label(item.label, gfx.Rect{X: badge.X + 9, Y: badge.Y, Width: max(0, badge.Width-27), Height: badge.Height}, gfx.TextStyle{Size: theme.Metrics.FontSizeS, Weight: gfx.WeightMedium, Color: theme.SecondaryForeground})
		}
		rect.Width = badge.Width
		host, isHost := parseNoteHost(item.tag)
		if isHost {
			s.paintNoteHostIcon(c, host.CatalogID, gfx.Rect{X: rect.X + 7, Y: rect.Y + offset + (badge.Height-14)/2, Width: 14, Height: 14}, badgeTheme.Secondary)
		}
		path, isGroup := parseNoteHostGroup(item.tag)
		if isGroup {
			_ = path
			c.DrawIcon(icons.FolderSolidOpen, gfx.Rect{X: rect.X + 7, Y: rect.Y + offset + (badge.Height-14)/2, Width: 14, Height: 14}, gfx.Color{Alpha: 1})
		}
		if p.tagCompact {
			id := hitNoteTagRemove.At(s.noteTagToken(item.tag))
			color := s.pal.muted
			if c.Hovered() == id {
				color = theme.SecondaryForeground
			}
			c.DrawIcon(icons.X, gfx.Rect{X: rect.X + rect.Width - 18, Y: badge.Y + (badge.Height-12)/2, Width: 12, Height: 12}, color)
		}
	}
}

// declareNoteTagPopup declares the tag picker over its trigger.
func (s *State) declareNoteTagPopup(c ui.C, n *notesUI) {
	v := &n.view
	p := &n.page
	theme := s.Theme()
	m := theme.Metrics
	query := strings.ToLower(strings.TrimSpace(v.picker.String()))
	queryChanged := v.tagQuery != query
	if queryChanged {
		v.tagQuery = query
	}
	candidates := s.noteTagCandidates()
	selected := noteWords(n.tags.String())
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
	width, maxHeight := s.notePopupSize(c, n)
	innerWidth := max(1, width-2*m.PadSurface-12)
	items, height := layoutNoteTags(c.Canvas(), filtered, innerWidth, m.FontSizeM, 32)
	contentHeight := min(2*m.PadSurface+38+height, maxHeight)
	anchor, anchored := p.anchorBox(n)
	if !anchored {
		v.popup = ""
		return
	}
	open := true
	ui.PopoverWith(c, anchor, ui.PopoverSpec{
		Scope: hitNoteDismiss, DismissID: hitNoteDismiss, ConsumeOutsidePress: true,
		Side: s.notePopoverSide(), Align: ui.Start, Width: width, InitialFocusID: hitNotePicker,
	}, &open, func() {
		ui.Row(c).Gap(6).Children(func() {
			p.picker.Bind(&v.picker)
			field := ui.TextFieldID(c, hitNotePicker, &p.picker, s.noteText("Search or add", "搜索或添加"), s.noteText("Search or add", "搜索或添加")).
				Height(30).Grow(1)
			if field.Submitted() && hasNew {
				c.After(func() { s.click(hitNoteTagSubmit, true) })
			}
			add := ui.Button(c, s.noteText("Add", "新增")).Outline().Height(30).Shrink(0).Hit(hitNoteTagSubmit, s.noteText("Add", "新增"))
			if !hasNew {
				add.Disabled(true)
			}
		})
		var area ui.Box
		if queryChanged {
			area = ui.ScrollAt(c, 0)
		} else {
			area = ui.Scroll(c)
		}
		area.Height(max(0, contentHeight-2*m.PadSurface-38)).FillWidth().Scrollbars()
		area.Children(func() {
			ui.Flow(c).Gap(6).LineGap(6).FillWidth().Children(func() {
				for _, item := range items {
					label := item.label
					active := slices.Contains(selected, item.tag)
					if active {
						label = "✓ " + label
					}
					button := ui.Button(c, label).Height(28).Hit(hitNoteTagRows.At(s.noteTagToken(item.tag)), item.tag)
					if active {
						button.Secondary()
					} else {
						button.Outline()
					}
				}
			})
		})
	})
	if !open {
		v.popup = ""
	}
}

var hitNotePickerScroll = tree.ID(notesHits, "picker-scroll")
