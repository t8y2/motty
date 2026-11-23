package workbench

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// refreshSnippets makes what the page shows of each snippet and the count under
// it, after the list or the language changed.
func (state *State) refreshSnippets() {
	form := &state.snippets
	for index := range form.list {
		entry := &form.list[index]
		command := entry.Command
		shortcut := ""
		if entry.Shortcut.Valid() {
			shortcut = chordLabel(entry.Shortcut)
		}
		entry.cells = [4]string{entry.Name, entry.Group, shortcut, strings.ReplaceAll(command, "\n", " ↵ ")}
	}
	form.selected = max(0, min(form.selected, len(form.list)-1))
	form.refreshSnippetGroups()
	form.syncRowSelection()
	form.countText = fmt.Sprintf(state.messages.Tools.SnippetCountFmt, len(form.list))
	if len(form.visible) != len(form.list) {
		form.countText = fmt.Sprintf(state.messages.Tools.SnippetCountFilteredFmt, len(form.visible), len(form.list))
	}
}

// enterKeyLabel is the keycap of a snippet that presses Enter after it is sent.
const enterKeyLabel = "↵"

const (
	// snippetExpandSize is the square of the chevron that opens a row's command.
	snippetExpandSize = float32(24)
	// snippetDetailLines caps the expanded command; the last line is ellipsized.
	// The editor dialog remains the place to see the whole command.
	snippetDetailLines = 10
	// snippetDetailPadding is the room around the expanded command.
	snippetDetailPadding = float32(12)
	// snippetCardLines and snippetCardWidth size the pointer's command preview.
	snippetCardLines = 12
	snippetCardWidth = float32(420)
	// snippetHoverDelay is how long the pointer rests on a row before its
	// preview opens. The canvas asks for one frame at the deadline; no timer or
	// goroutine stays behind.
	snippetHoverDelay = 400 * time.Millisecond
)

func (state *State) clickSnippetsPage(id gfx.ID) bool {
	if state.activeSection != sectionSnippets || state.modal != modalNone {
		return false
	}
	if id.Scope() == hitSnippetExpandList {
		index := item(id)
		if index >= 0 && index < len(state.snippets.list) {
			// The chevron shadows the row only inside its own square, so a
			// double-click on it toggles twice rather than sending the snippet.
			if state.snippets.expanded == index+1 {
				state.snippets.expanded = 0
			} else {
				state.snippets.expanded = index + 1
			}
			state.snippets.selected = index
			state.snippets.hoverTarget, state.snippets.hoverOpen, state.snippets.hoverRow = gfx.ID{}, false, 0
		}
		return true
	}
	if id.Scope() == hitSnippetSort {
		column := item(id)
		if column >= 0 && column < len(snippet{}.cells) && len(state.snippets.list) > 0 {
			if state.snippets.sort.Column == column && state.snippets.sort.Direction == components.TableSortDescending {
				state.snippets.sort = components.TableSortState{}
			} else {
				state.snippets.sort = state.snippets.sort.Toggle(column)
			}
			state.refreshSnippets()
		}
		return true
	}
	switch {
	case id == hitSnippetAdd:
		state.openSnippetDialog(-1)
	case id == hitSnippetGroups:
		state.openSnippetGroupsManager()
	case id == hitSnippetSend:
		state.sendSnippetFromPage(state.snippets.selected, state.snippets.sendAt)
	case id == hitSnippetEdit:
		state.openSnippetDialog(state.snippets.selected)
	case id == hitSnippetRemove:
		state.openSnippetDelete(state.snippets.selected)
	default:
		return false
	}
	return true
}

// snippetsSectionUI is what the snippets page's declaration leaves for its
// paint.
type snippetsSectionUI struct {
	rows, filter, groups, remove, edit, send, add, empty ui.Box
	removeWidth, sendWidth, editWidth, addWidth          float32
	removeIcon, sendIcon, editIcon, addIcon              uint8
	condensed                                            bool
	// widths are the three fixed columns' widths; the command column takes the
	// rest. The header and the rows are declared with the same numbers.
	widths      [3]float32
	manualOrder bool
	// keyWidth and commandWidth are the run keycap's inset and the room the
	// command column has, so its text and expander land where the table put them.
	keyWidth, commandWidth float32
	// draw is the page hook that paints the fixed footer position and empty state.
	draw func(*ui.DrawContext)
}

