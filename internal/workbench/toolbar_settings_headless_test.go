//go:build windows || darwin || linux

package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

func TestToolbarSettingsNativeControlsDragKeyboardAndSwitchAcrossScales(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.setLanguage(i18n.Chinese)
			openGeneralSettings(state)
			var invalidator app.Invalidator
			state.options.Invalidate = invalidator.Invalidate
			view, err := headless.Open(app.Config{Width: 1200, Height: 900, Tree: Tree(), Invalidator: &invalidator, OnWake: state.Wake, Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize}, nil, headless.Options{Scale: scale})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			click := func(id ui.ID) {
				t.Helper()
				box := dualHit(t, view, id)
				if err := view.Click(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
					t.Fatal(err)
				}
			}
			shot := func(name string) {
				t.Helper()
				if dir := os.Getenv("MOTTY_TOOLBAR_SCREENSHOT_DIR"); dir != "" {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("%s-%g.png", name, scale))); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
						t.Fatal(err)
					}
				}
			}
			shot("toolbar-general")
			for group := toolbarWindow; group <= toolbarSFTP; group++ {
				click(hitToolbarSettingsOpen[group])
				view.Driver().Advance(250 * time.Millisecond)
				shot(fmt.Sprintf("toolbar-opened-%d", group))
				original := state.toolbars.entries(group)[0].Key
				first := hitToolbarHandles.At(toolbarEntryIndex(group, original))
				lastEntries := state.toolbars.entries(group)
				last := hitToolbarHandles.At(toolbarEntryIndex(group, lastEntries[len(lastEntries)-1].Key))
				a, b := dualHit(t, view, first), dualHit(t, view, last)
				x, y := a.X+a.Width/2, a.Y+a.Height/2
				if err := view.Press(x, y); err != nil {
					t.Fatal(err)
				}
				if err := view.Move(b.X+b.Width/2, b.Y+b.Height/2); err != nil {
					t.Fatal(err)
				}
				if err := view.Driver().Leave(); err != nil {
					t.Fatal(err)
				}
				if err := view.Release(b.X+b.Width/2, b.Y+b.Height/2); err != nil {
					t.Fatal(err)
				}
				if state.toolbarEditor.list.Dragging() || state.toolbars.entries(group)[0].Key != original {
					t.Fatal("losing pointer capture retained or committed an interrupted drag")
				}
				if err := view.Press(x, y); err != nil {
					t.Fatal(err)
				}
				if err := view.Move(b.X+b.Width/2, b.Y+b.Height/2); err != nil {
					t.Fatal(err)
				}
				if state.toolbars.entries(group)[0].Key != original {
					t.Fatal("drag saved before drop")
				}
				if err := view.Release(b.X+b.Width/2, b.Y+b.Height/2); err != nil {
					t.Fatal(err)
				}
				entries := state.toolbars.entries(group)
				if entries[len(entries)-1].Key != original {
					t.Fatalf("group %d did not reorder on drop: %+v", group, entries)
				}
				if err := view.Keys("Home"); err != nil {
					t.Fatal(err)
				}
				if state.toolbars.entries(group)[0].Key != original {
					t.Fatal("keyboard could not move the focused entry back")
				}
				index := toolbarEntryIndex(group, original)
				click(hitToolbarToggles.At(index))
				if state.toolbars.entries(group)[0].Enabled {
					t.Fatal("switch failed to disable the entry")
				}
				view.Driver().Advance(250 * time.Millisecond)
				shot(fmt.Sprintf("toolbar-editor-%d", group))
				click(hitToolbarDisableAll)
				for _, entry := range state.toolbars.entries(group) {
					if entry.Enabled {
						t.Fatal("disable all left an entry enabled")
					}
				}
				click(hitToolbarReset)
				click(hitToolbarSettingsClose)
				view.Driver().Advance(250 * time.Millisecond)
			}
		})
	}
}

func TestToolbarSettingsSmallWindowCanScrollDuringDrag(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.setAppearance(appearanceLight)
	state.openToolbarSettings(toolbarTerminal)
	var invalidator app.Invalidator
	state.options.Invalidate = invalidator.Invalidate
	view, err := headless.Open(app.Config{Width: 640, Height: 480, Tree: Tree(), Invalidator: &invalidator, OnWake: state.Wake, Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize}, nil, headless.Options{Scale: 1.25})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	view.Driver().Advance(250 * time.Millisecond)
	if state.toolbarEditor.geometry.MaxOffsetY <= 0 {
		t.Fatal("small window fixture did not scroll")
	}
	entries := state.toolbars.entries(toolbarTerminal)
	first, last := entries[0].Key, entries[len(entries)-1].Key
	a := dualHit(t, view, hitToolbarHandles.At(toolbarEntryIndex(toolbarTerminal, first)))
	x, y := a.X+a.Width/2, a.Y+a.Height/2
	if err := view.Press(x, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(x, y+20); err != nil {
		t.Fatal(err)
	}
	box := state.toolbarEditor.geometry.Viewport
	if err := view.Scroll(ui.ScrollEvent{X: box.X + box.Width/2, Y: box.Y + box.Height/2, DeltaY: 1000}); err != nil {
		t.Fatal(err)
	}
	b := dualHit(t, view, hitToolbarHandles.At(toolbarEntryIndex(toolbarTerminal, last)))
	if err := view.Move(b.X+b.Width/2, b.Y+b.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(b.X+b.Width/2, b.Y+b.Height/2); err != nil {
		t.Fatal(err)
	}
	entries = state.toolbars.entries(toolbarTerminal)
	if entries[len(entries)-1].Key != first {
		t.Fatal("scrolling during a drag changed the wrong target")
	}
	for _, id := range []ui.ID{hitToolbarEnableAll, hitToolbarDisableAll, hitToolbarReset, hitToolbarSettingsClose} {
		bounds := dualHit(t, view, id)
		if bounds.Y < 0 || bounds.Y+bounds.Height > 480 {
			t.Fatal("small window made a footer action unreachable")
		}
	}
}
