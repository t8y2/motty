//go:build windows || darwin || linux

package workbench

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

// toolbarRowHit is the bounds of the editor's row for the entry key of group, which the
// page's list names by its label.

func toolbarRowHit(t *testing.T, state *State, view *headless.Session, group toolbarGroup, key string) (gfx.Rect, bool) {
	t.Helper()
	spec, _ := toolbarSpecFor(group, key)
	label := state.toolbarLabel(spec.id)
	for _, h := range view.Snapshot().Hits {
		if h.Role == gfx.RoleOption && h.Label == label && !h.Block {
			return h.Bounds, true
		}
	}
	return gfx.Rect{}, false
}

func toolbarHandleHit(t *testing.T, state *State, view *headless.Session, group toolbarGroup, key string) (gfx.Rect, bool) {
	t.Helper()
	row, ok := toolbarRowHit(t, state, view, group, key)
	if !ok {
		return gfx.Rect{}, false
	}
	spec, _ := toolbarSpecFor(group, key)
	label := state.toolbarLabel(spec.id)
	for _, hit := range view.Snapshot().Hits {
		if hit.Role == gfx.RoleButton && hit.Label == label && hit.Bounds.Width == 28 &&
			math.Abs(float64(hit.Bounds.X-(row.X+4))) < .75 {
			return hit.Bounds, true
		}
	}
	return gfx.Rect{}, false
}

