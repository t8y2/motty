package workbench

import (
	"strings"
	"testing"

	"motty/internal/ai/llm"
	"motty/internal/ai/risk"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

func selectAnswerText(t *testing.T, state *State, start, end int) *aiPanel {
	t.Helper()
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiAssistant, text: "alpha beta gamma"}}
	paintAt(state, panelWide)
	panel := &state.ai.panel
	panel.textSelect = aiTextSelection{row: 0, sel: components.MarkdownSelection{Anchor: start, Focus: end}}
	paintAt(state, panelWide)
	if text, ok := panel.aiTextSelectionText(); !ok || text == "" {
		t.Fatalf("the answer selection is not readable: %q %v", text, ok)
	}
	return panel
}

func hitIndex(hits []ui.Hit, id ui.ID) int {
	for index, hit := range hits {
		if hit.ID == id {
			return index
		}
	}
	return -1
}

func TestAnswerBodyIsAFocusableTextRegionBeforeLinksAndButtons(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiAssistant, text: "See [a link](https://example.com) and\n\n```\nls -la\n```\n"}}
	canvas, _ := paintAt(state, panelWide)
	textID, linkID, copyID := hitAIAnswerText.At(0), hitAILinks.At(0), hitAICopies.At(0)
	textHit, ok := canvasHit(canvas.Hits(), textID)
	if !ok {
		t.Fatalf("the answer body has no hit: %+v", canvas.Hits())
	}
	if textHit.Role != ui.RoleGroup || !textHit.Focusable || !textHit.SkipTab {
		t.Fatalf("answer body hit = %+v, want a focusable group outside Tab order", textHit)
	}
	if textHit.Label != state.messages.AIPanel.AnswerText {
		t.Fatalf("answer body label = %q", textHit.Label)
	}
	if textHit.Bounds != state.ai.panel.rows[0].textRect || textHit.Bounds.Height != state.ai.panel.rows[0].md.Height() {
		t.Errorf("answer body covers %+v, painted text is %+v", textHit.Bounds, state.ai.panel.rows[0].textRect)
	}
	textAt, linkAt, copyAt := hitIndex(canvas.Hits(), textID), hitIndex(canvas.Hits(), linkID), hitIndex(canvas.Hits(), copyID)
	if textAt < 0 || linkAt < 0 || copyAt < 0 {
		t.Fatalf("text, link and copy hits at %d, %d, %d: %+v", textAt, linkAt, copyAt, canvas.Hits())
	}
	if !(textAt < linkAt && linkAt < copyAt) {
		t.Fatalf("hits are declared text=%d link=%d copy=%d; links and buttons must win", textAt, linkAt, copyAt)
	}
}

func answerPointer(state *State, kind ui.PointerEventKind, at ui.Point, target ui.ID) ui.EventResult {
	return answerPointerCaptured(state, kind, at, target, kind != ui.PointerDown)
}

func answerPointerCaptured(state *State, kind ui.PointerEventKind, at ui.Point, target ui.ID, captured bool) ui.EventResult {
	return state.Pointer(ui.PointerEvent{
		Kind: kind, Button: ui.PointerButtonPrimary, X: at.X, Y: at.Y, Target: target, Captured: captured, Viewport: panelWide,
	})
}

func hitCenter(t *testing.T, canvas *ui.Canvas, id ui.ID) ui.Point {
	t.Helper()
	hit, ok := canvasHit(canvas.Hits(), id)
	if !ok {
		t.Fatalf("no hit %v in %+v", id, canvas.Hits())
	}
	return ui.Point{X: hit.Bounds.X + hit.Bounds.Width/2, Y: hit.Bounds.Y + hit.Bounds.Height/2}
}

