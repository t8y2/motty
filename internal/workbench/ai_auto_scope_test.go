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
	for _, want := range []string{m.RiskLow, m.RiskMedium, m.RiskHigh} {
		if !hasText(texts, want) {
			t.Errorf("menu does not show %q", want)
		}
	}
	if !panelHit(t, canvas, hitAIAutoScope).Expanded {
		t.Error("open menu is not announced as expanded")
	}
	if dismiss, ok := aiMenuBackdrop(canvas); !ok || dismiss.Bounds.Width < panelWide.Width || dismiss.Bounds.Height < panelWide.Height {
		t.Errorf("the menu's backdrop does not cover the window: %+v %v", dismiss.Bounds, ok)
	}
	for index, level := range []risk.Level{risk.Low, risk.Medium, risk.High} {
		row := aiMenuRow(t, canvas, state.aiScopeName(level))
		if row.Selected != (state.aiAutoUpTo() == level) {
			t.Errorf("row %q selected=%v for level %v", row.Label, row.Selected, level)
		}
		// The consequence is the row's accessible description, and the line
		// under its label shows it (truncated only when the window is narrow).
		if want := [...]string{m.AutoScopeLowDesc, m.AutoScopeMediumDesc, m.AutoScopeHighDesc}[index]; row.Description != want {
			t.Errorf("row %q description %q, want %q", row.Label, row.Description, want)
		}
	}
}

func TestApprovalScopeChoiceMovesTheGateNotTheSettings(t *testing.T) {
	state := agentPanel(t)
	paintAt(state, panelWide)
	state.Click(hitAIAutoScope)
	paintAt(state, panelWide)
	clickAIMenuRow(t, state, state.messages.AIPanel.RiskHigh)
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
	if state.ai.panel.autoMenu {
		t.Error("menu stayed open after a choice")
	}
}

func TestApprovalScopeNewConversationStartsFromTheSetting(t *testing.T) {
	state := agentPanel(t)
	paintAt(state, panelWide)
	state.Click(hitAIAutoScope)
	paintAt(state, panelWide)
	clickAIMenuRow(t, state, state.messages.AIPanel.RiskMedium)
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
	// Down on the closed chip opens its menu; the page menu then takes the
	// keyboard, so the next Down goes into its rows.
	windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyDown, Target: hitAIAutoScope})
	if !state.ai.panel.autoMenu {
		t.Fatal("Down on the chip did not open the menu")
	}
	canvas, _ := paintAt(state, panelWide)

	// Down from the chip goes to the first scope; the page menu's keyboard
	// routing is what moves the focus between its rows.
	result := windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyDown, Target: hitAIAutoScope})
	if result.Focus.IsZero() {
		t.Fatal("Down from the chip did not enter the menu")
	}
	canvas, _ = paintAt(state, panelWide)
	first, ok := hitByID(canvas, result.Focus)
	if !ok || first.Label != state.messages.AIPanel.RiskLow {
		t.Fatalf("Down focused %q, want the first scope", first.Label)
	}
	result = windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyDown, Target: result.Focus})
	canvas, _ = paintAt(state, panelWide)
	second, ok := hitByID(canvas, result.Focus)
	if !ok || second.Label != state.messages.AIPanel.RiskMedium {
		t.Fatalf("second Down focused %q, want the second scope", second.Label)
	}
	// Enter on a row is a click on it: the runtime delivers that in the event
	// pass, where the page menu reads its choice.
	clickMenuItem(t, state, second)
	if state.aiAutoUpTo() != risk.Medium {
		t.Fatalf("Enter chose %v", state.aiAutoUpTo())
	}
	if state.ai.panel.autoMenu {
		t.Fatal("Enter left the menu open")
	}

	// Escape is the runtime's dismiss click on the menu's escape target.
	state.Click(hitAIAutoScope)
	canvas, _ = paintAt(state, panelWide)
	result = windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyDown, Target: hitAIAutoScope})
	windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyEscape, Target: result.Focus})
	clickMenuItemByID(t, state, canvas.EscapeTarget())
	if state.ai.panel.autoMenu {
		t.Fatal("Escape left the menu open")
	}
}

func TestApprovalScopeMenuStartsOnTheFirstScopeAndKeepsTheWheel(t *testing.T) {
	state := agentPanel(t)
	paintAt(state, panelWide)
	state.Click(hitAIAutoScope)
	canvas, _ := paintAt(state, panelWide)
	result := windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyDown, Target: hitAIAutoScope})
	if row, ok := hitByID(canvas, result.Focus); !ok || row.Label != state.messages.AIPanel.RiskLow {
		t.Fatalf("the menu focus starts on %q, want the first scope", row.Label)
	}
	row := aiMenuRow(t, canvas, state.messages.AIPanel.RiskLow)
	before := aiListOffset(state)
	result = windowScrollRoutes(state, gfx.ScrollEvent{X: row.Bounds.X + 4, Y: row.Bounds.Y + 4, DeltaY: 120, Precise: true, Viewport: panelWide, Target: row.ID})
	if !result.Handled {
		t.Fatal("wheel over the open menu was not consumed")
	}
	if aiListOffset(state) != before {
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
	clickAIMenuRow(t, state, state.messages.AIPanel.RiskLow) // the setting is Low
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
