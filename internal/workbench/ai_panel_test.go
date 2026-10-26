package workbench

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/agent"
	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"motty/internal/ai/risk"
	"motty/internal/ai/store"
	"motty/internal/sshconn"
)

var (
	panelWide   = ui.Size{Width: 1400, Height: 900}
	panelNarrow = ui.Size{Width: 800, Height: 600}
)

// quietModel answers every request with one line of text.
type quietModel struct{ answer string }

func (model quietModel) Stream(context.Context, llm.Request) (<-chan llm.Event, error) {
	out := make(chan llm.Event, 2)
	out <- llm.Event{Kind: llm.EventText, Text: model.answer}
	out <- llm.Event{Kind: llm.EventDone}
	close(out)
	return out, nil
}

func (quietModel) Models(context.Context) ([]string, error) { return nil, nil }

// panelState is a window with one live session and the assistant pointed at a
// temporary database, painted once at the wide size. It has no model yet.
func panelState(t testing.TB, model llm.Provider) (*State, *fakeShell) {
	t.Helper()
	world, options := newWorld(t)
	options.Invalidate = func() {}
	options.AIStorePath = filepath.Join(t.TempDir(), "ai.db")
	if model != nil {
		options.AIProvider = func(store.Provider) llm.Provider { return model }
	}
	state := newTestState(t, options)
	t.Cleanup(state.Close)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	shell := fake.shell(t)
	shell.say("\x1b[0m")
	state.Wake()
	state.Paint(&ui.Canvas{}, panelWide)
	return state, shell
}

