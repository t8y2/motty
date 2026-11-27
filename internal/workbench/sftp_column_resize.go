package workbench

import (
	"github.com/vivaui/viva/gfx"
)

// The file lists' columns: the name takes the room left, and the size and
// modification time have widths the user may drag. The dividers are a Viva
// ColumnResize; this file keeps the state shared by both lists and fits a
// column to its content on a double click.
const (
	sftpColumnMinName     = float32(120)
	sftpColumnMinSize     = float32(54)
	sftpColumnMinModified = float32(76)
	sftpColumnIconWidth   = float32(16)
	sftpColumnNameGap     = float32(8)
)

var sftpColumnMinimums = [...]float32{sftpColumnMinName, sftpColumnMinSize, sftpColumnMinModified}

var sftpColumnResizeHits = tree.Feature("sftp-column-resize")

var (
	hitSFTPPanelColumnDivider = tree.Sub(sftpColumnResizeHits, tree.Name("panel-divider"))
	sftpWindowColumnDividers  = tree.Sub(sftpColumnResizeHits, tree.Name("window-divider"))
)

// measureSFTPColumnText records what the modification time needs at the
// window's current interface font and text size. The column used to be sized
// for one font's metrics; a wider font or a larger interface text size then cut
// the last digits off. The sample uses the layout the rows are formatted with,
// so the measurement and the painted text cannot drift apart.
func (state *State) measureSFTPColumnText(canvas *gfx.Canvas) {
	state.sftpColumnModifiedText = canvas.TextWidth(sftpModifiedLayout, metrics.FontSizeS)
	state.sftpColumnShortText = canvas.TextWidth(sftpModifiedLayout[5:], metrics.FontSizeS)
}

// sftpColumnMetrics is the three widths an SFTP list paints, in pixels.
type sftpColumnMetrics struct {
	name, size, modified float32
}

// sftpColumnResizeAvailable reports whether both fixed columns are on. A list
// that hides one keeps its old width-based layout, where there is no boundary
// for both dividers to sit on.
func (state *State) sftpColumnResizeAvailable() bool {
	return state.sftpColumns.enabled(sftpColumnSize) && state.sftpColumns.enabled(sftpColumnModified)
}

// sftpColumnMetrics resolves the widths for a list whose columns occupy area.
// Until the user drags one, the widths follow the room the list has; after
// that they are the chosen ones, clamped so the name keeps its minimum.
func (state *State) sftpColumnMetrics(area, gap float32) sftpColumnMetrics {
	if !state.sftpColumnWidthsChosen {
		state.sftpColumnSizeWidth, state.sftpColumnModifiedWidth = state.sftpColumnWidths(area)
	}
	size := max(state.sftpColumnSizeWidth, sftpColumnMinSize)
	modified := max(state.sftpColumnModifiedWidth, sftpColumnMinModified)
	name := area - size - modified - 2*gap
	if name < sftpColumnMinName {
		need := sftpColumnMinName - name
		take := min(need, size-sftpColumnMinSize)
		size -= take
		need -= take
		if need > 0 {
			modified -= min(need, modified-sftpColumnMinModified)
		}
		name = area - size - modified - 2*gap
	}
	return sftpColumnMetrics{name: max(name, 0), size: size, modified: modified}
}

// applySFTPColumnFit gives the left column of a divider the width its content
// needs, taking the difference from the column beside it.
func (state *State) applySFTPColumnFit(metrics sftpColumnMetrics, divider int, fit float32) sftpColumnMetrics {
	switch divider {
	case 0:
		total := metrics.name + metrics.size
		name := min(max(fit, sftpColumnMinName), total-sftpColumnMinSize)
		metrics.name, metrics.size = name, total-name
	case 1:
		total := metrics.size + metrics.modified
		size := min(max(fit, sftpColumnMinSize), total-sftpColumnMinModified)
		metrics.size, metrics.modified = size, total-size
	default:
		return metrics
	}
	state.sftpColumnSizeWidth = metrics.size
	state.sftpColumnModifiedWidth = metrics.modified
	state.sftpColumnWidthsChosen = true
	return metrics
}

// resizeSFTPColumns applies a pointer position to one divider. The widths are
// resolved from area, so the drag uses the geometry the last paint showed even
// when a pointer arrives before the next frame.
func (state *State) resizeSFTPColumns(divider int, x float32, area gfx.Rect, gap float32) bool {
	if divider < 0 || divider > 1 || area.Width <= 0 {
		return false
	}
	metrics := state.sftpColumnMetrics(area.Width, gap)
	widths := [3]float32{metrics.name, metrics.size, metrics.modified}
	left, right, ok := sftpColumnPairEdit(area, widths, gap, divider, x)
	if !ok {
		return false
	}
	if divider == 0 {
		state.sftpColumnSizeWidth = right
	} else {
		state.sftpColumnSizeWidth = left
		state.sftpColumnModifiedWidth = right
	}
	state.sftpColumnWidthsChosen = true
	return true
}

