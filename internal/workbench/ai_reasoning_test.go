package workbench

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/llm"
	"motty/internal/ai/reasoning"
	"motty/internal/ai/store"
)

type captureProvider struct {
	called  chan struct{}
	lastReq llm.Request
}

func (c *captureProvider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	c.lastReq = req
	if c.called != nil {
		select {
		case c.called <- struct{}{}:
		default:
		}
	}
	out := make(chan llm.Event, 2)
	out <- llm.Event{Kind: llm.EventText, Text: "ok"}
	out <- llm.Event{Kind: llm.EventDone}
	close(out)
	return out, nil
}

func (c *captureProvider) Models(ctx context.Context) ([]string, error) {
	return []string{"o3-mini", "gpt-4o"}, nil
}

func TestAIReasoningInferenceAndEffectiveEffort(t *testing.T) {
	prov := &captureProvider{}
	state, _ := panelState(t, prov)
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}

	// 保存带有 ReasoningConfig 的 provider 与模型
	p := store.Provider{ID: "p1", Name: "OpenAI", Kind: "openai", BaseURL: "https://api.openai.com", Enabled: true}
	_ = db.SaveProvider(p)
	_ = db.SaveModels("p1", []store.Model{
		{
			ProviderID: "p1",
			Name:       "o3-mini",
			Enabled:    true,
			Reasoning: &store.ReasoningConfig{
				Levels:  []string{reasoning.LevelLow, reasoning.LevelMedium, reasoning.LevelHigh},
				Default: reasoning.LevelMedium,
			},
		},
		{
			ProviderID: "p1",
			Name:       "gpt-4o",
			Enabled:    true,
		},
	})
	_ = db.SetDefaultModel("p1", "o3-mini")
	state.reloadAI()

	// 验证当前模型为 o3-mini 并识别推理档位
	m, levels, ok := state.aiCurrentModelReasoning()
	if !ok || m == nil {
		t.Fatalf("expected o3-mini to have reasoning support")
	}
	if !slices.Equal(levels, []string{reasoning.LevelLow, reasoning.LevelMedium, reasoning.LevelHigh}) {
		t.Fatalf("unexpected levels: %v", levels)
	}

	// 默认未覆盖时，effective effort 应为 "medium"
	if eff := state.aiEffectiveReasoningEffort(); eff != reasoning.LevelMedium {
		t.Fatalf("expected default effort 'medium', got %q", eff)
	}

	// 手动切换为 "high"
	state.ai.panel.reasoningEffort = reasoning.LevelHigh
	if eff := state.aiEffectiveReasoningEffort(); eff != reasoning.LevelHigh {
		t.Fatalf("expected overridden effort 'high', got %q", eff)
	}

	// 切换到 gpt-4o（不支持推理）
	_ = db.SetDefaultModel("p1", "gpt-4o")
	state.reloadAI()
	state.ai.panel.reasoningEffort = ""

	_, _, ok = state.aiCurrentModelReasoning()
	if ok {
		t.Fatalf("expected gpt-4o not to have reasoning support")
	}
	if eff := state.aiEffectiveReasoningEffort(); eff != "" {
		t.Fatalf("expected empty effort for gpt-4o, got %q", eff)
	}
}

