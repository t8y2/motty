//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

// The catalog pages whose tables can outgrow their card all scroll through the
// rows the same way: the wheel, the bar and the keyboard reach the last row
// while the headings stay above the scrolling body.
func TestCatalogPagesScrollLongLists(t *testing.T) {
	const count = 200

	t.Run("port forwarding", func(t *testing.T) {
		state := newTestState(t, testOptions(t))
		for index := range count {
			state.forwards = append(state.forwards, forwardRecord{
				ID: uint32(index + 1), Label: fmt.Sprintf("Forward %03d", index),
				HostLabel: "Host", HostAddress: "host.internal", HostPort: 22,
				LocalPort: uint16(20000 + index), DestinationHost: "target.internal", DestinationPort: uint16(index + 1),
			})
		}
		state.refreshForwards()
		state.activeSection = sectionForwards
		state.selectedForwardID = 1
		view := openCatalogWindow(t, state)
		assertTableScrolls(t, state, view, &state.forwardTable, count,
			hitForwardRowList.At(0), hitForwardRowList.At(count-1), hitForwardScrollThumb,
			state.messages.ForwardListScroll, state.messages.ForwardColumnStatus,
			func(state *State) {
				if state.selectedForwardID != state.forwards[count-1].ID {
					t.Fatalf("keyboard selection did not reach the last forward: %d", state.selectedForwardID)
				}
			})
	})

	t.Run("keychain", func(t *testing.T) {
		state := newTestState(t, testOptions(t))
		for index := range count {
			identity := identityRecord{ID: uint32(index + 1), Path: fmt.Sprintf("/home/deploy/.ssh/key-%03d", index), Name: fmt.Sprintf("key-%03d", index)}
			identity.refreshCells(state.keyMessages())
			state.identities = append(state.identities, identity)
		}
		state.activeSection = sectionKeys
		state.selectedIdentityID = 1
		view := openCatalogWindow(t, state)
		assertTableScrolls(t, state, view, &state.keysTable, count,
			hitKeyRowList.At(0), hitKeyRowList.At(count-1), hitKeysScrollThumb,
			state.messages.KeyListScroll, state.messages.KeyColumnFingerprint,
			func(state *State) {
				if state.selectedIdentityID != state.identities[count-1].ID {
					t.Fatalf("keyboard selection did not reach the last key: %d", state.selectedIdentityID)
				}
			})
	})

	t.Run("jump routes", func(t *testing.T) {
		state := newTestState(t, testOptions(t))
		for index := range count {
			state.proxies = append(state.proxies, proxyRecord{ID: uint32(index + 1), TargetID: state.hosts[0].ID, JumpID: state.hosts[1].ID})
		}
		state.refreshProxies()
		state.activeSection = sectionProxies
		state.selectedProxyID = 1
		view := openCatalogWindow(t, state)
		assertTableScrolls(t, state, view, &state.proxyTable, count,
			hitProxyRowList.At(0), hitProxyRowList.At(count-1), hitProxyScrollThumb,
			state.messages.ProxyListScroll, state.messages.ProxyColumnRoute,
			func(state *State) {
				if state.selectedProxyID != state.proxies[count-1].ID {
					t.Fatalf("keyboard selection did not reach the last route: %d", state.selectedProxyID)
				}
			})
	})

	t.Run("snippets", func(t *testing.T) {
		state := newTestState(t, testOptions(t))
		for index := range count {
			state.snippets.list = append(state.snippets.list, snippet{
				Name: fmt.Sprintf("Snippet %03d", index), Command: "echo one\necho two\necho three",
			})
		}
		state.refreshSnippets()
		state.snippets.expanded = 1 // the first row's command is open.
		state.activeSection = sectionSnippets
		view := openCatalogWindow(t, state)
		scroll := state.sectionBodies.snippets.rows.ScrollState()
		if scroll == nil {
			t.Fatal("the snippets page has no scroll area")
		}
		rowHeight := components.TableRowHeight(state.Theme())
		if scroll.Content <= float32(count)*rowHeight {
			t.Fatalf("expanded detail did not add height to the list: %v <= %v",
				scroll.Content, float32(count)*rowHeight)
		}
		first, ok := findSnapshotHit(view.Snapshot(), hitSnippetRowList.At(0))
		if !ok {
			t.Fatal("the first row is not painted")
		}
		if _, ok := findSnapshotHit(view.Snapshot(), hitSnippetRowList.At(count-1)); ok {
			t.Fatal("the list fits every row without scrolling")
		}
		at := gfx.Point{X: first.Bounds.X + first.Bounds.Width/2, Y: first.Bounds.Y + first.Bounds.Height/2}
		if err := view.Move(at.X, at.Y); err != nil {
			t.Fatal(err)
		}
		bar, ok := findSnapshotLabel(view.Snapshot(), "Vertical scrollbar")
		if !ok {
			t.Fatal("a list taller than its viewport has no scrollbar under the pointer")
		}
		if bar.Bounds.Width != catalogBarGutter {
			t.Fatalf("scrollbar width = %v, want %v", bar.Bounds.Width, catalogBarGutter)
		}
		headerAboveRows(t, view.Snapshot(), state.messages.Tools.SnippetColumnCommand, bar.Bounds)

		if err := view.Scroll(gfx.ScrollEvent{X: at.X, Y: at.Y, DeltaY: 10000, Precise: true}); err != nil {
			t.Fatal(err)
		}
		if offset := state.sectionBodies.snippets.rows.ScrollState().Offset; offset <= 0 {
			t.Fatalf("wheel left the list at offset %v", offset)
		}
		if _, ok := findSnapshotHit(view.Snapshot(), hitSnippetRowList.At(count-1)); !ok {
			t.Fatal("the last row is still unreachable at the end of the list")
		}
		if _, ok := findSnapshotHit(view.Snapshot(), hitSnippetRowList.At(0)); ok {
			t.Fatal("the first row is still painted at the end of the list")
		}
		if err := view.Scroll(gfx.ScrollEvent{X: at.X, Y: at.Y, DeltaY: -10000, Precise: true}); err != nil {
			t.Fatal(err)
		}
		if offset := state.sectionBodies.snippets.rows.ScrollState().Offset; offset != 0 {
			t.Fatalf("scrolling back left the list at %v", offset)
		}

		bar, ok = findSnapshotLabel(view.Snapshot(), "Vertical scrollbar")
		if !ok {
			t.Fatal("the scrollbar disappeared before the drag")
		}
		if err := view.Driver().Press(bar.Bounds.X+bar.Bounds.Width/2, bar.Bounds.Y+bar.Bounds.Height/2, gfx.PointerButtonPrimary); err != nil {
			t.Fatal(err)
		}
		if err := view.Driver().Move(bar.Bounds.X+bar.Bounds.Width/2, bar.Bounds.Y+bar.Bounds.Height/2+40); err != nil {
			t.Fatal(err)
		}
		if offset := state.sectionBodies.snippets.rows.ScrollState().Offset; offset == 0 {
			t.Fatal("dragging the bar did not move the list")
		}
		if err := view.Driver().Release(bar.Bounds.X+bar.Bounds.Width/2, bar.Bounds.Y+bar.Bounds.Height/2+40, gfx.PointerButtonPrimary); err != nil {
			t.Fatal(err)
		}
	})
}

