package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/ai/agent"
	"motty/internal/ai/risk"
)

// TestAIPanelPageReadsItsTitleControls drives the real event pass: the title
// row is declared by the workbench page, so the page (not the window's central
// click dispatcher) reads a click delivered to Close, History or New. The same
// pass is what the runtime produces for Enter or Space on a focused control, so
// the keyboard route is covered too.
func TestAIPanelPageReadsItsTitleControls(t *testing.T) {
	state, _ := panelState(t, nil)
	state.Click(hitAIToggle)
	paintAt(state, panelWide)
	page := state.chrome.windowPage
	if page == nil {
		t.Fatal("the assistant panel painted without the workbench page")
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
	if result := state.WindowConfig(app.Config{}).OnKey(gfx.KeyEvent{Key: gfx.KeyEscape, Target: hitAIInput}); result.Handled {
		t.Fatalf("the page took a key aimed at the composer: %+v", result)
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
	view := openAIPanelView(t, state)
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

// openAIPanelView opens a headless window whose events go to the workbench, as
// the application's window does.
func openAIPanelView(t testing.TB, state *State) *headless.Session {
	t.Helper()
	view, err := headless.Open(state.WindowConfig(app.Config{
		Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint, OnResize: state.Resize,
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll,
		TextInput: state.AIPanelInput,
	}), nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return view
}

// TestAIPanelEventPassGoesThroughTheWorkbenchPage drives the panel after it
// became part of the workbench page: a chip click, the composer's native typing
// and the message wheel all arrive through the page's event pass, and the open
// chip menu is a popup the page declares and owns.
func TestAIPanelEventPassGoesThroughTheWorkbenchPage(t *testing.T) {
	state, _ := panelState(t, quietModel{"ok"})
	withModel(t, state)
	state.ai.open = true
	state.ai.mode = agent.ModeAgent
	view := openAIPanelView(t, state)
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	page := state.chrome.windowPage
	if page == nil {
		t.Fatal("the panel painted without the workbench page")
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
	clickHit := func(hit gfx.Hit) {
		t.Helper()
		if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
	}

	// A chip click reaches the page's declaration: the mode pill turns Agent into
	// Ask and the next click turns it back.
	clickHit(find(hitAIMode))
	if state.ai.mode != agent.ModeAsk {
		t.Fatalf("mode after the chip click = %v", state.ai.mode)
	}
	clickHit(find(hitAIMode))
	if state.ai.mode != agent.ModeAgent {
		t.Fatalf("mode after the second chip click = %v", state.ai.mode)
	}

	// The approval scope menu is a popup of the workbench page: the page owns its
	// rows, and choosing one moves the scope.
	clickHit(find(hitAIAutoScope))
	if !state.aiMenuShown() {
		t.Fatal("the chip menu was not declared as an open popup")
	}
	row, found := gfx.Hit{}, false
	for _, hit := range view.Snapshot().Hits {
		if hit.Role == gfx.RoleMenuItem && hit.Label == state.aiScopeName(risk.High) {
			row, found = hit, true
			break
		}
	}
	if !found {
		t.Fatalf("no scope row %q in the page's menu", state.aiScopeName(risk.High))
	}
	if row.ID.IsZero() || !tree.Within(row.ID.Scope(), tree.Feature(shellPageRoot)) {
		t.Fatalf("the menu row %v is not named by the workbench page", row.ID)
	}
	clickHit(row)
	if state.aiAutoUpTo() != risk.High {
		t.Fatalf("scope after the menu row = %v", state.aiAutoUpTo())
	}

	// Typing goes to the composer through the native editor the event pass asks
	// the page for.
	clickHit(find(hitAIInput))
	if err := view.Type("hello there"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if got := state.ai.input.String(); got != "hello there" {
		t.Fatalf("typed %q", got)
	}

	// The wheel over the message list moves the conversation the page declared.
	for index := 0; index < 40; index++ {
		state.ai.items = append(state.ai.items, aiItem{kind: aiAssistant, text: "line of an answer that is long enough to be a message number " + string(rune('A'+index%26))})
	}
	list := state.ai.panel.list
	if list.Height <= 0 {
		t.Fatal("the page declared no message list")
	}
	// A move is a real pass through the runtime, so the new messages are painted
	// and the list measures again.
	if err := view.Move(list.X+list.Width/2, list.Y+list.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	before := aiListScroll(t, state).Offset
	if state.ai.panel.maxScroll <= 0 {
		t.Fatalf("the messages are not scrollable: max %v", state.ai.panel.maxScroll)
	}
	err := view.Scroll(gfx.ScrollEvent{X: list.X + list.Width/2, Y: list.Y + list.Height/2, DeltaY: -120, Precise: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if aiListScroll(t, state).Offset >= before {
		t.Fatalf("the wheel did not move the list: %v -> %v", before, aiListScroll(t, state).Offset)
	}
}

// TestAIListControlTakesRealRowAndListEvents drives the list's own control in
// the runtime's event pass: a click on a row, a drag over answer text, a press
// on the list's ground (which focuses the control) and the page keys it then
// gets. The control's handlers are what the page routes those events to; the
// rows stay the hit identities the conversation has always used.
func TestAIListControlTakesRealRowAndListEvents(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	conversationFixture(state)
	// Enough rows below the visible ones that PageDown has somewhere to go.
	for index := 0; index < 20; index++ {
		state.ai.items = append(state.ai.items, aiItem{kind: aiAssistant, text: "another answer below the fold"})
	}
	state.ai.open = true
	// Paint once with the conversation, put the reader at its top, then paint
	// the frame the headless window will start from.
	paintAt(state, panelWide)
	aiListScroll(t, state).ScrollTo(0)
	paintAt(state, panelWide)
	view := openAIPanelView(t, state)
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
	point := func(bounds gfx.Rect) (float32, float32) {
		return bounds.X + bounds.Width/2, bounds.Y + bounds.Height/2
	}

	// A press on the reasoning row's header is a click the runtime delivers as
	// the row's identity, and the panel's gesture opens it as it always has.
	row := find(hitAIRows.At(1))
	if err := view.Click(point(row.Bounds)); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !state.ai.panel.rows[1].expanded {
		t.Fatal("a real click on the row header did not expand it")
	}

	// A drag over the answer's text selects it through the list's Pointer path,
	// and Escape, aimed at the focused text, clears it.
	text := find(hitAIAnswerText.At(2))
	start := gfx.Point{X: text.Bounds.X + 2, Y: text.Bounds.Y + 2}
	end := gfx.Point{X: text.Bounds.X + text.Bounds.Width - 2, Y: text.Bounds.Y + text.Bounds.Height - 2}
	if err := view.Press(start.X, start.Y); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(end.X, end.Y); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(end.X, end.Y); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !state.ai.panel.hasAITextSelection() {
		t.Fatal("dragging over the answer left no selection")
	}
	if err := view.Keys("Escape"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if state.ai.panel.hasAITextSelection() {
		t.Fatal("Escape did not clear the selection")
	}

	// The list is a page Control: a press on its ground focuses it, and a page
	// key then reaches its Key handler and scrolls the conversation.
	snapshot := view.Snapshot()
	listbox := gfx.Hit{}
	for _, hit := range snapshot.Hits {
		if hit.Label == state.messages.AIPanel.ListLabel && hit.Role == gfx.RoleListBox {
			listbox = hit
			break
		}
	}
	if !listbox.Container {
		t.Fatal("the conversation List is not painted")
	}
	// The List's own row is the option around a message. A press on a point of
	// the row that its contents do not cover focuses the row, and a page key
	// then moves and reveals the next rows.
	option := gfx.Hit{}
	for _, hit := range snapshot.Hits {
		if hit.Role == gfx.RoleOption && pointInRect(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2, listbox.Bounds) {
			option = hit
			break
		}
	}
	if option.ID.IsZero() {
		t.Fatal("the conversation List declares no row")
	}
	x, y, found := float32(0), float32(0), false
	for py := option.Bounds.Y + 2; py < option.Bounds.Y+option.Bounds.Height && !found; py += 4 {
		for px := option.Bounds.X + 2; px < option.Bounds.X+option.Bounds.Width; px += 4 {
			top := gfx.ID{}
			for _, hit := range snapshot.Hits {
				if pointInRect(px, py, hit.Bounds) {
					top = hit.ID
				}
			}
			if top == option.ID {
				x, y, found = px, py, true
				break
			}
		}
	}
	if !found {
		t.Fatal("no point of a List row is uncovered by the row's contents")
	}
	if err := view.Click(x, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if got := view.Snapshot().Focused; got != option.ID {
		t.Fatalf("a press on a List row focused %v, want the row %v", got, option.ID)
	}
	before := aiListScroll(t, state).Offset
	if err := view.Keys("pagedown"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if aiListScroll(t, state).Offset <= before {
		t.Fatalf("PageDown on the List row did not scroll: %v -> %v", before, aiListScroll(t, state).Offset)
	}

	// The composer and the model menu still take their events through the page.
	input := find(hitAIInput)
	if err := view.Click(point(input.Bounds)); err != nil {
		t.Fatal(err)
	}
	if err := view.Type("hello"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if got := state.ai.input.String(); got != "hello" {
		t.Fatalf("typing reached the composer as %q", got)
	}
	chip := find(hitAIModelChip)
	if err := view.Click(point(chip.Bounds)); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !state.ai.panel.modelMenu {
		t.Fatal("the model chip did not open its menu")
	}
}