// withModel chooses a default model, as the settings section does.
func withModel(t testing.TB, state *State) {
	t.Helper()
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveProvider(store.Provider{ID: "p", Name: "Work", Kind: store.KindOpenAI, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDefaultModel("p", "gpt-x"); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
}

func paintAt(state *State, size ui.Size) (*ui.Canvas, []string) {
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	var texts []string
	for _, text := range canvas.Texts() {
		texts = append(texts, text.Value)
	}
	return &canvas, texts
}

func panelHit(t testing.TB, canvas *ui.Canvas, id ui.ID) ui.Hit {
	t.Helper()
	hit, ok := hitByID(canvas, id)
	if !ok {
		t.Fatalf("hit %d is not painted", id)
	}
	return hit
}

func terminalColumns(state *State) int {
	cols, _ := state.sessions[0].term.screen.Size()
	return cols
}

func hasText(texts []string, part string) bool {
	for _, text := range texts {
		if strings.Contains(text, part) {
			return true
		}
	}
	return false
}

func TestPanelOpensWithTheBarButtonAndTheTerminalsFollowItsWidth(t *testing.T) {
	state, _ := panelState(t, nil)
	before := terminalColumns(state)
	canvas, _ := paintAt(state, panelWide)
	if _, ok := hitByID(canvas, hitAIInput); ok {
		t.Fatal("the panel is up before it was asked for")
	}
	button := panelHit(t, canvas, hitAIToggle)
	if button.Label != state.messages.AIPanel.ToggleTip || button.Role != ui.RoleButton {
		t.Fatalf("bar button = %+v", button)
	}
	state.Click(hitAIToggle)
	canvas, texts := paintAt(state, panelWide)
	for _, id := range []ui.ID{hitAIInput, hitAIClose, hitAINew, hitAIHistory, hitAIMode, hitAISend} {
		panelHit(t, canvas, id)
	}
	if !hasText(texts, state.messages.AIPanel.Title) {
		t.Fatalf("no title in %q", texts)
	}
	after := terminalColumns(state)
	if after >= before || float32(before-after)*state.cellWidth < aiPanelWidth-state.cellWidth*2 {
		t.Fatalf("columns %d -> %d, want the panel's width taken", before, after)
	}
	terminal := panelHit(t, canvas, terminalHit(state, 0))
	if terminal.Bounds.X+terminal.Bounds.Width > panelHit(t, canvas, hitAIInput).Bounds.X {
		t.Fatalf("the terminal %+v runs under the panel", terminal.Bounds)
	}
	state.Click(hitAIClose)
	canvas, _ = paintAt(state, panelWide)
	if _, ok := hitByID(canvas, hitAIInput); ok {
		t.Fatal("the panel stayed after Close")
	}
	if terminalColumns(state) != before {
		t.Fatalf("columns after closing = %d, want %d", terminalColumns(state), before)
	}
}

func TestShortcutAndPaletteToggleThePanel(t *testing.T) {
	state, _ := panelState(t, nil)
	chord := state.shortcuts.chords[shortcutAssistant]
	if !chord.valid() || state.shortcuts.actionFor(chord) != shortcutAssistant {
		t.Fatalf("assistant chord %+v", chord)
	}
	event := ui.KeyEvent{Modifiers: chord.Modifiers, Text: string(rune(chord.Key))}
	result := state.Shortcut(event)
	if !result.Handled || !state.ai.open || result.Focus != hitAIInput {
		t.Fatalf("shortcut opened = %+v, open=%v", result, state.ai.open)
	}
	paintAt(state, panelWide)
	result = state.Shortcut(event)
	if !result.Handled || state.ai.open || result.Focus != state.activeTerminalFocusHit() {
		t.Fatalf("shortcut closed = %+v, open=%v", result, state.ai.open)
	}

	state.openCommandPalette()
	found := -1
	palette := &state.commandPalette
	for index := uint8(0); index < palette.entryCount; index++ {
		if palette.entries[index].label == state.messages.AIPanel.Toggle {
			found = int(index)
		}
	}
	if found < 0 {
		t.Fatal("the command palette has no assistant entry")
	}
	state.executeCommandPalette(uint8(found))
	if !state.ai.open || state.modal != modalNone {
		t.Fatalf("palette entry: open=%v modal=%v", state.ai.open, state.modal)
	}
}

func TestShortcutSettingsPageListsTheAssistantAction(t *testing.T) {
	state, _ := panelState(t, nil)
	showShortcutSettings(state)
	paintAt(state, panelWide)
	state.scrollSettings(ui.ScrollEvent{DeltaY: 4000, Precise: true}) // the last rows sit below the fold
	canvas, texts := paintAt(state, panelWide)
	if !hasText(texts, state.messages.AIPanel.Toggle) {
		t.Fatalf("the shortcut page lacks %q: %q", state.messages.AIPanel.Toggle, texts)
	}
	row := panelHit(t, canvas, hitShortcutRowList.At(int(shortcutAssistant-1)))
	if row.Label != state.shortcuts.labels[shortcutAssistant] {
		t.Fatalf("row label %q", row.Label)
	}
}

func TestEmptyPanelOffersExamplesThatFillTheInput(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.Click(hitAIToggle)
	canvas, texts := paintAt(state, panelWide)
	m := state.messages.AIPanel
	for _, want := range []string{m.EmptyTitle, m.Example1, m.Example2, m.Example3, "gpt-x"} {
		if !hasText(texts, want) {
			t.Errorf("missing %q in %q", want, texts)
		}
	}
	if _, ok := hitByID(canvas, hitAISetup); ok {
		t.Error("the setup button is shown although a model is chosen")
	}
	release := state.Pointer(ui.PointerEvent{Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Target: hitAIExamples.At(1)})
	if release.Focus != hitAIInput || release.Handled {
		t.Fatalf("releasing an example = %+v, want focus on the box and the click still delivered", release)
	}
	state.Click(hitAIExamples.At(1))
	if state.ai.input.String() != m.Example2 {
		t.Fatalf("input = %q", state.ai.input.String())
	}
	if state.ai.run != nil || len(state.ai.items) != 0 {
		t.Fatal("an example prompt was sent")
	}
}

func TestPanelOpensWithoutAHostAndGuidesTowardOne(t *testing.T) {
	_, options := newWorld(t)
	options.Invalidate = func() {}
	options.AIStorePath = filepath.Join(t.TempDir(), "ai.db")
	state := newTestState(t, options)
	t.Cleanup(state.Close)
	withModel(t, state)
	if state.deck.ActiveWorkspace() != 0 {
		t.Fatal("the test needs a window with no host open")
	}
	state.Click(hitAIToggle)
	if !state.ai.open || !state.aiPanelVisible() {
		t.Fatal("the panel did not open without a host")
	}
	canvas, texts := paintAt(state, panelWide)
	m := state.messages.AIPanel
	for _, want := range []string{m.IdleTitle, m.IdleExample1, m.IdleExample2, m.IdleExample3} {
		if !hasText(texts, want) {
			t.Errorf("missing %q in %q", want, texts)
		}
	}
	if hasText(texts, m.EmptyTitle) {
		t.Error("the panel talks about a terminal that is not there")
	}
	panelHit(t, canvas, hitAIInput)
	state.Click(hitAIExamples.At(2))
	if state.ai.input.String() != m.IdleExample3 {
		t.Fatalf("input = %q", state.ai.input.String())
	}
	state.Click(hitAIToggle)
	if state.aiPanelVisible() {
		t.Fatal("the panel stayed after the second toggle")
	}
}

func TestWithoutAModelThePanelOffersSetupAndSettingsOpens(t *testing.T) {
	state, _ := panelState(t, nil)
	state.Click(hitAIToggle)
	canvas, texts := paintAt(state, panelWide)
	m := state.messages.AIPanel
	if !hasText(texts, m.SetupModel) {
		t.Fatalf("no setup button: %q", texts)
	}
	send := panelHit(t, canvas, hitAISend)
	if !send.Disabled {
		t.Fatal("Send is enabled without a model")
	}
	state.Click(hitAISetup)
	if state.activeSection != sectionSettings {
		t.Fatalf("section = %d, want settings", state.activeSection)
	}
	// The panel waits where it was.
	if !state.ai.open {
		t.Fatal("going to Settings closed the panel")
	}
}

func conversationFixture(state *State) {
	state.ai.items = []aiItem{
		{kind: aiUser, text: "why is nginx down?"},
		{kind: aiReasoning, text: "The user wants the service state."},
		{kind: aiAssistant, text: "Let me check.\n\n```sh\nsystemctl status nginx\n```\n\nDone."},
		{kind: aiTool, call: llm.ToolCall{ID: "1", Name: "terminal_exec", Arguments: `{"command":"systemctl status nginx"}`}, status: aiToolRunning, risk: risk.Low},
		{kind: aiTool, call: llm.ToolCall{ID: "2", Name: "terminal_exec", Arguments: `{"command":"sudo systemctl restart nginx"}`}, status: aiToolWaiting, risk: risk.Medium},
		{kind: aiTool, call: llm.ToolCall{ID: "3", Name: "terminal_exec", Arguments: `{"command":"cat /etc/hostname"}`}, status: aiToolDone, risk: risk.Low, result: "web-1\nsecond line of output"},
		{kind: aiTool, call: llm.ToolCall{ID: "4", Name: "terminal_exec", Arguments: `{"command":"rm -rf build"}`}, status: aiToolFailed, risk: risk.High, result: "rm: build: Permission denied"},
		{kind: aiTool, call: llm.ToolCall{ID: "5", Name: "sftp_remove", Arguments: `{"path":"/var/log/old.log"}`}, status: aiToolDenied},
		{kind: aiNotice, text: "the model said no"},
	}
}

func TestConversationShowsEveryKindOfRowAndToolStatus(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	conversationFixture(state)
	state.ai.open = true
	state.ai.scroll.ScrollToStart()
	// A tall window so that everything is on screen.
	size := ui.Size{Width: 1400, Height: 2600}
	state.Resize(size)
	_, texts := paintAt(state, size)
	m := state.messages.AIPanel
	for _, want := range []string{
		"why is nginx down?", m.Thinking, "Let me check.", "systemctl status nginx", "Done.",
		m.ToolTerminalExec, m.StatusRunning, m.StatusWaiting, m.StatusDone, m.StatusFailed, m.StatusDenied,
		m.RiskLow, m.RiskMedium, m.RiskHigh, m.ToolSFTPRemove, "/var/log/old.log", "sudo systemctl restart nginx",
		m.ShowOutput, "web-1", "the model said no",
	} {
		if !hasText(texts, want) {
			t.Errorf("missing %q", want)
		}
	}
	if hasText(texts, "The user wants the service state.") {
		t.Error("reasoning is open by default")
	}
	if hasText(texts, "second line of output") {
		t.Error("tool output is open by default")
	}
	// Reasoning and output open on a click.
	state.Click(hitAIRows.At(1))
	state.Click(hitAIRows.At(5))
	_, texts = paintAt(state, size)
	if !hasText(texts, "The user wants the service state.") || !hasText(texts, "second line of output") || !hasText(texts, m.HideOutput) {
		t.Fatalf("expanding did not show the text: %q", texts)
	}
	state.Click(hitAIRows.At(5))
	_, texts = paintAt(state, size)
	if hasText(texts, "second line of output") {
		t.Fatal("collapsing kept the output")
	}
}

func TestCodeBlocksCopyAndShowIt(t *testing.T) {
	state, _ := panelState(t, nil)
	var copied string
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiAssistant, text: "Run:\n```\nls -la\n```"}}
	canvas, _ := paintAt(state, panelWide)
	button := panelHit(t, canvas, hitAICopies.At(0))
	if button.Label != state.messages.AIPanel.CopyCode {
		t.Fatalf("copy button = %+v", button)
	}
	state.Click(hitAICopies.At(0))
	if copied != "ls -la" {
		t.Fatalf("copied %q", copied)
	}
}

