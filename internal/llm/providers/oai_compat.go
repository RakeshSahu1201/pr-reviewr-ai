// Package providers contains concrete LLM provider implementations.
// Each satisfies the llm.Provider interface.
// This package imports llm/types (shared primitives only) — not the parent llm
// package — so there is no circular dependency.
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	ltype "pr-reviewer-ai/internal/llm/types"
)

// ─────────────────────────────────────────────────────────
// OpenAI-Compatible Provider
// Works for: Groq, DeepSeek, Mistral, Cerebras
// ─────────────────────────────────────────────────────────

// OAICompatConfig holds the settings for one OAI-compatible provider.
type OAICompatConfig struct {
	Name    ltype.ProviderName
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

// Pre-built factory funcs for each free provider.
var (
	GroqConfig = func(apiKey string) OAICompatConfig {
		return OAICompatConfig{
			Name: ltype.ProviderGroq, BaseURL: "https://api.groq.com/openai/v1",
			APIKey: apiKey, Model: "llama-3.3-70b-versatile", Timeout: 60 * time.Second,
		}
	}
	DeepSeekConfig = func(apiKey string) OAICompatConfig {
		return OAICompatConfig{
			Name: ltype.ProviderDeepSeek, BaseURL: "https://api.deepseek.com/v1",
			APIKey: apiKey, Model: "deepseek-coder", Timeout: 90 * time.Second,
		}
	}
	MistralConfig = func(apiKey string) OAICompatConfig {
		return OAICompatConfig{
			Name: ltype.ProviderMistral, BaseURL: "https://api.mistral.ai/v1",
			APIKey: apiKey, Model: "codestral-latest", Timeout: 60 * time.Second,
		}
	}
	CerebrasConfig = func(apiKey string) OAICompatConfig {
		return OAICompatConfig{
			Name: ltype.ProviderCerebras, BaseURL: "https://api.cerebras.ai/v1",
			APIKey: apiKey, Model: "llama-3.3-70b", Timeout: 30 * time.Second,
		}
	}
)

// OAICompatProvider satisfies llm.Provider for any OpenAI-compatible endpoint.
type OAICompatProvider struct {
	cfg  OAICompatConfig
	http *http.Client
}

func NewOAICompatProvider(cfg OAICompatConfig) *OAICompatProvider {
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	return &OAICompatProvider{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout}}
}

func (p *OAICompatProvider) Name() ltype.ProviderName { return p.cfg.Name }
func (p *OAICompatProvider) Model() string            { return p.cfg.Model }

func (p *OAICompatProvider) Complete(ctx context.Context, system, user string) (string, error) {
	return p.call(ctx, system, user)
}

func (p *OAICompatProvider) Analyze(ctx context.Context, codeCtx, prompt string) (string, error) {
	return p.call(ctx,
		"You are a senior software engineer performing automated code review. Be precise, technical, and actionable.",
		fmt.Sprintf("Repository context:\n%s\n\nTask:\n%s", codeCtx, prompt),
	)
}

func (p *OAICompatProvider) IsQuotaError(err error) bool { return ltype.IsQuota(err) }

// ── HTTP call ────────────────────────────────────────────

type oaiReq struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	Temperature float64      `json:"temperature"`
	MaxTokens   int          `json:"max_tokens"`
}
type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type oaiResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (p *OAICompatProvider) call(ctx context.Context, system, user string) (string, error) {
	msgs := make([]oaiMessage, 0, 2)
	if system != "" {
		msgs = append(msgs, oaiMessage{Role: "system", Content: system})
	}
	msgs = append(msgs, oaiMessage{Role: "user", Content: user})

	body, _ := json.Marshal(oaiReq{Model: p.cfg.Model, Messages: msgs, Temperature: 0.2, MaxTokens: 8192})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("providers/%s: %w", p.cfg.Name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)

	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("providers/%s: http: %w", p.cfg.Name, err)
	}
	defer resp.Body.Close()

	if ltype.IsHTTP429(resp.StatusCode) {
		return "", &ltype.QuotaError{Provider: p.cfg.Name, Message: "HTTP 429 — daily quota or rate limit hit"}
	}

	raw, _ := io.ReadAll(resp.Body)
	var r oaiResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("providers/%s: unmarshal: %w", p.cfg.Name, err)
	}
	if r.Error != nil {
		if r.Error.Type == "tokens_limit" || r.Error.Type == "rate_limit_exceeded" {
			return "", &ltype.QuotaError{Provider: p.cfg.Name, Message: r.Error.Message}
		}
		return "", fmt.Errorf("providers/%s: API error: %s", p.cfg.Name, r.Error.Message)
	}
	if len(r.Choices) == 0 {
		return "", fmt.Errorf("providers/%s: empty response", p.cfg.Name)
	}
	return r.Choices[0].Message.Content, nil
}
