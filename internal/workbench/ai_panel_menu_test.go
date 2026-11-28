package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
)

func aiMenuRightClick(state *State, at gfx.Point, target gfx.ID) gfx.EventResult {
	return state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, X: at.X, Y: at.Y, Target: target, Viewport: state.viewport,
	})
}

func TestAIMenuRightClickPreservesSelectionAndDisablesCopyWhenEmpty(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiAssistant, text: "alpha beta gamma"}}
	paintAt(state, panelWide)
	panel := &state.ai.panel
	at := hitCenter(t, func() *gfx.Canvas { c, _ := paintAt(state, panelWide); return c }(), hitAIAnswerText.At(0))

	if result := aiMenuRightClick(state, at, hitAIAnswerText.At(0)); !result.Handled || !result.Repaint {
		t.Fatalf("secondary press = %+v", result)
	}
	if !panel.contextMenu.open {
		t.Fatal("right-click did not open the assistant context menu")
	}
	canvas, _ := paintAt(state, panelWide)
	copyItem, copyOK := menuHit(canvas, state, hitAIMenuCopy)
	msgItem, msgOK := menuHit(canvas, state, hitAIMenuCopyMessage)
	followItem, followOK := menuHit(canvas, state, hitAIMenuFollowUp)
	allItem, allOK := menuHit(canvas, state, hitAIMenuSelectAll)
	if !copyOK || !copyItem.Disabled {
		t.Fatalf("Copy without selection = %+v, ok=%v; want disabled", copyItem, copyOK)
	}
	if !msgOK || msgItem.Disabled || !allOK || allItem.Disabled {
		t.Fatalf("CopyMessage=%+v(%v) SelectAll=%+v(%v); want both enabled", msgItem, msgOK, allItem, allOK)
	}
	if !followOK || followItem.Disabled || followItem.Label != state.messages.AIPanel.MenuFollowUpMessage {
		t.Fatalf("FollowUp without selection = %+v, ok=%v; want %q", followItem, followOK, state.messages.AIPanel.MenuFollowUpMessage)
	}
	if _, ok := aiMenuRowNamed(canvas, state.messages.AIPanel.CopyCode); ok {
		t.Fatal("Copy code appeared outside a fenced code block")
	}

	// Selecting text before right-clicking keeps the selection intact and enables Copy.
	panel.contextMenu.open = false
	selectAnswerRange(panel, 0, 0, 5)
	paintAt(state, panelWide)
	aiMenuRightClick(state, at, hitAIAnswerText.At(0))
	if got, ok := panel.aiTextSelectionText(); !ok || got != "alpha" {
		t.Fatalf("right-click dropped selection: %q, %v", got, ok)
	}
	canvas, _ = paintAt(state, panelWide)
	copyItem, copyOK = menuHit(canvas, state, hitAIMenuCopy)
	followItem, followOK = menuHit(canvas, state, hitAIMenuFollowUp)
	if !copyOK || copyItem.Disabled {
		t.Fatalf("Copy with selection = %+v, ok=%v; want enabled", copyItem, copyOK)
	}
	if !followOK || followItem.Label != state.messages.AIPanel.MenuFollowUpSelection {
		t.Fatalf("FollowUp with selection = %+v, ok=%v; want %q", followItem, followOK, state.messages.AIPanel.MenuFollowUpSelection)
	}
}

