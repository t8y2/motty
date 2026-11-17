//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/field"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

// Through the headless driver the two form fields still take the row's 70px,
// place their control at the shared field layout's slot, and keep the jump
// button reachable with Tab.
func TestHostFormProxyAndJumpRowsHeadlessGeometry(t *testing.T) {
	for _, scale := range []float64{1, 1.5} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.openEditHostID(hostByLabel(t, state, "Production").ID)
			var input app.TextInput
			view, err := headless.Open(app.Config{
				Width: 1100, Height: 900, Tree: Tree(), Paint: state.Paint,
				OnKey: state.Key, OnPointer: state.Pointer, OnShortcut: state.Shortcut,
				OnScroll: state.Scroll, OnResize: state.Resize,
				TextInput: func(c app.TextInputContext) *app.TextInput {
					e := state.EditorFor(c.ID)
					if e == nil {
						return nil
					}
					input = app.TextInput{Editor: e}
					return &input
				},
			}, nil, headless.Options{Scale: scale})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			theme := state.Theme()
			body := &state.editor.form.body
			for _, row := range []struct {
				name           string
				label          string
				field, control ui.Box
			}{
				{"proxy", state.messages.Tools.HostProxyRow, body.proxy, body.proxyRow},
				{"jump", state.messages.Tools.HostJumpRow, body.jump, body.jumpRow},
			} {
				bounds := row.field.Rect()
				if bounds.Height != hostFormRowHeight {
					t.Fatalf("%s field height = %v, want %v", row.name, bounds.Height, hostFormRowHeight)
				}
				layout := field.Measure(theme, bounds, field.Spec{Title: row.label})
				if got := row.control.Rect(); got != layout.Control {
					t.Fatalf("%s control is %+v, the shared layout says %+v", row.name, got, layout.Control)
				}
				if layout.Control.Y+layout.Control.Height > bounds.Y+bounds.Height {
					t.Fatalf("%s control %+v overflows the field %+v", row.name, layout.Control, bounds)
				}
			}

			reached := false
			for i := 0; i <= state.editor.form.count; i++ {
				if view.Snapshot().Focused == hitHostJumpConfigure {
					reached = true
					break
				}
				if err := view.Keys("Tab"); err != nil {
					t.Fatal(err)
				}
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
			}
			if !reached {
				t.Fatal("Tab never reached the jump row's button")
			}
			var jump gfx.Hit
			for _, hit := range view.Snapshot().Hits {
				if hit.ID == hitHostJumpConfigure && !hit.IsLabel {
					jump = hit
					break
				}
			}
			if jump.Bounds.Width <= 0 || jump.Bounds.Height <= 0 {
				t.Fatalf("the focused jump button has no bounds: %+v", jump.Bounds)
			}
		})
	}
}
