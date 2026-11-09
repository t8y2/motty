package workbench

import (
	"fmt"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/ai/probe"
	"motty/internal/ai/store"
)

func TestAIModelFilterExcludesNonChatModels(t *testing.T) {
	world := newSettingsWorld(t, newLister(
		"deepseek-chat",
		"deepseek-reasoner",
		"text-embedding-3-small",
		"bge-rerank-v2",
	))
	state := world.state
	provider := world.addOpenAI(t, "Custom", "https://api.example.com/v1", "key-123")

	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}

	models := []store.Model{
		{ProviderID: provider.ID, Name: "deepseek-chat", Enabled: true},
		{ProviderID: provider.ID, Name: "deepseek-reasoner", Enabled: true},
		{ProviderID: provider.ID, Name: "text-embedding-3-small", Enabled: false},
		{ProviderID: provider.ID, Name: "bge-rerank-v2", Enabled: false},
	}
	if err := db.SaveModels(provider.ID, models); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()

	// Only chat and reasoning models should be returned; embedding and rerank are excluded
	filtered := state.aiFilteredModels(provider.ID)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 chat models, got %d: %v", len(filtered), filtered)
	}
	if filtered[0].Name != "deepseek-chat" || filtered[1].Name != "deepseek-reasoner" {
		t.Fatalf("unexpected filtered models: %v", filtered)
	}

	total, enabled := state.aiModelsOf(provider.ID)
	if total != 2 || enabled != 2 {
		t.Fatalf("expected total=2, enabled=2; got total=%d, enabled=%d", total, enabled)
	}

	// Mark one as successful probe
	state.aiSetProbeResult(provider.ID, "deepseek-chat", probe.Result{
		Status:  probe.StatusSuccess,
		Latency: 120 * time.Millisecond,
	})
	res, ok := state.aiGetProbeResult(provider.ID, "deepseek-chat")
	if !ok || res.Status != probe.StatusSuccess || res.Latency != 120*time.Millisecond {
		t.Fatalf("probe result not stored properly: %+v", res)
	}
}

func TestAIModelProbeAllAndCancel(t *testing.T) {
	world := newSettingsWorld(t, newLister("model-1", "model-2"))
	state := world.state
	provider := world.addOpenAI(t, "Custom", "https://api.example.com/v1", "key-123")

	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	models := []store.Model{
		{ProviderID: provider.ID, Name: "model-1", Enabled: true},
		{ProviderID: provider.ID, Name: "model-2", Enabled: true},
	}
	_ = db.SaveModels(provider.ID, models)
	state.reloadAI()

	// Click probe all opens the probe configuration dialog
	state.Click(hitAISetModelProbeAll)
	if state.ai.cfg.dialog != aiDialogProbe {
		t.Fatalf("expected dialog to be aiDialogProbe, got %v", state.ai.cfg.dialog)
	}

	// Change parameters in dialog
	clickSettingsChoice(t, state, "2")
	clickSettingsChoice(t, state, "10s")
	if got := state.aiProbeConcurrency(); got != 2 {
		t.Fatalf("expected concurrency 2, got %d", got)
	}
	if got := state.aiProbeTimeout(); got != 10*time.Second {
		t.Fatalf("expected timeout 10s, got %v", got)
	}

	// Click start probe
	state.Click(hitAISetProbeStart)
	if state.ai.cfg.dialog != aiDialogNone {
		t.Fatalf("expected dialog to close, got %v", state.ai.cfg.dialog)
	}
	if state.ai.cfg.probeCancel == nil {
		t.Fatal("expected probeCancel to be set during probe all")
	}

	// While probing, clicking probe all button cancels probing
	state.Click(hitAISetModelProbeAll)
	if state.ai.cfg.probeCancel != nil {
		t.Fatal("expected probeCancel to be cleared after cancel")
	}
}

func TestAIModelPagination(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	provider := world.addOpenAI(t, "Custom", "https://api.example.com/v1", "key-123")

	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	// Add 9 models (page size is 8)
	var models []store.Model
	for i := 1; i <= 9; i++ {
		models = append(models, store.Model{
			ProviderID: provider.ID,
			Name:       fmt.Sprintf("chat-model-%d", i),
			Enabled:    true,
		})
	}
	if err := db.SaveModels(provider.ID, models); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()

	filtered := state.aiFilteredModels(provider.ID)
	if len(filtered) != 9 {
		t.Fatalf("expected 9 models, got %d", len(filtered))
	}

	if state.ai.cfg.page != 0 {
		t.Fatalf("expected page 0, got %d", state.ai.cfg.page)
	}

	// Clicking Next should advance to page 1
	state.Click(hitAISetModelNext)
	if state.ai.cfg.page != 1 {
		t.Fatalf("expected page 1, got %d", state.ai.cfg.page)
	}

	// Clicking Next on the last page should clamp to page 1
	state.Click(hitAISetModelNext)
	if state.ai.cfg.page != 1 {
		t.Fatalf("expected page 1, got %d", state.ai.cfg.page)
	}

	// Clicking Prev should go back to page 0
	state.Click(hitAISetModelPrev)
	if state.ai.cfg.page != 0 {
		t.Fatalf("expected page 0, got %d", state.ai.cfg.page)
	}
}

func TestAIProbeDialogChoicesMoveWithArrowKeys(t *testing.T) {
	world := newSettingsWorld(t, newLister("model-1"))
	state := world.state
	state.Click(hitAISetModelProbeAll)
	current := settingsChoiceHit(t, state, "4")
	result := state.Key(gfx.KeyEvent{Target: current.ID, Key: gfx.KeyRight})
	if !result.Handled || !result.FocusVisible || result.Focus.IsZero() || result.Activate != result.Focus {
		t.Fatalf("Right in the concurrency choice = %+v", result)
	}
	var canvas gfx.Canvas
	canvas.BeginEventPass(result.Activate)
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 3400})
	canvas.EndEventPass()
	if got := state.aiProbeConcurrency(); got != 8 {
		t.Fatalf("concurrency after Right = %d, want 8", got)
	}
}

func TestAIModelProbeConcurrencyAndTimeoutOptions(t *testing.T) {
	world := newSettingsWorld(t, newLister("model-1"))
	state := world.state

	// Default values
	if got := state.aiProbeConcurrency(); got != 4 {
		t.Fatalf("expected default concurrency 4, got %d", got)
	}
	if got := state.aiProbeTimeout(); got != 15*time.Second {
		t.Fatalf("expected default timeout 15s, got %v", got)
	}

	// The choices are in the probe dialog.
	state.Click(hitAISetModelProbeAll)
	if state.ai.cfg.dialog != aiDialogProbe {
		t.Fatalf("expected the probe dialog, got %v", state.ai.cfg.dialog)
	}

	// Change concurrency
	clickSettingsChoice(t, state, "1")
	if got := state.aiProbeConcurrency(); got != 1 {
		t.Fatalf("expected concurrency 1, got %d", got)
	}
	clickSettingsChoice(t, state, "8")
	if got := state.aiProbeConcurrency(); got != 8 {
		t.Fatalf("expected concurrency 8, got %d", got)
	}

	// Change timeout
	clickSettingsChoice(t, state, "5s")
	if got := state.aiProbeTimeout(); got != 5*time.Second {
		t.Fatalf("expected timeout 5s, got %v", got)
	}
	clickSettingsChoice(t, state, "30s")
	if got := state.aiProbeTimeout(); got != 30*time.Second {
		t.Fatalf("expected timeout 30s, got %v", got)
	}
}
