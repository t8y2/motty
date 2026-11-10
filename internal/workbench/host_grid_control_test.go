//go:build darwin || windows || linux

package workbench

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

// gridPageWindow opens the host grid through the workbench page, as a window
// does, so clicks, focus, menus and the grid's page drag are routed by the page.
func gridPageWindow(t *testing.T, state *State, size gfx.Size) *headless.Session {
	t.Helper()
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Hosts", Width: int(size.Width), Height: int(size.Height), Tree: Tree(),
		Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, OnResize: state.Resize,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	return view
}

// A card takes the pointer through the page: a click selects and focuses it, its
// checkbox toggles the checked set, and a secondary press opens the host menu.
func TestHostGridCardTakesPointerSelectionCheckboxAndMenu(t *testing.T) {
	state := manyHostsState(t, 3)
	state.hostView = hostViewGrid
	size := gfx.Size{Width: 1400, Height: 900}
	view := gridPageWindow(t, state, size)
	host := state.hosts[0]
	center := func(id gfx.ID) (float32, float32) {
		t.Helper()
		hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
		if !ok {
			t.Fatalf("%s is not painted", tree.Text(id))
		}
		return hit.Bounds.X + hit.Bounds.Width/2, hit.Bounds.Y + hit.Bounds.Height/2
	}
	card, check := hitHostList.At(int(host.ID)), hitHostCheckList.At(int(host.ID))
	x, y := center(card)
	if err := view.Driver().Click(x, y); err != nil {
		t.Fatal(err)
	}
	if state.selectedHostID != host.ID {
		t.Fatalf("the card click selected %d, want %d", state.selectedHostID, host.ID)
	}
	if focused := view.Snapshot().Focused; focused != card {
		t.Fatalf("the card click focused %s, want %s", tree.Text(focused), tree.Text(card))
	}
	x, y = center(check)
	if err := view.Driver().Click(x, y); err != nil {
		t.Fatal(err)
	}
	if !state.hostSelected(host.ID) {
		t.Fatal("the card checkbox did not check the host")
	}
	// Checking a host adds the batch actions row, so the card moved: read the
	// fresh frame before pressing it.
	x, y = center(card)
	if err := view.Driver().ClickButton(x, y, gfx.PointerButtonSecondary); err != nil {
		t.Fatal(err)
	}
	if !state.hostMenu.open || state.hostMenu.host != host.ID {
		t.Fatalf("the card's secondary press opened %+v", state.hostMenu)
	}
}

// Double-clicking a card connects to its host, as double-clicking a list row does.
func TestDoubleClickingACardConnectsToIt(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.hostView = hostViewGrid
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	host := state.hosts[0]
	card, ok := hitByID(canvas, hitHostList.At(int(host.ID)))
	if !ok {
		t.Fatal("the first card is not painted")
	}
	doubleClick(state, card.ID)
	if len(world.configs) != 1 || world.configs[0].Host != host.Address || world.configs[0].User != host.User {
		t.Fatalf("the card double click dialed %+v, want %s@%s", world.configs, host.User, host.Address)
	}
}

// Dragging a card grid group header onto another moves its hosts, through the
// page that routes the part drag.
func TestHostGridGroupDragThroughThePage(t *testing.T) {
	state := nestedHostGroupState(t)
	state.hostView = hostViewGrid
	size := gfx.Size{Width: 1400, Height: 900}
	view := gridPageWindow(t, state, size)
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	hit := func(path string) gfx.Hit {
		t.Helper()
		index := state.hostGroupIndex(path)
		if index < 0 {
			t.Fatalf("missing group %q", path)
		}
		found, ok := hitByIDSnapshot(view.Snapshot().Hits, hitHostGroupOpenList.At(index))
		if !ok {
			t.Fatalf("group %q has no header", path)
		}
		return found
	}
	source, target := hit("Team/Prod"), hit("Team2")
	driver := view.Driver()
	if err := driver.Press(source.Bounds.X+20, source.Bounds.Y+source.Bounds.Height/2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	// Take a fresh frame once the press has been routed to the page.
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	x, y := target.Bounds.X+target.Bounds.Width/2, target.Bounds.Y+target.Bounds.Height/2
	if err := driver.Move(x, y); err != nil {
		t.Fatal(err)
	}
	if !state.hostGridDrag.state.Dragging() || state.hostGridDrag.state.Target() != target.ID {
		t.Fatalf("part drag source=%v target=%v dragging=%v",
			state.hostGridDrag.state.Source(), state.hostGridDrag.state.Target(), state.hostGridDrag.state.Dragging())
	}
	if err := driver.Release(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if state.hosts[0].Group != "Team2/Prod/EU" {
		t.Fatalf("the page drag left the subtree at %q", state.hosts[0].Group)
	}
}

// TestProductionDeclaresNoUIView keeps the grid on Control parts: Motty no longer
// paints a surface through ui.View, so every page part is declared by the page
// and routed by it.
func TestProductionDeclaresNoUIView(t *testing.T) {
	root := moduleRoot(t)
	count, err := countUIViewUses(root)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d ui.View uses remain in production sources under %s", count, root)
	}
}

// moduleRoot is the directory of the module holding the test, wherever the test
// binary was started.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}
		dir = parent
	}
}

// countUIViewUses counts selector expressions ui.View in the non-test Go files
// below root, skipping directories that hold no production source.
func countUIViewUses(root string) (count int, err error) {
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".private", "node_modules", "testdata", "vendor", "website", "artifacts", "assets":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "View" {
				return true
			}
			if qualifier, ok := selector.X.(*ast.Ident); ok && qualifier.Name == "ui" {
				count++
			}
			return true
		})
		return nil
	})
	return count, err
}

// A click on a group header still toggles it: the press is the header's drag
// source, but a press that does not move stays a click.
func TestHostGridHeaderClickTogglesGroup(t *testing.T) {
	state := nestedHostGroupState(t)
	state.hostView = hostViewGrid
	size := gfx.Size{Width: 1400, Height: 900}
	view := gridPageWindow(t, state, size)
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	index := state.hostGroupIndex("Team/Prod")
	header, ok := hitByIDSnapshot(view.Snapshot().Hits, hitHostGroupOpenList.At(index))
	if !ok {
		t.Fatal("the group header is not painted")
	}
	expanded := state.hostGroups[index].Expanded
	if err := view.Driver().Click(header.Bounds.X+20, header.Bounds.Y+header.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.hostGroups[index].Expanded == expanded {
		t.Fatal("clicking the group header did not toggle the group")
	}
}
