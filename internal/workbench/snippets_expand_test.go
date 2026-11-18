//go:build windows || darwin || linux

package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

func snippetTestState(t *testing.T, snippets ...snippet) *State {
	t.Helper()
	state := newTestState(t, testOptions(t))
	state.snippets.list = snippets
	state.refreshSnippets()
	state.activeSection = sectionSnippets
	return state
}

func paintSnippetsPage(state *State, canvas *gfx.Canvas) {
	canvas.Reset()
	state.Paint(canvas, gfx.Size{Width: 1400, Height: 900})
}

func hasTextValue(canvas *gfx.Canvas, value string) bool {
	for _, text := range canvas.Texts() {
		if text.Value == value {
			return true
		}
	}
	return false
}

func TestExpandingASnippetShowsItsWholeCommand(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "Two lines", Command: "echo first\n  echo second"},
		snippet{Name: "One line", Command: "ls"})
	var canvas gfx.Canvas
	paintSnippetsPage(state, &canvas)
	// The row summary is one line with ↵; the detail is the only place the
	// command's own lines are painted as they are.
	if hasTextValue(&canvas, "echo first") {
		t.Fatal("a collapsed row painted its command's first line")
	}
	state.Click(hitSnippetExpandList.At(0))
	paintSnippetsPage(state, &canvas)
	if state.snippets.expanded != 1 {
		t.Fatalf("expanded = %d, want the first snippet", state.snippets.expanded)
	}
	if !hasTextValue(&canvas, "echo first") || !hasTextValue(&canvas, "  echo second") {
		t.Fatalf("the expanded command's lines are missing: %v", canvas.Texts())
	}
	expander, ok := hitByID(&canvas, hitSnippetExpandList.At(0))
	if !ok || expander.Role != gfx.RoleButton || !expander.Expanded || !expander.Expandable || expander.Description != "Two lines" {
		t.Fatalf("expander hit = %+v, want an expanded button naming its snippet", expander)
	}
	// The detail is one non-actionable region named by the snippet.
	found := false
	for _, hit := range canvas.Hits() {
		if hit.Container && hit.Role == gfx.RoleGroup && hit.Label == "Two lines" && hit.Description == "echo first\n  echo second" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the expanded command has no accessibility region")
	}
	state.Click(hitSnippetExpandList.At(0))
	paintSnippetsPage(state, &canvas)
	if state.snippets.expanded != 0 || hasTextValue(&canvas, "echo first") {
		t.Fatalf("collapsing left expanded = %d and the detail text", state.snippets.expanded)
	}
}

func TestOnlyOneSnippetRowExpandsAtATime(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "First", Command: "echo first\n  echo second"},
		snippet{Name: "Second", Command: "echo third\n  echo fourth"})
	state.Click(hitSnippetExpandList.At(0))
	state.Click(hitSnippetExpandList.At(1))
	var canvas gfx.Canvas
	paintSnippetsPage(state, &canvas)
	if state.snippets.expanded != 2 {
		t.Fatalf("expanded = %d, want the second snippet", state.snippets.expanded)
	}
	if hasTextValue(&canvas, "echo first") || !hasTextValue(&canvas, "echo third") {
		t.Fatalf("both rows stayed expanded: %v", canvas.Texts())
	}
}

func TestSnippetExpansionClearsWhenTheListChanges(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "First", Command: "echo first"},
		snippet{Name: "Second", Command: "echo second"})
	state.snippets.expanded = 1
	state.refreshSnippets()
	if state.snippets.expanded != 1 {
		t.Fatal("sorting or filtering must not close an expanded row")
	}
	state.moveSnippet(0, 1)
	if state.snippets.expanded != 0 {
		t.Fatalf("a move left expanded = %d", state.snippets.expanded)
	}
	state.snippets.expanded = 1
	state.deleteSnippet(0)
	if state.snippets.expanded != 0 {
		t.Fatalf("a delete left expanded = %d", state.snippets.expanded)
	}
}