func dragSelectWholeText(t *testing.T, state *State, canvas *ui.Canvas, id ui.ID) {
	t.Helper()
	hit, ok := canvasHit(canvas.Hits(), id)
	if !ok {
		t.Fatalf("no selectable text hit %v in %+v", id, canvas.Hits())
	}
	start := ui.Point{X: hit.Bounds.X + 1, Y: hit.Bounds.Y + 1}
	end := ui.Point{X: hit.Bounds.X + hit.Bounds.Width, Y: hit.Bounds.Y + hit.Bounds.Height - 1}
	if result := answerPointer(state, ui.PointerDown, start, id); !result.Handled || result.Focus != id {
		t.Fatalf("pressing selectable text = %+v, want focus %v", result, id)
	}
	answerPointer(state, ui.PointerMove, end, id)
	answerPointer(state, ui.PointerUp, end, id)
	if !state.ai.panel.hasAITextSelection() {
		t.Fatalf("dragging %v left no selection: %+v", id, state.ai.panel.textSelect)
	}
}

func copyChord(target ui.ID) ui.KeyEvent {
	return ui.KeyEvent{Text: "c", Modifiers: defaultShortcutModifier(), Target: target}
}

func TestCopyChordCopiesTheAnswerSelection(t *testing.T) {
	state, _ := panelState(t, nil)
	_ = selectAnswerText(t, state, 0, 5)
	var copied string
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	result := state.Shortcut(copyChord(hitAIAnswerText.At(0)))
	if !result.Handled || copied != "alpha" {
		t.Fatalf("copy chord copied %q, handled=%v; want the selected text", copied, result.Handled)
	}
}

func TestNamedCopyBindingCopiesTheAnswerSelection(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	selectAnswerText(t, state, 0, 5)
	state.shortcuts.bind(shortcutCopy, shortcutChord{Named: ui.KeyF8})
	var copied string
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	result := state.Key(ui.KeyEvent{Key: ui.KeyF8, Target: hitAIAnswerText.At(0)})
	if !result.Handled || copied != "alpha" {
		t.Fatalf("named copy binding copied %q, result=%+v", copied, result)
	}
}

func TestUserMessageDragSelectsAndCopiesWithoutVisualWraps(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	text := strings.Repeat("copy this user question ", 12)
	state.ai.items = []aiItem{{kind: aiUser, text: text}}
	canvas, _ := paintAt(state, panelWide)
	id := hitAIAnswerText.At(0)
	dragSelectWholeText(t, state, canvas, id)
	if got, ok := state.ai.panel.aiTextSelectionText(); !ok || got != text {
		t.Fatalf("selected user text %q, %v; want %q", got, ok, text)
	}
	var copied string
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	if result := state.Shortcut(copyChord(id)); !result.Handled || copied != text {
		t.Fatalf("copying user text = %q, %+v", copied, result)
	}
	canvas, _ = paintAt(state, panelWide)
	if rects := markdownSelectionRects(canvas, withAlpha(state.pal.text, 0.25)); len(rects) == 0 {
		t.Fatalf("the user selection was not highlighted: %+v", state.ai.panel.textSelect)
	}
}

func TestToolCommandAndOutputDragSelectAndCopy(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	command := "cd /dev && pwd"
	output := "/dev\n[exit status 0]"
	state.ai.items = []aiItem{{
		kind: aiTool, call: llm.ToolCall{ID: "1", Name: "terminal_exec", Arguments: `{"command":"` + command + `"}`},
		status: aiToolDone, result: output,
	}}
	paintAt(state, panelWide)
	state.Click(hitAIRows.At(0))
	canvas, _ := paintAt(state, panelWide)

	commandID := hitAIAnswerText.At(0)
	dragSelectWholeText(t, state, canvas, commandID)
	if got, ok := state.ai.panel.aiTextSelectionText(); !ok || got != command {
		t.Fatalf("selected command %q, %v; want %q", got, ok, command)
	}

	outputID := hitAIToolOutputText.At(0)
	dragSelectWholeText(t, state, canvas, outputID)
	if got, ok := state.ai.panel.aiTextSelectionText(); !ok || got != output {
		t.Fatalf("selected output %q, %v; want %q", got, ok, output)
	}
	var copied string
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	if result := state.Shortcut(copyChord(outputID)); !result.Handled || copied != output {
		t.Fatalf("copying tool output = %q, %+v", copied, result)
	}

	state.Click(hitAIRows.At(0))
	canvas, _ = paintAt(state, panelWide)
	if state.ai.panel.hasAITextSelection() {
		t.Fatal("collapsing the output kept its selection")
	}
	if _, ok := canvasHit(canvas.Hits(), outputID); ok {
		t.Fatal("collapsed output still has a selectable text hit")
	}
}