// declareSnippetsWorkspace declares the snippets page in the workbench page: its
// header, its open/empty body and its footer. The rows are a page List, the
// group filter a page Select and the empty state's icon painted by hand.
func (state *State) declareSnippetsWorkspace(c ui.C) {
	canvas := c.Canvas()
	pal, m := state.pal, state.messages.Tools
	form := &state.snippets
	bounds := state.catalogSectionBounds()
	// The title keeps a readable column beside the buttons; a card too narrow for
	// them shows the buttons as their icons, whose tooltips name them.
	const (
		titleLeast = float32(220)
		gap        = float32(10)
	)
	labeled := toolbarButtonWidth(canvas, m.SnippetDelete, false) + toolbarButtonWidth(canvas, m.SnippetEdit, false) +
		toolbarButtonWidth(canvas, m.SnippetSend, true) + toolbarButtonWidth(canvas, m.SnippetAdd, true) + 3*gap + gap + titleLeast
	condensed := labeled > bounds.Width-36
	removeWidth, removeIcon := toolbarButtonLayout(canvas, m.SnippetDelete, 0, iconTrash, condensed)
	sendWidth, sendIcon := toolbarButtonLayout(canvas, m.SnippetSend, iconPlay, iconPlay, condensed)
	editWidth, editIcon := toolbarButtonLayout(canvas, m.SnippetEdit, 0, iconPencil, condensed)
	addWidth, addIcon := toolbarButtonLayout(canvas, m.SnippetAdd, iconPlus, iconPlus, condensed)

	canAdd := len(form.list) < maxSnippets
	hasSelection := len(form.list) > 0
	view := &state.sectionBodies.snippets
	draw := view.draw
	*view = snippetsSectionUI{
		removeWidth: removeWidth, sendWidth: sendWidth, editWidth: editWidth, addWidth: addWidth,
		removeIcon: removeIcon, sendIcon: sendIcon, editIcon: editIcon, addIcon: addIcon,
		condensed: condensed,
	}
	if draw == nil {
		draw = func(d *ui.DrawContext) { state.paintSnippetsWorkspace(d.Canvas) }
	}
	view.draw = draw
	body := ui.Column(c).Fill().PadSides(12, 18, 0, 18)
	body.Children(func() {
		ui.Row(c).MinHeight(58).Gap(gap).MarginSides(0, 0, 12, 0).Items(ui.Start).Children(func() {
			ui.Column(c).Grow(1).Gap(3).MarginSides(4, 0, 0, 0).Children(func() {
				ui.Text(c, m.SnippetsTitle).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis()
				ui.Paragraph(c, m.SnippetsPageDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted)
			})
			view.remove = state.declareButtonSized(c, hitSnippetRemove, m.SnippetDelete, removeIcon, false, false, hasSelection, toolbarFont, condensed).Size(removeWidth, toolbarHeight)
			view.edit = state.declareButtonSized(c, hitSnippetEdit, m.SnippetEdit, editIcon, false, false, hasSelection, toolbarFont, condensed).Size(editWidth, toolbarHeight)
			view.send = state.declareButtonSized(c, hitSnippetSend, m.SnippetSend, sendIcon, false, false, hasSelection, toolbarFont, condensed).Size(sendWidth, toolbarHeight)
			view.add = state.declareButtonSized(c, hitSnippetAdd, m.SnippetAdd, addIcon, true, false, canAdd, toolbarFont, condensed).Size(addWidth, toolbarHeight)
		})
		if len(form.list) == 0 {
			view.empty = state.reserveEmptyPage(c, bounds, m.SnippetEmpty, m.SnippetEmptySub, m.SnippetAdd, hitSnippetAdd, iconSnippets, canAdd)
		} else {
			if len(form.groups) > 0 {
				ui.Row(c).Height(toolbarHeight).MarginSides(0, 0, 9, 0).Gap(9).Children(func() {
					view.filter = state.declareSnippetFilter(c).Size(220, toolbarHeight).Shrink(0)
					groupsWidth := toolbarButtonWidth(canvas, m.SnippetGroupsManage, false)
					view.groups = state.declareButtonSized(c, hitSnippetGroups, m.SnippetGroupsManage, 0, false, false, true, toolbarFont, false).
						Size(groupsWidth, toolbarHeight)
				})
			}
			theme := state.Theme()
			view.manualOrder = form.sort.Direction == components.TableSortNone && form.filter.all() && len(form.list) > 1
			state.fitSnippetColumns(canvas, theme)
			view.keyWidth = max(canvas.TextWidthWeight(enterKeyLabel, theme.Metrics.FontSizeS, gfx.WeightMedium)+8, theme.Metrics.FontSizeS+9)
			rowWidth := max(bounds.Width-36-catalogBarGutter, 0)
			view.commandWidth = max(rowWidth-view.widths[0]-view.widths[1]-view.widths[2], 0)
			_, lineHeight, detailHeight := state.snippetDetailHeight(canvas, rowWidth-2*snippetDetailPadding)
			form.detailLineHeight, form.detailHeight = lineHeight, detailHeight
			state.declareSnippetHeader(c, theme)
			form.prepareSnippetRows(state)
			if view.manualOrder {
				form.rows.Reorder = form.reorder
			} else {
				form.rows.Reorder = nil
			}
			form.selectedRow = slices.Index(form.visible, form.selected)
			form.hoverCandidate, form.hoverCandidateAnchor = 0, ui.Box{}
			view.rows = ui.List(c, &form.rows, len(form.visible), form.rowDecl).
				Grow(1).PadRight(catalogBarGutter).Scrollbars().Label(m.SnippetsTitle)
			state.afterSnippetRows(canvas)
		}
		ui.Divider(c).Background(pal.line)
		ui.Row(c).Height(50).PadSides(8, 0, 20, 0).Items(ui.Center).Children(func() {
			ui.Text(c, form.countText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).Width(162)
			if len(form.list) > 0 {
				ui.Text(c, form.list[form.selected].Name).FontSize(metrics.FontSizeS).Color(pal.muted).Grow(1).Ellipsis()
			}
		})
		state.declareSnippetHoverCard(c)
	})
	body.Clip().DrawOver(view.draw)
}