func TestSnippetHoverPreviewOpensAfterARestAndStaysOnTheCard(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "Two lines", Command: "echo first\n  echo second"},
		snippet{Name: "One line", Command: "ls"})
	var canvas gfx.Canvas
	now := time.Duration(0)
	canvas.SetClock(func() time.Duration { return now })
	// A throwaway paint names the rows without leaving its frame request on the
	// canvas whose frame deadline this test reads.
	var ids gfx.Canvas
	state.Paint(&ids, gfx.Size{Width: 1400, Height: 900})
	row := state.snippets.rows.RowID(0)

	canvas.SetInteraction(row, gfx.ID{}, gfx.ID{}, false)
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if state.snippets.hoverOpen {
		t.Fatal("the preview opened before the pointer rested")
	}
	if delay, ok := canvas.NextFrame(); !ok || delay != snippetHoverDelay {
		t.Fatalf("next frame = %v, %v, want the rest deadline %v", delay, ok, snippetHoverDelay)
	}

	now = snippetHoverDelay
	paintSnippetsPage(state, &canvas)
	if !state.snippets.hoverOpen || state.snippets.hoverRow != 1 {
		t.Fatalf("hover = %v at row %d after the rest", state.snippets.hoverOpen, state.snippets.hoverRow)
	}
	if !hasTextValue(&canvas, "echo first") || !hasTextValue(&canvas, "  echo second") {
		t.Fatalf("the preview does not show the command: %v", canvas.Texts())
	}
	// The card is still headed by the snippet's name, at the popover's title
	// weight, as components.HoverCard's title was.
	titled := false
	for _, painted := range canvas.Texts() {
		if painted.Value == "Two lines" && painted.Style.Weight == gfx.WeightMedium &&
			painted.Style.Size == state.Theme().Metrics.FontSizeM {
			titled = true
			break
		}
	}
	if !titled {
		t.Fatalf("the preview has no medium name title: %v", canvas.Texts())
	}

	// The pointer reaches the card: the preview stays while it is there.
	canvas.SetInteraction(hitSnippetHoverCard, gfx.ID{}, gfx.ID{}, false)
	paintSnippetsPage(state, &canvas)
	if !state.snippets.hoverOpen {
		t.Fatal("the preview closed when the pointer moved onto it")
	}

	// Leaving both the row and the card closes it.
	canvas.SetInteraction(gfx.ID{}, gfx.ID{}, gfx.ID{}, false)
	paintSnippetsPage(state, &canvas)
	if state.snippets.hoverOpen {
		t.Fatal("the preview stayed after the pointer left")
	}
}

func TestSnippetHoverPreviewSkipsRowsItWouldNotHelp(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "Two lines", Command: "echo first\n  echo second"},
		snippet{Name: "One line", Command: "ls"})
	var canvas gfx.Canvas
	now := snippetHoverDelay
	canvas.SetClock(func() time.Duration { return now })
	paintSnippetsPage(state, &canvas)

	canvas.SetInteraction(state.snippets.rows.RowID(1), gfx.ID{}, gfx.ID{}, false)
	paintSnippetsPage(state, &canvas)
	if state.snippets.hoverOpen {
		t.Fatal("a single-line command got a preview")
	}

	state.snippets.expanded = 1
	canvas.SetInteraction(state.snippets.rows.RowID(0), gfx.ID{}, gfx.ID{}, false)
	paintSnippetsPage(state, &canvas)
	if state.snippets.hoverOpen {
		t.Fatal("the expanded row got a preview on top of its detail")
	}

	state.snippets.expanded = 0
	state.modal = modalSnippet
	canvas.SetInteraction(state.snippets.rows.RowID(0), gfx.ID{}, gfx.ID{}, false)
	paintSnippetsPage(state, &canvas)
	if state.snippets.hoverOpen {
		t.Fatal("the preview opened under a dialog")
	}
}

