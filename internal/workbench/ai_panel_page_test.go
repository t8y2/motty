package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/ai/agent"
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

// TestAIComposerPageControlsTakeRealPointerClicks drives the composer through
// the runtime's pointer, not the central dispatcher directly: the chips and Send
// are page elements now, and a real press must still reach the same identities
// and the same actions.
func TestAIComposerPageControlsTakeRealPointerClicks(t *testing.T) {
	state, _ := panelState(t, quietModel{"ok"})
	withModel(t, state)
	state.ai.open = true
	state.ai.mode = agent.ModeAsk
	view, err := headless.Open(app.Config{
		Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint, OnResize: state.Resize,
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll,
		TextInput: state.AIPanelInput,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	find := func(id gfx.ID) gfx.Hit {
		t.Helper()
		for _, hit := range view.Snapshot().Hits {
			if hit.ID == id {
				return hit
			}
		}
		t.Fatalf("hit %v is not painted", id)
		return gfx.Hit{}
	}
	click := func(id gfx.ID) {
		t.Helper()
		hit := find(id)
		if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
	}

	click(hitAIMode)
	if state.ai.mode != agent.ModeAgent {
		t.Fatalf("mode after a real click = %v", state.ai.mode)
	}
	click(hitAIModelChip)
	if !state.ai.panel.modelMenu {
		t.Fatal("a real click on the model chip did not open its menu")
	}
	if err := view.Keys("Escape"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	click(hitAIInput)
	if err := view.Type("hello there"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	click(hitAISend)
	if got := state.ai.input.String(); got != "" {
		t.Fatalf("Send left the question in the box: %q", got)
	}
}
