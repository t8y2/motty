package workbench

import (
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/ai/agent"
	"motty/internal/ai/gate"
	"motty/internal/ai/risk"
)

// The pinned question is a card, and the card's own background is one of the
// page's children. The confirming buttons are slots painted by hand, so they
// must be painted after those children; a review found the Allow button reduced
// to its label, near-black on the dark card, because the card's fill covered
// every button surface. This asserts the invariant that matters: whatever is
// painted last over the middle of the button is the button's own surface, not
// something behind it.
func TestTheApprovalButtonsSurfaceIsPaintedOverTheCard(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.ai.mode = agent.ModeAgent
	state.setAIPanelOpen(true)
	state.ai.approvals = append(state.ai.approvals, &aiApproval{request: gate.Request{
		Tool:         "terminal_exec",
		Summary:      "touch created-by-assistant",
		SessionLabel: "staging-web",
		Assessment:   risk.Assessment{Level: risk.Medium},
	}})

	canvas, _ := paintAt(state, panelWide)
	approve := panelHit(t, canvas, hitAIApprove)
	want := components.ButtonRecipe(state.Theme(), components.ButtonDefault, gfx.State{}).Background
	center := gfx.Point{
		X: approve.Bounds.X + approve.Bounds.Width/2,
		Y: approve.Bounds.Y + approve.Bounds.Height/2,
	}

	var last gfx.Rectangle
	found := false
	for _, rectangle := range canvas.Rectangles() {
		bounds := rectangle.Bounds
		if center.X < bounds.X || center.Y < bounds.Y ||
			center.X >= bounds.X+bounds.Width || center.Y >= bounds.Y+bounds.Height {
			continue
		}
		// A translucent tint lets the layers under it show; only an opaque fill
		// can hide the button's surface.
		if rectangle.Color.Alpha < 1 {
			continue
		}
		last, found = rectangle, true
	}
	if !found || last.Bounds != approve.Bounds || last.Color != want {
		t.Fatalf("what is painted last over the approval button's middle is %+v, want the button's own fill %+v at %+v",
			last, want, approve.Bounds)
	}
}