func TestClickingASnippetRowSelectsIt(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "First", Command: "echo first"},
		snippet{Name: "Second", Command: "echo second"})
	view := openCatalogWindow(t, state)
	row, ok := findSnapshotHit(view.Snapshot(), state.snippets.rows.RowID(1))
	if !ok {
		t.Fatal("the second row is not painted")
	}
	if err := view.Driver().Click(row.Bounds.X+row.Bounds.Width/2, row.Bounds.Y+row.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.snippets.selected != 1 {
		t.Fatalf("selected = %d, want the second snippet", state.snippets.selected)
	}
	if state.snippets.rows.Selected() != 1 {
		t.Fatalf("the list's row = %d, want the second", state.snippets.rows.Selected())
	}
}

func TestExpandedSnippetSteadyPaintDoesNotAllocate(t *testing.T) {
	state := snippetTestState(t, snippet{Name: "Two lines", Command: "echo first\n  echo second"})
	state.snippets.expanded = 1
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("an expanded row's paint allocations = %v", allocations)
	}
}

func TestSnippetHoverCardSteadyPaintDoesNotAllocate(t *testing.T) {
	state := snippetTestState(t, snippet{Name: "Two lines", Command: "echo first\n  echo second"})
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	now := time.Duration(0)
	canvas.SetClock(func() time.Duration { return now })
	paintSnippetsPage(state, &canvas)
	canvas.SetInteraction(state.snippets.rows.RowID(0), gfx.ID{}, gfx.ID{}, false)
	state.Paint(&canvas, size)
	now = snippetHoverDelay
	paintSnippetsPage(state, &canvas)
	if !state.snippets.hoverOpen {
		t.Fatal("the preview did not open")
	}
	canvas.SetInteraction(hitSnippetHoverCard, gfx.ID{}, gfx.ID{}, false)
	allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		canvas.SetInteraction(hitSnippetHoverCard, gfx.ID{}, gfx.ID{}, false)
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("the preview's steady paint allocations = %v", allocations)
	}
}

// hasPaintedRect reports whether a rectangle of color and exactly bounds was
// painted, which is how the snippet page's bands, rules and accents are drawn.
func hasPaintedRect(canvas *gfx.Canvas, color gfx.Color, bounds gfx.Rect) bool {
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Color == color && rectangle.Bounds == bounds {
			return true
		}
	}
	return false
}

// snippetTextIn finds the label with value whose box starts inside bounds.
func snippetTextIn(canvas *gfx.Canvas, value string, bounds gfx.Rect) (gfx.Text, bool) {
	for _, painted := range canvas.Texts() {
		if painted.Value != value {
			continue
		}
		if painted.Bounds.Y >= bounds.Y && painted.Bounds.Y < bounds.Y+bounds.Height {
			return painted, true
		}
	}
	return gfx.Text{}, false
}

// The expanded command keeps the table's detail band: the theme background, a
// three pixel primary accent at the leading edge, and one hairline between the
// row and the detail. Ordinary rows have no rule, and the header has no
// divider.
func TestExpandedSnippetKeepsTheTablesDetailBand(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "First", Command: "echo first\n  echo second"},
		snippet{Name: "Second", Command: "ls"})
	state.snippets.expanded = 1
	var canvas gfx.Canvas
	paintSnippetsPage(state, &canvas)
	theme := state.Theme()
	row, ok := hitByID(&canvas, state.snippets.rows.RowID(0))
	if !ok {
		t.Fatal("the expanded row was not painted")
	}
	detailTop := row.Bounds.Y + components.TableRowHeight(theme)
	detailHeight := state.snippets.detailHeight
	if detailHeight <= 0 {
		t.Fatal("the expanded row has no measured detail")
	}
	band := gfx.Rect{X: row.Bounds.X, Y: detailTop, Width: row.Bounds.Width, Height: detailHeight}
	if !hasPaintedRect(&canvas, theme.Background, band) {
		var backgrounds []gfx.Rect
		for _, rectangle := range canvas.Rectangles() {
			if rectangle.Color == theme.Background {
				backgrounds = append(backgrounds, rectangle.Bounds)
			}
		}
		t.Fatalf("no theme background band at %v (row %v, height %v); backgrounds %v",
			band, row.Bounds, detailHeight, backgrounds)
	}
	accent := theme.Primary
	accent.Alpha *= 0.7
	if !hasPaintedRect(&canvas, accent, gfx.Rect{X: band.X, Y: band.Y, Width: 3, Height: band.Height}) {
		t.Fatal("the detail has no three pixel primary accent")
	}
	if !hasPaintedRect(&canvas, theme.Border, gfx.Rect{X: band.X, Y: detailTop - 1, Width: band.Width, Height: 1}) {
		t.Fatal("the row and its detail have no hairline between them")
	}
	second, ok := hitByID(&canvas, state.snippets.rows.RowID(1))
	if !ok {
		t.Fatal("the collapsed row was not painted")
	}
	if hasPaintedRect(&canvas, theme.Border, gfx.Rect{X: second.Bounds.X, Y: second.Bounds.Bottom() - 1, Width: second.Bounds.Width, Height: 1}) {
		t.Fatal("a collapsed row has a hairline")
	}
	header, ok := hitByID(&canvas, hitSnippetSort.At(0))
	if !ok {
		t.Fatal("the header was not painted")
	}
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Color == theme.Border && rectangle.Bounds.Y == header.Bounds.Bottom()-1 &&
			rectangle.Bounds.Width >= row.Bounds.Width {
			t.Fatal("the header still has a divider under it")
		}
	}
}