func TestAIReasoningMenuInteractionAndRequestTransmission(t *testing.T) {
	prov := &captureProvider{called: make(chan struct{}, 1)}
	state, _ := panelState(t, prov)
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}

	p := store.Provider{ID: "p1", Name: "OpenAI", Kind: "openai", BaseURL: "https://api.openai.com", Enabled: true}
	_ = db.SaveProvider(p)
	_ = db.SaveModels("p1", []store.Model{
		{
			ProviderID: "p1",
			Name:       "o3-mini",
			Enabled:    true,
			Reasoning: &store.ReasoningConfig{
				Levels:  []string{reasoning.LevelLow, reasoning.LevelMedium, reasoning.LevelHigh},
				Default: reasoning.LevelMedium,
			},
		},
	})
	_ = db.SetDefaultModel("p1", "o3-mini")
	state.reloadAI()
	state.ai.open = true
	paintAt(state, panelWide)

	// 点击 reasoning chip，开启菜单
	state.Click(hitAIReasoningChip)
	if !state.ai.panel.reasoningMenu {
		t.Fatalf("expected reasoningMenu to be open after chip click")
	}

	// 选择菜单中的第 2 项（index 2 对应 "high"）
	clickAIMenuRow(t, state, state.aiReasoningEffortLabel(reasoning.LevelHigh))
	if state.ai.panel.reasoningMenu {
		t.Fatalf("expected reasoningMenu to close after selection")
	}
	if state.ai.panel.reasoningEffort != reasoning.LevelHigh {
		t.Fatalf("expected selected effort 'high', got %q", state.ai.panel.reasoningEffort)
	}

	// 输入文本并发送
	state.ai.input.Insert("test reasoning")
	state.aiSend()

	select {
	case <-prov.called:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Stream to be called")
	}

	// 验证请求是否透传了 ReasoningEffort
	if prov.lastReq.ReasoningEffort != reasoning.LevelHigh {
		t.Fatalf("expected transmitted ReasoningEffort 'high', got %q", prov.lastReq.ReasoningEffort)
	}
}

