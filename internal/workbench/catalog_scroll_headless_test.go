//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
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
		assertCatalogListScrolls(t, state, view, &state.forwardList,
			hitForwardRowList.At(1), hitForwardRowList.At(count), hitForwardScrollThumb, state.messages.ForwardColumnStatus)
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
		assertCatalogListScrolls(t, state, view, &state.keysList,
			hitKeyRowList.At(1), hitKeyRowList.At(count), hitKeysScrollThumb, state.messages.KeyColumnFingerprint)
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
		assertCatalogListScrolls(t, state, view, &state.proxyList,
			hitProxyRowList.At(1), hitProxyRowList.At(count), hitProxyScrollThumb, state.messages.ProxyColumnRoute)
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
		rowHeight := components.TableRowHeight(state.Theme())
		if state.snippets.scroll.content <= float32(count)*rowHeight {
			t.Fatalf("expanded detail did not add height to the list: %v <= %v",
				state.snippets.scroll.content, float32(count)*rowHeight)
		}
		assertCatalogListScrolls(t, state, view, &state.snippets.scroll,
			hitSnippetRowList.At(0), hitSnippetRowList.At(count-1), hitSnippetScrollThumb, state.messages.Tools.SnippetColumnCommand)
	})
}

// openCatalogWindow shows a catalog page the way a window does, in a window
// small enough that more than one page of catalog rows overflows it.
func openCatalogWindow(t *testing.T, state *State) *headless.Session {
	t.Helper()
	view, err := headless.Open(app.Config{
		Title: "Motty", Width: 1200, Height: 800, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { view.Close() })
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	return view
}

// assertCatalogListScrolls holds one catalog table to the behavior every one of
// them shares, whatever the page is called.
func assertCatalogListScrolls(t *testing.T, state *State, view *headless.Session, list *catalogList, firstRow, lastRow, thumb gfx.ID, heading string) {
	t.Helper()
	bounds := list.bounds
	if bounds.Width <= 0 || bounds.Height <= 0 {
		t.Fatalf("catalog list has no body: %+v", bounds)
	}
	if list.content <= bounds.Height {
		t.Fatalf("list content %v fits its body %v", list.content, bounds.Height)
	}
	if _, ok := findSnapshotHit(view.Snapshot(), firstRow); !ok {
		t.Fatal("the first row is not painted")
	}
	if _, ok := findSnapshotHit(view.Snapshot(), lastRow); ok {
		t.Fatal("the list fits every row without scrolling")
	}
	// The bar is the hover style: it appears under the pointer, not at rest.
	at := gfx.Point{X: bounds.X + bounds.Width/2, Y: bounds.Y + bounds.Height/2}
	if err := view.Move(at.X, at.Y); err != nil {
		t.Fatal(err)
	}
	if _, ok := findSnapshotHit(view.Snapshot(), thumb); !ok {
		t.Fatal("a list taller than its body has no scrollbar under the pointer")
	}
	headerAboveRows(t, view.Snapshot(), heading, bounds)

	// The wheel moves the rows, not the page around them.
	if err := view.Scroll(gfx.ScrollEvent{X: at.X, Y: at.Y, DeltaY: 10000, Precise: true}); err != nil {
		t.Fatal(err)
	}
	if list.scroll <= 0 {
		t.Fatalf("wheel left the list at offset %v", list.scroll)
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
	if list.scroll != 0 {
		t.Fatalf("scrolling back left the list at %v", list.scroll)
	}
	headerAboveRows(t, view.Snapshot(), heading, bounds)

	// A press on the bar focuses the list, and the keyboard reaches its end.
	bar, ok := findSnapshotHit(view.Snapshot(), thumb)
	if !ok {
		t.Fatal("the scrollbar disappeared before the drag")
	}
	if err := view.Driver().Click(bar.Bounds.X+bar.Bounds.Width/2, bar.Bounds.Y+bar.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Driver().Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	maximum := list.area(state.Theme()).Geometry().MaxOffsetY
	if maximum <= 0 || list.scroll != maximum {
		t.Fatalf("End left the list at %v of %v", list.scroll, maximum)
	}
	if _, ok := findSnapshotHit(view.Snapshot(), lastRow); !ok {
		t.Fatal("End did not reveal the last row")
	}

	// While the bar is held, the row under the pointer neither highlights nor
	// takes a click: the press owns the pointer wherever it moves.
	row, ok := findSnapshotHit(view.Snapshot(), lastRow)
	if !ok {
		t.Fatal("the last row lost its hit before the drag")
	}
	bar, ok = findSnapshotHit(view.Snapshot(), thumb)
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
