package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/ai/gate"
	"motty/internal/ai/risk"
)

// agentPanel opens the assistant with a model in Agent mode: the only mode that
// runs commands, and so the only one that has an approval scope.
func agentPanel(t *testing.T) *State {
	t.Helper()
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.ai.open = true
	state.Click(hitAIMode)
	return state
}

func TestApprovalScopeChipIsAgentOnlyAndNamesTheScope(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.ai.open = true
	if canvas, _ := paintAt(state, panelWide); hasHit(canvas, hitAIAutoScope) {
		t.Fatal("Ask mode shows an approval scope")
	}
	state.Click(hitAIMode)
	canvas, _ := paintAt(state, panelWide)
	chip := panelHit(t, canvas, hitAIAutoScope)
	m := state.messages.AIPanel
	if chip.Label != m.RiskLow {
		t.Errorf("chip label %q, want the scope name alone (%q)", chip.Label, m.RiskLow)
	}
	if chip.Hint != m.AutoScopeHint {
		t.Errorf("chip hint %q", chip.Hint)
	}
	if chip.Expanded {
		t.Error("closed menu is announced as expanded")
	}
}

func TestApprovalScopeMenuListsTheScopesWithTheirConsequences(t *testing.T) {
	state := agentPanel(t)
	paintAt(state, panelWide)
	state.Click(hitAIAutoScope)
	canvas, texts := paintAt(state, panelWide)
	m := state.messages.AIPanel
	for _, want := range []string{m.RiskLow, m.RiskMedium, m.RiskHigh, m.AutoScopeLowDesc, m.AutoScopeMediumDesc, m.AutoScopeHighDesc} {
		if !hasText(texts, want) {
			t.Errorf("menu does not show %q", want)
		}
	}
	if !panelHit(t, canvas, hitAIAutoScope).Expanded {
		t.Error("open menu is not announced as expanded")
	}
	if !hasHit(canvas, hitAIAutoScopeDismiss) {
		t.Error("no backdrop dismisses the menu")
	}
	for index, level := range []risk.Level{risk.Low, risk.Medium, risk.High} {
		row := panelHit(t, canvas, hitAIAutoScopeLevels.At(index))
		if row.Selected != (state.aiAutoUpTo() == level) {
			t.Errorf("row %d selected=%v for level %v", index, row.Selected, level)
		}
	}
}

func TestApprovalScopeChoiceMovesTheGateNotTheSettings(t *testing.T) {
	state := agentPanel(t)
	paintAt(state, panelWide)
	state.Click(hitAIAutoScope)
	paintAt(state, panelWide)
	state.Click(hitAIAutoScopeLevels.At(2))
	if got := state.aiGate(gate.ModeAsk).Policy.AutoUpTo; got != risk.High {
		t.Fatalf("gate runs up to %v", got)
	}
	if got := state.ai.settings.autoUpTo; got != risk.Low {
		t.Fatalf("switcher rewrote the global default: %v", got)
	}
	canvas, _ := paintAt(state, panelWide)
	if chip := panelHit(t, canvas, hitAIAutoScope); !strings.Contains(chip.Label, state.messages.AIPanel.RiskHigh) {
		t.Errorf("chip label %q after choosing high", chip.Label)
	}
	if hasHit(canvas, hitAIAutoScopeLevels.At(0)) {
		t.Error("menu stayed open after a choice")
	}
}

func TestApprovalScopeNewConversationStartsFromTheSetting(t *testing.T) {
	state := agentPanel(t)
	paintAt(state, panelWide)
	state.Click(hitAIAutoScope)
	paintAt(state, panelWide)
	state.Click(hitAIAutoScopeLevels.At(1))
	if got := state.aiAutoUpTo(); got != risk.Medium {
		t.Fatalf("scope is %v after choosing medium", got)
	}
	state.aiNewConversation()
	if got := state.aiAutoUpTo(); got != risk.Low {
		t.Fatalf("a new conversation kept %v", got)
	}
	if got := state.aiGate(gate.ModeAsk).Policy.AutoUpTo; got != risk.Low {
		t.Fatalf("a new conversation runs up to %v", got)
	}
	canvas, _ := paintAt(state, panelWide)
	if chip := panelHit(t, canvas, hitAIAutoScope); !strings.Contains(chip.Label, state.messages.AIPanel.RiskLow) {
		t.Errorf("chip label %q for a new conversation", chip.Label)
	}
}

func TestApprovalScopeChipWaitsForRunningWork(t *testing.T) {
	state := agentPanel(t)
	approvalFixture(state, risk.High, false)
	canvas, _ := paintAt(state, panelWide)
	if !panelHit(t, canvas, hitAIAutoScope).Disabled {
		t.Fatal("chip is live while an approval waits")
	}
	state.Click(hitAIAutoScope)
	if state.ai.panel.autoMenu {
		t.Fatal("menu opened while an approval waits")
	}
	state.ai.approvals = nil
	state.ai.run = &aiRun{cancel: func() {}, open: -1}
	defer func() { state.ai.run = nil }()
	canvas, _ = paintAt(state, panelWide)
	if !panelHit(t, canvas, hitAIAutoScope).Disabled {
		t.Fatal("chip is live while a request runs")
	}
	state.Click(hitAIAutoScope)
	if state.ai.panel.autoMenu {
		t.Fatal("menu opened while a request runs")
	}
	state.ai.run = nil
	canvas, _ = paintAt(state, panelWide)
	if panelHit(t, canvas, hitAIAutoScope).Disabled {
		t.Error("chip stayed disabled after the request ended")
	}
}

