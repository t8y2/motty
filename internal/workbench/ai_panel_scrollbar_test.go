package workbench

import (
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/ai/store"
)

func aiScrollbarPanel(t testing.TB) *State {
	t.Helper()
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.ai.open = true
	longConversation(state, 40)
	return state
}

func aiBarPointer(state *State, target gfx.ID, kind gfx.PointerEventKind, button gfx.PointerButton, y float32, captured bool) gfx.EventResult {
	geometry := state.ai.panel.scrollbar.geometry
	return state.Pointer(gfx.PointerEvent{
		Kind: kind, Button: button, Target: target,
		X: geometry.Track.X + geometry.Track.Width/2, Y: y,
		Viewport: panelWide, Captured: captured,
	})
}

func aiScrollEqual(a, b float32) bool {
	const tolerance = 0.001
	return a-b < tolerance && b-a < tolerance
}

func TestAIMessageScrollbarDragsAndPages(t *testing.T) {
	state := aiScrollbarPanel(t)
	canvas, _ := paintAt(state, panelWide)
	bar := panelHit(t, canvas, hitAIMessageBar)
	if bar.Role != gfx.RoleSlider || bar.Label != state.messages.AIPanel.ScrollbarLabel || bar.Value != "100%" {
		t.Fatalf("message scrollbar semantics = %+v", bar)
	}
	geometry := state.ai.panel.scrollbar.geometry
	if geometry.MaxOffset <= 0 || geometry.Thumb.Height <= 0 {
		t.Fatalf("long conversation has no scrollbar: %+v", geometry)
	}

	// Grab the middle of the thumb: the press itself must not move the content.
	atEnd := state.ai.scroll.Offset()
	grab := geometry.Thumb.Y + geometry.Thumb.Height/2
	result := aiBarPointer(state, hitAIMessageBar, gfx.PointerDown, gfx.PointerButtonPrimary, grab, false)
	if !result.Handled || result.Focus != hitAIMessageBar || !state.ai.panel.scrollbar.dragging {
		t.Fatalf("thumb press = %+v dragging=%v", result, state.ai.panel.scrollbar.dragging)
	}
	if state.ai.scroll.Offset() != atEnd {
		t.Fatalf("pressing the thumb jumped from %v to %v", atEnd, state.ai.scroll.Offset())
	}

	// A hovered move is not part of the captured drag.
	result = aiBarPointer(state, hitAIMessageBar, gfx.PointerMove, gfx.PointerButtonPrimary, grab, false)
	if result.Handled || state.ai.scroll.Offset() != atEnd {
		t.Fatalf("uncaptured move = %+v offset=%v", result, state.ai.scroll.Offset())
	}
	result = aiBarPointer(state, hitAIMessageBar, gfx.PointerMove, gfx.PointerButtonPrimary, geometry.Track.Y, true)
	if !result.Handled || !result.Repaint || state.ai.scroll.Offset() != 0 {
		t.Fatalf("drag to start = %+v offset=%v", result, state.ai.scroll.Offset())
	}
	aiBarPointer(state, hitAIMessageBar, gfx.PointerUp, gfx.PointerButtonPrimary, geometry.Track.Y, true)
	if state.ai.panel.scrollbar.dragging {
		t.Fatal("pointer up left the scrollbar dragging")
	}
	paintAt(state, panelWide)
	geometry = state.ai.panel.scrollbar.geometry

	// Dragging back to the bottom restores following new messages.
	aiBarPointer(state, hitAIMessageBar, gfx.PointerDown, gfx.PointerButtonPrimary, geometry.Thumb.Y+1, false)
	aiBarPointer(state, hitAIMessageBar, gfx.PointerMove, gfx.PointerButtonPrimary, geometry.Track.Y+geometry.Track.Height, true)
	if state.ai.scroll.Offset() != geometry.MaxOffset || !state.ai.scroll.Following() {
		t.Fatalf("drag to end: offset=%v max=%v following=%v", state.ai.scroll.Offset(), geometry.MaxOffset, state.ai.scroll.Following())
	}
	aiBarPointer(state, hitAIMessageBar, gfx.PointerUp, gfx.PointerButtonPrimary, geometry.Track.Y+geometry.Track.Height, true)
	paintAt(state, panelWide)
	geometry = state.ai.panel.scrollbar.geometry

	// A press above the thumb pages up; one below it pages back down.
	before := state.ai.scroll.Offset()
	aiBarPointer(state, hitAIMessageBar, gfx.PointerDown, gfx.PointerButtonPrimary, geometry.Track.Y, false)
	page := state.ai.panel.list.Height * 0.9
	if want := max(before-page, 0); !aiScrollEqual(state.ai.scroll.Offset(), want) {
		t.Fatalf("track above: offset=%v want=%v", state.ai.scroll.Offset(), want)
	}
	if state.ai.panel.scrollbar.dragging {
		t.Fatal("track paging started a thumb drag")
	}
	paintAt(state, panelWide)
	geometry = state.ai.panel.scrollbar.geometry
	before = state.ai.scroll.Offset()
	aiBarPointer(state, hitAIMessageBar, gfx.PointerDown, gfx.PointerButtonPrimary, geometry.Track.Y+geometry.Track.Height-1, false)
	if want := min(before+page, geometry.MaxOffset); !aiScrollEqual(state.ai.scroll.Offset(), want) {
		t.Fatalf("track below: offset=%v want=%v", state.ai.scroll.Offset(), want)
	}

	result = aiBarPointer(state, hitAIMessageBar, gfx.PointerDown, gfx.PointerButtonSecondary, geometry.Thumb.Y+1, false)
	if result.Handled || state.ai.panel.scrollbar.dragging {
		t.Fatalf("secondary button press = %+v dragging=%v", result, state.ai.panel.scrollbar.dragging)
	}
	aiBarPointer(state, hitAIMessageBar, gfx.PointerDown, gfx.PointerButtonPrimary, geometry.Thumb.Y+1, false)
	aiBarPointer(state, hitAIMessageBar, gfx.PointerExit, gfx.PointerButtonPrimary, geometry.Thumb.Y+1, true)
	if state.ai.panel.scrollbar.dragging {
		t.Fatal("pointer exit left the scrollbar dragging")
	}
}