func TestAIReasoningSettingsRefresh(t *testing.T) {
	state, _ := panelState(t, &captureProvider{})
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveProvider(store.Provider{ID: "p1", Name: "Custom", Kind: "openai", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveModels("p1", []store.Model{{ProviderID: "p1", Name: "custom-model", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDefaultModel("p1", "custom-model"); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	if got := state.aiEffectiveReasoningEffort(); got != "" {
		t.Fatalf("initial effort = %q", got)
	}
	for _, tc := range []struct {
		name     string
		config   *store.ReasoningConfig
		disabled bool
		want     string
	}{
		{"enabled", &store.ReasoningConfig{Levels: []string{"low", "high"}, Default: "high"}, false, "high"},
		{"changed", &store.ReasoningConfig{Levels: []string{"low", "high"}, Default: "low"}, false, "low"},
		{"disabled", nil, true, ""},
		{"auto", nil, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := db.SetModelReasoning("p1", "custom-model", tc.config, tc.disabled); err != nil {
				t.Fatal(err)
			}
			state.setMCPEndpoint("", tc.name)
			pumpSettings(t, state, "reasoning settings", func() bool { return state.ai.mcpToken == tc.name })
			if got := state.aiEffectiveReasoningEffort(); got != tc.want {
				t.Fatalf("refreshed effort = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAIReasoningDialogSaveReachesOtherWindows(t *testing.T) {
	windows, _ := assistantWindowsOnOneStore(t)
	origin := windows[0]
	db := origin.ai.store
	if err := db.SaveProvider(store.Provider{ID: "p", Name: "Test", Kind: store.KindOpenAI, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveModels("p", []store.Model{{Name: "o3-mini", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDefaultModel("p", "o3-mini"); err != nil {
		t.Fatal(err)
	}
	origin.aiWrote()
	for _, state := range windows {
		state.Wake()
		state.ai.open = true
		state.ai.cfg.selected = "p"
		state.save()
	}
	for _, state := range windows {
		state.Wake()
		state.aiCurrentModelReasoning()
	}
	windows[1].ai.panel.reasoningEffort = reasoning.LevelHigh
	windows[2].ai.panel.reasoningEffort = reasoning.LevelXHigh
	m := origin.messages.AISettings
	for _, tc := range []struct {
		name, mode, preset string
		levels             []string
		disabled, override bool
	}{
		{"disabled", m.ModelReasoningDisable, "", nil, true, true},
		{"enabled", m.ModelReasoningEnable, m.ModelReasoningPreset6, []string{"minimal", "low", "medium", "high", "xhigh", "max"}, false, true},
		{"changed", m.ModelReasoningEnable, m.ModelReasoningPreset3, []string{"low", "medium", "high"}, false, true},
		{"auto", m.ModelReasoningAuto, "", []string{"low", "medium", "high"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origin.Click(hitAISetModelReasoning.At(0))
			if origin.ai.cfg.dialog != aiDialogModelReasoning {
				t.Fatal("reasoning dialog did not open")
			}
			clickSettingsChoice(t, origin, tc.mode)
			if tc.preset != "" {
				clickSettingsChoice(t, origin, tc.preset)
			}
			origin.Click(hitAISetReasoningSave)
			if origin.ai.cfg.dialog != aiDialogNone {
				t.Fatalf("save failed: %s", origin.ai.cfg.formErr)
			}
			for index, state := range windows {
				state.Wake()
				model, levels, supported := state.aiCurrentModelReasoning()
				if model == nil {
					t.Fatalf("window %d has no selected model", index)
				}
				wantEffort := reasoning.LevelMedium
				if tc.disabled {
					wantEffort = ""
				} else if index == 1 {
					wantEffort = reasoning.LevelHigh
				} else if index == 2 && tc.name == "enabled" {
					wantEffort = reasoning.LevelXHigh
				}
				if got := state.aiEffectiveReasoningEffort(); got != wantEffort {
					t.Errorf("window %d: effort=%q, want %q", index, got, wantEffort)
				}
				if want := []string{"", reasoning.LevelHigh, reasoning.LevelXHigh}[index]; state.ai.panel.reasoningEffort != want {
					t.Errorf("window %d: local effort override changed to %q, want %q", index, state.ai.panel.reasoningEffort, want)
				}
				_, _, _, override, disabled := state.aiModelReasoningInfo(model)
				if !slices.Equal(levels, tc.levels) || supported != (len(tc.levels) > 0) || disabled != tc.disabled || override != tc.override {
					t.Errorf("window %d: levels=%v supported=%v disabled=%v override=%v; want levels=%v disabled=%v override=%v", index, levels, supported, disabled, override, tc.levels, tc.disabled, tc.override)
				}
			}
		})
	}
}

func TestAIReasoningPaintWithoutPanic(t *testing.T) {
	prov := &captureProvider{}
	state, _ := panelState(t, prov)
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}

	// 模型不支持推理
	p := store.Provider{ID: "p1", Name: "OpenAI", Kind: "openai", BaseURL: "https://api.openai.com", Enabled: true}
	_ = db.SaveProvider(p)
	_ = db.SaveModels("p1", []store.Model{
		{
			ProviderID: "p1",
			Name:       "gpt-4o",
			Enabled:    true,
		},
	})
	_ = db.SetDefaultModel("p1", "gpt-4o")
	state.reloadAI()

	canvas := &gfx.Canvas{}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("paint panicked with unsupported reasoning model: %v", r)
		}
	}()

	state.Paint(canvas, panelWide)
}

func TestAIModelReasoningManualOverride(t *testing.T) {
	prov := &captureProvider{}
	state, _ := panelState(t, prov)
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}

	p := store.Provider{ID: "p1", Name: "DeepSeek", Kind: "openai", BaseURL: "https://api.deepseek.com", Enabled: true}
	_ = db.SaveProvider(p)
	_ = db.SaveModels("p1", []store.Model{
		{
			ProviderID: "p1",
			Name:       "deepseek-v4-pro",
			Enabled:    true,
		},
		{
			ProviderID: "p1",
			Name:       "z-custom-model",
			Enabled:    true,
		},
	})
	_ = db.SetDefaultModel("p1", "deepseek-v4-pro")
	state.reloadAI()
	state.ai.cfg.selected = "p1"
	state.activeSection = sectionSettings

	// 1. 验证默认自动感知：deepseek-v4-pro 自动推断为支持思考
	model0, ok := state.aiModelAt("p1", 0)
	if !ok || model0.Name != "deepseek-v4-pro" {
		t.Fatalf("expected deepseek-v4-pro at index 0, got %q (ok=%v)", model0.Name, ok)
	}
	levels, _, isReasoning, isOverride, isDisabled := state.aiModelReasoningInfo(&model0)
	if !isReasoning || isOverride || isDisabled || len(levels) == 0 {
		t.Fatalf("expected auto detected reasoning for deepseek-v4-pro: isReasoning=%v isOverride=%v isDisabled=%v levels=%v", isReasoning, isOverride, isDisabled, levels)
	}

	// 2. 点击第 0 行模型的思考药丸，打开配置对话框
	state.Click(hitAISetModelReasoning.At(0))
	if state.ai.cfg.dialog != aiDialogModelReasoning {
		t.Fatalf("expected dialog to be aiDialogModelReasoning, got %v", state.ai.cfg.dialog)
	}
	if state.ai.cfg.reasoningModelName != "deepseek-v4-pro" {
		t.Fatalf("expected reasoningModelName 'deepseek-v4-pro', got %q", state.ai.cfg.reasoningModelName)
	}
	if state.ai.cfg.reasoningMode != 0 {
		t.Fatalf("expected initial reasoningMode 0 (Auto), got %d", state.ai.cfg.reasoningMode)
	}

	// 3. 用户选择强制关闭 (Disable) 并保存
	clickSettingsChoice(t, state, state.messages.AISettings.ModelReasoningDisable)
	if state.ai.cfg.reasoningMode != 2 {
		t.Fatalf("expected reasoningMode 2, got %d", state.ai.cfg.reasoningMode)
	}
	state.Click(hitAISetReasoningSave)
	if state.ai.cfg.dialog != aiDialogNone {
		t.Fatalf("expected dialog to close after save")
	}

	// 重新读取模型信息，验证已强制关闭
	model0, _ = state.aiModelAt("p1", 0)
	_, _, isReasoning, isOverride, isDisabled = state.aiModelReasoningInfo(&model0)
	if isReasoning || !isDisabled || !isOverride {
		t.Fatalf("expected model to be disabled: isReasoning=%v isDisabled=%v isOverride=%v", isReasoning, isDisabled, isOverride)
	}
	// 验证会话中也不再提供思考档位
	_, _, ok = state.aiCurrentModelReasoning()
	if ok {
		t.Fatalf("expected aiCurrentModelReasoning to be false when model reasoning is disabled")
	}

	// 4. 用户针对未知模型 z-custom-model 手动强制开启 (Enable) 思考并选择细分六阶 (Preset 1)
	state.Click(hitAISetModelReasoning.At(1))
	if state.ai.cfg.reasoningModelName != "z-custom-model" {
		t.Fatalf("expected reasoningModelName 'z-custom-model', got %q", state.ai.cfg.reasoningModelName)
	}
	clickSettingsChoice(t, state, state.messages.AISettings.ModelReasoningEnable)
	clickSettingsChoice(t, state, state.messages.AISettings.ModelReasoningPreset6) // 6 档
	state.Click(hitAISetReasoningSave)

	model1, _ := state.aiModelAt("p1", 1)
	var def string
	levels, def, isReasoning, isOverride, isDisabled = state.aiModelReasoningInfo(&model1)
	if !isReasoning || !isOverride || isDisabled || len(levels) != 6 || def != reasoning.LevelMedium {
		t.Fatalf("expected custom model to be enabled with 6 levels: isReasoning=%v isOverride=%v isDisabled=%v levels=%v def=%q", isReasoning, isOverride, isDisabled, levels, def)
	}

	// 5. 将 deepseek-v4-pro 恢复为自动感知 (Auto)
	state.Click(hitAISetModelReasoning.At(0))
	clickSettingsChoice(t, state, state.messages.AISettings.ModelReasoningAuto)
	state.Click(hitAISetReasoningSave)

	model0, _ = state.aiModelAt("p1", 0)
	_, _, isReasoning, isOverride, isDisabled = state.aiModelReasoningInfo(&model0)
	if !isReasoning || isOverride || isDisabled {
		t.Fatalf("expected model to be restored to auto: isReasoning=%v isOverride=%v isDisabled=%v", isReasoning, isOverride, isDisabled)
	}

	// 6. 验证渲染弹窗不发生 panic
	state.Click(hitAISetModelReasoning.At(0))
	clickSettingsChoice(t, state, state.messages.AISettings.ModelReasoningEnable)
	drawDialogPage(t, state, func(c ui.C) { state.declareAIDialogs(c) })
	state.Click(hitAISetReasoningCancel)
	if state.ai.cfg.dialog != aiDialogNone {
		t.Fatalf("expected dialog to close after cancel")
	}
}
