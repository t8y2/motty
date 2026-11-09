package workbench

import (
	"fmt"
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
)

// Exercise the same input and paint entry points as the window, with enough
// rows to expose fractional offsets, clipped hits, and both scroll boundaries.
func TestPickerPixelScrolling(t *testing.T) {
	for _, kind := range []string{"commands", "connections", "fonts"} {
		t.Run(kind, func(t *testing.T) {
			families := make([]string, 30)
			hosts := make([]savedHost, 30)
			for i := range families {
				families[i] = fmt.Sprintf("Family %02d", i)
				hosts[i] = savedHost{Label: fmt.Sprintf("Host %02d", i), Address: fmt.Sprintf("host%d.example.com", i), Port: 22}
			}
			options := fontOptions(t, families)
			if err := saveState(options.StatePath, savedState{Hosts: hosts}); err != nil {
				t.Fatal(err)
			}
			state := newTestState(t, options)
			var offset *float32
			var step, maximum float32
			switch kind {
			case "fonts":
				state.openFontPicker(false)
				waitFor(t, state, func() bool { return !state.fonts.loading })
				state.refreshFontPicker()
				offset, step, maximum = &state.fonts.scroll, fontPickerRowStep, state.fonts.maxScroll()
			case "connections":
				state.Click(hitNewTab)
				state.refreshCommandPalette()
				offset, step, maximum = &state.commandPalette.scroll, commandPaletteRowStep, state.commandPalette.maxScroll()
			default:
				for id := uint32(1); id <= 20; id++ {
					state.sessions = append(state.sessions, Session{ID: id, Title: fmt.Sprintf("Session %d", id), restored: true})
				}
				state.openCommandPalette()
				state.refreshCommandPalette()
				offset, step, maximum = &state.commandPalette.scroll, commandPaletteRowStep, state.commandPalette.maxScroll()
			}
			if maximum <= step {
				t.Fatal("fixture must scroll")
			}
			var canvas gfx.Canvas
			size := gfx.Size{Width: 1400, Height: 900}
			paint := func() { canvas.Reset(); state.Paint(&canvas, size) }
			paint()
			scroll := func(delta float32, precise bool) {
				t.Helper()
				result := state.Scroll(gfx.ScrollEvent{DeltaY: delta, Precise: precise})
				if !result.Handled {
					t.Fatal("scroll not handled")
				}
			}
			near := func(want float32) {
				t.Helper()
				if math.Abs(float64(*offset-want)) > 0.0001 {
					t.Fatalf("offset %g, want %g", *offset, want)
				}
			}
			scroll(0.1, true)
			near(0.1)
			scroll(1, true)
			near(1.1)
			scroll(0, true)
			near(1.1)
			*offset = 0
			for range 10 {
				scroll(0.1, true)
			}
			near(1)
			*offset = 0
			scroll(1, true)
			near(1)
			paint()
			near(1)
			*offset = 0
			scroll(0.1, false)
			near(step)
			scroll(100000, true)
			near(maximum)
			scroll(100000, true)
			near(maximum)
			scroll(-0.1, true)
			near(maximum - 0.1)
			scroll(-100000, true)
			near(0)
			scroll(-100000, true)
			near(0)
			scroll(0.1, true)
			near(0.1)

			// Scroll past the first row. The second logical row is now clipped at the
			// viewport edge, and the first must have no hit region at all.
			*offset = step + 15
			paint()
			list := hitCommandPaletteItems
			viewportHeight := commandPaletteListHeight
			if kind == "fonts" {
				list = hitFontPickerItemList
				viewportHeight = fontPickerListHeight
			}
			if _, ok := hitByID(&canvas, list.At(0)); ok {
				t.Fatal("offscreen first row still has a hit")
			}
			hit, ok := hitByID(&canvas, list.At(1))
			if !ok {
				t.Fatal("partial second row has no hit")
			}
			if hit.Bounds.Height >= 30 || hit.Bounds.Height <= 0 {
				t.Fatalf("partial row hit not clipped: %+v", hit.Bounds)
			}
			top := hit.Bounds.Y
			for _, h := range canvas.Hits() {
				if _, ok := h.ID.In(list); ok && (h.Bounds.Y < top || h.Bounds.Y+h.Bounds.Height > top+viewportHeight) {
					t.Fatalf("hit escapes viewport: %+v", h)
				}
			}

			// Keyboard reveal works from a fractional offset, without making
			// ordinary paints pull the list back to its previous selection.
			search := hitCommandPaletteSearch
			if kind == "fonts" {
				search = hitFontPickerSearch
			}
			state.Key(gfx.KeyEvent{Key: gfx.KeyPageDown, Target: search})
			paint()
			selected := int(state.commandPalette.selected)
			if kind == "fonts" {
				selected = state.fonts.selected
			}
			selectedHit, ok := hitByID(&canvas, list.At(selected))
			if !ok || selectedHit.Bounds.Height < 30 {
				t.Fatal("keyboard selection is not fully visible")
			}
			if kind == "connections" {
				state.commandPalette.scroll = step + 15
				state.shellsListed()
				near(step + 15)
			}
			*offset = step + 15
			paint()
			if kind == "fonts" {
				selected := state.fonts.selected
				scroll(0.1, true)
				if state.fonts.selected != selected {
					t.Fatal("wheel changed font selection")
				}
				state.Click(list.At(1))
				if state.fonts.selected != 1 {
					t.Fatal("partial row click selected wrong font")
				}
				// A scrolled double click must confirm the logical font, not its slot.
				doubleClick(state, list.At(3))
				if state.modal != modalNone || state.fonts.family != families[1] {
					t.Fatalf("scrolled double click chose %q", state.fonts.family)
				}
			} else {
				// Commands follow the pointer; the new-tab picker keeps its selection.
				want := 1
				if kind == "connections" {
					want = state.commandPalette.selected
				}
				for range 10 {
					result := state.Scroll(gfx.ScrollEvent{DeltaY: 0.1, Precise: true, X: hit.Bounds.X + 10, Y: top + 1, Target: list.At(0)})
					if !result.Handled || state.commandPalette.selected != want {
						t.Fatalf("wheel selected %d, want %d", state.commandPalette.selected, want)
					}
					paint()
				}
				if kind == "connections" {
					state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: list.At(1)})
					if state.commandPalette.selected != 1 {
						t.Fatal("pointer movement did not update the new-tab selection")
					}
				}
				if kind == "commands" {
					target := state.commandPalette.entries[state.commandPalette.visible[1]].target
					state.Click(list.At(1))
					if state.modal != modalNone {
						t.Fatalf("partial command click did not execute %v", target)
					}
				}
			}
		})
	}
}

func TestPickerShortLists(t *testing.T) {
	state := newTestState(t, fontOptions(t, []string{"Only font"}))
	state.openFontPicker(false)
	waitFor(t, state, func() bool { return !state.fonts.loading })
	state.refreshFontPicker()
	state.Scroll(gfx.ScrollEvent{DeltaY: 100, Precise: true})
	if state.fonts.scroll != 0 {
		t.Fatal("short font list scrolled")
	}
	state.closeCommandPalette()
	state.openCommandPalette()
	state.commandPalette.search.Insert("settings")
	state.refreshCommandPalette()
	state.Scroll(gfx.ScrollEvent{DeltaY: 100, Precise: true})
	if state.commandPalette.scroll != 0 {
		t.Fatal("short command list scrolled")
	}
}
