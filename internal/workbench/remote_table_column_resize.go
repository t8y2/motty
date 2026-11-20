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
	dragging bool
	divider  int
	grab     float32
}

// Keep the final, flexible column reachable in a narrow window. Fit a copy of
// the chosen widths so widening the window restores the user's original sizes.
func fitRemoteTableColumns(columns []components.TableColumn, width float32) {
	if len(columns) < 2 || width <= 0 {
		return
	}
	minimum := min(remoteTableColumnMinimum, width/float32(len(columns)))
	available := max(width-minimum*2, minimum*float32(len(columns)-1))
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
		Minimums(resize.minimums[:resize.count]).Dividers(resize.ids[:resize.count-1]).HandleSize(8)
}

// Dividers paint after the sort hits so a drag cannot activate either header.
func (state *State) paintRemoteTableColumnResize(d *ui.DrawContext) {
	view := &state.remote
	resize := &view.columnResize
	if resize.tab != view.paintTab {
		resize.dragging = false
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
		resize.dragging, resize.divider = true, divider
		resize.grab = event.X - resize.dividers[divider].X
	case gfx.PointerMove, gfx.PointerUp:
		if !resize.dragging {
			return gfx.EventResult{}
		}
		edit, valid := resize.component(nil, gfx.Theme{}).Edit(resize.divider, event.X-resize.grab)
		if valid {
			chosen := &view.columnWidths[resize.tab]
			chosen.widths, chosen.chosen = resize.widths, true
			chosen.widths[resize.divider], chosen.widths[resize.divider+1] = edit.Left, edit.Right
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