func TestLinksInAnswersOpenTheirOwnDestination(t *testing.T) {
	state, _ := panelState(t, nil)
	var opened []string
	state.options.OpenURL = func(url string) error { opened = append(opened, url); return nil }
	state.ai.open = true
	state.ai.items = []aiItem{
		{kind: aiAssistant, text: "See [the first](https://one.example/a) and **bold** text."},
		{kind: aiAssistant, text: "And [the second](https://two.example/b), or [bad](javascript:alert(1))."},
	}
	canvas, texts := paintAt(state, panelWide)
	if !hasText(texts, "the second") || hasText(texts, "**") || hasText(texts, "](") {
		t.Fatalf("answers are not rendered as Markdown: %q", texts)
	}
	var ids []ui.ID
	for _, hit := range canvas.Hits() {
		if _, ok := hit.ID.In(hitAILinks); ok {
			ids = append(ids, hit.ID)
		}
	}
	if len(ids) != 2 {
		t.Fatalf("%d link hit regions, want one per safe link", len(ids))
	}
	state.Click(ids[1])
	state.Click(ids[0])
	if len(opened) != 2 || opened[0] != "https://two.example/b" || opened[1] != "https://one.example/a" {
		t.Fatalf("opened %v", opened)
	}
}

// A long message wraps in the box instead of running off its edge, and the
// arrow keys then go by the rows it wrapped to, keeping the column.
func TestMessageBoxWrapsLongMessagesAndMovesTheCaretByRow(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.ai.open = true
	value := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau\n\n中文也要换行,这一行也很长很长很长很长很长很长很长很长很长很长很长很长很长很长很长很长很长很长\n"
	state.ai.input.Insert(value)
	canvas, texts := paintAt(state, ui.Size{Width: 900, Height: 700})
	rows := 0
	box := panelHit(t, canvas, hitAIInput)
	for _, text := range canvas.Texts() {
		if text.Bounds.Y >= box.Bounds.Y && text.Bounds.Y < box.Bounds.Y+box.Bounds.Height && text.Bounds.X < box.Bounds.X+box.Bounds.Width {
			rows++
			if got := canvas.TextWidthWeight(text.Value, 13, ui.WeightRegular); got > box.Bounds.Width {
				t.Errorf("row %q is %v wide in a box %v wide", text.Value, got, box.Bounds.Width)
			}
		}
	}
	if rows < 4 {
		t.Fatalf("the message took %d rows, it should have wrapped: %q", rows, texts)
	}

	// Up from the end goes to the row above, not to the start of the message.
	end := state.ai.input.Caret()
	state.Key(ui.KeyEvent{Key: ui.KeyUp, Target: hitAIInput})
	if up := state.ai.input.Caret(); up >= end || up == 0 {
		t.Fatalf("Up moved the caret from %d to %d", end, up)
	}
	state.Key(ui.KeyEvent{Key: ui.KeyEnd, Modifiers: ui.ModifierShift, Target: hitAIInput})
	if selection := state.ai.input.Selection(); selection.Start >= selection.End {
		t.Errorf("Shift+End selected nothing: %+v", selection)
	}
}

func TestModelChipSwitchesTheModelThatAnswers(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveModels("p", []store.Model{{ProviderID: "p", Name: "gpt-x", Enabled: true}, {ProviderID: "p", Name: "gpt-y", Enabled: true}, {ProviderID: "p", Name: "gpt-off"}}); err != nil {
		t.Fatal(err)
	}
	state.aiWrote()
	state.ai.open = true
	canvas, _ := paintAt(state, panelWide)
	chip := panelHit(t, canvas, hitAIModelChip)
	if !strings.Contains(chip.Label, "gpt-x") {
		t.Fatalf("chip = %+v", chip)
	}
	state.Click(hitAIModelChip)
	canvas, texts := paintAt(state, panelWide)
	if !hasText(texts, "gpt-y") || !hasText(texts, "gpt-off") {
		t.Fatalf("the menu should list every model that was fetched, switched on or not: %q", texts)
	}
	// A fetched model that is still off is switched on by choosing it.
	state.Click(hitAIModels.At(0)) // gpt-off, the first by name
	if _, model, err := state.aiModel(); err != nil || model != "gpt-off" {
		t.Fatalf("model after choosing = %q %v", model, err)
	}
	for _, model := range state.aiModelList("p") {
		if model.Name == "gpt-off" && !model.Enabled {
			t.Error("the chosen model was not switched on")
		}
	}
	canvas, _ = paintAt(state, panelWide)
	if _, ok := hitByID(canvas, hitAIModels.At(0)); ok {
		t.Error("the menu stayed open after a choice")
	}
}