func TestToolOutputSelectionCopiesOnlyVisibleLines(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	state.ai.items = []aiItem{{
		kind: aiTool, call: llm.ToolCall{ID: "1", Name: "terminal_read"}, status: aiToolDone,
		result: strings.Repeat("visible\n", aiMaxOutput) + "hidden",
	}}
	paintAt(state, panelWide)
	state.Click(hitAIRows.At(0))
	canvas, _ := paintAt(state, panelWide)
	dragSelectWholeText(t, state, canvas, hitAIToolOutputText.At(0))
	want := strings.TrimSuffix(strings.Repeat("visible\n", aiMaxOutput), "\n")
	if got, ok := state.ai.panel.aiTextSelectionText(); !ok || got != want {
		t.Fatalf("selected truncated output %q, %v; want only %q", got, ok, want)
	}
}

func TestCopyChordWithoutAnAnswerSelectionLeavesTheClipboardAlone(t *testing.T) {
	state, _ := panelState(t, nil)
	panel := selectAnswerText(t, state, 0, 5)
	panel.clearAITextSelection()
	copied := "unchanged"
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	result := state.Shortcut(copyChord(hitAIAnswerText.At(0)))
	if !result.Handled || copied != "unchanged" {
		t.Fatalf("copy with no selection wrote %q, handled=%v", copied, result.Handled)
	}
}

func TestCopyChordInTheComposerDoesNotCopyTheAnswerSelection(t *testing.T) {
	state, _ := panelState(t, nil)
	panel := selectAnswerText(t, state, 0, 5)
	copied := "unchanged"
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	state.Shortcut(copyChord(hitAIInput))
	if copied != "unchanged" {
		t.Fatalf("the composer copy wrote the answer %q", copied)
	}
	if !panel.hasAITextSelection() {
		t.Fatal("a copy in the composer cleared the answer selection")
	}
}

func TestCopyChordInTheTerminalDoesNotCopyTheAnswerSelection(t *testing.T) {
	state, _ := panelState(t, nil)
	panel := selectAnswerText(t, state, 0, 5)
	copied := "unchanged"
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	state.Shortcut(copyChord(state.activeTerminalFocusHit()))
	if copied != "unchanged" {
		t.Fatalf("the terminal copy wrote the answer %q", copied)
	}
	if !panel.hasAITextSelection() {
		t.Fatal("a copy in the terminal cleared the answer selection")
	}
}

func TestEscapeClearsTheAnswerSelection(t *testing.T) {
	state, _ := panelState(t, nil)
	panel := selectAnswerText(t, state, 0, 5)
	if result := state.Key(ui.KeyEvent{Key: ui.KeyEscape, Target: terminalHit(state, 0)}); result.Handled && !panel.hasAITextSelection() {
		t.Fatal("Escape in the terminal cleared the answer selection")
	}
	result := state.Key(ui.KeyEvent{Key: ui.KeyEscape, Target: hitAIAnswerText.At(0)})
	if !result.Handled || !result.Repaint {
		t.Fatalf("Escape on the answer = %+v", result)
	}
	if panel.hasAITextSelection() {
		t.Fatalf("Escape kept the selection: %+v", panel.textSelect)
	}
}