// fitSnippetColumns fits the table's column widths once for the header and the
// rows: the name, the group and the shortcut are fixed, the command takes what
// is left.
func (state *State) fitSnippetColumns(canvas *gfx.Canvas, theme gfx.Theme) {
	m, view := state.messages.Tools, &state.sectionBodies.snippets
	columns := [...]components.TableColumn{{Width: 200}, {Width: 140}, {Width: 150}, {Width: 0}}
	headers := [...]components.TableHeader{
		{ID: hitSnippetSort.At(0), Label: m.SnippetColumnName},
		{ID: hitSnippetSort.At(1), Label: m.SnippetColumnGroup},
		{ID: hitSnippetSort.At(2), Label: m.SnippetColumnShortcut},
		{ID: hitSnippetSort.At(3), Label: m.SnippetColumnCommand},
	}
	components.FitSortableTableHeadings(canvas, theme, columns[:], headers[:])
	for index := range view.widths {
		view.widths[index] = columns[index].Width
	}
}

// declareSnippetHeader declares the sortable headings as page buttons of the
// list's three fixed columns and its command column. A click reaches the page's
// central sort handler, named by hitSnippetSort.
func (state *State) declareSnippetHeader(c ui.C, theme gfx.Theme) {
	view := &state.sectionBodies.snippets
	ui.Row(c).Height(components.TableHeaderHeight(theme)).FillWidth().MarginRight(catalogBarGutter).Children(func() {
		for index := range view.widths {
			state.declareSnippetHeading(c, theme, index, view.widths[index])
		}
		state.declareSnippetHeading(c, theme, 3, 0)
	})
}

