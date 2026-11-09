package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/workbench/icons"
)

func (s *State) noteHeaderIcon(c *gfx.Canvas, id gfx.ID, b gfx.Rect, label string, icon gfx.Icon, selected bool) {
	components.BeginButton(c, s.Theme(), id, b).Height(28).WithIcon(icon).Icon().Ghost(label)
	hits := c.Hits()
	if len(hits) > 0 && hits[len(hits)-1].ID == id {
		hits[len(hits)-1].Selected = selected
	}
}

func (s *State) paintNoteLibraryHeader(c *gfx.Canvas, b gfx.Rect, x, y, right float32) float32 {
	n, v := s.noteContent(), &s.noteContent().view
	theme := s.Theme()
	actionsWidth := float32(198)
	hasImages := len(noteImageURLs(n.body.String())) > 0
	if hasImages {
		actionsWidth += 34
	}
	actionsX, actionsY := right-actionsWidth, y
	titleRight := actionsX - 12
	stacked := titleRight-x < 120
	if stacked {
		titleRight = right
		actionsY += 36
	}
	// The title is a field with no surface: it reads as the page's own heading until it is
	// edited, so only its content is painted, with the text where the field's would sit.
	titleTheme := theme.WithFontSize(16)
	title := gfx.Rect{X: x, Y: y, Width: max(30, titleRight-x), Height: 28}
	inset := titleTheme.Metrics.TextInsetX()
	titleText := gfx.Rect{X: title.X + inset, Y: title.Y, Width: max(title.Width-2*inset, 0), Height: title.Height}
	components.PaintInputContent(c, titleTheme, title, titleText, components.InputSnapshot{ID: hitNoteTitle, Label: s.noteText("Title", "标题"), Value: n.title.String(), Placeholder: s.noteText("Untitled", "无标题"), Focused: c.Focused() == hitNoteTitle, PlatformEditing: c.Focused() == hitNoteTitle})
	s.noteHeaderIcon(c, hitNoteTagsOpen, gfx.Rect{X: actionsX, Y: actionsY, Width: 28, Height: 28}, s.noteText("Tags", "标签"), icons.Tags, false)
	s.noteHeaderIcon(c, hitNoteHostsOpen, gfx.Rect{X: actionsX + 34, Y: actionsY, Width: 28, Height: 28}, s.noteText("Linked hosts", "关联主机"), icons.Server, false)
	pinIcon, pinLabel := icons.Star, s.noteText("Pin note", "置顶笔记")
	if n.draft.Pinned {
		pinIcon, pinLabel = icons.StarSolid, s.noteText("Unpin note", "取消置顶")
	}
	s.noteHeaderIcon(c, hitNotePin, gfx.Rect{X: actionsX + 68, Y: actionsY, Width: 28, Height: 28}, pinLabel, pinIcon, n.draft.Pinned)
	groupX := actionsX + 102
	if hasImages {
		s.noteHeaderIcon(c, hitNoteLoadImages, gfx.Rect{X: groupX, Y: actionsY, Width: 28, Height: 28}, s.noteText("Load images", "加载图片"), icons.Image, false)
		groupX += 34
	}
	group := gfx.Rect{X: groupX, Y: actionsY, Width: 96, Height: 28}
	modes := []struct {
		mode  noteMode
		token int
		icon  gfx.Icon
	}{
		{noteSource, 0, icons.CodeXml}, {noteSplit, 2, icons.Columns2}, {notePreview, 1, icons.Eye},
	}
	options := make([]components.ButtonGroupOption, 0, len(modes))
	for _, item := range modes {
		variant := components.ButtonOutline
		if v.mode == item.mode {
			variant = components.ButtonSecondary
		}
		options = append(options, components.ButtonGroupOption{ID: hitNoteModes.At(item.token), Variant: variant})
	}
	firstHit := len(c.Hits())
	components.BeginButtonGroup(c, theme.WithControlHeight(28), group, options).Paint()
	for i, item := range modes {
		id := hitNoteModes.At(item.token)
		rect := gfx.Rect{X: group.X + float32(i)*32, Y: group.Y, Width: 32, Height: 28}
		c.DrawIcon(item.icon, gfx.Rect{X: rect.X + 8, Y: rect.Y + 6, Width: 16, Height: 16}, s.pal.text)
		// Keep the group's native interaction and one focus stop per action while
		// supplying the names hidden by the icon-only presentation.
		for j := firstHit; j < len(c.Hits()); j++ {
			hit := &c.Hits()[j]
			if hit.ID == id {
				hit.Label = s.noteModeLabel(item.mode)
				hit.Hint = hit.Label
				hit.Selected = v.mode == item.mode
			}
		}
	}

	y += 40
	if stacked {
		y += 36
	}
	if n.windowView {
		return y
	}
	separator := func() {
		components.PaintSeparator(c, theme, gfx.Rect{X: b.X + 16, Y: y - 5, Width: max(0, b.Width-32), Height: 1}, components.SeparatorHorizontal)
	}
	separator()
	if len(noteWords(n.tags.String())) > 0 {
		y += s.paintNoteLibraryTags(c, gfx.Rect{X: b.X + 20, Y: y, Width: max(0, b.Width-40), Height: 28}) + 9
		separator()
	}
	return y
}
