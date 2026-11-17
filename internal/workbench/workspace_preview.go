package workbench

import (
	"fmt"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

// Preview geometry comes from the same layout tree as a launched workspace,
// including the full-width last row when it has fewer panes.
func (state *State) paintWorkspaceGridPreview(canvas *gfx.Canvas, bounds gfx.Rect, hosts []string, spec layoutcore.LayoutSpec) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	if len(hosts) == 0 {
		canvas.RoundedBorder(bounds, radius.control, 1, state.pal.line, state.pal.terminal)
		if bounds.Width > 180 {
			monitorLabel(canvas, elide(canvas, state.messages.WorkspaceDraftPreviewEmpty, 12, gfx.WeightRegular, bounds.Width-24), bounds.Inset(gfx.All(12)), 12, state.pal.muted, gfx.WeightRegular)
		}
		return
	}
	sessions := make([]layoutcore.SessionID, len(hosts))
	for index := range sessions {
		sessions[index] = layoutcore.SessionID(index + 1)
	}
	layout, err := layoutcore.NewLayout(sessions, spec)
	if err != nil {
		return
	}
	state.paintWorkspacePreviewNode(canvas, bounds, &layout, layout.Root(), hosts)
}

func (state *State) paintWorkspacePreviewNode(canvas *gfx.Canvas, bounds gfx.Rect, layout *layoutcore.Layout, id layoutcore.NodeID, hosts []string) {
	node, ok := layout.Node(id)
	if !ok {
		return
	}
	if node.Kind == layoutcore.KindSplit {
		first, second := splitFractionBounds(FractionBounds{Width: 1, Height: 1}, node.Axis, node.Ratio)
		state.paintWorkspacePreviewNode(canvas, pixelBounds(bounds, first), layout, node.First, hosts)
		state.paintWorkspacePreviewNode(canvas, pixelBounds(bounds, second), layout, node.Second, hosts)
		return
	}
	box := bounds.Inset(gfx.All(2))
	canvas.RoundedBorder(box, 4, 1, state.pal.line, state.pal.terminal)
	index := int(node.Session) - 1
	color := state.pal.muted
	label := fmt.Sprintf("%d", index+1)
	if host, ok := state.workspaceHostByID(hosts[index]); ok {
		if accent := hostAccent(host.Color); accent.Alpha > 0 {
			canvas.Rounded(gfx.Rect{X: box.X + box.Width - 10, Y: box.Y + 6, Width: 4, Height: 4}, 2, accent)
		}
		if box.Width >= 88 && box.Height >= 38 {
			label += "  " + host.Label
		}
	}
	monitorLabel(canvas, elide(canvas, label, 11, gfx.WeightMedium, max(0, box.Width-16)), box.Inset(gfx.All(8)), 11, color, gfx.WeightMedium)
}
