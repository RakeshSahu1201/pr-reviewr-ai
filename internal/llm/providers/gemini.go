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

// GeminiProvider satisfies llm.Provider using the Gemini REST API.
// Imports only llm/types — no cycle with the parent llm package.
type GeminiProvider struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

// NewGeminiProvider creates a Gemini-backed provider.
// Defaults to "gemini-1.5-pro" (best for large-context repo brainstorming).
func NewGeminiProvider(apiKey, model string) *GeminiProvider {
	if model == "" {
		model = "gemini-1.5-pro"
	}
	return &GeminiProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://generativelanguage.googleapis.com/v1beta",
		http:    &http.Client{Timeout: 90 * time.Second},
	}
}

func (g *GeminiProvider) Name() ltype.ProviderName    { return ltype.ProviderGemini }
func (g *GeminiProvider) Model() string               { return g.model }
func (g *GeminiProvider) IsQuotaError(err error) bool { return ltype.IsQuota(err) }

func (g *GeminiProvider) Complete(ctx context.Context, system, user string) (string, error) {
	return g.call(ctx, system, user)
}

func (g *GeminiProvider) Analyze(ctx context.Context, codeCtx, prompt string) (string, error) {
	return g.call(ctx,
		"You are a senior software engineer. Perform deep code analysis using the full repository context.",
		fmt.Sprintf("Repository Context:\n%s\n\nTask:\n%s", codeCtx, prompt),
	)
}

// ── Gemini REST types ────────────────────────────────────

type gemReq struct {
	SystemInstruction *gemContent  `json:"system_instruction,omitempty"`
	Contents          []gemContent `json:"contents"`
	GenerationConfig  *gemGenCfg   `json:"generation_config,omitempty"`
}
type gemContent struct {
	Parts []gemPart `json:"parts"`
	Role  string    `json:"role,omitempty"`
}
type gemPart struct {
	Text string `json:"text"`
}
type gemGenCfg struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"max_output_tokens"`
}
type gemResp struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

func (g *GeminiProvider) call(ctx context.Context, system, user string) (string, error) {
	req := gemReq{
		Contents:         []gemContent{{Role: "user", Parts: []gemPart{{Text: user}}}},
		GenerationConfig: &gemGenCfg{Temperature: 0.2, MaxOutputTokens: 8192},
	}
	if system != "" {
		req.SystemInstruction = &gemContent{Parts: []gemPart{{Text: system}}}
	}

	body, _ := json.Marshal(req)
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", g.baseURL, g.model, g.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("providers/gemini: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("providers/gemini: http: %w", err)
	}
	defer resp.Body.Close()

	if ltype.IsHTTP429(resp.StatusCode) {
		return "", &ltype.QuotaError{Provider: ltype.ProviderGemini, Message: "HTTP 429 — daily quota exceeded"}
	}

	raw, _ := io.ReadAll(resp.Body)
	var gr gemResp
	if err := json.Unmarshal(raw, &gr); err != nil {
		return "", fmt.Errorf("providers/gemini: unmarshal: %w", err)
	}
	if gr.Error != nil {
		if gr.Error.Status == "RESOURCE_EXHAUSTED" {
			return "", &ltype.QuotaError{Provider: ltype.ProviderGemini, Message: gr.Error.Message}
		}
		return "", fmt.Errorf("providers/gemini: API error %d: %s", gr.Error.Code, gr.Error.Message)
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("providers/gemini: empty response")
	}
	return gr.Candidates[0].Content.Parts[0].Text, nil
}
