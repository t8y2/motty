package workbench

import (
	"fmt"
	"strings"

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

func (state *State) clickSnippetsPage(id gfx.ID) bool {
	if state.activeSection != sectionSnippets || state.modal != modalNone {
		return false
	}
	if state.clickSnippetFilter(id) {
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
		state.deleteSnippet(state.snippets.selected)
	default:
		return false
	}
	return true
}

// snippetsSectionUI is what the snippets page's declaration leaves for its
// paint.
type snippetsSectionUI struct {
	content, filter, remove, edit, send, add, empty ui.Box
	removeWidth, sendWidth, editWidth, addWidth     float32
	removeIcon, sendIcon, editIcon, addIcon         uint8
	condensed                                       bool
	// draw is the page hook that paints into these slots. It is kept so a paint
	// does not build a closure for the hook.
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
			view.empty = state.reserveEmptyPage(c, bounds, m.SnippetEmpty, m.SnippetEmptySub, m.SnippetAdd, hitSnippetAdd, iconNote, canAdd)
		} else {
			if len(form.groups) > 0 {
				ui.Row(c).Height(toolbarHeight).MarginSides(0, 0, 9, 0).Children(func() {
					view.filter = ui.Slot(c).Size(220, toolbarHeight)
				})
			}
			view.content = ui.Slot(c).Grow(1).MarginSides(0, 0, 9, 0)
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

// paintSnippetsWorkspace paints the snippets page's buttons, table, group filter
// and empty state into the slots its declaration left.
func (state *State) paintSnippetsWorkspace(canvas *gfx.Canvas) {
	m, form := state.messages.Tools, &state.snippets
	view := &state.sectionBodies.snippets

	form.sendAt = gfx.Point{X: view.send.Rect().X, Y: view.send.Rect().Y + toolbarHeight + 4}

	if len(form.list) == 0 {
		state.drawReservedEmptyPage(canvas, view.empty, iconNote)
		return
	}
	// The command column leaves room before its text for the Enter keycap of the
	// snippets that press it, so the commands of all rows start at the same place.
	theme := state.Theme()
	keyHeight := theme.Metrics.FontSizeS + 9
	keyWidth := max(canvas.TextWidthWeight(enterKeyLabel, theme.Metrics.FontSizeS, gfx.WeightMedium)+8, keyHeight)
	manualOrder := form.sort.Direction == components.TableSortNone && form.filter.all() && len(form.list) > 1
	nameInset := float32(0)
	if manualOrder {
		nameInset = 24
	}
	columns := [...]components.TableColumn{{Width: 200, Inset: nameInset}, {Width: 140}, {Width: 150}, {Width: 0, Inset: keyWidth + 6}}
	headings := [...]string{m.SnippetColumnName, m.SnippetColumnGroup, m.SnippetColumnShortcut, m.SnippetColumnCommand}
	var headers [4]components.TableHeader
	for index, heading := range headings {
		headers[index] = components.TableHeader{ID: hitSnippetSort.At(index), Label: heading, Description: m.SnippetSortAction}
		if form.sort.Direction != components.TableSortNone && form.sort.Column == index {
			headers[index].Sort = form.sort.Direction
			if form.sort.Direction == components.TableSortAscending {
				headers[index].Description = m.SnippetSortAscending
			} else {
				headers[index].Description = m.SnippetSortDescending
			}
		}
	}
	components.FitSortableTableHeadings(canvas, state.Theme(), columns[:], headers[:])
	table := components.BeginTable(canvas, state.Theme(), view.content.Rect(), columns[:])
	table.SortableHeader(headers[:])
	var grid components.ReorderGrid
	if manualOrder {
		grid = components.BeginReorderGrid(canvas, theme, view.content.Rect(), &form.grid)
		for index := range form.list {
			grid.Order(hitSnippetHandles.At(index), 0, index, false)
		}
	} else {
		form.grid.Cancel()
	}
	for _, index := range form.visible {
		rowID := hitSnippetRowList.At(index)
		var cellRects [4]gfx.Rect
		row := table.RowLayout(rowID, form.list[index].cells[:], components.TableRowState{Selected: index == form.selected}, cellRects[:0])
		if manualOrder && row.Bounds.Height > 0 {
			grid.Item(hitSnippetHandles.At(index), form.list[index].Name, 0, index, row.Bounds,
				gfx.Rect{X: row.Bounds.X + 7, Y: row.Bounds.Y + (row.Bounds.Height-24)/2, Width: 24, Height: 24}, false)
		}
		if clicked, ok := canvas.ClickedIn(hitSnippetRowList); ok && clicked == index && state.modal == modalNone {
			form.selected = index
		}
		if form.list[index].Run && len(row.Cells) == len(cellRects) {
			command := row.Cells[3]
			components.Kbd(canvas, theme, gfx.Rect{X: command.X + theme.Metrics.GapItem, Y: command.Y + (command.Height-keyHeight)/2, Height: keyHeight}, enterKeyLabel)
		}
		state.sendSnippetOn(canvas, rowID, index)
		state.openSnippetMenuOn(canvas, rowID, index)
	}
	if manualOrder {
		grid.End()
	}
	if view.filter.Valid() {
		state.paintSnippetFilter(canvas, view.filter.Rect())
	}
}