func TestEscapeDismissesTheApprovalBeforeClearingTheSelection(t *testing.T) {
	state, _ := panelState(t, nil)
	panel := selectAnswerText(t, state, 0, 5)
	approval := approvalFixture(state, risk.Medium, false)
	paintAt(state, panelWide)
	result := state.Key(ui.KeyEvent{Key: ui.KeyEscape, Target: hitAIAnswerText.At(0)})
	if !result.Handled {
		t.Fatal("Escape was not taken")
	}
	select {
	case answer := <-approval.reply:
		if answer.approved {
			t.Fatal("Escape approved the command")
		}
	default:
		t.Fatal("Escape did not answer the waiting approval")
	}
	if !panel.hasAITextSelection() {
		t.Fatalf("answering the approval cleared the selection: %+v", panel.textSelect)
	}
	paintAt(state, panelWide)
	state.Key(ui.KeyEvent{Key: ui.KeyEscape, Target: hitAIAnswerText.At(0)})
	if panel.hasAITextSelection() {
		t.Fatalf("the second Escape kept the selection: %+v", panel.textSelect)
	}
}

func firstLinkHit(t *testing.T, canvas *ui.Canvas) ui.ID {
	t.Helper()
	for _, hit := range canvas.Hits() {
		if hit.ID.Scope() == hitAILinks {
			return hit.ID
		}
	}
	t.Fatalf("no link hit in %+v", canvas.Hits())
	return ui.ID{}
}

func TestAnswerLinkOpensAndClearsTheSelection(t *testing.T) {
	state, _ := panelState(t, nil)
	var opened []string
	state.options.OpenURL = func(url string) error { opened = append(opened, url); return nil }
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiAssistant, text: "See [the first](https://one.example/a) and more text."}}
	paintAt(state, panelWide)
	panel := &state.ai.panel
	panel.textSelect = aiTextSelection{row: 0, sel: components.MarkdownSelection{Anchor: 0, Focus: 4}}
	canvas, _ := paintAt(state, panelWide)
	link := firstLinkHit(t, canvas)
	// The real pointer path first, like the runtime: pressing a link is a press
	// outside the answer text, so the selection goes.
	answerPointer(state, ui.PointerDown, hitCenter(t, canvas, link), link)
	if panel.hasAITextSelection() {
		t.Fatalf("pressing a link kept the selection: %+v", panel.textSelect)
	}
	state.Click(link)
	if len(opened) != 1 || opened[0] != "https://one.example/a" {
		t.Fatalf("opened %v", opened)
	}
}

func TestCodeCopyButtonCopiesItsBlockAndClearsTheSelection(t *testing.T) {
	state, _ := panelState(t, nil)
	var copied string
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiAssistant, text: "Run:\n```\nls -la\n```"}}
	paintAt(state, panelWide)
	panel := &state.ai.panel
	panel.textSelect = aiTextSelection{row: 0, sel: components.MarkdownSelection{Anchor: 0, Focus: 3}}
	canvas, _ := paintAt(state, panelWide)
	button := hitAICopies.At(0)
	answerPointer(state, ui.PointerDown, hitCenter(t, canvas, button), button)
	if panel.hasAITextSelection() {
		t.Fatalf("pressing the copy button kept the selection: %+v", panel.textSelect)
	}
	state.Click(button)
	if copied != "ls -la" {
		t.Fatalf("copy button copied %q", copied)
	}
}

func TestAnswerTextDragAcrossALinkStillSelects(t *testing.T) {
	state, _ := panelState(t, nil)
	var opened []string
	state.options.OpenURL = func(url string) error { opened = append(opened, url); return nil }
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiAssistant, text: "alpha [link](https://example.com) omega"}}
	paintAt(state, panelWide)
	panel := &state.ai.panel
	row := &panel.rows[0]
	id := hitAIAnswerText.At(0)
	start := ui.Point{X: row.textRect.X + 1, Y: row.textRect.Y + 1}
	end := ui.Point{X: row.textRect.X + row.textRect.Width - 1, Y: row.textRect.Y + 1}
	answerPointer(state, ui.PointerDown, start, id)
	answerPointer(state, ui.PointerMove, end, id)
	answerPointer(state, ui.PointerUp, end, id)
	if got, ok := panel.aiTextSelectionText(); !ok || got != "alpha link omega" {
		t.Fatalf("drag across a link selected %q, %v", got, ok)
	}
	if len(opened) != 0 {
		t.Fatalf("dragging across a link opened %v", opened)
	}
}