// declareSnippetHeading declares one heading button. width 0 is the command
// column, which takes the rest of the row.
func (state *State) declareSnippetHeading(c ui.C, theme gfx.Theme, column int, width float32) {
	m, form, view := state.messages.Tools, &state.snippets, &state.sectionBodies.snippets
	heading := [...]string{m.SnippetColumnName, m.SnippetColumnGroup, m.SnippetColumnShortcut, m.SnippetColumnCommand}[column]
	direction := components.TableSortNone
	description := m.RemoteSortAction
	if form.sort.Direction != components.TableSortNone && form.sort.Column == column {
		direction = form.sort.Direction
		if direction == components.TableSortAscending {
			description = m.RemoteSortAscending
		} else {
			description = m.RemoteSortDescending
		}
	}
	// The manual order's drag handle indents the name column by its 24px square;
	// the command column keeps the run keycap's inset and the expander's room.
	left, right := theme.Metrics.GapItem, theme.Metrics.GapItem
	if column == 0 && view.manualOrder {
		left += 24
	}
	if column == 3 {
		left += view.keyWidth + 6
		right += 24 + theme.Metrics.GapItem
	}
	button := ui.ButtonBase(c, heading).
		HitWith(hitSnippetSort.At(column), heading, gfx.HitOptions{Role: gfx.RoleButton, Description: description}).
		Height(components.TableHeaderHeight(theme)).PadLeft(left).PadRight(right).
		Justify(ui.Start).Items(ui.Center).
		Hover(ui.Look{}.Background(theme.Hover)).
		Focus(ui.Look{}.Ring(1, theme.Ring))
	if width > 0 {
		button.Width(width)
	} else {
		button.Grow(1)
	}
	button.Children(func() {
		ui.Text(c, heading).Grow(1).FontSize(theme.Metrics.FontSizeM).Weight(gfx.WeightMedium).
			Color(theme.Foreground).Ellipsis()
		icon := remoteTableUnsortedIcon
		switch direction {
		case components.TableSortAscending:
			icon = remoteTableAscendingIcon
		case components.TableSortDescending:
			icon = remoteTableDescendingIcon
		}
		ui.Icon(c, icon).Size(14, 14).Color(theme.Foreground).Shrink(0)
	})
}

// afterSnippetRows reads back the list's choice and times the pointer's rest on
// a multi-line row, once the rows are declared.
func (state *State) afterSnippetRows(canvas *gfx.Canvas) {
	form := &state.snippets
	if form.rows.Changed() {
		if selected := form.rows.Selected(); selected >= 0 && selected < len(form.visible) {
			form.selected = form.visible[selected]
		}
	} else if want := slices.Index(form.visible, form.selected); want >= 0 && form.rows.Selected() != want {
		form.rows.Select(want)
		// The rows were declared before the list could be told the choice: ask for
		// the paint that shows it, as the first one after the page opens does.
		canvas.RequestFrame()
	}
	state.updateSnippetHover(canvas, form.hoverCandidate, form.hoverCandidateAnchor)
}

// paintSnippetsWorkspace paints the snippets page's fixed footer position and
// empty state. The rows are page elements now.
func (state *State) paintSnippetsWorkspace(canvas *gfx.Canvas) {
	form := &state.snippets
	view := &state.sectionBodies.snippets
	form.sendAt = gfx.Point{X: view.send.Rect().X, Y: view.send.Rect().Y + toolbarHeight + 4}
	if len(form.list) == 0 {
		state.drawReservedEmptyPage(canvas, view.empty, iconSnippets)
	}
}

