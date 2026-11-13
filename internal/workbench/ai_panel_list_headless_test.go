package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/ai/risk"
	"motty/internal/ai/store"
)

func TestHeadlessApprovalActionsStayVisibleWithLongDetails(t *testing.T) {
	state := agentPanel(t)
	state.ai.panelWidth = aiPanelMin
	regular := approvalFixture(state, risk.Medium, false)
	critical := approvalFixture(state, risk.Critical, true)
	targets := strings.Repeat("web-production.example.com; ", 24) + "last-host"
	regular.request.SessionLabel = targets
	critical.request.SessionLabel = targets

	var nativeInput app.TextInput
	view, err := headless.Open(state.WindowConfig(app.Config{
		Width: MinimumWindowWidth, Height: MinimumWindowHeight, Tree: Tree(),
		Paint: state.Paint, OnResize: state.Resize, OnPointer: state.Pointer,
		OnKey: state.Key, OnScroll: state.Scroll,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if input := state.AIPanelInput(context); input != nil {
				return input
			}
			editor := state.EditorFor(context.ID)
			if editor == nil {
				return nil
			}
			nativeInput = app.TextInput{Editor: editor, NavigationKeys: context.NavigationKeys}
			return &nativeInput
		},
	}), nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	visible := func(id gfx.ID) gfx.Hit {
		t.Helper()
		panel := state.ai.panel.rect
		for _, hit := range view.Snapshot().Hits {
			if hit.ID == id {
				box := hit.Bounds
				if box.Width <= 0 || box.Height <= 0 || box.X < panel.X ||
					box.X+box.Width > panel.X+panel.Width || box.Y < panel.Y || box.Y+box.Height > panel.Y+panel.Height {
					t.Fatalf("%q is outside the assistant panel: bounds=%+v panel=%+v", hit.Label, box, panel)
				}
				return hit
			}
		}
		t.Fatalf("hit %v is not painted", id)
		return gfx.Hit{}
	}
	click := func(hit gfx.Hit) {
		t.Helper()
		box := hit.Bounds
		if err := view.Click(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
	}
	visible(hitAIDecline)
	approve := visible(hitAIApprove)
	if approve.Disabled {
		t.Fatal("manual approval in the low-risk scope is disabled")
	}

	details := state.ai.panel.approvalLay.bounds
	if err := view.Scroll(gfx.ScrollEvent{X: details.X + 8, Y: details.Y + 8, DeltaY: 10000, Precise: true}); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	scroll := state.ai.panel.approvalLay.scroll
	if scroll.Max() <= 0 || !scroll.AtEnd() || !view.HasText("last-host") {
		t.Fatalf("scrolling the approval details did not reveal the last target: offset=%v max=%v", scroll.Offset, scroll.Max())
	}
	if after := visible(hitAIApprove); after.Bounds != approve.Bounds {
		t.Fatalf("scrolling moved the Approve button: %+v -> %+v", approve.Bounds, after.Bounds)
	}
	click(approve)
	select {
	case answer := <-regular.reply:
		if !answer.approved {
			t.Fatalf("regular request was declined: %+v", answer)
		}
	default:
		t.Fatal("clicking Approve did not answer the regular request")
	}
	if state.ai.panel.approvalLay.scroll.Offset != 0 {
		t.Fatal("the next approval inherited the previous scroll position")
	}
	if !visible(hitAIApprove).Disabled {
		t.Fatal("critical approval is enabled without the confirmation word")
	}
	click(visible(hitAIConfirm))
	if err := view.Type(critical.request.Confirm); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	click(visible(hitAIApprove))
	select {
	case answer := <-critical.reply:
		if !answer.approved || answer.text != critical.request.Confirm {
			t.Fatalf("critical approval returned %+v", answer)
		}
	default:
		t.Fatal("clicking Approve with the confirmation word did not answer the critical request")
	}
}

// TestHeadlessConversationFollowsNewRowsAndLeavesAScrolledReaderAlone drives
// the page List through the runtime: a conversation follows rows that arrive
// while the reader is at its end, a wheel up releases that follow, and rows that
// arrive after it do not move the reader.
func TestHeadlessConversationFollowsNewRowsAndLeavesAScrolledReaderAlone(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	for index := 0; index < 30; index++ {
		state.ai.items = append(state.ai.items, aiItem{kind: aiAssistant, text: "answer number " + strings.Repeat("word ", 6)})
	}
	state.ai.open = true
	view := openAIPanelView(t, state)
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	scroll := aiListScroll(t, state)
	if !aiListFollows(state) || scroll.Offset != scroll.Max() || scroll.Max() <= 0 {
		t.Fatalf("the opened conversation is not at its end: following=%v offset=%v max=%v", aiListFollows(state), scroll.Offset, scroll.Max())
	}

	state.ai.items = append(state.ai.items, aiItem{kind: aiAssistant, text: "the newest message"})
	if err := view.Resize(1400, 900); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !view.HasText("the newest message") || !aiListFollows(state) {
		t.Fatalf("a following conversation did not show the new row: following=%v", aiListFollows(state))
	}

	list := state.ai.panel.list
	at := gfx.Point{X: list.X + list.Width/2, Y: list.Y + list.Height/2}
	if err := view.Scroll(gfx.ScrollEvent{X: at.X, Y: at.Y, DeltaY: -120, Precise: true}); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	left := aiListScroll(t, state).Offset
	if left >= aiListScroll(t, state).Max() || aiListFollows(state) {
		t.Fatalf("wheeling up did not leave the end: offset=%v max=%v following=%v", left, aiListScroll(t, state).Max(), aiListFollows(state))
	}

	state.ai.items = append(state.ai.items, aiItem{kind: aiAssistant, text: "a later message"})
	if err := view.Resize(1400, 900); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if got := aiListScroll(t, state).Offset; got != left {
		t.Fatalf("new text moved a reader who scrolled up: %v -> %v", left, got)
	}
	if view.HasText("a later message") {
		t.Fatal("a reader who scrolled up was jumped to the new row")
	}

	if err := view.Scroll(gfx.ScrollEvent{X: at.X, Y: at.Y, DeltaY: 10000, Precise: true}); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !aiListFollows(state) {
		t.Fatal("wheeling back to the end did not take the live edge again")
	}
	state.ai.items = append(state.ai.items, aiItem{kind: aiAssistant, text: "another newest message"})
	if err := view.Resize(1400, 900); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !aiListFollows(state) || !view.HasText("another newest message") {
		t.Fatal("the conversation stopped following after it was scrolled back to its end")
	}
}

// TestHeadlessHistoryRowsOpenRenameAndDelete drives the saved-conversation
// List's in-row actions through the runtime: a click on a row opens it, one on
// its pencil opens the rename dialog, and one on its bin deletes it.
func TestHeadlessHistoryRowsOpenRenameAndDelete(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	first, _ := db.CreateConversation("First chat", "x")
	_, _ = db.AppendMessage(first.ID, store.StoredMessage{Role: "user", Text: "hello there"})
	_, _ = db.AppendMessage(first.ID, store.StoredMessage{Role: "assistant", Text: "hi, how can I help"})
	_, _ = db.CreateConversation("Second chat", "x")
	state.ai.open = true
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

	click(hitAIHistory)
	if !state.ai.panel.history || !view.HasText("First chat") || !view.HasText("Second chat") {
		t.Fatal("the history List did not show the saved conversations")
	}
	// Newest first: index 1 is "First chat".
	click(hitAIHistoryOpen.At(1))
	if state.ai.panel.history || !view.HasText("hello there") || !view.HasText("hi, how can I help") {
		t.Fatal("clicking a history row did not open its conversation")
	}

	click(hitAIHistory)
	click(hitAIHistoryRename.At(1))
	if state.modal != modalAIRename || state.ai.rename.id != first.ID {
		t.Fatalf("the rename button opened modal %v for conversation %v", state.modal, state.ai.rename.id)
	}
	state.modal = modalNone

	click(hitAIHistoryDelete.At(1))
	list, err := db.Conversations("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Title != "Second chat" {
		t.Fatalf("after deleting the first chat the store holds %+v", list)
	}
}

// TestHeadlessWrappedRowHeightFollowsTheWidth drives the List's measurement of
// a long wrapped user message through the runtime: narrowing the panel wraps it
// into more lines, and the List's row height follows the declaration.
func TestHeadlessWrappedRowHeightFollowsTheWidth(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.ai.items = []aiItem{{kind: aiUser, text: strings.Repeat("a long sentence that wraps in the assistant panel ", 6)}}
	state.ai.open = true
	view := openAIPanelView(t, state)
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	wide := state.ai.panel.messageList.RowHeightAt(0)
	if wide <= aiBodyLine {
		t.Fatalf("the wrapped message measured %.1f, no taller than one line", wide)
	}
	state.ai.panelWidth = aiPanelMin
	if err := view.Resize(1400, 900); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	narrow := state.ai.panel.messageList.RowHeightAt(0)
	if narrow <= wide {
		t.Fatalf("narrowing the panel did not wrap the message further: %.1f -> %.1f", wide, narrow)
	}
}