// The sortable snippet headings keep components.Table's look: the foreground
// label, an arrow in every sort state, the hover surface, the focus ring and
// the sort description.
func TestSnippetHeaderKeepsTheTablesSortAffordances(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "First", Group: "Ops", Command: "echo first"},
		snippet{Name: "Second", Group: "Ops", Command: "echo second"})
	theme, m := state.Theme(), state.messages.Tools
	var canvas gfx.Canvas
	paintSnippetsPage(state, &canvas)
	id := hitSnippetSort.At(0)
	header, ok := hitByID(&canvas, id)
	if !ok {
		t.Fatal("the name heading was not painted")
	}
	if header.Description != m.SnippetSortAction {
		t.Fatalf("the unsorted heading description = %q", header.Description)
	}
	label, ok := snippetTextIn(&canvas, m.SnippetColumnName, header.Bounds)
	if !ok {
		t.Fatal("the heading label was not painted")
	}
	if label.Style.Color != theme.Foreground || label.Style.Weight != gfx.WeightMedium ||
		label.Style.Size != theme.Metrics.FontSizeM {
		t.Fatalf("the heading label style = %+v, want medium foreground", label.Style)
	}
	if remotePaintedTriangles(&canvas, theme.Foreground, header.Bounds) == 0 {
		t.Fatal("an unsorted heading has no sort arrow")
	}

	canvas.SetInteraction(id, gfx.ID{}, gfx.ID{}, false)
	paintSnippetsPage(state, &canvas)
	if !hasPaintedRect(&canvas, theme.Hover, header.Bounds) {
		t.Fatal("a hovered heading has no hover surface")
	}

	canvas.SetInteraction(gfx.ID{}, gfx.ID{}, id, true)
	paintSnippetsPage(state, &canvas)
	if remotePaintedTriangles(&canvas, theme.Ring, header.Bounds) == 0 {
		t.Fatal("a focused heading has no focus ring")
	}

	state.Click(id)
	paintSnippetsPage(state, &canvas)
	header, ok = hitByID(&canvas, id)
	if !ok {
		t.Fatal("the sorted heading was not painted")
	}
	if header.Description != m.SnippetSortAscending {
		t.Fatalf("the ascending heading description = %q", header.Description)
	}
	if remotePaintedTriangles(&canvas, theme.Foreground, header.Bounds) == 0 {
		t.Fatal("a sorted heading has no sort arrow")
	}
}