func TestModelMenuScrollsAProviderWithManyModels(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	var models []store.Model
	for index := 0; index < 40; index++ {
		models = append(models, store.Model{ProviderID: "p", Name: fmt.Sprintf("model-%02d", index)})
	}
	models = append(models, store.Model{ProviderID: "p", Name: "gpt-x", Enabled: true})
	if err := db.SaveModels("p", models); err != nil {
		t.Fatal(err)
	}
	state.aiWrote()
	state.ai.open = true
	paintAt(state, panelWide)
	state.Click(hitAIModelChip)
	canvas, texts := paintAt(state, panelWide)
	shown := 0
	for _, text := range texts {
		if strings.HasPrefix(text, "model-") || text == "gpt-x" {
			shown++
		}
	}
	if shown == 0 || shown > aiMenuRows+1 { // +1: the chip names the model that answers
		t.Fatalf("%d models shown at once, want a window of at most %d: %q", shown, aiMenuRows, texts)
	}
	rect := state.ai.panel.modelsRect
	list := &state.ai.panel.modelList
	before := list.Offset
	state.Scroll(ui.ScrollEvent{X: rect.X + 10, Y: rect.Y + 10, DeltaY: 96, Precise: true})
	if list.Offset != before+96 {
		t.Fatalf("scrolled from %v to %v, want 96 pixels further", before, list.Offset)
	}
	// The keyboard reaches the last model of a long list, and it is clickable there.
	state.Key(ui.KeyEvent{Key: ui.KeyEnd, Target: hitAIModels.At(0)})
	canvas, _ = paintAt(state, panelWide)
	last := hitAIModels.At(40)
	if hit, ok := hitByID(canvas, last); !ok || hit.Bounds.Y+hit.Bounds.Height > rect.Y+rect.Height {
		t.Fatalf("after End the last model is %+v (painted %v) outside the menu %+v", hit, ok, rect)
	}
	_ = canvas
}

func TestModelChipListsProvidersFirstWhenThereAreSeveral(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.SaveModels("p", []store.Model{{ProviderID: "p", Name: "gpt-x", Enabled: true}}))
	must(db.SaveProvider(store.Provider{ID: "q", Name: "Home", Kind: store.KindOpenAI, Enabled: true}))
	must(db.SaveModels("q", []store.Model{{ProviderID: "q", Name: "llama-a", Enabled: true}, {ProviderID: "q", Name: "llama-b", Enabled: true}}))
	state.aiWrote()
	state.ai.open = true
	paintAt(state, panelWide)
	state.Click(hitAIModelChip)
	canvas, texts := paintAt(state, panelWide)
	if !hasText(texts, "Work") || !hasText(texts, "Home") || hasText(texts, "llama-a") {
		t.Fatalf("the menu should list both providers and the models of the current one: %q", texts)
	}
	if _, ok := hitByID(canvas, hitAIModelProviders.At(1)); !ok {
		t.Fatal("the second provider is not offered")
	}
	state.Click(hitAIModelProviders.At(0)) // Home, which sorts before Work
	_, texts = paintAt(state, panelWide)
	if !hasText(texts, "llama-b") {
		t.Fatalf("opening a provider should show its models: %q", texts)
	}
	state.Click(hitAIModels.At(1))
	if provider, model, err := state.aiModel(); err != nil || model != "llama-b" || provider.ID != "q" {
		t.Fatalf("model after choosing = %v %q %v", provider, model, err)
	}
}

func approvalFixture(state *State, level risk.Level, needsText bool) *aiApproval {
	approval := &aiApproval{reply: make(chan aiAnswer, 1), request: gate.Request{
		Tool: "terminal_exec", Summary: "rm -rf /", NeedsText: needsText, Confirm: "erase",
		Assessment: risk.Assessment{Level: level, Reasons: []string{"deletes everything", "cannot be undone"}, Safer: "ls /"},
	}}
	state.ai.approvals = append(state.ai.approvals, approval)
	state.ai.open = true
	return approval
}

func TestApprovalCardShowsRiskReasonsAndGatesTheCriticalWord(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	approval := approvalFixture(state, risk.Critical, true)
	canvas, texts := paintAt(state, panelWide)
	m := state.messages.AIPanel
	for _, want := range []string{m.ApprovalTitle, m.RiskCritical, "rm -rf /", "deletes everything", "cannot be undone", "ls /", "Type erase to confirm"} {
		if !hasText(texts, want) {
			t.Errorf("missing %q in %q", want, texts)
		}
	}
	if !panelHit(t, canvas, hitAIApprove).Disabled {
		t.Fatal("Approve is enabled before the word is typed")
	}
	panelHit(t, canvas, hitAIConfirm)
	state.Click(hitAIApprove)
	if len(state.ai.approvals) != 1 {
		t.Fatal("approved without the word")
	}
	// A wrong word keeps it locked; the right one opens it.
	typeInto(state, hitAIConfirm, "eras")
	canvas, _ = paintAt(state, panelWide)
	if !panelHit(t, canvas, hitAIApprove).Disabled {
		t.Fatal("Approve opened for a wrong word")
	}
	typeInto(state, hitAIConfirm, "e")
	canvas, _ = paintAt(state, panelWide)
	if panelHit(t, canvas, hitAIApprove).Disabled {
		t.Fatal("Approve is still locked after the word")
	}
	result := state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: hitAIConfirm})
	if !result.Handled || len(state.ai.approvals) != 0 {
		t.Fatalf("Enter did not approve: %+v", result)
	}
	if answer := <-approval.reply; !answer.approved || answer.text != "erase" {
		t.Fatalf("answer = %+v", answer)
	}
}

