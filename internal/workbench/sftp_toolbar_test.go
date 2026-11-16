package workbench

import (
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

func TestSFTPToolbarKeepsMoreAtRightAndMovesOverflowIntoMenu(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	workspace := state.deck.ActiveWorkspace()
	for _, side := range []layoutcore.ContextSide{layoutcore.ContextLeft, layoutcore.ContextRight} {
		for _, width := range []float32{480, 340, 320, 300, 280, 340, 480} {
			context, _ := state.deck.Context(workspace)
			context.Side, context.Width = side, width
			if err := state.deck.SetContext(workspace, context); err != nil {
				t.Fatal(err)
			}
			for _, hidden := range []bool{false, true} {
				session.files.active().showHidden = hidden
				var canvas gfx.Canvas
				state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
				more, ok := hitByID(&canvas, contextID(hitSFTPHeaderMore))
				if !ok {
					t.Fatalf("side=%v width=%v hidden=%v: More is missing", side, width, hidden)
				}
				panel := state.contextRect
				if more.Bounds.Width != sftpHeaderButton || math.Abs(float64(more.Bounds.X+more.Bounds.Width-(panel.X+panel.Width-sftpPad))) > 0.1 {
					t.Fatalf("side=%v width=%v hidden=%v: More is clipped or leaves the right edge: %+v, panel=%+v", side, width, hidden, more.Bounds, panel)
				}
				for _, hit := range canvas.Hits() {
					if hit.ID != more.ID && hit.Bounds.Y == more.Bounds.Y && hit.Bounds.X >= panel.X && hit.Bounds.X < panel.X+panel.Width && hit.Bounds.X+hit.Bounds.Width > more.Bounds.X {
						t.Fatalf("toolbar action %s overlaps More: %+v", tree.Text(hit.ID), hit.Bounds)
					}
				}
				overflow := append([]barTool(nil), state.chrome.sftpPanel.overflowTools...)
				for _, tool := range overflow {
					if _, ok := hitByID(&canvas, contextID(tool.id)); ok {
						t.Fatalf("overflow action %s remains in the toolbar", tree.Text(tool.id))
					}
				}
				state.Click(contextID(hitSFTPHeaderMore))
				canvas.Reset()
				state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
				for _, tool := range overflow {
					id := tool.id
					switch id {
					case hitSFTPHiddenToggle:
						id = hitSFTPHeaderHidden
					case hitSFTPFollowToggle:
						id = hitSFTPHeaderFollow
					}
					if hit, ok := hitByID(&canvas, contextID(id)); !ok || hit.Disabled != tool.disabled {
						t.Fatalf("side=%v width=%v: overflow action %s missing or has wrong availability: %+v", side, width, tree.Text(id), hit)
					}
				}
				state.Click(contextID(hitSFTPHeaderMenuDismiss))
			}
		}
	}
}
