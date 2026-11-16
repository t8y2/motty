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
	result := windowKeyRoutes(state, gfx.KeyEvent{Target: current.ID, Key: gfx.KeyRight})
	if !result.Handled || !result.FocusVisible || result.Focus.IsZero() || result.Activate != result.Focus {
		t.Fatalf("Right in the concurrency choice = %+v", result)
	}
	var canvas gfx.Canvas
	canvas.BeginEventPass(result.Activate, gfx.ClickInfo{})
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

func TestAIModelFilterAndCounts(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	provider := world.addOpenAI(t, "Custom", "https://api.example.com/v1", "key-123")

	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	models := []store.Model{
		{ProviderID: provider.ID, Name: "chat-m1", Enabled: true},
		{ProviderID: provider.ID, Name: "chat-m2", Enabled: false},
		{ProviderID: provider.ID, Name: "chat-m3", Enabled: true},
		{ProviderID: provider.ID, Name: "chat-m4", Enabled: false},
	}
	if err := db.SaveModels(provider.ID, models); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()

	state.aiSetProbeResult(provider.ID, "chat-m1", probe.Result{Status: probe.StatusSuccess, Latency: 200 * time.Millisecond})
	state.aiSetProbeResult(provider.ID, "chat-m2", probe.Result{Status: probe.StatusFailed, Error: "timeout"})
	state.aiSetProbeResult(provider.ID, "chat-m3", probe.Result{Status: probe.StatusSuccess, Latency: 100 * time.Millisecond})
	// chat-m4 has no probe result

	total, avail, fail, en := state.aiModelCounts(provider.ID)
	if total != 4 || avail != 2 || fail != 1 || en != 2 {
		t.Fatalf("unexpected counts: total=%d, avail=%d, fail=%d, en=%d", total, avail, fail, en)
	}

	// Filter: Available
	state.Click(hitAISetFilterAvailable)
	filtered := state.aiFilteredModels(provider.ID)
	if len(filtered) != 2 || filtered[0].Name != "chat-m1" || filtered[1].Name != "chat-m3" {
		t.Fatalf("unexpected available models: %v", filtered)
	}

	// Filter: Failed
	state.Click(hitAISetFilterFailed)
	filtered = state.aiFilteredModels(provider.ID)
	if len(filtered) != 1 || filtered[0].Name != "chat-m2" {
		t.Fatalf("unexpected failed models: %v", filtered)
	}

	// Filter: Enabled
	state.Click(hitAISetFilterEnabled)
	filtered = state.aiFilteredModels(provider.ID)
	if len(filtered) != 2 || filtered[0].Name != "chat-m1" || filtered[1].Name != "chat-m3" {
		t.Fatalf("unexpected enabled models: %v", filtered)
	}

	// Filter: All
	state.Click(hitAISetFilterAll)
	filtered = state.aiFilteredModels(provider.ID)
	if len(filtered) != 4 {
		t.Fatalf("unexpected all models count: %d", len(filtered))
	}
}

func TestAIModelSortLatencyAndName(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	provider := world.addOpenAI(t, "Custom", "https://api.example.com/v1", "key-123")

	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	models := []store.Model{
		{ProviderID: provider.ID, Name: "model-c", Enabled: true},
		{ProviderID: provider.ID, Name: "model-a", Enabled: true},
		{ProviderID: provider.ID, Name: "model-b", Enabled: true},
		{ProviderID: provider.ID, Name: "model-fail", Enabled: true},
	}
	_ = db.SaveModels(provider.ID, models)
	state.reloadAI()

	state.aiSetProbeResult(provider.ID, "model-c", probe.Result{Status: probe.StatusSuccess, Latency: 500 * time.Millisecond})
	state.aiSetProbeResult(provider.ID, "model-a", probe.Result{Status: probe.StatusSuccess, Latency: 100 * time.Millisecond})
	state.aiSetProbeResult(provider.ID, "model-b", probe.Result{Status: probe.StatusSuccess, Latency: 300 * time.Millisecond})
	state.aiSetProbeResult(provider.ID, "model-fail", probe.Result{Status: probe.StatusFailed})

	// Latency ascending: model-a (100ms) -> model-b (300ms) -> model-c (500ms) -> model-fail
	state.Click(hitAISetSortLatencyAsc)
	filtered := state.aiFilteredModels(provider.ID)
	if len(filtered) != 4 || filtered[0].Name != "model-a" || filtered[1].Name != "model-b" || filtered[2].Name != "model-c" || filtered[3].Name != "model-fail" {
		t.Fatalf("unexpected latency asc order: %v", filtered)
	}

	// Latency descending: model-c (500ms) -> model-b (300ms) -> model-a (100ms) -> model-fail
	state.Click(hitAISetSortLatencyDesc)
	filtered = state.aiFilteredModels(provider.ID)
	if len(filtered) != 4 || filtered[0].Name != "model-c" || filtered[1].Name != "model-b" || filtered[2].Name != "model-a" || filtered[3].Name != "model-fail" {
		t.Fatalf("unexpected latency desc order: %v", filtered)
	}

	// Name ascending: model-a -> model-b -> model-c -> model-fail
	state.Click(hitAISetSortNameAsc)
	filtered = state.aiFilteredModels(provider.ID)
	if len(filtered) != 4 || filtered[0].Name != "model-a" || filtered[1].Name != "model-b" || filtered[2].Name != "model-c" || filtered[3].Name != "model-fail" {
		t.Fatalf("unexpected name asc order: %v", filtered)
	}
}

func TestAIModelDefaultPinnedToTop(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	provider := world.addOpenAI(t, "Custom", "https://api.example.com/v1", "key-123")

	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	models := []store.Model{
		{ProviderID: provider.ID, Name: "model-fast", Enabled: true},
		{ProviderID: provider.ID, Name: "model-slow", Enabled: true},
	}
	_ = db.SaveModels(provider.ID, models)
	// Set model-slow as default
	if err := db.SetDefaultModel(provider.ID, "model-slow"); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()

	state.aiSetProbeResult(provider.ID, "model-fast", probe.Result{Status: probe.StatusSuccess, Latency: 50 * time.Millisecond})
	state.aiSetProbeResult(provider.ID, "model-slow", probe.Result{Status: probe.StatusSuccess, Latency: 800 * time.Millisecond})

	// Even though model-fast has lower latency, model-slow must be pinned to top (index 0)
	state.Click(hitAISetSortLatencyAsc)
	filtered := state.aiFilteredModels(provider.ID)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 models, got %d", len(filtered))
	}
	if filtered[0].Name != "model-slow" {
		t.Fatalf("expected default model-slow to be pinned at index 0, got %s", filtered[0].Name)
	}
	if filtered[1].Name != "model-fast" {
		t.Fatalf("expected model-fast at index 1, got %s", filtered[1].Name)
	}
}