func TestToolbarSettingsNativeControlsDragKeyboardAndSwitchAcrossScales(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.setLanguage(i18n.Chinese)
			openGeneralSettings(state)
			var invalidator app.Invalidator
			state.options.Invalidate = invalidator.Invalidate
			view, err := headless.Open(state.WindowConfig(app.Config{Width: 1200, Height: 900, Tree: Tree(), Invalidator: &invalidator, OnWake: state.Wake, Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize}), nil, headless.Options{Scale: scale})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			click := func(id gfx.ID) {
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
				// The Logo color row can put a toolbar button below the viewport.
				// Reach it through the same scrolling page a user would use.
				for attempt := 0; attempt < 8; attempt++ {
					if hit, ok := hitByIDSnapshot(view.Snapshot().Hits, hitToolbarSettingsOpen[group]); ok && hit.Bounds.Height >= 24 {
						break
					}
					bounds := state.settingsWorkspaceBounds(gfx.Rect{Width: 1200, Height: 900})
					if err := view.Scroll(gfx.ScrollEvent{X: bounds.X + bounds.Width - 40, Y: bounds.Y + bounds.Height/2, DeltaY: 160}); err != nil {
						t.Fatal(err)
					}
					view.Driver().Advance(300 * time.Millisecond)
				}
				click(hitToolbarSettingsOpen[group])
				view.Driver().Advance(250 * time.Millisecond)
				shot(fmt.Sprintf("toolbar-opened-%d", group))
				original := state.toolbars.entries(group)[0].Key
				// The editor scrolls, so a long group does not show every entry at
				// once: the drag ends on the last one in view.
				lastEntries := state.toolbars.entries(group)
				lastSlot := len(lastEntries) - 1
				for lastSlot > 0 {
					// A row cut by the list's edge is not a place to drop on.
					if row, ok := toolbarRowHit(t, state, view, group, lastEntries[lastSlot].Key); ok && row.Height >= 48 {
						break
					}
					lastSlot--
				}
				b, _ := toolbarRowHit(t, state, view, group, lastEntries[lastSlot].Key)
				// The row starts with a 28px drag-handle button, as the
				// painted reorder list did.
				handleRect, ok := toolbarHandleHit(t, state, view, group, original)
				if !ok {
					t.Fatalf("group %d has no 28px drag handle at the row's start", group)
				}
				// The drag starts on the handle, as the painted reorder list's did.
				x, y := handleRect.X+handleRect.Width/2, handleRect.Y+handleRect.Height/2
				if err := view.Press(x, y); err != nil {
					t.Fatal(err)
				}
				if err := view.Move(b.X+b.Width/2, b.Y+b.Height/2); err != nil {
					t.Fatal(err)
				}
				if err := view.Driver().Cancel(); err != nil {
					t.Fatal(err)
				}
				if err := view.Release(b.X+b.Width/2, b.Y+b.Height/2); err != nil {
					t.Fatal(err)
				}
				if state.toolbars.entries(group)[0].Key != original {
					t.Fatal("a cancelled press retained or committed an interrupted drag")
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
				if entries[lastSlot].Key != original {
					t.Fatalf("group %d did not reorder %s onto %s on drop at (%g,%g), from handle %v there: %+v",
						group, original, lastEntries[lastSlot].Key, b.X+b.Width/2, b.Y+b.Height/2, handleRect, entries)
				}
				for range lastSlot {
					if err := view.Keys("Up"); err != nil {
						t.Fatal(err)
					}
				}
				if state.toolbars.entries(group)[0].Key != original {
					t.Fatal("keyboard could not move the focused entry back")
				}
				index := toolbarEntryIndex(group, original)
				before := state.toolbars.entries(group)[0].Enabled
				click(hitToolbarToggles.At(index))
				if state.toolbars.entries(group)[0].Enabled == before {
					t.Fatal("switch failed to flip the entry")
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

func TestToolbarSettingsSizeSwitchTakesClicks(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openToolbarSettings(toolbarTerminal)
	var invalidator app.Invalidator
	state.options.Invalidate = invalidator.Invalidate
	view, err := headless.Open(state.WindowConfig(app.Config{Width: 1200, Height: 900, Tree: Tree(), Invalidator: &invalidator, OnWake: state.Wake, Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	view.Driver().Advance(250 * time.Millisecond)
	for _, test := range []struct {
		label string
		want  terminalToolbarSize
	}{
		{state.messages.SettingsToolbarSizeMedium, terminalToolbarMedium},
		{state.messages.SettingsToolbarSizeLarge, terminalToolbarLarge},
	} {
		segment := dualChoiceHit(t, view, test.label)
		if err := view.Click(segment.Bounds.X+segment.Bounds.Width/2, segment.Bounds.Y+segment.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
		if state.terminalToolbarSize != test.want {
			t.Fatalf("size after clicking %q = %s", test.label, state.terminalToolbarSize.String())
		}
	}
}

func TestToolbarSettingsSmallWindowCanScrollDuringDrag(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.setAppearance(appearanceLight)
	state.openToolbarSettings(toolbarTerminal)
	var invalidator app.Invalidator
	state.options.Invalidate = invalidator.Invalidate
	view, err := headless.Open(state.WindowConfig(app.Config{Width: 640, Height: 480, Tree: Tree(), Invalidator: &invalidator, OnWake: state.Wake, Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize}), nil, headless.Options{Scale: 1.25})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	view.Driver().Advance(250 * time.Millisecond)
	if state.toolbarEditor.list.ScrollState().Max() <= 0 {
		t.Fatal("small window fixture did not scroll")
	}
	entries := state.toolbars.entries(toolbarTerminal)
	firstKey, last := entries[0].Key, entries[len(entries)-1].Key
	first, ok := toolbarHandleHit(t, state, view, toolbarTerminal, firstKey)
	if !ok {
		t.Fatal("the first row has no drag handle")
	}
	x, y := first.X+first.Width/2, first.Y+first.Height/2
	if err := view.Press(x, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(x, y+20); err != nil {
		t.Fatal(err)
	}
	box := state.toolbarEditor.list.Rect()
	if err := view.Scroll(gfx.ScrollEvent{X: box.X + box.Width/2, Y: box.Y + box.Height/2, DeltaY: 1000}); err != nil {
		t.Fatal(err)
	}
	target, ok := toolbarHandleHit(t, state, view, toolbarTerminal, last)
	if !ok {
		t.Fatal("the last row has no drag handle after scrolling")
	}
	if err := view.Move(target.X+target.Width/2, target.Y+target.Height-4); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(target.X+target.Width/2, target.Y+target.Height-4); err != nil {
		t.Fatal(err)
	}
	entries = state.toolbars.entries(toolbarTerminal)
	if entries[len(entries)-1].Key != firstKey {
		t.Fatal("scrolling during a drag changed the wrong target")
	}
	for _, id := range []gfx.ID{hitToolbarEnableAll, hitToolbarDisableAll, hitToolbarReset, hitToolbarSettingsClose} {
		bounds := dualHit(t, view, id)
		if bounds.Y < 0 || bounds.Y+bounds.Height > 480 {
			t.Fatal("small window made a footer action unreachable")
		}
	}
}

func TestToolbarSettingsScrollReachesTheWholeLastRow(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openToolbarSettings(toolbarTerminal)
	var invalidator app.Invalidator
	state.options.Invalidate = invalidator.Invalidate
	view, err := headless.Open(state.WindowConfig(app.Config{Width: 640, Height: 480, Tree: Tree(), Invalidator: &invalidator, OnWake: state.Wake, Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize}), nil, headless.Options{Scale: 1.25})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	view.Driver().Advance(250 * time.Millisecond)
	box := state.toolbarEditor.list.Rect()
	if err := view.Scroll(gfx.ScrollEvent{X: box.X + box.Width/2, Y: box.Y + box.Height/2, DeltaY: 2000}); err != nil {
		t.Fatal(err)
	}
	view.Driver().Advance(250 * time.Millisecond)
	entries := state.toolbars.entries(toolbarTerminal)
	last := entries[len(entries)-1]
	row, ok := toolbarRowHit(t, state, view, toolbarTerminal, last.Key)
	if !ok {
		t.Fatalf("last row %q was not painted at the end", last.Key)
	}
	list := state.toolbarEditor.list.Rect()
	if row.Y < list.Y-1 || row.Y+row.Height > list.Y+list.Height+1 {
		t.Fatalf("last row %v is not wholly inside list %v after scrolling to the end", row, list)
	}
	if !state.toolbarEditor.rows.AtEnd() {
		t.Fatal("the list did not report its end after scrolling to the bottom")
	}
	if dir := os.Getenv("MOTTY_TOOLBAR_SCREENSHOT_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(dir, "toolbar-scroll-end.png")); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
			t.Fatal(err)
		}
	}
}