func TestAIMenuCopySelectionCodeBlockAndMessage(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	answer := "Intro line\n\n```sh\necho hello\npwd\n```\n\nTail"
	state.ai.items = []aiItem{
		{kind: aiAssistant, text: answer},
		{kind: aiReasoning, text: "thinking steps"},
		{kind: aiTool, call: llm.ToolCall{ID: "1", Name: "terminal_exec", Arguments: `{"command":"ls -la"}`}, status: aiToolDone, result: "total 0"},
	}
	paintAt(state, panelWide)
	panel := &state.ai.panel

	var copied string
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }

	// Copy selection via menu.
	selectAnswerRange(panel, 0, 0, len("Intro"))
	paintAt(state, panelWide)
	introAt := answerCaretCenter(t, &panel.rows[0], 2)
	aiMenuRightClick(state, introAt, hitAIAnswerText.At(0))
	chooseMenu(t, state, hitAIMenuCopy)
	if copied != "Intro" {
		t.Fatalf("menu Copy copied %q, want %q", copied, "Intro")
	}

	// Copy fenced code block when right-clicking inside the code block.
	code := panel.rows[0].md.Codes()[0]
	codeAt := gfx.Point{
		X: panel.rows[0].textRect.X + code.Bounds.X + 12,
		Y: panel.rows[0].textRect.Y + code.Bounds.Y + 12,
	}
	aiMenuRightClick(state, codeAt, hitAIAnswerText.At(0))
	canvas, _ := paintAt(state, panelWide)
	if codeItem, ok := menuHit(canvas, state, hitAIMenuCopyCode); !ok || codeItem.Disabled {
		t.Fatalf("Copy code item missing or disabled inside code block: %+v, %v", codeItem, ok)
	}
	chooseMenu(t, state, hitAIMenuCopyCode)
	if copied != "echo hello\npwd" || panel.copied != "echo hello\npwd" {
		t.Fatalf("menu CopyCode copied %q (panel.copied=%q)", copied, panel.copied)
	}

	// Copy whole Markdown message from the assistant row.
	aiMenuRightClick(state, introAt, hitAIAnswerText.At(0))
	chooseMenu(t, state, hitAIMenuCopyMessage)
	if copied != answer || panel.copied != answer {
		t.Fatalf("menu CopyMessage copied %q, want %q", copied, answer)
	}

	// Collapsed reasoning allows CopyMessage (and disables SelectAll).
	canvas, _ = paintAt(state, panelWide)
	reasonAt := hitCenter(t, canvas, hitAIRows.At(1))
	aiMenuRightClick(state, reasonAt, hitAIRows.At(1))
	canvas, _ = paintAt(state, panelWide)
	if allItem, ok := menuHit(canvas, state, hitAIMenuSelectAll); !ok || !allItem.Disabled {
		t.Fatalf("collapsed reasoning SelectAll = %+v, %v; want disabled", allItem, ok)
	}
	chooseMenu(t, state, hitAIMenuCopyMessage)
	if copied != "thinking steps" {
		t.Fatalf("reasoning CopyMessage = %q", copied)
	}

	// Tool message copies both command and output.
	canvas, _ = paintAt(state, panelWide)
	toolAt := hitCenter(t, canvas, hitAIAnswerText.At(2))
	aiMenuRightClick(state, toolAt, hitAIAnswerText.At(2))
	chooseMenu(t, state, hitAIMenuCopyMessage)
	if copied != "ls -la\n\ntotal 0" {
		t.Fatalf("tool CopyMessage = %q, want %q", copied, "ls -la\n\ntotal 0")
	}
}