func TestPressesOutsideTextAndCollapsibleHeadersCreateNoSelection(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	state.ai.items = []aiItem{
		{kind: aiUser, text: "a question"},
		{kind: aiReasoning, text: "thinking about it"},
		{kind: aiTool, call: llm.ToolCall{ID: "1", Name: "terminal_read"}, status: aiToolDone},
		{kind: aiAssistant, text: "the answer"},
	}
	paintAt(state, panelWide)
	panel := &state.ai.panel
	for index, target := range []ui.ID{{}, hitAIRows.At(1), hitAIRows.At(2)} {
		row := &panel.rows[index]
		at := ui.Point{X: panel.list.X + 20, Y: panel.list.Y + row.top + row.height/2}
		answerPointer(state, ui.PointerDown, at, target)
		if panel.hasAITextSelection() {
			t.Fatalf("press on row %d left a selection: %+v", index, panel.textSelect)
		}
		answerPointer(state, ui.PointerUp, at, target)
	}
	for index := 1; index <= 2; index++ {
		if panel.rows[index].expanded {
			t.Fatalf("a press expanded row %d", index)
		}
	}
	state.Click(hitAIRows.At(1))
	if !panel.rows[1].expanded {
		t.Fatal("the reasoning row's click no longer expands it")
	}
}

func TestAnswerTextDragUpwardKeepsTheAnchorAcrossPaints(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiAssistant, text: "alpha beta\ngamma delta"}}
	paintAt(state, panelWide)
	panel := &state.ai.panel
	row := &panel.rows[0]
	id := hitAIAnswerText.At(0)
	// Start at the beginning of the second line and drag up.
	second := ui.Point{X: row.textRect.X + 1, Y: row.textRect.Y + row.md.Height() - 1}
	answerPointer(state, ui.PointerDown, second, id)
	anchor := panel.textSelect.sel.Anchor
	if anchor != len("alpha beta\n") {
		t.Fatalf("the drag started at %d, want the second line", anchor)
	}
	// Two moves with a repaint between each, as the application paints while
	// the pointer is still down.
	middle := ui.Point{X: row.textRect.X + row.textRect.Width - 20, Y: row.textRect.Y + 1}
	answerPointer(state, ui.PointerMove, middle, id)
	paintAt(state, panelWide)
	if got := panel.textSelect.sel.Anchor; got != anchor {
		t.Fatalf("the first repaint moved the anchor from %d to %d", anchor, got)
	}
	first := ui.Point{X: row.textRect.X + 1, Y: row.textRect.Y + 1}
	answerPointer(state, ui.PointerMove, first, id)
	paintAt(state, panelWide)
	if got := panel.textSelect.sel.Anchor; got != anchor {
		t.Fatalf("the second repaint moved the anchor from %d to %d", anchor, got)
	}
	answerPointer(state, ui.PointerUp, first, id)
	if got, ok := panel.aiTextSelectionText(); !ok || got != "alpha beta\n" {
		t.Fatalf("the upward drag selected %q, %v; want the whole first line and its break", got, ok)
	}
}

func TestAnswerTextDragIgnoresAnUncapturedMove(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiAssistant, text: "alpha beta gamma"}}
	paintAt(state, panelWide)
	panel := &state.ai.panel
	row := &panel.rows[0]
	id := hitAIAnswerText.At(0)
	start := ui.Point{X: row.textRect.X + 1, Y: row.textRect.Y + 1}
	end := ui.Point{X: row.textRect.X + row.textRect.Width - 1, Y: row.textRect.Y + 1}
	answerPointer(state, ui.PointerDown, start, id)
	answerPointerCaptured(state, ui.PointerMove, end, id, true)
	atEnd := panel.textSelect.sel.Focus
	answerPointerCaptured(state, ui.PointerMove, start, id, false)
	if got := panel.textSelect.sel.Focus; got != atEnd {
		t.Fatalf("an uncaptured move changed the focus from %d to %d", atEnd, got)
	}
}

