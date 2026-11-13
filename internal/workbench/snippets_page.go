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
	if state.clickSnippetFilter(id) {
		return true
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
	header, rows, filter, remove, edit, send, add, empty ui.Box
	removeWidth, sendWidth, editWidth, addWidth          float32
	removeIcon, sendIcon, editIcon, addIcon              uint8
	condensed                                            bool
	// rowsDraw paints the rows inside the scroll area's content slot. It is kept
	// so a paint does not build a closure for the hook.
	rowsDraw      func(*ui.DrawContext)
	rowsChild     func()
	c             ui.C
	contentHeight float32
	rowCells      [4]gfx.Rect
	// The table's columns and sortable headings are fitted once per declaration
	// for the header and the rows alike.
	columns             [4]components.TableColumn
	headings            [4]string
	headers             [4]components.TableHeader
	keyWidth, keyHeight float32
	manualOrder         bool
	// draw is the page hook that paints the fixed header, filter and empty state.
	draw func(*ui.DrawContext)
}

// declareSnippetsWorkspace declares the snippets page in the workbench page: its
// header, its open/empty body and its footer. The buttons, the table, the group
// filter and the empty state's icon are painted into the slots it leaves.
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
	draw, rowsDraw, rowsChild := view.draw, view.rowsDraw, view.rowsChild
	*view = snippetsSectionUI{
		removeWidth: removeWidth, sendWidth: sendWidth, editWidth: editWidth, addWidth: addWidth,
		removeIcon: removeIcon, sendIcon: sendIcon, editIcon: editIcon, addIcon: addIcon,
		condensed: condensed, rowsDraw: rowsDraw, rowsChild: rowsChild,
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
			view.empty = state.reserveEmptyPage(c, bounds, m.SnippetEmpty, m.SnippetEmptySub, m.SnippetAdd, hitSnippetAdd, iconNote, canAdd)
		} else {
			if len(form.groups) > 0 {
				ui.Row(c).Height(toolbarHeight).MarginSides(0, 0, 9, 0).Children(func() {
					view.filter = ui.Slot(c).Size(220, toolbarHeight)
				})
			}
			theme := state.Theme()
			view.keyHeight = theme.Metrics.FontSizeS + 9
			view.keyWidth = max(canvas.TextWidthWeight(enterKeyLabel, theme.Metrics.FontSizeS, gfx.WeightMedium)+8, view.keyHeight)
			view.manualOrder = form.sort.Direction == components.TableSortNone && form.filter.all() && len(form.list) > 1
			nameInset := float32(0)
			if view.manualOrder {
				nameInset = 24
			}
			view.columns = [...]components.TableColumn{
				{Width: 200, Inset: nameInset},
				{Width: 140},
				{Width: 150},
				{Width: 0, Inset: view.keyWidth + 6, EndInset: snippetExpandSize + theme.Metrics.GapItem},
			}
			view.headings = [...]string{m.SnippetColumnName, m.SnippetColumnGroup, m.SnippetColumnShortcut, m.SnippetColumnCommand}
			for index, heading := range view.headings {
				view.headers[index] = components.TableHeader{ID: hitSnippetSort.At(index), Label: heading, Description: m.SnippetSortAction}
				if form.sort.Direction != components.TableSortNone && form.sort.Column == index {
					view.headers[index].Sort = form.sort.Direction
					if form.sort.Direction == components.TableSortAscending {
						view.headers[index].Description = m.SnippetSortAscending
					} else {
						view.headers[index].Description = m.SnippetSortDescending
					}
				}
			}
			components.FitSortableTableHeadings(canvas, theme, view.columns[:], view.headers[:])
			rowWidth := max(bounds.Width-36-catalogBarGutter, 0)
			_, lineHeight, detailHeight := state.snippetDetailHeight(canvas, rowWidth-2*snippetDetailPadding)
			form.detailLineHeight, form.detailHeight = lineHeight, detailHeight
			contentHeight := float32(len(form.visible))*components.TableRowHeight(theme) + detailHeight
			view.header = ui.Slot(c).Height(components.TableHeaderHeight(theme)).MarginRight(catalogBarGutter)
			if view.rowsDraw == nil {
				view.rowsDraw = func(d *ui.DrawContext) { state.paintSnippetRows(d) }
			}
			view.c, view.contentHeight = c, contentHeight
			if view.rowsChild == nil {
				view.rowsChild = func() {
					ui.Slot(view.c).FillWidth().Height(view.contentHeight).Draw(view.rowsDraw)
				}
			}
			view.rows = ui.Scroll(c).Grow(1).PadRight(catalogBarGutter).Scrollbars().Label(m.SnippetsTitle)
			view.rows.Children(view.rowsChild)
		}
		ui.Divider(c).Background(pal.line)
		ui.Row(c).Height(50).PadSides(8, 0, 20, 0).Items(ui.Center).Children(func() {
			ui.Text(c, form.countText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).Width(162)
			if len(form.list) > 0 {
				ui.Text(c, form.list[form.selected].Name).FontSize(metrics.FontSizeS).Color(pal.muted).Grow(1).Ellipsis()
			}
		})
	})
	body.Clip().DrawOver(view.draw)
}

