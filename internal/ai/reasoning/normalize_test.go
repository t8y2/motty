package reasoning

import "testing"

func TestCanonicalBaseName(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"deepseek-r1", "deepseek-r1"},
		{"accounts/fireworks/models/deepseek-v4-flash", "deepseek-v4-flash"},
		{"openrouter/openai/o3-mini:free", "o3-mini"},
		{"openrouter/google/gemini-3.5-flash-lite:nitro", "gemini-3.5-flash-lite"},
		{"ollama/deepseek-r1:14b-q4_k_m", "deepseek-r1"},
		{"custom-channel/glm-5.3-flash-int4", "glm-5.3-flash"},
		{"glm-5.3-flash-fp16", "glm-5.3-flash"},
		{"zai.glm-5.3", "glm-5.3"},
		{"o1-preview (free)", "o1-preview"},
	}

	for _, c := range cases {
		actual := CanonicalBaseName(c.input)
		if actual != c.expected {
			t.Errorf("CanonicalBaseName(%q) = %q, expected %q", c.input, actual, c.expected)
		}
	}
}