func TestAnswerTextDragEndsWhenThePointerLeaves(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiAssistant, text: "alpha beta gamma"}}
	paintAt(state, panelWide)
	panel := &state.ai.panel
	row := &panel.rows[0]
	id := hitAIAnswerText.At(0)
	start := ui.Point{X: row.textRect.X + 1, Y: row.textRect.Y + 1}
	end := ui.Point{X: row.textRect.X + row.textRect.Width - 1, Y: row.textRect.Y + 1}
	answerPointer(state, ui.PointerDown, start, id)
	answerPointerCaptured(state, ui.PointerMove, end, id, true)
	answerPointerCaptured(state, ui.PointerExit, end, id, true)
	if panel.textSelect.dragging {
		t.Fatal("the drag is still active after the pointer left")
	}
	if !panel.hasAITextSelection() {
		t.Fatal("leaving the window dropped the selection")
	}
	atExit := panel.textSelect.sel.Focus
	answerPointerCaptured(state, ui.PointerMove, start, id, true)
	if got := panel.textSelect.sel.Focus; got != atExit {
		t.Fatalf("a move after leaving extended the selection from %d to %d", atExit, got)
	}
}

func TestAnswerSelectionSurvivesARewrap(t *testing.T) {
	state, _ := panelState(t, nil)
	panel := selectAnswerText(t, state, 0, 10)
	// A longer tail rebuilds the row; the existing text is its prefix, so the
	// selection stays.
	state.ai.items[0].text = "alpha beta gamma " + strings.Repeat("a longer tail that wraps. ", 30)
	paintAt(state, panelWide)
	if got, ok := panel.aiTextSelectionText(); !ok || got != "alpha beta" {
		t.Fatalf("before the rewrap: %q, %v", got, ok)
	}
	paintAt(state, ui.Size{Width: 1000, Height: 900})
	if got, ok := panel.aiTextSelectionText(); !ok || got != "alpha beta" {
		t.Fatalf("after the rewrap: %q, %v", got, ok)
	}
}

func TestAnswerSelectionSurvivesAnotherRowStreaming(t *testing.T) {
	state, _ := panelState(t, nil)
	panel := selectAnswerText(t, state, 0, 5)
	state.ai.items = append(state.ai.items, aiItem{kind: aiAssistant, text: "a second answer"})
	paintAt(state, panelWide)
	if got, ok := panel.aiTextSelectionText(); !ok || got != "alpha" {
		t.Fatalf("while another row streamed: %q, %v", got, ok)
	}
}

func TestAnswerTextDragSelectsAcrossVisualLines(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiAssistant, text: "alpha beta\ngamma delta"}}
	paintAt(state, panelWide)
	panel := &state.ai.panel
	row := &panel.rows[0]
	id := hitAIAnswerText.At(0)
	start := ui.Point{X: row.textRect.X + 1, Y: row.textRect.Y + 1}
	end := ui.Point{X: row.textRect.X + 1, Y: row.textRect.Y + row.md.Height() - 1}

	result := answerPointer(state, ui.PointerDown, start, id)
	if !result.Handled || result.Focus != id {
		t.Fatalf("pressing the answer = %+v, want it focused", result)
	}
	if !panel.textSelect.dragging || panel.textSelect.row != 0 || panel.textSelect.sel.Anchor != 0 {
		t.Fatalf("press did not start a selection: %+v", panel.textSelect)
	}
	answerPointer(state, ui.PointerMove, end, id)
	if !panel.textSelect.dragging {
		t.Fatal("the drag ended before the button was released")
	}
	if got := panel.textSelect.sel.Focus; got != len("alpha beta\n") {
		t.Fatalf("dragging to the second line selected to %d, want %d", got, len("alpha beta\n"))
	}
	if got, ok := panel.aiTextSelectionText(); !ok || got != "alpha beta\n" {
		t.Fatalf("selected %q, %v", got, ok)
	}

	canvas, _ := paintAt(state, panelWide)
	// The panel overrides the Markdown default for its palette (see
	// paintAIAssistant), so the highlight is the terminal's recipe.
	selected := withAlpha(state.pal.text, 0.25)
	if rects := markdownSelectionRects(canvas, selected); len(rects) == 0 {
		t.Fatalf("the selection was not highlighted: %+v", canvas.Rectangles())
	}
	answerPointer(state, ui.PointerUp, end, id)
	if panel.textSelect.dragging {
		t.Fatal("the selection is still dragging after the release")
	}
	if !panel.hasAITextSelection() {
		t.Fatal("the release cleared the selection")
	}
}

