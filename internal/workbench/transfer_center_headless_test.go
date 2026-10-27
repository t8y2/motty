//go:build darwin || linux || windows

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

func TestTransferCenterPopupRoutesClicksAndFitsSmallWindows(t *testing.T) {
	for _, size := range []ui.Size{{Width: 1400, Height: 900}, {Width: 900, Height: 600}} {
		t.Run(fmt.Sprintf("%dx%d", int(size.Width), int(size.Height)), func(t *testing.T) {
			state := transferPreviewState(t)
			if size.Width == 900 {
				state.chooseAppearanceMode(appearancePinnedLight)
			}
			view, err := headless.Open(app.Config{
				Title: "Motty", Width: int(size.Width), Height: int(size.Height), Tree: Tree(),
				Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
			}, nil, headless.Options{Scale: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			click := func(id ui.ID) {
				t.Helper()
				for _, hit := range view.Snapshot().Hits {
					if hit.ID == id && !hit.Block {
						if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
							t.Fatal(err)
						}
						return
					}
				}
				t.Fatalf("missing control %v", id)
			}
			if err := view.Wait(300 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			box := state.transfers.surface
			if box.X < 0 || box.Y < 0 || box.X+box.Width > size.Width || box.Y+box.Height > size.Height {
				t.Fatalf("popover outside viewport: %+v", box)
			}
			if hit, ok := findSnapshotHit(view.Snapshot(), hitTransferCenter); !ok || hit.Bounds.X >= state.chrome.bar.assistant.Rect().X {
				t.Fatal("transfer button is not left of the assistant")
			}
			if dir := os.Getenv("MOTTY_TRANSFER_SCREENSHOTS"); dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("transfers-%dx%d.png", int(size.Width), int(size.Height)))); err != nil {
					t.Fatal(err)
				}
			}
			if err := view.Driver().Key(ui.KeyEnd, 0); err != nil {
				t.Fatal(err)
			}
			if state.transfers.scroll != state.transfers.geometry.MaxOffsetY {
				t.Fatal("End did not reveal the bottom of history")
			}
			if err := view.Driver().Key(ui.KeyHome, 0); err != nil {
				t.Fatal(err)
			}
			if state.transfers.scroll != 0 {
				t.Fatal("Home did not return to the first transfer")
			}
			click(hitTransfersFilter.At(2))
			if state.transfers.filter != 2 || state.transfers.count != 2 {
				t.Fatalf("finished filter=%d count=%d", state.transfers.filter, state.transfers.count)
			}
			click(hitTransfersRemove.At(2))
			if len(state.transfers.entries) != 3 {
				t.Fatal("remove click did not remove a history entry")
			}
			if err := view.Driver().Key(ui.KeyEscape, 0); err != nil {
				t.Fatal(err)
			}
			if state.transfers.open {
				t.Fatal("Escape did not close popover")
			}
			click(hitTransferCenter)
			if !state.transfers.open {
				t.Fatal("trigger did not reopen popover")
			}
			click(hitTransferCenter)
			if state.transfers.open {
				t.Fatal("trigger did not toggle popover closed")
			}
			click(hitTransferCenter)
			click(hitTransfersClose)
			if state.transfers.open {
				t.Fatal("close button did not close popover")
			}
			click(hitTransferCenter)
			if err := view.Click(size.Width-5, size.Height-5); err != nil {
				t.Fatal(err)
			}
			if state.transfers.open {
				t.Fatal("outside click did not close popover")
			}
		})
	}
}

func findSnapshotHit(snapshot app.Snapshot, id ui.ID) (ui.Hit, bool) {
	for _, hit := range snapshot.Hits {
		if hit.ID == id {
			return hit, true
		}
	}
	return ui.Hit{}, false
}
