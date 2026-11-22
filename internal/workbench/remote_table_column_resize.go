package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const remoteTableColumnMinimum = float32(40)

type remoteTableColumnWidths struct {
	widths [remoteMaxColumns]float32
	chosen bool
}

// The header owns the divider events in both the modal and the native window.
// Its geometry is also the geometry used by the sortable header and row cells.
type remoteTableColumnResize struct {
	handlers ui.Handlers
	draw     func(*ui.DrawContext)
	bounds   gfx.Rect
	tab      remoteTab
	count    int
	widths   [remoteMaxColumns]float32
	minimums [remoteMaxColumns]float32
	columns  [remoteMaxColumns]gfx.Rect
	dividers [remoteMaxColumns - 1]gfx.Rect
	hits     [remoteMaxColumns - 1]gfx.Rect
	ids      [remoteMaxColumns - 1]gfx.ID
	labels   [remoteMaxColumns - 1]string
	// fits is each column's content width for a double click, measured when a
	// fit is pending rather than on every paint.
	fits       [remoteMaxColumns]float32
	fitPending bool
	fitDivider int
	dragging   bool
	divider    int
	grab       float32
}

// remoteTableColumnFitMinimum is the least width a column may take at a table
// width: the absolute minimum, unless the table is too narrow to hold one for
// every column.
func remoteTableColumnFitMinimum(width float32, count int) float32 {
	if count <= 0 {
		return 0
	}
	return min(remoteTableColumnMinimum, width/float32(count))
}

// Keep the final, flexible column reachable in a narrow window. Fit a copy of
// the chosen widths so widening the window restores the user's original sizes.
// Exactly one minimum is reserved for the flexible last column, the same floor
// the drag's ColumnResize.Edit enforces.
func fitRemoteTableColumns(columns []components.TableColumn, width float32) {
	if len(columns) < 2 || width <= 0 {
		return
	}
	minimum := remoteTableColumnFitMinimum(width, len(columns))
	available := max(width-minimum, minimum*float32(len(columns)-1))
	total := float32(0)
	for index := 0; index+1 < len(columns); index++ {
		columns[index].Width = max(columns[index].Width, minimum)
		total += columns[index].Width
	}
	if total <= available {
		return
	}
	base := minimum * float32(len(columns)-1)
	scale := max(available-base, 0) / max(total-base, 1)
	for index := 0; index+1 < len(columns); index++ {
		columns[index].Width = minimum + (columns[index].Width-minimum)*scale
	}
}

func (resize *remoteTableColumnResize) component(canvas *gfx.Canvas, theme gfx.Theme) components.ColumnResize {
	return components.BeginColumnResize(canvas, theme, resize.bounds, resize.widths[:resize.count]).
		Minimums(resize.minimums[:resize.count]).Fits(resize.fits[:resize.count]).
		Dividers(resize.ids[:resize.count-1]).HandleSize(8)
}

// remoteChosenWidths is the workbench's chosen widths for a tab. The tools
// dialog and every content window share them, so closing and reopening a
// surface keeps the sizes for the session, as the SFTP panel does.
func (state *State) remoteChosenWidths(tab remoteTab) *remoteTableColumnWidths {
	view := &state.remote
	if view.columnWidths == nil {
		view.columnWidths = &state.remoteColumnWidths
	}
	return &view.columnWidths[tab]
}

// storeRemoteTableColumnWidths keeps the pair a drag or a fit produced, both in
// the workbench's per-tab widths and in this frame's geometry.
func (state *State) storeRemoteTableColumnWidths(resize *remoteTableColumnResize, divider int, edit components.ColumnResizeEdit) {
	chosen := state.remoteChosenWidths(resize.tab)
	chosen.widths, chosen.chosen = resize.widths, true
	chosen.widths[divider], chosen.widths[divider+1] = edit.Left, edit.Right
	resize.widths[divider], resize.widths[divider+1] = edit.Left, edit.Right
}

// remoteTableColumnFitWidth is what a double click on a divider measures: the
// widest header or cell the left column shows, plus the padding and sort arrow
// the column spends before its text.
func (state *State) remoteTableColumnFitWidth(canvas *gfx.Canvas, column int) float32 {
	view, theme := &state.remote, state.Theme()
	size := theme.Metrics.FontSizeM
	longest := float32(0)
	if label := view.tableHeaderCells[column].Label; label != "" {
		longest = canvas.TextWidthWeight(label, size, gfx.WeightMedium) + remoteTableSortIconGap + remoteTableSortIconSize
	}
	// The rows at the top of the list are what the column has to show; measuring
	// a long process list row by row would make the click wait on all of it.
	end := min(len(view.rows), 1000)
	for index := 0; index < end; index++ {
		longest = max(longest, canvas.TextWidth(view.rows[index].cells[column], size))
	}
	return longest + 2*theme.Metrics.GapItem
}

// Dividers paint after the sort hits so a drag cannot activate either header.
func (state *State) paintRemoteTableColumnResize(d *ui.DrawContext) {
	view := &state.remote
	resize := &view.columnResize
	if resize.tab != view.paintTab {
		resize.dragging, resize.fitPending = false, false
	}
	resize.tab, resize.count, resize.bounds = view.paintTab, view.tableCount, d.Rect
	if resize.count < 2 {
		return
	}
	remaining := d.Rect.Width
	for index := 0; index < resize.count; index++ {
		width := view.tableColumns[index].Width
		if index == resize.count-1 {
			width = max(remaining, 0)
		}
		resize.widths[index] = width
		resize.minimums[index] = min(remoteTableColumnMinimum, width)
		remaining -= width
		if index+1 < resize.count {
			resize.ids[index] = resize.handlers.Parts().At(index)
			resize.labels[index] = state.messages.Tools.FilesResizeColumn
		}
	}
	if resize.fitPending {
		resize.fitPending = false
		if divider := resize.fitDivider; divider >= 0 && divider+1 < resize.count {
			resize.fits[divider] = state.remoteTableColumnFitWidth(d.Canvas, divider)
			if edit, ok := resize.component(d.Canvas, *d.Theme).Fit(divider); ok {
				state.storeRemoteTableColumnWidths(resize, divider, edit)
			}
		}
	}
	resize.component(d.Canvas, *d.Theme).Paint(resize.columns[:], resize.dividers[:], resize.hits[:], resize.labels[:])
}

func (state *State) remoteTableColumnPointer(event gfx.PointerEvent) gfx.EventResult {
	view := &state.remote
	resize := &view.columnResize
	divider, ok := event.Target.In(resize.handlers.Parts())
	if !ok || divider < 0 || divider+1 >= resize.count {
		return gfx.EventResult{}
	}
	switch event.Kind {
	case gfx.PointerDown:
		if event.Button != gfx.PointerButtonPrimary {
			return gfx.EventResult{}
		}
		if event.ClickCount >= 2 {
			resize.dragging, resize.fitPending = false, true
			resize.fitDivider = divider
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		resize.dragging, resize.divider = true, divider
		resize.grab = event.X - resize.dividers[divider].X
	case gfx.PointerMove, gfx.PointerUp:
		if !resize.dragging {
			return gfx.EventResult{}
		}
		edit, valid := resize.component(nil, gfx.Theme{}).Edit(resize.divider, event.X-resize.grab)
		if valid {
			state.storeRemoteTableColumnWidths(resize, resize.divider, edit)
		}
		if event.Kind == gfx.PointerUp {
			resize.dragging = false
		}
	case gfx.PointerExit:
		resize.dragging = false
	default:
		return gfx.EventResult{}
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}
