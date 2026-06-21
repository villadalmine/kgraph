// Package llm provides a small pluggable LLM client. The default provider is
// OpenRouter (OpenAI-compatible), configured via environment variables, but the
// Provider interface keeps the rest of the code provider-agnostic (BYO LLM).
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// DefaultModel is a free OpenRouter model that reliably handles instruction
// following over structured context. Override with OPENROUTER_MODEL or --model.
const DefaultModel = "qwen/qwen3-next-80b-a3b-instruct:free"

// fallbackModels are tried in order when no model is explicitly chosen, so a
// transiently rate-limited free model doesn't fail the whole request.
var fallbackModels = []string{
	DefaultModel,
	"openai/gpt-oss-120b:free",
	"meta-llama/llama-3.3-70b-instruct:free",
}

const openRouterURL = "https://openrouter.ai/api/v1/chat/completions"

// Provider is the minimal LLM capability the rest of kgraph depends on.
type Provider interface {
	// Complete sends a system + user prompt and returns the assistant text.
	Complete(ctx context.Context, system, user string) (string, error)
	// Model reports the model identifier in use.
	Model() string
}

// OpenRouter implements Provider against the OpenRouter API.
type OpenRouter struct {
	apiKey string
	models []string // tried in order until one succeeds
	http   *http.Client
}

// New builds an OpenRouter provider. If model is empty it falls back to
// OPENROUTER_MODEL, then to a built-in chain of free models. The API key comes
// from OPENROUTER_API_KEY.
func New(model string) (*OpenRouter, error) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY is not set (export it; do not hardcode the key)")
	}
	if model == "" {
		model = os.Getenv("OPENROUTER_MODEL")
	}
	models := fallbackModels
	if model != "" {
		models = []string{model} // explicit choice: no fallback
	}
	return &OpenRouter{
		apiKey: key,
		models: models,
		http:   &http.Client{Timeout: 120 * time.Second},
	}, nil
}

// Model reports the primary model identifier.
func (o *OpenRouter) Model() string { return o.models[0] }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

// Complete tries each configured model (with a couple of retries on rate
// limits) and returns the first successful completion.
func (o *OpenRouter) Complete(ctx context.Context, system, user string) (string, error) {
	const retriesPerModel = 2
	var lastErr error
	for _, model := range o.models {
		for attempt := 0; attempt <= retriesPerModel; attempt++ {
			text, retryable, err := o.call(ctx, model, system, user)
			if err == nil {
				return text, nil
			}
			lastErr = err
			if !retryable {
				break // permanent error for this model; try next model
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(2*(attempt+1)) * time.Second):
			}
		}
	}
	return "", fmt.Errorf("all models failed: %w", lastErr)
}

// call performs a single request to one model. retryable is true for transient
// failures (rate limits / 5xx) worth retrying.
func (o *OpenRouter) call(ctx context.Context, model, system, user string) (text string, retryable bool, err error) {
	body, err := json.Marshal(chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0.2,
		MaxTokens:   2000,
	})
	if err != nil {
		return "", false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterURL, bytes.NewReader(body))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/villadalmine/kgraph")
	req.Header.Set("X-Title", "kgraph")

	resp, err := o.http.Do(req)
	if err != nil {
		return "", true, fmt.Errorf("openrouter request: %w", err)
	}
	defer resp.Body.Close()

	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return "", true, fmt.Errorf("decoding response: %w", err)
	}
	if cr.Error != nil {
		retry := cr.Error.Code == 429 || cr.Error.Code >= 500
		return "", retry, fmt.Errorf("model %s error (%d): %s", model, cr.Error.Code, cr.Error.Message)
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return "", true, fmt.Errorf("model %s http %s", model, resp.Status)
	}
	if len(cr.Choices) == 0 {
		return "", false, fmt.Errorf("model %s returned no choices (status %s)", model, resp.Status)
	}
	return cr.Choices[0].Message.Content, false, nil
}
