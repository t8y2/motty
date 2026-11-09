package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestWorkspacePaneCountsKeepRowMajorGeometryInsideTheFrame(t *testing.T) {
	for _, count := range []int{1, 2, 3, 4, 7, 8} {
		t.Run(string(rune('0'+count))+" hosts", func(t *testing.T) {
			state := manyHostsState(t, count)
			indexes := make([]int, count)
			for index := range indexes {
				indexes[index] = index
			}
			launchTemporaryWorkspace(t, state, indexes...)
			size := gfx.Size{Width: 1400, Height: 900}
			frame := state.workspacePixelBounds(size)
			canvas, _ := paintAt(state, size)
			panes := state.Panes(state.panes[:0])
			if len(panes) != count {
				t.Fatalf("panes = %d, want %d", len(panes), count)
			}

			previousY := float32(-1)
			previousX := float32(-1)
			for index, pane := range panes {
				bounds := pixelBounds(frame, pane.Bounds)
				if bounds.X < frame.X-0.01 || bounds.Y < frame.Y-0.01 ||
					bounds.X+bounds.Width > frame.X+frame.Width+0.01 ||
					bounds.Y+bounds.Height > frame.Y+frame.Height+0.01 {
					t.Fatalf("pane %d lies outside the frame: pane=%+v frame=%+v", index, bounds, frame)
				}
				if index > 0 && bounds.Y < previousY-0.01 {
					t.Fatalf("pane %d moves upward in row-major order: %+v after y=%g", index, bounds, previousY)
				}
				if index > 0 && bounds.Y == previousY && bounds.X <= previousX {
					t.Fatalf("pane %d is not left-to-right on its row: %+v after x=%g", index, bounds, previousX)
				}
				previousY, previousX = bounds.Y, bounds.X
			}

			for _, text := range canvas.Texts() {
				if text.Bounds.X < frame.X || text.Bounds.X+text.Bounds.Width > frame.X+frame.Width ||
					text.Bounds.Y < frame.Y || text.Bounds.Y+text.Bounds.Height > frame.Y+frame.Height {
					continue
				}
				if text.Value != "" && canvas.TextWidthWeight(text.Value, text.Style.Size, text.Style.Weight) > text.Bounds.Width+0.5 &&
					!text.HasClip {
					t.Fatalf("pane text overflows without a clip: %+v", text)
				}
			}
		})
	}
}

func TestWorkspaceNarrowWindowKeepsPanelsAndControlsInsideTheWindow(t *testing.T) {
	state := manyHostsState(t, 8)
	launchTemporaryWorkspace(t, state, 0, 1, 2, 3, 4, 5, 6, 7)
	size := gfx.Size{Width: 420, Height: 620}
	canvas, _ := paintAt(state, size)
	for _, id := range []gfx.ID{
		hitWorkspaceHeaderBack,
		hitWorkspaceHeaderBroadcast,
		hitWorkspaceHeaderLayout,
	} {
		hit := panelHit(t, canvas, id)
		if hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > size.Width ||
			hit.Bounds.Y < 0 || hit.Bounds.Y+hit.Bounds.Height > size.Height {
			t.Fatalf("control %d lies outside the narrow window: %+v", id, hit.Bounds)
		}
	}
	frame := state.workspacePixelBounds(size)
	if frame.X < 0 || frame.Y < 0 || frame.X+frame.Width > size.Width+0.01 ||
		frame.Y+frame.Height > size.Height+0.01 {
		t.Fatalf("narrow workspace frame = %+v", frame)
	}
}