// openCatalogWindow shows a catalog page the way a window does, in a window
// small enough that more than one page of catalog rows overflows it.
func openCatalogWindow(t *testing.T, state *State) *headless.Session {
	t.Helper()
	config := state.WindowConfig(app.Config{
		Title: "Motty", Width: 1200, Height: 800, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
	})
	view, err := headless.Open(config, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { view.Close() })
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	return view
}

// assertTableScrolls holds one catalog table to the behavior the built-in table
// scroll shares, whatever the page is called.
func assertTableScrolls(t *testing.T, state *State, view *headless.Session, table *ui.TableState, count int, firstRow, lastRow, barID gfx.ID, barLabel, heading string, selectedLast func(*State)) {
	t.Helper()
	first, ok := findSnapshotHit(view.Snapshot(), firstRow)
	if !ok {
		t.Fatal("the first row is not painted")
	}
	if _, ok := findSnapshotHit(view.Snapshot(), lastRow); ok {
		t.Fatal("the list fits every row without scrolling")
	}
	if _, ok := findSnapshotHit(view.Snapshot(), barID); ok {
		t.Fatal("the scrollbar is painted before the pointer reaches the table")
	}

	// The bar is the hover style: it appears under the pointer, outside the rows.
	at := gfx.Point{X: first.Bounds.X + first.Bounds.Width/2, Y: first.Bounds.Y + first.Bounds.Height/2}
	if err := view.Move(at.X, at.Y); err != nil {
		t.Fatal(err)
	}
	bar, ok := findSnapshotHit(view.Snapshot(), barID)
	if !ok {
		t.Fatal("a list taller than its body has no scrollbar under the pointer")
	}
	if bar.Bounds.Width != catalogBarGutter {
		t.Fatalf("scrollbar width = %v, want %v", bar.Bounds.Width, catalogBarGutter)
	}
	headerAboveRows(t, view.Snapshot(), heading, bar.Bounds)

	// The wheel moves the rows, not the page around them.
	if err := view.Scroll(gfx.ScrollEvent{X: at.X, Y: at.Y, DeltaY: 10000, Precise: true}); err != nil {
		t.Fatal(err)
	}
	if table.Scroll.Offset <= 0 {
		t.Fatalf("wheel left the table at offset %v", table.Scroll.Offset)
	}
	if _, ok := findSnapshotHit(view.Snapshot(), lastRow); !ok {
		t.Fatal("the last row is still unreachable at the end of the list")
	}
	if _, ok := findSnapshotHit(view.Snapshot(), firstRow); ok {
		t.Fatal("the first row is still painted at the end of the list")
	}
	if err := view.Scroll(gfx.ScrollEvent{X: at.X, Y: at.Y, DeltaY: -10000, Precise: true}); err != nil {
		t.Fatal(err)
	}
	if table.Scroll.Offset != 0 {
		t.Fatalf("scrolling back left the table at %v", table.Scroll.Offset)
	}

	// The keyboard moves the selection to the last row and reveals it.
	first, ok = findSnapshotHit(view.Snapshot(), firstRow)
	if !ok {
		t.Fatal("the first row lost its hit")
	}
	if err := view.Driver().Click(first.Bounds.X+first.Bounds.Width/2, first.Bounds.Y+first.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Driver().Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if table.Selected() != count-1 {
		t.Fatalf("End selected row %d of %d", table.Selected(), count)
	}
	if maximum := table.Scroll.Max(); table.Scroll.Offset != maximum {
		t.Fatalf("End left the table at %v of %v", table.Scroll.Offset, maximum)
	}
	if _, ok := findSnapshotHit(view.Snapshot(), lastRow); !ok {
		t.Fatal("End did not reveal the last row")
	}
	selectedLast(state)

	// While the bar is held, the row under the pointer neither highlights nor
	// takes a click: the press owns the pointer wherever it moves.
	row, ok := findSnapshotHit(view.Snapshot(), lastRow)
	if !ok {
		t.Fatal("the last row lost its hit before the drag")
	}
	bar, ok = findSnapshotHit(view.Snapshot(), barID)
	if !ok {
		t.Fatal("the scrollbar disappeared before the drag")
	}
	atRow := gfx.Point{X: row.Bounds.X + 20, Y: row.Bounds.Y + row.Bounds.Height/2}
	if err := view.Driver().Press(bar.Bounds.X+bar.Bounds.Width/2, atRow.Y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := view.Driver().Move(atRow.X, atRow.Y); err != nil {
		t.Fatal(err)
	}
	if _, ok := findSnapshotHit(view.Snapshot(), lastRow); ok {
		t.Fatal("a row still takes hover under a held scrollbar")
	}
	if err := view.Driver().Release(atRow.X, atRow.Y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if _, ok := findSnapshotHit(view.Snapshot(), lastRow); !ok {
		t.Fatal("rows did not become interactive again after the bar was released")
	}
}

// findSnapshotLabel finds the front-most control with an accessibility label.
func findSnapshotLabel(snapshot app.Snapshot, label string) (gfx.Hit, bool) {
	for index := len(snapshot.Hits) - 1; index >= 0; index-- {
		hit := snapshot.Hits[index]
		if hit.Label == label && !hit.IsLabel && !hit.Backdrop && !hit.ID.IsZero() {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// headerAboveRows checks the column headings do not scroll with the rows: they
// stay above the row viewport wherever its offset is.
func headerAboveRows(t *testing.T, snapshot app.Snapshot, heading string, list gfx.Rect) {
	t.Helper()
	for _, text := range snapshot.Texts {
		if text.Value == heading && text.Bounds.Y < list.Y {
			return
		}
	}
	t.Fatalf("heading %q is not above the scrolling body", heading)
}