// paintSnippetsWorkspace paints the snippets page's fixed header, group filter
// and empty state. The rows are painted by paintSnippetRows inside the scroll
// area's content.
func (state *State) paintSnippetsWorkspace(canvas *gfx.Canvas) {
	form := &state.snippets
	view := &state.sectionBodies.snippets

	form.sendAt = gfx.Point{X: view.send.Rect().X, Y: view.send.Rect().Y + toolbarHeight + 4}

	if len(form.list) == 0 {
		state.drawReservedEmptyPage(canvas, view.empty, iconNote)
		return
	}
	head := components.BeginTable(canvas, state.Theme(), view.header.Rect(), view.columns[:])
	head.SortableHeader(view.headers[:])
	if view.filter.Valid() {
		state.paintSnippetFilter(canvas, view.filter.Rect())
	}
}

// paintSnippetRows paints the rows inside the scroll area's content slot. Only
// the rows the viewport shows are painted and take hits.
func (state *State) paintSnippetRows(d *ui.DrawContext) {
	canvas := d.Canvas
	form, view := &state.snippets, &state.sectionBodies.snippets
	theme := state.Theme()
	rowHeight := components.TableRowHeight(theme)
	viewport := view.rows.Rect()
	viewportBottom := viewport.Y + viewport.Height
	var grid components.ReorderGrid
	if view.manualOrder {
		grid = components.BeginReorderGrid(canvas, theme, viewport, &form.grid)
		for index := range form.list {
			grid.Order(hitSnippetHandles.At(index), 0, index, false)
		}
	} else {
		form.grid.Cancel()
	}
	holdingBar := false
	if scroll := view.rows.ScrollState(); scroll != nil {
		holdingBar = scroll.Dragging()
	}
	hovered := canvas.Hovered()
	hoverRow, hoverAnchor := 0, gfx.Rect{}
	y := d.Rect.Y
	for _, index := range form.visible {
		expanded := form.expanded == index+1 && form.detailHeight > 0
		height := rowHeight
		if expanded {
			height += form.detailHeight
		}
		if y+height <= viewport.Y || y >= viewportBottom {
			y += height
			continue
		}
		rowID := hitSnippetRowList.At(index)
		if holdingBar {
			// The press owns the pointer while the bar is dragged, so the row
			// under it neither highlights nor takes a click.
			rowID = gfx.ID{}
		}
		table := components.BeginTable(canvas, theme,
			gfx.Rect{X: d.Rect.X, Y: y, Width: d.Rect.Width, Height: height}, view.columns[:])
		row := table.RowLayout(rowID, form.list[index].cells[:], components.TableRowState{
			Selected: index == form.selected,
			Expanded: form.expanded == index+1,
		}, view.rowCells[:0])
		if view.manualOrder && row.Bounds.Height > 0 {
			grid.Item(hitSnippetHandles.At(index), form.list[index].Name, 0, index, row.Bounds,
				gfx.Rect{X: row.Bounds.X + 7, Y: row.Bounds.Y + (row.Bounds.Height-24)/2, Width: 24, Height: 24}, false)
		}
		if !holdingBar {
			if clicked, ok := canvas.ClickedIn(hitSnippetRowList); ok && clicked == index && state.modal == modalNone {
				form.selected = index
			}
		}
		if len(row.Cells) == len(view.rowCells) {
			command := row.Cells[3]
			if form.list[index].Run {
				components.Kbd(canvas, theme, gfx.Rect{X: command.X + theme.Metrics.GapItem, Y: command.Y + (command.Height-view.keyHeight)/2, Height: view.keyHeight}, enterKeyLabel)
			}
			state.paintSnippetExpander(canvas, index, command)
			if expanded {
				state.paintSnippetDetail(canvas, &table, index, row.Bounds, form.detailLines, form.detailLineHeight)
			}
			if !holdingBar && hovered == rowID && !expanded && strings.Contains(form.list[index].Command, "\n") {
				hoverRow, hoverAnchor = index+1, command
			}
		}
		state.sendSnippetOn(canvas, rowID, index)
		state.openSnippetMenuOn(canvas, rowID, index)
		y += height
	}
	if view.manualOrder {
		grid.End()
	}
	state.updateSnippetHover(canvas, hoverRow, hoverAnchor)
}

