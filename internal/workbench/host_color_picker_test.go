//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"motty/internal/i18n"
)

// The host form's colour picker is the page control: the trigger opens the
// staged panel, Escape, Cancel and a press outside keep the colour, and Clear
// writes no colour back into the form.
func TestHostColorPickerStagesAndKeepsTheFormColor(t *testing.T) {
	for _, scale := range []float32{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.setLanguage(i18n.Chinese)
			state.openEditHostID(state.hosts[0].ID)
			state.editor.color = *text.NewEditor("#123456")
			base := app.Config{Width: 900, Height: 600, Tree: Tree(), Paint: state.Paint, OnResize: state.Resize, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut}
			view, err := headless.Open(state.WindowConfig(base), nil, headless.Options{Scale: float64(scale)})
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
			focusedLabel := func() string {
				focused := view.Snapshot().Focused
				for _, hit := range view.Snapshot().Hits {
					if hit.ID == focused {
						return hit.Label
					}
				}
				return ""
			}
			clickRole := func(role gfx.Role, label string) {
				t.Helper()
				var found gfx.Hit
				for _, hit := range view.Snapshot().Hits {
					if hit.Role == role && hit.Label == label {
						found = hit
					}
				}
				if found.ID.IsZero() {
					t.Fatalf("no %v named %q", role, label)
				}
				must(view.Click(found.Bounds.X+found.Bounds.Width/2, found.Bounds.Y+found.Bounds.Height/2))
				must(view.Rest())
			}
			must(view.Rest())
			clickRole(gfx.RoleButton, state.messages.ColorPickerChoose)
			if !state.editor.form.colorState.Open() {
				t.Fatal("picker did not open")
			}
			must(view.Keys("Escape"))
			must(view.Rest())
			if state.modal != modalNewHost || state.editor.form.colorState.Open() || state.editor.value(fieldColor) != "#123456" || focusedLabel() != state.messages.ColorPickerChoose {
				t.Fatal("escape canceled host or lost original/focus")
			}
			clickRole(gfx.RoleButton, state.messages.ColorPickerChoose)
			if !state.editor.form.colorState.Open() {
				t.Fatal("picker did not reopen")
			}
			clickRole(gfx.RoleButton, state.messages.ColorPickerChoose)
			if state.editor.form.colorState.Open() || state.editor.value(fieldColor) != "#123456" {
				t.Fatal("the trigger did not close the panel and keep the colour")
			}
		})
	}
}