// Manual order keeps the reorder grid's 24 pixel grip seven pixels into the
// row, the 24 pixel name inset, and the command cell's leading keycap room and
// trailing expander inset.
func TestSnippetManualOrderKeepsTheHandleAndCommandInsets(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "Deploy", Group: "Ops", Command: "kubectl apply -f staging/\nwatch pods", Run: true},
		snippet{Name: "Tail", Command: "journalctl -fu app"})
	var canvas gfx.Canvas
	paintSnippetsPage(state, &canvas)
	theme, view := state.Theme(), &state.sectionBodies.snippets
	pad := theme.Metrics.GapItem
	rowHeight := components.TableRowHeight(theme)
	row, ok := hitByID(&canvas, state.snippets.rows.RowID(0))
	if !ok {
		t.Fatal("the first row was not painted")
	}
	handle, ok := hitByID(&canvas, hitSnippetHandles.At(0))
	if !ok {
		t.Fatal("the first row has no reorder handle")
	}
	wantHandle := gfx.Rect{X: row.Bounds.X + 7, Y: row.Bounds.Y + (rowHeight-24)/2, Width: 24, Height: 24}
	if handle.Bounds != wantHandle || handle.Role != gfx.RoleButton || handle.Cursor != gfx.PointerCursorHand ||
		handle.Label != "Deploy" {
		t.Fatalf("handle = %+v, want the reorder grid's 24 pixel grip at %v", handle, wantHandle)
	}
	name, ok := snippetTextIn(&canvas, "Deploy", row.Bounds)
	if !ok {
		t.Fatal("the first row has no name")
	}
	if got, want := name.Bounds.X, row.Bounds.X+pad+24; got != want {
		t.Fatalf("the name starts at %v, want the handle's 24 pixel inset at %v", got, want)
	}

	columnX := row.Bounds.X + view.columns[0].Width + view.columns[1].Width + view.columns[2].Width
	commandX := columnX + pad + view.keyWidth + 6
	commandCellWidth := row.Bounds.Width - (view.columns[0].Width + view.columns[1].Width + view.columns[2].Width)
	commandWidth := commandCellWidth - 2*pad - (view.keyWidth + 6) - (snippetExpandSize + pad)
	for index, value := range []string{"kubectl apply -f staging/ ↵ watch pods", "journalctl -fu app"} {
		at, ok := hitByID(&canvas, state.snippets.rows.RowID(index))
		if !ok {
			t.Fatalf("row %d was not painted", index)
		}
		text, ok := snippetTextIn(&canvas, value, at.Bounds)
		if !ok {
			t.Fatalf("the command %q was not painted in row %d", value, index)
		}
		wantX := at.Bounds.X + (commandX - row.Bounds.X)
		if text.Bounds.X != wantX {
			t.Fatalf("the command starts at %v, want the keycap inset %v", text.Bounds.X, wantX)
		}
		if text.Bounds.Width != commandWidth {
			t.Fatalf("the command width = %v, want the table cell's %v", text.Bounds.Width, commandWidth)
		}
	}
	expander, ok := hitByID(&canvas, hitSnippetExpandList.At(0))
	if !ok {
		t.Fatal("the first row has no expander")
	}
	if want := row.Bounds.X + row.Bounds.Width - pad - snippetExpandSize; expander.Bounds.X != want {
		t.Fatalf("the expander starts at %v, want %v", expander.Bounds.X, want)
	}

	if snippetExpanderFits(20, rowHeight) || !snippetExpanderFits(24, rowHeight) {
		t.Fatal("the expander is not omitted for a narrow command cell")
	}

	// Sorting leaves the manual order, and with it the handle and the name
	// inset, as the table did.
	state.Click(hitSnippetSort.At(0))
	paintSnippetsPage(state, &canvas)
	if _, ok := hitByID(&canvas, hitSnippetHandles.At(0)); ok {
		t.Fatal("a sorted row still has the manual order's handle")
	}
	row, ok = hitByID(&canvas, state.snippets.rows.RowID(0))
	if !ok {
		t.Fatal("the sorted row was not painted")
	}
	name, ok = snippetTextIn(&canvas, "Deploy", row.Bounds)
	if !ok {
		t.Fatalf("the sorted row has no name: %v", canvas.Texts())
	}
	if got, want := name.Bounds.X, row.Bounds.X+pad; got != want {
		t.Fatalf("the sorted name starts at %v, want %v without the handle inset", got, want)
	}
}