func TestAIMenuFollowUpQuotesMarkdownAndFocusesInput(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	state.ai.mode = agent.ModeAgent
	state.ai.items = []aiItem{
		{kind: aiAssistant, text: "line one\n\nline two"},
		{kind: aiNotice, text: strings.Repeat("x", 4500)},
	}
	paintAt(state, panelWide)
	// Keep row 0 inside the message list viewport rather than scrolled above the overlong notice.
	panel := &state.ai.panel
	panel.followSuppressed = true
	aiListScroll(t, state).ScrollTo(0)
	paintAt(state, panelWide)

	// Selection follow-up quotes as Markdown blockquote without sending or changing mode.
	selectAnswerRange(panel, 0, 0, len("line one\nline two"))
	paintAt(state, panelWide)
	at := answerCaretCenter(t, &panel.rows[0], 2)
	aiMenuRightClick(state, at, hitAIAnswerText.At(0))
	chooseMenu(t, state, hitAIMenuFollowUp)
	want := "> line one\n> line two\n\n"
	if got := state.ai.input.String(); got != want {
		t.Fatalf("follow-up input = %q, want %q", got, want)
	}
	if state.ai.run != nil || state.ai.mode != agent.ModeAgent {
		t.Fatalf("follow-up unexpectedly started a run or changed mode: run=%v mode=%v", state.ai.run, state.ai.mode)
	}
	canvas, _ := paintAt(state, panelWide)
	if got := canvas.FocusOnce(); got != hitAIInput {
		t.Fatalf("focus after follow-up = %v, want %v", got, hitAIInput)
	}

	// Existing draft is preserved and the quote is appended after it.
	state.ai.input.Insert("why?")
	panel.clearAITextSelection()
	paintAt(state, panelWide)
	aiMenuRightClick(state, at, hitAIAnswerText.At(0))
	chooseMenu(t, state, hitAIMenuFollowUp)
	wantAppended := "> line one\n> line two\n\nwhy?\n\n> line one\n>\n> line two\n\n"
	if got := state.ai.input.String(); got != wantAppended {
		t.Fatalf("appended follow-up = %q, want %q", got, wantAppended)
	}

	// Overlong messages are safely truncated before quoting.
	state.ai.input = textEditorWith("")
	state.aiMessagesToEnd()
	canvas, _ = paintAt(state, panelWide)
	noticeAt := hitCenter(t, canvas, hitAIAnswerText.At(1))
	aiMenuRightClick(state, noticeAt, hitAIAnswerText.At(1))
	chooseMenu(t, state, hitAIMenuFollowUp)
	if got := state.ai.input.String(); !strings.HasPrefix(got, "> ") || !strings.HasSuffix(got, "\n> …\n\n") || len(got) > 4100 {
		t.Fatalf("truncated follow-up unexpected: len=%d suffix=%q", len(got), got[max(len(got)-16, 0):])
	}
}

