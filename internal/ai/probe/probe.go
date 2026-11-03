package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"motty/internal/ai/llm"
	"motty/internal/ai/store"
)

// Category classifies the capability and intended workload of a model.
type Category uint8

const (
	CategoryChat Category = iota
	CategoryReasoning
	CategoryEmbedding
	CategoryRerank
	CategoryMedia
	CategoryAudio
)

// String returns a human-readable identifier for the category.
func (c Category) String() string {
	switch c {
	case CategoryReasoning:
		return "reasoning"
	case CategoryEmbedding:
		return "embedding"
	case CategoryRerank:
		return "rerank"
	case CategoryMedia:
		return "media"
	case CategoryAudio:
		return "audio"
	default:
		return "chat"
	}
}

// Status is the health state of a probed model.
type Status uint8

const (
	StatusIdle Status = iota
	StatusRunning
	StatusSuccess
	StatusFailed
	StatusSkipped
)

// Result is the outcome of a probe run.
type Result struct {
	Status    Status
	Latency   time.Duration
	Error     string
	Reason    string
	Timestamp time.Time
}

// ClassifyModel determines the capability category of a model from its name.
func ClassifyModel(name string) Category {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return CategoryChat
	}

	// 1. Audio / Speech models
	for _, kw := range [...]string{"whisper", "tts", "speech", "cosyvoice", "voice", "audio", "transcribe"} {
		if strings.Contains(lower, kw) {
			return CategoryAudio
		}
	}

	// 2. Image / Video media models (high cost probe)
	for _, kw := range [...]string{"dall-e", "midjourney", "flux", "stable-diffusion", "sdxl", "imagen", "cogview", "video"} {
		if strings.Contains(lower, kw) {
			return CategoryMedia
		}
	}

	// 3. Rerank models
	for _, kw := range [...]string{"rerank", "bge-rerank"} {
		if strings.Contains(lower, kw) {
			return CategoryRerank
		}
	}

	// 4. Embedding models
	for _, kw := range [...]string{"embed", "bge-", "gte-", "e5-", "text-embedding"} {
		if strings.Contains(lower, kw) {
			return CategoryEmbedding
		}
	}

	// 5. Reasoning / Deep thinking models
	for _, kw := range [...]string{"deepseek-reasoner", "reasoning", "qwq", "thinking"} {
		if strings.Contains(lower, kw) {
			return CategoryReasoning
		}
	}
	// Special short tokens: check whole parts or boundaries to avoid false positives
	for _, part := range strings.FieldsFunc(lower, func(r rune) bool {
		return r == '-' || r == '_' || r == '/' || r == ':' || r == '.'
	}) {
		if part == "o1" || part == "o3" || part == "r1" {
			return CategoryReasoning
		}
	}

	return CategoryChat
}

// IsChatOrReasoningModel reports whether the model is suitable for chat and reasoning.
func IsChatOrReasoningModel(name string) bool {
	cat := ClassifyModel(name)
	return cat == CategoryChat || cat == CategoryReasoning
}

// IsOllamaProvider reports whether the provider targets a local Ollama instance.
func IsOllamaProvider(provider store.Provider) bool {
	return strings.Contains(provider.BaseURL, "11434") || strings.EqualFold(provider.Name, "ollama")
}