// declareSnippetRow declares one snippet row in the page's list: the name, the
// group, the shortcut keycap and the command with its run keycap and expander,
// plus the expanded command under them. The row is the drag target of the
// manual order and takes the pointer's click through the List.
func (state *State) declareSnippetRow(row ui.ListRow) {
	form, view := &state.snippets, &state.sectionBodies.snippets
	m, theme := state.messages.Tools, state.Theme()
	c := row.C
	canvas := c.Canvas()
	index := form.visible[row.Index]
	entry := &form.list[index]
	pad := theme.Metrics.GapItem
	rowHeight := components.TableRowHeight(theme)
	expanded := form.expanded == index+1 && form.detailHeight > 0
	height := rowHeight
	if expanded {
		height += form.detailHeight
	}
	hovered := canvas.Hovered() == row.ID
	selected := row.Selected()
	if !form.rows.Changed() && form.selectedRow == row.Index {
		selected = true
	}
	paintBackground := false
	background := theme.Background
	switch {
	case selected:
		background, paintBackground = theme.Muted, true
	case expanded:
		background, paintBackground = theme.Hover, true
	case hovered:
		background, paintBackground = theme.Hover, true
	}
	row.Element.Height(height).FillWidth()
	row.Element.Children(func() {
		ui.Column(c).FillWidth().Children(func() {
			header := ui.Row(c).Height(rowHeight).FillWidth().Items(ui.Center)
			if paintBackground {
				// The old table's row surface covered the 36px row only; the
				// expanded band below it stays on the page background.
				header.Background(background)
			}
			header.Children(func() {
				nameLeft := pad
				if view.manualOrder {
					nameLeft += 24
				}
				// The headings fix these three columns' widths. A command wider
				// than its column is ellipsized there; without Shrink(0) its
				// unwrapped width would shrink these cells instead and shift
				// every later column in that row by the command's length (#1152).
				ui.Text(c, entry.cells[0]).Width(view.widths[0]).PadLeft(nameLeft).PadRight(pad).
					FontSize(theme.Metrics.FontSizeM).Color(theme.Foreground).Ellipsis().Shrink(0)
				ui.Text(c, entry.cells[1]).Width(view.widths[1]).PadX(pad).
					FontSize(theme.Metrics.FontSizeM).Color(theme.Foreground).Ellipsis().Shrink(0)
				ui.Row(c).Width(view.widths[2]).PadX(pad).Items(ui.Center).Shrink(0).Children(func() {
					if entry.Shortcut.Valid() {
						ui.Kbd(c, entry.cells[2])
					}
				})
				ui.Row(c).Grow(1).PadLeft(pad + view.keyWidth + 6).PadRight(pad).
					Items(ui.Center).Gap(pad).Children(func() {
					if entry.Run {
						// The run keycap stays at the cell's leading edge; the
						// text starts past the column's key inset, as the table's
						// command cell did.
						ui.Kbd(c, enterKeyLabel).Absolute().Left(pad).
							Top((snippetExpandSize - theme.Metrics.FontSizeS - 9) / 2).
							Height(theme.Metrics.FontSizeS + 9)
					}
					ui.Text(c, entry.cells[3]).Grow(1).
						FontSize(theme.Metrics.FontSizeM).Color(theme.Foreground).Ellipsis()
					// A command cell too narrow for the chevron drops it, as the
					// old painter did instead of squeezing the text.
					if view.commandWidth >= snippetExpandSize {
						open := form.expanded == index+1
						hint, glyph := m.SnippetExpand, iconChevronRight
						if open {
							hint, glyph = m.SnippetCollapse, iconChevronDown
						}
						state.declareIconButtonWithOptions(c, hitSnippetExpandList.At(index), glyph, hint,
							false, iconButtonQuiet, 0, gfx.HitOptions{
								Expanded: open, Expandable: true, Description: entry.Name,
							}).Size(snippetExpandSize, snippetExpandSize)
					}
				})
				if !expanded && hovered && strings.Contains(entry.Command, "\n") {
					// The preview aligns 4px in from the command cell, as the old
					// hover card's default align offset did, and hangs from the
					// row's bottom edge whatever the cell's own height.
					commandX := view.widths[0] + view.widths[1] + view.widths[2]
					marker := ui.Slot(c).Absolute().Left(commandX + 4).Top(0).Width(1).Height(rowHeight)
					form.hoverCandidate, form.hoverCandidateAnchor = index+1, marker
				}
			})
			if expanded {
				form.detailName, form.detailCommand = entry.Name, entry.Command
				detail := ui.Column(c).FillWidth().Height(form.detailHeight).
					Background(theme.Background).
					PadX(snippetDetailPadding).PadY(snippetDetailPadding)
				detail.Children(func() {
					for _, line := range form.detailLines {
						ui.Text(c, line).Mono().MonoFace(state.terminalFontFace).
							FontSize(theme.Metrics.FontSizeS).Color(theme.Foreground).
							Height(form.detailLineHeight).FillWidth()
					}
				})
				detail.DrawOver(form.detailAccent)
			}
		})
		if view.manualOrder {
			row.Element.DrawOver(form.handleDraw)
		}
	})
	state.sendSnippetOn(canvas, row.ID, index)
	state.openSnippetMenuOn(canvas, row.ID, index)
}

