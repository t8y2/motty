//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"motty/internal/sshconn"
)

// The icon field is a page select: pointer input opens its list, a row chooses
// the icon, and cancelling the form leaves the saved host alone.
func TestHostIconSelectNativeInput(test *testing.T) {
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
			must(view.Rest())
			must(view.ClickLabel(state.messages.FieldIcon))
			must(view.Rest())
			must(view.ClickLabel("Linux Mint"))
			must(view.Rest())
			if state.editor.icon != hostIconOf(sshconn.SystemMint) {
				test.Fatalf("choosing Linux Mint set the icon to %v", state.editor.icon)
			}
			state.Click(hitDialogCancel)
			if state.hosts[0].Icon != original {
				test.Fatal("cancel changed the saved host icon")
			}
		})
	}
}
