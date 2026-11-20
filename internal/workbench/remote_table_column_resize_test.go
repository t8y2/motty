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
	fitRemoteTableColumns(columns, 400)
	if columns[0].Width < remoteTableColumnMinimum || columns[1].Width < remoteTableColumnMinimum {
		t.Fatalf("fitted columns fell below minimums: %v", columns)
	}
	if columns[0].Width+columns[1].Width > 320.1 || columns[2].Width != 0 {
		t.Fatalf("fitted columns left no flexible space: %v", columns)
	}
}