// paintSnippetHandle draws the manual order's six-dot drag handle in the name
// column, at the 24px square the old reorder grid gave it. The page List owns
// the drag; the glyph keeps the place and size it had.
func (state *State) paintSnippetHandle(d *ui.DrawContext) {
	theme := state.Theme()
	handle := gfx.Rect{
		X: d.Rect.X + 7, Y: d.Rect.Y + (components.TableRowHeight(theme)-24)/2,
		Width: 24, Height: 24,
	}
	color := theme.MutedForeground
	const dot = float32(2.5)
	for y := float32(-5); y <= 5; y += 5 {
		for _, x := range [...]float32{-2.5, 2.5} {
			d.Canvas.Rounded(gfx.Rect{
				X:     handle.X + handle.Width/2 + x - dot/2,
				Y:     handle.Y + handle.Height/2 + y - dot/2,
				Width: dot, Height: dot,
			}, dot/2, color)
		}
	}
}

// paintSnippetDetailAccent rules the expanded band off from its row, marks its
// leading edge, and names the band as one region for assistive technology.
func (state *State) paintSnippetDetailAccent(d *ui.DrawContext) {
	theme := state.Theme()
	d.Canvas.Fill(gfx.Rect{X: d.Rect.X, Y: d.Rect.Y, Width: d.Rect.Width, Height: 1}, theme.Border)
	accent := theme.Primary
	accent.Alpha *= 0.7
	d.Canvas.Fill(gfx.Rect{X: d.Rect.X, Y: d.Rect.Y, Width: 3, Height: d.Rect.Height}, accent)
	state.paintSnippetDetailRegion(d)
}

// paintSnippetDetailRegion marks the expanded command as one region named by
// its snippet, with the whole command as its description.
func (state *State) paintSnippetDetailRegion(d *ui.DrawContext) {
	form := &state.snippets
	d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleGroup, form.detailName, gfx.HitOptions{Description: form.detailCommand})
}

// snippetDetailHeight wraps the expanded command at width and returns the lines,
// their line height and the band height the page reserves for them. Nothing is
// measured unless a row the filter shows is expanded.
func (state *State) snippetDetailHeight(canvas *gfx.Canvas, width float32) (lines []string, lineHeight, height float32) {
	form := &state.snippets
	if form.expanded < 1 || form.expanded > len(form.list) || !slices.Contains(form.visible, form.expanded-1) {
		return nil, 0, 0
	}
	theme := state.Theme()
	style := gfx.TextStyle{Size: theme.Metrics.FontSizeS, Mono: true, MonoFace: state.terminalFontFace, Color: theme.Foreground}
	_, lineHeight = canvas.MonoCellSize(style.Size, style.MonoFace)
	lineHeight = max(lineHeight, style.Size+4)
	lines = canvas.WrapLinesStyle(form.detailLines[:0], form.list[form.expanded-1].Command, style,
		max(width, 0), snippetDetailLines, &form.detailCache)
	form.detailLines = lines
	return lines, lineHeight, 2*snippetDetailPadding + float32(len(lines))*lineHeight
}

