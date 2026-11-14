//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestHostIconScrollbarNativeInput(test *testing.T) {
	for _, scale := range []float64{1, 1.5, 2} {
		test.Run(fmt.Sprint(scale), func(test *testing.T) {
			state := newTestState(test, testOptions(test))
			original := state.hosts[0].Icon
			state.openEditHostID(state.hosts[0].ID)
			view, err := headless.Open(app.Config{Width: 900, Height: 600, Tree: Tree(), Paint: state.Paint,
				OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll, OnResize: state.Resize,
			}, nil, headless.Options{Scale: scale})
			if err != nil {
				test.Fatal(err)
			}
			test.Cleanup(view.Close)
			must := func(err error) {
				test.Helper()
				if err != nil {
					test.Fatal(err)
				}
			}
			find := func(id gfx.ID) gfx.Hit {
				test.Helper()
				for _, hit := range view.Snapshot().Hits {
					if hit.ID == id && !hit.IsLabel {
						return hit
					}
				}
				test.Fatalf("control %v is not visible", id)
				return gfx.Hit{}
			}
			state.Click(hitDialogIcon)
			must(view.Rest())
			bar := find(hitHostIconScroll).Bounds
			before := state.editor.form.offset
			must(view.Driver().Move(bar.X+bar.Width/2, bar.Y+bar.Height-2))
			must(view.Rest())
			must(view.Click(bar.X+bar.Width/2, bar.Y+bar.Height-2))
			must(view.Rest())
			last := len(hostIconChoices()) - 1
			find(hitHostIconChoices.At(last))
			if !state.editor.iconPicker.control.Open || state.editor.icon != original || state.editor.form.offset != before {
				test.Fatal("scrollbar press changed the icon, closed the picker or scrolled the parent")
			}
			must(view.Driver().Press(bar.X+bar.Width/2, bar.Y+bar.Height-5, gfx.PointerButtonPrimary))
			must(view.Driver().Move(bar.X+bar.Width/2, bar.Y-40))
			must(view.Driver().Release(bar.X+bar.Width/2, bar.Y-40, gfx.PointerButtonPrimary))
			must(view.Rest())
			find(hitHostIconChoices.At(0))
			must(view.Scroll(gfx.ScrollEvent{X: bar.X - 20, Y: bar.Y + 10, DeltaY: 10000}))
			must(view.Rest())
			choice := find(hitHostIconChoices.At(last))
			must(view.Click(choice.Bounds.X+choice.Bounds.Width/2, choice.Bounds.Y+choice.Bounds.Height/2))
			must(view.Rest())
			if state.editor.icon != hostIconChoices()[last] || state.editor.iconPicker.control.Open {
				test.Fatal("a revealed icon could not be selected")
			}
			trigger := find(hitHostIconTrigger).Bounds
			must(view.Click(trigger.X+trigger.Width/2, trigger.Y+trigger.Height/2))
			must(view.Rest())
			if !state.editor.iconPicker.control.Open {
				test.Fatal("the icon picker did not reopen through pointer input")
			}
			if directory := os.Getenv("MOTTY_ICON_SCROLL_SCREENSHOTS"); directory != "" {
				if err := os.MkdirAll(directory, 0o755); err != nil {
					test.Fatal(err)
				}
				must(view.Screenshot(filepath.Join(directory, fmt.Sprintf("icon-scroll-%g.png", scale))))
			}
			must(view.Keys("Escape"))
			must(view.Rest())
			if state.modal != modalNewHost || state.editor.iconPicker.control.Open {
				test.Fatal("Escape did not dismiss only the picker")
			}
			state.Click(hitDialogCancel)
			if state.hosts[0].Icon != original {
				test.Fatal("cancel changed the saved host icon")
			}
		})
	}
}