// ProbeModel tests whether a model endpoint is responsive and measures its latency.
func ProbeModel(ctx context.Context, provider store.Provider, modelName string, client llm.Provider, timeout time.Duration) Result {
	cat := ClassifyModel(modelName)
	if cat == CategoryMedia {
		return Result{
			Status:    StatusSkipped,
			Reason:    "generation_cost",
			Timestamp: time.Now(),
		}
	}
	if cat == CategoryAudio {
		return Result{
			Status:    StatusSkipped,
			Reason:    "unsupported_probe",
			Timestamp: time.Now(),
		}
	}

	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	// Bound overall probe execution if not already constrained.
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()

	// Special check for Ollama: avoid loading entire model weights into VRAM.
	if IsOllamaProvider(provider) {
		latency, err := probeOllama(probeCtx, provider, modelName, timeout)
		if err != nil {
			return Result{Status: StatusFailed, Error: err.Error(), Timestamp: time.Now()}
		}
		return Result{Status: StatusSuccess, Latency: latency, Timestamp: time.Now()}
	}

	switch cat {
	case CategoryEmbedding:
		latency, err := probeEmbedding(probeCtx, provider, modelName, timeout)
		if err != nil {
			return Result{Status: StatusFailed, Error: err.Error(), Timestamp: time.Now()}
		}
		return Result{Status: StatusSuccess, Latency: latency, Timestamp: time.Now()}

	case CategoryRerank:
		latency, err := probeRerank(probeCtx, provider, modelName, timeout)
		if err != nil {
			return Result{Status: StatusFailed, Error: err.Error(), Timestamp: time.Now()}
		}
		return Result{Status: StatusSuccess, Latency: latency, Timestamp: time.Now()}

	default: // CategoryChat, CategoryReasoning
		if client == nil {
			return Result{Status: StatusFailed, Error: "no client available", Timestamp: time.Now()}
		}

		req := llm.Request{
			Model:     modelName,
			Messages:  []llm.Message{{Role: llm.RoleUser, Text: "hi"}},
			MaxTokens: 1,
		}

		events, err := client.Stream(probeCtx, req)
		if err != nil {
			return Result{Status: StatusFailed, Error: err.Error(), Timestamp: time.Now()}
		}

		// Stream until first meaningful token/reasoning event or error.
		// On first token, cancel probeCtx immediately to save quota and record latency.
		for ev := range events {
			switch ev.Kind {
			case llm.EventText, llm.EventReasoning:
				latency := time.Since(start)
				cancel() // Interrupt immediately
				return Result{Status: StatusSuccess, Latency: latency, Timestamp: time.Now()}
			case llm.EventError:
				errStr := "probe stream error"
				if ev.Err != nil {
					errStr = ev.Err.Error()
				}
				return Result{Status: StatusFailed, Error: errStr, Timestamp: time.Now()}
			case llm.EventDone:
				latency := time.Since(start)
				return Result{Status: StatusSuccess, Latency: latency, Timestamp: time.Now()}
			}
		}

		if probeCtx.Err() != nil && ctx.Err() == nil {
			return Result{Status: StatusFailed, Error: "probe timed out", Timestamp: time.Now()}
		}
		return Result{Status: StatusFailed, Error: "empty stream response", Timestamp: time.Now()}
	}
}

func probeOllama(ctx context.Context, provider store.Provider, modelName string, timeout time.Duration) (time.Duration, error) {
	start := time.Now()
	url := ollamaAPIBase(provider.BaseURL) + "/api/show"
	bodyBytes, _ := json.Marshal(map[string]string{"name": modelName})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return time.Since(start), nil
}

// ollamaAPIBase is the server root of an Ollama endpoint. The presets name it
// with the OpenAI-compatible path (/v1), which its native API does not live
// under, so the scheme and host are kept and everything below them dropped.
func ollamaAPIBase(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		return "http://localhost:11434"
	}
	parsed, err := neturl.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return base
	}
	return parsed.Scheme + "://" + parsed.Host
}

func probeEmbedding(ctx context.Context, provider store.Provider, modelName string, timeout time.Duration) (time.Duration, error) {
	start := time.Now()
	base := strings.TrimRight(provider.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	if !strings.HasSuffix(base, "/v1") && !strings.Contains(base, "/v1/") {
		base += "/v1"
	}
	url := base + "/embeddings"
	payload := map[string]any{
		"model": modelName,
		"input": "ping",
	}
	bodyBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return time.Since(start), nil
}

func probeRerank(ctx context.Context, provider store.Provider, modelName string, timeout time.Duration) (time.Duration, error) {
	start := time.Now()
	base := strings.TrimRight(provider.BaseURL, "/")
	if base == "" {
		base = "https://api.siliconflow.cn/v1"
	}
	url := base + "/rerank"
	payload := map[string]any{
		"model":     modelName,
		"query":     "test",
		"documents": []string{"test"},
	}
	bodyBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return time.Since(start), nil
}