func TestAIScrollbarKeyboardAndAccessibleValue(t *testing.T) {
	state := aiScrollbarPanel(t)
	paintAt(state, panelWide)
	key := func(k gfx.Key) gfx.EventResult {
		return state.Key(gfx.KeyEvent{Key: k, Target: hitAIMessageBar})
	}
	if result := key(gfx.KeyHome); !result.Handled || state.ai.scroll.Offset() != 0 {
		t.Fatalf("Home = %+v offset=%v", result, state.ai.scroll.Offset())
	}
	if result := key(gfx.KeyDown); !result.Handled || state.ai.scroll.Offset() != 24 {
		t.Fatalf("Down = %+v offset=%v", result, state.ai.scroll.Offset())
	}
	if result := key(gfx.KeyPageDown); !result.Handled || !aiScrollEqual(state.ai.scroll.Offset(), 24+state.ai.panel.list.Height*0.9) {
		t.Fatalf("PageDown = %+v offset=%v", result, state.ai.scroll.Offset())
	}
	if result := key(gfx.KeyEnd); !result.Handled || state.ai.scroll.Offset() != state.ai.panel.maxScroll {
		t.Fatalf("End = %+v offset=%v", result, state.ai.scroll.Offset())
	}
	if result := key(gfx.KeyUp); !result.Handled || state.ai.scroll.Offset() != state.ai.panel.maxScroll-24 {
		t.Fatalf("Up = %+v offset=%v", result, state.ai.scroll.Offset())
	}
	key(gfx.KeyHome)
	canvas, _ := paintAt(state, panelWide)
	if value := panelHit(t, canvas, hitAIMessageBar).Value; value != "0%" {
		t.Fatalf("scrollbar value at start = %q", value)
	}
}

