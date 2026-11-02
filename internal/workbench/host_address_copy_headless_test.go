//go:build darwin || windows || linux

package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

// The copy button after a host's address appears only while the pointer is on
// the host, puts the address alone on the clipboard without selecting or opening
// the host, and shows a check for a moment. The list and the cards behave alike.
func TestHostAddressCopyButtonThroughDriver(t *testing.T) {
	views := []struct {
		name string
		mode hostViewMode
	}{{"list", hostViewList}, {"cards", hostViewGrid}}
	for _, view := range views {
		t.Run(view.name, func(t *testing.T) {
			state := manyHostsState(t, 2)
			state.hostView = view.mode
			now := time.Second
			state.options.Now = func() time.Duration { return now }
			var copied string
			state.options.SetClipboardText = func(value string) error { copied = value; return nil }
			preview, err := headless.Open(app.Config{Title: "Host address copy", Width: 1100, Height: 760, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize}, nil, headless.Options{Scale: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer preview.Close()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			find := func(id ui.ID) (ui.Hit, bool) { return hitByIDSnapshot(preview.Snapshot().Hits, id) }

			host := state.hosts[1]
			button := hitHostCopyAddressList.At(int(host.ID))
			if _, ok := find(button); ok {
				t.Fatal("the copy button shows before the pointer is on the host")
			}
			row, ok := find(hitHostList.At(int(host.ID)))
			if !ok {
				t.Fatal("the host is not on the page")
			}
			must(preview.Move(row.Bounds.X+70, row.Bounds.Y+row.Bounds.Height/2))
			hit, ok := find(button)
			if !ok {
				t.Fatal("the hovered host has no copy button")
			}

			selected, sessions := state.selectedHostID, len(state.sessions)
			must(preview.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2))
			if copied != host.Address {
				t.Fatalf("copied %q, want the address %q alone", copied, host.Address)
			}
			if state.selectedHostID != selected || len(state.sessions) != sessions {
				t.Fatal("copying the address selected or opened the host")
			}

			// Away from the host the check still says it worked, until the moment passes.
			must(preview.Move(row.Bounds.X+row.Bounds.Width/2, 740))
			if _, ok := find(button); !ok {
				t.Fatal("the check went away with the pointer")
			}
			now += aiCopiedFor
			must(preview.Wait(aiCopiedFor))
			if _, ok := find(button); ok {
				t.Fatal("the button stayed after the check's moment, with the pointer elsewhere")
			}
		})
	}
}