func TestApprovalButtonsAndEscapeAnswer(t *testing.T) {
	state, _ := panelState(t, nil)
	first := approvalFixture(state, risk.Medium, false)
	second := approvalFixture(state, risk.High, false)
	canvas, texts := paintAt(state, panelWide)
	if _, ok := hitByID(canvas, hitAIConfirm); ok {
		t.Fatal("a plain question asks for a word")
	}
	if !hasText(texts, "1 more waiting") {
		t.Fatalf("no count of waiting questions: %q", texts)
	}
	if panelHit(t, canvas, hitAIApprove).Disabled {
		t.Fatal("Approve is locked for a plain question")
	}
	state.Click(hitAIApprove)
	if answer := <-first.reply; !answer.approved {
		t.Fatalf("first answer = %+v", answer)
	}
	paintAt(state, panelWide)
	result := state.Key(ui.KeyEvent{Key: ui.KeyEscape, Target: hitAIInput})
	if !result.Handled {
		t.Fatal("Escape was not taken")
	}
	if answer := <-second.reply; answer.approved {
		t.Fatalf("Escape approved: %+v", answer)
	}
	// Escape from the terminal is not the panel's.
	if result := state.Key(ui.KeyEvent{Key: ui.KeyEscape, Target: terminalHit(state, 0)}); result.Handled && len(state.ai.approvals) != 0 {
		t.Fatal("unexpected")
	}
}

func TestRunningShowsStopAndStopCancels(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.ai.open = true
	cancelled := false
	state.ai.run = &aiRun{cancel: func() { cancelled = true }, open: -1}
	canvas, _ := paintAt(state, panelWide)
	send := panelHit(t, canvas, hitAISend)
	if send.Label != state.messages.AIPanel.Stop || send.Disabled {
		t.Fatalf("send while running = %+v", send)
	}
	if !panelHit(t, canvas, hitAINew).Disabled {
		t.Error("New conversation is enabled during a run")
	}
	state.Click(hitAISend)
	if !cancelled {
		t.Fatal("Stop did not cancel the run")
	}
}

func TestErrorNoticesAreShown(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiUser, text: "hi"}, {kind: aiNotice, text: "provider said: 401 unauthorized"}}
	state.ai.notice = "could not save"
	_, texts := paintAt(state, panelWide)
	if !hasText(texts, "401 unauthorized") || !hasText(texts, "could not save") {
		t.Fatalf("errors not shown: %q", texts)
	}
}

func TestNarrowWindowsGetADrawerAndTerminalsKeepTheirWidth(t *testing.T) {
	state, _ := panelState(t, nil)
	state.Resize(panelNarrow)
	paintAt(state, panelNarrow)
	before := terminalColumns(state)
	state.Click(hitAIToggle)
	canvas, _ := paintAt(state, panelNarrow)
	panelHit(t, canvas, hitAIInput)
	dismiss := panelHit(t, canvas, hitAIDrawerDismiss)
	if terminalColumns(state) != before {
		t.Fatalf("a drawer took terminal width: %d -> %d", before, terminalColumns(state))
	}
	input := panelHit(t, canvas, hitAIInput)
	if input.Bounds.X+input.Bounds.Width > panelNarrow.Width || input.Bounds.X < panelNarrow.Width*0.4 {
		t.Fatalf("drawer input at %+v", input.Bounds)
	}
	state.Click(dismiss.ID)
	canvas, _ = paintAt(state, panelNarrow)
	if _, ok := hitByID(canvas, hitAIInput); ok || state.ai.open {
		t.Fatal("the drawer did not close")
	}
}

func TestWheelScrollsTheConversationAndFollowingTracksTheEnd(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.ai.open = true
	for index := 0; index < 40; index++ {
		state.ai.items = append(state.ai.items, aiItem{kind: aiAssistant, text: "line of an answer that is long enough to be a message number " + string(rune('A'+index%26))})
	}
	canvas, _ := paintAt(state, panelWide)
	if !state.ai.scroll.Following() || state.ai.panel.maxScroll <= 0 || state.ai.scroll.Offset() != state.ai.panel.maxScroll {
		t.Fatalf("not at the end: following=%v scroll=%v max=%v", state.ai.scroll.Following(), state.ai.scroll.Offset(), state.ai.panel.maxScroll)
	}
	if _, ok := hitByID(canvas, hitAIToEnd); ok {
		t.Fatal("the jump-to-end button shows at the end")
	}
	at := ui.Point{X: state.ai.panel.list.X + 100, Y: state.ai.panel.list.Y + 100}
	wheel := func(dy float32) ui.EventResult {
		return state.Scroll(ui.ScrollEvent{X: at.X, Y: at.Y, DeltaY: dy, Viewport: panelWide, Precise: true})
	}
	end := state.ai.scroll.Offset()
	if result := wheel(-120); !result.Handled || state.ai.scroll.Offset() != end-120 || state.ai.scroll.Following() {
		t.Fatalf("wheel up: %+v scroll=%v following=%v", result, state.ai.scroll.Offset(), state.ai.scroll.Following())
	}
	canvas, _ = paintAt(state, panelWide)
	if state.ai.scroll.Offset() != end-120 {
		t.Fatalf("a paint moved the reader: %v", state.ai.scroll.Offset())
	}
	panelHit(t, canvas, hitAIToEnd)
	// New messages do not drag a reader who scrolled up.
	state.ai.items = append(state.ai.items, aiItem{kind: aiAssistant, text: "new"})
	paintAt(state, panelWide)
	if state.ai.scroll.Offset() != end-120 || state.ai.scroll.Following() {
		t.Fatal("new text moved a reader who scrolled up")
	}
	// Wheeling to the end follows again.
	wheel(10000)
	if !state.ai.scroll.Following() {
		t.Fatal("not following at the end")
	}
	wheel(-50)
	if state.ai.scroll.Following() {
		t.Fatal("still following after scrolling up")
	}
	state.Click(hitAIToEnd)
	paintAt(state, panelWide)
	if !state.ai.scroll.Following() || state.ai.scroll.Offset() != state.ai.panel.maxScroll {
		t.Fatal("jump to end did not follow")
	}
	// A wheel outside the panel is the terminal's, one over its header is swallowed.
	if result := state.Scroll(ui.ScrollEvent{X: state.ai.panel.rect.X + 20, Y: state.ai.panel.rect.Y + 10, DeltaY: 50, Viewport: panelWide}); !result.Handled {
		t.Fatal("the wheel over the panel header leaked to the terminal")
	}
}