func TestAnswerTextPressOutsideClearsTheSelectionWithoutSwallowingThePress(t *testing.T) {
	state, _ := panelState(t, nil)
	panel := selectAnswerText(t, state, 0, 5)
	result := answerPointer(state, ui.PointerDown, ui.Point{X: 10, Y: 10}, hitAIInput)
	if !result.Repaint {
		t.Fatal("clearing the selection asked for no repaint")
	}
	if panel.hasAITextSelection() {
		t.Fatalf("a press outside kept the selection: %+v", panel.textSelect)
	}
}

func TestAnswerTextClickWithoutADragLeavesNoSelection(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiAssistant, text: "alpha beta"}}
	paintAt(state, panelWide)
	panel := &state.ai.panel
	row := &panel.rows[0]
	id := hitAIAnswerText.At(0)
	at := ui.Point{X: row.textRect.X + 1, Y: row.textRect.Y + 1}
	answerPointer(state, ui.PointerDown, at, id)
	answerPointer(state, ui.PointerUp, at, id)
	if panel.hasAITextSelection() {
		t.Fatalf("a click left a selection: %+v", panel.textSelect)
	}
}

func TestAnswerTextDragStaysInTheMessageItStartedIn(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	state.ai.items = []aiItem{
		{kind: aiAssistant, text: "first answer"},
		{kind: aiAssistant, text: "second answer"},
	}
	paintAt(state, panelWide)
	panel := &state.ai.panel
	first, second := &panel.rows[0], &panel.rows[1]
	id := hitAIAnswerText.At(0)
	answerPointer(state, ui.PointerDown, ui.Point{X: first.textRect.X + 1, Y: first.textRect.Y + 1}, id)
	answerPointer(state, ui.PointerMove, ui.Point{X: first.textRect.X + first.textRect.Width - 1, Y: second.textRect.Y + 1}, id)
	if panel.textSelect.row != 0 {
		t.Fatalf("the drag moved to row %d", panel.textSelect.row)
	}
	if got, ok := panel.aiTextSelectionText(); !ok || got != "first answer" {
		t.Fatalf("the drag selected %q, %v", got, ok)
	}
}

func markdownSelectionRects(canvas *ui.Canvas, color ui.Color) []ui.Rectangle {
	var found []ui.Rectangle
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Color == color {
			found = append(found, rectangle)
		}
	}
	return found
}

func TestAnswerSelectionClearsWhenTheConversationIsReset(t *testing.T) {
	state, _ := panelState(t, nil)
	panel := selectAnswerText(t, state, 0, 5)
	state.aiPanelReset()
	if panel.hasAITextSelection() || panel.textSelect.row != -1 {
		t.Fatalf("reset kept the selection: %+v", panel.textSelect)
	}

	panel = selectAnswerText(t, state, 0, 5)
	state.aiNewConversation()
	if panel.hasAITextSelection() {
		t.Fatalf("a new conversation kept the selection: %+v", panel.textSelect)
	}

	panel = selectAnswerText(t, state, 0, 5)
	state.setAIPanelOpen(false)
	if panel.hasAITextSelection() {
		t.Fatalf("closing the panel kept the selection: %+v", panel.textSelect)
	}
}