// sftpColumnFitWidth is what a double click on a divider measures: the widest
// name or size among the rows on screen, plus the icon and gaps the column
// spends before its text.
func (state *State) sftpColumnFitWidth(folder *sftpFolder, canvas *gfx.Canvas, divider int) float32 {
	// Fit the rows at the top of the folder, where a long name is what the
	// column has to show; measuring every row of a huge listing would make the
	// click wait on the whole folder.
	rows := folder.shownRows()
	end := min(len(rows), 1000)
	longest := float32(0)
	for index := 0; index < end; index++ {
		row := rows[index]
		if divider == 0 {
			name := row.cells[1]
			if row.entry.IsLink() && row.entry.LinkTarget != "" {
				name += " → " + row.entry.LinkTarget
			}
			longest = max(longest, canvas.TextWidth(name, metrics.FontSizeM))
			continue
		}
		longest = max(longest, canvas.TextWidth(row.cells[2], metrics.FontSizeS))
	}
	if divider == 0 {
		return longest + sftpColumnIconWidth + sftpColumnNameGap
	}
	return longest + sftpColumnNameGap
}

// pendingSFTPColumnMetrics applies a double-click's fit if one is waiting, then
// returns the widths this paint uses.
func (state *State) pendingSFTPColumnMetrics(view *sftpView, folder *sftpFolder, canvas *gfx.Canvas, area, gap float32) sftpColumnMetrics {
	metrics := state.sftpColumnMetrics(area, gap)
	if view == nil || !view.columnFitPending {
		return metrics
	}
	fit := state.sftpColumnFitWidth(folder, canvas, view.columnFitDivider)
	metrics = state.applySFTPColumnFit(metrics, view.columnFitDivider, fit)
	view.columnFitPending = false
	return metrics
}

// paintSFTPColumnResize paints the dividers and their hit strips over the
// header, and returns the geometry through the caller's slices.
func paintSFTPColumnResize(canvas *gfx.Canvas, theme gfx.Theme, area gfx.Rect, widths [3]float32, gap float32, ids [2]gfx.ID, label string, columns, dividers, hits []gfx.Rect) {
	values := [3]float32{widths[0], widths[1], widths[2]}
	if len(columns) > 2 {
		for index := range 3 {
			columns[index] = gfx.Rect{
				X: sftpColumnLeft(area, values, gap, index), Y: area.Y,
				Width: sftpColumnWidth(values, index), Height: area.Height,
			}
		}
	}
	const hitSize = float32(4)
	for index := range 2 {
		x := sftpColumnLeft(area, values, gap, index) + sftpColumnWidth(values, index) + gap/2
		if len(dividers) > index {
			dividers[index] = gfx.Rect{X: x, Y: area.Y, Width: 1, Height: area.Height}
		}
		if len(hits) > index {
			hits[index] = gfx.Rect{X: x - (hitSize-1)/2, Y: area.Y, Width: hitSize, Height: area.Height}
		}
		if dividers[index].Width <= 0 || dividers[index].Height <= 0 {
			continue
		}
		canvas.Fill(dividers[index], theme.Border)
		id := ids[index]
		if id.IsZero() {
			continue
		}
		line := theme.Ring
		switch {
		case canvas.Pressed() == id:
			line = theme.Primary
		case canvas.Hovered() != id:
			line = gfx.Color{}
		}
		if line.Alpha != 0 {
			// The hovered or pressed line grows a pixel above and below, as the
			// painted ColumnResize did.
			divider := dividers[index]
			canvas.Rounded(gfx.Rect{X: divider.X - 1, Y: divider.Y - 1, Width: 3, Height: divider.Height + 2}, 1, line)
		}
		canvas.HitWithOptions(id, hits[index], label, gfx.HitOptions{
			Role: gfx.RoleSeparator, Cursor: gfx.PointerCursorResizeHorizontal, SkipFocus: true,
		})
	}
}

// sftpColumnWidth is one column's width, at least its minimum.
func sftpColumnWidth(widths [3]float32, index int) float32 {
	return max(widths[index], sftpColumnMinimums[index])
}