func TestApprovalScopeMenuKeysChooseAndClose(t *testing.T) {
	state := agentPanel(t)
	paintAt(state, panelWide)
	result := state.Key(gfx.KeyEvent{Key: gfx.KeyDown, Target: hitAIAutoScope})
	if !state.ai.panel.autoMenu {
		t.Fatal("Down on the chip did not open the menu")
	}
	if result.Focus != hitAIAutoScopeLevels.At(0) {
		t.Fatalf("Down focused %d, want the first scope", result.Focus)
	}
	paintAt(state, panelWide)
	result = state.Key(gfx.KeyEvent{Key: gfx.KeyDown, Target: hitAIAutoScopeLevels.At(0)})
	if result.Focus != hitAIAutoScopeLevels.At(1) {
		t.Fatalf("second Down focused %d, want the second scope", result.Focus)
	}
	result = state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitAIAutoScopeLevels.At(1)})
	if state.aiAutoUpTo() != risk.Medium {
		t.Fatalf("Enter chose %v", state.aiAutoUpTo())
	}
	if state.ai.panel.autoMenu {
		t.Fatal("Enter left the menu open")
	}
	if result.Focus != hitAIAutoScope {
		t.Fatalf("Enter returned focus to %d, want the chip", result.Focus)
	}
	paintAt(state, panelWide)
	state.Click(hitAIAutoScope)
	paintAt(state, panelWide)
	result = state.Key(gfx.KeyEvent{Key: gfx.KeyEscape, Target: hitAIAutoScopeLevels.At(1)})
	if state.ai.panel.autoMenu {
		t.Fatal("Escape left the menu open")
	}
	if result.Focus != hitAIAutoScope {
		t.Fatalf("Escape returned focus to %d, want the chip", result.Focus)
	}
}

func TestApprovalScopeMenuStartsOnTheFirstScopeAndKeepsTheWheel(t *testing.T) {
	state := agentPanel(t)
	paintAt(state, panelWide)
	state.Click(hitAIAutoScope)
	canvas, _ := paintAt(state, panelWide)
	if got, want := canvas.InitialFocus(), hitAIAutoScopeLevels.At(0); got != want {
		t.Fatalf("menu focus starts at %d, want the first scope", got)
	}
	row := panelHit(t, canvas, hitAIAutoScopeLevels.At(0))
	before := state.ai.scroll.Offset()
	result := state.Scroll(gfx.ScrollEvent{X: row.Bounds.X + 4, Y: row.Bounds.Y + 4, DeltaY: 120, Precise: true, Viewport: panelWide})
	if !result.Handled {
		t.Fatal("wheel over the open menu was not consumed")
	}
	if state.ai.scroll.Offset() != before {
		t.Error("wheel over the menu scrolled the conversation")
	}
}

func TestApprovalScopeAndModelMenusAreExclusive(t *testing.T) {
	state := agentPanel(t)
	paintAt(state, panelWide)
	state.Click(hitAIModelChip)
	if !state.ai.panel.modelMenu {
		t.Fatal("model menu did not open")
	}
	state.Click(hitAIAutoScope)
	if state.ai.panel.modelMenu || !state.ai.panel.autoMenu {
		t.Fatalf("opening the scope menu left model=%v scope=%v", state.ai.panel.modelMenu, state.ai.panel.autoMenu)
	}
	state.Click(hitAIModelChip)
	if !state.ai.panel.modelMenu || state.ai.panel.autoMenu {
		t.Fatalf("opening the model menu left model=%v scope=%v", state.ai.panel.modelMenu, state.ai.panel.autoMenu)
	}
}

func TestApprovalScopeChoosingTheSettingFollowsIt(t *testing.T) {
	state := agentPanel(t)
	paintAt(state, panelWide)
	state.Click(hitAIAutoScope)
	paintAt(state, panelWide)
	state.Click(hitAIAutoScopeLevels.At(0)) // the setting is Low
	if state.ai.autoUpTo != nil {
		t.Fatalf("choosing the setting's own scope pinned %v", *state.ai.autoUpTo)
	}
	state.ai.settings.autoUpTo = risk.High
	if got := state.aiGate(gate.ModeAsk).Policy.AutoUpTo; got != risk.High {
		t.Fatalf("the conversation did not follow the setting: %v", got)
	}
}

func TestApprovalScopeChipFitsAtTheMinimumWindow(t *testing.T) {
	state := agentPanel(t)
	canvas, _ := paintAt(state, gfx.Size{Width: MinimumWindowWidth, Height: MinimumWindowHeight})
	panel := state.ai.panel.rect
	controls := []gfx.Rect{
		panelHit(t, canvas, hitAIModelChip).Bounds,
		panelHit(t, canvas, hitAIAutoScope).Bounds,
		panelHit(t, canvas, hitAISend).Bounds,
	}
	for _, bounds := range controls {
		if bounds.X < panel.X-0.5 || bounds.X+bounds.Width > panel.X+panel.Width+0.5 {
			t.Errorf("control at %v overflows the panel %v", bounds, panel)
		}
	}
	// The scope chip never shrinks: the model name yields first, and the scope
	// keeps its icon, label and chevron whole at the narrowest window.
	if scope := controls[1]; scope.Width < 90 {
		t.Errorf("the approval chip shrank to %v wide", scope.Width)
	}
}
