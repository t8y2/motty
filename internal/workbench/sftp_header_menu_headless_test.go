//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
	layoutcore "motty/internal/workspace"
)

func TestSFTPToolbarCustomizationFromContextMenu(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state, session, _, _ := openTestSFTP(t)
			state.setLanguage(i18n.Chinese)
			state.setAppearance(appearanceLight)
			// A files-only reset must preserve customization of the other bars.
			state.toolbars.customize(toolbarWindow)[0].Enabled = false
			state.toolbars.customize(toolbarTerminal)[1].Enabled = false
			other := state.toolbars.snapshot()
			view, err := headless.Open(state.WindowConfig(app.Config{
				Title: "files toolbar customization", Width: 1400, Height: 900, Tree: Tree(),
				Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize,
			}), nil, headless.Options{Scale: scale})
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
			rightClick := func(id gfx.ID) {
				t.Helper()
				hit := hitOf(id)
				must(view.Driver().ClickButton(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2, gfx.PointerButtonSecondary))
				must(view.Rest())
			}
			shot := func(name string) {
				t.Helper()
				if dir := os.Getenv("MOTTY_TOOLBAR_SCREENSHOT_DIR"); dir != "" {
					must(os.MkdirAll(dir, 0700))
					must(view.Screenshot(filepath.Join(dir, fmt.Sprintf("sftp-%s-%g.png", name, scale))))
				}
			}
			must(view.Rest())
			rightClick(hitSFTPUpload)
			shot("context-menu")
			for _, id := range []gfx.ID{hitToolbarDockHide, hitToolbarDockCustomize, hitToolbarDockReset} {
				hitOf(id)
			}
			if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitToolbarDockTop); ok {
				t.Fatal("the files toolbar offered moving an action out of its panel")
			}
			click(hitToolbarDockHide)
			if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitSFTPUpload); ok {
				t.Fatal("Hide left the upload button visible")
			}

			click(hitSFTPHeaderMore)
			shot("more-menu")
			for _, text := range view.Snapshot().Texts {
				if text.Value == state.messages.Toolbars.Customize || text.Value == state.messages.Toolbars.ResetLayout {
					t.Fatal("the More menu still offers toolbar customization")
				}
			}
			// A press outside the page menu closes it.
			must(view.Click(900, 600))
			must(view.Rest())
			if session.files.active().headerMenuOpen {
				t.Fatal("a press outside the files menu left it open")
			}
			rightClick(hitSFTPUploadDirectory)
			click(hitToolbarDockCustomize)
			if session.files.active().headerMenuOpen || state.modal != modalToolbarSettings || state.toolbarEditor.group != toolbarSFTP {
				t.Fatal("right-click Customize did not replace the menu with the files toolbar editor")
			}
			shot("customize")
			click(hitToolbarDisableAll)
			click(hitToolbarSettingsClose)
			for _, spec := range toolbarCatalog[toolbarSFTP] {
				if _, ok := hitByIDSnapshot(view.Snapshot().Hits, spec.id); ok {
					t.Fatalf("disabled toolbar button %s is still visible", spec.key)
				}
			}
			saved, _, err := loadState(state.options.StatePath)
			must(err)
			restored := newTestState(t, state.options)
			menuLabelState = state // the window is the one whose language names the menu's rows
			if !reflect.DeepEqual(restored.toolbars.snapshot(), saved.Toolbars) {
				t.Fatal("toolbar visibility did not survive reopening")
			}
			for _, entry := range restored.toolbars.entries(toolbarSFTP) {
				if entry.Enabled {
					t.Fatalf("reopening enabled %s", entry.Key)
				}
			}

			// More stays available even with every configurable button hidden.
			must(view.Resize(800, 600))
			must(view.Rest())
			rightClick(hitSFTPHeaderMore)
			if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitToolbarDockHide); ok {
				t.Fatal("More can be hidden, losing access to customization")
			}
			click(hitToolbarDockCustomize)
			if state.toolbarEditor.group != toolbarSFTP {
				t.Fatal("right-click Customize opened another toolbar's editor")
			}
			click(hitToolbarSettingsClose)
			rightClick(hitSFTPHeaderMore)
			click(hitToolbarDockReset)
			hitOf(hitSFTPUpload)
			if session.files.active().headerMenuOpen || state.toolbars.layouts[toolbarSFTP] != nil {
				t.Fatal("files toolbar reset did not restore its defaults and close the menu")
			}
			saved, _, err = loadState(state.options.StatePath)
			must(err)
			if saved.Toolbars.SFTP != nil || !reflect.DeepEqual(saved.Toolbars.Window, other.Window) || !reflect.DeepEqual(saved.Toolbars.Terminal, other.Terminal) {
				t.Fatal("files toolbar reset was not persisted independently of the other toolbars")
			}
			rightClick(hitSFTPUpload)
			click(hitToolbarDockHide)
			rightClick(hitSFTPHeaderMore)
			click(hitToolbarDockReset)
			hitOf(hitSFTPUpload)
			if state.toolbars.layouts[toolbarSFTP] != nil || !reflect.DeepEqual(state.toolbars.layouts[toolbarWindow], other.Window) || !reflect.DeepEqual(state.toolbars.layouts[toolbarTerminal], other.Terminal) {
				t.Fatal("right-click reset changed another toolbar")
			}
			// Busy buttons still permit hiding/customizing and never run their action.
			session.files.busy = true
			must(view.Rest())
			rightClick(hitSFTPUpload)
			click(hitToolbarDockHide)
			session.files.busy = false
			must(view.Rest())
			if state.toolbarVisible(toolbarSFTP, hitSFTPUpload) || session.files.dialog != sftpDialogNone {
				t.Fatal("the disabled upload button could not be hidden safely")
			}
		})
	}
}

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
			dualClickMenuLabel(t, view, state.panelMenuRowLabel(session.files, contextID(hitSFTPHeaderHidden)))
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
			dualClickMenuLabel(t, view, state.panelMenuRowLabel(session.files, contextID(hitSFTPNewDirectory)))
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

	// The page menu's backdrop covers the window, not just the panel: a press on
	// the bar under it closes the menu instead of moving or navigating.
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
