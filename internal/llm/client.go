// Package llm provides the LLM client abstraction and its Gemini implementation.
// All HTTP calls to the Gemini REST API are contained here.
// To switch providers, implement LLMClient and inject the new impl into Pipeline.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// LLMClient is the interface for any language model backend.
// Implementations: GeminiClient (production), MockClient (tests).
type LLMClient interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// GeminiClient calls the Google Gemini REST API over plain net/http.
// No external SDK dependency — only stdlib is required.
type GeminiClient struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

// NewGeminiClient creates a client for the Gemini REST API.
// model defaults to "gemini-1.5-flash" if empty.
func NewGeminiClient(apiKey, model string) *GeminiClient {
	if model == "" {
		model = "gemini-1.5-flash"
	}
	return &GeminiClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://generativelanguage.googleapis.com/v1beta",
		http:    &http.Client{Timeout: 90 * time.Second},
	}
}

// --- internal Gemini API types ---

type geminiRequest struct {
	SystemInstruction *geminiContent    `json:"system_instruction,omitempty"`
	Contents          []geminiContent   `json:"contents"`
	GenerationConfig  *generationConfig `json:"generation_config,omitempty"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
	Role  string       `json:"role,omitempty"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type generationConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"max_output_tokens"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finish_reason"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends a request to the Gemini API and returns the model's text output.
// Temperature is fixed at 0.2 for deterministic, precise code analysis.
func (g *GeminiClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	req := geminiRequest{
		Contents: []geminiContent{
			{Role: "user", Parts: []geminiPart{{Text: userPrompt}}},
		},
		GenerationConfig: &generationConfig{
			Temperature:     0.2,
			MaxOutputTokens: 8192,
		},
	}
	if systemPrompt != "" {
		req.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("llm: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", g.baseURL, g.model, g.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("llm: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("llm: http call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("llm: read body: %w", err)
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(respBody, &gemResp); err != nil {
		return "", fmt.Errorf("llm: unmarshal response: %w", err)
	}
	if gemResp.Error != nil {
		return "", fmt.Errorf("llm: gemini API error %d: %s", gemResp.Error.Code, gemResp.Error.Message)
	}
	if len(gemResp.Candidates) == 0 || len(gemResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("llm: empty response from model")
	}

	return gemResp.Candidates[0].Content.Parts[0].Text, nil
}