func TestHistoryListsOpensAndDeletes(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	db := state.ai.store
	one, _ := db.CreateConversation("First chat", "x")
	_, _ = db.AppendMessage(one.ID, store.StoredMessage{Role: "user", Text: "hello there"})
	_, _ = db.AppendMessage(one.ID, store.StoredMessage{Role: "assistant", Text: "hi, how can I help"})
	_, _ = db.CreateConversation("Second chat", "x")
	state.ai.open = true
	state.Click(hitAIHistory)
	canvas, texts := paintAt(state, panelWide)
	if !hasText(texts, "First chat") || !hasText(texts, "Second chat") || !hasText(texts, state.messages.AIPanel.HistoryTitle) {
		t.Fatalf("history not listed: %q", texts)
	}
	panelHit(t, canvas, hitAIHistoryDelete.At(1))
	// Newest first: index 1 is the first chat.
	state.Click(hitAIHistoryOpen.At(1))
	_, texts = paintAt(state, panelWide)
	if !hasText(texts, "hello there") || !hasText(texts, "hi, how can I help") || state.ai.panel.history {
		t.Fatalf("conversation not opened: %q", texts)
	}
	state.Click(hitAIHistory)
	paintAt(state, panelWide)
	state.Click(hitAIHistoryDelete.At(1))
	_, texts = paintAt(state, panelWide)
	if hasText(texts, "First chat") || !hasText(texts, "Second chat") {
		t.Fatalf("delete failed: %q", texts)
	}
	list, _ := db.Conversations("", 10)
	if len(list) != 1 {
		t.Fatalf("store holds %d conversations", len(list))
	}
	if len(state.ai.items) != 0 {
		t.Fatal("deleting the open conversation left it on screen")
	}
	// Empty history says so.
	state.Click(hitAIHistoryDelete.At(0))
	_, texts = paintAt(state, panelWide)
	if !hasText(texts, state.messages.AIPanel.HistoryEmpty) {
		t.Fatalf("no empty history text: %q", texts)
	}
}

func TestMessageBoxTakesEnterAndNewLines(t *testing.T) {
	state, _ := panelState(t, quietModel{answer: "hello back"})
	withModel(t, state)
	state.Click(hitAIToggle)
	paintAt(state, panelWide)
	input := state.AIPanelInput(app.TextInputContext{ID: hitAIInput})
	if input == nil || input.OnEvent == nil || input.Editor != nil {
		t.Fatalf("input = %+v", input)
	}
	if state.AIPanelInput(app.TextInputContext{ID: hitAISend}) != nil {
		t.Fatal("another hit got the message editor")
	}
	apply := func(event app.TextEvent) {
		input = state.AIPanelInput(app.TextInputContext{ID: hitAIInput})
		if _, err := input.OnEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	apply(app.TextEvent{Kind: app.TextInsert, Text: "first"})
	// Shift+Enter, Ctrl+J and Cmd+J make a line break; plain Enter is not one.
	state.Key(ui.KeyEvent{Key: ui.KeyEnter, Modifiers: ui.ModifierShift, Target: hitAIInput})
	state.Shortcut(ui.KeyEvent{Modifiers: ui.ModifierControl, Text: "j", Target: hitAIInput})
	apply(app.TextEvent{Kind: app.TextInsert, Text: "second"})
	if got := state.ai.input.String(); got != "first\n\nsecond" {
		t.Fatalf("input = %q", got)
	}
	// The native editor tells Shift+Enter from Enter: it makes a line, not a send.
	apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextInsertLineBreak})
	apply(app.TextEvent{Kind: app.TextInsert, Text: "third"})
	if got := state.ai.input.String(); got != "first\n\nsecond\nthird" || state.ai.run != nil || len(state.ai.items) != 0 {
		t.Fatalf("input = %q run=%v items=%d", got, state.ai.run, len(state.ai.items))
	}
	// The box grows with its lines.
	canvas, _ := paintAt(state, panelWide)
	if height := panelHit(t, canvas, hitAIInput).Bounds.Height; height <= 64 {
		t.Fatalf("box height %v did not grow", height)
	}
	// IME composition is not a send.
	state.ai.input.SetMarkedText("な", text.Range{Start: 0, End: 1})
	apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextInsertNewline})
	if state.ai.run != nil {
		t.Fatal("Enter during composition sent")
	}
	state.ai.input.Unmark()
	apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextInsertNewline})
	if state.ai.run == nil && len(state.ai.items) == 0 {
		t.Fatal("Enter did not send")
	}
	if state.ai.input.String() != "" {
		t.Fatalf("input after send = %q", state.ai.input.String())
	}
	pumpWindow(t, state, func() bool { return state.ai.run == nil })
	if last := state.ai.items[len(state.ai.items)-1]; last.kind != aiAssistant || last.text != "hello back" {
		t.Fatalf("items = %+v", state.ai.items)
	}
}