// updateSnippetHover opens the command preview after the pointer has rested on a
// multi-line row and keeps it open while the pointer is on the card. It mirrors
// components.HoverTooltip: the canvas asks for one frame at the deadline, so no
// timer outlives the hover.
func (state *State) updateSnippetHover(canvas *gfx.Canvas, row int, anchor ui.Box) {
	form := &state.snippets
	if state.activeSection != sectionSnippets || state.dialogOrMenuOpen() || !canvas.Pressed().IsZero() {
		form.hoverTarget, form.hoverOpen, form.hoverRow = gfx.ID{}, false, 0
		form.hoverAnchor = ui.Box{}
		return
	}
	if canvas.Hovered() == hitSnippetHoverCard && form.hoverOpen && form.hoverRow > 0 {
		return
	}
	if row <= 0 || !anchor.Valid() {
		form.hoverTarget, form.hoverOpen, form.hoverRow = gfx.ID{}, false, 0
		form.hoverAnchor = ui.Box{}
		return
	}
	target := form.rows.RowID(slices.Index(form.visible, row-1))
	now := canvas.Now()
	if target != form.hoverTarget {
		form.hoverTarget, form.hoverSince = target, now
		form.hoverOpen, form.hoverRow = false, 0
	}
	if elapsed := now - form.hoverSince; elapsed < snippetHoverDelay {
		canvas.RequestFrameIn(snippetHoverDelay - elapsed)
		return
	}
	form.hoverOpen, form.hoverRow, form.hoverAnchor = true, row, anchor
}

// declareSnippetHoverCard declares the preview the pointer opened over the page,
// anchored on the command cell it describes. The app keeps hoverOpen true while
// the pointer is on either the row or the card.
func (state *State) declareSnippetHoverCard(c ui.C) {
	form := &state.snippets
	if !form.hoverOpen || form.hoverRow <= 0 || form.hoverRow > len(form.list) ||
		state.activeSection != sectionSnippets || state.dialogOrMenuOpen() ||
		!c.Canvas().Pressed().IsZero() || !form.hoverAnchor.Valid() {
		return
	}
	entry := &form.list[form.hoverRow-1]
	theme := state.Theme()
	style := gfx.TextStyle{Size: theme.Metrics.FontSizeS, Mono: true, MonoFace: state.terminalFontFace, Color: theme.PopoverForeground}
	_, lineHeight := c.Canvas().MonoCellSize(style.Size, style.MonoFace)
	lineHeight = max(lineHeight, style.Size+4)
	width := max(0, snippetCardWidth-2*theme.Metrics.PadSurface)
	lines := c.Canvas().WrapLinesStyle(form.cardLines[:0], entry.Command, style, width, snippetCardLines, &form.cardCache)
	form.cardLines = lines
	form.hoverC, form.hoverName, form.hoverLineHeight = c, entry.Name, lineHeight
	if form.hoverBody == nil {
		form.hoverBody = state.declareSnippetHoverBody
	}
	ui.PopoverWith(c, form.hoverAnchor, &form.hoverOpen, ui.PopoverSpec{
		Side: components.PopoverBottom, Align: ui.Start, Offset: 4, KeepOpenOnAnchor: true,
		Title: entry.Name, Width: snippetCardWidth,
	}, form.hoverBody)
}

// declareSnippetHoverBody declares the card's wrapped command. It runs inside the
// popover, and its own hit keeps the preview open while the pointer is on it.
func (state *State) declareSnippetHoverBody() {
	form := &state.snippets
	c := form.hoverC
	theme := state.Theme()
	card := ui.Column(c).FillWidth().Hit(hitSnippetHoverCard, form.hoverName)
	card.Children(func() {
		for _, line := range form.cardLines {
			ui.Text(c, line).Mono().MonoFace(state.terminalFontFace).
				FontSize(theme.Metrics.FontSizeS).Color(theme.PopoverForeground).
				Height(form.hoverLineHeight).FillWidth()
		}
	})
}
