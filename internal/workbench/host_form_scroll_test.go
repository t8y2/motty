//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

func TestHostFormScrollKeepsFooterAndReachesEveryField(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.openHostDialog()
			state.editor.advancedOpen = true
			state.editor.algoMode = algoCustom
			var input app.TextInput
			view, err := headless.Open(app.Config{Width: 900, Height: 600, Tree: Tree(), Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize,
				TextInput: func(c app.TextInputContext) *app.TextInput {
					e := state.EditorFor(c.ID)
					if e == nil {
						return nil
					}
					input = app.TextInput{Editor: e}
					return &input
				}}, nil, headless.Options{Scale: scale})
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
			must(view.Rest())
			find := func(id ui.ID) ui.Hit {
				t.Helper()
				for _, h := range view.Snapshot().Hits {
					if h.ID == id && !h.IsLabel {
						return h
					}
				}
				t.Fatalf("missing visible control %v", id)
				return ui.Hit{}
			}
			footer := find(hitDialogSubmit).Bounds
			if state.editor.form.geometry.MaxOffsetY <= 0 {
				t.Fatal("short form should scroll")
			}
			seen := map[ui.ID]bool{}
			for i := 0; i < state.editor.form.count+2; i++ {
				id := view.Snapshot().Focused
				seen[id] = true
				h := find(id)
				if id != hitDialogCancel && id != hitDialogSubmit && (h.Bounds.Y < state.editor.form.geometry.Viewport.Y || h.Bounds.Y+h.Bounds.Height > footer.Y) {
					t.Fatalf("focus is clipped: %+v", h)
				}
				if find(hitDialogSubmit).Bounds != footer {
					t.Fatal("footer scrolled")
				}
				must(view.Keys("Tab"))
				must(view.Rest())
			}
			for _, id := range []ui.ID{hostPickerIDs.Trigger, hitHostProxyConfigure, hitDialogAlgorithms, hitDialogOTP, hitDialogForwardX11, hitDialogCancel, hitDialogSubmit} {
				if !seen[id] {
					t.Fatalf("Tab skipped %v", id)
				}
			}
			must(view.Keys("Shift+Tab"))
			must(view.Rest())
			find(view.Snapshot().Focused)
			// Wheel and a captured scrollbar drag use the same clamped geometry.
			b := state.editor.form.geometry.Viewport
			must(view.Driver().Scroll(ui.ScrollEvent{X: b.X + 4, Y: b.Y + 4, DeltaY: 10000}))
			must(view.Rest())
			if state.editor.form.offset != state.editor.form.geometry.MaxOffsetY {
				t.Fatal("wheel failed to reach bottom")
			}
			bar := state.editor.form.geometry.VerticalThumb
			must(view.Driver().Press(bar.X+bar.Width/2, bar.Y+bar.Height/2, ui.PointerButtonPrimary))
			must(view.Driver().Move(bar.X+bar.Width/2, b.Y-40))
			must(view.Driver().Release(bar.X+bar.Width/2, b.Y-40, ui.PointerButtonPrimary))
			must(view.Rest())
			if state.editor.form.offset != 0 {
				t.Fatal("captured scrollbar did not reach top")
			}
			// The open selector owns scrolling; its content escapes the form clip.
			state.Click(hitDialogIcon)
			must(view.Driver().Resize(900, 600))
			must(view.Rest())
			before := state.editor.form.offset
			state.scrollHostForm(ui.ScrollEvent{X: b.X + 2, Y: b.Y + 2, DeltaY: 100})
			if state.editor.form.offset != before {
				t.Fatal("scrolled behind a popup")
			}
			must(view.Keys("Escape"))
			must(view.Rest())
			if state.modal != modalNewHost || state.editor.iconOpen {
				t.Fatal("Escape closed the parent form")
			}
		})
	}
}

func TestHostFormRoomierLayoutAndReset(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.setLanguage(i18n.Chinese)
	state.openHostDialog()
	c, _ := paintAt(state, ui.Size{Width: 1100, Height: 900})
	var label, input, nextLabel ui.Rect
	for _, h := range c.Hits() {
		if h.ID == hitDialogAddress && h.IsLabel {
			nextLabel = h.Bounds
		}
		if h.ID == hitDialogLabel {
			if h.IsLabel {
				label = h.Bounds
			} else if h.Role == ui.RoleTextField {
				input = h.Bounds
			}
		}
	}
	if input.Y-label.Y-label.Height != state.Theme().Metrics.GapItem/2 || input.Height != 32 {
		t.Fatalf("label spacing/control size %+v %+v", label, input)
	}
	if nextLabel.Y-(input.Y+input.Height) < 12 {
		t.Fatal("rows are still crowded", input, nextLabel)
	}
	if state.editor.form.geometry.MaxOffsetY != 0 {
		t.Fatal("large default form should fit without scrolling")
	}
	state.editor.form.offset = 100
	state.Click(hitDialogCancel)
	state.openHostDialog()
	if state.editor.form.offset != 0 {
		t.Fatal("new form inherited scroll position")
	}
	if dir := os.Getenv("MOTTY_SPACING_SCREENSHOTS"); dir != "" {
		view, err := headless.Open(app.Config{Width: 1100, Height: 900, Tree: Tree(), Paint: state.Paint, OnResize: state.Resize}, nil, headless.Options{Scale: 1.25})
		if err != nil {
			t.Fatal(err)
		}
		defer view.Close()
		if err = view.Rest(); err != nil {
			t.Fatal(err)
		}
		if err = view.Screenshot(filepath.Join(dir, "host-form-spacing.png")); err != nil {
			t.Fatal(err)
		}
	}
}