func TestOpeningHistoryClearsTheAnswerSelection(t *testing.T) {
	state, _ := panelState(t, nil)
	panel := selectAnswerText(t, state, 0, 5)
	state.Click(hitAIHistory)
	if panel.hasAITextSelection() {
		t.Fatalf("the history list kept the selection: %+v", panel.textSelect)
	}
}

func TestAnswerSelectionSurvivesAStreamedAppend(t *testing.T) {
	state, _ := panelState(t, nil)
	panel := selectAnswerText(t, state, 0, 5)
	state.ai.items[0].text = "alpha beta gamma and a streamed tail"
	paintAt(state, panelWide)
	if got, ok := panel.aiTextSelectionText(); !ok || got != "alpha" {
		t.Fatalf("the appended answer selected %q, %v", got, ok)
	}
}

// Streaming can change how a source prefix renders, which shifts every offset
// after it. The selection has to go rather than point at other characters.
func TestAnswerSelectionClearsWhenStreamingChangesTheRendering(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiAssistant, text: "a `中"}}
	paintAt(state, panelWide)
	panel := &state.ai.panel
	// With the backtick still unclosed it is literal text, so "中" starts at 3.
	panel.textSelect = aiTextSelection{row: 0, sel: components.MarkdownSelection{Anchor: 3, Focus: 6}}
	paintAt(state, panelWide)
	if got, ok := panel.aiTextSelectionText(); !ok || got != "中" {
		t.Fatalf("the unclosed-backtick answer selected %q, %v", got, ok)
	}
	// The closing backtick renders the run as inline code: "a 中", one byte
	// shorter, so the old offsets no longer name the same characters.
	state.ai.items[0].text = "a `中`"
	paintAt(state, panelWide)
	if got, ok := panel.aiTextSelectionText(); ok {
		t.Fatalf("a rendering change kept the selection as %q", got)
	}
}

func TestAnswerSelectionClearsWhenTheMessageIsReplaced(t *testing.T) {
	state, _ := panelState(t, nil)
	panel := selectAnswerText(t, state, 0, 5)
	state.ai.items[0].text = "something else entirely"
	paintAt(state, panelWide)
	if panel.hasAITextSelection() {
		t.Fatalf("a replaced message kept the selection: %+v", panel.textSelect)
	}
}

func TestAnswerSelectionClearsWhenTheRowGoesAway(t *testing.T) {
	state, _ := panelState(t, nil)
	panel := selectAnswerText(t, state, 0, 5)
	state.ai.items = nil
	paintAt(state, panelWide)
	if panel.hasAITextSelection() {
		t.Fatalf("a removed row kept the selection: %+v", panel.textSelect)
	}
}

func TestAnswerSelectionClampsToTheRenderedText(t *testing.T) {
	state, _ := panelState(t, nil)
	panel := selectAnswerText(t, state, 0, 1000)
	if got, ok := panel.aiTextSelectionText(); !ok || got != "alpha beta gamma" {
		t.Fatalf("an oversized selection copies %q, %v", got, ok)
	}
	if panel.textSelect.sel.Focus != len("alpha beta gamma") {
		t.Fatalf("the selection was not clamped: %+v", panel.textSelect.sel)
	}
}

// A message past the block's line budget must say it was cut; before this the
// last painted row looked like the end of what the user sent.
func TestTruncatedUserMessageEndsWithAnEllipsis(t *testing.T) {
	state, _ := panelState(t, nil)
	state.ai.open = true
	var text strings.Builder
	for range 5000 {
		text.WriteString("line\n")
	}
	state.ai.items = []aiItem{{kind: aiUser, text: text.String()}}
	canvas, _ := paintAt(state, panelWide)
	for _, painted := range canvas.Texts() {
		if strings.HasSuffix(painted.Value, "…") {
			return
		}
	}
	t.Fatal("the truncated user message does not show that it was cut")
}
