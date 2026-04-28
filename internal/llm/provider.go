package llm

import (
	"context"

	"pr-reviewer-ai/internal/llm/types"
)

// Re-export shared type aliases so callers use llm.ProviderGemini etc.
type ProviderName = types.ProviderName

const (
	ProviderGemini   = types.ProviderGemini
	ProviderGroq     = types.ProviderGroq
	ProviderDeepSeek = types.ProviderDeepSeek
	ProviderMistral  = types.ProviderMistral
	ProviderCerebras = types.ProviderCerebras
)

// QuotaError and IsQuota are re-exported from llm/types for convenience.
type QuotaError = types.QuotaError

func IsQuota(err error) bool { return types.IsQuota(err) }

// Provider extends LLMClient with provider identity and quota detection.
// Every concrete provider (Gemini, Groq, etc.) must implement this interface.
type Provider interface {
	// Complete satisfies LLMClient — used by pipeline stages.
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)

	// Analyze is the primary user-facing code analysis entry point.
	// codeContext contains surrounding file/repo context.
	Analyze(ctx context.Context, codeContext, prompt string) (string, error)

	// Name returns the provider identifier.
	Name() ProviderName

	// Model returns the exact model string sent to the API.
	Model() string

	// IsQuotaError returns true when err signals a rate-limit or daily quota hit.
	IsQuotaError(err error) bool
}