func TestAIScrollbarZeroTravelAndStreamingReleaseKeepOffset(t *testing.T) {
	geometry := components.MessageScrollerGeometry{
		Track: gfx.Rect{Height: 18}, Thumb: gfx.Rect{Height: 18}, Offset: 40, MaxOffset: 100,
	}
	if got := geometry.OffsetForThumb(100); got != 40 {
		t.Fatalf("zero-travel thumb moved offset to %v", got)
	}

	state := aiScrollbarPanel(t)
	paintAt(state, panelWide)
	geometry = state.ai.panel.scrollbar.geometry
	y := geometry.Thumb.Y + geometry.Thumb.Height/2
	aiBarPointer(state, hitAIMessageBar, gfx.PointerDown, gfx.PointerButtonPrimary, y, false)
	longConversation(state, 20)
	paintAt(state, panelWide) // auto-follow grows the extent while the pointer stays still
	before := state.ai.scroll.Offset()
	aiBarPointer(state, hitAIMessageBar, gfx.PointerUp, gfx.PointerButtonPrimary, y, true)
	if state.ai.scroll.Offset() != before || state.ai.scroll.Following() || state.ai.panel.scrollbar.dragging {
		t.Fatalf("stationary release: before=%v after=%v following=%v dragging=%v", before, state.ai.scroll.Offset(), state.ai.scroll.Following(), state.ai.panel.scrollbar.dragging)
	}
}

func TestAIScrollbarStreamingDoesNotJumpOnFirstMove(t *testing.T) {
	state := aiScrollbarPanel(t)
	paintAt(state, panelWide)
	geometry := state.ai.panel.scrollbar.geometry
	y := geometry.Thumb.Y + geometry.Thumb.Height/2
	aiBarPointer(state, hitAIMessageBar, gfx.PointerDown, gfx.PointerButtonPrimary, y, false)
	if state.ai.scroll.Following() {
		t.Fatal("starting a thumb drag kept the live edge")
	}
	before := state.ai.scroll.Offset()
	longConversation(state, 20)
	paintAt(state, panelWide)
	if state.ai.scroll.Offset() != before {
		t.Fatalf("streaming moved a held thumb from %v to %v", before, state.ai.scroll.Offset())
	}
	result := aiBarPointer(state, hitAIMessageBar, gfx.PointerMove, gfx.PointerButtonPrimary, y+1, true)
	delta := state.ai.scroll.Offset() - before
	if !result.Handled || delta < 0 || delta > 100 || state.ai.scroll.Following() {
		t.Fatalf("first move after streaming = %+v before=%v after=%v delta=%v following=%v", result, before, state.ai.scroll.Offset(), delta, state.ai.scroll.Following())
	}
	aiBarPointer(state, hitAIMessageBar, gfx.PointerUp, gfx.PointerButtonPrimary, y+1, true)
}

func TestClosingAIPanelCancelsScrollbarDrag(t *testing.T) {
	state := aiScrollbarPanel(t)
	paintAt(state, panelWide)
	geometry := state.ai.panel.scrollbar.geometry
	aiBarPointer(state, hitAIMessageBar, gfx.PointerDown, gfx.PointerButtonPrimary, geometry.Thumb.Y+1, false)
	before := state.ai.scroll.Offset()
	state.setAIPanelOpen(false)
	paintAt(state, panelWide)
	if state.ai.panel.scrollbar.dragging {
		t.Fatal("closing the panel left the scrollbar dragging")
	}
	aiBarPointer(state, hitAIMessageBar, gfx.PointerMove, gfx.PointerButtonPrimary, geometry.Track.Y, true)
	if state.ai.scroll.Offset() != before {
		t.Fatalf("a cancelled drag moved the hidden conversation from %v to %v", before, state.ai.scroll.Offset())
	}
}

