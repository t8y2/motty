package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
	"motty/internal/update"
)

// A narrow window docks the assistant as a drawer, which is painted before the
// modal pass. The drawer and a dialog are separate overlays, so they must not
// share a layer: one layer paints all its shapes and then all its text, and a
// shared layer would float the drawer's answer over the dialog's surface.
func TestTheAssistantDrawerKeepsItsOwnLayerUnderADialog(t *testing.T) {
	world, options := newWorld(t)
	options.Invalidate = func() {}
	options.CheckUpdate = func() (update.Release, error) {
		return update.Release{Version: "99.0.0", URL: "https://example.com/release"}, nil
	}
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) {
		s.Phase = sshconn.PhaseConnected
		s.ServerVersion = "SSH-2.0-Test"
		s.Method = "password"
	})
	state.Wake()
	shell := fake.shell(t)
	shell.say("\x1b[0m")
	state.Wake()

	state.ai.open = true
	state.ai.items = []aiItem{{kind: aiAssistant, text: "## SOCKS\n\n- 在 ~/.ssh/config 里加 ServerAliveInterval 60"}}
	openGeneralSettings(state)
	state.Click(hitSettingsUpdateCheck)
	waitUpdate(t, state, func() bool { return state.modal == modalUpdate })
	state.activeSection = sectionTerminal

	// 726 is narrower than aiDockAt, so the assistant is the drawer.
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 726, Height: 546})

	assistantLayer, dialogLayer := -1, -1
	for _, text := range canvas.Texts() {
		switch {
		case strings.Contains(text.Value, "SOCKS"):
			assistantLayer = int(text.Layer)
		case strings.Contains(text.Value, "is available"):
			dialogLayer = int(text.Layer)
		}
	}
	if assistantLayer < 0 || dialogLayer < 0 {
		t.Fatalf("the frame lacks the drawer or the dialog: assistant %d, dialog %d", assistantLayer, dialogLayer)
	}
	if assistantLayer >= dialogLayer {
		t.Fatalf("the assistant's text is on layer %d and the dialog on %d: one layer paints its text after "+
			"its shapes, so the answer would show through the dialog", assistantLayer, dialogLayer)
	}
}

func TestAITargetTriggerStaysBelowAutoScopeMenuInDrawerMode(t *testing.T) {
	state := agentPanel(t)
	state.Click(hitAIAutoScope)
	if !state.ai.panel.autoMenu {
		t.Fatal("click on hitAIAutoScope did not open autoMenu")
	}

	// 800 is narrower than aiDockAt (1180), so the assistant is the drawer.
	canvas, _ := paintAt(state, panelNarrow)

	trigger, hasTrigger := hitByID(canvas, hitAITarget)
	if !hasTrigger {
		t.Fatal("canvas lacks hitAITarget")
	}

	triggerLayer, menuLayer := -1, -1
	for _, text := range canvas.Texts() {
		switch {
		case text.Value == trigger.Value || strings.HasPrefix(text.Value, trigger.Value):
			triggerLayer = int(text.Layer)
		case strings.Contains(text.Value, state.messages.AIPanel.RiskLow):
			menuLayer = int(text.Layer)
		}
	}
	t.Logf("triggerLayer=%d, menuLayer=%d, triggerValue=%q", triggerLayer, menuLayer, trigger.Value)
	if triggerLayer < 0 || menuLayer < 0 {
		t.Fatalf("frame lacks trigger (%d) or menu (%d)", triggerLayer, menuLayer)
	}
	if triggerLayer >= menuLayer {
		t.Fatalf("trigger text is on layer %d, menu is on layer %d: trigger text will overlap menu surface", triggerLayer, menuLayer)
	}

	// When the target dropdown itself is opened, its options list must still cover
	// the drawer's text. It sits above dialogLayer, which is safe only because
	// paintAITargetSelect returns early while a dialog is open: no frame paints
	// this list and a dialog together. Asserting the relation rather than the
	// number keeps the intent when the layer constants move.
	state.ai.panel.autoMenu = false
	state.Click(hitAITarget)
	if !state.ai.panel.targetOpen {
		t.Fatal("click on hitAITarget did not open targetOpen")
	}
	canvas, _ = paintAt(state, panelNarrow)
	dropdownOptionLayer := -1
	for _, text := range canvas.Texts() {
		if strings.Contains(text.Value, state.messages.AIPanel.TargetNone) {
			dropdownOptionLayer = int(text.Layer)
			break
		}
	}
	if dropdownOptionLayer < 0 {
		t.Fatal("frame lacks the target dropdown's options")
	}
	if dropdownOptionLayer <= triggerLayer {
		t.Fatalf("target dropdown option layer = %d, trigger = %d: the list would hide behind the drawer's text",
			dropdownOptionLayer, triggerLayer)
	}
}