func TestTerminalMenuAsksTheAssistantWithoutSending(t *testing.T) {
	state, shell := panelState(t, quietModel{answer: "x"})
	withModel(t, state)
	shell.say("error: disk full\r\n")
	state.Wake()
	paintAt(state, panelWide)
	session := &state.sessions[0]
	rightClick(state, session, 300, 300)
	canvas, _ := paintAt(state, panelWide)
	ask := panelHit(t, canvas, hitTerminalMenuAsk)
	if !ask.Disabled {
		t.Fatal("asking about the selection is enabled without one")
	}
	panelHit(t, canvas, hitTerminalMenuExplain)
	state.Click(hitTerminalMenuSelectAll)
	rightClick(state, session, 300, 300)
	paintAt(state, panelWide)
	state.Click(hitTerminalMenuAsk)
	if !state.ai.open || !strings.Contains(state.ai.input.String(), "error: disk full") || !strings.Contains(state.ai.input.String(), "```") {
		t.Fatalf("open=%v input=%q", state.ai.open, state.ai.input.String())
	}
	if state.ai.run != nil || len(state.ai.items) != 0 {
		t.Fatal("the question was sent without the user")
	}
	state.ai.input = text.Editor{}
	rightClick(state, session, 300, 300)
	paintAt(state, panelWide)
	state.Click(hitTerminalMenuExplain)
	if state.ai.input.String() != state.messages.AIPanel.PromptExplain || state.ai.mode != agent.ModeAsk {
		t.Fatalf("input = %q mode=%v", state.ai.input.String(), state.ai.mode)
	}
}

func TestAnApprovalOpensTheClosedPanelAndTheTerminalsFollow(t *testing.T) {
	state, _ := panelState(t, nil)
	before := terminalColumns(state)
	// What aiApprover does from the agent's goroutine, on the window thread.
	state.aiPost(func(s *State) {
		s.ai.approvals = append(s.ai.approvals, &aiApproval{reply: make(chan aiAnswer, 1), request: gate.Request{Summary: "ls"}})
		s.ai.open = true
	})
	state.Wake()
	if terminalColumns(state) >= before {
		t.Fatalf("columns %d -> %d: the posted open did not resize the terminals", before, terminalColumns(state))
	}
	_, texts := paintAt(state, panelWide)
	if !hasText(texts, state.messages.AIPanel.ApprovalTitle) {
		t.Fatalf("the question is not shown: %q", texts)
	}
}

func TestOtherSectionsShowADotWhileAQuestionWaits(t *testing.T) {
	state, _ := panelState(t, nil)
	approvalFixture(state, risk.Medium, false)
	state.activeSection = sectionHosts
	canvas, _ := paintAt(state, panelWide)
	if _, ok := hitByID(canvas, hitAIInput); ok {
		t.Fatal("the panel is shown over the host list")
	}
	state.Click(hitAIToggle)
	if state.activeSection != sectionTerminal || !state.ai.open {
		t.Fatalf("the button did not bring the panel up: section=%d", state.activeSection)
	}
}

// Painting the open panel must cost about what painting without it does.
func TestPaintingTheOpenPanelAllocatesLittle(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	for index := 0; index < 50; index++ {
		switch index % 3 {
		case 0:
			state.ai.items = append(state.ai.items, aiItem{kind: aiUser, text: "question number with some words in it"})
		case 1:
			state.ai.items = append(state.ai.items, aiItem{kind: aiAssistant, text: "An answer with a block:\n```\nls -la /var/log\n```\nand more words after it to wrap across lines."})
		default:
			state.ai.items = append(state.ai.items, aiItem{kind: aiTool, call: llm.ToolCall{ID: "x", Name: "terminal_exec", Arguments: `{"command":"ls -la"}`}, status: aiToolDone, result: "out\nput"})
		}
	}
	var canvas ui.Canvas
	paint := func() {
		canvas.Reset()
		state.Paint(&canvas, panelWide)
	}
	for range 3 {
		paint()
	}
	closed := testing.AllocsPerRun(20, paint)
	state.ai.open = true
	state.aiPanelSync()
	for range 3 {
		paint()
	}
	open := testing.AllocsPerRun(20, paint)
	t.Logf("allocations per paint: panel closed %.0f, open with 50 messages %.0f", closed, open)
	if open > closed+40 {
		t.Fatalf("the open panel allocates %.0f per paint against %.0f without it", open, closed)
	}
}

// pumpWindow runs the window thread's wake-ups until the condition holds.
func pumpWindow(t *testing.T, state *State, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out; items: %+v", state.ai.items)
		}
		state.Wake()
		time.Sleep(2 * time.Millisecond)
	}
}

