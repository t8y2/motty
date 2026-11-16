//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	layoutcore "motty/internal/workspace"
)

func TestSFTPToolbarOverflowActionsAfterNativeResize(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state, session, _, _ := openTestSFTP(t)
			// A customized order can put a primary action into overflow.
			entries := state.toolbars.customize(toolbarSFTP)
			for index, entry := range entries {
				if entry.Key == "new-folder" {
					copy(entries[index:], entries[index+1:])
					entries[len(entries)-1] = entry
					break
				}
			}
			workspace := state.deck.ActiveWorkspace()
			context, _ := state.deck.Context(workspace)
			context.Width = layoutcore.MaxContextWidth
			if err := state.deck.SetContext(workspace, context); err != nil {
				t.Fatal(err)
			}
			view, err := headless.Open(app.Config{
				Title: "files toolbar overflow", Width: 1400, Height: 900, Tree: Tree(),
				Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize,
			}, nil, headless.Options{Scale: scale})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			hitOf := func(id gfx.ID) gfx.Hit {
				t.Helper()
				hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
				if !ok {
					t.Fatalf("no hit for %s", tree.Text(id))
				}
				return hit
			}
			click := func(id gfx.ID) {
				t.Helper()
				hit := hitOf(id)
				must(view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2))
				must(view.Rest())
			}
			must(view.Rest())
			divider := hitOf(hitContextDivider)
			y := divider.Bounds.Y + divider.Bounds.Height/2
			x := state.contextRect.X + layoutcore.MinContextWidth + .5
			must(view.Press(divider.Bounds.X+divider.Bounds.Width/2, y))
			must(view.Move(x, y))
			must(view.Release(x, y))
			must(view.Rest())
			if _, ok := hitByIDSnapshot(view.Snapshot().Hits, contextID(hitSFTPHiddenToggle)); !ok {
				t.Fatal("the hidden-files indicator left the toolbar after narrowing")
			}
			click(contextID(hitSFTPHeaderMore))
			click(contextID(hitSFTPHeaderHidden))
			if session.files.active().headerMenuOpen || session.files.active().showHidden {
				t.Fatal("overflow hidden-files action did not close the menu and toggle visibility")
			}
			// The drawer has less room because of its own surface padding.
			must(view.Resize(800, 900))
			must(view.Rest())
			if _, ok := hitByIDSnapshot(view.Snapshot().Hits, contextID(hitSFTPNewDirectory)); ok {
				t.Fatal("New Folder did not move into overflow in the drawer")
			}
			click(contextID(hitSFTPHeaderMore))
			if !session.files.active().headerMenuOpen {
				t.Fatal("More cannot be opened after narrowing")
			}
			click(contextID(hitSFTPNewDirectory))
			if session.files.active().headerMenuOpen || session.files.dialog != sftpDialogCreateDirectory {
				t.Fatal("overflow New Folder did not close the menu and open its dialog")
			}
		})
	}
}

// The files panel's More menu dims the window, so its backdrop reaches the window
// bar. A press on the bar's Settings button with the menu open must only close the
// menu: the menu cannot be painted in the panel's own canvas, because the page
// intersects everything an embedded canvas contributes with its own clip, which
// would leave the backdrop covering the panel alone. The press would then land on
// the window's move region, and neither open Settings nor close the menu.
func TestSettingsButtonWithFilesMenuOpenOnlyClosesTheMenu(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view, err := headless.Open(app.Config{
		Title: "files menu", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	hitOf := func(id gfx.ID) gfx.Hit {
		t.Helper()
		hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
		if !ok {
			t.Fatalf("no hit for %s", tree.Text(id))
		}
		return hit
	}
	click := func(id gfx.ID) {
		t.Helper()
		hit := hitOf(id)
		if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
	}

	click(contextID(hitSFTPHeaderMore))
	if !session.files.active().headerMenuOpen {
		t.Fatal("the More menu did not open")
	}

	// The backdrop is the menu's, and it covers the window, not just the panel.
	window := state.windowRect()
	backdrop := hitOf(contextID(hitSFTPHeaderMenuDismiss))
	if backdrop.Bounds != window {
		t.Fatalf("backdrop covers %+v, want the whole window %+v", backdrop.Bounds, window)
	}
	gear := hitOf(hitWindowSettings)
	if kind := view.PressAt(gear.Bounds.X+gear.Bounds.Width/2, gear.Bounds.Y+gear.Bounds.Height/2); kind == app.PressMove {
		t.Fatal("a press on the bar under the backdrop starts a window move")
	}

	section := state.activeSection
	click(hitWindowSettings)
	if session.files.active().headerMenuOpen {
		t.Fatal("the press on Settings did not close the menu")
	}
	if state.activeSection != section {
		t.Fatalf("the closing press navigated as well: %v -> %v", section, state.activeSection)
	}

	// With the menu gone the same button opens Settings.
	click(hitWindowSettings)
	if state.activeSection == section {
		t.Fatal("Settings did not open after the menu closed")
	}
}

// A press anywhere outside the menu closes it, not only one on a control: with the
// backdrop covering the window the page has nothing left to click.
func TestPressOutsideTheFilesMenuClosesIt(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view, err := headless.Open(app.Config{
		Title: "files menu", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	more, ok := hitByIDSnapshot(view.Snapshot().Hits, contextID(hitSFTPHeaderMore))
	if !ok {
		t.Fatal("no More button")
	}
	if err := view.Click(more.Bounds.X+more.Bounds.Width/2, more.Bounds.Y+more.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !session.files.active().headerMenuOpen {
		t.Fatal("the More menu did not open")
	}
	// The middle of the terminal, well outside the panel.
	if err := view.Click(900, 600); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if session.files.active().headerMenuOpen {
		t.Fatal("a press outside the menu left it open")
	}
}
