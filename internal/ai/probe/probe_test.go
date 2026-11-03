package probe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"motty/internal/ai/llm"
	"motty/internal/ai/store"
)

func TestClassifyModel(t *testing.T) {
	cases := []struct {
		name     string
		expected Category
	}{
		{"gpt-4o", CategoryChat},
		{"deepseek-chat", CategoryChat},
		{"claude-3-5-sonnet-20241022", CategoryChat},
		{"deepseek-ai/DeepSeek-V3", CategoryChat},
		{"llama-3.3-70b-instruct", CategoryChat},

		// Reasoning
		{"deepseek-reasoner", CategoryReasoning},
		{"deepseek-ai/DeepSeek-R1", CategoryReasoning},
		{"o1-preview", CategoryReasoning},
		{"o1-mini", CategoryReasoning},
		{"o3-mini", CategoryReasoning},
		{"qwq-32b-preview", CategoryReasoning},

		// Embedding
		{"text-embedding-3-small", CategoryEmbedding},
		{"BAAI/bge-large-zh-v1.5", CategoryEmbedding},
		{"thenlper/gte-large-zh", CategoryEmbedding},
		{"intfloat/e5-large-v2", CategoryEmbedding},

		// Rerank
		{"BAAI/bge-rerank-v2-m3", CategoryRerank},
		{"rerank-multilingual-v3.0", CategoryRerank},

		// Media
		{"dall-e-3", CategoryMedia},
		{"stabilityai/stable-diffusion-3-medium", CategoryMedia},
		{"black-forest-labs/FLUX.1-schnell", CategoryMedia},

		// Audio
		{"whisper-large-v3", CategoryAudio},
		{"tts-1", CategoryAudio},
		{"FunAudioLLM/CosyVoice2-0.5B", CategoryAudio},
	}

	for _, tc := range cases {
		actual := ClassifyModel(tc.name)
		if actual != tc.expected {
			t.Errorf("ClassifyModel(%q) = %v; want %v", tc.name, actual, tc.expected)
		}
	}
}

type mockLLMClient struct {
	events []llm.Event
	err    error
}

func (m *mockLLMClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan llm.Event, len(m.events))
	go func() {
		defer close(ch)
		for _, ev := range m.events {
			select {
			case <-ctx.Done():
				return
			case ch <- ev:
			}
		}
	}()
	return ch, nil
}

func (m *mockLLMClient) Models(ctx context.Context) ([]string, error) {
	return nil, nil
}

func TestProbeModelChatStreamFirstToken(t *testing.T) {
	mockClient := &mockLLMClient{
		events: []llm.Event{
			{Kind: llm.EventReasoning, Text: "thinking..."},
			{Kind: llm.EventText, Text: "Hello"},
			{Kind: llm.EventDone},
		},
	}

	res := ProbeModel(context.Background(), store.Provider{}, "deepseek-ai/DeepSeek-R1", mockClient, 0)
	if res.Status != StatusSuccess {
		t.Fatalf("expected StatusSuccess, got %v (err: %s)", res.Status, res.Error)
	}
	if res.Latency <= 0 {
		t.Errorf("expected Latency > 0, got %v", res.Latency)
	}
}

func TestProbeModelMediaSkipped(t *testing.T) {
	res := ProbeModel(context.Background(), store.Provider{}, "dall-e-3", nil, 0)
	if res.Status != StatusSkipped {
		t.Fatalf("expected StatusSkipped, got %v", res.Status)
	}
	if res.Reason != "generation_cost" {
		t.Errorf("expected reason 'generation_cost', got %q", res.Reason)
	}
}

func TestProbeOllama(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			http.NotFound(w, r)
			return
		}
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["name"] == "llama3:latest" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"modelfile": "#..."}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "model not found"}`))
	}))
	defer ts.Close()

	provider := store.Provider{
		Name:    "Ollama",
		BaseURL: ts.URL,
	}

	// Success case
	res := ProbeModel(context.Background(), provider, "llama3:latest", nil, 0)
	if res.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess for llama3, got %v (err: %s)", res.Status, res.Error)
	}

	// Failed case
	resFail := ProbeModel(context.Background(), provider, "not-exist:latest", nil, 0)
	if resFail.Status != StatusFailed {
		t.Errorf("expected StatusFailed for not-exist, got %v", resFail.Status)
	}

	// The preset names the OpenAI-compatible path, which the native API does
	// not live under: the probe still reaches the server root.
	preset := store.Provider{Name: "Ollama", BaseURL: ts.URL + "/v1"}
	if res := ProbeModel(context.Background(), preset, "llama3:latest", nil, 0); res.Status != StatusSuccess {
		t.Errorf("the preset address probed %v (err: %s), want success", res.Status, res.Error)
	}
}

func TestProbeEmbedding(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1, 0.2]}]}`))
	}))
	defer ts.Close()

	provider := store.Provider{
		BaseURL: ts.URL,
	}

	res := ProbeModel(context.Background(), provider, "text-embedding-3-small", nil, 0)
	if res.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess for embedding, got %v (err: %s)", res.Status, res.Error)
	}
}