func TestDraggingTheEdgesResizesThePanelAndTheMessageBox(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.ai.open = true
	canvas, _ := paintAt(state, panelWide)
	divider := panelHit(t, canvas, hitAIDivider)
	before := state.aiPanelExtent(panelWide)
	x, y := divider.Bounds.X+divider.Bounds.Width/2, divider.Bounds.Y+100
	pointer := func(kind ui.PointerEventKind, at float32) {
		state.Pointer(ui.PointerEvent{Kind: kind, Target: hitAIDivider, X: at, Y: y, Viewport: panelWide, Captured: true, Button: ui.PointerButtonPrimary})
	}
	pointer(ui.PointerDown, x)
	pointer(ui.PointerMove, x-120)
	pointer(ui.PointerUp, x-120)
	if got := state.aiPanelExtent(panelWide); got < before+110 || got > before+130 {
		t.Fatalf("panel is %v wide after dragging its edge 120 left, was %v", got, before)
	}
	// It cannot be dragged past what leaves the panes their room.
	pointer(ui.PointerDown, x)
	pointer(ui.PointerMove, -500)
	pointer(ui.PointerUp, -500)
	if reserve := state.aiReserve(state.deck.ActiveWorkspace(), panelWide); reserve == 0 {
		t.Fatal("dragging the panel wide turned it into a drawer")
	}

	canvas, _ = paintAt(state, panelWide)
	grip := panelHit(t, canvas, hitAIComposerGrip)
	tall := state.ai.panel.boxHeight
	gx, gy := grip.Bounds.X+50, grip.Bounds.Y+grip.Bounds.Height/2
	event := func(kind ui.PointerEventKind, at float32) {
		state.Pointer(ui.PointerEvent{Kind: kind, Target: hitAIComposerGrip, X: gx, Y: at, Viewport: panelWide, Captured: true, Button: ui.PointerButtonPrimary})
	}
	event(ui.PointerDown, gy)
	event(ui.PointerMove, gy-90)
	event(ui.PointerUp, gy-90)
	paintAt(state, panelWide)
	if got := state.ai.panel.boxHeight; got < tall+80 {
		t.Fatalf("message box is %v tall after dragging its edge 90 up, was %v", got, tall)
	}
}

func TestAThoughtInProgressShowsItIsThinkingAndAFinishedOneDoesNot(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiUser, text: "why"}, {kind: aiReasoning, text: "let me look"}}
	state.ai.run = &aiRun{cancel: func() {}}
	canvas, texts := paintAt(state, panelWide)
	m := state.messages.AIPanel
	if !hasText(texts, m.ThinkingNow) {
		t.Fatalf("a thought being written does not say so: %q", texts)
	}
	if _, scheduled := canvas.NextFrame(); !scheduled {
		t.Error("the spinner is not animated")
	}
	state.ai.run = nil
	canvas, texts = paintAt(state, panelWide)
	if hasText(texts, m.ThinkingNow) || !hasText(texts, m.Thinking) {
		t.Fatalf("a finished thought still says it is thinking: %q", texts)
	}
	if _, scheduled := canvas.NextFrame(); scheduled {
		t.Error("a finished thought keeps the window animating")
	}
}

func TestACopiedCodeBlockShowsItsCheckForAMomentOnly(t *testing.T) {
	clocked, options := newClockedWorld(t)
	world := clocked.world
	options.Invalidate = func() {}
	options.AIStorePath = filepath.Join(t.TempDir(), "ai.db")
	options.SetClipboardText = func(string) error { return nil }
	state := newTestState(t, options)
	t.Cleanup(state.Close)
	fake := connect(t, state, world)
	withModel(t, state)
	*clocked.now = time.Second
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	fake.shell(t).say("\x1b[0m")
	state.Wake()
	state.terminalCursorSteady = true // nothing else in the window animates
	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiAssistant, text: "```\nls\n```"}}
	paintAt(state, panelWide)
	state.Click(hitAICopies.At(0))
	if delay, ok := nextFrameAfterPaint(state); !ok || delay > aiCopiedFor {
		t.Fatalf("a fresh copy asks for no frame to take its check away: %v %v", delay, ok)
	}
	*clocked.now += aiCopiedFor + time.Millisecond
	if _, ok := nextFrameAfterPaint(state); ok {
		t.Error("the check never goes away")
	}
}

func nextFrameAfterPaint(state *State) (time.Duration, bool) {
	canvas, _ := paintAt(state, panelWide)
	return canvas.NextFrame()
}

// The model list is a submenu level: Escape and Left close it and leave the
// provider menu open, and Right opens it again.
func TestModelSubmenuOwnsEscapeAndTheLeftRightKeys(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveModels("p", []store.Model{{ProviderID: "p", Name: "gpt-x", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	state.aiWrote()
	state.ai.open = true
	paintAt(state, panelWide)
	state.Click(hitAIModelChip)
	canvas, _ := paintAt(state, panelWide)
	if canvas.EscapeTarget() != hitAIModelSubDismiss {
		t.Fatalf("Escape target = %v, want the model submenu", canvas.EscapeTarget())
	}
	if _, ok := hitByID(canvas, hitAIModels.At(0)); !ok {
		t.Fatal("the model list is not open")
	}

	state.Click(hitAIModelSubDismiss)
	canvas, _ = paintAt(state, panelWide)
	if !state.ai.panel.modelMenu || state.ai.panel.modelSubmenu.IsAnyOpen() {
		t.Fatalf("closing the submenu: menu=%v submenu=%v", state.ai.panel.modelMenu, state.ai.panel.modelSubmenu)
	}
	if _, ok := hitByID(canvas, hitAIModels.At(0)); ok {
		t.Fatal("the model list stayed open")
	}
	if canvas.EscapeTarget() != hitAIModelDismiss {
		t.Fatalf("after closing the submenu Escape = %v, want the provider menu", canvas.EscapeTarget())
	}

	provider := hitAIModelProviders.At(0)
	result := state.Key(ui.KeyEvent{Key: ui.KeyRight, Target: provider})
	if !result.Handled || result.Focus != hitAIModels.At(0) {
		t.Fatalf("Right = %+v, want the first model focused", result)
	}
	canvas, _ = paintAt(state, panelWide)
	if _, ok := hitByID(canvas, hitAIModels.At(0)); !ok {
		t.Fatal("Right did not open the model list")
	}
	result = state.Key(ui.KeyEvent{Key: ui.KeyLeft, Target: hitAIModels.At(0)})
	if !result.Handled || result.Focus != provider {
		t.Fatalf("Left = %+v, want the provider row focused", result)
	}
	canvas, _ = paintAt(state, panelWide)
	if _, ok := hitByID(canvas, hitAIModels.At(0)); ok {
		t.Fatal("Left left the model list open")
	}
}