func TestAIMenuLifecycleAndScrollLock(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	for i := 0; i < 30; i++ {
		state.ai.items = append(state.ai.items, aiItem{kind: aiAssistant, text: "scrollable answer line"})
	}
	view, err := openHeadlessTestView(state, 1400, 720)
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}

	list := state.ai.panel.list
	if err := view.RightClick(list.X+list.Width/2, list.Y+list.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !state.ai.panel.contextMenu.open {
		t.Fatal("right-click inside scrollable list did not open menu")
	}

	// Scrolling the wheel while the menu is open must not scroll the conversation underneath.
	before := aiListScroll(t, state).Offset
	if err := view.Scroll(gfx.ScrollEvent{X: list.X + 20, Y: list.Y + 20, DeltaY: -120, Precise: true}); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if got := aiListScroll(t, state).Offset; got != before {
		t.Fatalf("wheel scrolled conversation while menu open: %v -> %v", before, got)
	}

	// Pressing Escape dismisses the menu without disturbing the panel.
	if err := view.Keys("Escape"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if state.ai.panel.contextMenu.open || !state.ai.open {
		t.Fatalf("Escape left menu=%v panel=%v", state.ai.panel.contextMenu.open, state.ai.open)
	}
}

func TestAIMenuHeadlessFlowWideAndDrawer(t *testing.T) {
	for _, width := range []int{1400, 800} {
		t.Run(func() string {
			if float32(width) < aiDockAt {
				return "drawer"
			}
			return "docked"
		}(), func(t *testing.T) {
			state, _ := panelState(t, quietModel{"ok"})
			withModel(t, state)
			state.ai.open = true
			state.ai.items = []aiItem{
				{kind: aiUser, text: "question one"},
				{kind: aiAssistant, text: "alpha beta_two\n\n```sh\necho hi\n```"},
				{kind: aiNotice, text: "notice body"},
			}
			var copied string
			state.options.SetClipboardText = func(value string) error { copied = value; return nil }
			view, err := openHeadlessTestView(state, width, 720)
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}

			row := &state.ai.panel.rows[1]
			wordAt := answerCaretCenter(t, row, strings.Index("alpha beta_two", "beta_two")+3)
			if err := view.Driver().ClickCount(wordAt.X, wordAt.Y, 2); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			if got, ok := state.ai.panel.aiTextSelectionText(); !ok || got != "beta_two" {
				t.Fatalf("headless double-click selected %q, %v; want %q", got, ok, "beta_two")
			}

			// Right-click keeps the selection and offers Copy.
			if err := view.RightClick(wordAt.X, wordAt.Y); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			if !state.ai.panel.contextMenu.open {
				t.Fatal("headless right-click did not open menu")
			}
			menuCopy := findMenuRowInSnapshot(t, view.Snapshot().Hits, state.messages.MenuCopy)
			if err := view.Click(menuCopy.Bounds.X+menuCopy.Bounds.Width/2, menuCopy.Bounds.Y+menuCopy.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			if copied != "beta_two" {
				t.Fatalf("headless menu Copy wrote %q, want %q", copied, "beta_two")
			}

			// Right-click inside the fenced code block copies the code block.
			code := state.ai.panel.rows[1].md.Codes()[0]
			codeAt := gfx.Point{
				X: state.ai.panel.rows[1].textRect.X + code.Bounds.X + 12,
				Y: state.ai.panel.rows[1].textRect.Y + code.Bounds.Y + 12,
			}
			if err := view.RightClick(codeAt.X, codeAt.Y); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			menuCode := findMenuRowInSnapshot(t, view.Snapshot().Hits, state.messages.AIPanel.CopyCode)
			if err := view.Click(menuCode.Bounds.X+menuCode.Bounds.Width/2, menuCode.Bounds.Y+menuCode.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			if copied != "echo hi" {
				t.Fatalf("headless menu CopyCode wrote %q, want %q", copied, "echo hi")
			}

			// Right-click Select All selects only the clicked message and focuses its text for Ctrl+C.
			noticeHit := findHitInSnapshot(t, view.Snapshot().Hits, hitAIAnswerText.At(2))
			nx, ny := noticeHit.Bounds.X+noticeHit.Bounds.Width/2, noticeHit.Bounds.Y+noticeHit.Bounds.Height/2
			if err := view.RightClick(nx, ny); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			menuAll := findMenuRowInSnapshot(t, view.Snapshot().Hits, state.messages.MenuSelectAll)
			if err := view.Click(menuAll.Bounds.X+menuAll.Bounds.Width/2, menuAll.Bounds.Y+menuAll.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			if got, ok := state.ai.panel.aiTextSelectionText(); !ok || got != "notice body" {
				t.Fatalf("headless menu SelectAll selected %q, %v", got, ok)
			}

			// Right-click Follow up on selection quotes it and lets the user type immediately.
			if err := view.RightClick(nx, ny); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			menuFollow := findMenuRowInSnapshot(t, view.Snapshot().Hits, state.messages.AIPanel.MenuFollowUpSelection)
			if err := view.Click(menuFollow.Bounds.X+menuFollow.Bounds.Width/2, menuFollow.Bounds.Y+menuFollow.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			if err := view.Type("why?"); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			if got := state.ai.input.String(); got != "> notice body\n\nwhy?" {
				t.Fatalf("headless follow-up typing = %q, want %q", got, "> notice body\n\nwhy?")
			}
		})
	}
}

func openHeadlessTestView(state *State, width, height int) (*headless.Session, error) {
	return headless.Open(state.WindowConfig(app.Config{
		Width: width, Height: height, Tree: Tree(),
		Paint: state.Paint, OnResize: state.Resize, OnPointer: state.Pointer,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll,
		TextInput: state.AIPanelInput,
	}), nil, headless.Options{})
}

func findMenuRowInSnapshot(t *testing.T, hits []gfx.Hit, label string) gfx.Hit {
	t.Helper()
	for _, hit := range hits {
		if hit.Role == gfx.RoleMenuItem && hit.Label == label {
			return hit
		}
	}
	t.Fatalf("menu item %q not found in hits", label)
	return gfx.Hit{}
}

func findHitInSnapshot(t *testing.T, hits []gfx.Hit, id gfx.ID) gfx.Hit {
	t.Helper()
	for _, hit := range hits {
		if hit.ID == id {
			return hit
		}
	}
	t.Fatalf("hit %v not found in hits", id)
	return gfx.Hit{}
}
