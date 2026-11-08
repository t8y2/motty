package reasoning

import (
	"reflect"
	"testing"

	"motty/internal/ai/store"
)

func TestReasoningLevels(t *testing.T) {
	for _, l := range AllLevels {
		if !IsValidLevel(l) {
			t.Errorf("level %q should be valid", l)
		}
	}
	if IsValidLevel("non-existent") {
		t.Errorf("non-existent level should not be valid")
	}
}

func TestNearestLevel(t *testing.T) {
	supported := []string{LevelNone, LevelLow, LevelHigh}

	// Exact match
	if got := NearestLevel(LevelLow, supported); got != LevelLow {
		t.Errorf("expected low, got %s", got)
	}

	// Distance match: medium -> high (or low)
	if got := NearestLevel(LevelMedium, supported); got != LevelLow && got != LevelHigh {
		t.Errorf("expected low or high for medium, got %s", got)
	}

	// Default fallback
	if got := NearestLevel(LevelDefault, supported); got != LevelLow {
		t.Errorf("expected low for default, got %s", got)
	}

	// Empty supported
	if got := NearestLevel(LevelHigh, nil); got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

func TestFromModel(t *testing.T) {
	m := store.Model{
		Name: "o3-mini",
		Reasoning: &store.ReasoningConfig{
			Levels:  []string{LevelLow, LevelHigh},
			Default: LevelHigh,
		},
	}
	cfg, err := FromModel(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil || !reflect.DeepEqual(cfg.Levels, []string{LevelLow, LevelHigh}) || cfg.Default != LevelHigh {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}

	badModel := store.Model{
		Reasoning: &store.ReasoningConfig{
			Levels: []string{"invalid_level"},
		},
	}
	if _, err := FromModel(badModel); err == nil {
		t.Errorf("expected error for invalid level")
	}
}

func TestInferReasoning(t *testing.T) {
	cases := []struct {
		model    string
		expected bool
	}{
		{"o1-preview", true},
		{"o3-mini", true},
		{"deepseek-r1", true},
		{"deepseek-reasoner", true},
		{"qwq-32b", true},
		{"claude-3-7-sonnet", true},
		{"deepseek-flash", true},
		{"deepseek-v4-flash", true},
		{"deepseek-v4-pro", true},
		{"gemini-3.1-flash-lite", true},
		{"gemini-3.5-flash-lite", true},
		{"glm-5.3", true},
		{"glm-5.3-flash", true},
		// 中转平台前缀与量化后缀包装
		{"accounts/fireworks/models/deepseek-v4-flash:free", true},
		{"openrouter/openai/o3-mini", true},
		{"openrouter/google/gemini-3.5-flash-lite:nitro", true},
		{"custom-channel/glm-5.3-flash-int4", true},
		{"ollama/deepseek-r1:14b-q4_k_m", true},
		{"zai.glm-5.3", true},
		{"o1-preview (free)", true},
		// 未来代际泛化支持（代码未改动即自动支持未来代际）
		{"o5-mini", true},
		{"deepseek-v5-pro", true},
		{"glm-6.0", true},
		{"gemini-4-flash", true},
		// 通用语义线索与私有微调模型
		{"my-custom-model-cot", true},
		{"qwen-max-thinking", true},
		{"internal-service-reasoner", true},
		// 否定守卫
		{"deepseek-v4-non-reasoning", false},
		{"gemini-3-no-think", false},
		// 普通模型
		{"microsoft/microsoft-decision-1", false},
		{"gpt-4o", false},
		{"claude-3-5-sonnet", false},
	}

	for _, c := range cases {
		cfg := InferReasoning(c.model)
		if (cfg != nil) != c.expected {
			t.Errorf("model %q infer reasoning got %v, expected %v", c.model, cfg != nil, c.expected)
		}
	}
}