// sftpColumnLeft is where a column starts in area.
func sftpColumnLeft(area gfx.Rect, widths [3]float32, gap float32, index int) float32 {
	x := area.X
	for at := 0; at < index && at < len(widths); at++ {
		x += sftpColumnWidth(widths, at) + gap
	}
	return x
}

// sftpColumnPairEdit maps a pointer position to the widths of the two columns
// either side of divider. Their sum does not change, and neither goes below its
// minimum.
func sftpColumnPairEdit(area gfx.Rect, widths [3]float32, gap float32, divider int, x float32) (left, right float32, ok bool) {
	if divider < 0 || divider+1 >= len(widths) {
		return 0, 0, false
	}
	total := sftpColumnWidth(widths, divider) + sftpColumnWidth(widths, divider+1)
	left = x - sftpColumnLeft(area, widths, gap, divider) - gap/2
	low, high := sftpColumnMinimums[divider], sftpColumnMinimums[divider+1]
	if low+high > total {
		if low+high <= 0 {
			left = total / 2
		} else {
			left = total * low / (low + high)
		}
	} else {
		left = min(max(left, low), total-high)
	}
	return left, total - left, true
}

// sftpColumnPointer handles the docked file panel's column dividers. It runs
// with the list's pointer, before a row's press can take the divider.
func (state *State) sftpColumnPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if state.activeSection != sectionTerminal || state.modal != modalNone || !state.sftpColumnResizeAvailable() {
		return gfx.EventResult{}, false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.files == nil {
		return gfx.EventResult{}, false
	}
	view := session.files
	panel := &state.chrome.sftpPanel
	if panel.view != view || panel.columnArea.Width <= 0 {
		return gfx.EventResult{}, false
	}
	if view.columnDragging {
		switch event.Kind {
		case gfx.PointerMove:
			state.resizeSFTPColumns(view.columnDrag, event.X, panel.columnArea, 0)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		case gfx.PointerUp, gfx.PointerExit:
			view.columnDragging = false
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		return gfx.EventResult{Handled: true}, true
	}
	if event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonPrimary {
		return gfx.EventResult{}, false
	}
	column, ok := event.Target.In(hitSFTPPanelColumnDivider)
	if !ok || column > 1 {
		return gfx.EventResult{}, false
	}
	if event.ClickCount >= 2 {
		view.columnFitPending, view.columnFitDivider = true, column
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	view.columnDragging, view.columnDrag = true, column
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

// paintColumnDividers draws one pane's column dividers over its header.
func (w *sftpWindow) paintColumnDividers(canvas *gfx.Canvas, side int) {
	area := w.ui.columnArea[side]
	if area.Width <= 0 {
		return
	}
	ids := [2]gfx.ID{sftpWindowColumnDividers.At(side * 2), sftpWindowColumnDividers.At(side*2 + 1)}
	var columns [3]gfx.Rect
	var dividers, hits [2]gfx.Rect
	paintSFTPColumnResize(canvas, w.owner.Theme(), area, w.ui.columnWidths[side], sftpColumnNameGap, ids,
		w.owner.messages.Tools.FilesResizeColumn, columns[:], dividers[:], hits[:])
}

// columnResizePointer handles the dual-pane window's column dividers, one side
// at a time. It runs before the window's early return for non-press events so
// the drag sees every move.
func (w *sftpWindow) columnResizePointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if !w.owner.sftpColumnResizeAvailable() {
		return gfx.EventResult{}, false
	}
	for side := range w.ui.columnDragging {
		if !w.ui.columnDragging[side] {
			continue
		}
		switch event.Kind {
		case gfx.PointerMove:
			w.owner.resizeSFTPColumns(w.ui.columnDrag[side], event.X, w.ui.columnArea[side], sftpColumnNameGap)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		case gfx.PointerUp, gfx.PointerExit:
			w.ui.columnDragging[side] = false
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		return gfx.EventResult{Handled: true}, true
	}
	if event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonPrimary {
		return gfx.EventResult{}, false
	}
	index, ok := event.Target.In(sftpWindowColumnDividers)
	if !ok || index >= 4 {
		return gfx.EventResult{}, false
	}
	side, divider := index/2, index%2
	if w.ui.columnArea[side].Width <= 0 {
		return gfx.EventResult{}, false
	}
	if event.ClickCount >= 2 {
		w.ui.columnFitPending[side], w.ui.columnFitDivider[side] = true, divider
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	w.ui.columnDragging[side], w.ui.columnDrag[side] = true, divider
	return gfx.EventResult{Handled: true, Repaint: true}, true
}
