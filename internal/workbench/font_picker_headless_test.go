//go:build windows || darwin || linux

package workbench

import (
	"fmt"
	"github.com/vivaui/viva/gfx"
	"testing"
)

func TestFontPickerNavigationConfirmsTheHighlightedFont(t *testing.T) {
	for _, test := range []struct {
		name  string
		start int
		key   string
		want  int
	}{
		{"down", 2, "Down", 3},
		{"up", 3, "Up", 2},
		{"down past bottom", 7, "Down", 8},
		{"page down", 2, "PageDown", 10},
		{"page up", 7, "PageUp", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			families := make([]string, 20)
			for i := range families {
				families[i] = fmt.Sprintf("Family %02d", i)
			}
			state := newTestState(t, fontOptions(t, families))
			state.openFontPicker(false)
			waitFor(t, state, func() bool { return !state.fonts.loading })
			view := openPickerView(t, state, 1)
			hit := dualHit(t, view, hitFontPickerItemList.At(test.start))
			if err := view.Click(hit.X+hit.Width/2, hit.Y+hit.Height/2); err != nil {
				t.Fatal(err)
			}
			if err := view.Keys(test.key); err != nil {
				t.Fatal(err)
			}
			if state.fonts.selected != test.want {
				t.Fatalf("highlighted %d, want %d", state.fonts.selected, test.want)
			}
			wantFocus := hitFontPickerItemList.At(test.want)
			if got := view.Snapshot().Focused; got != wantFocus {
				t.Errorf("focus %v, want highlighted row %v", got, wantFocus)
			}
			want := state.fonts.entries[state.fonts.filtered[test.want]]
			if err := view.Keys("Enter"); err != nil {
				t.Fatal(err)
			}
			if state.modal != modalNone || state.fonts.family != want.family || state.fonts.classic != (want.face == gfx.MonoClassic) {
				t.Fatalf("Enter applied family %q (classic=%v), want %q", state.fonts.family, state.fonts.classic, want.label)
			}
		})
	}
}

func TestFontPickerNavigationKeepsSearchFocus(t *testing.T) {
	state := newTestState(t, fontOptions(t, testFonts))
	state.openFontPicker(false)
	waitFor(t, state, func() bool { return !state.fonts.loading })
	view := openPickerView(t, state, 1)
	if err := view.Keys("Down"); err != nil {
		t.Fatal(err)
	}
	if state.fonts.selected != 1 || view.Snapshot().Focused != hitFontPickerSearch {
		t.Fatalf("selected %d, focus %v: search navigation must retain text focus", state.fonts.selected, view.Snapshot().Focused)
	}
}
