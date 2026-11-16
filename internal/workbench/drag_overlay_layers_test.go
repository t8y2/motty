package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// The card ghost is a hitless element of a page overlay on ui.LayerDrag, the
// band below dialogs and menus. A dialog opening over a drag cancels it, so no
// ghost is left to reappear later.
func TestWorkspaceCardDragPaintsOnTheDragLayerAndCancelsUnderADialog(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "API", workspaceCatalogIDs(state, 0, 1, 2), 2)
	state.templates = []WorkspaceTemplate{template}
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	source, ok := hitByID(&canvas, hitWorkspaceTemplateCards.At(0))
	if !ok {
		t.Fatal("the workspaces page painted no template card")
	}
	at := gfx.Point{X: source.Bounds.X + 20, Y: source.Bounds.Y + 20}
	target := hitWorkspaceTemplateCards.At(0)
	press(state, target, at)
	dragTo(state, target, gfx.Point{X: at.X + 40, Y: at.Y + 30})
	if !state.workspaceCardDrag.active {
		t.Fatal("the card drag did not start")
	}

	canvas.Reset()
	state.Paint(&canvas, size)
	drag := state.workspaceCardDrag
	ghost := gfx.Rect{X: drag.at.X + 12, Y: drag.at.Y + 12, Width: 176, Height: 32}
	onDragLayer := false
	for _, text := range canvas.Texts() {
		if text.Layer == uint16(ui.LayerDrag) && pointInRect(text.Bounds.X, text.Bounds.Y, ghost) {
			onDragLayer = true
			break
		}
	}
	if !onDragLayer {
		t.Fatalf("the card ghost is not on the drag layer %d (dialogs paint at %d)", ui.LayerDrag, dialogLayer)
	}

	state.openCommandPalette()
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.workspaceCardDrag.active || state.workspaceCardDrag.pressed {
		t.Fatalf("opening a dialog left the card drag %+v", state.workspaceCardDrag)
	}
	for _, text := range canvas.Texts() {
		if text.Layer == uint16(ui.LayerDrag) {
			t.Fatalf("the cancelled card drag still paints %q on the drag layer", text.Value)
		}
	}

	state.closeCommandPalette()
	press(state, target, at)
	dragTo(state, target, gfx.Point{X: at.X + 40, Y: at.Y + 30})
	if !state.workspaceCardDrag.active {
		t.Fatal("the card drag did not restart")
	}
	state.tabMenu = tabMenu{open: true}
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.workspaceCardDrag.active || state.workspaceCardDrag.pressed {
		t.Fatalf("opening a menu left the card drag %+v", state.workspaceCardDrag)
	}
	for _, text := range canvas.Texts() {
		if text.Layer == uint16(ui.LayerDrag) {
			t.Fatalf("the menu-cancelled card drag still paints %q on the drag layer", text.Value)
		}
	}
	state.tabMenu = tabMenu{}
}

// The docked toolbar's drag feedback is a hitless page overlay on ui.LayerDrag,
// below dialogs and menus, and it is cancelled when one opens.
func TestToolbarDockDragPaintsOnTheDragLayerAndCancelsUnderADialog(t *testing.T) {
	state, _, _ := toolbarDockView(t)
	size := state.viewport
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	var target gfx.ID
	var at gfx.Point
	for _, hit := range state.toolbarDock.hits[:state.toolbarDock.count] {
		if hit.tool.id == hitTheme {
			target = hit.tool.id
			at = gfx.Point{X: hit.bounds.X + hit.bounds.Width/2, Y: hit.bounds.Y + hit.bounds.Height/2}
			break
		}
	}
	if target.IsZero() {
		t.Fatal("the docked toolbar painted no theme tool")
	}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: target, X: at.X, Y: at.Y, Viewport: size})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: target, Captured: true, X: at.X + 40, Y: at.Y + 30, Viewport: size})
	if !state.toolbarDock.drag.active {
		t.Fatal("the toolbar drag did not start")
	}

	canvas.Reset()
	state.Paint(&canvas, size)
	onDragLayer := false
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Layer == uint16(ui.LayerDrag) {
			onDragLayer = true
			break
		}
	}
	if !onDragLayer {
		t.Fatalf("the toolbar drag feedback is not on the drag layer %d (dialogs paint at %d)", ui.LayerDrag, dialogLayer)
	}

	state.openCommandPalette()
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.toolbarDock.drag.active || state.toolbarDock.drag.pressed {
		t.Fatalf("opening a dialog left the toolbar drag %+v", state.toolbarDock.drag)
	}
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Layer == uint16(ui.LayerDrag) {
			t.Fatalf("the cancelled toolbar drag still paints on the drag layer: %+v", rectangle.Bounds)
		}
	}

	state.closeCommandPalette()
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: target, X: at.X, Y: at.Y, Viewport: size})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: target, Captured: true, X: at.X + 40, Y: at.Y + 30, Viewport: size})
	if !state.toolbarDock.drag.active {
		t.Fatal("the toolbar drag did not restart")
	}
	state.tabMenu = tabMenu{open: true}
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.toolbarDock.drag.active || state.toolbarDock.drag.pressed {
		t.Fatalf("opening a menu left the toolbar drag %+v", state.toolbarDock.drag)
	}
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Layer == uint16(ui.LayerDrag) {
			t.Fatalf("the menu-cancelled toolbar drag still paints on the drag layer: %+v", rectangle.Bounds)
		}
	}
	state.tabMenu = tabMenu{}
}
