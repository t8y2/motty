package workbench

import (
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

// An action that cannot run right now keeps that state when the narrow panel
// moves it into the More menu.
func TestSFTPToolbarOverflowKeepsDisabledActionsDisabled(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	// Put an action that can be unavailable at the end, where narrowing sends it.
	entries := state.toolbars.customize(toolbarSFTP)
	for index, entry := range entries {
		if entry.Key == "upload-file" {
			copy(entries[index:], entries[index+1:])
			entries[len(entries)-1] = entry
			break
		}
	}
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	context.Width = 280
	if err := state.deck.SetContext(state.deck.ActiveWorkspace(), context); err != nil {
		t.Fatal(err)
	}
	session.files.local = false
	session.files.busy = true
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, contextID(hitSFTPUpload)); ok {
		t.Fatal("the unavailable upload action stayed in the toolbar")
	}
	state.Click(contextID(hitSFTPHeaderMore))
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	hit, ok := hitByID(&canvas, contextID(hitSFTPUpload))
	if !ok || !hit.Disabled {
		t.Fatalf("overflowed upload action: %+v, painted=%t want disabled", hit, ok)
	}
}

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
				if hidden {
					// The eye is the status affordance for hidden entries on
					// screen; narrowing must not take it away with the actions.
					if _, ok := hitByID(&canvas, contextID(hitSFTPHiddenToggle)); !ok {
						t.Fatalf("side=%v width=%v: the hidden-files indicator left the toolbar", side, width)
					}
				}
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
				if hit, ok := hitByID(&canvas, contextID(hitSFTPHeaderHidden)); !ok || hit.Checked != hidden {
					t.Fatalf("side=%v width=%v hidden=%v: hidden-files menu entry checked=%t", side, width, hidden, hit.Checked)
				}
				state.Click(contextID(hitSFTPHeaderMenuDismiss))
			}
		}
	}
}
