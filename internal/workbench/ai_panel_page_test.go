package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// TestAIPanelPageReadsItsTitleControls drives the real event pass: the title
// row is declared by the panel's page, so the page (not the window's central
// click dispatcher) reads a click delivered to Close, History or New. The same
// pass is what the runtime produces for Enter or Space on a focused control, so
// the keyboard route is covered too.
func TestAIPanelPageReadsItsTitleControls(t *testing.T) {
	state, _ := panelState(t, nil)
	state.Click(hitAIToggle)
	paintAt(state, panelWide)
	page := state.ai.panel.page
	if page == nil {
		t.Fatal("the assistant panel painted without a page")
	}

	// The title controls no longer go through State.Click: the page's
	// declaration is where the event pass is read.
	state.Click(hitAIClose)
	if !state.ai.open {
		t.Fatal("a click outside the event pass closed the panel")
	}

	clickAIPanelPage(t, state, hitAIHistory)
	if !state.ai.panel.history {
		t.Fatal("the page did not read the history click")
	}
	clickAIPanelPage(t, state, hitAIHistory)
	if state.ai.panel.history {
		t.Fatal("the page did not read the second history click")
	}

	clickAIPanelPage(t, state, hitAIClose)
	if state.ai.open {
		t.Fatal("the page did not read the close click")
	}

	// The page is consulted before the panel's own key handling and leaves a
	// key aimed at the hand-painted composer to it.
	if result, handled := page.Key(gfx.KeyEvent{Key: gfx.KeyEscape, Target: hitAIInput}); handled || result.Handled {
		t.Fatalf("the page took a key aimed at the composer: %+v %v", result, handled)
	}
}