// paintSnippetExpander draws the chevron that opens one row's command. It sits
// in the command cell's trailing inset, so the cell's text never runs under it.
func (state *State) paintSnippetExpander(canvas *gfx.Canvas, index int, cell gfx.Rect) {
	theme, m := state.Theme(), state.messages.Tools
	size := min(snippetExpandSize, cell.Height-4)
	if size < 12 || cell.Width < size {
		return
	}
	open := state.snippets.expanded == index+1
	hint, glyph := m.SnippetExpand, iconChevronRight
	if open {
		hint, glyph = m.SnippetCollapse, iconChevronDown
	}
	bounds := gfx.Rect{
		X:      cell.Right() - theme.Metrics.GapItem - size,
		Y:      cell.Y + (cell.Height-size)/2,
		Width:  size,
		Height: size,
	}
	state.paintIconButtonWithOptions(canvas, hitSnippetExpandList.At(index), glyph, bounds, hint, false, iconButtonQuiet, gfx.Color{}, 0,
		gfx.HitOptions{Expanded: open, Expandable: true, Description: state.snippets.list[index].Name})
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

// paintSnippetDetail paints the expanded command under its row, in the
// terminal's fixed-pitch face, capped at snippetDetailLines so one long command
// cannot push every other row out of view. lines and lineHeight come from
// snippetDetailHeight, which sized the row for them.
func (state *State) paintSnippetDetail(canvas *gfx.Canvas, table *components.Table, index int, rowBounds gfx.Rect, lines []string, lineHeight float32) {
	form := &state.snippets
	entry := &form.list[index]
	theme := state.Theme()
	style := gfx.TextStyle{Size: theme.Metrics.FontSizeS, Mono: true, MonoFace: state.terminalFontFace, Color: theme.Foreground}
	width := max(0, rowBounds.Width-2*snippetDetailPadding)
	height := 2*snippetDetailPadding + float32(len(lines))*lineHeight
	detail := table.Detail(height)
	if detail == (gfx.Rect{}) {
		return
	}
	canvas.Clip(detail)
	for line := range lines {
		canvas.Label(lines[line], gfx.Rect{
			X: detail.X + snippetDetailPadding, Y: detail.Y + snippetDetailPadding + float32(line)*lineHeight,
			Width: width, Height: lineHeight,
		}, style)
	}
	canvas.Unclip()
	canvas.ContainerWithOptions(detail, gfx.RoleGroup, entry.Name, gfx.HitOptions{Description: entry.Command})
}

// updateSnippetHover opens the command preview after the pointer has rested on a
// multi-line row and keeps it open while the pointer is on the card. It mirrors
// components.HoverTooltip: the canvas asks for one frame at the deadline, so no
// timer outlives the hover.
func (state *State) updateSnippetHover(canvas *gfx.Canvas, row int, anchor gfx.Rect) {
	form := &state.snippets
	if state.activeSection != sectionSnippets || state.dialogOrMenuOpen() || !canvas.Pressed().IsZero() || form.grid.Dragging() {
		form.hoverTarget, form.hoverOpen, form.hoverRow = gfx.ID{}, false, 0
		return
	}
	if canvas.Hovered() == hitSnippetHoverCard && form.hoverOpen && form.hoverRow > 0 {
		return
	}
	if row <= 0 {
		form.hoverTarget, form.hoverOpen, form.hoverRow = gfx.ID{}, false, 0
		return
	}
	target := hitSnippetRowList.At(row - 1)
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

// paintSnippetHoverCard paints the preview the pointer opened. It runs in the
// overlay pass: inside the page it would be clipped to the snippets card.
func (state *State) paintSnippetHoverCard(canvas *gfx.Canvas, viewport gfx.Rect) {
	form := &state.snippets
	if !form.hoverOpen || form.hoverRow <= 0 || form.hoverRow > len(form.list) ||
		state.activeSection != sectionSnippets || state.dialogOrMenuOpen() ||
		!canvas.Pressed().IsZero() || form.grid.Dragging() {
		return
	}
	entry := &form.list[form.hoverRow-1]
	theme := state.Theme()
	style := gfx.TextStyle{Size: theme.Metrics.FontSizeS, Mono: true, MonoFace: state.terminalFontFace, Color: theme.PopoverForeground}
	_, lineHeight := canvas.MonoCellSize(style.Size, style.MonoFace)
	lineHeight = max(lineHeight, style.Size+4)
	width := max(0, snippetCardWidth-2*theme.Metrics.PadSurface)
	lines := canvas.WrapLinesStyle(form.cardLines[:0], entry.Command, style, width, snippetCardLines, &form.cardCache)
	form.cardLines = lines
	card := components.BeginHoverCard(canvas, theme, viewport, form.hoverAnchor, components.HoverCardOptions{
		Layer: hoverCardLayer, ID: hitSnippetHoverCard, Side: components.HoverCardBottom,
		Align: components.HoverCardAlignStart, ContentSize: gfx.Size{Width: snippetCardWidth},
		BodyHeight: float32(len(lines)) * lineHeight,
	})
	layout := card.Begin(entry.Name, "")
	canvas.Clip(layout.Body)
	for line := range lines {
		canvas.Label(lines[line], gfx.Rect{
			X: layout.Body.X, Y: layout.Body.Y + float32(line)*lineHeight,
			Width: layout.Body.Width, Height: lineHeight,
		}, style)
	}
	canvas.Unclip()
	card.End()
}