func TestAIHistoryScrollbarDragsAndUsesKeyboard(t *testing.T) {
	state := aiScrollbarPanel(t)
	for index := 0; index < 40; index++ {
		state.ai.conversations = append(state.ai.conversations, store.Conversation{ID: int64(index + 1), Title: "conversation"})
	}
	state.ai.panel.history = true
	canvas, _ := paintAt(state, panelWide)
	bar := panelHit(t, canvas, hitAIHistoryBar)
	if bar.Role != gfx.RoleSlider || bar.Label != state.messages.AIPanel.HistoryScrollbarLabel || bar.Value != "0%" {
		t.Fatalf("history scrollbar semantics = %+v", bar)
	}
	geometry := state.ai.panel.scrollbar.geometry
	aiBarPointer(state, hitAIHistoryBar, gfx.PointerDown, gfx.PointerButtonPrimary, geometry.Thumb.Y+1, false)
	aiBarPointer(state, hitAIHistoryBar, gfx.PointerMove, gfx.PointerButtonPrimary, geometry.Track.Y+geometry.Track.Height, true)
	if state.ai.panel.historyTop != geometry.MaxOffset {
		t.Fatalf("history drag to end = %v, want %v", state.ai.panel.historyTop, geometry.MaxOffset)
	}
	aiBarPointer(state, hitAIHistoryBar, gfx.PointerUp, gfx.PointerButtonPrimary, geometry.Track.Y+geometry.Track.Height, true)
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyHome, Target: hitAIHistoryBar}); !result.Handled || state.ai.panel.historyTop != 0 {
		t.Fatalf("history Home = %+v offset=%v", result, state.ai.panel.historyTop)
	}
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyDown, Target: hitAIHistoryBar}); !result.Handled || state.ai.panel.historyTop != 24 {
		t.Fatalf("history Down = %+v offset=%v", result, state.ai.panel.historyTop)
	}
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnd, Target: hitAIHistoryBar}); !result.Handled || state.ai.panel.historyTop != state.ai.panel.maxTop {
		t.Fatalf("history End = %+v offset=%v", result, state.ai.panel.historyTop)
	}
}

func TestAIScrollbarThemeKeepsThumbVisible(t *testing.T) {
	state, _ := panelState(t, nil)
	for _, appearance := range []appearance{appearanceDark, appearanceLight} {
		state.setAppearance(appearance)
		theme := state.aiScrollbarTheme()
		// The thumb used to take the hairline border color, which is invisible
		// against the panel; it must differ from it and from the track, and the
		// hover state must be stronger than the resting one.
		if theme.Border == state.pal.line {
			t.Fatalf("%s resting thumb still uses the hairline color", appearance)
		}
		if theme.MutedForeground == theme.Border {
			t.Fatalf("%s hover thumb is not stronger than the resting one", appearance)
		}
		if state.Theme().Border != state.pal.line {
			t.Fatalf("%s scrollbar changed the global border color", appearance)
		}
	}
}

func TestAIScrollbarDragEndsOnANewPressElsewhere(t *testing.T) {
	state := aiScrollbarPanel(t)
	paintAt(state, panelWide)
	geometry := state.ai.panel.scrollbar.geometry
	y := geometry.Thumb.Y + geometry.Thumb.Height/2
	aiBarPointer(state, hitAIMessageBar, gfx.PointerDown, gfx.PointerButtonPrimary, y, false)
	if !state.ai.panel.scrollbar.dragging {
		t.Fatal("the thumb did not start dragging")
	}
	// The next press lands somewhere else because the release was lost (the
	// window deactivated mid-drag). It must pass through, not be swallowed.
	result, handled := state.aiPanelScrollbarPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hitAIToEnd, X: 10, Y: 10, Viewport: panelWide})
	if state.ai.panel.scrollbar.dragging {
		t.Fatal("a press elsewhere kept the stale drag")
	}
	if handled {
		t.Fatalf("the scrollbar handler swallowed the press: %+v", result)
	}
}

func TestAIMessageScrollbarNeedsOverflow(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiAssistant, text: "short answer"}}
	canvas, _ := paintAt(state, panelWide)
	if _, ok := hitByID(canvas, hitAIMessageBar); ok {
		t.Fatal("a short conversation paints an interactive scrollbar")
	}
}
