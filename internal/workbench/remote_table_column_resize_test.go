package workbench

import (
	"math"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

func remoteColumnDivider(t *testing.T, driver *app.Driver, view *remoteToolsView, column int) gfx.Hit {
	t.Helper()
	id := view.columnResize.handlers.Parts().At(column)
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == id && !hit.Block {
			if hit.Role != gfx.RoleSeparator || hit.Cursor != gfx.PointerCursorResizeHorizontal {
				t.Fatalf("column divider semantics = %+v", hit)
			}
			return hit
		}
	}
	t.Fatalf("missing divider for column %d", column)
	return gfx.Hit{}
}

func dragRemoteColumn(t *testing.T, driver *app.Driver, view *remoteToolsView, column int, delta float32) {
	t.Helper()
	hit := remoteColumnDivider(t, driver, view, column)
	x, y := hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2
	if err := driver.Press(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	// Leave the header's hit strip vertically as well as horizontally: the
	// captured press must keep resizing until the button is released.
	if err := driver.Move(x+delta, y+80); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(x+delta, y+80, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteModalColumnResizeKeepsWidthsAcrossRefreshAndTabs(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	driver := nativeWindowDriver(t, state.WindowConfig(app.Config{
		Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
	}))
	view := &state.remote
	before, next := view.tableColumns[0].Width, view.tableColumns[1].Width
	order, selected := view.tableSort(), view.selected
	dragRemoteColumn(t, driver, view, 0, 20)
	if state.modal != modalRemoteTools {
		t.Fatal("dragging a divider dismissed the tools dialog")
	}
	if got := view.tableColumns[0].Width; math.Abs(float64(got-before-20)) > 1 {
		t.Fatalf("dragged first width = %v, want %v", got, before+20)
	}
	if got := view.tableColumns[1].Width; math.Abs(float64(got-next+20)) > 1 {
		t.Fatalf("neighbour width = %v, want %v", got, next-20)
	}
	if view.tableSort() != order || view.selected != selected || view.processDetails.open {
		t.Fatal("resizing sorted or selected a row, or opened details")
	}
	chosen := view.tableColumns[0].Width
	driverClick(t, driver, driverHit(t, driver, view.tableHeadings[0], gfx.RoleButton))
	if view.tableSort().Column != 0 || view.tableSort().Direction != components.TableSortAscending || view.tableColumns[0].Width != chosen {
		t.Fatal("the resized header did not sort while keeping its width")
	}
	replaceRemoteQuery(t, state, "nginx")
	if err := driver.Resize(1400, 900); err != nil {
		t.Fatal(err)
	}
	if len(view.rows) != 1 || view.tableColumns[0].Width != chosen {
		t.Fatal("filtering discarded the chosen process width")
	}
	state.refreshRemoteTools()
	waitFor(t, state, func() bool { return !view.loading })
	if err := driver.Resize(1400, 900); err != nil {
		t.Fatal(err)
	}
	if view.tableColumns[0].Width != chosen {
		t.Fatal("refresh discarded the chosen process width")
	}
	clickSettingsChoice(t, state, state.messages.Tools.RemoteTabContainers)
	waitFor(t, state, func() bool { return !view.loading })
	if err := driver.Resize(1400, 900); err != nil {
		t.Fatal(err)
	}
	dragRemoteColumn(t, driver, view, 0, -25)
	containerWidth := view.tableColumns[0].Width
	clickSettingsChoice(t, state, state.messages.Tools.RemoteTabProcesses)
	waitFor(t, state, func() bool { return !view.loading })
	if err := driver.Resize(1400, 900); err != nil {
		t.Fatal(err)
	}
	if view.tableColumns[0].Width != chosen {
		t.Fatal("switching tabs discarded the process width")
	}
	clickSettingsChoice(t, state, state.messages.Tools.RemoteTabContainers)
	waitFor(t, state, func() bool { return !view.loading })
	if err := driver.Resize(1400, 900); err != nil {
		t.Fatal(err)
	}
	if view.tableColumns[0].Width != containerWidth {
		t.Fatal("switching tabs discarded the container width")
	}
	// A smaller modal fits the chosen columns without overwriting their sizes.
	if err := driver.Resize(740, 900); err != nil {
		t.Fatal(err)
	}
	for column := 0; column < view.tableCount; column++ {
		if view.columnResize.widths[column] < remoteTableColumnMinimum-1 {
			t.Fatalf("column %d collapsed in the narrow modal: %v", column, view.columnResize.widths)
		}
	}
	if err := driver.Resize(1400, 900); err != nil {
		t.Fatal(err)
	}
	if view.tableColumns[0].Width != containerWidth {
		t.Fatal("widening the modal did not restore the chosen widths")
	}
}

func TestRemoteColumnWidthsSurviveClosingTheToolsDialog(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	driver := nativeWindowDriver(t, state.WindowConfig(app.Config{
		Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
	}))
	dragRemoteColumn(t, driver, &state.remote, 0, 20)
	chosen := state.remoteChosenWidths(remoteProcesses)
	if !chosen.chosen {
		t.Fatal("the drag did not record the process widths")
	}
	want := chosen.widths[0]
	state.closeRemoteToolsSurface()
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("the session disappeared with the tools dialog")
	}
	state.openRemoteToolsTab(session, remoteProcesses, false)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if err := driver.Resize(1400, 900); err != nil {
		t.Fatal(err)
	}
	if got := state.remote.tableColumns[0].Width; got != want {
		t.Fatalf("reopening the tools dialog reset the process width: %v, want %v", got, want)
	}
}

func TestRemoteColumnDoubleClickFitsTheColumnToItsContent(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	driver := nativeWindowDriver(t, state.WindowConfig(app.Config{
		Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
	}))
	view := &state.remote
	before := view.tableColumns[0].Width
	dragRemoteColumn(t, driver, view, 0, 50)
	widened := view.tableColumns[0].Width
	if widened <= before {
		t.Fatalf("the drag did not widen the first column: %v", widened)
	}
	hit := remoteColumnDivider(t, driver, view, 0)
	if err := driver.DoubleClick(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if got := view.tableColumns[0].Width; got >= widened-1 {
		t.Fatalf("double-clicking the divider did not fit the column to its content: %v", got)
	}
	if got := view.tableColumns[0].Width; got < remoteTableColumnMinimum-1 {
		t.Fatalf("the fit went below the minimum: %v", got)
	}
}

func TestRemoteNativeWindowColumnResizeEveryTab(t *testing.T) {
	withRemoteWindowApplication(t, func(application *app.Application, host *remoteWindowHost) {
		state, _, fake, _ := liveSession(t)
		fake.mu.Lock()
		fake.runCommand = (&remoteServer{}).run
		fake.mu.Unlock()
		state.options.OpenContentWindow = application.OpenContentWindow
		state.Click(hitRemoteToolsOpen)
		w := state.remoteWindows[0]
		host.until(t, func() bool { return !w.ui.remote.loading })
		driver := nativeWindowDriver(t, host.windows[0].config)
		view, m := &w.ui.remote, w.ui.messages.Tools
		for _, label := range []string{m.RemoteTabProcesses, m.RemoteTabContainers, m.RemoteTabImages, m.RemoteTabVolumes, m.RemoteTabNetworks, m.RemoteTabCompose} {
			t.Run(label, func(t *testing.T) {
				clickRemoteTabChoice(t, w, label)
				host.until(t, func() bool { return !view.loading })
				if err := driver.Resize(1100, 780); err != nil {
					t.Fatal(err)
				}
				before := view.tableColumns[0].Width
				order := view.tableSort()
				dragRemoteColumn(t, driver, view, 0, 15)
				if math.Abs(float64(view.tableColumns[0].Width-before-15)) > 1 {
					t.Fatalf("first width = %v, want %v", view.tableColumns[0].Width, before+15)
				}
				if view.tableSort() != order {
					t.Fatal("dragging a divider sorted the table")
				}
				// The last divider also adjusts the flexible final column.
				dragRemoteColumn(t, driver, view, view.tableCount-2, 10000)
				last := view.columnResize.widths[view.tableCount-1]
				if last < remoteTableColumnMinimum-1 {
					t.Fatalf("last column collapsed to %v", last)
				}
				if err := driver.Resize(960, 780); err != nil {
					t.Fatal(err)
				}
				resize := &view.columnResize
				for column := 0; column+1 < view.tableCount; column++ {
					hit := remoteColumnDivider(t, driver, view, column)
					if hit.Bounds.X < resize.bounds.X || hit.Bounds.X+hit.Bounds.Width > resize.bounds.X+resize.bounds.Width {
						t.Fatalf("divider %d lies outside the narrowed table: %v in %v", column, hit.Bounds, resize.bounds)
					}
				}
			})
		}
	})
}

func TestFitRemoteTableColumnsPreservesMinimumsAndFlexibleSpace(t *testing.T) {
	columns := []components.TableColumn{{Width: 600}, {Width: 300}, {Width: 0}}
	const width = float32(400)
	fitRemoteTableColumns(columns, width)
	minimum := remoteTableColumnFitMinimum(width, len(columns))
	for index := 0; index+1 < len(columns); index++ {
		if columns[index].Width < minimum {
			t.Fatalf("column %d = %v, below the %v minimum: %v", index, columns[index].Width, minimum, columns)
		}
	}
	if last := width - columns[0].Width - columns[1].Width; last < minimum {
		t.Fatalf("the flexible last column = %v, below the %v minimum: %v", last, minimum, columns)
	}
	if columns[2].Width != 0 {
		t.Fatalf("the fit wrote the flexible column: %v", columns)
	}
}

func TestFitRemoteTableColumnsReservesOneMinimumForTheFlexibleColumn(t *testing.T) {
	// Six columns at 890px: the final divider must keep following the pointer
	// until the last column reaches the minimum a drag clamps at, rather than
	// stopping one minimum early.
	const width = float32(890)
	minimum := remoteTableColumnFitMinimum(width, 6)
	columns := []components.TableColumn{
		{Width: 170}, {Width: 170}, {Width: 170}, {Width: 170}, {Width: 170}, {Width: 0},
	}
	fitRemoteTableColumns(columns, width)
	widths := make([]float32, len(columns))
	minimums := make([]float32, len(columns))
	fixed := float32(0)
	for index := range columns {
		if index+1 < len(columns) && columns[index].Width < minimum {
			t.Fatalf("column %d = %v, below the %v minimum: %v", index, columns[index].Width, minimum, columns)
		}
		widths[index] = columns[index].Width
		fixed += columns[index].Width
	}
	widths[len(widths)-1] = max(width-fixed, 0)
	if widths[len(widths)-1] < minimum {
		t.Fatalf("last column = %v, below the %v minimum", widths[len(widths)-1], minimum)
	}
	// The fit reserves exactly one minimum for the flexible column; reserving
	// two would leave it wider than a drag can reach.
	if widths[len(widths)-1] > minimum+0.5 {
		t.Fatalf("the fit reserved %v for the last column, want one minimum %v", widths[len(widths)-1], minimum)
	}
	// The drag's Edit path clamps at the same floor, so the divider follows the
	// pointer until then.
	for index := range widths {
		minimums[index] = min(remoteTableColumnMinimum, widths[index])
	}
	resize := components.BeginColumnResize(nil, gfx.Theme{}, gfx.Rect{Width: width}, widths).Minimums(minimums)
	edit, ok := resize.Edit(len(widths)-2, math.MaxFloat32)
	if !ok {
		t.Fatal("the final divider is not editable")
	}
	if math.Abs(float64(edit.Right-minimum)) > 0.5 {
		t.Fatalf("a drag clamps the last column at %v, the fit keeps %v", edit.Right, minimum)
	}
}
